package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pblumer/clio-workbench/internal/clio"
)

// queryStage is the single refinement primitive shared by all three scope
// layers (docs/SCOPE.md §4): the global Queries pipeline and every discipline
// lens are lists of these. It carries the narrowing dimensions of the
// environment/query form (subject prefix, event types, id bounds) plus a source
// substring. Each stage further decimates the survivors of the stage before it
// — an AND across all stages.
type queryStage struct {
	Subject    string
	Types      []string
	LowerBound string
	UpperBound string
	Source     string
}

// eventKey is the minimal event projection a queryStage matches against. It
// lets the same matcher serve both the minimal and the payload-carrying read.
type eventKey struct {
	Subject string
	Type    string
	ID      string
	Source  string
}

// label renders a stage as a compact human-readable filter expression.
func (q queryStage) label() string {
	var parts []string
	if q.Subject != "" {
		parts = append(parts, "subject "+q.Subject)
	}
	if len(q.Types) > 0 {
		parts = append(parts, "type "+strings.Join(q.Types, "|"))
	}
	if q.LowerBound != "" {
		parts = append(parts, "from "+q.LowerBound)
	}
	if q.UpperBound != "" {
		parts = append(parts, "to "+q.UpperBound)
	}
	if q.Source != "" {
		parts = append(parts, "source "+q.Source)
	}
	if len(parts) == 0 {
		return "any"
	}
	return strings.Join(parts, " · ")
}

// empty reports whether a stage carries no filter at all (would be a no-op).
func (q queryStage) empty() bool {
	return q.Subject == "" && len(q.Types) == 0 && q.LowerBound == "" &&
		q.UpperBound == "" && q.Source == ""
}

// stages returns a copy of the current pipeline (safe for concurrent reads).
func (s *Server) stages() []queryStage {
	s.pipelineMu.Lock()
	defer s.pipelineMu.Unlock()
	out := make([]queryStage, len(s.pipeline))
	copy(out, s.pipeline)
	return out
}

// matchStage reports whether an event survives a stage. Subject is a
// segment-aware prefix, Types an exact set, the bounds an id range, and Source a
// substring.
func matchStage(k eventKey, st queryStage) bool {
	if st.Subject != "" {
		want := "/" + strings.Trim(st.Subject, "/")
		if k.Subject != want && !strings.HasPrefix(k.Subject, want+"/") {
			return false
		}
	}
	if len(st.Types) > 0 {
		hit := false
		for _, t := range st.Types {
			if t == k.Type {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if st.LowerBound != "" && k.ID < st.LowerBound {
		return false
	}
	if st.UpperBound != "" && k.ID > st.UpperBound {
		return false
	}
	if st.Source != "" && !strings.Contains(k.Source, st.Source) {
		return false
	}
	return true
}

// survives reports whether an event passes every stage of the pipeline.
func survives(k eventKey, stages []queryStage) bool {
	for _, st := range stages {
		if !matchStage(k, st) {
			return false
		}
	}
	return true
}

// refinement composes the two in-process scope layers (docs/SCOPE.md §1): the
// global Queries pipeline followed by an optional discipline lens, in that
// order. Empty lens stages are dropped so a no-op lens stays free.
func (s *Server) refinement(lens ...queryStage) []queryStage {
	stages := s.stages()
	for _, st := range lens {
		if !st.empty() {
			stages = append(stages, st)
		}
	}
	return stages
}

// applyPipeline filters the minimal-event stream through every refinement stage
// (global Queries plus the given discipline lens).
func (s *Server) applyPipeline(events []clio.Event, lens ...queryStage) []clio.Event {
	stages := s.refinement(lens...)
	if len(stages) == 0 {
		return events
	}
	out := events[:0:0]
	for _, e := range events {
		if survives(eventKey{e.Subject, e.Type, e.ID, e.Source}, stages) {
			out = append(out, e)
		}
	}
	return out
}

// applyPipelineFull filters the full-event stream (with payloads) the same way.
func (s *Server) applyPipelineFull(events []clio.FullEvent, lens ...queryStage) []clio.FullEvent {
	stages := s.refinement(lens...)
	if len(stages) == 0 {
		return events
	}
	out := events[:0:0]
	for _, e := range events {
		if survives(eventKey{e.Subject, e.Type, e.ID, e.Source}, stages) {
			out = append(out, e)
		}
	}
	return out
}

// scopedEvents reads the active environment's universe from Clio and lays the
// refinement (global Queries plus the optional discipline lens) over it — the
// single chokepoint every analysis panel uses so the three scope layers
// (docs/SCOPE.md) compose uniformly. A view with no lens of its own calls this
// without arguments and inherits exactly environment + queries.
func (s *Server) scopedEvents(ctx context.Context, lens ...queryStage) ([]clio.Event, error) {
	events, err := s.clio.ReadScoped(ctx, s.activeScope())
	if err != nil {
		return nil, err
	}
	return s.applyPipeline(events, lens...), nil
}

// scopedFullEvents is scopedEvents for the payload-carrying read.
func (s *Server) scopedFullEvents(ctx context.Context, lens ...queryStage) ([]clio.FullEvent, error) {
	events, err := s.clio.ReadFullScoped(ctx, s.activeScope())
	if err != nil {
		return nil, err
	}
	return s.applyPipelineFull(events, lens...), nil
}

// ---- view & handlers ----

type queryStageView struct {
	Index   int
	Label   string
	Events  int // survivors after this stage
	Subject int // distinct subjects after this stage
}

type queriesView struct {
	State      string // ok, offline, unauthorized, error
	Message    string
	BaseEvents int // events the environment yields, before any stage
	BaseSubj   int
	Stages     []queryStageView
	Final      int // survivors after the whole chain
}

// handleQueries renders the pipeline panel: the environment's base count and,
// per stage, how many events/subjects survive — the exploration funnel.
func (s *Server) handleQueries(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readTimeout)
	defer cancel()

	events, err := s.clio.ReadScoped(ctx, s.activeScope())
	v := queriesView{}
	switch {
	case err == nil:
		v.State = "ok"
		v.BaseEvents = len(events)
		v.BaseSubj = distinctSubjects(events)
		stages := s.stages()
		cur := events
		for i, st := range stages {
			next := cur[:0:0]
			for _, e := range cur {
				if matchStage(eventKey{e.Subject, e.Type, e.ID, e.Source}, st) {
					next = append(next, e)
				}
			}
			cur = next
			v.Stages = append(v.Stages, queryStageView{
				Index:   i,
				Label:   st.label(),
				Events:  len(cur),
				Subject: distinctSubjects(cur),
			})
		}
		v.Final = len(cur)
	default:
		v.State, v.Message = readErrState(err)
	}
	s.render(w, "queries.html", v)
}

func (s *Server) handleAddQuery(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	st := queryStage{
		Subject:    strings.TrimSpace(r.FormValue("subject")),
		Types:      splitTypes(r.FormValue("types")),
		LowerBound: strings.TrimSpace(r.FormValue("lowerBound")),
		UpperBound: strings.TrimSpace(r.FormValue("upperBound")),
		Source:     strings.TrimSpace(r.FormValue("source")),
	}
	if !st.empty() {
		s.pipelineMu.Lock()
		s.pipeline = append(s.pipeline, st)
		s.pipelineMu.Unlock()
		s.log.Info("query stage added", "depth", len(s.pipeline))
	}
	w.Header().Set("HX-Trigger", "scope-changed")
	s.handleQueries(w, r)
}

func (s *Server) handleDeleteQuery(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	i := atoiSafe(r.FormValue("index"))
	s.pipelineMu.Lock()
	if i >= 0 && i < len(s.pipeline) {
		s.pipeline = append(s.pipeline[:i], s.pipeline[i+1:]...)
	}
	s.pipelineMu.Unlock()
	w.Header().Set("HX-Trigger", "scope-changed")
	s.handleQueries(w, r)
}

func (s *Server) handleClearQueries(w http.ResponseWriter, r *http.Request) {
	s.pipelineMu.Lock()
	s.pipeline = nil
	s.pipelineMu.Unlock()
	w.Header().Set("HX-Trigger", "scope-changed")
	s.handleQueries(w, r)
}

// readErrState maps a clio read error onto a panel state + message, so every
// analysis handler reports offline/auth/error the same way.
func readErrState(err error) (state, msg string) {
	switch {
	case errors.Is(err, clio.ErrOffline):
		return "offline", "no Clio connected — pick a server to explore its events"
	case errors.Is(err, clio.ErrUnauthorized):
		return "unauthorized", "Clio rejected the token"
	default:
		return "error", "could not read events from Clio"
	}
}

func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return -1
	}
	return n
}

func distinctSubjects(events []clio.Event) int {
	seen := make(map[string]struct{}, len(events))
	for _, e := range events {
		seen[e.Subject] = struct{}{}
	}
	return len(seen)
}
