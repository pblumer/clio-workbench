package process

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Density is the aggregated overview the Event Space falls back to when there are
// too many subjects or events to draw one dot each (docs/SPACE-LOD.md). Instead
// of dropping the long tail it bins every event into a fixed grid of
// subject-bands × time-columns, so the *whole* population stays visible — just
// coarser. ByTime mirrors the dotted chart's axis choice; Total is the distinct
// subject count (all of them, nothing capped); Max is the busiest cell, the
// reference the colour scale stretches against.
type Density struct {
	Rows   []DensityRow
	Cols   int
	Cells  []DensityCell
	Events int
	ByTime bool
	Total  int
	Max    int
}

// DensityRow is one horizontal band: a contiguous group of subjects. Prefix is
// the band's common subject path-prefix when it has one (so a click can drill in
// via the subject filter); it is empty when the band's subjects share no path
// segment. Subjects/Count report how much the band rolls up.
type DensityRow struct {
	Label    string
	Prefix   string
	Subjects int
	Count    int
}

// DensityCell is one bucket of the grid: how many events fell into a
// (band × time-column) and the lifecycle phase that dominates them. MinID/MaxID
// bound the events lexicographically so a click can drill into exactly this
// slice through the event-id range filter (from:/to:), independent of how Clio
// formats ids. Empty cells are not emitted.
type DensityCell struct {
	Row   int
	Col   int
	Count int
	Phase Phase
	MinID string
	MaxID string
}

// phaseOrder is the tie-break when two phases are equally frequent in a cell:
// the more attention-worthy phase wins, so a cell that is half errors never
// hides them behind an equal count of completes.
var phaseOrder = []Phase{PhaseError, PhaseActive, PhaseInfo, PhaseComplete}

// BuildDensity bins events into a maxRows × cols grid. Subjects are ordered like
// the dotted chart (first event, then name) and, when there are more than
// maxRows, grouped into maxRows equal-sized bands; with maxRows or fewer each
// subject is its own band. The X axis uses real timestamps when all parse and
// span, else sequence order — the same rule as BuildDotted, so the two views
// agree on what "time" means.
func BuildDensity(events []TimedEvent, maxRows, cols int) Density {
	n := len(events)
	if n == 0 {
		return Density{}
	}
	if maxRows < 1 {
		maxRows = 60
	}
	if cols < 1 {
		cols = 1
	}

	val, useTime := axisValues(events)
	min, span := minSpan(val)

	// Per-subject aggregates drive the row order and the banding.
	type agg struct {
		count int
		first float64
	}
	subs := map[string]*agg{}
	for i, e := range events {
		a := subs[e.Subject]
		if a == nil {
			a = &agg{first: val[i]}
			subs[e.Subject] = a
		}
		a.count++
		if val[i] < a.first {
			a.first = val[i]
		}
	}
	names := make([]string, 0, len(subs))
	for s := range subs {
		names = append(names, s)
	}
	sort.Slice(names, func(i, j int) bool {
		if subs[names[i]].first != subs[names[j]].first {
			return subs[names[i]].first < subs[names[j]].first
		}
		return names[i] < names[j]
	})

	total := len(names)
	bands := bandSubjects(names, maxRows)
	rowOf := make(map[string]int, len(names))
	for r, band := range bands {
		for _, s := range band {
			rowOf[s] = r
		}
	}

	d := Density{Cols: cols, Events: n, ByTime: useTime, Total: total}
	d.Rows = make([]DensityRow, len(bands))
	for r, band := range bands {
		cnt := 0
		for _, s := range band {
			cnt += subs[s].count
		}
		d.Rows[r] = DensityRow{
			Label:    bandLabel(band),
			Prefix:   bandPrefix(band),
			Subjects: len(band),
			Count:    cnt,
		}
	}

	// Accumulate into a (row,col) map, then emit non-empty cells in row-major
	// order for a stable, test-friendly result.
	type acc struct {
		count  int
		phases map[Phase]int
		minID  string
		maxID  string
	}
	grid := map[[2]int]*acc{}
	for i, e := range events {
		row := rowOf[e.Subject]
		col := int((val[i] - min) / span * float64(cols))
		if col >= cols {
			col = cols - 1
		}
		if col < 0 {
			col = 0
		}
		key := [2]int{row, col}
		c := grid[key]
		if c == nil {
			c = &acc{phases: map[Phase]int{}, minID: e.ID, maxID: e.ID}
			grid[key] = c
		}
		c.count++
		_, phase := Classify(e.Type)
		c.phases[phase]++
		if e.ID < c.minID {
			c.minID = e.ID
		}
		if e.ID > c.maxID {
			c.maxID = e.ID
		}
	}
	for key, c := range grid {
		if c.count > d.Max {
			d.Max = c.count
		}
		d.Cells = append(d.Cells, DensityCell{
			Row:   key[0],
			Col:   key[1],
			Count: c.count,
			Phase: dominantPhase(c.phases),
			MinID: c.minID,
			MaxID: c.maxID,
		})
	}
	sort.Slice(d.Cells, func(i, j int) bool {
		if d.Cells[i].Row != d.Cells[j].Row {
			return d.Cells[i].Row < d.Cells[j].Row
		}
		return d.Cells[i].Col < d.Cells[j].Col
	})
	return d
}

// axisValues maps each event to its X value and reports whether that axis is
// real time. It mirrors BuildDotted: time only if every timestamp parses and the
// span is non-zero, otherwise plain sequence order.
func axisValues(events []TimedEvent) (val []float64, useTime bool) {
	n := len(events)
	val = make([]float64, n)
	useTime = true
	parsed := make([]time.Time, n)
	for i, e := range events {
		t, err := time.Parse(time.RFC3339, e.Time)
		if err != nil {
			useTime = false
		}
		parsed[i] = t
	}
	if useTime {
		for i := range events {
			val[i] = float64(parsed[i].UnixNano())
		}
		if allEqual(val) { // every event at the same instant: no span to chart
			useTime = false
		}
	}
	if !useTime {
		for i := range events {
			val[i] = float64(i)
		}
	}
	return val, useTime
}

func allEqual(v []float64) bool {
	for _, x := range v {
		if x != v[0] {
			return false
		}
	}
	return true
}

// minSpan returns the minimum value and a non-zero span (max-min, clamped to 1).
func minSpan(v []float64) (min, span float64) {
	min, max := v[0], v[0]
	for _, x := range v {
		if x < min {
			min = x
		}
		if x > max {
			max = x
		}
	}
	span = max - min
	if span == 0 {
		span = 1
	}
	return min, span
}

// bandSubjects groups an ordered subject list into at most maxRows contiguous
// bands of as-equal-as-possible size. With maxRows or fewer subjects every
// subject is its own band.
func bandSubjects(names []string, maxRows int) [][]string {
	if len(names) <= maxRows {
		out := make([][]string, len(names))
		for i, s := range names {
			out[i] = []string{s}
		}
		return out
	}
	out := make([][]string, 0, maxRows)
	nb := maxRows
	base := len(names) / nb
	rem := len(names) % nb
	start := 0
	for r := 0; r < nb; r++ {
		size := base
		if r < rem { // spread the remainder over the first bands
			size++
		}
		out = append(out, names[start:start+size])
		start += size
	}
	return out
}

// bandPrefix returns the band's common subject path-prefix, trimmed to a path
// segment boundary so it round-trips through the segment-aware subject filter
// (matchStage). A bare "/" (no shared segment) yields "".
func bandPrefix(band []string) string {
	if len(band) == 1 {
		return band[0]
	}
	cp := band[0]
	for _, s := range band[1:] {
		cp = commonPrefix(cp, s)
		if cp == "" {
			return ""
		}
	}
	if i := strings.LastIndex(cp, "/"); i > 0 {
		return cp[:i]
	}
	return ""
}

func commonPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}

// bandLabel names a band for the row gutter: the single subject when it stands
// alone, otherwise the subject range and how many it rolls up.
func bandLabel(band []string) string {
	if len(band) == 1 {
		return band[0]
	}
	first, last := band[0], band[len(band)-1]
	return fmt.Sprintf("%s … %s · %d", first, last, len(band))
}

// dominantPhase picks the most frequent phase, breaking ties by phaseOrder so
// the more attention-worthy phase shows through.
func dominantPhase(phases map[Phase]int) Phase {
	best, bestN := PhaseComplete, -1
	for _, p := range phaseOrder {
		if c := phases[p]; c > bestN {
			best, bestN = p, c
		}
	}
	return best
}
