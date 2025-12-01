package job

import (
	"context"
	"testing"
)

func TestMemoryRepository_Save(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	job := New()

	err := repo.Save(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it was saved
	saved, err := repo.FindByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saved.ID != job.ID {
		t.Errorf("expected ID %s, got %s", job.ID, saved.ID)
	}
}

func TestMemoryRepository_Save_Update(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	job := New()

	// Save initial
	_ = repo.Save(ctx, job)

	// Update job
	_ = job.Start()
	job.Progress = 50
	_ = repo.Save(ctx, job)

	// Verify update
	saved, _ := repo.FindByID(ctx, job.ID)
	if saved.Status != StatusRunning {
		t.Errorf("expected status %s, got %s", StatusRunning, saved.Status)
	}
	if saved.Progress != 50 {
		t.Errorf("expected progress 50, got %d", saved.Progress)
	}
}

func TestMemoryRepository_FindByID_NotFound(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	_, err := repo.FindByID(ctx, "nonexistent")
	if err != ErrJobNotFound {
		t.Errorf("expected ErrJobNotFound, got %v", err)
	}
}

func TestMemoryRepository_FindByID_ReturnsClone(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	job := New()
	_ = repo.Save(ctx, job)

	// Get job
	found, _ := repo.FindByID(ctx, job.ID)

	// Modify returned job
	found.Progress = 99
	_ = found.Start()

	// Original in repo should be unchanged
	original, _ := repo.FindByID(ctx, job.ID)
	if original.Progress != 0 {
		t.Error("modifying returned job should not affect repository")
	}
	if original.Status != StatusInQueue {
		t.Error("modifying returned job status should not affect repository")
	}
}

func TestMemoryRepository_List(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	// Empty list
	jobs, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobs))
	}

	// Add jobs
	job1 := New()
	job2 := New()
	_ = repo.Save(ctx, job1)
	_ = repo.Save(ctx, job2)

	jobs, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(jobs))
	}
}

func TestMemoryRepository_List_ReturnsClones(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	job := New()
	_ = repo.Save(ctx, job)

	// Get list
	jobs, _ := repo.List(ctx)

	// Modify returned job
	jobs[0].Progress = 99

	// Original in repo should be unchanged
	original, _ := repo.FindByID(ctx, job.ID)
	if original.Progress != 0 {
		t.Error("modifying listed job should not affect repository")
	}
}

func TestMemoryRepository_Delete(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	job := New()
	_ = repo.Save(ctx, job)

	err := repo.Delete(ctx, job.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify deleted
	_, err = repo.FindByID(ctx, job.ID)
	if err != ErrJobNotFound {
		t.Errorf("expected ErrJobNotFound, got %v", err)
	}
}

func TestMemoryRepository_Delete_NotFound(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	err := repo.Delete(ctx, "nonexistent")
	if err != ErrJobNotFound {
		t.Errorf("expected ErrJobNotFound, got %v", err)
	}
}

func TestMemoryRepository_ConcurrentAccess(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	done := make(chan bool)

	// Concurrent writes
	go func() {
		for i := 0; i < 100; i++ {
			job := New()
			_ = repo.Save(ctx, job)
		}
		done <- true
	}()

	// Concurrent reads
	go func() {
		for i := 0; i < 100; i++ {
			_, _ = repo.List(ctx)
		}
		done <- true
	}()

	<-done
	<-done
	// If no race conditions, test passes
}
