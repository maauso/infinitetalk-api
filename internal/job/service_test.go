package job

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/maauso/infinitetalk-api/internal/audio"
	"github.com/maauso/infinitetalk-api/internal/runpod"
	"github.com/stretchr/testify/mock"
)

// Mock implementations for testing

// mockProcessor implements media.Processor for testing
type mockProcessor struct {
	mock.Mock
}

func (m *mockProcessor) ResizeImageWithPadding(ctx context.Context, src, dst string, w, h int) error {
	args := m.Called(ctx, src, dst, w, h)
	return args.Error(0)
}

func (m *mockProcessor) JoinVideos(ctx context.Context, videoPaths []string, output string) error {
	args := m.Called(ctx, videoPaths, output)
	return args.Error(0)
}

func (m *mockProcessor) ExtractLastFrame(ctx context.Context, videoPath string) ([]byte, error) {
	args := m.Called(ctx, videoPath)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

// mockSplitter implements audio.Splitter for testing
type mockSplitter struct {
	mock.Mock
}

func (m *mockSplitter) Split(ctx context.Context, inputWav, outputDir string, opts audio.SplitOpts) ([]string, error) {
	args := m.Called(ctx, inputWav, outputDir, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

// mockRunpodClient implements runpod.Client for testing
type mockRunpodClient struct {
	mock.Mock
}

func (m *mockRunpodClient) Submit(ctx context.Context, imageB64, audioB64 string, opts runpod.SubmitOptions) (string, error) {
	args := m.Called(ctx, imageB64, audioB64, opts)
	return args.String(0), args.Error(1)
}

func (m *mockRunpodClient) Poll(ctx context.Context, jobID string) (runpod.PollResult, error) {
	args := m.Called(ctx, jobID)
	return args.Get(0).(runpod.PollResult), args.Error(1)
}

// mockStorage implements storage.Storage for testing
type mockStorage struct {
	mock.Mock
}

func (m *mockStorage) SaveTemp(ctx context.Context, name string, data io.Reader) (string, error) {
	args := m.Called(ctx, name, data)
	return args.String(0), args.Error(1)
}

func (m *mockStorage) LoadTemp(ctx context.Context, path string) (io.ReadCloser, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func (m *mockStorage) CleanupTemp(ctx context.Context, paths []string) error {
	args := m.Called(ctx, paths)
	return args.Error(0)
}

func (m *mockStorage) UploadToS3(ctx context.Context, key string, data io.Reader) (string, error) {
	args := m.Called(ctx, key, data)
	return args.String(0), args.Error(1)
}

// Helper function to create a test service with all mocks
func newTestService(t *testing.T) (*ProcessVideoService, *mockProcessor, *mockSplitter, *mockRunpodClient, *mockStorage, Repository) {
	repo := NewMemoryRepository()
	processor := &mockProcessor{}
	splitter := &mockSplitter{}
	runpodClient := &mockRunpodClient{}
	storageClient := &mockStorage{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	svc := NewProcessVideoService(repo, processor, splitter, runpodClient, storageClient, logger,
		WithPollInterval(10*time.Millisecond),
	)

	return svc, processor, splitter, runpodClient, storageClient, repo
}

func TestNewProcessVideoService(t *testing.T) {
	repo := NewMemoryRepository()
	processor := &mockProcessor{}
	splitter := &mockSplitter{}
	runpodClient := &mockRunpodClient{}
	storageClient := &mockStorage{}

	// With nil logger
	svc := NewProcessVideoService(repo, processor, splitter, runpodClient, storageClient, nil)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if svc.repo != repo {
		t.Error("expected repo to be set")
	}

	// With custom logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc2 := NewProcessVideoService(repo, processor, splitter, runpodClient, storageClient, logger)
	if svc2.logger != logger {
		t.Error("expected custom logger to be set")
	}
}

func TestNewProcessVideoService_WithOptions(t *testing.T) {
	repo := NewMemoryRepository()
	processor := &mockProcessor{}
	splitter := &mockSplitter{}
	runpodClient := &mockRunpodClient{}
	storageClient := &mockStorage{}

	svc := NewProcessVideoService(repo, processor, splitter, runpodClient, storageClient, nil,
		WithSplitOpts(audio.SplitOpts{ChunkTargetSec: 30}),
		WithPollInterval(10*time.Second),
	)

	if svc.splitOpts.ChunkTargetSec != 30 {
		t.Errorf("expected ChunkTargetSec 30, got %d", svc.splitOpts.ChunkTargetSec)
	}
	if svc.pollInterval != 10*time.Second {
		t.Errorf("expected pollInterval 10s, got %v", svc.pollInterval)
	}
}

func TestProcessVideoService_CreateJob(t *testing.T) {
	svc, _, _, _, _, repo := newTestService(t)
	ctx := context.Background()

	input := ProcessVideoInput{
		ImageBase64: base64.StdEncoding.EncodeToString([]byte("test-image")),
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("test-audio")),
		Width:       384,
		Height:      576,
		PushToS3:    true,
	}

	job, err := svc.CreateJob(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.ID == "" {
		t.Error("expected job ID to be set")
	}
	if job.Status != StatusInQueue {
		t.Errorf("expected status %s, got %s", StatusInQueue, job.Status)
	}
	if job.Width != 384 {
		t.Errorf("expected width 384, got %d", job.Width)
	}
	if job.Height != 576 {
		t.Errorf("expected height 576, got %d", job.Height)
	}
	if !job.PushToS3 {
		t.Error("expected PushToS3 to be true")
	}

	// Verify job was saved
	saved, err := repo.FindByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("job should be saved in repository: %v", err)
	}
	if saved.ID != job.ID {
		t.Errorf("saved job ID mismatch: expected %s, got %s", job.ID, saved.ID)
	}
}

func TestProcessVideoService_GetJob(t *testing.T) {
	svc, _, _, _, _, _ := newTestService(t)
	ctx := context.Background()

	// Create job first
	created, err := svc.CreateJob(ctx, ProcessVideoInput{Width: 512, Height: 512})
	if err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	// Get job
	found, err := svc.GetJob(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, found.ID)
	}
}

func TestProcessVideoService_GetJob_NotFound(t *testing.T) {
	svc, _, _, _, _, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.GetJob(ctx, "nonexistent")
	if !errors.Is(err, ErrJobNotFound) {
		t.Errorf("expected ErrJobNotFound, got %v", err)
	}
}

func TestProcessVideoService_Process_Success(t *testing.T) {
	svc, processor, splitter, runpodClient, storageClient, repo := newTestService(t)
	ctx := context.Background()

	imageData := []byte("test-image-data")
	audioData := []byte("test-audio-data")
	videoData := []byte("test-video-data")
	imageB64 := base64.StdEncoding.EncodeToString(imageData)
	audioB64 := base64.StdEncoding.EncodeToString(audioData)
	videoB64 := base64.StdEncoding.EncodeToString(videoData)

	input := ProcessVideoInput{
		ImageBase64: imageB64,
		AudioBase64: audioB64,
		Width:       384,
		Height:      576,
		PushToS3:    false,
	}

	// Setup mock expectations
	storageClient.On("SaveTemp", mock.Anything, "image.png", mock.Anything).Return("/tmp/image.png", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, "audio.wav", mock.Anything).Return("/tmp/audio.wav", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, mock.MatchedBy(func(s string) bool {
		return len(s) > 5 && s[:6] == "chunk_"
	}), mock.Anything).Return("/tmp/chunk_0.mp4", nil).Once()
	storageClient.On("CleanupTemp", mock.Anything, mock.Anything).Return(nil)

	processor.On("ResizeImageWithPadding", mock.Anything, "/tmp/image.png", mock.Anything, 384, 576).
		Run(func(args mock.Arguments) {
			// Create the resized image file so fileToBase64 can read it
			dst := args.Get(2).(string)
			_ = os.WriteFile(dst, imageData, 0644)
		}).
		Return(nil).Once()
	processor.On("JoinVideos", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	splitter.On("Split", mock.Anything, "/tmp/audio.wav", "/tmp", mock.Anything).
		Return([]string{"/tmp/chunk_0.wav"}, nil).Once()

	// Mock RunPod - first call submits, second call polls and returns completed
	runpodClient.On("Submit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("runpod-job-123", nil).Once()
	runpodClient.On("Poll", mock.Anything, "runpod-job-123").
		Return(runpod.PollResult{Status: runpod.StatusCompleted, VideoBase64: videoB64}, nil).Once()

	// Create a temp file for the audio chunk so fileToBase64 works
	_ = os.WriteFile("/tmp/chunk_0.wav", audioData, 0644)
	defer os.Remove("/tmp/chunk_0.wav")

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.JobID == "" {
		t.Error("expected JobID to be set")
	}
	if output.Status != StatusCompleted {
		t.Errorf("expected status %s, got %s", StatusCompleted, output.Status)
	}
	if output.Error != "" {
		t.Errorf("expected no error, got %s", output.Error)
	}

	// Verify job in repository
	job, err := repo.FindByID(ctx, output.JobID)
	if err != nil {
		t.Fatalf("job should exist in repository: %v", err)
	}
	if job.Status != StatusCompleted {
		t.Errorf("expected job status COMPLETED, got %s", job.Status)
	}
	if job.Progress != 100 {
		t.Errorf("expected progress 100, got %d", job.Progress)
	}

	// Verify mock expectations
	processor.AssertExpectations(t)
	splitter.AssertExpectations(t)
	runpodClient.AssertExpectations(t)
	storageClient.AssertExpectations(t)

	// Cleanup temp file created by test
	os.Remove("/tmp/image.png")
}

func TestProcessVideoService_Process_SaveImageFails(t *testing.T) {
	svc, _, _, _, storageClient, _ := newTestService(t)
	ctx := context.Background()

	input := ProcessVideoInput{
		ImageBase64: base64.StdEncoding.EncodeToString([]byte("test-image")),
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("test-audio")),
		Width:       384,
		Height:      576,
	}

	storageClient.On("SaveTemp", mock.Anything, "image.png", mock.Anything).
		Return("", errors.New("storage error")).Once()
	// CleanupTemp should NOT be called because no temp files were created

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("Process should not return error, got: %v", err)
	}

	if output.Status != StatusFailed {
		t.Errorf("expected status FAILED, got %s", output.Status)
	}
	if output.Error == "" {
		t.Error("expected error message")
	}

	storageClient.AssertExpectations(t)
}

func TestProcessVideoService_Process_ResizeImageFails(t *testing.T) {
	svc, processor, _, _, storageClient, _ := newTestService(t)
	ctx := context.Background()

	input := ProcessVideoInput{
		ImageBase64: base64.StdEncoding.EncodeToString([]byte("test-image")),
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("test-audio")),
		Width:       384,
		Height:      576,
	}

	storageClient.On("SaveTemp", mock.Anything, "image.png", mock.Anything).Return("/tmp/image.png", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, "audio.wav", mock.Anything).Return("/tmp/audio.wav", nil).Once()
	storageClient.On("CleanupTemp", mock.Anything, mock.Anything).Return(nil)

	processor.On("ResizeImageWithPadding", mock.Anything, "/tmp/image.png", mock.Anything, 384, 576).
		Return(errors.New("resize error")).Once()

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("Process should not return error, got: %v", err)
	}

	if output.Status != StatusFailed {
		t.Errorf("expected status FAILED, got %s", output.Status)
	}
	if output.Error == "" {
		t.Error("expected error message")
	}

	processor.AssertExpectations(t)
	storageClient.AssertExpectations(t)
}

func TestProcessVideoService_Process_SplitAudioFails(t *testing.T) {
	svc, processor, splitter, _, storageClient, _ := newTestService(t)
	ctx := context.Background()

	imageData := []byte("test-image-data")
	input := ProcessVideoInput{
		ImageBase64: base64.StdEncoding.EncodeToString(imageData),
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("test-audio")),
		Width:       384,
		Height:      576,
	}

	storageClient.On("SaveTemp", mock.Anything, "image.png", mock.Anything).Return("/tmp/image.png", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, "audio.wav", mock.Anything).Return("/tmp/audio.wav", nil).Once()
	storageClient.On("CleanupTemp", mock.Anything, mock.Anything).Return(nil)

	processor.On("ResizeImageWithPadding", mock.Anything, "/tmp/image.png", mock.Anything, 384, 576).
		Run(func(args mock.Arguments) {
			dst := args.Get(2).(string)
			_ = os.WriteFile(dst, imageData, 0644)
		}).
		Return(nil).Once()

	splitter.On("Split", mock.Anything, "/tmp/audio.wav", "/tmp", mock.Anything).
		Return(nil, errors.New("split error")).Once()

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("Process should not return error, got: %v", err)
	}

	if output.Status != StatusFailed {
		t.Errorf("expected status FAILED, got %s", output.Status)
	}

	processor.AssertExpectations(t)
	splitter.AssertExpectations(t)
	storageClient.AssertExpectations(t)

	// Cleanup
	os.Remove("/tmp/image.png")
}

func TestProcessVideoService_Process_RunPodSubmitFails(t *testing.T) {
	svc, processor, splitter, runpodClient, storageClient, _ := newTestService(t)
	ctx := context.Background()

	imageData := []byte("test-image-data")
	audioData := []byte("test-audio-data")
	input := ProcessVideoInput{
		ImageBase64: base64.StdEncoding.EncodeToString(imageData),
		AudioBase64: base64.StdEncoding.EncodeToString(audioData),
		Width:       384,
		Height:      576,
	}

	storageClient.On("SaveTemp", mock.Anything, "image.png", mock.Anything).Return("/tmp/image.png", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, "audio.wav", mock.Anything).Return("/tmp/audio.wav", nil).Once()
	storageClient.On("CleanupTemp", mock.Anything, mock.Anything).Return(nil)

	processor.On("ResizeImageWithPadding", mock.Anything, "/tmp/image.png", mock.Anything, 384, 576).
		Run(func(args mock.Arguments) {
			dst := args.Get(2).(string)
			_ = os.WriteFile(dst, imageData, 0644)
		}).
		Return(nil).Once()

	splitter.On("Split", mock.Anything, "/tmp/audio.wav", "/tmp", mock.Anything).
		Return([]string{"/tmp/chunk_0.wav"}, nil).Once()

	// Create the audio chunk file so fileToBase64 works
	_ = os.WriteFile("/tmp/chunk_0.wav", audioData, 0644)
	defer os.Remove("/tmp/chunk_0.wav")

	runpodClient.On("Submit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", errors.New("runpod submit error")).Once()

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("Process should not return error, got: %v", err)
	}

	if output.Status != StatusFailed {
		t.Errorf("expected status FAILED, got %s", output.Status)
	}

	processor.AssertExpectations(t)
	splitter.AssertExpectations(t)
	runpodClient.AssertExpectations(t)
	storageClient.AssertExpectations(t)

	// Cleanup
	os.Remove("/tmp/image.png")
}

func TestProcessVideoService_Process_RunPodPollFails(t *testing.T) {
	svc, processor, splitter, runpodClient, storageClient, _ := newTestService(t)
	ctx := context.Background()

	imageData := []byte("test-image-data")
	audioData := []byte("test-audio-data")
	input := ProcessVideoInput{
		ImageBase64: base64.StdEncoding.EncodeToString(imageData),
		AudioBase64: base64.StdEncoding.EncodeToString(audioData),
		Width:       384,
		Height:      576,
	}

	storageClient.On("SaveTemp", mock.Anything, "image.png", mock.Anything).Return("/tmp/image.png", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, "audio.wav", mock.Anything).Return("/tmp/audio.wav", nil).Once()
	storageClient.On("CleanupTemp", mock.Anything, mock.Anything).Return(nil)

	processor.On("ResizeImageWithPadding", mock.Anything, "/tmp/image.png", mock.Anything, 384, 576).
		Run(func(args mock.Arguments) {
			dst := args.Get(2).(string)
			_ = os.WriteFile(dst, imageData, 0644)
		}).
		Return(nil).Once()

	splitter.On("Split", mock.Anything, "/tmp/audio.wav", "/tmp", mock.Anything).
		Return([]string{"/tmp/chunk_0.wav"}, nil).Once()

	// Create the audio chunk file
	_ = os.WriteFile("/tmp/chunk_0.wav", audioData, 0644)
	defer os.Remove("/tmp/chunk_0.wav")

	runpodClient.On("Submit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("runpod-job-123", nil).Once()
	runpodClient.On("Poll", mock.Anything, "runpod-job-123").
		Return(runpod.PollResult{Status: runpod.StatusFailed, Error: "RunPod processing error"}, nil).Once()

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("Process should not return error, got: %v", err)
	}

	if output.Status != StatusFailed {
		t.Errorf("expected status FAILED, got %s", output.Status)
	}

	processor.AssertExpectations(t)
	splitter.AssertExpectations(t)
	runpodClient.AssertExpectations(t)
	storageClient.AssertExpectations(t)

	// Cleanup
	os.Remove("/tmp/image.png")
}

func TestProcessVideoService_Process_JoinVideosFails(t *testing.T) {
	svc, processor, splitter, runpodClient, storageClient, _ := newTestService(t)
	ctx := context.Background()

	imageData := []byte("test-image-data")
	audioData := []byte("test-audio-data")
	videoData := []byte("test-video-data")
	videoB64 := base64.StdEncoding.EncodeToString(videoData)
	input := ProcessVideoInput{
		ImageBase64: base64.StdEncoding.EncodeToString(imageData),
		AudioBase64: base64.StdEncoding.EncodeToString(audioData),
		Width:       384,
		Height:      576,
	}

	storageClient.On("SaveTemp", mock.Anything, "image.png", mock.Anything).Return("/tmp/image.png", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, "audio.wav", mock.Anything).Return("/tmp/audio.wav", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, mock.MatchedBy(func(s string) bool {
		return len(s) > 5 && s[:6] == "chunk_"
	}), mock.Anything).Return("/tmp/chunk_0.mp4", nil).Once()
	storageClient.On("CleanupTemp", mock.Anything, mock.Anything).Return(nil)

	processor.On("ResizeImageWithPadding", mock.Anything, "/tmp/image.png", mock.Anything, 384, 576).
		Run(func(args mock.Arguments) {
			dst := args.Get(2).(string)
			_ = os.WriteFile(dst, imageData, 0644)
		}).
		Return(nil).Once()
	processor.On("JoinVideos", mock.Anything, mock.Anything, mock.Anything).
		Return(errors.New("join error")).Once()

	splitter.On("Split", mock.Anything, "/tmp/audio.wav", "/tmp", mock.Anything).
		Return([]string{"/tmp/chunk_0.wav"}, nil).Once()

	// Create the audio chunk file
	_ = os.WriteFile("/tmp/chunk_0.wav", audioData, 0644)
	defer os.Remove("/tmp/chunk_0.wav")

	runpodClient.On("Submit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("runpod-job-123", nil).Once()
	runpodClient.On("Poll", mock.Anything, "runpod-job-123").
		Return(runpod.PollResult{Status: runpod.StatusCompleted, VideoBase64: videoB64}, nil).Once()

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("Process should not return error, got: %v", err)
	}

	if output.Status != StatusFailed {
		t.Errorf("expected status FAILED, got %s", output.Status)
	}

	processor.AssertExpectations(t)
	splitter.AssertExpectations(t)
	runpodClient.AssertExpectations(t)
	storageClient.AssertExpectations(t)

	// Cleanup
	os.Remove("/tmp/image.png")
}

func TestProcessVideoService_Process_WithS3Upload(t *testing.T) {
	svc, processor, splitter, runpodClient, storageClient, _ := newTestService(t)
	ctx := context.Background()

	imageData := []byte("test-image-data")
	audioData := []byte("test-audio-data")
	videoData := []byte("test-video-data")
	videoB64 := base64.StdEncoding.EncodeToString(videoData)
	input := ProcessVideoInput{
		ImageBase64: base64.StdEncoding.EncodeToString(imageData),
		AudioBase64: base64.StdEncoding.EncodeToString(audioData),
		Width:       384,
		Height:      576,
		PushToS3:    true,
	}

	storageClient.On("SaveTemp", mock.Anything, "image.png", mock.Anything).Return("/tmp/image.png", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, "audio.wav", mock.Anything).Return("/tmp/audio.wav", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, mock.MatchedBy(func(s string) bool {
		return len(s) > 5 && s[:6] == "chunk_"
	}), mock.Anything).Return("/tmp/chunk_0.mp4", nil).Once()
	storageClient.On("UploadToS3", mock.Anything, mock.MatchedBy(func(s string) bool {
		return len(s) > 7 && s[:7] == "videos/"
	}), mock.Anything).Return("https://s3.example.com/videos/output.mp4", nil).Once()
	storageClient.On("CleanupTemp", mock.Anything, mock.Anything).Return(nil)

	processor.On("ResizeImageWithPadding", mock.Anything, "/tmp/image.png", mock.Anything, 384, 576).
		Run(func(args mock.Arguments) {
			dst := args.Get(2).(string)
			_ = os.WriteFile(dst, imageData, 0644)
		}).
		Return(nil).Once()
	processor.On("JoinVideos", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			output := args.Get(2).(string)
			_ = os.WriteFile(output, videoData, 0644)
		}).
		Return(nil).Once()

	splitter.On("Split", mock.Anything, "/tmp/audio.wav", "/tmp", mock.Anything).
		Return([]string{"/tmp/chunk_0.wav"}, nil).Once()

	_ = os.WriteFile("/tmp/chunk_0.wav", audioData, 0644)
	defer os.Remove("/tmp/chunk_0.wav")

	runpodClient.On("Submit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("runpod-job-123", nil).Once()
	runpodClient.On("Poll", mock.Anything, "runpod-job-123").
		Return(runpod.PollResult{Status: runpod.StatusCompleted, VideoBase64: videoB64}, nil).Once()

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Status != StatusCompleted {
		t.Errorf("expected status COMPLETED, got %s", output.Status)
	}
	if output.VideoURL != "https://s3.example.com/videos/output.mp4" {
		t.Errorf("expected S3 URL, got %s", output.VideoURL)
	}

	processor.AssertExpectations(t)
	splitter.AssertExpectations(t)
	runpodClient.AssertExpectations(t)
	storageClient.AssertExpectations(t)

	// Cleanup
	os.Remove("/tmp/image.png")
}

func TestProcessVideoService_Process_MultipleChunks(t *testing.T) {
	svc, processor, splitter, runpodClient, storageClient, repo := newTestService(t)
	ctx := context.Background()

	imageData := []byte("test-image-data")
	audioData := []byte("test-audio-data")
	videoData := []byte("test-video-data")
	videoB64 := base64.StdEncoding.EncodeToString(videoData)
	input := ProcessVideoInput{
		ImageBase64: base64.StdEncoding.EncodeToString(imageData),
		AudioBase64: base64.StdEncoding.EncodeToString(audioData),
		Width:       384,
		Height:      576,
	}

	storageClient.On("SaveTemp", mock.Anything, "image.png", mock.Anything).Return("/tmp/image.png", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, "audio.wav", mock.Anything).Return("/tmp/audio.wav", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, mock.MatchedBy(func(s string) bool {
		return len(s) > 5 && s[:6] == "chunk_"
	}), mock.Anything).Return("/tmp/chunk.mp4", nil).Times(3)
	storageClient.On("CleanupTemp", mock.Anything, mock.Anything).Return(nil)

	processor.On("ResizeImageWithPadding", mock.Anything, "/tmp/image.png", mock.Anything, 384, 576).
		Run(func(args mock.Arguments) {
			dst := args.Get(2).(string)
			_ = os.WriteFile(dst, imageData, 0644)
		}).
		Return(nil).Once()
	processor.On("JoinVideos", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	// ExtractLastFrame is called for each chunk except the last one (3 chunks = 2 calls)
	processor.On("ExtractLastFrame", mock.Anything, mock.AnythingOfType("string")).
		Return([]byte("fake-frame-data"), nil).Times(2)

	// Return 3 audio chunks
	splitter.On("Split", mock.Anything, "/tmp/audio.wav", "/tmp", mock.Anything).
		Return([]string{"/tmp/chunk_0.wav", "/tmp/chunk_1.wav", "/tmp/chunk_2.wav"}, nil).Once()

	// Create chunk files
	for i := 0; i < 3; i++ {
		_ = os.WriteFile(fmt.Sprintf("/tmp/chunk_%d.wav", i), audioData, 0644)
	}
	defer func() {
		for i := 0; i < 3; i++ {
			os.Remove(fmt.Sprintf("/tmp/chunk_%d.wav", i))
		}
	}()

	// Mock RunPod for each chunk
	runpodClient.On("Submit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("runpod-job", nil).Times(3)
	runpodClient.On("Poll", mock.Anything, "runpod-job").
		Return(runpod.PollResult{Status: runpod.StatusCompleted, VideoBase64: videoB64}, nil).Times(3)

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Status != StatusCompleted {
		t.Errorf("expected status COMPLETED, got %s", output.Status)
	}

	// Verify job has correct chunk count
	job, _ := repo.FindByID(ctx, output.JobID)
	if len(job.Chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(job.Chunks))
	}

	processor.AssertExpectations(t)
	splitter.AssertExpectations(t)
	runpodClient.AssertExpectations(t)
	storageClient.AssertExpectations(t)

	// Cleanup
	os.Remove("/tmp/image.png")
}

func TestProcessVideoService_Process_ContextCancellation(t *testing.T) {
	svc, processor, splitter, runpodClient, storageClient, _ := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())

	imageData := []byte("test-image-data")
	audioData := []byte("test-audio-data")
	input := ProcessVideoInput{
		ImageBase64: base64.StdEncoding.EncodeToString(imageData),
		AudioBase64: base64.StdEncoding.EncodeToString(audioData),
		Width:       384,
		Height:      576,
	}

	storageClient.On("SaveTemp", mock.Anything, "image.png", mock.Anything).Return("/tmp/image.png", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, "audio.wav", mock.Anything).Return("/tmp/audio.wav", nil).Once()
	storageClient.On("CleanupTemp", mock.Anything, mock.Anything).Return(nil)

	processor.On("ResizeImageWithPadding", mock.Anything, "/tmp/image.png", mock.Anything, 384, 576).
		Run(func(args mock.Arguments) {
			dst := args.Get(2).(string)
			_ = os.WriteFile(dst, imageData, 0644)
		}).
		Return(nil).Once()

	splitter.On("Split", mock.Anything, "/tmp/audio.wav", "/tmp", mock.Anything).
		Return([]string{"/tmp/chunk_0.wav"}, nil).Once()

	_ = os.WriteFile("/tmp/chunk_0.wav", audioData, 0644)
	defer os.Remove("/tmp/chunk_0.wav")

	runpodClient.On("Submit", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("runpod-job-123", nil).Once()
	runpodClient.On("Poll", mock.Anything, "runpod-job-123").
		Run(func(args mock.Arguments) {
			// Cancel context during poll
			cancel()
		}).
		Return(runpod.PollResult{Status: runpod.StatusRunning}, nil).Maybe()

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("Process should not return error, got: %v", err)
	}

	if output.Status != StatusFailed {
		t.Errorf("expected status FAILED due to context cancellation, got %s", output.Status)
	}

	// Cleanup
	os.Remove("/tmp/image.png")
}

func TestProcessVideoService_Process_InvalidBase64Image(t *testing.T) {
	svc, _, _, _, _, _ := newTestService(t)
	ctx := context.Background()

	input := ProcessVideoInput{
		ImageBase64: "not-valid-base64!!!",
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("test-audio")),
		Width:       384,
		Height:      576,
	}

	// No CleanupTemp expectation - the base64 decoding fails before any temp file is created

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("Process should not return error, got: %v", err)
	}

	if output.Status != StatusFailed {
		t.Errorf("expected status FAILED, got %s", output.Status)
	}
	if output.Error == "" {
		t.Error("expected error message for invalid base64")
	}
}

func TestProcessVideoService_pollForResult_PollingWithRetry(t *testing.T) {
	svc, _, _, runpodClient, _, _ := newTestService(t)
	ctx := context.Background()

	videoB64 := base64.StdEncoding.EncodeToString([]byte("video-data"))

	// First call returns IN_QUEUE, second returns RUNNING, third returns COMPLETED
	runpodClient.On("Poll", mock.Anything, "job-123").
		Return(runpod.PollResult{Status: runpod.StatusInQueue}, nil).Once()
	runpodClient.On("Poll", mock.Anything, "job-123").
		Return(runpod.PollResult{Status: runpod.StatusRunning}, nil).Once()
	runpodClient.On("Poll", mock.Anything, "job-123").
		Return(runpod.PollResult{Status: runpod.StatusCompleted, VideoBase64: videoB64}, nil).Once()

	result, err := svc.pollForResult(ctx, "test-job", 0, "job-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != videoB64 {
		t.Error("expected video base64 in result")
	}

	runpodClient.AssertExpectations(t)
}

func TestFileToBase64(t *testing.T) {
	svc, _, _, _, _, _ := newTestService(t)

	// Create a temp file
	content := []byte("test content")
	tmpFile := "/tmp/test_file_to_base64.txt"
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile)

	result, err := svc.fileToBase64(tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(result)
	if err != nil {
		t.Fatalf("result is not valid base64: %v", err)
	}

	if !bytes.Equal(decoded, content) {
		t.Errorf("decoded content doesn't match: expected %s, got %s", content, decoded)
	}
}

func TestFileToBase64_NonExistentFile(t *testing.T) {
	svc, _, _, _, _, _ := newTestService(t)

	_, err := svc.fileToBase64("/tmp/nonexistent_file_12345.txt")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestProcessVideoService_Process_DryRun(t *testing.T) {
	svc, processor, splitter, runpodClient, storageClient, repo := newTestService(t)
	ctx := context.Background()

	imageData := []byte("test-image-data")
	audioData := []byte("test-audio-data")
	imageB64 := base64.StdEncoding.EncodeToString(imageData)
	audioB64 := base64.StdEncoding.EncodeToString(audioData)

	input := ProcessVideoInput{
		ImageBase64: imageB64,
		AudioBase64: audioB64,
		Width:       384,
		Height:      576,
		PushToS3:    false,
		DryRun:      true,
	}

	// Setup mock expectations - only preprocessing steps should be called
	storageClient.On("SaveTemp", mock.Anything, "image.png", mock.Anything).Return("/tmp/image.png", nil).Once()
	storageClient.On("SaveTemp", mock.Anything, "audio.wav", mock.Anything).Return("/tmp/audio.wav", nil).Once()
	storageClient.On("CleanupTemp", mock.Anything, mock.Anything).Return(nil)

	processor.On("ResizeImageWithPadding", mock.Anything, "/tmp/image.png", mock.Anything, 384, 576).
		Run(func(args mock.Arguments) {
			dst := args.Get(2).(string)
			_ = os.WriteFile(dst, imageData, 0644)
		}).
		Return(nil).Once()

	splitter.On("Split", mock.Anything, "/tmp/audio.wav", "/tmp", mock.Anything).
		Return([]string{"/tmp/chunk_0.wav", "/tmp/chunk_1.wav"}, nil).Once()

	output, err := svc.Process(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.JobID == "" {
		t.Error("expected JobID to be set")
	}
	if output.Status != StatusCompleted {
		t.Errorf("expected status %s, got %s", StatusCompleted, output.Status)
	}
	if output.Error != "" {
		t.Errorf("expected no error, got %s", output.Error)
	}

	// Verify job in repository
	job, err := repo.FindByID(ctx, output.JobID)
	if err != nil {
		t.Fatalf("job should exist in repository: %v", err)
	}
	if job.Status != StatusCompleted {
		t.Errorf("expected job status COMPLETED, got %s", job.Status)
	}
	if job.Progress != 100 {
		t.Errorf("expected progress 100, got %d", job.Progress)
	}
	if len(job.Chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(job.Chunks))
	}

	// Verify RunPod was NOT called (dry-run should skip it)
	runpodClient.AssertNotCalled(t, "Submit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	runpodClient.AssertNotCalled(t, "Poll", mock.Anything, mock.Anything)

	// Verify preprocessing mocks were called
	processor.AssertExpectations(t)
	splitter.AssertExpectations(t)
	storageClient.AssertExpectations(t)

	// Cleanup temp file created by test
	os.Remove("/tmp/image.png")
}
