# Delete Local Video Feature

Goal: Add an endpoint to delete the final video file generated for a Job. This feature only targets local files (no S3). The chosen route is

        POST /jobs/{id}/video/delete

Design constraints (explicit):
- Only local video deletion. S3 support is removed for this feature.
- Return `404 Not Found` only if the Job does not exist.
- No authentication/authorization in this phase.
- Deletion should be idempotent (return success even if file already missing).

Phases
------

Phase 1 — Service
- Add `DeleteJobVideo(ctx context.Context, jobID string) error` to `internal/job/service.go`.
- Behavior:
    - Load the `Job` with `repo.FindByID`.
    - If job not found, return a not-found error (service-level sentinel or wrapped error).
    - If `job.OutputPath` is non-empty, attempt `os.Remove(job.OutputPath)`.
        - If `os.IsNotExist` treat as success.
        - On other filesystem errors return an internal error.
    - Clear job output metadata (e.g., `job.SetOutput("", "")` or `job.ClearOutput()` helper) and persist with `repo.Save`.
- Acceptance criteria:
    - When video file exists: service removes the file, clears job fields, saves job, returns nil.
    - When video file missing: service clears job fields, saves job, returns nil (idempotent).
    - When job not found: service returns an error that maps to HTTP 404.

Phase 2 — HTTP Handler & Route
- Add `(*Handlers).DeleteJobVideo(w http.ResponseWriter, r *http.Request)` to `internal/server/handlers.go`.
- Register route in `internal/server/routes.go` as `POST /jobs/{id}/video/delete`.
- Handler behavior:
    - Parse `id` from URL.
    - Call `service.DeleteJobVideo(ctx, id)`.
    - Map errors:
        - Job not found -> `404`.
        - Success -> `204 No Content` (no body).
        - Internal errors -> `500` with structured JSON `{ "error": "message" }`.
- Acceptance criteria:
    - Endpoint returns `204` on successful deletion or if file already missing.
    - Endpoint returns `404` when the job does not exist.
    - Route is discoverable and covered by an `httptest` verifying status codes.

Phase 3 — Job model helper
- Add a small helper to `internal/job/job.go` like `ClearOutput()` or reuse `SetOutput("", "")` to centralize clearing output metadata.
- Acceptance criteria:
    - `Job` exposes a clear method and service uses it; unit tests assert fields were cleared.

Phase 4 — Tests, docs & cleanup
- Add unit tests for `DeleteJobVideo` using the in-memory repository or mocked repo and creating a real temp file to ensure deletion.
- Add handler tests (`httptest`) for:
    - successful deletion when file exists,
    - successful deletion when file missing (idempotent),
    - `404` when job not found.
- Update `DeleteFeature.md` with this plan (this file) and add a short note to `README.md` describing the new endpoint.
- Acceptance criteria:
    - Tests cover the three scenarios above and pass locally.
    - Documentation updated with the route and expected responses.

Rollout & Further considerations
-------------------------------
- Concurrency: this simple implementation does not coordinate with running jobs. For production, consider rejecting deletes for jobs in non-terminal states or implement a lock.
- Permissions: no auth is implemented in this phase — plan a follow-up for authorization.
- Idempotency: kept — API returns success even when resource missing, per request.
- Observability: log attempts, removed path, and job_id at info level.

Implementation checklist (completed)
------------------------------------
- [x] Add `ClearOutput()` helper to `Job` model in `internal/job/job.go`
- [x] Add unit test for `ClearOutput()` in `internal/job/job_test.go`
- [x] Add `DeleteJobVideo` service method in `internal/job/service.go`
- [x] Add unit tests for `DeleteJobVideo` in `internal/job/service_test.go`
- [x] Add `DeleteJobVideo` HTTP handler in `internal/server/handlers.go`
- [x] Register route `POST /jobs/{id}/video/delete` in `internal/server/routes.go`
- [x] Add handler tests in `internal/server/handlers_test.go`
- [x] Update README.md with new endpoint documentation
- [x] Create DeleteFeature.md documentation
