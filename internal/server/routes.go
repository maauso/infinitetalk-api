package server

import (
	"log/slog"
	"net/http"
)

// Config contains server configuration options.
type Config struct {
	// AllowedOrigins is the list of allowed CORS origins.
	AllowedOrigins []string
}

// DefaultConfig returns a Config with default values.
func DefaultConfig() Config {
	return Config{
		AllowedOrigins: []string{"*"},
	}
}

// NewRouter creates a new HTTP router with all routes configured.
// It uses Go 1.22+ ServeMux with method-based routing.
func NewRouter(h *Handlers, logger *slog.Logger, cfg Config) http.Handler {
	mux := http.NewServeMux()

	// Register routes with method-based patterns (Go 1.22+)
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("POST /jobs", h.CreateJob)
	mux.HandleFunc("GET /jobs/{id}", h.GetJob)

	// Apply middleware chain
	chain := ChainMiddleware(
		RecoveryMiddleware(logger),
		LoggingMiddleware(logger),
		CORSMiddleware(cfg.AllowedOrigins),
	)

	return chain(mux)
}
