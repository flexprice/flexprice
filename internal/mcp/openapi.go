package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/getkin/kin-openapi/openapi3"
)

// Op describes a single OpenAPI operation for MCP tool registration.
type Op struct {
	Method      string
	Path        string
	Summary     string
	Description string
	OperationID string
	Parameters  openapi3.Parameters
	RequestBody *openapi3.RequestBodyRef
}

// LoadSpec loads an OpenAPI 3 spec from path.
// Validation is skipped (direct json.Unmarshal) so specs with minor issues
// (e.g. duplicate parameter names across path/operation) still load.
func LoadSpec(ctx context.Context, path string) (*openapi3.T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read openapi spec: %w", err)
	}
	var doc openapi3.T
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse openapi spec: %w", err)
	}
	_ = ctx
	return &doc, nil
}

// EachOperation calls fn for every (path, method, operation) in the spec.
// basePath is prepended to path (e.g. /v1). Paths from doc are used as-is;
// callers should pass doc.Servers or basePath from spec.
func EachOperation(doc *openapi3.T, basePath string, fn func(path, method string, op *Op)) {
	if doc == nil || doc.Paths == nil {
		return
	}
	pathPrefix := basePath
	if pathPrefix != "" && pathPrefix[0] != '/' {
		pathPrefix = "/" + pathPrefix
	}
	for path, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		fullPath := pathPrefix + path
		for method, operation := range pathItem.Operations() {
			if operation == nil {
				continue
			}
			params := pathItem.Parameters
			if len(operation.Parameters) > 0 {
				merged := make(openapi3.Parameters, 0, len(params)+len(operation.Parameters))
				merged = append(merged, params...)
				merged = append(merged, operation.Parameters...)
				params = merged
			}
			fn(fullPath, method, &Op{
				Method:      method,
				Path:        fullPath,
				Summary:     operation.Summary,
				Description: operation.Description,
				OperationID: operation.OperationID,
				Parameters:  params,
				RequestBody: operation.RequestBody,
			})
		}
	}
}
