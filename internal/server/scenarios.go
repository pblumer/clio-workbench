package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/scenario"
	"github.com/pblumer/clio-workbench/internal/store"
	"github.com/pblumer/clio-workbench/internal/validate"
)

// scenarios.go is the Test Studio's scenario editor + sequence tests + path
// view (docs/TESTSTUDIO.md §3.2/§3.3, roadmap WP-4).
//
// A suite belongs to a model (draft) and groups named cases. A case is an
// ordered sequence of event types plus an expectation (accept | reject, with an
// optional required end state). Running a case walks the sequence through the
// model with internal/validate.CheckSequence and reports pass/fail, the reason
// for the first deviation, and the path taken.

// scenarioView is the view model for the scenario editor pane.
type scenarioView struct {
	Drafts  []model.Draft
	DraftID string
	Suites  []scenario.Suite // suites belonging to the selected draft
	SuiteID string
	Suite   *scenario.Suite // the selected suite (nil if none)
	Nodes   []model.Node    // selected draft's nodes (end-state options, labels)
	Drift   bool            // suite written against a now-changed model
	Results []caseResult    // populated by a run
	Ran     bool
	Message string // usage note (e.g. selected model has no graph)
}

// caseResult is the outcome of running one case.
type caseResult struct {
	Name   string
	Seq    []string // the event-type sequence
	Pass   bool
	Detail string   // explanation (reason / confirmation)
	Path   []string // node labels visited, in order
}

func (s *Server) handleScenarios(w http.ResponseWriter, r *http.Request) {
	s.renderScenario(w, r.URL.Query().Get("draft"), r.URL.Query().Get("suite"))
}

// handleCreateSuite creates a suite for a draft, snapshotting the model
// revision for drift detection.
func (s *Server) handleCreateSuite(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	draftID := r.FormValue("draft")
	d, err := s.store.Get(draftID)
	if err != nil {
		s.scenarioErr(w, draftID, "", err)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.renderScenario(w, draftID, "")
		return
	}
	su := &scenario.Suite{
		ID:       randID("su"),
		Name:     name,
		DraftID:  draftID,
		DraftRev: scenario.DraftRev(*d),
	}
	if err := s.scenarios.Save(su); err != nil {
		s.serverError(w, "save suite", err)
		return
	}
	s.renderScenario(w, draftID, su.ID)
}

func (s *Server) handleDeleteSuite(w http.ResponseWriter, r *http.Request) {
	if err := s.scenarios.Delete(r.PathValue("suite")); err != nil && !errors.Is(err, scenario.ErrNotFound) {
		s.serverError(w, "delete suite", err)
		return
	}
	s.renderScenario(w, r.URL.Query().Get("draft"), "")
}

// handleAddCase appends a case (sequence + expectation) to a suite.
func (s *Server) handleAddCase(w http.ResponseWriter, r *http.Request) {
	su, ok := s.loadSuite(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	steps := parseSequence(r.FormValue("sequence"))
	if name == "" || len(steps) == 0 {
		s.renderScenario(w, su.DraftID, su.ID)
		return
	}
	outcome := scenario.ExpectAccept
	if r.FormValue("outcome") == string(scenario.ExpectReject) {
		outcome = scenario.ExpectReject
	}
	su.Cases = append(su.Cases, scenario.Case{
		ID:    randID("ca"),
		Name:  name,
		Steps: steps,
		Expect: scenario.Expectation{
			Outcome:  outcome,
			EndState: strings.TrimSpace(r.FormValue("endState")),
		},
	})
	if err := s.scenarios.Save(su); err != nil {
		s.userOrServerError(w, "save suite", err)
		return
	}
	s.renderScenario(w, su.DraftID, su.ID)
}

func (s *Server) handleDeleteCase(w http.ResponseWriter, r *http.Request) {
	su, ok := s.loadSuite(w, r)
	if !ok {
		return
	}
	id := r.PathValue("case")
	out := su.Cases[:0]
	for _, c := range su.Cases {
		if c.ID != id {
			out = append(out, c)
		}
	}
	su.Cases = out
	if err := s.scenarios.Save(su); err != nil {
		s.serverError(w, "save suite", err)
		return
	}
	s.renderScenario(w, su.DraftID, su.ID)
}

// handleRunSuite runs every case in the suite against its model and renders the
// results (pass/fail, reason, path).
func (s *Server) handleRunSuite(w http.ResponseWriter, r *http.Request) {
	su, ok := s.loadSuite(w, r)
	if !ok {
		return
	}
	d, err := s.store.Get(su.DraftID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.render(w, "scenario-results", scenarioView{Message: "the suite's model no longer exists"})
			return
		}
		s.serverError(w, "get draft", err)
		return
	}
	v := scenarioView{Ran: true}
	if len(d.Nodes) == 0 {
		v.Message = "this model has no graph (states/transitions) to check sequences against"
	}
	labels := nodeLabels(d.Nodes)
	m := validate.NewMachine(*d)
	for _, c := range su.Cases {
		v.Results = append(v.Results, runCase(m, labels, c))
	}
	s.render(w, "scenario-results", v)
}

// loadSuite fetches the suite named in the path, rendering a 404/500 on failure.
func (s *Server) loadSuite(w http.ResponseWriter, r *http.Request) (*scenario.Suite, bool) {
	su, err := s.scenarios.Get(r.PathValue("suite"))
	if err != nil {
		if errors.Is(err, scenario.ErrNotFound) {
			http.NotFound(w, r)
			return nil, false
		}
		s.serverError(w, "get suite", err)
		return nil, false
	}
	return su, true
}

// renderScenario rebuilds and renders the whole scenario panel.
func (s *Server) renderScenario(w http.ResponseWriter, draftID, suiteID string) {
	v, ok := s.buildScenarioView(w, draftID, suiteID)
	if !ok {
		return
	}
	s.render(w, "scenario-panel", v)
}

// scenarioErr renders the panel after a non-fatal draft lookup error, or 500s.
func (s *Server) scenarioErr(w http.ResponseWriter, draftID, suiteID string, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.renderScenario(w, draftID, suiteID)
		return
	}
	s.serverError(w, "get draft", err)
}

// buildScenarioView assembles the panel view model: the selected draft, the
// suites for it, the selected suite and whether it has drifted from the model.
func (s *Server) buildScenarioView(w http.ResponseWriter, draftID, suiteID string) (scenarioView, bool) {
	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return scenarioView{}, false
	}
	v := scenarioView{Drafts: drafts, DraftID: draftID, SuiteID: suiteID}
	d := findDraft(drafts, &v.DraftID)
	if d == nil {
		return v, true
	}
	v.Nodes = d.Nodes

	all, err := s.scenarios.List()
	if err != nil {
		s.serverError(w, "list suites", err)
		return scenarioView{}, false
	}
	for _, su := range all {
		if su.DraftID == v.DraftID {
			v.Suites = append(v.Suites, su)
		}
	}
	if su := findSuite(v.Suites, &v.SuiteID); su != nil {
		v.Suite = su
		v.Drift = scenario.Drift(*su, *d)
	}
	return v, true
}

// runCase walks one case's sequence and judges it against its expectation.
func runCase(m *validate.Machine, labels map[string]string, c scenario.Case) caseResult {
	types := make([]string, len(c.Steps))
	for i, st := range c.Steps {
		types[i] = st.Type
	}
	out := m.CheckSequence(types)

	r := caseResult{Name: c.Name, Seq: types}
	for _, id := range out.Path {
		r.Path = append(r.Path, labelOf(labels, id))
	}

	if c.Expect.Outcome == scenario.ExpectReject {
		r.Pass = !out.OK
		if out.OK {
			r.Detail = "expected rejection, but the sequence is a valid walk"
		} else {
			r.Detail = "correctly rejected — " + out.Reason
		}
		return r
	}

	// ExpectAccept.
	switch {
	case !out.OK:
		r.Detail = out.Reason
	case c.Expect.EndState != "" && !endsIn(out.Path, c.Expect.EndState):
		r.Detail = "ended in " + labelOf(labels, lastOf(out.Path)) + ", expected " + labelOf(labels, c.Expect.EndState)
	default:
		r.Pass = true
		r.Detail = "valid walk"
	}
	return r
}

// parseSequence splits a free-form sequence ("a → b, c\nd") into steps.
func parseSequence(s string) []scenario.Step {
	s = strings.ReplaceAll(s, "→", ",")
	s = strings.ReplaceAll(s, "->", ",")
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	var steps []scenario.Step
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			steps = append(steps, scenario.Step{Type: t})
		}
	}
	return steps
}

func nodeLabels(nodes []model.Node) map[string]string {
	m := make(map[string]string, len(nodes))
	for _, n := range nodes {
		m[n.ID] = n.Label
	}
	return m
}

func labelOf(labels map[string]string, id string) string {
	if l := labels[id]; l != "" {
		return l
	}
	return id
}

func endsIn(path []string, nodeID string) bool {
	return len(path) > 0 && path[len(path)-1] == nodeID
}

func lastOf(path []string) string {
	if len(path) == 0 {
		return ""
	}
	return path[len(path)-1]
}

// findSuite mirrors findDraft for the suite select.
func findSuite(suites []scenario.Suite, id *string) *scenario.Suite {
	for i := range suites {
		if suites[i].ID == *id {
			return &suites[i]
		}
	}
	if *id == "" && len(suites) > 0 {
		*id = suites[0].ID
		return &suites[0]
	}
	return nil
}
