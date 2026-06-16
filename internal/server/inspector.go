package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pblumer/clio-workbench/internal/clio"
)

const nodeEventsCap = 300

type nodeEventItem struct {
	Subject string
	Source  string
	Time    string
	Data    string // pretty-printed JSON ("—" when absent)
}

type nodeEventsView struct {
	State   string // ok, empty, offline, unauthorized, error
	Message string
	Type    string
	Count   int
	Capped  bool
	Items   []nodeEventItem
}

// handleNodeEvents renders the inspector fragment: a compact, filterable list of
// the events of one type, each with its data payload.
func (s *Server) handleNodeEvents(w http.ResponseWriter, r *http.Request) {
	typ := strings.TrimSpace(r.URL.Query().Get("type"))
	v := nodeEventsView{Type: typ}
	if typ == "" {
		v.State, v.Message = "error", "no event type given"
		s.render(w, "nodeevents.html", v)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()

	events, err := s.clio.ReadEventsByType(ctx, typ, nodeEventsCap)
	switch {
	case err == nil:
		if len(events) == 0 {
			v.State, v.Message = "empty", "no events of this type"
			break
		}
		v.State = "ok"
		v.Count = len(events)
		v.Capped = len(events) >= nodeEventsCap
		for _, e := range events {
			v.Items = append(v.Items, nodeEventItem{
				Subject: e.Subject,
				Source:  e.Source,
				Time:    e.Time,
				Data:    prettyJSON(e.Data),
			})
		}
	case errors.Is(err, clio.ErrOffline):
		v.State, v.Message = "offline", "no Clio connected"
	case errors.Is(err, clio.ErrUnauthorized):
		v.State, v.Message = "unauthorized", "Clio rejected the token"
	default:
		v.State, v.Message = "error", "could not read events"
		s.log.Warn("read events by type", "type", typ, "err", err)
	}

	s.render(w, "nodeevents.html", v)
}

// prettyJSON indents a raw JSON payload for display, decoding \uXXXX escapes to
// real characters (so "Müller" shows as "Müller") while preserving field
// order. Empty/null becomes "—"; invalid JSON is shown as-is.
func prettyJSON(raw json.RawMessage) string {
	t := strings.TrimSpace(string(raw))
	if t == "" || t == "null" {
		return "—"
	}
	dec := json.NewDecoder(strings.NewReader(t))
	dec.UseNumber()
	var b strings.Builder
	if err := writeJSONValue(&b, dec, 0); err != nil {
		var buf bytes.Buffer
		if json.Indent(&buf, raw, "", "  ") == nil {
			return buf.String()
		}
		return t
	}
	return b.String()
}

func jsonIndent(depth int) string { return strings.Repeat("  ", depth) }

// encString re-encodes a Go string as JSON without HTML escaping and without
// \uXXXX escaping of printable runes — so umlauts etc. render literally.
func encString(s string) string {
	var bb bytes.Buffer
	enc := json.NewEncoder(&bb)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(bb.String(), "\n")
}

func writeJSONValue(b *strings.Builder, dec *json.Decoder, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			return writeJSONObject(b, dec, depth)
		case '[':
			return writeJSONArray(b, dec, depth)
		}
	case string:
		b.WriteString(encString(v))
	case json.Number:
		b.WriteString(v.String())
	case bool:
		if v {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case nil:
		b.WriteString("null")
	}
	return nil
}

func writeJSONObject(b *strings.Builder, dec *json.Decoder, depth int) error {
	b.WriteString("{")
	first := true
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString("\n")
		b.WriteString(jsonIndent(depth + 1))
		b.WriteString(encString(keyTok.(string)))
		b.WriteString(": ")
		if err := writeJSONValue(b, dec, depth+1); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil { // consume '}'
		return err
	}
	if !first {
		b.WriteString("\n")
		b.WriteString(jsonIndent(depth))
	}
	b.WriteString("}")
	return nil
}

func writeJSONArray(b *strings.Builder, dec *json.Decoder, depth int) error {
	b.WriteString("[")
	first := true
	for dec.More() {
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString("\n")
		b.WriteString(jsonIndent(depth + 1))
		if err := writeJSONValue(b, dec, depth+1); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil { // consume ']'
		return err
	}
	if !first {
		b.WriteString("\n")
		b.WriteString(jsonIndent(depth))
	}
	b.WriteString("]")
	return nil
}
