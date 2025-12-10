package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Static errors for media operations.
var (
	// ErrInvalidDimensions is returned when the provided dimensions are not positive.
	ErrInvalidDimensions = errors.New("invalid dimensions: width and height must be positive")
	// ErrNoVideoPaths is returned when no video paths are provided for joining.
	ErrNoVideoPaths = errors.New("no video paths provided")
)

// FFmpegProcessor implements Processor using the ffmpeg CLI.
type FFmpegProcessor struct {
	// ffmpegPath is the path to the ffmpeg binary. Defaults to "ffmpeg".
	ffmpegPath string
}

// NewFFmpegProcessor creates a new FFmpegProcessor.
// If ffmpegPath is empty, it defaults to "ffmpeg" (found via PATH).
func NewFFmpegProcessor(ffmpegPath string) *FFmpegProcessor {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &FFmpegProcessor{ffmpegPath: ffmpegPath}
}

// ResizeImageWithPadding resizes an image to the specified dimensions while
// maintaining aspect ratio. Black padding is added to fill any remaining space.
func (p *FFmpegProcessor) ResizeImageWithPadding(ctx context.Context, src, dst string, w, h int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("%w: width=%d, height=%d", ErrInvalidDimensions, w, h)
	}

	// FFmpeg filter to scale with aspect ratio preservation and add black padding
	// scale: scales to fit within w x h while maintaining aspect ratio
	// pad: adds black padding to center the image and reach exact dimensions
	filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:black", w, h, w, h)

	args := []string{
		"-y",      // Overwrite output file without asking
		"-i", src, // Input file
		"-vf", filter, // Video filter
		"-frames:v", "1", // Output single frame (image)
		dst, // Output file
	}

	return p.runFFmpeg(ctx, args)
}

// JoinVideos concatenates multiple video files into a single output file.
// It first attempts a fast copy (no re-encoding) and falls back to re-encoding
// with libx264/aac if the copy fails.
func (p *FFmpegProcessor) JoinVideos(ctx context.Context, videoPaths []string, output string) error {
	if len(videoPaths) == 0 {
		return ErrNoVideoPaths
	}

	if len(videoPaths) == 1 {
		// Single video: just copy the file
		return p.copyFile(videoPaths[0], output)
	}

	// Create a temporary file list for the concat demuxer
	listFile, err := p.createConcatList(videoPaths)
	if err != nil {
		return fmt.Errorf("create concat list: %w", err)
	}
	defer func() { _ = os.Remove(listFile) }()

	// Try fast copy first (no re-encoding)
	err = p.joinWithCopy(ctx, listFile, output)
	if err == nil {
		return nil
	}

	// Fast copy failed, fall back to re-encoding
	return p.joinWithReencode(ctx, listFile, output)
}

// joinWithCopy attempts to concatenate videos using stream copy (no re-encoding).
func (p *FFmpegProcessor) joinWithCopy(ctx context.Context, listFile, output string) error {
	args := []string{
		"-y",           // Overwrite output file
		"-f", "concat", // Use concat demuxer
		"-safe", "0", // Allow absolute paths
		"-i", listFile, // Input file list
		"-c", "copy", // Copy streams without re-encoding
		output, // Output file
	}
	return p.runFFmpeg(ctx, args)
}

// joinWithReencode concatenates videos by re-encoding with libx264/aac.
func (p *FFmpegProcessor) joinWithReencode(ctx context.Context, listFile, output string) error {
	args := []string{
		"-y",           // Overwrite output file
		"-f", "concat", // Use concat demuxer
		"-safe", "0", // Allow absolute paths
		"-i", listFile, // Input file list
		"-c:v", "libx264", // Video codec
		"-preset", "fast", // Encoding speed preset
		"-crf", "23", // Quality (lower = better, 23 is default)
		"-c:a", "aac", // Audio codec
		"-b:a", "128k", // Audio bitrate
		output, // Output file
	}
	return p.runFFmpeg(ctx, args)
}

// createConcatList creates a temporary file containing the list of video files
// in the format required by ffmpeg's concat demuxer.
func (p *FFmpegProcessor) createConcatList(videoPaths []string) (string, error) {
	f, err := os.CreateTemp("", "ffmpeg-concat-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = f.Close() }()

	for _, path := range videoPaths {
		// Convert to absolute path for safety
		absPath, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("get absolute path for %s: %w", path, err)
		}
		// Escape single quotes in path
		escapedPath := strings.ReplaceAll(absPath, "'", "'\\''")
		if _, err := fmt.Fprintf(f, "file '%s'\n", escapedPath); err != nil {
			return "", fmt.Errorf("write to concat list: %w", err)
		}
	}

	return f.Name(), nil
}

// copyFile copies a file from src to dst.
func (p *FFmpegProcessor) copyFile(src, dst string) error {
	input, err := os.ReadFile(src) // #nosec G304 - src is provided by trusted internal code
	if err != nil {
		return fmt.Errorf("read source file: %w", err)
	}
	if err := os.WriteFile(dst, input, 0600); err != nil {
		return fmt.Errorf("write destination file: %w", err)
	}
	return nil
}

// runFFmpeg executes ffmpeg with the given arguments and returns an error
// containing stderr output if the command fails.
func (p *FFmpegProcessor) runFFmpeg(ctx context.Context, args []string) error {
	// #nosec G204 - ffmpegPath is set by the application, not user input
	cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Check if context was cancelled
		if ctx.Err() != nil {
			return fmt.Errorf("ffmpeg cancelled: %w", ctx.Err())
		}
		return &FFmpegError{
			Args:   args,
			Stderr: stderr.String(),
			Err:    err,
		}
	}

	return nil
}

// FFmpegError represents an error from running ffmpeg, including the stderr output.
type FFmpegError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *FFmpegError) Error() string {
	return fmt.Sprintf("ffmpeg error: %v\nargs: %v\nstderr: %s", e.Err, e.Args, e.Stderr)
}

func (e *FFmpegError) Unwrap() error {
	return e.Err
}
