package job

import (
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	job := New()

	if job.ID == "" {
		t.Error("expected job to have an ID")
	}
	if job.Status != StatusInQueue {
		t.Errorf("expected status %s, got %s", StatusInQueue, job.Status)
	}
	if job.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if job.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
	if job.Chunks == nil {
		t.Error("expected Chunks to be initialized")
	}
}

func TestNewWithID(t *testing.T) {
	id := "test-job-123"
	job := NewWithID(id)

	if job.ID != id {
		t.Errorf("expected ID %s, got %s", id, job.ID)
	}
	if job.Status != StatusInQueue {
		t.Errorf("expected status %s, got %s", StatusInQueue, job.Status)
	}
}

func TestJob_ValidTransitions(t *testing.T) {
	tests := []struct {
		name     string
		from     Status
		to       Status
		wantErr  bool
	}{
		// Valid transitions from IN_QUEUE
		{"IN_QUEUE to RUNNING", StatusInQueue, StatusRunning, false},
		{"IN_QUEUE to CANCELLED", StatusInQueue, StatusCancelled, false},
		{"IN_QUEUE to TIMED_OUT", StatusInQueue, StatusTimedOut, false},
		// Valid transitions from RUNNING
		{"RUNNING to COMPLETED", StatusRunning, StatusCompleted, false},
		{"RUNNING to FAILED", StatusRunning, StatusFailed, false},
		{"RUNNING to CANCELLED", StatusRunning, StatusCancelled, false},
		{"RUNNING to TIMED_OUT", StatusRunning, StatusTimedOut, false},
		// Invalid transitions
		{"IN_QUEUE to COMPLETED", StatusInQueue, StatusCompleted, true},
		{"IN_QUEUE to FAILED", StatusInQueue, StatusFailed, true},
		{"COMPLETED to IN_QUEUE", StatusCompleted, StatusInQueue, true},
		{"COMPLETED to RUNNING", StatusCompleted, StatusRunning, true},
		{"FAILED to RUNNING", StatusFailed, StatusRunning, true},
		{"FAILED to COMPLETED", StatusFailed, StatusCompleted, true},
		{"CANCELLED to RUNNING", StatusCancelled, StatusRunning, true},
		{"TIMED_OUT to RUNNING", StatusTimedOut, StatusRunning, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := NewWithID("test")
			job.Status = tt.from

			err := job.TransitionTo(tt.to)

			if tt.wantErr && err == nil {
				t.Errorf("expected error for transition %s -> %s", tt.from, tt.to)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for transition %s -> %s: %v", tt.from, tt.to, err)
			}
		})
	}
}

func TestJob_Start(t *testing.T) {
	job := New()
	beforeStart := time.Now()

	err := job.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Status != StatusRunning {
		t.Errorf("expected status %s, got %s", StatusRunning, job.Status)
	}
	if job.StartedAt.Before(beforeStart) {
		t.Error("expected StartedAt to be set after test start")
	}
}

func TestJob_Complete(t *testing.T) {
	job := New()
	_ = job.Start()

	err := job.Complete()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Status != StatusCompleted {
		t.Errorf("expected status %s, got %s", StatusCompleted, job.Status)
	}
	if job.CompletedAt.IsZero() {
		t.Error("expected CompletedAt to be set")
	}
}

func TestJob_Fail(t *testing.T) {
	job := New()
	_ = job.Start()

	errMsg := "something went wrong"
	err := job.Fail(errMsg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Status != StatusFailed {
		t.Errorf("expected status %s, got %s", StatusFailed, job.Status)
	}
	if job.Error != errMsg {
		t.Errorf("expected error %q, got %q", errMsg, job.Error)
	}
	if job.CompletedAt.IsZero() {
		t.Error("expected CompletedAt to be set on failure")
	}
}

func TestJob_Cancel(t *testing.T) {
	job := New()
	_ = job.Start()

	err := job.Cancel()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Status != StatusCancelled {
		t.Errorf("expected status %s, got %s", StatusCancelled, job.Status)
	}
}

func TestJob_Timeout(t *testing.T) {
	job := New()

	err := job.Timeout()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Status != StatusTimedOut {
		t.Errorf("expected status %s, got %s", StatusTimedOut, job.Status)
	}
}

func TestJob_CannotTransitionFromTerminalState(t *testing.T) {
	terminalStates := []Status{StatusCompleted, StatusFailed, StatusCancelled, StatusTimedOut}
	allStates := []Status{StatusInQueue, StatusRunning, StatusCompleted, StatusFailed, StatusCancelled, StatusTimedOut}

	for _, terminal := range terminalStates {
		for _, target := range allStates {
			t.Run(string(terminal)+"_to_"+string(target), func(t *testing.T) {
				job := NewWithID("test")
				job.Status = terminal

				err := job.TransitionTo(target)
				if err == nil {
					t.Errorf("expected error when transitioning from %s to %s", terminal, target)
				}
				if err != ErrInvalidTransition {
					t.Errorf("expected ErrInvalidTransition, got %v", err)
				}
			})
		}
	}
}

func TestJob_IsTerminal(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			job := NewWithID("test")
			job.Status = tt.status

			if got := job.IsTerminal(); got != tt.terminal {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.terminal)
			}
		})
	}
}

func TestJob_SetChunks(t *testing.T) {
	job := New()
	chunks := []Chunk{
		{ID: "chunk-1", Index: 0, Status: ChunkStatusPending},
		{ID: "chunk-2", Index: 1, Status: ChunkStatusPending},
	}

	job.SetChunks(chunks)

	if len(job.Chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(job.Chunks))
	}
	if job.Chunks[0].ID != "chunk-1" {
		t.Errorf("expected chunk ID chunk-1, got %s", job.Chunks[0].ID)
	}
}

func TestJob_UpdateChunk(t *testing.T) {
	job := New()
	job.SetChunks([]Chunk{
		{ID: "chunk-1", Index: 0, Status: ChunkStatusPending},
	})

	updatedChunk := Chunk{
		ID:          "chunk-1",
		Index:       0,
		Status:      ChunkStatusCompleted,
		RunPodJobID: "runpod-123",
	}
	job.UpdateChunk(0, updatedChunk)

	if job.Chunks[0].Status != ChunkStatusCompleted {
		t.Errorf("expected status %s, got %s", ChunkStatusCompleted, job.Chunks[0].Status)
	}
	if job.Chunks[0].RunPodJobID != "runpod-123" {
		t.Errorf("expected RunPodJobID runpod-123, got %s", job.Chunks[0].RunPodJobID)
	}
}

func TestJob_UpdateProgress(t *testing.T) {
	job := New()

	tests := []struct {
		input    int
		expected int
	}{
		{50, 50},
		{0, 0},
		{100, 100},
		{-10, 0},  // Clamped to 0
		{150, 100}, // Clamped to 100
	}

	for _, tt := range tests {
		job.UpdateProgress(tt.input)
		if job.Progress != tt.expected {
			t.Errorf("UpdateProgress(%d): expected %d, got %d", tt.input, tt.expected, job.Progress)
		}
	}
}

func TestJob_SetOutput(t *testing.T) {
	job := New()

	job.SetOutput("/tmp/video.mp4", "https://s3.example.com/video.mp4")

	if job.OutputVideoPath != "/tmp/video.mp4" {
		t.Errorf("expected OutputVideoPath /tmp/video.mp4, got %s", job.OutputVideoPath)
	}
	if job.VideoURL != "https://s3.example.com/video.mp4" {
		t.Errorf("expected VideoURL https://s3.example.com/video.mp4, got %s", job.VideoURL)
	}
}

func TestJob_Clone(t *testing.T) {
	job := New()
	job.Status = StatusRunning
	job.Progress = 50
	job.SetChunks([]Chunk{
		{ID: "chunk-1", Index: 0, Status: ChunkStatusCompleted},
	})

	clone := job.Clone()

	// Verify clone has same values
	if clone.ID != job.ID {
		t.Errorf("expected ID %s, got %s", job.ID, clone.ID)
	}
	if clone.Status != job.Status {
		t.Errorf("expected Status %s, got %s", job.Status, clone.Status)
	}
	if clone.Progress != job.Progress {
		t.Errorf("expected Progress %d, got %d", job.Progress, clone.Progress)
	}

	// Verify clone is independent
	clone.Status = StatusCompleted
	if job.Status == StatusCompleted {
		t.Error("modifying clone should not affect original")
	}

	// Verify chunks are independent
	clone.Chunks[0].Status = ChunkStatusFailed
	if job.Chunks[0].Status == ChunkStatusFailed {
		t.Error("modifying clone chunks should not affect original")
	}
}

func TestJob_GetStatus_ThreadSafe(t *testing.T) {
	job := New()

	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			_ = job.GetStatus()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = job.Start()
		}
		done <- true
	}()

	<-done
	<-done
	// If no race conditions, test passes
}
