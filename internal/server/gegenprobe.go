package server

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/store"
	"github.com/pblumer/clio-workbench/internal/validate"
)

// gegenprobe.go is the Test Studio's Soll/Ist Gegenprobe (docs/TESTSTUDIO.md §7,
// roadmap WP-9): it reads the real events of a Clio instance and checks each
// subject's sequence against the *designed* model graph.
//
// Consolidation note: this closes the loop on the engine in internal/validate —
// the Soll side (scenarios, generator, push round-trip) and the Ist side (this
// Gegenprobe) now share the exact same CheckSequence. The older BPMN-template
// conformance (conformance.go / process.CheckConformance) is a different
// algorithm — a linear expected-sequence matcher over an uploaded BPMN, not a
// graph walk — and stays as its own Workbench feature rather than being forced
// onto a model it does not fit.

// gegenView is the view model for the Gegenprobe form.
type gegenView struct {
	Drafts  []model.Draft
	DraftID string
}

// gegenDeviation is one subject whose real sequence breaks the designed graph.
type gegenDeviation struct {
	Subject string
	Reason  string
}

// gegenResult answers the three Soll/Ist questions of §7.
type gegenResult struct {
	State        string // ok | offline | unauthorized | error
	Message      string
	Scope        string
	Subjects     int
	Conforming   int
	FitPct       int
	Deviations   []gegenDeviation // real transitions that contradict the design
	UnusedTypes  []string         // designed event types that never occur (dead design)
	UnknownTypes []string         // real event types missing from the model
}

const gegenMaxDeviations = 20

func (s *Server) handleGegenprobe(w http.ResponseWriter, r *http.Request) {
	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return
	}
	v := gegenView{Drafts: drafts, DraftID: r.URL.Query().Get("draft")}
	findDraft(drafts, &v.DraftID)
	s.render(w, "gegenprobe-form", v)
}

func (s *Server) handleGegenprobeRun(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	d, err := s.store.Get(r.FormValue("draft"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, "get draft", err)
		return
	}

	prefix := subjectScopePrefix(d.SubjectStyle)
	events, err := s.clio.ReadEventsUnder(r.Context(), prefix, s.cfg.EventCap)
	if err != nil {
		s.render(w, "gegenprobe-result", gegenReadError(err, prefix))
		if !errors.Is(err, clio.ErrOffline) && !errors.Is(err, clio.ErrUnauthorized) {
			s.log.Warn("read events (gegenprobe)", "err", err)
		}
		return
	}
	res := computeGegen(*d, events)
	res.State, res.Scope = "ok", prefix
	s.render(w, "gegenprobe-result", res)
}

// computeGegen groups events by subject and checks each sequence against the
// model with the shared engine, then derives the dead-design and unknown-type
// answers of §7.
func computeGegen(d model.Draft, events []clio.Event) gegenResult {
	m := validate.NewMachine(d)
	seqs := make(map[string][]string)
	var order []string
	realTypes := make(map[string]bool)
	for _, e := range events {
		if _, seen := seqs[e.Subject]; !seen {
			order = append(order, e.Subject)
		}
		seqs[e.Subject] = append(seqs[e.Subject], e.Type)
		realTypes[e.Type] = true
	}

	var res gegenResult
	res.Subjects = len(order)
	for _, sub := range order {
		if out := m.CheckSequence(seqs[sub]); out.OK {
			res.Conforming++
		} else if len(res.Deviations) < gegenMaxDeviations {
			res.Deviations = append(res.Deviations, gegenDeviation{Subject: sub, Reason: out.Reason})
		}
	}
	if res.Subjects > 0 {
		res.FitPct = res.Conforming * 100 / res.Subjects
	}

	// Designed event types (edge types), in draft order, deduplicated.
	modelTypes := make(map[string]bool)
	for _, e := range d.Edges {
		if e.Type == "" || modelTypes[e.Type] {
			continue
		}
		modelTypes[e.Type] = true
		if !realTypes[e.Type] {
			res.UnusedTypes = append(res.UnusedTypes, e.Type)
		}
	}
	for t := range realTypes {
		if !modelTypes[t] {
			res.UnknownTypes = append(res.UnknownTypes, t)
		}
	}
	sort.Strings(res.UnknownTypes)
	return res
}

// subjectScopePrefix is the static subject prefix of a subject style — the part
// before the first {placeholder} — used to scope the read cheaply.
func subjectScopePrefix(style string) string {
	style = strings.TrimSpace(style)
	if i := strings.IndexByte(style, '{'); i >= 0 {
		style = style[:i]
	}
	return strings.TrimRight(style, "/")
}

// gegenReadError maps a read failure onto the result vocabulary.
func gegenReadError(err error, prefix string) gegenResult {
	switch {
	case errors.Is(err, clio.ErrOffline):
		return gegenResult{State: "offline", Message: "keine Clio verbunden — verbinde unten eine Instanz", Scope: prefix}
	case errors.Is(err, clio.ErrUnauthorized):
		return gegenResult{State: "unauthorized", Message: "Clio hat den Token abgelehnt", Scope: prefix}
	default:
		return gegenResult{State: "error", Message: "Events konnten nicht gelesen werden", Scope: prefix}
	}
}
