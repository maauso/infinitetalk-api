package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewS3Storage(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "infinitetalk_s3_test_"+randomSuffix())
	defer os.RemoveAll(tempDir)

	cfg := S3Config{
		Bucket:          "test-bucket",
		Region:          "us-east-1",
		Endpoint:        "http://localhost:4566", // LocalStack-like endpoint
		AccessKeyID:     "test-access-key",
		SecretAccessKey: "test-secret-key",
	}

	storage, err := NewS3Storage(tempDir, cfg)
	if err != nil {
		t.Fatalf("NewS3Storage() error = %v", err)
	}

	if storage.bucket != cfg.Bucket {
		t.Errorf("bucket = %v, want %v", storage.bucket, cfg.Bucket)
	}
	if storage.region != cfg.Region {
		t.Errorf("region = %v, want %v", storage.region, cfg.Region)
	}
}

func TestS3Storage_InheritsLocalStorage(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "infinitetalk_s3_test_"+randomSuffix())
	defer os.RemoveAll(tempDir)

	cfg := S3Config{
		Bucket:          "test-bucket",
		Region:          "us-east-1",
		Endpoint:        "http://localhost:4566",
		AccessKeyID:     "test-access-key",
		SecretAccessKey: "test-secret-key",
	}

	storage, err := NewS3Storage(tempDir, cfg)
	if err != nil {
		t.Fatalf("NewS3Storage() error = %v", err)
	}

	ctx := context.Background()

	// Test inherited SaveTemp
	path, err := storage.SaveTemp(ctx, "test", bytes.NewReader([]byte("test data")))
	if err != nil {
		t.Fatalf("SaveTemp() error = %v", err)
	}
	defer os.Remove(path)

	// Test inherited LoadTemp
	reader, err := storage.LoadTemp(ctx, path)
	if err != nil {
		t.Fatalf("LoadTemp() error = %v", err)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "test data" {
		t.Errorf("got %q, want %q", string(content), "test data")
	}

	// Test inherited CleanupTemp
	err = storage.CleanupTemp(ctx, []string{path})
	if err != nil {
		t.Fatalf("CleanupTemp() error = %v", err)
	}
}

func TestS3Storage_UploadToS3_MockServer(t *testing.T) {
	// Create a mock S3 server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT method, got %s", r.Method)
		}

		if !strings.Contains(r.URL.Path, "/test-key") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		if string(body) != "test content" {
			t.Errorf("unexpected body: %s", string(body))
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tempDir := filepath.Join(os.TempDir(), "infinitetalk_s3_mock_test_"+randomSuffix())
	defer os.RemoveAll(tempDir)

	cfg := S3Config{
		Bucket:          "test-bucket",
		Region:          "us-east-1",
		Endpoint:        server.URL,
		AccessKeyID:     "test-access-key",
		SecretAccessKey: "test-secret-key",
	}

	storage, err := NewS3Storage(tempDir, cfg)
	if err != nil {
		t.Fatalf("NewS3Storage() error = %v", err)
	}

	ctx := context.Background()
	url, err := storage.UploadToS3(ctx, "test-key", bytes.NewReader([]byte("test content")))
	if err != nil {
		t.Fatalf("UploadToS3() error = %v", err)
	}

	expectedURL := "https://test-bucket.s3.us-east-1.amazonaws.com/test-key"
	if url != expectedURL {
		t.Errorf("url = %v, want %v", url, expectedURL)
	}
}
