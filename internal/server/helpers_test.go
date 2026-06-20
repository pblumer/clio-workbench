package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/pblumer/clio-workbench/internal/config"
	"github.com/pblumer/clio-workbench/internal/envstore"
	"github.com/pblumer/clio-workbench/internal/scenario"
	"github.com/pblumer/clio-workbench/internal/store"
)

// discardLogger is a logger that throws everything away — tests don't care
// about log output, only behaviour.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestServer builds a Server backed by temporary draft/environment stores.
// The clio client starts unconfigured (offline) unless a test points it at an
// upstream via s.clio.SetTarget. cfg is taken as-is so a test can tune Servers,
// EventCap, DataDir, etc.
func newTestServer(t *testing.T, cfg config.Config) *Server {
	t.Helper()
	dir := t.TempDir()
	if cfg.DataDir == "" {
		cfg.DataDir = dir
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	envs, err := envstore.Open(dir)
	if err != nil {
		t.Fatalf("open envstore: %v", err)
	}
	scen, err := scenario.Open(dir)
	if err != nil {
		t.Fatalf("open scenario store: %v", err)
	}
	srv, err := New(cfg, st, envs, scen, discardLogger())
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return srv
}

// defaultCfg is a minimal usable config for tests.
func defaultCfg() config.Config {
	return config.Config{
		Servers:  []string{"https://clio.example"},
		EventCap: 1000,
	}
}

// fakeClio is a configurable httptest upstream Clio. Routes:
//
//	GET /api/v1/read-event-types  → probe (connection check)
//	GET /api/v1/info              → instance telemetry
//	GET /api/v1/events...         → NDJSON event stream (minimal or full)
//
// It can be told to return 401 (unauthorized), a non-2xx error, or arbitrary
// NDJSON bodies. The status field gates every route uniformly.
type fakeClio struct {
	server *httptest.Server
	// status, when non-zero and not 200, is returned for every request.
	status int
	// ndjson is written verbatim as the body of event reads.
	ndjson string
	// infoJSON is the body for /api/v1/info.
	infoJSON string
	// lastEventsQuery captures the raw query of the most recent events read.
	lastEventsQuery string
}

func newFakeClio(t *testing.T) *fakeClio {
	t.Helper()
	f := &fakeClio{
		status:   http.StatusOK,
		infoJSON: `{"eventsTotal":3,"activeObservers":1}`,
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.status != 0 && f.status != http.StatusOK {
			w.WriteHeader(f.status)
			return
		}
		switch {
		case r.URL.Path == "/api/v1/read-event-types":
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = io.WriteString(w, "")
		case r.URL.Path == "/api/v1/info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, f.infoJSON)
		case strings.HasPrefix(r.URL.Path, "/api/v1/events"):
			f.lastEventsQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = io.WriteString(w, f.ndjson)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

// connect points the server's clio client at the fake upstream.
func (f *fakeClio) connect(s *Server) {
	s.clio.SetTarget(f.server.URL, "tok")
}

// ndjsonLines joins event lines into an NDJSON body.
func ndjsonLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

// do runs a request through the server mux and returns the recorder.
func (s *Server) do(method, target string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, body)
	if body != nil && (method == http.MethodPost) {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// form encodes values for a POST body.
func form(vals map[string]string) io.Reader {
	v := url.Values{}
	for k, val := range vals {
		v.Set(k, val)
	}
	return strings.NewReader(v.Encode())
}

// ensure context import is used (helpers may pass contexts in some tests).
var _ = context.Background

// flushRecorder is an httptest.ResponseRecorder that also satisfies
// http.Flusher, so handlers that type-assert for SSE streaming run their live
// path. Writes are guarded by a mutex because the handler writes from a
// goroutine while the test reads.
type flushRecorder struct {
	mu  sync.Mutex
	rec *httptest.ResponseRecorder
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{rec: httptest.NewRecorder()}
}

func (f *flushRecorder) Header() http.Header { return f.rec.Header() }

func (f *flushRecorder) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rec.Write(p)
}

func (f *flushRecorder) WriteHeader(code int) { f.rec.WriteHeader(code) }

func (f *flushRecorder) Flush() {}

func (f *flushRecorder) body() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rec.Body.String()
}

// brokenWriter is an http.ResponseWriter whose Write always errors, used to
// drive the "connection gone" branch of writeSSE.
type brokenWriter struct{}

func (brokenWriter) Header() http.Header       { return http.Header{} }
func (brokenWriter) WriteHeader(int)           {}
func (brokenWriter) Write([]byte) (int, error) { return 0, errBadBody }

// failAfterFlusher is an http.Flusher ResponseWriter that succeeds for the
// first n writes, then fails — so a streaming handler reaches its mid-loop
// write-error branch after the initial setup writes.
type failAfterFlusher struct {
	mu        sync.Mutex
	header    http.Header
	remaining int
}

func newFailAfterFlusher(n int) *failAfterFlusher {
	return &failAfterFlusher{header: http.Header{}, remaining: n}
}

func (f *failAfterFlusher) Header() http.Header { return f.header }
func (f *failAfterFlusher) WriteHeader(int)     {}
func (f *failAfterFlusher) Flush()              {}

func (f *failAfterFlusher) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.remaining <= 0 {
		return 0, errBadBody
	}
	f.remaining--
	return len(p), nil
}
