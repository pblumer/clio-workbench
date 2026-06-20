package process

import (
	"encoding/json"
	"testing"
)

// --- bpmn.go ---

// ParseBPMN: malformed XML returns an error.
func TestParseBPMNInvalidXML(t *testing.T) {
	if _, err := ParseBPMN([]byte("<not-closed")); err == nil {
		t.Fatal("expected error for malformed XML")
	}
}

// ParseBPMN: the first empty process (no id, no flows) is skipped, and the
// process without a startEvent picks the node with no incoming flow as start.
func TestParseBPMNSkipsEmptyProcessAndInfersStart(t *testing.T) {
	const b = `<?xml version="1.0"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <bpmn:process/>
  <bpmn:process id="P">
    <bpmn:intermediateCatchEvent id="c1" name="a"/>
    <bpmn:intermediateCatchEvent id="c2" name="b"/>
    <bpmn:sequenceFlow id="f" sourceRef="c1" targetRef="c2"/>
  </bpmn:process>
</bpmn:definitions>`
	m, err := ParseBPMN([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	// c1 has no incoming flow → inferred start; expected sequence a then b.
	if len(m.Expected) != 2 || m.Expected[0] != "a" || m.Expected[1] != "b" {
		t.Fatalf("expected = %v, want [a b]", m.Expected)
	}
	// No collaboration participant and no lane → empty subject, process is the id.
	if m.Process != "P" {
		t.Errorf("process = %q, want P", m.Process)
	}
	if m.Subject != "" {
		t.Errorf("subject = %q, want empty", m.Subject)
	}
}

// ParseBPMN: a flow whose target id is unknown (no node) is walked through the
// `kind` miss (continue) branch.
func TestParseBPMNFlowToUnknownNode(t *testing.T) {
	const b = `<?xml version="1.0"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <bpmn:process id="P">
    <bpmn:startEvent id="s" name="a"/>
    <bpmn:sequenceFlow id="f1" sourceRef="s" targetRef="ghost"/>
    <bpmn:sequenceFlow id="f2" sourceRef="ghost" targetRef="e"/>
    <bpmn:endEvent id="e" name="b"/>
  </bpmn:process>
</bpmn:definitions>`
	m, err := ParseBPMN([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	// "ghost" has no kind → skipped; flow continues s → ghost → e.
	if len(m.Expected) != 2 || m.Expected[0] != "a" || m.Expected[1] != "b" {
		t.Fatalf("expected = %v, want [a b]", m.Expected)
	}
}

// subjectPattern is exercised with an empty segment (double slash) and a
// leading/trailing slash so the seg=="" continue branch runs.
func TestSubjectPatternEmptySegments(t *testing.T) {
	if got := subjectPattern("/orders//1234/"); got != "/orders/{id}" {
		t.Errorf("subjectPattern = %q, want /orders/{id}", got)
	}
	if got := subjectPattern(""); got != "/" {
		t.Errorf("subjectPattern(\"\") = %q, want /", got)
	}
}

// ScopePrefix: literal prefix up to {id}, and the empty-result branch.
func TestScopePrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/employees/{id}/employee-onboarding", "/employees"},
		{"/a//b/{id}", "/a/b"},
		{"/{id}/x", ""}, // first segment is {id} → empty
		{"", ""},        // nothing at all
		{"/{id}", ""},   // only an id
	}
	for _, c := range cases {
		if got := ScopePrefix(c.in); got != c.want {
			t.Errorf("ScopePrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// deviationDetail: the three branches beyond "stopped at step".
func TestDeviationDetailBranches(t *testing.T) {
	exp := []string{"a", "b"}
	// matched < len(exp): stopped early.
	if got := deviationDetail(exp, 1, []string{"a"}, 0); got == "" {
		t.Error("stopped-early detail empty")
	}
	// all matched, unexpected > 0.
	if got := deviationDetail(exp, 2, []string{"a", "b"}, 1); got != "all steps in order, but 1 unexpected event(s)" {
		t.Errorf("unexpected detail = %q", got)
	}
	// all matched, no unexpected, but projected longer than exp.
	if got := deviationDetail(exp, 2, []string{"a", "b", "b"}, 0); got != "all steps seen but with extra/repeated events" {
		t.Errorf("extra detail = %q", got)
	}
	// all matched, no unexpected, projected same length → out of order.
	if got := deviationDetail(exp, 2, []string{"a", "b"}, 0); got != "out of order" {
		t.Errorf("out-of-order detail = %q", got)
	}
}

// CheckConformance: drive deviations that exercise the "extra/repeated" and
// "unexpected" deviationDetail branches, plus the maxDeviations<=0 default and
// the deviation cap.
func TestCheckConformanceDeviationKinds(t *testing.T) {
	m := BpmnModel{Process: "P", Expected: []string{"a", "b"}}
	seqs := map[string][]string{
		// all steps + an unexpected event mixed in.
		"/s/1": {"a", "x", "b"},
		// all steps but a repeated expected event (projected longer than exp).
		"/s/2": {"a", "b", "b"},
	}
	c := CheckConformance(m, seqs, 0) // 0 → default maxDeviations
	if c.Relevant != 2 {
		t.Fatalf("relevant = %d, want 2", c.Relevant)
	}
	if c.Conforming != 0 {
		t.Errorf("conforming = %d, want 0", c.Conforming)
	}
	if len(c.Deviations) != 2 {
		t.Fatalf("deviations = %d, want 2", len(c.Deviations))
	}
}

// CheckConformance: a deviation cap of 1 means later non-conforming subjects are
// counted but not appended to Deviations.
func TestCheckConformanceDeviationCap(t *testing.T) {
	m := BpmnModel{Process: "P", Expected: []string{"a", "b"}}
	seqs := map[string][]string{
		"/s/1": {"a"},
		"/s/2": {"a"},
		"/s/3": {"a"},
	}
	c := CheckConformance(m, seqs, 1)
	if len(c.Deviations) != 1 {
		t.Fatalf("deviations = %d, want 1 (capped)", len(c.Deviations))
	}
	if c.Relevant != 3 {
		t.Errorf("relevant = %d, want 3", c.Relevant)
	}
}

// CheckConformance: more than 12 distinct subject patterns trims SamplePatterns.
func TestCheckConformanceSamplePatternsTrim(t *testing.T) {
	m := BpmnModel{Process: "P", Expected: []string{"a"}}
	seqs := map[string][]string{}
	for i := 0; i < 20; i++ {
		seqs["/coll"+itoa(i)+"/1"] = []string{"a"}
	}
	c := CheckConformance(m, seqs, 12)
	if len(c.SamplePatterns) != 12 {
		t.Errorf("SamplePatterns = %d, want 12 (trimmed)", len(c.SamplePatterns))
	}
	// "a" is present everywhere; ExpectedMissing should be empty.
	if len(c.ExpectedMissing) != 0 {
		t.Errorf("ExpectedMissing = %v, want none", c.ExpectedMissing)
	}
}

// CheckConformance: an expected type that never appears lands in ExpectedMissing.
func TestCheckConformanceExpectedMissing(t *testing.T) {
	m := BpmnModel{Process: "P", Expected: []string{"a", "ghost"}}
	c := CheckConformance(m, map[string][]string{"/s/1": {"a"}}, 12)
	missing := false
	for _, e := range c.ExpectedMissing {
		if e == "ghost" {
			missing = true
		}
	}
	if !missing {
		t.Errorf("ExpectedMissing = %v, want it to contain ghost", c.ExpectedMissing)
	}
}

// CheckConformance: two unexpected types with different counts exercise the
// Unexpected sort's count-comparison branch.
func TestCheckConformanceUnexpectedSort(t *testing.T) {
	m := BpmnModel{Process: "P", Expected: []string{"a"}}
	seqs := map[string][]string{
		// "x" appears twice, "y" once → different counts → sort by count.
		"/s/1": {"a", "x", "x"},
		"/s/2": {"a", "y"},
	}
	c := CheckConformance(m, seqs, 12)
	if len(c.Unexpected) != 2 {
		t.Fatalf("unexpected = %+v, want 2 types", c.Unexpected)
	}
	if c.Unexpected[0].Type != "x" || c.Unexpected[0].Count != 2 {
		t.Errorf("top unexpected = %+v, want x x2", c.Unexpected[0])
	}
}

// itoa: zero and negative inputs.
func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 5: "5", 42: "42", -7: "-7", -100: "-100"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

// SubjectSequences groups event types per subject preserving order.
func TestSubjectSequences(t *testing.T) {
	evs := []Event{
		{Subject: "/o/1", Type: "a"},
		{Subject: "/o/2", Type: "x"},
		{Subject: "/o/1", Type: "b"},
	}
	seqs := SubjectSequences(evs)
	if len(seqs["/o/1"]) != 2 || seqs["/o/1"][0] != "a" || seqs["/o/1"][1] != "b" {
		t.Errorf("/o/1 seq = %v, want [a b]", seqs["/o/1"])
	}
	if len(seqs["/o/2"]) != 1 || seqs["/o/2"][0] != "x" {
		t.Errorf("/o/2 seq = %v, want [x]", seqs["/o/2"])
	}
}

// --- dotted.go ---

// BuildDotted: empty input returns the zero value.
func TestBuildDottedEmpty(t *testing.T) {
	d := BuildDotted(nil, 60)
	if d.Events != 0 || len(d.Dots) != 0 || len(d.Rows) != 0 {
		t.Errorf("empty input gave %+v, want zero", d)
	}
}

// BuildDotted: maxRows < 1 defaults to 60.
func TestBuildDottedDefaultMaxRows(t *testing.T) {
	d := BuildDotted([]TimedEvent{{Subject: "/o/1", Type: "a"}}, 0)
	if d.Total != 1 || d.Shown != 1 {
		t.Errorf("got total=%d shown=%d, want 1/1", d.Total, d.Shown)
	}
}

// BuildDotted: all timestamps parse but are identical → span collapses → falls
// back to sequence order (useTime false).
func TestBuildDottedIdenticalTimesFallBack(t *testing.T) {
	evs := []TimedEvent{
		{Subject: "/o/1", Type: "a", Time: "2026-01-01T10:00:00Z"},
		{Subject: "/o/2", Type: "b", Time: "2026-01-01T10:00:00Z"},
	}
	d := BuildDotted(evs, 60)
	if d.ByTime {
		t.Error("ByTime = true, want false (identical timestamps cannot span)")
	}
	if len(d.Dots) != 2 {
		t.Errorf("dots = %d, want 2", len(d.Dots))
	}
}

// BuildDotted: a single event (sequence mode) → span is 0 → forced to 1, X=0.
func TestBuildDottedSingleEventSpanZero(t *testing.T) {
	d := BuildDotted([]TimedEvent{{Subject: "/o/1", Type: "a"}}, 60)
	if len(d.Dots) != 1 || d.Dots[0].X != 0 {
		t.Errorf("dot = %+v, want single dot at X=0", d.Dots)
	}
}

// BuildDotted: a capped-out subject's events are skipped (rowOf miss branch).
func TestBuildDottedCappedSubjectDotsSkipped(t *testing.T) {
	evs := []TimedEvent{
		{Subject: "/o/1", Type: "a"},
		{Subject: "/o/1", Type: "b"},
		{Subject: "/o/2", Type: "c"}, // /o/2 capped out at maxRows=1
	}
	d := BuildDotted(evs, 1)
	if d.Shown != 1 || d.Total != 2 {
		t.Fatalf("shown=%d total=%d, want 1/2", d.Shown, d.Total)
	}
	for _, dot := range d.Dots {
		if dot.Subject == "/o/2" {
			t.Error("capped subject /o/2 produced a dot")
		}
	}
}

// BuildDotted: in time mode, a subject's later input event with an earlier
// timestamp updates its `first` (val[i] < a.first branch). Two subjects sharing
// the same first timestamp exercise the final name tiebreak (names[i] < names[j]).
func TestBuildDottedFirstUpdateAndNameTiebreak(t *testing.T) {
	evs := []TimedEvent{
		// /o/1: first input event is later in time than its second → first updates.
		{Subject: "/o/1", Type: "a", Time: "2026-01-01T10:00:00Z"},
		{Subject: "/o/1", Type: "b", Time: "2026-01-01T09:00:00Z"},
		// /o/2 shares /o/1's earliest timestamp (09:00) → equal first → name tiebreak.
		{Subject: "/o/2", Type: "c", Time: "2026-01-01T09:00:00Z"},
		{Subject: "/o/2", Type: "d", Time: "2026-01-01T11:00:00Z"},
	}
	d := BuildDotted(evs, 60)
	if !d.ByTime {
		t.Fatal("ByTime = false, want true")
	}
	// Both subjects' first event is 09:00 → equal → sorted by name → /o/1 first.
	if d.Rows[0].Subject != "/o/1" || d.Rows[1].Subject != "/o/2" {
		t.Errorf("row order = %q,%q, want /o/1,/o/2 (name tiebreak)", d.Rows[0].Subject, d.Rows[1].Subject)
	}
}

// --- process.go ---

// splitRuns: empty sequence returns nil.
func TestSplitRunsEmpty(t *testing.T) {
	if r := splitRuns(nil, map[string]bool{}, map[string]bool{}); r != nil {
		t.Errorf("splitRuns(nil) = %v, want nil", r)
	}
}

// Discover: an empty subject sequence is skipped in pass 1 (len(seq)==0 branch).
// Achieved by an event whose subject also appears with no further events is hard;
// instead feed a subject that exists in order but yields an empty seq via a
// zero-length append is impossible, so we cover the no-events path of Discover.
func TestDiscoverEmptyEvents(t *testing.T) {
	g := Discover(nil, 0)
	if g.Subjects != 0 || g.Events != 0 || g.Traces != 0 {
		t.Errorf("empty Discover = %+v, want zeros", g)
	}
}

// --- references.go ---

// firstSegment: empty / slash-only subjects return "".
func TestFirstSegmentEmpty(t *testing.T) {
	if got := firstSegment(""); got != "" {
		t.Errorf("firstSegment(\"\") = %q, want \"\"", got)
	}
	if got := firstSegment("///"); got != "" {
		t.Errorf("firstSegment(\"///\") = %q, want \"\"", got)
	}
	if got := firstSegment("/Orders/1"); got != "orders" {
		t.Errorf("firstSegment = %q, want orders (lowercased)", got)
	}
}

// resolveCollection: singular stem matches plural collection; an "s"-ending stem
// tries its singular; unknown stem returns itself with known=false.
func TestResolveCollection(t *testing.T) {
	cols := map[string]bool{"customers": true, "tag": true}
	if name, known := resolveCollection("customer", cols); name != "customers" || !known {
		t.Errorf("resolveCollection(customer) = %q,%v, want customers,true", name, known)
	}
	if name, known := resolveCollection("tags", cols); name != "tag" || !known {
		t.Errorf("resolveCollection(tags) = %q,%v, want tag,true (singular)", name, known)
	}
	if name, known := resolveCollection("widget", cols); name != "widget" || known {
		t.Errorf("resolveCollection(widget) = %q,%v, want widget,false", name, known)
	}
}

// BuildReferences: events with no owner, empty data, invalid JSON, an "id" field,
// non-FK fields, a self-referential FK (target==owner), and a duplicate target
// (seen) are all handled.
func TestBuildReferencesEdgeCases(t *testing.T) {
	events := []RefEvent{
		{Subject: "", Type: "no-owner", Data: json.RawMessage(`{"customerId":"c1"}`)},            // no owner
		{Subject: "/orders/1", Type: "empty-data"},                                               // empty data
		{Subject: "/orders/1", Type: "bad-json", Data: json.RawMessage(`{bad`)},                  // invalid JSON
		{Subject: "/orders/1", Type: "id-only", Data: json.RawMessage(`{"id":"x"}`)},             // id field skipped
		{Subject: "/orders/1", Type: "plain", Data: json.RawMessage(`{"name":"hi"}`)},            // non-FK field
		{Subject: "/orders/1", Type: "self", Data: json.RawMessage(`{"orderId":"5"}`)},           // FK to self → skipped
		{Subject: "/orders/1", Type: "dup", Data: json.RawMessage(`{"tagId":"a","tagRef":"b"}`)}, // two fields → one target "tag"
	}
	g := BuildReferences(events)
	// orders must be a known node.
	found := false
	for _, n := range g.Nodes {
		if n.Name == "orders" && n.Known {
			found = true
		}
	}
	if !found {
		t.Error("orders node missing or not known")
	}
	// No edge to itself.
	if e := refEdge(g, "orders", "orders"); e != nil {
		t.Error("self edge orders→orders should not exist")
	}
	// The duplicate target "tag" yields a single n:1/1:n edge, not two.
	if e := refEdge(g, "orders", "tag"); e == nil {
		t.Error("orders→tag edge missing")
	}
}

// BuildReferences: an association event whose two FKs resolve to known
// collections produces an n:m edge and the targets are sorted.
func TestBuildReferencesAssociationKnownTargets(t *testing.T) {
	events := []RefEvent{
		{Subject: "/products/1", Type: "p", Data: json.RawMessage(`{}`)},
		{Subject: "/orders/1", Type: "o", Data: json.RawMessage(`{}`)},
		{Subject: "/links/1", Type: "link", Data: json.RawMessage(`{"productId":"1","orderId":"2"}`)},
	}
	g := BuildReferences(events)
	// Sorted targets: orders < products.
	if e := refEdge(g, "orders", "products"); e == nil || e.Kind != "n:m" {
		t.Fatalf("orders↔products = %+v, want n:m", e)
	}
}

// BuildReferences: multiple edges with equal counts exercise the edge sort's
// From and To tiebreakers.
func TestBuildReferencesEdgeSortTiebreak(t *testing.T) {
	events := []RefEvent{
		// orders → customers and orders → products, each count 1 (equal count →
		// From equal → To tiebreak).
		{Subject: "/orders/1", Type: "t1", Data: json.RawMessage(`{"customerId":"c"}`)},
		{Subject: "/orders/2", Type: "t2", Data: json.RawMessage(`{"productId":"p"}`)},
		// users → customers, count 1 (equal count, different From → From tiebreak).
		{Subject: "/users/1", Type: "t3", Data: json.RawMessage(`{"customerId":"c"}`)},
	}
	g := BuildReferences(events)
	if len(g.Edges) < 3 {
		t.Fatalf("edges = %d, want >= 3", len(g.Edges))
	}
	// All counts are 1, so order is From then To: orders→customers, orders→products,
	// users→customers.
	for i := 1; i < len(g.Edges); i++ {
		a, b := g.Edges[i-1], g.Edges[i]
		if a.Count == b.Count {
			if a.From > b.From || (a.From == b.From && a.To > b.To) {
				t.Errorf("edges not sorted at %d: %+v before %+v", i, a, b)
			}
		}
	}
}

// --- relations.go ---

// isID: extra branches — empty string, long hex, long alphanumeric with a digit,
// a non-id string with separators but no digit.
func TestIsIDExtraBranches(t *testing.T) {
	trueCases := []string{
		"a1b2c3d4e5f6a7b8",   // 16 alnum with digit (>=12) and hex → allHex path
		"abc123def456ghi789", // long alnum with digit, not all hex
	}
	for _, s := range trueCases {
		if !isID(s) {
			t.Errorf("isID(%q) = false, want true", s)
		}
	}
	falseCases := []string{
		"",          // empty
		"user-name", // idLike + sep but no digit
		"shortid",   // alnum no digit
		"a1",        // alnum digit but too short
	}
	for _, s := range falseCases {
		if isID(s) {
			t.Errorf("isID(%q) = true, want false", s)
		}
	}
}

// BuildSubjectTree: empty subjects and a subject with empty segments (double
// slash) hit the seg=="" continue and produce only the synthetic root.
func TestBuildSubjectTreeEmptyAndBlankSegments(t *testing.T) {
	root := BuildSubjectTree([]string{"", "//", "/orders//1"})
	orders := child(root, "orders")
	if orders == nil {
		t.Fatal("orders missing")
	}
	// /orders//1 → orders then {id}, blank segment skipped.
	if child(orders, "{id}") == nil {
		t.Error("orders/{id} missing (blank segment not skipped)")
	}
}

// convertRel: children are ordered by Events desc then Seg — exercise the tie on
// Events so the Seg comparison runs.
func TestConvertRelOrdering(t *testing.T) {
	// /a/x and /a/y each occur once → equal Events under "a" → sorted by Seg.
	root := BuildSubjectTree([]string{"/a/y", "/a/x"})
	a := child(root, "a")
	if a == nil || len(a.Children) != 2 {
		t.Fatalf("a children = %+v", a)
	}
	if a.Children[0].Seg != "x" || a.Children[1].Seg != "y" {
		t.Errorf("children order = %q,%q, want x,y (Seg tiebreak)", a.Children[0].Seg, a.Children[1].Seg)
	}
}
