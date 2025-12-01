// Package server provides the HTTP server for the InfiniteTalk API.
// It includes handlers, middleware, routes, and DTOs separated from domain types.
package server

// CreateJobRequest is the HTTP request body for creating a new job.
type CreateJobRequest struct {
	// ImageBase64 is the base64-encoded source image.
	ImageBase64 string `json:"image_base64" validate:"required,base64"`
	// AudioBase64 is the base64-encoded source audio.
	AudioBase64 string `json:"audio_base64" validate:"required,base64"`
	// Width is the target video width.
	Width int `json:"width" validate:"required,min=1,max=4096"`
	// Height is the target video height.
	Height int `json:"height" validate:"required,min=1,max=4096"`
	// PushToS3 indicates whether to upload the final video to S3.
	PushToS3 bool `json:"push_to_s3"`
}

// CreateJobResponse is the HTTP response after creating a job.
type CreateJobResponse struct {
	// ID is the unique identifier for the created job.
	ID string `json:"id"`
	// Status is the initial job status.
	Status string `json:"status"`
}

// JobResponse is the HTTP response for getting job details.
type JobResponse struct {
	// ID is the unique identifier for the job.
	ID string `json:"id"`
	// Status is the current job status.
	Status string `json:"status"`
	// Progress is the percentage of completion (0-100).
	Progress int `json:"progress"`
	// Error contains any error message if the job failed.
	Error string `json:"error,omitempty"`
	// VideoBase64 is the base64-encoded video content (if push_to_s3=false and completed).
	VideoBase64 string `json:"video_base64,omitempty"`
	// VideoURL is the S3 URL of the output video (if push_to_s3=true and completed).
	VideoURL string `json:"video_url,omitempty"`
}

// ErrorResponse is the standard error response format.
type ErrorResponse struct {
	// Error is the human-readable error message.
	Error string `json:"error"`
	// Code is the error code for programmatic handling.
	Code string `json:"code"`
}

// HealthResponse is the HTTP response for the health check endpoint.
type HealthResponse struct {
	// Status is the health status of the service.
	Status string `json:"status"`
}
