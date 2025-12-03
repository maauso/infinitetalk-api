// Package runpod provides an HTTP client for the RunPod lip-sync video generation API.
package runpod

// Status represents the status of a RunPod job.
type Status string

// RunPod job statuses aligned with the RunPod API.
const (
	StatusInQueue    Status = "IN_QUEUE"
	StatusRunning    Status = "RUNNING"
	StatusInProgress Status = "IN_PROGRESS"
	StatusCompleted  Status = "COMPLETED"
	StatusFailed     Status = "FAILED"
	StatusCancelled  Status = "CANCELLED"
	StatusTimedOut   Status = "TIMED_OUT"
)

// IsTerminal returns true if the status is a terminal state.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled, StatusTimedOut:
		return true
	default:
		return false
	}
}

// SubmitOptions contains optional parameters for submitting a job to RunPod.
type SubmitOptions struct {
	Prompt      string // Prompt text for lip-sync (default: "high quality, realistic, speaking naturally")
	Width       int    // Video width in pixels (e.g., 384, 512)
	Height      int    // Video height in pixels (e.g., 576, 512)
	InputType   string // Input type (default: "image")
	PersonCount string // Person count (default: "single")
}

// DefaultSubmitOptions returns the default options for submitting a job.
func DefaultSubmitOptions() SubmitOptions {
	return SubmitOptions{
		Prompt:      "high quality, realistic, speaking naturally",
		Width:       384,
		Height:      576,
		InputType:   "image",
		PersonCount: "single",
	}
}

// runRequest represents the request body for RunPod's /run endpoint.
type runRequest struct {
	Input runInput `json:"input"`
}

// runInput represents the input field in a RunPod run request.
type runInput struct {
	InputType     string `json:"input_type"`
	PersonCount   string `json:"person_count"`
	Prompt        string `json:"prompt"`
	ImageBase64   string `json:"image_base64"`
	WavBase64     string `json:"wav_base64"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	NetworkVolume bool   `json:"network_volume"`
	ForceOffload  bool   `json:"force_offload"`
}

// runResponse represents the response from RunPod's /run endpoint.
type runResponse struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

// statusResponse represents the response from RunPod's /status endpoint.
type statusResponse struct {
	ID     string       `json:"id"`
	Status string       `json:"status"`
	Output statusOutput `json:"output,omitempty"`
	Error  string       `json:"error,omitempty"`
}

// statusOutput represents the output field in a status response.
type statusOutput struct {
	Video string `json:"video,omitempty"`
}

// PollResult contains the result of polling a job's status.
type PollResult struct {
	Status      Status
	VideoBase64 string // Base64-encoded video data (only set when Status is StatusCompleted)
	Error       string // Error message (only set when Status is StatusFailed)
}
