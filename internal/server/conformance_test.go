package server

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/process"
)

// A BPMN with a lane subject so ScopePrefix is non-empty (drives the scoped
// read path in readForConformance).
const laneBPMN = `<?xml version="1.0"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <bpmn:process id="P">
    <bpmn:laneSet><bpmn:lane id="L" name="/orders/{id}"/></bpmn:laneSet>
    <bpmn:startEvent id="s" name="created"><bpmn:outgoing>f1</bpmn:outgoing></bpmn:startEvent>
    <bpmn:endEvent id="e" name="shipped"/>
    <bpmn:sequenceFlow id="f1" sourceRef="s" targetRef="e"/>
  </bpmn:process>
</bpmn:definitions>`

// A BPMN without a lane → ScopePrefix is empty (full-store read path).
const plainBPMN = `<?xml version="1.0"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <bpmn:process id="P">
    <bpmn:startEvent id="s" name="created"><bpmn:outgoing>f1</bpmn:outgoing></bpmn:startEvent>
    <bpmn:endEvent id="e" name="shipped"/>
    <bpmn:sequenceFlow id="f1" sourceRef="s" targetRef="e"/>
  </bpmn:process>
</bpmn:definitions>`

// multipartBPMN builds a multipart body with a "bpmn" file part.
func multipartBPMN(t *testing.T, field, filename, content string) (string, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return mw.FormDataContentType(), &buf
}

func postMultipart(t *testing.T, s *Server, target, ct string, body *bytes.Buffer) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHandleConformanceOK(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	// Two subjects, each created→shipped, matching the model exactly.
	f.ndjson = ndjsonLines(
		`{"id":"1","subject":"/orders/1","type":"created","time":"t1"}`,
		`{"id":"2","subject":"/orders/1","type":"shipped","time":"t2"}`,
		`{"id":"3","subject":"/orders/2","type":"created","time":"t3"}`,
		`{"id":"4","subject":"/orders/2","type":"shipped","time":"t4"}`,
	)
	f.connect(s)

	ct, body := multipartBPMN(t, "bpmn", "model.bpmn", laneBPMN)
	rec := postMultipart(t, s, "/conformance", ct, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "conform") {
		t.Errorf("missing conformance result, got:\n%s", rec.Body.String())
	}
}

func TestHandleConformancePlainModelFullRead(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = ndjsonLines(
		`{"id":"1","subject":"/orders/1","type":"created","time":"t1"}`,
		`{"id":"2","subject":"/orders/1","type":"shipped","time":"t2"}`,
	)
	f.connect(s)

	ct, body := multipartBPMN(t, "bpmn", "model.bpmn", plainBPMN)
	rec := postMultipart(t, s, "/conformance", ct, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleConformanceOffline(t *testing.T) {
	s := newTestServer(t, defaultCfg()) // no clio
	ct, body := multipartBPMN(t, "bpmn", "model.bpmn", laneBPMN)
	rec := postMultipart(t, s, "/conformance", ct, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no Clio is connected") {
		t.Errorf("expected offline message, got:\n%s", rec.Body.String())
	}
}

func TestHandleConformanceUnauthorized(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.status = http.StatusUnauthorized
	f.connect(s)

	// Plain model → ReadEvents (full read) returns ErrUnauthorized directly.
	ct, body := multipartBPMN(t, "bpmn", "model.bpmn", plainBPMN)
	rec := postMultipart(t, s, "/conformance", ct, body)
	if !strings.Contains(rec.Body.String(), "rejected the token") {
		t.Errorf("expected unauthorized message, got:\n%s", rec.Body.String())
	}
}

func TestHandleConformanceNoBPMNFile(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	// Multipart with a wrong field name → FormFile("bpmn") fails.
	ct, body := multipartBPMN(t, "other", "x.txt", "hello")
	rec := postMultipart(t, s, "/conformance", ct, body)
	if !strings.Contains(rec.Body.String(), "choose a .bpmn file first") {
		t.Errorf("expected file-required message, got:\n%s", rec.Body.String())
	}
}

func TestHandleConformanceInvalidBPMN(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	ct, body := multipartBPMN(t, "bpmn", "x.bpmn", "<<<not xml>>>")
	rec := postMultipart(t, s, "/conformance", ct, body)
	if !strings.Contains(rec.Body.String(), "not a valid BPMN file") {
		t.Errorf("expected invalid-bpmn message, got:\n%s", rec.Body.String())
	}
}

func TestHandleConformanceNoExpectedEvents(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	// Valid XML BPMN but no named events → empty Expected.
	const emptyBPMN = `<?xml version="1.0"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <bpmn:process id="P">
    <bpmn:startEvent id="s"/>
  </bpmn:process>
</bpmn:definitions>`
	ct, body := multipartBPMN(t, "bpmn", "x.bpmn", emptyBPMN)
	rec := postMultipart(t, s, "/conformance", ct, body)
	if !strings.Contains(rec.Body.String(), "no message/start/catch/end events") {
		t.Errorf("expected no-events message, got:\n%s", rec.Body.String())
	}
}

func TestHandleConformanceBadMultipart(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	// Declared multipart but a malformed body → ParseMultipartForm fails.
	req := httptest.NewRequest(http.MethodPost, "/conformance", strings.NewReader("garbage"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "could not read upload") {
		t.Errorf("expected upload error, got:\n%s", rec.Body.String())
	}
}

// TestReadForConformanceServerError drives the generic-error branch: the scoped
// read returns ErrUnauthorized, the fallback full read also returns it, so the
// handler renders the "error" or "unauthorized" branch. Here we use a lane
// model + 500 so both reads fail with a non-sentinel error → "error" state.
func TestHandleConformanceServerError(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.status = http.StatusInternalServerError
	f.connect(s)

	ct, body := multipartBPMN(t, "bpmn", "model.bpmn", plainBPMN)
	rec := postMultipart(t, s, "/conformance", ct, body)
	if !strings.Contains(rec.Body.String(), "could not read events from Clio") {
		t.Errorf("expected generic error message, got:\n%s", rec.Body.String())
	}
}

// TestReadForConformanceFallback covers readForConformance's diagnostic
// fallback: the scoped (under-prefix) read returns zero events, so the handler
// falls back to a full read that returns data.
func TestReadForConformanceFallback(t *testing.T) {
	s := newTestServer(t, defaultCfg())

	// Upstream returns events only for the root read, not the scoped subtree.
	root := ndjsonLines(`{"id":"1","subject":"/elsewhere/1","type":"created","time":"t"}`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		if strings.Contains(r.URL.Path, "/orders") {
			_, _ = w.Write(nil) // nothing under the scoped prefix
			return
		}
		_, _ = w.Write([]byte(root))
	}))
	defer upstream.Close()
	s.clio.SetTarget(upstream.URL, "tok")

	model := process.BpmnModel{Subject: "/orders/{id}", Expected: []string{"created"}}
	events, err := s.readForConformance(context.Background(), model)
	if err != nil {
		t.Fatalf("readForConformance: %v", err)
	}
	if len(events) != 1 || events[0].Subject != "/elsewhere/1" {
		t.Fatalf("expected fallback full read, got %+v", events)
	}
}
