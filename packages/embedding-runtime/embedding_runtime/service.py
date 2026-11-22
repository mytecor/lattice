"""Lightweight HTTP supervisor; imports no torch/transformers modules."""
import argparse
import asyncio
from collections import deque
from dataclasses import dataclass, field
import ipaddress
import json
import logging
import os
import signal
import sys
import tempfile
import time

from aiohttp import web

from .contract import DIMENSIONS, Failure, MAX_BYTES, validate

LOG = logging.getLogger("embedding-runtime")


@dataclass
class Config:
    model_path: str
    model: str = "google/embeddinggemma-2"
    idle_seconds: float = 1200
    timeout_seconds: float = 300
    threads: int = 2
    device: str = "cpu"
    max_queue: int = 4
    allow_ips: tuple[str, ...] = ()
    worker_command: list[str] | None = None


@dataclass
class Job:
    request: dict
    profile: int
    future: asyncio.Future
    received: float = field(default_factory=time.monotonic)


class Supervisor:
    def __init__(self, config, clock=time.monotonic):
        self.config, self.clock = config, clock
        self.queue = deque()
        self.wake = asyncio.Event()
        self.process = None
        self.temporary = None
        self.profile = 0
        self.active = None
        self.operation = None
        self.last_used = clock()
        self.loads = self.unloads = self.errors = self.requests = 0
        self.load_seconds = self.request_seconds = 0.0
        self.runner = None
        self.closing = False
        self.stopping = None

    async def start(self):
        self.runner = asyncio.create_task(self.run())

    async def close(self):
        self.closing = True
        if self.operation:
            self.operation.cancel()
        if self.runner:
            self.runner.cancel()
            await asyncio.gather(self.runner, return_exceptions=True)
        for job in self.queue:
            if not job.future.done():
                job.future.set_exception(Failure("worker_failed", 503))
        self.queue.clear()
        await self.stop_worker()

    async def submit(self, request):
        profile = validate(request, self.config.model)
        # The first queued item will become active on the next event-loop turn.
        capacity = self.config.max_queue + (0 if self.active else 1)
        if self.closing or len(self.queue) >= capacity:
            raise Failure("queue_full", 429)
        job = Job(request, profile, asyncio.get_running_loop().create_future())
        self.queue.append(job)
        self.wake.set()
        try:
            return await asyncio.wait_for(job.future, self.config.timeout_seconds)
        except TimeoutError:
            raise Failure("deadline_exceeded", 504) from None
        finally:
            if job.future.cancelled():
                if self.active is job and self.operation:
                    self.operation.cancel()
                else:
                    try:
                        self.queue.remove(job)
                    except ValueError:
                        pass
                self.wake.set()

    async def stop_worker(self):
        if self.stopping is None:
            self.stopping = asyncio.create_task(self._stop_worker())
        stopping = self.stopping
        try:
            await asyncio.shield(stopping)
        except asyncio.CancelledError:
            await stopping
            raise
        finally:
            if stopping.done() and self.stopping is stopping:
                self.stopping = None

    async def _stop_worker(self):
        process, self.process = self.process, None
        self.profile = 0
        if process is None:
            if self.temporary:
                self.temporary.cleanup()
                self.temporary = None
            return
        # Kill the process group as decoders may have spawned ffmpeg children.
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            await asyncio.wait_for(process.wait(), 2)
        except TimeoutError:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            await process.wait()
        if self.temporary:
            self.temporary.cleanup()
            self.temporary = None
        self.unloads += 1
        LOG.info(json.dumps({"event": "worker_unloaded"}))

    async def infer(self, job):
        profile = self.profile | job.profile
        if self.process is not None and (self.process.returncode is not None or profile != self.profile):
            await self.stop_worker()
        if self.process is None:
            started = self.clock()
            command = self.config.worker_command or [sys.executable, "-m", "embedding_runtime.worker"]
            extra_args = ["--device", self.config.device] if self.config.device != "cpu" else []
            self.temporary = tempfile.TemporaryDirectory(prefix="embedding-worker-")
            worker_env = {**os.environ, "TMPDIR": self.temporary.name}
            if not self.config.worker_command:
                worker_env["PYTHONPATH"] = os.pathsep.join(p for p in sys.path if p)
            self.process = await asyncio.create_subprocess_exec(
                *command, "--model-path", self.config.model_path, "--profile", str(profile),
                "--threads", str(self.config.threads), *extra_args, stdin=asyncio.subprocess.PIPE,
                stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL,
                limit=MAX_BYTES, start_new_session=True,
                env=worker_env,
            )
            if await self.process.stdout.readline() != b'{"ready":true}\n':
                raise Failure("worker_failed", 503)
            self.profile = profile
            self.loads += 1
            self.load_seconds += self.clock() - started
            LOG.info(json.dumps({"event": "worker_loaded", "profile": profile}))
        self.process.stdin.write(json.dumps(job.request, separators=(",", ":"), ensure_ascii=False).encode() + b"\n")
        await self.process.stdin.drain()
        line = await self.process.stdout.readline()
        if not line or len(line) > 2 << 20:
            raise Failure("worker_failed", 503)
        result = json.loads(line)
        if "error" in result:
            code = result["error"]
            if code not in ("context_length_exceeded", "unsupported_media", "invalid_embedding_request"):
                code = "worker_failed"
            raise Failure(code, 503 if code == "worker_failed" else 400)
        return result

    async def run(self):
        try:
            while True:
                if not self.queue:
                    idle = self.config.idle_seconds - (self.clock() - self.last_used)
                    if self.process is not None and idle <= 0:
                        await self.stop_worker()
                    if self.queue:
                        continue
                    self.wake.clear()
                    try:
                        await asyncio.wait_for(self.wake.wait(), max(0.01, idle) if self.process else None)
                    except TimeoutError:
                        pass
                    continue
                job = self.queue.popleft()
                if job.future.done():
                    continue
                self.active = job
                self.operation = asyncio.create_task(self.infer(job))
                started = self.clock()
                try:
                    result = await self.operation
                    if not job.future.done():
                        job.future.set_result(result)
                except asyncio.CancelledError:
                    if not job.future.done():
                        job.future.cancel()
                    await self.stop_worker()
                    if self.closing:
                        raise
                except Exception as exc:
                    self.errors += 1
                    failure = exc if isinstance(exc, Failure) else Failure("worker_failed", 503)
                    if failure.code == "worker_failed":
                        await self.stop_worker()
                    if not job.future.done():
                        job.future.set_exception(Failure(failure.code, failure.status))
                    LOG.warning(json.dumps({"event": "embedding_failed", "code": failure.code}))
                finally:
                    self.requests += 1
                    self.request_seconds += self.clock() - started
                    self.last_used = self.clock()
                    self.active = self.operation = None
                    job = result = None
        finally:
            await self.stop_worker()

    def metrics(self):
        values = {
            "worker_loaded": int(self.process is not None and self.process.returncode is None),
            "active": int(self.active is not None), "queue_depth": len(self.queue),
            "worker_loads_total": self.loads, "worker_unloads_total": self.unloads,
            "requests_total": self.requests, "errors_total": self.errors,
            "load_duration_seconds_sum": self.load_seconds,
            "load_duration_seconds_count": self.loads,
            "request_duration_seconds_sum": self.request_seconds,
            "request_duration_seconds_count": self.requests,
        }
        lines = [f"embedding_runtime_{key} {value}" for key, value in values.items()]
        for name, bit in (("vision", 1), ("audio", 2)):
            lines.append(f'embedding_runtime_encoder_loaded{{encoder="{name}"}} {int(self.process is not None and bool(self.profile & bit))}')
        return "\n".join(lines) + "\n"


def _peer_allowed(remote, networks):
    if not remote:
        return False
    try:
        addr = ipaddress.ip_address(remote.split("%", 1)[0])
    except ValueError:
        return False
    candidates = [addr]
    if isinstance(addr, ipaddress.IPv6Address) and addr.ipv4_mapped is not None:
        candidates.append(addr.ipv4_mapped)
    return any(candidate in net for candidate in candidates for net in networks)


def application(config, supervisor=None):
    manager = supervisor or Supervisor(config)
    networks = tuple(ipaddress.ip_network(entry, strict=False) for entry in config.allow_ips)
    middlewares = []
    if networks:
        @web.middleware
        async def allow_ip_middleware(request, handler):
            if not _peer_allowed(request.remote, networks):
                return web.json_response({"error": {"code": "forbidden"}}, status=403)
            return await handler(request)
        middlewares.append(allow_ip_middleware)
    app = web.Application(client_max_size=MAX_BYTES, middlewares=middlewares)

    async def lifecycle(app):
        await manager.start()
        yield
        await manager.close()
    app.cleanup_ctx.append(lifecycle)

    async def health(request):
        return web.json_response({"status": "ok"})

    async def models(request):
        return web.json_response({"data": [{"id": config.model, "input_modalities": ["text", "image", "audio", "video"], "embedding_dimensions": list(DIMENSIONS)}]})

    async def metrics(request):
        return web.Response(text=manager.metrics(), content_type="text/plain")

    async def embed(request):
        try:
            data = await request.json()
            result = await manager.submit(data)
            return web.json_response(result)
        except Failure as exc:
            headers = {"Retry-After": "1"} if exc.status == 429 else None
            return web.json_response({"error": {"code": exc.code}}, status=exc.status, headers=headers)
        except (ValueError, TypeError):
            return web.json_response({"error": {"code": "invalid_embedding_request"}}, status=400)

    app.router.add_get("/healthz", health)
    app.router.add_get("/models", models)
    app.router.add_get("/metrics", metrics)
    app.router.add_post("/embed", embed)
    return app


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model-path", required=True)
    parser.add_argument("--host", action="append", default=None)
    parser.add_argument("--port", type=int, default=9225)
    parser.add_argument("--idle-seconds", type=float, default=1200)
    parser.add_argument("--timeout-seconds", type=float, default=300)
    parser.add_argument("--threads", type=int, default=2)
    parser.add_argument("--device", choices=("cpu", "mps", "cuda"), default="cpu")
    parser.add_argument("--allow-ip", action="append", default=[])
    parser.add_argument("--worker-python", default=None)
    args = parser.parse_args()
    if args.idle_seconds <= 0 or args.timeout_seconds <= 0 or args.threads <= 0:
        parser.error("timeouts and threads must be positive")
    hosts = args.host or ["127.0.0.1"]
    logging.basicConfig(level=logging.INFO, format="%(message)s")
    worker_command = [args.worker_python, "-m", "embedding_runtime.worker"] if args.worker_python else None
    config = Config(
        args.model_path,
        idle_seconds=args.idle_seconds,
        timeout_seconds=args.timeout_seconds,
        threads=args.threads,
        device=args.device,
        allow_ips=tuple(args.allow_ip),
        worker_command=worker_command,
    )
    # Peer disconnect cancels the handler, its queued job, or active worker.
    web.run_app(application(config), host=hosts if len(hosts) > 1 else hosts[0], port=args.port,
                handler_cancellation=True, access_log=None, print=None)


if __name__ == "__main__":
    main()
