package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/process"
)

type relationsView struct {
	State     string // ok, empty, offline, unauthorized, error
	Message   string
	Root      *process.RelNode
	Refs      []process.RefEdge // inferred from data payloads
	Subjects  int
	Events    int
	Truncated bool
	Cap       int
}

// handleRelations derives relationships from real events: 1:n containment from
// the subject hierarchy (/orders/{id}/items/{id}) plus references inferred from
// data payloads (FK fields → 1:1/1:n/n:1, association events → n:m).
func (s *Server) handleRelations(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()

	sc := s.activeScope()
	events, err := s.scopedFullEvents(ctx)
	v := relationsView{}
	switch {
	case err == nil:
		subjects := make([]string, len(events))
		refIn := make([]process.RefEvent, len(events))
		distinct := make(map[string]struct{})
		for i, e := range events {
			subjects[i] = e.Subject
			distinct[e.Subject] = struct{}{}
			refIn[i] = process.RefEvent{Subject: e.Subject, Type: e.Type, Data: e.Data}
		}
		root := process.BuildSubjectTree(subjects)
		if len(root.Children) == 0 {
			v.State, v.Message = "empty", "Clio is connected, but there are no subjects yet."
			break
		}
		v.State = "ok"
		v.Root = root
		v.Refs = process.BuildReferences(refIn).Edges
		v.Subjects = len(distinct)
		v.Events = len(events)
		v.Truncated = len(events) >= sc.Limit
		v.Cap = sc.Limit
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
