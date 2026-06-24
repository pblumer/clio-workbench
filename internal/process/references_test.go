package process

import (
	"encoding/json"
	"testing"
)

func TestReferenceCollection(t *testing.T) {
	cases := []struct {
		field string
		want  string
		ok    bool
	}{
		{"employeeId", "employees", true},
		{"customerID", "customers", true},
		{"tagIds", "tags", true},
		{"productRef", "products", true},
		{"managerRefs", "managers", true},
		{"orderId", "orders", true},
		{"id", "", false},   // the entity's own id is never a reference
		{"ID", "", false},   // case-insensitive
		{"name", "", false}, // no fk-like suffix
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := ReferenceCollection(c.field)
		if ok != c.ok || got != c.want {
			t.Errorf("ReferenceCollection(%q) = (%q, %v), want (%q, %v)",
				c.field, got, ok, c.want, c.ok)
		}
	}
}

func refEdge(g RefGraph, from, to string) *RefEdge {
	for i := range g.Edges {
		if g.Edges[i].From == from && g.Edges[i].To == to {
			return &g.Edges[i]
		}
	}
	return nil
}

func TestBuildReferencesScalarAndArray(t *testing.T) {
	events := []RefEvent{
		// orders reference a customer (scalar FK → n:1) — twice, same target collection.
		{Subject: "/orders/1", Type: "order-placed", Data: json.RawMessage(`{"customerId":"c1","amount":5}`)},
		{Subject: "/orders/2", Type: "order-placed", Data: json.RawMessage(`{"customerId":"c2"}`)},
		// a customer exists as a collection too
		{Subject: "/customers/c1", Type: "customer-created", Data: json.RawMessage(`{}`)},
		// array FK → 1:n
		{Subject: "/orders/1", Type: "order-tagged", Data: json.RawMessage(`{"tagIds":["a","b"]}`)},
	}
	g := BuildReferences(events)

	e := refEdge(g, "orders", "customers")
	if e == nil || e.Kind != "n:1" || e.Count != 2 {
		t.Fatalf("orders→customers = %+v, want n:1 count 2", e)
	}
	if e.Via != "customerId" {
		t.Errorf("via = %q, want customerId", e.Via)
	}
	// "tagIds" → stem "tag"; unresolved target keeps the stem as-is.
	ta := refEdge(g, "orders", "tag")
	if ta == nil || ta.Kind != "1:n" {
		t.Fatalf("orders→tag = %+v, want 1:n (array)", ta)
	}
	// customers is a known collection, tag is not.
	for _, n := range g.Nodes {
		if n.Name == "customers" && !n.Known {
			t.Error("customers should be Known")
		}
		if n.Name == "tag" && n.Known {
			t.Error("tag should be unknown (no /tag subjects)")
		}
	}
}

func TestBuildReferencesAssociationNM(t *testing.T) {
	events := []RefEvent{
		{Subject: "/orders/1", Type: "order-created", Data: json.RawMessage(`{}`)},
		{Subject: "/products/9", Type: "product-created", Data: json.RawMessage(`{}`)},
		// association event with two FKs → n:m between orders and products
		{Subject: "/links/1", Type: "order-product-linked", Data: json.RawMessage(`{"orderId":"1","productId":"9"}`)},
	}
	g := BuildReferences(events)
	e := refEdge(g, "orders", "products")
	if e == nil || e.Kind != "n:m" {
		t.Fatalf("orders↔products = %+v, want n:m", e)
	}
	if e.Via != "order-product-linked" {
		t.Errorf("via = %q, want the association event type", e.Via)
	}
}
