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

// DensityRow is one horizontal band: a group of subjects. Prefix is the band's
// common subject path-prefix when it has one (so a click can drill in via the
// subject filter); it is empty when the band's subjects share no path segment.
// Subjects/Count report how much the band rolls up.
type DensityRow struct {
	Label    string
	Prefix   string
	From     string // inclusive subject range bounds (set for name-contiguous
	To       string // bands) so a click can drill to exactly this band's subjects
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

// Band is one row of the density grid: a group of subjects shown together. Label
// names the row; Prefix is the subjects' common path segment when they share one
// (so a click can drill by subject), otherwise empty. Bands come from a strategy
// (SubjectBands or VariantBands) so the caller decides how subjects roll up.
type Band struct {
	Subjects []string
	Label    string
	Prefix   string
	From     string // inclusive subject range [From,To] when the band is a
	To       string // contiguous slice of the name-sorted subjects, else empty
}

// phaseOrder is the tie-break when two phases are equally frequent in a cell:
// the more attention-worthy phase wins, so a cell that is half errors never
// hides them behind an equal count of completes.
var phaseOrder = []Phase{PhaseError, PhaseActive, PhaseInfo, PhaseComplete}

// BuildDensity bins events into a len(bands) × cols grid: each band is a row, the
// X axis a time (or sequence) bucket. The axis rule matches BuildDotted, so both
// levels of detail agree on what "time" means. Subjects not covered by a band
// are skipped.
func BuildDensity(events []TimedEvent, bands []Band, cols int) Density {
	n := len(events)
	if n == 0 || len(bands) == 0 {
		return Density{}
	}
	if cols < 1 {
		cols = 1
	}

	val, useTime := axisValues(events)
	min, span := minSpan(val)

	rowOf := make(map[string]int)
	total := 0
	for r, band := range bands {
		for _, s := range band.Subjects {
			rowOf[s] = r
			total++
		}
	}

	d := Density{Cols: cols, Events: n, ByTime: useTime, Total: total}
	d.Rows = make([]DensityRow, len(bands))
	for r, band := range bands {
		d.Rows[r] = DensityRow{
			Label:    band.Label,
			Prefix:   band.Prefix,
			From:     band.From,
			To:       band.To,
			Subjects: len(band.Subjects),
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
		row, ok := rowOf[e.Subject]
		if !ok {
			continue
		}
		d.Rows[row].Count++
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

// SubjectBands groups subjects into at most maxRows contiguous bands ordered by
// subject name. With maxRows or fewer subjects each subject is its own band.
// Because the bands are slices of the name-sorted subjects, each one is an exact
// lexicographic [From,To] range — the handle a click drills through even in flat
// namespaces where no path prefix is selective (docs/SPACE-LOD.md §6).
func SubjectBands(events []TimedEvent, maxRows int) []Band {
	if maxRows < 1 {
		maxRows = 60
	}
	seen := map[string]bool{}
	names := make([]string, 0, len(events))
	for _, e := range events {
		if !seen[e.Subject] {
			seen[e.Subject] = true
			names = append(names, e.Subject)
		}
	}
	sort.Strings(names)
	groups := chunk(names, maxRows)
	bands := make([]Band, len(groups))
	for i, g := range groups {
		bands[i] = Band{
			Subjects: g,
			Label:    bandLabel(g),
			Prefix:   bandPrefix(g),
			From:     g[0],
			To:       g[len(g)-1],
		}
	}
	return bands
}

// VariantBands groups subjects by their behavioural signature — the sequence of
// event types they received — so subjects that ran the same way share a row.
// Bands are ordered by size (the most common variant first). When there are more
// distinct variants than maxRows, the smallest are merged into one trailing
// "more variants" band so nothing is dropped (docs/SPACE-LOD.md §3).
func VariantBands(events []TimedEvent, maxRows int) []Band {
	if maxRows < 1 {
		maxRows = 60
	}
	// Per-subject type sequence, in encounter order.
	order := make([]string, 0)
	seqs := map[string][]string{}
	for _, e := range events {
		if _, ok := seqs[e.Subject]; !ok {
			order = append(order, e.Subject)
		}
		seqs[e.Subject] = append(seqs[e.Subject], e.Type)
	}
	// Group subjects by signature, keeping a representative sequence per group.
	members := map[string][]string{}
	seqOf := map[string][]string{}
	for _, s := range order {
		key := strings.Join(seqs[s], "\x00")
		if _, ok := seqOf[key]; !ok {
			seqOf[key] = seqs[s]
		}
		members[key] = append(members[key], s)
	}
	keys := make([]string, 0, len(members))
	for k := range members {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(members[keys[i]]) != len(members[keys[j]]) {
			return len(members[keys[i]]) > len(members[keys[j]])
		}
		return keys[i] < keys[j]
	})

	// Below the cap every variant is its own band; above it, keep the busiest
	// maxRows-1 and fold the rest into a single trailing band.
	head := keys
	var tail []string
	if len(keys) > maxRows {
		head = keys[:maxRows-1]
		tail = keys[maxRows-1:]
	}
	bands := make([]Band, 0, len(head)+1)
	for _, k := range head {
		bands = append(bands, variantBand(members[k], seqOf[k]))
	}
	if len(tail) > 0 {
		var rest []string
		for _, k := range tail {
			rest = append(rest, members[k]...)
		}
		bands = append(bands, Band{
			Subjects: rest,
			Label:    fmt.Sprintf("+%d more variants · %d", len(tail), len(rest)),
			Prefix:   bandPrefix(rest),
		})
	}
	return bands
}

func variantBand(subjects, seq []string) Band {
	return Band{Subjects: subjects, Label: variantLabel(seq, len(subjects)), Prefix: bandPrefix(subjects)}
}

// variantLabel renders a trace signature compactly: the type chain, abbreviated
// when long, plus how many subjects share it.
func variantLabel(seq []string, n int) string {
	var chain string
	switch {
	case len(seq) == 0:
		chain = "—"
	case len(seq) <= 3:
		chain = strings.Join(seq, " → ")
	default:
		chain = fmt.Sprintf("%s → … → %s (%d)", seq[0], seq[len(seq)-1], len(seq))
	}
	return fmt.Sprintf("%s · %d", chain, n)
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

// chunk splits an ordered list into at most maxRows contiguous near-equal parts.
// With maxRows or fewer items each item is its own part.
func chunk(names []string, maxRows int) [][]string {
	if len(names) <= maxRows {
		out := make([][]string, len(names))
		for i, s := range names {
			out[i] = []string{s}
		}
		return out
	}
	out := make([][]string, 0, maxRows)
	base := len(names) / maxRows
	rem := len(names) % maxRows
	start := 0
	for r := 0; r < maxRows; r++ {
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

// bandLabel names a subject band for the row gutter: the single subject when it
// stands alone, otherwise the subject range and how many it rolls up.
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
