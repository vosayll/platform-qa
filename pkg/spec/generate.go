package spec

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"locali-e2e-engine/pkg/scenario"
)

const (
	chunkSize = 40 // untagged endpoints per fallback scenario
	// maxKeySlugLen keeps "spec_smoke_<slug>" within the scenario key limit
	// of 40 characters enforced by scenario.Validate.
	maxKeySlugLen = 29
	maxStepIDLen  = 60
)

var slugSanitize = regexp.MustCompile(`[^a-z0-9]+`)

// GenerateScenarios turns an imported specification into smoke scenarios:
// one scenario per tag (chunked by chunkSize when a tag exceeds the
// scenario step limit), endpoints without tags are grouped into
// spec_smoke_part1..N chunks of at most chunkSize steps.
func GenerateScenarios(meta *Meta) []*scenario.Scenario {
	if meta == nil {
		return nil
	}

	tagged := make(map[string][]EndpointInfo)
	var untagged []EndpointInfo
	for _, ep := range meta.Endpoints {
		tag := strings.TrimSpace(ep.Tag)
		if tag == "" {
			untagged = append(untagged, ep)
			continue
		}
		tagged[tag] = append(tagged[tag], ep)
	}

	tags := make([]string, 0, len(tagged))
	for t := range tagged {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	type group struct {
		title  string
		items  []EndpointInfo
		keyFor func(part int) string
	}
	groups := make([]group, 0, len(tags)+1)

	for gi, tag := range tags {
		slug := slugify(tag, maxKeySlugLen)
		if slug == "" {
			slug = fmt.Sprintf("group%d", gi+1)
		}
		base := "spec_smoke_" + slug
		groups = append(groups, group{
			title: "Smoke: " + tag,
			items: tagged[tag],
			keyFor: func(part int) string {
				if part == 1 {
					return base
				}
				return fmt.Sprintf("%s_part%d", base, part)
			},
		})
	}
	if len(untagged) > 0 {
		groups = append(groups, group{
			title: "Smoke",
			items: untagged,
			keyFor: func(part int) string {
				if part == 1 {
					return "spec_smoke_part1"
				}
				return fmt.Sprintf("spec_smoke_part%d", part)
			},
		})
	}

	usedKeys := map[string]bool{}
	out := make([]*scenario.Scenario, 0, len(groups))
	for _, g := range groups {
		chunks := (len(g.items) + chunkSize - 1) / chunkSize
		for i := 0; i < len(g.items); i += chunkSize {
			end := i + chunkSize
			if end > len(g.items) {
				end = len(g.items)
			}
			part := i/chunkSize + 1
			key := uniqueKey(usedKeys, g.keyFor(part))
			title := g.title
			if chunks > 1 {
				title = fmt.Sprintf("%s (%d/%d)", title, part, chunks)
			}
			out = append(out, buildScenario(key, title, g.items[i:end]))
		}
	}

	return out
}

// buildScenario assembles one smoke scenario from endpoint descriptors,
// validating uniqueness of step ids within the scenario.
func buildScenario(key, title string, endpoints []EndpointInfo) *scenario.Scenario {
	steps := make([]scenario.Step, 0, len(endpoints))
	seenIDs := map[string]bool{}
	for _, ep := range endpoints {
		id := uniqueID(seenIDs, stepID(ep.Method, ep.Path))
		steps = append(steps, endpointStep(ep, id))
	}
	return &scenario.Scenario{
		Key:         key,
		Title:       title,
		Description: "Автосгенерировано из спецификации",
		Tags:        []string{"spec"},
		Category:    "custom",
		Steps:       steps,
	}
}

func endpointStep(ep EndpointInfo, id string) scenario.Step {
	method := strings.ToUpper(ep.Method)

	title := "[" + method + "] " + ep.Path
	if summary := strings.TrimSpace(ep.Summary); summary != "" {
		title += " — " + summary
	}

	path := concretePath(ep.Path)
	if qs := queryString(ep.RequiredQuery); qs != "" {
		path += "?" + qs
	}

	step := scenario.Step{
		ID:           id,
		Title:        title,
		Type:         "http",
		Role:         "none",
		Method:       method,
		Path:         path,
		ExpectStatus: "!5xx", // success = any non-5xx answer (401/404 are contract-compliant)
	}
	if ep.HasBody && len(ep.ExampleBody) > 0 {
		step.Body = append(json.RawMessage{}, ep.ExampleBody...)
	}
	return step
}

// concretePath substitutes every path placeholder with a literal sample value
// ({id} -> 1).
func concretePath(p string) string {
	return pathParamRe.ReplaceAllString(p, "1")
}

// queryString renders required query parameters with typed sample values.
func queryString(params []Param) string {
	if len(params) == 0 {
		return ""
	}
	parts := make([]string, 0, len(params))
	for _, p := range params {
		value := "test"
		switch p.Type {
		case "integer", "number":
			value = "1"
		case "boolean":
			value = "true"
		}
		parts = append(parts, url.QueryEscape(p.Name)+"="+url.QueryEscape(value))
	}
	return strings.Join(parts, "&")
}

// stepID builds "<method>_<path_snake>": slashes become underscores and path
// placeholders are dropped (/users/{id}/orders -> get_users_orders).
func stepID(method, path string) string {
	noParams := pathParamRe.ReplaceAllString(path, "")
	slug := slugify(noParams, maxStepIDLen)
	if slug == "" {
		slug = "root"
	}
	return strings.ToLower(strings.TrimSpace(method)) + "_" + slug
}

// slugify normalizes free-form text into a bounded snake_case fragment.
func slugify(s string, maxLen int) string {
	clean := slugSanitize.ReplaceAllString(strings.ToLower(s), "_")
	clean = strings.Trim(clean, "_")
	if len(clean) > maxLen {
		clean = strings.TrimRight(clean[:maxLen], "_")
	}
	return clean
}

// uniqueKey returns a collision-free scenario key within the 40-char limit.
func uniqueKey(used map[string]bool, base string) string {
	base = slugify(base, 40)
	if !used[base] && base != "" {
		used[base] = true
		return base
	}
	for n := 2; ; n++ {
		suffix := fmt.Sprintf("_%d", n)
		root := base
		if len(root)+len(suffix) > 40 {
			root = strings.TrimRight(root[:40-len(suffix)], "_")
		}
		candidate := root + suffix
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

// uniqueID disambiguates duplicate step ids inside one scenario.
func uniqueID(used map[string]bool, base string) string {
	if base == "" {
		base = "endpoint"
	}
	if !used[base] {
		used[base] = true
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s_%d", base, n)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}
