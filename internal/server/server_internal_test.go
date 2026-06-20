package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/config"
	"github.com/pblumer/clio-workbench/internal/envstore"
	"github.com/pblumer/clio-workbench/internal/scenario"
	"github.com/pblumer/clio-workbench/internal/store"
)

// newBrokenStoreServer returns a server whose store directory has been removed,
// so every store.List/Get read fails — exercising the serverError branches.
func newBrokenStoreServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
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
	srv, err := New(config.Config{Servers: []string{"x"}, EventCap: 10, DataDir: dir}, st, envs, scen, discardLogger())
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	// Remove the backing directory so List() returns a read error.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("rm dir: %v", err)
	}
	return srv
}

func TestStoreListFailuresAre500(t *testing.T) {
	cases := []struct{ method, target string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/drafts"},
		{http.MethodGet, "/studio/scenarios"},
	}
	for _, c := range cases {
		s := newBrokenStoreServer(t)
		rec := s.do(c.method, c.target, nil)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s status = %d, want 500", c.method, c.target, rec.Code)
		}
	}
}

// TestCreateDraftWriteFailure drives handleCreateDraft's userOrServerError 500
// branch: on a store whose directory has been removed, Create's write fails
// with a non-validation error.
func TestCreateDraftWriteFailure(t *testing.T) {
	s := newBrokenStoreServer(t)
	rec := s.do(http.MethodPost, "/drafts", form(map[string]string{"name": "X", "kind": "entity"}))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("create on broken store status = %d, want 500", rec.Code)
	}
}

// TestRenderUnknownTemplate exercises render's error path: a template name that
// does not exist makes ExecuteTemplate fail → serverError (500).
func TestRenderUnknownTemplate(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := httptest.NewRecorder()
	s.render(rec, "does-not-exist.html", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("render unknown template status = %d, want 500", rec.Code)
	}
}

// TestWriteJSONEncodeError drives writeJSON's encode-error branch with a value
// that cannot be marshalled (a channel).
func TestWriteJSONEncodeError(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := httptest.NewRecorder()
	s.writeJSON(rec, make(chan int)) // json: unsupported type → logs error
}

func TestServerError(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := httptest.NewRecorder()
	s.serverError(rec, "op", os.ErrInvalid)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("serverError status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "internal error") {
		t.Errorf("serverError body = %q", rec.Body.String())
	}
}

func TestUserOrServerErrorServerBranch(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := httptest.NewRecorder()
	// An error with no validation marker → 500 (server branch).
	s.userOrServerError(rec, "op", os.ErrPermission)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("userOrServerError status = %d, want 500", rec.Code)
	}
}

func TestStaticAssetsCacheControl(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	// Any /static/ request runs through cacheControl. The asset may or may not
	// exist, but the Cache-Control header is always set by the middleware.
	rec := s.do(http.MethodGet, "/static/favicon.svg", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("static asset status = %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Errorf("missing Cache-Control header, got %q", rec.Header().Get("Cache-Control"))
	}
}

func TestHandlerReturnsMux(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	if s.Handler() == nil {
		t.Fatalf("Handler() is nil")
	}
}
