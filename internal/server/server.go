// Package server wires the Workbench HTTP surface: embedded UI, draft store
// handlers and the /api reverse proxy to an upstream Clio.
package server

import (
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/config"
	"github.com/pblumer/clio-workbench/internal/store"
	"github.com/pblumer/clio-workbench/web"
)

// Server holds the Workbench dependencies and routing.
type Server struct {
	cfg   config.Config
	store *store.Store
	clio  *clio.Client
	log   *slog.Logger
	tmpl  *template.Template
	mux   *http.ServeMux
}

// New constructs a Server with routes registered.
func New(cfg config.Config, st *store.Store, log *slog.Logger) (*Server, error) {
	tmpl, err := template.ParseFS(web.Templates, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	s := &Server{
		cfg:   cfg,
		store: st,
		clio:  clio.New(cfg.ClioURL, cfg.ClioToken),
		log:   log,
		tmpl:  tmpl,
		mux:   http.NewServeMux(),
	}
	if err := s.routes(); err != nil {
		return nil, err
	}
	return s, nil
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() error {
	// Static assets (CSS, htmx, canvas JS).
	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		return fmt.Errorf("sub static fs: %w", err)
	}
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", cacheControl(http.FileServerFS(staticFS))))

	// Pages and draft handlers.
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /connection", s.handleConnection)
	s.mux.HandleFunc("POST /connect", s.handleConnect)
	s.mux.HandleFunc("POST /disconnect", s.handleDisconnect)
	s.mux.HandleFunc("GET /space", s.handleSpace)
	s.mux.HandleFunc("GET /process", s.handleProcess)
	s.mux.HandleFunc("GET /node-events", s.handleNodeEvents)
	s.mux.HandleFunc("GET /drafts", s.handleListDrafts)
	s.mux.HandleFunc("POST /drafts", s.handleCreateDraft)
	s.mux.HandleFunc("GET /drafts/{id}", s.handleGetDraft)
	s.mux.HandleFunc("DELETE /drafts/{id}", s.handleDeleteDraft)

	// /api reverse proxy to the upstream Clio (token injected server-side).
	// The target is dynamic: it follows the server picked in the GUI, and
	// 503s when none is selected. Seeded from CLIO_URL at startup.
	s.mux.Handle("/api/", newProxy(s.clio))
	s.log.Info("api proxy ready (dynamic target)", "seed", s.cfg.ClioURL, "configured", s.clio.Configured())
	return nil
}

func cacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		next.ServeHTTP(w, r)
	})
}
