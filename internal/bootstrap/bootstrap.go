// Package bootstrap provides dependency initialization for the InfiniteTalk API.
package bootstrap

import (
	"fmt"
	"log/slog"

	"github.com/maauso/infinitetalk-api/internal/audio"
	"github.com/maauso/infinitetalk-api/internal/config"
	"github.com/maauso/infinitetalk-api/internal/job"
	"github.com/maauso/infinitetalk-api/internal/media"
	"github.com/maauso/infinitetalk-api/internal/runpod"
	"github.com/maauso/infinitetalk-api/internal/storage"
)

// Dependencies holds all initialized dependencies for the HTTP server.
type Dependencies struct {
	VideoService *job.ProcessVideoService
}

// NewDependencies creates and initializes all dependencies for the application.
func NewDependencies(cfg *config.Config, logger *slog.Logger) (*Dependencies, error) {
	// Initialize storage
	store, err := initStorage(cfg, logger)
	if err != nil {
		return nil, err
	}

	// Initialize RunPod client
	runpodClient, err := runpod.NewClient(cfg.RunPodEndpointID, runpod.WithAPIKey(cfg.RunPodAPIKey))
	if err != nil {
		return nil, fmt.Errorf("create RunPod client: %w", err)
	}

	// Initialize media processor and audio splitter
	processor := media.NewFFmpegProcessor("")
	splitter := audio.NewFFmpegSplitter("")

	// Initialize job repository
	repo := job.NewMemoryRepository()

	// Configure audio split options
	splitOpts := audio.SplitOpts{
		ChunkTargetSec:  cfg.ChunkTargetSec,
		MinSilenceMs:    500,
		SilenceThreshDB: -40,
	}

	// Initialize ProcessVideoService
	svc := job.NewProcessVideoService(
		repo,
		processor,
		splitter,
		runpodClient,
		store,
		logger,
		job.WithSplitOpts(splitOpts),
	)

	return &Dependencies{
		VideoService: svc,
	}, nil
}

// initStorage creates the appropriate storage backend based on configuration.
func initStorage(cfg *config.Config, logger *slog.Logger) (storage.Storage, error) {
	if cfg.S3Enabled() {
		s3Cfg := storage.S3Config{
			Bucket:          cfg.S3Bucket,
			Region:          cfg.S3Region,
			AccessKeyID:     cfg.AWSAccessKeyID,
			SecretAccessKey: cfg.AWSSecretAccessKey,
		}
		s3Store, err := storage.NewS3Storage(cfg.TempDir, s3Cfg)
		if err != nil {
			return nil, fmt.Errorf("create S3 storage: %w", err)
		}
		logger.Info("S3 storage configured",
			slog.String("bucket", cfg.S3Bucket),
			slog.String("region", cfg.S3Region),
		)
		return s3Store, nil
	}

	localStore, err := storage.NewLocalStorage(cfg.TempDir)
	if err != nil {
		return nil, fmt.Errorf("create local storage: %w", err)
	}
	logger.Info("local storage configured",
		slog.String("temp_dir", cfg.TempDir),
	)
	return localStore, nil
}
