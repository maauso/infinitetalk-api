package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrS3NotConfigured is returned when S3 operations are attempted
// without proper configuration.
var ErrS3NotConfigured = errors.New("S3 storage is not configured")

// LocalStorage implements the Storage interface using local disk.
// It stores temporary files in a configurable directory and does not
// support S3 operations unless wrapped with S3Storage.
type LocalStorage struct {
	tempDir string
}

// NewLocalStorage creates a new LocalStorage instance.
// The tempDir parameter specifies where temporary files are stored.
// If tempDir is empty, os.TempDir() is used.
// The directory is created if it doesn't exist.
func NewLocalStorage(tempDir string) (*LocalStorage, error) {
	if tempDir == "" {
		tempDir = filepath.Join(os.TempDir(), "infinitetalk")
	}

	if err := os.MkdirAll(tempDir, 0750); err != nil {
		return nil, fmt.Errorf("create temp directory: %w", err)
	}

	return &LocalStorage{tempDir: tempDir}, nil
}

// TempDir returns the temporary directory path.
func (s *LocalStorage) TempDir() string {
	return s.tempDir
}

// SaveTemp saves data to a temporary file and returns the file path.
// The name is used as a base for the filename with a unique suffix.
func (s *LocalStorage) SaveTemp(ctx context.Context, name string, data io.Reader) (string, error) {
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("context cancelled: %w", ctx.Err())
	default:
	}

	f, err := os.CreateTemp(s.tempDir, name+"_*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	fileName := f.Name()
	if _, err := io.Copy(f, data); err != nil {
		_ = f.Close()
		_ = os.Remove(fileName)
		return "", fmt.Errorf("write temp file: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(fileName)
		return "", fmt.Errorf("close temp file: %w", err)
	}

	return fileName, nil
}

// LoadTemp reads a temporary file and returns a reader.
// The caller is responsible for closing the returned ReadCloser.
func (s *LocalStorage) LoadTemp(ctx context.Context, path string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled: %w", ctx.Err())
	default:
	}

	f, err := os.Open(path) // #nosec G304 - path is provided by trusted caller
	if err != nil {
		return nil, fmt.Errorf("open temp file: %w", err)
	}

	return f, nil
}

// CleanupTemp removes the specified temporary files.
// It continues cleanup even if some files fail to delete,
// returning the first error encountered.
func (s *LocalStorage) CleanupTemp(ctx context.Context, paths []string) error {
	var firstErr error
	for _, p := range paths {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled: %w", ctx.Err())
		default:
		}

		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			if firstErr == nil {
				firstErr = fmt.Errorf("remove temp file %s: %w", p, err)
			}
		}
	}
	return firstErr
}

// UploadToS3 is not supported by LocalStorage and returns ErrS3NotConfigured.
func (s *LocalStorage) UploadToS3(_ context.Context, _ string, _ io.Reader) (string, error) {
	return "", ErrS3NotConfigured
}
