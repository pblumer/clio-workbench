// Package server wires the Workbench HTTP surface: embedded UI, draft store
// handlers and the /api reverse proxy to an upstream Clio.
package server

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/config"
	"github.com/pblumer/clio-workbench/internal/envstore"
	"github.com/pblumer/clio-workbench/internal/scenario"
	"github.com/pblumer/clio-workbench/internal/schemagen"
	"github.com/pblumer/clio-workbench/internal/store"
	"github.com/pblumer/clio-workbench/web"
)

// Server holds the Workbench dependencies and routing.
type Server struct {
	cfg       config.Config
	store     *store.Store
	envs      *envstore.Store
	scenarios *scenario.Store
	clio      *clio.Client
	log       *slog.Logger
	tmpl      *template.Template
	mux       *http.ServeMux

	// pipeline is the in-memory chain of refinement queries that further
	// narrow the active environment's scope (an exploration funnel). It is
	// session state, deliberately not persisted: it resets on restart.
	pipelineMu sync.Mutex
	pipeline   []queryStage
}

// New constructs a Server with routes registered.
func New(cfg config.Config, st *store.Store, envs *envstore.Store, scen *scenario.Store, log *slog.Logger) (*Server, error) {
	// root is captured by the "partial" func below so the shell can render a
	// View's body template chosen at runtime (the {{template}} action only
	// accepts a constant name). It is assigned right after ParseFS.
	var root *template.Template
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"eventSchema": schemagen.EventSchema,
		"inc":         func(i int) int { return i + 1 },
		"partial": func(name string, data any) (template.HTML, error) {
			var b bytes.Buffer
			if err := root.ExecuteTemplate(&b, name, data); err != nil {
				return "", err
			}
			return template.HTML(b.String()), nil //nolint:gosec // trusted embedded templates
		},
	}).ParseFS(web.Templates, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	root = tmpl
	s := &Server{
		cfg:       cfg,
		store:     st,
		envs:      envs,
		scenarios: scen,
		clio:      clio.New(cfg.ClioURL, cfg.ClioToken),
		log:       log,
		tmpl:      tmpl,
		mux:       http.NewServeMux(),
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
	s.mux.HandleFunc("GET /space/stream", s.handleSpaceStream)
	s.mux.HandleFunc("GET /space/event", s.handleSpaceEvent)
	s.mux.HandleFunc("GET /queries", s.handleQueries)
	s.mux.HandleFunc("POST /queries", s.handleAddQuery)
	s.mux.HandleFunc("POST /queries/delete", s.handleDeleteQuery)
	s.mux.HandleFunc("POST /queries/clear", s.handleClearQueries)
	s.mux.HandleFunc("GET /process", s.handleProcess)
	s.mux.HandleFunc("GET /node-events", s.handleNodeEvents)
	s.mux.HandleFunc("GET /relations", s.handleRelations)
	s.mux.HandleFunc("GET /environments", s.handleEnvironments)
	s.mux.HandleFunc("POST /environments", s.handleSaveEnvironment)
	s.mux.HandleFunc("POST /environments/activate", s.handleActivateEnvironment)
	s.mux.HandleFunc("POST /environments/delete", s.handleDeleteEnvironment)
	s.mux.HandleFunc("POST /conformance", s.handleConformance)

	// Test Studio (docs/TESTSTUDIO.md): schema-test (WP-2).
	s.mux.HandleFunc("GET /studio/schema-test", s.handleStudioSchema)
	s.mux.HandleFunc("GET /studio/schema-test/fields", s.handleStudioSchemaFields)
	s.mux.HandleFunc("POST /studio/schema-test", s.handleStudioSchemaCheck)

	// Test Studio: scenario editor + sequence tests + path view (WP-4).
	s.mux.HandleFunc("GET /studio/scenarios", s.handleScenarios)
	s.mux.HandleFunc("POST /studio/scenarios", s.handleCreateSuite)
	s.mux.HandleFunc("POST /studio/scenarios/{suite}/delete", s.handleDeleteSuite)
	s.mux.HandleFunc("POST /studio/scenarios/{suite}/cases", s.handleAddCase)
	s.mux.HandleFunc("POST /studio/scenarios/{suite}/cases/{case}/delete", s.handleDeleteCase)
	s.mux.HandleFunc("POST /studio/scenarios/{suite}/run", s.handleRunSuite)

	// Test Studio: generator — property sampling + mutation + report (WP-6).
	s.mux.HandleFunc("GET /studio/generator", s.handleGenerator)
	s.mux.HandleFunc("POST /studio/generator/run", s.handleGeneratorRun)
	s.mux.HandleFunc("GET /studio/generator/report", s.handleGeneratorReport)
	s.mux.HandleFunc("GET /drafts", s.handleListDrafts)
	s.mux.HandleFunc("POST /drafts", s.handleCreateDraft)
	s.mux.HandleFunc("GET /drafts/{id}", s.handleGetDraft)
	s.mux.HandleFunc("DELETE /drafts/{id}", s.handleDeleteDraft)

	// Outline process editor.
	s.mux.HandleFunc("GET /editor/{id}", s.handleEditor)
	s.mux.HandleFunc("POST /drafts/{id}/meta", s.handleSaveMeta)
	s.mux.HandleFunc("POST /drafts/{id}/steps", s.handleAddStep)
	s.mux.HandleFunc("POST /drafts/{id}/steps/{stepId}", s.handleUpdateStep)
	s.mux.HandleFunc("POST /drafts/{id}/steps/{stepId}/move", s.handleMoveStep)
	s.mux.HandleFunc("DELETE /drafts/{id}/steps/{stepId}", s.handleDeleteStep)
	s.mux.HandleFunc("POST /drafts/{id}/steps/{stepId}/fields", s.handleAddField)
	s.mux.HandleFunc("POST /drafts/{id}/steps/{stepId}/fields/{fieldId}", s.handleUpdateField)
	s.mux.HandleFunc("DELETE /drafts/{id}/steps/{stepId}/fields/{fieldId}", s.handleDeleteField)
	s.mux.HandleFunc("GET /drafts/{id}/export/schemas", s.handleExportSchemas)
	s.mux.HandleFunc("GET /drafts/{id}/export/bpmn", s.handleExportBPMN)

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
