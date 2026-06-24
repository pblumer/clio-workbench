package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/store"
)

// modeler.go renders the BPMN-style canvas editor: the same draft Steps the
// outline editor authors, laid out left-to-right as a sequence of BPMN shapes
// (start/catch/end events and send tasks) with sequence-flow connectors. It is
// a shell editor tab (docs/FRAMEWORK.md) — the "Modeler" View. The shapes mirror
// the bpmngen mapping one-to-one, so what the canvas draws is exactly what the
// BPMN export produces.
//
// The layout is *derived* from the ordered Steps; nothing new is persisted, so
// the schema/BPMN/producer generators stay untouched (the hybrid Stufe-1 plan).
// Every structural edit re-renders the whole #modeler-slot via the shared step
// CRUD handlers, which dispatch to the modeler fragment when the request carries
// view=modeler (renderAfterEdit). Selection is server state too: a click reloads
// the canvas with ?sel=<stepId>, so the highlighted shape and the properties
// panel never drift apart.

// Canvas geometry (BPMN-ish proportions, space-look spacing). Kept in one place
// so the Go layout and the SVG template agree.
const (
	mdlPadX    = 56.0  // pool inner padding, left/right of the chain
	mdlLaneHdr = 28.0  // width of the vertical lane header band
	mdlLaneH   = 168.0 // pool/lane height
	mdlGap     = 56.0  // minimum horizontal gap between shape bodies
	mdlEventR  = 19.0  // event circle radius
	mdlEndR    = 21.0  // end event circle radius (drawn bolder)
	mdlMarkerR = 16.0  // start/end pseudo marker radius
	mdlTaskW   = 116.0 // send-task width
	mdlTaskH   = 76.0  // send-task height
	mdlPadY    = 28.0  // pool padding above/below the lane

	// Event labels sit *under* the orb, centred, and are often wider than the
	// orb itself ("order-delivered" is ~5× the circle). The geometric gap alone
	// therefore lets long names collide, so the layout also reserves room from
	// the estimated label width (html/template has no text metrics). mdlCharW is
	// a deliberately generous per-glyph advance at the 11px label font.
	mdlCharW    = 6.4  // estimated glyph advance at the label font-size
	mdlLabelGap = 16.0 // minimum horizontal gap between adjacent labels
)

// mdlShape is one laid-out BPMN element on the canvas. Derived coordinates are
// precomputed here rather than in the template (html/template has no float
// arithmetic), so the SVG stays declarative.
type mdlShape struct {
	StepID   string  // "" for the synthetic start/end markers
	Kind     string  // start, end, catch, task, marker-start, marker-end
	Phase    string  // lifecycle phase (events); drives the orb colour
	Label    string  // the event-type / command name shown under the shape
	CX, CY   float64 // centre
	R        float64 // radius (events/markers)
	X, Y     float64 // top-left (tasks)
	W, H     float64 // size (tasks)
	HaloR    float64 // selection-halo radius (events)
	InnerR   float64 // inner ring radius (catch events)
	LabelY   float64 // baseline of the label under an event
	IconX    float64 // send-task icon anchor
	IconY    float64
	Selected bool
}

// mdlFlow is a sequence-flow connector between two shapes.
type mdlFlow struct{ D string }

// modelerData is the view model for modeler.html.
type modelerData struct {
	Draft     *model.Draft
	LaneLabel string // the pool/lane caption (subject collection or process name)
	Shapes    []mdlShape
	Flows     []mdlFlow
	Sel       string      // selected step id ("" = nothing selected)
	Selected  *model.Step // the selected step, for the properties panel
	W, H      float64     // canvas content bounds
	HalfH     float64     // vertical centre, for the lane label
	Empty     bool        // the draft has no steps yet
}

// handleModeler renders the modeler fragment for a draft (or an empty prompt
// when no draft is chosen). It is the entry the "edit" action and selection
// clicks both hit: GET /modeler?draft=<id>&sel=<stepId>.
func (s *Server) handleModeler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("draft"))
	if id == "" {
		s.render(w, "modeler-empty", nil)
		return
	}
	d, err := s.store.Get(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, "get draft", err)
		return
	}
	s.renderModeler(w, d, strings.TrimSpace(r.URL.Query().Get("sel")))
}

// renderModeler builds the layout and renders the modeler body.
func (s *Server) renderModeler(w http.ResponseWriter, d *model.Draft, sel string) {
	s.render(w, "modeler.html", buildModeler(d, sel))
}

// renderAfterEdit is the shared render tail of the step/field CRUD handlers. The
// outline editor (default) gets the procsteps fragment; the modeler gets its
// canvas. selOverride wins over the request's sel (used when adding a step, so
// the fresh shape comes back selected).
func (s *Server) renderAfterEdit(w http.ResponseWriter, r *http.Request, d *model.Draft, selOverride string) {
	if r.FormValue("view") == "modeler" {
		sel := selOverride
		if sel == "" {
			sel = strings.TrimSpace(r.FormValue("sel"))
		}
		s.renderModeler(w, d, sel)
		return
	}
	s.renderSteps(w, d)
}

// handleReorderStep moves a step to an absolute index in the outline. It backs
// the canvas drag-to-reorder gesture (POST .../reorder?to=N), and falls through
// to the shared render tail like the other step handlers.
func (s *Server) handleReorderStep(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDraft(w, r)
	if !ok {
		return
	}
	id := r.PathValue("stepId")
	to, _ := strconv.Atoi(r.FormValue("to"))
	from := -1
	for i := range d.Steps {
		if d.Steps[i].ID == id {
			from = i
			break
		}
	}
	if from >= 0 {
		if to < 0 {
			to = 0
		}
		if to >= len(d.Steps) {
			to = len(d.Steps) - 1
		}
		st := d.Steps[from]
		d.Steps = append(d.Steps[:from], d.Steps[from+1:]...)
		d.Steps = append(d.Steps, model.Step{})
		copy(d.Steps[to+1:], d.Steps[to:])
		d.Steps[to] = st
	}
	if !s.saveDraft(w, d) {
		return
	}
	s.renderAfterEdit(w, r, d, id)
}

// buildModeler lays the ordered Steps out as a horizontal BPMN chain. The
// event/task → start/catch/end/task mapping mirrors bpmngen exactly, so the
// canvas and the exported .bpmn always agree.
func buildModeler(d *model.Draft, sel string) modelerData {
	md := modelerData{Draft: d, LaneLabel: laneLabel(d), Sel: sel}
	md.Selected = stepByID(d, sel)

	firstEv, lastEv := -1, -1
	for i, st := range d.Steps {
		if st.Kind == model.StepEvent {
			if firstEv < 0 {
				firstEv = i
			}
			lastEv = i
		}
	}

	midY := mdlPadY + mdlLaneH/2

	// Shapes are placed left-to-right. Each centre sits far enough from the
	// previous one to clear *both* the orb bodies (mdlGap) and the labels
	// (mdlLabelGap) — the larger of the two pitches wins, so long event names
	// push their neighbours apart instead of overlapping. labelHalf is the
	// outward reach of a shape's own label (0 for markers and tasks, whose
	// captions live inside the body).
	var shapes []mdlShape
	var prevCX, prevHalf, prevLabelHalf float64
	placed := false
	place := func(sh mdlShape, halfW, labelHalf float64) mdlShape {
		if !placed {
			sh.CX = mdlPadX + mdlLaneHdr + halfW
			placed = true
		} else {
			pitch := prevHalf + mdlGap + halfW
			if lbl := prevLabelHalf + mdlLabelGap + labelHalf; lbl > pitch {
				pitch = lbl
			}
			sh.CX = prevCX + pitch
		}
		sh.CY = midY
		prevCX, prevHalf, prevLabelHalf = sh.CX, halfW, labelHalf
		return sh
	}

	// Leading start marker (the "trigger" before the first real event).
	shapes = append(shapes, place(mdlShape{Kind: "marker-start", R: mdlMarkerR}, mdlMarkerR, 0))

	for i, st := range d.Steps {
		sh := mdlShape{StepID: st.ID, Phase: st.Phase, Label: st.Name, Selected: st.ID == sel && sel != ""}
		if st.Kind == model.StepEvent {
			sh.R = mdlEventR
			switch i {
			case firstEv:
				sh.Kind = "start"
			case lastEv:
				sh.Kind, sh.R = "end", mdlEndR
			default:
				sh.Kind = "catch"
			}
			sh = place(sh, sh.R, estLabelHalf(st.Name, "event"))
			sh.X, sh.Y = sh.CX-sh.R, sh.CY-sh.R
			sh.HaloR = sh.R + 7
			sh.InnerR = sh.R - 4
			sh.LabelY = sh.CY + sh.R + 15
		} else {
			sh.Kind, sh.W, sh.H = "task", mdlTaskW, mdlTaskH
			sh = place(sh, mdlTaskW/2, 0)
			sh.X, sh.Y = sh.CX-mdlTaskW/2, sh.CY-mdlTaskH/2
			sh.IconX, sh.IconY = sh.X+9, sh.Y+17
		}
		shapes = append(shapes, sh)
	}

	// Trailing end marker (a terminal sink after the last step).
	shapes = append(shapes, place(mdlShape{Kind: "marker-end", R: mdlMarkerR}, mdlMarkerR, 0))

	// Straight horizontal sequence flows between consecutive shapes.
	var flows []mdlFlow
	for i := 0; i+1 < len(shapes); i++ {
		a, b := shapes[i], shapes[i+1]
		x1 := a.CX + shapeHalf(a)
		x2 := b.CX - shapeHalf(b)
		flows = append(flows, mdlFlow{D: "M " + f(x1) + " " + f(midY) + " L " + f(x2) + " " + f(midY)})
	}

	md.Shapes = shapes
	md.Flows = flows
	md.W = prevCX + prevHalf + mdlPadX // right edge of the trailing marker
	md.H = mdlPadY*2 + mdlLaneH
	md.HalfH = md.H / 2
	md.Empty = len(d.Steps) == 0
	return md
}

// shapeHalf is the horizontal half-extent of a shape (radius or half-width).
func shapeHalf(sh mdlShape) float64 {
	if sh.Kind == "task" {
		return sh.W / 2
	}
	return sh.R
}

// estLabelHalf approximates half the rendered width of a centred label, so the
// layout can reserve horizontal room and long event names never collide. An
// empty name falls back to the placeholder the template draws. There is no text
// metric available server-side; mdlCharW is intentionally generous.
func estLabelHalf(name, placeholder string) float64 {
	if name == "" {
		name = placeholder
	}
	return float64(len([]rune(name))) * mdlCharW / 2
}

// laneLabel derives the pool caption from the subject template (the collection),
// falling back to the process name — matching the lane bpmngen emits.
func laneLabel(d *model.Draft) string {
	for _, seg := range strings.Split(strings.Trim(d.SubjectStyle, "/"), "/") {
		if seg != "" && seg != "{id}" {
			return seg
		}
	}
	return d.Name
}

// f formats a coordinate compactly for SVG path data.
func f(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
