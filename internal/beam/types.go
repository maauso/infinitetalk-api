// Package beam provides an HTTP client for the Beam.cloud Task Queue API.
package beam

// Status represents the status of a Beam task.
type Status string

// Beam task statuses aligned with the Beam API.
const (
	StatusPending   Status = "PENDING"
	StatusRunning   Status = "RUNNING"
	StatusCompleted Status = "COMPLETED"
	StatusComplete  Status = "COMPLETE" // Beam sometimes returns "COMPLETE" instead of "COMPLETED"
	StatusFailed    Status = "FAILED"
	StatusError     Status = "ERROR"    // Beam returns "ERROR" when a task fails
	StatusCanceled  Status = "CANCELED" // Beam uses "CANCELED" (American spelling)
)

// IsTerminal returns true if the status is a terminal state.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusComplete, StatusFailed, StatusError, StatusCanceled:
		return true
	default:
		return false
	}
}

// SubmitOptions contains optional parameters for submitting a task to Beam.
type SubmitOptions struct {
	Prompt       string // Prompt text for lip-sync
	Width        int    // Video width in pixels
	Height       int    // Video height in pixels
	ForceOffload bool   // Whether to force offload
}

// DefaultSubmitOptions returns the default options for submitting a task.
func DefaultSubmitOptions() SubmitOptions {
	return SubmitOptions{
		Prompt:       "A person talking naturally",
		Width:        384,
		Height:       540,
		ForceOffload: true,
	}
}

// taskRequest represents the request body for Beam's task queue endpoint.
type taskRequest struct {
	Prompt       string `json:"prompt,omitempty"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	ForceOffload *bool  `json:"force_offload,omitempty"`
	ImageBase64  string `json:"image_base64,omitempty"`
	ImageURL     string `json:"image_url,omitempty"`
	WavBase64    string `json:"wav_base64,omitempty"`
	WavURL       string `json:"wav_url,omitempty"`
}

// taskResponse represents the response from Beam's task submission endpoint.
type taskResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

// statusResponse represents the response from Beam's task status endpoint.
type statusResponse struct {
	TaskID  string       `json:"task_id"`
	Status  string       `json:"status"`
	Outputs []taskOutput `json:"outputs,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// taskOutput represents a single output file from a Beam task.
type taskOutput struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

// PollResult contains the result of polling a task's status.
type PollResult struct {
	Status    Status
	OutputURL string // URL to download the output video
	Error     string // Error message (only set when Status is StatusFailed)
}
