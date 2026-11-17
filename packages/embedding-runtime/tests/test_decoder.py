"""Uploaded media must not turn decoder auto-detection into a URL fetcher."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import base64
import importlib.util
import io
import shutil
import subprocess
import tempfile
import threading
import unittest

from embedding_runtime.worker import prepare_parts, probe


@unittest.skipUnless(importlib.util.find_spec("PIL") and importlib.util.find_spec("numpy"), "image decoder stack required")
class ImageOrientationTests(unittest.TestCase):
    def test_exif_orientation_matches_upright_pixels(self):
        from PIL import Image

        pixels = [(255, 0, 0), (0, 255, 0), (0, 0, 255),
                  (0, 255, 255), (255, 0, 255), (255, 255, 0)]
        source = Image.new("RGB", (3, 2))
        source.putdata(pixels)
        cases = {
            1: ((3, 2), [0, 1, 2, 3, 4, 5]),
            2: ((3, 2), [2, 1, 0, 5, 4, 3]),
            3: ((3, 2), [5, 4, 3, 2, 1, 0]),
            4: ((3, 2), [3, 4, 5, 0, 1, 2]),
            5: ((2, 3), [0, 3, 1, 4, 2, 5]),
            6: ((2, 3), [3, 0, 4, 1, 5, 2]),
            7: ((2, 3), [5, 2, 4, 1, 3, 0]),
            8: ((2, 3), [2, 5, 1, 4, 0, 3]),
        }
        for orientation, (size, order) in cases.items():
            with self.subTest(orientation=orientation), tempfile.TemporaryDirectory() as directory:
                exif = Image.Exif()
                exif[274] = orientation
                encoded = io.BytesIO()
                source.save(encoded, format="PNG", exif=exif)
                part = {"inlineData": {"mimeType": "image/png",
                                       "data": base64.b64encode(encoded.getvalue()).decode()}}
                result = prepare_parts([part], directory)["content"][0]["image"]
                self.assertEqual(result.mode, "RGB")
                self.assertEqual(result.size, size)
                actual = [result.getpixel((x, y)) for y in range(size[1]) for x in range(size[0])]
                self.assertEqual(actual, [pixels[i] for i in order])
                self.assertIn(result.getexif().get(274), (None, 1))


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
