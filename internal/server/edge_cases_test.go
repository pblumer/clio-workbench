package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/clio"
)

// --- proxy edge cases ---

func TestProxyNoTokenDropsAuth(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	c := clio.New(upstream.URL, "") // no token
	proxy := newProxy(c)

	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req.Header.Set("Authorization", "Bearer attacker") // must be dropped
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if gotAuth != "" {
		t.Errorf("auth should be dropped when no token set, got %q", gotAuth)
	}
}

func TestProxyUnparseableTargetIsHandled(t *testing.T) {
	c := clio.New("http://[::1:bad", "tok") // unparseable but non-empty → Configured
	proxy := newProxy(c)
	rec := httptest.NewRecorder()
	// Director returns early on the parse error; the proxy then fails the
	// request (no scheme/host). We only assert the handler does not panic.
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ping", nil))
	if rec.Code == http.StatusOK {
		t.Errorf("unparseable target should not yield 200")
	}
}

func TestSingleJoinSlashEmpty(t *testing.T) {
	// a == "/" and b == "" → "/".
	if got := singleJoin("/", ""); got != "/" {
		t.Errorf("singleJoin(/,\"\") = %q, want /", got)
	}
	if got := singleJoin("", ""); got != "/" {
		t.Errorf("singleJoin(\"\",\"\") = %q, want /", got)
	}
}

// --- handleDeleteDraft delete-error (non-NotFound) → 500 ---

func TestDeleteDraftRemoveErrorIs500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	// Create a *directory* at the draft's file path so os.Remove fails with a
	// non-ENOENT error (directory not empty), driving the serverError branch.
	id := "dirdraft"
	p := filepath.Join(s.cfg.DataDir, id+".json")
	if err := os.MkdirAll(filepath.Join(p, "child"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rec := s.do(http.MethodDelete, "/drafts/"+id, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("delete dir-path status = %d, want 500", rec.Code)
	}
}

// --- space stream: no-flusher and after-empty (currentMaxID) paths ---

// nonFlusher is a ResponseWriter that is NOT an http.Flusher.
type nonFlusher struct{ h http.Header }

func (n nonFlusher) Header() http.Header     { return n.h }
func (nonFlusher) Write([]byte) (int, error) { return 0, nil }
func (nonFlusher) WriteHeader(int)           {}

func TestHandleSpaceStreamNoFlusher(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.connect(s)

	w := nonFlusher{h: http.Header{}}
	req := httptest.NewRequest(http.MethodGet, "/space/stream", nil)
	// Call the handler directly with a writer that does not implement Flusher.
	s.handleSpaceStream(w, req)
	// No panic and no flush path taken is the assertion (coverage of the guard).
}

// TestHandleSpaceStreamAfterEmptyResolvesTail drives the after=="" branch where
// the stream anchors at the current tail via currentMaxID, then cancels.
func TestHandleSpaceStreamAfterEmptyResolvesTail(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = ndjsonLines(`{"id":"010","subject":"/o/1","type":"created","time":"t"}`)
	f.connect(s)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/space/stream", nil).WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		s.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	// Cancel almost immediately; we only need the setup (flush + anchor) covered.
	cancel()
	<-done

	if !strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Errorf("stream content-type = %q", rec.Header().Get("Content-Type"))
	}
}
