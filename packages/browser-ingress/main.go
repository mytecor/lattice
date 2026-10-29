package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
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

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func targetIDFromPath(path string) string {
	const prefix = "/devtools/page/"
	if strings.HasPrefix(path, prefix) {
		return strings.TrimPrefix(path, prefix)
	}
	return ""
}

type targetState struct {
	activeClients int
	lastActive    time.Time
}

type tracker struct {
	mu          sync.Mutex
	idleTimeout time.Duration
	states      map[string]*targetState
	upstreams   []*url.URL
	httpClient  *http.Client
	stopCh      chan struct{}
}

func newTracker(idleTimeout, checkInterval time.Duration, upstreams ...*url.URL) *tracker {
	t := &tracker{
		idleTimeout: idleTimeout,
		states:      make(map[string]*targetState),
		upstreams:   upstreams,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		stopCh:      make(chan struct{}),
	}
	if idleTimeout > 0 && checkInterval > 0 {
		go t.run(checkInterval)
	}
	return t
}

func (t *tracker) Close() {
	if t == nil || t.stopCh == nil {
		return
	}
	select {
	case <-t.stopCh:
	default:
		close(t.stopCh)
	}
}

func (t *tracker) clientConnected(upstream *url.URL, targetID string) {
	if t == nil || targetID == "" || upstream == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key := upstream.String() + "|" + targetID
	st, ok := t.states[key]
	if !ok {
		st = &targetState{}
		t.states[key] = st
	}
	st.activeClients++
	st.lastActive = time.Now()
}

func (t *tracker) clientDisconnected(upstream *url.URL, targetID string) {
	if t == nil || targetID == "" || upstream == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key := upstream.String() + "|" + targetID
	if st, ok := t.states[key]; ok {
		st.activeClients--
		if st.activeClients < 0 {
			st.activeClients = 0
		}
		st.lastActive = time.Now()
	}
}

func (t *tracker) run(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case now := <-ticker.C:
			t.reap(now)
		}
	}
}

type discoveryTarget struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	URL    string `json:"url"`
	Socket string `json:"webSocketDebuggerUrl"`
}

func (t *tracker) reap(now time.Time) {
	for _, upstream := range t.upstreams {
		endpoint := upstream.ResolveReference(&url.URL{Path: "/json/list"})
		resp, err := t.httpClient.Get(endpoint.String())
		if err != nil {
			continue
		}
		var targets []discoveryTarget
		err = json.NewDecoder(resp.Body).Decode(&targets)
		resp.Body.Close()
		if err != nil {
			continue
		}

		liveKeys := make(map[string]bool)
		for _, target := range targets {
			if target.Type != "page" {
				continue
			}
			key := upstream.String() + "|" + target.ID
			liveKeys[key] = true

			if target.URL == "about:blank" {
				t.mu.Lock()
				if st, ok := t.states[key]; ok && st.activeClients == 0 {
					st.lastActive = now
				}
				t.mu.Unlock()
				continue
			}

			t.mu.Lock()
			st, ok := t.states[key]
			if !ok {
				st = &targetState{lastActive: now}
				t.states[key] = st
			}
			if st.activeClients > 0 {
				st.lastActive = now
				t.mu.Unlock()
				continue
			}
			idle := now.Sub(st.lastActive) >= t.idleTimeout
			t.mu.Unlock()

			if idle {
				log.Printf("target %s (%s) idle for %v, resetting to about:blank", target.ID, target.URL, now.Sub(st.lastActive))
				socketURL := target.Socket
				if socketURL == "" {
					socketURL = fmt.Sprintf("ws://%s/devtools/page/%s", upstream.Host, target.ID)
				} else if u, err := url.Parse(socketURL); err == nil {
					u.Host = upstream.Host
					u.Scheme = "ws"
					socketURL = u.String()
				}
				if err := navigateToBlank(socketURL); err != nil {
					log.Printf("failed to reset target %s to about:blank: %v", target.ID, err)
				} else {
					t.mu.Lock()
					st.lastActive = now
					t.mu.Unlock()
				}
			}
		}

		t.mu.Lock()
		for key := range t.states {
			if strings.HasPrefix(key, upstream.String()+"|") && !liveKeys[key] {
				delete(t.states, key)
			}
		}
		t.mu.Unlock()
	}
}

func navigateToBlank(wsURL string) error {
	endpoint, err := url.Parse(wsURL)
	if err != nil {
		return err
	}
	host := endpoint.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	conn, err := net.DialTimeout("tcp", host, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	key := "dGhlIHNhbXBsZSBub25jZQ=="
	req := "GET " + endpoint.RequestURI() + " HTTP/1.1\r\n" +
		"Host: " + endpoint.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"

	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(req)); err != nil {
		return err
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("unexpected websocket status: %s", resp.Status)
	}

	payload := []byte(`{"id":1,"method":"Page.navigate","params":{"url":"about:blank"}}`)
	frame := make([]byte, 2+4+len(payload))
	frame[0] = 0x81
	frame[1] = 0x80 | byte(len(payload))
	mask := []byte{0x12, 0x34, 0x56, 0x78}
	copy(frame[2:6], mask)
	for i, b := range payload {
		frame[6+i] = b ^ mask[i%4]
	}

	if _, err := conn.Write(frame); err != nil {
		return err
	}

	closeFrame := []byte{0x88, 0x80, 0x00, 0x00, 0x00, 0x00}
	_, _ = conn.Write(closeFrame)
	return nil
}

func proxyHandler(upstream *url.URL, frontend http.FileSystem, prefix string, trk *tracker) http.Handler {
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
		if isWebSocketUpgrade(r) {
			if targetID := targetIDFromPath(r.URL.Path); targetID != "" && trk != nil {
				trk.clientConnected(upstream, targetID)
				defer trk.clientDisconnected(upstream, targetID)
			}
		}
		proxy.ServeHTTP(w, r)
	})
}

// Both backends share one authenticated site and the same hosted frontend.
// The public prefix is removed only when forwarding to the fixed loopback
// backend, and retained in discovery so clients never fall through to Moli.
func browserHandler(moli *url.URL, foxbridge string, frontend http.FileSystem, trk *tracker) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/moli/", http.StripPrefix("/moli", proxyHandler(moli, frontend, "/moli", trk)))
	if foxbridge != "" {
		upstream, err := url.Parse(foxbridge)
		if err != nil {
			panic(err)
		}
		mux.Handle("/foxbridge/", http.StripPrefix("/foxbridge", proxyHandler(upstream, frontend, "/foxbridge", trk)))
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
	idleTimeout := flag.Duration("idle-timeout", 20*time.Minute, "idle timeout before resetting inactive browser tabs to about:blank (0 disables)")
	checkInterval := flag.Duration("check-interval", 30*time.Second, "interval to check for idle browser tabs")
	flag.Parse()
	if *frontend == "" {
		log.Fatal("--frontend is required")
	}
	upstream, err := url.Parse(*backend)
	if err != nil {
		log.Fatal(err)
	}
	upstreams := []*url.URL{upstream}
	if *foxbridge != "" {
		if fbURL, err := url.Parse(*foxbridge); err == nil {
			upstreams = append(upstreams, fbURL)
		}
	}
	trk := newTracker(*idleTimeout, *checkInterval, upstreams...)
	defer trk.Close()
	handler := browserHandler(upstream, *foxbridge, http.Dir(*frontend), trk)
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(server.ListenAndServe())
}
