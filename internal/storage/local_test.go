package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewLocalStorage(t *testing.T) {
	t.Run("creates directory if not exists", func(t *testing.T) {
		tempDir := filepath.Join(os.TempDir(), "infinitetalk_test_"+randomSuffix())
		defer func() { _ = os.RemoveAll(tempDir) }()

		storage, err := NewLocalStorage(tempDir)
		if err != nil {
			t.Fatalf("NewLocalStorage() error = %v", err)
		}

		if storage.TempDir() != tempDir {
			t.Errorf("TempDir() = %v, want %v", storage.TempDir(), tempDir)
		}

		info, err := os.Stat(tempDir)
		if err != nil {
			t.Fatalf("directory not created: %v", err)
		}
		if !info.IsDir() {
			t.Error("expected directory, got file")
		}
	})

	t.Run("uses default directory when empty", func(t *testing.T) {
		storage, err := NewLocalStorage("")
		if err != nil {
			t.Fatalf("NewLocalStorage() error = %v", err)
		}

		expected := filepath.Join(os.TempDir(), "infinitetalk")
		if storage.TempDir() != expected {
			t.Errorf("TempDir() = %v, want %v", storage.TempDir(), expected)
		}
	})
}

func TestLocalStorage_SaveTemp(t *testing.T) {
	storage := setupTestStorage(t)

	t.Run("saves data to temp file", func(t *testing.T) {
		ctx := context.Background()
		data := bytes.NewReader([]byte("test data"))

		path, err := storage.SaveTemp(ctx, "test", data)
		if err != nil {
			t.Fatalf("SaveTemp() error = %v", err)
		}
		defer func() { _ = os.Remove(path) }()

		if !strings.Contains(path, "test_") {
			t.Errorf("path %s should contain 'test_'", path)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read saved file: %v", err)
		}
		if string(content) != "test data" {
			t.Errorf("got %q, want %q", string(content), "test data")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := storage.SaveTemp(ctx, "test", bytes.NewReader([]byte("data")))
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

func TestLocalStorage_LoadTemp(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	t.Run("loads saved file", func(t *testing.T) {
		path, err := storage.SaveTemp(ctx, "load_test", bytes.NewReader([]byte("load data")))
		if err != nil {
			t.Fatalf("SaveTemp() error = %v", err)
		}
		defer func() { _ = os.Remove(path) }()

		reader, err := storage.LoadTemp(ctx, path)
		if err != nil {
			t.Fatalf("LoadTemp() error = %v", err)
		}
		defer func() { _ = reader.Close() }()

		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("failed to read: %v", err)
		}
		if string(content) != "load data" {
			t.Errorf("got %q, want %q", string(content), "load data")
		}
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		_, err := storage.LoadTemp(ctx, "/non/existent/file")
		if err == nil {
			t.Error("expected error for non-existent file")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := storage.LoadTemp(ctx, "/some/path")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

func TestLocalStorage_CleanupTemp(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	t.Run("removes files", func(t *testing.T) {
		var paths []string
		for i := 0; i < 3; i++ {
			path, err := storage.SaveTemp(ctx, "cleanup", bytes.NewReader([]byte("data")))
			if err != nil {
				t.Fatalf("SaveTemp() error = %v", err)
			}
			paths = append(paths, path)
		}

		err := storage.CleanupTemp(ctx, paths)
		if err != nil {
			t.Fatalf("CleanupTemp() error = %v", err)
		}

		for _, p := range paths {
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Errorf("file %s still exists", p)
			}
		}
	})

	t.Run("ignores non-existent files", func(t *testing.T) {
		err := storage.CleanupTemp(ctx, []string{"/non/existent/file"})
		if err != nil {
			t.Errorf("CleanupTemp() should ignore non-existent files, got %v", err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := storage.CleanupTemp(ctx, []string{"/some/path"})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

func TestLocalStorage_UploadToS3(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	_, err := storage.UploadToS3(ctx, "key", bytes.NewReader([]byte("data")))
	if err != ErrS3NotConfigured {
		t.Errorf("expected ErrS3NotConfigured, got %v", err)
	}
}

func setupTestStorage(t *testing.T) *LocalStorage {
	t.Helper()
	tempDir := filepath.Join(os.TempDir(), "infinitetalk_test_"+randomSuffix())
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })

	storage, err := NewLocalStorage(tempDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	return storage
}

func randomSuffix() string {
	return time.Now().Format("20060102150405.000000000")
}
