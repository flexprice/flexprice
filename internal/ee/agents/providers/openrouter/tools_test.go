package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/stretchr/testify/require"
)

func toolRequest() *Request {
	return &Request{
		Model: "test/model",
		Messages: []Message{
			{Role: RoleSystem, Content: []ContentPart{{Type: ContentTypeText, Text: "match"}}},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Type: ToolTypeFunction, Function: ToolCallFunction{Name: "search", Arguments: `{"q":"acme"}`}}}},
			{Role: RoleTool, ToolCallID: "call_1", Content: []ContentPart{{Type: ContentTypeText, Text: `[{"id":"cust_1"}]`}}},
		},
		Tools: []Tool{{Type: ToolTypeFunction, Function: ToolFunction{
			Name: "search", Description: "Search", Parameters: json.RawMessage(`{"type":"object"}`),
		}}},
		ToolChoice: &ToolChoice{Mode: ToolChoiceAuto},
	}
}

func TestComplete_SendsToolsAndParsesToolCalls(t *testing.T) {
	var raw map[string]any
	c := newTestClient(t, config.OpenRouterConfig{APIKey: "sk-test"}, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &raw))
		_, _ = w.Write([]byte(`{"id":"gen-2","model":"test/model","choices":[{"finish_reason":"tool_calls","message":{"content":null,"tool_calls":[{"id":"call_2","type":"function","function":{"name":"get","arguments":"{\"id\":\"cust_1\"}"}}]}}]}`))
	})

	resp, err := c.Complete(context.Background(), toolRequest())
	require.NoError(t, err)
	require.Equal(t, FinishReasonToolCalls, resp.FinishReason)
	require.Equal(t, []ToolCall{{ID: "call_2", Type: ToolTypeFunction, Function: ToolCallFunction{Name: "get", Arguments: `{"id":"cust_1"}`}}}, resp.ToolCalls)

	require.Equal(t, "auto", raw["tool_choice"])
	tools := raw["tools"].([]any)
	require.Len(t, tools, 1)
	require.Equal(t, "search", tools[0].(map[string]any)["function"].(map[string]any)["name"])

	msgs := raw["messages"].([]any)
	system := msgs[0].(map[string]any)
	require.IsType(t, []any{}, system["content"])
	assistant := msgs[1].(map[string]any)
	require.Nil(t, assistant["content"])
	require.Len(t, assistant["tool_calls"].([]any), 1)
	tool := msgs[2].(map[string]any)
	require.Equal(t, `[{"id":"cust_1"}]`, tool["content"])
	require.Equal(t, "call_1", tool["tool_call_id"])
}

func TestToolChoice_JSON(t *testing.T) {
	tests := []struct {
		name   string
		choice ToolChoice
		want   string
	}{
		{name: "mode", choice: ToolChoice{Mode: ToolChoiceRequired}, want: `"required"`},
		{name: "forced function", choice: ToolChoice{Function: "submit"}, want: `{"function":{"name":"submit"},"type":"function"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.choice)
			require.NoError(t, err)
			require.JSONEq(t, tt.want, string(b))

			var back ToolChoice
			require.NoError(t, json.Unmarshal(b, &back))
			require.Equal(t, tt.choice, back)
		})
	}
}

func TestMessage_RoundTrip(t *testing.T) {
	for _, m := range toolRequest().Messages {
		b, err := json.Marshal(m)
		require.NoError(t, err)
		var back Message
		require.NoError(t, json.Unmarshal(b, &back))
		require.Equal(t, m, back)
	}
}
