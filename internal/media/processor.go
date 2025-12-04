// Package media provides image and video processing capabilities.
package media

import "context"

// Processor defines the interface for image and video processing operations.
// Implementations should use ffmpeg or similar tools for media manipulation.
type Processor interface {
	// ResizeImageWithPadding resizes an image to the specified dimensions while
	// maintaining aspect ratio. Black padding is added to fill any remaining space.
	// The source image is read from src and the result is written to dst.
	ResizeImageWithPadding(ctx context.Context, src, dst string, w, h int) error

	// JoinVideos concatenates multiple video files into a single output file.
	// It first attempts a fast copy (no re-encoding) and falls back to re-encoding
	// with libx264/aac if the copy fails due to incompatible codecs.
	JoinVideos(ctx context.Context, videoPaths []string, output string) error

	// ExtractLastFrame extracts the last frame from a video file as a PNG image.
	// Returns the image data as bytes. The caller is responsible for encoding
	// to base64 if needed.
	ExtractLastFrame(ctx context.Context, videoPath string) ([]byte, error)
}
