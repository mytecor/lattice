import asyncio
import base64
import os
from pathlib import Path
import sys
import unittest

from aiohttp import ClientSession, web

from embedding_runtime.contract import Failure, validate
from embedding_runtime.service import Config, Supervisor, application


def request(text="hello", mime=None):
    part = {"text": text} if mime is None else {"inlineData": {"mimeType": mime, "data": base64.b64encode(b"fixture").decode()}}
    return {"model": "google/embeddinggemma-2", "items": [{"parts": [part], "dimensions": 128}]}


async def eventually(predicate):
    for _ in range(300):
        if predicate():
            return
        await asyncio.sleep(0.01)
    raise AssertionError("state transition did not complete")


class RuntimeTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.clock = [0.0]
        self.config = Config("/unused", worker_command=[sys.executable, str(Path(__file__).with_name("fixture_worker.py"))])
        self.manager = Supervisor(self.config, clock=lambda: self.clock[0])
        await self.manager.start()

    async def asyncTearDown(self):
        await self.manager.close()

    async def test_cold_start_profile_upgrade_and_idle_expiry(self):
        results = await asyncio.gather(*(self.manager.submit(request()) for _ in range(5)))
        self.assertEqual(len(results), 5)
        self.assertEqual(self.manager.loads, 1)
        first_pid = self.manager.process.pid
        await self.manager.submit(request(mime="image/png"))
        self.assertEqual(self.manager.profile, 1)
        with self.assertRaises(ProcessLookupError):
            os.kill(first_pid, 0)
        await self.manager.submit(request(mime="audio/wav"))
        self.assertEqual(self.manager.profile, 3)
        await self.manager.submit(request())
        self.assertEqual(self.manager.loads, 3)
        self.clock[0] = 1199
        self.manager.wake.set()
        await asyncio.sleep(0.03)
        self.assertIsNotNone(self.manager.process)
        self.clock[0] = 1201
        self.manager.wake.set()
        await eventually(lambda: self.manager.process is None)
        await eventually(lambda: self.manager.unloads == 3)
        await self.manager.submit(request())
        self.assertEqual(self.manager.profile, 0)
        self.assertEqual(self.manager.loads, 4)

    async def test_queue_backpressure_queued_cancel_and_active_cancel(self):
        active = asyncio.create_task(self.manager.submit(request("block")))
        await eventually(lambda: self.manager.loads == 1)
        temporary = self.manager.temporary.name
        pending = [asyncio.create_task(self.manager.submit(request())) for _ in range(4)]
        await eventually(lambda: len(self.manager.queue) == 4)
        with self.assertRaises(Failure) as raised:
            await self.manager.submit(request())
        self.assertEqual(raised.exception.code, "queue_full")
        pending[0].cancel()
        await asyncio.gather(pending[0], return_exceptions=True)
        await eventually(lambda: len(self.manager.queue) == 3)
        # Idle expiry must not terminate active work even if time advances.
        self.clock[0] = 5000
        self.manager.wake.set()
        await asyncio.sleep(0.03)
        self.assertIsNotNone(self.manager.process)
        active.cancel()
        await asyncio.gather(active, return_exceptions=True)
        await asyncio.gather(*pending[1:])
        self.assertFalse(Path(temporary).exists())
        self.assertEqual(self.manager.loads, 2)

    async def test_crash_and_deadline_recover(self):
        with self.assertRaises(Failure) as raised:
            await self.manager.submit(request("crash"))
        self.assertEqual(raised.exception.code, "worker_failed")
        await self.manager.submit(request())
        self.config.timeout_seconds = 0.1
        with self.assertRaises(Failure) as raised:
            await self.manager.submit(request("block"))
        self.assertEqual(raised.exception.status, 504)
        await eventually(lambda: self.manager.active is None)
        self.config.timeout_seconds = 5
        await self.manager.submit(request())
        self.assertEqual(self.manager.loads, 3)

    async def test_health_catalog_metrics_do_not_load_or_reset_idle(self):
        await self.manager.close()
        self.manager = Supervisor(self.config, clock=lambda: self.clock[0])
        runner = web.AppRunner(application(self.config, self.manager), handler_cancellation=True)
        await runner.setup()
        site = web.TCPSite(runner, "127.0.0.1", 0)
        await site.start()
        base = "http://127.0.0.1:" + str(site._server.sockets[0].getsockname()[1])
        try:
            async with ClientSession() as client:
                for path in ("/healthz", "/models", "/metrics"):
                    async with client.get(base + path) as response:
                        self.assertEqual(response.status, 200)
                self.assertEqual(self.manager.loads, 0)
                async with client.post(base + "/embed", json=request()) as response:
                    self.assertEqual(response.status, 200)
                    self.assertEqual(len((await response.json())["embeddings"][0]["values"]), 128)
                last = self.manager.last_used
                self.clock[0] = 800
                async with client.get(base + "/models") as response:
                    await response.read()
                self.assertEqual(self.manager.last_used, last)
                self.assertNotIn("torch", sys.modules)
        finally:
            await runner.cleanup()

    async def test_http_disconnect_terminates_active_worker(self):
        await self.manager.close()
        self.manager = Supervisor(self.config)
        runner = web.AppRunner(application(self.config, self.manager), handler_cancellation=True)
        await runner.setup()
        site = web.TCPSite(runner, "127.0.0.1", 0)
        await site.start()
        base = "http://127.0.0.1:" + str(site._server.sockets[0].getsockname()[1])
        try:
            async with ClientSession() as client:
                pending = asyncio.create_task(client.post(base + "/embed", json=request("block")))
                await eventually(lambda: self.manager.loads == 1)
                pid = self.manager.process.pid
                pending.cancel()
                await asyncio.gather(pending, return_exceptions=True)
                await eventually(lambda: self.manager.active is None and self.manager.process is None)
                with self.assertRaises(ProcessLookupError):
                    os.kill(pid, 0)
        finally:
            await runner.cleanup()


class ContractTests(unittest.TestCase):
    def test_invalid_requests_and_profile_union(self):
        self.assertEqual(validate(request(), "google/embeddinggemma-2"), 0)
        for mime, expected in (("image/png", 1), ("video/webm", 1), ("audio/ogg", 2)):
            self.assertEqual(validate(request(mime=mime), "google/embeddinggemma-2"), expected)
        for invalid in ({}, {"model": "wrong", "items": []}, {"model": "google/embeddinggemma-2", "items": [None]}, request(mime="application/pdf")):
            with self.assertRaises(Failure):
                validate(invalid, "google/embeddinggemma-2")


if __name__ == "__main__":
    unittest.main()
