package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pblumer/clio-workbench/internal/clio"
)

const nodeEventsCap = 300

type nodeEventItem struct {
	Subject string
	Source  string
	Time    string
	Data    string // pretty-printed JSON ("—" when absent)
}

type nodeEventsView struct {
	State   string // ok, empty, offline, unauthorized, error
	Message string
	Type    string
	Count   int
	Capped  bool
	Items   []nodeEventItem
}

// handleNodeEvents renders the inspector fragment: a compact, filterable list of
// the events of one type, each with its data payload.
func (s *Server) handleNodeEvents(w http.ResponseWriter, r *http.Request) {
	typ := strings.TrimSpace(r.URL.Query().Get("type"))
	v := nodeEventsView{Type: typ}
	if typ == "" {
		v.State, v.Message = "error", "no event type given"
		s.render(w, "nodeevents.html", v)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()

	events, err := s.clio.ReadEventsByType(ctx, typ, nodeEventsCap)
	switch {
	case err == nil:
		if len(events) == 0 {
			v.State, v.Message = "empty", "no events of this type"
			break
		}
		v.State = "ok"
		v.Count = len(events)
		v.Capped = len(events) >= nodeEventsCap
		for _, e := range events {
			v.Items = append(v.Items, nodeEventItem{
				Subject: e.Subject,
				Source:  e.Source,
				Time:    e.Time,
				Data:    prettyJSON(e.Data),
			})
		}
	case errors.Is(err, clio.ErrOffline):
		v.State, v.Message = "offline", "no Clio connected"
	case errors.Is(err, clio.ErrUnauthorized):
		v.State, v.Message = "unauthorized", "Clio rejected the token"
	default:
		v.State, v.Message = "error", "could not read events"
		s.log.Warn("read events by type", "type", typ, "err", err)
	}

	s.render(w, "nodeevents.html", v)
}

// prettyJSON indents a raw JSON payload for display; empty/null becomes "—".
func prettyJSON(raw json.RawMessage) string {
	t := strings.TrimSpace(string(raw))
	if t == "" || t == "null" {
		return "—"
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return t // not valid JSON: show as-is
	}
	return buf.String()
}
