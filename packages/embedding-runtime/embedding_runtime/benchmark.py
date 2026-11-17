"""Opt-in real-weight acceptance and measurements. Never runs in normal CI."""
import argparse
import asyncio
import base64
import io
import json
import math
from pathlib import Path
import struct
import subprocess
import tempfile
import time
import wave

from .service import Config, Supervisor


def fixtures(directory):
    from PIL import Image
    data = {}
    for color in ("red", "blue"):
        for fmt, mime in (("PNG", "image/png"), ("JPEG", "image/jpeg"), ("WEBP", "image/webp")):
            output = io.BytesIO()
            Image.new("RGB", (128, 128), color).save(output, format=fmt)
            data[f"{color}-{fmt}"] = {"inlineData": {"mimeType": mime, "data": base64.b64encode(output.getvalue()).decode()}}
    path = Path(directory) / "tone.wav"
    with wave.open(str(path), "wb") as output:
        output.setnchannels(1)
        output.setsampwidth(2)
        output.setframerate(16000)
        output.writeframes(b"".join(struct.pack("<h", int(12000 * math.sin(2 * math.pi * 440 * i / 16000))) for i in range(16000)))
    for extension, mime in (("wav", "audio/wav"), ("mp3", "audio/mpeg"), ("flac", "audio/flac"), ("ogg", "audio/ogg")):
        target = Path(directory) / f"tone.{extension}"
        if extension != "wav":
            subprocess.run(["ffmpeg", "-v", "error", "-nostdin", "-y", "-i", str(path), str(target)], check=True)
        data[extension] = {"inlineData": {"mimeType": mime, "data": base64.b64encode(target.read_bytes()).decode()}}
    for extension, mime, codec in (("mp4", "video/mp4", "mpeg4"), ("webm", "video/webm", "libvpx-vp9")):
        target = Path(directory) / f"clip.{extension}"
        subprocess.run(["ffmpeg", "-v", "error", "-nostdin", "-y", "-f", "lavfi", "-i", "testsrc2=size=128x128:rate=2:duration=2", "-an", "-c:v", codec, "-threads", "2", str(target)], check=True)
        data[extension] = {"inlineData": {"mimeType": mime, "data": base64.b64encode(target.read_bytes()).decode()}}
    return data


def rss(pid):
    values = {}
    for line in Path(f"/proc/{pid}/status").read_text().splitlines():
        if line.startswith(("VmRSS:", "VmHWM:")):
            key, value, _ = line.split()
            values[key[:-1]] = int(value) * 1024
    return values


async def measure(model_path, directory):
    media = fixtures(directory)
    profiles = {
        "text": [{"text": "A red square and a pure musical tone."}],
        "vision": [media["red-PNG"]],
        "audio": [media["wav"]],
        "full": [{"text": "Before "}, media["red-PNG"], {"text": " then "}, media["wav"], media["mp4"]],
    }
    results = {}
    for name, parts in profiles.items():
        config = Config(model_path)
        manager = Supervisor(config)
        await manager.start()
        payload = {"model": config.model, "items": [{"parts": parts, "dimensions": 768}]}
        try:
            started = time.monotonic()
            first = await manager.submit(payload)
            cold = time.monotonic() - started
            started = time.monotonic()
            second = await manager.submit(payload)
            warm = time.monotonic() - started
            assert first == second, "non-deterministic embeddings"
            profile_result = {"cold_seconds": cold, "warm_seconds": warm, "tokens": first["embeddings"][0]["tokens"], **rss(manager.process.pid)}
            if name == "full":
                for key, part in media.items():
                    await manager.submit({"model": config.model, "items": [{"parts": [part], "dimensions": 128}]})
                red = await manager.submit({"model": config.model, "items": [{"parts": [media["red-PNG"]], "dimensions": 768}]})
                blue = await manager.submit({"model": config.model, "items": [{"parts": [media["blue-PNG"]], "dimensions": 768}]})
                assert max(abs(a-b) for a,b in zip(red["embeddings"][0]["values"],blue["embeddings"][0]["values"])) > 1e-6, "media was ignored"
                profile_result.update(rss(manager.process.pid))
            old_pid = manager.process.pid
            # Same expiry path as production, with time advanced instead of sleeping 20m.
            manager.last_used -= 1201
            manager.wake.set()
            for _ in range(300):
                if manager.process is None and manager.unloads:
                    break
                await asyncio.sleep(0.01)
            assert manager.process is None and not Path(f"/proc/{old_pid}").exists(), "worker was not reaped"
            profile_result["unloaded"] = True
            results[name] = profile_result
            print(json.dumps({"profile": name, **profile_result}), flush=True)
        finally:
            await manager.close()
    return results


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model-path", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="embedding-benchmark-") as directory:
        result = asyncio.run(measure(args.model_path, directory))
    Path(args.output).write_text(json.dumps(result, indent=2) + "\n")


if __name__ == "__main__":
    main()
