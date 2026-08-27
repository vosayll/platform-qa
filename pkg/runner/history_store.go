package runner

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxStoredRuns caps both the number of run files kept on disk and the
// in-memory history size. Oldest runs (by StartTime) are evicted first.
const MaxStoredRuns = 200

// historyStore persists finished test runs as one JSON file per run under
// <DataDir>/runs/<runID>.json and enforces the retention limit. All file
// operations are serialized by mu, so concurrent finalizing runs never race.
type historyStore struct {
	mu  sync.Mutex
	dir string
}

// newHistoryStore creates <dataDir>/runs and returns the store.
func newHistoryStore(dataDir string) (*historyStore, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("history store: data dir is required")
	}
	dir := filepath.Join(dataDir, "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("history store: mkdir %s: %w", dir, err)
	}
	return &historyStore{dir: dir}, nil
}

// Save atomically persists the run (tmp file + rename).
func (h *historyStore) Save(run *TestRun) error {
	if h == nil {
		return fmt.Errorf("history store: not initialized")
	}
	if run == nil || run.ID == "" {
		return fmt.Errorf("history store: run has no id")
	}
	data, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("history store: marshal run %s: %w", run.ID, err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	final := h.filePath(run.ID)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("history store: write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("history store: rename %s: %w", final, err)
	}
	return nil
}

// Load reads every stored run, skips corrupt files, sorts by StartTime
// descending (newest first) and returns at most max runs.
func (h *historyStore) Load(max int) []*TestRun {
	if h == nil || max <= 0 {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	entries, err := os.ReadDir(h.dir)
	if err != nil {
		log.Printf("[HISTORY] read %s: %v", h.dir, err)
		return nil
	}

	runs := make([]*TestRun, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var run TestRun
		if err := readRunFile(filepath.Join(h.dir, e.Name()), &run); err != nil {
			log.Printf("[HISTORY] skipping corrupt run file %s: %v", e.Name(), err)
			continue
		}
		if run.ID == "" {
			run.ID = strings.TrimSuffix(e.Name(), ".json")
		}
		runs = append(runs, &run)
	}

	sort.SliceStable(runs, func(i, j int) bool { return runs[i].StartTime.After(runs[j].StartTime) })
	if len(runs) > max {
		runs = runs[:max]
	}
	return runs
}

// Prune deletes stale .tmp artifacts and, when more than max run files exist,
// removes the oldest ones by StartTime.
func (h *historyStore) Prune(max int) {
	if h == nil || max <= 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	entries, err := os.ReadDir(h.dir)
	if err != nil {
		return
	}

	type stored struct {
		path  string
		start time.Time
	}
	kept := make([]stored, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(name, ".tmp") {
			// Leftover of an interrupted Save; Save itself cannot run
			// concurrently because it shares this mutex.
			_ = os.Remove(filepath.Join(h.dir, name))
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		var probe struct {
			StartTime time.Time `json:"startTime"`
		}
		if err := readRunFile(filepath.Join(h.dir, name), &probe); err != nil {
			log.Printf("[HISTORY] pruning: skipping unreadable file %s: %v", name, err)
			continue
		}
		kept = append(kept, stored{path: filepath.Join(h.dir, name), start: probe.StartTime})
	}

	if len(kept) <= max {
		return
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].start.Before(kept[j].start) })
	for _, s := range kept[:len(kept)-max] {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			log.Printf("[HISTORY] prune remove %s: %v", s.path, err)
		}
	}
}

func (h *historyStore) filePath(runID string) string {
	return filepath.Join(h.dir, runID+".json")
}

func readRunFile(path string, dst interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
