package process

import (
	"sort"
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

func TestDiscoverSuppressesEndToStartLoop(t *testing.T) {
	// A subject reused for a second run: deployed (end) then new (start).
	g := Discover(ev("/e/1", "new", "created", "deployed", "new", "created", "deployed"), 0)
	if c := edge(g, "deployed", "new"); c != 0 {
		t.Errorf("deployed→new = %d, want 0 (instance restart suppressed)", c)
	}
	// Real forward steps are still there.
	if edge(g, "new", "created") == 0 || edge(g, "created", "deployed") == 0 {
		t.Error("forward edges new→created / created→deployed missing")
	}
	// The reused subject is split into 2 runs → one variant (single run), x2.
	if g.Traces != 2 {
		t.Errorf("traces = %d, want 2 runs", g.Traces)
	}
	if len(g.Variants) != 1 || g.Variants[0].Count != 2 {
		t.Fatalf("variants = %+v, want one variant x2", g.Variants)
	}
	if strings.Join(g.Variants[0].Sequence, " ") != "new created deployed" {
		t.Errorf("variant = %v, want a single run [new created deployed]", g.Variants[0].Sequence)
	}
}

func TestDiscoverSelfLoop(t *testing.T) {
	g := Discover(ev("/o/1", "tick", "tick", "tick"), 0)
	if c := edge(g, "tick", "tick"); c != 2 {
		t.Errorf("self-loop tick→tick = %d, want 2", c)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		in       string
		wantTask string
		wantPh   Phase
	}{
		{"shipping.started", "shipping", PhaseActive},
		{"shipping.completed", "shipping", PhaseComplete},
		{"shipping.failed", "shipping", PhaseError},
		{"order-payment-failed", "order-payment", PhaseError},
		{"order-status-updated", "order-status", PhaseInfo},
		{"order-placed", "order-placed", PhaseComplete}, // no marker → standalone fact
		{"cancelled", "cancelled", PhaseError},          // bare error word
	}
	for _, c := range cases {
		task, ph := Classify(c.in)
		if task != c.wantTask || ph != c.wantPh {
			t.Errorf("Classify(%q) = (%q,%s), want (%q,%s)", c.in, task, ph, c.wantTask, c.wantPh)
		}
	}
}

func TestDiscoverSetsPhase(t *testing.T) {
	g := Discover(ev("/o/1", "shipping.started", "shipping.completed", "shipping.failed"), 0)
	if node(g, "shipping.started").Phase != PhaseActive {
		t.Errorf("started phase = %s", node(g, "shipping.started").Phase)
	}
	if node(g, "shipping.failed").Phase != PhaseError {
		t.Errorf("failed phase = %s", node(g, "shipping.failed").Phase)
	}
	if node(g, "shipping.started").Task != "shipping" {
		t.Errorf("task = %s, want shipping", node(g, "shipping.started").Task)
	}
}

// group reports whether g.Concurrent contains exactly a group with these
// members (order-independent).
func hasGroup(g Graph, members ...string) bool {
	sortStrings := func(s []string) string {
		cp := append([]string(nil), s...)
		sort.Strings(cp)
		return strings.Join(cp, " ")
	}
	want := sortStrings(members)
	for _, grp := range g.Concurrent {
		if sortStrings(grp) == want {
			return true
		}
	}
	return false
}

func TestDiscoverDetectsConcurrency(t *testing.T) {
	// b and c run in parallel after a, before d: across many subjects both
	// orderings (b→c and c→b) occur in balanced numbers.
	// Unequal but balanced counts (b→c = 8, c→b = 12) so the dependency measure
	// still reads parallel and the |fwd-rev| sign-flip branch is exercised.
	var events []Event
	for i := 0; i < 8; i++ {
		events = append(events, ev("/o/b"+itoa(i), "a", "b", "c", "d")...)
	}
	for i := 0; i < 12; i++ {
		events = append(events, ev("/o/c"+itoa(i), "a", "c", "b", "d")...)
	}
	g := Discover(events, 0)

	if !hasGroup(g, "b", "c") {
		t.Fatalf("expected concurrent group {b,c}, got %v", g.Concurrent)
	}
	// a→d are sequential and must NOT be in a concurrent group.
	if hasGroup(g, "a", "d") || len(g.Concurrent) != 1 {
		t.Errorf("unexpected concurrent groups: %v", g.Concurrent)
	}
	// The within-block edges b↔c are flagged Parallel; the spine edges are not.
	for _, e := range g.Edges {
		inBlock := (e.From == "b" && e.To == "c") || (e.From == "c" && e.To == "b")
		if e.Parallel != inBlock {
			t.Errorf("edge %s→%s Parallel=%v, want %v", e.From, e.To, e.Parallel, inBlock)
		}
	}
	// Interleavings collapse: a b c d and a c b d are ONE variant x20, not two.
	if len(g.Variants) != 1 {
		t.Fatalf("variants = %d, want 1 (interleavings collapsed): %v", len(g.Variants), g.Variants)
	}
	if g.Variants[0].Count != 20 || strings.Join(g.Variants[0].Sequence, " ") != "a b c d" {
		t.Errorf("variant = %q x%d, want 'a b c d' x20", strings.Join(g.Variants[0].Sequence, " "), g.Variants[0].Count)
	}
	// Concurrent siblings share a column: b and c get the same rank.
	if rb, rc := node(g, "b").Rank, node(g, "c").Rank; rb != rc {
		t.Errorf("parallel b/c ranks differ: b=%d c=%d", rb, rc)
	}
}

func TestDiscoverMultipleConcurrentBlocks(t *testing.T) {
	// Two independent parallel blocks {b,c} and {x,y} → groups get sorted.
	var events []Event
	for i := 0; i < 5; i++ {
		events = append(events, ev("/o/1-"+itoa(i), "a", "b", "c", "x", "y", "z")...)
		events = append(events, ev("/o/2-"+itoa(i), "a", "c", "b", "y", "x", "z")...)
	}
	g := Discover(events, 0)
	if len(g.Concurrent) != 2 {
		t.Fatalf("groups = %v, want 2", g.Concurrent)
	}
	// Sorted by first member: {b,c} before {x,y}.
	if !hasGroup(g, "b", "c") || !hasGroup(g, "x", "y") {
		t.Errorf("groups = %v", g.Concurrent)
	}
	if g.Concurrent[0][0] != "b" || g.Concurrent[1][0] != "x" {
		t.Errorf("groups not sorted: %v", g.Concurrent)
	}
}

func TestDiscoverKeepsLopsidedPairSequential(t *testing.T) {
	// a→b dominates massively; a single reverse b→a is noise, not concurrency.
	var events []Event
	for i := 0; i < 30; i++ {
		events = append(events, ev("/o/"+itoa(i), "a", "b")...)
	}
	events = append(events, ev("/o/odd", "b", "a", "b")...)
	g := Discover(events, 0)
	if len(g.Concurrent) != 0 {
		t.Errorf("lopsided pair must stay sequential, got groups %v", g.Concurrent)
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
