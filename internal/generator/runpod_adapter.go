package generator

import (
	"context"
	"fmt"

	"github.com/maauso/infinitetalk-api/internal/runpod"
)

// RunPodAdapter adapts the RunPod client to the Generator interface.
type RunPodAdapter struct {
	client runpod.Client
}

// NewRunPodAdapter creates a new RunPod generator adapter.
func NewRunPodAdapter(client runpod.Client) *RunPodAdapter {
	return &RunPodAdapter{client: client}
}

// Submit sends a lip-sync job to RunPod.
func (a *RunPodAdapter) Submit(ctx context.Context, imageB64, audioB64 string, opts SubmitOptions) (string, error) {
	runpodOpts := runpod.SubmitOptions{
		Prompt:       opts.Prompt,
		Width:        opts.Width,
		Height:       opts.Height,
		ForceOffload: opts.ForceOffload,
	}
	jobID, err := a.client.Submit(ctx, imageB64, audioB64, runpodOpts)
	if err != nil {
		return "", fmt.Errorf("runpod adapter submit: %w", err)
	}
	return jobID, nil
}

// Poll checks the status of a RunPod job.
func (a *RunPodAdapter) Poll(ctx context.Context, jobID string) (PollResult, error) {
	result, err := a.client.Poll(ctx, jobID)
	if err != nil {
		return PollResult{}, fmt.Errorf("runpod adapter poll: %w", err)
	}

	// Map RunPod status to common status
	var status Status
	switch result.Status {
	case runpod.StatusInQueue:
		status = StatusInQueue
	case runpod.StatusRunning, runpod.StatusInProgress:
		status = StatusRunning
	case runpod.StatusCompleted:
		status = StatusCompleted
	case runpod.StatusFailed:
		status = StatusFailed
	case runpod.StatusCancelled:
		status = StatusCancelled
	case runpod.StatusTimedOut:
		status = StatusTimedOut
	default:
		status = Status(result.Status)
	}

	return PollResult{
		Status:      status,
		VideoBase64: result.VideoBase64,
		Error:       result.Error,
	}, nil
}

// DownloadOutput is a no-op for RunPod since it returns video as base64.
func (a *RunPodAdapter) DownloadOutput(ctx context.Context, outputURL, destPath string) error {
	// RunPod returns base64 directly, no download needed
	return nil
}

// Compile-time check that RunPodAdapter implements Generator.
var _ Generator = (*RunPodAdapter)(nil)
