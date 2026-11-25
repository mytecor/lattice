"""Validation of the shared driverproto/embeddings JSON contract."""
import base64
import binascii

MAX_BYTES = 40 << 20
MAX_RESPONSE_BYTES = 4 << 20
MAX_ITEMS = 64
MAX_TOKENS = 8192
DIMENSIONS = (128, 256, 512, 768)
MIME = {
    "image/jpeg": (1, ".jpg"), "image/png": (1, ".png"), "image/webp": (1, ".webp"),
    "audio/wav": (2, ".wav"), "audio/x-wav": (2, ".wav"), "audio/mpeg": (2, ".mp3"),
    "audio/mp3": (2, ".mp3"), "audio/flac": (2, ".flac"), "audio/ogg": (2, ".ogg"),
    "video/mp4": (1, ".mp4"), "video/webm": (1, ".webm"),
}
PROMPTS = {
    "RETRIEVAL_QUERY": "task: search result | query: ",
    "SEMANTIC_SIMILARITY": "task: sentence similarity | query: ",
    "CLASSIFICATION": "task: classification | query: ",
    "CLUSTERING": "task: clustering | query: ",
    "QUESTION_ANSWERING": "task: question answering | query: ",
    "FACT_VERIFICATION": "task: fact checking | query: ",
    "CODE_RETRIEVAL_QUERY": "task: code retrieval | query: ",
}


class Failure(Exception):
    def __init__(self, code, status=400):
        super().__init__(code)
        self.code, self.status = code, status


def fields(value, allowed):
    if not isinstance(value, dict) or value.keys() - set(allowed):
        raise Failure("invalid_embedding_request")


def validate(request, model):
    fields(request, ("model", "items"))
    if request.get("model") != model:
        raise Failure("model_not_found", 404)
    items = request.get("items")
    if not isinstance(items, list) or not 1 <= len(items) <= MAX_ITEMS:
        raise Failure("invalid_embedding_request")
    profile = 0
    for item in items:
        fields(item, ("parts", "token_ids", "task_type", "title", "dimensions"))
        if type(item.get("dimensions")) is not int or item["dimensions"] not in DIMENSIONS:
            raise Failure("invalid_embedding_request")
        parts, tokens = item.get("parts"), item.get("token_ids")
        if (parts is not None) == (tokens is not None):
            raise Failure("invalid_embedding_request")
        media = False
        if tokens is not None:
            if not isinstance(tokens, list) or not 1 <= len(tokens) <= MAX_TOKENS or any(type(t) is not int or t < 0 for t in tokens):
                raise Failure("invalid_embedding_request")
        else:
            if not isinstance(parts, list) or not 1 <= len(parts) <= 128:
                raise Failure("invalid_embedding_request")
            for part in parts:
                fields(part, ("text", "inlineData"))
                if len(part) != 1:
                    raise Failure("invalid_embedding_request")
                if "text" in part:
                    if not isinstance(part["text"], str) or not part["text"]:
                        raise Failure("invalid_embedding_request")
                    continue
                blob = part["inlineData"]
                fields(blob, ("mimeType", "data"))
                if blob.get("mimeType") not in MIME:
                    raise Failure("unsupported_media", 415)
                if not isinstance(blob.get("data"), str) or not blob["data"]:
                    raise Failure("invalid_embedding_request")
                try:
                    base64.b64decode(blob["data"], validate=True)
                except (ValueError, binascii.Error):
                    raise Failure("invalid_embedding_request") from None
                profile |= MIME[blob["mimeType"]][0]
                media = True
        task, title = item.get("task_type", ""), item.get("title", "")
        if not isinstance(task, str) or task not in ("", "RETRIEVAL_DOCUMENT", *PROMPTS) or not isinstance(title, str):
            raise Failure("invalid_embedding_request")
        if (title and task != "RETRIEVAL_DOCUMENT") or (media and (task or title)):
            raise Failure("invalid_embedding_request")
    return profile


def prompt(item):
    if item.get("task_type") == "RETRIEVAL_DOCUMENT":
        return f"title: {item.get('title') or 'none'} | text: "
    return PROMPTS.get(item.get("task_type"), "")
