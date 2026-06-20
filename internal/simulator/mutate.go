package simulator

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"

	"github.com/pblumer/clio-workbench/internal/model"
)

// mutate.go produces deliberately-broken variants of a valid stream
// (docs/TESTSTUDIO.md §4.3): negative material that proves the engine rejects,
// not just accepts. Mutations are structural; the caller runs them back through
// internal/validate to record whether they were actually rejected (the
// "produce vs check" separation, kept here too).

const bogusType = "__mutant_unknown_type__"

// Mutation is one broken variant of a stream with a description of the damage.
type Mutation struct {
	Kind   string `json:"kind"`
	Desc   string `json:"desc"`
	Stream Stream `json:"stream"`
}

// Mutations returns the applicable broken variants of a valid stream, chosen
// deterministically from the seed:
//
//   - insert-unknown: splice in an event type that is no edge anywhere;
//   - swap-order: swap two adjacent events;
//   - drop-required: remove a required field from a payload;
//   - wrong-type: overwrite a field value with the wrong JSON type.
//
// drop-required and wrong-type apply only when the stream carries a suitable
// field; swap-order needs at least two events.
func Mutations(d model.Draft, s Stream, seed int64) []Mutation {
	rng := rand.New(rand.NewSource(seed))
	fields := buildGraph(d).fields

	var out []Mutation
	if m, ok := mutInsertUnknown(s, rng); ok {
		out = append(out, m)
	}
	if m, ok := mutSwapOrder(s, rng); ok {
		out = append(out, m)
	}
	if m, ok := mutDropRequired(s, fields, rng); ok {
		out = append(out, m)
	}
	if m, ok := mutWrongType(s, fields, rng); ok {
		out = append(out, m)
	}
	return out
}

func mutInsertUnknown(s Stream, rng *rand.Rand) (Mutation, bool) {
	pos := rng.Intn(len(s.Events) + 1)
	evs := make([]Event, 0, len(s.Events)+1)
	evs = append(evs, s.Events[:pos]...)
	evs = append(evs, Event{Type: bogusType})
	evs = append(evs, s.Events[pos:]...)
	return Mutation{
		Kind:   "insert-unknown",
		Desc:   fmt.Sprintf("unbekannten Event-Typ %q an Position %d eingefügt", bogusType, pos),
		Stream: withEvents(s, evs),
	}, true
}

func mutSwapOrder(s Stream, rng *rand.Rand) (Mutation, bool) {
	if len(s.Events) < 2 {
		return Mutation{}, false
	}
	i := rng.Intn(len(s.Events) - 1)
	evs := cloneEvents(s.Events)
	evs[i], evs[i+1] = evs[i+1], evs[i]
	return Mutation{
		Kind:   "swap-order",
		Desc:   fmt.Sprintf("Events an Position %d und %d vertauscht", i, i+1),
		Stream: withEvents(s, evs),
	}, true
}

func mutDropRequired(s Stream, fields map[string][]model.Field, rng *rand.Rand) (Mutation, bool) {
	type cand struct {
		idx   int
		field string
	}
	var cands []cand
	for i, e := range s.Events {
		for _, f := range fields[e.Type] {
			if f.Required && strings.TrimSpace(f.Name) != "" {
				cands = append(cands, cand{i, f.Name})
			}
		}
	}
	if len(cands) == 0 {
		return Mutation{}, false
	}
	c := cands[rng.Intn(len(cands))]
	data, ok := dropKey(s.Events[c.idx].Data, c.field)
	if !ok {
		return Mutation{}, false
	}
	evs := cloneEvents(s.Events)
	evs[c.idx].Data = data
	return Mutation{
		Kind:   "drop-required",
		Desc:   fmt.Sprintf("Pflichtfeld %q aus %q entfernt", c.field, evs[c.idx].Type),
		Stream: withEvents(s, evs),
	}, true
}

func mutWrongType(s Stream, fields map[string][]model.Field, rng *rand.Rand) (Mutation, bool) {
	type cand struct {
		idx   int
		field model.Field
	}
	var cands []cand
	for i, e := range s.Events {
		for _, f := range fields[e.Type] {
			if strings.TrimSpace(f.Name) != "" {
				cands = append(cands, cand{i, f})
			}
		}
	}
	if len(cands) == 0 {
		return Mutation{}, false
	}
	c := cands[rng.Intn(len(cands))]
	// A string where a number/bool is expected, or a number where a string is.
	var wrong any = 123
	switch c.field.Type {
	case "integer", "number", "boolean":
		wrong = "mutant"
	}
	data, ok := setKey(s.Events[c.idx].Data, c.field.Name, wrong)
	if !ok {
		return Mutation{}, false
	}
	evs := cloneEvents(s.Events)
	evs[c.idx].Data = data
	return Mutation{
		Kind:   "wrong-type",
		Desc:   fmt.Sprintf("Feld %q in %q mit falschem Typ überschrieben", c.field.Name, evs[c.idx].Type),
		Stream: withEvents(s, evs),
	}, true
}

// withEvents returns a copy of s carrying the given events; the path/edge trail
// no longer describes the mutated stream, so it is cleared.
func withEvents(s Stream, evs []Event) Stream {
	s.Events = evs
	s.Path = nil
	s.EdgeIDs = nil
	s.Complete = false
	return s
}

func cloneEvents(evs []Event) []Event {
	out := make([]Event, len(evs))
	copy(out, evs)
	return out
}

func dropKey(data json.RawMessage, key string) (json.RawMessage, bool) {
	m, ok := toMap(data)
	if !ok {
		return nil, false
	}
	if _, present := m[key]; !present {
		return nil, false
	}
	delete(m, key)
	b, _ := json.Marshal(m)
	return b, true
}

func setKey(data json.RawMessage, key string, val any) (json.RawMessage, bool) {
	m, ok := toMap(data)
	if !ok {
		return nil, false
	}
	m[key] = val
	b, _ := json.Marshal(m)
	return b, true
}

func toMap(data json.RawMessage) (map[string]any, bool) {
	if len(data) == 0 {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}
