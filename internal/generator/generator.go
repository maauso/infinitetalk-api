// Package generator provides the common interface for video generation providers.
// Both RunPod and Beam adapters implement this interface.
package generator

import "context"

// Status represents the status of a generation job.
type Status string

// Common job statuses across providers.
const (
	StatusPending   Status = "PENDING"    // Job submitted but not yet running
	StatusInQueue   Status = "IN_QUEUE"   // Job waiting in queue
	StatusRunning   Status = "RUNNING"    // Job is currently processing
	StatusCompleted Status = "COMPLETED"  // Job finished successfully
	StatusFailed    Status = "FAILED"     // Job failed with error
	StatusCancelled Status = "CANCELLED"  // Job was cancelled
	StatusTimedOut  Status = "TIMED_OUT"  // Job exceeded time limit
)

// IsTerminal returns true if the status represents a final state.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled, StatusTimedOut:
		return true
	default:
		return false
	}
}

// SubmitOptions contains parameters for submitting a job.
type SubmitOptions struct {
	Prompt       string // Prompt text for generation
	Width        int    // Video width in pixels
	Height       int    // Video height in pixels
	ForceOffload bool   // Whether to force offload (supported by both Beam and RunPod)
}

// PollResult contains the result of polling a job's status.
type PollResult struct {
	Status      Status // Current job status
	VideoBase64 string // Base64-encoded video (if completed and available inline)
	VideoURL    string // URL to download video (Beam returns URLs, RunPod returns base64)
	Error       string // Error message (if failed)
}

// Generator defines the interface for video generation providers.
// Both RunPod and Beam implement this interface.
type Generator interface {
	// Submit sends a lip-sync generation job and returns a job ID.
	Submit(ctx context.Context, imageB64, audioB64 string, opts SubmitOptions) (jobID string, err error)

	// Poll checks the status of a job and returns the result.
	Poll(ctx context.Context, jobID string) (PollResult, error)

	// DownloadOutput downloads the video from a URL (if applicable).
	// For RunPod, this is a no-op since it returns base64 directly.
	// For Beam, this downloads from the output URL to local temp storage.
	DownloadOutput(ctx context.Context, outputURL, destPath string) error
}
