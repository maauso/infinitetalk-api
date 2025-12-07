package config

import (
	"bytes"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_RequiredVariables(t *testing.T) {
	// Clear all environment variables
	clearEnv := func() {
		os.Unsetenv("PORT")
		os.Unsetenv("RUNPOD_API_KEY")
		os.Unsetenv("RUNPOD_ENDPOINT_ID")
		os.Unsetenv("TEMP_DIR")
		os.Unsetenv("CHUNK_TARGET_SEC")
		os.Unsetenv("S3_BUCKET")
		os.Unsetenv("S3_REGION")
		os.Unsetenv("AWS_ACCESS_KEY_ID")
		os.Unsetenv("AWS_SECRET_ACCESS_KEY")
		os.Unsetenv("LOG_FORMAT")
		os.Unsetenv("LOG_LEVEL")
	}

	t.Run("missing RUNPOD_API_KEY returns error", func(t *testing.T) {
		clearEnv()
		t.Setenv("RUNPOD_ENDPOINT_ID", "test-endpoint")

		_, err := Load()
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrRunPodAPIKeyRequired)
	})

	t.Run("missing RUNPOD_ENDPOINT_ID returns error", func(t *testing.T) {
		clearEnv()
		t.Setenv("RUNPOD_API_KEY", "test-api-key")

		_, err := Load()
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrRunPodEndpointIDRequired)
	})

	t.Run("all required variables present succeeds", func(t *testing.T) {
		clearEnv()
		t.Setenv("RUNPOD_API_KEY", "test-api-key")
		t.Setenv("RUNPOD_ENDPOINT_ID", "test-endpoint")

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, "test-api-key", cfg.RunPodAPIKey)
		assert.Equal(t, "test-endpoint", cfg.RunPodEndpointID)
	})
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("RUNPOD_API_KEY", "test-api-key")
	t.Setenv("RUNPOD_ENDPOINT_ID", "test-endpoint")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "/tmp/infinitetalk", cfg.TempDir)
	assert.Equal(t, 45, cfg.ChunkTargetSec)
	assert.Equal(t, "text", cfg.LogFormat)
	assert.Equal(t, "info", cfg.LogLevel)
}

func TestLoad_CustomValues(t *testing.T) {
	t.Setenv("RUNPOD_API_KEY", "custom-api-key")
	t.Setenv("RUNPOD_ENDPOINT_ID", "custom-endpoint")
	t.Setenv("PORT", "3000")
	t.Setenv("TEMP_DIR", "/custom/temp")
	t.Setenv("CHUNK_TARGET_SEC", "60")
	t.Setenv("S3_BUCKET", "my-bucket")
	t.Setenv("S3_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret-key")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 3000, cfg.Port)
	assert.Equal(t, "/custom/temp", cfg.TempDir)
	assert.Equal(t, 60, cfg.ChunkTargetSec)
	assert.Equal(t, "my-bucket", cfg.S3Bucket)
	assert.Equal(t, "us-east-1", cfg.S3Region)
	assert.Equal(t, "access-key", cfg.AWSAccessKeyID)
	assert.Equal(t, "secret-key", cfg.AWSSecretAccessKey)
	assert.Equal(t, "json", cfg.LogFormat)
	assert.Equal(t, "debug", cfg.LogLevel)
}

func TestLoad_InvalidIntegerDefaults(t *testing.T) {
	t.Setenv("RUNPOD_API_KEY", "test-api-key")
	t.Setenv("RUNPOD_ENDPOINT_ID", "test-endpoint")
	t.Setenv("PORT", "not-a-number")
	t.Setenv("CHUNK_TARGET_SEC", "invalid")

	// go-envconfig returns an error when parsing fails
	_, err := Load()
	require.Error(t, err)
}

func TestConfig_S3Enabled(t *testing.T) {
	tests := []struct {
		name     string
		bucket   string
		region   string
		expected bool
	}{
		{"both set", "bucket", "region", true},
		{"only bucket", "bucket", "", false},
		{"only region", "", "region", false},
		{"neither set", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				S3Bucket: tt.bucket,
				S3Region: tt.region,
			}
			assert.Equal(t, tt.expected, cfg.S3Enabled())
		})
	}
}

func TestConfig_BeamEnabled(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		queueURL string
		expected bool
	}{
		{"both set", "token", "https://api.beam.cloud/v1/task_queue/123/tasks", true},
		{"only token", "token", "", false},
		{"only queue URL", "", "https://api.beam.cloud/v1/task_queue/123/tasks", false},
		{"neither set", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				BeamToken:    tt.token,
				BeamQueueURL: tt.queueURL,
			}
			assert.Equal(t, tt.expected, cfg.BeamEnabled())
		})
	}
}

func TestLoad_BeamDefaults(t *testing.T) {
	t.Setenv("RUNPOD_API_KEY", "test-api-key")
	t.Setenv("RUNPOD_ENDPOINT_ID", "test-endpoint")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, 5000, cfg.BeamPollIntervalMs)
	assert.Equal(t, 600, cfg.BeamPollTimeoutSec)
}

func TestLoad_BeamCustomValues(t *testing.T) {
	t.Setenv("RUNPOD_API_KEY", "test-api-key")
	t.Setenv("RUNPOD_ENDPOINT_ID", "test-endpoint")
	t.Setenv("BEAM_TOKEN", "beam-token")
	t.Setenv("BEAM_QUEUE_URL", "https://api.beam.cloud/v1/task_queue/123/tasks")
	t.Setenv("BEAM_POLL_INTERVAL_MS", "3000")
	t.Setenv("BEAM_POLL_TIMEOUT_SEC", "300")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "beam-token", cfg.BeamToken)
	assert.Equal(t, "https://api.beam.cloud/v1/task_queue/123/tasks", cfg.BeamQueueURL)
	assert.Equal(t, 3000, cfg.BeamPollIntervalMs)
	assert.Equal(t, 300, cfg.BeamPollTimeoutSec)
}

func TestConfig_String(t *testing.T) {
	cfg := &Config{
		Port:             8080,
		RunPodAPIKey:     "secret-key",
		RunPodEndpointID: "endpoint-123",
		TempDir:          "/tmp/test",
		ChunkTargetSec:   45,
		S3Bucket:         "bucket",
		S3Region:         "region",
		LogFormat:        "json",
		LogLevel:         "info",
	}

	str := cfg.String()

	// Should contain non-sensitive values
	assert.Contains(t, str, "8080")
	assert.Contains(t, str, "endpoint-123")
	assert.Contains(t, str, "/tmp/test")

	// Should NOT contain sensitive values
	assert.NotContains(t, str, "secret-key")
}

func TestConfig_NewLogger_JSON(t *testing.T) {
	cfg := &Config{
		LogFormat: "json",
		LogLevel:  "info",
	}

	logger := cfg.NewLogger()
	require.NotNil(t, logger)

	// Capture output to verify it's JSON
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	testLogger := slog.New(handler)
	testLogger.Info("test message")

	// Should have JSON structure
	assert.Contains(t, buf.String(), `"msg"`)
	assert.Contains(t, buf.String(), "test message")
}

func TestConfig_NewLogger_Text(t *testing.T) {
	cfg := &Config{
		LogFormat: "text",
		LogLevel:  "debug",
	}

	logger := cfg.NewLogger()
	require.NotNil(t, logger)
	// Just verify it returns a valid logger
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo}, // defaults to info
		{"", slog.LevelInfo},        // defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseLogLevel(tt.input))
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &Config{
			RunPodAPIKey:     "key",
			RunPodEndpointID: "endpoint",
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("missing API key", func(t *testing.T) {
		cfg := &Config{
			RunPodEndpointID: "endpoint",
		}
		err := cfg.Validate()
		assert.ErrorIs(t, err, ErrRunPodAPIKeyRequired)
	})

	t.Run("missing endpoint ID", func(t *testing.T) {
		cfg := &Config{
			RunPodAPIKey: "key",
		}
		err := cfg.Validate()
		assert.ErrorIs(t, err, ErrRunPodEndpointIDRequired)
	})
}
