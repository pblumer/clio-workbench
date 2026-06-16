// Package bpmngen renders a process Draft as a BPMN file that round-trips with
// the conformance check (and opens in bpmn.io). Events become message
// start/catch/end events (carrying the event-type name); tasks become send
// tasks. The lane is the subject collection and the participant is the process,
// so the conformance parser recovers the same subject scope.
package bpmngen

import (
	"fmt"
	"strings"

	"github.com/pblumer/clio-workbench/internal/model"
)

type genNode struct {
	id, kind, name string
	w, h, x, y     float64
}

// GenerateBPMN renders the draft as BPMN XML.
func GenerateBPMN(d model.Draft) string {
	segs := splitSubject(d.SubjectStyle)
	lane := ""
	if len(segs) > 0 {
		lane = segs[0]
	}
	process := ""
	if len(segs) > 0 {
		process = segs[len(segs)-1]
	}
	if process == "" || process == "{id}" {
		process = d.Name
	}

	firstEv, lastEv := -1, -1
	for i, s := range d.Steps {
		if s.Kind == model.StepEvent {
			if firstEv < 0 {
				firstEv = i
			}
			lastEv = i
		}
	}

	var nodes []genNode
	x := 180.0
	for i, s := range d.Steps {
		n := genNode{id: fmt.Sprintf("Node_%d", i), name: s.Name}
		if s.Kind == model.StepEvent {
			switch i {
			case firstEv:
				n.kind = "start"
			case lastEv:
				n.kind = "end"
			default:
				n.kind = "catch"
			}
			n.w, n.h, n.x, n.y = 36, 36, x, 182
		} else {
			n.kind, n.w, n.h, n.x, n.y = "task", 100, 80, x, 160
		}
		nodes = append(nodes, n)
		x += 200
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL" xmlns:bpmndi="http://www.omg.org/spec/BPMN/20100524/DI" xmlns:dc="http://www.omg.org/spec/DD/20100524/DC" xmlns:di="http://www.omg.org/spec/DD/20100524/DI" id="Definitions_1" targetNamespace="http://bpmn.io/schema/bpmn">` + "\n")
	b.WriteString(`  <bpmn:collaboration id="Collaboration_1">` + "\n")
	b.WriteString(`    <bpmn:participant id="Participant_1" name="` + esc(process) + `" processRef="Process_1" />` + "\n")
	b.WriteString(`  </bpmn:collaboration>` + "\n")
	b.WriteString(`  <bpmn:process id="Process_1" isExecutable="false">` + "\n")

	if lane != "" {
		b.WriteString(`    <bpmn:laneSet id="LaneSet_1">` + "\n")
		b.WriteString(`      <bpmn:lane id="Lane_1" name="` + esc(lane) + `">` + "\n")
		for _, n := range nodes {
			b.WriteString(`        <bpmn:flowNodeRef>` + n.id + `</bpmn:flowNodeRef>` + "\n")
		}
		b.WriteString(`      </bpmn:lane>` + "\n")
		b.WriteString(`    </bpmn:laneSet>` + "\n")
	}

	for i, n := range nodes {
		inc, out := "", ""
		if i > 0 {
			inc = fmt.Sprintf("      <bpmn:incoming>Flow_%d</bpmn:incoming>\n", i-1)
		}
		if i < len(nodes)-1 {
			out = fmt.Sprintf("      <bpmn:outgoing>Flow_%d</bpmn:outgoing>\n", i)
		}
		switch n.kind {
		case "start":
			b.WriteString(`    <bpmn:startEvent id="` + n.id + `" name="` + esc(n.name) + `">` + "\n")
			b.WriteString(out)
			b.WriteString(`      <bpmn:messageEventDefinition id="` + n.id + `_msg" />` + "\n")
			b.WriteString(`    </bpmn:startEvent>` + "\n")
		case "end":
			b.WriteString(`    <bpmn:endEvent id="` + n.id + `" name="` + esc(n.name) + `">` + "\n")
			b.WriteString(inc)
			b.WriteString(`      <bpmn:messageEventDefinition id="` + n.id + `_msg" />` + "\n")
			b.WriteString(`    </bpmn:endEvent>` + "\n")
		case "catch":
			b.WriteString(`    <bpmn:intermediateCatchEvent id="` + n.id + `" name="` + esc(n.name) + `">` + "\n")
			b.WriteString(inc)
			b.WriteString(out)
			b.WriteString(`      <bpmn:messageEventDefinition id="` + n.id + `_msg" />` + "\n")
			b.WriteString(`    </bpmn:intermediateCatchEvent>` + "\n")
		default: // task
			b.WriteString(`    <bpmn:sendTask id="` + n.id + `" name="` + esc(n.name) + `">` + "\n")
			b.WriteString(inc)
			b.WriteString(out)
			b.WriteString(`    </bpmn:sendTask>` + "\n")
		}
	}

	for i := 0; i+1 < len(nodes); i++ {
		b.WriteString(fmt.Sprintf(`    <bpmn:sequenceFlow id="Flow_%d" sourceRef="%s" targetRef="%s" />`+"\n", i, nodes[i].id, nodes[i+1].id))
	}
	b.WriteString(`  </bpmn:process>` + "\n")

	// Diagram interchange (simple linear layout).
	b.WriteString(`  <bpmndi:BPMNDiagram id="Diagram_1">` + "\n")
	b.WriteString(`    <bpmndi:BPMNPlane id="Plane_1" bpmnElement="Collaboration_1">` + "\n")
	maxX := 360.0
	if len(nodes) > 0 {
		maxX = nodes[len(nodes)-1].x + 160
	}
	b.WriteString(fmt.Sprintf(`      <bpmndi:BPMNShape id="Participant_1_di" bpmnElement="Participant_1" isHorizontal="true"><dc:Bounds x="130" y="80" width="%.0f" height="250" /></bpmndi:BPMNShape>`+"\n", maxX-100))
	if lane != "" {
		b.WriteString(fmt.Sprintf(`      <bpmndi:BPMNShape id="Lane_1_di" bpmnElement="Lane_1" isHorizontal="true"><dc:Bounds x="160" y="80" width="%.0f" height="250" /></bpmndi:BPMNShape>`+"\n", maxX-130))
	}
	for _, n := range nodes {
		b.WriteString(fmt.Sprintf(`      <bpmndi:BPMNShape id="%s_di" bpmnElement="%s"><dc:Bounds x="%.0f" y="%.0f" width="%.0f" height="%.0f" /></bpmndi:BPMNShape>`+"\n",
			n.id, n.id, n.x, n.y, n.w, n.h))
	}
	for i := 0; i+1 < len(nodes); i++ {
		a, c := nodes[i], nodes[i+1]
		b.WriteString(fmt.Sprintf(`      <bpmndi:BPMNEdge id="Flow_%d_di" bpmnElement="Flow_%d"><di:waypoint x="%.0f" y="200" /><di:waypoint x="%.0f" y="200" /></bpmndi:BPMNEdge>`+"\n",
			i, i, a.x+a.w, c.x))
	}
	b.WriteString(`    </bpmndi:BPMNPlane>` + "\n")
	b.WriteString(`  </bpmndi:BPMNDiagram>` + "\n")
	b.WriteString(`</bpmn:definitions>` + "\n")
	return b.String()
}

func splitSubject(s string) []string {
	var out []string
	for _, seg := range strings.Split(strings.Trim(s, "/"), "/") {
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
