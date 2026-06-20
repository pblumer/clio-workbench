package validate

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
)

// orderLifecycle is a small entity model used across the sequence tests:
//
//	(start) new --created--> placed --paid--> paid --shipped--> shipped (end)
//	                                  \--cancelled--> cancelled (end)
func orderLifecycle() model.Draft {
	return model.Draft{
		Nodes: []model.Node{
			{ID: "new", Label: "New", Start: true},
			{ID: "placed", Label: "Placed"},
			{ID: "paid", Label: "Paid"},
			{ID: "shipped", Label: "Shipped", End: true},
			{ID: "cancelled", Label: "Cancelled", End: true},
		},
		Edges: []model.Edge{
			{ID: "e1", Type: "created", From: "new", To: "placed"},
			{ID: "e2", Type: "paid", From: "placed", To: "paid"},
			{ID: "e3", Type: "shipped", From: "paid", To: "shipped"},
			{ID: "e4", Type: "cancelled", From: "placed", To: "cancelled"},
		},
	}
}

func TestCheckSequence(t *testing.T) {
	m := NewMachine(orderLifecycle())
	tests := []struct {
		name     string
		types    []string
		wantOK   bool
		wantFail int
		wantPath []string
	}{
		{"happy path to end", []string{"created", "paid", "shipped"}, true, -1,
			[]string{"new", "placed", "paid", "shipped"}},
		{"branch to cancelled", []string{"created", "cancelled"}, true, -1,
			[]string{"new", "placed", "cancelled"}},
		{"empty sequence is trivially ok", nil, true, -1, nil},
		{"unknown first type", []string{"shipped"}, false, 0, []string{"new"}},
		{"illegal transition mid-stream", []string{"created", "shipped"}, false, 1,
			[]string{"new", "placed"}},
		{"ends in non-terminal state", []string{"created", "paid"}, false, 2,
			[]string{"new", "placed", "paid"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := m.CheckSequence(tc.types)
			if got.OK != tc.wantOK {
				t.Fatalf("OK = %v, want %v (reason %q)", got.OK, tc.wantOK, got.Reason)
			}
			if got.FailIx != tc.wantFail {
				t.Errorf("FailIx = %d, want %d", got.FailIx, tc.wantFail)
			}
			if tc.wantPath != nil && !reflect.DeepEqual(got.Path, tc.wantPath) {
				t.Errorf("Path = %v, want %v", got.Path, tc.wantPath)
			}
			if !tc.wantOK && got.Reason == "" {
				t.Errorf("expected a non-empty Reason on failure")
			}
		})
	}
}

func TestCheckSequenceNoGraph(t *testing.T) {
	m := NewMachine(model.Draft{})
	got := m.CheckSequence([]string{"created"})
	if got.OK || got.FailIx != -1 || got.Reason == "" {
		t.Fatalf("empty graph: got %+v", got)
	}
}

func TestCheckSequenceNoStart(t *testing.T) {
	d := model.Draft{
		Nodes: []model.Node{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		Edges: []model.Edge{{ID: "e", Type: "go", From: "a", To: "b"}},
	}
	got := NewMachine(d).CheckSequence([]string{"go"})
	if got.OK || got.FailIx != 0 || got.Reason != "model has no start state" {
		t.Fatalf("no start: got %+v", got)
	}
}

// With no End nodes marked, any consumed sequence is terminal-agnostic and OK.
func TestCheckSequenceNoEndNodes(t *testing.T) {
	d := model.Draft{
		Nodes: []model.Node{{ID: "a", Start: true}, {ID: "b"}},
		Edges: []model.Edge{{ID: "e", Type: "go", From: "a", To: "b"}},
	}
	got := NewMachine(d).CheckSequence([]string{"go"})
	if !got.OK {
		t.Fatalf("expected OK without End nodes, got %+v", got)
	}
}

// Two start states: the walk tries them in draft order and the deepest failure
// is reported; a viable start still succeeds.
func TestCheckSequenceMultipleStarts(t *testing.T) {
	d := model.Draft{
		Nodes: []model.Node{
			{ID: "s1", Label: "S1", Start: true},
			{ID: "s2", Label: "S2", Start: true},
			{ID: "mid"},
			{ID: "fin", End: true},
		},
		Edges: []model.Edge{
			// s1 dead-ends for type "go"; s2 carries the sequence through.
			{ID: "a", Type: "other", From: "s1", To: "mid"},
			{ID: "b", Type: "go", From: "s2", To: "mid"},
			{ID: "c", Type: "done", From: "mid", To: "fin"},
		},
	}
	m := NewMachine(d)
	if got := m.CheckSequence([]string{"go", "done"}); !got.OK {
		t.Fatalf("expected a viable start to succeed, got %+v", got)
	}
	// Neither start accepts "nope": the furthest-progressing attempt is reported.
	if got := m.CheckSequence([]string{"nope"}); got.OK || got.FailIx != 0 {
		t.Fatalf("expected failure at 0, got %+v", got)
	}
}

// A node whose label is empty falls back to its id in messages.
func TestLabelFallsBackToID(t *testing.T) {
	d := model.Draft{Nodes: []model.Node{{ID: "lonely", Start: true}}}
	got := NewMachine(d).CheckSequence([]string{"x"})
	if got.Reason != `no transition from state "lonely" via event type "x"` {
		t.Fatalf("label fallback: %q", got.Reason)
	}
}

func fields() []model.Field {
	return []model.Field{
		{Name: "id", Type: "reference", Format: "uuid", Required: true},
		{Name: "amount", Type: "number", Required: true},
		{Name: "qty", Type: "integer"},
		{Name: "paid", Type: "boolean"},
		{Name: "currency", Type: "enum", Enum: []string{"EUR", "USD"}},
		{Name: "when", Type: "datetime"},
		{Name: "email", Type: "string", Format: "email"},
		{Name: "note", Type: "string"},
		{Name: "", Type: "string"}, // unnamed → skipped
	}
}

func TestCheckPayloadValid(t *testing.T) {
	data := `{
		"id": "123e4567-e89b-12d3-a456-426614174000",
		"amount": 9.99,
		"qty": 3,
		"paid": true,
		"currency": "EUR",
		"when": "2026-06-20T10:00:00Z",
		"email": "a@b.de",
		"note": "hello",
		"extra": "ignored"
	}`
	errs, err := CheckPayload(fields(), json.RawMessage(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no field errors, got %+v", errs)
	}
}

func TestCheckPayloadErrors(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		field string
		rule  string
	}{
		{"missing required", `{"amount": 1}`, "id", "required"},
		{"null counts as missing", `{"id": null, "amount": 1}`, "id", "required"},
		{"number wrong type", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":"x"}`, "amount", "type"},
		{"integer not whole", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":1,"qty":1.5}`, "qty", "type"},
		{"integer wrong type", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":1,"qty":"x"}`, "qty", "type"},
		{"boolean wrong type", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":1,"paid":"yes"}`, "paid", "type"},
		{"enum value", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":1,"currency":"GBP"}`, "currency", "enum"},
		{"enum wrong type", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":1,"currency":5}`, "currency", "type"},
		{"datetime bad", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":1,"when":"nope"}`, "when", "format"},
		{"datetime wrong type", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":1,"when":5}`, "when", "type"},
		{"uuid format", `{"id":"not-a-uuid","amount":1}`, "id", "format"},
		{"email format", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":1,"email":"nope"}`, "email", "format"},
		{"string wrong type", `{"id":"123e4567-e89b-12d3-a456-426614174000","amount":1,"note":5}`, "note", "type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs, err := CheckPayload(fields(), json.RawMessage(tc.data))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !hasError(errs, tc.field, tc.rule) {
				t.Fatalf("want error on %q/%q, got %+v", tc.field, tc.rule, errs)
			}
		})
	}
}

func TestCheckPayloadDateTimeFieldType(t *testing.T) {
	// A datetime *field type* (not just format) validates the value too.
	errs, err := CheckPayload([]model.Field{{Name: "when", Type: "datetime"}}, json.RawMessage(`{"when":"2026-06-20T10:00:00Z"}`))
	if err != nil || len(errs) != 0 {
		t.Fatalf("valid datetime: errs=%+v err=%v", errs, err)
	}
}

func TestCheckPayloadEmptyAndNull(t *testing.T) {
	req := []model.Field{{Name: "id", Type: "string", Required: true}}
	for _, data := range []string{"", "{}", "null"} {
		errs, err := CheckPayload(req, json.RawMessage(data))
		if err != nil {
			t.Fatalf("data %q: unexpected error %v", data, err)
		}
		if !hasError(errs, "id", "required") {
			t.Fatalf("data %q: want required error, got %+v", data, errs)
		}
	}
}

func TestCheckPayloadNonObject(t *testing.T) {
	errs, err := CheckPayload(fields(), json.RawMessage(`[1,2,3]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 1 || errs[0].Rule != "type" || errs[0].Field != "" {
		t.Fatalf("want a single whole-payload type error, got %+v", errs)
	}
}

func TestCheckPayloadInvalidJSON(t *testing.T) {
	if _, err := CheckPayload(fields(), json.RawMessage(`{not json`)); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

func TestCheckStringFormatUnknownPasses(t *testing.T) {
	errs, err := CheckPayload([]model.Field{{Name: "x", Type: "string", Format: "phone"}}, json.RawMessage(`{"x":"whatever"}`))
	if err != nil || len(errs) != 0 {
		t.Fatalf("unknown format should pass: errs=%+v err=%v", errs, err)
	}
}

func hasError(errs []FieldError, field, rule string) bool {
	for _, e := range errs {
		if e.Field == field && e.Rule == rule {
			return true
		}
	}
	return false
}
