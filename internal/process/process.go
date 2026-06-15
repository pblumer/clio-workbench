// Package process turns a flat list of events into a discovered process: the
// directly-follows graph (DFG) plus the distinct variants (traces).
//
// This is the analytic heart of the Gegenprobe (docs/WORKBENCH.md §7). For each
// subject we take the ordered sequence of event types it received; consecutive
// pairs (A → B) become weighted edges, the first/last types become start/end
// markers, and each whole sequence is a variant whose frequency we count.
package process

import (
	"sort"
	"strings"
)

// Event is the minimal input: which type landed on which subject. Input order
// is assumed chronological (Clio returns events in monotone-id order).
type Event struct {
	Subject string
	Type    string
}

// Node is an event type with how often it occurred and how often it started or
// ended a subject's sequence. Rank is its column in a left-to-right layout
// (longest path from a start node).
type Node struct {
	Type       string
	Count      int
	StartCount int
	EndCount   int
	Rank       int
}

// Edge is a directly-follows transition A → B with how often it was observed.
type Edge struct {
	From  string
	To    string
	Count int
}

// Variant is a distinct full type-sequence (trace) and how many subjects
// followed it.
type Variant struct {
	Sequence []string
	Count    int
}

// Graph is the discovered process.
type Graph struct {
	Nodes    []Node
	Edges    []Edge
	Variants []Variant
	Subjects int // distinct subjects observed
	Events   int // events fed in
}

// Discover builds the process graph from events. maxVariants caps the returned
// variant list (<= 0 means all).
func Discover(events []Event, maxVariants int) Graph {
	// Preserve per-subject encounter order.
	order := make([]string, 0)
	seqs := make(map[string][]string)
	for _, e := range events {
		if _, ok := seqs[e.Subject]; !ok {
			order = append(order, e.Subject)
		}
		seqs[e.Subject] = append(seqs[e.Subject], e.Type)
	}

	nodes := make(map[string]*Node)
	node := func(t string) *Node {
		n := nodes[t]
		if n == nil {
			n = &Node{Type: t}
			nodes[t] = n
		}
		return n
	}
	edgeCount := make(map[string]map[string]int)
	variantCount := make(map[string]int)
	variantSeq := make(map[string][]string)

	for _, subj := range order {
		seq := seqs[subj]
		if len(seq) == 0 {
			continue
		}
		for _, t := range seq {
			node(t).Count++
		}
		node(seq[0]).StartCount++
		node(seq[len(seq)-1]).EndCount++
		for i := 0; i+1 < len(seq); i++ {
			from, to := seq[i], seq[i+1]
			if edgeCount[from] == nil {
				edgeCount[from] = make(map[string]int)
			}
			edgeCount[from][to]++
		}
		key := strings.Join(seq, " ")
		variantCount[key]++
		if _, ok := variantSeq[key]; !ok {
			variantSeq[key] = seq
		}
	}

	g := Graph{Subjects: len(order), Events: len(events)}

	// Edges, sorted for stable output (by count desc, then names).
	for from, tos := range edgeCount {
		for to, c := range tos {
			g.Edges = append(g.Edges, Edge{From: from, To: to, Count: c})
		}
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

	assignRanks(nodes, edgeCount)

	for _, n := range nodes {
		g.Nodes = append(g.Nodes, *n)
	}
	sort.Slice(g.Nodes, func(i, j int) bool {
		a, b := g.Nodes[i], g.Nodes[j]
		if a.Rank != b.Rank {
			return a.Rank < b.Rank
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Type < b.Type
	})

	// Variants, sorted by frequency then sequence.
	for key, c := range variantCount {
		g.Variants = append(g.Variants, Variant{Sequence: variantSeq[key], Count: c})
	}
	sort.Slice(g.Variants, func(i, j int) bool {
		a, b := g.Variants[i], g.Variants[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return strings.Join(a.Sequence, " ") < strings.Join(b.Sequence, " ")
	})
	if maxVariants > 0 && len(g.Variants) > maxVariants {
		g.Variants = g.Variants[:maxVariants]
	}

	return g
}

// assignRanks sets Node.Rank to the longest path from a start node, walking
// forward along edges. Start nodes are pinned to rank 0; back-edges (nodes
// already on the DFS stack) are skipped so cycles cannot diverge.
func assignRanks(nodes map[string]*Node, edges map[string]map[string]int) {
	rank := make(map[string]int)
	onStack := make(map[string]bool)

	var visit func(t string, depth int)
	visit = func(t string, depth int) {
		if depth > rank[t] {
			rank[t] = depth
		}
		onStack[t] = true
		for to := range edges[t] {
			if onStack[to] {
				continue // back-edge
			}
			if depth+1 > rank[to] {
				visit(to, depth+1)
			}
		}
		onStack[t] = false
	}

	// Start from every start node (deterministic order).
	starts := make([]string, 0)
	for t, n := range nodes {
		if n.StartCount > 0 {
			starts = append(starts, t)
		}
	}
	sort.Strings(starts)
	for _, s := range starts {
		visit(s, 0)
	}

	for t, n := range nodes {
		n.Rank = rank[t]
	}
}
