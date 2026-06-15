package server

import (
	"errors"
	"io"
	"net/http"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/process"
)

const conformanceMaxUpload = 4 << 20 // 4 MiB

type confStep struct {
	Name string
	Seen int
	Pct  int
}

type conformanceView struct {
	State   string // ok, error, offline, unauthorized
	Message string
	Conf    process.Conformance
	FitPct  int
	Steps   []confStep
}

// handleConformance accepts an uploaded .bpmn file, derives the expected event
// sequence and checks the real Clio events against it.
func (s *Server) handleConformance(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(conformanceMaxUpload); err != nil {
		s.render(w, "conformance.html", conformanceView{State: "error", Message: "could not read upload"})
		return
	}
	file, _, err := r.FormFile("bpmn")
	if err != nil {
		s.render(w, "conformance.html", conformanceView{State: "error", Message: "choose a .bpmn file first"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, conformanceMaxUpload))
	if err != nil {
		s.render(w, "conformance.html", conformanceView{State: "error", Message: "could not read file"})
		return
	}
	model, err := process.ParseBPMN(data)
	if err != nil {
		s.render(w, "conformance.html", conformanceView{State: "error", Message: "not a valid BPMN file"})
		return
	}
	if len(model.Expected) == 0 {
		s.render(w, "conformance.html", conformanceView{State: "error", Message: "no message/start/catch/end events with names found in the BPMN"})
		return
	}

	events, err := s.clio.ReadEvents(r.Context(), processEventCap)
	if err != nil {
		v := conformanceView{Conf: process.Conformance{Process: model.Process, Expected: model.Expected}}
		switch {
		case errors.Is(err, clio.ErrOffline):
			v.State, v.Message = "offline", "parsed the BPMN, but no Clio is connected to check against"
		case errors.Is(err, clio.ErrUnauthorized):
			v.State, v.Message = "unauthorized", "Clio rejected the token"
		default:
			v.State, v.Message = "error", "could not read events from Clio"
			s.log.Warn("read events (conformance)", "err", err)
		}
		s.render(w, "conformance.html", v)
		return
	}

	in := make([]process.Event, len(events))
	for i, e := range events {
		in[i] = process.Event{Subject: e.Subject, Type: e.Type}
	}
	conf := process.CheckConformance(model, process.SubjectSequences(in), 12)

	v := conformanceView{State: "ok", Conf: conf}
	for _, st := range conf.Steps {
		row := confStep{Name: st.Name, Seen: st.Seen}
		if conf.Relevant > 0 {
			row.Pct = st.Seen * 100 / conf.Relevant
		}
		v.Steps = append(v.Steps, row)
	}
	if conf.Relevant > 0 {
		v.FitPct = conf.Conforming * 100 / conf.Relevant
	}
	s.render(w, "conformance.html", v)
}
