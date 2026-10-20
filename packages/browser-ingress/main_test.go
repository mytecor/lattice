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
			proxyHandler(upstream, http.Dir(t.TempDir()), "").ServeHTTP(res, req)
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
	proxyHandler(upstream, http.Dir(t.TempDir()), "").ServeHTTP(res, httptest.NewRequest("PUT", "http://browser.example/json/new?https://example.org", strings.NewReader("payload")))
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
	handler := proxyHandler(upstream, frontend, "")
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
				handler = browserHandler(selected, moli.URL, http.FS(fstest.MapFS{}))
			} else {
				handler = browserHandler(upstream, fox.URL, http.FS(fstest.MapFS{}))
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
		handler := browserHandler(upstream, foxbridge, http.FS(fstest.MapFS{}))
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
	}))
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
