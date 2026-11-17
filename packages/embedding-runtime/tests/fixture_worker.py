"""A real disposable subprocess for supervisor tests; never loads model weights."""
import json
import os
import sys
import time

from embedding_runtime.contract import DIMENSIONS
assert 768 in DIMENSIONS

print('{"ready":true}', flush=True)
for line in sys.stdin:
    request = json.loads(line)
    command = request["items"][0].get("parts", [{}])[0].get("text", "")
    if command == "block":
        time.sleep(30)
    if command == "crash":
        os._exit(1)
    result = {"embeddings": [{"values": [1.0] + [0.0] * (i["dimensions"] - 1), "tokens": 3} for i in request["items"]]}
    print(json.dumps(result), flush=True)
