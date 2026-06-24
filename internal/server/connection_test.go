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
	cfg.EventCap = 1000
	s := newTestServer(t, cfg)
	f := newFakeClio(t)
	f.infoJSON = `{"eventsTotal":55723}`
	f.connect(s)

	rec := s.do(http.MethodGet, "/connection", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	// The warning must read loaded-of-total in plain language, with thousands
	// separators, so the numbers are stimmig auf den ersten Blick.
	if !strings.Contains(body, "loaded 1,000 of 55,723") {
		t.Errorf("connection body missing loaded/total warning:\n%s", body)
	}
	// The newest 54,723 events are beyond the cap and not loaded.
	if !strings.Contains(body, "54,723") {
		t.Errorf("connection body missing not-loaded count (54,723):\n%s", body)
	}
}

// TestConnectionOnlineNoCap guards the default: with no read cap (EventCap 0)
// every event is loaded, so no limit warning may appear even when the store is
// large. The activeScope must carry an unlimited (0) read.
func TestConnectionOnlineNoCap(t *testing.T) {
	cfg := defaultCfg()
	cfg.EventCap = 0 // no cap — the new default
	s := newTestServer(t, cfg)
	f := newFakeClio(t)
	f.infoJSON = `{"eventsTotal":55723}`
	f.connect(s)

	if sc := s.activeScope(); sc.Limit != 0 {
		t.Fatalf("activeScope Limit = %d, want 0 (unlimited)", sc.Limit)
	}

	rec := s.do(http.MethodGet, "/connection", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	// The store size is still shown, but no limit warning.
	if !strings.Contains(body, "55,723 ev") {
		t.Errorf("connection body should show the store size:\n%s", body)
	}
	if strings.Contains(body, "loaded") || strings.Contains(body, "limit-warn") {
		t.Errorf("no-cap connection must not warn about a limit:\n%s", body)
	}

	// The environment panel reads "no limit" rather than "limit 0".
	envRec := s.do(http.MethodGet, "/environments", nil)
	envBody := envRec.Body.String()
	if !strings.Contains(envBody, "no limit") || strings.Contains(envBody, "limit 0") {
		t.Errorf("env panel should read \"no limit\":\n%s", envBody)
	}
}
