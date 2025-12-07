// Package job provides the Job aggregate for managing video processing jobs.
// It includes the Job entity with state machine transitions aligned with RunPod states,
// as well as repository interfaces for persistence.
package job

import (
	"errors"
	"sync"
	"time"

	"github.com/maauso/infinitetalk-api/internal/job/id"
)

// Provider represents the video generation provider for the job.
type Provider string

const (
	// ProviderRunPod uses RunPod for video generation.
	ProviderRunPod Provider = "runpod"
	// ProviderBeam uses Beam for video generation.
	ProviderBeam Provider = "beam"
)

// IsValid returns true if the provider is valid.
func (p Provider) IsValid() bool {
	return p == ProviderRunPod || p == ProviderBeam
}

// Status represents the current state of a Job.
// States are aligned with RunPod job states.
type Status string

const (
	// StatusInQueue indicates the job is waiting for an available worker.
	StatusInQueue Status = "IN_QUEUE"
	// StatusRunning indicates the job is being processed by a worker.
	StatusRunning Status = "RUNNING"
	// StatusCompleted indicates the job finished successfully.
	StatusCompleted Status = "COMPLETED"
	// StatusFailed indicates the job encountered an error during execution.
	StatusFailed Status = "FAILED"
	// StatusCancelled indicates the job was manually cancelled.
	StatusCancelled Status = "CANCELLED"
	// StatusTimedOut indicates the job expired before pickup or worker did not respond in time.
	StatusTimedOut Status = "TIMED_OUT"
)

// ErrInvalidTransition is returned when an invalid state transition is attempted.
var ErrInvalidTransition = errors.New("invalid state transition")

// validTransitions defines which state transitions are allowed.
var validTransitions = map[Status][]Status{
	StatusInQueue:   {StatusRunning, StatusCancelled, StatusTimedOut},
	StatusRunning:   {StatusCompleted, StatusFailed, StatusCancelled, StatusTimedOut},
	StatusCompleted: {},
	StatusFailed:    {},
	StatusCancelled: {},
	StatusTimedOut:  {},
}

// canTransition checks if a transition from one status to another is valid.
func canTransition(from, to Status) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// ChunkStatus represents the status of a single audio/video chunk.
type ChunkStatus string

const (
	// ChunkStatusPending indicates the chunk is waiting to be processed.
	ChunkStatusPending ChunkStatus = "PENDING"
	// ChunkStatusProcessing indicates the chunk is currently being processed.
	ChunkStatusProcessing ChunkStatus = "PROCESSING"
	// ChunkStatusCompleted indicates the chunk was processed successfully.
	ChunkStatusCompleted ChunkStatus = "COMPLETED"
	// ChunkStatusFailed indicates the chunk processing failed.
	ChunkStatusFailed ChunkStatus = "FAILED"
)

// Chunk represents a segment of audio/video being processed.
type Chunk struct {
	// ID is the unique identifier for this chunk.
	ID string
	// Index is the position of this chunk in the sequence.
	Index int
	// Status is the current processing status.
	Status ChunkStatus
	// InputPath is the path to the input audio file.
	InputPath string
	// OutputPath is the path to the output video file.
	OutputPath string
	// RunPodJobID is the ID assigned by RunPod for this chunk.
	RunPodJobID string
	// Error contains any error message if processing failed.
	Error string
	// StartedAt is when chunk processing started.
	StartedAt time.Time
	// CompletedAt is when chunk processing finished.
	CompletedAt time.Time
}

// Job represents a video generation job aggregate.
// It contains all state related to processing a lip-sync video request.
type Job struct {
	mu sync.RWMutex

	// ID is the unique identifier for this job.
	ID string
	// Provider is the video generation provider (runpod or beam).
	Provider Provider
	// Status is the current job state.
	Status Status
	// Chunks contains the audio/video segments being processed.
	Chunks []Chunk
	// Progress is the percentage of completion (0-100).
	Progress int
	// Error contains any error message if the job failed.
	Error string
	// InputImagePath is the path to the source image.
	InputImagePath string
	// InputAudioPath is the path to the source audio.
	InputAudioPath string
	// OutputVideoPath is the path to the final output video.
	OutputVideoPath string
	// Width is the target video width.
	Width int
	// Height is the target video height.
	Height int
	// PushToS3 indicates whether to upload the result to S3.
	PushToS3 bool
	// VideoURL is the S3 URL if PushToS3 was true.
	VideoURL string
	// CreatedAt is when the job was created.
	CreatedAt time.Time
	// UpdatedAt is when the job was last updated.
	UpdatedAt time.Time
	// StartedAt is when processing started.
	StartedAt time.Time
	// CompletedAt is when processing finished.
	CompletedAt time.Time
}

// New creates a new Job with a generated ID and initial IN_QUEUE status.
// Provider defaults to RunPod.
func New() *Job {
	now := time.Now()
	return &Job{
		ID:        id.Generate(),
		Provider:  ProviderRunPod,
		Status:    StatusInQueue,
		Chunks:    make([]Chunk, 0),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewWithID creates a new Job with the specified ID and initial IN_QUEUE status.
// Useful for testing or when ID needs to be externally generated.
// Provider defaults to RunPod.
func NewWithID(jobID string) *Job {
	now := time.Now()
	return &Job{
		ID:        jobID,
		Provider:  ProviderRunPod,
		Status:    StatusInQueue,
		Chunks:    make([]Chunk, 0),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// TransitionTo attempts to change the job status to the specified state.
// Returns ErrInvalidTransition if the transition is not allowed.
func (j *Job) TransitionTo(status Status) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if !canTransition(j.Status, status) {
		return ErrInvalidTransition
	}

	j.Status = status
	j.UpdatedAt = time.Now()

	// Set timestamps based on state
	switch status {
	case StatusRunning:
		j.StartedAt = j.UpdatedAt
	case StatusCompleted, StatusFailed, StatusCancelled, StatusTimedOut:
		j.CompletedAt = j.UpdatedAt
	}

	return nil
}

// Start transitions the job from IN_QUEUE to RUNNING.
// Returns ErrInvalidTransition if the job is not in IN_QUEUE state.
func (j *Job) Start() error {
	return j.TransitionTo(StatusRunning)
}

// Complete transitions the job to COMPLETED state.
// Returns ErrInvalidTransition if the transition is not allowed.
func (j *Job) Complete() error {
	return j.TransitionTo(StatusCompleted)
}

// Fail transitions the job to FAILED state with an error message.
// Returns ErrInvalidTransition if the transition is not allowed.
func (j *Job) Fail(errMsg string) error {
	j.mu.Lock()
	j.Error = errMsg
	j.mu.Unlock()
	return j.TransitionTo(StatusFailed)
}

// Cancel transitions the job to CANCELLED state.
// Returns ErrInvalidTransition if the transition is not allowed.
func (j *Job) Cancel() error {
	return j.TransitionTo(StatusCancelled)
}

// Timeout transitions the job to TIMED_OUT state.
// Returns ErrInvalidTransition if the transition is not allowed.
func (j *Job) Timeout() error {
	return j.TransitionTo(StatusTimedOut)
}

// GetStatus returns the current job status (thread-safe).
func (j *Job) GetStatus() Status {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.Status
}

// SetChunks sets the chunks for this job.
func (j *Job) SetChunks(chunks []Chunk) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Chunks = chunks
	j.UpdatedAt = time.Now()
}

// UpdateChunk updates a specific chunk by index.
func (j *Job) UpdateChunk(index int, chunk Chunk) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if index >= 0 && index < len(j.Chunks) {
		j.Chunks[index] = chunk
		j.UpdatedAt = time.Now()
	}
}

// UpdateProgress sets the progress percentage (0-100).
func (j *Job) UpdateProgress(progress int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	j.Progress = progress
	j.UpdatedAt = time.Now()
}

// SetOutput sets the output video path and optional S3 URL.
func (j *Job) SetOutput(videoPath, videoURL string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.OutputVideoPath = videoPath
	j.VideoURL = videoURL
	j.UpdatedAt = time.Now()
}

// ClearOutput clears the output video path and URL.
// This is used when deleting the job's video file.
func (j *Job) ClearOutput() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.OutputVideoPath = ""
	j.VideoURL = ""
	j.UpdatedAt = time.Now()
}

// IsTerminal returns true if the job is in a terminal state.
func (j *Job) IsTerminal() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.Status == StatusCompleted ||
		j.Status == StatusFailed ||
		j.Status == StatusCancelled ||
		j.Status == StatusTimedOut
}

// Clone creates a deep copy of the job for safe reads.
func (j *Job) Clone() *Job {
	j.mu.RLock()
	defer j.mu.RUnlock()

	chunks := make([]Chunk, len(j.Chunks))
	copy(chunks, j.Chunks)

	return &Job{
		ID:              j.ID,
		Provider:        j.Provider,
		Status:          j.Status,
		Chunks:          chunks,
		Progress:        j.Progress,
		Error:           j.Error,
		InputImagePath:  j.InputImagePath,
		InputAudioPath:  j.InputAudioPath,
		OutputVideoPath: j.OutputVideoPath,
		Width:           j.Width,
		Height:          j.Height,
		PushToS3:        j.PushToS3,
		VideoURL:        j.VideoURL,
		CreatedAt:       j.CreatedAt,
		UpdatedAt:       j.UpdatedAt,
		StartedAt:       j.StartedAt,
		CompletedAt:     j.CompletedAt,
	}
}
