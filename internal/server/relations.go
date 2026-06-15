package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/process"
)

type relationsView struct {
	State    string // ok, empty, offline, unauthorized, error
	Message  string
	Root     *process.RelNode
	Subjects int
	Events   int
}

// handleRelations derives the 1:n relationship tree from the subject hierarchy
// of real events (e.g. /orders/{id}/items/{id}).
func (s *Server) handleRelations(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()

	events, err := s.clio.ReadEvents(ctx, processEventCap)
	v := relationsView{}
	switch {
	case err == nil:
		subjects := make([]string, len(events))
		distinct := make(map[string]struct{})
		for i, e := range events {
			subjects[i] = e.Subject
			distinct[e.Subject] = struct{}{}
		}
		root := process.BuildSubjectTree(subjects)
		if len(root.Children) == 0 {
			v.State, v.Message = "empty", "Clio is connected, but there are no subjects yet."
			break
		}
		v.State = "ok"
		v.Root = root
		v.Subjects = len(distinct)
		v.Events = len(events)
	case errors.Is(err, clio.ErrOffline):
		v.State, v.Message = "offline", "no Clio connected — pick a server to map relationships"
	case errors.Is(err, clio.ErrUnauthorized):
		v.State, v.Message = "unauthorized", "Clio rejected the token"
	default:
		v.State, v.Message = "error", "could not read events from Clio"
		s.log.Warn("read events (relations)", "err", err)
	}

	s.render(w, "relations.html", v)
}
