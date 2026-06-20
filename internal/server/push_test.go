package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/model"
)

// recordingClio is a fake Clio that stores appended CloudEvents and serves them
// back as NDJSON for reads under a subject prefix — enough for a round-trip.
type recordingClio struct {
	mu         sync.Mutex
	events     []map[string]any
	server     *httptest.Server
	failAppend bool
	failRead   bool
}

func newRecordingClio(t *testing.T) *recordingClio {
	t.Helper()
	f := &recordingClio{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/events":
			if f.failAppend {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			b, _ := io.ReadAll(r.Body)
			var ev map[string]any
			_ = json.Unmarshal(b, &ev)
			f.mu.Lock()
			f.events = append(f.events, ev)
			f.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/events"):
			if f.failRead {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			prefix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/events"), "/")
			w.Header().Set("Content-Type", "application/x-ndjson")
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, ev := range f.events {
				subj, _ := ev["subject"].(string)
				if prefix == "" || strings.HasPrefix(strings.Trim(subj, "/"), prefix) {
					line, _ := json.Marshal(map[string]any{"subject": subj, "type": ev["type"]})
					_, _ = w.Write(append(line, '\n'))
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *recordingClio) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// armedPushServer returns a server connected to a recording Clio and armed.
func armedPushServer(t *testing.T) (*Server, *recordingClio) {
	t.Helper()
	s := newTestServer(t, defaultCfg())
	rec := newRecordingClio(t)
	s.clio.SetTarget(rec.server.URL, "tok")
	s.do(http.MethodPost, "/studio/push/arm", nil)
	if !s.armed() {
		t.Fatal("server should be armed after arm")
	}
	return s, rec
}

func TestPushPanelNoServer(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/studio/push", nil)
	if !strings.Contains(rec.Body.String(), "Kein Server verbunden") {
		t.Fatalf("expected no-server state: %s", rec.Body.String())
	}
}

func TestPushArmRequiresServer(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	// Arming with no server connected is a no-op.
	s.do(http.MethodPost, "/studio/push/arm", nil)
	if s.armed() {
		t.Fatal("must not arm without a server")
	}
}

func TestPushGateBlocksUnarmed(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := newRecordingClio(t)
	s.clio.SetTarget(rec.server.URL, "tok")
	seedGraphDraftWithFields(t, s)

	// Panel shows the locked gate.
	panel := s.do(http.MethodGet, "/studio/push", nil)
	if !strings.Contains(panel.Body.String(), "Als Wegwerf-Instanz bestätigen") {
		t.Errorf("expected arm button on locked panel")
	}
	// Running without arming is refused and writes nothing.
	run := s.do(http.MethodPost, "/studio/push/run", form(map[string]string{"draft": "order"}))
	if !strings.Contains(run.Body.String(), "throwaway instance") {
		t.Errorf("expected arm-first message, got: %s", run.Body.String())
	}
	if rec.count() != 0 {
		t.Errorf("nothing should have been pushed, got %d", rec.count())
	}
}

func TestPushArmRunRoundTrip(t *testing.T) {
	s, rec := armedPushServer(t)
	seedGraphDraftWithFields(t, s)

	run := s.do(http.MethodPost, "/studio/push/run", form(map[string]string{
		"draft": "order", "seed": "1", "samples": "5",
	}))
	body := run.Body.String()
	if !strings.Contains(body, "gepusht") || !strings.Contains(body, "/_test/") {
		t.Fatalf("expected push summary with namespace: %s", body)
	}
	if !strings.Contains(body, "konform") {
		t.Errorf("expected round-trip conformance: %s", body)
	}
	if rec.count() == 0 {
		t.Fatalf("no events recorded")
	}
	// Every pushed subject must be under the test namespace.
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, ev := range rec.events {
		subj, _ := ev["subject"].(string)
		if !strings.HasPrefix(subj, "/_test/") {
			t.Errorf("pushed subject not isolated: %q", subj)
		}
	}
}

func TestPushDisarm(t *testing.T) {
	s, _ := armedPushServer(t)
	s.do(http.MethodPost, "/studio/push/disarm", nil)
	if s.armed() {
		t.Fatal("should be disarmed")
	}
}

func TestPushSwitchingServerDisarms(t *testing.T) {
	s, _ := armedPushServer(t)
	// Pointing at a different server invalidates the confirmation.
	s.clio.SetTarget("http://elsewhere.example", "tok")
	if s.armed() {
		t.Fatal("switching servers must disarm the gate")
	}
}

func TestPushWriteFailure(t *testing.T) {
	s, rec := armedPushServer(t)
	seedGraphDraftWithFields(t, s)
	rec.failAppend = true
	run := s.do(http.MethodPost, "/studio/push/run", form(map[string]string{"draft": "order", "samples": "3"}))
	if !strings.Contains(run.Body.String(), "write failed") {
		t.Fatalf("expected write failure, got: %s", run.Body.String())
	}
}

func TestPushNoStartState(t *testing.T) {
	s, _ := armedPushServer(t)
	if err := s.store.Create(&model.Draft{ID: "flat", Name: "Flat", Kind: model.KindEntity}); err != nil {
		t.Fatal(err)
	}
	run := s.do(http.MethodPost, "/studio/push/run", form(map[string]string{"draft": "flat"}))
	if !strings.Contains(run.Body.String(), "no start state") {
		t.Fatalf("expected no-start note, got: %s", run.Body.String())
	}
}

func TestPushUnknownDraftIs404(t *testing.T) {
	s, _ := armedPushServer(t)
	run := s.do(http.MethodPost, "/studio/push/run", form(map[string]string{"draft": "ghost"}))
	if run.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", run.Code)
	}
}

func TestPushBadFormIs400(t *testing.T) {
	s, _ := armedPushServer(t)
	run := s.do(http.MethodPost, "/studio/push/run", strings.NewReader("%zz"))
	if run.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", run.Code)
	}
}

func TestPushRoundTripReadFails(t *testing.T) {
	s, rec := armedPushServer(t)
	seedGraphDraftWithFields(t, s)
	rec.failRead = true // appends succeed, read-back fails
	run := s.do(http.MethodPost, "/studio/push/run", form(map[string]string{"draft": "order", "samples": "3"}))
	body := run.Body.String()
	if !strings.Contains(body, "gepusht") {
		t.Fatalf("push should still report success: %s", body)
	}
	if !strings.Contains(body, "nicht möglich") {
		t.Errorf("expected round-trip-failed note: %s", body)
	}
}

func TestPushRunDecodeErrorIs500(t *testing.T) {
	s, _ := armedPushServer(t)
	seedGraphDraftWithFields(t, s)
	corruptDraft(t, s, "order")
	run := s.do(http.MethodPost, "/studio/push/run", form(map[string]string{"draft": "order"}))
	if run.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", run.Code)
	}
}

func TestPushArmDisarmBrokenStoreIs500(t *testing.T) {
	for _, target := range []string{"/studio/push/arm", "/studio/push/disarm"} {
		s := newBrokenStoreServer(t)
		if rec := s.do(http.MethodPost, target, nil); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s on broken store = %d, want 500", target, rec.Code)
		}
	}
}

func TestClioErrMessage(t *testing.T) {
	if !strings.Contains(clioErrMessage(clio.ErrOffline), "no Clio") {
		t.Errorf("offline message")
	}
	if !strings.Contains(clioErrMessage(clio.ErrUnauthorized), "rejected") {
		t.Errorf("unauthorized message")
	}
	if !strings.Contains(clioErrMessage(errors.New("boom")), "write failed") {
		t.Errorf("generic message")
	}
}

func TestApplySubjectStyle(t *testing.T) {
	cases := []struct {
		style, ns, name, id, want string
	}{
		{"/orders/{id}", "", "", "abc", "/orders/abc"},
		{"orders/{id}/items/{x}", "", "", "z", "/orders/z/items/z"}, // every placeholder → id
		{"", "order", "Order Name", "7", "/order/7"},                // fallback to namespace
		{"", "", "Order Name", "7", "/order-name/7"},                // fallback to name slug
		{"", "", "", "7", "/entity/7"},                              // ultimate fallback
	}
	for _, c := range cases {
		d := model.Draft{SubjectStyle: c.style, Namespace: c.ns, Name: c.name}
		if got := applySubjectStyle(d, c.id); got != c.want {
			t.Errorf("applySubjectStyle(%q,%q,%q) = %q, want %q", c.style, c.ns, c.name, got, c.want)
		}
	}
}

func TestCloudEvent(t *testing.T) {
	// With data.
	var env map[string]any
	_ = json.Unmarshal(cloudEvent("order-paid", "/_test/x/orders/1", []byte(`{"amount":1}`)), &env)
	if env["type"] != "order-paid" || env["subject"] != "/_test/x/orders/1" || env["specversion"] != "1.0" {
		t.Fatalf("envelope fields wrong: %+v", env)
	}
	if data, _ := env["data"].(map[string]any); data["amount"].(float64) != 1 {
		t.Errorf("data not embedded: %+v", env["data"])
	}
	// Without data → empty object.
	_ = json.Unmarshal(cloudEvent("x", "/s", nil), &env)
	if data, ok := env["data"].(map[string]any); !ok || len(data) != 0 {
		t.Errorf("nil data should become {}: %+v", env["data"])
	}
}
