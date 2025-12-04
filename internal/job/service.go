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
	"sync"
	"time"

	"github.com/maauso/infinitetalk-api/internal/audio"
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
	// DryRun skips RunPod calls and completes after preprocessing.
	DryRun bool
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
	repo      Repository
	processor media.Processor
	splitter  audio.Splitter
	runpod    runpod.Client
	storage   storage.Storage
	logger    *slog.Logger
	// maxConcurrentChunks limits parallel RunPod submissions.
	maxConcurrentChunks int
	// splitOpts configures audio splitting behavior.
	splitOpts audio.SplitOpts
	// pollInterval is the duration between RunPod status polls.
	pollInterval time.Duration
}

// ServiceOption is a function that configures a ProcessVideoService.
type ServiceOption func(*ProcessVideoService)

// WithMaxConcurrentChunks sets the maximum number of concurrent chunk processing.
func WithMaxConcurrentChunks(n int) ServiceOption {
	return func(s *ProcessVideoService) {
		if n > 0 {
			s.maxConcurrentChunks = n
		}
	}
}

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
	storageClient storage.Storage,
	logger *slog.Logger,
	opts ...ServiceOption,
) *ProcessVideoService {
	if logger == nil {
		logger = slog.Default()
	}
	s := &ProcessVideoService{
		repo:                repo,
		processor:           processor,
		splitter:            splitter,
		runpod:              runpodClient,
		storage:             storageClient,
		logger:              logger,
		maxConcurrentChunks: 3, // Default concurrency
		splitOpts:           audio.DefaultSplitOpts(),
		pollInterval:        5 * time.Second,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
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
//
// Note: ImageBase64 and AudioBase64 from input are not stored directly in the Job.
// They will be decoded and saved as files during processing, and the
// resulting file paths will be stored in InputImagePath and InputAudioPath.
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

	// Dry-run mode: skip RunPod processing and complete immediately
	if input.DryRun {
		s.logger.Info("dry-run mode: skipping RunPod processing",
			slog.String("job_id", job.ID),
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

	// Step 5: Process chunks in parallel with semaphore
	videoPaths, err := s.processChunksParallel(ctx, job, resizedImageB64, audioChunks, input.Width, input.Height)
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

// processChunksParallel processes audio chunks in parallel with limited concurrency.
func (s *ProcessVideoService) processChunksParallel(
	ctx context.Context,
	job *Job,
	imageB64 string,
	audioChunks []string,
	width, height int,
) ([]string, error) {
	var (
		mu         sync.Mutex
		wg         sync.WaitGroup
		sem        = make(chan struct{}, s.maxConcurrentChunks)
		videoPaths = make([]string, len(audioChunks))
		firstErr   error
		errOnce    sync.Once
	)

	for i, chunkPath := range audioChunks {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled: %w", ctx.Err())
		default:
		}

		// Check if there was an error from another goroutine
		mu.Lock()
		hasErr := firstErr != nil
		mu.Unlock()
		if hasErr {
			break
		}

		wg.Add(1)
		go func(idx int, audioPath string) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errOnce.Do(func() {
					mu.Lock()
					firstErr = ctx.Err()
					mu.Unlock()
				})
				return
			}

			// Check again for errors
			mu.Lock()
			if firstErr != nil {
				mu.Unlock()
				return
			}
			mu.Unlock()

			// Process the chunk
			videoPath, err := s.processChunk(ctx, job, idx, imageB64, audioPath, width, height)
			if err != nil {
				errOnce.Do(func() {
					mu.Lock()
					firstErr = fmt.Errorf("chunk %d failed: %w", idx, err)
					mu.Unlock()
				})
				return
			}

			mu.Lock()
			videoPaths[idx] = videoPath
			// Update progress
			completedChunks := 0
			for _, p := range videoPaths {
				if p != "" {
					completedChunks++
				}
			}
			progress := (completedChunks * 90) / len(audioChunks) // Reserve 10% for joining
			job.UpdateProgress(progress)
			mu.Unlock()

			// Save progress
			if err := s.repo.Save(ctx, job); err != nil {
				s.logger.Warn("failed to save job progress",
					slog.String("job_id", job.ID),
					slog.String("error", err.Error()),
				)
			}
		}(i, chunkPath)
	}

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	return videoPaths, nil
}

// processChunk processes a single audio chunk through RunPod.
func (s *ProcessVideoService) processChunk(
	ctx context.Context,
	job *Job,
	idx int,
	imageB64, audioPath string,
	width, height int,
) (string, error) {
	// Update chunk status to processing
	s.updateChunkStatus(job, idx, ChunkStatusProcessing, "")

	s.logger.Info("processing chunk",
		slog.String("job_id", job.ID),
		slog.Int("chunk_index", idx),
	)

	// Read audio as base64
	audioB64, err := s.fileToBase64(audioPath)
	if err != nil {
		s.updateChunkStatus(job, idx, ChunkStatusFailed, err.Error())
		return "", fmt.Errorf("failed to encode audio: %w", err)
	}

	// Submit to RunPod
	opts := runpod.SubmitOptions{
		Width:  width,
		Height: height,
	}
	runpodJobID, err := s.runpod.Submit(ctx, imageB64, audioB64, opts)
	if err != nil {
		s.updateChunkStatus(job, idx, ChunkStatusFailed, err.Error())
		return "", fmt.Errorf("failed to submit to RunPod: %w", err)
	}

	// Update chunk with RunPod job ID
	job.mu.Lock()
	if idx < len(job.Chunks) {
		job.Chunks[idx].RunPodJobID = runpodJobID
		job.Chunks[idx].StartedAt = time.Now()
	}
	job.mu.Unlock()

	s.logger.Info("chunk submitted to RunPod",
		slog.String("job_id", job.ID),
		slog.Int("chunk_index", idx),
		slog.String("runpod_job_id", runpodJobID),
	)

	// Poll for result
	videoB64, err := s.pollForResult(ctx, job.ID, idx, runpodJobID)
	if err != nil {
		s.updateChunkStatus(job, idx, ChunkStatusFailed, err.Error())
		return "", fmt.Errorf("failed to poll RunPod: %w", err)
	}

	// Save video to temp storage
	videoData, err := base64.StdEncoding.DecodeString(videoB64)
	if err != nil {
		s.updateChunkStatus(job, idx, ChunkStatusFailed, err.Error())
		return "", fmt.Errorf("failed to decode video: %w", err)
	}

	videoFileName := fmt.Sprintf("chunk_%s_%d.mp4", job.ID, idx)
	videoPath, err := s.storage.SaveTemp(ctx, videoFileName, bytes.NewReader(videoData))
	if err != nil {
		s.updateChunkStatus(job, idx, ChunkStatusFailed, err.Error())
		return "", fmt.Errorf("failed to save video: %w", err)
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

// pollForResult polls RunPod until the job completes or fails.
func (s *ProcessVideoService) pollForResult(ctx context.Context, jobID string, chunkIdx int, runpodJobID string) (string, error) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	var (
		attempt    int
		prevStatus runpod.Status
		firstPoll  = true
	)

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("context cancelled: %w", ctx.Err())
		case <-ticker.C:
			attempt++
			result, err := s.runpod.Poll(ctx, runpodJobID)
			if err != nil {
				s.logger.Warn("poll error, retrying",
					slog.String("job_id", jobID),
					slog.Int("chunk_index", chunkIdx),
					slog.String("runpod_job_id", runpodJobID),
					slog.Int("attempt", attempt),
					slog.String("error", err.Error()),
				)
				continue
			}

			// Consolidated poll log: Info if status changed or error, Debug otherwise
			logLevel := slog.LevelDebug
			if result.Status != prevStatus || result.Error != "" {
				logLevel = slog.LevelInfo
			}
			prevStatusStr := string(prevStatus)
			if firstPoll {
				prevStatusStr = "initial"
			}
			s.logger.Log(ctx, logLevel, "runpod poll update",
				slog.String("job_id", jobID),
				slog.Int("chunk_index", chunkIdx),
				slog.String("runpod_job_id", runpodJobID),
				slog.Int("attempt", attempt),
				slog.String("status", string(result.Status)),
				slog.String("prev_status", prevStatusStr),
				slog.String("error", result.Error),
			)

			if result.Error != "" {
				s.logger.Info("runpod reported error",
					slog.String("job_id", jobID),
					slog.Int("chunk_index", chunkIdx),
					slog.String("runpod_job_id", runpodJobID),
					slog.String("error", result.Error),
				)
			}

			// If status changed since last poll (and not first poll), record it at info level.
			if result.Status != prevStatus && !firstPoll {
				s.logger.Info("runpod status changed",
					slog.String("job_id", jobID),
					slog.Int("chunk_index", chunkIdx),
					slog.String("runpod_job_id", runpodJobID),
					slog.String("from", string(prevStatus)),
					slog.String("to", string(result.Status)),
				)
			}
			firstPoll = false
			prevStatus = result.Status

			switch result.Status {
			case runpod.StatusCompleted:
				return result.VideoBase64, nil
			case runpod.StatusFailed:
				return "", fmt.Errorf("%w: %s", ErrRunPodJobFailed, result.Error)
			case runpod.StatusCancelled:
				return "", ErrRunPodJobCancelled
			case runpod.StatusTimedOut:
				return "", ErrRunPodJobTimedOut
			case runpod.StatusInQueue, runpod.StatusRunning, runpod.StatusInProgress:
				// Continue polling
			default:
				s.logger.Warn("unknown RunPod status",
					slog.String("job_id", jobID),
					slog.String("status", string(result.Status)),
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
