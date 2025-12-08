#!/usr/bin/env python3
"""
Infinitetalk API Client
Simple client to interact with the Infinitetalk API for lip-sync video generation.
Supports both RunPod and Beam providers.
"""

import os
import sys
import time
import json
import base64
import argparse
import requests
from tqdm import tqdm
from dotenv import load_dotenv

# Load environment variables from .env
load_dotenv()

# Configuration
API_URL = os.getenv("INFINITETALK_API_URL", "http://localhost:8080")

def file_to_base64(path):
    """Convert a file to base64 encoding."""
    with open(path, "rb") as f:
        return base64.b64encode(f.read()).decode("utf-8")

def create_job(api_url, image_b64, audio_b64, width, height, provider, push_to_s3, dry_run):
    """Create a new job via the API."""
    url = f"{api_url}/jobs"

    payload = {
        "image_base64": image_b64,
        "audio_base64": audio_b64,
        "width": width,
        "height": height,
        "provider": provider,
        "push_to_s3": push_to_s3,
        "dry_run": dry_run
    }

    headers = {
        "Content-Type": "application/json"
    }

    try:
        response = requests.post(url, json=payload, headers=headers, timeout=30)
        response.raise_for_status()
        return response.json()
    except requests.exceptions.RequestException as e:
        print(f"❌ Error creating job: {e}")
        if hasattr(e, 'response') and e.response is not None:
            print(f"Response: {e.response.text}")
        sys.exit(1)

def get_job_status(api_url, job_id):
    """Get the status of a job."""
    url = f"{api_url}/jobs/{job_id}"

    try:
        response = requests.get(url, timeout=10)
        response.raise_for_status()
        return response.json()
    except requests.exceptions.RequestException as e:
        print(f"❌ Error getting job status: {e}")
        sys.exit(1)

def poll_job_until_complete(api_url, job_id, poll_interval=5, timeout=600):
    """Poll the job status until it's complete or failed."""
    print("⏳ Waiting for job completion...")

    start_time = time.time()
    pbar = tqdm(total=100, bar_format="{l_bar}{bar}| {n_fmt}% [{elapsed}]")

    last_progress = 0
    last_status = ""

    while True:
        # Check timeout
        elapsed = time.time() - start_time
        if elapsed > timeout:
            pbar.close()
            print(f"⏱️ Timeout: Job did not complete within {timeout} seconds")
            sys.exit(1)

        # Get job status
        job_info = get_job_status(api_url, job_id)
        status = job_info.get("status", "UNKNOWN")
        progress = job_info.get("progress", 0)
        error = job_info.get("error", "")

        # Update progress bar
        if progress > last_progress:
            pbar.update(progress - last_progress)
            last_progress = progress

        # Update status description if changed
        if status != last_status:
            pbar.set_description(f"Status: {status}")
            last_status = status

        # Check terminal states
        if status == "COMPLETED":
            pbar.n = 100
            pbar.refresh()
            pbar.close()
            print("🎉 Job Completed!")
            return job_info

        if status in ["FAILED", "CANCELLED", "TIMED_OUT"]:
            pbar.close()
            print(f"❌ Job {status}")
            if error:
                print(f"Error: {error}")
            sys.exit(1)

        # Wait before next poll
        time.sleep(poll_interval)

def save_video_output(job_info, output_path):
    """Save the video output from job info."""
    # Check for S3 URL
    video_url = job_info.get("video_url")
    if video_url:
        print(f"📥 Downloading video from S3: {video_url}")
        try:
            response = requests.get(video_url, stream=True, timeout=60)
            response.raise_for_status()

            total_size = int(response.headers.get('content-length', 0))

            with open(output_path, 'wb') as f, tqdm(
                desc=output_path,
                total=total_size,
                unit='iB',
                unit_scale=True,
                unit_divisor=1024,
            ) as bar:
                for chunk in response.iter_content(chunk_size=8192):
                    size = f.write(chunk)
                    bar.update(size)

            print(f"✅ Video saved to {output_path}")
            return
        except Exception as e:
            print(f"❌ Error downloading from S3: {e}")
            sys.exit(1)

    # Check for base64 video
    video_b64 = job_info.get("video_base64")
    if video_b64:
        print(f"💾 Decoding base64 video...")
        try:
            video_data = base64.b64decode(video_b64)
            with open(output_path, 'wb') as f:
                f.write(video_data)
            print(f"✅ Video saved to {output_path}")
            return
        except Exception as e:
            print(f"❌ Error decoding video: {e}")
            sys.exit(1)

    print("⚠️ No video output found in job result")
    sys.exit(1)

def main():
    parser = argparse.ArgumentParser(
        description="Infinitetalk API Client - Lip-sync video generation",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Using RunPod (default)
  %(prog)s -i photo.jpg -a audio.wav -o output.mp4

  # Using Beam provider
  %(prog)s -i photo.jpg -a audio.wav -o output.mp4 --provider beam

  # Custom dimensions
  %(prog)s -i photo.jpg -a audio.wav -w 512 -H 512 -o output.mp4

  # Push to S3 instead of returning base64
  %(prog)s -i photo.jpg -a audio.wav -o output.mp4 --push-to-s3

  # Dry run (preprocessing only, no video generation)
  %(prog)s -i photo.jpg -a audio.wav --dry-run
        """
    )

    # Required arguments
    parser.add_argument("-i", "--image", required=True, help="Input image file path")
    parser.add_argument("-a", "--audio", required=True, help="Input audio file path (WAV, MP3, etc.)")
    parser.add_argument("-o", "--output", default="output.mp4", help="Output video filename (default: output.mp4)")

    # Optional arguments
    parser.add_argument("-w", "--width", type=int, default=384, help="Video width in pixels (default: 384)")
    parser.add_argument("-H", "--height", type=int, default=384, help="Video height in pixels (default: 384)")
    parser.add_argument("--provider", choices=["runpod", "beam"], default="runpod",
                        help="Video generation provider (default: runpod)")
    parser.add_argument("--push-to-s3", action="store_true",
                        help="Upload result to S3 instead of returning base64")
    parser.add_argument("--dry-run", action="store_true",
                        help="Preprocessing only, skip video generation")

    # API configuration
    parser.add_argument("--api-url", default=API_URL,
                        help=f"Infinitetalk API URL (default: {API_URL})")
    parser.add_argument("--poll-interval", type=int, default=5,
                        help="Status polling interval in seconds (default: 5)")
    parser.add_argument("--timeout", type=int, default=3600,
                        help="Job timeout in seconds (default: 3600)")

    args = parser.parse_args()

    # Validate input files exist
    if not os.path.isfile(args.image):
        print(f"❌ Error: Image file not found: {args.image}")
        sys.exit(1)

    if not os.path.isfile(args.audio):
        print(f"❌ Error: Audio file not found: {args.audio}")
        sys.exit(1)

    # Convert files to base64
    print(f"📷 Reading image: {args.image}")
    image_b64 = file_to_base64(args.image)

    print(f"🔊 Reading audio: {args.audio}")
    audio_b64 = file_to_base64(args.audio)

    # Create job
    print(f"\n🚀 Creating job (provider: {args.provider}, {args.width}x{args.height})...")
    job_response = create_job(
        args.api_url,
        image_b64,
        audio_b64,
        args.width,
        args.height,
        args.provider,
        args.push_to_s3,
        args.dry_run
    )

    job_id = job_response.get("id")
    initial_status = job_response.get("status")

    print(f"✅ Job created: {job_id}")
    print(f"   Initial status: {initial_status}")

    # Poll until complete
    job_info = poll_job_until_complete(
        args.api_url,
        job_id,
        poll_interval=args.poll_interval,
        timeout=args.timeout
    )

    # Save output video
    if not args.dry_run:
        save_video_output(job_info, args.output)
    else:
        print("🏁 Dry run completed (no video generated)")

if __name__ == "__main__":
    main()
