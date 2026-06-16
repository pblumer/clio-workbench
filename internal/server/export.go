package server

import (
	"net/http"

	"github.com/pblumer/clio-workbench/internal/bpmngen"
	"github.com/pblumer/clio-workbench/internal/schemagen"
)

// handleExportSchemas serves the importable register-event-schema collection.
func (s *Server) handleExportSchemas(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDraft(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+d.ID+`-schemas.json"`)
	_, _ = w.Write([]byte(schemagen.SchemaCollection(*d)))
}

// handleExportBPMN serves the generated BPMN for the process.
func (s *Server) handleExportBPMN(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDraft(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Disposition", `attachment; filename="`+d.ID+`.bpmn"`)
	_, _ = w.Write([]byte(bpmngen.GenerateBPMN(*d)))
}
