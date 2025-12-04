
# New feature: Expose chunk status in GET /jobs/{id}

Goal: allow the API to return the status of the "chunks" (audio/processing segments) associated with a `Job`, so clients can know how many chunks exist, how many are queued, how many are being processed, and the individual state of each.

Background:
- The domain already models `Chunk` and `ChunkStatus` in `internal/job/job.go`.
- The current processing logic updates `Chunks` and saves the `Job` to the repository.
- The `GET /jobs/{id}` handler currently returns only `id`, `status`, `progress`, `error`, and the video when completed.
- We can change the schema (not in production), so we'll propose extending the DTO and API.

Recommended design (summary):
- Add a `chunks_summary` with counts by status (total, pending, processing, completed, failed).
- Add `chunks` (detail) in the response. Previously we considered making it optional with `?include_chunks=1` to keep responses small; see decisions below.
- `ChunkResponse` will expose: `id`, `index`, `status`, `runpod_job_id` (omitempty), `error` (omitempty), `started_at` (RFC3339, omitempty), `completed_at` (RFC3339, omitempty).
- Do not expose local file paths or binary content per chunk.

Implementation phases


Phase 1 — DTOs and handler
- Update `internal/server/types.go` to add:
  - `ChunkResponse` struct
  - `ChunksSummary` struct
  - Extend `JobResponse` with `ChunksSummary` and `Chunks []ChunkResponse` (`omitempty` for optional fields).
- Update `internal/server/handlers.go` (`GetJob`) to:
  - Parse `include_chunks` from `r.URL.Query()` if we keep optional behavior.
  - Map `foundJob.Chunks` to `[]ChunkResponse` when requested or include by default depending on decision.
  - Always calculate and populate `ChunksSummary`.
  - Keep current video logic unchanged.
- Add appropriate logging.

Phase 2 — Repository and persistence
- Verify existing repository adapters (memory, others) persist `Chunks` when saving a `Job`. `MemoryRepository` already does.
- If there is a persistent backend (DB), design schema: JSONB/columns for `chunks` or a `job_chunks` table. (Not required now if only memory repo is used.)

Phase 3 — Tests and OpenAPI
- Add unit tests in `internal/server/handlers_test.go` for:
  - GET job with `include_chunks=1` → verify `chunks_summary` and `chunks`.
  - GET job without param → verify `chunks_summary` only and absence of `chunks` if optional.
- Update `openapi.yaml` to document `chunks_summary`, `chunks`, and the `include_chunks` query param.

Phase 4 — Local integration and verification (0.5–1h)
- Test end-to-end locally:
  - Create a job (`POST /jobs`) with audio that generates multiple chunks (use `dry_run` or real mode as appropriate).
  - Query `GET /jobs/{id}` with and without `include_chunks` (or check default inclusion) and verify counts and statuses as chunks move to `PROCESSING` and `COMPLETED`.
  - Verify local file paths and binaries are not exposed.

Phase 5 — Deployment and monitoring (0.5–1h)
- If relevant, deploy to staging.
- Verify response sizes and latency for jobs with many chunks.
- Add metrics/logging if desired (e.g., chunk count per job, per-chunk latency).

Decisions taken
- Decision: include `chunks` by default in the `GET /jobs/{id}` response (there will always be at least 1 chunk).
- Decision: expose `runpod_job_id` in each chunk's response.
- Decision: expose `started_at` and `completed_at` as RFC3339 strings; omit fields if empty.

Impact and compatibility
- Backwards compatible if `chunks` is added with `omitempty` and `chunks_summary` is a non-mandatory new field. Existing clients will still receive `id/status/progress`.
- Main risk: response size if there are many chunks. Mitigated by sending only metadata per chunk.



---
Date: 2025-12-03
