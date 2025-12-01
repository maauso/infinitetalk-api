package job

import (
	"context"
	"sync"
)

// Compile-time check that MemoryRepository implements Repository.
var _ Repository = (*MemoryRepository)(nil)

// MemoryRepository is an in-memory implementation of Repository.
// It uses a map with RWMutex for thread-safe access.
// Suitable for development and testing; swap for persistent storage in production.
type MemoryRepository struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

// NewMemoryRepository creates a new in-memory job repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		jobs: make(map[string]*Job),
	}
}

// Save persists a job to the in-memory storage.
// Creates a clone to avoid external mutations.
func (r *MemoryRepository) Save(_ context.Context, job *Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs[job.ID] = job.Clone()
	return nil
}

// FindByID retrieves a job by its ID.
// Returns a clone to prevent external mutations.
func (r *MemoryRepository) FindByID(_ context.Context, id string) (*Job, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	job, ok := r.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}
	return job.Clone(), nil
}

// List returns all jobs in the repository.
// Returns clones to prevent external mutations.
func (r *MemoryRepository) List(_ context.Context) ([]*Job, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		result = append(result, job.Clone())
	}
	return result, nil
}

// Delete removes a job from storage.
func (r *MemoryRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.jobs[id]; !ok {
		return ErrJobNotFound
	}
	delete(r.jobs, id)
	return nil
}
