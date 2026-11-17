"""Uploaded media must not turn decoder auto-detection into a URL fetcher."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import importlib.util
import shutil
import subprocess
import tempfile
import threading
import unittest

from embedding_runtime.worker import prepare_parts, probe


@unittest.skipUnless(shutil.which("ffprobe"), "ffmpeg required for decoder contract")
class DecoderTests(unittest.TestCase):
    @unittest.skipUnless(importlib.util.find_spec("PIL") and importlib.util.find_spec("numpy"), "full decoder stack required")
    def test_declared_inline_formats_still_decode(self):
        from embedding_runtime.benchmark import fixtures
        with tempfile.TemporaryDirectory() as directory:
            for name, part in fixtures(directory).items():
                with self.subTest(format=name), tempfile.TemporaryDirectory() as decoded:
                    value = prepare_parts([part], decoded)
                    self.assertEqual(len(value["content"]), 1)
                    expected = part["inlineData"]["mimeType"].split("/")[0]
                    self.assertEqual(value["content"][0]["type"], expected)

    def test_disguised_playlist_cannot_fetch_remote_segments(self):
        calls = []

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                calls.append(self.path)
                self.send_response(404)
                self.end_headers()

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as directory:
                upload = Path(directory) / "upload.mp4"
                upload.write_text("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\n"
                                  f"http://127.0.0.1:{server.server_port}/segment.ts\n#EXT-X-ENDLIST\n")
                with self.assertRaises(subprocess.SubprocessError):
                    probe(upload)
            self.assertEqual(calls, [], "decoder fetched a URI embedded in an upload")
        finally:
            server.shutdown()
            server.server_close()
            thread.join()
