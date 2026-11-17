"""Disposable ML process. Only this module imports the inference stack."""
import argparse
import base64
import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

from .contract import Failure, MAX_BYTES, MAX_TOKENS, MIME, prompt


def probe(path):
    result = subprocess.run(
        ["ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "format=duration:stream=codec_type,width,height",
         "-of", "json", str(path)], capture_output=True, timeout=15, check=True,
    )
    info = json.loads(result.stdout)
    # Bound decode work independently of the compressed byte limit.
    duration = float(info["format"]["duration"])
    if not 0 < duration <= 330:
        raise Failure("unsupported_media")
    for stream in info.get("streams", []):
        if stream.get("codec_type") == "video" and stream.get("width", 0) * stream.get("height", 0) > 16_777_216:
            raise Failure("unsupported_media")


def prepare_parts(parts, directory):
    import numpy as np
    from PIL import Image, ImageOps

    content = []
    media = False
    for index, part in enumerate(parts):
        if "text" in part:
            # Templates use these tokens internally. Literal markers could suppress
            # automatic placeholders and silently change media placement.
            if any(marker in part["text"] for marker in ("<|image|>", "<|audio|>", "<|video|>")):
                raise Failure("invalid_embedding_request")
            content.append({"type": "text", "text": part["text"]})
            continue
        media = True
        blob = part["inlineData"]
        raw = base64.b64decode(blob["data"], validate=True)
        mime = blob["mimeType"]
        if mime.startswith("image/"):
            with Image.open(io.BytesIO(raw)) as image:
                if image.width * image.height > 16_777_216 or image.format not in ("JPEG", "PNG", "WEBP"):
                    raise Failure("unsupported_media")
                image.load()
                value = ImageOps.exif_transpose(image).convert("RGB")
            content.append({"type": "image", "image": value})
        else:
            path = Path(directory) / f"media-{index}{MIME[mime][1]}"
            path.write_bytes(raw)
            probe(path)
            if mime.startswith("audio/"):
                audio = subprocess.run(
                    ["ffmpeg", "-v", "error", "-nostdin", "-protocol_whitelist", "file,pipe", "-i", str(path), "-vn", "-ac", "1", "-ar", "16000",
                     "-t", "330", "-f", "f32le", "pipe:1"], capture_output=True, check=True, timeout=30,
                )
                value = np.frombuffer(audio.stdout, dtype="<f4").copy()
                if not value.size:
                    raise Failure("unsupported_media")
                content.append({"type": "audio", "audio": value})
            else:
                content.append({"type": "video", "video": str(path)})
    if not media:
        return "".join(part["text"] for part in content)
    # Explicit structured messages preserve repeats and exact interleaving order.
    return {"role": "user", "content": content}


class Encoder:
    def __init__(self, path, profile, threads):
        import torch
        from sentence_transformers import SentenceTransformer

        torch.set_num_threads(threads)
        torch.set_num_interop_threads(1)
        config = {}
        if not profile & 1:
            config["vision_config"] = None
        if not profile & 2:
            config["audio_config"] = None
        self.model = SentenceTransformer(
            path, device="cpu", local_files_only=True, trust_remote_code=False,
            config_kwargs=config, model_kwargs={"dtype": torch.float32},
        ).eval()
        self.torch = torch

    def encode(self, request):
        torch = self.torch
        vectors = []
        with torch.inference_mode():
            for item in request["items"]:
                with tempfile.TemporaryDirectory(prefix="embedding-") as directory:
                    prefix = prompt(item)
                    if "token_ids" in item:
                        ids = list(item["token_ids"])
                        if any(t >= self.model.tokenizer.vocab_size for t in ids):
                            raise Failure("invalid_embedding_request")
                        if prefix:
                            prefix_ids = self.model.tokenizer.encode(prefix, add_special_tokens=False)
                            offset = int(ids[0] == self.model.tokenizer.bos_token_id)
                            ids[offset:offset] = prefix_ids
                        features = {"input_ids": torch.tensor([ids]), "attention_mask": torch.ones((1, len(ids)), dtype=torch.long), "modality": "text"}
                    else:
                        value = prepare_parts(item["parts"], directory)
                        if isinstance(value, str):
                            value = prefix + value
                        features = self.model.preprocess([value], processing_kwargs={
                            "text": {"truncation": False},
                            "chat_template": {"truncation": False},
                            "video": {"fps": 1, "max_frames": 32, "overflow_strategy": "uniform", "add_timestamps": False},
                            "audio": {"sampling_rate": 16000, "truncation": False},
                        })
                    count = int(features["attention_mask"].sum().item())
                    if not 0 < count <= MAX_TOKENS:
                        raise Failure("context_length_exceeded")
                    # Use the saved model's projection, pooling and normalization
                    # modules for both token IDs and preprocessed multimodal inputs.
                    vector = self.model(features)["sentence_embedding"][0, :item["dimensions"]].float()
                    norm = torch.linalg.vector_norm(vector)
                    if not torch.isfinite(vector).all() or not torch.isfinite(norm) or norm <= 0:
                        raise Failure("worker_failed", 503)
                    vector = vector / norm
                    vectors.append({"values": vector.tolist(), "tokens": count})
        return {"embeddings": vectors}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model-path", required=True)
    parser.add_argument("--profile", type=int, required=True)
    parser.add_argument("--threads", type=int, default=2)
    args = parser.parse_args()
    os.environ.update(HF_HUB_OFFLINE="1", TRANSFORMERS_OFFLINE="1", TOKENIZERS_PARALLELISM="false")
    output = sys.stdout
    # Library progress/warnings must never corrupt the JSON protocol.
    with contextlib.redirect_stdout(sys.stderr):
        encoder = Encoder(args.model_path, args.profile, args.threads)
    output.write('{"ready":true}\n')
    output.flush()
    while True:
        line = sys.stdin.buffer.readline(MAX_BYTES + 1)
        if not line:
            return
        if len(line) > MAX_BYTES:
            return
        try:
            with contextlib.redirect_stdout(sys.stderr):
                result = encoder.encode(json.loads(line))
        except Failure as exc:
            result = {"error": exc.code}
        except (OSError, ValueError, subprocess.SubprocessError):
            result = {"error": "unsupported_media"}
        except Exception:
            result = {"error": "worker_failed"}
        output.write(json.dumps(result, separators=(",", ":"), allow_nan=False) + "\n")
        output.flush()


if __name__ == "__main__":
    main()
