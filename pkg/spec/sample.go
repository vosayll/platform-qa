package spec

import (
	"encoding/json"
	"strings"
)

// sampleFromSchema builds a minimal JSON document satisfying the schema:
// required object properties only (capped at maxBodyFields), with
// example/default winning over synthesized values and $ref chains resolved.
func sampleFromSchema(root map[string]interface{}, schemaNode interface{}) interface{} {
	return buildSample(root, schemaNode, 1)
}

// buildSample synthesizes a value for one schema node. depth counts container
// nesting; containers beyond maxObjectDepth collapse to empty objects so that
// deep or cyclic schemas cannot blow up the generated body.
func buildSample(root map[string]interface{}, node interface{}, depth int) interface{} {
	schema := derefNode(root, node, map[string]bool{})
	if schema == nil {
		return "test"
	}
	if v, ok := explicitValue(schema["example"]); ok {
		return v
	}
	if v, ok := explicitValue(schema["default"]); ok {
		return v
	}
	if enums, ok := schema["enum"].([]interface{}); ok && len(enums) > 0 && enums[0] != nil {
		return enums[0]
	}

	typeStr := strings.TrimSpace(stringOf(schema["type"]))
	if typeStr == "" {
		if schema["properties"] != nil || schema["required"] != nil {
			typeStr = "object"
		} else if schema["items"] != nil {
			typeStr = "array"
		}
	}

	switch typeStr {
	case "object":
		if depth > maxObjectDepth {
			return map[string]interface{}{}
		}
		return objectSample(root, schema, depth)
	case "array":
		if depth > maxObjectDepth {
			return []interface{}{}
		}
		// arrays do not add an object nesting level themselves
		return []interface{}{buildSample(root, schema["items"], depth)}
	case "integer", "number":
		return 1
	case "boolean":
		return true
	case "string":
		return stringSample(schema)
	default:
		return "test"
	}
}

// objectSample collects required properties only (≤ maxBodyFields).
func objectSample(root map[string]interface{}, schema map[string]interface{}, depth int) interface{} {
	props, _ := schema["properties"].(map[string]interface{})
	required := requiredList(schema["required"])

	out := make(map[string]interface{}, len(required))
	for i, name := range required {
		if i >= maxBodyFields {
			break
		}
		propNode, ok := props[name]
		if !ok {
			out[name] = "test"
			continue
		}
		out[name] = buildSample(root, propNode, depth+1)
	}
	return out
}

// explicitValue reports a usable example/default: present and non-nil.
func explicitValue(v interface{}) (interface{}, bool) {
	if v == nil {
		return nil, false
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
		return nil, false
	}
	return v, true
}

func requiredList(raw interface{}) []string {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if name := stringOf(item); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// stringSample picks a realistic placeholder for well-known string formats.
func stringSample(schema map[string]interface{}) string {
	switch strings.ToLower(stringOf(schema["format"])) {
	case "email":
		return "test@test.io"
	case "date-time":
		return "2026-01-01T00:00:00Z"
	case "date":
		return "2026-01-01"
	case "uuid":
		return "123e4567-e89b-12d3-a456-426614174000"
	case "uri", "url":
		return "https://test.io/api"
	default:
		return "test"
	}
}

// marshalSample renders a synthesized value as raw JSON.
func marshalSample(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
