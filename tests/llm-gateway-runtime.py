"""Verify the packaged core can discover its HTTP driver and serve strict JSON."""
import http.server
import json
import pathlib
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request


class Upstream(http.server.BaseHTTPRequestHandler):
    requests = []

    def log_message(self, *_args):
        pass

    def do_GET(self):
        data = json.dumps({"object": "list", "data": [{"id": "fixture-model", "object": "model"}]}).encode()
        self.send_response(200)
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.requests.append(payload)
        # The integration boundary must preserve both the mapped model and schema.
        assert payload["model"] == "fixture-model"
        assert payload["response_format"]["json_schema"]["strict"] is True
        wrapped = payload["messages"][0]["content"] == "Return wrapped value 1."
        content = '<think>fixture thought</think>{"value":1}' if wrapped else ('{"value":-1}' if len(self.requests) == 1 else '{"value":1}')
        if payload.get("stream"):
            events = []
            for part in ["<thi", "nk>fixture thought</thi", 'nk>{"value":1}']:
                events.append({"id": "fixture-stream", "object": "chat.completion.chunk", "model": "fixture-model", "choices": [{"index": 0, "delta": {"content": part}, "finish_reason": None}]})
            events.append({"id": "fixture-stream", "object": "chat.completion.chunk", "model": "fixture-model", "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}]})
            events.append({"id": "fixture-stream", "object": "chat.completion.chunk", "model": "fixture-model", "choices": [], "usage": {"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}})
            data = ("".join("data: " + json.dumps(event) + "\n\n" for event in events) + "data: [DONE]\n\n").encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            return
        data = json.dumps({
            "id": "fixture-response", "object": "chat.completion", "model": "fixture-model",
            "choices": [{"index": 0, "message": {"role": "assistant", "content": content}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5},
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def unused_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def main():
    upstream = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Upstream)
    worker = threading.Thread(target=upstream.serve_forever, daemon=True)
    worker.start()
    port, metrics_port = unused_port(), unused_port()
    config = {
        "host": "127.0.0.1", "port": port, "metrics_host": "127.0.0.1", "metrics_port": metrics_port,
        "providers": [{"id": "fixture", "config": {
            "base_provider": "openai", "inference_url": f"http://127.0.0.1:{upstream.server_port}/v1",
            "allow_private_network": True,
        }}],
        "rules": [
            {"route": "fixture", "action": "filter", "where": {"model": {"eq": "logical"}}},
            {"route": "fixture", "action": "map", "providers": ["fixture"], "native": "fixture-model"},
            {"route": "fixture", "action": "race", "count": 1},
            {"route": "fixture", "action": "transform", "type": "split_thinking"},
            {"route": "raw", "action": "filter", "where": {"model": {"eq": "raw"}}},
            {"route": "raw", "action": "map", "providers": ["fixture"], "native": "fixture-model"},
            {"route": "raw", "action": "race", "count": 1},
        ],
    }
    try:
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "config.json"
            path.write_text(json.dumps(config))
            # check itself discovers the packaged driver; no provider credentials exist.
            subprocess.run([sys.argv[1], "--config", str(path), "check"], check=True, timeout=20)
            with (path.parent / "gateway.log").open("w+") as log:
                process = subprocess.Popen([sys.argv[1], "--config", str(path), "serve"], stdout=log, stderr=log)
                try:
                    url = f"http://127.0.0.1:{port}"
                    for _ in range(100):
                        try:
                            with urllib.request.urlopen(url + "/healthz", timeout=1) as response:
                                assert response.status == 200
                            break
                        except (OSError, urllib.error.URLError):
                            if process.poll() is not None:
                                raise RuntimeError("packaged gateway exited")
                            time.sleep(0.05)
                    else:
                        raise RuntimeError("packaged gateway did not start")
                    request = {
                        "model": "logical", "messages": [{"role": "user", "content": "Return value 1."}],
                        "response_format": {"type": "json_schema", "json_schema": {
                            "name": "answer", "strict": True,
                            "schema": {"type": "object", "properties": {"value": {"type": "integer", "minimum": 0}}, "required": ["value"]},
                        }},
                    }
                    query = urllib.request.Request(url + "/v1/chat/completions", data=json.dumps(request).encode(), headers={"Content-Type": "application/json"})
                    with urllib.request.urlopen(query, timeout=20) as response:
                        result = json.load(response)
                    assert result["model"] == "logical"
                    assert json.loads(result["choices"][0]["message"]["content"]) == {"value": 1}
                    assert len(Upstream.requests) == 2
                    assert Upstream.requests[1]["messages"][-1]["role"] == "user"
                    assert result["usage"]["prompt_tokens"] == 6

                    # Exercise the installed transformation boundary, including
                    # delimiter fragments arriving in separate HTTP SSE events.
                    request["messages"] = [{"role": "user", "content": "Return wrapped value 1."}]
                    for stream in (False, True):
                        before = len(Upstream.requests)
                        request["stream"] = stream
                        request["stream_options"] = {"include_usage": True}
                        query = urllib.request.Request(url + "/v1/chat/completions", data=json.dumps(request).encode(), headers={"Content-Type": "application/json"})
                        with urllib.request.urlopen(query, timeout=20) as response:
                            if stream:
                                events = [json.loads(line[6:]) for line in response.read().decode().splitlines() if line.startswith("data: ") and line != "data: [DONE]"]
                                content = "".join(choice.get("delta", {}).get("content", "") for event in events for choice in event.get("choices", []))
                                reasoning = "".join(choice.get("delta", {}).get("reasoning_content", "") for event in events for choice in event.get("choices", []))
                            else:
                                result = json.load(response)
                                content = result["choices"][0]["message"]["content"]
                                reasoning = result["choices"][0]["message"]["reasoning_content"]
                        assert json.loads(content) == {"value": 1}
                        assert reasoning == "fixture thought"
                        assert len(Upstream.requests) == before + 1

                    # A second logical model with the same physical provider
                    # has no transform and rejects the same wrapped JSON.
                    request["model"] = "raw"
                    request["stream"] = False
                    query = urllib.request.Request(url + "/v1/chat/completions", data=json.dumps(request).encode(), headers={"Content-Type": "application/json"})
                    before = len(Upstream.requests)
                    try:
                        urllib.request.urlopen(query, timeout=20)
                    except urllib.error.HTTPError as error:
                        assert error.code == 502
                        assert json.load(error)["error"]["code"] == "structured_output_validation_failed"
                    else:
                        raise AssertionError("transformation leaked to an unconfigured logical model")
                    assert len(Upstream.requests) == before + 3
                except BaseException:
                    log.flush()
                    print(pathlib.Path(log.name).read_text(), file=sys.stderr)
                    raise
                finally:
                    process.terminate()
                    process.wait(timeout=10)
    finally:
        upstream.shutdown()
        upstream.server_close()


if __name__ == "__main__":
    main()
