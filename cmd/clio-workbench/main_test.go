package main

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// quietLog is a logger that discards output so test runs stay clean.
func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// freeAddr returns a currently-free 127.0.0.1 address. There is a tiny race
// between closing the probe listener and re-binding, which is acceptable for a
// local test.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// TestRun_StoreOpenError: when the data dir cannot be created (it is an existing
// regular file), store.Open fails and run returns that error.
func TestRun_StoreOpenError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKBENCH_DATA", f)
	t.Setenv("CLIO_URL", "")

	if err := run(quietLog()); err == nil {
		t.Fatal("expected error from store.Open, got nil")
	}
}

// TestRun_EnvstoreOpenError: the data dir is valid (store.Open succeeds and
// skips directories), but environments.json is a directory, so envstore.Open's
// ReadFile fails and run returns that error.
func TestRun_EnvstoreOpenError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "environments.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKBENCH_DATA", dir)
	t.Setenv("CLIO_URL", "")

	if err := run(quietLog()); err == nil {
		t.Fatal("expected error from envstore.Open, got nil")
	}
}

// TestRun_ListenError: when the configured address is already in use,
// ListenAndServe fails and run surfaces the error via errCh.
func TestRun_ListenError(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy port: %v", err)
	}
	defer l.Close()

	t.Setenv("WORKBENCH_DATA", t.TempDir())
	t.Setenv("WORKBENCH_ADDR", l.Addr().String())
	t.Setenv("CLIO_URL", "")

	if err := run(quietLog()); err == nil {
		t.Fatal("expected ListenAndServe error, got nil")
	}
}

// TestRun_Lifecycle: run starts, serves requests, then a SIGTERM triggers the
// graceful-shutdown path and run returns nil. Waiting for the server to answer
// guarantees the signal handler is installed before we raise the signal.
func TestRun_Lifecycle(t *testing.T) {
	addr := freeAddr(t)
	t.Setenv("WORKBENCH_DATA", t.TempDir())
	t.Setenv("WORKBENCH_ADDR", addr)
	t.Setenv("CLIO_URL", "")

	done := make(chan error, 1)
	go func() { done <- run(quietLog()) }()

	// Wait until the server answers (and thus NotifyContext is registered).
	url := "http://" + addr + "/"
	up := false
	for i := 0; i < 100; i++ {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			up = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !up {
		t.Fatal("server did not come up")
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("run returned error on shutdown: %v", err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("run did not shut down after SIGTERM")
	}
}
