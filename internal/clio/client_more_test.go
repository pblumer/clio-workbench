package clio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResultOK(t *testing.T) {
	if !(Result{Status: StatusOnline}).OK() {
		t.Error("online result: OK() = false, want true")
	}
	for _, s := range []Status{StatusOffline, StatusUnauthorized, StatusUnreachable} {
		if (Result{Status: s}).OK() {
			t.Errorf("status %q: OK() = true, want false", s)
		}
	}
}

func TestSnapshotAndAccessors(t *testing.T) {
	c := New("http://example.test/", "tok")
	base, token := c.Snapshot()
	if base != "http://example.test" || token != "tok" {
		t.Fatalf("Snapshot = %q,%q", base, token)
	}
	if !c.HasToken() {
		t.Error("HasToken = false, want true")
	}
	c.SetTarget("  http://other.test/  ", "")
	if got := c.BaseURL(); got != "http://other.test" {
		t.Errorf("BaseURL = %q, want trimmed", got)
	}
	if c.HasToken() {
		t.Error("HasToken = true after clearing token")
	}
}

func TestOptionsIgnoreInvalid(t *testing.T) {
	// nil HTTP client and non-positive timeout are ignored, leaving defaults.
	c := New("http://example.test", "tok", WithHTTPClient(nil), WithTimeout(0))
	if c.httpc == nil {
		t.Fatal("httpc became nil")
	}
	if c.httpc.Timeout != defaultTimeout {
		t.Errorf("Timeout = %v, want default %v", c.httpc.Timeout, defaultTimeout)
	}
}

func TestCheckConnection_BadURL(t *testing.T) {
	// A control character in the URL makes http.NewRequestWithContext fail
	// before any request is sent.
	c := New("http://exa\x7fmple", "tok")
	res := c.CheckConnection(context.Background())
	if res.Status != StatusUnreachable {
		t.Fatalf("status = %q, want unreachable", res.Status)
	}
	if res.Latency != 0 {
		t.Errorf("latency = %v, want 0 (no request sent)", res.Latency)
	}
}

func TestCheckConnection_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	res := c.CheckConnection(ctx)
	if res.Status != StatusUnreachable {
		t.Fatalf("status = %q, want unreachable", res.Status)
	}
	if !strings.Contains(res.Detail, "canceled") {
		t.Errorf("detail = %q, want it to mention cancellation", res.Detail)
	}
}

func TestReadEventTypes_BadURL(t *testing.T) {
	c := New("http://exa\x7fmple", "tok")
	if _, err := c.ReadEventTypes(context.Background()); err == nil {
		t.Fatal("expected request build error, got nil")
	}
}

func TestReadEventTypes_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	u := srv.URL
	srv.Close()
	c := New(u, "tok", WithTimeout(500*time.Millisecond))
	if _, err := c.ReadEventTypes(context.Background()); err == nil {
		t.Fatal("expected transport error, got nil")
	}
}

func TestReadEventTypes_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	_, err := c.ReadEventTypes(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexpected HTTP 500") {
		t.Fatalf("err = %v, want unexpected HTTP 500", err)
	}
}

func TestReadEventTypes_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json}\n"))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	_, err := c.ReadEventTypes(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode event type") {
		t.Fatalf("err = %v, want decode event type error", err)
	}
}

func TestFetchInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/info" {
			t.Errorf("path = %q, want /api/v1/info", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"eventsTotal":42,"activeObservers":3,"databaseFileBytes":1024,"uptime":"1h"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	info, err := c.FetchInfo(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.EventsTotal != 42 || info.ActiveObservers != 3 || info.DatabaseFileBytes != 1024 || info.Uptime != "1h" {
		t.Fatalf("info = %+v", info)
	}
}

func TestFetchInfo_Offline(t *testing.T) {
	c := New("", "tok")
	if _, err := c.FetchInfo(context.Background()); !errors.Is(err, ErrOffline) {
		t.Fatalf("err = %v, want ErrOffline", err)
	}
}

func TestFetchInfo_BadURL(t *testing.T) {
	c := New("http://exa\x7fmple", "tok")
	if _, err := c.FetchInfo(context.Background()); err == nil {
		t.Fatal("expected request build error, got nil")
	}
}

func TestFetchInfo_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	u := srv.URL
	srv.Close()
	c := New(u, "tok", WithTimeout(500*time.Millisecond))
	if _, err := c.FetchInfo(context.Background()); err == nil {
		t.Fatal("expected transport error, got nil")
	}
}

func TestFetchInfo_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	c := New(srv.URL, "bad", WithHTTPClient(srv.Client()))
	if _, err := c.FetchInfo(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestFetchInfo_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	_, err := c.FetchInfo(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexpected HTTP 502") {
		t.Fatalf("err = %v, want unexpected HTTP 502", err)
	}
}

func TestFetchInfo_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	_, err := c.FetchInfo(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode info") {
		t.Fatalf("err = %v, want decode info error", err)
	}
}

func TestScopedURL(t *testing.T) {
	c := New("http://example.test", "tok")
	// Full scope: subject trimmed, multiple types (empty filtered), bounds.
	sc := Scope{
		Subject:    "/orders/2024/",
		Types:      []string{"placed", "", "paid"},
		LowerBound: "10",
		UpperBound: "99",
	}
	got := c.scopedURL("http://example.test", sc)
	if !strings.HasPrefix(got, "http://example.test/api/v1/events/orders/2024?") {
		t.Fatalf("url = %q, want events/orders/2024 prefix", got)
	}
	for _, want := range []string{"recursive=true", "type=placed", "type=paid", "lowerBound=10", "upperBound=99"} {
		if !strings.Contains(got, want) {
			t.Errorf("url %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "type=&") || strings.HasSuffix(got, "type=") {
		t.Errorf("url %q contains empty type", got)
	}

	// Empty subject → no path segment appended.
	got2 := c.scopedURL("http://example.test", Scope{})
	if !strings.HasPrefix(got2, "http://example.test/api/v1/events?") {
		t.Fatalf("empty-scope url = %q, want bare events path", got2)
	}
}

func TestReadScoped(t *testing.T) {
	const body = `{"id":"1","subject":"/o/1","type":"placed"}
{"id":"2","subject":"/o/1","type":"paid"}
{"id":"3","subject":"/o/1","type":"done"}
`
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))

	evs, err := c.ReadScoped(context.Background(), Scope{Subject: "o/1", Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/events/o/1" {
		t.Errorf("path = %q, want /api/v1/events/o/1", gotPath)
	}
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2 (limit)", len(evs))
	}
}

func TestReadScoped_Offline(t *testing.T) {
	c := New("", "tok")
	if _, err := c.ReadScoped(context.Background(), Scope{}); !errors.Is(err, ErrOffline) {
		t.Fatalf("err = %v, want ErrOffline", err)
	}
}

func TestReadFullScoped(t *testing.T) {
	const body = `{"id":"1","subject":"/o/1","type":"placed","data":{"a":1}}
{"id":"2","subject":"/o/1","type":"paid","data":{"b":2}}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))

	evs, err := c.ReadFullScoped(context.Background(), Scope{Subject: "o/1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evs) != 2 || string(evs[0].Data) != `{"a":1}` {
		t.Fatalf("events = %+v", evs)
	}
}

func TestReadFullScoped_Offline(t *testing.T) {
	c := New("", "tok")
	if _, err := c.ReadFullScoped(context.Background(), Scope{}); !errors.Is(err, ErrOffline) {
		t.Fatalf("err = %v, want ErrOffline", err)
	}
}

func TestReadFullEventsURL_Errors(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		c := New(srv.URL, "bad", WithHTTPClient(srv.Client()))
		if _, err := c.ReadFullScoped(context.Background(), Scope{}); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("err = %v, want ErrUnauthorized", err)
		}
	})
	t.Run("unexpected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}))
		defer srv.Close()
		c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
		_, err := c.ReadFullScoped(context.Background(), Scope{})
		if err == nil || !strings.Contains(err.Error(), "unexpected HTTP 418") {
			t.Fatalf("err = %v, want unexpected HTTP 418", err)
		}
	})
	t.Run("decode", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("{bad}\n"))
		}))
		defer srv.Close()
		c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
		_, err := c.ReadFullScoped(context.Background(), Scope{})
		if err == nil || !strings.Contains(err.Error(), "decode event") {
			t.Fatalf("err = %v, want decode event error", err)
		}
	})
	t.Run("transport", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		u := srv.URL
		srv.Close()
		c := New(u, "tok", WithTimeout(500*time.Millisecond))
		if _, err := c.ReadFullScoped(context.Background(), Scope{}); err == nil {
			t.Fatal("expected transport error, got nil")
		}
	})
}

func TestReadEventsUnder(t *testing.T) {
	t.Run("with-prefix", func(t *testing.T) {
		var gotPath, gotQuery string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"id":"1","subject":"/o/1","type":"x"}` + "\n"))
		}))
		defer srv.Close()
		c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
		evs, err := c.ReadEventsUnder(context.Background(), "/orders/", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotPath != "/api/v1/events/orders" || gotQuery != "recursive=true" {
			t.Errorf("path=%q query=%q", gotPath, gotQuery)
		}
		if len(evs) != 1 {
			t.Fatalf("got %d events, want 1", len(evs))
		}
	})
	t.Run("empty-prefix-falls-back-to-root", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
		if _, err := c.ReadEventsUnder(context.Background(), "  /  ", 0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotPath != "/api/v1/events" {
			t.Errorf("path = %q, want root events path", gotPath)
		}
	})
}

func TestReadEvents_Errors(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()
		c := New(srv.URL, "bad", WithHTTPClient(srv.Client()))
		if _, err := c.ReadEvents(context.Background(), 0); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("err = %v, want ErrUnauthorized", err)
		}
	})
	t.Run("unexpected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()
		c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
		_, err := c.ReadEvents(context.Background(), 0)
		if err == nil || !strings.Contains(err.Error(), "unexpected HTTP 503") {
			t.Fatalf("err = %v, want unexpected HTTP 503", err)
		}
	})
	t.Run("decode", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("{bad}\n"))
		}))
		defer srv.Close()
		c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
		_, err := c.ReadEvents(context.Background(), 0)
		if err == nil || !strings.Contains(err.Error(), "decode event") {
			t.Fatalf("err = %v, want decode event error", err)
		}
	})
	t.Run("transport", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		u := srv.URL
		srv.Close()
		c := New(u, "tok", WithTimeout(500*time.Millisecond))
		if _, err := c.ReadEvents(context.Background(), 0); err == nil {
			t.Fatal("expected transport error, got nil")
		}
	})
}

func TestReadFullEvents(t *testing.T) {
	const body = `{"id":"1","subject":"/o/1","type":"placed","data":{"k":"v"}}
{"id":"2","subject":"/o/1","type":"paid","data":null}
`
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))

	evs, err := c.ReadFullEvents(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/events" || !strings.Contains(gotQuery, "recursive=true") || !strings.Contains(gotQuery, "limit=1") {
		t.Errorf("path=%q query=%q, want recursive=true & limit=1", gotPath, gotQuery)
	}
	if len(evs) != 1 || string(evs[0].Data) != `{"k":"v"}` {
		t.Fatalf("events = %+v", evs)
	}
}

func TestReadFullEvents_Offline(t *testing.T) {
	c := New("", "tok")
	if _, err := c.ReadFullEvents(context.Background(), 0); !errors.Is(err, ErrOffline) {
		t.Fatalf("err = %v, want ErrOffline", err)
	}
}

func TestReadFullEvents_BadURL(t *testing.T) {
	c := New("http://exa\x7fmple", "tok")
	if _, err := c.ReadFullEvents(context.Background(), 0); err == nil {
		t.Fatal("expected request build error, got nil")
	}
}

func TestReadFullEvents_Errors(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		c := New(srv.URL, "bad", WithHTTPClient(srv.Client()))
		if _, err := c.ReadFullEvents(context.Background(), 0); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("err = %v, want ErrUnauthorized", err)
		}
	})
	t.Run("unexpected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
		_, err := c.ReadFullEvents(context.Background(), 0)
		if err == nil || !strings.Contains(err.Error(), "unexpected HTTP 500") {
			t.Fatalf("err = %v, want unexpected HTTP 500", err)
		}
	})
	t.Run("decode", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("{bad}\n"))
		}))
		defer srv.Close()
		c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
		_, err := c.ReadFullEvents(context.Background(), 0)
		if err == nil || !strings.Contains(err.Error(), "decode event") {
			t.Fatalf("err = %v, want decode event error", err)
		}
	})
	t.Run("transport", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		u := srv.URL
		srv.Close()
		c := New(u, "tok", WithTimeout(500*time.Millisecond))
		if _, err := c.ReadFullEvents(context.Background(), 0); err == nil {
			t.Fatal("expected transport error, got nil")
		}
	})
}

func TestReadEventsByType(t *testing.T) {
	const body = `{"id":"1","subject":"/o/1","type":"placed","data":{}}
{"id":"2","subject":"/o/2","type":"placed","data":{}}
{"id":"3","subject":"/o/3","type":"placed","data":{}}
`
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))

	evs, err := c.ReadEventsByType(context.Background(), "order placed", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotQuery, "type=order+placed") && !strings.Contains(gotQuery, "type=order%20placed") {
		t.Errorf("query = %q, want escaped type", gotQuery)
	}
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2 (limit)", len(evs))
	}
}

func TestReadEventsByType_Offline(t *testing.T) {
	c := New("", "tok")
	if _, err := c.ReadEventsByType(context.Background(), "x", 0); !errors.Is(err, ErrOffline) {
		t.Fatalf("err = %v, want ErrOffline", err)
	}
}

func TestReadEventsByType_Errors(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer srv.Close()
		c := New(srv.URL, "bad", WithHTTPClient(srv.Client()))
		if _, err := c.ReadEventsByType(context.Background(), "x", 0); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("err = %v, want ErrUnauthorized", err)
		}
	})
	t.Run("unexpected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
		_, err := c.ReadEventsByType(context.Background(), "x", 0)
		if err == nil || !strings.Contains(err.Error(), "unexpected HTTP 404") {
			t.Fatalf("err = %v, want unexpected HTTP 404", err)
		}
	})
	t.Run("decode", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("{bad}\n"))
		}))
		defer srv.Close()
		c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
		_, err := c.ReadEventsByType(context.Background(), "x", 0)
		if err == nil || !strings.Contains(err.Error(), "decode event") {
			t.Fatalf("err = %v, want decode event error", err)
		}
	})
	t.Run("transport", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		u := srv.URL
		srv.Close()
		c := New(u, "tok", WithTimeout(500*time.Millisecond))
		if _, err := c.ReadEventsByType(context.Background(), "x", 0); err == nil {
			t.Fatal("expected transport error, got nil")
		}
	})
}

func TestReadEventsByType_NoTokenOmitsHeader(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(srv.URL, "", WithHTTPClient(srv.Client()))
	if _, err := c.ReadEventsByType(context.Background(), "x", 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hadAuth {
		t.Fatal("Authorization header sent despite empty token")
	}
}

func TestReadScoped_NoTokenOmitsHeader(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(srv.URL, "", WithHTTPClient(srv.Client()))
	if _, err := c.ReadScoped(context.Background(), Scope{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hadAuth {
		t.Fatal("Authorization header sent despite empty token")
	}
}

func TestReadFullScoped_NoTokenOmitsHeader(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := New(srv.URL, "", WithHTTPClient(srv.Client()))
	if _, err := c.ReadFullScoped(context.Background(), Scope{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hadAuth {
		t.Fatal("Authorization header sent despite empty token")
	}
}

// hugeLine returns an NDJSON line longer than the scanner's max token size so
// bufio.Scanner.Scan returns false with sc.Err() == bufio.ErrTooLong.
func hugeLine(n int) string {
	return `{"id":"` + strings.Repeat("x", n) + `"}` + "\n"
}

func TestReadEventTypes_ScannerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(hugeLine(2 << 20))) // exceeds 1<<20 max
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	_, err := c.ReadEventTypes(context.Background())
	if err == nil || !strings.Contains(err.Error(), "read event types") {
		t.Fatalf("err = %v, want read event types scanner error", err)
	}
}

func TestReadEvents_ScannerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(hugeLine(8 << 20))) // exceeds 4<<20 max
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	_, err := c.ReadEvents(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "read events") {
		t.Fatalf("err = %v, want read events scanner error", err)
	}
}

func TestReadFullScoped_ScannerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(hugeLine(16 << 20))) // exceeds 8<<20 max
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	_, err := c.ReadFullScoped(context.Background(), Scope{})
	if err == nil || !strings.Contains(err.Error(), "read events") {
		t.Fatalf("err = %v, want read events scanner error", err)
	}
}

func TestReadFullEvents_ScannerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(hugeLine(16 << 20)))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	_, err := c.ReadFullEvents(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "read full events") {
		t.Fatalf("err = %v, want read full events scanner error", err)
	}
}

func TestReadEventsByType_ScannerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(hugeLine(16 << 20)))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	_, err := c.ReadEventsByType(context.Background(), "x", 0)
	if err == nil || !strings.Contains(err.Error(), "read events by type") {
		t.Fatalf("err = %v, want read events by type scanner error", err)
	}
}

func TestReadEventsByType_BadURL(t *testing.T) {
	c := New("http://exa\x7fmple", "tok")
	if _, err := c.ReadEventsByType(context.Background(), "x", 0); err == nil {
		t.Fatal("expected request build error, got nil")
	}
}

// TestReadFull_BlankLinesAndLimit exercises the blank-line skip and limit break
// inside the FullEvent NDJSON reader.
func TestReadFull_BlankLinesAndLimit(t *testing.T) {
	const body = `{"id":"1","type":"a","data":{}}

{"id":"2","type":"b","data":{}}
{"id":"3","type":"c","data":{}}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))

	all, err := c.ReadFullEvents(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d events, want 3 (blank line skipped)", len(all))
	}
	limited, err := c.ReadFullScoped(context.Background(), Scope{Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("got %d events, want 2 (limit)", len(limited))
	}
}

func TestReadEventsByType_BlankLineSkip(t *testing.T) {
	const body = `{"id":"1","type":"a","data":{}}

{"id":"2","type":"a","data":{}}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	evs, err := c.ReadEventsByType(context.Background(), "a", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2 (blank line skipped)", len(evs))
	}
}

// TestReadLimitReachesClio guards the core fix: every capped read must carry
// the limit to Clio as a `limit` query param (otherwise Clio applies its own
// default ceiling and the Workbench silently loads fewer events than its limit
// advertises). A non-positive limit is "read all" and must omit the param.
func TestReadLimitReachesClio(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"id":"1","subject":"/o/1","type":"x"}` + "\n"))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	ctx := context.Background()

	cases := []struct {
		name string
		call func()
		want string // "" means: limit must be absent
	}{
		{"ReadScoped", func() { _, _ = c.ReadScoped(ctx, Scope{Limit: 7}) }, "limit=7"},
		{"ReadScoped/all", func() { _, _ = c.ReadScoped(ctx, Scope{}) }, ""},
		{"ReadFullScoped", func() { _, _ = c.ReadFullScoped(ctx, Scope{Limit: 8}) }, "limit=8"},
		{"ReadEvents", func() { _, _ = c.ReadEvents(ctx, 9) }, "limit=9"},
		{"ReadEvents/all", func() { _, _ = c.ReadEvents(ctx, 0) }, ""},
		{"ReadEventsUnder", func() { _, _ = c.ReadEventsUnder(ctx, "/o", 11) }, "limit=11"},
		{"ReadFullEvents", func() { _, _ = c.ReadFullEvents(ctx, 12) }, "limit=12"},
		{"ReadEventsByType", func() { _, _ = c.ReadEventsByType(ctx, "t", 13) }, "limit=13"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotQuery = ""
			tc.call()
			has := strings.Contains(gotQuery, "limit=")
			if tc.want == "" {
				if has {
					t.Errorf("query %q carries a limit, want none", gotQuery)
				}
				return
			}
			if !strings.Contains(gotQuery, tc.want) {
				t.Errorf("query %q missing %q", gotQuery, tc.want)
			}
		})
	}
}

// TestScopedURLLimit checks the limit param is emitted only for a positive cap.
func TestScopedURLLimit(t *testing.T) {
	c := New("http://example.test", "tok")
	if got := c.scopedURL("http://example.test", Scope{Limit: 500}); !strings.Contains(got, "limit=500") {
		t.Errorf("url %q missing limit=500", got)
	}
	if got := c.scopedURL("http://example.test", Scope{}); strings.Contains(got, "limit=") {
		t.Errorf("url %q should not carry a limit", got)
	}
}

// TestWithLimit covers the small URL helper directly, including the
// query-separator choice and the read-all no-op.
func TestWithLimit(t *testing.T) {
	cases := []struct {
		raw  string
		n    int
		want string
	}{
		{"http://x/api/v1/events?recursive=true", 5, "http://x/api/v1/events?recursive=true&limit=5"},
		{"http://x/api/v1/events", 5, "http://x/api/v1/events?limit=5"},
		{"http://x/api/v1/events?recursive=true", 0, "http://x/api/v1/events?recursive=true"},
		{"http://x/api/v1/events", -1, "http://x/api/v1/events"},
	}
	for _, tc := range cases {
		if got := withLimit(tc.raw, tc.n); got != tc.want {
			t.Errorf("withLimit(%q, %d) = %q, want %q", tc.raw, tc.n, got, tc.want)
		}
	}
}

func TestFullEventRoundTrip(t *testing.T) {
	// Guard that FullEvent.Data carries raw JSON through unchanged.
	var fe FullEvent
	if err := json.Unmarshal([]byte(`{"id":"1","data":{"nested":[1,2]}}`), &fe); err != nil {
		t.Fatal(err)
	}
	if string(fe.Data) != `{"nested":[1,2]}` {
		t.Fatalf("data = %q", fe.Data)
	}
}
