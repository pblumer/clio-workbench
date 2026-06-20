package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/schemagen"
	"github.com/pblumer/clio-workbench/internal/store"
	"github.com/pblumer/clio-workbench/internal/validate"
)

// studio.go is the Test Studio's server surface (docs/TESTSTUDIO.md, WP-2).
//
// WP-2 ships the schema-test view (§3.1): pick one of your models and one of
// its event types, paste a data payload, and check it against that type's
// fields using the engine in internal/validate. It is purely local — no Clio
// instance is involved.

// studioSchemaView is the view model for the schema-test tab.
type studioSchemaView struct {
	Drafts  []model.Draft
	DraftID string
	Steps   []model.Step // named event steps of the selected draft
	StepID  string
	Schema  string // generated JSON Schema preview for the selected step
	Data    string // echoed-back payload
}

// studioResult is the outcome fragment of a single schema-test run.
type studioResult struct {
	Checked bool
	OK      bool
	Errors  []validate.FieldError
	Message string // usage/plumbing note (invalid JSON, no event type, …)
}

// handleStudioSchema renders the full schema-test form fragment.
func (s *Server) handleStudioSchema(w http.ResponseWriter, r *http.Request) {
	v, ok := s.studioSchema(w, r)
	if !ok {
		return
	}
	s.render(w, "studio-schema", v)
}

// handleStudioSchemaFields renders just the event-type select + schema preview,
// reloaded when the chosen model or event type changes.
func (s *Server) handleStudioSchemaFields(w http.ResponseWriter, r *http.Request) {
	v, ok := s.studioSchema(w, r)
	if !ok {
		return
	}
	s.render(w, "studio-schema-fields", v)
}

func (s *Server) studioSchema(w http.ResponseWriter, r *http.Request) (studioSchemaView, bool) {
	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return studioSchemaView{}, false
	}
	return buildStudioSchema(drafts, r.URL.Query().Get("draft"), r.URL.Query().Get("step")), true
}

// handleStudioSchemaCheck validates the posted payload against the selected
// event type's fields and renders the result fragment.
func (s *Server) handleStudioSchemaCheck(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	d, err := s.store.Get(r.FormValue("draft"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.render(w, "studio-schema-result", studioResult{Message: "choose a model first"})
			return
		}
		s.serverError(w, "get draft", err)
		return
	}
	st := stepByID(d, r.FormValue("step"))
	if st == nil || st.Kind != model.StepEvent {
		s.render(w, "studio-schema-result", studioResult{Message: "choose an event type first"})
		return
	}
	errs, verr := validate.CheckPayload(st.Fields, json.RawMessage(strings.TrimSpace(r.FormValue("data"))))
	if verr != nil {
		s.render(w, "studio-schema-result", studioResult{Message: verr.Error()})
		return
	}
	s.render(w, "studio-schema-result", studioResult{Checked: true, OK: len(errs) == 0, Errors: errs})
}

// buildStudioSchema resolves the selected draft (defaulting to the first), its
// named event steps, the selected step (defaulting to the first), and that
// step's generated schema preview.
func buildStudioSchema(drafts []model.Draft, draftID, stepID string) studioSchemaView {
	v := studioSchemaView{Drafts: drafts, DraftID: draftID, StepID: stepID}
	sel := findDraft(drafts, &v.DraftID)
	if sel == nil {
		return v
	}
	for _, st := range sel.Steps {
		if st.Kind == model.StepEvent && strings.TrimSpace(st.Name) != "" {
			v.Steps = append(v.Steps, st)
		}
	}
	if st := findStep(v.Steps, &v.StepID); st != nil {
		v.Schema = schemagen.EventSchema(st.Fields)
	}
	return v
}

// findDraft returns the draft whose id matches *id, defaulting to the first
// draft when *id is empty (and updating *id to it). Returns nil if none match.
func findDraft(drafts []model.Draft, id *string) *model.Draft {
	for i := range drafts {
		if drafts[i].ID == *id {
			return &drafts[i]
		}
	}
	if *id == "" && len(drafts) > 0 {
		*id = drafts[0].ID
		return &drafts[0]
	}
	return nil
}

// findStep mirrors findDraft for the event-step select.
func findStep(steps []model.Step, id *string) *model.Step {
	for i := range steps {
		if steps[i].ID == *id {
			return &steps[i]
		}
	}
	if *id == "" && len(steps) > 0 {
		*id = steps[0].ID
		return &steps[0]
	}
	return nil
}
