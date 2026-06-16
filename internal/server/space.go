package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/process"
)

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
	Subject string
	Type    string
	Time    string
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
}

// handleSpace renders the "event space" as a dotted chart: one row per subject,
// X = time (or sequence), each event a dot coloured by lifecycle phase. It
// reveals bursts, gaps, variants and outliers across all subjects at a glance.
func (s *Server) handleSpace(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()

	sc := s.activeScope()
	events, err := s.clio.ReadScoped(ctx, sc)
	if err != nil {
		v := dottedView{}
		switch {
		case errors.Is(err, clio.ErrOffline):
			v.State, v.Message = "offline", "no Clio connected — pick a server to chart its events"
		case errors.Is(err, clio.ErrUnauthorized):
			v.State, v.Message = "unauthorized", "Clio rejected the token"
		default:
			v.State, v.Message = "error", "could not read events from Clio"
			s.log.Warn("read events (space)", "err", err)
		}
		s.render(w, "space.html", v)
		return
	}

	in := make([]process.TimedEvent, len(events))
	for i, e := range events {
		in[i] = process.TimedEvent{Subject: e.Subject, Type: e.Type, Time: e.Time}
	}
	v := buildDottedView(process.BuildDotted(in, dMaxRows))
	v.Truncated = len(events) >= sc.Limit
	v.Cap = sc.Limit
	s.render(w, "space.html", v)
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

	for _, dot := range d.Dots {
		v.Dots = append(v.Dots, dotPoint{
			X:       dGutter + dot.X*plotW,
			Y:       dTop + (float64(dot.Row)+0.5)*dRowH,
			Phase:   string(dot.Phase),
			Subject: dot.Subject,
			Type:    dot.Type,
			Time:    dot.Time,
		})
	}
	return v
}

// dotR is exposed to the template for the dot radius.
func (dottedView) DotR() string { return fmt.Sprintf("%.1f", dDotR) }
