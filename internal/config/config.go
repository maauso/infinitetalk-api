// Package config provides configuration loading from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
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
	Port int `json:"port"`

	// RunPod settings
	RunPodAPIKey     string `json:"-"` // Masked in JSON
	RunPodEndpointID string `json:"runpod_endpoint_id"`

	// Storage settings
	TempDir string `json:"temp_dir"`

	// Processing settings
	MaxConcurrentChunks int `json:"max_concurrent_chunks"`
	ChunkTargetSec      int `json:"chunk_target_sec"`

	// Optional S3 settings
	S3Bucket          string `json:"s3_bucket,omitempty"`
	S3Region          string `json:"s3_region,omitempty"`
	AWSAccessKeyID    string `json:"-"` // Masked in JSON
	AWSSecretAccessKey string `json:"-"` // Masked in JSON

	// Logging settings
	LogFormat string `json:"log_format"` // "json" or "text"
	LogLevel  string `json:"log_level"`  // "debug", "info", "warn", "error"
}

// S3Enabled returns true if S3 configuration is provided.
func (c *Config) S3Enabled() bool {
	return c.S3Bucket != "" && c.S3Region != ""
}

// Load reads configuration from environment variables.
// It returns an error if required variables are not set.
func Load() (*Config, error) {
	cfg := &Config{
		// Defaults
		Port:                getEnvInt("PORT", 8080),
		TempDir:             getEnv("TEMP_DIR", "/tmp/infinitetalk"),
		MaxConcurrentChunks: getEnvInt("MAX_CONCURRENT_CHUNKS", 3),
		ChunkTargetSec:      getEnvInt("CHUNK_TARGET_SEC", 45),
		LogFormat:           getEnv("LOG_FORMAT", "text"),
		LogLevel:            getEnv("LOG_LEVEL", "info"),

		// Required
		RunPodAPIKey:     os.Getenv("RUNPOD_API_KEY"),
		RunPodEndpointID: os.Getenv("RUNPOD_ENDPOINT_ID"),

		// Optional S3
		S3Bucket:          os.Getenv("S3_BUCKET"),
		S3Region:          os.Getenv("S3_REGION"),
		AWSAccessKeyID:    os.Getenv("AWS_ACCESS_KEY_ID"),
		AWSSecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
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
		"Config{Port: %d, RunPodEndpointID: %s, TempDir: %s, MaxConcurrentChunks: %d, ChunkTargetSec: %d, S3Bucket: %s, S3Region: %s, LogFormat: %s, LogLevel: %s}",
		c.Port,
		c.RunPodEndpointID,
		c.TempDir,
		c.MaxConcurrentChunks,
		c.ChunkTargetSec,
		c.S3Bucket,
		c.S3Region,
		c.LogFormat,
		c.LogLevel,
	)
}

// getEnv returns the value of the environment variable or a default value.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt returns the value of the environment variable as an integer or a default value.
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
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
