package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/process"
)

// streamPoll is how often the live stream re-tails Clio for new events. It is a
// server-side poll turned into a real SSE push to the browser: the Workbench is
// the event source, so the chart stays live even though Clio exposes no tail.
const streamPoll = 2 * time.Second

// streamDot is one live event pushed to the browser.
type streamDot struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Type    string `json:"type"`
	Time    string `json:"time"`
	Phase   string `json:"phase"`
	Color   string `json:"color"`
}

// handleSpaceStream is the live feed for the Event Space. The browser opens it
// (EventSource) with ?after=<id> — the newest id already on the chart — and the
// server pushes every newer event that survives the active scope + pipeline as
// an SSE `dot` message. It runs until the client disconnects.
func (s *Server) handleSpaceStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if !s.clio.Configured() {
		http.Error(w, "offline", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()
	after := strings.TrimSpace(r.URL.Query().Get("after"))
	if after == "" {
		// No anchor given: start from the current tail so we never replay the
		// whole history into the live feed.
		after = s.currentMaxID(ctx)
	}
	// The same in-panel space filter applies to the live feed, so streamed dots
	// match the charted ones.
	filter := parseSpaceFilter(r.URL.Query().Get("q"))

	flusher.Flush()
	ticker := time.NewTicker(streamPoll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			newer, max := s.readSince(ctx, after, filter)
			if max != "" {
				after = max
			}
			for _, d := range newer {
				if !writeSSE(w, "dot", d) {
					return
				}
			}
			// Heartbeat keeps intermediaries from closing an idle stream and
			// lets us notice a vanished client.
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// currentMaxID reads the active scope once and returns the highest event id, so
// a freshly opened stream resumes from the present rather than the past.
func (s *Server) currentMaxID(ctx context.Context) string {
	rctx, cancel := context.WithTimeout(ctx, connectionTimeout)
	defer cancel()
	events, err := s.clio.ReadScoped(rctx, s.activeScope())
	if err != nil {
		return ""
	}
	max := ""
	for _, e := range events {
		if e.ID > max {
			max = e.ID
		}
	}
	return max
}

// readSince tails the active scope for events with an id strictly greater than
// after, applies the query pipeline and the in-panel space filter, and returns
// them as stream dots plus the new high-water id. It scopes the Clio read with
// lowerBound = after so only fresh events come back.
func (s *Server) readSince(ctx context.Context, after string, filter spaceFilter) ([]streamDot, string) {
	rctx, cancel := context.WithTimeout(ctx, connectionTimeout)
	defer cancel()

	sc := s.activeScope()
	if after != "" && after > sc.LowerBound {
		sc.LowerBound = after
	}
	events, err := s.clio.ReadScoped(rctx, sc)
	if err != nil {
		return nil, ""
	}
	// Compose Queries with the in-panel discipline lens through the shared seam,
	// then apply the lens's free-text needles on top.
	stages := s.refinement(filter.lens)
	max := after
	var out []streamDot
	for _, e := range events {
		if e.ID <= after { // lowerBound is inclusive — drop the anchor itself
			continue
		}
		if e.ID > max {
			max = e.ID
		}
		if !survives(eventKey{e.Subject, e.Type, e.ID, e.Source}, stages) {
			continue
		}
		if !filter.matchNeedles(e.Subject, e.Type) {
			continue
		}
		_, phase := process.Classify(e.Type)
		out = append(out, streamDot{
			ID:      e.ID,
			Subject: e.Subject,
			Type:    e.Type,
			Time:    e.Time,
			Phase:   string(phase),
			Color:   typeColor(e.Type),
		})
	}
	return out, max
}

// writeSSE writes one named SSE event with a JSON payload. It returns false if
// the connection is gone so the caller can stop.
func writeSSE(w http.ResponseWriter, event string, v any) bool {
	payload, err := json.Marshal(v)
	if err != nil {
		return true
	}
	if _, err := w.Write([]byte("event: " + event + "\ndata: ")); err != nil {
		return false
	}
	if _, err := w.Write(payload); err != nil {
		return false
	}
	_, err = w.Write([]byte("\n\n"))
	return err == nil
}

// spaceEventView is the hover card: an event's metadata plus its pretty payload.
type spaceEventView struct {
	State   string
	Subject string
	Type    string
	Time    string
	Source  string
	ID      string
	Phase   string
	Data    string
}

// handleSpaceEvent returns the rich hover card for a single dot: it reads the
// dot's subject (cheap, scoped to one subtree) and renders the matching event's
// metadata and JSON payload. Looked up lazily so thousands of dots stay light.
func (s *Server) handleSpaceEvent(w http.ResponseWriter, r *http.Request) {
	subject := strings.TrimSpace(r.URL.Query().Get("subject"))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if subject == "" || id == "" {
		s.render(w, "spaceevent.html", spaceEventView{State: "error"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()

	events, err := s.clio.ReadFullScoped(ctx, clio.Scope{Subject: subject, Limit: 1000})
	if err != nil {
		st := "error"
		if errors.Is(err, clio.ErrOffline) {
			st = "offline"
		} else if errors.Is(err, clio.ErrUnauthorized) {
			st = "unauthorized"
		}
		s.render(w, "spaceevent.html", spaceEventView{State: st})
		return
	}
	for _, e := range events {
		if e.ID != id {
			continue
		}
		_, phase := process.Classify(e.Type)
		s.render(w, "spaceevent.html", spaceEventView{
			State:   "ok",
			Subject: e.Subject,
			Type:    e.Type,
			Time:    e.Time,
			Source:  e.Source,
			ID:      e.ID,
			Phase:   string(phase),
			Data:    prettyJSON(e.Data),
		})
		return
	}
	s.render(w, "spaceevent.html", spaceEventView{State: "empty"})
}
