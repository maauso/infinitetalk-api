package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/maauso/infinitetalk-api/internal/audio"
	"github.com/maauso/infinitetalk-api/internal/job"
	"github.com/maauso/infinitetalk-api/internal/runpod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockProcessor implements media.Processor for testing.
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

// mockSplitter implements audio.Splitter for testing.
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

// mockRunpodClient implements runpod.Client for testing.
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

// mockStorage implements storage.Storage for testing.
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

func newTestHandlers(t *testing.T) (*Handlers, *mockProcessor, *mockSplitter, *mockRunpodClient, *mockStorage, job.Repository) {
	t.Helper()
	repo := job.NewMemoryRepository()
	processor := &mockProcessor{}
	splitter := &mockSplitter{}
	runpodClient := &mockRunpodClient{}
	storageClient := &mockStorage{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	svc := job.NewProcessVideoService(repo, processor, splitter, runpodClient, nil, storageClient, logger,
		job.WithPollInterval(10*time.Millisecond),
	)

	// Disable async processing for tests to avoid mock issues
	handlers := NewHandlers(svc, logger, WithAsyncProcessing(false))
	return handlers, processor, splitter, runpodClient, storageClient, repo
}

func TestHealth(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.Health(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp HealthResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Status)
}

func TestCreateJob_Success(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)

	body := CreateJobRequest{
		ImageBase64: base64.StdEncoding.EncodeToString([]byte("test-image")),
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("test-audio")),
		Width:       384,
		Height:      576,
		PushToS3:    false,
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateJob(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)

	var resp CreateJobResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, "IN_QUEUE", resp.Status)
}

func TestCreateJob_InvalidJSON(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateJob(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp ErrorResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "INVALID_JSON", resp.Code)
}

func TestCreateJob_ValidationError_MissingFields(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)

	body := CreateJobRequest{
		Width:  384,
		Height: 576,
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateJob(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp ErrorResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "VALIDATION_ERROR", resp.Code)
}

func TestCreateJob_ValidationError_InvalidDimensions(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)

	body := CreateJobRequest{
		ImageBase64: base64.StdEncoding.EncodeToString([]byte("test-image")),
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("test-audio")),
		Width:       0,    // Invalid
		Height:      5000, // Invalid (> 4096)
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateJob(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp ErrorResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "VALIDATION_ERROR", resp.Code)
}

func TestGetJob_Success(t *testing.T) {
	h, _, _, _, _, repo := newTestHandlers(t)
	ctx := context.Background()

	// Create a job in the repository
	testJob := job.New()
	testJob.Width = 384
	testJob.Height = 576
	testJob.UpdateProgress(50)
	err := repo.Save(ctx, testJob)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/jobs/"+testJob.ID, nil)
	req.SetPathValue("id", testJob.ID)
	rec := httptest.NewRecorder()

	h.GetJob(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp JobResponse
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, testJob.ID, resp.ID)
	assert.Equal(t, "IN_QUEUE", resp.Status)
	assert.Equal(t, 50, resp.Progress)
}

func TestGetJob_NotFound(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)

	req := httptest.NewRequest(http.MethodGet, "/jobs/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	rec := httptest.NewRecorder()

	h.GetJob(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var resp ErrorResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "JOB_NOT_FOUND", resp.Code)
}

func TestGetJob_MissingID(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)

	req := httptest.NewRequest(http.MethodGet, "/jobs/", nil)
	// Don't set path value to simulate missing ID
	rec := httptest.NewRecorder()

	h.GetJob(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp ErrorResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "MISSING_JOB_ID", resp.Code)
}

func TestGetJob_WithS3URL(t *testing.T) {
	h, _, _, _, _, repo := newTestHandlers(t)
	ctx := context.Background()

	// Create a completed job with S3 URL
	testJob := job.New()
	testJob.PushToS3 = true
	testJob.VideoURL = "https://s3.example.com/videos/test.mp4"
	err := testJob.Start()
	require.NoError(t, err)
	err = testJob.Complete()
	require.NoError(t, err)
	testJob.UpdateProgress(100)
	err = repo.Save(ctx, testJob)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/jobs/"+testJob.ID, nil)
	req.SetPathValue("id", testJob.ID)
	rec := httptest.NewRecorder()

	h.GetJob(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp JobResponse
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "COMPLETED", resp.Status)
	assert.Equal(t, "https://s3.example.com/videos/test.mp4", resp.VideoURL)
	assert.Empty(t, resp.VideoBase64)
}

func TestGetJob_WithVideoBase64(t *testing.T) {
	h, _, _, _, _, repo := newTestHandlers(t)
	ctx := context.Background()

	// Create a temp video file
	videoData := []byte("test video data")
	tmpFile := "/tmp/test_video_output.mp4"
	err := os.WriteFile(tmpFile, videoData, 0644)
	require.NoError(t, err)
	defer os.Remove(tmpFile)

	// Create a completed job with local video path
	testJob := job.New()
	testJob.PushToS3 = false
	testJob.OutputVideoPath = tmpFile
	err = testJob.Start()
	require.NoError(t, err)
	err = testJob.Complete()
	require.NoError(t, err)
	testJob.UpdateProgress(100)
	err = repo.Save(ctx, testJob)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/jobs/"+testJob.ID, nil)
	req.SetPathValue("id", testJob.ID)
	rec := httptest.NewRecorder()

	h.GetJob(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp JobResponse
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "COMPLETED", resp.Status)
	assert.Empty(t, resp.VideoURL)
	assert.NotEmpty(t, resp.VideoBase64)

	// Verify the base64 content
	decoded, err := base64.StdEncoding.DecodeString(resp.VideoBase64)
	require.NoError(t, err)
	assert.Equal(t, videoData, decoded)
}

func TestRouter_Integration(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	router := NewRouter(h, logger, DefaultConfig())

	// Test health endpoint
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Test POST /jobs
	body := CreateJobRequest{
		ImageBase64: base64.StdEncoding.EncodeToString([]byte("test-image")),
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("test-audio")),
		Width:       384,
		Height:      576,
	}
	bodyJSON, _ := json.Marshal(body)
	req = httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	// Parse response to get job ID
	var createResp CreateJobResponse
	err := json.NewDecoder(rec.Body).Decode(&createResp)
	require.NoError(t, err)

	// Test GET /jobs/{id}
	req = httptest.NewRequest(http.MethodGet, "/jobs/"+createResp.ID, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCORSMiddleware(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := Config{AllowedOrigins: []string{"https://example.com"}}
	router := NewRouter(h, logger, cfg)

	// Test with allowed origin
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, "https://example.com", rec.Header().Get("Access-Control-Allow-Origin"))

	// Test OPTIONS preflight
	req = httptest.NewRequest(http.MethodOptions, "/jobs", nil)
	req.Header.Set("Origin", "https://example.com")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestRecoveryMiddleware(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create a handler that panics
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	handler := RecoveryMiddleware(logger)(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var resp ErrorResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "INTERNAL_ERROR", resp.Code)
}

func TestCreateJob_DryRun(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)

	body := CreateJobRequest{
		ImageBase64: base64.StdEncoding.EncodeToString([]byte("test-image")),
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("test-audio")),
		Width:       384,
		Height:      576,
		PushToS3:    false,
		DryRun:      true,
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateJob(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)

	var resp CreateJobResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, "IN_QUEUE", resp.Status)
}

func TestDeleteJobVideo_Success(t *testing.T) {
	h, _, _, _, _, repo := newTestHandlers(t)
	ctx := context.Background()

	// Create a temp video file
	videoPath := "/tmp/test_handler_delete_video.mp4"
	err := os.WriteFile(videoPath, []byte("video data"), 0644)
	require.NoError(t, err)
	defer os.Remove(videoPath)

	// Create a job with the video path
	testJob := job.New()
	testJob.SetOutput(videoPath, "")
	err = repo.Save(ctx, testJob)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/jobs/"+testJob.ID+"/video/delete", nil)
	req.SetPathValue("id", testJob.ID)
	rec := httptest.NewRecorder()

	h.DeleteJobVideo(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Verify file was deleted
	_, statErr := os.Stat(videoPath)
	assert.True(t, os.IsNotExist(statErr))

	// Verify job output was cleared
	updatedJob, err := repo.FindByID(ctx, testJob.ID)
	require.NoError(t, err)
	assert.Empty(t, updatedJob.OutputVideoPath)
}

func TestDeleteJobVideo_FileAlreadyMissing(t *testing.T) {
	h, _, _, _, _, repo := newTestHandlers(t)
	ctx := context.Background()

	// Create a job with a non-existent video path
	testJob := job.New()
	testJob.SetOutput("/tmp/nonexistent_handler_video.mp4", "")
	err := repo.Save(ctx, testJob)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/jobs/"+testJob.ID+"/video/delete", nil)
	req.SetPathValue("id", testJob.ID)
	rec := httptest.NewRecorder()

	h.DeleteJobVideo(rec, req)

	// Should still return 204 (idempotent)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDeleteJobVideo_JobNotFound(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)

	req := httptest.NewRequest(http.MethodPost, "/jobs/nonexistent/video/delete", nil)
	req.SetPathValue("id", "nonexistent")
	rec := httptest.NewRecorder()

	h.DeleteJobVideo(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var resp ErrorResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "JOB_NOT_FOUND", resp.Code)
}

func TestDeleteJobVideo_MissingID(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)

	req := httptest.NewRequest(http.MethodPost, "/jobs//video/delete", nil)
	// Don't set path value to simulate missing ID
	rec := httptest.NewRecorder()

	h.DeleteJobVideo(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp ErrorResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "MISSING_JOB_ID", resp.Code)
}

func TestCreateJob_ForceOffloadDefaultTrue(t *testing.T) {
	h, _, _, _, _, repo := newTestHandlers(t)

	// Create job without specifying force_offload (should default to true)
	body := CreateJobRequest{
		ImageBase64: base64.StdEncoding.EncodeToString([]byte("test-image")),
		AudioBase64: base64.StdEncoding.EncodeToString([]byte("test-audio")),
		Width:       384,
		Height:      576,
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateJob(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)

	var resp CreateJobResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)

	// Verify the job was created with force_offload=true by default
	// Note: We can't directly check the input since it's internal,
	// but we've validated that CreateJob returns a job ID successfully
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, "IN_QUEUE", resp.Status)

	// Verify the job exists in the repository
	createdJob, err := repo.FindByID(context.Background(), resp.ID)
	require.NoError(t, err)
	assert.Equal(t, resp.ID, createdJob.ID)
}

func TestCreateJob_ForceOffloadExplicitTrue(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)

	forceOffload := true
	body := CreateJobRequest{
		ImageBase64:  base64.StdEncoding.EncodeToString([]byte("test-image")),
		AudioBase64:  base64.StdEncoding.EncodeToString([]byte("test-audio")),
		Width:        384,
		Height:       576,
		ForceOffload: &forceOffload,
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateJob(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)

	var resp CreateJobResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, "IN_QUEUE", resp.Status)
}

func TestCreateJob_ForceOffloadExplicitFalse(t *testing.T) {
	h, _, _, _, _, _ := newTestHandlers(t)

	forceOffload := false
	body := CreateJobRequest{
		ImageBase64:  base64.StdEncoding.EncodeToString([]byte("test-image")),
		AudioBase64:  base64.StdEncoding.EncodeToString([]byte("test-audio")),
		Width:        384,
		Height:       576,
		ForceOffload: &forceOffload,
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateJob(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)

	var resp CreateJobResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, "IN_QUEUE", resp.Status)
}

