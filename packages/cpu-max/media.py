"""Decode the inline media accepted by the CPU-Max HTTP server."""

import base64
import binascii
import io
import json
import math
from pathlib import Path
import struct
import subprocess
import sys
import tempfile

from PIL import Image, ImageOps

MAX_JSON_BYTES = 40 << 20
MAX_PIXELS = 16_777_216
MAX_SECONDS = 330
FORMATS = {"input_audio": "wav,mp3,flac,ogg", "input_video": "mov,matroska,webm"}


def aligned_png(image, budget):
    width, height = image.size
    if width * height > MAX_PIXELS:
        raise ValueError("image exceeds the pixel limit")
    image = ImageOps.exif_transpose(image).convert("RGB")
    width, height = image.size
    # A soft token covers a 3 × 3 pool of 16 × 16 patches. Match the
    # EmbeddingGemma 2 processor's aspect-ratio and patch-budget calculation.
    scale = math.sqrt(budget * 48**2 / (width * height))
    target_width = max(1, math.floor(width * scale / 48))
    target_height = max(1, math.floor(height * scale / 48))
    size = (min(target_width, budget) * 48, min(target_height, budget) * 48)
    resized = image.resize(size, Image.Resampling.BICUBIC)
    out = io.BytesIO()
    resized.save(out, format="PNG")
    return base64.b64encode(out.getvalue()).decode("ascii")


def payload_bytes(encoded):
    if encoded.startswith(b"data:"):
        header, _, encoded = encoded.partition(b",")
        if not header.endswith(b";base64"):
            raise ValueError("expected an inline media data URI")
    return base64.b64decode(encoded, validate=True)


def media_command(kind, name, args):
    return subprocess.run(
        [
            name, "-v", "error", "-protocol_whitelist", "file,pipe",
            "-format_whitelist", FORMATS[kind], *args,
        ],
        check=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=30,
    ).stdout


def prepare_clip(kind, raw):
    with tempfile.TemporaryDirectory(prefix="cpu-max-media-") as directory:
        path = Path(directory) / "clip"
        path.write_bytes(raw)
        info = json.loads(media_command(kind, "ffprobe", [
            "-show_entries", "format=duration:stream=codec_type,width,height",
            "-of", "json", str(path),
        ]))
        duration = float(info["format"]["duration"])
        if not 0 < duration <= MAX_SECONDS:
            raise ValueError("clip exceeds the duration limit")

        if kind == "input_audio":
            pcm = media_command(kind, "ffmpeg", [
                "-nostdin", "-threads", "1", "-i", str(path), "-map", "0:a:0",
                "-ac", "1", "-ar", "16000", "-f", "f32le", "pipe:1",
            ])
            if not pcm or len(pcm) > MAX_SECONDS * 16000 * 4:
                raise ValueError("audio exceeds the sample limit")
            header = struct.pack(
                "<4sI4s4sIHHIIHH4sI", b"RIFF", 36 + len(pcm), b"WAVE", b"fmt ",
                16, 3, 1, 16000, 64000, 4, 32, b"data", len(pcm),
            )
            return {"type": "audio", "data": base64.b64encode(header + pcm).decode("ascii")}

        for stream in info.get("streams", []):
            if stream.get("codec_type") == "video" and stream["width"] * stream["height"] > MAX_PIXELS:
                raise ValueError("video exceeds the pixel limit")
        count = math.ceil(duration)
        indices = range(count) if count <= 32 else (i * (count - 1) // 31 for i in range(32))
        selector = "+".join(f"eq(n,{index})" for index in indices)
        media_command(kind, "ffmpeg", [
            "-nostdin", "-threads", "1", "-i", str(path), "-map", "0:v:0",
            "-vf", f"fps=1:start_time=0:round=up,select='{selector}'",
            "-fps_mode", "vfr", "-frames:v", "32", "-threads", "1",
            str(Path(directory) / "%02d.png"),
        ])
        frames = []
        for frame in sorted(Path(directory).glob("*.png")):
            with Image.open(frame) as image:
                frames.append(aligned_png(image, 140))
            frame.unlink()
        if not frames:
            raise ValueError("video produced no frames")
        return {"type": "video", "fps": 1, "frames": frames}


def main():
    kind = sys.argv[1]
    encoded = sys.stdin.buffer.read(MAX_JSON_BYTES + 1)
    if len(encoded) > MAX_JSON_BYTES:
        return 1
    try:
        raw = payload_bytes(encoded)
        if kind == "image_url":
            with Image.open(io.BytesIO(raw), formats=["JPEG", "PNG", "WEBP"]) as image:
                part = {"type": "image", "data": aligned_png(image, 280)}
        else:
            part = prepare_clip(kind, raw)
        json.dump(part, sys.stdout, separators=(",", ":"))
    except (binascii.Error, ValueError, OSError, KeyError, subprocess.SubprocessError):
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
