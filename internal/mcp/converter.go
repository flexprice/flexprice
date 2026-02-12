package mcp

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// ToolDef holds the MCP tool name, description, and input schema for one operation.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
	// PathTemplate is the OpenAPI path with {param} placeholders for CallAPI.
	PathTemplate string
	Method       string
	// PathParamNames are required for path interpolation.
	PathParamNames []string
	// QueryParamNames for building query string.
	QueryParamNames []string
	// HasBody is true if the operation has a request body (use "body" in args).
	HasBody bool
}

var (
	pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)
	slugRe      = regexp.MustCompile(`[^a-zA-Z0-9]+`)
)

// ToToolDef converts an OpenAPI Op into an MCP ToolDef (name, description, inputSchema).
func ToToolDef(op *Op) *ToolDef {
	name := op.OperationID
	if name == "" {
		name = deriveToolName(op.Method, op.Path)
	}
	desc := op.Summary
	if op.Description != "" {
		if desc != "" {
			desc = desc + ". " + op.Description
		} else {
			desc = op.Description
		}
	}
	schema := buildInputSchema(op)
	pathParams, queryParams := paramNamesByIn(op.Parameters)
	hasBody := op.RequestBody != nil && op.RequestBody.Value != nil
	return &ToolDef{
		Name:           name,
		Description:    desc,
		InputSchema:    schema,
		PathTemplate:   op.Path,
		Method:         strings.ToUpper(op.Method),
		PathParamNames: pathParams,
		QueryParamNames: queryParams,
		HasBody:        hasBody,
	}
}

func deriveToolName(method, path string) string {
	m := strings.ToLower(method)
	// Remove leading slash and base path segments; replace {param} with param name.
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimPrefix(path, "v1/")
	path = pathParamRe.ReplaceAllString(path, "_$1")
	parts := slugRe.Split(path, -1)
	var tokens []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		tokens = append(tokens, p)
	}
	if len(tokens) == 0 {
		return m
	}
	return m + "_" + strings.Join(tokens, "_")
}

func paramNamesByIn(params openapi3.Parameters) (pathNames, queryNames []string) {
	for _, ref := range params {
		if ref == nil || ref.Value == nil {
			continue
		}
		p := ref.Value
		switch p.In {
		case "path":
			pathNames = append(pathNames, p.Name)
		case "query":
			queryNames = append(queryNames, p.Name)
		}
	}
	return pathNames, queryNames
}

func buildInputSchema(op *Op) map[string]any {
	properties := make(map[string]any)
	var required []string

	for _, ref := range op.Parameters {
		if ref == nil || ref.Value == nil {
			continue
		}
		p := ref.Value
		prop := paramSchemaToJSONSchema(p)
		properties[p.Name] = prop
		if p.Required {
			required = append(required, p.Name)
		}
	}

	if op.RequestBody != nil && op.RequestBody.Value != nil {
		body := op.RequestBody.Value
		if body.Content != nil {
			if mt := body.GetMediaType("application/json"); mt != nil && mt.Schema != nil {
				bodySchema := schemaRefToMap(mt.Schema)
				if bodySchema != nil {
					properties["body"] = bodySchema
					if body.Required {
						required = append(required, "body")
					}
				}
			}
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func paramSchemaToJSONSchema(p *openapi3.Parameter) map[string]any {
	if p.Schema != nil {
		m := schemaRefToMap(p.Schema)
		if m != nil {
			if p.Description != "" {
				m["description"] = p.Description
			}
			return m
		}
	}
	// Fallback
	m := map[string]any{"type": "string"}
	if p.Description != "" {
		m["description"] = p.Description
	}
	return m
}

func schemaRefToMap(ref *openapi3.SchemaRef) map[string]any {
	if ref == nil {
		return nil
	}
	// Marshal the ref to JSON and back to map to get a JSON-Schema-like object.
	// This preserves $ref or inline schema.
	data, err := json.Marshal(ref)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}
