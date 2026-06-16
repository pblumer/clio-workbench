package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/model"
)

// effectiveLimit is the read cap for the active environment (its own Limit, or
// the global cap).
func (s *Server) effectiveLimit() int {
	if env, ok := s.envs.Active(); ok && env.Limit > 0 {
		return env.Limit
	}
	return s.cfg.EventCap
}

// activeScope is the clio read scope derived from the active environment.
func (s *Server) activeScope() clio.Scope {
	sc := clio.Scope{Limit: s.cfg.EventCap}
	if env, ok := s.envs.Active(); ok {
		sc.Subject = env.Subject
		sc.Types = env.Types
		sc.LowerBound = env.LowerBound
		sc.UpperBound = env.UpperBound
		if env.Limit > 0 {
			sc.Limit = env.Limit
		}
	}
	return sc
}

type environmentsView struct {
	Environments []model.Environment
	ActiveID     string
	Active       model.Environment
	HasActive    bool
	Limit        int
}

func (s *Server) handleEnvironments(w http.ResponseWriter, _ *http.Request) {
	v := environmentsView{Environments: s.envs.List(), ActiveID: s.envs.ActiveID(), Limit: s.effectiveLimit()}
	v.Active, v.HasActive = s.envs.Active()
	s.render(w, "environments.html", v)
}

func (s *Server) handleSaveEnvironment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("limit")))
	env := model.Environment{
		ID:         slugify(name),
		Name:       name,
		ServerURL:  strings.TrimRight(strings.TrimSpace(r.FormValue("serverUrl")), "/"),
		Subject:    strings.TrimSpace(r.FormValue("subject")),
		Types:      splitTypes(r.FormValue("types")),
		LowerBound: strings.TrimSpace(r.FormValue("lowerBound")),
		UpperBound: strings.TrimSpace(r.FormValue("upperBound")),
		Limit:      limit,
	}
	if env.ID == "" {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	if err := s.envs.Upsert(env); err != nil {
		s.serverError(w, "save environment", err)
		return
	}
	s.log.Info("environment saved", "id", env.ID)
	s.handleEnvironments(w, r)
}

func (s *Server) handleActivateEnvironment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	if err := s.envs.SetActive(id); err != nil {
		http.Error(w, "unknown environment", http.StatusBadRequest)
		return
	}
	// If the activated environment names a server, point the client there.
	// The token is never stored — keep it only if the server is unchanged.
	if env, ok := s.envs.Active(); ok && env.ServerURL != "" && env.ServerURL != s.clio.BaseURL() {
		s.clio.SetTarget(env.ServerURL, "")
	}
	s.log.Info("environment activated", "id", id)
	// Refresh every panel and the connection status.
	w.Header().Set("HX-Trigger", "clio-changed")
	s.handleEnvironments(w, r)
}

func (s *Server) handleDeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	if err := s.envs.Delete(id); err != nil {
		http.Error(w, "unknown environment", http.StatusBadRequest)
		return
	}
	w.Header().Set("HX-Trigger", "clio-changed")
	s.handleEnvironments(w, r)
}

func splitTypes(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
	})
	if len(fields) == 0 {
		return nil
	}
	return fields
}
