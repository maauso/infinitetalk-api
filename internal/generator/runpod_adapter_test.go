package generator

import (
	"context"
	"errors"
	"testing"

	"github.com/maauso/infinitetalk-api/internal/runpod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockRunPodClient is a simple mock for testing RunPodAdapter.
type mockRunPodClient struct {
	mock.Mock
}

func (m *mockRunPodClient) Submit(ctx context.Context, imageB64, audioB64 string, opts runpod.SubmitOptions) (string, error) {
	args := m.Called(ctx, imageB64, audioB64, opts)
	return args.String(0), args.Error(1)
}

func (m *mockRunPodClient) Poll(ctx context.Context, jobID string) (runpod.PollResult, error) {
	args := m.Called(ctx, jobID)
	return args.Get(0).(runpod.PollResult), args.Error(1)
}

func TestRunPodAdapter_Submit(t *testing.T) {
	ctx := context.Background()
	mockClient := &mockRunPodClient{}
	adapter := NewRunPodAdapter(mockClient)

	imageB64 := "base64image"
	audioB64 := "base64audio"
	opts := SubmitOptions{
		Prompt: "test prompt",
		Width:  512,
		Height: 512,
	}

	mockClient.On("Submit", ctx, imageB64, audioB64, mock.MatchedBy(func(o runpod.SubmitOptions) bool {
		return o.Prompt == opts.Prompt && o.Width == opts.Width && o.Height == opts.Height
	})).Return("job-123", nil)

	jobID, err := adapter.Submit(ctx, imageB64, audioB64, opts)
	require.NoError(t, err)
	assert.Equal(t, "job-123", jobID)
	mockClient.AssertExpectations(t)
}

func TestRunPodAdapter_Submit_Error(t *testing.T) {
	ctx := context.Background()
	mockClient := &mockRunPodClient{}
	adapter := NewRunPodAdapter(mockClient)

	mockClient.On("Submit", ctx, mock.Anything, mock.Anything, mock.Anything).
		Return("", errors.New("submit failed"))

	_, err := adapter.Submit(ctx, "img", "audio", SubmitOptions{})
	require.Error(t, err)
	mockClient.AssertExpectations(t)
}

func TestRunPodAdapter_Poll(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		runpodStatus   runpod.Status
		expectedStatus Status
	}{
		{"in_queue", runpod.StatusInQueue, StatusInQueue},
		{"running", runpod.StatusRunning, StatusRunning},
		{"in_progress", runpod.StatusInProgress, StatusRunning},
		{"completed", runpod.StatusCompleted, StatusCompleted},
		{"failed", runpod.StatusFailed, StatusFailed},
		{"cancelled", runpod.StatusCancelled, StatusCancelled},
		{"timed_out", runpod.StatusTimedOut, StatusTimedOut},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockRunPodClient{}
			adapter := NewRunPodAdapter(mockClient)

			mockClient.On("Poll", ctx, "job-123").
				Return(runpod.PollResult{
					Status:      tt.runpodStatus,
					VideoBase64: "video-data",
				}, nil)

			result, err := adapter.Poll(ctx, "job-123")
			require.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.Equal(t, "video-data", result.VideoBase64)
			mockClient.AssertExpectations(t)
		})
	}
}

func TestRunPodAdapter_Poll_WithError(t *testing.T) {
	ctx := context.Background()
	mockClient := &mockRunPodClient{}
	adapter := NewRunPodAdapter(mockClient)

	mockClient.On("Poll", ctx, "job-123").
		Return(runpod.PollResult{
			Status: runpod.StatusFailed,
			Error:  "processing failed",
		}, nil)

	result, err := adapter.Poll(ctx, "job-123")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, result.Status)
	assert.Equal(t, "processing failed", result.Error)
	mockClient.AssertExpectations(t)
}

func TestRunPodAdapter_DownloadOutput(t *testing.T) {
	adapter := NewRunPodAdapter(nil)

	// DownloadOutput should be a no-op for RunPod
	err := adapter.DownloadOutput(context.Background(), "http://example.com/video.mp4", "/tmp/video.mp4")
	assert.NoError(t, err)
}
