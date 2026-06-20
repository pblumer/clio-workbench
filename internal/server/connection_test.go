package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestHandleConnectionOffline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/connection", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OFFLINE") {
		t.Errorf("expected OFFLINE label, got:\n%s", rec.Body.String())
	}
}

func TestHandleConnectionOnline(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.connect(s)

	rec := s.do(http.MethodGet, "/connection", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "UPLINK") {
		t.Errorf("expected UPLINK (online) label, got:\n%s", rec.Body.String())
	}
}

func TestHandleConnect(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)

	rec := s.do(http.MethodPost, "/connect", form(map[string]string{"url": f.server.URL, "token": "secret"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("HX-Trigger") != "clio-changed" {
		t.Errorf("missing HX-Trigger clio-changed")
	}
	if s.clio.BaseURL() != f.server.URL {
		t.Errorf("target not set: %q", s.clio.BaseURL())
	}
	if !s.clio.HasToken() {
		t.Errorf("token not stored")
	}
}

func TestHandleConnectBadForm(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodPost, "/connect", &badReader{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleDisconnect(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.connect(s)

	rec := s.do(http.MethodPost, "/disconnect", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("HX-Trigger") != "clio-changed" {
		t.Errorf("missing HX-Trigger clio-changed")
	}
	if s.clio.Configured() {
		t.Errorf("clio should be cleared")
	}
}

// TestConnectionViewUnauthorized drives the AUTH FAIL branch via a 401 upstream.
func TestConnectionViewUnauthorized(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.status = http.StatusUnauthorized
	f.connect(s)

	rec := s.do(http.MethodGet, "/connection", nil)
	if !strings.Contains(rec.Body.String(), "AUTH FAIL") {
		t.Errorf("expected AUTH FAIL, got:\n%s", rec.Body.String())
	}
}

// TestLogConnectionCheck exercises the startup probe for online and offline.
func TestLogConnectionCheck(t *testing.T) {
	// Offline.
	s := newTestServer(t, defaultCfg())
	s.LogConnectionCheck(context.Background())

	// Online.
	f := newFakeClio(t)
	f.connect(s)
	s.LogConnectionCheck(context.Background())

	// Unreachable/warn branch: point at a closed server.
	f2 := newFakeClio(t)
	url := f2.server.URL
	f2.server.Close()
	s.clio.SetTarget(url, "")
	s.LogConnectionCheck(context.Background())
}

// TestConnectionOnlineLimitHit triggers the LimitHit branch in connectionView
// by making the instance report more events than the effective cap.
func TestConnectionOnlineLimitHit(t *testing.T) {
	cfg := defaultCfg()
	cfg.EventCap = 1
	s := newTestServer(t, cfg)
	f := newFakeClio(t)
	f.infoJSON = `{"eventsTotal":1000}`
	f.connect(s)

	rec := s.do(http.MethodGet, "/connection", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
