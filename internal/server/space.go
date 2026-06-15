package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/pblumer/clio-workbench/internal/clio"
	"github.com/pblumer/clio-workbench/internal/process"
)

// Layout constants for the bipartite subject↔type space.
const (
	sRowH   = 92.0
	sPadY   = 60.0
	sLeftX  = 150.0
	sRightX = 760.0
)

type spaceView struct {
	State        string // ok, empty, offline, unauthorized, error
	Message      string
	W, H         float64
	Nodes        []procNode // subjects and types share the node shape
	Edges        []procEdge
	SubjectCount int
	TypeCount    int
	Events       int
}

// handleSpace reads real events and renders the "event space": the bipartite
// graph where subjects (grouped to their top-level prefix) meet the event types
// that occur on them. It emits the same .proc-graph structure as the process
// view, so the shared force engine (process.js) gives it zoom/pan/drag/hover.
func (s *Server) handleSpace(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()

	events, err := s.clio.ReadEvents(ctx, processEventCap)
	if err != nil {
		v := spaceView{}
		switch {
		case errors.Is(err, clio.ErrOffline):
			v.State, v.Message = "offline", "no Clio connected — pick a server to map subjects ↔ events"
		case errors.Is(err, clio.ErrUnauthorized):
			v.State, v.Message = "unauthorized", "Clio rejected the token"
		default:
			v.State, v.Message = "error", "could not read events from Clio"
			s.log.Warn("read events (space)", "err", err)
		}
		s.render(w, "space.html", v)
		return
	}

	in := make([]process.Event, len(events))
	for i, e := range events {
		in[i] = process.Event{Subject: e.Subject, Type: e.Type}
	}
	g := process.SubjectTypeGraph(in, 1)
	s.render(w, "space.html", buildSpaceView(g))
}

func buildSpaceView(g process.MeetGraph) spaceView {
	if len(g.Subjects) == 0 || len(g.Types) == 0 {
		return spaceView{State: "empty", Message: "Clio is connected, but there are no events to map yet.", Events: g.Events}
	}

	v := spaceView{State: "ok", SubjectCount: len(g.Subjects), TypeCount: len(g.Types), Events: g.Events}

	rows := len(g.Subjects)
	if len(g.Types) > rows {
		rows = len(g.Types)
	}
	v.W = sRightX + sLeftX
	v.H = sPadY*2 + float64(rows)*sRowH

	maxCount := 1
	for _, n := range g.Subjects {
		if n.Count > maxCount {
			maxCount = n.Count
		}
	}
	for _, n := range g.Types {
		if n.Count > maxCount {
			maxCount = n.Count
		}
	}
	radius := func(c int) float64 { return 16 + 16*float64(c)/float64(maxCount) }

	pos := map[string]*procNode{}
	column := func(items int) float64 {
		return sPadY + float64(rows-items)*sRowH/2 + sRowH/2
	}

	subjTop := column(len(g.Subjects))
	for i, sub := range g.Subjects {
		n := procNode{
			Type:  "subj:" + sub.Subject,
			Label: sub.Subject,
			Count: sub.Count,
			X:     sLeftX,
			Y:     subjTop + float64(i)*sRowH,
			R:     radius(sub.Count),
		}
		n.LabelY = n.Y + n.R + 16
		v.Nodes = append(v.Nodes, n)
		cp := n
		pos[n.Type] = &cp
	}

	typeTop := column(len(g.Types))
	for i, ty := range g.Types {
		n := procNode{
			Type:  "type:" + ty.Type,
			Label: ty.Type,
			Phase: string(ty.Phase),
			Count: ty.Count,
			X:     sRightX,
			Y:     typeTop + float64(i)*sRowH,
			R:     radius(ty.Count),
		}
		n.LabelY = n.Y + n.R + 16
		v.Nodes = append(v.Nodes, n)
		cp := n
		pos[n.Type] = &cp
	}

	maxLink := 1
	for _, l := range g.Links {
		if l.Count > maxLink {
			maxLink = l.Count
		}
	}
	for _, l := range g.Links {
		from, to := pos["subj:"+l.Subject], pos["type:"+l.Type]
		if from == nil || to == nil {
			continue
		}
		e := procEdge{From: from.Type, To: to.Type, Count: l.Count, Width: 1 + 3.0*float64(l.Count)/float64(maxLink)}
		e.D, e.LabelX, e.LabelY = edgePath(from, to)
		v.Edges = append(v.Edges, e)
	}

	return v
}
