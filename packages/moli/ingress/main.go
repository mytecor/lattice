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
		// Moli does not ship a DevTools web frontend. Chrome's bundled
		// inspector also rejects arbitrary remote WebSockets through CSP.
		// Do not advertise a link that cannot connect; use chrome://inspect
		// through an SSH tunnel and its native "inspect fallback" action.
		delete(v, "devtoolsFrontendUrl")
		if raw, ok := v["webSocketDebuggerUrl"].(string); ok {
			if endpoint, err := url.Parse(raw); err == nil {
				endpoint.Host, endpoint.Scheme = host, scheme
				v["webSocketDebuggerUrl"] = endpoint.String()
			}
		}
	}
}

func proxyHandler(upstream *url.URL) http.Handler {
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/json/list", http.StatusTemporaryRedirect)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

func main() {
	listen := flag.String("listen", "127.0.0.1:9224", "loopback ingress address")
	backend := flag.String("upstream", "http://127.0.0.1:9223", "Moli address")
	flag.Parse()
	upstream, err := url.Parse(*backend)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: *listen, Handler: proxyHandler(upstream), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(server.ListenAndServe())
}
