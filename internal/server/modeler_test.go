package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
)

// addStep adds a step of the given kind to a draft and returns its id.
func addStep(t *testing.T, s *Server, id, kind string) string {
	t.Helper()
	rec := s.do(http.MethodPost, "/drafts/"+id+"/steps?kind="+kind, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("add step status = %d", rec.Code)
	}
	d, _ := s.store.Get(id)
	return d.Steps[len(d.Steps)-1].ID
}

func TestHandleModeler(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Canvas")

	// No draft chosen → the empty pick prompt.
	rec := s.do(http.MethodGet, "/modeler", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty modeler status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Modelle") {
		t.Errorf("empty modeler should prompt to pick a model")
	}

	// A real draft → the canvas root.
	rec = s.do(http.MethodGet, "/modeler?draft="+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("modeler status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"mdl-root", "mdl-svg", "mdl-palette", "mdl-props"} {
		if !strings.Contains(body, want) {
			t.Errorf("modeler body missing %q", want)
		}
	}

	// Missing draft → 404.
	if rec := s.do(http.MethodGet, "/modeler?draft=ghost", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing draft modeler = %d", rec.Code)
	}
}

func TestHandleModelerDecodeErrorIs500(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	seedDraft(t, s, "Mc")
	corruptDraft(t, s, "mc")

	rec := s.do(http.MethodGet, "/modeler?draft=mc", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("modeler decode error status = %d, want 500", rec.Code)
	}
}

func TestHandleModelerSelection(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Pick")
	sid := addStep(t, s, id, "event")

	rec := s.do(http.MethodGet, "/modeler?draft="+id+"&sel="+sid, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("select status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "is-selected") {
		t.Errorf("selected shape should carry is-selected")
	}
	if !strings.Contains(body, "Properties") {
		t.Errorf("properties panel should render for the selected step")
	}
}

func TestBuildModeler(t *testing.T) {
	tests := []struct {
		name      string
		steps     []model.Step
		sel       string
		wantEmpty bool
		// wantKinds is the expected Kind of each shape (incl. the two markers).
		wantKinds []string
		wantFlows int
		wantSel   bool
	}{
		{
			name:      "empty draft is two markers and one flow",
			steps:     nil,
			wantEmpty: true,
			wantKinds: []string{"marker-start", "marker-end"},
			wantFlows: 1,
		},
		{
			name:      "single event is a start (first==last picks start)",
			steps:     []model.Step{{ID: "a", Kind: model.StepEvent}},
			wantKinds: []string{"marker-start", "start", "marker-end"},
			wantFlows: 2,
		},
		{
			name: "events frame the chain, task in the middle",
			steps: []model.Step{
				{ID: "a", Kind: model.StepEvent},
				{ID: "b", Kind: model.StepTask},
				{ID: "c", Kind: model.StepEvent},
				{ID: "d", Kind: model.StepEvent},
			},
			wantKinds: []string{"marker-start", "start", "task", "catch", "end", "marker-end"},
			wantFlows: 5,
		},
		{
			name:      "selection marks the chosen step",
			steps:     []model.Step{{ID: "a", Kind: model.StepEvent}, {ID: "b", Kind: model.StepEvent}},
			sel:       "b",
			wantKinds: []string{"marker-start", "start", "end", "marker-end"},
			wantFlows: 3,
			wantSel:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := &model.Draft{Name: "P", Steps: tc.steps}
			md := buildModeler(d, tc.sel)
			if md.Empty != tc.wantEmpty {
				t.Errorf("Empty = %v, want %v", md.Empty, tc.wantEmpty)
			}
			if len(md.Shapes) != len(tc.wantKinds) {
				t.Fatalf("shapes = %d, want %d", len(md.Shapes), len(tc.wantKinds))
			}
			for i, want := range tc.wantKinds {
				if md.Shapes[i].Kind != want {
					t.Errorf("shape[%d].Kind = %q, want %q", i, md.Shapes[i].Kind, want)
				}
			}
			if len(md.Flows) != tc.wantFlows {
				t.Errorf("flows = %d, want %d", len(md.Flows), tc.wantFlows)
			}
			if tc.wantSel {
				if md.Selected == nil || md.Selected.ID != tc.sel {
					t.Errorf("Selected = %v, want step %q", md.Selected, tc.sel)
				}
				var found bool
				for _, sh := range md.Shapes {
					if sh.StepID == tc.sel && sh.Selected {
						found = true
					}
				}
				if !found {
					t.Errorf("selected shape not flagged")
				}
			}
			if md.W <= 0 || md.H <= 0 || md.HalfH != md.H/2 {
				t.Errorf("bounds malformed: W=%v H=%v HalfH=%v", md.W, md.H, md.HalfH)
			}
		})
	}
}

func TestHandleReorderStep(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Reorder")
	a := addStep(t, s, id, "event")
	b := addStep(t, s, id, "task")
	c := addStep(t, s, id, "event")

	// Move the first step (a) to the end.
	rec := s.do(http.MethodPost, "/drafts/"+id+"/steps/"+a+"/reorder", form(map[string]string{"to": "2", "view": "modeler"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder status = %d", rec.Code)
	}
	// view=modeler must re-render the canvas, not the outline.
	if !strings.Contains(rec.Body.String(), "mdl-root") {
		t.Errorf("reorder with view=modeler should render the canvas")
	}
	d, _ := s.store.Get(id)
	if d.Steps[0].ID != b || d.Steps[1].ID != c || d.Steps[2].ID != a {
		t.Fatalf("order after reorder = %v %v %v", d.Steps[0].ID, d.Steps[1].ID, d.Steps[2].ID)
	}

	// Out-of-range index clamps to the last slot (no-op here, a already last).
	s.do(http.MethodPost, "/drafts/"+id+"/steps/"+a+"/reorder", form(map[string]string{"to": "99"}))
	d, _ = s.store.Get(id)
	if d.Steps[2].ID != a {
		t.Errorf("clamp high failed")
	}
	// Negative index clamps to the front.
	s.do(http.MethodPost, "/drafts/"+id+"/steps/"+a+"/reorder", form(map[string]string{"to": "-5"}))
	d, _ = s.store.Get(id)
	if d.Steps[0].ID != a {
		t.Errorf("clamp low failed")
	}

	// Unknown step → no-op, still 200.
	if rec := s.do(http.MethodPost, "/drafts/"+id+"/steps/nope/reorder", form(map[string]string{"to": "0"})); rec.Code != http.StatusOK {
		t.Fatalf("unknown reorder status = %d", rec.Code)
	}
	// Missing draft → 404.
	if rec := s.do(http.MethodPost, "/drafts/ghost/steps/"+a+"/reorder", form(map[string]string{"to": "0"})); rec.Code != http.StatusNotFound {
		t.Fatalf("missing draft reorder = %d", rec.Code)
	}
}

// TestModelerRenderDispatch checks that the shared step/field/meta handlers
// render the canvas fragment when the request carries view=modeler, and the
// outline (procsteps) otherwise.
func TestModelerRenderDispatch(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Dispatch")
	sid := addStep(t, s, id, "event")

	// Default (no view) → outline fragment.
	rec := s.do(http.MethodPost, "/drafts/"+id+"/steps/"+sid, form(map[string]string{"name": "order.created"}))
	if strings.Contains(rec.Body.String(), "mdl-root") {
		t.Errorf("default update should render the outline, not the canvas")
	}

	// view=modeler → canvas fragment, with the step still selected.
	rec = s.do(http.MethodPost, "/drafts/"+id+"/steps/"+sid, form(map[string]string{
		"name": "order.created", "view": "modeler", "sel": sid,
	}))
	body := rec.Body.String()
	if !strings.Contains(body, "mdl-root") {
		t.Errorf("view=modeler update should render the canvas")
	}
	if !strings.Contains(body, "is-selected") {
		t.Errorf("canvas should keep the edited step selected")
	}

	// Adding a step on the canvas selects the fresh shape.
	rec = s.do(http.MethodPost, "/drafts/"+id+"/steps?kind=task&view=modeler", nil)
	if !strings.Contains(rec.Body.String(), "is-selected") {
		t.Errorf("new canvas step should come back selected")
	}

	// Meta save on the canvas re-renders the canvas.
	rec = s.do(http.MethodPost, "/drafts/"+id+"/meta?view=modeler", form(map[string]string{
		"name": "Renamed", "subject": "/orders/{id}",
	}))
	if !strings.Contains(rec.Body.String(), "mdl-root") {
		t.Errorf("meta save with view=modeler should render the canvas")
	}
}

func TestLaneLabel(t *testing.T) {
	tests := []struct {
		subject, name, want string
	}{
		{"/orders/{id}", "Order", "orders"},
		{"/{id}", "Order", "Order"}, // only an id placeholder → fall back to name
		{"", "Checkout", "Checkout"},
		{"employees/{id}/onboarding", "X", "employees"},
	}
	for _, tc := range tests {
		got := laneLabel(&model.Draft{Name: tc.name, SubjectStyle: tc.subject})
		if got != tc.want {
			t.Errorf("laneLabel(%q,%q) = %q, want %q", tc.subject, tc.name, got, tc.want)
		}
	}
}

func TestShapeHalf(t *testing.T) {
	if got := shapeHalf(mdlShape{Kind: "task", W: 100}); got != 50 {
		t.Errorf("task half = %v, want 50", got)
	}
	if got := shapeHalf(mdlShape{Kind: "start", R: 19}); got != 19 {
		t.Errorf("event half = %v, want 19", got)
	}
}

func TestLabelHalf(t *testing.T) {
	tests := []struct {
		name string
		sh   mdlShape
		want float64
	}{
		{"event scales with name length", mdlShape{Kind: "catch", Label: "abcde"}, 5 * mdlCharW / 2},
		{"start event counts too", mdlShape{Kind: "start", Label: "ab"}, 2 * mdlCharW / 2},
		{"end event counts too", mdlShape{Kind: "end", Label: "abc"}, 3 * mdlCharW / 2},
		{"task label stays inside its box", mdlShape{Kind: "task", Label: "order-shipped"}, 0},
		{"markers have no label", mdlShape{Kind: "marker-start"}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := labelHalf(tc.sh); got != tc.want {
				t.Errorf("labelHalf(%+v) = %v, want %v", tc.sh, got, tc.want)
			}
		})
	}
}

// TestBuildModelerLabelsNeverOverlap pins the bug from the screenshot: long
// adjacent event names ("order-delivered" next to "order-cancelled") must be
// spaced so their centred labels keep a clear gap, not collide.
func TestBuildModelerLabelsNeverOverlap(t *testing.T) {
	names := []string{"order-placed", "order-paid", "order-shipped", "order-delivered", "order-cancelled"}
	steps := make([]model.Step, len(names))
	for i, n := range names {
		steps[i] = model.Step{ID: n, Kind: model.StepEvent, Name: n}
	}
	md := buildModeler(&model.Draft{Name: "Orders", Steps: steps}, "")

	prev := mdlShape{}
	have := false
	for _, sh := range md.Shapes {
		if labelHalf(sh) == 0 {
			continue // markers carry no below-label
		}
		if have {
			// The two label half-widths plus the minimum gap must fit between
			// the centres — otherwise the captions would touch or overlap.
			gap := sh.CX - prev.CX - labelHalf(prev) - labelHalf(sh)
			if gap < mdlLabelGap-0.01 { // -ε: pushed pairs land exactly on the gap
				t.Errorf("labels %q→%q overlap: gap %.1f < %.1f", prev.Label, sh.Label, gap, mdlLabelGap)
			}
		}
		prev, have = sh, true
	}
}
