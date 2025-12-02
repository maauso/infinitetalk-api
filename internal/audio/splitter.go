// Package audio provides interfaces and implementations for audio processing.
package audio

import "context"

// SplitOpts configures the behavior of audio splitting.
type SplitOpts struct {
	// ChunkTargetSec is the target duration for each audio chunk in seconds.
	// Audio will be split at silence boundaries close to this duration.
	// Default: 45 seconds.
	ChunkTargetSec int

	// MinSilenceMs is the minimum silence duration in milliseconds
	// to consider for a split point.
	// Default: 500 milliseconds.
	MinSilenceMs int

	// SilenceThreshDB is the volume threshold in dBFS below which
	// audio is considered silence.
	// Default: -40 dBFS.
	SilenceThreshDB float64
}

// DefaultSplitOpts returns the default options for audio splitting.
func DefaultSplitOpts() SplitOpts {
	return SplitOpts{
		ChunkTargetSec:  45,
		MinSilenceMs:    500,
		SilenceThreshDB: -40,
	}
}

// Splitter defines the interface for splitting audio files at silence boundaries.
type Splitter interface {
	// Split divides an audio file into chunks at silence boundaries.
	// If the audio is shorter than or equal to ChunkTargetSec, it returns
	// a single path pointing to a copy of the input file.
	//
	// Returns paths to the generated chunk files. The caller is responsible
	// for cleaning up these temporary files.
	//
	// The output files are created in the same directory as inputWav
	// unless a temporary directory is configured.
	Split(ctx context.Context, inputWav, outputDir string, opts SplitOpts) ([]string, error)
}
