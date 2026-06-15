package process

import (
	"sort"
	"time"
)

// TimedEvent is an event with its timestamp, for the dotted chart.
type TimedEvent struct {
	Subject string
	Type    string
	Time    string // RFC3339; empty/unparseable falls back to sequence order
}

// Dot is one event placed in the chart: a row (subject) and an X position in
// [0,1] across the time (or sequence) span.
type Dot struct {
	Row     int
	X       float64
	Type    string
	Phase   Phase
	Subject string
	Time    string
}

// DRow is a chart row: one subject with its event count.
type DRow struct {
	Subject string
	Count   int
}

// Dotted is the dotted-chart model. ByTime reports whether the X axis is real
// time (true) or sequence order (false). Shown/Total reflect row capping.
type Dotted struct {
	Rows   []DRow
	Dots   []Dot
	Events int
	ByTime bool
	Shown  int
	Total  int
}

// BuildDotted lays events out as a dotted chart. Rows are subjects sorted by
// their first event; when there are more than maxRows subjects, the busiest are
// kept. The X axis uses real timestamps when all parse, else sequence order.
func BuildDotted(events []TimedEvent, maxRows int) Dotted {
	n := len(events)
	if n == 0 {
		return Dotted{}
	}
	if maxRows < 1 {
		maxRows = 60
	}

	// Decide the axis: real time only if every timestamp parses and spans.
	useTime := true
	parsed := make([]time.Time, n)
	for i, e := range events {
		t, err := time.Parse(time.RFC3339, e.Time)
		if err != nil {
			useTime = false
		}
		parsed[i] = t
	}
	val := make([]float64, n)
	if useTime {
		for i := range events {
			val[i] = float64(parsed[i].UnixNano())
		}
		min, max := val[0], val[0]
		for _, v := range val {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
		if max <= min {
			useTime = false
		}
	}
	if !useTime {
		for i := range events {
			val[i] = float64(i)
		}
	}

	min, max := val[0], val[0]
	for _, v := range val {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	span := max - min
	if span == 0 {
		span = 1
	}

	// Per-subject aggregates.
	type agg struct {
		count int
		first float64
	}
	subs := map[string]*agg{}
	for i, e := range events {
		a := subs[e.Subject]
		if a == nil {
			a = &agg{first: val[i]}
			subs[e.Subject] = a
		}
		a.count++
		if val[i] < a.first {
			a.first = val[i]
		}
	}

	names := make([]string, 0, len(subs))
	for s := range subs {
		names = append(names, s)
	}

	// If too many subjects, keep the busiest.
	total := len(names)
	if total > maxRows {
		sort.Slice(names, func(i, j int) bool {
			if subs[names[i]].count != subs[names[j]].count {
				return subs[names[i]].count > subs[names[j]].count
			}
			return names[i] < names[j]
		})
		names = names[:maxRows]
	}

	// Final row order: by first event, then name.
	sort.Slice(names, func(i, j int) bool {
		if subs[names[i]].first != subs[names[j]].first {
			return subs[names[i]].first < subs[names[j]].first
		}
		return names[i] < names[j]
	})
	rowOf := make(map[string]int, len(names))
	d := Dotted{Events: n, ByTime: useTime, Shown: len(names), Total: total}
	for i, s := range names {
		rowOf[s] = i
		d.Rows = append(d.Rows, DRow{Subject: s, Count: subs[s].count})
	}

	for i, e := range events {
		row, ok := rowOf[e.Subject]
		if !ok {
			continue // subject capped out
		}
		_, phase := Classify(e.Type)
		d.Dots = append(d.Dots, Dot{
			Row:     row,
			X:       (val[i] - min) / span,
			Type:    e.Type,
			Phase:   phase,
			Subject: e.Subject,
			Time:    e.Time,
		})
	}
	return d
}
