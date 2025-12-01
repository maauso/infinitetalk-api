# Infinitetalk API - AI Agent Instructions

## Project Overview

Infinitetalk API is a Go REST service that provides a front-end interface for RunPod lip-sync video generation. The project is migrating Python functionality (`libsync.py`) to Go with hexagonal architecture, following Effective Go, SOLID principles, and tactical DDD.

**Current Status**: Early stage - Python prototype exists, Go implementation follows phased plan in `plan.md`.

## Architecture & Design Philosophy

### Hexagonal Architecture (Ports & Adapters)
- **Domain core** in `internal/{job,media,audio}` defines interfaces (ports)
- **Adapters** implement interfaces: `ffmpeg.go`, `runpod/client.go`, `storage/local.go`
- Dependencies point inward: infrastructure depends on domain, never reverse

### Package Organization
Organized by **functionality**, not layers (idiomatic Go):
```
internal/
├── job/         # Job aggregate: domain entity + repository interface + use case
├── media/       # Image/video processing (Processor interface + ffmpeg impl)
├── audio/       # Audio splitting (Splitter interface + ffmpeg impl)
├── runpod/      # RunPod HTTP client adapter
├── storage/     # Temp storage + optional S3
└── server/      # HTTP handlers, routes, DTOs
```

**Pattern**: Each package exports interface (port) + concrete implementation(s).

## Critical Domain Knowledge

### Video Processing Workflow
1. **Image preparation**: Resize with padding to target dimensions (384x576, 512x512, etc.)
2. **Audio splitting**: Smart chunking at silence boundaries (~45s target, 500ms min silence, -40dBFS threshold)
3. **Parallel processing**: Submit chunks to RunPod, poll status, collect partial videos
4. **Video stitching**: Concatenate with `ffmpeg -c copy` (fast), fallback to re-encode if needed
5. **Delivery**: Return video inline or push to S3 based on `push_to_s3` flag

### Job States (aligned with RunPod)
```
IN_QUEUE → RUNNING → COMPLETED
                  ↘ FAILED
                  ↘ CANCELLED
                  ↘ TIMED_OUT
```
Implement state machine with valid transitions in `internal/job/job.go`.

### FFmpeg Patterns
- **Concatenation**: Try `-c copy` first (stream copy, no re-encode), fallback to `-c:v libx264 -c:a aac`
- **Error handling**: Capture stderr, include in structured errors
- **Timeouts**: Use `context.WithTimeout` for all CLI calls
- **Audio analysis**: `ffmpeg -af silencedetect` to find split points

## Development Guidelines

### Go Standards
- Go **1.25.4+** (uses `net/http` ServeMux with method routing from 1.22+)
- Short variable names, `gofmt` formatting
- Stdlib-first: `net/http`, `context`, `slog` for logging
- No external frameworks - prefer standard library

### Routing (net/http)
```go
mux := http.NewServeMux()
mux.HandleFunc("POST /jobs", handlers.CreateJob)
mux.HandleFunc("GET /jobs/{id}", handlers.GetJob)
```

### Configuration
- All config from env vars (use `envconfig` or similar lightweight lib)
- Required: `RUNPOD_API_KEY`, `RUNPOD_ENDPOINT_ID`, `PORT`
- Optional: `S3_BUCKET`, `MAX_CONCURRENT_CHUNKS`, `TEMP_DIR`

### Error Handling
- Return structured JSON: `{"error": "message", "code": "ERROR_CODE"}`
- Wrap errors with context: `fmt.Errorf("resize image: %w", err)`
- Include ffmpeg stderr in media/audio errors

### Testing Strategy
- Mock interfaces for unit tests (use `httptest` for HTTP, mock ffmpeg with test fixtures)
- Integration tests with real ffmpeg (check `ffmpeg -version` in CI)
- E2E with docker-compose

## Implementation Phases (see plan.md)

**Current workflow**: Follow `plan.md` phases sequentially. Key dependencies:
- Phase 0: Scaffold (Dockerfile with ffmpeg, Makefile, go.mod)
- Phases 1-4, 7: Independent (media, audio, job domain, runpod, storage)
- Phase 5: Use case (requires 1-4, 7)
- Phase 6: HTTP server (requires 3, 5)
- Phases 8-9: Config/observability, E2E

**When adding features**: Implement interface in domain package first, then adapter.

## Key File References

- `libsync.py`: Original Python implementation - reference for audio splitting logic and RunPod integration patterns
- `plan.md`: Detailed phase-by-phase implementation guide with acceptance criteria
- Future `internal/job/service.go`: Orchestrates entire workflow with semaphore for concurrency control
- Future `internal/server/types.go`: HTTP DTOs (separate from domain `Job` entity)

## RunPod Integration

### API Patterns
- **Submit**: POST `/v2/{ENDPOINT_ID}/run` with `image_base64` and `wav_base64`
- **Poll**: GET `/v2/{ENDPOINT_ID}/status/{job_id}` until status is terminal
- **Auth**: Bearer token from `RUNPOD_API_KEY`

### Request Payload Structure
```go
type RunPodRequest struct {
    Input struct {
        InputType     string `json:"input_type"`      // "image"
        PersonCount   string `json:"person_count"`    // "single"
        Prompt        string `json:"prompt"`
        ImageBase64   string `json:"image_base64"`
        WavBase64     string `json:"wav_base64"`
        Width         int    `json:"width"`
        Height        int    `json:"height"`
        NetworkVolume bool   `json:"network_volume"`  // false
        ForceOffload  bool   `json:"force_offload"`   // false
    } `json:"input"`
}
```

## Common Tasks

### Adding New Storage Backend
1. Implement `Storage` interface in `internal/storage/`
2. Add config vars for credentials
3. Update constructor in `cmd/server/main.go` to select implementation

### Changing Concurrency Model
- Modify `MAX_CONCURRENT_CHUNKS` env var
- Update semaphore in `ProcessVideoService` (Phase 5)

### Adding New Media Operation
1. Add method to `Processor` interface in `internal/media/processor.go`
2. Implement in `internal/media/ffmpeg.go`
3. Add unit test with fixture files

## Docker & Build

- **Dockerfile**: Multi-stage (Go builder + Debian slim with ffmpeg)
- **Makefile targets**: `build`, `test`, `lint` (golangci-lint)
- **CI**: Lint + test on PR (GitHub Actions / GitLab CI)

## Extensibility Points

- **Notification strategy**: Future interface for webhook/SSE (currently polling only)
- **Job persistence**: Interface ready to swap in-memory for Redis/Postgres
- **Status delivery**: `JobStatusStrategy` interface for different client patterns
