package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// corruptDraft writes invalid JSON to a draft's file so store.Get returns a
// decode error (not ErrNotFound), driving the serverError (500) branches.
func corruptDraft(t *testing.T, s *Server, id string) {
	t.Helper()
	p := filepath.Join(s.cfg.DataDir, id+".json")
	if err := os.WriteFile(p, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("corrupt draft: %v", err)
	}
}

func TestGetDraftDecodeErrorIs500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedDraft(t, s, "Corrupt")
	corruptDraft(t, s, "corrupt")

	rec := s.do(http.MethodGet, "/drafts/corrupt", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("get corrupt status = %d, want 500", rec.Code)
	}
}

func TestDeleteDraftListFailureIs500(t *testing.T) {
	// After a successful delete the handler re-lists; remove the dir between
	// delete and list is not possible without changing prod. Instead corrupt a
	// *second* draft so the post-delete List() fails to read it → 500.
	s := newTestServer(t, defaultCfg())
	seedDraft(t, s, "Keep")
	seedDraft(t, s, "Gone")
	corruptDraft(t, s, "keep") // List() will choke decoding this one

	rec := s.do(http.MethodDelete, "/drafts/gone", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("delete-then-list status = %d, want 500", rec.Code)
	}
}

func TestCreateDraftListFailureIs500(t *testing.T) {
	// Create succeeds, then List() chokes on a pre-existing corrupt draft → 500.
	s := newTestServer(t, defaultCfg())
	seedDraft(t, s, "Bad")
	corruptDraft(t, s, "bad")

	rec := s.do(http.MethodPost, "/drafts", form(map[string]string{"name": "Fresh", "kind": "entity"}))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("create-then-list status = %d, want 500", rec.Code)
	}
}

func TestEditorGetDecodeErrorIs500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedDraft(t, s, "Ec")
	corruptDraft(t, s, "ec")

	rec := s.do(http.MethodGet, "/editor/ec", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("editor decode error status = %d, want 500", rec.Code)
	}
}

// TestLoadDraftDecodeErrorIs500 drives loadDraft's serverError branch through a
// handler that uses it (handleAddStep).
func TestLoadDraftDecodeErrorIs500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedDraft(t, s, "Ld")
	corruptDraft(t, s, "ld")

	rec := s.do(http.MethodPost, "/drafts/ld/steps", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("loadDraft decode error status = %d, want 500", rec.Code)
	}
}

// TestSaveMetaInvalidNamespaceIs400 drives saveDraft's error branch: an invalid
// namespace makes store.Save fail validation → userOrServerError → 400.
func TestSaveMetaInvalidNamespaceIs400(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Sm")

	rec := s.do(http.MethodPost, "/drafts/"+id+"/meta", form(map[string]string{
		"name": "ok", "namespace": "bad namespace!!", // space + ! → invalid
	}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("save invalid namespace status = %d, want 400", rec.Code)
	}
}
