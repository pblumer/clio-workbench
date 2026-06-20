package bpmngen

import (
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/process"
)

func TestGenerateBPMNRoundTrips(t *testing.T) {
	d := model.Draft{
		Name:         "Employee Onboarding",
		SubjectStyle: "/employees/{id}/employee-onboarding",
		Steps: []model.Step{
			{Kind: model.StepEvent, Name: "identity.employee.new"},
			{Kind: model.StepTask, Name: "attachMailbox"},
			{Kind: model.StepEvent, Name: "identity.employee.mailbox.attached"},
			{Kind: model.StepEvent, Name: "identity.employee.deployed"},
		},
	}
	xml := GenerateBPMN(d)

	m, err := process.ParseBPMN([]byte(xml))
	if err != nil {
		t.Fatalf("generated BPMN does not parse: %v\n%s", err, xml)
	}
	want := []string{"identity.employee.new", "identity.employee.mailbox.attached", "identity.employee.deployed"}
	if strings.Join(m.Expected, "|") != strings.Join(want, "|") {
		t.Errorf("expected sequence = %v, want %v", m.Expected, want)
	}
	if m.Subject != "/employees/{id}/employee-onboarding" {
		t.Errorf("subject scope = %q, want /employees/{id}/employee-onboarding", m.Subject)
	}
	// the send task must not leak into the event sequence
	for _, e := range m.Expected {
		if e == "attachMailbox" {
			t.Error("task leaked into expected events")
		}
	}
}

// TestGenerateBPMNProcessFallsBackToName exercises the branch where the last
// subject segment is empty or "{id}", so the participant name falls back to the
// draft Name.
func TestGenerateBPMNProcessFallsBackToName(t *testing.T) {
	cases := []struct {
		name         string
		subjectStyle string
	}{
		{"empty subject style", ""},
		{"trailing id placeholder", "/orders/{id}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := model.Draft{
				Name:         "My Fallback Process",
				SubjectStyle: tc.subjectStyle,
				Steps: []model.Step{
					{Kind: model.StepEvent, Name: "order.created"},
					{Kind: model.StepTask, Name: "ship"},
					{Kind: model.StepEvent, Name: "order.shipped"},
				},
			}
			xml := GenerateBPMN(d)
			want := `name="My Fallback Process"`
			if !strings.Contains(xml, want) {
				t.Errorf("expected participant %s in generated BPMN, got:\n%s", want, xml)
			}
		})
	}
}
