// Package id provides unique identifier generation for jobs.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Generate creates a new unique job ID.
// Format: job-<timestamp>-<random>
// Example: job-1701432000-a1b2c3d4
func Generate() string {
	timestamp := time.Now().Unix()
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		// Fallback to timestamp only if crypto/rand fails
		return fmt.Sprintf("job-%d", timestamp)
	}
	return fmt.Sprintf("job-%d-%s", timestamp, hex.EncodeToString(random))
}
