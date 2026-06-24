// Package simulator is the Test Studio's generator (docs/TESTSTUDIO.md §4,
// roadmap WP-5): it turns a designed model into event streams.
//
// It walks the draft graph from a start state along (optionally weighted) edges
// to an end state, faking a schema-valid data payload for each traversed
// event type from its authored fields. Every walk is seeded, so the same seed
// and model yield an identical stream (§4.4) — the basis for reproducible runs.
//
// The walk honours per-subject cardinality (model.CardinalityOnce): a "once"
// edge is taken at most once per stream, so generated streams never contradict
// the same rule the validation engine enforces.
//
// The generator deliberately walks the graph directly rather than reaching into
// validate.Machine: it *produces* streams, while internal/validate *checks*
// them. A completed walk is, by construction, a valid sequence — property tests
// assert exactly that by running generated streams back through validate.
package simulator

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/pblumer/clio-workbench/internal/model"
)

// defaultMaxSteps caps a walk so cyclic models cannot loop forever.
const defaultMaxSteps = 100

// Options tunes a generation run.
type Options struct {
	Seed     int64          // deterministic source (0 is a valid, fixed seed)
	MaxSteps int            // safety cap on walk length (≤0 → defaultMaxSteps)
	Weights  map[string]int // optional per-edge-id bias; absent/≤0 → weight 1
}

// Event is one generated event: its type and a faked data payload.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Stream is the result of one walk.
type Stream struct {
	Seed     int64    `json:"seed"`
	Events   []Event  `json:"events,omitempty"`
	Path     []string `json:"path"`              // node ids visited, entry first
	EdgeIDs  []string `json:"edgeIds,omitempty"` // edges traversed, in order
	Complete bool     `json:"complete"`          // reached an end state
}

// graph is the walkable projection of a draft.
type graph struct {
	out    map[string][]model.Edge // node id -> outgoing edges, in draft order
	starts []string                // start node ids, in draft order
	isEnd  map[string]bool
	fields map[string][]model.Field // event-type name -> authored fields
}

func buildGraph(d model.Draft) graph {
	g := graph{
		out:    make(map[string][]model.Edge),
		isEnd:  make(map[string]bool),
		fields: make(map[string][]model.Field),
	}
	for _, n := range d.Nodes {
		if n.Start {
			g.starts = append(g.starts, n.ID)
		}
		if n.End {
			g.isEnd[n.ID] = true
		}
	}
	for _, e := range d.Edges {
		g.out[e.From] = append(g.out[e.From], e)
	}
	for _, st := range d.Steps {
		if st.Kind == model.StepEvent && strings.TrimSpace(st.Name) != "" {
			g.fields[st.Name] = st.Fields
		}
	}
	return g
}

// Generate produces one seeded walk of the model.
func Generate(d model.Draft, opts Options) (Stream, error) {
	g := buildGraph(d)
	if len(g.starts) == 0 {
		return Stream{}, errors.New("model has no start state")
	}
	max := opts.MaxSteps
	if max <= 0 {
		max = defaultMaxSteps
	}
	rng := rand.New(rand.NewSource(opts.Seed))

	cur := g.starts[rng.Intn(len(g.starts))]
	st := Stream{Seed: opts.Seed, Path: []string{cur}}
	usedOnce := map[string]bool{} // event types already emitted via a "once" edge
	for i := 0; i < max; i++ {
		if g.isEnd[cur] {
			st.Complete = true
			break
		}
		edges := availableEdges(g.out[cur], usedOnce)
		if len(edges) == 0 {
			break // dead end (no outgoing edge, or all spent by cardinality)
		}
		e := pickEdge(edges, opts.Weights, rng)
		if e.Cardinality == model.CardinalityOnce {
			usedOnce[e.Type] = true
		}
		st.Events = append(st.Events, Event{Type: e.Type, Data: fakePayload(g.fields[e.Type], rng)})
		st.EdgeIDs = append(st.EdgeIDs, e.ID)
		cur = e.To
		st.Path = append(st.Path, cur)
	}
	return st, nil
}

// GenerateN produces n walks with deterministically derived seeds (Seed+i), so
// the whole batch is reproducible from a single seed.
func GenerateN(d model.Draft, n int, opts Options) ([]Stream, error) {
	streams := make([]Stream, 0, n)
	for i := 0; i < n; i++ {
		o := opts
		o.Seed = opts.Seed + int64(i)
		s, err := Generate(d, o)
		if err != nil {
			return nil, err
		}
		streams = append(streams, s)
	}
	return streams, nil
}

// EdgeCoverage reports how many of the model's edges the streams traversed.
func EdgeCoverage(d model.Draft, streams []Stream) (covered, total int) {
	seen := make(map[string]bool)
	for _, s := range streams {
		for _, id := range s.EdgeIDs {
			seen[id] = true
		}
	}
	return len(seen), len(d.Edges)
}

// availableEdges drops "once" edges whose type was already emitted in this walk,
// so a generated stream never violates per-subject cardinality (and thus always
// validates). It returns the input unchanged when nothing is filtered, avoiding
// an allocation on the common (cardinality-free) path; otherwise it copies so
// the graph's backing slice is never mutated.
func availableEdges(edges []model.Edge, usedOnce map[string]bool) []model.Edge {
	spent := false
	for _, e := range edges {
		if e.Cardinality == model.CardinalityOnce && usedOnce[e.Type] {
			spent = true
			break
		}
	}
	if !spent {
		return edges
	}
	avail := make([]model.Edge, 0, len(edges))
	for _, e := range edges {
		if e.Cardinality == model.CardinalityOnce && usedOnce[e.Type] {
			continue
		}
		avail = append(avail, e)
	}
	return avail
}

// pickEdge chooses an outgoing edge by weight. weightOf is ≥1, so total ≥1 and
// the loop always returns; the trailing return is defensive (see docs/TESTING.md).
func pickEdge(edges []model.Edge, weights map[string]int, rng *rand.Rand) model.Edge {
	total := 0
	for _, e := range edges {
		total += weightOf(weights, e.ID)
	}
	r := rng.Intn(total)
	for _, e := range edges {
		r -= weightOf(weights, e.ID)
		if r < 0 {
			return e
		}
	}
	return edges[len(edges)-1]
}

func weightOf(weights map[string]int, id string) int {
	if w, ok := weights[id]; ok && w > 0 {
		return w
	}
	return 1
}

// fakePayload builds a schema-valid data object for an event type's fields.
// Returns nil when the type has no named fields.
func fakePayload(fields []model.Field, rng *rand.Rand) json.RawMessage {
	obj := make(map[string]any)
	for _, f := range fields {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			continue
		}
		obj[name] = fakeValue(f, rng)
	}
	if len(obj) == 0 {
		return nil
	}
	b, _ := json.Marshal(obj)
	return b
}

func fakeValue(f model.Field, rng *rand.Rand) any {
	switch f.Type {
	case "integer":
		return rng.Intn(1000)
	case "number":
		return float64(rng.Intn(100000)) / 100 // two decimals
	case "boolean":
		return rng.Intn(2) == 1
	case "enum":
		if len(f.Enum) > 0 {
			return f.Enum[rng.Intn(len(f.Enum))]
		}
		return "value"
	case "datetime":
		return fakeTime(rng)
	default: // string, reference
		return fakeString(f.Format, rng)
	}
}

func fakeString(format string, rng *rand.Rand) string {
	switch format {
	case "uuid":
		return fakeUUID(rng)
	case "email":
		return fmt.Sprintf("user%d@example.com", rng.Intn(100000))
	case "date-time":
		return fakeTime(rng)
	default:
		return fmt.Sprintf("sample-%d", rng.Intn(100000))
	}
}

func fakeTime(rng *rand.Rand) string {
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	return time.Unix(base+int64(rng.Intn(100_000_000)), 0).UTC().Format(time.RFC3339)
}

func fakeUUID(rng *rand.Rand) string {
	var b [16]byte
	binary.LittleEndian.PutUint64(b[0:8], rng.Uint64())
	binary.LittleEndian.PutUint64(b[8:16], rng.Uint64())
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
