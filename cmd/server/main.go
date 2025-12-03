// Package main provides the entry point for the InfiniteTalk API server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maauso/infinitetalk-api/internal/bootstrap"
	"github.com/maauso/infinitetalk-api/internal/config"
	"github.com/maauso/infinitetalk-api/internal/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration from environment
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Create structured logger
	logger := cfg.NewLogger()
	slog.SetDefault(logger)

	logger.Info("starting InfiniteTalk API",
		slog.Int("port", cfg.Port),
		slog.String("log_format", cfg.LogFormat),
		slog.String("log_level", cfg.LogLevel),
		slog.String("temp_dir", cfg.TempDir),
		slog.Int("max_concurrent_chunks", cfg.MaxConcurrentChunks),
		slog.Int("chunk_target_sec", cfg.ChunkTargetSec),
		slog.Bool("s3_enabled", cfg.S3Enabled()),
	)

	// Initialize dependencies using bootstrap
	deps, err := bootstrap.NewDependencies(cfg, logger)
	if err != nil {
		return fmt.Errorf("initialize dependencies: %w", err)
	}

	// Initialize HTTP handlers and router
	handlers := server.NewHandlers(deps.VideoService, logger)
	router := server.NewRouter(handlers, logger, server.DefaultConfig())

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second, // Allow for long video processing
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown handling
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening",
			slog.String("addr", srv.Addr),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("server failed: %w", err)
		}
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-shutdownCh:
		logger.Info("received shutdown signal",
			slog.String("signal", sig.String()),
		)
	case err := <-errCh:
		return err
	}

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger.Info("shutting down server...")
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown failed: %w", err)
	}

	logger.Info("server stopped gracefully")
	return nil
}
