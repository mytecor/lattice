package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestDiscovery(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("compressed discovery requested")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[{"url":"http://127.0.0.1:9223/keep","title":"127.0.0.1:9223","webSocketDebuggerUrl":"ws://127.0.0.1:9223/devtools/page/42","devtoolsFrontendUrl":"/devtools/inspector.html?ws=127.0.0.1:9223/devtools/page/42"}]`)
	}))
	defer backend.Close()
	upstream, _ := url.Parse(backend.URL)
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://browser.example/json/list?ignored=true", nil)
			req.Header.Set("X-Forwarded-Proto", scheme)
			res := httptest.NewRecorder()
			proxyHandler(upstream, http.Dir(t.TempDir()), "", nil).ServeHTTP(res, req)
			if res.Code != 200 {
				t.Fatal(res.Code, res.Body.String())
			}
			var targets []map[string]string
			if err := json.Unmarshal(res.Body.Bytes(), &targets); err != nil {
				t.Fatal(err)
			}
			target := targets[0]
			ws := "ws"
			if scheme == "https" {
				ws = "wss"
			}
			if target["webSocketDebuggerUrl"] != ws+"://browser.example/devtools/page/42" {
				t.Fatal(target)
			}
			frontend, err := url.Parse(target["devtoolsFrontendUrl"])
			if err != nil || frontend.Host != "" || frontend.Path != "/devtools/frontend/inspector.html" || frontend.Query().Get(ws) != "browser.example/devtools/page/42" {
				t.Fatal("hosted inspection must use the same external WebSocket endpoint", target)
			}
			if target["url"] != "http://127.0.0.1:9223/keep" || target["title"] != "127.0.0.1:9223" {
				t.Fatal("page data mutated", target)
			}
		})
	}
}

func TestPassThrough(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.RequestURI() != "/json/new?https://example.org" {
			t.Error("request changed", r.Method, r.URL)
		}
		w.WriteHeader(http.StatusCreated)
		io.Copy(w, r.Body)
	}))
	defer backend.Close()
	upstream, _ := url.Parse(backend.URL)
	res := httptest.NewRecorder()
	proxyHandler(upstream, http.Dir(t.TempDir()), "", nil).ServeHTTP(res, httptest.NewRequest("PUT", "http://browser.example/json/new?https://example.org", strings.NewReader("payload")))
	if res.Code != http.StatusCreated || res.Body.String() != "payload" {
		t.Fatal(res)
	}
}

// An inspector link must load real assets and point its socket at the same
// public site. CDP paths must remain proxy routes, not static-file routes.
func TestHostedInspector(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/list" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"type":"page","webSocketDebuggerUrl":"ws://127.0.0.1:9223/devtools/page/dynamic-id"}]`)
			return
		}
		w.WriteHeader(http.StatusTeapot)
	}))
	defer backend.Close()
	upstream, _ := url.Parse(backend.URL)
	frontend := http.FS(fstest.MapFS{"inspector.html": {Data: []byte("official inspector")}})
	handler := proxyHandler(upstream, frontend, "", nil)
	for _, scheme := range []string{"http", "https"} {
		request := httptest.NewRequest("GET", "http://browser.example/", nil)
		request.Header.Set("X-Forwarded-Proto", scheme)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusTemporaryRedirect {
			t.Fatal(response.Code, response.Body)
		}
		link, err := url.Parse(response.Header().Get("Location"))
		ws := "ws"
		if scheme == "https" {
			ws = "wss"
		}
		if err != nil || link.Host != "" || link.Path != "/devtools/frontend/inspector.html" || link.Query().Get(ws) != "browser.example/devtools/page/dynamic-id" {
			t.Fatal("invalid inspector redirect", link)
		}
	}
	for _, path := range []string{"/devtools/frontend/inspector.html", "/devtools/frontend/missing.js", "/devtools/page/dynamic-id"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "http://browser.example"+path, nil))
		expected := map[string]int{"/devtools/frontend/inspector.html": 200, "/devtools/frontend/missing.js": 404, "/devtools/page/dynamic-id": 418}[path]
		if response.Code != expected {
			t.Fatal(path, response.Code, response.Body)
		}
	}
}

func TestBackendRouteKeepsDiscoveryOnSelectedBackend(t *testing.T) {
	for _, prefix := range []string{"/moli", "/foxbridge"} {
		t.Run(prefix, func(t *testing.T) {
			moli := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }))
			defer moli.Close()
			fox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/json/list" {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, `[{"type":"page","devtoolsFrontendUrl":"","webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/page/fox-target"}]`)
					return
				}
				if r.URL.Path == "/devtools/page/fox-target" {
					w.WriteHeader(http.StatusAccepted)
					return
				}
				http.NotFound(w, r)
			}))
			defer fox.Close()
			upstream, _ := url.Parse(moli.URL)
			var handler http.Handler
			if prefix == "/moli" {
				selected, _ := url.Parse(fox.URL)
				handler = browserHandler(selected, moli.URL, http.FS(fstest.MapFS{}), nil)
			} else {
				handler = browserHandler(upstream, fox.URL, http.FS(fstest.MapFS{}), nil)
			}
			alias := httptest.NewRecorder()
			handler.ServeHTTP(alias, httptest.NewRequest("GET", "http://browser.example"+prefix, nil))
			if alias.Code < 300 || alias.Code >= 400 || alias.Header().Get("Location") != prefix+"/" {
				t.Fatal("invalid backend alias", alias.Code, alias.Header())
			}
			for _, scheme := range []string{"http", "https"} {
				request := httptest.NewRequest("GET", "http://browser.example"+prefix+"/json/list", nil)
				request.Header.Set("X-Forwarded-Proto", scheme)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				var targets []map[string]string
				if err := json.Unmarshal(response.Body.Bytes(), &targets); err != nil {
					t.Fatal(response.Code, response.Body)
				}
				ws := "ws"
				if scheme == "https" {
					ws = "wss"
				}
				if targets[0]["webSocketDebuggerUrl"] != ws+"://browser.example"+prefix+"/devtools/page/fox-target" {
					t.Fatal(targets)
				}
				link, _ := url.Parse(targets[0]["devtoolsFrontendUrl"])
				if link.Query().Get(ws) != "browser.example"+prefix+"/devtools/page/fox-target" {
					t.Fatal(link)
				}
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", "http://browser.example"+prefix+"/", nil))
			link, _ := url.Parse(response.Header().Get("Location"))
			if response.Code != 307 || link.Query().Get("ws") != "browser.example"+prefix+"/devtools/page/fox-target" {
				t.Fatal(response.Code, link)
			}
			for path, status := range map[string]int{prefix + "/devtools/page/fox-target": 202} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest("GET", "http://browser.example"+path, nil))
				if response.Code != status {
					t.Fatal(path, response.Code)
				}
			}
		})
	}
}

func TestBrowserIndexDoesNotContactBackends(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("index contacted a browser", r.URL)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer backend.Close()
	upstream, _ := url.Parse(backend.URL)
	for _, foxbridge := range []string{"", backend.URL} {
		handler := browserHandler(upstream, foxbridge, http.FS(fstest.MapFS{}), nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "http://browser.example/", nil))
		body := response.Body.String()
		if response.Code != http.StatusOK || response.Header().Get("Location") != "" || !strings.Contains(body, `href="/moli/"`) {
			t.Fatal(response.Code, response.Header(), body)
		}
		if strings.Contains(body, `href="/foxbridge/"`) != (foxbridge != "") {
			t.Fatal("index must only offer configured browsers", body)
		}
	}
}

func TestUnprefixedEndpointsAreNotProxied(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unprefixed request reached backend", r.URL)
	}))
	defer backend.Close()
	upstream, _ := url.Parse(backend.URL)
	handler := browserHandler(upstream, backend.URL, http.FS(fstest.MapFS{
		"inspector.html": {Data: []byte("official inspector")},
	}), nil)
	for _, path := range []string{"/json", "/json/list", "/json/version", "/json/new?about:blank", "/devtools/page/target", "/devtools/browser/target", "/unknown"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "http://browser.example"+path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatal(path, response.Code)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "http://browser.example/devtools/frontend/inspector.html", nil))
	if response.Code != http.StatusOK || response.Body.String() != "official inspector" {
		t.Fatal("shared frontend unavailable", response.Code, response.Body)
	}
}

func TestIdleTargetReset(t *testing.T) {
	navigated := make(chan string, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/list" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"id":"tab-1","type":"page","url":"https://github.com/","webSocketDebuggerUrl":"ws://`+r.Host+`/devtools/page/tab-1"}]`)
			return
		}
		if r.URL.Path == "/devtools/page/tab-1" && isWebSocketUpgrade(r) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijack not supported", 500)
				return
			}
			conn, brw, err := hj.Hijack()
			if err != nil {
				return
			}
			defer conn.Close()
			brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n")
			brw.Flush()

			hdr := make([]byte, 2)
			if _, err := io.ReadFull(brw, hdr); err != nil {
				return
			}
			isMasked := (hdr[1] & 0x80) != 0
			length := int(hdr[1] & 0x7F)
			var mask [4]byte
			if isMasked {
				io.ReadFull(brw, mask[:])
			}
			payload := make([]byte, length)
			io.ReadFull(brw, payload)
			if isMasked {
				for i := range payload {
					payload[i] ^= mask[i%4]
				}
			}
			navigated <- string(payload)
			return
		}
		http.NotFound(w, r)
	}))
	defer backend.Close()

	u, _ := url.Parse(backend.URL)
	tr := newTracker(50*time.Millisecond, 15*time.Millisecond, u)
	defer tr.Close()

	select {
	case msg := <-navigated:
		if !strings.Contains(msg, "Page.navigate") || !strings.Contains(msg, "about:blank") {
			t.Fatalf("unexpected navigation message: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for idle reset to about:blank")
	}
}

func TestActiveClientPreventsReset(t *testing.T) {
	navigated := make(chan string, 10)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/list" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"id":"tab-2","type":"page","url":"https://github.com/","webSocketDebuggerUrl":"ws://`+r.Host+`/devtools/page/tab-2"}]`)
			return
		}
		if r.URL.Path == "/devtools/page/tab-2" && isWebSocketUpgrade(r) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijack not supported", 500)
				return
			}
			conn, brw, err := hj.Hijack()
			if err != nil {
				return
			}
			defer conn.Close()
			brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n")
			brw.Flush()

			hdr := make([]byte, 2)
			if _, err := io.ReadFull(brw, hdr); err != nil {
				return
			}
			isMasked := (hdr[1] & 0x80) != 0
			length := int(hdr[1] & 0x7F)
			var mask [4]byte
			if isMasked {
				io.ReadFull(brw, mask[:])
			}
			payload := make([]byte, length)
			io.ReadFull(brw, payload)
			if isMasked {
				for i := range payload {
					payload[i] ^= mask[i%4]
				}
			}
			navigated <- string(payload)
			return
		}
		http.NotFound(w, r)
	}))
	defer backend.Close()

	u, _ := url.Parse(backend.URL)
	tr := newTracker(80*time.Millisecond, 20*time.Millisecond, u)
	defer tr.Close()

	// Simulate active client connected to tab-2
	tr.clientConnected(u, "tab-2")

	// Wait past idle timeout while client is active
	time.Sleep(120 * time.Millisecond)
	select {
	case msg := <-navigated:
		t.Fatalf("target was reset while client was active: %s", msg)
	default:
	}

	// Client disconnects
	tr.clientDisconnected(u, "tab-2")

	// Now wait for idle timeout after disconnect
	select {
	case msg := <-navigated:
		if !strings.Contains(msg, "Page.navigate") || !strings.Contains(msg, "about:blank") {
			t.Fatalf("unexpected navigation message: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reset after disconnect")
	}
}
