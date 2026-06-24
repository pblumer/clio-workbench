package process

import "testing"

func TestBuildDensityEmpty(t *testing.T) {
	d := BuildDensity(nil, 70, 10)
	if d.Events != 0 || len(d.Cells) != 0 || len(d.Rows) != 0 {
		t.Fatalf("empty input must yield empty density, got %+v", d)
	}
}

func TestBuildDensityByTime(t *testing.T) {
	evs := []TimedEvent{
		{ID: "a", Subject: "/o/1", Type: "placed", Time: "2026-01-01T09:00:00Z"},
		{ID: "b", Subject: "/o/2", Type: "placed", Time: "2026-01-01T10:00:00Z"},
		{ID: "c", Subject: "/o/1", Type: "shipping.failed", Time: "2026-01-01T11:00:00Z"},
	}
	d := BuildDensity(evs, 70, 2)

	if !d.ByTime {
		t.Fatal("ByTime = false, want true (all timestamps parse and span)")
	}
	if d.Events != 3 || d.Total != 2 {
		t.Fatalf("counts: events=%d total=%d", d.Events, d.Total)
	}
	// Few subjects: each is its own band, ordered by first event (/o/1 first).
	if len(d.Rows) != 2 || d.Rows[0].Prefix != "/o/1" || d.Rows[1].Prefix != "/o/2" {
		t.Fatalf("rows = %+v, want one band per subject ordered by first event", d.Rows)
	}
	// Earliest event (09:00) lands in column 0, the 11:00 one in the last column.
	var lo, hi = d.Cols, -1
	for _, c := range d.Cells {
		if c.Col < lo {
			lo = c.Col
		}
		if c.Col > hi {
			hi = c.Col
		}
	}
	if lo != 0 || hi != d.Cols-1 {
		t.Errorf("column span = [%d,%d], want [0,%d]", lo, hi, d.Cols-1)
	}
}

func TestBuildDensityDominantPhaseAndDrillBounds(t *testing.T) {
	// One subject, one column: two errors beat one complete, and the cell's id
	// bounds span the whole burst regardless of input order.
	evs := []TimedEvent{
		{ID: "m", Subject: "/o/1", Type: "step.completed", Time: "2026-01-01T09:00:00Z"},
		{ID: "z", Subject: "/o/1", Type: "step.failed", Time: "2026-01-01T09:00:01Z"},
		{ID: "a", Subject: "/o/1", Type: "other.failed", Time: "2026-01-01T09:00:02Z"},
	}
	d := BuildDensity(evs, 70, 1)
	if len(d.Cells) != 1 {
		t.Fatalf("want one cell, got %d", len(d.Cells))
	}
	c := d.Cells[0]
	if c.Count != 3 || c.Phase != PhaseError {
		t.Errorf("cell = count %d phase %s, want 3 error", c.Count, c.Phase)
	}
	if c.MinID != "a" || c.MaxID != "z" {
		t.Errorf("drill bounds = [%s,%s], want [a,z] (lexicographic min/max)", c.MinID, c.MaxID)
	}
	if d.Max != 3 {
		t.Errorf("Max = %d, want 3 (busiest cell)", d.Max)
	}
}

func TestBuildDensityBandsAndPrefix(t *testing.T) {
	// More subjects than rows: they roll up into bands. Subjects sharing a path
	// segment expose it as the band Prefix for subject drill-down.
	evs := []TimedEvent{
		{ID: "1", Subject: "/team/a", Type: "x", Time: "2026-01-01T09:00:00Z"},
		{ID: "2", Subject: "/team/b", Type: "x", Time: "2026-01-01T09:00:01Z"},
		{ID: "3", Subject: "/team/c", Type: "x", Time: "2026-01-01T09:00:02Z"},
		{ID: "4", Subject: "/team/d", Type: "x", Time: "2026-01-01T09:00:03Z"},
	}
	d := BuildDensity(evs, 2, 4)
	if len(d.Rows) != 2 {
		t.Fatalf("want 2 bands, got %d", len(d.Rows))
	}
	tot := 0
	for _, r := range d.Rows {
		if r.Subjects != 2 {
			t.Errorf("band %q rolls up %d subjects, want 2", r.Label, r.Subjects)
		}
		if r.Prefix != "/team" {
			t.Errorf("band prefix = %q, want /team", r.Prefix)
		}
		tot += r.Subjects
	}
	if tot != 4 || d.Total != 4 {
		t.Errorf("subjects represented = %d / total %d, want all 4", tot, d.Total)
	}
}

func TestBuildDensitySequenceFallback(t *testing.T) {
	// Unparseable timestamps fall back to sequence order, like BuildDotted.
	evs := []TimedEvent{
		{ID: "1", Subject: "/o/1", Type: "x", Time: "nope"},
		{ID: "2", Subject: "/o/1", Type: "x", Time: ""},
	}
	d := BuildDensity(evs, 70, 2)
	if d.ByTime {
		t.Error("ByTime = true, want false (timestamps do not parse)")
	}
}

func TestBuildDensityAllSameInstant(t *testing.T) {
	// Every event at the same parseable instant has no span to chart, so the axis
	// falls back to sequence order even though the timestamps are valid.
	evs := []TimedEvent{
		{ID: "1", Subject: "/o/1", Type: "x", Time: "2026-01-01T09:00:00Z"},
		{ID: "2", Subject: "/o/2", Type: "x", Time: "2026-01-01T09:00:00Z"},
	}
	d := BuildDensity(evs, 70, 3)
	if d.ByTime {
		t.Error("ByTime = true, want false (no span between identical instants)")
	}
	if d.Total != 2 || len(d.Cells) == 0 {
		t.Errorf("expected 2 subjects and some cells, got total=%d cells=%d", d.Total, len(d.Cells))
	}
}

func TestBandPrefixNoCommonSegment(t *testing.T) {
	// Subjects under different top segments share no path segment → no prefix.
	if got := bandPrefix([]string{"/a/1", "/b/2"}); got != "" {
		t.Errorf("bandPrefix = %q, want empty (no shared segment)", got)
	}
	// A single-subject band is its own prefix.
	if got := bandPrefix([]string{"/a/1"}); got != "/a/1" {
		t.Errorf("bandPrefix = %q, want /a/1", got)
	}
}
