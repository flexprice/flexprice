package openrouter

import (
	"encoding/json"

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
}

type Message struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
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
			Content string `json:"content"`
			Refusal string `json:"refusal"`
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
	ID           string `json:"id"`
	Model        string `json:"model"`
	Content      string `json:"content"`
	FinishReason string `json:"finish_reason"`
	Usage        *Usage `json:"usage"`
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
