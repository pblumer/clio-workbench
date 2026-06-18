package server

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/http"
	"sort"
	"strconv"

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

type dotPoint struct {
	X, Y    float64
	Phase   string
	Color   string
	ID      string
	Subject string
	Type    string
	Time    string
}

// typeLegendItem is one entry of the by-type colour legend.
type typeLegendItem struct {
	Type  string
	Color string
	Count int
}

type dotRow struct {
	Label string
	BandY float64
	TextY float64
	Count int
}

type gridLine struct{ X float64 }

type dottedView struct {
	State            string // ok, empty, offline, unauthorized, error
	Message          string
	W, H             float64
	PlotL, PlotW     float64
	LabelX           float64
	GridTop, GridBot float64
	AxisX, AxisY     float64
	Rows             []dotRow
	Dots             []dotPoint
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
}

// handleSpace renders the "event space" as a dotted chart: one row per subject,
// X = time (or sequence), each event a dot coloured by lifecycle phase. It
// reveals bursts, gaps, variants and outliers across all subjects at a glance.
func (s *Server) handleSpace(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
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

	// The "frame" keeps only the last N events (the newest tail of the scope),
	// the window the user pans across while live events stream in.
	frame := frameSize(r.URL.Query().Get("frame"))
	truncated := len(events) >= sc.Limit
	framed := false
	if frame > 0 && len(events) > frame {
		events = events[len(events)-frame:]
		framed = true
	}

	in := make([]process.TimedEvent, len(events))
	for i, e := range events {
		in[i] = process.TimedEvent{ID: e.ID, Subject: e.Subject, Type: e.Type, Time: e.Time}
	}
	v := buildDottedView(process.BuildDotted(in, dMaxRows))
	v.Truncated = truncated
	v.Cap = sc.Limit
	v.Frame = frame
	v.Framed = framed
	if n := len(events); n > 0 {
		v.AfterID = events[n-1].ID
	}
	s.render(w, "space.html", v)
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
