package main

import (
	"bytes"
	"encoding/json"
	"flag"
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
func rewriteDiscovery(value any, host, scheme string) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			rewriteDiscovery(item, host, scheme)
		}
	case map[string]any:
		if raw, ok := v["webSocketDebuggerUrl"].(string); ok {
			if endpoint, err := url.Parse(raw); err == nil {
				endpoint.Host, endpoint.Scheme = host, scheme
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

func proxyHandler(upstream *url.URL, frontend http.FileSystem) http.Handler {
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
		rewriteDiscovery(value, r.Request.Host, scheme)
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
				http.Error(w, "Moli discovery unavailable", http.StatusBadGateway)
				return
			}
			defer response.Body.Close()
			var targets []struct {
				Type   string `json:"type"`
				Socket string `json:"webSocketDebuggerUrl"`
			}
			if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&targets) != nil {
				http.Error(w, "Invalid Moli discovery", http.StatusBadGateway)
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
				if r.Header.Get("X-Forwarded-Proto") == "https" {
					socket.Scheme = "wss"
				}
				http.Redirect(w, r, frontendURL(socket), http.StatusTemporaryRedirect)
				return
			}
			http.Error(w, "No Moli page targets", http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

func main() {
	listen := flag.String("listen", "127.0.0.1:9224", "loopback ingress address")
	backend := flag.String("upstream", "http://127.0.0.1:9223", "Moli address")
	frontend := flag.String("frontend", "", "hosted DevTools frontend directory")
	flag.Parse()
	if *frontend == "" {
		log.Fatal("--frontend is required")
	}
	upstream, err := url.Parse(*backend)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: *listen, Handler: proxyHandler(upstream, http.Dir(*frontend)), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(server.ListenAndServe())
}
