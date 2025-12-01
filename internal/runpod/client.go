package runpod

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client defines the interface for interacting with the RunPod API.
type Client interface {
	// Submit sends a lip-sync job to RunPod and returns the job ID.
	Submit(ctx context.Context, imageB64, audioB64 string, opts SubmitOptions) (jobID string, err error)

	// Poll checks the status of a job and returns the result.
	Poll(ctx context.Context, jobID string) (PollResult, error)
}

// HTTPClient is the HTTP implementation of the RunPod Client interface.
type HTTPClient struct {
	apiKey      string
	endpointID  string
	baseURL     string
	httpClient  *http.Client
	maxRetries  int
	baseBackoff time.Duration
}

// ClientOption is a function that configures an HTTPClient.
type ClientOption func(*HTTPClient)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(hc *HTTPClient) {
		hc.httpClient = c
	}
}

// WithBaseURL sets a custom base URL for the RunPod API.
func WithBaseURL(url string) ClientOption {
	return func(hc *HTTPClient) {
		hc.baseURL = url
	}
}

// WithMaxRetries sets the maximum number of retries for transient failures.
func WithMaxRetries(n int) ClientOption {
	return func(hc *HTTPClient) {
		hc.maxRetries = n
	}
}

// WithBaseBackoff sets the initial backoff duration for retries.
func WithBaseBackoff(d time.Duration) ClientOption {
	return func(hc *HTTPClient) {
		hc.baseBackoff = d
	}
}

// NewClient creates a new RunPod HTTP client.
// The API key is read from the environment variable RUNPOD_API_KEY if not provided.
// The endpoint ID must be provided.
func NewClient(endpointID string, opts ...ClientOption) (*HTTPClient, error) {
	if endpointID == "" {
		return nil, fmt.Errorf("runpod: endpoint ID is required")
	}

	apiKey := os.Getenv("RUNPOD_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("runpod: RUNPOD_API_KEY environment variable is not set")
	}

	c := &HTTPClient{
		apiKey:      apiKey,
		endpointID:  endpointID,
		baseURL:     "https://api.runpod.ai/v2",
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		maxRetries:  3,
		baseBackoff: 1 * time.Second,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// Submit sends a lip-sync job to RunPod and returns the job ID.
func (c *HTTPClient) Submit(ctx context.Context, imageB64, audioB64 string, opts SubmitOptions) (string, error) {
	// Apply defaults if not set
	if opts.InputType == "" {
		opts.InputType = "image"
	}
	if opts.PersonCount == "" {
		opts.PersonCount = "single"
	}
	if opts.Prompt == "" {
		opts.Prompt = "high quality, realistic, speaking naturally"
	}

	reqBody := runRequest{
		Input: runInput{
			InputType:     opts.InputType,
			PersonCount:   opts.PersonCount,
			Prompt:        opts.Prompt,
			ImageBase64:   imageB64,
			WavBase64:     audioB64,
			Width:         opts.Width,
			Height:        opts.Height,
			NetworkVolume: false,
			ForceOffload:  false,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("runpod: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/%s/run", c.baseURL, c.endpointID)

	var resp runResponse
	if err := c.doRequestWithRetry(ctx, http.MethodPost, url, bodyBytes, &resp); err != nil {
		return "", err
	}

	if resp.ID == "" {
		if resp.Error != "" {
			return "", fmt.Errorf("runpod: submit failed: %s", resp.Error)
		}
		return "", fmt.Errorf("runpod: submit failed: no job ID returned")
	}

	return resp.ID, nil
}

// Poll checks the status of a job and returns the result.
func (c *HTTPClient) Poll(ctx context.Context, jobID string) (PollResult, error) {
	if jobID == "" {
		return PollResult{}, fmt.Errorf("runpod: job ID is required")
	}

	url := fmt.Sprintf("%s/%s/status/%s", c.baseURL, c.endpointID, jobID)

	var resp statusResponse
	if err := c.doRequestWithRetry(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return PollResult{}, err
	}

	result := PollResult{
		Status: Status(resp.Status),
	}

	switch result.Status {
	case StatusCompleted:
		result.VideoBase64 = resp.Output.Video
	case StatusFailed:
		result.Error = resp.Error
	}

	return result, nil
}

// doRequestWithRetry performs an HTTP request with exponential backoff retry.
func (c *HTTPClient) doRequestWithRetry(ctx context.Context, method, url string, body []byte, result interface{}) error {
	var lastErr error
	backoff := c.baseBackoff

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("runpod: context cancelled: %w", ctx.Err())
			case <-time.After(backoff):
				backoff *= 2 // Exponential backoff
			}
		}

		err := c.doRequest(ctx, method, url, body, result)
		if err == nil {
			return nil
		}

		// Check if error is retryable
		if !isRetryable(err) {
			return err
		}

		lastErr = err
	}

	return fmt.Errorf("runpod: max retries exceeded: %w", lastErr)
}

// doRequest performs a single HTTP request.
func (c *HTTPClient) doRequest(ctx context.Context, method, url string, body []byte, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("runpod: create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &retryableError{err: fmt.Errorf("runpod: request failed: %w", err)}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &retryableError{err: fmt.Errorf("runpod: read response: %w", err)}
	}

	// Handle non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 5xx errors are retryable
		if resp.StatusCode >= 500 {
			return &retryableError{err: fmt.Errorf("runpod: server error %d: %s", resp.StatusCode, string(respBody))}
		}
		// 429 (rate limit) is retryable
		if resp.StatusCode == 429 {
			return &retryableError{err: fmt.Errorf("runpod: rate limited: %s", string(respBody))}
		}
		// Other errors are not retryable
		return fmt.Errorf("runpod: request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("runpod: unmarshal response: %w", err)
		}
	}

	return nil
}

// retryableError wraps errors that should be retried.
type retryableError struct {
	err error
}

func (e *retryableError) Error() string {
	return e.err.Error()
}

func (e *retryableError) Unwrap() error {
	return e.err
}

// isRetryable returns true if the error should be retried.
func isRetryable(err error) bool {
	_, ok := err.(*retryableError)
	return ok
}
