package process

import (
	"strings"
	"testing"
)

func TestBuildDensityEmpty(t *testing.T) {
	d := BuildDensity(nil, nil, 10)
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
	d := BuildDensity(evs, SubjectBands(evs, 70), 2)

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
	d := BuildDensity(evs, SubjectBands(evs, 70), 1)
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
	d := BuildDensity(evs, SubjectBands(evs, 2), 4)
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
	d := BuildDensity(evs, SubjectBands(evs, 70), 2)
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
	d := BuildDensity(evs, SubjectBands(evs, 70), 3)
	if d.ByTime {
		t.Error("ByTime = true, want false (no span between identical instants)")
	}
	if d.Total != 2 || len(d.Cells) == 0 {
		t.Errorf("expected 2 subjects and some cells, got total=%d cells=%d", d.Total, len(d.Cells))
	}
}

func TestSubjectBandsNameRangeIsExact(t *testing.T) {
	// Subjects arrive out of name order; bands must be name-sorted contiguous
	// slices whose [From,To] selects exactly that band — and nothing else.
	evs := []TimedEvent{
		{ID: "1", Subject: "/e/d", Type: "x", Time: "2026-01-01T09:00:00Z"},
		{ID: "2", Subject: "/e/a", Type: "x", Time: "2026-01-01T09:00:01Z"},
		{ID: "3", Subject: "/e/c", Type: "x", Time: "2026-01-01T09:00:02Z"},
		{ID: "4", Subject: "/e/b", Type: "x", Time: "2026-01-01T09:00:03Z"},
	}
	bands := SubjectBands(evs, 2)
	if len(bands) != 2 {
		t.Fatalf("bands = %d, want 2", len(bands))
	}
	// Name order a,b | c,d → exact ranges.
	if bands[0].From != "/e/a" || bands[0].To != "/e/b" {
		t.Errorf("band 0 range = %q..%q, want /e/a../e/b", bands[0].From, bands[0].To)
	}
	if bands[1].From != "/e/c" || bands[1].To != "/e/d" {
		t.Errorf("band 1 range = %q..%q, want /e/c../e/d", bands[1].From, bands[1].To)
	}
	// Every subject lands in exactly the band whose range contains it.
	for _, b := range bands {
		for _, s := range b.Subjects {
			if s < b.From || s > b.To {
				t.Errorf("subject %q outside its band range [%s,%s]", s, b.From, b.To)
			}
		}
	}
}

func TestVariantBandsGroupBehaviour(t *testing.T) {
	// Two subjects ran created→done, one ran created→failed: two variant bands,
	// the busier one first.
	evs := []TimedEvent{
		{ID: "1", Subject: "/o/1", Type: "created", Time: "2026-01-01T09:00:00Z"},
		{ID: "2", Subject: "/o/1", Type: "done", Time: "2026-01-01T09:00:01Z"},
		{ID: "3", Subject: "/o/2", Type: "created", Time: "2026-01-01T09:00:02Z"},
		{ID: "4", Subject: "/o/2", Type: "done", Time: "2026-01-01T09:00:03Z"},
		{ID: "5", Subject: "/o/3", Type: "created", Time: "2026-01-01T09:00:04Z"},
		{ID: "6", Subject: "/o/3", Type: "failed", Time: "2026-01-01T09:00:05Z"},
	}
	bands := VariantBands(evs, 70)
	if len(bands) != 2 {
		t.Fatalf("variants = %d, want 2", len(bands))
	}
	if len(bands[0].Subjects) != 2 {
		t.Errorf("busiest band has %d subjects, want 2", len(bands[0].Subjects))
	}
	if !strings.Contains(bands[0].Label, "created → done") {
		t.Errorf("band label = %q, want the created → done chain", bands[0].Label)
	}
	if bands[0].Prefix != "/o" {
		t.Errorf("band prefix = %q, want /o", bands[0].Prefix)
	}
	// All three subjects must be represented across the bands — nothing dropped.
	d := BuildDensity(evs, bands, 4)
	if d.Total != 3 {
		t.Errorf("total subjects = %d, want 3", d.Total)
	}
}

func TestVariantBandsMergesTail(t *testing.T) {
	// More distinct variants than rows: the smallest fold into one trailing band.
	evs := []TimedEvent{
		{ID: "1", Subject: "/o/1", Type: "a", Time: "2026-01-01T09:00:00Z"},
		{ID: "2", Subject: "/o/2", Type: "b", Time: "2026-01-01T09:00:01Z"},
		{ID: "3", Subject: "/o/3", Type: "c", Time: "2026-01-01T09:00:02Z"},
	}
	bands := VariantBands(evs, 2)
	if len(bands) != 2 {
		t.Fatalf("bands = %d, want 2 (one head + a merged tail)", len(bands))
	}
	tail := bands[len(bands)-1]
	if !strings.Contains(tail.Label, "more variants") {
		t.Errorf("tail label = %q, want a merged-variants summary", tail.Label)
	}
	got := 0
	for _, b := range bands {
		got += len(b.Subjects)
	}
	if got != 3 {
		t.Errorf("subjects across bands = %d, want all 3", got)
	}
}

func TestVariantLabelAbbreviates(t *testing.T) {
	long := variantLabel([]string{"a", "b", "c", "d", "e"}, 7)
	if !strings.Contains(long, "a → … → e (5)") || !strings.Contains(long, "· 7") {
		t.Errorf("long label = %q, want abbreviated chain with step + subject counts", long)
	}
	if got := variantLabel(nil, 1); !strings.HasPrefix(got, "—") {
		t.Errorf("empty-sequence label = %q, want a dash", got)
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
