package id

import (
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	id := Generate()

	// Check format
	if !strings.HasPrefix(id, "job-") {
		t.Errorf("expected ID to start with 'job-', got %s", id)
	}

	// Check uniqueness
	id2 := Generate()
	if id == id2 {
		t.Error("expected different IDs for consecutive calls")
	}
}

func TestGenerate_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := Generate()
		if seen[id] {
			t.Errorf("duplicate ID generated: %s", id)
		}
		seen[id] = true
	}
}
