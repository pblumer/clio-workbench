package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/process"
)

// processMaxVariant bounds the number of distinct variants listed.
const processMaxVariant = 8

// Layout constants for the server-side SVG graph (left-to-right by rank). Kept
// roomy so the long event-type labels under each node have space and don't
// overlap — both in the no-JS fallback and as the viewBox the live force
// layout (process.js) spreads into.
const (
	pColW = 330.0
	pRowH = 150.0
	pPadX = 90.0
	pPadY = 64.0
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
	// Truncated reports the read hit the event cap (Cap), so older events only.
	Truncated bool
	Cap       int
	// Subject is the active subject-prefix filter (empty = all).
	Subject string
	// Source is the active source substring filter (empty = all).
	Source string
	// ReplayJSON is the ordered event stream for the client-side timeline
	// replay ([{s,t,ts}, ...]).
	ReplayJSON template.JS
}

// handleProcess discovers the process from real Clio events and renders the
// directly-follows graph plus the top variants.
func (s *Server) handleProcess(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()

	subject := strings.TrimSpace(r.URL.Query().Get("subject"))
	source := strings.TrimSpace(r.URL.Query().Get("source"))

	sc := s.activeScope()
	events, err := s.clio.ReadScoped(ctx, sc)
	truncated := err == nil && len(events) >= sc.Limit
	if err != nil {
		v := processView{Subject: subject, Source: source}
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

	// Optional filters: subject is a path prefix, source a substring match.
	if subject != "" || source != "" {
		f := make([]clio.Event, 0, len(events))
		for _, e := range events {
			if subject != "" && !strings.HasPrefix(e.Subject, subject) {
				continue
			}
			if source != "" && !strings.Contains(e.Source, source) {
				continue
			}
			f = append(f, e)
		}
		events = f
	}

	in := make([]process.Event, len(events))
	for i, e := range events {
		in[i] = process.Event{Subject: e.Subject, Type: e.Type}
	}
	g := process.Discover(in, processMaxVariant)
	v := buildProcessView(g)
	v.Subject = subject
	v.Source = source
	v.Truncated = truncated
	v.Cap = sc.Limit
	v.ReplayJSON = replayJSON(events)
	s.render(w, "process.html", v)
}

// replayJSON marshals the ordered events for the timeline replay. encoding/json
// escapes <, >, & so the payload is safe inside a <script> element.
func replayJSON(events []clio.Event) template.JS {
	type rep struct {
		S  string `json:"s"`
		T  string `json:"t"`
		Ts string `json:"ts"`
	}
	arr := make([]rep, len(events))
	for i, e := range events {
		arr[i] = rep{S: e.Subject, T: e.Type, Ts: e.Time}
	}
	b, err := json.Marshal(arr)
	if err != nil {
		return template.JS("[]")
	}
	return template.JS(b)
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
	hasEdge := map[string]bool{}
	for _, e := range g.Edges {
		if e.Count > maxEdge {
			maxEdge = e.Count
		}
		hasEdge[e.From+" -> "+e.To] = true
	}
	for _, e := range g.Edges {
		from, to := pos[e.From], pos[e.To]
		if from == nil || to == nil {
			continue
		}
		// Bow a pair of opposite edges apart so both stay legible.
		bend := e.From != e.To && hasEdge[e.To+" -> "+e.From]
		pe := procEdge{From: e.From, To: e.To, Count: e.Count, Width: 1.2 + 3.0*float64(e.Count)/float64(maxEdge)}
		pe.D, pe.LabelX, pe.LabelY = edgePath(from, to, bend)
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

// edgePath builds a path from one node to another and the position for its count
// label. Edges attach to the node boundary along the straight line between the
// two centres, so they leave and enter pointing at each other (no fixed
// left/right stubs that bow). A single edge is a straight line; when an opposite
// edge also exists (bend), it bows gently to one side so both stay legible.
// Self-loops loop above the node.
func edgePath(from, to *procNode, bend bool) (d string, lx, ly float64) {
	if from.Type == to.Type {
		x, y := from.X, from.Y-from.R
		d = fmt.Sprintf("M%.1f %.1f C%.1f %.1f %.1f %.1f %.1f %.1f",
			x-9, y, x-46, y-58, x+46, y-58, x+9, y)
		return d, x, y - 50
	}
	dx, dy := to.X-from.X, to.Y-from.Y
	dist := math.Hypot(dx, dy)
	if dist == 0 {
		dist = 0.01
	}
	ux, uy := dx/dist, dy/dist // unit vector from → to
	px, py := -uy, ux          // left-hand normal
	x1, y1 := from.X+ux*from.R, from.Y+uy*from.R
	x2, y2 := to.X-ux*to.R, to.Y-uy*to.R
	off := 0.0
	if bend {
		off = math.Min(dist*0.18, 48)
	}
	mx, my := (x1+x2)/2+px*off, (y1+y2)/2+py*off
	d = fmt.Sprintf("M%.1f %.1f Q%.1f %.1f %.1f %.1f", x1, y1, mx, my, x2, y2)
	loff := off*0.5 + 9
	return d, (x1+x2)/2 + px*loff, (y1+y2)/2 + py*loff
}
