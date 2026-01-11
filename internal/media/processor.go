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

	// GetMediaDuration returns the duration in seconds of a media file (audio or video).
	GetMediaDuration(ctx context.Context, path string) (float64, error)

	// GenerateMovingVideo creates a video from a static image with zoom/pan motion.
	// The duration of the generated video matches the specified duration in seconds.
	// The output video will have a smooth zoom effect to prevent static frames.
	GenerateMovingVideo(ctx context.Context, imagePath, outputPath string, duration float64, width, height int) error
}
