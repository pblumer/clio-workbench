package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestProducerPanel(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraftWithFields(t, s)

	// Default language is Go.
	rec := s.do(http.MethodGet, "/studio/producer?draft=order", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Order", "package producer", "SendPaid", `name="lang"`, "producer.go"} {
		if !strings.Contains(body, want) {
			t.Errorf("panel missing %q", want)
		}
	}

	// Switching language re-renders with that language.
	py := s.do(http.MethodGet, "/studio/producer?draft=order&lang=python", nil)
	if !strings.Contains(py.Body.String(), "@dataclass") || !strings.Contains(py.Body.String(), "producer.py") {
		t.Errorf("python panel not rendered: %s", py.Body.String())
	}

	// Unknown language falls back to Go.
	fb := s.do(http.MethodGet, "/studio/producer?draft=order&lang=cobol", nil)
	if !strings.Contains(fb.Body.String(), "package producer") {
		t.Errorf("unknown lang should fall back to go")
	}
}

func TestProducerPanelEmpty(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/studio/producer", nil)
	if !strings.Contains(rec.Body.String(), "Noch keine Modelle") {
		t.Fatalf("expected empty state")
	}
}

func TestProducerDownload(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraftWithFields(t, s)

	rec := s.do(http.MethodGet, "/studio/producer/download?draft=order&lang=ts", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "producer.ts") {
		t.Errorf("disposition = %q", cd)
	}
	if !strings.Contains(rec.Body.String(), "export interface") {
		t.Errorf("download body not TS: %s", rec.Body.String())
	}
}

func TestProducerDownloadUnknownLangIs400(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraftWithFields(t, s)
	rec := s.do(http.MethodGet, "/studio/producer/download?draft=order&lang=cobol", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestProducerDownloadUnknownDraftIs404(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/studio/producer/download?draft=ghost&lang=go", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestFilenameForFallback(t *testing.T) {
	if filenameFor("go") != "producer.go" {
		t.Errorf("known lang filename")
	}
	if filenameFor("bogus") != "producer.txt" {
		t.Errorf("unknown lang should fall back to producer.txt")
	}
}

func TestProducerDownloadDecodeErrorIs500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedGraphDraftWithFields(t, s)
	corruptDraft(t, s, "order")
	rec := s.do(http.MethodGet, "/studio/producer/download?draft=order&lang=go", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
