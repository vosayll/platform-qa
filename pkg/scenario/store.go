package scenario

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store is a thread-safe in-memory registry of custom scenarios
// persisted as individual JSON files <dir>/scenarios/<key>.json.
type Store struct {
	mu    sync.RWMutex
	dir   string
	items map[string]*Scenario
}

// NewStore creates the storage directory and loads all existing scenarios from disk.
func NewStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("scenario store: directory is required")
	}
	s := &Store{
		dir:   dir,
		items: make(map[string]*Scenario),
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("scenario store: mkdir %s: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("scenario store: read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			log.Printf("[SCENARIO] skipping unreadable file %s: %v", e.Name(), err)
			continue
		}
		var sc Scenario
		if err := json.Unmarshal(data, &sc); err != nil {
			log.Printf("[SCENARIO] skipping corrupt file %s: %v", e.Name(), err)
			continue
		}
		if sc.Category == "" {
			sc.Category = "custom"
		}
		s.items[sc.Key] = &sc
	}
	return s, nil
}

// List returns all scenarios sorted by key.
func (st *Store) List() []*Scenario {
	st.mu.RLock()
	defer st.mu.RUnlock()
	out := make([]*Scenario, 0, len(st.items))
	for _, sc := range st.items {
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Get returns a copy of the scenario by key.
func (st *Store) Get(key string) (*Scenario, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	sc, ok := st.items[key]
	if !ok {
		return nil, false
	}
	cp := *sc
	return &cp, true
}

// Exists reports whether the scenario key is present.
func (st *Store) Exists(key string) bool {
	st.mu.RLock()
	defer st.mu.RUnlock()
	_, ok := st.items[key]
	return ok
}

// Save validates the scenario and atomically persists it (tmp file + rename).
func (st *Store) Save(sc *Scenario) error {
	if sc == nil {
		return fmt.Errorf("scenario: nil")
	}
	sc.Category = "custom"
	if err := sc.Validate(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return fmt.Errorf("scenario %s marshal: %w", sc.Key, err)
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	final := st.filePath(sc.Key)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("scenario %s write tmp: %w", sc.Key, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("scenario %s rename: %w", sc.Key, err)
	}

	cp := *sc
	st.items[sc.Key] = &cp
	return nil
}

// Delete removes the scenario from memory and disk.
func (st *Store) Delete(key string) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	if _, ok := st.items[key]; !ok {
		return fmt.Errorf("scenario %q not found", key)
	}
	delete(st.items, key)

	err := os.Remove(st.filePath(key))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("scenario %s remove file: %w", key, err)
	}
	return nil
}

func (st *Store) filePath(key string) string {
	return filepath.Join(st.dir, key+".json")
}
