// Package process turns a flat list of events into a discovered process: the
// directly-follows graph (DFG) plus the distinct variants (traces).
//
// This is the analytic heart of the Gegenprobe (docs/WORKBENCH.md §7). For each
// subject we take the ordered sequence of event types it received; consecutive
// pairs (A → B) become weighted edges, the first/last types become start/end
// markers, and each whole sequence is a variant whose frequency we count.
//
// On top of the raw DFG we detect concurrency: when two activities run in
// parallel their relative order is arbitrary, so a pure DFG records every
// interleaving as a separate thin edge and every reordering as a separate
// variant — a "spaghetti" of look-alike paths. detectConcurrency recognises
// such pairs (seen in BOTH directions with balanced counts) and groups them, so
// the view can collapse the interleavings: one parallel block instead of a
// near-complete subgraph, one variant instead of a factorial of them.
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

// Phase is a BPMN task lifecycle phase inferred from an event type. A task
// typically emits an event when it becomes active, one when it completes, and
// possibly ones for errors or other information.
type Phase string

const (
	PhaseActive   Phase = "active"
	PhaseComplete Phase = "complete"
	PhaseError    Phase = "error"
	PhaseInfo     Phase = "info"
)

// Lifecycle suffix vocabularies for the default convention. They match the last
// dot/dash/underscore segment of an event type (e.g. "shipping.failed" → error,
// task "shipping"). This is a sensible default, meant to be overridable later.
var (
	errorWords    = words("failed failure error errored rejected denied cancelled canceled aborted timeout timedout expired declined")
	activeWords   = words("started starting start initiated requested opened began begun resumed active created submitted queued scheduled")
	completeWords = words("completed complete finished done succeeded success closed ended fulfilled")
	infoWords     = words("updated changed modified noted logged progress progressed status info recorded observed")
)

func words(s string) map[string]bool {
	m := make(map[string]bool)
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// Classify infers a task name and lifecycle phase from an event type using the
// default convention: the trailing segment is a lifecycle marker and the rest
// is the task. Event types without a recognised marker stand alone and are
// treated as a completed fact.
func Classify(eventType string) (task string, phase Phase) {
	lower := strings.ToLower(eventType)
	idx := strings.LastIndexAny(lower, ".-_/")
	if idx > 0 && idx < len(lower)-1 {
		suffix := lower[idx+1:]
		prefix := eventType[:idx]
		switch {
		case errorWords[suffix]:
			return prefix, PhaseError
		case activeWords[suffix]:
			return prefix, PhaseActive
		case completeWords[suffix]:
			return prefix, PhaseComplete
		case infoWords[suffix]:
			return prefix, PhaseInfo
		}
	}
	// No lifecycle marker: a standalone domain fact.
	if errorWords[lower] {
		return eventType, PhaseError
	}
	return eventType, PhaseComplete
}

// Node is an event type with how often it occurred and how often it started or
// ended a subject's sequence. Rank is its column in a left-to-right layout
// (longest path from a start node). Task/Phase are the inferred lifecycle.
type Node struct {
	Type       string
	Task       string
	Phase      Phase
	Count      int
	StartCount int
	EndCount   int
	Rank       int
}

// Edge is a directly-follows transition A → B with how often it was observed.
// Parallel marks the edge as living inside a concurrent block (its endpoints run
// in parallel) — such edges are an artefact of interleaving, not a real step, so
// the view collapses them into the block instead of drawing them.
type Edge struct {
	From     string
	To       string
	Count    int
	Parallel bool
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
	// Concurrent lists the maximal groups of event types that run in parallel
	// (each group has ≥2 members, members sorted). The interleavings of a group
	// are folded into a single variant and the view draws it as one block.
	Concurrent [][]string
	Subjects   int // distinct subjects observed
	Events     int // events fed in
	Traces     int // process runs (subjects split at restart boundaries)
}

// Concurrency tuning. A pair of types is treated as parallel when it occurs in
// BOTH directions, each direction at least concurrencyMinSupport times, and the
// Heuristics-Miner dependency measure |fwd-rev| / (fwd+rev+1) stays at or below
// concurrencyDepMax (≈ within a 3:1 ratio). A strongly one-sided pair (e.g.
// 800 vs 5) stays sequential — the rare reverse is read as noise, not parallelism.
const (
	concurrencyDepMax     = 0.5
	concurrencyMinSupport = 2
)

// splitRuns splits a subject's event sequence into separate runs at every
// end→start boundary (an end-type event immediately followed by a different
// start-type event = a restart on a reused subject).
func splitRuns(seq []string, startType, endType map[string]bool) [][]string {
	if len(seq) == 0 {
		return nil
	}
	var runs [][]string
	start := 0
	for i := 1; i < len(seq); i++ {
		if seq[i-1] != seq[i] && endType[seq[i-1]] && startType[seq[i]] {
			runs = append(runs, seq[start:i])
			start = i
		}
	}
	return append(runs, seq[start:])
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
			n.Task, n.Phase = Classify(t)
			nodes[t] = n
		}
		return n
	}
	startType := make(map[string]bool)
	endType := make(map[string]bool)

	// Pass 1: event counts and the set of start/end types, from the raw
	// per-subject sequences.
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
		startType[seq[0]] = true
		endType[seq[len(seq)-1]] = true
	}

	// Pass 2: split each subject's sequence into runs at end→start boundaries
	// (a reused subject = several runs), then derive edges from the runs. So a
	// restart (e.g. deployed → new.v2) is never a step. The runs are kept so
	// variants can be built in a second step, once concurrency is known.
	edgeCount := make(map[string]map[string]int)
	var runs [][]string
	traces := 0
	for _, subj := range order {
		for _, run := range splitRuns(seqs[subj], startType, endType) {
			traces++
			for i := 0; i+1 < len(run); i++ {
				from, to := run[i], run[i+1]
				if edgeCount[from] == nil {
					edgeCount[from] = make(map[string]int)
				}
				edgeCount[from][to]++
			}
			runs = append(runs, run)
		}
	}

	// Concurrency: which type pairs run in parallel and the maximal groups they
	// form. groupOf maps a type to its group index for the canonicalisation below.
	parallel, groups := detectConcurrency(edgeCount)
	groupOf := make(map[string]int, len(parallel))
	for gi, grp := range groups {
		for _, t := range grp {
			groupOf[t] = gi
		}
	}

	// Variants from the runs, each concurrent group canonicalised so the many
	// interleavings of parallel activities collapse into ONE variant instead of
	// inflating into a factorial of look-alike traces.
	variantCount := make(map[string]int)
	variantSeq := make(map[string][]string)
	for _, run := range runs {
		cr := canonicalizeRun(run, groupOf)
		key := strings.Join(cr, " ")
		variantCount[key]++
		if _, ok := variantSeq[key]; !ok {
			variantSeq[key] = cr
		}
	}

	g := Graph{Subjects: len(order), Events: len(events), Traces: traces, Concurrent: groups}

	// Edges, sorted for stable output (by count desc, then names). Within-block
	// pairs are flagged Parallel so the view can fold them into the block.
	for from, tos := range edgeCount {
		for to, c := range tos {
			g.Edges = append(g.Edges, Edge{From: from, To: to, Count: c, Parallel: parallel[from][to]})
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

	// Ranking ignores within-block (parallel) edges so concurrent activities
	// share a column instead of being strung out left-to-right by their spurious
	// orderings.
	structural := make(map[string]map[string]int, len(edgeCount))
	for from, tos := range edgeCount {
		for to, c := range tos {
			if parallel[from][to] {
				continue
			}
			if structural[from] == nil {
				structural[from] = make(map[string]int)
			}
			structural[from][to] = c
		}
	}
	assignRanks(nodes, structural)

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

// detectConcurrency classifies each unordered pair of types as sequential or
// parallel from the directly-follows counts, using the Heuristics-Miner
// dependency measure dep = |fwd-rev| / (fwd+rev+1): a pair seen in BOTH
// directions with a balanced count (low dependency) is concurrent — neither
// reliably precedes the other. It returns the symmetric parallel relation and
// the maximal concurrent groups (connected components of that relation, ≥2
// members). Connected components, not cliques, is a deliberate first cut: it is
// linear and robust; in the wild, interleaved activities tend to form a clique
// anyway. (Pairing on a substring of a near-clique can over-group; refining to
// cliques is left for later, see docs/WORKBENCH.md §7.)
func detectConcurrency(edges map[string]map[string]int) (parallel map[string]map[string]bool, groups [][]string) {
	types := map[string]bool{}
	for a, tos := range edges {
		types[a] = true
		for b := range tos {
			types[b] = true
		}
	}
	list := make([]string, 0, len(types))
	for t := range types {
		list = append(list, t)
	}
	sort.Strings(list)

	parallel = map[string]map[string]bool{}
	link := func(a, b string) {
		if parallel[a] == nil {
			parallel[a] = map[string]bool{}
		}
		if parallel[b] == nil {
			parallel[b] = map[string]bool{}
		}
		parallel[a][b] = true
		parallel[b][a] = true
	}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			a, b := list[i], list[j]
			fwd, rev := edges[a][b], edges[b][a]
			if fwd == 0 || rev == 0 {
				continue
			}
			lo := fwd
			if rev < lo {
				lo = rev
			}
			if lo < concurrencyMinSupport {
				continue
			}
			diff := fwd - rev
			if diff < 0 {
				diff = -diff
			}
			if float64(diff)/float64(fwd+rev+1) <= concurrencyDepMax {
				link(a, b)
			}
		}
	}

	// Connected components over the parallel relation (deterministic order).
	seen := map[string]bool{}
	for _, t := range list {
		if seen[t] || len(parallel[t]) == 0 {
			continue
		}
		var comp []string
		queue := []string{t}
		seen[t] = true
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			comp = append(comp, cur)
			nbrs := make([]string, 0, len(parallel[cur]))
			for n := range parallel[cur] {
				nbrs = append(nbrs, n)
			}
			sort.Strings(nbrs)
			for _, n := range nbrs {
				if !seen[n] {
					seen[n] = true
					queue = append(queue, n)
				}
			}
		}
		if len(comp) >= 2 {
			sort.Strings(comp)
			groups = append(groups, comp)
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i][0] < groups[j][0] })
	return parallel, groups
}

// canonicalizeRun collapses the interleavings of a concurrent group into one
// canonical order: any maximal stretch of consecutive events all belonging to
// the same group is sorted. So three parallel activities, observed in any order,
// yield the same variant. groupOf maps a type to its group index (absent = not
// concurrent). The run is copied; the input is left untouched.
func canonicalizeRun(run []string, groupOf map[string]int) []string {
	if len(run) < 2 || len(groupOf) == 0 {
		return run
	}
	out := make([]string, len(run))
	copy(out, run)
	for i := 0; i < len(out); {
		g, ok := groupOf[out[i]]
		if !ok {
			i++
			continue
		}
		j := i + 1
		for j < len(out) {
			if gj, ok := groupOf[out[j]]; !ok || gj != g {
				break
			}
			j++
		}
		if j-i > 1 {
			sort.Strings(out[i:j])
		}
		i = j
	}
	return out
}
