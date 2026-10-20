package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
			proxyHandler(upstream).ServeHTTP(res, req)
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
			if err != nil || frontend.Scheme != "devtools" || frontend.Host != "devtools" || frontend.Path != "/bundled/inspector.html" || frontend.RawQuery != "" {
				t.Fatal("native inspection requires a frontend identifier without a blocked remote transport", target)
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
	proxyHandler(upstream).ServeHTTP(res, httptest.NewRequest("PUT", "http://browser.example/json/new?https://example.org", strings.NewReader("payload")))
	if res.Code != http.StatusCreated || res.Body.String() != "payload" {
		t.Fatal(res)
	}
}
