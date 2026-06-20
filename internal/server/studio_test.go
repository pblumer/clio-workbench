package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
)

// seedStudioDraft creates a draft with one named event step carrying a few
// fields, so the schema-test view has something to check against.
func seedStudioDraft(t *testing.T, s *Server) *model.Draft {
	t.Helper()
	d := &model.Draft{
		ID:        "order",
		Name:      "Order",
		Kind:      model.KindEntity,
		Namespace: "order",
		Steps: []model.Step{
			{ID: "st1", Kind: model.StepEvent, Name: "order-placed", Fields: []model.Field{
				{ID: "f1", Name: "id", Type: "reference", Format: "uuid", Required: true},
				{ID: "f2", Name: "amount", Type: "number", Required: true},
			}},
			{ID: "st2", Kind: model.StepTask, Name: "ship"}, // task, must be ignored
			{ID: "st3", Kind: model.StepEvent, Name: ""},    // unnamed, must be ignored
		},
	}
	if err := s.store.Create(d); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	return d
}

func TestStudioSchemaForm(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedStudioDraft(t, s)

	rec := s.do(http.MethodGet, "/studio/schema-test", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Order", "order-placed", `name="data"`} {
		if !strings.Contains(body, want) {
			t.Errorf("form missing %q\n%s", want, body)
		}
	}
	// The task step and the unnamed event step must not appear as options.
	if strings.Contains(body, ">ship<") {
		t.Errorf("task step leaked into event-type options")
	}
}

func TestStudioSchemaFormNoDrafts(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/studio/schema-test", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Noch keine Modelle") {
		t.Fatalf("expected empty-state hint, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStudioSchemaFieldsFragment(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedStudioDraft(t, s)
	rec := s.do(http.MethodGet, "/studio/schema-test/fields?draft=order", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "order-placed") || !strings.Contains(body, "date-time") && !strings.Contains(body, "uuid") {
		t.Errorf("fields fragment missing event type / schema preview:\n%s", body)
	}
}

func TestStudioSchemaCheck(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedStudioDraft(t, s)

	tests := []struct {
		name     string
		data     string
		wantOK   bool
		contains string
	}{
		{"valid", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":9.99}`, true, "konform"},
		{"missing required", `{"amount":1}`, false, "required"},
		{"bad uuid", `{"id":"nope","amount":1}`, false, "format"},
		{"bad type", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":"x"}`, false, "type"},
		{"invalid json", `{not json`, false, "not valid JSON"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := s.do(http.MethodPost, "/studio/schema-test", form(map[string]string{
				"draft": "order", "step": "st1", "data": tc.data,
			}))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			body := rec.Body.String()
			if tc.wantOK && !strings.Contains(body, "konform") {
				t.Errorf("expected conform result, got: %s", body)
			}
			if !strings.Contains(body, tc.contains) {
				t.Errorf("result missing %q:\n%s", tc.contains, body)
			}
		})
	}
}

func TestStudioSchemaCheckUnknownDraft(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodPost, "/studio/schema-test", form(map[string]string{
		"draft": "ghost", "step": "st1", "data": "{}",
	}))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "choose a model") {
		t.Fatalf("expected model-not-found note, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStudioSchemaCheckUnknownStep(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedStudioDraft(t, s)
	rec := s.do(http.MethodPost, "/studio/schema-test", form(map[string]string{
		"draft": "order", "step": "ghost", "data": "{}",
	}))
	if !strings.Contains(rec.Body.String(), "choose an event type") {
		t.Fatalf("expected event-type note, got: %s", rec.Body.String())
	}
}

func TestBuildStudioSchemaDefaults(t *testing.T) {
	drafts := []model.Draft{
		{ID: "a", Name: "A", Steps: []model.Step{{ID: "e1", Kind: model.StepEvent, Name: "created"}}},
		{ID: "b", Name: "B"},
	}
	// Empty selectors default to the first draft and its first event step.
	v := buildStudioSchema(drafts, "", "")
	if v.DraftID != "a" || v.StepID != "e1" {
		t.Fatalf("defaults: draft=%q step=%q", v.DraftID, v.StepID)
	}
	// A draft without event steps yields no steps and an empty schema.
	v = buildStudioSchema(drafts, "b", "")
	if len(v.Steps) != 0 || v.Schema != "" {
		t.Fatalf("draft b should have no steps, got %+v", v)
	}
	// An unknown draft id resolves to nil selection (no steps).
	if v := buildStudioSchema(drafts, "ghost", ""); len(v.Steps) != 0 {
		t.Fatalf("ghost draft should have no steps")
	}
	// A known draft but unknown step id leaves the schema empty (findStep nil).
	if v := buildStudioSchema(drafts, "a", "ghoststep"); v.Schema != "" {
		t.Fatalf("unknown step should yield empty schema, got %q", v.Schema)
	}
	// An explicit, matching step id selects that step (findStep loop match).
	if v := buildStudioSchema(drafts, "a", "e1"); v.StepID != "e1" || v.Schema == "" {
		t.Fatalf("explicit step: step=%q schema=%q", v.StepID, v.Schema)
	}
}

func TestStudioSchemaCheckBadForm(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	// An invalid percent-escape in the body makes ParseForm fail → 400.
	rec := s.do(http.MethodPost, "/studio/schema-test", strings.NewReader("data=%zz"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad form status = %d, want 400", rec.Code)
	}
}

// The store-error branches mirror the package's existing corruptDraft tests:
// a draft whose file is invalid JSON makes List()/Get() fail with a decode
// error (not ErrNotFound), which the handlers surface as 500.

func TestStudioSchemaListFailureIs500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedStudioDraft(t, s)
	corruptDraft(t, s, "order")
	for _, target := range []string{"/studio/schema-test", "/studio/schema-test/fields"} {
		if rec := s.do(http.MethodGet, target, nil); rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s status = %d, want 500", target, rec.Code)
		}
	}
}

func TestStudioSchemaCheckDecodeErrorIs500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedStudioDraft(t, s)
	corruptDraft(t, s, "order")
	rec := s.do(http.MethodPost, "/studio/schema-test", form(map[string]string{
		"draft": "order", "step": "st1", "data": "{}",
	}))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("decode error status = %d, want 500", rec.Code)
	}
}
