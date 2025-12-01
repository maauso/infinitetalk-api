package job

import (
	"context"
	"errors"
)

// ErrJobNotFound is returned when a job cannot be found by ID.
var ErrJobNotFound = errors.New("job not found")

// Repository defines the interface for job persistence.
// It acts as a port in the hexagonal architecture pattern.
type Repository interface {
	// Save persists a job to the storage.
	// If the job already exists, it should be updated.
	Save(ctx context.Context, job *Job) error

	// FindByID retrieves a job by its unique identifier.
	// Returns ErrJobNotFound if the job does not exist.
	FindByID(ctx context.Context, id string) (*Job, error)

	// List returns all jobs.
	List(ctx context.Context) ([]*Job, error)

	// Delete removes a job from storage.
	// Returns ErrJobNotFound if the job does not exist.
	Delete(ctx context.Context, id string) error
}
