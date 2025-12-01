# Implementation Plan: Infinitetalk API

> **Infinitetalk** is a front-end interface for RunPod for lip-sync video generation.

## Summary

Migrate the functionalities of `libsync.py` to a REST API in Go following:

- **Effective Go**: short names, small interfaces, `gofmt`, idiomatic error handling, goroutines/channels for concurrency.
- **Hexagonal Architecture (Ports & Adapters)**: domain core decoupled from infrastructure.
- **SOLID**: especially SRP, OCP, DIP (dependencies toward abstractions).
- **Tactical DDD**: Value Objects, lightweight Aggregates, Application Services.

### Phase Status

| Phase | Description | Status |
|-------|-------------|--------|
| **Phase 0** | Scaffold and Base CI | ✅ Completed (Makefile/CI pending) |
| **Phase 1** | `internal/media` - FFmpeg CLI | ✅ Completed |
| **Phase 2** | `internal/audio` - Silence-based Splitter | ✅ Completed |
| **Phase 3** | `internal/job` - Job Aggregate | ✅ Completed |
| **Phase 4** | `internal/runpod` - RunPod Client | ✅ Completed |
| **Phase 5** | Use Case `ProcessVideo` | ✅ Completed |
| **Phase 6** | HTTP Server | ⏳ Not started |
| **Phase 7** | `internal/storage` - Storage | ✅ Completed |
| **Phase 8** | Configuration and Observability | ⏳ Not started |
| **Phase 9** | Integration and E2E | ⏳ Not started |

---

## Technical Decisions

| Decision | Value | Justification |
|----------|-------|---------------|
| **Go version** | `go1.25.4` | Latest stable; `ServeMux` with advanced routing since 1.22 |
| **HTTP Router** | `net/http` stdlib | Simple API; `ServeMux` supports `{id}`, methods, sufficient without framework |
| **Jobs Persistence** | In-memory (`map` + `sync.RWMutex`) | Simple MVP; interface ready for swap |
| **Temporary Storage** | Local disk (`/tmp` or configurable) | Better than memory for large video files |
| **Video Delivery** | Direct response + optional S3 push flag | `push_to_s3` in payload enables S3 upload |
| **Status Query** | Polling (`GET /jobs/{id}`) | Extensible via `JobStatusStrategy` interface |
| **Video Concatenation** | `-c copy` first, fallback re-encode | Faster if codecs compatible |

---

## Proposed Folder Structure

**Organization by functionality** (idiomatic Go), not by technical layers:

```
infinitetalk-api/
├── cmd/
│   └── server/          # Entrypoint (main.go)
├── internal/
│   ├── job/             # Job Aggregate (domain + use cases)
│   │   ├── job.go           # Job Entity, states, transitions
│   │   ├── repository.go    # JobRepository Interface (port)
│   │   ├── memory.go        # In-memory implementation
│   │   └── service.go       # Use case: ProcessVideo
│   ├── media/           # Image/video processing
│   │   ├── processor.go     # Processor Interface (port)
│   │   └── ffmpeg.go        # Implementation via ffmpeg CLI
│   ├── audio/           # Audio processing
│   │   ├── splitter.go      # Splitter Interface (port)
│   │   └── ffmpeg.go        # Implementation via ffmpeg CLI
│   ├── runpod/          # RunPod Client (adapter)
│   │   ├── client.go        # Interface + HTTP implementation
│   │   └── types.go         # RunPod DTOs
│   ├── storage/         # Temporary storage and S3
│   │   ├── storage.go       # StoragePort Interface (port)
│   │   ├── local.go         # Local disk storage
│   │   └── s3.go            # S3 storage (optional)
│   ├── server/          # HTTP server
│   │   ├── handlers.go      # HTTP Handlers
│   │   ├── middleware.go    # Middlewares (logging, recovery, CORS)
│   │   ├── routes.go        # Route configuration
│   │   └── types.go         # HTTP DTOs (request/response)
│   └── config/          # Configuration
│       └── config.go        # Load from env
├── pkg/                 # Reusable code (if needed)
├── api/                 # OpenAPI spec (optional)
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod / go.sum
└── README.md
```

**Principles of this structure:**
- ✅ **Cohesion**: Related code together (job.go + repository.go + service.go)
- ✅ **Idiomatic Go**: Organization by functionality, not abstract layers
- ✅ **Hexagonal**: Interfaces (ports) in each package, concrete implementations
- ✅ **Navigability**: "Need to change the job → `internal/job/`"
- ✅ **Testable**: Easy to mock interfaces of each package

---

## Phase 0 — Scaffold and Base CI ✅ COMPLETED

> **Status**: Completed (2024-12-01)
> **Pending**: `Makefile` and CI pipeline (GitHub Actions) to be added separately.

**Description**
Create the Go project skeleton: `go mod init`, folder structure, `Makefile` with targets (`build`, `test`, `lint`), base Dockerfile with `ffmpeg`, and minimal CI pipeline (lint + test).

**Deliverables**

- ✅ `go.mod` with module `github.com/<org>/infinitetalk-api` and `go 1.25`.
- ✅ Multi-stage Dockerfile: Go 1.25 builder + final Debian slim image with `ffmpeg`.
- ⏳ `Makefile` with `make build`, `make test`, `make lint` (golangci-lint) — *pending*.
- ⏳ GitHub Actions / GitLab CI that runs lint + test on each PR — *pending*.

**Acceptance Criteria**

1. ⏳ `make build` generates binary without errors.
2. ⏳ `make lint` passes with default `golangci-lint` config.
3. ✅ `docker build .` produces functional image with `ffmpeg -version` OK.
4. ⏳ Green CI pipeline on `main` branch.

---

## Phase 1 — Package `internal/media` (FFmpeg CLI) ✅ COMPLETED

> **Status**: Completed (2024-12-01)

**Description**
Implement image/video processing via `ffmpeg` with `os/exec`:

- Concatenate videos (concat demuxer + fallback re-encode).
- Resize image with padding (using `ffmpeg` or `imaging` library).
- Timeout with `context`, stderr capture, structured errors.

**Files**:
- ✅ `internal/media/processor.go` - Processor Interface (port)
- ✅ `internal/media/ffmpeg.go` - Implementation via CLI
- ✅ `internal/media/ffmpeg_test.go` - Unit tests

**Interface**

```go
package media

type Processor interface {
    ResizeImageWithPadding(ctx context.Context, src, dst string, w, h int) error
    JoinVideos(ctx context.Context, videoPaths []string, output string) error
}
```

**Acceptance Criteria**

1. ✅ Unit tests with dummy files (1x1 image, short videos) pass.
2. ✅ `JoinVideos` tries `-c copy`; if it fails, re-encodes with `libx264/aac`.
3. ✅ Errors include `ffmpeg` stderr for debugging (`FFmpegError` type).
4. ✅ Supports cancellation via `context.WithTimeout`.

---

## Phase 2 — Package `internal/audio` (Silence-based Splitter) ✅ COMPLETED

> **Status**: Completed (2024-12-01)

**Description**
Replicate `split_audio_smartly` from Python using `ffmpeg` CLI:

- Call `ffmpeg -af silencedetect` + parse stdout.
- Cut segments with `ffmpeg` (less dependency than `libav`).

**Files**:
- ✅ `internal/audio/splitter.go` - Splitter Interface (port)
- ✅ `internal/audio/ffmpeg.go` - Implementation via CLI
- ✅ `internal/audio/ffmpeg_test.go` - Unit tests

**Interface**

```go
package audio

type Splitter interface {
    Split(ctx context.Context, inputWav, outputDir string, opts SplitOpts) ([]string, error)
}

type SplitOpts struct {
    ChunkTargetSec  int     // 45
    MinSilenceMs    int     // 500
    SilenceThreshDb float64 // -40
}
```

**Acceptance Criteria**

1. ✅ Given a 2-min WAV with silences, generates N chunks in temporary folder.
2. ✅ If audio ≤ `ChunkTargetSec`, returns slice with a single path.
3. ✅ Tests with test WAV file (can be generated with `ffmpeg -f lavfi`).
4. ✅ Temporary file cleanup managed by caller (returns paths).

---

## Phase 3 — Package `internal/job` (Job Aggregate) ✅ COMPLETED

> **Status**: Completed (2024-12-01)

**Description**
Implement the Job aggregate with its domain, repository and use case:

- `Job` entity: ID, Status, list of `Chunk` with their state, timestamps, errors.
- **Job States** (aligned with RunPod): `IN_QUEUE`, `RUNNING`, `COMPLETED`, `FAILED`, `CANCELLED`, `TIMED_OUT`.
- In-memory repository (`map` + `sync.RWMutex`).
- `ProcessVideo` use case (completed in Phase 5).

**Files**:
- ✅ `internal/job/job.go` - Job Entity, states, transitions
- ✅ `internal/job/repository.go` - JobRepository Interface (port)
- ✅ `internal/job/memory.go` - In-memory implementation
- ✅ `internal/job/service.go` - ProcessVideo use case (scaffold)
- ✅ `internal/job/id/id.go` - Job ID generation (Value Object)
- ✅ Test files: `job_test.go`, `memory_test.go`, `service_test.go`, `id/id_test.go`

**RunPod States (reference)**:
```
IN_QUEUE   → Waiting for available worker
RUNNING    → Worker processing
COMPLETED  → Success, result available
FAILED     → Error during execution
CANCELLED  → Manually cancelled via /cancel/{job_id}
TIMED_OUT  → Expired before pickup or worker did not respond in time
```

**Acceptance Criteria**

1. ✅ `Job` has methods for valid state transitions (state machine).
2. ✅ `JobRepository` interface defined; in-memory implementation functional.
3. ✅ Tests validate business rules (e.g., cannot transition from `COMPLETED` to `IN_QUEUE`).
4. ✅ Godoc documentation on all public types.

---

## Phase 4 — Package `internal/runpod` (RunPod Client) ✅ COMPLETED

> **Status**: Completed (2024-12-01)

**Description**
HTTP client for RunPod using `net/http` stdlib:

- `Submit(ctx, imageB64, audioB64, opts) (jobID string, error)`
- `Poll(ctx, jobID) (status, videoB64 string, error)`

Retry with exponential backoff. Mapping of RunPod states to domain.

**Files**:
- ✅ `internal/runpod/client.go` - Interface + HTTP implementation
- ✅ `internal/runpod/types.go` - RunPod request/response DTOs
- ✅ `internal/runpod/client_test.go` - Unit tests with mock server

**RunPod States to handle**: `IN_QUEUE`, `RUNNING`, `COMPLETED`, `FAILED`, `CANCELLED`, `TIMED_OUT`.

**Acceptance Criteria**

1. ✅ Configurable timeout; respects `context`.
2. ✅ Handles all RunPod states correctly.
3. ✅ Tests with mock server (`httptest`).
4. ✅ API key read from env (`RUNPOD_API_KEY`).

---

## Phase 5 — Use Case `ProcessVideo` (`internal/job/service.go`) ✅ COMPLETED

> **Status**: Completed (2025-12-01)

**Description**
Complete `ProcessVideoService` in `internal/job/service.go` that orchestrates:

1. Create `Job` in repository.
2. Use `media.Processor` to resize image → base64.
3. Use `audio.Splitter` to split audio → chunks.
4. For each chunk in parallel (goroutines with semaphore): submit to RunPod, poll, save partial video.
5. Use `media.Processor` to join videos.
6. Update `Job` to `COMPLETED` or `FAILED`.

**Dependencies**: `media.Processor`, `audio.Splitter`, `runpod.Client`, `storage.Storage`, `JobRepository`.

**Files**:
- ✅ `internal/job/service.go` - ProcessVideoService use case implementation
- ✅ `internal/job/service_test.go` - Comprehensive tests with mocks

**Acceptance Criteria**

1. ✅ Limited concurrency (configurable, e.g., 3 workers).
2. ✅ If a chunk fails, mark `Job` as `FAILED` and stop.
3. ✅ Emits structured logs (`slog`) for progress.
4. ✅ Tests with mocks of all dependencies.

---

## Phase 6 — HTTP Server (`internal/server`)

**Description**
REST handlers using **`net/http` stdlib** (Go 1.22+ `ServeMux`):

```go
mux := http.NewServeMux()
mux.HandleFunc("POST /jobs", h.CreateJob)
mux.HandleFunc("GET /jobs/{id}", h.GetJob)
mux.HandleFunc("GET /health", h.Health)
```

**Files**:
- `internal/server/handlers.go` - HTTP Handlers
- `internal/server/middleware.go` - Middlewares (logging, recovery, CORS)
- `internal/server/routes.go` - Route configuration
- `internal/server/types.go` - HTTP DTOs (separated from domain)

- `POST /jobs`: receives `image_base64`, `audio_base64` or multipart, `width`, `height`, `push_to_s3` (optional bool).
- `GET /jobs/{id}`: status, progress, final video (inline or S3 URL if `push_to_s3` was true).
- Custom middlewares: logging, recover, CORS, rate-limit.

**DTOs** separated from domain (`internal/job.Job`); validation with `go-playground/validator`.

**Request example**:
```json
{
  "image_base64": "...",
  "audio_base64": "...",
  "width": 384,
  "height": 576,
  "push_to_s3": false
}
```

**Response GET /jobs/{id}**:
```json
{
  "id": "job-123",
  "status": "COMPLETED",
  "progress": 100,
  "video_base64": "...",      // if push_to_s3=false
  "video_url": "https://..."  // if push_to_s3=true
}
```

**Acceptance Criteria**

1. OpenAPI spec documented (manual or generated).
2. Errors return structured JSON (`{"error": "...", "code": "..."}`).
3. Integration tests with `httptest` + use case mocks.
4. Health endpoint `GET /health`.
5. Video returned in response body (streaming) or S3 URL according to flag.

---

## Phase 7 — Package `internal/storage` (Storage) ✅ COMPLETED

> **Status**: Completed (2024-12-01)

**Description**
Implement temporary storage (local disk) and optional S3:

- **Temporary storage**: Local disk (`/tmp` or configurable `TEMP_DIR`) for audio/video chunks during processing.
- **Final storage**: Optional S3 push if `push_to_s3=true` in request.

**Files**:
- ✅ `internal/storage/storage.go` - Storage Interface (port)
- ✅ `internal/storage/local.go` - Local disk implementation
- ✅ `internal/storage/s3.go` - S3 implementation using AWS SDK v2
- ✅ `internal/storage/local_test.go` - Unit tests for local storage
- ✅ `internal/storage/s3_test.go` - Unit tests for S3 storage

**Storage Interface**:
```go
type Storage interface {
    SaveTemp(ctx context.Context, name string, data io.Reader) (path string, err error)
    LoadTemp(ctx context.Context, path string) (io.ReadCloser, error)
    CleanupTemp(ctx context.Context, paths []string) error

    // Optional S3
    UploadToS3(ctx context.Context, key string, data io.Reader) (url string, err error)
}
```

**Note**: `JobRepository` is in `internal/job/repository.go` (Phase 3).

**Acceptance Criteria**

1. ✅ Swappable interface (constructor receives implementation).
2. ✅ Tests with local storage in `/tmp`.
3. ✅ Optional S3 client, activated by config (`S3_BUCKET`, `S3_REGION`).
4. ⏳ Automatic cleanup of temporary files after completing job — *implemented in Storage, to be wired in Phase 5*.

---

## Phase 8 — Configuration and Observability

**Description**

- `internal/config`: load from env / `.env` with `envconfig` (lightweight, no dependencies).
- Structured logging: `slog` (stdlib Go 1.21+).
- Prometheus metrics (optional): `/metrics`, job counter, duration.
- OpenTelemetry tracing (optional).

**Environment variables**:
```
PORT=8080
RUNPOD_API_KEY=xxx
RUNPOD_ENDPOINT_ID=xxx
TEMP_DIR=/tmp/infinitetalk
MAX_CONCURRENT_CHUNKS=3
CHUNK_TARGET_SEC=45
# Optional S3
S3_BUCKET=
S3_REGION=
AWS_ACCESS_KEY_ID=
AWS_SECRET_ACCESS_KEY=
```

**Acceptance Criteria**

1. All sensitive variables (API keys) come from env, not hardcoded.
2. JSON logs in production (`LOG_FORMAT=json`).
3. `docker-compose.yml` starts service + Prometheus (optional).

---

## Phase 9 — Integration and E2E

**Description**

- Integrate all phases.
- E2E script or test: sends real request, waits for result.
- Document `README.md` with usage instructions.

**Acceptance Criteria**

1. `docker compose up` starts service ready to receive requests.
2. Example curl in README works.
3. E2E test passes (can be manual or CI with RunPod sandbox if available).

---

## Phase Dependency Diagram

```
Phase 0 (Scaffold)
    │
    ├──► Phase 1 (media/ffmpeg)
    │        │
    ├──► Phase 2 (audio/splitter)
    │        │
    ├──► Phase 3 (domain) ◄───────────────┐
    │        │                           │
    │        ▼                           │
    ├──► Phase 4 (runpod adapter) ────────┤
    │                                    │
    ├──► Phase 7 (storage adapter) ───────┤
    │                                    │
    │        ┌───────────────────────────┘
    │        ▼
    └──► Phase 5 (use case) ──► Phase 6 (http) ──► Phase 8 (config/obs) ──► Phase 9 (E2E)
```

**Parallel phases**: 1 (`media`), 2 (`audio`), 3 (`job`), 4 (`runpod`), 7 (`storage`) can be developed simultaneously after Phase 0.
**Phase 5** (use case) requires: 1, 2, 3, 4, 7.
**Phase 6** (`server`) requires: 3 and 5.
**Phases 8 and 9** are final integrations.

---

## Considerations and Open Questions

| # | Question | Decision |
|---|----------|----------|
| 1 | **HTTP Router**: Gin, Chi, or stdlib? | ✅ `net/http` stdlib (Go 1.22+ `ServeMux`) — sufficient for this API |
| 2 | **Jobs Persistence**: In-memory sufficient? | ✅ In-memory for MVP (`map` + `sync.RWMutex`) |
| 3 | **Status Query**: Polling or push? | ✅ Polling (`GET /jobs/{id}`); extensible interface for future webhook/SSE |
| 4 | **Video Delivery**: How to return result? | ✅ Response body by default; S3 URL if `push_to_s3=true` |
| 5 | **Temporary Storage**: Memory or disk? | ✅ Local disk (better for large video files) |
| 6 | **Re-encode Fallback**: Always `-c copy` first? | ✅ Yes, `-c copy` first (faster), fallback if it fails |

---

## Future Extensibility: Notification Strategy

To facilitate the addition of other notification methods (webhook, SSE, WebSocket):

```go
// internal/job/notifier.go
type Notifier interface {
    Notify(ctx context.Context, job *Job) error
}

// Future implementations:
// - PollingNotifier (no-op, client does GET)
// - WebhookNotifier (POST to callback URL)
// - SSENotifier (Server-Sent Events)
```

The use case (`ProcessVideoService`) will call `notifier.Notify(ctx, job)` on each state change. For polling, it will be a no-op.

---

*Last updated: 2025-12-01*
