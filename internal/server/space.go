package server

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/process"
)

// defaultFrame is the size of the live window when the user asks for "the last
// N" without giving a number. dMaxRows already caps subjects; this caps events.
const defaultFrame = 1000

// typePalette is the fixed neon-leaning set the dotted chart colours event
// types with. A type is mapped to a slot by a stable hash so the same type
// keeps its colour across the static render and the live stream.
var typePalette = []string{
	"#38e1ff", "#46f0a0", "#ff5a6e", "#c79bff", "#ffd166",
	"#ff9f5a", "#5ad1ff", "#9fe06a", "#ff7ac0", "#7aa2ff",
	"#6ff0d6", "#f0e15a",
}

// typeColor maps an event type to a stable colour from the palette.
func typeColor(t string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(t))
	return typePalette[h.Sum32()%uint32(len(typePalette))]
}

// Dotted-chart layout (server-side; the SVG is then pan/zoomed client-side).
const (
	dMaxRows = 70
	dW       = 940.0
	dRowH    = 20.0
	dTop     = 44.0
	dBottom  = 34.0
	dGutter  = 180.0 // left space for subject labels
	dRight   = 28.0
	dDotR    = 3.4
)

// Level-of-detail switch (docs/SPACE-LOD.md): beyond either budget the Event
// Space draws an aggregated density grid instead of one dot per event, so the
// whole population stays visible rather than being capped to the busiest rows.
const (
	dMaxDots = 6000 // point budget — more charted events than this flips to density
	dCols    = 120  // density grid: number of time columns
)

type dotPoint struct {
	X, Y    float64
	Phase   string
	Color   string
	ID      string
	Subject string
	Type    string
	Time    string
}

// typeLegendItem is one entry of the by-type colour legend. In the Event Space
// the entries double as filter chips: Active marks a type that is currently
// pinned by the space filter, and Toggled is the filter expression you'd get by
// clicking the chip (adding the type if absent, removing it if present).
type typeLegendItem struct {
	Type    string
	Color   string
	Count   int
	Chip    bool // a clickable filter chip (false for the "+N more" summary)
	Active  bool
	Toggled string
}

type dotRow struct {
	Label string
	BandY float64
	TextY float64
	Count int
}

type gridLine struct{ X float64 }

// dcell is one rendered density bucket: a coloured rectangle whose opacity
// encodes how many events fell into it. The data-* carriers let a click drill
// into exactly this slice — Prefix narrows the subject, MinID/MaxID the event-id
// range (from:/to:).
type dcell struct {
	X, Y, W, H float64
	Phase      string // lifecycle class → themed colour
	Opacity    string // intensity (log-scaled), as a fill-opacity string
	Count      int
	Prefix     string
	MinID      string
	MaxID      string
}

type dottedView struct {
	State            string // ok, empty, offline, unauthorized, error
	Message          string
	Mode             string // "dots" (default) or "density"
	Group            string // density row roll-up: "" (subject) or "variant"
	W, H             float64
	PlotL, PlotW     float64
	LabelX           float64
	GridTop, GridBot float64
	AxisX, AxisY     float64
	Rows             []dotRow
	Dots             []dotPoint
	Cells            []dcell // density buckets (Mode == "density")
	Grid             []gridLine
	Axis             string
	Shown            int
	Total            int
	Events           int
	Capped           bool
	Truncated        bool
	Cap              int
	Legend           []typeLegendItem
	Frame            int    // active window size (0 = all)
	Framed           bool   // the read was clipped to the last Frame events
	AfterID          string // newest id shown — the live stream resumes past it
	Query            string // the raw space-filter expression (echoed into the input)
	Filtered         bool   // a space filter is narrowing the charted events
	TypeFilterOn     bool   // an explicit type selection is in force (chips render on/off)
	TypeBulk         bool   // there is at least one togglable chip → show the all/none buttons
	ShowAllQ         string // filter expression the "all" button applies (every type charted)
	ShowNoneQ        string // filter expression the "none" button applies (no type charted)
	AllActive        bool   // no type selection is active — every type already shown
	NoneActive       bool   // the explicit "show none" selection is active
}

// setTypeControls fills in the legend's all/none bulk-toggle state from the
// current filter. The two buttons sit beside the per-type chips and share their
// query-driven mechanics: each carries the filter expression a click applies.
func (v *dottedView) setTypeControls(f spaceFilter, chips []typeLegendItem) {
	for _, c := range chips {
		if c.Chip {
			v.TypeBulk = true
			break
		}
	}
	v.ShowAllQ = f.withAllTypes().String()
	v.ShowNoneQ = f.withNoTypes().String()
	v.AllActive = !f.typeSelectionActive()
	v.NoneActive = f.showsNoTypes()
	v.TypeFilterOn = f.typeSelectionActive()
}

// handleSpace renders the "event space" as a dotted chart: one row per subject,
// X = time (or sequence), each event a dot coloured by lifecycle phase. It
// reveals bursts, gaps, variants and outliers across all subjects at a glance.
func (s *Server) handleSpace(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readTimeout)
	defer cancel()

	sc := s.activeScope()
	events, err := s.scopedEvents(ctx)
	if err != nil {
		v := dottedView{}
		v.State, v.Message = readErrState(err)
		if v.State == "error" {
			s.log.Warn("read events (space)", "err", err)
		}
		s.render(w, "space.html", v)
		return
	}
	truncated := sc.Limit > 0 && len(events) >= sc.Limit

	// Nothing in scope at all: a genuine empty state — there is nothing to
	// filter, so skip the filter chrome entirely.
	if len(events) == 0 {
		s.render(w, "space.html", dottedView{
			State:   "empty",
			Message: "Clio is connected, but there are no events to chart yet.",
		})
		return
	}

	// The in-panel space filter narrows *which* of the scoped events get
	// charted — a transient, view-only refinement that never touches the
	// environment or the query pipeline. The colour legend doubles as a set of
	// clickable type chips, so the chips are built over the *unfiltered* scope:
	// every type stays togglable even after the filter hides its dots.
	filter := parseSpaceFilter(r.URL.Query().Get("q"))
	chips := buildTypeChips(events, filter)
	if !filter.empty() {
		kept := events[:0:0]
		for _, e := range events {
			if filter.match(eventKey{e.Subject, e.Type, e.ID, e.Source}) {
				kept = append(kept, e)
			}
		}
		events = kept
	}

	// The "frame" keeps only the last N events (the newest tail of the scope),
	// the window the user pans across while live events stream in.
	frame := frameSize(r.URL.Query().Get("frame"))
	framed := false
	if frame > 0 && len(events) > frame {
		events = events[len(events)-frame:]
		framed = true
	}

	// The filter matched nothing: keep the filter chrome on screen (so the user
	// can adjust it) but show a note in place of the chart.
	if len(events) == 0 {
		v := dottedView{
			State:     "filtered-empty",
			Cap:       sc.Limit,
			Truncated: truncated,
			Frame:     frame,
			Legend:    chips,
			Query:     filter.String(),
			Filtered:  true,
		}
		v.setTypeControls(filter, chips)
		s.render(w, "space.html", v)
		return
	}

	in := make([]process.TimedEvent, len(events))
	for i, e := range events {
		in[i] = process.TimedEvent{ID: e.ID, Subject: e.Subject, Type: e.Type, Time: e.Time}
	}
	// Pick the level of detail (docs/SPACE-LOD.md): one dot per event while it
	// stays within budget, an aggregated density grid beyond it. ?mode= overrides
	// the automatic choice so the user can force either view.
	mode := r.URL.Query().Get("mode")
	dense := mode == "density" ||
		(mode != "dots" && (distinctSubjects(events) > dMaxRows || len(events) > dMaxDots))
	var v dottedView
	if dense {
		// Two roll-up strategies: contiguous subject bands (default) or grouping
		// subjects by their process variant (docs/SPACE-LOD.md §3).
		group := r.URL.Query().Get("group")
		var bands []process.Band
		if group == "variant" {
			bands = process.VariantBands(in, dMaxRows)
		} else {
			bands = process.SubjectBands(in, dMaxRows)
		}
		v = buildDensityView(process.BuildDensity(in, bands, dCols))
		v.Group = group
	} else {
		v = buildDottedView(process.BuildDotted(in, dMaxRows))
	}
	v.Truncated = truncated
	v.Cap = sc.Limit
	v.Frame = frame
	v.Framed = framed
	if n := len(events); n > 0 {
		v.AfterID = events[n-1].ID
	}
	// Replace the dot-derived legend with the full-scope type chips so every
	// type can be toggled, and echo the active filter back to the UI.
	v.Legend = chips
	v.Query = filter.String()
	v.Filtered = !filter.empty()
	v.setTypeControls(filter, chips)
	s.render(w, "space.html", v)
}

// spaceFilter is the Event Space's discipline lens (docs/SCOPE.md §3.3): the
// view-only, per-panel layer that narrows the charted events on top of the
// global Environment and the shared Queries pipeline, without touching either.
// Its structured dimensions are carried by the shared queryStage primitive
// (subject prefix, exact types, id bounds, source substring) so it composes
// through the same seam as every other layer; on top it adds free-text needles
// that match an event's type or subject by case-insensitive substring — the
// playful escape hatch a plain queryStage can't express. The same filter can be
// clicked together from the type chips or typed by hand (e.g.
// `type:order.created subject:/orders orders`).
type spaceFilter struct {
	lens    queryStage
	needles []string // lower-cased substrings; an event must contain them all
	noTypes bool     // an explicit "show no types" selection (empty whitelist on purpose)
}

// noTypeToken is the value of a `type:` token that means "no type is allowed" —
// the serialised form of an explicit empty selection ("show none"). A bare dash
// can never be a real Clio event type, so it round-trips without collisions.
const noTypeToken = "-"

// parseSpaceFilter reads a space-separated filter expression. Recognised keys
// are subject/type(s)/from/to/source (with a few aliases); every other token is
// a free-text needle.
func parseSpaceFilter(raw string) spaceFilter {
	var f spaceFilter
	for _, tok := range strings.Fields(raw) {
		key, val, ok := strings.Cut(tok, ":")
		if !ok {
			f.needles = append(f.needles, strings.ToLower(tok))
			continue
		}
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		switch strings.ToLower(key) {
		case "subject", "subj", "s":
			f.lens.Subject = val
		case "type", "types", "t":
			for _, ty := range splitTypes(val) {
				if ty == noTypeToken {
					f.noTypes = true
					continue
				}
				f.lens.Types = append(f.lens.Types, ty)
			}
		case "from", "after", "lower", "min":
			f.lens.LowerBound = val
		case "to", "before", "upper", "max":
			f.lens.UpperBound = val
		case "source", "src":
			f.lens.Source = val
		default:
			f.needles = append(f.needles, strings.ToLower(tok))
		}
	}
	return f
}

// empty reports whether the filter carries no constraint (a no-op).
func (f spaceFilter) empty() bool {
	return f.lens.empty() && len(f.needles) == 0 && !f.noTypes
}

// showsNoTypes reports whether the filter is an explicit "show none" selection:
// the empty whitelist was chosen on purpose, not left blank. A pinned type wins,
// so a contradictory `type:- type:x` still shows x.
func (f spaceFilter) showsNoTypes() bool {
	return f.noTypes && len(f.lens.Types) == 0
}

// typeSelectionActive reports whether any explicit type selection is in force —
// either a non-empty whitelist or the deliberate "show none". When true, chips
// outside the selection render dimmed ("off").
func (f spaceFilter) typeSelectionActive() bool {
	return len(f.lens.Types) > 0 || f.noTypes
}

// hasType reports whether t is currently pinned by the filter.
func (f spaceFilter) hasType(t string) bool {
	for _, x := range f.lens.Types {
		if x == t {
			return true
		}
	}
	return false
}

// matchNeedles reports whether the free-text needles all hit the event's type
// or subject. The structured dimensions are matched via the shared matchStage.
func (f spaceFilter) matchNeedles(subject, typ string) bool {
	if len(f.needles) == 0 {
		return true
	}
	ls, lt := strings.ToLower(subject), strings.ToLower(typ)
	for _, n := range f.needles {
		if !strings.Contains(lt, n) && !strings.Contains(ls, n) {
			return false
		}
	}
	return true
}

// match reports whether an event survives the whole filter: the structured
// lens (via the shared matcher) and the free-text needles. An explicit "show
// none" selection rejects every event.
func (f spaceFilter) match(k eventKey) bool {
	if f.showsNoTypes() {
		return false
	}
	return matchStage(k, f.lens) && f.matchNeedles(k.Subject, k.Type)
}

// withTypeToggled returns a copy of the filter with type t flipped in or out of
// the pinned-type set — the effect of clicking a type chip. Pinning a concrete
// type leaves the "show none" mode behind.
func (f spaceFilter) withTypeToggled(t string) spaceFilter {
	nf := f
	nf.noTypes = false
	types := make([]string, 0, len(f.lens.Types)+1)
	found := false
	for _, x := range f.lens.Types {
		if x == t {
			found = true
			continue
		}
		types = append(types, x)
	}
	if !found {
		types = append(types, t)
	}
	nf.lens.Types = types
	return nf
}

// withAllTypes returns a copy with the whole type selection dropped, so every
// type is charted again — the effect of the legend's "all" button. Other filter
// dimensions (subject, needles, bounds, source) are left untouched.
func (f spaceFilter) withAllTypes() spaceFilter {
	nf := f
	nf.lens.Types = nil
	nf.noTypes = false
	return nf
}

// withNoTypes returns a copy whose type selection is the explicit empty set, so
// nothing is charted — the effect of the legend's "none" button, the blank
// slate to then click types back on one by one.
func (f spaceFilter) withNoTypes() spaceFilter {
	nf := f
	nf.lens.Types = nil
	nf.noTypes = true
	return nf
}

// String renders the filter back to its canonical expression, so a parsed
// filter round-trips and the input always shows a normalised form.
func (f spaceFilter) String() string {
	var parts []string
	if f.lens.Subject != "" {
		parts = append(parts, "subject:"+f.lens.Subject)
	}
	if f.showsNoTypes() {
		parts = append(parts, "type:"+noTypeToken)
	}
	for _, t := range f.lens.Types {
		parts = append(parts, "type:"+t)
	}
	if f.lens.LowerBound != "" {
		parts = append(parts, "from:"+f.lens.LowerBound)
	}
	if f.lens.UpperBound != "" {
		parts = append(parts, "to:"+f.lens.UpperBound)
	}
	if f.lens.Source != "" {
		parts = append(parts, "source:"+f.lens.Source)
	}
	parts = append(parts, f.needles...)
	return strings.Join(parts, " ")
}

// buildTypeChips lists the distinct event types of the (unfiltered) scope as
// filter chips: colour + count, busiest first, each marked Active when pinned
// and carrying the filter expression a click would produce. Beyond legendCap
// types it collapses the tail into a non-clickable "+N more" summary.
func buildTypeChips(events []clio.Event, f spaceFilter) []typeLegendItem {
	counts := make(map[string]int, 16)
	for _, e := range events {
		counts[e.Type]++
	}
	items := make([]typeLegendItem, 0, len(counts))
	for t, c := range counts {
		items = append(items, typeLegendItem{Type: t, Color: typeColor(t), Count: c})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Type < items[j].Type
	})
	if len(items) > legendCap {
		rest := 0
		for _, it := range items[legendCap:] {
			rest += it.Count
		}
		extra := len(counts) - legendCap
		items = items[:legendCap]
		items = append(items, typeLegendItem{Type: fmt.Sprintf("+%d more types", extra), Count: rest})
	}
	for i := range items {
		it := &items[i]
		if it.Color == "" { // the "+N more" summary is not a togglable chip
			continue
		}
		it.Chip = true
		it.Active = f.hasType(it.Type)
		it.Toggled = f.withTypeToggled(it.Type).String()
	}
	return items
}

// frameSize parses the frame query param. "all"/"0"/"" means no window; a bare
// number or a "-1000"/"last" style hint means the last N events.
func frameSize(raw string) int {
	switch raw {
	case "", "all", "0":
		return 0
	case "live", "last":
		return defaultFrame
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	if n < 0 {
		n = -n // "-1000" reads as "the last 1000"
	}
	return n
}

func buildDottedView(d process.Dotted) dottedView {
	if len(d.Rows) == 0 {
		return dottedView{State: "empty", Message: "Clio is connected, but there are no events to chart yet."}
	}

	v := dottedView{
		State:  "ok",
		W:      dW,
		PlotL:  dGutter,
		Shown:  d.Shown,
		Total:  d.Total,
		Events: d.Events,
		Capped: d.Total > d.Shown,
	}
	v.Axis = "sequence →"
	if d.ByTime {
		v.Axis = "time →"
	}
	v.H = dTop + float64(len(d.Rows))*dRowH + dBottom
	plotW := dW - dGutter - dRight
	v.PlotW = plotW
	v.LabelX = dGutter - 10
	v.GridTop = dTop
	v.GridBot = v.H - dBottom
	v.AxisX = dW - dRight
	v.AxisY = v.H - 12

	for i, row := range d.Rows {
		label := row.Subject
		if len(label) > 26 {
			label = "…" + label[len(label)-25:]
		}
		v.Rows = append(v.Rows, dotRow{
			Label: label,
			BandY: dTop + float64(i)*dRowH,
			TextY: dTop + (float64(i)+0.5)*dRowH,
			Count: row.Count,
		})
	}

	for _, frac := range []float64{0, 0.25, 0.5, 0.75, 1} {
		v.Grid = append(v.Grid, gridLine{X: dGutter + frac*plotW})
	}

	// A subject's lifecycle events often arrive in a tight time burst, so
	// without help they land on the same pixel and only the last-drawn dot
	// shows. Within each row, fan out (dodge) runs of dots closer than a
	// dot-width, centred on the run's true mean time so each event stays
	// visible without drifting from where it happened.
	const dMinGap = 2*dDotR + 1.5
	loX, hiX := dGutter, dGutter+plotW
	byRow := make(map[int][]process.Dot, len(d.Rows))
	for _, dot := range d.Dots {
		byRow[dot.Row] = append(byRow[dot.Row], dot)
	}
	for row := 0; row < len(d.Rows); row++ {
		grp := byRow[row]
		sort.SliceStable(grp, func(i, j int) bool { return grp[i].X < grp[j].X })
		y := dTop + (float64(row)+0.5)*dRowH
		xs := make([]float64, len(grp))
		for i, dot := range grp {
			xs[i] = dGutter + dot.X*plotW
		}
		// Walk runs of overlapping dots and spread each, centred on its mean.
		for i := 0; i < len(grp); {
			j, sum := i+1, xs[i]
			for j < len(grp) && xs[j] < xs[j-1]+dMinGap {
				sum += xs[j]
				j++
			}
			if n := j - i; n > 1 {
				width := float64(n-1) * dMinGap
				start := sum/float64(n) - width/2
				if start < loX {
					start = loX
				}
				if start+width > hiX {
					start = hiX - width
				}
				for k := i; k < j; k++ {
					xs[k] = start + float64(k-i)*dMinGap
				}
			}
			i = j
		}
		for i, dot := range grp {
			v.Dots = append(v.Dots, dotPoint{
				X:       xs[i],
				Y:       y,
				Phase:   string(dot.Phase),
				Color:   typeColor(dot.Type),
				ID:      dot.ID,
				Subject: dot.Subject,
				Type:    dot.Type,
				Time:    dot.Time,
			})
		}
	}
	v.Legend = buildTypeLegend(d.Dots)
	return v
}

// buildDensityView lays a process.Density out as an SVG heat-grid: one row band
// per subject group, one column per time bucket, each cell a rectangle whose
// opacity is the log-scaled event count. It reuses the dotted chart's frame so
// the two levels of detail share gutters, labels and axis (docs/SPACE-LOD.md).
func buildDensityView(d process.Density) dottedView {
	if len(d.Rows) == 0 {
		return dottedView{State: "empty", Message: "Clio is connected, but there are no events to chart yet."}
	}
	v := dottedView{
		State:  "ok",
		Mode:   "density",
		W:      dW,
		PlotL:  dGutter,
		Shown:  len(d.Rows),
		Total:  d.Total,
		Events: d.Events,
	}
	v.Axis = "sequence →"
	if d.ByTime {
		v.Axis = "time →"
	}
	v.H = dTop + float64(len(d.Rows))*dRowH + dBottom
	plotW := dW - dGutter - dRight
	v.PlotW = plotW
	v.LabelX = dGutter - 10
	v.GridTop = dTop
	v.GridBot = v.H - dBottom
	v.AxisX = dW - dRight
	v.AxisY = v.H - 12

	for i, row := range d.Rows {
		label := row.Label
		if len(label) > 26 {
			label = "…" + label[len(label)-25:]
		}
		v.Rows = append(v.Rows, dotRow{
			Label: label,
			BandY: dTop + float64(i)*dRowH,
			TextY: dTop + (float64(i)+0.5)*dRowH,
			Count: row.Count,
		})
	}
	for _, frac := range []float64{0, 0.25, 0.5, 0.75, 1} {
		v.Grid = append(v.Grid, gridLine{X: dGutter + frac*plotW})
	}

	cellW := plotW / float64(d.Cols)
	denom := math.Log1p(float64(d.Max))
	for _, c := range d.Cells {
		// Log-scale the count to a [floor,1] opacity so the long tail of quiet
		// cells stays visible next to a few busy ones (docs/SPACE-LOD.md §3).
		t := 1.0
		if denom > 0 {
			t = math.Log1p(float64(c.Count)) / denom
		}
		v.Cells = append(v.Cells, dcell{
			X:       dGutter + float64(c.Col)*cellW,
			Y:       dTop + float64(c.Row)*dRowH + 1,
			W:       cellW,
			H:       dRowH - 2,
			Phase:   string(c.Phase),
			Opacity: fmt.Sprintf("%.3f", 0.14+0.86*t),
			Count:   c.Count,
			Prefix:  d.Rows[c.Row].Prefix,
			MinID:   c.MinID,
			MaxID:   c.MaxID,
		})
	}
	return v
}

// legendCap bounds how many distinct types the legend lists before collapsing
// the rest into a single "+N more" hint.
const legendCap = 14

// buildTypeLegend lists the distinct event types with their colour and count,
// busiest first, so the chart's colours are readable at a glance.
func buildTypeLegend(dots []process.Dot) []typeLegendItem {
	counts := make(map[string]int, 16)
	for _, d := range dots {
		counts[d.Type]++
	}
	items := make([]typeLegendItem, 0, len(counts))
	for t, c := range counts {
		items = append(items, typeLegendItem{Type: t, Color: typeColor(t), Count: c})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Type < items[j].Type
	})
	if len(items) > legendCap {
		rest := 0
		for _, it := range items[legendCap:] {
			rest += it.Count
		}
		items = items[:legendCap]
		items = append(items, typeLegendItem{Type: fmt.Sprintf("+%d more types", len(counts)-legendCap), Count: rest})
	}
	return items
}

// dotR is exposed to the template for the dot radius.
func (dottedView) DotR() string { return fmt.Sprintf("%.1f", dDotR) }
