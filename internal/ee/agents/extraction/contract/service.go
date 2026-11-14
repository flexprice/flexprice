package contract

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/flexprice/flexprice/internal/ee/agents/extraction"
	"github.com/flexprice/flexprice/internal/ee/agents/openrouter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/samber/lo"
)

type ContractExtractionService struct {
	client openrouter.OpenRouterClient
	logger *logger.Logger
}

func NewContractExtractionService(client openrouter.OpenRouterClient, logger *logger.Logger) extraction.ExtractionServiceInterface {
	return &ContractExtractionService{
		client: client,
		logger: logger,
	}
}

func (s *ContractExtractionService) Extract(ctx context.Context, req *extraction.ExtractRequest) (*extraction.Extraction, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = s.client.DefaultModel()
	}
	if model == "" {
		return nil, ierr.NewError("no extraction model configured").
			WithHint("Set openrouter.model in the server configuration").
			Mark(ierr.ErrServiceUnavailable)
	}

	orReq := buildRequest(model, req)
	var usage *openrouter.Usage
	for attempt := 1; attempt <= maxParseAttempts; attempt++ {
		resp, err := s.client.Complete(ctx, orReq)
		if err != nil {
			return nil, err
		}
		usage = addUsage(usage, resp.Usage)

		result, perr := parseResult(resp.Content)
		if perr == nil {
			sanitize(result)
			logged := lo.FromPtr(usage)
			s.logger.Info(ctx, "contract extracted",
				"model", resp.Model,
				"charges", len(result.Charges),
				"prompt_tokens", logged.PromptTokens,
				"completion_tokens", logged.CompletionTokens,
				"cost_usd", logged.CostUSD)
			return &extraction.Extraction{Result: result, Model: resp.Model, Usage: usage}, nil
		}
		s.logger.Info(ctx, "contract extraction output invalid", "attempt", attempt, "finish_reason", resp.FinishReason)
		if attempt == maxParseAttempts {
			return nil, perr
		}
	}
	return nil, ierr.NewError("contract extraction failed").Mark(ierr.ErrInternal)
}

func validateRequest(req *extraction.ExtractRequest) error {
	if req == nil || len(req.Data) == 0 {
		return ierr.NewError("file is required").
			WithHint("Upload a contract file").
			Mark(ierr.ErrValidation)
	}
	switch req.MediaType {
	case MediaTypePDF, MediaTypePNG, MediaTypeJPEG:
		return nil
	default:
		return ierr.NewErrorf("unsupported media type %q", req.MediaType).
			WithHint("Upload a PDF, PNG or JPG file").
			Mark(ierr.ErrValidation)
	}
}

func buildRequest(model string, req *extraction.ExtractRequest) *openrouter.Request {
	file := openrouter.ContentPart{Type: openrouter.ContentTypeImageURL, ImageURL: &openrouter.ImageURL{URL: openrouter.DataURL(req.MediaType, req.Data)}}
	if req.MediaType == MediaTypePDF {
		file = openrouter.ContentPart{Type: openrouter.ContentTypeFile, File: &openrouter.FilePart{Filename: fileName(req.FileName), FileData: openrouter.PDFDataURL(req.Data)}}
	}
	temperature := 0.0
	return &openrouter.Request{
		Model: model,
		Messages: []openrouter.Message{
			{Role: openrouter.RoleSystem, Content: []openrouter.ContentPart{{Type: openrouter.ContentTypeText, Text: systemPrompt}}},
			{Role: openrouter.RoleUser, Content: []openrouter.ContentPart{{Type: openrouter.ContentTypeText, Text: userPrompt}, file}},
		},
		ResponseFormat: &openrouter.ResponseFormat{
			Type:       openrouter.ResponseFormatJSONSchema,
			JSONSchema: &openrouter.JSONSchema{Name: schemaName, Strict: true, Schema: ResponseSchema()},
		},
		Temperature: &temperature,
	}
}

func fileName(name string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return "contract.pdf"
}

func addUsage(a, b *openrouter.Usage) *openrouter.Usage {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &openrouter.Usage{
		PromptTokens:     a.PromptTokens + b.PromptTokens,
		CompletionTokens: a.CompletionTokens + b.CompletionTokens,
		CostUSD:          a.CostUSD + b.CostUSD,
	}
}

func parseResult(content string) (*extraction.Result, error) {
	text := jsonObjectText(content)
	if text == "" {
		return nil, ierr.NewError("model output is not JSON").
			WithHint("The AI model returned an unreadable result").
			Mark(ierr.ErrHTTPClient)
	}
	var result extraction.Result
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, ierr.WithError(err).
			WithHint("The AI model returned a result in the wrong shape").
			Mark(ierr.ErrHTTPClient)
	}
	return &result, nil
}

func jsonObjectText(content string) string {
	t := strings.TrimSpace(content)
	if strings.HasPrefix(t, "```") {
		t = strings.TrimPrefix(strings.TrimPrefix(t, "```json"), "```")
		t = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(t), "```"))
	}
	if json.Valid([]byte(t)) {
		return t
	}
	start, end := strings.Index(t, "{"), strings.LastIndex(t, "}")
	if start >= 0 && end > start && json.Valid([]byte(t[start:end+1])) {
		return t[start : end+1]
	}
	return ""
}
