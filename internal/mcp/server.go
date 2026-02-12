package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/getkin/kin-openapi/openapi3"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerConfig holds base URL, API key, and optional spec path for the MCP adapter.
type ServerConfig struct {
	BaseURL   string
	APIKey    string
	SpecPath  string
	APIDoc    *openapi3.T
	BasePath  string
}

// NewServer creates an MCP server and registers one tool per OpenAPI operation.
func NewServer(cfg *ServerConfig) (*mcpsdk.Server, error) {
	doc := cfg.APIDoc
	if doc == nil {
		return nil, nil
	}
	basePath := cfg.BasePath
	if basePath == "" {
		basePath = "/v1"
	}

	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "flexprice",
		Version: "1.0",
	}, &mcpsdk.ServerOptions{
		Logger: slog.Default(),
	})

	EachOperation(doc, basePath, func(path, method string, op *Op) {
		def := ToToolDef(op)
		tool := &mcpsdk.Tool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: def.InputSchema,
		}
		handler := makeToolHandler(cfg.BaseURL, cfg.APIKey, def)
		server.AddTool(tool, handler)
	})

	return server, nil
}

func makeToolHandler(baseURL, apiKey string, def *ToolDef) mcpsdk.ToolHandler {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		var args map[string]any
		if req.Params != nil && req.Params.Arguments != nil {
			// Arguments is json.RawMessage ([]byte) in the SDK.
			raw := req.Params.Arguments
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &args)
			}
		}
		if args == nil {
			args = make(map[string]any)
		}

		body, _, isErr, err := CallAPI(ctx, baseURL, apiKey, def, args)
		if err != nil {
			res := &mcpsdk.CallToolResult{}
			res.SetError(err)
			return res, nil
		}

		text := string(body)
		if text == "" {
			text = "OK"
		}
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
			IsError: isErr,
		}, nil
	}
}
