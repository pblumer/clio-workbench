package process

import "testing"

func TestTopLevelSubject(t *testing.T) {
	cases := []struct {
		in    string
		depth int
		want  string
	}{
		{"/orders/123", 1, "/orders"},
		{"/orders/123/items/9", 1, "/orders"},
		{"/orders/123", 2, "/orders/123"},
		{"orders/123", 1, "/orders"},
		{"/", 1, "/"},
		{"", 1, "/"},
	}
	for _, c := range cases {
		if got := TopLevelSubject(c.in, c.depth); got != c.want {
			t.Errorf("TopLevelSubject(%q,%d) = %q, want %q", c.in, c.depth, got, c.want)
		}
	}
}

func TestSubjectTypeGraph(t *testing.T) {
	var events []Event
	events = append(events, ev("/orders/1", "placed", "shipping.failed")...)
	events = append(events, ev("/orders/2", "placed")...)
	events = append(events, ev("/users/9", "registered")...)

	g := SubjectTypeGraph(events, 1)

	if g.Events != 4 {
		t.Errorf("events = %d, want 4", g.Events)
	}
	if len(g.Subjects) != 2 {
		t.Fatalf("subjects = %d, want 2 (/orders,/users)", len(g.Subjects))
	}
	if g.Subjects[0].Subject != "/orders" || g.Subjects[0].Count != 3 {
		t.Errorf("top subject = %+v, want /orders x3", g.Subjects[0])
	}

	link := func(s, ty string) int {
		for _, l := range g.Links {
			if l.Subject == s && l.Type == ty {
				return l.Count
			}
		}
		return 0
	}
	if link("/orders", "placed") != 2 {
		t.Errorf("/orders→placed = %d, want 2", link("/orders", "placed"))
	}
	if link("/users", "registered") != 1 {
		t.Errorf("/users→registered = %d, want 1", link("/users", "registered"))
	}

	// shipping.failed must carry the error phase.
	for _, ty := range g.Types {
		if ty.Type == "shipping.failed" && ty.Phase != PhaseError {
			t.Errorf("shipping.failed phase = %s, want error", ty.Phase)
		}
	}
}
