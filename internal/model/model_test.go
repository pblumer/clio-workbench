package model

import (
	"strings"
	"testing"
)

func baseDraft() *Draft {
	return &Draft{
		ID:        "order",
		Name:      "Order",
		Kind:      KindEntity,
		Namespace: "order",
		Nodes: []Node{
			{ID: "created", Label: "created", Start: true},
			{ID: "shipped", Label: "shipped"},
		},
		Edges: []Edge{
			{ID: "e1", Type: "shipped", From: "created", To: "shipped"},
		},
	}
}

func TestValidateOK(t *testing.T) {
	if err := baseDraft().Validate(); err != nil {
		t.Fatalf("valid draft rejected: %v", err)
	}
	// Process kind and empty namespace are also valid.
	d := baseDraft()
	d.Kind = KindProcess
	d.Namespace = ""
	if err := d.Validate(); err != nil {
		t.Fatalf("process/empty-namespace draft rejected: %v", err)
	}
	// Both explicit cardinalities (and the empty default) are valid.
	for _, c := range []Cardinality{"", CardinalityOnce, CardinalityMany} {
		d := baseDraft()
		d.Edges[0].Cardinality = c
		if err := d.Validate(); err != nil {
			t.Fatalf("cardinality %q rejected: %v", c, err)
		}
	}
}

func TestValidateErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Draft)
		wantSub string
	}{
		{"invalid id", func(d *Draft) { d.ID = "Bad Id" }, "invalid draft id"},
		{"empty id", func(d *Draft) { d.ID = "" }, "invalid draft id"},
		{"empty name", func(d *Draft) { d.Name = "" }, "name must not be empty"},
		{"invalid kind", func(d *Draft) { d.Kind = "weird" }, "invalid kind"},
		{"invalid namespace", func(d *Draft) { d.Namespace = ".bad" }, "invalid namespace"},
		{"empty node id", func(d *Draft) { d.Nodes[0].ID = "" }, "node id must not be empty"},
		{"duplicate node id", func(d *Draft) { d.Nodes[1].ID = "created" }, "duplicate node id"},
		{"empty edge id", func(d *Draft) { d.Edges[0].ID = "" }, "edge id must not be empty"},
		{"duplicate edge id", func(d *Draft) {
			d.Edges = append(d.Edges, Edge{ID: "e1", From: "created", To: "shipped"})
		}, "duplicate edge id"},
		{"unknown source", func(d *Draft) { d.Edges[0].From = "ghost" }, "unknown source node"},
		{"unknown target", func(d *Draft) { d.Edges[0].To = "ghost" }, "unknown target node"},
		{"invalid cardinality", func(d *Draft) { d.Edges[0].Cardinality = "twice" }, "invalid cardinality"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := baseDraft()
			tt.mutate(d)
			err := d.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantSub)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantSub)
			}
		})
	}
}

func TestValidID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"order", true},
		{"a", true},
		{"a1", true},
		{"order-123", true},
		{"a-b-c", true},
		{"", false},
		{"-leading", false},
		{"trailing-", false},
		{"Caps", false},
		{"with space", false},
		{"under_score", false},
	}
	for _, tt := range tests {
		if got := ValidID(tt.in); got != tt.want {
			t.Errorf("ValidID(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
