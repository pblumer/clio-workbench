package process

import (
	"sort"
	"strings"
)

// TypeNode is an event type with its occurrence count and lifecycle phase.
type TypeNode struct {
	Type  string
	Count int
	Phase Phase
}

// SubjectNode is a subject (grouped to a prefix) with its event count.
type SubjectNode struct {
	Subject string
	Count   int
}

// Link is a subject↔type co-occurrence: how often that type landed on that
// subject group.
type Link struct {
	Subject string
	Type    string
	Count   int
}

// MeetGraph is the bipartite graph where subjects and event types meet.
type MeetGraph struct {
	Subjects []SubjectNode
	Types    []TypeNode
	Links    []Link
	Events   int
}

// TopLevelSubject reduces a subject path to its first depth segments, e.g.
// ("/orders/123/items", 1) → "/orders". depth <= 0 is treated as 1.
func TopLevelSubject(subject string, depth int) string {
	if depth < 1 {
		depth = 1
	}
	parts := strings.Split(strings.Trim(subject, "/"), "/")
	out := make([]string, 0, depth)
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
		if len(out) == depth {
			break
		}
	}
	if len(out) == 0 {
		return "/"
	}
	return "/" + strings.Join(out, "/")
}

// SubjectTypeGraph builds the bipartite subject↔type graph from events,
// grouping subjects to the given prefix depth.
func SubjectTypeGraph(events []Event, depth int) MeetGraph {
	subjCount := map[string]int{}
	typeCount := map[string]int{}
	linkCount := map[string]map[string]int{}

	for _, e := range events {
		s := TopLevelSubject(e.Subject, depth)
		subjCount[s]++
		typeCount[e.Type]++
		if linkCount[s] == nil {
			linkCount[s] = map[string]int{}
		}
		linkCount[s][e.Type]++
	}

	g := MeetGraph{Events: len(events)}

	for s, c := range subjCount {
		g.Subjects = append(g.Subjects, SubjectNode{Subject: s, Count: c})
	}
	sort.Slice(g.Subjects, func(i, j int) bool {
		a, b := g.Subjects[i], g.Subjects[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Subject < b.Subject
	})

	for t, c := range typeCount {
		_, phase := Classify(t)
		g.Types = append(g.Types, TypeNode{Type: t, Count: c, Phase: phase})
	}
	sort.Slice(g.Types, func(i, j int) bool {
		a, b := g.Types[i], g.Types[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Type < b.Type
	})

	for s, tos := range linkCount {
		for t, c := range tos {
			g.Links = append(g.Links, Link{Subject: s, Type: t, Count: c})
		}
	}
	sort.Slice(g.Links, func(i, j int) bool {
		a, b := g.Links[i], g.Links[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.Type < b.Type
	})

	return g
}
