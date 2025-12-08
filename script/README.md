# Infinitetalk API Clients

This directory contains client scripts to interact with the Infinitetalk API and provider services.

## Available Clients

### 1. `api_client.py` - Main Infinitetalk API Client

Python client for the Infinitetalk REST API. Handles file conversion, job creation, status polling, and video output retrieval.

#### Installation

```bash
pip install -r requirements.txt
```

#### Usage

**Basic usage with RunPod (default):**
```bash
./api_client.py -i photo.jpg -a audio.wav -o output.mp4
```

**Using Beam provider:**
```bash
./api_client.py -i photo.jpg -a audio.wav -o output.mp4 --provider beam
```

**Custom dimensions:**
```bash
./api_client.py -i photo.jpg -a audio.wav -w 384 -H 384 -o result.mp4
```

**Push to S3:**
```bash
./api_client.py -i photo.jpg -a audio.wav -o output.mp4 --push-to-s3
```

**Dry run (preprocessing only):**
```bash
./api_client.py -i photo.jpg -a audio.wav --dry-run
```

**Remote API:**
```bash
./api_client.py -i photo.jpg -a audio.wav --api-url https://api.example.com
```

#### Arguments

| Argument | Required | Default | Description |
|----------|----------|---------|-------------|
| `-i`, `--image` | Yes | — | Input image file path |
| `-a`, `--audio` | Yes | — | Input audio file path (WAV, MP3, etc.) |
| `-o`, `--output` | No | `output.mp4` | Output video filename |
| `-w`, `--width` | No | `384` | Video width in pixels |
| `-H`, `--height` | No | `384` | Video height in pixels |
| `--provider` | No | `runpod` | Provider: `runpod` or `beam` |
| `--push-to-s3` | No | `false` | Upload to S3 instead of base64 |
| `--dry-run` | No | `false` | Preprocessing only, skip generation |
| `--api-url` | No | `http://localhost:8080` | Infinitetalk API URL |
| `--poll-interval` | No | `5` | Status polling interval (seconds) |
| `--timeout` | No | `3600` | Job timeout (seconds) |

#### Environment Variables

```bash
# Optional: Set default API URL
export INFINITETALK_API_URL=http://localhost:8080
```

#### Features

- ✅ Automatic file to base64 conversion
- ✅ Job creation and submission
- ✅ Polling with progress bar (0-100%)
- ✅ Automatic output handling (base64 or S3)
- ✅ Support for both RunPod and Beam providers
- ✅ Configurable timeouts and polling intervals
- ✅ Dry-run mode for testing

#### Workflow

1. **Read files** - Loads image and audio from disk
2. **Convert to base64** - Encodes files for API transmission
3. **Create job** - POST to `/jobs` endpoint
4. **Poll status** - GET `/jobs/{id}` until completion
5. **Save output** - Downloads from S3 URL or decodes base64 to file

---

### 2. `beam_client.py` - Direct Beam.cloud Client

Low-level Python client for direct interaction with Beam.cloud Task Queue API. Useful for testing Beam integration without the Infinitetalk API layer.

#### Installation

```bash
pip install -r requirements.txt
```

#### Configuration

Set your Beam token:
```bash
export BEAM_TOKEN=your-beam-token-here
```

Or create a `.env` file in the project root:
```bash
BEAM_TOKEN=your-beam-token-here
```

#### Usage

```bash
./beam_client.py --url https://api.beam.cloud/v1/task_queue/YOUR_ID/tasks \
  -i photo.jpg \
  -a audio.wav \
  -w 384 \
  -H 540 \
  -o output.mp4
```

#### Arguments

| Argument | Required | Default | Description |
|----------|----------|---------|-------------|
| `--url` | Yes | — | Beam task queue webhook URL |
| `-i`, `--image` | Yes | — | Input image (path or URL) |
| `-a`, `--audio` | Yes | — | Input audio (path or URL) |
| `-p`, `--prompt` | No | `"A person talking naturally"` | Prompt text |
| `-w`, `--width` | No | `384` | Video width |
| `-H`, `--height` | No | `540` | Video height |
| `-o`, `--output` | No | `output.mp4` | Output filename |
| `--force-offload` | No | — | Enable force offload |
| `--no-force-offload` | No | — | Disable force offload |

#### Features

- ✅ Direct Beam.cloud API interaction
- ✅ Support for file paths or URLs
- ✅ Task submission and status polling
- ✅ Video download from Beam output URL
- ✅ Progress bars for polling and download
- ✅ Configurable force offload option

---

## Requirements

Both clients require:
- Python 3.7+
- Dependencies listed in `requirements.txt`

Install with:
```bash
pip install -r requirements.txt
```

---

## Comparison

| Feature | `api_client.py` | `beam_client.py` |
|---------|-----------------|------------------|
| **Target** | Infinitetalk API | Beam.cloud API |
| **Provider support** | RunPod + Beam | Beam only |
| **Audio splitting** | Yes (via API) | No (single task) |
| **Video stitching** | Yes (via API) | No (single output) |
| **S3 upload** | Yes (via API) | No |
| **Progress tracking** | Job progress (0-100%) | Task status only |
| **Use case** | Production API usage | Testing Beam integration |

---

## Examples

### Example 1: Generate video with RunPod
```bash
./api_client.py -i examples/face.jpg -a examples/speech.wav -o result_runpod.mp4
```

### Example 2: Generate video with Beam
```bash
./api_client.py -i examples/face.jpg -a examples/speech.wav -o result_beam.mp4 --provider beam
```

### Example 3: Test Beam directly
```bash
export BEAM_TOKEN=your-token
./beam_client.py \
  --url https://api.beam.cloud/v1/task_queue/YOUR_ID/tasks \
  -i examples/face.jpg \
  -a examples/speech.wav \
  -o direct_beam.mp4
```

### Example 4: Dry run to test preprocessing
```bash
./api_client.py -i examples/face.jpg -a examples/speech.wav --dry-run
```

---

## Troubleshooting

### API Connection Error
```
❌ Error creating job: Connection refused
```
**Solution:** Ensure the Infinitetalk API is running on the specified URL.

### Beam Token Not Set
```
❌ Error: BEAM_TOKEN environment variable not set
```
**Solution:** Export the token or add it to `.env`:
```bash
export BEAM_TOKEN=your-beam-token-here
```

### File Not Found
```
❌ Error: Image file not found: photo.jpg
```
**Solution:** Check the file path is correct and the file exists.

### Job Timeout
```
⏱️ Timeout: Job did not complete within 3600 seconds
```
**Solution:** Increase timeout with `--timeout 1200` or check job status manually.

---

## API Reference

For full API documentation, see the main [README.md](../README.md) or the OpenAPI specification at `api/openapi.yaml`.
