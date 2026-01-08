package generator

import (
	"context"
	"errors"
	"testing"

	"github.com/maauso/infinitetalk-api/internal/beam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockBeamClient is a simple mock for testing BeamAdapter.
type mockBeamClient struct {
	mock.Mock
}

func (m *mockBeamClient) Submit(ctx context.Context, imageB64, audioB64 string, opts beam.SubmitOptions) (string, error) {
	args := m.Called(ctx, imageB64, audioB64, opts)
	return args.String(0), args.Error(1)
}

func (m *mockBeamClient) Poll(ctx context.Context, taskID string) (beam.PollResult, error) {
	args := m.Called(ctx, taskID)
	return args.Get(0).(beam.PollResult), args.Error(1)
}

func (m *mockBeamClient) DownloadOutput(ctx context.Context, outputURL, destPath string) error {
	args := m.Called(ctx, outputURL, destPath)
	return args.Error(0)
}

func TestBeamAdapter_Submit(t *testing.T) {
	ctx := context.Background()
	mockClient := &mockBeamClient{}
	adapter := NewBeamAdapter(mockClient)

	imageB64 := "base64image"
	audioB64 := "base64audio"
	opts := SubmitOptions{
		Prompt:       "test prompt",
		Width:        512,
		Height:       512,
		ForceOffload: true,
	}

	mockClient.On("Submit", ctx, imageB64, audioB64, mock.MatchedBy(func(o beam.SubmitOptions) bool {
		return o.Prompt == opts.Prompt && o.Width == opts.Width && o.Height == opts.Height && o.ForceOffload == opts.ForceOffload
	})).Return("task-456", nil)

	taskID, err := adapter.Submit(ctx, imageB64, audioB64, opts)
	require.NoError(t, err)
	assert.Equal(t, "task-456", taskID)
	mockClient.AssertExpectations(t)
}

func TestBeamAdapter_Submit_Error(t *testing.T) {
	ctx := context.Background()
	mockClient := &mockBeamClient{}
	adapter := NewBeamAdapter(mockClient)

	mockClient.On("Submit", ctx, mock.Anything, mock.Anything, mock.Anything).
		Return("", errors.New("submit failed"))

	_, err := adapter.Submit(ctx, "img", "audio", SubmitOptions{})
	require.Error(t, err)
	mockClient.AssertExpectations(t)
}

func TestBeamAdapter_Poll(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		beamStatus     beam.Status
		expectedStatus Status
		outputURL      string
	}{
		{"pending", beam.StatusPending, StatusPending, ""},
		{"running", beam.StatusRunning, StatusRunning, ""},
		{"completed", beam.StatusCompleted, StatusCompleted, "https://example.com/video.mp4"},
		{"complete", beam.StatusComplete, StatusCompleted, "https://example.com/video.mp4"},
		{"failed", beam.StatusFailed, StatusFailed, ""},
		{"error", beam.StatusError, StatusFailed, ""},
		{"canceled", beam.StatusCanceled, StatusCancelled, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockBeamClient{}
			adapter := NewBeamAdapter(mockClient)

			mockClient.On("Poll", ctx, "task-456").
				Return(beam.PollResult{
					Status:    tt.beamStatus,
					OutputURL: tt.outputURL,
				}, nil)

			result, err := adapter.Poll(ctx, "task-456")
			require.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.Equal(t, tt.outputURL, result.VideoURL)
			mockClient.AssertExpectations(t)
		})
	}
}

func TestBeamAdapter_Poll_WithError(t *testing.T) {
	ctx := context.Background()
	mockClient := &mockBeamClient{}
	adapter := NewBeamAdapter(mockClient)

	mockClient.On("Poll", ctx, "task-456").
		Return(beam.PollResult{
			Status: beam.StatusFailed,
			Error:  "processing failed",
		}, nil)

	result, err := adapter.Poll(ctx, "task-456")
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, result.Status)
	assert.Equal(t, "processing failed", result.Error)
	mockClient.AssertExpectations(t)
}

func TestBeamAdapter_DownloadOutput(t *testing.T) {
	ctx := context.Background()
	mockClient := &mockBeamClient{}
	adapter := NewBeamAdapter(mockClient)

	outputURL := "https://example.com/video.mp4"
	destPath := "/tmp/video.mp4"

	mockClient.On("DownloadOutput", ctx, outputURL, destPath).
		Return(nil)

	err := adapter.DownloadOutput(ctx, outputURL, destPath)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestBeamAdapter_DownloadOutput_Error(t *testing.T) {
	ctx := context.Background()
	mockClient := &mockBeamClient{}
	adapter := NewBeamAdapter(mockClient)

	mockClient.On("DownloadOutput", ctx, mock.Anything, mock.Anything).
		Return(errors.New("download failed"))

	err := adapter.DownloadOutput(ctx, "http://url", "/tmp/file")
	require.Error(t, err)
	mockClient.AssertExpectations(t)
}
