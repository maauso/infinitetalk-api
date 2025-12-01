package audio

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// FFmpegSplitter implements Splitter using ffmpeg CLI.
type FFmpegSplitter struct {
	ffmpegPath string
}

// NewFFmpegSplitter creates a new FFmpegSplitter.
// If ffmpegPath is empty, it defaults to "ffmpeg" (found in PATH).
func NewFFmpegSplitter(ffmpegPath string) *FFmpegSplitter {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &FFmpegSplitter{ffmpegPath: ffmpegPath}
}

// silenceInterval represents a detected silence interval in the audio.
type silenceInterval struct {
	start float64
	end   float64
}

// Split implements Splitter.Split using ffmpeg silencedetect and segment extraction.
func (s *FFmpegSplitter) Split(ctx context.Context, inputWav, outputDir string, opts SplitOpts) ([]string, error) {
	// Validate input file exists
	if _, err := os.Stat(inputWav); os.IsNotExist(err) {
		return nil, fmt.Errorf("input file does not exist: %s", inputWav)
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
	cmd := exec.CommandContext(ctx, s.ffmpegPath,
		"-i", inputPath,
		"-hide_banner",
		"-f", "null", "-",
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// ffmpeg writes duration info to stderr
	_ = cmd.Run() // Ignore error as ffmpeg exits with error when output is null

	// Parse duration from stderr
	// Looking for: "Duration: HH:MM:SS.ms"
	output := stderr.String()
	re := regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) < 5 {
		return 0, fmt.Errorf("could not parse duration from ffmpeg output: %s", output)
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
func (s *FFmpegSplitter) detectSilences(ctx context.Context, inputPath string, opts SplitOpts) ([]silenceInterval, error) {
	// Build silencedetect filter
	filter := fmt.Sprintf("silencedetect=noise=%ddB:d=%f",
		int(opts.SilenceThreshDb),
		float64(opts.MinSilenceMs)/1000.0,
	)

	cmd := exec.CommandContext(ctx, s.ffmpegPath,
		"-i", inputPath,
		"-af", filter,
		"-f", "null",
		"-hide_banner",
		"-",
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// ffmpeg writes silencedetect output to stderr
	_ = cmd.Run() // Ignore error as ffmpeg exits with error when output is null

	return parseSilenceOutput(stderr.String())
}

// parseSilenceOutput parses ffmpeg silencedetect output.
func parseSilenceOutput(output string) ([]silenceInterval, error) {
	var intervals []silenceInterval

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
			intervals = append(intervals, silenceInterval{
				start: currentStart,
				end:   val,
			})
			hasStart = false
		}
	}

	return intervals, nil
}

// calculateSplitPoints determines optimal split points based on silence intervals.
func (s *FFmpegSplitter) calculateSplitPoints(silences []silenceInterval, totalDuration float64, targetSec int) []float64 {
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
			splitPoint := (bestSilence.start + bestSilence.end) / 2
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
func findBestSilence(silences []silenceInterval, idealPoint, tolerance float64) *silenceInterval {
	var best *silenceInterval
	bestDistance := tolerance

	for i := range silences {
		// Use the middle of the silence as reference
		silenceMiddle := (silences[i].start + silences[i].end) / 2

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
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	// Build segment boundaries
	var segments [][2]float64
	start := 0.0
	for _, point := range splitPoints {
		segments = append(segments, [2]float64{start, point})
		start = point
	}
	// Add final segment
	segments = append(segments, [2]float64{start, totalDuration})

	var chunks []string
	for i, seg := range segments {
		outputPath := filepath.Join(outputDir, fmt.Sprintf("chunk_%03d.wav", i))

		if err := s.extractSegment(ctx, inputPath, outputPath, seg[0], seg[1]-seg[0]); err != nil {
			// Cleanup already created chunks on error
			for _, chunk := range chunks {
				os.Remove(chunk)
			}
			return nil, fmt.Errorf("extract segment %d: %w", i, err)
		}

		chunks = append(chunks, outputPath)
	}

	return chunks, nil
}

// extractSegment extracts a portion of audio to a new file.
func (s *FFmpegSplitter) extractSegment(ctx context.Context, inputPath, outputPath string, start, duration float64) error {
	args := []string{
		"-y", // Overwrite output
		"-ss", fmt.Sprintf("%.3f", start),
		"-t", fmt.Sprintf("%.3f", duration),
		"-i", inputPath,
		"-c", "copy", // Copy without re-encoding
		outputPath,
	}

	cmd := exec.CommandContext(ctx, s.ffmpegPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg error: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// copyAudio copies an audio file to a new location.
func (s *FFmpegSplitter) copyAudio(ctx context.Context, src, dst string) error {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	cmd := exec.CommandContext(ctx, s.ffmpegPath,
		"-y",
		"-i", src,
		"-c", "copy",
		dst,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg error: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

// GetSilences is a utility function to get silence intervals from an audio file.
// This can be useful for debugging or testing.
func (s *FFmpegSplitter) GetSilences(ctx context.Context, inputPath string, opts SplitOpts) ([]silenceInterval, error) {
	return s.detectSilences(ctx, inputPath, opts)
}

// ListChunks lists all chunk files in a directory sorted by name.
func ListChunks(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
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

// parseSilenceDetectOutput is an alternative parser using bufio for efficiency.
func parseSilenceDetectOutput(output string) ([]silenceInterval, error) {
	var intervals []silenceInterval
	scanner := bufio.NewScanner(strings.NewReader(output))

	var currentStart float64
	hasStart := false

	for scanner.Scan() {
		line := scanner.Text()

		if strings.Contains(line, "silence_start:") {
			parts := strings.Split(line, "silence_start:")
			if len(parts) > 1 {
				val, err := strconv.ParseFloat(strings.TrimSpace(strings.Fields(parts[1])[0]), 64)
				if err == nil {
					currentStart = val
					hasStart = true
				}
			}
		}

		if strings.Contains(line, "silence_end:") && hasStart {
			parts := strings.Split(line, "silence_end:")
			if len(parts) > 1 {
				val, err := strconv.ParseFloat(strings.TrimSpace(strings.Fields(parts[1])[0]), 64)
				if err == nil {
					intervals = append(intervals, silenceInterval{
						start: currentStart,
						end:   val,
					})
					hasStart = false
				}
			}
		}
	}

	return intervals, scanner.Err()
}
