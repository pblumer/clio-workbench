package server

import (
	"net/http"
	"strings"
	"testing"
)

// TestHandleIndexRendersShellAndAllSlots verifies the shell chrome plus every
// contributed View body renders without error and exposes the expected DOM
// slots/tabs. This also exercises the "partial" template func (used by the
// shell to render each View's body by name) indirectly.
func TestHandleIndexRendersShellAndAllSlots(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()

	// Analysis fragment slots (rendered by the View bodies in the shell).
	wantSlots := []string{
		"events-slot", "process-slot", "relations-slot",
		"environments-slot", "queries-slot", "conformance-result",
		`id="drafts"`, `id="inspector"`,
	}
	for _, w := range wantSlots {
		if !strings.Contains(body, w) {
			t.Errorf("index body missing %q", w)
		}
	}

	// Activity / editor / panel tabs (from contributions()).
	wantTabs := []string{"Forschung", "Modelle", "Umgebung", "Event Space", "Process", "Relationships", "Konformität", "Output"}
	for _, w := range wantTabs {
		if !strings.Contains(body, w) {
			t.Errorf("index body missing tab %q", w)
		}
	}

	// The status-bar stage badge.
	if !strings.Contains(body, "Stufe 0") {
		t.Errorf("index body missing stage badge")
	}
}

func TestHandleHealthz(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("healthz = %d %q", rec.Code, rec.Body.String())
	}
}

func TestHandleListDrafts(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/drafts", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `id="drafts"`) {
		t.Errorf("drafts fragment missing #drafts")
	}
}

func TestHandleCreateDraft(t *testing.T) {
	s := newTestServer(t, defaultCfg())

	// Named draft → slug id derived from the name.
	rec := s.do(http.MethodPost, "/drafts", form(map[string]string{"name": "My Order", "kind": "process"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d", rec.Code)
	}
	if _, err := s.store.Get("my-order"); err != nil {
		t.Fatalf("expected draft my-order: %v", err)
	}

	// Duplicate id → 409 Conflict.
	rec = s.do(http.MethodPost, "/drafts", form(map[string]string{"name": "My Order", "kind": "process"}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want 409", rec.Code)
	}

	// A name that slugifies to "" (only punctuation) but is non-empty → a
	// generated draft-<timestamp> id, while validation (name != "") passes.
	rec = s.do(http.MethodPost, "/drafts", form(map[string]string{"name": "###", "kind": "entity"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("punctuation-name create status = %d", rec.Code)
	}
	list, _ := s.store.List()
	foundGenerated := false
	for _, d := range list {
		if strings.HasPrefix(d.ID, "draft-") {
			foundGenerated = true
		}
	}
	if !foundGenerated {
		t.Errorf("expected a generated draft-* id")
	}
}

// TestHandleCreateDraftInvalidKind exercises userOrServerError's 400 mapping:
// an invalid kind yields a validation error containing "invalid".
func TestHandleCreateDraftInvalidKind(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodPost, "/drafts", form(map[string]string{"name": "Gamma", "kind": "bogus"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleCreateDraftBadForm(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	// A POST with a body but a content type that yields a parse error.
	req := mustBadForm(http.MethodPost, "/drafts")
	rec := s.do(req.method, req.target, req.body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleGetDraft(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	s.do(http.MethodPost, "/drafts", form(map[string]string{"name": "Alpha", "kind": "entity"}))

	rec := s.do(http.MethodGet, "/drafts/alpha", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"id": "alpha"`) {
		t.Errorf("body missing id:\n%s", rec.Body.String())
	}

	// Missing draft → 404.
	rec = s.do(http.MethodGet, "/drafts/nope", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing draft status = %d, want 404", rec.Code)
	}
}

func TestHandleDeleteDraft(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	s.do(http.MethodPost, "/drafts", form(map[string]string{"name": "Beta", "kind": "entity"}))

	rec := s.do(http.MethodDelete, "/drafts/beta", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}
	if _, err := s.store.Get("beta"); err == nil {
		t.Errorf("draft beta should be gone")
	}

	// Deleting again → 404.
	rec = s.do(http.MethodDelete, "/drafts/beta", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("re-delete status = %d, want 404", rec.Code)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My Order":        "my-order",
		"  trim/me  ":     "trim-me",
		"a__b--c":         "a-b-c",
		"Ünïcödé":         "ncd", // non-ascii dropped, no separators
		"":                "",
		"---":             "",
		"Hello World 123": "hello-world-123",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// badReq bundles a request whose body cannot be url-decoded so ParseForm fails.
type badReq struct {
	method, target string
	body           *badReader
}

func mustBadForm(method, target string) badReq {
	return badReq{method: method, target: target, body: &badReader{}}
}

// badReader always errors, so r.ParseForm() (which reads the body) fails.
type badReader struct{}

func (*badReader) Read([]byte) (int, error) { return 0, errBadBody }

var errBadBody = errReader("boom")

type errReader string

func (e errReader) Error() string { return string(e) }
