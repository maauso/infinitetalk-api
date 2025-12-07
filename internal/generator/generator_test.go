package generator

import "testing"

func TestStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"pending not terminal", StatusPending, false},
		{"in_queue not terminal", StatusInQueue, false},
		{"running not terminal", StatusRunning, false},
		{"completed is terminal", StatusCompleted, true},
		{"failed is terminal", StatusFailed, true},
		{"cancelled is terminal", StatusCancelled, true},
		{"timed_out is terminal", StatusTimedOut, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.want {
				t.Errorf("Status.IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}
