package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

// render executes a named template into a buffer first, so a template error
// never leaves a half-written response on the wire.
func (s *Server) render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.serverError(w, "render "+name, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		s.log.Error("encode json", "err", err)
	}
}

// serverError logs and returns a 500.
func (s *Server) serverError(w http.ResponseWriter, op string, err error) {
	s.log.Error(op, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// userOrServerError maps draft validation failures to 400 and the rest to
// 500. model.Draft.Validate returns descriptive errors; we surface them to
// the user as bad input.
func (s *Server) userOrServerError(w http.ResponseWriter, op string, err error) {
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{"invalid", "must not be empty", "duplicate", "references unknown"} {
		if strings.Contains(msg, marker) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	s.serverError(w, op, err)
}
