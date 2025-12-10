package runpod

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// setTestEnv sets the RUNPOD_API_KEY env var and returns a cleanup function.
func setTestEnv(t *testing.T) {
	t.Helper()
	if err := os.Setenv("RUNPOD_API_KEY", "test-key"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("RUNPOD_API_KEY")
	})
}

func TestStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status   Status
		terminal bool
	}{
		{StatusInQueue, false},
		{StatusRunning, false},
		{StatusCompleted, true},
		{StatusFailed, true},
		{StatusCancelled, true},
		{StatusTimedOut, true},
		{Status("UNKNOWN"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.terminal {
				t.Errorf("Status(%q).IsTerminal() = %v, want %v", tt.status, got, tt.terminal)
			}
		})
	}
}

func TestDefaultSubmitOptions(t *testing.T) {
	opts := DefaultSubmitOptions()

	if opts.Prompt != "high quality, realistic, speaking naturally" {
		t.Errorf("expected Prompt 'high quality, realistic, speaking naturally', got %q", opts.Prompt)
	}
	if opts.Width == 0 {
		t.Error("expected non-zero Width")
	}
	if opts.Height == 0 {
		t.Error("expected non-zero Height")
	}
	if opts.InputType == "" {
		t.Error("expected non-empty InputType")
	}
	if opts.PersonCount == "" {
		t.Error("expected non-empty PersonCount")
	}
}

func TestNewClient_MissingEndpointID(t *testing.T) {
	setTestEnv(t)

	_, err := NewClient("")
	if err == nil {
		t.Error("expected error for missing endpoint ID")
	}
}

func TestNewClient_MissingAPIKey(t *testing.T) {
	// Ensure API key is not set
	_ = os.Unsetenv("RUNPOD_API_KEY")

	_, err := NewClient("test-endpoint")
	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestNewClient_Success(t *testing.T) {
	setTestEnv(t)

	client, err := NewClient("test-endpoint")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClient_WithAPIKeyOption(t *testing.T) {
	// Ensure environment API key is NOT set
	_ = os.Unsetenv("RUNPOD_API_KEY")

	client, err := NewClient("test-endpoint", WithAPIKey("explicit-api-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.apiKey != "explicit-api-key" {
		t.Errorf("expected apiKey to be 'explicit-api-key', got '%s'", client.apiKey)
	}
}

func TestNewClient_WithAPIKeyOptionOverridesEnv(t *testing.T) {
	setTestEnv(t) // Sets RUNPOD_API_KEY=test-key

	client, err := NewClient("test-endpoint", WithAPIKey("explicit-api-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// WithAPIKey should be used instead of env
	if client.apiKey != "explicit-api-key" {
		t.Errorf("expected apiKey to be 'explicit-api-key', got '%s'", client.apiKey)
	}
}

func TestSubmit_Success(t *testing.T) {
	setTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}

		// Verify request body
		var req runRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if req.Input.ImageBase64 != "image-data" {
			t.Errorf("expected image-data, got %s", req.Input.ImageBase64)
		}
		if req.Input.WavBase64 != "audio-data" {
			t.Errorf("expected audio-data, got %s", req.Input.WavBase64)
		}

		// Return success response
		_ = json.NewEncoder(w).Encode(runResponse{ID: "job-123"})
	}))
	defer server.Close()

	client, _ := NewClient("test-endpoint", WithBaseURL(server.URL))

	jobID, err := client.Submit(context.Background(), "image-data", "audio-data", DefaultSubmitOptions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jobID != "job-123" {
		t.Errorf("expected job-123, got %s", jobID)
	}
}

func TestSubmit_Error(t *testing.T) {
	setTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(runResponse{Error: "invalid input"})
	}))
	defer server.Close()

	client, _ := NewClient("test-endpoint", WithBaseURL(server.URL))

	_, err := client.Submit(context.Background(), "image-data", "audio-data", DefaultSubmitOptions())
	if err == nil {
		t.Error("expected error")
	}
}

func TestSubmit_ContextCancelled(t *testing.T) {
	setTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // Simulate slow response
	}))
	defer server.Close()

	client, _ := NewClient("test-endpoint", WithBaseURL(server.URL))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.Submit(ctx, "image-data", "audio-data", DefaultSubmitOptions())
	if err == nil {
		t.Error("expected error due to context cancellation")
	}
}

func TestPoll_AllStatuses(t *testing.T) {
	setTestEnv(t)

	tests := []struct {
		name           string
		response       statusResponse
		expectedStatus Status
		expectedVideo  string
		expectedError  string
	}{
		{
			name:           "IN_QUEUE",
			response:       statusResponse{ID: "job-1", Status: "IN_QUEUE"},
			expectedStatus: StatusInQueue,
		},
		{
			name:           "RUNNING",
			response:       statusResponse{ID: "job-1", Status: "RUNNING"},
			expectedStatus: StatusRunning,
		},
		{
			name: "COMPLETED",
			response: statusResponse{
				ID:     "job-1",
				Status: "COMPLETED",
				Output: statusOutput{Video: "video-base64-data"},
			},
			expectedStatus: StatusCompleted,
			expectedVideo:  "video-base64-data",
		},
		{
			name: "FAILED",
			response: statusResponse{
				ID:     "job-1",
				Status: "FAILED",
				Error:  "processing failed",
			},
			expectedStatus: StatusFailed,
			expectedError:  "processing failed",
		},
		{
			name:           "CANCELLED",
			response:       statusResponse{ID: "job-1", Status: "CANCELLED"},
			expectedStatus: StatusCancelled,
		},
		{
			name:           "TIMED_OUT",
			response:       statusResponse{ID: "job-1", Status: "TIMED_OUT"},
			expectedStatus: StatusTimedOut,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET, got %s", r.Method)
				}
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client, _ := NewClient("test-endpoint", WithBaseURL(server.URL))

			result, err := client.Poll(context.Background(), "job-1")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != tt.expectedStatus {
				t.Errorf("expected status %v, got %v", tt.expectedStatus, result.Status)
			}
			if result.VideoBase64 != tt.expectedVideo {
				t.Errorf("expected video %q, got %q", tt.expectedVideo, result.VideoBase64)
			}
			if result.Error != tt.expectedError {
				t.Errorf("expected error %q, got %q", tt.expectedError, result.Error)
			}
		})
	}
}

func TestPoll_EmptyJobID(t *testing.T) {
	setTestEnv(t)

	client, _ := NewClient("test-endpoint")

	_, err := client.Poll(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty job ID")
	}
}

func TestRetry_TransientFailure(t *testing.T) {
	setTestEnv(t)

	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			// First two attempts fail with 503
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("service unavailable"))
			return
		}
		// Third attempt succeeds
		_ = json.NewEncoder(w).Encode(statusResponse{ID: "job-1", Status: "COMPLETED"})
	}))
	defer server.Close()

	client, _ := NewClient("test-endpoint",
		WithBaseURL(server.URL),
		WithMaxRetries(3),
		WithBaseBackoff(10*time.Millisecond),
	)

	result, err := client.Poll(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("expected COMPLETED, got %v", result.Status)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetry_MaxRetriesExceeded(t *testing.T) {
	setTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("service unavailable"))
	}))
	defer server.Close()

	client, _ := NewClient("test-endpoint",
		WithBaseURL(server.URL),
		WithMaxRetries(2),
		WithBaseBackoff(10*time.Millisecond),
	)

	_, err := client.Poll(context.Background(), "job-1")
	if err == nil {
		t.Error("expected error after max retries exceeded")
	}
}

func TestRetry_NonRetryableError(t *testing.T) {
	setTestEnv(t)

	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest) // 400 is not retryable
		_, _ = w.Write([]byte("bad request"))
	}))
	defer server.Close()

	client, _ := NewClient("test-endpoint",
		WithBaseURL(server.URL),
		WithMaxRetries(3),
		WithBaseBackoff(10*time.Millisecond),
	)

	_, err := client.Poll(context.Background(), "job-1")
	if err == nil {
		t.Error("expected error")
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt (no retries for 400), got %d", attempts)
	}
}

func TestRetry_RateLimited(t *testing.T) {
	setTestEnv(t)

	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 2 {
			w.WriteHeader(http.StatusTooManyRequests) // 429 is retryable
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		_ = json.NewEncoder(w).Encode(statusResponse{ID: "job-1", Status: "COMPLETED"})
	}))
	defer server.Close()

	client, _ := NewClient("test-endpoint",
		WithBaseURL(server.URL),
		WithMaxRetries(3),
		WithBaseBackoff(10*time.Millisecond),
	)

	result, err := client.Poll(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Errorf("expected COMPLETED, got %v", result.Status)
	}
}

func TestWithHTTPClient(t *testing.T) {
	setTestEnv(t)

	customClient := &http.Client{Timeout: 60 * time.Second}
	client, err := NewClient("test-endpoint", WithHTTPClient(customClient))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.httpClient != customClient {
		t.Error("expected custom HTTP client to be set")
	}
}

func TestSubmit_DefaultOptions(t *testing.T) {
	setTestEnv(t)

	var receivedReq runRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedReq)
		_ = json.NewEncoder(w).Encode(runResponse{ID: "job-123"})
	}))
	defer server.Close()

	client, _ := NewClient("test-endpoint", WithBaseURL(server.URL))

	// Submit with empty options to test defaults
	_, err := client.Submit(context.Background(), "image", "audio", SubmitOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify defaults were applied
	if receivedReq.Input.InputType != "image" {
		t.Errorf("expected default InputType 'image', got %q", receivedReq.Input.InputType)
	}
	if receivedReq.Input.PersonCount != "single" {
		t.Errorf("expected default PersonCount 'single', got %q", receivedReq.Input.PersonCount)
	}
	if receivedReq.Input.Prompt != "high quality, realistic, speaking naturally" {
		t.Errorf("expected default Prompt 'high quality, realistic, speaking naturally', got %q", receivedReq.Input.Prompt)
	}
}
