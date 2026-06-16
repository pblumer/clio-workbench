package process

import (
	"strings"
	"testing"
)

const sampleBPMN = `<?xml version="1.0"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <bpmn:collaboration><bpmn:participant name="identity.employee.new" processRef="P"/></bpmn:collaboration>
  <bpmn:process id="P">
    <bpmn:startEvent id="s" name="identity.employee.new"><bpmn:outgoing>f1</bpmn:outgoing></bpmn:startEvent>
    <bpmn:intermediateCatchEvent id="c1" name="identity.employee.created"/>
    <bpmn:sendTask id="t1" name="attachMailbox"/>
    <bpmn:intermediateCatchEvent id="c2" name="identity.employee.mailbox.attached"/>
    <bpmn:endEvent id="e" name="identity.employee.deployed"/>
    <bpmn:sequenceFlow id="f1" sourceRef="s" targetRef="c1"/>
    <bpmn:sequenceFlow id="f2" sourceRef="c1" targetRef="t1"/>
    <bpmn:sequenceFlow id="f3" sourceRef="t1" targetRef="c2"/>
    <bpmn:sequenceFlow id="f4" sourceRef="c2" targetRef="e"/>
  </bpmn:process>
</bpmn:definitions>`

func TestParseBPMN(t *testing.T) {
	m, err := ParseBPMN([]byte(sampleBPMN))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Process != "identity.employee.new" {
		t.Errorf("process name = %q", m.Process)
	}
	want := []string{
		"identity.employee.new",
		"identity.employee.created",
		"identity.employee.mailbox.attached",
		"identity.employee.deployed",
	}
	if strings.Join(m.Expected, " | ") != strings.Join(want, " | ") {
		t.Fatalf("expected = %v, want %v", m.Expected, want)
	}
	// the sendTask must NOT be an expected event step
	for _, s := range m.Expected {
		if s == "attachMailbox" {
			t.Error("task leaked into expected event sequence")
		}
	}
}

func TestConformanceScopesToLaneSubject(t *testing.T) {
	const laneBPMN = `<?xml version="1.0"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <bpmn:process id="P">
    <bpmn:laneSet><bpmn:lane id="L" name="/identity/1234"/></bpmn:laneSet>
    <bpmn:startEvent id="s" name="a"/>
    <bpmn:endEvent id="e" name="b"/>
    <bpmn:sequenceFlow id="f" sourceRef="s" targetRef="e"/>
  </bpmn:process>
</bpmn:definitions>`
	m, err := ParseBPMN([]byte(laneBPMN))
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "/identity/{id}" {
		t.Fatalf("lane subject pattern = %q, want /identity/{id}", m.Subject)
	}
	c := CheckConformance(m, map[string][]string{
		"/identity/1": {"a", "b"}, // in scope, conforming
		"/identity/2": {"a"},      // in scope, deviation
		"/orders/9":   {"a", "b"}, // out of scope → ignored
	}, 12)
	if c.Subject != "/identity/{id}" {
		t.Errorf("scope = %q, want /identity/{id}", c.Subject)
	}
	if c.Relevant != 2 || c.Conforming != 1 {
		t.Errorf("relevant=%d conforming=%d, want 2/1 (orders out of scope)", c.Relevant, c.Conforming)
	}
}

func TestSubjectScopeCollectionPlusProcess(t *testing.T) {
	const b = `<?xml version="1.0"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <bpmn:collaboration><bpmn:participant name="employee-onboarding" processRef="P"/></bpmn:collaboration>
  <bpmn:process id="P">
    <bpmn:laneSet><bpmn:lane id="L" name="employees"/></bpmn:laneSet>
    <bpmn:startEvent id="s" name="a"/>
    <bpmn:endEvent id="e" name="b"/>
    <bpmn:sequenceFlow id="f" sourceRef="s" targetRef="e"/>
  </bpmn:process>
</bpmn:definitions>`
	m, err := ParseBPMN([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	if m.Process != "employee-onboarding" {
		t.Errorf("process = %q", m.Process)
	}
	if m.Subject != "/employees/{id}/employee-onboarding" {
		t.Fatalf("scope = %q, want /employees/{id}/employee-onboarding", m.Subject)
	}
	c := CheckConformance(m, map[string][]string{
		"/employees/42/employee-onboarding": {"a", "b"}, // in scope, conforming
		"/employees/43/employee-onboarding": {"a"},      // in scope, deviation
		"/identity/1":                       {"a", "b"}, // out of scope
	}, 12)
	if c.Relevant != 2 || c.Conforming != 1 {
		t.Errorf("relevant=%d conforming=%d, want 2/1", c.Relevant, c.Conforming)
	}
}

func TestConformanceMatchesPrefixedIDs(t *testing.T) {
	const b = `<?xml version="1.0"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <bpmn:collaboration><bpmn:participant name="employee-onboarding" processRef="P"/></bpmn:collaboration>
  <bpmn:process id="P">
    <bpmn:laneSet><bpmn:lane id="L" name="employees"/></bpmn:laneSet>
    <bpmn:startEvent id="s" name="identity.employee.new"/>
    <bpmn:endEvent id="e" name="identity.employee.deployed"/>
    <bpmn:sequenceFlow id="f" sourceRef="s" targetRef="e"/>
  </bpmn:process>
</bpmn:definitions>`
	m, _ := ParseBPMN([]byte(b))
	// Real subjects use an EMP-#### id (letters + hyphen + digits).
	c := CheckConformance(m, map[string][]string{
		"/employees/EMP-30000/employee-onboarding": {"identity.employee.new", "identity.employee.deployed"},
		"/employees/EMP-30001/employee-onboarding": {"identity.employee.new"},
	}, 12)
	if c.Relevant != 2 {
		t.Fatalf("relevant = %d, want 2 (EMP-#### ids must match {id})", c.Relevant)
	}
	if c.Conforming != 1 {
		t.Errorf("conforming = %d, want 1", c.Conforming)
	}
}

func TestCheckConformance(t *testing.T) {
	m, _ := ParseBPMN([]byte(sampleBPMN))
	seqs := map[string][]string{
		// conforming (exact, in order; task command events not in model are ignored? they count as unexpected)
		"/e/1": {"identity.employee.new", "identity.employee.created", "identity.employee.mailbox.attached", "identity.employee.deployed"},
		// deviation: stops early (missing deployed)
		"/e/2": {"identity.employee.new", "identity.employee.created"},
		// unexpected event mixed in
		"/e/3": {"identity.employee.new", "identity.employee.created", "identity.employee.suspended", "identity.employee.mailbox.attached", "identity.employee.deployed"},
		// not relevant (no expected events)
		"/other/1": {"something.else"},
	}
	c := CheckConformance(m, seqs, 12)

	if c.Relevant != 3 {
		t.Errorf("relevant = %d, want 3", c.Relevant)
	}
	if c.Conforming != 1 {
		t.Errorf("conforming = %d, want 1 (/e/1)", c.Conforming)
	}
	// /e/3 introduced an unexpected type
	found := false
	for _, u := range c.Unexpected {
		if u.Type == "identity.employee.suspended" {
			found = true
		}
	}
	if !found {
		t.Error("unexpected type identity.employee.suspended not reported")
	}
	// step coverage: 'new' seen by all 3 relevant; 'deployed' by 2 (/e/1,/e/3)
	if c.Steps[0].Seen != 3 {
		t.Errorf("step new seen = %d, want 3", c.Steps[0].Seen)
	}
	if c.Steps[len(c.Steps)-1].Seen != 2 {
		t.Errorf("step deployed seen = %d, want 2", c.Steps[len(c.Steps)-1].Seen)
	}
}
