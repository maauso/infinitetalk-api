package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-playground/validator/v10"

	"github.com/maauso/infinitetalk-api/internal/job"
)

// Handlers contains the HTTP handlers for the API.
type Handlers struct {
	service            *job.ProcessVideoService
	validator          *validator.Validate
	logger             *slog.Logger
	enableAsyncProcess bool
}

// HandlerOption is a function that configures a Handlers instance.
type HandlerOption func(*Handlers)

// WithAsyncProcessing enables or disables background processing.
// When disabled, CreateJob only creates the job and returns immediately
// without starting background processing.
func WithAsyncProcessing(enabled bool) HandlerOption {
	return func(h *Handlers) {
		h.enableAsyncProcess = enabled
	}
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(service *job.ProcessVideoService, logger *slog.Logger, opts ...HandlerOption) *Handlers {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handlers{
		service:            service,
		validator:          validator.New(),
		logger:             logger,
		enableAsyncProcess: true, // Default to enabled
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Health handles GET /health requests.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

// CreateJob handles POST /jobs requests.
func (h *Handlers) CreateJob(w http.ResponseWriter, r *http.Request) {
	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("failed to decode request body",
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusBadRequest, "invalid JSON body", "INVALID_JSON")
		return
	}

	// Validate request
	if err := h.validator.Struct(req); err != nil {
		h.logger.Warn("request validation failed",
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	// Create the job through the service
	input := job.ProcessVideoInput{
		ImageBase64: req.ImageBase64,
		AudioBase64: req.AudioBase64,
		Width:       req.Width,
		Height:      req.Height,
		PushToS3:    req.PushToS3,
	}

	// Create job first (synchronously)
	createdJob, err := h.service.CreateJob(r.Context(), input)
	if err != nil {
		h.logger.Error("failed to create job",
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "failed to create job", "JOB_CREATION_FAILED")
		return
	}

	// Start processing in background with a detached context
	// Use context.WithoutCancel to prevent cancellation when the request ends
	if h.enableAsyncProcess {
		go func(ctx context.Context, jobID string, inp job.ProcessVideoInput) {
			_, processErr := h.service.ProcessExistingJob(ctx, jobID, inp)
			if processErr != nil {
				h.logger.Error("background processing failed",
					slog.String("job_id", jobID),
					slog.String("error", processErr.Error()),
				)
			}
		}(context.WithoutCancel(r.Context()), createdJob.ID, input)
	}

	h.logger.Info("job created",
		slog.String("job_id", createdJob.ID),
		slog.Int("width", req.Width),
		slog.Int("height", req.Height),
	)

	writeJSON(w, http.StatusAccepted, CreateJobResponse{
		ID:     createdJob.ID,
		Status: string(createdJob.Status),
	})
}

// GetJob handles GET /jobs/{id} requests.
func (h *Handlers) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "job ID is required", "MISSING_JOB_ID")
		return
	}

	foundJob, err := h.service.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, job.ErrJobNotFound) {
			writeError(w, http.StatusNotFound, "job not found", "JOB_NOT_FOUND")
			return
		}
		h.logger.Error("failed to get job",
			slog.String("job_id", jobID),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "failed to get job", "JOB_FETCH_FAILED")
		return
	}

	resp := JobResponse{
		ID:       foundJob.ID,
		Status:   string(foundJob.Status),
		Progress: foundJob.Progress,
		Error:    foundJob.Error,
	}

	// Include video content if completed
	if foundJob.Status == job.StatusCompleted {
		if foundJob.PushToS3 && foundJob.VideoURL != "" {
			resp.VideoURL = foundJob.VideoURL
		} else if foundJob.OutputVideoPath != "" {
			// Read video file and encode to base64
			videoData, err := os.ReadFile(foundJob.OutputVideoPath)
			if err != nil {
				h.logger.Error("failed to read output video",
					slog.String("job_id", jobID),
					slog.String("path", foundJob.OutputVideoPath),
					slog.String("error", err.Error()),
				)
				// Don't fail the request, just log and omit video
			} else {
				resp.VideoBase64 = base64.StdEncoding.EncodeToString(videoData)
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode JSON response", slog.String("error", err.Error()))
	}
}

// writeError writes an error response in the standard format.
func writeError(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, ErrorResponse{
		Error: message,
		Code:  code,
	})
}
