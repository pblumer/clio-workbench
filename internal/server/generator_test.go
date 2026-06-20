package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/simulator"
	"github.com/pblumer/clio-workbench/internal/validate"
)

func validateMachine(d model.Draft) *validate.Machine { return validate.NewMachine(d) }

// genStream builds a stream from alternating (type, data-json) pairs.
func genStream(pairs ...string) simulator.Stream {
	var s simulator.Stream
	for i := 0; i+1 < len(pairs); i += 2 {
		s.Events = append(s.Events, simulator.Event{Type: pairs[i], Data: []byte(pairs[i+1])})
	}
	return s
}

func TestGeneratorForm(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	rec := s.do(http.MethodGet, "/studio/generator", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Order", `name="seed"`, `name="samples"`} {
		if !strings.Contains(body, want) {
			t.Errorf("form missing %q", want)
		}
	}
}

func TestGeneratorFormEmpty(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/studio/generator", nil)
	if !strings.Contains(rec.Body.String(), "Noch keine Modelle") {
		t.Fatalf("expected empty state")
	}
}

func TestGeneratorRun(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraftWithFields(t, s)

	rec := s.do(http.MethodPost, "/studio/generator/run", form(map[string]string{
		"draft": "order", "seed": "1", "samples": "30",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	// All samples should be valid walks, full edge coverage, and every guaranteed
	// mutation rejected.
	if !strings.Contains(body, "30/30 gültig") {
		t.Errorf("expected 30/30 valid, got: %s", body)
	}
	if !strings.Contains(body, "Negativ-Prüfungen") || !strings.Contains(body, "drop-required") {
		t.Errorf("expected negative checks incl drop-required: %s", body)
	}
	if !strings.Contains(body, "report?format=md") || !strings.Contains(body, "report?format=json") {
		t.Errorf("expected download links: %s", body)
	}
}

func TestGeneratorRunNoStart(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	if err := s.store.Create(&model.Draft{ID: "flat", Name: "Flat", Kind: model.KindEntity}); err != nil {
		t.Fatal(err)
	}
	rec := s.do(http.MethodPost, "/studio/generator/run", form(map[string]string{"draft": "flat"}))
	if !strings.Contains(rec.Body.String(), "no start state") {
		t.Fatalf("expected no-start note, got: %s", rec.Body.String())
	}
}

func TestGeneratorRunUnknownDraftIs404(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodPost, "/studio/generator/run", form(map[string]string{"draft": "ghost"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGeneratorReportDownloads(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraftWithFields(t, s)

	md := s.do(http.MethodGet, "/studio/generator/report?format=md&draft=order&seed=1&samples=10", nil)
	if md.Code != http.StatusOK {
		t.Fatalf("md status = %d", md.Code)
	}
	if ct := md.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("md content-type = %q", ct)
	}
	if cd := md.Header().Get("Content-Disposition"); !strings.Contains(cd, "order-report.md") {
		t.Errorf("md disposition = %q", cd)
	}
	if !strings.Contains(md.Body.String(), "# Teststudio") {
		t.Errorf("md body not a report: %s", md.Body.String())
	}

	js := s.do(http.MethodGet, "/studio/generator/report?format=json&draft=order&seed=1&samples=10", nil)
	if ct := js.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("json content-type = %q", ct)
	}
	if !strings.Contains(js.Body.String(), `"model": "Order"`) {
		t.Errorf("json body not a report: %s", js.Body.String())
	}
}

func TestGeneratorReportUnknownDraftIs404(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/studio/generator/report?format=md&draft=ghost", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestBuildGeneratorRunDeterministic(t *testing.T) {
	d := graphDraftWithFields()
	a := buildGeneratorRun(d, 5, 25)
	b := buildGeneratorRun(d, 5, 25)
	if a.Passed != b.Passed || a.Failed != b.Failed || a.CoveredEdges != b.CoveredEdges {
		t.Fatalf("non-deterministic run: %+v vs %+v", a, b)
	}
	if a.Passed != 25 || a.Failed != 0 {
		t.Fatalf("expected all valid, got passed=%d failed=%d", a.Passed, a.Failed)
	}
	if a.CoveredEdges != a.TotalEdges {
		t.Fatalf("expected full coverage, got %d/%d", a.CoveredEdges, a.TotalEdges)
	}
	// Guaranteed-invalid mutations must be reported as rejected.
	for _, n := range a.Negatives {
		if (n.Kind == "insert-unknown" || n.Kind == "drop-required" || n.Kind == "wrong-type") && !n.Rejected {
			t.Errorf("mutation %q should be rejected", n.Kind)
		}
	}
}

func TestGeneratorRunBadFormIs400(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodPost, "/studio/generator/run", strings.NewReader("%zz"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad form = %d, want 400", rec.Code)
	}
}

func TestGeneratorDraftDecodeErrorIs500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraftWithFields(t, s)
	corruptDraft(t, s, "order")
	// Both the run and the report resolve the draft via genTarget → 500.
	if rec := s.do(http.MethodPost, "/studio/generator/run", form(map[string]string{"draft": "order"})); rec.Code != http.StatusInternalServerError {
		t.Fatalf("run decode = %d, want 500", rec.Code)
	}
	if rec := s.do(http.MethodGet, "/studio/generator/report?draft=order", nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("report decode = %d, want 500", rec.Code)
	}
}

// ambiguousModel has two edges of the same type from the start; whichever the
// generator picks, validate's greedy walk dead-ends, so every sample fails.
// This drives buildGeneratorRun's failure path and streamRejected's bad-sequence
// branch deterministically.
func ambiguousModel() model.Draft {
	return model.Draft{
		ID: "amb", Name: "Amb", Kind: model.KindEntity,
		Nodes: []model.Node{
			{ID: "x", Start: true},
			{ID: "a"}, // dead end, not terminal
			{ID: "b", End: true},
		},
		Edges: []model.Edge{
			{ID: "e1", Type: "t", From: "x", To: "a"},
			{ID: "e2", Type: "t", From: "x", To: "b"},
		},
	}
}

func TestBuildGeneratorRunFailures(t *testing.T) {
	run := buildGeneratorRun(ambiguousModel(), 1, 10)
	if run.Failed != 10 || run.Passed != 0 {
		t.Fatalf("expected all samples to fail, got passed=%d failed=%d", run.Passed, run.Failed)
	}
	if len(run.Failures) == 0 || run.Failures[0].Reason == "" {
		t.Fatalf("expected failures with reasons, got %+v", run.Failures)
	}
}

func TestStreamRejected(t *testing.T) {
	d := graphDraftWithFields()
	m := validateMachine(d)
	// Valid: created → paid (paid carries a valid payload).
	good := genStream("created", `null`, "paid", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":1}`)
	if rej, _ := streamRejected(d, m, good); rej {
		t.Errorf("valid stream should not be rejected")
	}
	// Bad sequence: an unknown type.
	if rej, reason := streamRejected(d, m, genStream("nope", `null`)); !rej || reason == "" {
		t.Errorf("bad sequence should be rejected with a reason")
	}
	// Bad payload: valid walk but paid's required id missing.
	bad := genStream("created", `null`, "paid", `{"amount":1}`)
	if rej, reason := streamRejected(d, m, bad); !rej || reason != "payload rejected" {
		t.Errorf("bad payload should be rejected as payload, got rej=%v reason=%q", rej, reason)
	}
}

func TestParseSeedAndSamples(t *testing.T) {
	if parseSeed("") != genDefaultSeed || parseSeed("x") != genDefaultSeed {
		t.Errorf("seed default")
	}
	if parseSeed("-7") != -7 {
		t.Errorf("seed parse")
	}
	if parseSamples("") != genDefaultSamples || parseSamples("0") != genDefaultSamples {
		t.Errorf("samples default")
	}
	if parseSamples("99999") != genMaxSamples {
		t.Errorf("samples clamp")
	}
	if parseSamples("5") != 5 {
		t.Errorf("samples parse")
	}
}

// graphDraftWithFields is the order lifecycle whose paid event carries fields,
// so payload faking and payload mutations have something to work with.
func graphDraftWithFields() model.Draft {
	return model.Draft{
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
		Steps: []model.Step{
			{ID: "s1", Kind: model.StepEvent, Name: "paid", Fields: []model.Field{
				{Name: "id", Type: "reference", Format: "uuid", Required: true},
				{Name: "amount", Type: "number", Required: true},
			}},
		},
	}
}

func seedGraphDraftWithFields(t *testing.T, s *Server) {
	t.Helper()
	d := graphDraftWithFields()
	if err := s.store.Create(&d); err != nil {
		t.Fatalf("seed: %v", err)
	}
}
