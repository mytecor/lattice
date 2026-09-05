#!/usr/bin/env python3

import argparse
import json
import signal
import socket
import sqlite3
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


GATEWAY_KEY = "spike-client-key"
LOGICAL_MODELS = ["cheap", "standard", "strong", "frontier"]


class QuietThreadingHTTPServer(ThreadingHTTPServer):
    def handle_error(self, _request, _client_address):
        # Losing race/hedge attempts are cancelled by the gateway.
        pass


def free_port():
    server = QuietThreadingHTTPServer(("127.0.0.1", 0), BaseHTTPRequestHandler)
    port = server.server_address[1]
    server.server_close()
    return port


class FakeUpstream:
    def __init__(self, name, api_key, delay=0.0, models=None):
        self.name = name
        self.api_key = api_key
        self.delay = delay
        self.models = models or [f"real-{name}-{model}" for model in LOGICAL_MODELS]
        self.requests = []
        self.statuses = []
        self.lock = threading.Lock()
        owner = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, _format, *_args):
                pass

            def _record(self, body=None):
                with owner.lock:
                    owner.requests.append(
                        {
                            "path": self.path,
                            "authorization": self.headers.get("Authorization"),
                            "body": body,
                            "time": time.monotonic(),
                        }
                    )

            def do_GET(self):
                self._record()
                if self.path != "/v1/models":
                    self.send_error(404)
                    return
                payload = {
                    "object": "list",
                    "data": [
                        {"id": model, "object": "model", "owned_by": owner.name}
                        for model in owner.models
                    ],
                }
                self._json(payload)

            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                body = json.loads(self.rfile.read(length) or b"{}")
                self._record(body)
                if self.headers.get("Authorization") != f"Bearer {owner.api_key}":
                    self._json({"error": "wrong upstream credential"}, status=401)
                    return
                with owner.lock:
                    status = owner.statuses.pop(0) if owner.statuses else 200
                if status == "drop":
                    self.close_connection = True
                    self.connection.shutdown(socket.SHUT_RDWR)
                    self.connection.close()
                    return
                if status != 200:
                    self._json({"error": f"forced {status}"}, status=status)
                    return
                time.sleep(owner.delay)
                if self.path == "/v1/chat/completions":
                    self._chat(body)
                    return
                if self.path == "/v1/responses":
                    self._responses(body)
                    return
                self.send_error(404)

            def _json(self, payload, status=200):
                data = json.dumps(payload).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                try:
                    self.wfile.write(data)
                except (BrokenPipeError, ConnectionResetError):
                    pass

            def _sse(self, events):
                data = "".join(f"data: {json.dumps(event)}\n\n" for event in events)
                data += "data: [DONE]\n\n"
                encoded = data.encode()
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.send_header("Content-Length", str(len(encoded)))
                self.end_headers()
                try:
                    self.wfile.write(encoded)
                except (BrokenPipeError, ConnectionResetError):
                    pass

            def _chat(self, body):
                payload = {
                    "id": f"chat-{owner.name}",
                    "object": "chat.completion.chunk" if body.get("stream") else "chat.completion",
                    "created": 1,
                    "model": body.get("model"),
                    "choices": [
                        {
                            "index": 0,
                            "delta": {"content": owner.name},
                            "finish_reason": "stop",
                        }
                    ],
                }
                if body.get("stream"):
                    self._sse([payload])
                else:
                    self._json(payload)

            def _responses(self, body):
                response = {
                    "id": f"resp-{owner.name}",
                    "object": "response",
                    "created_at": 1,
                    "status": "completed",
                    "model": body.get("model"),
                    "output": [],
                }
                if body.get("stream"):
                    self._sse(
                        [
                            {"type": "response.created", "response": response},
                            {"type": "response.completed", "response": response},
                        ]
                    )
                else:
                    self._json(response)

        self.server = QuietThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.port = self.server.server_address[1]
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    @property
    def base_url(self):
        return f"http://127.0.0.1:{self.port}/v1"

    def start(self):
        self.thread.start()

    def queue_statuses(self, *statuses):
        with self.lock:
            self.statuses.extend(statuses)

    def post_count(self):
        with self.lock:
            return sum(request["body"] is not None for request in self.requests)

    def stop(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)


def upstream_config(upstream, providers, priority=0):
    return {
        "id": upstream.name,
        "providers": providers,
        "base_url": upstream.base_url,
        "credential": {"type": "api_keys", "api_keys": [upstream.api_key]},
        "priority": priority,
        "available_models": LOGICAL_MODELS,
        "model_mappings": {
            model: f"real-{upstream.name}-{model}" for model in LOGICAL_MODELS
        },
    }


class Proxy:
    def __init__(
        self,
        binary,
        upstreams,
        strategy,
        same_upstream_retry_count=0,
        cooldown_secs=1,
    ):
        self.tempdir = tempfile.TemporaryDirectory(prefix="lattice-token-proxy-")
        self.port = free_port()
        config = {
            "host": "127.0.0.1",
            "port": self.port,
            "local_api_key": GATEWAY_KEY,
            "log_level": "silent",
            "model_list_prefix": False,
            "model_list_prefix_default_on_migrated": True,
            "retryable_failure_cooldown_secs": cooldown_secs,
            "same_upstream_retry_count": same_upstream_retry_count,
            "upstream_strategy": strategy,
            "hot_model_mappings": {},
            "upstreams": upstreams,
        }
        self.config_path = Path(self.tempdir.name) / "config.json"
        self.config_path.write_text(json.dumps(config), encoding="utf-8")
        self.process = subprocess.Popen(
            [binary, "--config", str(self.config_path), "serve"],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        self._wait_ready()

    def _wait_ready(self):
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                stdout, stderr = self.process.communicate()
                raise AssertionError(f"token-proxy exited early:\n{stdout}\n{stderr}")
            try:
                request("GET", self.url("/v1/models"), timeout=0.25)
                return
            except (urllib.error.URLError, TimeoutError):
                time.sleep(0.05)
        raise AssertionError("token-proxy did not become ready")

    def url(self, path):
        return f"http://127.0.0.1:{self.port}{path}"

    def request_log_rows(self):
        deadline = time.monotonic() + 2
        database = Path(self.tempdir.name) / "data.db"
        while time.monotonic() < deadline:
            try:
                with sqlite3.connect(database) as connection:
                    rows = connection.execute(
                        "SELECT upstream_id, status, response_error, request_body, response_body "
                        "FROM request_logs ORDER BY id"
                    ).fetchall()
                if rows:
                    return rows
            except sqlite3.Error:
                pass
            time.sleep(0.05)
        raise AssertionError("token_proxy request diagnostics were not written")

    def stop(self):
        if self.process.poll() is None:
            self.process.send_signal(signal.SIGINT)
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
        if self.process.returncode != 0:
            stdout, stderr = self.process.communicate()
            raise AssertionError(
                f"token-proxy exited with {self.process.returncode}:\n{stdout}\n{stderr}"
            )
        self.tempdir.cleanup()


def request(method, url, payload=None, key=None, timeout=5):
    data = None if payload is None else json.dumps(payload).encode()
    headers = {}
    if payload is not None:
        headers["Content-Type"] = "application/json"
    if key is not None:
        headers["Authorization"] = f"Bearer {key}"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as response:
            return response.status, response.headers, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.headers, error.read()


def sse_events(body):
    events = []
    for line in body.decode().splitlines():
        if not line.startswith("data: ") or line == "data: [DONE]":
            continue
        events.append(json.loads(line.removeprefix("data: ")))
    return events


def assert_auth_and_protocols(binary, chat, responses):
    proxy = Proxy(
        binary,
        [
            upstream_config(chat, ["openai"]),
            upstream_config(responses, ["openai-response"]),
        ],
        {"order": "fill_first", "dispatch": {"type": "serial"}},
    )
    try:
        status, _, _ = request(
            "POST",
            proxy.url("/v1/chat/completions"),
            {"model": "cheap", "messages": [{"role": "user", "content": "hello"}]},
        )
        assert status == 401, f"unauthenticated request returned {status}"

        status, _, body = request(
            "POST",
            proxy.url("/v1/chat/completions"),
            {
                "model": "cheap",
                "messages": [{"role": "user", "content": "sensitive-prompt-marker"}],
            },
            GATEWAY_KEY,
        )
        assert status == 200, body

        status, headers, body = request(
            "POST",
            proxy.url("/v1/chat/completions"),
            {
                "model": "cheap",
                "stream": True,
                "messages": [{"role": "user", "content": "hello"}],
            },
            GATEWAY_KEY,
        )
        assert status == 200
        assert headers.get_content_type() == "text/event-stream"
        chat_events = sse_events(body)
        assert chat_events and chat_events[0]["model"] == "cheap"
        assert chat.requests[-1]["body"]["model"] == "real-chat-cheap"
        assert chat.requests[-1]["authorization"] == "Bearer provider-chat-key"

        status, headers, body = request(
            "POST",
            proxy.url("/v1/responses"),
            {"model": "strong", "stream": True, "input": "hello"},
            GATEWAY_KEY,
        )
        assert status == 200
        assert headers.get_content_type() == "text/event-stream"
        response_events = sse_events(body)
        assert response_events and response_events[-1]["response"]["model"] == "strong"
        assert responses.requests[-1]["body"]["model"] == "real-responses-strong"
        assert responses.requests[-1]["authorization"] == "Bearer provider-responses-key"

        status, _, body = request("GET", proxy.url("/v1/models"))
        assert status == 200
        model_ids = sorted(item["id"] for item in json.loads(body)["data"])
        assert model_ids == sorted(LOGICAL_MODELS), model_ids
        assert not any("real-" in model for model in model_ids)

        chat_before = chat.post_count()
        responses_before = responses.post_count()
        status, _, body = request(
            "POST",
            proxy.url("/v1/chat/completions"),
            {
                "model": "real-chat-cheap",
                "messages": [{"role": "user", "content": "hello"}],
            },
            GATEWAY_KEY,
        )
        assert status == 404, body
        assert chat.post_count() == chat_before
        assert responses.post_count() == responses_before

        diagnostics = repr(proxy.request_log_rows())
        assert "sensitive-prompt-marker" not in diagnostics
        assert GATEWAY_KEY not in diagnostics
        assert chat.api_key not in diagnostics
        assert responses.api_key not in diagnostics
    finally:
        proxy.stop()


def assert_parallel_dispatch(binary, strategy_type, slow, fast):
    dispatch = {"type": strategy_type, "max_parallel": 2}
    if strategy_type == "hedged":
        dispatch["delay_ms"] = 100
    proxy = Proxy(
        binary,
        [
            upstream_config(slow, ["openai"]),
            upstream_config(fast, ["openai"]),
        ],
        {"order": "fill_first", "dispatch": dispatch},
    )
    try:
        before_slow = len(slow.requests)
        before_fast = len(fast.requests)
        started = time.monotonic()
        status, _, body = request(
            "POST",
            proxy.url("/v1/chat/completions"),
            {"model": "standard", "messages": [{"role": "user", "content": "hello"}]},
            GATEWAY_KEY,
        )
        elapsed = time.monotonic() - started
        assert status == 200
        payload = json.loads(body)
        assert payload["id"] == "chat-fast", payload
        assert elapsed < slow.delay, (strategy_type, elapsed)
        assert len(slow.requests) > before_slow, f"{strategy_type} did not start slow upstream"
        assert len(fast.requests) > before_fast, f"{strategy_type} did not start fast upstream"
        assert slow.requests[-1]["authorization"] == "Bearer provider-slow-key"
        assert fast.requests[-1]["authorization"] == "Bearer provider-fast-key"
    finally:
        proxy.stop()


def assert_retry(binary, upstream):
    upstream.queue_statuses(500, 200)
    proxy = Proxy(
        binary,
        [upstream_config(upstream, ["openai"])],
        {"order": "fill_first", "dispatch": {"type": "serial"}},
        same_upstream_retry_count=1,
    )
    try:
        before = upstream.post_count()
        status, _, body = request(
            "POST",
            proxy.url("/v1/chat/completions"),
            {"model": "cheap", "messages": [{"role": "user", "content": "hello"}]},
            GATEWAY_KEY,
        )
        assert status == 200, body
        assert upstream.post_count() == before + 2
    finally:
        proxy.stop()


def assert_fallback_and_cooldown(binary, primary, fallback, failure_status):
    primary.queue_statuses(failure_status)
    proxy = Proxy(
        binary,
        [
            upstream_config(primary, ["openai"]),
            upstream_config(fallback, ["openai"]),
        ],
        {"order": "fill_first", "dispatch": {"type": "serial"}},
        cooldown_secs=5,
    )
    try:
        primary_before = primary.post_count()
        fallback_before = fallback.post_count()
        for _ in range(2):
            status, _, body = request(
                "POST",
                proxy.url("/v1/chat/completions"),
                {"model": "strong", "messages": [{"role": "user", "content": "hello"}]},
                GATEWAY_KEY,
            )
            assert status == 200, body
            assert json.loads(body)["id"] == f"chat-{fallback.name}"
        assert primary.post_count() == primary_before + 1
        assert fallback.post_count() == fallback_before + 2
        rows = proxy.request_log_rows()
        assert any(row[0] == primary.name and row[1] == failure_status for row in rows), rows
        assert any(row[0] == fallback.name and row[1] == 200 for row in rows), rows
        diagnostics = repr(rows)
        assert GATEWAY_KEY not in diagnostics
        assert primary.api_key not in diagnostics
        assert fallback.api_key not in diagnostics
    finally:
        proxy.stop()


def assert_transport_fallback(binary, primary, fallback):
    # The transport layer may transparently reconnect once before gateway
    # failover, so keep the primary unavailable across several connections.
    primary.queue_statuses("drop", "drop", "drop", "drop")
    proxy = Proxy(
        binary,
        [
            upstream_config(primary, ["openai"]),
            upstream_config(fallback, ["openai"]),
        ],
        {"order": "fill_first", "dispatch": {"type": "serial"}},
    )
    try:
        primary_before = primary.post_count()
        fallback_before = fallback.post_count()
        status, _, body = request(
            "POST",
            proxy.url("/v1/chat/completions"),
            {"model": "cheap", "messages": [{"role": "user", "content": "hello"}]},
            GATEWAY_KEY,
        )
        assert status == 200, body
        payload = json.loads(body)
        assert payload["id"] == f"chat-{fallback.name}", (
            payload,
            primary.post_count() - primary_before,
            fallback.post_count() - fallback_before,
        )
        assert primary.post_count() > primary_before
        assert fallback.post_count() == fallback_before + 1
    finally:
        proxy.stop()


def assert_priority(binary, high, low):
    proxy = Proxy(
        binary,
        [
            upstream_config(low, ["openai"], priority=0),
            upstream_config(high, ["openai"], priority=100),
        ],
        {"order": "fill_first", "dispatch": {"type": "serial"}},
    )
    try:
        high_before = high.post_count()
        low_before = low.post_count()
        status, _, body = request(
            "POST",
            proxy.url("/v1/chat/completions"),
            {"model": "frontier", "messages": [{"role": "user", "content": "hello"}]},
            GATEWAY_KEY,
        )
        assert status == 200, body
        assert json.loads(body)["id"] == f"chat-{high.name}"
        assert high.post_count() == high_before + 1
        assert low.post_count() == low_before
    finally:
        proxy.stop()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--token-proxy", required=True)
    args = parser.parse_args()

    upstreams = [
        FakeUpstream("chat", "provider-chat-key"),
        FakeUpstream("responses", "provider-responses-key"),
        FakeUpstream("slow", "provider-slow-key", delay=0.8),
        FakeUpstream("fast", "provider-fast-key", delay=0.05),
    ]
    for upstream in upstreams:
        upstream.start()
    try:
        assert_auth_and_protocols(args.token_proxy, upstreams[0], upstreams[1])
        assert_retry(args.token_proxy, upstreams[0])
        assert_fallback_and_cooldown(args.token_proxy, upstreams[0], upstreams[1], 429)
        assert_fallback_and_cooldown(args.token_proxy, upstreams[0], upstreams[1], 503)
        assert_transport_fallback(args.token_proxy, upstreams[0], upstreams[1])
        assert_priority(args.token_proxy, upstreams[3], upstreams[2])
        assert_parallel_dispatch(args.token_proxy, "race", upstreams[2], upstreams[3])
        assert_parallel_dispatch(args.token_proxy, "hedged", upstreams[2], upstreams[3])
    finally:
        for upstream in upstreams:
            upstream.stop()
    print(
        "token_proxy spike passed: auth, credential isolation, Chat/Responses SSE, "
        "models, retry, 429/5xx/transport fallback, cooldown, priority, race, hedged, diagnostics"
    )


if __name__ == "__main__":
    main()
