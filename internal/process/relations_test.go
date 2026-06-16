package process

import "testing"

func child(n *RelNode, seg string) *RelNode {
	for _, c := range n.Children {
		if c.Seg == seg {
			return c
		}
	}
	return nil
}

func TestIsID(t *testing.T) {
	for _, s := range []string{"123", "00000000-0000-0000-0000-000000000000", "deadbeefdeadbeef", "01ARZ3NDEKTSV4RRFFQ69G5FAV", "EMP-30000", "order_123"} {
		if !isID(s) {
			t.Errorf("isID(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"orders", "items", "user-profile", "abc", "employee-onboarding"} {
		if isID(s) {
			t.Errorf("isID(%q) = true, want false", s)
		}
	}
}

func TestBuildSubjectTree(t *testing.T) {
	subs := []string{
		"/orders/1", "/orders/1", "/orders/2",
		"/orders/1/items/9",
		"/users/7",
	}
	root := BuildSubjectTree(subs)

	orders := child(root, "orders")
	if orders == nil || orders.Events != 4 {
		t.Fatalf("orders = %+v, want Events 4", orders)
	}
	oid := child(orders, "{id}")
	if oid == nil || !oid.IsID || oid.Instances != 2 {
		t.Fatalf("orders/{id} = %+v, want IsID + 2 instances", oid)
	}
	items := child(oid, "items")
	if items == nil {
		t.Fatal("orders/{id}/items missing")
	}
	iid := child(items, "{id}")
	if iid == nil || iid.Instances != 1 {
		t.Fatalf("items/{id} = %+v, want 1 instance", iid)
	}
	users := child(root, "users")
	if users == nil || child(users, "{id}").Instances != 1 {
		t.Fatalf("users/{id} wrong: %+v", users)
	}
}
