package simulator

import (
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/validate"
)

// rejected reports whether a mutated stream is refused by the engine — either
// its sequence is not a valid walk, or one of its payloads fails its schema.
func rejected(d model.Draft, m *validate.Machine, s Stream) bool {
	types := make([]string, len(s.Events))
	for i, e := range s.Events {
		types[i] = e.Type
	}
	if !m.CheckSequence(types).OK {
		return true
	}
	for _, e := range s.Events {
		if errs, err := validate.CheckPayload(fieldsFor(d, e.Type), e.Data); err != nil || len(errs) != 0 {
			return true
		}
	}
	return false
}

func TestMutationsAreRejected(t *testing.T) {
	d := orderGraph()
	m := validate.NewMachine(d)

	// A valid base stream that traverses the paid edge (so it carries fields).
	var base Stream
	for seed := int64(0); seed < 100; seed++ {
		s, _ := Generate(d, Options{Seed: seed})
		if streamHasType(s, "paid") {
			base = s
			break
		}
	}
	if !streamHasType(base, "paid") {
		t.Fatal("could not generate a base stream containing the paid event")
	}

	muts := Mutations(d, base, 1)
	kinds := map[string]bool{}
	for _, mut := range muts {
		kinds[mut.Kind] = true
		// The guaranteed-invalid mutations must be rejected.
		switch mut.Kind {
		case "insert-unknown", "drop-required", "wrong-type":
			if !rejected(d, m, mut.Stream) {
				t.Errorf("%s mutation was not rejected: %+v", mut.Kind, mut.Stream)
			}
		}
		if mut.Desc == "" {
			t.Errorf("%s mutation has no description", mut.Kind)
		}
	}
	for _, want := range []string{"insert-unknown", "swap-order", "drop-required", "wrong-type"} {
		if !kinds[want] {
			t.Errorf("expected a %q mutation for the order stream", want)
		}
	}
}

func streamHasType(s Stream, typ string) bool {
	for _, e := range s.Events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func TestMutationsDeterministic(t *testing.T) {
	d := orderGraph()
	base, _ := Generate(d, Options{Seed: 5})
	a := Mutations(d, base, 9)
	b := Mutations(d, base, 9)
	if len(a) != len(b) {
		t.Fatalf("non-deterministic count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || a[i].Desc != b[i].Desc {
			t.Fatalf("mutation %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestMutationsNotApplicable(t *testing.T) {
	// A model with no fields and single-step streams: drop/wrong/swap don't apply,
	// only insert-unknown does.
	d := model.Draft{
		Nodes: []model.Node{{ID: "a", Start: true}, {ID: "b", End: true}},
		Edges: []model.Edge{{ID: "e", Type: "go", From: "a", To: "b"}},
	}
	s, _ := Generate(d, Options{Seed: 1}) // single event "go"
	muts := Mutations(d, s, 1)
	if len(muts) != 1 || muts[0].Kind != "insert-unknown" {
		t.Fatalf("expected only insert-unknown, got %+v", muts)
	}
}

func TestInsertUnknownOnEmptyStream(t *testing.T) {
	// start==end → no events; insert-unknown still applies (splice at 0).
	d := model.Draft{Nodes: []model.Node{{ID: "a", Start: true, End: true}}}
	s, _ := Generate(d, Options{})
	muts := Mutations(d, s, 1)
	if len(muts) != 1 || len(muts[0].Stream.Events) != 1 {
		t.Fatalf("expected one inserted event, got %+v", muts)
	}
}

func TestPayloadMutationsSkipNilData(t *testing.T) {
	// An event whose type declares required/typed fields but carries no payload
	// (inconsistent input): the payload mutations can't operate and are skipped.
	d := orderGraph() // "paid" has required id + amount fields
	s := Stream{Events: []Event{{Type: "paid", Data: nil}}}
	for _, m := range Mutations(d, s, 1) {
		if m.Kind == "drop-required" || m.Kind == "wrong-type" {
			t.Fatalf("payload mutation should be skipped on nil data, got %s", m.Kind)
		}
	}
}

func TestKeyHelpers(t *testing.T) {
	if _, ok := dropKey(nil, "x"); ok {
		t.Errorf("dropKey on nil should fail")
	}
	if _, ok := dropKey([]byte(`{"a":1}`), "missing"); ok {
		t.Errorf("dropKey of absent key should fail")
	}
	got, ok := dropKey([]byte(`{"a":1,"b":2}`), "a")
	if !ok || strings.Contains(string(got), `"a"`) {
		t.Errorf("dropKey did not remove key: %s", got)
	}
	if _, ok := setKey(nil, "x", 1); ok {
		t.Errorf("setKey on nil should fail")
	}
	if _, ok := toMap([]byte(`[1,2]`)); ok {
		t.Errorf("toMap of non-object should fail")
	}
	if _, ok := toMap([]byte(`null`)); ok {
		t.Errorf("toMap of null should fail")
	}
}
