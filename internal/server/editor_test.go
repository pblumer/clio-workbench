package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
)

// seedDraft creates a draft and returns its id.
func seedDraft(t *testing.T, s *Server, name string) string {
	t.Helper()
	rec := s.do(http.MethodPost, "/drafts", form(map[string]string{"name": name, "kind": "process"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("seed draft status = %d", rec.Code)
	}
	return slugify(name)
}

// firstStepID returns the id of a draft's first step.
func firstStepID(t *testing.T, s *Server, id string) string {
	t.Helper()
	d, err := s.store.Get(id)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if len(d.Steps) == 0 {
		t.Fatalf("draft %s has no steps", id)
	}
	return d.Steps[0].ID
}

func TestHandleEditor(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Editme")

	rec := s.do(http.MethodGet, "/editor/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("editor status = %d", rec.Code)
	}

	// Missing draft → 404.
	rec = s.do(http.MethodGet, "/editor/ghost", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing editor status = %d, want 404", rec.Code)
	}
}

func TestHandleSaveMeta(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Meta")

	rec := s.do(http.MethodPost, "/drafts/"+id+"/meta", form(map[string]string{
		"name": "New Name", "namespace": "ns.one", "subject": "/orders/{id}",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("meta status = %d", rec.Code)
	}
	d, _ := s.store.Get(id)
	if d.Name != "New Name" || d.Namespace != "ns.one" || d.SubjectStyle != "/orders/{id}" {
		t.Fatalf("meta not saved: %+v", d)
	}

	// Missing draft → 404.
	if rec := s.do(http.MethodPost, "/drafts/ghost/meta", form(map[string]string{"name": "x"})); rec.Code != http.StatusNotFound {
		t.Fatalf("missing draft meta status = %d", rec.Code)
	}
	// Bad form on an existing draft.
	if rec := s.do(http.MethodPost, "/drafts/"+id+"/meta", &badReader{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad form meta status = %d", rec.Code)
	}
}

func TestHandleAddStep(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Steps")

	// Default kind (event).
	rec := s.do(http.MethodPost, "/drafts/"+id+"/steps", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("add step status = %d", rec.Code)
	}
	// Explicit task kind.
	s.do(http.MethodPost, "/drafts/"+id+"/steps?kind=task", nil)
	// Unknown kind → coerced to event.
	s.do(http.MethodPost, "/drafts/"+id+"/steps?kind=bogus", nil)

	d, _ := s.store.Get(id)
	if len(d.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(d.Steps))
	}
	if d.Steps[0].Kind != model.StepEvent || d.Steps[1].Kind != model.StepTask || d.Steps[2].Kind != model.StepEvent {
		t.Errorf("step kinds = %v/%v/%v", d.Steps[0].Kind, d.Steps[1].Kind, d.Steps[2].Kind)
	}

	// Missing draft → 404.
	if rec := s.do(http.MethodPost, "/drafts/ghost/steps", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing draft add step = %d", rec.Code)
	}
}

func TestHandleUpdateStep(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Upd")
	s.do(http.MethodPost, "/drafts/"+id+"/steps", nil) // event
	sid := firstStepID(t, s, id)

	// Update with explicit phase.
	rec := s.do(http.MethodPost, "/drafts/"+id+"/steps/"+sid, form(map[string]string{
		"name": "order.created", "description": "desc", "phase": "complete",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("update step status = %d", rec.Code)
	}
	d, _ := s.store.Get(id)
	if d.Steps[0].Name != "order.created" || d.Steps[0].Phase != "complete" {
		t.Fatalf("step not updated: %+v", d.Steps[0])
	}

	// Update with empty phase but a name → phase inferred from name.
	s.do(http.MethodPost, "/drafts/"+id+"/steps/"+sid, form(map[string]string{
		"name": "order.shipped", "phase": "",
	}))
	d, _ = s.store.Get(id)
	if d.Steps[0].Phase == "" {
		t.Errorf("phase should be inferred from name")
	}

	// Unknown step id → no change, still 200.
	if rec := s.do(http.MethodPost, "/drafts/"+id+"/steps/nope", form(map[string]string{"name": "x"})); rec.Code != http.StatusOK {
		t.Fatalf("unknown step status = %d", rec.Code)
	}

	// Missing draft → 404; bad form → 400.
	if rec := s.do(http.MethodPost, "/drafts/ghost/steps/"+sid, form(map[string]string{"name": "x"})); rec.Code != http.StatusNotFound {
		t.Fatalf("missing draft update step = %d", rec.Code)
	}
	if rec := s.do(http.MethodPost, "/drafts/"+id+"/steps/"+sid, &badReader{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad form update step = %d", rec.Code)
	}
}

func TestHandleMoveStep(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Move")
	s.do(http.MethodPost, "/drafts/"+id+"/steps", nil)
	s.do(http.MethodPost, "/drafts/"+id+"/steps", nil)
	d, _ := s.store.Get(id)
	first, second := d.Steps[0].ID, d.Steps[1].ID

	// Move second up → swaps.
	rec := s.do(http.MethodPost, "/drafts/"+id+"/steps/"+second+"/move?dir=up", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("move up status = %d", rec.Code)
	}
	d, _ = s.store.Get(id)
	if d.Steps[0].ID != second {
		t.Errorf("move up did not swap")
	}

	// Move it down → back.
	s.do(http.MethodPost, "/drafts/"+id+"/steps/"+second+"/move?dir=down", nil)
	d, _ = s.store.Get(id)
	if d.Steps[0].ID != first {
		t.Errorf("move down did not swap back")
	}

	// Move first up (already top) → no-op boundary.
	s.do(http.MethodPost, "/drafts/"+id+"/steps/"+first+"/move?dir=up", nil)

	// Unknown step id → no-op.
	if rec := s.do(http.MethodPost, "/drafts/"+id+"/steps/nope/move", nil); rec.Code != http.StatusOK {
		t.Fatalf("unknown move status = %d", rec.Code)
	}
	// Missing draft → 404.
	if rec := s.do(http.MethodPost, "/drafts/ghost/steps/"+first+"/move", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing draft move = %d", rec.Code)
	}
}

func TestHandleDeleteStep(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Del")
	// Two steps so deleting one exercises the "keep the other" branch.
	s.do(http.MethodPost, "/drafts/"+id+"/steps", nil)
	s.do(http.MethodPost, "/drafts/"+id+"/steps", nil)
	sid := firstStepID(t, s, id)

	rec := s.do(http.MethodDelete, "/drafts/"+id+"/steps/"+sid, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete step status = %d", rec.Code)
	}
	d, _ := s.store.Get(id)
	if len(d.Steps) != 1 {
		t.Errorf("expected 1 remaining step, got %d", len(d.Steps))
	}

	// Missing draft → 404.
	if rec := s.do(http.MethodDelete, "/drafts/ghost/steps/"+sid, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing draft delete step = %d", rec.Code)
	}
}

func TestHandleFields(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Fields")
	s.do(http.MethodPost, "/drafts/"+id+"/steps", nil)
	sid := firstStepID(t, s, id)

	// Add a field.
	rec := s.do(http.MethodPost, "/drafts/"+id+"/steps/"+sid+"/fields", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("add field status = %d", rec.Code)
	}
	d, _ := s.store.Get(id)
	fid := d.Steps[0].Fields[0].ID

	// Update the field.
	rec = s.do(http.MethodPost, "/drafts/"+id+"/steps/"+sid+"/fields/"+fid, form(map[string]string{
		"name": "email", "type": "string", "required": "on", "format": "email",
		"ref": "users", "enum": "a,b,c",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("update field status = %d", rec.Code)
	}
	d, _ = s.store.Get(id)
	f := d.Steps[0].Fields[0]
	if f.Name != "email" || !f.Required || f.Format != "email" || f.Ref != "users" || len(f.Enum) != 3 {
		t.Fatalf("field not updated: %+v", f)
	}

	// Update unknown field id → no-op, still 200.
	if rec := s.do(http.MethodPost, "/drafts/"+id+"/steps/"+sid+"/fields/nope", form(map[string]string{"name": "x"})); rec.Code != http.StatusOK {
		t.Fatalf("unknown field update status = %d", rec.Code)
	}
	// Add field to unknown step → no-op (st == nil), still 200.
	if rec := s.do(http.MethodPost, "/drafts/"+id+"/steps/nope/fields", nil); rec.Code != http.StatusOK {
		t.Fatalf("add field to unknown step status = %d", rec.Code)
	}

	// Add a second field so deleting one exercises the "keep the other" branch.
	s.do(http.MethodPost, "/drafts/"+id+"/steps/"+sid+"/fields", nil)

	// Delete the first field.
	rec = s.do(http.MethodDelete, "/drafts/"+id+"/steps/"+sid+"/fields/"+fid, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete field status = %d", rec.Code)
	}
	d, _ = s.store.Get(id)
	if len(d.Steps[0].Fields) != 1 {
		t.Errorf("expected 1 remaining field, got %d", len(d.Steps[0].Fields))
	}
	// Delete on unknown step → no-op.
	if rec := s.do(http.MethodDelete, "/drafts/"+id+"/steps/nope/fields/"+fid, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete field unknown step status = %d", rec.Code)
	}

	// Error branches: missing draft / bad form.
	if rec := s.do(http.MethodPost, "/drafts/ghost/steps/"+sid+"/fields", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("add field missing draft = %d", rec.Code)
	}
	if rec := s.do(http.MethodPost, "/drafts/ghost/steps/"+sid+"/fields/"+fid, form(map[string]string{"name": "x"})); rec.Code != http.StatusNotFound {
		t.Fatalf("update field missing draft = %d", rec.Code)
	}
	if rec := s.do(http.MethodPost, "/drafts/"+id+"/steps/"+sid+"/fields/"+fid, &badReader{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("update field bad form = %d", rec.Code)
	}
	if rec := s.do(http.MethodDelete, "/drafts/ghost/steps/"+sid+"/fields/"+fid, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete field missing draft = %d", rec.Code)
	}
}

// TestEditorFocusIDs mirrors TestModelerFocusIDs for the outline editor: the
// step/field forms re-render the whole #proc-steps fragment on every change, so
// htmx needs a stable id on each control to restore the caret after the swap.
func TestEditorFocusIDs(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "EditFocus")
	sid := addStep(t, s, id, "event")
	s.do(http.MethodPost, "/drafts/"+id+"/steps/"+sid+"/fields", nil)
	d, _ := s.store.Get(id)
	fid := d.Steps[0].Fields[0].ID // default type "string" → the format qualifier renders

	body := s.do(http.MethodGet, "/editor/"+id, nil).Body.String()
	for _, want := range []string{
		`id="ed-step-name-` + sid + `"`,
		`id="ed-step-phase-` + sid + `"`,
		`id="ed-fld-name-` + fid + `"`,
		`id="ed-fld-type-` + fid + `"`,
		`id="ed-fld-req-` + fid + `"`,
		`id="ed-fld-format-` + fid + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("editor missing stable focus id %q", want)
		}
	}
}

func TestStepByIDAndIDGenerators(t *testing.T) {
	d := &model.Draft{Steps: []model.Step{{ID: "a"}, {ID: "b"}}}
	if stepByID(d, "b") == nil {
		t.Errorf("stepByID(b) should be found")
	}
	if stepByID(d, "zzz") != nil {
		t.Errorf("stepByID(zzz) should be nil")
	}
	if !strings.HasPrefix(newStepID(), "st") || len(newStepID()) != 12 {
		t.Errorf("newStepID malformed: %q", newStepID())
	}
	if !strings.HasPrefix(newFieldID(), "fl") {
		t.Errorf("newFieldID malformed: %q", newFieldID())
	}
}

func TestHandleExport(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Exp")

	rec := s.do(http.MethodGet, "/drafts/"+id+"/export/schemas", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export schemas status = %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), id+"-schemas.json") {
		t.Errorf("missing schemas filename header")
	}

	rec = s.do(http.MethodGet, "/drafts/"+id+"/export/bpmn", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export bpmn status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/xml" {
		t.Errorf("bpmn content-type = %q", ct)
	}

	// Missing draft → 404 on both.
	if rec := s.do(http.MethodGet, "/drafts/ghost/export/schemas", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("export schemas missing = %d", rec.Code)
	}
	if rec := s.do(http.MethodGet, "/drafts/ghost/export/bpmn", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("export bpmn missing = %d", rec.Code)
	}
}
