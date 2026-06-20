package server

import (
	"errors"
	"net/http"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/producergen"
	"github.com/pblumer/clio-workbench/internal/store"
)

// producer.go is the Test Studio's producer-code tab (docs/TESTSTUDIO.md §9,
// roadmap WP-7): generate example producer code for the selected model in a
// chosen language, preview it and download it.

// producerView is the view model for the producer-code panel.
type producerView struct {
	Drafts   []model.Draft
	DraftID  string
	Langs    []producergen.Lang
	Lang     string
	Code     string
	Filename string
	Err      string
}

func (s *Server) handleProducer(w http.ResponseWriter, r *http.Request) {
	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return
	}
	v := producerView{
		Drafts:  drafts,
		DraftID: r.URL.Query().Get("draft"),
		Lang:    pickLang(r.URL.Query().Get("lang")),
		Langs:   producergen.Languages(),
	}
	v.Filename = filenameFor(v.Lang)
	if d := findDraft(drafts, &v.DraftID); d != nil {
		code, gerr := producergen.Generate(*d, v.Lang)
		if gerr != nil {
			v.Err = gerr.Error()
		} else {
			v.Code = code
		}
	}
	s.render(w, "producer-panel", v)
}

// handleProducerDownload streams the generated code as a file attachment.
func (s *Server) handleProducerDownload(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	if !producergen.SupportedLang(lang) {
		http.Error(w, "unknown language", http.StatusBadRequest)
		return
	}
	d, err := s.store.Get(r.URL.Query().Get("draft"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, "get draft", err)
		return
	}
	code, err := producergen.Generate(*d, lang)
	if err != nil {
		s.serverError(w, "generate producer", err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filenameFor(lang)+`"`)
	_, _ = w.Write([]byte(code))
}

// pickLang returns a supported language id, defaulting to the first.
func pickLang(id string) string {
	if producergen.SupportedLang(id) {
		return id
	}
	return producergen.Languages()[0].ID
}

func filenameFor(lang string) string {
	for _, l := range producergen.Languages() {
		if l.ID == lang {
			return l.Filename
		}
	}
	return "producer.txt"
}
