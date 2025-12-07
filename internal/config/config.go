// Package config provides configuration loading from environment variables.
package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/sethvargo/go-envconfig"
)

// Static errors for configuration validation.
var (
	// ErrRunPodAPIKeyRequired is returned when RUNPOD_API_KEY is not set.
	ErrRunPodAPIKeyRequired = errors.New("config: RUNPOD_API_KEY is required")
	// ErrRunPodEndpointIDRequired is returned when RUNPOD_ENDPOINT_ID is not set.
	ErrRunPodEndpointIDRequired = errors.New("config: RUNPOD_ENDPOINT_ID is required")
)

// Config holds all configuration for the application.
type Config struct {
	// Server settings
	Port int `env:"PORT, default=8080" json:"port"`

	// RunPod settings
	RunPodAPIKey     string `env:"RUNPOD_API_KEY, required" json:"-"` // Masked in JSON
	RunPodEndpointID string `env:"RUNPOD_ENDPOINT_ID, required" json:"runpod_endpoint_id"`

	// Beam settings (optional)
	BeamToken          string `env:"BEAM_TOKEN" json:"-"`                               // Masked in JSON
	BeamQueueURL       string `env:"BEAM_QUEUE_URL" json:"beam_queue_url,omitempty"`   // Task queue webhook URL
	BeamPollIntervalMs int    `env:"BEAM_POLL_INTERVAL_MS, default=5000" json:"beam_poll_interval_ms"` // Default 5s
	BeamPollTimeoutSec int    `env:"BEAM_POLL_TIMEOUT_SEC, default=600" json:"beam_poll_timeout_sec"`  // Default 10min

	// Storage settings
	TempDir string `env:"TEMP_DIR, default=/tmp/infinitetalk" json:"temp_dir"`

	// Processing settings
	ChunkTargetSec int `env:"CHUNK_TARGET_SEC, default=45" json:"chunk_target_sec"`

	// Optional S3 settings
	S3Bucket           string `env:"S3_BUCKET" json:"s3_bucket,omitempty"`
	S3Region           string `env:"S3_REGION" json:"s3_region,omitempty"`
	AWSAccessKeyID     string `env:"AWS_ACCESS_KEY_ID" json:"-"`     // Masked in JSON
	AWSSecretAccessKey string `env:"AWS_SECRET_ACCESS_KEY" json:"-"` // Masked in JSON

	// Logging settings
	LogFormat string `env:"LOG_FORMAT, default=text" json:"log_format"` // "json" or "text"
	LogLevel  string `env:"LOG_LEVEL, default=info" json:"log_level"`   // "debug", "info", "warn", "error"
}

// S3Enabled returns true if S3 configuration is provided.
func (c *Config) S3Enabled() bool {
	return c.S3Bucket != "" && c.S3Region != ""
}

// BeamEnabled returns true if Beam configuration is provided.
func (c *Config) BeamEnabled() bool {
	return c.BeamToken != "" && c.BeamQueueURL != ""
}

// Load reads configuration from environment variables using go-envconfig.
// It returns an error if required variables are not set.
func Load() (*Config, error) {
	cfg := &Config{}

	if err := envconfig.Process(context.Background(), cfg); err != nil {
		// Map envconfig errors to our domain errors for required fields
		if strings.Contains(err.Error(), "RUNPOD_API_KEY") {
			return nil, ErrRunPodAPIKeyRequired
		}
		if strings.Contains(err.Error(), "RUNPOD_ENDPOINT_ID") {
			return nil, ErrRunPodEndpointIDRequired
		}
		return nil, fmt.Errorf("config: %w", err)
	}

	return cfg, nil
}

// Validate checks that all required configuration is present.
func (c *Config) Validate() error {
	if c.RunPodAPIKey == "" {
		return ErrRunPodAPIKeyRequired
	}
	if c.RunPodEndpointID == "" {
		return ErrRunPodEndpointIDRequired
	}
	return nil
}

// NewLogger creates a structured logger based on the configuration.
// When LogFormat is "json", it outputs JSON logs suitable for production.
// Otherwise, it outputs human-readable text logs.
func (c *Config) NewLogger() *slog.Logger {
	level := parseLogLevel(c.LogLevel)

	var handler slog.Handler
	if strings.ToLower(c.LogFormat) == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})
	}

	return slog.New(handler)
}

// String returns a string representation of the config with sensitive values masked.
func (c *Config) String() string {
	return fmt.Sprintf(
		"Config{Port: %d, RunPodEndpointID: %s, BeamQueueURL: %s, TempDir: %s, ChunkTargetSec: %d, S3Bucket: %s, S3Region: %s, LogFormat: %s, LogLevel: %s}",
		c.Port,
		c.RunPodEndpointID,
		c.BeamQueueURL,
		c.TempDir,
		c.ChunkTargetSec,
		c.S3Bucket,
		c.S3Region,
		c.LogFormat,
		c.LogLevel,
	)
}

// parseLogLevel converts a string log level to slog.Level.
func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
