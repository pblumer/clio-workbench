// Package envstore persists named "environments" — saved, switchable working
// contexts (server + data scope) — as a single JSON file under the data dir.
package envstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/pblumer/clio-workbench/internal/model"
)

// ErrNotFound is returned when an environment id does not exist.
var ErrNotFound = errors.New("environment not found")

type file struct {
	Environments []model.Environment `json:"environments"`
	Active       string              `json:"active"`
}

// Store is a file-backed collection of environments, safe for concurrent use.
type Store struct {
	path string
	mu   sync.RWMutex
	f    file
}

// Open loads (or initialises) the environments file in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "environments.json")}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// fresh
	case err != nil:
		return nil, err
	default:
		_ = json.Unmarshal(b, &s.f)
	}
	return s, nil
}

// List returns the environments sorted by name.
func (s *Store) List() []model.Environment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Environment, len(s.f.Environments))
	copy(out, s.f.Environments)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Upsert adds or replaces an environment by id.
func (s *Store) Upsert(env model.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	replaced := false
	for i := range s.f.Environments {
		if s.f.Environments[i].ID == env.ID {
			s.f.Environments[i] = env
			replaced = true
			break
		}
	}
	if !replaced {
		s.f.Environments = append(s.f.Environments, env)
	}
	return s.save()
}

// Delete removes an environment (and clears it as active if needed).
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.f.Environments[:0]
	found := false
	for _, e := range s.f.Environments {
		if e.ID == id {
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		return ErrNotFound
	}
	s.f.Environments = out
	if s.f.Active == id {
		s.f.Active = ""
	}
	return s.save()
}

// Active returns the active environment, if any.
func (s *Store) Active() (model.Environment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.f.Environments {
		if e.ID == s.f.Active {
			return e, true
		}
	}
	return model.Environment{}, false
}

// ActiveID returns the active environment id ("" = none).
func (s *Store) ActiveID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.f.Active
}

// SetActive marks an environment active ("" clears it).
func (s *Store) SetActive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != "" {
		ok := false
		for _, e := range s.f.Environments {
			if e.ID == id {
				ok = true
				break
			}
		}
		if !ok {
			return ErrNotFound
		}
	}
	s.f.Active = id
	return s.save()
}

// save atomically writes the file (caller holds the lock).
func (s *Store) save() error {
	b, err := json.MarshalIndent(s.f, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
