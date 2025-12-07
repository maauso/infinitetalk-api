package beam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Static errors for Beam client operations.
var (
	// ErrQueueURLRequired is returned when the queue URL is not provided.
	ErrQueueURLRequired = errors.New("beam: queue URL is required")
	// ErrTokenNotSet is returned when the BEAM_TOKEN is not provided.
	ErrTokenNotSet = errors.New("beam: token is required")
	// ErrTaskIDRequired is returned when the task ID is not provided.
	ErrTaskIDRequired = errors.New("beam: task ID is required")
	// ErrNoTaskIDReturned is returned when the submit response contains no task ID.
	ErrNoTaskIDReturned = errors.New("beam: submit failed: no task ID returned")
	// ErrSubmitFailed is returned when the submit operation fails.
	ErrSubmitFailed = errors.New("beam: submit failed")
	// ErrServerError is returned when the server returns a 5xx status code.
	ErrServerError = errors.New("beam: server error")
	// ErrRateLimited is returned when the server returns a 429 status code.
	ErrRateLimited = errors.New("beam: rate limited")
	// ErrRequestFailed is returned when the request fails with a non-2xx status code.
	ErrRequestFailed = errors.New("beam: request failed")
	// ErrNoOutputURL is returned when a completed task has no output URL.
	ErrNoOutputURL = errors.New("beam: no output URL in completed task")
)

// Client defines the interface for interacting with the Beam Task Queue API.
type Client interface {
	// Submit sends a lip-sync task to Beam and returns the task ID.
	Submit(ctx context.Context, imageB64, audioB64 string, opts SubmitOptions) (taskID string, err error)

	// Poll checks the status of a task and returns the result.
	Poll(ctx context.Context, taskID string) (PollResult, error)

	// DownloadOutput downloads the video from the output URL to the specified path.
	DownloadOutput(ctx context.Context, outputURL, destPath string) error
}

// HTTPClient is the HTTP implementation of the Beam Client interface.
type HTTPClient struct {
	token       string
	queueURL    string
	httpClient  *http.Client
	maxRetries  int
	baseBackoff time.Duration
}

// ClientOption is a function that configures an HTTPClient.
type ClientOption func(*HTTPClient)

// WithToken sets the API token for authentication.
func WithToken(token string) ClientOption {
	return func(hc *HTTPClient) {
		hc.token = token
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(hc *HTTPClient) {
		hc.httpClient = c
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

// NewClient creates a new Beam HTTP client.
// The token can be set via the WithToken option. If not provided,
// it is read from the environment variable BEAM_TOKEN.
// The queue URL must be provided.
func NewClient(queueURL string, opts ...ClientOption) (*HTTPClient, error) {
	if queueURL == "" {
		return nil, ErrQueueURLRequired
	}

	c := &HTTPClient{
		queueURL:    queueURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		maxRetries:  3,
		baseBackoff: 1 * time.Second,
	}

	// Apply options first to allow WithToken to set the token
	for _, opt := range opts {
		opt(c)
	}

	// If token was not set via option, try environment variable
	if c.token == "" {
		c.token = os.Getenv("BEAM_TOKEN")
	}

	if c.token == "" {
		return nil, ErrTokenNotSet
	}

	return c, nil
}

// Submit sends a lip-sync task to Beam and returns the task ID.
func (c *HTTPClient) Submit(ctx context.Context, imageB64, audioB64 string, opts SubmitOptions) (string, error) {
	reqBody := taskRequest{
		Prompt:      opts.Prompt,
		Width:       opts.Width,
		Height:      opts.Height,
		ImageBase64: imageB64,
		WavBase64:   audioB64,
	}

	// Only include force_offload if explicitly set
	if opts.ForceOffload {
		val := opts.ForceOffload
		reqBody.ForceOffload = &val
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("beam: marshal request: %w", err)
	}

	var resp taskResponse
	if err := c.doRequestWithRetry(ctx, http.MethodPost, c.queueURL, bodyBytes, &resp); err != nil {
		return "", err
	}

	if resp.TaskID == "" {
		if resp.Error != "" {
			return "", fmt.Errorf("%w: %s", ErrSubmitFailed, resp.Error)
		}
		return "", ErrNoTaskIDReturned
	}

	return resp.TaskID, nil
}

// Poll checks the status of a task and returns the result.
func (c *HTTPClient) Poll(ctx context.Context, taskID string) (PollResult, error) {
	if taskID == "" {
		return PollResult{}, ErrTaskIDRequired
	}

	url := fmt.Sprintf("https://api.beam.cloud/v2/task/%s/", taskID)

	var resp statusResponse
	if err := c.doRequestWithRetry(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return PollResult{}, err
	}

	// Map Beam status
	var mapped Status
	switch resp.Status {
	case "PENDING":
		mapped = StatusPending
	case "RUNNING":
		mapped = StatusRunning
	case "COMPLETED", "COMPLETE":
		mapped = StatusCompleted
	case "FAILED":
		mapped = StatusFailed
	case "CANCELED":
		mapped = StatusCanceled
	default:
		mapped = Status(resp.Status)
	}

	result := PollResult{
		Status: mapped,
	}

	switch result.Status {
	case StatusCompleted:
		// Extract output URL
		if len(resp.Outputs) > 0 && resp.Outputs[0].URL != "" {
			result.OutputURL = resp.Outputs[0].URL
		} else {
			result.Error = "no output URL available"
		}
	case StatusFailed:
		result.Error = resp.Error
	}

	return result, nil
}

// DownloadOutput downloads the video from the output URL to the specified path.
func (c *HTTPClient) DownloadOutput(ctx context.Context, outputURL, destPath string) error {
	if outputURL == "" {
		return ErrNoOutputURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, outputURL, nil)
	if err != nil {
		return fmt.Errorf("beam: create download request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("beam: download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("beam: download failed with status %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("beam: create output file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("beam: copy download data: %w", err)
	}

	return nil
}

// doRequestWithRetry performs an HTTP request with exponential backoff retry.
func (c *HTTPClient) doRequestWithRetry(ctx context.Context, method, url string, body []byte, result interface{}) error {
	var lastErr error
	backoff := c.baseBackoff

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("beam: context cancelled: %w", ctx.Err())
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

	return fmt.Errorf("beam: max retries exceeded: %w", lastErr)
}

// doRequest performs a single HTTP request.
func (c *HTTPClient) doRequest(ctx context.Context, method, url string, body []byte, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("beam: create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &retryableError{err: fmt.Errorf("beam: request failed: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &retryableError{err: fmt.Errorf("beam: read response: %w", err)}
	}

	// Handle non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 5xx errors are retryable
		if resp.StatusCode >= 500 {
			return &retryableError{err: fmt.Errorf("%w %d: %s", ErrServerError, resp.StatusCode, string(respBody))}
		}
		// 429 (rate limit) is retryable
		if resp.StatusCode == 429 {
			return &retryableError{err: fmt.Errorf("%w: %s", ErrRateLimited, string(respBody))}
		}
		// Other errors are not retryable
		return fmt.Errorf("%w with status %d: %s", ErrRequestFailed, resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("beam: unmarshal response: %w", err)
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
	var re *retryableError
	return errors.As(err, &re)
}
