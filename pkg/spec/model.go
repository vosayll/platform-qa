// Package spec imports OpenAPI 3.x / Swagger 2.0 specifications, extracts
// their operations and generates ready-to-run smoke scenarios (one step per
// endpoint, expectStatus "!5xx") persisted through the regular scenario store.
package spec

import (
	"encoding/json"
	"time"
)

// GeneratedPrefix is the scenario key prefix owned by the spec importer.
// Regenerating wipes all previously generated scenarios with this prefix.
const GeneratedPrefix = "spec_smoke_"

// Param is a required request parameter extracted from a specification.
type Param struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"` // integer|number|boolean|string
}

// EndpointInfo describes a single API operation of an imported specification.
type EndpointInfo struct {
	Method        string          `json:"method"`
	Path          string          `json:"path"`
	Summary       string          `json:"summary,omitempty"`
	Tag           string          `json:"tag,omitempty"`
	HasBody       bool            `json:"hasBody"`
	RequiredPath  []string        `json:"requiredPath,omitempty"`
	RequiredQuery []Param         `json:"requiredQuery,omitempty"`
	ExampleBody   json.RawMessage `json:"exampleBody,omitempty"`
}

// Meta is the import summary persisted next to the raw specification.
type Meta struct {
	Key        string         `json:"key"`
	Title      string         `json:"title"`
	Version    string         `json:"version"` // "2.0" or "3.x.y"
	SourceURL  string         `json:"sourceUrl,omitempty"`
	ImportedAt time.Time      `json:"importedAt"`
	Endpoints  []EndpointInfo `json:"endpoints"`
}
