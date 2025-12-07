Beam Integration Plan (proposal)

Goal: Add Beam as an alternative video-generation provider alongside RunPod, selectable per job, keeping existing audio-splitting, stitching, and storage flows.

Reference: current Beam client script at `script/beam_client.py` (end-to-end example of submit + poll + download).

Phases

1) Discover & config surface
- Deliverables: config keys documented (`BEAM_TOKEN`, `BEAM_QUEUE_URL` task queue webhook, optional `BEAM_POLL_INTERVAL_SEC`, `BEAM_POLL_TIMEOUT_SEC`), provider flag added to job payload (`provider: "runpod" | "beam"`, default `runpod`).
- Notes: map Beam task statuses to domain (`PENDING/RUNNING/COMPLETED/FAILED/CANCELED`).

2) Domain updates (ports/types)
- Deliverables: job entity carries provider; validation for allowed providers; repository persistence updated; service factory signature accepts provider. Interface/port for video generators generalized (e.g., `Generator` with submit/poll/download methods) so Beam and RunPod adapters plug in without branching logic everywhere.

3) Beam adapter (infrastructure)
- Deliverables: new `internal/beam` package implementing generator port. Implements submit (POST queue webhook with image/audio base64/URL, prompt, width/height, `force_offload` flag), poll (`GET /v2/task/{id}`), and artifact fetch (download first output URL to temp file). Include request/response structs, stderr-rich errors, context timeouts, and unit tests with HTTP test server.
- Edge cases: missing outputs, empty `url`, retries on transient HTTP 5xx, honor poll interval/timeout envs.

4) Orchestration wiring (use case)
- Deliverables: processing service chooses adapter based on job.provider. Beam path still uses existing pipeline: image prep + audio split → submit each chunk sequentially (no parallel submits) → poll each task (same poll interval as RunPod) → stitch outputs. Keep concurrency limits for RunPod; Beam uses serial flow. Progress mapping updated to reflect Beam task states.

5) HTTP surface & docs
- Deliverables: request DTO updated with `provider` flag; OpenAPI/spec and README updated; server validation returns 400 for unsupported providers; default remains RunPod. Add example curl for Beam.

6) Storage & cleanup
- Deliverables: download Beam output to temp storage (reuse storage adapter), ensure file naming per chunk for stitching, cleanup on failure/cancel, support S3 upload path unchanged.

7) Testing & observability
- Deliverables: unit tests for Beam adapter (submit/poll/output parsing/error paths), service tests with Beam mock, handler test covering provider flag. Optional: structured logs include provider and task_id for correlation.

Open questions
- None pending (per-chunk sequential confirmed; poll interval aligns with RunPod).
