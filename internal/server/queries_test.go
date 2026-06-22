package server

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/clio"
)

// fakeEvents is a small NDJSON event body shared by the analysis tests.
func fakeEventsBody() string {
	return ndjsonLines(
		`{"id":"001","source":"svc","subject":"/orders/1","type":"created","time":"2024-01-01T00:00:00Z"}`,
		`{"id":"002","source":"svc","subject":"/orders/1","type":"shipped","time":"2024-01-01T01:00:00Z"}`,
		`{"id":"003","source":"svc","subject":"/orders/2","type":"created","time":"2024-01-01T02:00:00Z"}`,
		`{"id":"004","source":"svc","subject":"/users/9","type":"login","time":"2024-01-01T03:00:00Z"}`,
	)
}

func TestHandleQueriesOffline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/queries", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bpmn-state-offline") {
		t.Errorf("expected offline state, got:\n%s", rec.Body.String())
	}
}

func TestHandleQueriesOnlineAndPipeline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = fakeEventsBody()
	f.connect(s)

	// Base read: ok, all 4 events.
	rec := s.do(http.MethodGet, "/queries", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	// Add a stage that keeps only /orders → renders the funnel with survivors.
	rec = s.do(http.MethodPost, "/queries", form(map[string]string{"subject": "/orders"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("add status = %d", rec.Code)
	}
	if rec.Header().Get("HX-Trigger") != "scope-changed" {
		t.Errorf("missing HX-Trigger scope-changed")
	}
	if len(s.stages()) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(s.stages()))
	}

	// Add an empty stage → ignored (no-op).
	s.do(http.MethodPost, "/queries", form(map[string]string{}))
	if len(s.stages()) != 1 {
		t.Errorf("empty stage should be ignored, got %d", len(s.stages()))
	}

	// Add a typed stage so handleQueries iterates multiple stages.
	s.do(http.MethodPost, "/queries", form(map[string]string{"types": "created"}))
	if len(s.stages()) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(s.stages()))
	}

	// Re-render: funnel now has two stages.
	rec = s.do(http.MethodGet, "/queries", nil)
	if !strings.Contains(rec.Body.String(), "queries-slot") {
		t.Errorf("missing queries-slot after stages")
	}

	// Delete stage index 0.
	rec = s.do(http.MethodPost, "/queries/delete", form(map[string]string{"index": "0"}))
	if rec.Code != http.StatusOK || len(s.stages()) != 1 {
		t.Fatalf("delete: status=%d stages=%d", rec.Code, len(s.stages()))
	}
	// Out-of-range delete index → no-op.
	s.do(http.MethodPost, "/queries/delete", form(map[string]string{"index": "99"}))
	if len(s.stages()) != 1 {
		t.Errorf("out-of-range delete should be a no-op")
	}

	// Clear.
	rec = s.do(http.MethodPost, "/queries/clear", nil)
	if rec.Code != http.StatusOK || len(s.stages()) != 0 {
		t.Fatalf("clear: status=%d stages=%d", rec.Code, len(s.stages()))
	}
}

func TestHandleQueriesUnauthorized(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.status = http.StatusUnauthorized
	f.connect(s)

	rec := s.do(http.MethodGet, "/queries", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bpmn-state-unauthorized") {
		t.Errorf("expected unauthorized state, got:\n%s", rec.Body.String())
	}
}

func TestQueryHandlersBadForm(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	for _, target := range []string{"/queries", "/queries/delete"} {
		if rec := s.do(http.MethodPost, target, &badReader{}); rec.Code != http.StatusBadRequest {
			t.Errorf("%s bad form status = %d, want 400", target, rec.Code)
		}
	}
}

func TestQueryStageLabelAndEmpty(t *testing.T) {
	if (queryStage{}).label() != "any" {
		t.Errorf("empty stage label != any")
	}
	if !(queryStage{}).empty() {
		t.Errorf("empty stage should be empty")
	}
	full := queryStage{Subject: "/o", Types: []string{"a", "b"}, LowerBound: "1", UpperBound: "9"}
	want := "subject /o · type a|b · from 1 · to 9"
	if got := full.label(); got != want {
		t.Errorf("label = %q, want %q", got, want)
	}
	if full.empty() {
		t.Errorf("full stage should not be empty")
	}
}

func TestMatchStageAndSurvives(t *testing.T) {
	st := queryStage{Subject: "orders", Types: []string{"created"}, LowerBound: "001", UpperBound: "010"}
	// Matches.
	if !matchStage(eventKey{Subject: "/orders/1", Type: "created", ID: "005"}, st) {
		t.Errorf("expected match")
	}
	// Wrong subject.
	if matchStage(eventKey{Subject: "/users/1", Type: "created", ID: "005"}, st) {
		t.Errorf("subject should not match")
	}
	// Exact-subject match (no trailing slash path).
	if !matchStage(eventKey{Subject: "/orders", Type: "created", ID: "005"}, st) {
		t.Errorf("exact subject should match")
	}
	// Wrong type.
	if matchStage(eventKey{Subject: "/orders/1", Type: "shipped", ID: "005"}, st) {
		t.Errorf("type should not match")
	}
	// Below lower bound.
	if matchStage(eventKey{Subject: "/orders/1", Type: "created", ID: "000"}, st) {
		t.Errorf("below lower bound should fail")
	}
	// Above upper bound.
	if matchStage(eventKey{Subject: "/orders/1", Type: "created", ID: "020"}, st) {
		t.Errorf("above upper bound should fail")
	}
	// Source substring narrows too.
	src := queryStage{Source: "svc-orders"}
	if !matchStage(eventKey{Subject: "/orders/1", Source: "svc-orders-1"}, src) {
		t.Errorf("source substring should match")
	}
	if matchStage(eventKey{Subject: "/orders/1", Source: "svc-users"}, src) {
		t.Errorf("source substring should not match")
	}
	// survives across multiple stages.
	if !survives(eventKey{Subject: "/orders/1", Type: "created", ID: "005"}, []queryStage{st}) {
		t.Errorf("expected survives")
	}
	if survives(eventKey{Subject: "/orders/1", Type: "shipped", ID: "005"}, []queryStage{st}) {
		t.Errorf("expected not to survive")
	}
}

// TestDisciplineLens covers the third scope layer (docs/SCOPE.md §3.3): a lens
// passed to applyPipeline narrows on top of the global Queries pipeline.
func TestDisciplineLens(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	events := []clio.Event{
		{ID: "1", Subject: "/orders/1", Type: "created", Source: "svc-a"},
		{ID: "2", Subject: "/orders/2", Type: "created", Source: "svc-b"},
		{ID: "3", Subject: "/users/1", Type: "login", Source: "svc-a"},
	}
	// No global pipeline, lens keeps only /orders with source svc-a.
	got := s.applyPipeline(events, queryStage{Subject: "/orders", Source: "svc-a"})
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("lens applyPipeline = %+v", got)
	}
	// An empty lens is a no-op.
	if got := s.applyPipeline(events, queryStage{}); len(got) != 3 {
		t.Fatalf("empty lens should not filter, got %d", len(got))
	}
	// The lens composes with the global pipeline (AND).
	s.pipeline = []queryStage{{Source: "svc-a"}}
	got = s.applyPipeline(events, queryStage{Subject: "/users"})
	if len(got) != 1 || got[0].ID != "3" {
		t.Fatalf("pipeline+lens applyPipeline = %+v", got)
	}
}

func TestApplyPipeline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	events := []clio.Event{
		{ID: "1", Subject: "/orders/1", Type: "created"},
		{ID: "2", Subject: "/users/1", Type: "login"},
	}
	// No stages → unchanged.
	if got := s.applyPipeline(events); len(got) != 2 {
		t.Fatalf("no-stage applyPipeline = %d", len(got))
	}
	full := []clio.FullEvent{
		{ID: "1", Subject: "/orders/1", Type: "created"},
		{ID: "2", Subject: "/users/1", Type: "login"},
	}
	if got := s.applyPipelineFull(full); len(got) != 2 {
		t.Fatalf("no-stage applyPipelineFull = %d", len(got))
	}

	// With a stage that keeps only /orders.
	s.pipeline = []queryStage{{Subject: "/orders"}}
	if got := s.applyPipeline(events); len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("filtered applyPipeline = %+v", got)
	}
	if got := s.applyPipelineFull(full); len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("filtered applyPipelineFull = %+v", got)
	}
}

func TestScopedEventsErrorPropagates(t *testing.T) {
	s := newTestServer(t, defaultCfg()) // offline
	if _, err := s.scopedEvents(t.Context()); !errors.Is(err, clio.ErrOffline) {
		t.Errorf("scopedEvents err = %v, want ErrOffline", err)
	}
	if _, err := s.scopedFullEvents(t.Context()); !errors.Is(err, clio.ErrOffline) {
		t.Errorf("scopedFullEvents err = %v, want ErrOffline", err)
	}
}

func TestReadErrState(t *testing.T) {
	cases := []struct {
		err   error
		state string
	}{
		{clio.ErrOffline, "offline"},
		{clio.ErrUnauthorized, "unauthorized"},
		{errors.New("boom"), "error"},
	}
	for _, c := range cases {
		if state, msg := readErrState(c.err); state != c.state || msg == "" {
			t.Errorf("readErrState(%v) = %q,%q", c.err, state, msg)
		}
	}
}

func TestAtoiSafeAndDistinctSubjects(t *testing.T) {
	if atoiSafe(" 5 ") != 5 {
		t.Errorf("atoiSafe parse failed")
	}
	if atoiSafe("nope") != -1 {
		t.Errorf("atoiSafe should return -1 on error")
	}
	got := distinctSubjects([]clio.Event{{Subject: "/a"}, {Subject: "/a"}, {Subject: "/b"}})
	if got != 2 {
		t.Errorf("distinctSubjects = %d, want 2", got)
	}
}

func TestStagesReturnsCopy(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	s.pipeline = []queryStage{{Subject: "/x"}}
	cp := s.stages()
	cp[0].Subject = "/mutated"
	if s.pipeline[0].Subject != "/x" {
		t.Errorf("stages() did not return a copy")
	}
	if !reflect.DeepEqual(s.stages(), []queryStage{{Subject: "/x"}}) {
		t.Errorf("unexpected stages snapshot")
	}
}
