package job

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func TestNewProcessVideoService(t *testing.T) {
	repo := NewMemoryRepository()

	// With nil logger
	svc := NewProcessVideoService(repo, nil)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if svc.repo != repo {
		t.Error("expected repo to be set")
	}
	if svc.maxConcurrentChunks != 3 {
		t.Errorf("expected maxConcurrentChunks 3, got %d", svc.maxConcurrentChunks)
	}

	// With custom logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc2 := NewProcessVideoService(repo, logger)
	if svc2.logger != logger {
		t.Error("expected custom logger to be set")
	}
}

func TestProcessVideoService_SetMaxConcurrentChunks(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewProcessVideoService(repo, nil)

	svc.SetMaxConcurrentChunks(5)
	if svc.maxConcurrentChunks != 5 {
		t.Errorf("expected 5, got %d", svc.maxConcurrentChunks)
	}

	// Invalid value should be ignored
	svc.SetMaxConcurrentChunks(0)
	if svc.maxConcurrentChunks != 5 {
		t.Errorf("expected 5 (unchanged), got %d", svc.maxConcurrentChunks)
	}

	svc.SetMaxConcurrentChunks(-1)
	if svc.maxConcurrentChunks != 5 {
		t.Errorf("expected 5 (unchanged), got %d", svc.maxConcurrentChunks)
	}
}

func TestProcessVideoService_CreateJob(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewProcessVideoService(repo, nil)
	ctx := context.Background()

	input := ProcessVideoInput{
		ImageBase64: "test-image",
		AudioBase64: "test-audio",
		Width:       384,
		Height:      576,
		PushToS3:    true,
	}

	job, err := svc.CreateJob(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.ID == "" {
		t.Error("expected job ID to be set")
	}
	if job.Status != StatusInQueue {
		t.Errorf("expected status %s, got %s", StatusInQueue, job.Status)
	}
	if job.Width != 384 {
		t.Errorf("expected width 384, got %d", job.Width)
	}
	if job.Height != 576 {
		t.Errorf("expected height 576, got %d", job.Height)
	}
	if !job.PushToS3 {
		t.Error("expected PushToS3 to be true")
	}

	// Verify job was saved
	saved, err := repo.FindByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("job should be saved in repository: %v", err)
	}
	if saved.ID != job.ID {
		t.Errorf("saved job ID mismatch: expected %s, got %s", job.ID, saved.ID)
	}
}

func TestProcessVideoService_GetJob(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewProcessVideoService(repo, nil)
	ctx := context.Background()

	// Create job first
	created, _ := svc.CreateJob(ctx, ProcessVideoInput{Width: 512, Height: 512})

	// Get job
	found, err := svc.GetJob(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, found.ID)
	}
}

func TestProcessVideoService_GetJob_NotFound(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewProcessVideoService(repo, nil)
	ctx := context.Background()

	_, err := svc.GetJob(ctx, "nonexistent")
	if err != ErrJobNotFound {
		t.Errorf("expected ErrJobNotFound, got %v", err)
	}
}

func TestProcessVideoService_Process(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewProcessVideoService(repo, nil)
	ctx := context.Background()

	input := ProcessVideoInput{
		ImageBase64: "test-image",
		AudioBase64: "test-audio",
		Width:       384,
		Height:      576,
	}

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.JobID == "" {
		t.Error("expected JobID to be set")
	}
	if output.Status != StatusInQueue {
		t.Errorf("expected status %s, got %s", StatusInQueue, output.Status)
	}

	// Verify job exists in repo
	_, err = repo.FindByID(ctx, output.JobID)
	if err != nil {
		t.Fatalf("job should exist in repository: %v", err)
	}
}
