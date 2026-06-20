package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/simulator"
	"github.com/pblumer/clio-workbench/internal/store"
	"github.com/pblumer/clio-workbench/internal/validate"
)

// push.go is the Test Studio's instance integration (docs/TESTSTUDIO.md §7,
// roadmap WP-8): generate streams and push them to a Clio instance, then read
// them back and validate (round-trip).
//
// Because Clio is append-only (§7.3), pushed test data can never be deleted.
// Two safeguards make this hard to do by accident:
//
//   - Hard gate: a push is refused unless the active server was explicitly
//     confirmed as a throwaway instance this session (testScopeURL).
//   - Auto-prefix: every pushed subject is namespaced under /_test/<run-id>/…,
//     so test data stays clearly separated even in a shared instance.

const pushSource = "clio-workbench-teststudio"

// pushView is the view model for the push panel.
type pushView struct {
	Drafts  []model.Draft
	DraftID string
	Seed    int64
	Samples int
	Server  string // active Clio base URL ("" = none)
	Armed   bool   // throwaway instance confirmed for this server
}

// pushResult is the outcome of a push run.
type pushResult struct {
	Pushed       int    // events written
	Subjects     int    // distinct subjects written
	Prefix       string // the run's test namespace
	Message      string // usage note (not armed, no start state, …)
	Error        string // a write failure
	RoundTrip    bool   // read-back succeeded
	ReadBack     int    // events read back under the prefix
	SubjectsBack int    // distinct subjects read back
	Conform      int    // read-back subjects whose sequence is a valid walk
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	v, ok := s.pushView(w, r.URL.Query().Get("draft"), r.URL.Query().Get("seed"), r.URL.Query().Get("samples"))
	if !ok {
		return
	}
	s.render(w, "push-panel", v)
}

func (s *Server) handlePushArm(w http.ResponseWriter, r *http.Request) {
	base := s.clio.BaseURL()
	if base != "" {
		s.testScopeMu.Lock()
		s.testScopeURL = base
		s.testScopeMu.Unlock()
	}
	v, ok := s.pushView(w, r.URL.Query().Get("draft"), r.URL.Query().Get("seed"), r.URL.Query().Get("samples"))
	if !ok {
		return
	}
	s.render(w, "push-panel", v)
}

func (s *Server) handlePushDisarm(w http.ResponseWriter, r *http.Request) {
	s.testScopeMu.Lock()
	s.testScopeURL = ""
	s.testScopeMu.Unlock()
	v, ok := s.pushView(w, r.URL.Query().Get("draft"), r.URL.Query().Get("seed"), r.URL.Query().Get("samples"))
	if !ok {
		return
	}
	s.render(w, "push-panel", v)
}

// handlePushRun generates streams and pushes them under a fresh test namespace,
// then reads them back and validates.
func (s *Server) handlePushRun(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !s.armed() {
		s.render(w, "push-result", pushResult{Message: "confirm this is a throwaway instance before pushing"})
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
	streams, gerr := simulator.GenerateN(*d, parseSamples(r.FormValue("samples")), simulator.Options{Seed: parseSeed(r.FormValue("seed"))})
	if gerr != nil {
		s.render(w, "push-result", pushResult{Message: gerr.Error()})
		return
	}

	runID := randID("run")
	res := pushResult{Prefix: "/_test/" + runID}
	if !s.pushStreams(r.Context(), *d, res.Prefix, runID, streams, &res) {
		s.render(w, "push-result", res)
		return
	}
	s.roundTrip(r.Context(), *d, res.Prefix, &res)
	s.render(w, "push-result", res)
}

// pushStreams writes every event of every stream under the test prefix. It
// returns false (with res.Error set) on the first write failure.
func (s *Server) pushStreams(ctx context.Context, d model.Draft, prefix, runID string, streams []simulator.Stream, res *pushResult) bool {
	subjects := make(map[string]bool)
	for i, st := range streams {
		subject := prefix + applySubjectStyle(d, fmt.Sprintf("%s-%03d", runID, i))
		for _, e := range st.Events {
			if err := s.clio.AppendEvent(ctx, cloudEvent(e.Type, subject, e.Data)); err != nil {
				res.Error = clioErrMessage(err)
				return false
			}
			res.Pushed++
		}
		subjects[subject] = true
	}
	res.Subjects = len(subjects)
	return true
}

// roundTrip reads the pushed events back under the prefix and checks each
// subject's sequence against the model. Read failures are non-fatal — the push
// already succeeded — so they simply leave RoundTrip false.
func (s *Server) roundTrip(ctx context.Context, d model.Draft, prefix string, res *pushResult) {
	events, err := s.clio.ReadEventsUnder(ctx, prefix, s.cfg.EventCap)
	if err != nil {
		return
	}
	res.RoundTrip = true
	res.ReadBack = len(events)

	m := validate.NewMachine(d)
	seqs := make(map[string][]string)
	var order []string
	for _, ev := range events {
		if _, seen := seqs[ev.Subject]; !seen {
			order = append(order, ev.Subject)
		}
		seqs[ev.Subject] = append(seqs[ev.Subject], ev.Type)
	}
	res.SubjectsBack = len(order)
	for _, sub := range order {
		if m.CheckSequence(seqs[sub]).OK {
			res.Conform++
		}
	}
}

// armed reports whether the active server was confirmed as a throwaway instance.
func (s *Server) armed() bool {
	s.testScopeMu.Lock()
	defer s.testScopeMu.Unlock()
	return s.testScopeURL != "" && s.testScopeURL == s.clio.BaseURL()
}

func (s *Server) pushView(w http.ResponseWriter, draftID, seed, samples string) (pushView, bool) {
	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return pushView{}, false
	}
	v := pushView{
		Drafts:  drafts,
		DraftID: draftID,
		Seed:    parseSeed(seed),
		Samples: parseSamples(samples),
		Server:  s.clio.BaseURL(),
		Armed:   s.armed(),
	}
	findDraft(drafts, &v.DraftID)
	return v, true
}

// cloudEvent builds a structured CloudEvents envelope for one event.
func cloudEvent(eventType, subject string, data json.RawMessage) []byte {
	env := map[string]any{
		"specversion":     "1.0",
		"type":            eventType,
		"source":          pushSource,
		"id":              randID("ev"),
		"subject":         subject,
		"datacontenttype": "application/json",
		"data":            json.RawMessage("{}"),
	}
	if len(data) > 0 {
		env["data"] = data
	}
	b, _ := json.Marshal(env)
	return b
}

// applySubjectStyle renders a subject for one instance from the draft's subject
// style (every {placeholder} → id), falling back to /<namespace-or-name>/<id>.
func applySubjectStyle(d model.Draft, id string) string {
	style := strings.TrimSpace(d.SubjectStyle)
	if style == "" {
		base := slugify(d.Namespace)
		if base == "" {
			base = slugify(d.Name)
		}
		if base == "" {
			base = "entity"
		}
		return "/" + base + "/" + id
	}
	var b strings.Builder
	inBrace := false
	for _, r := range style {
		switch {
		case r == '{':
			inBrace = true
		case r == '}':
			inBrace = false
			b.WriteString(id)
		case !inBrace:
			b.WriteRune(r)
		}
	}
	s := b.String()
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

func clioErrMessage(err error) string {
	switch {
	case errors.Is(err, clio.ErrOffline):
		return "no Clio connected"
	case errors.Is(err, clio.ErrUnauthorized):
		return "Clio rejected the token"
	default:
		return "write failed: " + err.Error()
	}
}
