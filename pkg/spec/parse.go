package spec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pb33f/libopenapi"
	"gopkg.in/yaml.v3"
)

const (
	maxBodyFields   = 12
	maxObjectDepth  = 2
	maxRefHops      = 16
	maxBodyBytes    = 8 << 20 // safety cap when reading downloaded specs
	specMethodCount = 5
)

var httpMethods = [specMethodCount]string{"get", "post", "put", "patch", "delete"}

var pathParamRe = regexp.MustCompile(`\{([^{}]+)\}`)

// Parse validates a raw OpenAPI/Swagger document and extracts every operation
// (get/post/put/patch/delete) into a Meta summary. JSON and YAML inputs are
// accepted; Swagger 2.0 and OpenAPI 3.x are supported.
func Parse(raw []byte) (*Meta, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, fmt.Errorf("спецификация пуста")
	}

	norm, err := normalizeToJSON(raw)
	if err != nil {
		return nil, err
	}

	// libopenapi performs the structural validation and reliable version
	// detection for both dialects; the extraction below walks plain JSON so
	// that $ref resolution stays under our control.
	doc, docErr := libopenapi.NewDocument(norm)
	if docErr != nil {
		return nil, fmt.Errorf("спецификация не читается: %v", docErr)
	}
	info := doc.GetSpecInfo()

	var root map[string]interface{}
	if err := json.Unmarshal(norm, &root); err != nil {
		return nil, fmt.Errorf("спецификация не является JSON-объектом: %v", err)
	}

	specType := ""
	if info != nil {
		specType = info.SpecType
	}
	version := detectVersion(root, specType)
	if version == "" {
		return nil, fmt.Errorf("не удалось определить версию спецификации: ожидался swagger \"2.0\" или openapi \"3.x\"")
	}

	meta := &Meta{
		Key:        specKey(root, version),
		Title:      infoTitle(root),
		Version:    version,
		ImportedAt: time.Now().UTC(),
	}

	endpoints := extractEndpoints(root, version)
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("в спецификации не найдено ни одной операции (get/post/put/patch/delete)")
	}
	meta.Endpoints = endpoints
	return meta, nil
}

// normalizeToJSON passes JSON through untouched and converts YAML documents.
func normalizeToJSON(raw []byte) ([]byte, error) {
	var probe interface{}
	if err := json.Unmarshal(raw, &probe); err == nil {
		return raw, nil
	}
	var y interface{}
	if err := yaml.Unmarshal(raw, &y); err != nil {
		return nil, fmt.Errorf("документ не является корректным JSON или YAML")
	}
	out, err := json.Marshal(y)
	if err != nil {
		return nil, fmt.Errorf("не удалось преобразовать YAML в JSON: %v", err)
	}
	return out, nil
}

func detectVersion(root map[string]interface{}, specType string) string {
	if v, _ := root["swagger"].(string); strings.TrimSpace(v) == "2.0" {
		return "2.0"
	}
	if v, ok := root["openapi"].(string); ok && strings.HasPrefix(strings.TrimSpace(v), "3") {
		return strings.TrimSpace(v)
	}
	switch strings.TrimSpace(specType) {
	case "swagger":
		return "2.0"
	case "openapi":
		return "3"
	}
	return ""
}

func infoTitle(root map[string]interface{}) string {
	info, _ := root["info"].(map[string]interface{})
	title, _ := info["title"].(string)
	return strings.TrimSpace(title)
}

func specKey(root map[string]interface{}, version string) string {
	key := slugify(infoTitle(root), 40)
	if key == "" {
		key = slugify(version, 40)
	}
	if key == "" {
		key = "spec"
	}
	return key
}

func extractEndpoints(root map[string]interface{}, version string) []EndpointInfo {
	isV2 := version == "2.0"
	paths, _ := root["paths"].(map[string]interface{})

	out := make([]EndpointInfo, 0)
	for p, rawItem := range paths {
		item, ok := rawItem.(map[string]interface{})
		if !ok || p == "" || p[0] != '/' {
			continue
		}
		itemParams := parseParams(item["parameters"])

		for _, m := range httpMethods {
			rawOp, ok := item[m]
			if !ok {
				continue
			}
			op, ok := rawOp.(map[string]interface{})
			if !ok {
				continue
			}
			ep := EndpointInfo{
				Method:       strings.ToUpper(m),
				Path:         p,
				Summary:      firstNonEmptyString(op["summary"], op["operationId"]),
				Tag:          firstTag(op["tags"]),
				RequiredPath: pathPlaceholders(p),
			}

			params := mergeParams(itemParams, parseParams(op["parameters"]))
			if isV2 {
				schema, hasBody := v2BodySchema(params)
				ep.HasBody = hasBody
				if hasBody {
					ep.ExampleBody = marshalSample(sampleFromSchema(root, schema))
				}
			} else {
				schema, hasBody := v3BodySchema(root, op)
				ep.HasBody = hasBody
				if hasBody {
					ep.ExampleBody = marshalSample(sampleFromSchema(root, schema))
				}
			}
			ep.RequiredQuery = requiredQueryParams(root, params, isV2)
			out = append(out, ep)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Tag != out[j].Tag {
			return out[i].Tag < out[j].Tag
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// param is the normalized intersection of Swagger 2.0 / OpenAPI 3 parameters.
type param struct {
	Name     string
	In       string
	Required bool
	Schema   interface{} // v2: parameter itself carries type; v3: schema object
	Type     string      // v2 scalar type
}

func parseParams(raw interface{}) []param {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]param, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		p := param{
			Name:    stringOf(m["name"]),
			In:      stringOf(m["in"]),
			Schema:  m["schema"],
			Type:    stringOf(m["type"]),
			Required: boolOf(m["required"], false),
		}
		if p.Name == "" || p.In == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// mergeParams overlays operation-level parameters on path-item-level ones
// (same name+in wins).
func mergeParams(base, overlay []param) []param {
	out := make([]param, 0, len(base)+len(overlay))
	out = append(out, base...)
	for _, o := range overlay {
		replaced := false
		for i, b := range out {
			if b.Name == o.Name && b.In == o.In {
				out[i] = o
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, o)
		}
	}
	return out
}

func v2BodySchema(params []param) (interface{}, bool) {
	for _, p := range params {
		if p.In == "body" && p.Schema != nil {
			return p.Schema, true
		}
	}
	return nil, false
}

func v3BodySchema(root map[string]interface{}, op map[string]interface{}) (interface{}, bool) {
	rb := derefNode(root, op["requestBody"], map[string]bool{})
	if rb == nil {
		return nil, false
	}
	content, ok := rb["content"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	mediaName := ""
	if _, ok := content["application/json"]; ok {
		mediaName = "application/json"
	} else if _, ok := content["application/json; charset=utf-8"]; ok {
		mediaName = "application/json; charset=utf-8"
	} else {
		names := make([]string, 0, len(content))
		for k := range content {
			names = append(names, k)
		}
		sort.Strings(names)
		if len(names) > 0 {
			mediaName = names[0]
		}
	}
	media, ok := content[mediaName].(map[string]interface{})
	if !ok {
		return nil, false
	}
	return media["schema"], media["schema"] != nil
}

func requiredQueryParams(root map[string]interface{}, params []param, isV2 bool) []Param {
	out := make([]Param, 0)
	for _, p := range params {
		if p.In != "query" || !p.Required {
			continue
		}
		t := p.Type
		if !isV2 && t == "" && p.Schema != nil {
			s := derefNode(root, p.Schema, map[string]bool{})
			t = stringOf(s["type"])
		}
		out = append(out, Param{Name: p.Name, Type: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) == 0 {
		return nil
	}
	return out
}

func pathPlaceholders(p string) []string {
	matches := pathParamRe.FindAllStringSubmatch(p, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func firstNonEmptyString(vals ...interface{}) string {
	for _, v := range vals {
		if s := stringOf(v); s != "" {
			return s
		}
	}
	return ""
}

func firstTag(raw interface{}) string {
	arr, ok := raw.([]interface{})
	if !ok {
		return ""
	}
	for _, item := range arr {
		if s := stringOf(item); s != "" {
			return s
		}
	}
	return ""
}

func stringOf(v interface{}) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func boolOf(v interface{}, def bool) bool {
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}

// nodeRef returns the $ref value when the node is a reference object.
func nodeRef(node interface{}) string {
	m, ok := node.(map[string]interface{})
	if !ok {
		return ""
	}
	ref, _ := m["$ref"].(string)
	return ref
}

// derefNode follows "$ref" chains (up to maxRefHops) and returns the target
// as a map. visited tracks refs along the current branch only; a cycle or a
// dangling ref yields nil.
func derefNode(root map[string]interface{}, node interface{}, visited map[string]bool) map[string]interface{} {
	cur := node
	for hop := 0; hop < maxRefHops; hop++ {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		ref := nodeRef(m)
		if ref == "" {
			return m
		}
		if visited[ref] {
			return nil
		}
		target := resolvePointer(root, ref)
		if target == nil {
			return nil
		}
		next := make(map[string]bool, len(visited)+1)
		for k := range visited {
			next[k] = true
		}
		next[ref] = true
		visited = next
		cur = target
	}
	return nil
}

// resolvePointer resolves a "#/a/b" style JSON pointer against root,
// un-escaping ~0/~1 sequences. A nil root treats the node as inline data.
func resolvePointer(root map[string]interface{}, ref string) interface{} {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if i := strings.Index(ref, "#"); i >= 0 {
		ref = ref[i+1:]
	}
	ref = strings.TrimPrefix(ref, "/")
	if ref == "" {
		return root
	}
	var cur interface{} = root
	for _, token := range strings.Split(ref, "/") {
		token = strings.ReplaceAll(token, "~1", "/")
		token = strings.ReplaceAll(token, "~0", "~")
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		next, ok := m[token]
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}
