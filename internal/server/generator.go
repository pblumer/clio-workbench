package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/simulator"
	"github.com/pblumer/clio-workbench/internal/store"
	"github.com/pblumer/clio-workbench/internal/testreport"
	"github.com/pblumer/clio-workbench/internal/validate"
)

// generator.go is the Test Studio's generator panel (docs/TESTSTUDIO.md §4/§8,
// roadmap WP-6): generate N seeded streams, check each against the model
// (property sampling), confirm that mutations are rejected, and render a
// reproducible report with edge coverage — downloadable as Markdown or JSON.

const (
	genDefaultSamples = 20
	genMaxSamples     = 1000
	genDefaultSeed    = 1
)

// generatorView is the view model for the generator form.
type generatorView struct {
	Drafts  []model.Draft
	DraftID string
	Seed    int64
	Samples int
}

// genResultView carries a finished run plus the params needed for the download
// links (the report regenerates deterministically from them).
type genResultView struct {
	Run     testreport.Run
	DraftID string
	Seed    int64
	Samples int
}

func (s *Server) handleGenerator(w http.ResponseWriter, r *http.Request) {
	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return
	}
	v := generatorView{
		Drafts:  drafts,
		DraftID: r.URL.Query().Get("draft"),
		Seed:    parseSeed(r.URL.Query().Get("seed")),
		Samples: parseSamples(r.URL.Query().Get("samples")),
	}
	findDraft(drafts, &v.DraftID)
	s.render(w, "generator-form", v)
}

func (s *Server) handleGeneratorRun(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	d, seed, samples, ok := s.genTarget(w, r)
	if !ok {
		return
	}
	s.render(w, "generator-result", genResultView{
		Run:     buildGeneratorRun(*d, seed, samples),
		DraftID: d.ID, Seed: seed, Samples: samples,
	})
}

// handleGeneratorReport downloads the run as Markdown or JSON (?format=md|json).
func (s *Server) handleGeneratorReport(w http.ResponseWriter, r *http.Request) {
	d, seed, samples, ok := s.genTarget(w, r)
	if !ok {
		return
	}
	run := buildGeneratorRun(*d, seed, samples)
	if r.URL.Query().Get("format") == "json" {
		body, err := run.JSON()
		if err != nil {
			s.serverError(w, "render report json", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="`+d.ID+`-report.json"`)
		_, _ = w.Write([]byte(body))
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+d.ID+`-report.md"`)
	_, _ = w.Write([]byte(run.Markdown()))
}

// genTarget resolves the draft + seed + samples from form or query, 404ing a
// missing model. It reads the form for POST and the query for GET.
func (s *Server) genTarget(w http.ResponseWriter, r *http.Request) (*model.Draft, int64, int, bool) {
	get := r.FormValue // FormValue covers both query and posted form
	d, err := s.store.Get(get("draft"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return nil, 0, 0, false
		}
		s.serverError(w, "get draft", err)
		return nil, 0, 0, false
	}
	return d, parseSeed(get("seed")), parseSamples(get("samples")), true
}

// buildGeneratorRun generates and checks samples, runs mutation (negative)
// checks against the first stream, and assembles the report.
func buildGeneratorRun(d model.Draft, seed int64, samples int) testreport.Run {
	run := testreport.Run{
		Model: d.Name, Seed: seed, When: time.Now().UTC(),
		Samples: samples, TotalEdges: len(d.Edges),
	}
	m := validate.NewMachine(d)
	streams, err := simulator.GenerateN(d, samples, simulator.Options{Seed: seed})
	if err != nil {
		run.Note = err.Error()
		return run
	}

	for _, st := range streams {
		if rejected, reason := streamRejected(d, m, st); rejected {
			run.Failed++
			run.Failures = append(run.Failures, testreport.Failure{Seed: st.Seed, Sequence: streamTypes(st), Reason: reason})
		} else {
			run.Passed++
		}
	}
	run.CoveredEdges, run.TotalEdges = simulator.EdgeCoverage(d, streams)

	if len(streams) > 0 {
		for _, mut := range simulator.Mutations(d, mutationBase(d, streams), seed) {
			rej, reason := streamRejected(d, m, mut.Stream)
			run.Negatives = append(run.Negatives, testreport.Negative{
				Kind: mut.Kind, Desc: mut.Desc, Rejected: rej, Reason: reason,
			})
		}
	}
	return run
}

// streamRejected reports whether a stream is refused by the engine — its
// sequence is not a valid walk, or one of its payloads fails its schema — and
// why. It is the single judgement used for both positive samples and the
// negative (mutation) checks.
func streamRejected(d model.Draft, m *validate.Machine, st simulator.Stream) (bool, string) {
	if out := m.CheckSequence(streamTypes(st)); !out.OK {
		return true, out.Reason
	}
	if !payloadsValid(d, st) {
		return true, "payload rejected"
	}
	return false, ""
}

// mutationBase prefers a stream that carries payload fields, so the payload
// mutations (drop-required, wrong-type) have something to break; it falls back
// to the first stream.
func mutationBase(d model.Draft, streams []simulator.Stream) simulator.Stream {
	for _, st := range streams {
		for _, e := range st.Events {
			if len(eventFields(d, e.Type)) > 0 {
				return st
			}
		}
	}
	return streams[0]
}

func streamTypes(s simulator.Stream) []string {
	types := make([]string, len(s.Events))
	for i, e := range s.Events {
		types[i] = e.Type
	}
	return types
}

func payloadsValid(d model.Draft, s simulator.Stream) bool {
	for _, e := range s.Events {
		if errs, err := validate.CheckPayload(eventFields(d, e.Type), e.Data); err != nil || len(errs) != 0 {
			return false
		}
	}
	return true
}

// eventFields returns the authored fields of an event type in the draft.
func eventFields(d model.Draft, typ string) []model.Field {
	for _, st := range d.Steps {
		if st.Kind == model.StepEvent && st.Name == typ {
			return st.Fields
		}
	}
	return nil
}

func parseSeed(v string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return genDefaultSeed
	}
	return n
}

func parseSamples(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return genDefaultSamples
	}
	if n > genMaxSamples {
		return genMaxSamples
	}
	return n
}
