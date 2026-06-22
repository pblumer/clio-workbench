package server

import (
	"net/http"
	"strings"
	"testing"
)

// minimalDraftJSON is a valid draft body for paste-import tests.
const minimalDraftJSON = `{
  "id": "pasted-order",
  "name": "Pasted Order",
  "kind": "entity",
  "namespace": "order",
  "nodes": [{"id": "cart", "label": "Cart", "start": true}],
  "edges": []
}`

// TestImportDraftPaste imports a draft from pasted JSON (no URL, no filesystem)
// — the SaaS path where the browser is the only access to the Workbench.
func TestImportDraftPaste(t *testing.T) {
	s := newTestServer(t, defaultCfg())

	rec := s.do(http.MethodPost, "/drafts/import", form(map[string]string{"json": minimalDraftJSON}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Pasted Order") {
		t.Errorf("response missing imported draft name; body=%s", rec.Body.String())
	}
	if _, err := s.store.Get("pasted-order"); err != nil {
		t.Errorf("draft not persisted: %v", err)
	}
}

// TestImportDraftPasteInvalidJSON surfaces a parse error inline rather than 500.
func TestImportDraftPasteInvalidJSON(t *testing.T) {
	s := newTestServer(t, defaultCfg())

	rec := s.do(http.MethodPost, "/drafts/import", form(map[string]string{"json": "{ not json"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (inline error); body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid JSON") {
		t.Errorf("expected inline invalid-JSON message; body=%s", rec.Body.String())
	}
}

// TestImportDraftNeither errors clearly when neither paste nor URL is given.
func TestImportDraftNeither(t *testing.T) {
	s := newTestServer(t, defaultCfg())

	rec := s.do(http.MethodPost, "/drafts/import", form(map[string]string{}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "paste JSON or provide an import URL") {
		t.Errorf("expected guidance message; body=%s", rec.Body.String())
	}
}

// TestImportScenarioPaste imports a suite from pasted JSON and snapshots the
// referenced draft's revision when the suite carries none.
func TestImportScenarioPaste(t *testing.T) {
	s := newTestServer(t, defaultCfg())

	// Import the model first so the suite can reference it.
	if rec := s.do(http.MethodPost, "/drafts/import", form(map[string]string{"json": minimalDraftJSON})); rec.Code != http.StatusOK {
		t.Fatalf("import draft: status %d", rec.Code)
	}

	suiteJSON := `{
  "id": "pasted-suite",
  "name": "Pasted Suite",
  "draftId": "pasted-order",
  "cases": []
}`
	rec := s.do(http.MethodPost, "/studio/scenarios/import",
		form(map[string]string{"json": suiteJSON, "draft": "pasted-order"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	su, err := s.scenarios.Get("pasted-suite")
	if err != nil {
		t.Fatalf("suite not persisted: %v", err)
	}
	if su.DraftRev == "" {
		t.Errorf("expected draftRev to be snapshotted from the referenced draft")
	}
}

// TestImportScenarioPasteKeepsRev leaves an explicit draftRev untouched.
func TestImportScenarioPasteKeepsRev(t *testing.T) {
	s := newTestServer(t, defaultCfg())

	suiteJSON := `{
  "id": "rev-suite",
  "name": "Rev Suite",
  "draftId": "pasted-order",
  "draftRev": "deadbeef",
  "cases": []
}`
	rec := s.do(http.MethodPost, "/studio/scenarios/import",
		form(map[string]string{"json": suiteJSON, "draft": "pasted-order"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	su, err := s.scenarios.Get("rev-suite")
	if err != nil {
		t.Fatalf("suite not persisted: %v", err)
	}
	if su.DraftRev != "deadbeef" {
		t.Errorf("draftRev = %q, want preserved %q", su.DraftRev, "deadbeef")
	}
}
