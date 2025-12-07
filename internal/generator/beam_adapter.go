package generator

import (
	"context"
	"fmt"

	"github.com/maauso/infinitetalk-api/internal/beam"
)

// BeamAdapter adapts the Beam client to the Generator interface.
type BeamAdapter struct {
	client beam.Client
}

// NewBeamAdapter creates a new Beam generator adapter.
func NewBeamAdapter(client beam.Client) *BeamAdapter {
	return &BeamAdapter{client: client}
}

// Submit sends a lip-sync task to Beam.
func (a *BeamAdapter) Submit(ctx context.Context, imageB64, audioB64 string, opts SubmitOptions) (string, error) {
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
	case beam.StatusFailed:
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
