"""Real Moli discovery, WS upgrade, CDP execution, and rendering via ingress."""
import asyncio
import json
import time
import urllib.request

from websockets.asyncio.client import connect


def discover(path, scheme="http"):
    request = urllib.request.Request(
        "http://127.0.0.1:19224" + path,
        headers={"Host": "browser.test", "X-Forwarded-Proto": scheme},
    )
    with urllib.request.urlopen(request, timeout=2) as response:
        return json.load(response)


for attempt in range(100):
    try:
        version = discover("/json/version")
        break
    except OSError:
        time.sleep(0.1)
else:
    raise RuntimeError("Moli did not become ready")

assert version["webSocketDebuggerUrl"].startswith("ws://browser.test/")
assert discover("/json/version", "https")["webSocketDebuggerUrl"].startswith("wss://browser.test/")
targets = discover("/json/list")
assert targets and all(t["webSocketDebuggerUrl"].startswith("ws://browser.test/") for t in targets)
assert all(target.get("devtoolsFrontendUrl") == "devtools://devtools/bundled/inspector.html" for target in targets)


async def smoke():
    endpoint = targets[0]["webSocketDebuggerUrl"].replace("browser.test", "127.0.0.1:19224")
    async with connect(endpoint, open_timeout=5) as socket:
        async def command(number, method, params):
            await socket.send(json.dumps({"id": number, "method": method, "params": params}))
            while True:
                response = json.loads(await asyncio.wait_for(socket.recv(), 10))
                if response.get("id") == number:
                    assert "error" not in response, response
                    return response["result"]

        result = await command(1, "Runtime.evaluate", {"expression": "6 * 7", "returnByValue": True})
        assert result["result"]["value"] == 42, result
        image = await command(2, "Page.captureScreenshot", {"format": "png"})
        assert image["data"].startswith("iVBOR"), image


asyncio.run(smoke())
