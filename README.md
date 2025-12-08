# Infinitetalk API

> REST API for lip-sync video generation powered by RunPod and Beam.

```
┌──────────────────┐      ┌───────────────────┐      ┌───────────────────┐
│ Client (browser) │ ───▶ │  Infinitetalk API │ ───▶ │ RunPod / Beam     │
└──────────────────┘      └───────────────────┘      └───────────────────┘
                                   │   ▲
                                   ▼   │
                          ┌─────────────────────┐
                          │  Local temp storage │
                          │  (audio chunks, mp4)│
                          └─────────────────────┘
```

Infinitetalk is a lightweight REST API that creates lip-synced videos. It accepts an image and a WAV audio file, splits the audio into chunks at silence boundaries, submits chunks to a RunPod or Beam worker, stitches the partial videos with `ffmpeg`, and returns or uploads the final video.

## Features

- **Silence-based audio splitting** — cuts audio at natural pauses to avoid artifacts.
- **Parallel chunk processing** — configurable concurrency for faster throughput.
- **Video stitching** — concatenates partial videos using `ffmpeg` (stream-copy first, re-encode fallback).
- **Optional S3 upload** — return video inline or push to S3.

## Requirements

- Go 1.25+
- `ffmpeg` (bundled in the provided `Dockerfile`)

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `PORT` | No | `8080` | HTTP server port |
| `RUNPOD_API_KEY` | **Yes** | — | RunPod API key |
| `RUNPOD_ENDPOINT_ID` | **Yes** | — | RunPod endpoint ID |
| `BEAM_TOKEN` | No | — | Beam.cloud API token (optional) |
| `BEAM_QUEUE_URL` | No | — | Beam task queue webhook URL (optional) |
| `BEAM_POLL_INTERVAL_MS` | No | `5000` | Beam status poll interval (ms) |
| `BEAM_POLL_TIMEOUT_SEC` | No | `600` | Beam task timeout (seconds) |
| `TEMP_DIR` | No | `/tmp/infinitetalk` | Directory for temporary files |
| `MAX_CONCURRENT_CHUNKS` | No | `3` | Max parallel RunPod submissions |
| `CHUNK_TARGET_SEC` | No | `45` | Target chunk duration (seconds) |
| `S3_BUCKET` | No | — | S3 bucket for video upload |
| `S3_REGION` | No | — | AWS region |
| `AWS_ACCESS_KEY_ID` | No | — | AWS credentials |
| `AWS_SECRET_ACCESS_KEY` | No | — | AWS credentials |

## Build & Run

### Local

```bash
go build -o infinitetalk ./
PORT=8080 RUNPOD_API_KEY=xxx RUNPOD_ENDPOINT_ID=yyy ./infinitetalk
```

### Docker

```bash
docker build -t infinitetalk:latest .
docker run --rm -p 8080:8080 \
  -e RUNPOD_API_KEY=... \
  -e RUNPOD_ENDPOINT_ID=... \
  infinitetalk:latest
```

### Tests

```bash
go test ./...
```

## API Usage

### Create a Job

#### Using RunPod (default)

```bash
curl -X POST http://localhost:8080/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "image_base64": "<base64-encoded-image>",
    "audio_base64": "<base64-encoded-wav>",
    "width": 384,
    "height": 576,
    "provider": "runpod",
    "push_to_s3": false,
    "dry_run": false
  }'
```

#### Using Beam

```bash
curl -X POST http://localhost:8080/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "image_base64": "<base64-encoded-image>",
    "audio_base64": "<base64-encoded-wav>",
    "width": 384,
    "height": 540,
    "provider": "beam",
    "push_to_s3": false,
    "dry_run": false
  }'
```

**Note:** The `provider` field is optional and defaults to `"runpod"`. Valid values are `"runpod"` or `"beam"`.

**Note:** Beam integration is currently configured at the API level but full orchestration support in `ProcessVideoService` is pending. Jobs with `provider: "beam"` will be rejected with a helpful error message.

Response (`202 Accepted`):

```json
{
  "id": "job-1234567890-abc12345",
  "status": "IN_QUEUE"
}
```

**Dry-Run Mode:** Set `"dry_run": true` to execute preprocessing (decode, resize, split) without calling the provider. Useful for testing and validation. The job completes immediately after audio splitting.

### Poll Job Status

```bash
curl http://localhost:8080/jobs/{id}
```

Response when completed:

```json
{
  "id": "job-1234567890-abc12345",
  "provider": "runpod",
  "status": "COMPLETED",
  "progress": 100,
  "video_base64": "<base64-encoded-mp4>"
}
```

If `push_to_s3` was `true`, the response contains `video_url` instead.

### Delete Job Video

Delete the local video file for a completed job. This endpoint is idempotent — it returns success even if the file is already missing.

```bash
curl -X POST http://localhost:8080/jobs/{id}/video/delete
```

Response: `204 No Content` on success.

If the job does not exist, returns `404 Not Found`:

```json
{
  "error": "job not found",
  "code": "JOB_NOT_FOUND"
}
```

### Health Check

```bash
curl http://localhost:8080/health
```

## Converting Files to Base64

### Linux

```bash
base64 -w 0 input.wav > input.wav.b64
```

### macOS

```bash
base64 -i input.wav | tr -d '\n' > input.wav.b64
```

### Python

```bash
python3 -c "import base64; print(base64.b64encode(open('input.wav','rb').read()).decode())"
```

### Node.js

```bash
node -e "console.log(require('fs').readFileSync('input.wav').toString('base64'))"
```

## How Silence-Based Audio Splitting Works

Long audio files are split into smaller chunks for efficient processing. The splitter uses `ffmpeg`'s `silencedetect` filter to find natural pause points.

**Pipeline:**

1. **Detect silences** — Run `ffmpeg -af silencedetect=noise=-40dB:d=0.5 -f null -` and parse output for silence intervals.
2. **Plan cuts** — Select cut points near the target chunk duration (default 45s), preferring silence boundaries.
3. **Extract segments** — Re-encode each segment to WAV PCM (`pcm_s16le`) format for maximum compatibility.
4. **Validate chunks** — Use `ffprobe` to verify each chunk has correct format, codec, and duration.

**Parameters:**

| Parameter | Default | Description |
|-----------|---------|-------------|
| `CHUNK_TARGET_SEC` | `45` | Target chunk length |
| Silence threshold | `-40 dB` | Amplitude below which audio is considered silent |
| Min silence duration | `500 ms` | Minimum silence length to consider as a cut point |

This approach minimizes audible artifacts by avoiding cuts in the middle of speech.

## Audio Format Requirements

The API exclusively uses **WAV PCM (pcm_s16le)** format for audio chunks to ensure maximum compatibility with RunPod workers (PyAV/librosa).

- All audio chunks are re-encoded to `pcm_s16le` during splitting.
- If encoding fails, the system automatically retries with normalized settings (16kHz mono).
- Each chunk is validated with `ffprobe` to ensure:
  - Format: `wav`
  - Codec: `pcm_s16le`
  - Duration: > 0 seconds

This ensures that RunPod/ComfyUI can decode audio without "Invalid data found when processing input" errors.

## Repository Layout

```
internal/
├── audio/      # Silence-based audio splitting
├── beam/       # Beam.cloud HTTP client
├── generator/  # Common interface for video generation providers
├── job/        # Domain, repository, use cases
├── media/      # Video/image operations (ffmpeg)
├── runpod/     # RunPod HTTP client
├── server/     # HTTP handlers and middlewares
└── storage/    # Temp storage and S3
api/
└── openapi.yaml   # OpenAPI 3.0 specification
```

## Provider Support

### RunPod
- **Status:** ✅ Fully supported
- **Configuration:** `RUNPOD_API_KEY`, `RUNPOD_ENDPOINT_ID`
- **Features:** Parallel chunk processing, base64 video response

### Beam
- **Status:** 🚧 Partially implemented
- **Configuration:** `BEAM_TOKEN`, `BEAM_QUEUE_URL`
- **Implementation:** Client, API types, and Generator adapter are complete
- **Pending:** Full orchestration integration in `ProcessVideoService`
- **Current Behavior:** Jobs with `provider: "beam"` will return an error indicating the feature is not yet fully integrated

## CI/CD and Releases

This repository uses GitHub Actions for continuous integration and deployment.

### Automated Releases

When a PR is merged to `main`, the release workflow automatically:
1. Determines the next semantic version by incrementing the patch number (e.g., `v0.1.0` → `v0.1.1`)
2. Creates a new GitHub Release with the version tag
3. Builds a Docker image and pushes it to GitHub Container Registry (ghcr.io)
4. Tags the Docker image with both the version number and `latest`

### Docker Images

Docker images are available at:
```
ghcr.io/maauso/infinitetalk-api:latest
ghcr.io/maauso/infinitetalk-api:<version>
```

Pull the latest image:
```bash
docker pull ghcr.io/maauso/infinitetalk-api:latest
```

Pull a specific version:
```bash
docker pull ghcr.io/maauso/infinitetalk-api:0.1.0
```

### GitHub Secrets

The release workflow uses the following secrets (automatically provided by GitHub):
- `GITHUB_TOKEN` - Used for creating releases and pushing to GitHub Container Registry

No additional secrets configuration is required. The workflow uses GitHub's built-in authentication.

### Manual Version Control

If you need to create a major or minor version bump instead of a patch:
1. Create and push a tag manually:
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```
2. The next automated release will increment from that version

## License

MIT — see [LICENSE](LICENSE).

## Contributing

Issues and PRs are welcome. Please include tests for new functionality.
