package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/process"
)

// Bounds for reading events and listing variants, so a large store stays light.
const (
	processEventCap   = 5000
	processMaxVariant = 8
)

// Layout constants for the server-side SVG graph (left-to-right by rank).
const (
	pColW = 190.0
	pRowH = 104.0
	pPadX = 70.0
	pPadY = 56.0
)

type procNode struct {
	Type           string
	Label          string
	Task           string
	Phase          string
	Count          int
	StartCount     int
	EndCount       int
	X, Y, R        float64
	LabelY         float64
	StartMX, EndMX float64
	Start, End     bool
}

// procGroup is a translucent backdrop around the nodes of one task (only drawn
// when a task spans more than one event type).
type procGroup struct {
	X, Y, W, H float64
	Label      string
}

type procEdge struct {
	From, To       string
	D              string
	LabelX, LabelY float64
	Count          int
	Width          float64
}

type procVariant struct {
	Sequence []string
	Count    int
	Pct      int
}

type processView struct {
	State    string // ok, empty, offline, unauthorized, error
	Message  string
	W, H     float64
	Groups   []procGroup
	Nodes    []procNode
	Edges    []procEdge
	Variants []procVariant
	Subjects int
	Events   int
}

// handleProcess discovers the process from real Clio events and renders the
// directly-follows graph plus the top variants.
func (s *Server) handleProcess(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()

	events, err := s.clio.ReadEvents(ctx, processEventCap)
	if err != nil {
		v := processView{}
		switch {
		case errors.Is(err, clio.ErrOffline):
			v.State, v.Message = "offline", "no Clio connected — pick a server to discover its process"
		case errors.Is(err, clio.ErrUnauthorized):
			v.State, v.Message = "unauthorized", "Clio rejected the token"
		default:
			v.State, v.Message = "error", "could not read events from Clio"
			s.log.Warn("read events", "err", err)
		}
		s.render(w, "process.html", v)
		return
	}

	in := make([]process.Event, len(events))
	for i, e := range events {
		in[i] = process.Event{Subject: e.Subject, Type: e.Type}
	}
	g := process.Discover(in, processMaxVariant)
	s.render(w, "process.html", buildProcessView(g))
}

// buildProcessView turns the discovered graph into a laid-out SVG view model.
func buildProcessView(g process.Graph) processView {
	if len(g.Nodes) == 0 {
		return processView{State: "empty", Message: "Clio is connected, but no events have been written yet.", Subjects: g.Subjects, Events: g.Events}
	}

	v := processView{State: "ok", Subjects: g.Subjects, Events: g.Events}

	// Group nodes by rank (g.Nodes is already sorted by rank then count).
	byRank := map[int][]process.Node{}
	maxRank, maxRows, maxCount := 0, 0, 1
	for _, n := range g.Nodes {
		byRank[n.Rank] = append(byRank[n.Rank], n)
		if n.Rank > maxRank {
			maxRank = n.Rank
		}
		if l := len(byRank[n.Rank]); l > maxRows {
			maxRows = l
		}
		if n.Count > maxCount {
			maxCount = n.Count
		}
	}

	v.W = pPadX*2 + float64(maxRank)*pColW
	v.H = pPadY*2 + float64(maxRows)*pRowH

	pos := map[string]*procNode{}
	for rank := 0; rank <= maxRank; rank++ {
		col := byRank[rank]
		colTop := pPadY + float64(maxRows-len(col))*pRowH/2 + pRowH/2
		for i, n := range col {
			pn := procNode{
				Type:       n.Type,
				Task:       n.Task,
				Phase:      string(n.Phase),
				Count:      n.Count,
				StartCount: n.StartCount,
				EndCount:   n.EndCount,
				X:          pPadX + float64(rank)*pColW,
				Y:          colTop + float64(i)*pRowH,
				R:          18 + 16*float64(n.Count)/float64(maxCount),
				Start:      n.StartCount > 0,
				End:        n.EndCount > 0,
			}
			pn.LabelY = pn.Y + pn.R + 17
			pn.StartMX = pn.X - pn.R - 14
			pn.EndMX = pn.X + pn.R + 14
			v.Nodes = append(v.Nodes, pn)
			cp := pn
			pos[n.Type] = &cp
		}
	}

	v.Groups = taskGroups(v.Nodes)

	maxEdge := 1
	for _, e := range g.Edges {
		if e.Count > maxEdge {
			maxEdge = e.Count
		}
	}
	for _, e := range g.Edges {
		from, to := pos[e.From], pos[e.To]
		if from == nil || to == nil {
			continue
		}
		pe := procEdge{From: e.From, To: e.To, Count: e.Count, Width: 1.2 + 3.0*float64(e.Count)/float64(maxEdge)}
		pe.D, pe.LabelX, pe.LabelY = edgePath(from, to)
		v.Edges = append(v.Edges, pe)
	}

	for _, va := range g.Variants {
		pct := 0
		if g.Subjects > 0 {
			pct = va.Count * 100 / g.Subjects
		}
		v.Variants = append(v.Variants, procVariant{Sequence: va.Sequence, Count: va.Count, Pct: pct})
	}
	return v
}

// taskGroups computes a backdrop box around the nodes of each task that spans
// more than one event type, so lifecycle siblings read as one task.
func taskGroups(nodes []procNode) []procGroup {
	byTask := map[string][]procNode{}
	for _, n := range nodes {
		byTask[n.Task] = append(byTask[n.Task], n)
	}
	tasks := make([]string, 0, len(byTask))
	for t := range byTask {
		tasks = append(tasks, t)
	}
	sort.Strings(tasks)

	var groups []procGroup
	const pad, labelGap = 16.0, 14.0
	for _, t := range tasks {
		ns := byTask[t]
		if len(ns) < 2 {
			continue
		}
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, n := range ns {
			minX = math.Min(minX, n.X-n.R)
			minY = math.Min(minY, n.Y-n.R)
			maxX = math.Max(maxX, n.X+n.R)
			maxY = math.Max(maxY, n.Y+n.R)
		}
		groups = append(groups, procGroup{
			X:     minX - pad,
			Y:     minY - pad - labelGap,
			W:     (maxX - minX) + 2*pad,
			H:     (maxY - minY) + 2*pad + labelGap,
			Label: t,
		})
	}
	return groups
}

// edgePath builds a cubic-bezier path from one node to another and the position
// for its count label. Forward edges curve right→left; self-loops loop above;
// back/same-rank edges arc below to stay legible.
func edgePath(from, to *procNode) (d string, lx, ly float64) {
	if from.Type == to.Type {
		x, y := from.X, from.Y-from.R
		d = fmt.Sprintf("M%.1f %.1f C%.1f %.1f %.1f %.1f %.1f %.1f",
			x-9, y, x-46, y-58, x+46, y-58, x+9, y)
		return d, x, y - 50
	}
	dx := to.X - from.X
	if dx > 0 { // forward
		x1, y1 := from.X+from.R, from.Y
		x2, y2 := to.X-to.R, to.Y
		d = fmt.Sprintf("M%.1f %.1f C%.1f %.1f %.1f %.1f %.1f %.1f",
			x1, y1, x1+dx*0.4, y1, x2-dx*0.4, y2, x2, y2)
		return d, (x1 + x2) / 2, (y1+y2)/2 - 8
	}
	// back or same-rank: arc below both nodes
	x1, y1 := from.X, from.Y+from.R
	x2, y2 := to.X, to.Y+to.R
	dip := 64.0
	d = fmt.Sprintf("M%.1f %.1f C%.1f %.1f %.1f %.1f %.1f %.1f",
		x1, y1, x1, y1+dip, x2, y2+dip, x2, y2)
	mid := y1
	if y2 > mid {
		mid = y2
	}
	return d, (x1 + x2) / 2, mid + dip - 6
}
