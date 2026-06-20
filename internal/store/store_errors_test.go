package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// notDirStore returns a store whose dir lives under a regular file, so every
// filesystem operation against it fails with a non-permission error
// (ENOTDIR). This lets us exercise error paths even when the test runs as
// root, where chmod-based permission denial is ineffective.
func notDirStore(t *testing.T) *Store {
	t.Helper()
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return &Store{dir: filepath.Join(f, "sub")}
}

func TestOpenMkdirAllError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// A directory cannot be created beneath a regular file.
	if _, err := Open(filepath.Join(f, "sub")); err == nil {
		t.Fatal("expected error creating data dir under a file")
	}
}

func TestListReadDirError(t *testing.T) {
	s := notDirStore(t)
	if _, err := s.List(); err == nil {
		t.Fatal("expected error reading missing data dir")
	}
}

func TestListReadFileError(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// A .json file containing invalid JSON makes readFile fail during List.
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write broken: %v", err)
	}
	if _, err := s.List(); err == nil {
		t.Fatal("expected error from invalid json during list")
	}
}

func TestListSkipsNonJSON(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Subdirectory and a non-.json file should both be skipped.
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	if err := s.Create(newDraft("order")); err != nil {
		t.Fatalf("create: %v", err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 draft, got %d", len(list))
	}
}

func TestListNewestFirst(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Create(newDraft("first")); err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := newDraft("second")
	if err := s.Create(second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 drafts, got %d", len(list))
	}
	if !list[0].UpdatedAt.After(list[1].UpdatedAt) && !list[0].UpdatedAt.Equal(list[1].UpdatedAt) {
		t.Fatalf("list not sorted newest-first: %v then %v", list[0].UpdatedAt, list[1].UpdatedAt)
	}
}

func TestGetInvalidID(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.Get("Bad Id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for invalid id, got %v", err)
	}
}

func TestCreateStatError(t *testing.T) {
	s := notDirStore(t)
	// Stat of s.path(id) fails with ENOTDIR (not ErrNotExist).
	err := s.Create(newDraft("order"))
	if err == nil {
		t.Fatal("expected stat error")
	}
	if errors.Is(err, ErrExists) {
		t.Fatalf("did not expect ErrExists, got %v", err)
	}
}

func TestSaveNewDraftZeroCreatedAt(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	d := newDraft("brand-new") // does not exist on disk yet, CreatedAt zero
	if err := s.Save(d); err != nil {
		t.Fatalf("save: %v", err)
	}
	if d.CreatedAt.IsZero() {
		t.Fatal("save should stamp CreatedAt for a brand-new draft")
	}
	if d.UpdatedAt.IsZero() {
		t.Fatal("save should stamp UpdatedAt")
	}
}

func TestSaveNewDraftPreservesProvidedCreatedAt(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	d := newDraft("preset")
	d.CreatedAt = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := s.Save(d); err != nil {
		t.Fatalf("save: %v", err)
	}
	if !d.CreatedAt.Equal(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("non-zero CreatedAt should be preserved, got %v", d.CreatedAt)
	}
}

func TestSaveReadError(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Existing on-disk file with invalid JSON => read returns a decode error
	// that is neither nil nor ErrNotFound, hitting the error branch in Save.
	if err := os.WriteFile(s.path("order"), []byte("{broken"), 0o644); err != nil {
		t.Fatalf("write broken: %v", err)
	}
	if err := s.Save(newDraft("order")); err == nil {
		t.Fatal("expected error from corrupt existing draft during save")
	}
}

func TestDeleteGenericError(t *testing.T) {
	s := notDirStore(t)
	// Remove of s.path(id) fails with ENOTDIR, not ErrNotExist.
	err := s.Delete("order")
	if err == nil {
		t.Fatal("expected remove error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("did not expect ErrNotFound, got %v", err)
	}
}

func TestDeleteInvalidID(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Delete("Bad Id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for invalid id, got %v", err)
	}
}

func TestSaveValidateError(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Save(newDraft("Bad Id")); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDeleteSuccess(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Create(newDraft("order")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Delete("order"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get("order"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft should be gone, got %v", err)
	}
}

func TestWriteRenameError(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Make s.path("order") a non-empty directory so the final os.Rename
	// (file -> existing non-empty dir) fails. We call write directly because
	// the public Create/Save entry points would reject this state earlier.
	dst := s.path("order")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "x"), []byte("x"), 0o644); err != nil {
		t.Fatalf("populate dst: %v", err)
	}
	if err := s.write(newDraft("order")); err == nil {
		t.Fatal("expected rename error")
	}
}

func TestWriteCreateTempError(t *testing.T) {
	// dir does not exist. Create's Stat(dir/order.json) returns ENOENT
	// (== os.ErrNotExist), so Create proceeds into write(), where
	// os.CreateTemp(dir, ...) fails because the directory is absent.
	s := &Store{dir: filepath.Join(t.TempDir(), "missing")}
	if err := s.Create(newDraft("order")); err == nil {
		t.Fatal("expected write/createtemp error")
	}
}
