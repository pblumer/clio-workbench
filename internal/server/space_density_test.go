package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/process"
)

// manySubjectsBody is an NDJSON body spread over more than dMaxRows subjects, so
// the Event Space auto-switches to the density overview.
func manySubjectsBody(n int) string {
	lines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%04d", i)
		lines = append(lines, fmt.Sprintf(
			`{"id":"%s","subject":"/employees/EMP-%s","type":"task.created","time":"2024-01-01T00:%02d:00Z"}`,
			id, id, i%60))
	}
	return ndjsonLines(lines...)
}

// Beyond the row budget the view rolls subjects up into a density grid: the
// aggregated cells render and the header reports the density mode.
func TestHandleSpaceDensityAuto(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = manySubjectsBody(dMaxRows + 30)
	f.connect(s)

	rec := s.do(http.MethodGet, "/space", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "density-graph") || !strings.Contains(body, "class=\"dcell") {
		t.Errorf("expected a density grid, got:\n%s", body)
	}
	if !strings.Contains(body, "events · density") {
		t.Errorf("expected the density header, got:\n%s", body)
	}
	// Drill carriers must be present so a click can refine the filter.
	if !strings.Contains(body, "data-min=") || !strings.Contains(body, "data-max=") {
		t.Errorf("density cells missing drill bounds")
	}
}

// ?mode=dots forces the per-event detail view even past the budget; ?mode=density
// forces the overview even for a tiny scope.
func TestHandleSpaceModeOverride(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = manySubjectsBody(dMaxRows + 30)
	f.connect(s)

	dots := s.do(http.MethodGet, "/space?mode=dots", nil).Body.String()
	if strings.Contains(dots, "class=\"dcell") {
		t.Errorf("mode=dots must not render density cells")
	}
	if !strings.Contains(dots, "class=\"dot\"") {
		t.Errorf("mode=dots must render per-event dots")
	}

	// A small scope normally renders dots; mode=density forces the overview.
	f.ndjson = fakeEventsBody()
	dense := s.do(http.MethodGet, "/space?mode=density", nil).Body.String()
	if !strings.Contains(dense, "class=\"dcell") {
		t.Errorf("mode=density must render density cells even for a small scope")
	}
}

// In density mode ?group=variant rolls rows up by process variant; the chosen
// grouping is echoed into the hidden form field so it survives later requests.
func TestHandleSpaceGroupVariant(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = manySubjectsBody(dMaxRows + 30)
	f.connect(s)

	body := s.do(http.MethodGet, "/space?mode=density&group=variant", nil).Body.String()
	if !strings.Contains(body, "class=\"dcell") {
		t.Fatalf("expected a density grid, got:\n%s", body)
	}
	if !strings.Contains(body, `name="group" value="variant"`) {
		t.Errorf("group choice not persisted into the form")
	}
	if !strings.Contains(body, "rows: variant") {
		t.Errorf("group toggle missing its active label")
	}
}

// buildDensityView maps a process.Density onto the shared chart frame: a row per
// band, a rect per non-empty cell, the time axis label and the density mode.
func TestBuildDensityView(t *testing.T) {
	d := process.Density{
		Rows: []process.DensityRow{
			{Label: "/e/1", Prefix: "/e/1", Subjects: 1, Count: 2},
			{Label: "/e/2 … /e/9 · 8", Prefix: "/e", Subjects: 8, Count: 5},
		},
		Cols:   4,
		ByTime: true,
		Total:  9,
		Events: 7,
		Max:    5,
		Cells: []process.DensityCell{
			{Row: 0, Col: 0, Count: 2, Phase: process.PhaseComplete, MinID: "a", MaxID: "b"},
			{Row: 1, Col: 3, Count: 5, Phase: process.PhaseError, MinID: "c", MaxID: "d"},
		},
	}
	v := buildDensityView(d)
	if v.State != "ok" || v.Mode != "density" {
		t.Fatalf("state/mode = %q/%q, want ok/density", v.State, v.Mode)
	}
	if v.Total != 9 || v.Shown != 2 {
		t.Errorf("Total/Shown = %d/%d, want 9/2", v.Total, v.Shown)
	}
	if v.Axis != "time →" {
		t.Errorf("Axis = %q, want time →", v.Axis)
	}
	if len(v.Cells) != 2 {
		t.Fatalf("cells = %d, want 2", len(v.Cells))
	}
	// The busiest cell (count == Max) is fully opaque; the band prefix rides along.
	busy := v.Cells[1]
	if busy.Opacity != "1.000" {
		t.Errorf("busiest cell opacity = %q, want 1.000", busy.Opacity)
	}
	if busy.Prefix != "/e" || busy.MinID != "c" || busy.MaxID != "d" {
		t.Errorf("drill carriers = %q/%q/%q, want /e/c/d", busy.Prefix, busy.MinID, busy.MaxID)
	}
	if busy.Phase != "error" {
		t.Errorf("phase class = %q, want error", busy.Phase)
	}
}
