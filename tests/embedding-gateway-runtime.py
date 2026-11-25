"""Installed gateway/driver boundary against an isolated OpenAI fixture."""
import base64
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


class Fixture(BaseHTTPRequestHandler):
    calls = []
    blocking = 0
    unavailable_model = None
    lock = threading.Lock()
    release = threading.Event()

    def log_message(self, *args):
        pass

    def respond(self, value):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(value).encode())

    def do_GET(self):
        assert self.path == "/v1/models"
        self.respond({"data": [{"id": "native"}, {"id": "native-alt"}]})

    def do_POST(self):
        assert self.path == "/v1/embeddings"
        request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        native = request["model"]
        assert native in ("native", "native-alt") and request["encoding_format"] == "float"
        inputs = request["input"]
        for item in inputs:
            if isinstance(item, dict):
                assert item == {"content": [
                    {"type": "text", "text": "before "},
                    {"type": "image_url", "image_url": {"url": "data:image/png;base64,+/8="}},
                    {"type": "text", "text": " after"},
                ]}
        with self.lock:
            type(self).calls.append(native)
            unavailable = native == self.unavailable_model
        if unavailable:
            self.send_error(503)
            return
        if inputs[0] == "block":
            with self.lock:
                type(self).blocking += 1
            assert self.release.wait(4), "fixture release timed out"
        self.respond({
            "data": [{"index": index, "embedding": [1.0] + [0.0] * 767} for index in range(len(inputs))],
            "usage": {"prompt_tokens": 4 * len(inputs)},
        })


def main(binary):
    fixture = ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    threading.Thread(target=fixture.serve_forever, daemon=True).start()
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    config = {
        "host": "127.0.0.1", "port": port, "log_level": "silent",
        "default_embedding_model": "embedding",
        "client_api_keys": [{"id": "fixture", "key": "fixture-key"}],
        "providers": [
            {"id": name, "driver": "embeddings", "config": {"inference_url": f"http://127.0.0.1:{fixture.server_port}"}}
            for name in ("local", "alternate")
        ],
        "rules": [
            {"route": "embedding", "action": "filter", "where": {"model": {"eq": "embedding"}}},
            {"route": "embedding", "action": "map", "providers": ["local"], "native": "native"},
            {"route": "embedding", "action": "map", "providers": ["alternate"], "native": "native-alt"},
            {"route": "embedding", "action": "admission", "max_in_flight": 5, "max_pending": 0},
            {"route": "embedding", "action": "rank", "strategy": "priority"},
            {"route": "embedding", "action": "balance", "strategy": "round_robin"},
            {"route": "embedding", "action": "race", "count": 1},
            {"route": "embedding", "action": "retry", "attempts": 2},
            {"route": "embedding", "action": "semaphore", "max_calls": 2, "max_calls_per_provider": 1, "max_in_flight": 1},
            {"route": "embedding", "action": "timeout", "duration": "5s"},
        ],
    }
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / "config.json"
        path.write_text(json.dumps(config))
        process = subprocess.Popen([binary, "--config", str(path), "serve"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            for _ in range(200):
                try:
                    with urlopen(f"http://127.0.0.1:{port}/healthz", timeout=1):
                        break
                except (URLError, TimeoutError):
                    if process.poll() is not None:
                        raise AssertionError("packaged gateway failed to start")
                    time.sleep(0.05)
            else:
                raise AssertionError("packaged gateway did not become ready")

            def post(endpoint, payload, google=False):
                headers = {"Content-Type": "application/json"}
                headers["x-goog-api-key" if google else "Authorization"] = "fixture-key" if google else "Bearer fixture-key"
                request = Request(f"http://127.0.0.1:{port}{endpoint}", data=json.dumps(payload).encode(), headers=headers)
                with urlopen(request, timeout=10) as response:
                    return json.load(response)

            result = post("/v1/embeddings", {"input": ["one", "two"], "dimensions": 128, "encoding_format": "base64"})
            assert result["model"] == "embedding" and [row["index"] for row in result["data"]] == [0, 1]
            raw = base64.b64decode(result["data"][0]["embedding"])
            assert len(raw) == 128*4 and struct.unpack_from("<f", raw)[0] == 1.0
            content = {"parts": [{"text": "before "}, {"inlineData": {"mimeType": "image/png", "data": "+/8="}}, {"text": " after"}]}
            oai = post("/v1/embeddings", {"model": "embedding", "input": content, "dimensions": 256})
            sdk_content = {"parts": [{"text": "before "}, {"inline_data": {"mime_type": "image/png", "data": "-_8"}}, {"text": " after"}]}
            google = post("/v1beta/models/embedding:embedContent", {"content": sdk_content, "outputDimensionality": 256}, True)
            assert oai["data"][0]["embedding"] == google["embedding"]["values"]
            batch = post("/v1beta/models/embedding:batchEmbedContents", {"requests": [{"content": content, "outputDimensionality": 128}, {"content": content, "outputDimensionality": 512}]}, True)
            assert [len(v["values"]) for v in batch["embeddings"]] == [128, 512]
            assert len(Fixture.calls) == 4
            first = len(Fixture.calls)
            for _ in range(2):
                assert post("/v1/embeddings", {"input": content})["model"] == "embedding"
            assert set(Fixture.calls[first:]) == {"native", "native-alt"}
            with Fixture.lock:
                Fixture.unavailable_model = "native-alt"
                first = len(Fixture.calls)
            for _ in range(2):
                assert post("/v1/embeddings", {"input": content})["model"] == "embedding"
            with Fixture.lock:
                assert set(Fixture.calls[first:]) == {"native", "native-alt"}
                Fixture.unavailable_model = None
            with ThreadPoolExecutor(max_workers=5) as pool:
                blocked = [pool.submit(post, "/v1/embeddings", {"model": "embedding", "input": "block", "dimensions": 128}) for _ in range(5)]
                try:
                    for _ in range(100):
                        if Fixture.blocking == 5:
                            break
                        time.sleep(0.02)
                    assert Fixture.blocking == 5
                    try:
                        post("/v1/embeddings", {"model": "embedding", "input": "overload"})
                        raise AssertionError("gateway added an unbounded queue")
                    except HTTPError as error:
                        assert error.code == 429 and error.headers.get("Retry-After")
                finally:
                    Fixture.release.set()
                for result in blocked:
                    assert result.result()["model"] == "embedding"
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
            fixture.shutdown()
    print("installed embeddings gateway/driver smoke passed")


if __name__ == "__main__":
    main(sys.argv[1])
