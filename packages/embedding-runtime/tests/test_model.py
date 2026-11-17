"""Real-weight acceptance, explicitly enabled on the target CPU only."""
import base64
import io
import os
import tempfile
import unittest
import wave


@unittest.skipUnless(os.environ.get("EMBEDDING_MODEL_PATH"), "real-weight acceptance is opt-in")
class ModelTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        from embedding_runtime.worker import Encoder
        cls.encoder = Encoder(os.environ["EMBEDDING_MODEL_PATH"], 3, 2)

    def encode(self, item):
        return self.encoder.encode({"items": [item]})["embeddings"][0]

    def test_exact_token_ids_reference_and_dimensions(self):
        import numpy as np
        from embedding_runtime.contract import prompt
        text = "A red bicycle stands beside a tree."
        baseline = self.encoder.model.encode(text, normalize_embeddings=True)
        actual = self.encode({"parts": [{"text": text}], "dimensions": 768})
        np.testing.assert_allclose(actual["values"], baseline, rtol=1e-5, atol=1e-6)
        ids = self.encoder.model.preprocess([text])["input_ids"][0].tolist()
        tokenized = self.encode({"token_ids": ids, "dimensions": 768})
        np.testing.assert_allclose(tokenized["values"], baseline, rtol=1e-5, atol=1e-6)
        for dimension in (128, 256, 512):
            reduced = self.encode({"parts": [{"text": text}], "dimensions": dimension})
            expected = baseline[:dimension] / np.linalg.norm(baseline[:dimension])
            np.testing.assert_allclose(reduced["values"], expected, rtol=1e-5, atol=1e-6)
        item = {"parts": [{"text": text}], "dimensions": 768, "task_type": "RETRIEVAL_DOCUMENT", "title": "Bicycle"}
        expected = self.encoder.model.encode(prompt(item) + text, normalize_embeddings=True)
        np.testing.assert_allclose(self.encode(item)["values"], expected, rtol=1e-5, atol=1e-6)

    def test_repeated_interleaved_images_match_direct_reference(self):
        import numpy as np
        from PIL import Image
        red, blue = Image.new("RGB", (64, 64), "red"), Image.new("RGB", (64, 64), "blue")
        def part(image):
            data = io.BytesIO()
            image.save(data, format="PNG")
            return {"inlineData": {"mimeType": "image/png", "data": base64.b64encode(data.getvalue()).decode()}}
        parts = [{"text": "First "}, part(red), {"text": " then "}, part(blue)]
        direct = {"role": "user", "content": [
            {"type": "text", "text": "First "}, {"type": "image", "image": red},
            {"type": "text", "text": " then "}, {"type": "image", "image": blue},
        ]}
        expected = self.encoder.model.encode(direct, normalize_embeddings=True)
        actual = self.encode({"parts": parts, "dimensions": 768})
        np.testing.assert_allclose(actual["values"], expected, rtol=1e-5, atol=1e-6)

    def test_audio_beyond_thirty_seconds_is_not_truncated(self):
        output = io.BytesIO()
        with wave.open(output, "wb") as audio:
            audio.setnchannels(1)
            audio.setsampwidth(2)
            audio.setframerate(16000)
            audio.writeframes(b"\0\0" * (16000 * 31))
        actual = self.encode({"parts": [{"inlineData": {"mimeType": "audio/wav", "data": base64.b64encode(output.getvalue()).decode()}}], "dimensions": 128})
        self.assertGreater(actual["tokens"], 752)

    def test_context_overflow_fails_before_forward(self):
        from embedding_runtime.contract import Failure
        with self.assertRaises(Failure) as raised:
            self.encode({"token_ids": [2] * 8193, "dimensions": 128})
        self.assertEqual(raised.exception.code, "context_length_exceeded")


if __name__ == "__main__":
    unittest.main()
