package server

import (
	"net/http"
	"strings"
	"testing"
)

// fullEventsBody is an NDJSON body of payload-carrying events that produce a
// non-trivial subject tree and reference edges.
func fullEventsBody() string {
	return ndjsonLines(
		`{"id":"001","source":"svc","subject":"/orders/1","type":"order.created","time":"2024-01-01T00:00:00Z","data":{"customerId":"c1"}}`,
		`{"id":"002","source":"svc","subject":"/orders/1/items/9","type":"item.added","time":"2024-01-01T00:01:00Z","data":{}}`,
		`{"id":"003","source":"svc","subject":"/orders/2","type":"order.created","time":"2024-01-01T00:02:00Z","data":{"customerId":"c2"}}`,
	)
}

// --- space ---

func TestHandleSpaceOnline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = fakeEventsBody()
	f.connect(s)

	rec := s.do(http.MethodGet, "/space", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "events-slot") {
		t.Errorf("missing events-slot")
	}
}

func TestHandleSpaceFramed(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = fakeEventsBody()
	f.connect(s)

	// frame=2 → keep only the last 2 of the 4 events (framed branch).
	rec := s.do(http.MethodGet, "/space?frame=2", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleSpaceEmpty(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = "" // no events → empty state
	f.connect(s)

	rec := s.do(http.MethodGet, "/space", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no events") {
		t.Errorf("expected empty-state message, got:\n%s", rec.Body.String())
	}
}

func TestHandleSpaceOffline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/space", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bpmn-state-offline") {
		t.Errorf("expected offline state")
	}
}

func TestHandleSpaceErrorState(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.status = http.StatusInternalServerError // 500 → generic error state
	f.connect(s)

	rec := s.do(http.MethodGet, "/space", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bpmn-state-error") {
		t.Errorf("expected error state, got:\n%s", rec.Body.String())
	}
}

func TestFrameSize(t *testing.T) {
	cases := map[string]int{
		"":      0,
		"all":   0,
		"0":     0,
		"live":  defaultFrame,
		"last":  defaultFrame,
		"250":   250,
		"-1000": 1000,
		"xyz":   0,
	}
	for in, want := range cases {
		if got := frameSize(in); got != want {
			t.Errorf("frameSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestDottedViewDotR(t *testing.T) {
	if (dottedView{}).DotR() == "" {
		t.Errorf("DotR should be non-empty")
	}
}

func TestTypeColorStable(t *testing.T) {
	if typeColor("x") != typeColor("x") {
		t.Errorf("typeColor not stable")
	}
}

// --- process ---

func TestHandleProcessOnline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = fakeEventsBody()
	f.connect(s)

	rec := s.do(http.MethodGet, "/process", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "process-slot") {
		t.Errorf("missing process-slot")
	}
}

func TestHandleProcessFiltered(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = fakeEventsBody()
	f.connect(s)

	// subject + source filters exercise the filtering branch.
	rec := s.do(http.MethodGet, "/process?subject=/orders&source=svc", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	// A source filter that matches nothing exercises the source-skip continue.
	rec = s.do(http.MethodGet, "/process?source=does-not-exist", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("source-filter status = %d", rec.Code)
	}
}

func TestHandleProcessEmpty(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = ""
	f.connect(s)

	rec := s.do(http.MethodGet, "/process", nil)
	if !strings.Contains(rec.Body.String(), "no events") {
		t.Errorf("expected empty state, got:\n%s", rec.Body.String())
	}
}

func TestHandleProcessOffline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/process", nil)
	if !strings.Contains(rec.Body.String(), "bpmn-state-offline") {
		t.Errorf("expected offline state")
	}
}

func TestHandleProcessUnauthorized(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.status = http.StatusUnauthorized
	f.connect(s)
	rec := s.do(http.MethodGet, "/process", nil)
	if !strings.Contains(rec.Body.String(), "bpmn-state-unauthorized") {
		t.Errorf("expected unauthorized state")
	}
}

func TestHandleProcessError(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.status = http.StatusInternalServerError
	f.connect(s)
	rec := s.do(http.MethodGet, "/process", nil)
	if !strings.Contains(rec.Body.String(), "bpmn-state-error") {
		t.Errorf("expected error state")
	}
}

// --- relations ---

func TestHandleRelationsOnline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = fullEventsBody()
	f.connect(s)

	rec := s.do(http.MethodGet, "/relations", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "relations-slot") {
		t.Errorf("missing relations-slot")
	}
}

func TestHandleRelationsEmpty(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = "" // no subjects → empty state
	f.connect(s)

	rec := s.do(http.MethodGet, "/relations", nil)
	if !strings.Contains(rec.Body.String(), "no subjects yet") {
		t.Errorf("expected empty state, got:\n%s", rec.Body.String())
	}
}

func TestHandleRelationsOffline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/relations", nil)
	if !strings.Contains(rec.Body.String(), "bpmn-state-offline") {
		t.Errorf("expected offline state")
	}
}

func TestHandleRelationsUnauthorized(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.status = http.StatusUnauthorized
	f.connect(s)
	rec := s.do(http.MethodGet, "/relations", nil)
	if !strings.Contains(rec.Body.String(), "bpmn-state-unauthorized") {
		t.Errorf("expected unauthorized state")
	}
}

func TestHandleRelationsError(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.status = http.StatusInternalServerError
	f.connect(s)
	rec := s.do(http.MethodGet, "/relations", nil)
	if !strings.Contains(rec.Body.String(), "bpmn-state-error") {
		t.Errorf("expected error state")
	}
}
