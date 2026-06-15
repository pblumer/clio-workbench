package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pblumer/clio-workbench/internal/config"
)

func TestProxyInjectsTokenAndStripsPrefix(t *testing.T) {
	var gotPath, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	proxy, err := newProxy(config.Config{ClioURL: upstream.URL, ClioToken: "s3cret"})
	if err != nil {
		t.Fatalf("newProxy: %v", err)
	}

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
