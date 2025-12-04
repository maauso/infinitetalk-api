package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// skipIfNoFFmpeg skips the test if ffmpeg is not available.
func skipIfNoFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found in PATH, skipping test")
	}
}

// createTestImage creates a simple test image using ffmpeg.
func createTestImage(t *testing.T, path string, width, height int) {
	t.Helper()

	// Create a simple solid color image using ffmpeg
	cmd := exec.Command("ffmpeg",
		"-y",
		"-f", "lavfi",
		"-i", fmt.Sprintf("color=c=red:s=%dx%d:d=1", width, height),
		"-frames:v", "1",
		path,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create test image: %v\noutput: %s", err, output)
	}
}

// createTestVideo creates a simple test video using ffmpeg.
func createTestVideo(t *testing.T, path string, duration float64, color string) {
	t.Helper()

	// Create a simple video with solid color and silent audio
	cmd := exec.Command("ffmpeg",
		"-y",
		"-f", "lavfi",
		"-i", fmt.Sprintf("color=c=%s:s=64x64:d=%.1f", color, duration),
		"-f", "lavfi",
		"-i", fmt.Sprintf("anullsrc=r=44100:cl=mono:d=%.1f", duration),
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-c:a", "aac",
		"-shortest",
		path,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create test video: %v\noutput: %s", err, output)
	}
}

func TestNewFFmpegProcessor(t *testing.T) {
	t.Run("default path", func(t *testing.T) {
		p := NewFFmpegProcessor("")
		if p.ffmpegPath != "ffmpeg" {
			t.Errorf("expected default path 'ffmpeg', got %q", p.ffmpegPath)
		}
	})

	t.Run("custom path", func(t *testing.T) {
		p := NewFFmpegProcessor("/usr/local/bin/ffmpeg")
		if p.ffmpegPath != "/usr/local/bin/ffmpeg" {
			t.Errorf("expected custom path, got %q", p.ffmpegPath)
		}
	})
}

func TestResizeImageWithPadding(t *testing.T) {
	skipIfNoFFmpeg(t)

	tmpDir := t.TempDir()
	p := NewFFmpegProcessor("")

	t.Run("resize landscape to square with padding", func(t *testing.T) {
		src := filepath.Join(tmpDir, "landscape.png")
		dst := filepath.Join(tmpDir, "resized_square.png")

		// Create a 100x50 landscape image
		createTestImage(t, src, 100, 50)

		ctx := context.Background()
		err := p.ResizeImageWithPadding(ctx, src, dst, 64, 64)
		if err != nil {
			t.Fatalf("ResizeImageWithPadding failed: %v", err)
		}

		// Verify output exists
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			t.Error("output file was not created")
		}

		// Verify dimensions using ffprobe
		verifyImageDimensions(t, dst, 64, 64)
	})

	t.Run("resize portrait to square with padding", func(t *testing.T) {
		src := filepath.Join(tmpDir, "portrait.png")
		dst := filepath.Join(tmpDir, "resized_portrait.png")

		// Create a 50x100 portrait image
		createTestImage(t, src, 50, 100)

		ctx := context.Background()
		err := p.ResizeImageWithPadding(ctx, src, dst, 64, 64)
		if err != nil {
			t.Fatalf("ResizeImageWithPadding failed: %v", err)
		}

		verifyImageDimensions(t, dst, 64, 64)
	})

	t.Run("resize to specific dimensions", func(t *testing.T) {
		src := filepath.Join(tmpDir, "input.png")
		dst := filepath.Join(tmpDir, "output_384x576.png")

		createTestImage(t, src, 200, 300)

		ctx := context.Background()
		err := p.ResizeImageWithPadding(ctx, src, dst, 384, 576)
		if err != nil {
			t.Fatalf("ResizeImageWithPadding failed: %v", err)
		}

		verifyImageDimensions(t, dst, 384, 576)
	})

	t.Run("invalid dimensions", func(t *testing.T) {
		src := filepath.Join(tmpDir, "valid.png")
		dst := filepath.Join(tmpDir, "invalid_dims.png")

		createTestImage(t, src, 100, 100)

		ctx := context.Background()

		tests := []struct {
			w, h int
		}{
			{0, 100},
			{100, 0},
			{-1, 100},
			{100, -1},
		}

		for _, tc := range tests {
			err := p.ResizeImageWithPadding(ctx, src, dst, tc.w, tc.h)
			if err == nil {
				t.Errorf("expected error for dimensions w=%d h=%d, got nil", tc.w, tc.h)
			}
		}
	})

	t.Run("non-existent source", func(t *testing.T) {
		ctx := context.Background()
		err := p.ResizeImageWithPadding(ctx, "/nonexistent/image.png", filepath.Join(tmpDir, "out.png"), 64, 64)
		if err == nil {
			t.Error("expected error for non-existent source, got nil")
		}
		// Verify it's an FFmpegError
		if _, ok := err.(*FFmpegError); !ok {
			t.Errorf("expected FFmpegError, got %T", err)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		src := filepath.Join(tmpDir, "cancel_src.png")
		dst := filepath.Join(tmpDir, "cancel_dst.png")

		createTestImage(t, src, 100, 100)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := p.ResizeImageWithPadding(ctx, src, dst, 64, 64)
		if err == nil {
			t.Error("expected error for cancelled context, got nil")
		}
	})

	t.Run("context timeout", func(t *testing.T) {
		src := filepath.Join(tmpDir, "timeout_src.png")
		dst := filepath.Join(tmpDir, "timeout_dst.png")

		createTestImage(t, src, 100, 100)

		// Create context that's already expired by using a deadline in the past
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
		defer cancel()

		err := p.ResizeImageWithPadding(ctx, src, dst, 64, 64)
		if err == nil {
			t.Error("expected error for timed out context, got nil")
		}
	})
}

func TestJoinVideos(t *testing.T) {
	skipIfNoFFmpeg(t)

	tmpDir := t.TempDir()
	p := NewFFmpegProcessor("")

	t.Run("join multiple videos", func(t *testing.T) {
		video1 := filepath.Join(tmpDir, "video1.mp4")
		video2 := filepath.Join(tmpDir, "video2.mp4")
		output := filepath.Join(tmpDir, "joined.mp4")

		createTestVideo(t, video1, 0.5, "red")
		createTestVideo(t, video2, 0.5, "blue")

		ctx := context.Background()
		err := p.JoinVideos(ctx, []string{video1, video2}, output)
		if err != nil {
			t.Fatalf("JoinVideos failed: %v", err)
		}

		// Verify output exists and has content
		info, err := os.Stat(output)
		if os.IsNotExist(err) {
			t.Error("output file was not created")
		}
		if info.Size() == 0 {
			t.Error("output file is empty")
		}

		// Verify duration is approximately the sum of inputs
		duration := getVideoDuration(t, output)
		if duration < 0.9 || duration > 1.1 {
			t.Errorf("expected joined video duration ~1.0s, got %.2f", duration)
		}
	})

	t.Run("single video", func(t *testing.T) {
		video := filepath.Join(tmpDir, "single.mp4")
		output := filepath.Join(tmpDir, "single_out.mp4")

		createTestVideo(t, video, 0.5, "green")

		ctx := context.Background()
		err := p.JoinVideos(ctx, []string{video}, output)
		if err != nil {
			t.Fatalf("JoinVideos with single video failed: %v", err)
		}

		// Verify output exists
		if _, err := os.Stat(output); os.IsNotExist(err) {
			t.Error("output file was not created")
		}
	})

	t.Run("empty video list", func(t *testing.T) {
		ctx := context.Background()
		err := p.JoinVideos(ctx, []string{}, filepath.Join(tmpDir, "empty.mp4"))
		if err == nil {
			t.Error("expected error for empty video list, got nil")
		}
	})

	t.Run("non-existent video", func(t *testing.T) {
		ctx := context.Background()
		err := p.JoinVideos(ctx, []string{"/nonexistent/video.mp4"}, filepath.Join(tmpDir, "out.mp4"))
		if err == nil {
			t.Error("expected error for non-existent video, got nil")
		}
	})

	t.Run("join three videos", func(t *testing.T) {
		video1 := filepath.Join(tmpDir, "v1.mp4")
		video2 := filepath.Join(tmpDir, "v2.mp4")
		video3 := filepath.Join(tmpDir, "v3.mp4")
		output := filepath.Join(tmpDir, "joined3.mp4")

		createTestVideo(t, video1, 0.3, "red")
		createTestVideo(t, video2, 0.3, "green")
		createTestVideo(t, video3, 0.3, "blue")

		ctx := context.Background()
		err := p.JoinVideos(ctx, []string{video1, video2, video3}, output)
		if err != nil {
			t.Fatalf("JoinVideos with 3 videos failed: %v", err)
		}

		// Verify output exists
		if _, err := os.Stat(output); os.IsNotExist(err) {
			t.Error("output file was not created")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		video1 := filepath.Join(tmpDir, "cancel1.mp4")
		video2 := filepath.Join(tmpDir, "cancel2.mp4")
		output := filepath.Join(tmpDir, "cancelled.mp4")

		createTestVideo(t, video1, 0.5, "red")
		createTestVideo(t, video2, 0.5, "blue")

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := p.JoinVideos(ctx, []string{video1, video2}, output)
		if err == nil {
			t.Error("expected error for cancelled context, got nil")
		}
	})
}

func TestExtractLastFrame(t *testing.T) {
	skipIfNoFFmpeg(t)

	tmpDir := t.TempDir()
	p := NewFFmpegProcessor("")
	ctx := context.Background()

	t.Run("extracts last frame from video", func(t *testing.T) {
		// Create a test video (2 seconds, red color)
		videoPath := filepath.Join(tmpDir, "test_video.mp4")
		createTestVideo(t, videoPath, 2.0, "red")

		frameData, err := p.ExtractLastFrame(ctx, videoPath)
		if err != nil {
			t.Fatalf("ExtractLastFrame failed: %v", err)
		}

		// Verify we got PNG data (PNG magic bytes: 0x89 0x50 0x4E 0x47)
		if len(frameData) < 8 {
			t.Fatalf("frame data too small: %d bytes", len(frameData))
		}
		if frameData[0] != 0x89 || frameData[1] != 0x50 || frameData[2] != 0x4E || frameData[3] != 0x47 {
			t.Error("extracted frame is not a valid PNG")
		}

		// Verify temp file was cleaned up
		tempFile := videoPath + "_last_frame.png"
		if _, err := os.Stat(tempFile); !os.IsNotExist(err) {
			t.Error("temporary frame file was not cleaned up")
		}
	})

	t.Run("fails with non-existent video", func(t *testing.T) {
		_, err := p.ExtractLastFrame(ctx, "/non/existent/video.mp4")
		if err == nil {
			t.Error("expected error for non-existent video")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		videoPath := filepath.Join(tmpDir, "test_video_cancel.mp4")
		createTestVideo(t, videoPath, 1.0, "blue")

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := p.ExtractLastFrame(ctx, videoPath)
		if err == nil {
			t.Error("expected error when context is cancelled")
		}
	})
}

func TestFFmpegError(t *testing.T) {
	err := &FFmpegError{
		Args:   []string{"-i", "input.mp4", "-c", "copy", "output.mp4"},
		Stderr: "Error opening input file",
		Err:    fmt.Errorf("exit status 1"),
	}

	// Test Error() method
	errStr := err.Error()
	if errStr == "" {
		t.Error("Error() returned empty string")
	}

	// Verify error contains key information
	if !strings.Contains(errStr, "exit status 1") {
		t.Error("Error() should contain underlying error")
	}
	if !strings.Contains(errStr, "Error opening input file") {
		t.Error("Error() should contain stderr")
	}

	// Test Unwrap() method
	unwrapped := err.Unwrap()
	if unwrapped == nil {
		t.Error("Unwrap() returned nil")
	}
	if unwrapped.Error() != "exit status 1" {
		t.Errorf("Unwrap() returned wrong error: %v", unwrapped)
	}
}

// Helper functions

func verifyImageDimensions(t *testing.T, path string, expectedW, expectedH int) {
	t.Helper()

	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=s=x:p=0",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffprobe failed: %v", err)
	}

	var w, h int
	n, err := fmt.Sscanf(string(output), "%dx%d", &w, &h)
	if err != nil || n != 2 {
		t.Fatalf("failed to parse dimensions from ffprobe output: %s", output)
	}

	if w != expectedW || h != expectedH {
		t.Errorf("expected dimensions %dx%d, got %dx%d", expectedW, expectedH, w, h)
	}
}

func getVideoDuration(t *testing.T, path string) float64 {
	t.Helper()

	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffprobe failed: %v", err)
	}

	var duration float64
	if _, err := fmt.Sscanf(string(output), "%f", &duration); err != nil {
		t.Fatalf("failed to parse duration: %s", output)
	}

	return duration
}
