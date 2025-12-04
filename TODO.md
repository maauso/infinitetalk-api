
# Background processing: safer "fire-and-forget" sequence

The service currently starts background goroutines from HTTP handlers using
`context.WithoutCancel(r.Context())`. To make background processing robust,
observable and safe during shutdown, follow this sequence:

1. Create a server-level context (`serverCtx`) in `main` which is cancelled on
	shutdown (OS signal). Use this as the root for long-lived background work.

2. Replace `context.WithoutCancel(r.Context())` with a context derived from
	`serverCtx`. For per-job limits, derive with a timeout: e.g.
	`ctx, cancel := context.WithTimeout(serverCtx, MaxJobDuration)` and `defer cancel()`
	inside the goroutine.

3. Track background workers using a `sync.WaitGroup` or an `errgroup.Group`
	tied to `serverCtx`. This allows graceful shutdown to wait for in-flight
	jobs (with a global shutdown timeout).

4. Control concurrency with a semaphore/worker pool. Reuse existing
	`maxConcurrentChunks` concept at the service level so chunk submission and
	overall background job starts are bounded.

5. Add panic handling inside goroutines: `defer func(){ if r := recover(); r!=nil { log } }()`
	to avoid unexpected process exits and to log context (job_id).

6. Ensure all background operations accept `ctx` and return promptly on
	cancellation. `ProcessVideoService` already checks `ctx.Done()` in places
	(e.g. `processChunksParallel`) — keep this discipline across helpers.

7. Persist job state transitions immediately (STARTED, CHUNK statuses,
	COMPLETED/FAILED) so interrupted jobs can be resumed/retried or inspected.

8. Add unit and integration tests for:
	- launching background jobs and observing progress updates
	- graceful shutdown waiting for jobs with a timeout
	- concurrency limits (semaphore) under high load

9. Add observability: metrics (running jobs, queued jobs, chunk failures) and
	structured logs for lifecycle events (job started, job completed, job failed).

10. Optional: Implement a durable queue (Redis/DB) for jobs if you need
	 reliability across process restarts.

This sequence makes background job processing predictable, safe on shutdown,
and easier to debug in production.
