package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/ExeQue/dbro/internal/mcpserver"
)

// headlessJS stands in for the Wails runtime in a browser: bound methods become POST /api/<Method>,
// events arrive over server-sent events, and the clipboard is the browser's.
const headlessJS = `(() => {
  const token = new URLSearchParams(location.search).get('token') ?? '';
  const call = (m) => async (...args) => {
    const r = await fetch('/api/' + m, { method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token }, body: JSON.stringify(args) });
    const j = await r.json().catch(() => ({ error: r.status + ' ' + r.statusText }));
    if (j.error !== undefined) throw j.error;
    return j.result;
  };
  window.go = { main: { App: new Proxy({}, { get: (_, m) => (m === 'CopyText' ? (t) => navigator.clipboard.writeText(t) : call(m)) }) } };
  const on = {};
  new EventSource('/events?token=' + encodeURIComponent(token)).onmessage = (e) => {
    const { event, data } = JSON.parse(e.data);
    for (const f of on[event] ?? []) f(data);
  };
  window.runtime = {
    EventsOnMultiple: (n, f) => {
      (on[n] ??= []).push(f);
      return () => (on[n] = on[n].filter((x) => x !== f));
    },
    EventsOff: (n) => delete on[n],
    BrowserOpenURL: (u) => window.open(u, '_blank', 'noopener'),
    LogInfo: () => {},
    LogError: () => {},
  };
})();
`

// hub fans events out to every connected browser.
type hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func (h *hub) emit(event string, data any) {
	b, err := json.Marshal(map[string]any{"event": event, "data": data})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.subs {
		select {
		case c <- b:
		default: // a slow client drops events rather than blocking queries
		}
	}
}

func (h *hub) serveEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	c := make(chan []byte, 256)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subs, c)
		h.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-c:
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		}
	}
}

var errorType = reflect.TypeFor[error]()

// rpc calls an exported App method with JSON arguments, like the Wails bridge does.
func rpc(app *App) http.HandlerFunc {
	v := reflect.ValueOf(app)
	return func(w http.ResponseWriter, r *http.Request) {
		m := v.MethodByName(strings.TrimPrefix(r.URL.Path, "/api/"))
		if r.Method != http.MethodPost || !m.IsValid() {
			http.NotFound(w, r)
			return
		}
		var raw []json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		t := m.Type()
		args := make([]reflect.Value, t.NumIn())
		for i := range args {
			p := reflect.New(t.In(i))
			if i < len(raw) {
				if err := json.Unmarshal(raw[i], p.Interface()); err != nil {
					http.Error(w, fmt.Sprintf("argument %d: %v", i+1, err), http.StatusBadRequest)
					return
				}
			}
			args[i] = p.Elem()
		}
		out := map[string]any{}
		for _, o := range m.Call(args) {
			if o.Type() == errorType {
				if !o.IsNil() {
					out["error"] = o.Interface().(error).Error()
				}
			} else {
				out["result"] = o.Interface()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

// guard protects the server. Writes must be same-origin. The API, events and MCP need the token
// when one is set (always beyond loopback), from "Authorization: Bearer" or ?token= (EventSource
// cannot send headers). Without a token, loopback is checked against DNS rebinding and the UI may
// only be framed by itself; with a token any application that knows it may embed the UI.
func guard(token string, loopback bool, next http.Handler) http.Handler {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			host, _, err := net.SplitHostPort(r.Host)
			if err != nil {
				host = r.Host
			}
			if loopback && host != "localhost" && host != "127.0.0.1" && host != "::1" {
				http.Error(w, "forbidden host", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
		} else if p := r.URL.Path; p == "/events" || p == "/mcp" || strings.HasPrefix(p, "/api/") {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok {
				got = r.URL.Query().Get("token")
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				http.Error(w, "unauthorized: open the URL dbro printed, or send Authorization: Bearer <token>", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
	return http.NewCrossOriginProtection().Handler(h)
}

// listen parses the address flag and works out the token: DBRO_TOKEN, or a fresh one beyond loopback.
func listen(args []string, name, def string) (addr, token string, loopback bool) {
	f := flag.NewFlagSet(name, flag.ExitOnError)
	f.StringVar(&addr, "addr", def, "address to listen on")
	f.Parse(args)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dbro:", err)
		os.Exit(2)
	}
	ip := net.ParseIP(host)
	loopback = host == "localhost" || ip != nil && ip.IsLoopback()
	token = os.Getenv("DBRO_TOKEN")
	if token == "" && !loopback {
		b := make([]byte, 16)
		rand.Read(b)
		token = hex.EncodeToString(b)
	}
	return addr, token, loopback
}

// serveHeadless runs the app without a window: the UI in a browser and MCP at /mcp.
func serveHeadless(args []string) error {
	addr, token, loopback := listen(args, "serve", "127.0.0.1:7810")
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return err
	}
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return err
	}
	index = []byte(strings.Replace(string(index), "<script", `<script src="/headless.js"></script><script`, 1))

	h := &hub{subs: map[chan []byte]struct{}{}}
	app := NewApp()
	app.headless = h.emit
	app.startup(context.Background())
	defer app.shutdown(context.Background())

	mux := http.NewServeMux()
	mux.Handle("/api/", rpc(app))
	mux.HandleFunc("/events", h.serveEvents)
	mux.Handle("/mcp", mcpserver.Handler(version))
	mux.HandleFunc("/headless.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write([]byte(headlessJS))
	})
	files := http.FileServerFS(dist)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(index)
			return
		}
		files.ServeHTTP(w, r)
	})

	url := "http://" + addr + "/"
	if token != "" {
		url += "?token=" + token
	}
	fmt.Fprintf(os.Stderr, "dbro serving on %s\n  UI:  %s\n  MCP: http://%s/mcp\n", addr, url, addr)
	if token == "" {
		fmt.Fprintln(os.Stderr, "  Set DBRO_TOKEN to embed the UI in other applications (iframe with ?token=...).")
	}
	return http.ListenAndServe(addr, guard(token, loopback, mux))
}

// serveMCP serves only MCP over streamable HTTP.
func serveMCP(args []string) error {
	addr, token, loopback := listen(args, "mcp", "127.0.0.1:7811")
	fmt.Fprintf(os.Stderr, "dbro MCP on http://%s/mcp\n", addr)
	if token != "" {
		fmt.Fprintf(os.Stderr, "  Authorization: Bearer %s\n", token)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpserver.Handler(version))
	return http.ListenAndServe(addr, guard(token, loopback, mux))
}
