// Package storage provides temporary and persistent file storage capabilities.
// It defines the Storage interface (port) for hexagonal architecture and
// implementations for local disk and S3 storage.
package storage

import (
	"context"
	"io"
)

// Storage defines the interface for temporary and persistent file storage.
// Implementations must handle temporary files during processing and
// optionally support S3 uploads for final video delivery.
type Storage interface {
	// SaveTemp saves data to a temporary file and returns the file path.
	// The name parameter is used as a hint for the filename.
	SaveTemp(ctx context.Context, name string, data io.Reader) (path string, err error)

	// LoadTemp reads a temporary file and returns a reader.
	// The caller is responsible for closing the returned ReadCloser.
	LoadTemp(ctx context.Context, path string) (io.ReadCloser, error)

	// CleanupTemp removes the specified temporary files.
	// It continues cleanup even if some files fail to delete.
	CleanupTemp(ctx context.Context, paths []string) error

	// UploadToS3 uploads data to S3 and returns the public URL.
	// Returns ErrS3NotConfigured if S3 is not configured.
	UploadToS3(ctx context.Context, key string, data io.Reader) (url string, err error)
}
