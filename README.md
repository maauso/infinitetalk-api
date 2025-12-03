# Infinitetalk API

> REST API for lip-sync video generation powered by RunPod.

```
┌──────────────────┐      ┌───────────────────┐      ┌───────────────────┐
│ Client (browser) │ ───▶ │  Infinitetalk API │ ───▶ │   RunPod Worker   │
└──────────────────┘      └───────────────────┘      └───────────────────┘
                                   │   ▲
                                   ▼   │
                          ┌─────────────────────┐
                          │  Local temp storage │
                          │  (audio chunks, mp4)│
                          └─────────────────────┘
```

Infinitetalk is a lightweight REST API that creates lip-synced videos. It accepts an image and a WAV audio file, splits the audio into chunks at silence boundaries, submits chunks in parallel to a RunPod worker, stitches the partial videos with `ffmpeg`, and returns or uploads the final video.

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

```bash
curl -X POST http://localhost:8080/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "image_base64": "<base64-encoded-image>",
    "audio_base64": "<base64-encoded-wav>",
    "width": 384,
    "height": 576,
    "push_to_s3": false,
    "dry_run": false
  }'
```

Response (`202 Accepted`):

```json
{
  "id": "job-1234567890-abc12345",
  "status": "IN_QUEUE"
}
```

**Dry-Run Mode:** Set `"dry_run": true` to execute preprocessing (decode, resize, split) without calling RunPod. Useful for testing and validation. The job completes immediately after audio splitting.

### Poll Job Status

```bash
curl http://localhost:8080/jobs/{id}
```

Response when completed:

```json
{
  "id": "job-1234567890-abc12345",
  "status": "COMPLETED",
  "progress": 100,
  "video_base64": "<base64-encoded-mp4>"
}
```

If `push_to_s3` was `true`, the response contains `video_url` instead.

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
3. **Extract segments** — Use `ffmpeg -ss START -to END -c copy` to extract each chunk.

**Parameters:**

| Parameter | Default | Description |
|-----------|---------|-------------|
| `CHUNK_TARGET_SEC` | `45` | Target chunk length |
| Silence threshold | `-40 dB` | Amplitude below which audio is considered silent |
| Min silence duration | `500 ms` | Minimum silence length to consider as a cut point |

This approach minimizes audible artifacts by avoiding cuts in the middle of speech.

## Repository Layout

```
internal/
├── audio/     # Silence-based audio splitting
├── job/       # Domain, repository, use cases
├── media/     # Video/image operations (ffmpeg)
├── runpod/    # RunPod HTTP client
├── server/    # HTTP handlers and middlewares
└── storage/   # Temp storage and S3
api/
└── openapi.yaml   # OpenAPI 3.0 specification
```

## License

MIT — see [LICENSE](LICENSE).

## Contributing

Issues and PRs are welcome. Please include tests for new functionality.
