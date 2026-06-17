package server

import (
	"testing"

	"github.com/pblumer/clio-workbench/internal/process"
)

// Overlapping events in the same row must be fanned out so each stays visible,
// while a lone dot keeps its position.
func TestBuildDottedViewDodgesOverlap(t *testing.T) {
	d := process.Dotted{
		Rows:   []process.DRow{{Subject: "/e/1", Count: 3}, {Subject: "/e/2", Count: 1}},
		ByTime: true,
		Shown:  2,
		Total:  2,
		Events: 4,
		Dots: []process.Dot{
			{Row: 0, X: 0.5, Type: "a"},
			{Row: 0, X: 0.5, Type: "b"},
			{Row: 0, X: 0.5, Type: "c"},
			{Row: 1, X: 0.2, Type: "a"},
		},
	}

	v := buildDottedView(d)
	if v.State != "ok" {
		t.Fatalf("state = %q, want ok", v.State)
	}

	// Collect row-0 dot X positions; all three must be distinct and ordered.
	var row0 []float64
	row0Y := v.Rows[0].TextY
	for _, p := range v.Dots {
		if p.Y == row0Y {
			row0 = append(row0, p.X)
		}
	}
	if len(row0) != 3 {
		t.Fatalf("row 0 dots = %d, want 3", len(row0))
	}
	for i := 1; i < len(row0); i++ {
		if row0[i] <= row0[i-1] {
			t.Errorf("dot %d X=%.2f not after %.2f — overlap not resolved", i, row0[i], row0[i-1])
		}
	}

	// The single dot in row 1 keeps its exact position (no dodge).
	wantX := dGutter + 0.2*(dW-dGutter-dRight)
	row1Y := v.Rows[1].TextY
	for _, p := range v.Dots {
		if p.Y == row1Y && p.X != wantX {
			t.Errorf("lone dot X = %.3f, want %.3f", p.X, wantX)
		}
	}
}
