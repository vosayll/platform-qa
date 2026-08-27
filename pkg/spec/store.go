package spec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	specFileName = "spec.json" // raw specification document
	metaFileName = "meta.json" // parsed Meta summary
)

// Store persists the imported specification (<DataDir>/specs/spec.json)
// together with its Meta summary (meta.json). Both files are written
// atomically; the last successfully saved pair survives restarts.
type Store struct {
	mu   sync.RWMutex
	dir  string
	raw  []byte
	meta *Meta
}

// SpecsDir returns the directory holding the imported specification files.
func SpecsDir(dataDir string) string {
	return filepath.Join(dataDir, "specs")
}

// NewStore creates the storage directory and restores the previously saved
// pair if present.
func NewStore(dataDir string) (*Store, error) {
	dir := SpecsDir(dataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("spec store: mkdir %s: %w", dir, err)
	}
	s := &Store{dir: dir}
	raw, err := os.ReadFile(filepath.Join(dir, specFileName))
	if err != nil {
		return s, nil // nothing imported yet
	}
	mData, err := os.ReadFile(filepath.Join(dir, metaFileName))
	if err != nil {
		return s, nil // raw without meta: treat as not imported
	}
	var meta Meta
	if err := json.Unmarshal(mData, &meta); err != nil {
		return s, nil
	}
	s.raw = raw
	s.meta = &meta
	return s, nil
}

// Save atomically persists the raw document and its Meta summary.
func (st *Store) Save(meta *Meta, raw []byte) error {
	if meta == nil {
		return fmt.Errorf("spec store: meta is nil")
	}
	if raw == nil {
		raw = []byte{}
	}
	mData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("spec store: marshal meta: %w", err)
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	if err := writeFileAtomic(filepath.Join(st.dir, specFileName), raw); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(st.dir, metaFileName), mData); err != nil {
		return err
	}

	cp := *meta
	st.meta = &cp
	st.raw = append([]byte(nil), raw...)
	return nil
}

// Raw returns a copy of the stored specification document.
func (st *Store) Raw() ([]byte, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	if st.meta == nil {
		return nil, false
	}
	return append([]byte(nil), st.raw...), true
}

// Meta returns a copy of the stored import summary.
func (st *Store) Meta() (*Meta, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	if st.meta == nil {
		return nil, false
	}
	cp := *st.meta
	return &cp, true
}

// Delete removes both files and forgets the in-memory copy.
func (st *Store) Delete() error {
	st.mu.Lock()
	defer st.mu.Unlock()

	for _, name := range []string{specFileName, metaFileName} {
		if err := os.Remove(filepath.Join(st.dir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("spec store: remove %s: %w", name, err)
		}
	}
	st.raw = nil
	st.meta = nil
	return nil
}

// writeFileAtomic persists data through a tmp file + rename.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("spec store: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("spec store: rename %s: %w", path, err)
	}
	return nil
}
