package audio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// checkFFmpeg skips test if ffmpeg is not available.
func checkFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found in PATH, skipping test")
	}
}

// createTestWAV creates a test WAV file with specified duration and optional silences.
// silenceAt is a list of [start, duration] pairs indicating where to insert silence.
func createTestWAV(t *testing.T, outputPath string, durationSec float64, silenceAt [][2]float64) {
	t.Helper()

	// Build a filter chain that creates audio with silences
	// Base: sine wave at 440Hz
	var filterParts []string

	if len(silenceAt) == 0 {
		// Simple sine wave
		filter := "sine=frequency=440:duration=" + formatDuration(durationSec)
		cmd := exec.Command("ffmpeg", "-y",
			"-f", "lavfi", "-i", filter,
			"-ar", "16000", "-ac", "1",
			outputPath,
		)
		var stderr []byte
		stderr, _ = cmd.CombinedOutput()
		if _, err := os.Stat(outputPath); os.IsNotExist(err) {
			t.Fatalf("failed to create test WAV: %s", string(stderr))
		}
		return
	}

	// Create audio with silences using concat filter
	// Generate parts: audio, silence, audio, silence, ...
	currentTime := 0.0
	partIndex := 0

	for _, silence := range silenceAt {
		silenceStart := silence[0]
		silenceDuration := silence[1]

		// Audio before silence
		if silenceStart > currentTime {
			audioDuration := silenceStart - currentTime
			filterParts = append(filterParts,
				"-f", "lavfi", "-i", "sine=frequency=440:duration="+formatDuration(audioDuration))
			partIndex++
		}

		// Silence
		filterParts = append(filterParts,
			"-f", "lavfi", "-i", "anullsrc=channel_layout=mono:sample_rate=16000:duration="+formatDuration(silenceDuration))
		partIndex++

		currentTime = silenceStart + silenceDuration
	}

	// Remaining audio after last silence
	if currentTime < durationSec {
		remainingDuration := durationSec - currentTime
		filterParts = append(filterParts,
			"-f", "lavfi", "-i", "sine=frequency=440:duration="+formatDuration(remainingDuration))
		partIndex++
	}

	// Build concat filter
	var concatInputs string
	for i := 0; i < partIndex; i++ {
		concatInputs += "[" + strconv.Itoa(i) + ":a]"
	}
	concatFilter := concatInputs + "concat=n=" + strconv.Itoa(partIndex) + ":v=0:a=1[out]"

	args := append(filterParts,
		"-filter_complex", concatFilter,
		"-map", "[out]",
		"-ar", "16000", "-ac", "1",
		"-y", outputPath,
	)

	cmd := exec.Command("ffmpeg", args...)
	stderr, _ := cmd.CombinedOutput()
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatalf("failed to create test WAV with silences: %s", string(stderr))
	}
}

func formatDuration(sec float64) string {
	// Format with 3 decimal places for ffmpeg
	return fmt.Sprintf("%.3f", sec)
}

func formatChunkName(i int) string {
	// Zero-padded to 3 digits
	return fmt.Sprintf("chunk_%03d.wav", i)
}

func TestFFmpegSplitter_ShortAudio(t *testing.T) {
	checkFFmpeg(t)

	// Create a short audio file (10 seconds)
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "short.wav")
	outputDir := filepath.Join(tmpDir, "output")

	createTestWAV(t, inputPath, 10, nil)

	splitter := NewFFmpegSplitter("")
	opts := SplitOpts{
		ChunkTargetSec:  45,
		MinSilenceMs:    500,
		SilenceThreshDB: -40,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	chunks, err := splitter.Split(ctx, inputPath, outputDir, opts)
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}

	// Should return single chunk
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}

	// Verify chunk exists
	if _, err := os.Stat(chunks[0]); os.IsNotExist(err) {
		t.Errorf("chunk file does not exist: %s", chunks[0])
	}
}

func TestFFmpegSplitter_LongAudioWithSilences(t *testing.T) {
	checkFFmpeg(t)

	// Create a 2-minute audio file with silences at ~45s and ~90s
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "long.wav")
	outputDir := filepath.Join(tmpDir, "output")

	// Create 120 second audio with silences at 45s and 90s
	silences := [][2]float64{
		{44.0, 1.0}, // 1 second silence at 44s
		{89.0, 1.0}, // 1 second silence at 89s
	}
	createTestWAV(t, inputPath, 120, silences)

	splitter := NewFFmpegSplitter("")
	opts := SplitOpts{
		ChunkTargetSec:  45,
		MinSilenceMs:    500,
		SilenceThreshDB: -40,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	chunks, err := splitter.Split(ctx, inputPath, outputDir, opts)
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}

	// Should generate multiple chunks
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks for 2-minute audio, got %d", len(chunks))
	}

	// Verify all chunks exist
	for i, chunk := range chunks {
		if _, err := os.Stat(chunk); os.IsNotExist(err) {
			t.Errorf("chunk %d does not exist: %s", i, chunk)
		}
	}

	// Verify chunks are named correctly
	for i, chunk := range chunks {
		expectedName := filepath.Join(outputDir, formatChunkName(i))
		if chunk != expectedName {
			t.Errorf("chunk %d has unexpected name: got %s, want %s", i, chunk, expectedName)
		}
	}
}

func TestFFmpegSplitter_NoSilences(t *testing.T) {
	checkFFmpeg(t)

	// Create continuous audio without silences
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "continuous.wav")
	outputDir := filepath.Join(tmpDir, "output")

	// 100 second audio with no silences
	createTestWAV(t, inputPath, 100, nil)

	splitter := NewFFmpegSplitter("")
	opts := SplitOpts{
		ChunkTargetSec:  45,
		MinSilenceMs:    500,
		SilenceThreshDB: -40,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	chunks, err := splitter.Split(ctx, inputPath, outputDir, opts)
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}

	// Should still split at fixed intervals
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks for 100s audio with 45s target, got %d", len(chunks))
	}

	// All chunks should exist
	for i, chunk := range chunks {
		if _, err := os.Stat(chunk); os.IsNotExist(err) {
			t.Errorf("chunk %d does not exist: %s", i, chunk)
		}
	}
}

func TestFFmpegSplitter_ContextCancellation(t *testing.T) {
	checkFFmpeg(t)

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "test.wav")
	outputDir := filepath.Join(tmpDir, "output")

	createTestWAV(t, inputPath, 10, nil)

	splitter := NewFFmpegSplitter("")
	opts := DefaultSplitOpts()

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := splitter.Split(ctx, inputPath, outputDir, opts)
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

func TestFFmpegSplitter_NonExistentFile(t *testing.T) {
	splitter := NewFFmpegSplitter("")
	opts := DefaultSplitOpts()

	ctx := context.Background()
	_, err := splitter.Split(ctx, "/nonexistent/file.wav", "/tmp/output", opts)
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestDefaultSplitOpts(t *testing.T) {
	opts := DefaultSplitOpts()

	if opts.ChunkTargetSec != 45 {
		t.Errorf("ChunkTargetSec: got %d, want 45", opts.ChunkTargetSec)
	}
	if opts.MinSilenceMs != 500 {
		t.Errorf("MinSilenceMs: got %d, want 500", opts.MinSilenceMs)
	}
	if opts.SilenceThreshDB != -40 {
		t.Errorf("SilenceThreshDB: got %f, want -40", opts.SilenceThreshDB)
	}
}

func TestParseSilenceOutput(t *testing.T) {
	// Sample ffmpeg silencedetect output
	output := `
[silencedetect @ 0x55f1a2b3c4d0] silence_start: 10.5
[silencedetect @ 0x55f1a2b3c4d0] silence_end: 11.2 | silence_duration: 0.7
[silencedetect @ 0x55f1a2b3c4d0] silence_start: 45.0
[silencedetect @ 0x55f1a2b3c4d0] silence_end: 46.5 | silence_duration: 1.5
`

	intervals, err := parseSilenceOutput(output)
	if err != nil {
		t.Fatalf("parseSilenceOutput failed: %v", err)
	}

	if len(intervals) != 2 {
		t.Fatalf("expected 2 intervals, got %d", len(intervals))
	}

	// Check first interval
	if intervals[0].Start != 10.5 || intervals[0].End != 11.2 {
		t.Errorf("interval 0: got start=%f end=%f, want start=10.5 end=11.2",
			intervals[0].Start, intervals[0].End)
	}

	// Check second interval
	if intervals[1].Start != 45.0 || intervals[1].End != 46.5 {
		t.Errorf("interval 1: got start=%f end=%f, want start=45.0 end=46.5",
			intervals[1].Start, intervals[1].End)
	}
}

func TestListChunks(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some chunk files
	files := []string{"chunk_000.wav", "chunk_001.wav", "chunk_002.wav", "other.txt"}
	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	chunks, err := ListChunks(tmpDir)
	if err != nil {
		t.Fatalf("ListChunks failed: %v", err)
	}

	if len(chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(chunks))
	}

	// Verify sorted order
	for i, expected := range []string{"chunk_000.wav", "chunk_001.wav", "chunk_002.wav"} {
		if filepath.Base(chunks[i]) != expected {
			t.Errorf("chunk %d: got %s, want %s", i, filepath.Base(chunks[i]), expected)
		}
	}
}

func TestNewFFmpegSplitter_DefaultPath(t *testing.T) {
	splitter := NewFFmpegSplitter("")
	if splitter.ffmpegPath != "ffmpeg" {
		t.Errorf("expected default path 'ffmpeg', got '%s'", splitter.ffmpegPath)
	}
}

func TestNewFFmpegSplitter_CustomPath(t *testing.T) {
	splitter := NewFFmpegSplitter("/custom/path/ffmpeg")
	if splitter.ffmpegPath != "/custom/path/ffmpeg" {
		t.Errorf("expected custom path, got '%s'", splitter.ffmpegPath)
	}
}
