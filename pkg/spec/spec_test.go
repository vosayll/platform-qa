package spec

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"locali-e2e-engine/pkg/scenario"
)

const openapi3Doc = `{
  "openapi": "3.0.1",
  "info": {"title": "Demo API", "version": "1.0.0"},
  "paths": {
    "/users/{id}": {
      "get": {
        "tags": ["users"],
        "summary": "Получить пользователя",
        "operationId": "getUser",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "schema": {"type": "integer"}},
          {"name": "expand", "in": "query", "schema": {"type": "string"}}
        ]
      }
    },
    "/health": {
      "get": {
        "tags": ["system"],
        "summary": "Health check",
        "responses": {"200": {"description": "ok"}}
      }
    },
    "/orders": {
      "post": {
        "tags": ["orders"],
        "summary": "Создать заказ",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {"$ref": "#/components/schemas/CreateOrder"}
            }
          }
        },
        "responses": {"201": {"description": "created"}}
      }
    }
  },
  "components": {
    "schemas": {
      "CreateOrder": {
        "type": "object",
        "required": ["restId", "items"],
        "properties": {
          "restId": {"type": "string"},
          "items": {
            "type": "array",
            "items": {"$ref": "#/components/schemas/OrderItem"}
          },
          "comment": {"type": "string"}
        }
      },
      "OrderItem": {
        "type": "object",
        "required": ["productId", "qty"],
        "properties": {
          "productId": {"type": "string"},
          "qty": {"type": "integer"}
        }
      }
    }
  }
}`

func TestParseOpenAPI3(t *testing.T) {
	meta, err := Parse([]byte(openapi3Doc))
	require.NoError(t, err)

	assert.Equal(t, "Demo API", meta.Title)
	assert.Equal(t, "3.0.1", meta.Version)
	require.Len(t, meta.Endpoints, 3)

	// sorted by tag: orders < system < users
	assert.Equal(t, "POST", meta.Endpoints[0].Method)
	assert.Equal(t, "/orders", meta.Endpoints[0].Path)
	assert.Equal(t, "orders", meta.Endpoints[0].Tag)
	assert.True(t, meta.Endpoints[0].HasBody)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(meta.Endpoints[0].ExampleBody, &body))
	assert.Equal(t, "test", body["restId"])
	items, ok := body["items"].([]interface{})
	require.True(t, ok, "items must be an array")
	require.Len(t, items, 1)
	item := items[0].(map[string]interface{})
	assert.Equal(t, "test", item["productId"])
	assert.Equal(t, float64(1), item["qty"])
	assert.NotContains(t, body, "comment", "only required properties are generated")

	assert.Equal(t, "GET", meta.Endpoints[1].Method)
	assert.Equal(t, "/health", meta.Endpoints[1].Path)
	assert.False(t, meta.Endpoints[1].HasBody)

	users := meta.Endpoints[2]
	assert.Equal(t, "/users/{id}", users.Path)
	assert.Equal(t, []string{"id"}, users.RequiredPath)
	assert.Equal(t, "Получить пользователя", users.Summary)
}

func TestParseOpenAPI3RequiredQuery(t *testing.T) {
	doc := `{
	  "openapi": "3.0.0",
	  "info": {"title": "Q", "version": "1"},
	  "paths": {
	    "/search": {
	      "get": {
	        "operationId": "search",
	        "parameters": [
	          {"name": "page", "in": "query", "required": true, "schema": {"type": "integer"}},
	          {"name": "active", "in": "query", "required": true, "schema": {"type": "boolean"}},
	          {"name": "q", "in": "query", "required": true, "schema": {"type": "string"}}
	        ],
	        "responses": {"200": {"description": "ok"}}
	      }
	    }
	  }
	}`
	meta, err := Parse([]byte(doc))
	require.NoError(t, err)
	require.Len(t, meta.Endpoints[0].RequiredQuery, 3)

	scenarios := GenerateScenarios(meta)
	require.Len(t, scenarios, 1)
	step := scenarios[0].Steps[0]
	assert.Equal(t, "/search?active=true&page=1&q=test", step.Path)
}

const swagger2Doc = `{
  "swagger": "2.0",
  "info": {"title": "Legacy API", "version": "2.0"},
  "paths": {
    "/pets": {
      "post": {
        "tags": ["pets"],
        "operationId": "addPet",
        "parameters": [
          {"name": "limit", "in": "query", "type": "integer", "required": true},
          {"name": "body", "in": "body", "required": true, "schema": {"$ref": "#/definitions/Pet"}}
        ],
        "responses": {"201": {"description": "created"}}
      },
      "get": {
        "tags": ["pets"],
        "summary": "List pets",
        "parameters": [
          {"name": "offset", "in": "query", "type": "integer"}
        ],
        "responses": {"200": {"description": "ok"}}
      }
    }
  },
  "definitions": {
    "Pet": {
      "type": "object",
      "required": ["name", "email", "bornAt"],
      "properties": {
        "name": {"type": "string"},
        "email": {"type": "string", "format": "email"},
        "bornAt": {"type": "string", "format": "date-time"},
        "age": {"type": "integer"},
        "friendly": {"type": "boolean"}
      }
    }
  }
}`

func TestParseSwagger2(t *testing.T) {
	meta, err := Parse([]byte(swagger2Doc))
	require.NoError(t, err)

	assert.Equal(t, "2.0", meta.Version)
	require.Len(t, meta.Endpoints, 2)

	// sorted by tag, then path, then method: GET /pets before POST /pets
	get := meta.Endpoints[0]
	assert.Equal(t, "GET", get.Method)
	assert.False(t, get.HasBody)
	assert.Nil(t, get.RequiredQuery)

	post := meta.Endpoints[1]
	assert.Equal(t, "POST", post.Method)
	assert.True(t, post.HasBody)
	assert.Equal(t, "addPet", post.Summary, "summary falls back to operationId")
	require.Len(t, post.RequiredQuery, 1)
	assert.Equal(t, Param{Name: "limit", Type: "integer"}, post.RequiredQuery[0])

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(post.ExampleBody, &body))
	assert.Equal(t, "test", body["name"])
	assert.Equal(t, "test@test.io", body["email"])
	assert.Equal(t, "2026-01-01T00:00:00Z", body["bornAt"])
	assert.NotContains(t, body, "age")
	assert.NotContains(t, body, "friendly")
}

func TestParseYAMLAndGarbage(t *testing.T) {
	yamlDoc := `
openapi: 3.0.0
info:
  title: YAML API
  version: "1.0"
paths:
  /ping:
    get:
      tags: [sys]
      summary: Ping
      responses:
        '200':
          description: pong
`
	meta, err := Parse([]byte(yamlDoc))
	require.NoError(t, err)
	assert.Equal(t, "YAML API", meta.Title)
	require.Len(t, meta.Endpoints, 1)
	assert.Equal(t, "sys", meta.Endpoints[0].Tag)

	_, err = Parse([]byte("это не спецификация"))
	require.Error(t, err)

	_, err = Parse(nil)
	require.Error(t, err)
}

func TestGenerateScenariosTagGroupsAndChunks(t *testing.T) {
	meta, err := Parse([]byte(openapi3Doc))
	require.NoError(t, err)

	scenarios := GenerateScenarios(meta)
	require.Len(t, scenarios, 3) // orders, system, users

	byKey := map[string]*scenario.Scenario{}
	for _, sc := range scenarios {
		byKey[sc.Key] = sc
		assert.Equal(t, "custom", sc.Category)
		assert.Equal(t, []string{"spec"}, sc.Tags)
		assert.Equal(t, "Автосгенерировано из спецификации", sc.Description)
		assert.NoError(t, sc.Validate())
	}

	orders := byKey["spec_smoke_orders"]
	require.NotNil(t, orders)
	assert.Equal(t, "Smoke: orders", orders.Title)
	require.Len(t, orders.Steps, 1)

	st := orders.Steps[0]
	assert.Equal(t, "post_orders", st.ID)
	assert.Equal(t, "[POST] /orders — Создать заказ", st.Title)
	assert.Equal(t, "http", st.Type)
	assert.Equal(t, "none", st.Role)
	assert.Equal(t, "!5xx", st.ExpectStatus)
	assert.NotNil(t, st.Body)

	sys := byKey["spec_smoke_system"]
	require.NotNil(t, sys)
	assert.Equal(t, "get_health", sys.Steps[0].ID)
	assert.Equal(t, "[GET] /health — Health check", sys.Steps[0].Title)
	assert.Equal(t, "/health", sys.Steps[0].Path)

	users := byKey["spec_smoke_users"]
	require.NotNil(t, users)
	assert.Equal(t, "/users/1", users.Steps[0].Path, "{id} must be substituted with 1")
}

func TestGenerateScenariosUntaggedChunking(t *testing.T) {
	endpoints := make([]EndpointInfo, 0, 85)
	for i := 0; i < 85; i++ {
		endpoints = append(endpoints, EndpointInfo{
			Method: "GET",
			Path:   fmt.Sprintf("/resource%d", i),
			Summary: fmt.Sprintf("Resource %d", i),
		})
	}
	scenarios := GenerateScenarios(&Meta{Endpoints: endpoints})
	require.Len(t, scenarios, 3)
	assert.Equal(t, "spec_smoke_part1", scenarios[0].Key)
	assert.Equal(t, "spec_smoke_part2", scenarios[1].Key)
	assert.Equal(t, "spec_smoke_part3", scenarios[2].Key)
	assert.Len(t, scenarios[0].Steps, chunkSize)
	assert.Len(t, scenarios[2].Steps, 5)
	for _, sc := range scenarios {
		assert.NoError(t, sc.Validate())
	}
}

func TestSampleFromSchemaExamplePriorityAndDepth(t *testing.T) {
	root := map[string]interface{}{
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"Deep": map[string]interface{}{
					"type": "object",
					"required": []interface{}{"inner"},
					"properties": map[string]interface{}{
						"inner": map[string]interface{}{"type": "object"},
					},
				},
				"Leaf": map[string]interface{}{"type": "string"},
			},
		},
	}
	schema := map[string]interface{}{
		"type": "object",
		"required": []interface{}{"a", "b", "c", "d"},
		"properties": map[string]interface{}{
			"a": map[string]interface{}{"type": "string", "example": "custom-example"},
			"b": map[string]interface{}{"type": "string", "enum": []interface{}{"first", "second"}},
			"c": map[string]interface{}{"$ref": "#/components/schemas/Deep"},
			"d": map[string]interface{}{"$ref": "#/components/schemas/Leaf"},
		},
	}
	sample := sampleFromSchema(root, schema).(map[string]interface{})
	assert.Equal(t, "custom-example", sample["a"], "example wins over synthesis")
	assert.Equal(t, "first", sample["b"], "enum picks its first value")

	deep := sample["c"].(map[string]interface{})
	inner, ok := deep["inner"].(map[string]interface{})
	require.True(t, ok)
	assert.Empty(t, inner, "object depth capped at 2 levels")
	assert.Equal(t, "test", sample["d"], "$ref chains are resolved")
}

func TestStoreSaveLoadDelete(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(dir)
	require.NoError(t, err)

	_, ok := st.Meta()
	assert.False(t, ok, "empty store has no meta")

	raw := []byte(openapi3Doc)
	meta, err := Parse(raw)
	require.NoError(t, err)
	meta.SourceURL = "http://example.com/spec.json"
	require.NoError(t, st.Save(meta, raw))

	gotMeta, ok := st.Meta()
	require.True(t, ok)
	assert.Equal(t, meta.Key, gotMeta.Key)
	assert.Len(t, gotMeta.Endpoints, 3)

	gotRaw, ok := st.Raw()
	require.True(t, ok)
	assert.JSONEq(t, openapi3Doc, string(gotRaw))

	// Reload from disk into a fresh store instance.
	reloaded, err := NewStore(dir)
	require.NoError(t, err)
	_, ok = reloaded.Meta()
	require.True(t, ok, "meta must survive restart")

	require.NoError(t, reloaded.Delete())
	_, ok = reloaded.Meta()
	assert.False(t, ok)

	again, err := NewStore(dir)
	require.NoError(t, err)
	_, ok = again.Meta()
	assert.False(t, ok, "delete must be persisted")
}

func TestSlugifyAndStepIDs(t *testing.T) {
	assert.Equal(t, "user_accounts", slugify("/User-Accounts/", maxKeySlugLen))
	assert.Equal(t, "", slugify("!!!", maxKeySlugLen))
	assert.Equal(t, "abcde", slugify("abcde", 5))
	assert.Equal(t, "abcd", slugify("abcdef", 4))

	assert.Equal(t, "get_users_orders", stepID("GET", "/users/{id}/orders"))
	assert.Equal(t, "post_pets", stepID("post", "/pets"))
	assert.Equal(t, "get_root", stepID("GET", "/{id}"))
}
