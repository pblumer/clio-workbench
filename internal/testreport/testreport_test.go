package testreport

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleRun() Run {
	return Run{
		Model:        "Order",
		Seed:         42,
		When:         time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC),
		Samples:      20,
		Passed:       19,
		Failed:       1,
		CoveredEdges: 3,
		TotalEdges:   4,
		Failures: []Failure{
			{Seed: 7, Sequence: []string{"created", "shipped"}, Reason: "no transition from state \"placed\" via event type \"shipped\""},
		},
		Negatives: []Negative{
			{Kind: "insert-unknown", Desc: "inserted X", Rejected: true, Reason: "no transition"},
			{Kind: "swap-order", Desc: "swapped 0 and 1", Rejected: false, Reason: "not rejected"},
		},
	}
}

func TestCoveragePct(t *testing.T) {
	if got := sampleRun().CoveragePct(); got != 75 {
		t.Fatalf("CoveragePct = %d, want 75", got)
	}
	if got := (Run{}).CoveragePct(); got != 0 {
		t.Fatalf("empty CoveragePct = %d, want 0", got)
	}
}

func TestMarkdown(t *testing.T) {
	md := sampleRun().Markdown()
	for _, want := range []string{
		"# Teststudio — Generierungs-Report",
		"**Modell:** Order",
		"**Seed:** 42",
		"2026-06-20T10:00:00Z",
		"20 (19 gültig, 1 ungültig)",
		"3/4 (75%)",
		"## Fehlgeschlagene Stichproben",
		"created → shipped",
		"## Negativ-Prüfungen",
		"✓ abgelehnt",
		"✗ NICHT abgelehnt",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
}

func TestMarkdownWithNoteNoSections(t *testing.T) {
	r := Run{Model: "Flat", Note: "model has no start state"}
	md := r.Markdown()
	if !strings.Contains(md, "> model has no start state") {
		t.Errorf("note missing:\n%s", md)
	}
	if strings.Contains(md, "## Fehlgeschlagene") || strings.Contains(md, "## Negativ") {
		t.Errorf("empty run should have no failure/negative sections:\n%s", md)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	r := sampleRun()
	s, err := r.JSON()
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	var back Run
	if err := json.Unmarshal([]byte(s), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Model != r.Model || back.Passed != r.Passed || len(back.Negatives) != 2 {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}
