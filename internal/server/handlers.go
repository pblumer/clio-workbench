package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/store"
)

// indexData is the view model for the start page.
type indexData struct {
	// ClioURL is the currently selected upstream (prefills the connect form).
	ClioURL string
	// HasToken reports whether a token is already set, without revealing it.
	HasToken bool
	DataDir  string
	Drafts   []model.Draft
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return
	}
	s.render(w, "index.html", indexData{
		ClioURL:  s.clio.BaseURL(),
		HasToken: s.clio.HasToken(),
		DataDir:  s.cfg.DataDir,
		Drafts:   drafts,
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("ok"))
}

// handleListDrafts renders the drafts list fragment (HTMX).
func (s *Server) handleListDrafts(w http.ResponseWriter, _ *http.Request) {
	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return
	}
	s.render(w, "drafts.html", drafts)
}

// handleCreateDraft creates a new empty draft from the start form and returns
// the refreshed drafts list fragment.
func (s *Server) handleCreateDraft(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	kind := model.Kind(strings.TrimSpace(r.FormValue("kind")))
	namespace := strings.TrimSpace(r.FormValue("namespace"))
	if namespace == "" {
		namespace = slugify(name)
	}

	draft := &model.Draft{
		ID:        slugify(name),
		Name:      name,
		Kind:      kind,
		Namespace: namespace,
		Nodes:     []model.Node{},
		Edges:     []model.Edge{},
	}
	if draft.ID == "" {
		draft.ID = "draft-" + time.Now().UTC().Format("20060102-150405")
	}

	if err := s.store.Create(draft); err != nil {
		if errors.Is(err, store.ErrExists) {
			http.Error(w, "a draft with that id already exists", http.StatusConflict)
			return
		}
		s.userOrServerError(w, "create draft", err)
		return
	}
	s.log.Info("draft created", "id", draft.ID, "kind", draft.Kind)

	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return
	}
	s.render(w, "drafts.html", drafts)
}

func (s *Server) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, "get draft", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	s.writeJSON(w, d)
}

func (s *Server) handleDeleteDraft(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, "delete draft", err)
		return
	}
	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return
	}
	s.render(w, "drafts.html", drafts)
}

// slugify reduces a label to a URL/file-safe slug.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_' || r == '/':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
