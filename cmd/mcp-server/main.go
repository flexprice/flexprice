package main

import (
	"context"
	"log"
	"os"

	"github.com/flexprice/flexprice/internal/mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	baseURL := os.Getenv("FLEXPRICE_BASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("BASE_URL")
	}
	if baseURL == "" {
		log.Fatal("FLEXPRICE_BASE_URL or BASE_URL is required")
	}

	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("API_KEY")
	}
	if apiKey == "" {
		log.Fatal("FLEXPRICE_API_KEY or API_KEY is required")
	}

	specPath := os.Getenv("FLEXPRICE_OPENAPI_SPEC")
	if specPath == "" {
		specPath = "docs/swagger/swagger-3-0.json"
	}

	ctx := context.Background()
	doc, err := mcp.LoadSpec(ctx, specPath)
	if err != nil {
		log.Fatalf("load OpenAPI spec: %v", err)
	}

	cfg := &mcp.ServerConfig{
		BaseURL:  baseURL,
		APIKey:   apiKey,
		APIDoc:   doc,
		BasePath: "/v1",
	}
	server, err := mcp.NewServer(cfg)
	if err != nil {
		log.Fatalf("create MCP server: %v", err)
	}
	if server == nil {
		log.Fatal("MCP server is nil")
	}

	if err := server.Run(ctx, &mcpsdk.StdioTransport{}); err != nil {
		log.Fatalf("run MCP server: %v", err)
	}
}
