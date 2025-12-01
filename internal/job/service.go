// Package job provides the ProcessVideoService use case for orchestrating
// lip-sync video generation. The complete implementation will be done in Phase 5.
package job

import (
	"context"
	"log/slog"
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
	// PushToS3 indicates whether to upload the final video to S3.
	PushToS3 bool
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
//
// Dependencies (to be injected in Phase 5):
//   - media.Processor: Image/video processing
//   - audio.Splitter: Audio chunking
//   - runpod.Client: RunPod API integration
//   - storage.Storage: Temporary and S3 storage
//   - Repository: Job persistence
type ProcessVideoService struct {
	repo   Repository
	logger *slog.Logger
	// maxConcurrentChunks limits parallel RunPod submissions.
	maxConcurrentChunks int
}

// NewProcessVideoService creates a new ProcessVideoService.
// The service is initialized with basic dependencies. Full dependency
// injection will be implemented in Phase 5.
func NewProcessVideoService(repo Repository, logger *slog.Logger) *ProcessVideoService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProcessVideoService{
		repo:                repo,
		logger:              logger,
		maxConcurrentChunks: 3, // Default concurrency
	}
}

// SetMaxConcurrentChunks configures the maximum number of chunks
// that can be processed in parallel.
func (s *ProcessVideoService) SetMaxConcurrentChunks(n int) {
	if n > 0 {
		s.maxConcurrentChunks = n
	}
}

// CreateJob creates a new job and persists it to the repository.
// The job is created in IN_QUEUE status, ready for processing.
func (s *ProcessVideoService) CreateJob(ctx context.Context, input ProcessVideoInput) (*Job, error) {
	job := New()
	job.Width = input.Width
	job.Height = input.Height
	job.PushToS3 = input.PushToS3

	s.logger.Info("creating new job",
		slog.String("job_id", job.ID),
		slog.Int("width", input.Width),
		slog.Int("height", input.Height),
		slog.Bool("push_to_s3", input.PushToS3),
	)

	if err := s.repo.Save(ctx, job); err != nil {
		s.logger.Error("failed to save job",
			slog.String("job_id", job.ID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	return job, nil
}

// GetJob retrieves a job by ID.
func (s *ProcessVideoService) GetJob(ctx context.Context, id string) (*Job, error) {
	return s.repo.FindByID(ctx, id)
}

// Process executes the complete video processing workflow.
// This is a scaffold that will be fully implemented in Phase 5.
//
// The workflow:
//  1. Create Job in repository
//  2. Use media.Processor to resize image → base64
//  3. Use audio.Splitter to split audio → chunks
//  4. For each chunk in parallel: submit to RunPod, poll, save partial video
//  5. Use media.Processor to join videos
//  6. Update Job to COMPLETED or FAILED
func (s *ProcessVideoService) Process(ctx context.Context, input ProcessVideoInput) (*ProcessVideoOutput, error) {
	// Create and persist job
	job, err := s.CreateJob(ctx, input)
	if err != nil {
		return nil, err
	}

	s.logger.Info("job created, processing will be implemented in Phase 5",
		slog.String("job_id", job.ID),
	)

	// TODO (Phase 5): Implement full processing workflow
	// 1. Decode and save input files
	// 2. Resize image with padding
	// 3. Split audio at silence boundaries
	// 4. Submit chunks to RunPod in parallel (with semaphore)
	// 5. Poll for results
	// 6. Join video segments
	// 7. Optionally push to S3
	// 8. Update job status

	return &ProcessVideoOutput{
		JobID:  job.ID,
		Status: job.Status,
	}, nil
}
