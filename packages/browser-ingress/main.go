package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// Only protocol endpoint fields are rewritten. A page's URL/title/content must
// never change just because it contains the upstream's address.
func rewriteDiscovery(value any, host, scheme, prefix string) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			rewriteDiscovery(item, host, scheme, prefix)
		}
	case map[string]any:
		if raw, ok := v["webSocketDebuggerUrl"].(string); ok {
			if endpoint, err := url.Parse(raw); err == nil {
				endpoint.Host, endpoint.Scheme = host, scheme
				endpoint.Path = prefix + endpoint.Path
				endpoint.RawPath = ""
				v["webSocketDebuggerUrl"] = endpoint.String()
				if _, target := v["devtoolsFrontendUrl"]; target {
					v["devtoolsFrontendUrl"] = frontendURL(endpoint)
				}
			}
		}
	}
}

// The frontend and its WebSocket stay on the same site, so the browser can
// reuse the Authentik cookie for the upgrade without native DevTools CSP.
func frontendURL(socket *url.URL) string {
	query := url.Values{socket.Scheme: {socket.Host + socket.EscapedPath()}}
	return "/devtools/frontend/inspector.html?" + query.Encode()
}

func proxyHandler(upstream *url.URL, frontend http.FileSystem, prefix string) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		r.Header.Set("Accept-Encoding", "identity")
	}
	proxy.ModifyResponse = func(r *http.Response) error {
		if r.StatusCode != http.StatusOK || !strings.HasPrefix(r.Request.URL.Path, "/json") ||
			!strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			return nil
		}
		defer r.Body.Close()
		var value any
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			return err
		}
		scheme := "ws"
		// Only the loopback Caddy ingress can reach this listener.
		if r.Request.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "wss"
		}
		rewriteDiscovery(value, r.Request.Host, scheme, prefix)
		body, err := json.Marshal(value)
		if err != nil {
			return err
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Del("Content-Length")
		r.Header.Del("ETag")
		return nil
	}
	files := http.StripPrefix("/devtools/frontend/", http.FileServer(frontend))
	client := &http.Client{Timeout: 10 * time.Second}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/devtools/frontend/") {
			files.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/" {
			// Moli publishes its default target on discovery. Never assume a
			// target id or create a second browser just to open the inspector.
			endpoint := upstream.ResolveReference(&url.URL{Path: "/json/list"})
			response, err := client.Get(endpoint.String())
			if err != nil {
				http.Error(w, "Browser discovery unavailable", http.StatusBadGateway)
				return
			}
			defer response.Body.Close()
			var targets []struct {
				Type   string `json:"type"`
				Socket string `json:"webSocketDebuggerUrl"`
			}
			if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&targets) != nil {
				http.Error(w, "Invalid browser discovery", http.StatusBadGateway)
				return
			}
			for _, target := range targets {
				if target.Type != "page" {
					continue
				}
				socket, err := url.Parse(target.Socket)
				if err != nil || socket.Host == "" {
					continue
				}
				socket.Host, socket.Scheme = r.Host, "ws"
				socket.Path = prefix + socket.Path
				socket.RawPath = ""
				if r.Header.Get("X-Forwarded-Proto") == "https" {
					socket.Scheme = "wss"
				}
				http.Redirect(w, r, frontendURL(socket), http.StatusTemporaryRedirect)
				return
			}
			http.Error(w, "No browser page targets", http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

// Both backends share one authenticated site and the same hosted frontend.
// The public prefix is removed only when forwarding to the fixed loopback
// backend, and retained in discovery so clients never fall through to Moli.
func browserHandler(moli *url.URL, foxbridge string, frontend http.FileSystem) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/moli/", http.StripPrefix("/moli", proxyHandler(moli, frontend, "/moli")))
	if foxbridge != "" {
		upstream, err := url.Parse(foxbridge)
		if err != nil {
			panic(err)
		}
		mux.Handle("/foxbridge/", http.StripPrefix("/foxbridge", proxyHandler(upstream, frontend, "/foxbridge")))
	}
	mux.Handle("/devtools/frontend/", http.StripPrefix("/devtools/frontend/", http.FileServer(frontend)))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := browserIndex.Execute(w, foxbridge != ""); err != nil {
			log.Printf("render browser index: %v", err)
		}
	}))
	return mux
}

var browserIndex = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="ru">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Браузеры</title>
<style>
body { font: 18px system-ui, sans-serif; max-width: 42rem; margin: 10vh auto; padding: 0 1.5rem; color: #1f2937; background: #f8fafc; }
li { margin: 1rem 0; }
a { color: #1d4ed8; }
</style>
<h1>Браузеры</h1>
<p>Выберите браузер, чтобы открыть DevTools.</p>
<ul>
<li><a href="/moli/">Moli</a></li>
{{if .}}<li><a href="/foxbridge/">Foxbridge / Camoufox</a></li>{{end}}
</ul>
</html>`))

func main() {
	listen := flag.String("listen", "127.0.0.1:9224", "loopback ingress address")
	backend := flag.String("upstream", "http://127.0.0.1:9223", "Moli address")
	frontend := flag.String("frontend", "", "hosted DevTools frontend directory")
	foxbridge := flag.String("foxbridge", "", "optional Foxbridge HTTP upstream")
	flag.Parse()
	if *frontend == "" {
		log.Fatal("--frontend is required")
	}
	upstream, err := url.Parse(*backend)
	if err != nil {
		log.Fatal(err)
	}
	handler := browserHandler(upstream, *foxbridge, http.Dir(*frontend))
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(server.ListenAndServe())
}
