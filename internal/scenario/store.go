package scenario

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

// ErrNotFound is returned when a suite id does not exist.
var ErrNotFound = errors.New("suite not found")

// Store is a file-backed collection of scenario suites, one JSON file per suite
// under <DataDir>/scenarios/. Safe for concurrent use.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// Open returns a store rooted at <dir>/scenarios, creating it if needed.
func Open(dir string) (*Store, error) {
	root := filepath.Join(dir, "scenarios")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create scenarios dir: %w", err)
	}
	return &Store{dir: root}, nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// List returns all suites, newest first.
func (s *Store) List() ([]Suite, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("read scenarios dir: %w", err)
	}
	var suites []Suite
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		su, err := s.readFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			return nil, err
		}
		suites = append(suites, *su)
	}
	sort.Slice(suites, func(i, j int) bool {
		return suites[i].UpdatedAt.After(suites[j].UpdatedAt)
	})
	return suites, nil
}

// Get returns the suite with the given id.
func (s *Store) Get(id string) (*Suite, error) {
	if !model.ValidID(id) {
		return nil, fmt.Errorf("%w: invalid id %q", ErrNotFound, id)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.read(id)
}

// Save persists a suite (create or replace), refreshing UpdatedAt and
// preserving the original CreatedAt when the suite already exists on disk.
func (s *Store) Save(su *Suite) error {
	if err := su.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if prev, err := s.read(su.ID); err == nil {
		su.CreatedAt = prev.CreatedAt
	} else if !errors.Is(err, ErrNotFound) {
		return err
	} else if su.CreatedAt.IsZero() {
		su.CreatedAt = time.Now().UTC()
	}
	su.UpdatedAt = time.Now().UTC()
	return s.write(su)
}

// Delete removes a suite.
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

// read loads a suite by id (caller holds the lock).
func (s *Store) read(id string) (*Suite, error) {
	su, err := s.readFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return su, err
}

func (s *Store) readFile(path string) (*Suite, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var su Suite
	if err := json.Unmarshal(b, &su); err != nil {
		return nil, fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return &su, nil
}

// write atomically persists a suite via a temp file + rename (caller holds the
// lock).
func (s *Store) write(su *Suite) error {
	b, err := json.MarshalIndent(su, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, su.ID+".*.tmp")
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
	return os.Rename(tmpName, s.path(su.ID))
}
