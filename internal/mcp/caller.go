package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// CallAPI performs an HTTP request to the FlexPrice API and returns the response body as JSON or an error.
// Path is the template (e.g. /v1/addons/{id}); path params are taken from args and interpolated.
// Query params are taken from args (names in queryParamNames) and added to the URL.
// For POST/PUT/PATCH, if hasBody is true, args["body"] is sent as JSON; otherwise no body.
func CallAPI(ctx context.Context, baseURL, apiKey string, def *ToolDef, args map[string]any) (responseBody []byte, statusCode int, isErr bool, err error) {
	path := interpolatePath(def.PathTemplate, args, def.PathParamNames)
	fullURL := strings.TrimSuffix(baseURL, "/") + path
	if len(def.QueryParamNames) > 0 {
		q := url.Values{}
		for _, name := range def.QueryParamNames {
			if v, ok := args[name]; ok && v != nil {
				q.Set(name, fmt.Sprint(v))
			}
		}
		if q.Encode() != "" {
			fullURL = fullURL + "?" + q.Encode()
		}
	}

	var body []byte
	if def.HasBody && (def.Method == http.MethodPost || def.Method == http.MethodPut || def.Method == http.MethodPatch) {
		if b, ok := args["body"]; ok && b != nil {
			body, err = json.Marshal(b)
			if err != nil {
				return nil, 0, true, fmt.Errorf("marshal body: %w", err)
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, def.Method, fullURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, true, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	client := &http.Client{Timeout: defaultTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, true, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, resp.StatusCode, true, fmt.Errorf("read response: %w", err)
	}
	code := resp.StatusCode
	isErr = code >= 400
	return buf.Bytes(), code, isErr, nil
}

func interpolatePath(template string, args map[string]any, pathParamNames []string) string {
	return pathParamRe.ReplaceAllStringFunc(template, func(match string) string {
		name := match[1 : len(match)-1]
		if v, ok := args[name]; ok && v != nil {
			return fmt.Sprint(v)
		}
		return match
	})
}
