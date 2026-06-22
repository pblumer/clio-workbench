package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/process"
	"github.com/pblumer/clio-workbench/internal/store"
)

// handleEditor renders the full outline editor page for a draft.
func (s *Server) handleEditor(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, "get draft", err)
		return
	}
	s.render(w, "editor.html", d)
}

// handleSaveMeta updates the process metadata (name, namespace, subject).
func (s *Server) handleSaveMeta(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDraft(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if n := strings.TrimSpace(r.FormValue("name")); n != "" {
		d.Name = n
	}
	d.Namespace = strings.TrimSpace(r.FormValue("namespace"))
	d.SubjectStyle = strings.TrimSpace(r.FormValue("subject"))
	if !s.saveDraft(w, d) {
		return
	}
	if r.FormValue("view") == "modeler" {
		s.renderModeler(w, d, strings.TrimSpace(r.FormValue("sel")))
		return
	}
	s.renderMeta(w, d)
}

// handleAddStep appends an event or task step.
func (s *Server) handleAddStep(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDraft(w, r)
	if !ok {
		return
	}
	kind := model.StepKind(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind != model.StepEvent && kind != model.StepTask {
		kind = model.StepEvent
	}
	id := newStepID()
	d.Steps = append(d.Steps, model.Step{ID: id, Kind: kind})
	if !s.saveDraft(w, d) {
		return
	}
	// On the canvas, surface the fresh shape as the selected element.
	s.renderAfterEdit(w, r, d, id)
}

// handleUpdateStep edits a step's name/phase/description.
func (s *Server) handleUpdateStep(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDraft(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id := r.PathValue("stepId")
	for i := range d.Steps {
		if d.Steps[i].ID != id {
			continue
		}
		d.Steps[i].Name = strings.TrimSpace(r.FormValue("name"))
		d.Steps[i].Description = strings.TrimSpace(r.FormValue("description"))
		if d.Steps[i].Kind == model.StepEvent {
			phase := strings.TrimSpace(r.FormValue("phase"))
			if phase == "" && d.Steps[i].Name != "" {
				_, p := process.Classify(d.Steps[i].Name) // suggest from the name
				phase = string(p)
			}
			d.Steps[i].Phase = phase
		}
		break
	}
	if !s.saveDraft(w, d) {
		return
	}
	s.renderAfterEdit(w, r, d, "")
}

// handleMoveStep reorders a step up or down.
func (s *Server) handleMoveStep(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDraft(w, r)
	if !ok {
		return
	}
	id := r.PathValue("stepId")
	up := r.URL.Query().Get("dir") != "down"
	for i := range d.Steps {
		if d.Steps[i].ID != id {
			continue
		}
		j := i - 1
		if !up {
			j = i + 1
		}
		if j >= 0 && j < len(d.Steps) {
			d.Steps[i], d.Steps[j] = d.Steps[j], d.Steps[i]
		}
		break
	}
	if !s.saveDraft(w, d) {
		return
	}
	s.renderAfterEdit(w, r, d, "")
}

// handleDeleteStep removes a step.
func (s *Server) handleDeleteStep(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDraft(w, r)
	if !ok {
		return
	}
	id := r.PathValue("stepId")
	out := d.Steps[:0]
	for _, st := range d.Steps {
		if st.ID != id {
			out = append(out, st)
		}
	}
	d.Steps = out
	if !s.saveDraft(w, d) {
		return
	}
	s.renderAfterEdit(w, r, d, "")
}

func (s *Server) loadDraft(w http.ResponseWriter, r *http.Request) (*model.Draft, bool) {
	d, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return nil, false
		}
		s.serverError(w, "get draft", err)
		return nil, false
	}
	return d, true
}

func (s *Server) saveDraft(w http.ResponseWriter, d *model.Draft) bool {
	if err := s.store.Save(d); err != nil {
		s.userOrServerError(w, "save draft", err)
		return false
	}
	return true
}

func (s *Server) renderSteps(w http.ResponseWriter, d *model.Draft) { s.render(w, "procsteps.html", d) }
func (s *Server) renderMeta(w http.ResponseWriter, d *model.Draft)  { s.render(w, "procmeta.html", d) }

func newStepID() string  { return randID("st") }
func newFieldID() string { return randID("fl") }

func randID(prefix string) string {
	var b [5]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

// stepByID returns a pointer to the step with the given id, or nil.
func stepByID(d *model.Draft, id string) *model.Step {
	for i := range d.Steps {
		if d.Steps[i].ID == id {
			return &d.Steps[i]
		}
	}
	return nil
}

// handleAddField appends a data field to an event step.
func (s *Server) handleAddField(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDraft(w, r)
	if !ok {
		return
	}
	if st := stepByID(d, r.PathValue("stepId")); st != nil {
		st.Fields = append(st.Fields, model.Field{ID: newFieldID(), Type: "string"})
	}
	if !s.saveDraft(w, d) {
		return
	}
	s.renderAfterEdit(w, r, d, "")
}

// handleUpdateField edits a field's name/type/required/format/ref/enum.
func (s *Server) handleUpdateField(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDraft(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	st := stepByID(d, r.PathValue("stepId"))
	if st != nil {
		fid := r.PathValue("fieldId")
		for i := range st.Fields {
			if st.Fields[i].ID != fid {
				continue
			}
			st.Fields[i].Name = strings.TrimSpace(r.FormValue("name"))
			st.Fields[i].Type = strings.TrimSpace(r.FormValue("type"))
			st.Fields[i].Required = r.FormValue("required") != ""
			st.Fields[i].Format = strings.TrimSpace(r.FormValue("format"))
			st.Fields[i].Ref = strings.TrimSpace(r.FormValue("ref"))
			st.Fields[i].Enum = splitTypes(r.FormValue("enum"))
			break
		}
	}
	if !s.saveDraft(w, d) {
		return
	}
	s.renderAfterEdit(w, r, d, "")
}

// handleDeleteField removes a field from an event step.
func (s *Server) handleDeleteField(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDraft(w, r)
	if !ok {
		return
	}
	if st := stepByID(d, r.PathValue("stepId")); st != nil {
		fid := r.PathValue("fieldId")
		out := st.Fields[:0]
		for _, f := range st.Fields {
			if f.ID != fid {
				out = append(out, f)
			}
		}
		st.Fields = out
	}
	if !s.saveDraft(w, d) {
		return
	}
	s.renderAfterEdit(w, r, d, "")
}
