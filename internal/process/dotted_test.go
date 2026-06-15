package process

import "testing"

func TestBuildDottedByTime(t *testing.T) {
	evs := []TimedEvent{
		{Subject: "/o/2", Type: "placed", Time: "2026-01-01T10:00:00Z"},
		{Subject: "/o/1", Type: "placed", Time: "2026-01-01T09:00:00Z"},
		{Subject: "/o/1", Type: "shipping.failed", Time: "2026-01-01T11:00:00Z"},
	}
	d := BuildDotted(evs, 60)

	if !d.ByTime {
		t.Fatal("ByTime = false, want true (all timestamps parse and span)")
	}
	if d.Events != 3 || d.Shown != 2 || d.Total != 2 {
		t.Fatalf("counts: events=%d shown=%d total=%d", d.Events, d.Shown, d.Total)
	}
	// /o/1 starts at 09:00, before /o/2 at 10:00 → row 0.
	if d.Rows[0].Subject != "/o/1" {
		t.Errorf("row 0 = %q, want /o/1 (earliest first event)", d.Rows[0].Subject)
	}
	// Earliest event sits at X=0, latest at X=1.
	var minX, maxX = 1.0, 0.0
	for _, dot := range d.Dots {
		if dot.X < minX {
			minX = dot.X
		}
		if dot.X > maxX {
			maxX = dot.X
		}
		if dot.Type == "shipping.failed" && dot.Phase != PhaseError {
			t.Errorf("failed dot phase = %s, want error", dot.Phase)
		}
	}
	if minX != 0 || maxX != 1 {
		t.Errorf("X span = [%v,%v], want [0,1]", minX, maxX)
	}
}

func TestBuildDottedFallsBackToSequence(t *testing.T) {
	evs := []TimedEvent{
		{Subject: "/o/1", Type: "a"},
		{Subject: "/o/1", Type: "b"},
	}
	d := BuildDotted(evs, 60)
	if d.ByTime {
		t.Fatal("ByTime = true, want false (no timestamps)")
	}
	if len(d.Dots) != 2 {
		t.Fatalf("dots = %d, want 2", len(d.Dots))
	}
}

func TestBuildDottedCapsRows(t *testing.T) {
	var evs []TimedEvent
	// 3 subjects, /o/1 busiest.
	evs = append(evs, TimedEvent{Subject: "/o/1", Type: "a"}, TimedEvent{Subject: "/o/1", Type: "b"})
	evs = append(evs, TimedEvent{Subject: "/o/2", Type: "a"})
	evs = append(evs, TimedEvent{Subject: "/o/3", Type: "a"})
	d := BuildDotted(evs, 2)
	if d.Shown != 2 || d.Total != 3 {
		t.Fatalf("shown=%d total=%d, want 2/3", d.Shown, d.Total)
	}
	// The busiest subject must survive the cap.
	found := false
	for _, r := range d.Rows {
		if r.Subject == "/o/1" {
			found = true
		}
	}
	if !found {
		t.Error("busiest subject /o/1 was capped out")
	}
}
