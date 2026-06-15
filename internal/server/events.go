package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/pblumer/clio-workbench/internal/clio"
)

// eventsView is the view model for the BPMN events fragment.
type eventsView struct {
	// State is one of: ok, offline, unauthorized, error.
	State   string
	Message string
	Types   []clio.EventType
	// Total is the sum of all occurrence counts across event types — the
	// number shown in the header bubble.
	Total int
}

// handleEvents reads the event types from the connected Clio and renders a
// rudimentary BPMN view: each type as a send task with an attached data object,
// a per-type count bubble, and a header bubble with the total.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()

	types, err := s.clio.ReadEventTypes(ctx)
	v := eventsView{}
	switch {
	case err == nil:
		v.State = "ok"
		v.Types = types
		for _, t := range types {
			v.Total += t.Count
		}
	case errors.Is(err, clio.ErrOffline):
		v.State = "offline"
		v.Message = "no Clio connected — set CLIO_URL to read live events"
	case errors.Is(err, clio.ErrUnauthorized):
		v.State = "unauthorized"
		v.Message = "Clio rejected the token"
	default:
		v.State = "error"
		v.Message = "could not reach Clio"
		s.log.Warn("read event types", "err", err)
	}

	s.render(w, "events.html", v)
}
