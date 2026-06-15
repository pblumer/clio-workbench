// Package store persists Workbench drafts as local JSON files.
//
// Local files are the natural choice for a developer tool (git-friendly,
// versionable) — see docs/WORKBENCH.md §9.1. Each draft lives in
// <DataDir>/<id>.json. The store is safe for concurrent use.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/pblumer/clio-workbench/internal/model"
)

// ErrNotFound is returned when a draft id does not exist.
var ErrNotFound = errors.New("draft not found")

// ErrExists is returned when creating a draft whose id is already taken.
var ErrExists = errors.New("draft already exists")

// Store is a file-backed collection of drafts.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// Open returns a store rooted at dir, creating the directory if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// List returns all drafts, newest first.
func (s *Store) List() ([]model.Draft, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("read data dir: %w", err)
	}
	var drafts []model.Draft
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		d, err := s.readFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, *d)
	}
	sort.Slice(drafts, func(i, j int) bool {
		return drafts[i].UpdatedAt.After(drafts[j].UpdatedAt)
	})
	return drafts, nil
}

// Get returns the draft with the given id.
func (s *Store) Get(id string) (*model.Draft, error) {
	if !model.ValidID(id) {
		return nil, fmt.Errorf("%w: invalid id %q", ErrNotFound, id)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.read(id)
}

// Create persists a new draft, failing if the id already exists.
func (s *Store) Create(d *model.Draft) error {
	if err := d.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.path(d.ID)); err == nil {
		return fmt.Errorf("%w: %q", ErrExists, d.ID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	now := time.Now().UTC()
	d.CreatedAt = now
	d.UpdatedAt = now
	return s.write(d)
}

// Save persists an existing draft, refreshing UpdatedAt. It preserves the
// original CreatedAt if the draft already exists on disk.
func (s *Store) Save(d *model.Draft) error {
	if err := d.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if prev, err := s.read(d.ID); err == nil {
		d.CreatedAt = prev.CreatedAt
	} else if !errors.Is(err, ErrNotFound) {
		return err
	} else if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	d.UpdatedAt = time.Now().UTC()
	return s.write(d)
}

// Delete removes a draft.
func (s *Store) Delete(id string) error {
	if !model.ValidID(id) {
		return fmt.Errorf("%w: invalid id %q", ErrNotFound, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.path(id)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// read loads a draft by id (caller holds the lock).
func (s *Store) read(id string) (*model.Draft, error) {
	d, err := s.readFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *Store) readFile(path string) (*model.Draft, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d model.Draft
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return &d, nil
}

// write atomically persists a draft via a temp file + rename (caller holds
// the lock).
func (s *Store) write(d *model.Draft) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, d.ID+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path(d.ID))
}
