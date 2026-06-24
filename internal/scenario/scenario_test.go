package scenario

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
)

func sampleSuite() *Suite {
	return &Suite{
		ID:      "order-tests",
		Name:    "Order tests",
		DraftID: "order",
		Cases: []Case{
			{
				ID:   "c1",
				Name: "happy path",
				Steps: []Step{
					{Type: "order-created"},
					{Type: "order-paid", Data: []byte(`{"amount":1}`)},
				},
				Expect: Expectation{Outcome: ExpectAccept, EndState: "paid"},
			},
			{
				ID:     "c2",
				Name:   "cancel after ship is rejected",
				Steps:  []Step{{Type: "order-shipped"}, {Type: "order-cancelled"}},
				Expect: Expectation{Outcome: ExpectReject},
			},
		},
	}
}

func TestStoreRoundTrip(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	su := sampleSuite()
	if err := st.Save(su); err != nil {
		t.Fatalf("save: %v", err)
	}
	if su.CreatedAt.IsZero() || su.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not set: %+v", su)
	}

	got, err := st.Get("order-tests")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != su.Name || len(got.Cases) != 2 || got.Cases[0].Expect.EndState != "paid" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	// MarshalIndent re-indents RawMessage; compare compacted JSON, not bytes.
	var compact bytes.Buffer
	if err := json.Compact(&compact, got.Cases[0].Steps[1].Data); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if compact.String() != `{"amount":1}` {
		t.Fatalf("payload not preserved: %q", compact.String())
	}
}

func TestStoreSavePreservesCreatedAt(t *testing.T) {
	st, _ := Open(t.TempDir())
	su := sampleSuite()
	if err := st.Save(su); err != nil {
		t.Fatalf("save: %v", err)
	}
	created := su.CreatedAt
	su.Name = "Renamed"
	if err := st.Save(su); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if !su.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt changed: %v != %v", su.CreatedAt, created)
	}
	if !su.UpdatedAt.After(created) && !su.UpdatedAt.Equal(created) {
		t.Fatalf("UpdatedAt not refreshed")
	}
}

func TestStoreListSorted(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir)
	a := &Suite{ID: "a", Name: "A", DraftID: "d"}
	b := &Suite{ID: "b", Name: "B", DraftID: "d"}
	if err := st.Save(a); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(b); err != nil { // saved later → newer → first
		t.Fatal(err)
	}
	// Stray non-JSON entries in the dir must be skipped by List.
	if err := os.WriteFile(filepath.Join(dir, "scenarios", "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "scenarios", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	list, err := st.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].ID != "b" {
		t.Fatalf("expected newest-first [b a], got %v", ids(list))
	}
}

func TestStoreDelete(t *testing.T) {
	st, _ := Open(t.TempDir())
	su := sampleSuite()
	if err := st.Save(su); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("order-tests"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.Get("order-tests"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
	if err := st.Delete("order-tests"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete = %v, want ErrNotFound", err)
	}
}

func TestStoreGetMissingAndInvalid(t *testing.T) {
	st, _ := Open(t.TempDir())
	if _, err := st.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing = %v, want ErrNotFound", err)
	}
	if _, err := st.Get("Bad Id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid id = %v, want ErrNotFound", err)
	}
	if err := st.Delete("Bad Id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid delete = %v, want ErrNotFound", err)
	}
}

func TestStoreDecodeError(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir)
	// Write garbage into the scenarios subdir so List/Get fail to decode.
	bad := filepath.Join(dir, "scenarios", "broken.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.List(); err == nil {
		t.Fatalf("expected decode error from List")
	}
	if _, err := st.Get("broken"); err == nil {
		t.Fatalf("expected decode error from Get")
	}
}

func TestOpenMkdirError(t *testing.T) {
	// Opening under a path that is a *file* makes MkdirAll fail.
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(f); err == nil {
		t.Fatalf("expected mkdir error opening under a file")
	}
}

func TestListReadDirError(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir)
	if err := os.RemoveAll(filepath.Join(dir, "scenarios")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.List(); err == nil {
		t.Fatalf("expected readdir error after dir removal")
	}
}

func TestSaveOverCorruptIsError(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir)
	bad := filepath.Join(dir, "scenarios", "x.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Save must read the existing file (to preserve CreatedAt); a decode error
	// there is surfaced rather than silently overwritten.
	if err := st.Save(&Suite{ID: "x", Name: "X", DraftID: "d"}); err == nil {
		t.Fatalf("expected decode error from Save's read of a corrupt file")
	}
}

func TestSaveInvalidRawDataIsError(t *testing.T) {
	st, _ := Open(t.TempDir())
	// A Step.Data holding invalid raw JSON passes Validate (which doesn't parse
	// payloads) but fails json marshalling in write.
	su := &Suite{
		ID: "x", Name: "X", DraftID: "d",
		Cases: []Case{{ID: "c", Expect: Expectation{Outcome: ExpectAccept},
			Steps: []Step{{Type: "t", Data: []byte("{invalid")}}}},
	}
	if err := st.Save(su); err == nil {
		t.Fatalf("expected marshal error for invalid raw payload")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Suite)
		ok   bool
	}{
		{"valid", func(*Suite) {}, true},
		{"bad id", func(s *Suite) { s.ID = "Not A Slug" }, false},
		{"empty name", func(s *Suite) { s.Name = "" }, false},
		{"no draft", func(s *Suite) { s.DraftID = "" }, false},
		{"empty case id", func(s *Suite) { s.Cases[0].ID = "" }, false},
		{"dup case id", func(s *Suite) { s.Cases[1].ID = s.Cases[0].ID }, false},
		{"bad outcome", func(s *Suite) { s.Cases[0].Expect.Outcome = "maybe" }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			su := sampleSuite()
			tc.mut(su)
			err := su.Validate()
			if tc.ok && err != nil {
				t.Fatalf("want valid, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("want error")
			}
		})
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	st, _ := Open(t.TempDir())
	if err := st.Save(&Suite{ID: "x"}); err == nil { // missing name + draft
		t.Fatalf("expected validation error on save")
	}
}

func sampleDraft() model.Draft {
	return model.Draft{
		ID: "order",
		Nodes: []model.Node{
			{ID: "new", Label: "New", Start: true, X: 1, Y: 2},
			{ID: "paid", Label: "Paid", End: true},
		},
		Edges: []model.Edge{{ID: "e1", Type: "order-paid", From: "new", To: "paid"}},
		Steps: []model.Step{
			{ID: "s1", Kind: model.StepEvent, Name: "order-paid", Fields: []model.Field{
				{Name: "amount", Type: "number", Required: true},
			}},
			{ID: "s2", Kind: model.StepTask, Name: "ship"}, // ignored by rev
		},
	}
}

func TestDraftRevAndDrift(t *testing.T) {
	d := sampleDraft()
	rev := DraftRev(d)
	if rev == "" || len(rev) != 12 {
		t.Fatalf("rev should be 12 hex chars, got %q", rev)
	}

	// Layout-only change (and a task step rename) must NOT change the rev.
	d2 := sampleDraft()
	d2.Nodes[0].X = 999
	d2.Steps[1].Name = "ship-it"
	if DraftRev(d2) != rev {
		t.Fatalf("layout/task change must not move rev")
	}

	// A meaningful change (a field becomes required-changed) MUST move the rev.
	d3 := sampleDraft()
	d3.Steps[0].Fields[0].Required = false
	if DraftRev(d3) == rev {
		t.Fatalf("field change must move rev")
	}

	// Cardinality is outcome-relevant, so annotating an edge MUST move the rev.
	d4 := sampleDraft()
	d4.Edges[0].Cardinality = model.CardinalityOnce
	if DraftRev(d4) == rev {
		t.Fatalf("cardinality change must move rev")
	}

	// Drift: empty rev never drifts; matching rev doesn't; stale rev does.
	if Drift(Suite{DraftRev: ""}, d) {
		t.Fatalf("empty rev must not drift")
	}
	if Drift(Suite{DraftRev: rev}, d) {
		t.Fatalf("matching rev must not drift")
	}
	if !Drift(Suite{DraftRev: "stale00000000"}, d) {
		t.Fatalf("stale rev must drift")
	}
}

func ids(ss []Suite) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.ID
	}
	return out
}
