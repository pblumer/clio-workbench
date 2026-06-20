package server

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/scenario"
	"github.com/pblumer/clio-workbench/internal/validate"
)

// seedGraphDraft creates a draft with a real lifecycle graph so CheckSequence
// has transitions to walk:
//
//	(start) new --created--> placed --paid--> paid (end)
//	                              \--cancelled--> cancelled (end)
func seedGraphDraft(t *testing.T, s *Server) *model.Draft {
	t.Helper()
	d := &model.Draft{
		ID: "order", Name: "Order", Kind: model.KindEntity, Namespace: "order",
		Nodes: []model.Node{
			{ID: "new", Label: "New", Start: true},
			{ID: "placed", Label: "Placed"},
			{ID: "paid", Label: "Paid", End: true},
			{ID: "cancelled", Label: "Cancelled", End: true},
		},
		Edges: []model.Edge{
			{ID: "e1", Type: "created", From: "new", To: "placed"},
			{ID: "e2", Type: "paid", From: "placed", To: "paid"},
			{ID: "e3", Type: "cancelled", From: "placed", To: "cancelled"},
		},
	}
	if err := s.store.Create(d); err != nil {
		t.Fatalf("seed graph draft: %v", err)
	}
	return d
}

func seedSuite(t *testing.T, s *Server, id, draftID string, cases ...scenario.Case) *scenario.Suite {
	t.Helper()
	su := &scenario.Suite{ID: id, Name: id, DraftID: draftID, Cases: cases}
	if err := s.scenarios.Save(su); err != nil {
		t.Fatalf("seed suite: %v", err)
	}
	return su
}

func TestScenariosEmptyState(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/studio/scenarios", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Noch keine Modelle") {
		t.Fatalf("expected empty state, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateSuite(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)

	rec := s.do(http.MethodPost, "/studio/scenarios", form(map[string]string{
		"draft": "order", "name": "Smoke",
	}))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Smoke") {
		t.Fatalf("create suite: %d %s", rec.Code, rec.Body.String())
	}
	list, _ := s.scenarios.List()
	if len(list) != 1 || list[0].DraftID != "order" || list[0].DraftRev == "" {
		t.Fatalf("suite not persisted with rev: %+v", list)
	}

	// Empty name → no suite added.
	s.do(http.MethodPost, "/studio/scenarios", form(map[string]string{"draft": "order", "name": "  "}))
	if list, _ := s.scenarios.List(); len(list) != 1 {
		t.Fatalf("empty name should not create a suite, have %d", len(list))
	}
}

func TestCreateSuiteUnknownDraft(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodPost, "/studio/scenarios", form(map[string]string{"draft": "ghost", "name": "X"}))
	if rec.Code != http.StatusOK { // ErrNotFound → panel re-render, not 500
		t.Fatalf("unknown draft create status = %d", rec.Code)
	}
}

func TestAddCaseAndRun(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	seedSuite(t, s, "suite1", "order")

	// Add an accepting happy-path case.
	rec := s.do(http.MethodPost, "/studio/scenarios/suite1/cases", form(map[string]string{
		"name": "happy", "sequence": "created → paid", "outcome": "accept", "endState": "paid",
	}))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "happy") {
		t.Fatalf("add case: %d %s", rec.Code, rec.Body.String())
	}
	su, _ := s.scenarios.Get("suite1")
	if len(su.Cases) != 1 || len(su.Cases[0].Steps) != 2 {
		t.Fatalf("case not stored: %+v", su.Cases)
	}

	// Run: the happy path must pass and the path must reach Paid.
	rec = s.do(http.MethodPost, "/studio/scenarios/suite1/run", nil)
	body := rec.Body.String()
	if !strings.Contains(body, "✓") || !strings.Contains(body, "Paid") {
		t.Fatalf("run result missing pass/path: %s", body)
	}
}

func TestAddCaseEmptyIgnored(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	seedSuite(t, s, "suite1", "order")

	// Empty sequence → no case.
	s.do(http.MethodPost, "/studio/scenarios/suite1/cases", form(map[string]string{
		"name": "x", "sequence": "  ", "outcome": "accept",
	}))
	if su, _ := s.scenarios.Get("suite1"); len(su.Cases) != 0 {
		t.Fatalf("empty sequence should not add a case")
	}
}

func TestDeleteCaseAndSuite(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	seedSuite(t, s, "suite1", "order", scenario.Case{
		ID: "c1", Name: "x", Steps: []scenario.Step{{Type: "created"}},
		Expect: scenario.Expectation{Outcome: scenario.ExpectAccept},
	})

	s.do(http.MethodPost, "/studio/scenarios/suite1/cases/c1/delete", nil)
	if su, _ := s.scenarios.Get("suite1"); len(su.Cases) != 0 {
		t.Fatalf("case not deleted")
	}

	rec := s.do(http.MethodPost, "/studio/scenarios/suite1/delete?draft=order", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete suite status = %d", rec.Code)
	}
	if list, _ := s.scenarios.List(); len(list) != 0 {
		t.Fatalf("suite not deleted, have %d", len(list))
	}
	// Deleting a missing suite is a no-op (idempotent), not a 500.
	if rec := s.do(http.MethodPost, "/studio/scenarios/suite1/delete?draft=order", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete missing suite status = %d", rec.Code)
	}
}

func TestRunMissingSuiteIs404(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodPost, "/studio/scenarios/ghost/run", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("run missing suite status = %d, want 404", rec.Code)
	}
}

func TestRunNoGraph(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	// A draft with no nodes/edges.
	if err := s.store.Create(&model.Draft{ID: "flat", Name: "Flat", Kind: model.KindEntity}); err != nil {
		t.Fatal(err)
	}
	seedSuite(t, s, "suite1", "flat", scenario.Case{
		ID: "c1", Name: "x", Steps: []scenario.Step{{Type: "created"}},
		Expect: scenario.Expectation{Outcome: scenario.ExpectAccept},
	})
	rec := s.do(http.MethodPost, "/studio/scenarios/suite1/run", nil)
	if !strings.Contains(rec.Body.String(), "no graph") {
		t.Fatalf("expected no-graph note, got: %s", rec.Body.String())
	}
}

func TestRunMissingModel(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedSuite(t, s, "suite1", "ghost") // references a draft that doesn't exist
	rec := s.do(http.MethodPost, "/studio/scenarios/suite1/run", nil)
	if !strings.Contains(rec.Body.String(), "no longer exists") {
		t.Fatalf("expected missing-model note, got: %s", rec.Body.String())
	}
}

func TestScenarioDriftWarning(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	su := seedSuite(t, s, "suite1", "order")
	su.DraftRev = "stale00000000" // pretend it was written against another revision
	if err := s.scenarios.Save(su); err != nil {
		t.Fatal(err)
	}
	rec := s.do(http.MethodGet, "/studio/scenarios?draft=order&suite=suite1", nil)
	if !strings.Contains(rec.Body.String(), "anderen Modellstand") {
		t.Fatalf("expected drift warning, got: %s", rec.Body.String())
	}
}

func TestParseSequence(t *testing.T) {
	got := parseSequence("a → b -> c, d\n e \n\n,")
	want := []string{"a", "b", "c", "d", "e"}
	var types []string
	for _, st := range got {
		types = append(types, st.Type)
	}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("parseSequence = %v, want %v", types, want)
	}
	if len(parseSequence("   ")) != 0 {
		t.Fatalf("blank sequence should be empty")
	}
}

func TestRunCase(t *testing.T) {
	d := seedGraphDraftValue()
	m := validate.NewMachine(d)
	labels := nodeLabels(d.Nodes)

	mk := func(out scenario.Outcome, end string, types ...string) scenario.Case {
		steps := make([]scenario.Step, len(types))
		for i, ty := range types {
			steps[i] = scenario.Step{Type: ty}
		}
		return scenario.Case{Name: "c", Steps: steps, Expect: scenario.Expectation{Outcome: out, EndState: end}}
	}

	tests := []struct {
		name string
		c    scenario.Case
		pass bool
		det  string // substring expected in Detail
	}{
		{"accept happy", mk(scenario.ExpectAccept, "", "created", "paid"), true, "valid walk"},
		{"accept endstate ok", mk(scenario.ExpectAccept, "paid", "created", "paid"), true, "valid walk"},
		{"accept endstate wrong", mk(scenario.ExpectAccept, "cancelled", "created", "paid"), false, "expected"},
		{"accept bad transition", mk(scenario.ExpectAccept, "", "paid"), false, "no transition"},
		{"reject correctly", mk(scenario.ExpectReject, "", "paid"), true, "correctly rejected"},
		{"reject but valid", mk(scenario.ExpectReject, "", "created", "paid"), false, "valid walk"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := runCase(m, labels, tc.c)
			if r.Pass != tc.pass {
				t.Fatalf("Pass = %v, want %v (detail %q)", r.Pass, tc.pass, r.Detail)
			}
			if !strings.Contains(r.Detail, tc.det) {
				t.Fatalf("Detail %q missing %q", r.Detail, tc.det)
			}
		})
	}
}

func TestLabelHelpers(t *testing.T) {
	labels := map[string]string{"a": "Alpha", "b": ""}
	if labelOf(labels, "a") != "Alpha" {
		t.Errorf("labelled id")
	}
	if labelOf(labels, "b") != "b" { // empty label falls back to id
		t.Errorf("empty-label fallback")
	}
	if labelOf(labels, "z") != "z" { // missing falls back to id
		t.Errorf("missing fallback")
	}
	if lastOf(nil) != "" || lastOf([]string{"x", "y"}) != "y" {
		t.Errorf("lastOf")
	}
}

// corruptSuite writes invalid JSON into a suite's file so Get/List fail to
// decode (the scenario-store analogue of corruptDraft).
func corruptSuite(t *testing.T, s *Server, id string) {
	t.Helper()
	p := filepath.Join(s.cfg.DataDir, "scenarios", id+".json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("corrupt suite: %v", err)
	}
}

func TestScenariosBadFormIs400(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	seedSuite(t, s, "suite1", "order")
	// handleCreateSuite parses the form up front.
	if rec := s.do(http.MethodPost, "/studio/scenarios", strings.NewReader("%zz")); rec.Code != http.StatusBadRequest {
		t.Fatalf("create bad form = %d, want 400", rec.Code)
	}
	// handleAddCase loads the suite first, then parses the form.
	if rec := s.do(http.MethodPost, "/studio/scenarios/suite1/cases", strings.NewReader("%zz")); rec.Code != http.StatusBadRequest {
		t.Fatalf("add-case bad form = %d, want 400", rec.Code)
	}
}

func TestScenarioStoreDecodeErrorsAre500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	seedSuite(t, s, "suite1", "order")
	corruptSuite(t, s, "suite1")

	// buildScenarioView lists suites → decode error.
	if rec := s.do(http.MethodGet, "/studio/scenarios?draft=order", nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("list suites decode = %d, want 500", rec.Code)
	}
	// loadSuite gets a single suite → decode error.
	if rec := s.do(http.MethodPost, "/studio/scenarios/suite1/run", nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("get suite decode = %d, want 500", rec.Code)
	}
}

func TestCreateSuiteDraftDecodeErrorIs500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	corruptDraft(t, s, "order")
	rec := s.do(http.MethodPost, "/studio/scenarios", form(map[string]string{"draft": "order", "name": "X"}))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("create with corrupt draft = %d, want 500", rec.Code)
	}
}

func TestRunSuiteDraftDecodeErrorIs500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	seedSuite(t, s, "suite1", "order")
	corruptDraft(t, s, "order") // the suite's model fails to decode
	if rec := s.do(http.MethodPost, "/studio/scenarios/suite1/run", nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("run with corrupt model = %d, want 500", rec.Code)
	}
}

// seedGraphDraftValue is the seedGraphDraft model as a plain value (no store).
func seedGraphDraftValue() model.Draft {
	return model.Draft{
		Nodes: []model.Node{
			{ID: "new", Label: "New", Start: true},
			{ID: "placed", Label: "Placed"},
			{ID: "paid", Label: "Paid", End: true},
			{ID: "cancelled", Label: "Cancelled", End: true},
		},
		Edges: []model.Edge{
			{ID: "e1", Type: "created", From: "new", To: "placed"},
			{ID: "e2", Type: "paid", From: "placed", To: "paid"},
			{ID: "e3", Type: "cancelled", From: "placed", To: "cancelled"},
		},
	}
}
