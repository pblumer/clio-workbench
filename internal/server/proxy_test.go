package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pblumer/clio-workbench/internal/clio"
)

func TestProxyInjectsTokenAndStripsPrefix(t *testing.T) {
	var gotPath, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	proxy := newProxy(clio.New(upstream.URL, "s3cret"))

	req := httptest.NewRequest(http.MethodGet, "/api/read-event-types", nil)
	// Client must not be able to inject its own credentials.
	req.Header.Set("Authorization", "Bearer attacker")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if gotPath != "/read-event-types" {
		t.Fatalf("path = %q, want /read-event-types", gotPath)
	}
	if gotAuth != "Bearer s3cret" {
		t.Fatalf("auth = %q, want server-injected token", gotAuth)
	}
}

// TestProxyOfflineAndRuntimeTargetSwitch covers the GUI flow: with no Clio
// selected the proxy 503s; after SetTarget it forwards to the chosen upstream.
func TestProxyOfflineAndRuntimeTargetSwitch(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	c := clio.New("", "")
	proxy := newProxy(c)

	// No target selected → 503.
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ping", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("offline status = %d, want 503", rec.Code)
	}

	// Pick a server at runtime → forwards with the server-side token.
	c.SetTarget(upstream.URL, "runtime-tok")
	rec = httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ping", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status after connect = %d, want 200", rec.Code)
	}
	if gotAuth != "Bearer runtime-tok" {
		t.Fatalf("auth = %q, want Bearer runtime-tok", gotAuth)
	}
}

func TestSingleJoin(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"", "/read-event-types", "/read-event-types"},
		{"/", "/read-event-types", "/read-event-types"},
		{"/base", "/read-event-types", "/base/read-event-types"},
		{"/base/", "read-event-types", "/base/read-event-types"},
		{"/base", "", "/base"},
	}
	for _, c := range cases {
		if got := singleJoin(c.a, c.b); got != c.want {
			t.Errorf("singleJoin(%q,%q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}
