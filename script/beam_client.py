#!/usr/bin/env python3
"""
Beam.cloud Task Queue Client for InfiniteTalk
Submits jobs asynchronously and polls for completion.
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
BEAM_TOKEN = os.getenv("BEAM_TOKEN")
if not BEAM_TOKEN:
    print("❌ Error: BEAM_TOKEN environment variable not set")
    print("Configure it in .env file or set: export BEAM_TOKEN='your-token-here'")
    sys.exit(1)

# The queue URL is usually standard, but we need the specific mapping ID.
# For now we'll assume the user will provide it or we'll find it after deployment.
# It typically looks like: https://api.beam.cloud/v1/task_queue/<ID>/tasks
APP_NAME = "infinitetalk-queue"

def file_to_base64(path):
    with open(path, "rb") as f:
        return base64.b64encode(f.read()).decode("utf-8")

# (Removed dead code: get_queue_url_from_name)

def submit_task(queue_url, token, payload):
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json"
    }

    # Beam Task Queue API expects the payload directly
    response = requests.post(queue_url, json=payload, headers=headers)

    try:
        response.raise_for_status()
        return response.json()["task_id"]
    except Exception as e:
        print(f"❌ Error submitting task: {e}")
        print(f"Response: {response.text}")
        sys.exit(1)

def poll_task(task_id, token, retries=5):
    # Correct URL: https://api.beam.cloud/v2/task/{TASK_ID}/
    url = f"https://api.beam.cloud/v2/task/{task_id}/"
    headers = {"Authorization": f"Bearer {token}"}

    for attempt in range(retries):
        try:
            response = requests.get(url, headers=headers)
            response.raise_for_status()
            return response.json()
        except Exception as e:
            if attempt < retries - 1:
                time.sleep(2)
                continue
            raise e

def main():
    parser = argparse.ArgumentParser(description="InfiniteTalk Task Queue Client")
    parser.add_argument("--url", required=True, help="Task Queue Webhook URL")
    parser.add_argument("-i", "--image", required=True, help="Input image (path or URL)")
    parser.add_argument("-a", "--audio", required=True, help="Input audio (path or URL)")
    parser.add_argument("-p", "--prompt", default="A person talking naturally", help="Prompt text")
    parser.add_argument("-w", "--width", type=int, default=384, help="Width")
    parser.add_argument("-H", "--height", type=int, default=540, help="Height")
    parser.add_argument("-o", "--output", default="output.mp4", help="Output filename")
    parser.add_argument("--force-offload", action="store_true", default=None, help="Enable force offload (default: True in handler)")
    parser.add_argument("--no-force-offload", action="store_false", dest="force_offload", help="Disable force offload")

    args = parser.parse_args()

    # Prepare payload
    payload = {
        "prompt": args.prompt,
        "width": args.width,
        "height": args.height
    }

    if args.force_offload is not None:
        payload["force_offload"] = args.force_offload

    # Process Image
    if args.image.startswith("http"):
        payload["image_url"] = args.image
        print(f"📷 Image URL: {args.image}")
    else:
        print(f"📷 Image File: {args.image}")
        payload["image_base64"] = file_to_base64(args.image)

    # Process Audio
    if args.audio.startswith("http"):
        payload["wav_url"] = args.audio
        print(f"🔊 Audio URL: {args.audio}")
    else:
        print(f"🔊 Audio File: {args.audio}")
        payload["wav_base64"] = file_to_base64(args.audio)

    print(f"🚀 Submitting task to {args.url}...")
    task_id = submit_task(args.url, BEAM_TOKEN, payload)
    print(f"✅ Task ID: {task_id}")

    # Poll loop
    print("⏳ Waiting for completion...")
    pbar = tqdm(total=100, bar_format="{l_bar}{bar}| {n_fmt}/{total_fmt} [{elapsed}]")

    status = "PENDING"
    last_status = ""

    while status not in ["COMPLETED", "COMPLETE", "FAILED", "CANCELED"]:
        info = poll_task(task_id, BEAM_TOKEN)
        status = info["status"]

        # Update progress bar description
        if status != last_status:
            pbar.set_description(f"Status: {status}")
            last_status = status

        if status == "RUNNING":
             # Fake progress increment for visual feedback
             pbar.update(1)
             if pbar.n >= 99: pbar.n = 99

        if status in ["COMPLETED", "COMPLETE", "FAILED", "CANCELED"]:
            break

        time.sleep(5)

    pbar.close()

    if status in ["COMPLETED", "COMPLETE"]:
        print("🎉 Task Completed!")

        # Beam returns a list of outputs
        # "outputs": [ { "name": "output.mp4", "url": "...", ... } ]
        outputs = info.get("outputs", [])

        if not outputs:
             print("⚠️ No output files found in task info.")
             print(json.dumps(info, indent=2))
             sys.exit(1)

        # Find the video file (or just take the first one)
        video_url = outputs[0].get("url")
        if not video_url:
             print("⚠️ Output URL not found.")
             print("Outputs:", json.dumps(outputs, indent=2))
             sys.exit(1)

        print(f"📥 Downloading video from: {video_url}")

        # Download file
        with requests.get(video_url, stream=True) as r:
            r.raise_for_status()
            total_size = int(r.headers.get('content-length', 0))

            with open(args.output, 'wb') as f, tqdm(
                desc=args.output,
                total=total_size,
                unit='iB',
                unit_scale=True,
                unit_divisor=1024,
            ) as bar:
                for chunk in r.iter_content(chunk_size=8192):
                    size = f.write(chunk)
                    bar.update(size)

        print(f"✅ Video saved to {args.output}")

    else:
        print(f"❌ Task Failed with status: {status}")
        # Print logs if available? (Requires separate API call usually)

if __name__ == "__main__":
    main()
