// Package process — bpmn.go parses a BPMN file into an expected event-type
// sequence and checks real per-subject event sequences against it
// (conformance / Gegenprobe, docs/WORKBENCH.md §7).
//
// Message/start/catch/throw/end events carry the event-type names; tasks are
// treated as commands and are not matched. Branching is followed linearly
// (first outgoing flow) for this first cut.
package process

import (
	"encoding/xml"
	"sort"
	"strings"
)

// BpmnStep is one node of the modelled flow in order.
type BpmnStep struct {
	Name    string
	Kind    string // start, catch, throw, end, task, gateway
	IsEvent bool
}

// BpmnModel is the parsed template.
type BpmnModel struct {
	Process  string
	Subject  string // the lane/subject that receives the events (e.g. /identity/1234)
	Steps    []BpmnStep
	Expected []string // event-step names in order (the expected sequence)
}

type xnode struct {
	ID   string `xml:"id,attr"`
	Name string `xml:"name,attr"`
}
type xflow struct {
	Source string `xml:"sourceRef,attr"`
	Target string `xml:"targetRef,attr"`
}
type xprocess struct {
	ID      string `xml:"id,attr"`
	LaneSet struct {
		Lanes []xnode `xml:"lane"`
	} `xml:"laneSet"`
	Start       []xnode `xml:"startEvent"`
	Catch       []xnode `xml:"intermediateCatchEvent"`
	Throw       []xnode `xml:"intermediateThrowEvent"`
	End         []xnode `xml:"endEvent"`
	SendTask    []xnode `xml:"sendTask"`
	ReceiveTask []xnode `xml:"receiveTask"`
	ServiceTask []xnode `xml:"serviceTask"`
	UserTask    []xnode `xml:"userTask"`
	Task        []xnode `xml:"task"`
	ExclusiveGw []xnode `xml:"exclusiveGateway"`
	ParallelGw  []xnode `xml:"parallelGateway"`
	InclusiveGw []xnode `xml:"inclusiveGateway"`
	Flows       []xflow `xml:"sequenceFlow"`
}
type xdefs struct {
	Collaboration struct {
		Participants []struct {
			Name       string `xml:"name,attr"`
			ProcessRef string `xml:"processRef,attr"`
		} `xml:"participant"`
	} `xml:"collaboration"`
	Processes []xprocess `xml:"process"`
}

// ParseBPMN parses a .bpmn file into a model with an ordered expected sequence.
func ParseBPMN(data []byte) (BpmnModel, error) {
	var defs xdefs
	if err := xml.Unmarshal(data, &defs); err != nil {
		return BpmnModel{}, err
	}

	// Pick the first process that actually has nodes.
	var p xprocess
	for _, cand := range defs.Processes {
		if cand.ID != "" || len(cand.Flows) > 0 {
			p = cand
			break
		}
	}

	kind := map[string]string{}
	name := map[string]string{}
	add := func(ns []xnode, k string) {
		for _, n := range ns {
			kind[n.ID] = k
			name[n.ID] = n.Name
		}
	}
	add(p.Start, "start")
	add(p.Catch, "catch")
	add(p.Throw, "throw")
	add(p.End, "end")
	add(p.SendTask, "task")
	add(p.ReceiveTask, "task")
	add(p.ServiceTask, "task")
	add(p.UserTask, "task")
	add(p.Task, "task")
	add(p.ExclusiveGw, "gateway")
	add(p.ParallelGw, "gateway")
	add(p.InclusiveGw, "gateway")

	next := map[string]string{}
	hasIncoming := map[string]bool{}
	for _, f := range p.Flows {
		if _, ok := next[f.Source]; !ok { // keep first outgoing
			next[f.Source] = f.Target
		}
		hasIncoming[f.Target] = true
	}

	// Start node: a startEvent, else any node without incoming flow.
	start := ""
	if len(p.Start) > 0 {
		start = p.Start[0].ID
	} else {
		for id := range kind {
			if !hasIncoming[id] {
				start = id
				break
			}
		}
	}

	m := BpmnModel{Process: processName(defs, p)}
	laneName := ""
	for _, lane := range p.LaneSet.Lanes {
		if lane.Name != "" {
			laneName = lane.Name
			break
		}
	}
	m.Subject = subjectScope(laneName, m.Process)
	seen := map[string]bool{}
	for cur := start; cur != "" && !seen[cur]; cur = next[cur] {
		seen[cur] = true
		k, ok := kind[cur]
		if !ok {
			continue
		}
		step := BpmnStep{Name: name[cur], Kind: k, IsEvent: isEventKind(k)}
		m.Steps = append(m.Steps, step)
		if step.IsEvent && step.Name != "" {
			m.Expected = append(m.Expected, step.Name)
		}
	}
	return m, nil
}

func isEventKind(k string) bool {
	return k == "start" || k == "catch" || k == "throw" || k == "end"
}

func processName(defs xdefs, p xprocess) string {
	for _, part := range defs.Collaboration.Participants {
		if part.Name != "" {
			return part.Name
		}
	}
	return p.ID
}

// --- conformance ---

// StepStat is per expected step: how many subjects reached it in order.
type StepStat struct {
	Name string
	Seen int
}

// TypeCount is an event type with an occurrence count.
type TypeCount struct {
	Type  string
	Count int
}

// SubjectDiff is a sample non-conforming subject.
type SubjectDiff struct {
	Subject string
	Detail  string
}

// subjectScope derives the subject pattern an event stream lives on from the
// BPMN lane and process name. Two conventions are supported:
//
//   - a bare collection lane ("employees") + process name ("employee-onboarding")
//     → /employees/{id}/employee-onboarding (an instance's process stream);
//   - a lane already written as a path ("/identity/1234") → /identity/{id}.
func subjectScope(lane, process string) string {
	lane = strings.Trim(strings.TrimSpace(lane), "/")
	if lane == "" {
		return ""
	}
	if strings.Contains(lane, "/") {
		return subjectPattern("/" + lane)
	}
	pat := "/" + lane + "/{id}"
	if process != "" {
		pat += "/" + process
	}
	return pat
}

// subjectPattern collapses instance ids to "{id}" so subjects of the same
// aggregate share a pattern (/identity/1234 → /identity/{id}).
func subjectPattern(s string) string {
	var parts []string
	for _, seg := range strings.Split(strings.Trim(s, "/"), "/") {
		if seg == "" {
			continue
		}
		if isID(seg) {
			parts = append(parts, "{id}")
		} else {
			parts = append(parts, seg)
		}
	}
	return "/" + strings.Join(parts, "/")
}

// Conformance is the result of checking real sequences against a model.
type Conformance struct {
	Process    string
	Subject    string // subject pattern the check was scoped to (if any)
	Expected   []string
	Steps      []StepStat
	Relevant   int // subjects that participate (≥1 expected event)
	Conforming int // subjects whose projected sequence equals Expected
	Unexpected []TypeCount
	Deviations []SubjectDiff
}

// CheckConformance compares per-subject type sequences against the model.
func CheckConformance(m BpmnModel, subjectSeqs map[string][]string, maxDeviations int) Conformance {
	if maxDeviations <= 0 {
		maxDeviations = 12
	}
	exp := m.Expected
	expSet := map[string]bool{}
	for _, e := range exp {
		expSet[e] = true
	}

	wantPattern := ""
	if m.Subject != "" {
		wantPattern = subjectPattern(m.Subject)
	}

	c := Conformance{Process: m.Process, Subject: wantPattern, Expected: exp}
	c.Steps = make([]StepStat, len(exp))
	for i, e := range exp {
		c.Steps[i].Name = e
	}
	unexpected := map[string]int{}

	// deterministic subject order
	subs := make([]string, 0, len(subjectSeqs))
	for s := range subjectSeqs {
		subs = append(subs, s)
	}
	sort.Strings(subs)

	for _, sub := range subs {
		if wantPattern != "" && subjectPattern(sub) != wantPattern {
			continue // out of the lane's subject scope
		}
		seq := subjectSeqs[sub]
		relevant := false
		subjUnexpected := 0
		var projected []string
		for _, t := range seq {
			if expSet[t] {
				relevant = true
				projected = append(projected, t)
			} else {
				unexpected[t]++
				subjUnexpected++
			}
		}
		if !relevant {
			continue
		}
		c.Relevant++

		// In-order match pointer over expected.
		p := 0
		for _, t := range projected {
			if p < len(exp) && t == exp[p] {
				c.Steps[p].Seen++
				p++
			}
		}
		conforming := p == len(exp) && len(projected) == len(exp) && subjUnexpected == 0
		if conforming {
			c.Conforming++
		} else if len(c.Deviations) < maxDeviations {
			c.Deviations = append(c.Deviations, SubjectDiff{
				Subject: sub,
				Detail:  deviationDetail(exp, p, projected, subjUnexpected),
			})
		}
	}

	for t, n := range unexpected {
		c.Unexpected = append(c.Unexpected, TypeCount{Type: t, Count: n})
	}
	sort.Slice(c.Unexpected, func(i, j int) bool {
		if c.Unexpected[i].Count != c.Unexpected[j].Count {
			return c.Unexpected[i].Count > c.Unexpected[j].Count
		}
		return c.Unexpected[i].Type < c.Unexpected[j].Type
	})
	return c
}

func deviationDetail(exp []string, matched int, projected []string, unexpected int) string {
	if matched < len(exp) {
		return "stopped at step " + itoa(matched+1) + "/" + itoa(len(exp)) + " — missing “" + exp[matched] + "”"
	}
	if unexpected > 0 {
		return "all steps in order, but " + itoa(unexpected) + " unexpected event(s)"
	}
	if len(projected) > len(exp) {
		return "all steps seen but with extra/repeated events"
	}
	return "out of order"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// SubjectSequences groups events into per-subject ordered type sequences,
// preserving input (chronological) order.
func SubjectSequences(events []Event) map[string][]string {
	seqs := map[string][]string{}
	for _, e := range events {
		seqs[e.Subject] = append(seqs[e.Subject], e.Type)
	}
	return seqs
}
