package clio

import (
	"context"
	"errors"
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

func TestReadEventTypes_ParsesNDJSON(t *testing.T) {
	const body = `{"type":"order-placed","count":3,"hasSchema":true}
{"type":"order-shipped","count":2,"hasSchema":false}

{"type":"order-cancelled","count":1,"hasSchema":true}
`
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	types, err := c.ReadEventTypes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer tok")
	}
	if len(types) != 3 {
		t.Fatalf("got %d types, want 3 (blank lines skipped): %+v", len(types), types)
	}
	total := 0
	for _, et := range types {
		total += et.Count
	}
	if total != 6 {
		t.Errorf("total count = %d, want 6", total)
	}
	if types[0].Type != "order-placed" || types[0].Count != 3 || !types[0].HasSchema {
		t.Errorf("first type = %+v, want order-placed/3/true", types[0])
	}
}

func TestReadEventTypes_Offline(t *testing.T) {
	c := New("", "tok")
	if _, err := c.ReadEventTypes(context.Background()); !errors.Is(err, ErrOffline) {
		t.Fatalf("err = %v, want ErrOffline", err)
	}
}

func TestReadEventTypes_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(srv.URL, "bad", WithHTTPClient(srv.Client()))
	if _, err := c.ReadEventTypes(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestReadEvents_ParsesAndLimits(t *testing.T) {
	const body = `{"id":"1","subject":"/o/1","type":"placed","time":"t1"}
{"id":"2","subject":"/o/1","type":"paid","time":"t2"}
{"id":"3","subject":"/o/2","type":"placed","time":"t3"}
`
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	evs, err := c.ReadEvents(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/events?recursive=true" {
		t.Errorf("probed %q, want /api/v1/events?recursive=true", gotPath)
	}
	if len(evs) != 3 || evs[1].Type != "paid" || evs[2].Subject != "/o/2" {
		t.Fatalf("parsed events wrong: %+v", evs)
	}

	limited, err := c.ReadEvents(context.Background(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("limit not honoured: got %d, want 2", len(limited))
	}
}

func TestReadEvents_Offline(t *testing.T) {
	c := New("", "")
	if _, err := c.ReadEvents(context.Background(), 0); !errors.Is(err, ErrOffline) {
		t.Fatalf("err = %v, want ErrOffline", err)
	}
}

func TestSetTargetSwitchesState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("", "") // start offline
	if got := c.CheckConnection(context.Background()).Status; got != StatusOffline {
		t.Fatalf("initial status = %q, want offline", got)
	}

	c.SetTarget(srv.URL+"/", "tok") // pick a server at runtime (trailing slash trimmed)
	if !c.Configured() || !c.HasToken() {
		t.Fatalf("after SetTarget: Configured=%v HasToken=%v", c.Configured(), c.HasToken())
	}
	if c.BaseURL() != srv.URL {
		t.Fatalf("BaseURL = %q, want %q", c.BaseURL(), srv.URL)
	}
	c.httpc = srv.Client()
	if got := c.CheckConnection(context.Background()).Status; got != StatusOnline {
		t.Fatalf("after connect status = %q, want online", got)
	}

	c.SetTarget("", "") // disconnect
	if got := c.CheckConnection(context.Background()).Status; got != StatusOffline {
		t.Fatalf("after disconnect status = %q, want offline", got)
	}
}

func TestNewTrimsTrailingSlash(t *testing.T) {
	c := New("http://example.test/", "tok")
	if c.baseURL != "http://example.test" {
		t.Fatalf("baseURL = %q, want trailing slash trimmed", c.baseURL)
	}
}
