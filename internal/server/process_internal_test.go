package server

import (
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/process"
)

// TestBuildProcessViewRich crafts a graph that exercises the layout branches:
//   - a task that spans two event types (→ a task group backdrop)
//   - a self-loop edge
//   - a bidirectional pair (→ bowed/bent edges)
//   - variants with a non-zero trace count (→ percentages)
func TestBuildProcessViewRich(t *testing.T) {
	g := process.Graph{
		Subjects: 3,
		Events:   9,
		Traces:   4,
		Nodes: []process.Node{
			{Type: "order.created", Task: "order", Phase: process.PhaseActive, Count: 4, StartCount: 4, Rank: 0},
			{Type: "order.completed", Task: "order", Phase: process.PhaseComplete, Count: 3, EndCount: 3, Rank: 1},
			{Type: "ping", Task: "ping", Count: 2, Rank: 1},
		},
		Edges: []process.Edge{
			{From: "order.created", To: "order.completed", Count: 3},
			{From: "order.completed", To: "order.created", Count: 1}, // opposite → bend
			{From: "ping", To: "ping", Count: 2},                     // self-loop
			{From: "order.created", To: "ghost", Count: 1},           // dangling target → skipped
		},
		Variants: []process.Variant{
			{Sequence: []string{"order.created", "order.completed"}, Count: 3},
			{Sequence: []string{"ping"}, Count: 1},
		},
	}

	v := buildProcessView(g)
	if v.State != "ok" {
		t.Fatalf("state = %q", v.State)
	}
	// Task "order" spans two event types → one group; "ping" stands alone → none.
	if len(v.Groups) != 1 || v.Groups[0].Label != "order" {
		t.Fatalf("groups = %+v", v.Groups)
	}
	// The dangling edge to "ghost" was skipped; 3 real edges remain.
	if len(v.Edges) != 3 {
		t.Errorf("edges = %d, want 3", len(v.Edges))
	}
	// Variants carry percentages computed against Traces.
	if len(v.Variants) != 2 || v.Variants[0].Pct == 0 {
		t.Errorf("variants = %+v", v.Variants)
	}
}

func TestReplayJSON(t *testing.T) {
	out := replayJSON([]clio.Event{
		{Subject: "/o/1", Type: "created", Time: "t1"},
		{Subject: "/o/<2>", Type: "shipped", Time: "t2"}, // <,> get escaped
	})
	s := string(out)
	// encoding/json escapes < and > so the raw characters never reach the
	// <script> element; only their unicode-escaped forms appear.
	if !strings.Contains(s, `"created"`) {
		t.Errorf("replayJSON missing type: %s", s)
	}
	if strings.ContainsAny(s, "<>") {
		t.Errorf("replayJSON left raw angle brackets unescaped: %s", s)
	}
	// Empty input → "[]".
	if got := string(replayJSON(nil)); got != "[]" {
		t.Errorf("empty replayJSON = %q", got)
	}
}

// TestBuildTypeLegendCollapses drives the >legendCap path: 16 distinct types
// collapse into 14 + a "+N more types" entry.
func TestBuildTypeLegendCollapses(t *testing.T) {
	var dots []process.Dot
	for i := 0; i < 16; i++ {
		// Give each type a different count so ordering is deterministic.
		typ := string(rune('a' + i))
		for c := 0; c <= i; c++ {
			dots = append(dots, process.Dot{Type: typ})
		}
	}
	legend := buildTypeLegend(dots)
	if len(legend) != legendCap+1 {
		t.Fatalf("legend len = %d, want %d", len(legend), legendCap+1)
	}
	last := legend[len(legend)-1]
	if !strings.Contains(last.Type, "more types") {
		t.Errorf("expected collapse entry, got %q", last.Type)
	}
}

// TestEdgePathDegenerate covers edgePath's guard for two distinct nodes that
// occupy the same coordinates (dist == 0).
func TestEdgePathDegenerate(t *testing.T) {
	from := &procNode{Type: "a", X: 100, Y: 100, R: 18}
	to := &procNode{Type: "b", X: 100, Y: 100, R: 18} // same position, different type
	d, lx, ly := edgePath(from, to, false)
	if d == "" {
		t.Errorf("edgePath returned empty path")
	}
	_ = lx
	_ = ly
}

// TestEdgePathSelfLoop covers the self-loop branch (from.Type == to.Type).
func TestEdgePathSelfLoop(t *testing.T) {
	n := &procNode{Type: "a", X: 100, Y: 100, R: 18}
	if d, _, _ := edgePath(n, n, false); d == "" {
		t.Errorf("self-loop path empty")
	}
}

// TestBuildDottedViewClampsDodge crafts many overlapping dots near the right
// edge so the dodge-spread clamping branches (start < loX / start+width > hiX)
// fire.
func TestBuildDottedViewClampsDodge(t *testing.T) {
	var dots []process.Dot
	// 30 dots all at X≈1.0 (the far right edge) in one row → the spread runs
	// past hiX and gets clamped back.
	for i := 0; i < 30; i++ {
		dots = append(dots, process.Dot{Row: 0, X: 1.0, Type: "t"})
	}
	// And 30 dots at X≈0.0 (far left) in another row → clamped to loX.
	for i := 0; i < 30; i++ {
		dots = append(dots, process.Dot{Row: 1, X: 0.0, Type: "t"})
	}
	d := process.Dotted{
		Rows:  []process.DRow{{Subject: "/a", Count: 30}, {Subject: "/b", Count: 30}},
		Shown: 2, Total: 2, Events: 60,
		Dots: dots,
	}
	v := buildDottedView(d)
	if v.State != "ok" {
		t.Fatalf("state = %q", v.State)
	}
}

// TestBuildDottedViewLongLabelTruncates covers the subject-label truncation and
// the "capped" total>shown branch.
func TestBuildDottedViewLongLabelTruncates(t *testing.T) {
	long := "/very/long/subject/path/that/exceeds/twenty/six/characters/here"
	d := process.Dotted{
		Rows:   []process.DRow{{Subject: long, Count: 1}},
		Shown:  1,
		Total:  5, // > Shown → Capped
		Events: 1,
		Dots:   []process.Dot{{Row: 0, X: 0.5, Type: "a"}},
	}
	v := buildDottedView(d)
	if !v.Capped {
		t.Errorf("expected Capped")
	}
	if !strings.HasPrefix(v.Rows[0].Label, "…") {
		t.Errorf("long label not truncated: %q", v.Rows[0].Label)
	}
}
