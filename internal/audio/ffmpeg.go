package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Static errors for audio operations.
var (
	// ErrInputNotFound is returned when the input file does not exist.
	ErrInputNotFound = errors.New("input file does not exist")
	// ErrParseDuration is returned when the duration cannot be parsed from ffmpeg output.
	ErrParseDuration = errors.New("could not parse duration from ffmpeg output")
	// ErrInvalidWAVFormat is returned when a chunk does not conform to WAV PCM format.
	ErrInvalidWAVFormat = errors.New("invalid WAV format: expected pcm_s16le codec")
	// ErrInvalidDuration is returned when a chunk has an invalid duration.
	ErrInvalidDuration = errors.New("invalid chunk duration")
)

// codecPCM16LE is the expected codec name for valid WAV chunks.
const codecPCM16LE = "pcm_s16le"

// Pre-compiled regular expressions for ffprobe output parsing.
var (
	reFormatName = regexp.MustCompile(`"format_name"\s*:\s*"([^"]+)"`)
	reCodecName  = regexp.MustCompile(`"codec_name"\s*:\s*"([^"]+)"`)
	reSampleRate = regexp.MustCompile(`"sample_rate"\s*:\s*"(\d+)"`)
	reChannels   = regexp.MustCompile(`"channels"\s*:\s*(\d+)`)
	reDuration   = regexp.MustCompile(`"duration"\s*:\s*"?([\d.]+)"?`)
)

// SilenceInterval represents a detected silence interval in the audio.
type SilenceInterval struct {
	Start float64
	End   float64
}

// WAVInfo contains validation information about a WAV file.
type WAVInfo struct {
	FormatName string
	CodecName  string
	SampleRate int
	Channels   int
	Duration   float64
}

// FFmpegSplitter implements Splitter using ffmpeg CLI.
type FFmpegSplitter struct {
	ffmpegPath  string
	ffprobePath string
}

// NewFFmpegSplitter creates a new FFmpegSplitter.
// If ffmpegPath is empty, it defaults to "ffmpeg" (found in PATH).
// If ffprobePath is empty, it defaults to "ffprobe" (found in PATH).
func NewFFmpegSplitter(ffmpegPath string) *FFmpegSplitter {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &FFmpegSplitter{
		ffmpegPath:  ffmpegPath,
		ffprobePath: "ffprobe",
	}
}

// NewFFmpegSplitterWithProbe creates a new FFmpegSplitter with custom paths.
func NewFFmpegSplitterWithProbe(ffmpegPath, ffprobePath string) *FFmpegSplitter {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if ffprobePath == "" {
		ffprobePath = "ffprobe"
	}
	return &FFmpegSplitter{
		ffmpegPath:  ffmpegPath,
		ffprobePath: ffprobePath,
	}
}

// Split implements Splitter.Split using ffmpeg silencedetect and segment extraction.
func (s *FFmpegSplitter) Split(ctx context.Context, inputWav, outputDir string, opts SplitOpts) ([]string, error) {
	// Validate input file exists
	if _, err := os.Stat(inputWav); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrInputNotFound, inputWav)
	}

	// Get audio duration
	duration, err := s.getAudioDuration(ctx, inputWav)
	if err != nil {
		return nil, fmt.Errorf("get audio duration: %w", err)
	}

	// If audio is shorter than or equal to target, return single file
	if duration <= float64(opts.ChunkTargetSec) {
		outputPath := filepath.Join(outputDir, "chunk_000.wav")
		if err := s.copyAudio(ctx, inputWav, outputPath); err != nil {
			return nil, fmt.Errorf("copy audio: %w", err)
		}
		return []string{outputPath}, nil
	}

	// Detect silence intervals
	silences, err := s.detectSilences(ctx, inputWav, opts)
	if err != nil {
		return nil, fmt.Errorf("detect silences: %w", err)
	}

	// Calculate split points based on target chunk duration
	splitPoints := s.calculateSplitPoints(silences, duration, opts.ChunkTargetSec)

	// Extract chunks
	chunks, err := s.extractChunks(ctx, inputWav, outputDir, splitPoints, duration)
	if err != nil {
		return nil, fmt.Errorf("extract chunks: %w", err)
	}

	return chunks, nil
}

// getAudioDuration returns the duration of an audio file in seconds.
func (s *FFmpegSplitter) getAudioDuration(ctx context.Context, inputPath string) (float64, error) {
	// #nosec G204 - ffmpegPath is set by the application, not user input
	cmd := exec.CommandContext(ctx, s.ffmpegPath,
		"-i", inputPath,
		"-hide_banner",
		"-f", "null", "-",
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// ffmpeg writes duration info to stderr and exits with error code when output is null.
	// We capture stderr to extract duration regardless of exit code, but we still check
	// if we can parse the duration (missing duration indicates an actual error).
	_ = cmd.Run()

	// Parse duration from stderr
	// Looking for: "Duration: HH:MM:SS.ms"
	output := stderr.String()
	re := regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) < 5 {
		return 0, fmt.Errorf("%w: %s", ErrParseDuration, output)
	}

	hours, _ := strconv.ParseFloat(matches[1], 64)
	minutes, _ := strconv.ParseFloat(matches[2], 64)
	seconds, _ := strconv.ParseFloat(matches[3], 64)
	ms, _ := strconv.ParseFloat(matches[4], 64)

	// Convert milliseconds - handle different precision
	msDivisor := 1.0
	for i := 0; i < len(matches[4]); i++ {
		msDivisor *= 10
	}

	return hours*3600 + minutes*60 + seconds + ms/msDivisor, nil
}

// detectSilences uses ffmpeg silencedetect to find silence intervals.
func (s *FFmpegSplitter) detectSilences(ctx context.Context, inputPath string, opts SplitOpts) ([]SilenceInterval, error) {
	// Build silencedetect filter
	filter := fmt.Sprintf("silencedetect=noise=%ddB:d=%f",
		int(opts.SilenceThreshDB),
		float64(opts.MinSilenceMs)/1000.0,
	)

	// #nosec G204 - ffmpegPath is set by the application, not user input
	cmd := exec.CommandContext(ctx, s.ffmpegPath,
		"-i", inputPath,
		"-af", filter,
		"-f", "null",
		"-hide_banner",
		"-",
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// ffmpeg writes silencedetect output to stderr and exits with error when output is null.
	// We capture stderr to extract silence intervals regardless of exit code.
	_ = cmd.Run()

	return parseSilenceOutput(stderr.String())
}

// parseSilenceOutput parses ffmpeg silencedetect output.
func parseSilenceOutput(output string) ([]SilenceInterval, error) {
	var intervals []SilenceInterval

	// Regex patterns for silence start and end
	startRe := regexp.MustCompile(`silence_start:\s*([\d.]+)`)
	endRe := regexp.MustCompile(`silence_end:\s*([\d.]+)`)

	lines := strings.Split(output, "\n")
	var currentStart float64
	hasStart := false

	for _, line := range lines {
		if startMatch := startRe.FindStringSubmatch(line); len(startMatch) > 1 {
			val, err := strconv.ParseFloat(startMatch[1], 64)
			if err != nil {
				continue
			}
			currentStart = val
			hasStart = true
		}

		if endMatch := endRe.FindStringSubmatch(line); len(endMatch) > 1 && hasStart {
			val, err := strconv.ParseFloat(endMatch[1], 64)
			if err != nil {
				continue
			}
			intervals = append(intervals, SilenceInterval{
				Start: currentStart,
				End:   val,
			})
			hasStart = false
		}
	}

	return intervals, nil
}

// calculateSplitPoints determines optimal split points based on silence intervals.
func (s *FFmpegSplitter) calculateSplitPoints(silences []SilenceInterval, totalDuration float64, targetSec int) []float64 {
	if len(silences) == 0 {
		// No silences detected, split at fixed intervals
		return s.fixedSplitPoints(totalDuration, targetSec)
	}

	target := float64(targetSec)
	var splitPoints []float64
	lastSplit := 0.0

	for lastSplit < totalDuration-target/2 {
		// Find the best silence boundary near the target
		idealPoint := lastSplit + target
		bestSilence := findBestSilence(silences, idealPoint, target/3) // Allow 1/3 deviation

		if bestSilence != nil {
			// Split at the middle of the silence
			splitPoint := (bestSilence.Start + bestSilence.End) / 2
			if splitPoint > lastSplit+1 { // Ensure some minimum chunk size
				splitPoints = append(splitPoints, splitPoint)
				lastSplit = splitPoint
			} else {
				// Fall back to ideal point
				lastSplit = idealPoint
				if idealPoint < totalDuration {
					splitPoints = append(splitPoints, idealPoint)
				}
			}
		} else {
			// No suitable silence found, split at ideal point if not too close to end
			if idealPoint < totalDuration-1 {
				splitPoints = append(splitPoints, idealPoint)
			}
			lastSplit = idealPoint
		}
	}

	return splitPoints
}

// fixedSplitPoints generates evenly spaced split points when no silences are found.
func (s *FFmpegSplitter) fixedSplitPoints(totalDuration float64, targetSec int) []float64 {
	var points []float64
	target := float64(targetSec)

	for t := target; t < totalDuration-1; t += target {
		points = append(points, t)
	}

	return points
}

// findBestSilence finds the silence interval closest to the ideal point within tolerance.
func findBestSilence(silences []SilenceInterval, idealPoint, tolerance float64) *SilenceInterval {
	var best *SilenceInterval
	bestDistance := tolerance

	for i := range silences {
		// Use the middle of the silence as reference
		silenceMiddle := (silences[i].Start + silences[i].End) / 2

		// Only consider silences after some minimum point
		if silenceMiddle < idealPoint-tolerance {
			continue
		}
		if silenceMiddle > idealPoint+tolerance {
			break // Silences are sorted by time
		}

		distance := abs(silenceMiddle - idealPoint)
		if distance < bestDistance {
			bestDistance = distance
			best = &silences[i]
		}
	}

	return best
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// extractChunks creates audio chunk files based on split points.
func (s *FFmpegSplitter) extractChunks(ctx context.Context, inputPath, outputDir string, splitPoints []float64, totalDuration float64) ([]string, error) {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	// Build segment boundaries
	segments := make([][2]float64, 0, len(splitPoints)+1)
	start := 0.0
	for _, point := range splitPoints {
		segments = append(segments, [2]float64{start, point})
		start = point
	}
	// Add final segment
	segments = append(segments, [2]float64{start, totalDuration})

	chunks := make([]string, 0, len(segments))
	for i, seg := range segments {
		outputPath := filepath.Join(outputDir, fmt.Sprintf("chunk_%03d.wav", i))

		if err := s.extractSegment(ctx, inputPath, outputPath, seg[0], seg[1]-seg[0]); err != nil {
			// Cleanup already created chunks on error (best-effort, ignore errors)
			for _, chunk := range chunks {
				_ = os.Remove(chunk)
			}
			return nil, fmt.Errorf("extract segment %d: %w", i, err)
		}

		chunks = append(chunks, outputPath)
	}

	return chunks, nil
}

// extractSegment extracts a portion of audio to a new WAV file with pcm_s16le encoding.
// It places -ss after -i for precise seeking and uses -to for accurate timing.
// If extraction or validation fails, it retries with normalized settings (16kHz mono).
func (s *FFmpegSplitter) extractSegment(ctx context.Context, inputPath, outputPath string, start, duration float64) error {
	// Try extraction with source sample rate/channels first
	err := s.extractSegmentWithArgs(ctx, inputPath, outputPath, start, duration, nil)
	if err == nil {
		// Validate the output file format
		info, validateErr := s.validateWAVChunk(ctx, outputPath)
		if validateErr == nil && info.CodecName == codecPCM16LE && info.Duration > 0 {
			return nil
		}
		// Validation failed, fall through to retry with normalization
	}

	// Retry with normalization: 16kHz mono
	normalizeArgs := []string{"-ar", "16000", "-ac", "1"}
	if retryErr := s.extractSegmentWithArgs(ctx, inputPath, outputPath, start, duration, normalizeArgs); retryErr != nil {
		if err != nil {
			return fmt.Errorf("extraction failed with normalization: %w", errors.Join(retryErr, err))
		}
		return fmt.Errorf("extraction failed with normalization: %w", retryErr)
	}

	// Validate after normalization
	info, validateErr := s.validateWAVChunk(ctx, outputPath)
	if validateErr != nil {
		return fmt.Errorf("validation failed after normalization: %w", validateErr)
	}
	if info.CodecName != codecPCM16LE {
		return fmt.Errorf("%w: got codec %s", ErrInvalidWAVFormat, info.CodecName)
	}
	if info.Duration <= 0 {
		return fmt.Errorf("%w: %.3f", ErrInvalidDuration, info.Duration)
	}

	return nil
}

// extractSegmentWithArgs extracts audio segment with optional extra arguments.
func (s *FFmpegSplitter) extractSegmentWithArgs(ctx context.Context, inputPath, outputPath string, start, duration float64, extraArgs []string) error {
	// Build args: -y -i input -ss start -to end -vn -acodec pcm_s16le [extraArgs] output
	// Place -ss after -i for precise seeking
	endTime := start + duration
	args := make([]string, 0, 10+len(extraArgs)+1)
	args = append(args,
		"-y", // Overwrite output
		"-i", inputPath,
		"-ss", fmt.Sprintf("%.3f", start),
		"-to", fmt.Sprintf("%.3f", endTime),
		"-vn",                   // No video
		"-acodec", codecPCM16LE, // Force PCM 16-bit little-endian
	)

	// Add extra arguments (e.g., normalization)
	args = append(args, extraArgs...)
	args = append(args, outputPath)

	// #nosec G204 - ffmpegPath is set by the application, not user input
	cmd := exec.CommandContext(ctx, s.ffmpegPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg error: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// copyAudio copies an audio file to a new location as WAV with pcm_s16le encoding.
// If the initial copy fails or validation fails, it retries with normalized settings (16kHz mono).
func (s *FFmpegSplitter) copyAudio(ctx context.Context, src, dst string) error {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(dst), 0750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Try with source sample rate/channels first
	err := s.copyAudioWithArgs(ctx, src, dst, nil)
	if err == nil {
		// Validate the output file format
		info, validateErr := s.validateWAVChunk(ctx, dst)
		if validateErr == nil && info.CodecName == codecPCM16LE && info.Duration > 0 {
			return nil
		}
		// Validation failed, fall through to retry with normalization
	}

	// Retry with normalization: 16kHz mono
	normalizeArgs := []string{"-ar", "16000", "-ac", "1"}
	if retryErr := s.copyAudioWithArgs(ctx, src, dst, normalizeArgs); retryErr != nil {
		if err != nil {
			return fmt.Errorf("copy failed with normalization: %w", errors.Join(retryErr, err))
		}
		return fmt.Errorf("copy failed with normalization: %w", retryErr)
	}

	// Validate after normalization
	info, validateErr := s.validateWAVChunk(ctx, dst)
	if validateErr != nil {
		return fmt.Errorf("validation failed after normalization: %w", validateErr)
	}
	if info.CodecName != codecPCM16LE {
		return fmt.Errorf("%w: got codec %s", ErrInvalidWAVFormat, info.CodecName)
	}
	if info.Duration <= 0 {
		return fmt.Errorf("%w: %.3f", ErrInvalidDuration, info.Duration)
	}

	return nil
}

// copyAudioWithArgs copies audio with optional extra arguments.
func (s *FFmpegSplitter) copyAudioWithArgs(ctx context.Context, src, dst string, extraArgs []string) error {
	args := make([]string, 0, 6+len(extraArgs)+1)
	args = append(args,
		"-y",
		"-i", src,
		"-vn",                   // No video
		"-acodec", codecPCM16LE, // Force PCM 16-bit little-endian
	)

	// Add extra arguments (e.g., normalization)
	args = append(args, extraArgs...)
	args = append(args, dst)

	// #nosec G204 - ffmpegPath is set by the application, not user input
	cmd := exec.CommandContext(ctx, s.ffmpegPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg error: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// validateWAVChunk uses ffprobe to verify that a file is a valid WAV with pcm_s16le codec.
// Returns WAVInfo containing format details or an error if validation fails.
func (s *FFmpegSplitter) validateWAVChunk(ctx context.Context, filePath string) (*WAVInfo, error) {
	// Run ffprobe to get format info in JSON
	// #nosec G204 - ffprobePath is set by the application, not user input
	cmd := exec.CommandContext(ctx, s.ffprobePath,
		"-v", "quiet",
		"-show_format",
		"-show_streams",
		"-select_streams", "a:0",
		"-of", "json",
		filePath,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe error: %w, stderr: %s", err, stderr.String())
	}

	// Parse JSON output
	info := parseFFprobeOutput(stdout.String())

	return info, nil
}

// parseFFprobeOutput parses ffprobe JSON output to extract WAV info.
func parseFFprobeOutput(output string) *WAVInfo {
	// Simple regex-based parsing using pre-compiled patterns
	info := &WAVInfo{}

	// Extract format_name
	if match := reFormatName.FindStringSubmatch(output); len(match) > 1 {
		info.FormatName = match[1]
	}

	// Extract codec_name
	if match := reCodecName.FindStringSubmatch(output); len(match) > 1 {
		info.CodecName = match[1]
	}

	// Extract sample_rate
	if match := reSampleRate.FindStringSubmatch(output); len(match) > 1 {
		if rate, err := strconv.Atoi(match[1]); err == nil {
			info.SampleRate = rate
		}
	}

	// Extract channels
	if match := reChannels.FindStringSubmatch(output); len(match) > 1 {
		if ch, err := strconv.Atoi(match[1]); err == nil {
			info.Channels = ch
		}
	}

	// Extract duration (from format section)
	if match := reDuration.FindStringSubmatch(output); len(match) > 1 {
		if dur, err := strconv.ParseFloat(match[1], 64); err == nil {
			info.Duration = dur
		}
	}

	return info
}

// ValidateChunk validates that a chunk file is a valid WAV with pcm_s16le encoding.
// This is a public utility function for external validation.
func (s *FFmpegSplitter) ValidateChunk(ctx context.Context, filePath string) (*WAVInfo, error) {
	info, err := s.validateWAVChunk(ctx, filePath)
	if err != nil {
		return nil, err
	}

	// Check for valid WAV format
	if info.CodecName != codecPCM16LE {
		return info, fmt.Errorf("%w: got codec %s", ErrInvalidWAVFormat, info.CodecName)
	}
	if info.Duration <= 0 {
		return info, fmt.Errorf("%w: %.3f", ErrInvalidDuration, info.Duration)
	}

	return info, nil
}

// GetSilences is a utility function to get silence intervals from an audio file.
// This can be useful for debugging or testing.
func (s *FFmpegSplitter) GetSilences(ctx context.Context, inputPath string, opts SplitOpts) ([]SilenceInterval, error) {
	return s.detectSilences(ctx, inputPath, opts)
}

// ListChunks lists all chunk files in a directory sorted by name.
func ListChunks(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var chunks []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "chunk_") && strings.HasSuffix(entry.Name(), ".wav") {
			chunks = append(chunks, filepath.Join(dir, entry.Name()))
		}
	}

	sort.Strings(chunks)
	return chunks, nil
}

// Verify interface implementation at compile time.
var _ Splitter = (*FFmpegSplitter)(nil)
