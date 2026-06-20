package simulator

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/validate"
)

// orderGraph: (start) new --created--> placed --paid--> paid (end)
//
//	\--cancelled--> cancelled (end)
//
// order-paid carries a couple of fields so the faker has work to do.
func orderGraph() model.Draft {
	return model.Draft{
		Nodes: []model.Node{
			{ID: "new", Label: "New", Start: true},
			{ID: "placed", Label: "Placed"},
			{ID: "paid", Label: "Paid", End: true},
			{ID: "cancelled", Label: "Cancelled", End: true},
		},
		Edges: []model.Edge{
			{ID: "e1", Type: "created", From: "new", To: "placed"},
			{ID: "e2", Type: "paid", From: "placed", To: "paid"},
			{ID: "e3", Type: "cancelled", From: "placed", To: "cancelled"},
		},
		Steps: []model.Step{
			{ID: "s1", Kind: model.StepEvent, Name: "paid", Fields: []model.Field{
				{Name: "id", Type: "reference", Format: "uuid", Required: true},
				{Name: "amount", Type: "number", Required: true},
			}},
			{ID: "s2", Kind: model.StepTask, Name: "ship"}, // ignored
		},
	}
}

func TestGenerateDeterministic(t *testing.T) {
	d := orderGraph()
	a, err := Generate(d, Options{Seed: 42})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	b, _ := Generate(d, Options{Seed: 42})
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same seed must yield identical streams:\n%+v\n%+v", a, b)
	}
	if !a.Complete || len(a.Path) < 2 {
		t.Fatalf("expected a completed walk, got %+v", a)
	}
}

func TestGeneratedStreamsAreValid(t *testing.T) {
	d := orderGraph()
	m := validate.NewMachine(d)
	streams, err := GenerateN(d, 50, Options{Seed: 1})
	if err != nil {
		t.Fatalf("generateN: %v", err)
	}
	for i, s := range streams {
		if !s.Complete {
			t.Fatalf("stream %d did not complete: %+v", i, s)
		}
		types := make([]string, len(s.Events))
		for j, e := range s.Events {
			types[j] = e.Type
			// Each faked payload must satisfy the event type's schema.
			fields := fieldsFor(d, e.Type)
			if errs, err := validate.CheckPayload(fields, e.Data); err != nil || len(errs) != 0 {
				t.Fatalf("stream %d event %q payload invalid: errs=%+v err=%v data=%s", i, e.Type, errs, err, e.Data)
			}
		}
		if out := m.CheckSequence(types); !out.OK {
			t.Fatalf("stream %d is not a valid walk: %s (%v)", i, out.Reason, types)
		}
	}
}

func TestGenerateNoStart(t *testing.T) {
	d := model.Draft{Nodes: []model.Node{{ID: "a"}}, Edges: []model.Edge{{ID: "e", Type: "t", From: "a", To: "a"}}}
	if _, err := Generate(d, Options{}); err == nil {
		t.Fatalf("expected error for model with no start state")
	}
	if _, err := GenerateN(d, 3, Options{}); err == nil {
		t.Fatalf("GenerateN should propagate the no-start error")
	}
}

func TestEdgeCoverage(t *testing.T) {
	d := orderGraph()
	streams, _ := GenerateN(d, 50, Options{Seed: 7})
	covered, total := EdgeCoverage(d, streams)
	if total != 3 {
		t.Fatalf("total edges = %d, want 3", total)
	}
	if covered != 3 {
		t.Fatalf("expected all 3 edges covered across 50 walks, got %d", covered)
	}
}

func TestWeightsBiasChoice(t *testing.T) {
	d := orderGraph()
	// Force the placed→cancelled edge to dominate; every walk should cancel.
	opts := Options{Seed: 3, Weights: map[string]int{"e3": 1000, "e2": 0}}
	streams, _ := GenerateN(d, 20, opts)
	for i, s := range streams {
		last := s.Path[len(s.Path)-1]
		if last != "cancelled" {
			t.Fatalf("stream %d ended in %q, expected cancelled under heavy e3 weight", i, last)
		}
	}
}

func TestStartIsAlsoEnd(t *testing.T) {
	d := model.Draft{Nodes: []model.Node{{ID: "a", Start: true, End: true}}}
	s, err := Generate(d, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Complete || len(s.Events) != 0 {
		t.Fatalf("start==end should complete with no events, got %+v", s)
	}
}

func TestDeadEnd(t *testing.T) {
	// b is neither terminal nor has outgoing edges → walk stops, incomplete.
	d := model.Draft{
		Nodes: []model.Node{{ID: "a", Start: true}, {ID: "b"}},
		Edges: []model.Edge{{ID: "e", Type: "go", From: "a", To: "b"}},
	}
	s, _ := Generate(d, Options{})
	if s.Complete {
		t.Fatalf("dead-end walk must not be complete: %+v", s)
	}
	if len(s.Events) != 1 || s.Events[0].Type != "go" {
		t.Fatalf("expected one 'go' event, got %+v", s.Events)
	}
}

func TestMaxStepsCapsCycle(t *testing.T) {
	// A cycle with no end state must be capped by MaxSteps.
	d := model.Draft{
		Nodes: []model.Node{{ID: "a", Start: true}, {ID: "b"}},
		Edges: []model.Edge{
			{ID: "e1", Type: "ab", From: "a", To: "b"},
			{ID: "e2", Type: "ba", From: "b", To: "a"},
		},
	}
	s, _ := Generate(d, Options{MaxSteps: 5})
	if s.Complete {
		t.Fatalf("cyclic walk should not complete")
	}
	if len(s.Events) != 5 {
		t.Fatalf("expected exactly MaxSteps=5 events, got %d", len(s.Events))
	}
}

func TestFakePayloadCoversAllTypes(t *testing.T) {
	rng := newRNG(99)
	fields := []model.Field{
		{Name: "i", Type: "integer"},
		{Name: "n", Type: "number"},
		{Name: "b", Type: "boolean"},
		{Name: "e", Type: "enum", Enum: []string{"A", "B"}},
		{Name: "e0", Type: "enum"}, // no values → "value"
		{Name: "dt", Type: "datetime"},
		{Name: "ref", Type: "reference", Format: "uuid"},
		{Name: "mail", Type: "string", Format: "email"},
		{Name: "ts", Type: "string", Format: "date-time"},
		{Name: "plain", Type: "string"},
		{Name: "", Type: "string"}, // unnamed → skipped
	}
	data := fakePayload(fields, rng)
	// The generated payload must validate against the same fields.
	if errs, err := validate.CheckPayload(fields, data); err != nil || len(errs) != 0 {
		t.Fatalf("faked payload invalid: errs=%+v err=%v data=%s", errs, err, data)
	}
}

func TestFakePayloadEmpty(t *testing.T) {
	if got := fakePayload(nil, newRNG(1)); got != nil {
		t.Fatalf("no fields should yield nil payload, got %s", got)
	}
	// Only an unnamed field → still nil.
	if got := fakePayload([]model.Field{{Type: "string"}}, newRNG(1)); got != nil {
		t.Fatalf("unnamed-only should yield nil payload, got %s", got)
	}
}

// fieldsFor returns the authored fields of an event type in the draft.
func fieldsFor(d model.Draft, typ string) []model.Field {
	for _, st := range d.Steps {
		if st.Kind == model.StepEvent && st.Name == typ {
			return st.Fields
		}
	}
	return nil
}

func newRNG(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }
