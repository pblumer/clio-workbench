package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- nodeevents (inspector) ---

func TestHandleNodeEventsBySubject(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = ndjsonLines(
		`{"id":"1","source":"svc","subject":"/orders/1","type":"created","time":"t","data":{"x":1}}`,
	)
	f.connect(s)

	rec := s.do(http.MethodGet, "/node-events?subject=/orders/1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/orders/1") {
		t.Errorf("expected subject in body")
	}
}

func TestHandleNodeEventsByType(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = ndjsonLines(
		`{"id":"1","source":"svc","subject":"/orders/1","type":"created","time":"t","data":null}`,
	)
	f.connect(s)

	rec := s.do(http.MethodGet, "/node-events?type=created", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleNodeEventsMissingParams(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/node-events", nil)
	if !strings.Contains(rec.Body.String(), "no event type or subject given") {
		t.Errorf("expected missing-params message, got:\n%s", rec.Body.String())
	}
}

func TestHandleNodeEventsEmpty(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = "" // no events
	f.connect(s)

	rec := s.do(http.MethodGet, "/node-events?type=created", nil)
	if !strings.Contains(rec.Body.String(), "no events here") {
		t.Errorf("expected empty message, got:\n%s", rec.Body.String())
	}
}

func TestHandleNodeEventsOffline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/node-events?type=created", nil)
	if !strings.Contains(rec.Body.String(), "no Clio connected") {
		t.Errorf("expected offline message, got:\n%s", rec.Body.String())
	}
}

func TestHandleNodeEventsUnauthorized(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.status = http.StatusUnauthorized
	f.connect(s)
	rec := s.do(http.MethodGet, "/node-events?subject=/orders/1", nil)
	if !strings.Contains(rec.Body.String(), "rejected the token") {
		t.Errorf("expected unauthorized message, got:\n%s", rec.Body.String())
	}
}

func TestHandleNodeEventsError(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.status = http.StatusInternalServerError
	f.connect(s)
	rec := s.do(http.MethodGet, "/node-events?type=created", nil)
	if !strings.Contains(rec.Body.String(), "could not read events") {
		t.Errorf("expected error message, got:\n%s", rec.Body.String())
	}
}

// --- spaceevent hover card ---

func TestHandleSpaceEventOK(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = ndjsonLines(
		`{"id":"42","source":"svc","subject":"/orders/1","type":"created","time":"t","data":{"k":"v"}}`,
	)
	f.connect(s)

	rec := s.do(http.MethodGet, "/space/event?subject=/orders/1&id=42", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "created") {
		t.Errorf("expected event type in card, got:\n%s", rec.Body.String())
	}
}

func TestHandleSpaceEventNotFound(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = ndjsonLines(
		`{"id":"1","subject":"/orders/1","type":"created","time":"t","data":null}`,
	)
	f.connect(s)

	// id not present → empty state.
	rec := s.do(http.MethodGet, "/space/event?subject=/orders/1&id=999", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleSpaceEventMissingParams(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/space/event", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleSpaceEventOffline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/space/event?subject=/orders/1&id=1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleSpaceEventUnauthorized(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.status = http.StatusUnauthorized
	f.connect(s)
	rec := s.do(http.MethodGet, "/space/event?subject=/orders/1&id=1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

// --- prettyJSON deeper coverage (arrays, numbers, bools, null, fallback) ---

func TestPrettyJSONComplex(t *testing.T) {
	in := json.RawMessage(`{"arr":[1,2.5,true,false,null,"s"],"obj":{"nested":[]}}`)
	out := prettyJSON(in)
	for _, want := range []string{"arr", "nested", "true", "false", "null", "2.5"} {
		if !strings.Contains(out, want) {
			t.Errorf("prettyJSON output missing %q:\n%s", want, out)
		}
	}
}

func TestPrettyJSONInvalidFallsBack(t *testing.T) {
	// Truncated JSON: writeJSONValue errors, json.Indent also fails → raw text.
	in := json.RawMessage(`{"a":`)
	if got := prettyJSON(in); got != `{"a":` {
		t.Errorf("invalid JSON fallback = %q", got)
	}
}

// TestPrettyJSONTruncatedNested exercises the streaming error-returns inside
// writeJSONObject/writeJSONArray (truncated mid-structure). The handler falls
// back to the raw text after the streaming decode fails.
func TestPrettyJSONTruncatedNested(t *testing.T) {
	cases := []string{
		`{"a":[1,2`,        // array not closed inside object
		`{"a":{"b":1`,      // nested object not closed
		`[1,{"x":`,         // array of object with missing value
		`{"a":1,"b":[true`, // unterminated array value
		`{"a":1,"bcd`,      // truncated mid-key → key-token read error
		`{"a":1,"b":2`,     // object not closed → consume-'}' read error
	}
	for _, c := range cases {
		// Should not panic; returns the raw text (both decoders fail).
		if got := prettyJSON(json.RawMessage(c)); got == "" {
			t.Errorf("prettyJSON(%q) returned empty", c)
		}
	}
}

// --- space stream ---

func TestHandleSpaceStreamOffline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/space/stream", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestHandleSpaceStreamPushes drives the live loop: an upstream that grows over
// time, a cancellable request context, and assertions on the SSE dot pushed.
func TestHandleSpaceStreamPushes(t *testing.T) {
	s := newTestServer(t, defaultCfg())

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		// Always return one event with a high id so readSince yields it as new.
		_, _ = w.Write([]byte(ndjsonLines(
			`{"id":"500","subject":"/orders/1","type":"created","time":"t"}`,
		)))
	}))
	defer upstream.Close()
	s.clio.SetTarget(upstream.URL, "tok")

	// Start the stream with after=0 so the high-id event is "new".
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/space/stream?after=0", nil).WithContext(ctx)
	rec := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		s.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	// Wait for at least one ticker fire (streamPoll = 2s) to push a dot.
	deadline := time.After(6 * time.Second)
	for {
		if strings.Contains(rec.body(), "event: dot") {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("no SSE dot pushed:\n%s", rec.body())
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	<-done

	if !strings.Contains(rec.body(), `"type":"created"`) {
		t.Errorf("dot payload missing type:\n%s", rec.body())
	}
}

// streamServerWithNewEvent returns a server whose upstream always yields one
// high-id event, so the live loop pushes a dot each tick.
func streamServerWithNewEvent(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, defaultCfg())
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(ndjsonLines(`{"id":"900","subject":"/orders/1","type":"created","time":"t"}`)))
	}))
	t.Cleanup(upstream.Close)
	s.clio.SetTarget(upstream.URL, "tok")
	return s
}

// TestSpaceStreamWriteFailsMidLoop drives the writeSSE-returns-false branch in
// the live loop: the writer fails on its first write, so the first pushed dot
// makes the handler return.
func TestSpaceStreamWriteFailsMidLoop(t *testing.T) {
	s := streamServerWithNewEvent(t)
	req := httptest.NewRequest(http.MethodGet, "/space/stream?after=0", nil)
	w := newFailAfterFlusher(0) // every write fails → writeSSE returns false
	done := make(chan struct{})
	go func() { s.handleSpaceStream(w, req); close(done) }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatalf("handler did not return after write failure")
	}
}

// TestSpaceStreamPingFails drives the heartbeat (ping) write-error branch: the
// dot writes succeed, then the ping write fails and the handler returns.
func TestSpaceStreamPingFails(t *testing.T) {
	s := streamServerWithNewEvent(t)
	req := httptest.NewRequest(http.MethodGet, "/space/stream?after=0", nil)
	// writeSSE for one dot performs 3 writes; let those succeed, fail the ping.
	w := newFailAfterFlusher(3)
	done := make(chan struct{})
	go func() { s.handleSpaceStream(w, req); close(done) }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatalf("handler did not return after ping failure")
	}
}

// TestReadSinceSkipsFiltered covers readSince's survives()==false skip: a query
// stage filters out the new event so no dot is emitted.
func TestReadSinceSkipsFiltered(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = ndjsonLines(`{"id":"030","subject":"/users/1","type":"login","time":"t"}`)
	f.connect(s)

	// A stage that only keeps /orders → the /users event is filtered out.
	s.pipeline = []queryStage{{Subject: "/orders"}}
	dots, max := s.readSince(context.Background(), "000", spaceFilter{})
	if len(dots) != 0 {
		t.Errorf("expected filtered (0 dots), got %d", len(dots))
	}
	if max != "030" {
		t.Errorf("max should still advance to 030, got %q", max)
	}
}

func TestCurrentMaxIDAndReadSince(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = ndjsonLines(
		`{"id":"010","subject":"/orders/1","type":"created","time":"t"}`,
		`{"id":"020","subject":"/orders/2","type":"shipped","time":"t"}`,
	)
	f.connect(s)

	if max := s.currentMaxID(context.Background()); max != "020" {
		t.Errorf("currentMaxID = %q, want 020", max)
	}

	// readSince after "010" → only the 020 event survives.
	dots, max := s.readSince(context.Background(), "010", spaceFilter{})
	if max != "020" {
		t.Errorf("readSince max = %q, want 020", max)
	}
	if len(dots) != 1 || dots[0].ID != "020" {
		t.Fatalf("readSince dots = %+v", dots)
	}

	// Offline readSince → nil, empty.
	s.clio.SetTarget("", "")
	if dots, max := s.readSince(context.Background(), "", spaceFilter{}); dots != nil || max != "" {
		t.Errorf("offline readSince = %+v %q", dots, max)
	}
	if max := s.currentMaxID(context.Background()); max != "" {
		t.Errorf("offline currentMaxID = %q", max)
	}
}

func TestWriteSSE(t *testing.T) {
	// Successful write.
	rec := httptest.NewRecorder()
	if !writeSSE(rec, "dot", map[string]string{"a": "b"}) {
		t.Errorf("writeSSE should succeed")
	}
	if !strings.Contains(rec.Body.String(), "event: dot") {
		t.Errorf("missing event line: %q", rec.Body.String())
	}
	// Unmarshalable payload → returns true (skip), writes nothing.
	rec2 := httptest.NewRecorder()
	if !writeSSE(rec2, "dot", make(chan int)) {
		t.Errorf("writeSSE with bad payload should return true")
	}
	if rec2.Body.Len() != 0 {
		t.Errorf("bad payload should not write")
	}
	// Broken writer → returns false (the very first write fails).
	if writeSSE(brokenWriter{}, "dot", map[string]string{"a": "b"}) {
		t.Errorf("writeSSE on broken writer should return false")
	}
	// Writer that fails only on the final "\n\n" write (first two succeed) →
	// returns false via the last write-error branch.
	if writeSSE(newFailAfterFlusher(2), "dot", map[string]string{"a": "b"}) {
		t.Errorf("writeSSE should return false when the final write fails")
	}
	// Writer that fails on the payload (second) write → returns false.
	if writeSSE(newFailAfterFlusher(1), "dot", map[string]string{"a": "b"}) {
		t.Errorf("writeSSE should return false when the payload write fails")
	}
}
