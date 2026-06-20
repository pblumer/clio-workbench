package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/model"
)

func TestComputeGegen(t *testing.T) {
	d := model.Draft{
		Nodes: []model.Node{
			{ID: "new", Label: "New", Start: true},
			{ID: "placed", Label: "Placed"},
			{ID: "paid", Label: "Paid", End: true},
			{ID: "cancelled", Label: "Cancelled", End: true},
			{ID: "shipped", Label: "Shipped", End: true},
		},
		Edges: []model.Edge{
			{ID: "e1", Type: "created", From: "new", To: "placed"},
			{ID: "e2", Type: "paid", From: "placed", To: "paid"},
			{ID: "e3", Type: "cancelled", From: "placed", To: "cancelled"},
			{ID: "e4", Type: "shipped", From: "paid", To: "shipped"},
			{ID: "e5", Type: "created", From: "placed", To: "new"}, // duplicate type → skipped
			{ID: "e6", Type: "", From: "new", To: "new"},           // empty type → skipped
		},
	}
	events := []clio.Event{
		{Subject: "/orders/1", Type: "created"},
		{Subject: "/orders/1", Type: "paid"},
		{Subject: "/orders/2", Type: "created"},
		{Subject: "/orders/2", Type: "refunded"}, // unknown type → deviation
	}
	res := computeGegen(d, events)
	if res.Subjects != 2 || res.Conforming != 1 || res.FitPct != 50 {
		t.Fatalf("subjects/conforming/fit = %d/%d/%d", res.Subjects, res.Conforming, res.FitPct)
	}
	if len(res.Deviations) != 1 || res.Deviations[0].Subject != "/orders/2" || res.Deviations[0].Reason == "" {
		t.Fatalf("deviations = %+v", res.Deviations)
	}
	if strings.Join(res.UnusedTypes, ",") != "cancelled,shipped" {
		t.Fatalf("unused = %v, want [cancelled shipped]", res.UnusedTypes)
	}
	if strings.Join(res.UnknownTypes, ",") != "refunded" {
		t.Fatalf("unknown = %v, want [refunded]", res.UnknownTypes)
	}
}

func TestComputeGegenDeviationCap(t *testing.T) {
	d := model.Draft{
		Nodes: []model.Node{{ID: "a", Start: true}, {ID: "b", End: true}},
		Edges: []model.Edge{{ID: "e", Type: "go", From: "a", To: "b"}},
	}
	// 25 subjects each with an unknown type → all deviate; list is capped at 20.
	var events []clio.Event
	for i := 0; i < 25; i++ {
		events = append(events, clio.Event{Subject: "/s/" + string(rune('a'+i)), Type: "nope"})
	}
	res := computeGegen(d, events)
	if res.Subjects != 25 || res.Conforming != 0 {
		t.Fatalf("subjects/conforming = %d/%d", res.Subjects, res.Conforming)
	}
	if len(res.Deviations) != gegenMaxDeviations {
		t.Fatalf("deviations = %d, want capped at %d", len(res.Deviations), gegenMaxDeviations)
	}
}

func TestGegenprobeForm(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	rec := s.do(http.MethodGet, "/studio/gegenprobe", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Order") {
		t.Fatalf("form: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGegenprobeFormEmpty(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	if rec := s.do(http.MethodGet, "/studio/gegenprobe", nil); !strings.Contains(rec.Body.String(), "Noch keine Modelle") {
		t.Fatalf("expected empty state")
	}
}

func TestGegenprobeRunOnline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	f := newFakeClio(t)
	f.ndjson = ndjsonLines(
		`{"subject":"/orders/1","type":"created"}`,
		`{"subject":"/orders/1","type":"paid"}`,
		`{"subject":"/orders/2","type":"created"}`,
	)
	f.connect(s)

	rec := s.do(http.MethodPost, "/studio/gegenprobe/run", form(map[string]string{"draft": "order"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	// /orders/1 conforms (created→paid, end); /orders/2 ends in non-terminal placed.
	if !strings.Contains(body, "1/2 Subjects konform") {
		t.Errorf("expected 1/2 conform: %s", body)
	}
	if !strings.Contains(body, "/orders/2") {
		t.Errorf("expected /orders/2 deviation: %s", body)
	}
}

func TestGegenprobeRunEmpty(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	f := newFakeClio(t)
	f.connect(s) // default ndjson is empty
	rec := s.do(http.MethodPost, "/studio/gegenprobe/run", form(map[string]string{"draft": "order"}))
	if !strings.Contains(rec.Body.String(), "Keine Events") {
		t.Fatalf("expected no-events note: %s", rec.Body.String())
	}
}

func TestGegenprobeRunOffline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s) // no Clio connected
	rec := s.do(http.MethodPost, "/studio/gegenprobe/run", form(map[string]string{"draft": "order"}))
	if !strings.Contains(rec.Body.String(), "keine Clio verbunden") {
		t.Fatalf("expected offline note: %s", rec.Body.String())
	}
}

func TestGegenprobeRunUnauthorized(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	f := newFakeClio(t)
	f.status = http.StatusUnauthorized
	f.connect(s)
	rec := s.do(http.MethodPost, "/studio/gegenprobe/run", form(map[string]string{"draft": "order"}))
	if !strings.Contains(rec.Body.String(), "Token abgelehnt") {
		t.Fatalf("expected unauthorized note: %s", rec.Body.String())
	}
}

func TestGegenprobeRunReadError(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	f := newFakeClio(t)
	f.status = http.StatusInternalServerError
	f.connect(s)
	rec := s.do(http.MethodPost, "/studio/gegenprobe/run", form(map[string]string{"draft": "order"}))
	if !strings.Contains(rec.Body.String(), "konnten nicht gelesen") {
		t.Fatalf("expected read-error note: %s", rec.Body.String())
	}
}

func TestGegenprobeRunUnknownDraftIs404(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodPost, "/studio/gegenprobe/run", form(map[string]string{"draft": "ghost"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGegenprobeRunDecodeErrorIs500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraft(t, s)
	corruptDraft(t, s, "order")
	rec := s.do(http.MethodPost, "/studio/gegenprobe/run", form(map[string]string{"draft": "order"}))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestGegenprobeRunBadFormIs400(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodPost, "/studio/gegenprobe/run", strings.NewReader("%zz"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSubjectScopePrefix(t *testing.T) {
	cases := map[string]string{
		"/orders/{id}":           "/orders",
		"/orders/{id}/items/{x}": "/orders",
		"/orders/":               "/orders",
		"":                       "",
		"{id}":                   "",
	}
	for in, want := range cases {
		if got := subjectScopePrefix(in); got != want {
			t.Errorf("subjectScopePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
