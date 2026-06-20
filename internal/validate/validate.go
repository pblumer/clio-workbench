// Package validate is the Test Studio's validation engine (docs/TESTSTUDIO.md §6).
//
// It checks event streams against a designed model in two independent layers:
//
//   - Transition / sequence validation (§6.2): walk a sequence of event types
//     through the draft graph (start nodes, edges as event types, terminal
//     nodes) and report the first deviation.
//   - Payload validation (§6.1): check an event's data payload against the
//     authored fields (model.Field) — required, type, enum and light format.
//
// It is deliberately dependency-free. Rather than embed a general JSON Schema
// engine, payload validation runs directly against the constrained field model,
// mirroring schemagen.propSchema. That sidesteps the open library question
// (docs/TESTSTUDIO.md §12.1) for v1 and keeps the single-binary promise.
//
// The Gegenprobe (docs/WORKBENCH.md §7) is meant to converge on this same engine
// (roadmap T5 / WP-9), so the transition logic lives here, not in two places.
package validate

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/pblumer/clio-workbench/internal/model"
)

// Machine is the executable transition graph derived from a draft: which nodes
// are entry/terminal states and, per node, the outgoing event-type edges.
type Machine struct {
	order    []string          // node ids in draft order (deterministic walks)
	labels   map[string]string // node id -> label (id if empty)
	start    map[string]bool   // node ids marked Start
	end      map[string]bool   // node ids marked End
	out      map[string][]edge // node id -> outgoing edges, in draft order
	hasNodes bool              // false when the draft carries no graph
}

type edge struct {
	typ string
	to  string
}

// NewMachine builds the transition machine from a draft's graph (nodes + edges).
func NewMachine(d model.Draft) *Machine {
	m := &Machine{
		labels:   make(map[string]string, len(d.Nodes)),
		start:    make(map[string]bool),
		end:      make(map[string]bool),
		out:      make(map[string][]edge),
		hasNodes: len(d.Nodes) > 0,
	}
	for _, n := range d.Nodes {
		m.order = append(m.order, n.ID)
		if strings.TrimSpace(n.Label) != "" {
			m.labels[n.ID] = n.Label
		} else {
			m.labels[n.ID] = n.ID
		}
		if n.Start {
			m.start[n.ID] = true
		}
		if n.End {
			m.end[n.ID] = true
		}
	}
	for _, e := range d.Edges {
		m.out[e.From] = append(m.out[e.From], edge{typ: e.Type, to: e.To})
	}
	return m
}

// SeqOutcome is the result of checking an event-type sequence against a Machine.
type SeqOutcome struct {
	OK     bool     // whether the whole sequence is a valid walk
	Path   []string // node ids visited, starting at the entry node
	FailIx int      // index into the input of the first bad step; -1 when OK
	Reason string   // human-readable reason when !OK
}

// CheckSequence walks the given event-type sequence through the machine and
// reports the first deviation. An empty sequence trivially conforms.
//
// When several start states (or several edges of the first type) are possible,
// the walk is greedy and deterministic: start states are tried in draft order
// and, at each node, the first matching edge in draft order is taken. The
// failure reported is the attempt that progressed furthest.
func (m *Machine) CheckSequence(types []string) SeqOutcome {
	if !m.hasNodes {
		return SeqOutcome{FailIx: -1, Reason: "model has no graph to check against"}
	}
	if len(types) == 0 {
		return SeqOutcome{OK: true, FailIx: -1}
	}
	var starts []string
	for _, id := range m.order {
		if m.start[id] {
			starts = append(starts, id)
		}
	}
	if len(starts) == 0 {
		return SeqOutcome{FailIx: 0, Reason: "model has no start state"}
	}

	best := SeqOutcome{FailIx: -2} // sentinel below any real FailIx
	for _, s := range starts {
		out := m.walk(s, types)
		if out.OK {
			return out
		}
		if out.FailIx > best.FailIx {
			best = out
		}
	}
	return best
}

// walk attempts the full sequence from a single start node.
func (m *Machine) walk(start string, types []string) SeqOutcome {
	cur := start
	path := []string{cur}
	for i, t := range types {
		next, ok := m.step(cur, t)
		if !ok {
			return SeqOutcome{
				Path:   path,
				FailIx: i,
				Reason: fmt.Sprintf("no transition from state %q via event type %q", m.labels[cur], t),
			}
		}
		cur = next
		path = append(path, cur)
	}
	if len(m.end) > 0 && !m.end[cur] {
		return SeqOutcome{
			Path:   path,
			FailIx: len(types),
			Reason: fmt.Sprintf("sequence ends in non-terminal state %q", m.labels[cur]),
		}
	}
	return SeqOutcome{OK: true, Path: path, FailIx: -1}
}

// step returns the target of the first outgoing edge of typ from node.
func (m *Machine) step(node, typ string) (string, bool) {
	for _, e := range m.out[node] {
		if e.typ == typ {
			return e.to, true
		}
	}
	return "", false
}

// FieldError is one payload validation failure, located at a field.
type FieldError struct {
	Field   string // field name ("" for whole-payload errors)
	Rule    string // required | type | enum | format
	Message string
}

var (
	uuidRe  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// CheckPayload validates a data payload against the authored fields. It returns
// nil when the payload conforms. Unknown extra properties are allowed (the
// generated schema does not forbid them); fields with an empty name are skipped.
//
// The error return is reserved for plumbing failures (data is not valid JSON);
// a structurally valid but non-object payload is reported as a FieldError so the
// UI can surface it like any other.
func CheckPayload(fields []model.Field, data json.RawMessage) ([]FieldError, error) {
	var obj map[string]json.RawMessage
	if len(data) > 0 {
		if err := json.Unmarshal(data, &obj); err != nil {
			var any any
			if json.Unmarshal(data, &any) == nil {
				return []FieldError{{Rule: "type", Message: "data must be a JSON object"}}, nil
			}
			return nil, fmt.Errorf("data is not valid JSON: %w", err)
		}
	}

	var errs []FieldError
	for _, f := range fields {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			continue
		}
		raw, present := obj[name]
		if !present || isJSONNull(raw) {
			if f.Required {
				errs = append(errs, FieldError{Field: name, Rule: "required", Message: "missing required field"})
			}
			continue
		}
		if fe := checkField(f, name, raw); fe != nil {
			errs = append(errs, *fe)
		}
	}
	return errs, nil
}

// checkField validates one present field value against its declared type.
func checkField(f model.Field, name string, raw json.RawMessage) *FieldError {
	switch f.Type {
	case "integer":
		n, ok := asNumber(raw)
		if !ok {
			return typeErr(name, "integer")
		}
		if n != math.Trunc(n) {
			return &FieldError{Field: name, Rule: "type", Message: "expected an integer"}
		}
	case "number":
		if _, ok := asNumber(raw); !ok {
			return typeErr(name, "number")
		}
	case "boolean":
		if !isJSONBool(raw) {
			return typeErr(name, "boolean")
		}
	case "enum":
		s, ok := asString(raw)
		if !ok {
			return typeErr(name, "string")
		}
		if len(f.Enum) > 0 && !contains(f.Enum, s) {
			return &FieldError{Field: name, Rule: "enum", Message: fmt.Sprintf("value %q is not one of the allowed values", s)}
		}
	case "datetime":
		s, ok := asString(raw)
		if !ok {
			return typeErr(name, "string")
		}
		if !validDateTime(s) {
			return &FieldError{Field: name, Rule: "format", Message: "expected an RFC 3339 date-time"}
		}
	default: // string, reference
		s, ok := asString(raw)
		if !ok {
			return typeErr(name, "string")
		}
		return checkStringFormat(name, f.Format, s)
	}
	return nil
}

// checkStringFormat applies best-effort string-format checks. Unknown formats
// pass — the engine never rejects what it does not understand.
func checkStringFormat(name, format, s string) *FieldError {
	switch format {
	case "uuid":
		if !uuidRe.MatchString(s) {
			return &FieldError{Field: name, Rule: "format", Message: "expected a UUID"}
		}
	case "email":
		if !emailRe.MatchString(s) {
			return &FieldError{Field: name, Rule: "format", Message: "expected an email address"}
		}
	case "date-time":
		if !validDateTime(s) {
			return &FieldError{Field: name, Rule: "format", Message: "expected an RFC 3339 date-time"}
		}
	}
	return nil
}

func typeErr(field, want string) *FieldError {
	return &FieldError{Field: field, Rule: "type", Message: "expected type " + want}
}

func asNumber(raw json.RawMessage) (float64, bool) {
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	return n, true
}

func asString(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func isJSONBool(raw json.RawMessage) bool {
	var b bool
	return json.Unmarshal(raw, &b) == nil
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func validDateTime(s string) bool {
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
