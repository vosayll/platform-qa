// Package report renders finished test runs into portable formats
// (JUnit XML, Allure results archive) and delivers failure notifications
// (Telegram). It is a leaf package: it reads runner.TestRun and the registry,
// never calls back into orchestration.
package report

import (
	"encoding/xml"
	"sort"
	"strings"

	"locali-e2e-engine/pkg/registry"
	"locali-e2e-engine/pkg/runner"
)

// entry is a flattened, ordered view of a single check result of a run.
type entry struct {
	SuiteKey   string
	CheckID    string
	Title      string
	Status     string
	Message    string
	DurationMs int64
}

// orderedEntries lists every check result of the run in canonical order:
// registry check order per suite for built-ins, sorted IDs for anything not
// covered by the registry (custom scenarios). Checks without a stored result
// are omitted; leftover results unknown to the registry are appended last.
func orderedEntries(run *runner.TestRun) []entry {
	if run == nil || len(run.Results) == 0 {
		return nil
	}

	type group struct {
		key string
		ids []string
	}
	var groups []group

	appendGroup := func(key string) {
		if key == "" {
			key = "suite"
		}
		for _, g := range groups {
			if g.key == key {
				return
			}
		}
		groups = append(groups, group{key: key})
	}

	addIDs := func(g *group, ids []string, seen map[string]bool) {
		for _, id := range ids {
			if _, ok := run.Results[id]; ok && !seen[id] {
				g.ids = append(g.ids, id)
				seen[id] = true
			}
		}
	}

	seen := make(map[string]bool, len(run.Results))
	if run.SuiteKey == "all" {
		for _, s := range registry.All() {
			groups = append(groups, group{key: s.Key})
			g := &groups[len(groups)-1]
			ids := make([]string, 0, len(s.Checks))
			for _, c := range s.Checks {
				ids = append(ids, c.ID)
			}
			addIDs(g, ids, seen)
			if len(g.ids) == 0 {
				groups = groups[:len(groups)-1]
			}
		}
	} else {
		appendGroup(run.SuiteKey)
		var ids []string
		if s, ok := registry.Get(run.SuiteKey); ok {
			for _, c := range s.Checks {
				ids = append(ids, c.ID)
			}
		}
		addIDs(&groups[0], ids, seen)
	}

	var rest []string
	for id := range run.Results {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)

	if len(groups) == 0 && len(rest) > 0 {
		appendGroup(run.SuiteKey)
	}
	if len(rest) > 0 {
		idx := -1
		for i := range groups {
			if groups[i].key == orKey(run.SuiteKey) {
				idx = i
				break
			}
		}
		if idx < 0 {
			groups = append(groups, group{key: orKey(run.SuiteKey)})
			idx = len(groups) - 1
		}
		groups[idx].ids = append(groups[idx].ids, rest...)
	}

	out := make([]entry, 0, len(run.Results))
	for _, g := range groups {
		for _, id := range g.ids {
			res := run.Results[id]
			out = append(out, entry{
				SuiteKey:   g.key,
				CheckID:    id,
				Title:      checkTitle(g.key, id),
				Status:     res.Status,
				Message:    res.Message,
				DurationMs: res.DurationMs,
			})
		}
	}
	return out
}

func orKey(k string) string {
	if k == "" {
		return "suite"
	}
	return k
}

// checkTitle resolves a human-readable title from the registry; custom
// scenario checks have no titles, so their IDs are used as names.
func checkTitle(suiteKey, checkID string) string {
	if s, ok := registry.Get(suiteKey); ok {
		for _, c := range s.Checks {
			if c.ID == checkID {
				return c.Title
			}
		}
	}
	return checkID
}

// suiteDisplayName returns a human-readable suite title: registry title for
// built-ins, the run's own name otherwise, falling back to the raw key.
func suiteDisplayName(run *runner.TestRun, suiteKey string) string {
	if s, ok := registry.Get(suiteKey); ok {
		return s.Title
	}
	if run != nil && suiteKey == run.SuiteKey && run.SuiteName != "" {
		return run.SuiteName
	}
	return suiteKey
}

// xmlEscape escapes text/attribute values for XML output. encoding/xml's
// EscapeText handles &, <, >, quotes and control characters and passes valid
// UTF-8 (including Cyrillic) through unchanged.
func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
