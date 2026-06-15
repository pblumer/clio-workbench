// Command clio-workbench is a drawing board for event-sourcing models.
//
// It is a separate, single Go binary that talks to a Clio instance only over
// its public HTTP API (see docs/WORKBENCH.md). This is the Stufe-0 scaffold:
// embedded UI, a file-backed draft store, a start page and the /api reverse
// proxy with server-side token injection.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pblumer/clio-workbench/internal/config"
	"github.com/pblumer/clio-workbench/internal/server"
	"github.com/pblumer/clio-workbench/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg := config.Load()

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}

	srv, err := server.New(cfg, st, log)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("clio-workbench listening", "addr", cfg.Addr, "data", cfg.DataDir)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Probe the upstream Clio without blocking startup: offline drafting must
	// stay possible even when Clio is down (docs/WORKBENCH.md §3.3).
	go srv.LogConnectionCheck(ctx)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}
