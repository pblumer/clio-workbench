package envstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pblumer/clio-workbench/internal/model"
)

func env(id, name string) model.Environment {
	return model.Environment{ID: id, Name: name, ServerURL: "https://clio.example.com"}
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

func TestOpenFresh(t *testing.T) {
	s := openTemp(t)
	if got := s.List(); len(got) != 0 {
		t.Fatalf("fresh store should be empty, got %d", len(got))
	}
	if id := s.ActiveID(); id != "" {
		t.Fatalf("fresh store active id = %q, want empty", id)
	}
	if _, ok := s.Active(); ok {
		t.Fatal("fresh store should have no active environment")
	}
}

func TestOpenMkdirError(t *testing.T) {
	// Create a regular file, then ask Open to MkdirAll a path beneath it.
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := Open(filepath.Join(f, "sub")); err == nil {
		t.Fatal("expected error when dir parent is a file")
	}
}

func TestOpenCorruptFileIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "environments.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Corrupt JSON is tolerated: unmarshal error is ignored, store opens empty.
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("corrupt file should yield empty store, got %d", len(got))
	}
}

func TestOpenReadError(t *testing.T) {
	// A path that exists as a directory makes ReadFile fail with a non-NotExist
	// error, exercising the `case err != nil` branch.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "environments.json"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("expected read error when file path is a directory")
	}
}

func TestUpsertInsertAndReplace(t *testing.T) {
	s := openTemp(t)
	if err := s.Upsert(env("a", "Alpha")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.Upsert(env("b", "Bravo")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Replace existing.
	updated := env("a", "Alpha2")
	if err := s.Upsert(updated); err != nil {
		t.Fatalf("upsert replace: %v", err)
	}
	list := s.List()
	if len(list) != 2 {
		t.Fatalf("want 2 envs, got %d", len(list))
	}
	// List is sorted by name: Alpha2 < Bravo.
	if list[0].Name != "Alpha2" || list[1].Name != "Bravo" {
		t.Fatalf("unexpected sort order: %q, %q", list[0].Name, list[1].Name)
	}
}

func TestListSortedAndCopy(t *testing.T) {
	s := openTemp(t)
	_ = s.Upsert(env("z", "Zulu"))
	_ = s.Upsert(env("m", "Mike"))
	list := s.List()
	if list[0].Name != "Mike" || list[1].Name != "Zulu" {
		t.Fatalf("not sorted: %v", list)
	}
	// Mutating the returned slice must not affect the store.
	list[0].Name = "MUTATED"
	if s.List()[0].Name != "Mike" {
		t.Fatal("List returned an aliased slice")
	}
}

func TestDelete(t *testing.T) {
	s := openTemp(t)
	_ = s.Upsert(env("a", "Alpha"))
	_ = s.Upsert(env("b", "Bravo"))
	if err := s.SetActive("a"); err != nil {
		t.Fatalf("setactive: %v", err)
	}

	// Deleting the active env clears active.
	if err := s.Delete("a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if s.ActiveID() != "" {
		t.Fatalf("active should be cleared, got %q", s.ActiveID())
	}
	if len(s.List()) != 1 {
		t.Fatalf("want 1 env after delete, got %d", len(s.List()))
	}

	// Deleting a non-active env leaves active untouched.
	_ = s.Upsert(env("c", "Charlie"))
	_ = s.SetActive("b")
	if err := s.Delete("c"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if s.ActiveID() != "b" {
		t.Fatalf("active should remain b, got %q", s.ActiveID())
	}
}

func TestDeleteNotFound(t *testing.T) {
	s := openTemp(t)
	if err := s.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestActiveAndSetActive(t *testing.T) {
	s := openTemp(t)
	_ = s.Upsert(env("a", "Alpha"))

	if err := s.SetActive("a"); err != nil {
		t.Fatalf("setactive: %v", err)
	}
	got, ok := s.Active()
	if !ok || got.ID != "a" {
		t.Fatalf("Active() = %+v, %v", got, ok)
	}
	if s.ActiveID() != "a" {
		t.Fatalf("ActiveID = %q", s.ActiveID())
	}

	// Clearing with "" is allowed and skips the existence check.
	if err := s.SetActive(""); err != nil {
		t.Fatalf("clear active: %v", err)
	}
	if s.ActiveID() != "" {
		t.Fatalf("active should be cleared")
	}
	if _, ok := s.Active(); ok {
		t.Fatal("Active() should report none after clear")
	}
}

func TestSetActiveNotFound(t *testing.T) {
	s := openTemp(t)
	if err := s.SetActive("ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPersistenceRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s1, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s1.Upsert(env("a", "Alpha")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s1.Upsert(env("b", "Bravo")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s1.SetActive("b"); err != nil {
		t.Fatalf("setactive: %v", err)
	}

	// Re-open from the same dir and verify the data was loaded back.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	list := s2.List()
	if len(list) != 2 {
		t.Fatalf("want 2 envs after reopen, got %d", len(list))
	}
	if s2.ActiveID() != "b" {
		t.Fatalf("active not persisted, got %q", s2.ActiveID())
	}
	got, ok := s2.Active()
	if !ok || got.Name != "Bravo" {
		t.Fatalf("active env not restored: %+v, %v", got, ok)
	}
}
