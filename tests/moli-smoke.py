"""Real Moli discovery, WS upgrade, CDP execution, and rendering via ingress."""
import asyncio
import http.client
import json
import threading
import time
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit, parse_qs

from websockets.asyncio.client import connect
from websockets.exceptions import InvalidStatus


class AuthentikFixture(BaseHTTPRequestHandler):
    def do_GET(self):
        if urlsplit(self.path).path != "/outpost.goauthentik.io/auth/caddy":
            self.send_error(404)
            return
        if self.headers.get("Cookie") == "session=smoke":
            self.send_response(204)
        else:
            self.send_response(302)
            self.send_header("Location", "/login")
        self.end_headers()


def request_caddy(path, authorization=None, cookie=None, method="GET"):
    headers = {"Host": "browser.test:19225"}
    if authorization is not None:
        headers["Authorization"] = authorization
    if cookie is not None:
        headers["Cookie"] = cookie
    connection = http.client.HTTPConnection("127.0.0.1", 19225, timeout=2)
    try:
        connection.request(method, path, headers=headers)
        response = connection.getresponse()
        return response.status, response.getheader("WWW-Authenticate"), response.read()
    finally:
        connection.close()


def discover(path, scheme="http"):
    request = urllib.request.Request(
        "http://127.0.0.1:19224" + path,
        headers={"Host": "browser.test", "X-Forwarded-Proto": scheme},
    )
    with urllib.request.urlopen(request, timeout=2) as response:
        return json.load(response)


for attempt in range(100):
    try:
        version = discover("/moli/json/version")
        break
    except OSError:
        time.sleep(0.1)
else:
    raise RuntimeError("Moli did not become ready")

assert version["webSocketDebuggerUrl"].startswith("ws://browser.test/moli/")
assert discover("/moli/json/version", "https")["webSocketDebuggerUrl"].startswith("wss://browser.test/moli/")
targets = discover("/moli/json/list")
assert targets and all(t["webSocketDebuggerUrl"].startswith("ws://browser.test/moli/") for t in targets)
for target in targets:
    frontend = urlsplit(target["devtoolsFrontendUrl"])
    assert frontend.path == "/devtools/frontend/inspector.html"
    assert parse_qs(frontend.query)["ws"][0].startswith("browser.test/")

for path in ["/devtools/frontend/inspector.html", "/devtools/frontend/entrypoints/inspector/inspector.js"]:
    with urllib.request.urlopen("http://127.0.0.1:19224" + path, timeout=2) as response:
        assert response.status == 200
        assert response.read()


async def smoke():
    discovery_path = "/moli/json/version"
    for _ in range(100):
        try:
            status, _, _ = request_caddy(discovery_path, "Bearer smoke-key-a")
            if status == 200:
                break
        except OSError:
            pass
        await asyncio.sleep(0.1)
    else:
        raise RuntimeError("CDP discovery did not become ready through Caddy")

    for prefix in ("/moli", "/foxbridge"):
        discovery_path = prefix + "/json/version"
        for key in ("smoke-key-a", "smoke-key-b"):
            status, _, body = request_caddy(discovery_path, f"bEaReR {key}")
            assert status == 200, (prefix, status)
            browser_socket = json.loads(body)["webSocketDebuggerUrl"]
            assert browser_socket.startswith(f"ws://browser.test:19225{prefix}/devtools/browser/")
            assert key.encode() not in body
        assert request_caddy(discovery_path, "Bearer invalid")[:2] == (401, "Bearer")
        assert request_caddy(discovery_path, "Bearer")[:2] == (401, "Bearer")
        assert request_caddy(discovery_path)[0] == 302
        assert request_caddy(discovery_path, cookie="session=smoke")[0] == 200
        async with connect(browser_socket, host="127.0.0.1", additional_headers={"Authorization": "Bearer smoke-key-b"}, open_timeout=5):
            pass

    for path in ("/", "/moli/", "/devtools/frontend/inspector.html",
                 "/moli/devtools/frontend/%2e%2e/page/target"):
        assert request_caddy(path, "Bearer smoke-key-a")[0] == 302, path
    assert request_caddy("/moli/json/version?token=smoke-key-a")[0] == 302
    assert request_caddy("/devtools/frontend/inspector.html", cookie="session=smoke")[0] == 200
    assert request_caddy("/_cdp_auth", "Bearer smoke-key-a")[0] == 404
    status, _, body = request_caddy("/moli/json/new?about:blank", "Bearer smoke-key-a", method="PUT")
    assert status == 200 and json.loads(body)["url"] == "about:blank", status

    status, _, body = request_caddy("/moli/json/list", "Bearer smoke-key-a")
    assert status == 200, status
    endpoint = json.loads(body)[0]["webSocketDebuggerUrl"]
    try:
        async with connect(endpoint, host="127.0.0.1", additional_headers={"Authorization": "Bearer invalid"}, open_timeout=5):
            raise AssertionError("WebSocket accepted an invalid key")
    except InvalidStatus as error:
        assert error.response.status_code == 401

    async with connect(endpoint, host="127.0.0.1", additional_headers={"Authorization": "Bearer smoke-key-a"}, open_timeout=5) as socket:
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


with ThreadingHTTPServer(("127.0.0.1", 19226), AuthentikFixture) as authentik:
    threading.Thread(target=authentik.serve_forever, daemon=True).start()
    try:
        asyncio.run(smoke())
    finally:
        authentik.shutdown()
