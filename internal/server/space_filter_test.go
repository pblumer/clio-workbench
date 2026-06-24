package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/pblumer/clio-workbench/internal/clio"
)

func TestParseSpaceFilter(t *testing.T) {
	f := parseSpaceFilter("subject:/orders type:created,shipped from:001 to:099 foo Bar")
	if f.lens.Subject != "/orders" {
		t.Errorf("subject = %q", f.lens.Subject)
	}
	if !reflect.DeepEqual(f.lens.Types, []string{"created", "shipped"}) {
		t.Errorf("types = %v", f.lens.Types)
	}
	if f.lens.LowerBound != "001" || f.lens.UpperBound != "099" {
		t.Errorf("bounds = %q..%q", f.lens.LowerBound, f.lens.UpperBound)
	}
	// Bare tokens become lower-cased needles.
	if !reflect.DeepEqual(f.needles, []string{"foo", "bar"}) {
		t.Errorf("needles = %v", f.needles)
	}
	if f.empty() {
		t.Errorf("filter should not be empty")
	}
	if !parseSpaceFilter("   ").empty() {
		t.Errorf("blank filter should be empty")
	}
}

func TestSpaceFilterMatch(t *testing.T) {
	ek := func(subject, typ, id string) eventKey {
		return eventKey{Subject: subject, Type: typ, ID: id}
	}

	// type pin: only the exact type survives.
	f := parseSpaceFilter("type:created")
	if !f.match(ek("/orders/1", "created", "002")) {
		t.Errorf("created should match a type:created filter")
	}
	if f.match(ek("/orders/1", "shipped", "002")) {
		t.Errorf("shipped should not match a type:created filter")
	}

	// source substring (the dimension shared with the Queries layer).
	src := parseSpaceFilter("source:checkout")
	if !src.match(eventKey{Subject: "/orders/1", Type: "created", ID: "1", Source: "checkout-svc"}) {
		t.Errorf("source substring should match")
	}
	if src.match(eventKey{Subject: "/orders/1", Type: "created", ID: "1", Source: "billing"}) {
		t.Errorf("source substring should reject a non-match")
	}

	// free-text needle: matches type OR subject by substring, case-insensitive.
	n := parseSpaceFilter("ORDER")
	if !n.match(ek("/orders/1", "login", "001")) {
		t.Errorf("needle should match against the subject")
	}
	if !n.match(ek("/users/9", "order.created", "001")) {
		t.Errorf("needle should match against the type")
	}
	if n.match(ek("/users/9", "login", "001")) {
		t.Errorf("needle should reject a non-matching event")
	}

	// multiple needles must all hold.
	m := parseSpaceFilter("order created")
	if !m.match(ek("/orders/1", "created", "001")) {
		t.Errorf("both needles satisfied → match")
	}
	if m.match(ek("/orders/1", "shipped", "001")) {
		t.Errorf("one needle missing → no match")
	}
}

func TestSpaceFilterToggleAndString(t *testing.T) {
	f := parseSpaceFilter("subject:/orders type:created foo")

	// Toggling an absent type adds it; toggling a present one removes it.
	added := f.withTypeToggled("shipped")
	if !added.hasType("shipped") {
		t.Errorf("shipped should be pinned after toggle-on")
	}
	removed := added.withTypeToggled("created")
	if removed.hasType("created") {
		t.Errorf("created should be gone after toggle-off")
	}

	// String round-trips back through the parser unchanged.
	got := f.String()
	if got != "subject:/orders type:created foo" {
		t.Errorf("String() = %q", got)
	}
	if rt := parseSpaceFilter(got).String(); rt != got {
		t.Errorf("round-trip drift: %q -> %q", got, rt)
	}

	// The original filter is not mutated by a toggle (slice copy).
	if !f.hasType("created") || f.hasType("shipped") {
		t.Errorf("withTypeToggled mutated the receiver: %v", f.lens.Types)
	}
}

// The "show none" selection (type:-) is an explicit empty whitelist: it parses
// back, rejects every event, and round-trips through String().
func TestSpaceFilterShowNone(t *testing.T) {
	f := parseSpaceFilter("type:-")
	if !f.showsNoTypes() {
		t.Fatalf("type:- should be an explicit show-none selection")
	}
	if f.empty() {
		t.Errorf("show-none is a real constraint, not empty")
	}
	if !f.typeSelectionActive() {
		t.Errorf("show-none should count as an active type selection")
	}
	if f.match(eventKey{Subject: "/orders/1", Type: "created", ID: "001"}) {
		t.Errorf("show-none must reject every event")
	}
	if got := f.String(); got != "type:-" {
		t.Errorf("String() = %q, want type:-", got)
	}

	// A concrete pin wins over a contradictory show-none token.
	mixed := parseSpaceFilter("type:- type:created")
	if mixed.showsNoTypes() {
		t.Errorf("a pinned type should override show-none")
	}
	if !mixed.match(eventKey{Type: "created"}) {
		t.Errorf("the pinned type should still match")
	}
}

// The legend's bulk buttons clear or empty the type whitelist while leaving the
// other filter dimensions in place.
func TestSpaceFilterBulkTypes(t *testing.T) {
	f := parseSpaceFilter("subject:/orders type:created foo")

	all := f.withAllTypes()
	if all.typeSelectionActive() {
		t.Errorf("withAllTypes should drop the type selection")
	}
	if got := all.String(); got != "subject:/orders foo" {
		t.Errorf("withAllTypes String() = %q", got)
	}

	none := f.withNoTypes()
	if !none.showsNoTypes() {
		t.Errorf("withNoTypes should select the empty set")
	}
	if got := none.String(); got != "subject:/orders type:- foo" {
		t.Errorf("withNoTypes String() = %q", got)
	}

	// The receiver is untouched by either copy.
	if !f.hasType("created") || f.noTypes {
		t.Errorf("bulk helpers mutated the receiver: %+v", f)
	}
}

func TestBuildTypeChips(t *testing.T) {
	events := []clio.Event{
		{ID: "1", Subject: "/o/1", Type: "created"},
		{ID: "2", Subject: "/o/1", Type: "created"},
		{ID: "3", Subject: "/o/2", Type: "shipped"},
	}
	chips := buildTypeChips(events, parseSpaceFilter("type:created"))
	if len(chips) != 2 {
		t.Fatalf("chips = %d, want 2", len(chips))
	}
	// Busiest first: created (2) before shipped (1).
	if chips[0].Type != "created" || chips[0].Count != 2 {
		t.Errorf("chip[0] = %+v", chips[0])
	}
	if !chips[0].Active {
		t.Errorf("created chip should be Active (pinned)")
	}
	if chips[1].Active {
		t.Errorf("shipped chip should not be Active")
	}
	// Clicking the active chip toggles it back off (empty filter).
	if chips[0].Toggled != "" {
		t.Errorf("toggling the only pinned type should clear it, got %q", chips[0].Toggled)
	}
	// Clicking the inactive chip adds it to the pin set.
	if chips[1].Toggled != "type:created type:shipped" {
		t.Errorf("shipped toggle = %q", chips[1].Toggled)
	}
}

// A type: filter on the space narrows the charted events and the legend chips
// reflect the pinned selection.
func TestHandleSpaceFilteredByType(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = fakeEventsBody() // created, shipped, created, login
	f.connect(s)

	rec := s.do(http.MethodGet, "/space?q=type:login", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "lg-toggle on") {
		t.Errorf("expected an active (pinned) chip, got:\n%s", body)
	}
	if !strings.Contains(body, "· filtered") {
		t.Errorf("header should report the filter is active")
	}
	// The /users/9 login subject should appear; an /orders subject should not.
	if !strings.Contains(body, "/users/9") {
		t.Errorf("login event row missing")
	}
	if strings.Contains(body, "/orders/1") {
		t.Errorf("orders rows should be filtered out")
	}
}

// The legend offers one-click "all" and "none" buttons; "none" (type:-) charts
// nothing yet keeps the chips so types can be clicked back on.
func TestHandleSpaceBulkTypeToggles(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = fakeEventsBody() // created, shipped, created, login
	f.connect(s)

	// Unfiltered: both bulk buttons render, "all" marked active.
	body := s.do(http.MethodGet, "/space", nil).Body.String()
	if !strings.Contains(body, "lg-bulk-btn") {
		t.Fatalf("expected the all/none bulk buttons, got:\n%s", body)
	}
	if !strings.Contains(body, `"q":"type:-"`) {
		t.Errorf("the none button should apply the type:- selection")
	}

	// type:- charts nothing but keeps the chips visible for recovery.
	rec := s.do(http.MethodGet, "/space?q=type:-", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	none := rec.Body.String()
	if !strings.Contains(none, "No events match this filter") {
		t.Errorf("show-none should yield the empty-chart note, got:\n%s", none)
	}
	if !strings.Contains(none, "lg-toggle") {
		t.Errorf("type chips should remain so types can be clicked back on")
	}
	// The "none" bulk button is now the active selection.
	if !strings.Contains(none, "lg-bulk-btn on") {
		t.Errorf("the none button should render active under a show-none filter")
	}
}

// A filter that matches nothing keeps the filter chrome on screen so the user
// can recover.
func TestHandleSpaceFilterNoMatch(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	f := newFakeClio(t)
	f.ndjson = fakeEventsBody()
	f.connect(s)

	rec := s.do(http.MethodGet, "/space?q=type:nope", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "No events match this filter") {
		t.Errorf("expected no-match note, got:\n%s", body)
	}
	// The filter input must still be present so the user can adjust it.
	if !strings.Contains(body, `name="q"`) {
		t.Errorf("filter input should remain visible on no match")
	}
	// And the type chips remain so a type can be toggled back on.
	if !strings.Contains(body, "lg-toggle") {
		t.Errorf("type chips should remain on no match")
	}
}
