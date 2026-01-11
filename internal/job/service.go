// Package job provides the ProcessVideoService use case for orchestrating
// lip-sync video generation.
package job

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/maauso/infinitetalk-api/internal/audio"
	"github.com/maauso/infinitetalk-api/internal/beam"
	"github.com/maauso/infinitetalk-api/internal/generator"
	"github.com/maauso/infinitetalk-api/internal/media"
	"github.com/maauso/infinitetalk-api/internal/runpod"
	"github.com/maauso/infinitetalk-api/internal/storage"
)

// Static errors for job service operations.
var (
	// ErrRunPodJobFailed is returned when a RunPod job fails.
	ErrRunPodJobFailed = errors.New("RunPod job failed")
	// ErrRunPodJobCancelled is returned when a RunPod job is cancelled.
	ErrRunPodJobCancelled = errors.New("RunPod job cancelled")
	// ErrRunPodJobTimedOut is returned when a RunPod job times out.
	ErrRunPodJobTimedOut = errors.New("RunPod job timed out")
	// ErrInvalidProvider is returned when an invalid provider is specified.
	ErrInvalidProvider = errors.New("invalid provider")
	// ErrBeamClientNotInitialized is returned when Beam provider is requested but client is not initialized.
	ErrBeamClientNotInitialized = errors.New("beam client not initialized")
	// ErrNoVideoOutput is returned when provider returns neither base64 nor URL.
	ErrNoVideoOutput = errors.New("no video output from provider")
	// ErrProviderJobFailed is returned when provider job fails.
	ErrProviderJobFailed = errors.New("provider job failed")
	// ErrProviderJobCancelled is returned when provider job is cancelled.
	ErrProviderJobCancelled = errors.New("provider job cancelled")
	// ErrProviderJobTimedOut is returned when provider job times out.
	ErrProviderJobTimedOut = errors.New("provider job timed out")
)

// ProcessVideoInput contains the input parameters for video processing.
type ProcessVideoInput struct {
	// ImageBase64 is the base64-encoded source image.
	ImageBase64 string
	// AudioBase64 is the base64-encoded source audio.
	AudioBase64 string
	// Width is the target video width.
	Width int
	// Height is the target video height.
	Height int
	// Prompt is the text prompt for video generation.
	Prompt string
	// Provider is the video generation provider ("runpod" or "beam").
	Provider string
	// PushToS3 indicates whether to upload the final video to S3.
	PushToS3 bool
	// DryRun skips RunPod calls and completes after preprocessing.
	DryRun bool
	// ForceOffload forces offload on the provider. Defaults to true if not specified.
	ForceOffload bool
	// LongVideo enables V2V mode for long videos (Beam only).
	LongVideo bool
}

// ProcessVideoOutput contains the result of video processing.
type ProcessVideoOutput struct {
	// JobID is the unique identifier for the created job.
	JobID string
	// Status is the final job status.
	Status Status
	// VideoPath is the local path to the output video (if not pushed to S3).
	VideoPath string
	// VideoURL is the S3 URL of the output video (if pushed to S3).
	VideoURL string
	// Error contains any error message if processing failed.
	Error string
}

// ProcessVideoService orchestrates the video processing workflow.
// It coordinates between media processing, audio splitting, RunPod integration,
// and storage to produce a lip-sync video.
type ProcessVideoService struct {
	repo       Repository
	processor  media.Processor
	splitter   audio.Splitter
	runpod     runpod.Client
	beamClient beam.Client
	storage    storage.Storage
	logger     *slog.Logger
	// splitOpts configures audio splitting behavior.
	splitOpts audio.SplitOpts
	// pollInterval is the duration between RunPod status polls.
	pollInterval time.Duration
}

// ServiceOption is a function that configures a ProcessVideoService.
type ServiceOption func(*ProcessVideoService)

// WithSplitOpts sets the audio splitting options.
func WithSplitOpts(opts audio.SplitOpts) ServiceOption {
	return func(s *ProcessVideoService) {
		s.splitOpts = opts
	}
}

// WithPollInterval sets the polling interval for RunPod status checks.
func WithPollInterval(d time.Duration) ServiceOption {
	return func(s *ProcessVideoService) {
		if d > 0 {
			s.pollInterval = d
		}
	}
}

// NewProcessVideoService creates a new ProcessVideoService with all dependencies.
func NewProcessVideoService(
	repo Repository,
	processor media.Processor,
	splitter audio.Splitter,
	runpodClient runpod.Client,
	beamClient beam.Client,
	storageClient storage.Storage,
	logger *slog.Logger,
	opts ...ServiceOption,
) *ProcessVideoService {
	if logger == nil {
		logger = slog.Default()
	}
	s := &ProcessVideoService{
		repo:         repo,
		processor:    processor,
		splitter:     splitter,
		runpod:       runpodClient,
		beamClient:   beamClient,
		storage:      storageClient,
		logger:       logger,
		splitOpts:    audio.DefaultSplitOpts(),
		pollInterval: 5 * time.Second,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// getGenerator returns the appropriate generator based on the provider.
func (s *ProcessVideoService) getGenerator(provider Provider) (generator.Generator, error) {
	switch provider {
	case ProviderRunPod:
		return generator.NewRunPodAdapter(s.runpod), nil
	case ProviderBeam:
		if s.beamClient == nil {
			return nil, ErrBeamClientNotInitialized
		}
		return generator.NewBeamAdapter(s.beamClient).WithProcessor(s.processor), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidProvider, provider)
	}
}

// CreateJob creates a new job and persists it to the repository.
// The job is created in IN_QUEUE status, ready for processing.
//
// Note: ImageBase64 and AudioBase64 from input are not stored directly in the Job.
// They will be decoded and saved as files during processing, and the
// resulting file paths will be stored in InputImagePath and InputAudioPath.
func (s *ProcessVideoService) CreateJob(ctx context.Context, input ProcessVideoInput) (*Job, error) {
	job := New()
	job.Width = input.Width
	job.Height = input.Height
	job.PushToS3 = input.PushToS3

	// Set prompt (default to "A person talking naturally" if not provided)
	if input.Prompt == "" {
		job.Prompt = "high quality, realistic, speaking naturally"
	} else {
		job.Prompt = input.Prompt
	}

	// Set provider (default to runpod if empty)
	if input.Provider == "" {
		job.Provider = ProviderRunPod
	} else {
		job.Provider = Provider(input.Provider)
	}

	// Validate provider
	if !job.Provider.IsValid() {
		return nil, fmt.Errorf("%w: %s", ErrInvalidProvider, input.Provider)
	}

	s.logger.Info("creating new job",
		slog.String("job_id", job.ID),
		slog.String("provider", string(job.Provider)),
		slog.String("prompt", job.Prompt),
		slog.Int("width", input.Width),
		slog.Int("height", input.Height),
		slog.Bool("push_to_s3", input.PushToS3),
		slog.Bool("force_offload", input.ForceOffload),
	)

	if err := s.repo.Save(ctx, job); err != nil {
		s.logger.Error("failed to save job",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("save job: %w", err)
	}

	return job, nil
}

// GetJob retrieves a job by ID.
func (s *ProcessVideoService) GetJob(ctx context.Context, id string) (*Job, error) {
	job, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find job: %w", err)
	}
	return job, nil
}

// ProcessExistingJob executes the video processing workflow for an existing job.
// This is used when the job has already been created and needs to be processed.
func (s *ProcessVideoService) ProcessExistingJob(ctx context.Context, jobID string, input ProcessVideoInput) (*ProcessVideoOutput, error) {
	// Retrieve the existing job
	job, err := s.repo.FindByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("find job: %w", err)
	}

	return s.processJob(ctx, job, input)
}

// Process executes the complete video processing workflow.
//
// The workflow:
//  1. Create Job in repository
//  2. Decode and save input files (image and audio)
//  3. Use media.Processor to resize image → base64
//  4. Use audio.Splitter to split audio → chunks
//  5. For each chunk in parallel: submit to RunPod, poll, save partial video
//  6. Use media.Processor to join videos
//  7. Update Job to COMPLETED or FAILED
//  8. Optionally push to S3
func (s *ProcessVideoService) Process(ctx context.Context, input ProcessVideoInput) (*ProcessVideoOutput, error) {
	// Create and persist job
	job, err := s.CreateJob(ctx, input)
	if err != nil {
		return nil, err
	}

	return s.processJob(ctx, job, input)
}

// processJob executes the video processing workflow for the given job.
func (s *ProcessVideoService) processJob(ctx context.Context, job *Job, input ProcessVideoInput) (*ProcessVideoOutput, error) {
	// Get appropriate generator for the provider
	gen, err := s.getGenerator(job.Provider)
	if err != nil {
		return s.failJob(ctx, job, err.Error())
	}

	// Track temporary files for cleanup
	var tempFiles []string
	defer func() { //nolint:contextcheck // Using context.Background() intentionally for cleanup
		if len(tempFiles) > 0 {
			// Cleanup should happen even after the original context is cancelled
			if cleanupErr := s.storage.CleanupTemp(context.Background(), tempFiles); cleanupErr != nil {
				s.logger.Warn("failed to cleanup temp files",
					slog.String("job_id", job.ID),
					slog.String("error", cleanupErr.Error()),
				)
			}
		}
	}()

	// Transition to RUNNING state
	if err := job.Start(); err != nil {
		s.logger.Error("failed to start job",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return s.failJob(ctx, job, fmt.Sprintf("failed to start job: %v", err))
	}
	if err := s.repo.Save(ctx, job); err != nil {
		return nil, fmt.Errorf("save job: %w", err)
	}

	s.logger.Info("job started, processing video",
		slog.String("job_id", job.ID),
		slog.String("provider", string(job.Provider)),
	)

	// Step 1: Decode and save input image
	imagePath, err := s.saveBase64ToTemp(ctx, input.ImageBase64, "image.png")
	if err != nil {
		s.logger.Error("failed to save image",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return s.failJob(ctx, job, fmt.Sprintf("failed to save image: %v", err))
	}
	tempFiles = append(tempFiles, imagePath)
	job.InputImagePath = imagePath

	// Step 2: Decode and save input audio
	audioPath, err := s.saveBase64ToTemp(ctx, input.AudioBase64, "audio.wav")
	if err != nil {
		s.logger.Error("failed to save audio",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return s.failJob(ctx, job, fmt.Sprintf("failed to save audio: %v", err))
	}
	tempFiles = append(tempFiles, audioPath)
	job.InputAudioPath = audioPath

	s.logger.Info("input files saved",
		slog.String("job_id", job.ID),
		slog.String("image_path", imagePath),
		slog.String("audio_path", audioPath),
	)

	// Step 3: Resize image with padding
	resizedImagePath := filepath.Join(filepath.Dir(imagePath), fmt.Sprintf("resized_%s.png", job.ID))
	if err := s.processor.ResizeImageWithPadding(ctx, imagePath, resizedImagePath, input.Width, input.Height); err != nil {
		s.logger.Error("failed to resize image",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return s.failJob(ctx, job, fmt.Sprintf("failed to resize image: %v", err))
	}
	tempFiles = append(tempFiles, resizedImagePath)

	// Read resized image as base64
	resizedImageB64, err := s.fileToBase64(resizedImagePath)
	if err != nil {
		s.logger.Error("failed to encode resized image",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return s.failJob(ctx, job, fmt.Sprintf("failed to encode resized image: %v", err))
	}

	s.logger.Info("image resized",
		slog.String("job_id", job.ID),
		slog.Int("width", input.Width),
		slog.Int("height", input.Height),
	)

	// Step 4: Split audio into chunks
	outputDir := filepath.Dir(audioPath)
	audioChunks, err := s.splitter.Split(ctx, audioPath, outputDir, s.splitOpts)
	if err != nil {
		s.logger.Error("failed to split audio",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return s.failJob(ctx, job, fmt.Sprintf("failed to split audio: %v", err))
	}
	tempFiles = append(tempFiles, audioChunks...)

	s.logger.Info("audio split into chunks",
		slog.String("job_id", job.ID),
		slog.Int("chunk_count", len(audioChunks)),
	)

	// Initialize chunks in job
	chunks := make([]Chunk, len(audioChunks))
	for i, chunkPath := range audioChunks {
		chunks[i] = Chunk{
			ID:        fmt.Sprintf("%s-chunk-%d", job.ID, i),
			Index:     i,
			Status:    ChunkStatusPending,
			InputPath: chunkPath,
		}
	}
	job.SetChunks(chunks)
	if err := s.repo.Save(ctx, job); err != nil {
		return nil, fmt.Errorf("save job: %w", err)
	}

	// Dry-run mode: skip provider processing and complete immediately
	if input.DryRun {
		s.logger.Info("dry-run mode: skipping provider processing",
			slog.String("job_id", job.ID),
			slog.String("provider", string(job.Provider)),
			slog.Int("chunk_count", len(audioChunks)),
		)
		job.UpdateProgress(100)
		if err := job.Complete(); err != nil {
			return nil, fmt.Errorf("complete job: %w", err)
		}
		if err := s.repo.Save(ctx, job); err != nil {
			return nil, fmt.Errorf("save job: %w", err)
		}
		return &ProcessVideoOutput{
			JobID:  job.ID,
			Status: job.Status,
		}, nil
	}

	// Step 5: Process chunks sequentially with frame continuity
	videoPaths, err := s.processChunksSequential(ctx, job, gen, resizedImageB64, audioChunks, input.Width, input.Height, input.ForceOffload, input.LongVideo)
	if err != nil {
		s.logger.Error("failed to process chunks",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return s.failJob(ctx, job, err.Error())
	}
	tempFiles = append(tempFiles, videoPaths...)

	s.logger.Info("all chunks processed",
		slog.String("job_id", job.ID),
		slog.Int("video_count", len(videoPaths)),
	)

	// Step 6: Join videos
	outputVideoPath := filepath.Join(outputDir, fmt.Sprintf("output_%s.mp4", job.ID))
	if err := s.processor.JoinVideos(ctx, videoPaths, outputVideoPath); err != nil {
		s.logger.Error("failed to join videos",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return s.failJob(ctx, job, fmt.Sprintf("failed to join videos: %v", err))
	}

	s.logger.Info("videos joined",
		slog.String("job_id", job.ID),
		slog.String("output_path", outputVideoPath),
	)

	// Step 7: Optional S3 upload
	var videoURL string
	if input.PushToS3 {
		videoFile, err := os.Open(outputVideoPath) // #nosec G304 - outputVideoPath is constructed internally
		if err != nil {
			s.logger.Error("failed to open output video for S3 upload",
				slog.String("job_id", job.ID),
				slog.String("error", err.Error()),
			)
			return s.failJob(ctx, job, fmt.Sprintf("failed to open output video: %v", err))
		}
		defer func() { _ = videoFile.Close() }()

		s3Key := fmt.Sprintf("videos/%s.mp4", job.ID)
		videoURL, err = s.storage.UploadToS3(ctx, s3Key, videoFile)
		if err != nil {
			s.logger.Error("failed to upload to S3",
				slog.String("job_id", job.ID),
				slog.String("error", err.Error()),
			)
			return s.failJob(ctx, job, fmt.Sprintf("failed to upload to S3: %v", err))
		}

		s.logger.Info("video uploaded to S3",
			slog.String("job_id", job.ID),
			slog.String("video_url", videoURL),
		)

		// Add output video to temp files for cleanup since it's now in S3
		tempFiles = append(tempFiles, outputVideoPath)
	}

	// Step 8: Complete job
	job.SetOutput(outputVideoPath, videoURL)
	job.UpdateProgress(100)
	if err := job.Complete(); err != nil {
		s.logger.Error("failed to complete job",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("complete job: %w", err)
	}
	if err := s.repo.Save(ctx, job); err != nil {
		return nil, fmt.Errorf("save job: %w", err)
	}

	s.logger.Info("job completed successfully",
		slog.String("job_id", job.ID),
		slog.String("status", string(job.Status)),
	)

	return &ProcessVideoOutput{
		JobID:     job.ID,
		Status:    job.Status,
		VideoPath: outputVideoPath,
		VideoURL:  videoURL,
	}, nil
}

// processChunksSequential processes audio chunks one by one, using the same
// source image for all chunks to maintain visual consistency and avoid
// cumulative visual drift.
func (s *ProcessVideoService) processChunksSequential(
	ctx context.Context,
	job *Job,
	gen generator.Generator,
	initialImageB64 string,
	audioChunks []string,
	width, height int,
	forceOffload bool,
	longVideo bool,
) ([]string, error) {
	videoPaths := make([]string, 0, len(audioChunks))

	for i, chunkPath := range audioChunks {
		// Check context before starting each chunk
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled: %w", ctx.Err())
		default:
		}

		s.logger.Info("processing chunk sequentially",
			slog.String("job_id", job.ID),
			slog.Int("chunk_index", i),
			slog.Int("total_chunks", len(audioChunks)),
		)

		// Process this chunk with the original image
		videoPath, err := s.processChunkWithGenerator(
			ctx, job, gen, i, initialImageB64, chunkPath, width, height, forceOffload, longVideo,
		)

		if err != nil {
			return nil, fmt.Errorf("chunk %d failed: %w", i, err)
		}
		videoPaths = append(videoPaths, videoPath)

		// Update progress
		progress := ((i + 1) * 90) / len(audioChunks) // Reserve 10% for joining
		job.UpdateProgress(progress)
		if err := s.repo.Save(ctx, job); err != nil {
			s.logger.Warn("failed to save job progress",
				slog.String("job_id", job.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	return videoPaths, nil
}

// processChunkWithGenerator processes a single audio chunk using a generator interface.
func (s *ProcessVideoService) processChunkWithGenerator(
	ctx context.Context,
	job *Job,
	gen generator.Generator,
	idx int,
	imageB64, audioPath string,
	width, height int,
	forceOffload bool,
	longVideo bool,
) (string, error) {
	// Update chunk status to processing
	s.updateChunkStatus(job, idx, ChunkStatusProcessing, "")

	s.logger.Info("processing chunk",
		slog.String("job_id", job.ID),
		slog.String("provider", string(job.Provider)),
		slog.Int("chunk_index", idx),
		slog.Bool("force_offload", forceOffload),
	)

	// Read audio as base64
	audioB64, err := s.fileToBase64(audioPath)
	if err != nil {
		s.updateChunkStatus(job, idx, ChunkStatusFailed, err.Error())
		return "", fmt.Errorf("failed to encode audio: %w", err)
	}

	// Submit using generator interface
	submitOpts := generator.SubmitOptions{
		Prompt:       job.Prompt,
		Width:        width,
		Height:       height,
		ForceOffload: forceOffload,
		LongVideo:    longVideo,
	}
	providerJobID, err := gen.Submit(ctx, imageB64, audioB64, submitOpts)
	if err != nil {
		s.updateChunkStatus(job, idx, ChunkStatusFailed, err.Error())
		return "", fmt.Errorf("failed to submit to provider: %w", err)
	}

	// Update chunk with provider job ID
	job.mu.Lock()
	if idx < len(job.Chunks) {
		job.Chunks[idx].RunPodJobID = providerJobID // Reuse this field for both providers
		job.Chunks[idx].StartedAt = time.Now()
	}
	job.mu.Unlock()

	s.logger.Info("chunk submitted to provider",
		slog.String("job_id", job.ID),
		slog.String("provider", string(job.Provider)),
		slog.Int("chunk_index", idx),
		slog.String("provider_job_id", providerJobID),
	)

	// Poll for result using generator
	pollResult, err := s.pollForResultWithGenerator(ctx, gen, job.ID, idx, providerJobID)
	if err != nil {
		s.updateChunkStatus(job, idx, ChunkStatusFailed, err.Error())
		return "", fmt.Errorf("failed to poll provider: %w", err)
	}

	// Handle video output differently based on provider
	var videoPath string
	switch {
	case pollResult.VideoBase64 != "":
		// RunPod path: decode base64 to file
		videoData, err := base64.StdEncoding.DecodeString(pollResult.VideoBase64)
		if err != nil {
			s.updateChunkStatus(job, idx, ChunkStatusFailed, err.Error())
			return "", fmt.Errorf("failed to decode video: %w", err)
		}
		videoFileName := fmt.Sprintf("chunk_%s_%d.mp4", job.ID, idx)
		videoPath, err = s.storage.SaveTemp(ctx, videoFileName, bytes.NewReader(videoData))
		if err != nil {
			s.updateChunkStatus(job, idx, ChunkStatusFailed, err.Error())
			return "", fmt.Errorf("failed to save video: %w", err)
		}
	case pollResult.VideoURL != "":
		// Beam path: download to temp file
		videoPath = filepath.Join(filepath.Dir(audioPath), fmt.Sprintf("chunk_%s_%d.mp4", job.ID, idx))
		if err := gen.DownloadOutput(ctx, pollResult.VideoURL, videoPath); err != nil {
			s.updateChunkStatus(job, idx, ChunkStatusFailed, err.Error())
			return "", fmt.Errorf("failed to download video: %w", err)
		}
	default:
		s.updateChunkStatus(job, idx, ChunkStatusFailed, ErrNoVideoOutput.Error())
		return "", ErrNoVideoOutput
	}

	// Update chunk status to completed
	job.mu.Lock()
	if idx < len(job.Chunks) {
		job.Chunks[idx].Status = ChunkStatusCompleted
		job.Chunks[idx].OutputPath = videoPath
		job.Chunks[idx].CompletedAt = time.Now()
	}
	job.mu.Unlock()

	s.logger.Info("chunk processing completed",
		slog.String("job_id", job.ID),
		slog.Int("chunk_index", idx),
		slog.String("video_path", videoPath),
	)

	return videoPath, nil
}

// pollForResultWithGenerator polls using the generator interface until the job completes or fails.
func (s *ProcessVideoService) pollForResultWithGenerator(
	ctx context.Context,
	gen generator.Generator,
	jobID string,
	chunkIdx int,
	providerJobID string,
) (generator.PollResult, error) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	var (
		attempt    int
		prevStatus generator.Status
		firstPoll  = true
	)

	for {
		select {
		case <-ctx.Done():
			return generator.PollResult{}, fmt.Errorf("context cancelled: %w", ctx.Err())
		case <-ticker.C:
			attempt++
			pollResult, err := gen.Poll(ctx, providerJobID)
			if err != nil {
				s.logger.Warn("poll error, retrying",
					slog.String("job_id", jobID),
					slog.Int("chunk_index", chunkIdx),
					slog.String("provider_job_id", providerJobID),
					slog.Int("attempt", attempt),
					slog.String("error", err.Error()),
				)
				continue
			}

			// Consolidated poll log: Info if status changed or error, Debug otherwise
			logLevel := slog.LevelDebug
			if pollResult.Status != prevStatus || pollResult.Error != "" {
				logLevel = slog.LevelInfo
			}
			prevStatusStr := string(prevStatus)
			if firstPoll {
				prevStatusStr = "initial"
			}
			s.logger.Log(ctx, logLevel, "provider poll update",
				slog.String("job_id", jobID),
				slog.Int("chunk_index", chunkIdx),
				slog.String("provider_job_id", providerJobID),
				slog.Int("attempt", attempt),
				slog.String("status", string(pollResult.Status)),
				slog.String("prev_status", prevStatusStr),
				slog.String("error", pollResult.Error),
			)

			if pollResult.Error != "" {
				s.logger.Info("provider reported error",
					slog.String("job_id", jobID),
					slog.Int("chunk_index", chunkIdx),
					slog.String("provider_job_id", providerJobID),
					slog.String("error", pollResult.Error),
				)
			}

			// If status changed since last poll (and not first poll), record it at info level.
			if pollResult.Status != prevStatus && !firstPoll {
				s.logger.Info("provider status changed",
					slog.String("job_id", jobID),
					slog.Int("chunk_index", chunkIdx),
					slog.String("provider_job_id", providerJobID),
					slog.String("from", string(prevStatus)),
					slog.String("to", string(pollResult.Status)),
				)
			}
			firstPoll = false
			prevStatus = pollResult.Status

			// Map generator status to job status and handle terminal states
			switch pollResult.Status {
			case generator.StatusCompleted:
				return pollResult, nil
			case generator.StatusFailed:
				if pollResult.Error != "" {
					return pollResult, fmt.Errorf("%w: %s", ErrProviderJobFailed, pollResult.Error)
				}
				return pollResult, ErrProviderJobFailed
			case generator.StatusCancelled:
				return pollResult, ErrProviderJobCancelled
			case generator.StatusTimedOut:
				return pollResult, ErrProviderJobTimedOut
			case generator.StatusPending, generator.StatusInQueue, generator.StatusRunning:
				// Continue polling
			default:
				s.logger.Warn("unknown provider status",
					slog.String("job_id", jobID),
					slog.String("status", string(pollResult.Status)),
				)
			}
		}
	}
}

// updateChunkStatus updates the status of a chunk in the job.
func (s *ProcessVideoService) updateChunkStatus(job *Job, idx int, status ChunkStatus, errMsg string) {
	job.mu.Lock()
	defer job.mu.Unlock()
	if idx >= 0 && idx < len(job.Chunks) {
		job.Chunks[idx].Status = status
		job.Chunks[idx].Error = errMsg
		switch status {
		case ChunkStatusProcessing:
			job.Chunks[idx].StartedAt = time.Now()
		case ChunkStatusCompleted, ChunkStatusFailed:
			job.Chunks[idx].CompletedAt = time.Now()
		}
	}
}

// saveBase64ToTemp decodes base64 data and saves it to temporary storage.
func (s *ProcessVideoService) saveBase64ToTemp(ctx context.Context, b64Data, fileName string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	path, err := s.storage.SaveTemp(ctx, fileName, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("save to temp: %w", err)
	}

	return path, nil
}

// fileToBase64 reads a file and returns its base64-encoded content.
func (s *ProcessVideoService) fileToBase64(path string) (string, error) {
	data, err := os.ReadFile(path) // #nosec G304 - path is constructed internally
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// failJob marks the job as failed and returns the appropriate output.
// The second return value is always nil, as we want to return a valid output with error info.
func (s *ProcessVideoService) failJob(ctx context.Context, job *Job, errMsg string) (*ProcessVideoOutput, error) { //nolint:unparam
	if err := job.Fail(errMsg); err != nil {
		s.logger.Error("failed to transition job to failed state",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
	}
	if err := s.repo.Save(ctx, job); err != nil {
		s.logger.Error("failed to save failed job",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
	}

	s.logger.Error("job failed",
		slog.String("job_id", job.ID),
		slog.String("error", errMsg),
	)

	return &ProcessVideoOutput{
		JobID:  job.ID,
		Status: job.Status,
		Error:  errMsg,
	}, nil
}

// DeleteJobVideo deletes the local video file for a job and clears output metadata.
// This operation is idempotent - it returns success even if the file is already missing.
// Returns ErrJobNotFound if the job does not exist.
func (s *ProcessVideoService) DeleteJobVideo(ctx context.Context, jobID string) error {
	// Find the job
	job, err := s.repo.FindByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("find job: %w", err)
	}

	s.logger.Info("deleting job video",
		slog.String("job_id", jobID),
		slog.String("output_path", job.OutputVideoPath),
	)

	// If there's an output path, attempt to delete the file
	if job.OutputVideoPath != "" {
		if err := os.Remove(job.OutputVideoPath); err != nil {
			// Treat file not found as success (idempotent)
			if !os.IsNotExist(err) {
				s.logger.Error("failed to delete video file",
					slog.String("job_id", jobID),
					slog.String("path", job.OutputVideoPath),
					slog.String("error", err.Error()),
				)
				return fmt.Errorf("delete video file: %w", err)
			}
			s.logger.Info("video file already missing (idempotent delete)",
				slog.String("job_id", jobID),
				slog.String("path", job.OutputVideoPath),
			)
		} else {
			s.logger.Info("video file deleted",
				slog.String("job_id", jobID),
				slog.String("path", job.OutputVideoPath),
			)
		}
	}

	// Clear output metadata and persist
	job.ClearOutput()
	if err := s.repo.Save(ctx, job); err != nil {
		return fmt.Errorf("save job: %w", err)
	}

	s.logger.Info("job video deletion completed",
		slog.String("job_id", jobID),
	)

	return nil
}
