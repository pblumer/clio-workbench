package process

import (
	"strings"
	"testing"
)

func ev(subject string, types ...string) []Event {
	out := make([]Event, 0, len(types))
	for _, t := range types {
		out = append(out, Event{Subject: subject, Type: t})
	}
	return out
}

func edge(g Graph, from, to string) int {
	for _, e := range g.Edges {
		if e.From == from && e.To == to {
			return e.Count
		}
	}
	return 0
}

func node(g Graph, t string) Node {
	for _, n := range g.Nodes {
		if n.Type == t {
			return n
		}
	}
	return Node{}
}

func TestDiscoverDFGAndVariants(t *testing.T) {
	var events []Event
	// Two subjects follow placed→paid→shipped, one follows placed→cancelled.
	events = append(events, ev("/o/1", "placed", "paid", "shipped")...)
	events = append(events, ev("/o/2", "placed", "paid", "shipped")...)
	events = append(events, ev("/o/3", "placed", "cancelled")...)

	g := Discover(events, 0)

	if g.Subjects != 3 {
		t.Errorf("subjects = %d, want 3", g.Subjects)
	}
	if g.Events != 8 {
		t.Errorf("events = %d, want 8", g.Events)
	}
	if c := edge(g, "placed", "paid"); c != 2 {
		t.Errorf("edge placed→paid = %d, want 2", c)
	}
	if c := edge(g, "paid", "shipped"); c != 2 {
		t.Errorf("edge paid→shipped = %d, want 2", c)
	}
	if c := edge(g, "placed", "cancelled"); c != 1 {
		t.Errorf("edge placed→cancelled = %d, want 1", c)
	}

	if n := node(g, "placed"); n.StartCount != 3 {
		t.Errorf("placed StartCount = %d, want 3", n.StartCount)
	}
	if n := node(g, "shipped"); n.EndCount != 2 {
		t.Errorf("shipped EndCount = %d, want 2", n.EndCount)
	}
	if n := node(g, "cancelled"); n.EndCount != 1 {
		t.Errorf("cancelled EndCount = %d, want 1", n.EndCount)
	}
	if n := node(g, "paid"); n.Count != 2 {
		t.Errorf("paid Count = %d, want 2", n.Count)
	}

	// Variants: the happy path (2) ranks before the cancel path (1).
	if len(g.Variants) != 2 {
		t.Fatalf("variants = %d, want 2", len(g.Variants))
	}
	if got := strings.Join(g.Variants[0].Sequence, " "); got != "placed paid shipped" || g.Variants[0].Count != 2 {
		t.Errorf("top variant = %q x%d, want 'placed paid shipped' x2", got, g.Variants[0].Count)
	}
}

func TestDiscoverRanksLeftToRight(t *testing.T) {
	g := Discover(ev("/o/1", "a", "b", "c", "d"), 0)
	for _, want := range []struct {
		t string
		r int
	}{{"a", 0}, {"b", 1}, {"c", 2}, {"d", 3}} {
		if got := node(g, want.t).Rank; got != want.r {
			t.Errorf("rank %q = %d, want %d", want.t, got, want.r)
		}
	}
}

func TestDiscoverHandlesCycles(t *testing.T) {
	// a→b→a→b→c : a back-edge must not cause infinite ranking.
	g := Discover(ev("/o/1", "a", "b", "a", "b", "c"), 0)
	if edge(g, "a", "b") != 2 || edge(g, "b", "a") != 1 {
		t.Errorf("cycle edges wrong: a→b=%d b→a=%d", edge(g, "a", "b"), edge(g, "b", "a"))
	}
	if node(g, "a").Rank != 0 {
		t.Errorf("a rank = %d, want 0 (start)", node(g, "a").Rank)
	}
	if node(g, "c").Rank < 1 {
		t.Errorf("c rank = %d, want >= 1", node(g, "c").Rank)
	}
}

func TestDiscoverSelfLoop(t *testing.T) {
	g := Discover(ev("/o/1", "tick", "tick", "tick"), 0)
	if c := edge(g, "tick", "tick"); c != 2 {
		t.Errorf("self-loop tick→tick = %d, want 2", c)
	}
}

func TestDiscoverMaxVariants(t *testing.T) {
	var events []Event
	events = append(events, ev("/o/1", "a")...)
	events = append(events, ev("/o/2", "b")...)
	events = append(events, ev("/o/3", "c")...)
	g := Discover(events, 2)
	if len(g.Variants) != 2 {
		t.Errorf("variants capped = %d, want 2", len(g.Variants))
	}
}
