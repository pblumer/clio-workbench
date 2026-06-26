// Package process — references.go infers relationships from event data
// payloads: foreign-key-like fields (customerId, tagIds, …) and association
// events (two FKs → n:m). This is heuristic, meant as a starting point a
// developer refines, not authoritative schema.
package process

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// RefEvent is the input: an event's subject (owner) and its data payload.
type RefEvent struct {
	Subject string
	Type    string
	Data    json.RawMessage
}

// RefNode is a collection in the reference graph. Known is false for a
// referenced target that is not itself a written collection.
type RefNode struct {
	Name   string
	Known  bool
	Events int
}

// RefEdge is an inferred relationship. Kind is "n:1", "1:n" or "n:m"; Via names
// the field (or association event type) it was inferred from.
type RefEdge struct {
	From  string
	To    string
	Kind  string
	Via   string
	Count int
}

// RefGraph is the inferred reference graph.
type RefGraph struct {
	Nodes []RefNode
	Edges []RefEdge
}

var fkRe = regexp.MustCompile(`(?i)^(.+?)(ids|id|refs|ref)$`)

// ReferenceCollection reports whether a payload field name looks like a
// foreign-key reference (employeeId, customerId, tagIds, productRef, …) and, if
// so, the collection it points at — the best-effort plural of the stem
// (employeeId → "employees"). It exposes the same fkRe heuristic BuildReferences
// uses, so a payload value can be linked to the referenced subject.
//
// The plural is naive (stem, or stem already ending in "s") and is NOT resolved
// against actually-written collections — matching this package's stance that
// these references are a starting point a developer refines, not authoritative.
// A field whose stem only resembles a suffix (e.g. "valid" → "val"+"id") is a
// known false positive; the worst case is a link to an empty event list.
func ReferenceCollection(field string) (string, bool) {
	lk := strings.ToLower(strings.TrimSpace(field))
	if lk == "" || lk == "id" {
		return "", false
	}
	m := fkRe.FindStringSubmatch(lk)
	if m == nil {
		return "", false
	}
	stem := m[1] // fkRe's (.+?) guarantees a non-empty stem
	if !strings.HasSuffix(stem, "s") {
		stem += "s"
	}
	return stem, true
}

func firstSegment(subject string) string {
	for _, s := range strings.Split(strings.Trim(subject, "/"), "/") {
		if s != "" {
			return strings.ToLower(s)
		}
	}
	return ""
}

// resolveCollection maps a field stem to a known collection name, trying the
// stem, its plural and singular. Returns (name, known).
func resolveCollection(stem string, collections map[string]bool) (string, bool) {
	stem = strings.ToLower(stem)
	cands := []string{stem, stem + "s"}
	if strings.HasSuffix(stem, "s") {
		cands = append(cands, strings.TrimSuffix(stem, "s"))
	}
	for _, c := range cands {
		if collections[c] {
			return c, true
		}
	}
	return stem, false
}

// BuildReferences infers the reference graph from events' data payloads.
func BuildReferences(events []RefEvent) RefGraph {
	collections := map[string]bool{}
	nodeEvents := map[string]int{}
	for _, e := range events {
		if owner := firstSegment(e.Subject); owner != "" {
			collections[owner] = true
			nodeEvents[owner]++
		}
	}

	type fk struct {
		field  string
		target string
		known  bool
		array  bool
	}
	type edgeKey struct{ from, to, kind string }
	agg := map[edgeKey]*RefEdge{}
	addEdge := func(from, to, kind, via string) {
		if from == "" || to == "" || from == to {
			return
		}
		k := edgeKey{from, to, kind}
		e := agg[k]
		if e == nil {
			e = &RefEdge{From: from, To: to, Kind: kind, Via: via}
			agg[k] = e
		}
		e.Count++
	}

	extra := map[string]bool{} // referenced but unknown targets

	for _, e := range events {
		owner := firstSegment(e.Subject)
		if owner == "" || len(e.Data) == 0 {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(e.Data, &obj); err != nil {
			continue
		}
		var fks []fk
		seen := map[string]bool{}
		for key, raw := range obj {
			lk := strings.ToLower(key)
			if lk == "id" {
				continue
			}
			m := fkRe.FindStringSubmatch(lk)
			if m == nil {
				continue
			}
			stem, suffix := m[1], m[2]
			target, known := resolveCollection(stem, collections)
			if target == owner {
				continue
			}
			array := suffix == "ids" || suffix == "refs" || strings.HasPrefix(strings.TrimSpace(string(raw)), "[")
			if seen[target] {
				continue
			}
			seen[target] = true
			fks = append(fks, fk{field: key, target: target, known: known, array: array})
			if !known {
				extra[target] = true
			}
		}

		switch {
		case len(fks) >= 2:
			// Association event: link the first two distinct targets (n:m).
			sort.Slice(fks, func(i, j int) bool { return fks[i].target < fks[j].target })
			addEdge(fks[0].target, fks[1].target, "n:m", e.Type)
		case len(fks) == 1:
			f := fks[0]
			kind := "n:1"
			if f.array {
				kind = "1:n"
			}
			addEdge(owner, f.target, kind, f.field)
		}
	}

	g := RefGraph{}
	names := map[string]bool{}
	for c := range collections {
		names[c] = true
	}
	for t := range extra {
		names[t] = true
	}
	for _, e := range agg {
		names[e.From] = true
		names[e.To] = true
	}
	for name := range names {
		g.Nodes = append(g.Nodes, RefNode{Name: name, Known: collections[name], Events: nodeEvents[name]})
	}
	sort.Slice(g.Nodes, func(i, j int) bool {
		if g.Nodes[i].Events != g.Nodes[j].Events {
			return g.Nodes[i].Events > g.Nodes[j].Events
		}
		return g.Nodes[i].Name < g.Nodes[j].Name
	})
	for _, e := range agg {
		g.Edges = append(g.Edges, *e)
	}
	sort.Slice(g.Edges, func(i, j int) bool {
		a, b := g.Edges[i], g.Edges[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	return g
}
