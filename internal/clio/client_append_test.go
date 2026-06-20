package clio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAppendEvent(t *testing.T) {
	var gotPath, gotAuth, gotCT, gotBody, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
	if err := c.AppendEvent(context.Background(), []byte(`{"type":"x"}`)); err != nil {
		t.Fatalf("append: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != appendPath {
		t.Errorf("method/path = %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "application/cloudevents+json") {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotBody != `{"type":"x"}` {
		t.Errorf("body = %q", gotBody)
	}
}

func TestAppendEventTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listening now → dial fails
	c := New(url, "tok")
	if err := c.AppendEvent(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected a transport error")
	}
}

func TestAppendEventOffline(t *testing.T) {
	c := New("", "tok")
	if err := c.AppendEvent(context.Background(), []byte(`{}`)); !errors.Is(err, ErrOffline) {
		t.Fatalf("offline = %v, want ErrOffline", err)
	}
}

func TestAppendEventStatuses(t *testing.T) {
	tests := []struct {
		code    int
		wantErr error
		any     bool
	}{
		{http.StatusUnauthorized, ErrUnauthorized, false},
		{http.StatusForbidden, ErrUnauthorized, false},
		{http.StatusInternalServerError, nil, true},
	}
	for _, tc := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.code)
		}))
		c := New(srv.URL, "tok", WithHTTPClient(srv.Client()))
		err := c.AppendEvent(context.Background(), []byte(`{}`))
		srv.Close()
		if tc.any {
			if err == nil {
				t.Errorf("status %d: want an error", tc.code)
			}
			continue
		}
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("status %d: err = %v, want %v", tc.code, err, tc.wantErr)
		}
	}
}
