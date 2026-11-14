package openrouter

import (
	"encoding/json"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

type Request struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Plugins        []Plugin        `json:"plugins,omitempty"`
	Provider       *Provider       `json:"provider,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`

	Tools             []Tool      `json:"tools,omitempty"`
	ToolChoice        *ToolChoice `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool       `json:"parallel_tool_calls,omitempty"`
}

type Message struct {
	Role       string        `json:"role"`
	Content    []ContentPart `json:"content"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolChoice is "auto", "required" or "none" when Function is empty, otherwise it forces that function.
type ToolChoice struct {
	Mode     string
	Function string
}

type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	File     *FilePart `json:"file,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL string `json:"url"`
}

type FilePart struct {
	Filename string `json:"filename"`
	FileData string `json:"file_data"`
}

type ResponseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

type JSONSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type Plugin struct {
	ID  string     `json:"id"`
	PDF *PDFConfig `json:"pdf,omitempty"`
}

type PDFConfig struct {
	Engine string `json:"engine"`
}

type Provider struct {
	DataCollection    string `json:"data_collection,omitempty"`
	ZDR               *bool  `json:"zdr,omitempty"`
	RequireParameters bool   `json:"require_parameters,omitempty"`
}

type completionResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string     `json:"content"`
			Refusal   string     `json:"refusal"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		Cost             float64 `json:"cost"`
	} `json:"usage"`
	Error *struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
	} `json:"error"`
}

type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

type Response struct {
	ID           string     `json:"id"`
	Model        string     `json:"model"`
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"`
	Usage        *Usage     `json:"usage"`
}

func (r *Request) hasFilePart() bool {
	for _, m := range r.Messages {
		for _, p := range m.Content {
			if p.File != nil {
				return true
			}
		}
	}
	return false
}

func (cr *completionResponse) toResponse() (*Response, error) {
	if cr.Error != nil && cr.Error.Message != "" {
		return nil, ierr.NewError("openrouter returned an error").
			WithHint("AI provider rejected the request").
			WithReportableDetails(map[string]interface{}{"message": cr.Error.Message}).
			Mark(ierr.ErrHTTPClient)
	}
	if len(cr.Choices) == 0 {
		return nil, ierr.NewError("no choices in openrouter response").
			WithHint("AI provider returned an empty result").
			Mark(ierr.ErrHTTPClient)
	}
	choice := cr.Choices[0]
	if choice.Message.Refusal != "" {
		return nil, ierr.NewError("model refused the request").
			WithHint("The AI model declined to process this document").
			Mark(ierr.ErrInvalidOperation)
	}

	resp := &Response{
		ID:           cr.ID,
		Model:        cr.Model,
		Content:      choice.Message.Content,
		ToolCalls:    choice.Message.ToolCalls,
		FinishReason: choice.FinishReason,
	}
	if cr.Usage != nil {
		resp.Usage = &Usage{
			PromptTokens:     cr.Usage.PromptTokens,
			CompletionTokens: cr.Usage.CompletionTokens,
			CostUSD:          cr.Usage.Cost,
		}
	}
	return resp, nil
}

func (m Message) MarshalJSON() ([]byte, error) {
	type wire struct {
		Role       string     `json:"role"`
		Content    any        `json:"content"`
		ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
		ToolCallID string     `json:"tool_call_id,omitempty"`
	}
	w := wire{Role: m.Role, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID}
	switch {
	case len(m.Content) == 0:
	case m.textOnly():
		w.Content = m.text()
	default:
		w.Content = m.Content
	}
	return json.Marshal(w)
}

func (m *Message) UnmarshalJSON(data []byte) error {
	var w struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		ToolCalls  []ToolCall      `json:"tool_calls"`
		ToolCallID string          `json:"tool_call_id"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*m = Message{Role: w.Role, ToolCalls: w.ToolCalls, ToolCallID: w.ToolCallID}
	if len(w.Content) == 0 || string(w.Content) == "null" {
		return nil
	}
	var text string
	if json.Unmarshal(w.Content, &text) == nil {
		m.Content = []ContentPart{{Type: ContentTypeText, Text: text}}
		return nil
	}
	return json.Unmarshal(w.Content, &m.Content)
}

// textOnly reports whether the content is plain text that providers accept as a single string,
// which tool and assistant messages require.
func (m Message) textOnly() bool {
	if m.Role != RoleTool && m.Role != RoleAssistant {
		return false
	}
	for _, p := range m.Content {
		if p.Type != ContentTypeText {
			return false
		}
	}
	return true
}

func (m Message) text() string {
	var b strings.Builder
	for _, p := range m.Content {
		b.WriteString(p.Text)
	}
	return b.String()
}

func (tc ToolChoice) MarshalJSON() ([]byte, error) {
	if tc.Function == "" {
		return json.Marshal(tc.Mode)
	}
	return json.Marshal(map[string]any{
		"type":     ToolTypeFunction,
		"function": map[string]string{"name": tc.Function},
	})
}

func (tc *ToolChoice) UnmarshalJSON(data []byte) error {
	var mode string
	if json.Unmarshal(data, &mode) == nil {
		*tc = ToolChoice{Mode: mode}
		return nil
	}
	var forced struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(data, &forced); err != nil {
		return err
	}
	*tc = ToolChoice{Function: forced.Function.Name}
	return nil
}
