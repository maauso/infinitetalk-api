package generator

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/maauso/infinitetalk-api/internal/beam"
	"github.com/maauso/infinitetalk-api/internal/media"
)

// Static errors for beam adapter.
var (
	// ErrProcessorRequired is returned when V2V mode is enabled but processor is not set.
	ErrProcessorRequired = errors.New("V2V mode requires media processor")
)

// BeamAdapter adapts the Beam client to the Generator interface.
type BeamAdapter struct {
	client    beam.Client
	processor media.Processor
}

// NewBeamAdapter creates a new Beam generator adapter.
// The processor parameter is optional - if nil, V2V mode will not be supported.
func NewBeamAdapter(client beam.Client) *BeamAdapter {
	return &BeamAdapter{
		client:    client,
		processor: nil,
	}
}

// WithProcessor sets the media processor for V2V support.
func (a *BeamAdapter) WithProcessor(processor media.Processor) *BeamAdapter {
	a.processor = processor
	return a
}

// Submit sends a lip-sync task to Beam.
// If opts.LongVideo is true, it generates an intermediate video from the image before submission.
func (a *BeamAdapter) Submit(ctx context.Context, imageB64, audioB64 string, opts SubmitOptions) (string, error) {
	// If not long_video mode, use traditional I2V flow
	if !opts.LongVideo {
		beamOpts := beam.SubmitOptions{
			Prompt:       opts.Prompt,
			Width:        opts.Width,
			Height:       opts.Height,
			ForceOffload: opts.ForceOffload,
		}
		taskID, err := a.client.Submit(ctx, imageB64, audioB64, beamOpts)
		if err != nil {
			return "", fmt.Errorf("beam adapter submit: %w", err)
		}
		return taskID, nil
	}

	// V2V mode: generate intermediate video
	if a.processor == nil {
		return "", ErrProcessorRequired
	}

	// Create temp directory for processing
	tempDir, err := os.MkdirTemp("", "beam-v2v-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	// Step 1: Decode audio to temp file to get duration
	audioData, err := base64.StdEncoding.DecodeString(audioB64)
	if err != nil {
		return "", fmt.Errorf("decode audio base64: %w", err)
	}
	audioPath := filepath.Join(tempDir, "audio.wav")
	if err := os.WriteFile(audioPath, audioData, 0600); err != nil {
		return "", fmt.Errorf("write audio file: %w", err)
	}

	// Step 2: Get audio duration
	duration, err := a.processor.GetMediaDuration(ctx, audioPath)
	if err != nil {
		return "", fmt.Errorf("get audio duration: %w", err)
	}

	// Step 3: Decode image to temp file
	imageData, err := base64.StdEncoding.DecodeString(imageB64)
	if err != nil {
		return "", fmt.Errorf("decode image base64: %w", err)
	}
	imagePath := filepath.Join(tempDir, "image.png")
	if err := os.WriteFile(imagePath, imageData, 0600); err != nil {
		return "", fmt.Errorf("write image file: %w", err)
	}

	// Step 4: Generate moving video matching audio duration
	videoPath := filepath.Join(tempDir, "intermediate.mp4")
	if err := a.processor.GenerateMovingVideo(ctx, imagePath, videoPath, duration, opts.Width, opts.Height); err != nil {
		return "", fmt.Errorf("generate moving video: %w", err)
	}

	// Step 5: Encode video to base64
	videoData, err := os.ReadFile(videoPath) // #nosec G304 - videoPath is constructed internally
	if err != nil {
		return "", fmt.Errorf("read video file: %w", err)
	}
	videoB64 := base64.StdEncoding.EncodeToString(videoData)

	// Step 6: Submit V2V request to Beam with custom payload
	// We need to construct the request manually since the client doesn't support V2V
	beamOpts := beam.SubmitOptions{
		Prompt:       opts.Prompt,
		Width:        opts.Width,
		Height:       opts.Height,
		ForceOffload: opts.ForceOffload,
		LongVideo:    true,
		MaxFrame:     1000, // As specified in requirements
	}

	// Use a special internal method to submit V2V
	taskID, err := a.submitV2V(ctx, videoB64, audioB64, beamOpts)
	if err != nil {
		return "", fmt.Errorf("beam adapter v2v submit: %w", err)
	}

	return taskID, nil
}

// submitV2V submits a V2V task to Beam.
// This is an internal method that directly constructs the V2V payload.
func (a *BeamAdapter) submitV2V(ctx context.Context, videoB64, audioB64 string, opts beam.SubmitOptions) (string, error) {
	taskID, err := a.client.SubmitV2V(ctx, videoB64, audioB64, opts)
	if err != nil {
		return "", fmt.Errorf("submit v2v to beam: %w", err)
	}
	return taskID, nil
}

// Poll checks the status of a Beam task.
func (a *BeamAdapter) Poll(ctx context.Context, taskID string) (PollResult, error) {
	result, err := a.client.Poll(ctx, taskID)
	if err != nil {
		return PollResult{}, fmt.Errorf("beam adapter poll: %w", err)
	}

	// Map Beam status to common status
	var status Status
	switch result.Status {
	case beam.StatusPending:
		status = StatusPending
	case beam.StatusRunning:
		status = StatusRunning
	case beam.StatusCompleted, beam.StatusComplete:
		status = StatusCompleted
	case beam.StatusFailed, beam.StatusError:
		status = StatusFailed
	case beam.StatusCanceled:
		status = StatusCancelled
	default:
		status = Status(result.Status)
	}

	return PollResult{
		Status:   status,
		VideoURL: result.OutputURL,
		Error:    result.Error,
	}, nil
}

// DownloadOutput downloads the video from the Beam output URL.
func (a *BeamAdapter) DownloadOutput(ctx context.Context, outputURL, destPath string) error {
	if err := a.client.DownloadOutput(ctx, outputURL, destPath); err != nil {
		return fmt.Errorf("beam adapter download: %w", err)
	}
	return nil
}

// Compile-time check that BeamAdapter implements Generator.
var _ Generator = (*BeamAdapter)(nil)
