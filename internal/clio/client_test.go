package clio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckConnection_Offline(t *testing.T) {
	c := New("", "tok")
	res := c.CheckConnection(context.Background())
	if res.Status != StatusOffline {
		t.Fatalf("got %q, want %q", res.Status, StatusOffline)
	}
	if c.Configured() {
		t.Fatal("Configured() = true for empty base URL")
	}
}

func TestCheckConnection_StatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       Status
	}{
		{"ok", http.StatusOK, StatusOnline},
		{"no-content", http.StatusNoContent, StatusOnline},
		{"unauthorized", http.StatusUnauthorized, StatusUnauthorized},
		{"forbidden", http.StatusForbidden, StatusUnauthorized},
		{"server-error", http.StatusInternalServerError, StatusUnreachable},
		{"not-found", http.StatusNotFound, StatusUnreachable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != probePath {
					t.Errorf("probed %q, want %q", r.URL.Path, probePath)
				}
				if r.Method != http.MethodGet {
					t.Errorf("method %q, want GET", r.Method)
				}
				w.WriteHeader(tc.statusCode)
			}))
			defer srv.Close()

			c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
			res := c.CheckConnection(context.Background())
			if res.Status != tc.want {
				t.Fatalf("status %d → %q, want %q (detail %q)", tc.statusCode, res.Status, tc.want, res.Detail)
			}
			if res.Latency <= 0 {
				t.Errorf("latency not measured: %v", res.Latency)
			}
			if res.Detail == "" {
				t.Error("detail is empty")
			}
		})
	}
}

func TestCheckConnection_TokenInjection(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "s3cr3t", WithHTTPClient(srv.Client()))
	if res := c.CheckConnection(context.Background()); res.Status != StatusOnline {
		t.Fatalf("status = %q, want online", res.Status)
	}
	if want := "Bearer s3cr3t"; gotAuth != want {
		t.Fatalf("Authorization = %q, want %q", gotAuth, want)
	}
}

func TestCheckConnection_NoTokenOmitsHeader(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "", WithHTTPClient(srv.Client()))
	c.CheckConnection(context.Background())
	if hadAuth {
		t.Fatal("Authorization header sent despite empty token")
	}
}

func TestCheckConnection_Unreachable(t *testing.T) {
	// Point at a server we immediately close so the dial fails.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := New(url, "tok", WithTimeout(500*time.Millisecond))
	res := c.CheckConnection(context.Background())
	if res.Status != StatusUnreachable {
		t.Fatalf("status = %q, want %q (detail %q)", res.Status, StatusUnreachable, res.Detail)
	}
	if !strings.Contains(res.Detail, "connection failed") {
		t.Errorf("detail = %q, want it to mention connection failure", res.Detail)
	}
}

func TestCheckConnection_ContextTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // hang until the client gives up
		close(block)
	}))
	defer srv.Close()
	defer func() { <-block }()

	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	res := c.CheckConnection(ctx)
	if res.Status != StatusUnreachable {
		t.Fatalf("status = %q, want %q", res.Status, StatusUnreachable)
	}
	if !strings.Contains(res.Detail, "timeout") {
		t.Errorf("detail = %q, want it to mention timeout", res.Detail)
	}
}

func TestNewTrimsTrailingSlash(t *testing.T) {
	c := New("http://example.test/", "tok")
	if c.baseURL != "http://example.test" {
		t.Fatalf("baseURL = %q, want trailing slash trimmed", c.baseURL)
	}
}
