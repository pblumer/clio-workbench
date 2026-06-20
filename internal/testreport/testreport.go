// Package testreport renders a Test Studio generation run as a versionable
// artifact (docs/TESTSTUDIO.md §8): Markdown for the repo/wiki and JSON for
// further processing. Because every run is seeded, the report is exactly
// reproducible.
//
// This package is pure formatting over the Run data; the orchestration that
// fills a Run (generate → validate → mutate → coverage) lives with the caller,
// keeping rendering free of the simulator/validate dependencies.
package testreport

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Run is the outcome of one sampling run: positive samples checked for
// validity, plus negative (mutation) checks, plus edge coverage.
type Run struct {
	Model        string     `json:"model"`
	Seed         int64      `json:"seed"`
	When         time.Time  `json:"when"`
	Samples      int        `json:"samples"`
	Passed       int        `json:"passed"`
	Failed       int        `json:"failed"`
	CoveredEdges int        `json:"coveredEdges"`
	TotalEdges   int        `json:"totalEdges"`
	Note         string     `json:"note,omitempty"`
	Failures     []Failure  `json:"failures,omitempty"`
	Negatives    []Negative `json:"negatives,omitempty"`
}

// Failure is a positive sample that unexpectedly did not validate.
type Failure struct {
	Seed     int64    `json:"seed"`
	Sequence []string `json:"sequence"`
	Reason   string   `json:"reason"`
}

// Negative is a mutated (deliberately broken) stream and whether the engine
// rejected it as it should.
type Negative struct {
	Kind     string `json:"kind"`
	Desc     string `json:"desc"`
	Rejected bool   `json:"rejected"`
	Reason   string `json:"reason,omitempty"`
}

// JSON renders the run as pretty-printed JSON.
func (r Run) JSON() (string, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CoveragePct is the edge coverage as a whole-number percentage (0 when the
// model has no edges).
func (r Run) CoveragePct() int {
	if r.TotalEdges == 0 {
		return 0
	}
	return r.CoveredEdges * 100 / r.TotalEdges
}

// Markdown renders the run as a Markdown report.
func (r Run) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Teststudio — Generierungs-Report\n\n")
	fmt.Fprintf(&b, "- **Modell:** %s\n", r.Model)
	fmt.Fprintf(&b, "- **Seed:** %d\n", r.Seed)
	fmt.Fprintf(&b, "- **Zeitpunkt:** %s\n", r.When.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- **Stichproben:** %d (%d gültig, %d ungültig)\n", r.Samples, r.Passed, r.Failed)
	fmt.Fprintf(&b, "- **Kanten-Überdeckung:** %d/%d (%d%%)\n", r.CoveredEdges, r.TotalEdges, r.CoveragePct())
	if r.Note != "" {
		fmt.Fprintf(&b, "\n> %s\n", r.Note)
	}

	if len(r.Failures) > 0 {
		fmt.Fprintf(&b, "\n## Fehlgeschlagene Stichproben\n\n")
		for _, f := range r.Failures {
			fmt.Fprintf(&b, "- Seed %d: `%s` — %s\n", f.Seed, strings.Join(f.Sequence, " → "), f.Reason)
		}
	}

	if len(r.Negatives) > 0 {
		fmt.Fprintf(&b, "\n## Negativ-Prüfungen (Mutationen)\n\n")
		for _, n := range r.Negatives {
			mark := "✓ abgelehnt"
			if !n.Rejected {
				mark = "✗ NICHT abgelehnt"
			}
			fmt.Fprintf(&b, "- **%s** — %s → %s", n.Kind, n.Desc, mark)
			if n.Reason != "" {
				fmt.Fprintf(&b, " (%s)", n.Reason)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
