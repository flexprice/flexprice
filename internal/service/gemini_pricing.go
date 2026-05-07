package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
)

const defaultGeminiModel = "gemini-2.5-flash-lite"
const maxGeminiParseAttempts = 2

// GeminiPricingService calls Gemini generateContent and returns canonical JSON for the pricing schema payload.
type GeminiPricingService interface {
	ParsePricing(ctx context.Context, req *dto.ParseGeminiPricingRequest) (json.RawMessage, error)
}

type geminiPricingService struct {
	cfg    *config.Configuration
	client httpclient.Client
	log    *logger.Logger
}

// NewGeminiPricingService builds a Gemini proxy for AI pricing parse. Uses a long HTTP timeout for LLM latency.
func NewGeminiPricingService(cfg *config.Configuration, log *logger.Logger) GeminiPricingService {
	return newGeminiPricingService(cfg, nil, log)
}

func newGeminiPricingService(cfg *config.Configuration, httpClient httpclient.Client, log *logger.Logger) GeminiPricingService {
	if httpClient == nil {
		httpClient = httpclient.NewClientWithConfig(httpclient.ClientConfig{
			Timeout: 120 * time.Second,
		})
	}
	return &geminiPricingService{
		cfg:    cfg,
		client: httpClient,
		log:    log,
	}
}

type geminiGenerateRequest struct {
	SystemInstruction *geminiContentParts `json:"system_instruction,omitempty"`
	Contents          []geminiUserContent `json:"contents"`
	GenerationConfig  geminiGenerationCfg `json:"generationConfig"`
}

type geminiContentParts struct {
	Parts []geminiTextPart `json:"parts"`
}

type geminiTextPart struct {
	Text string `json:"text"`
}

type geminiUserContent struct {
	Role  string           `json:"role"`
	Parts []geminiTextPart `json:"parts"`
}

type geminiGenerationCfg struct {
	ResponseMimeType string          `json:"responseMimeType"`
	ResponseSchema   json.RawMessage `json:"responseSchema"`
}

type geminiGenerateResponse struct {
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Candidates []struct {
		Content *struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

func (s *geminiPricingService) ParsePricing(ctx context.Context, req *dto.ParseGeminiPricingRequest) (json.RawMessage, error) {
	if s.cfg == nil {
		return nil, ierr.NewError("configuration is nil").
			WithHint("AI pricing is not available").
			Mark(ierr.ErrServiceUnavailable)
	}
	apiKey := strings.TrimSpace(s.cfg.Gemini.APIKey)
	if apiKey == "" {
		return nil, ierr.NewError("gemini api key not configured").
			WithHint("AI pricing is not configured on this server").
			Mark(ierr.ErrServiceUnavailable)
	}

	model := strings.TrimSpace(s.cfg.Gemini.Model)
	if model == "" {
		model = defaultGeminiModel
	}

	genReq := geminiGenerateRequest{
		SystemInstruction: &geminiContentParts{
			Parts: []geminiTextPart{{Text: req.SystemPrompt}},
		},
		Contents: []geminiUserContent{
			{
				Role:  "user",
				Parts: []geminiTextPart{{Text: req.UserPrompt}},
			},
		},
		GenerationConfig: geminiGenerationCfg{
			ResponseMimeType: "application/json",
			ResponseSchema:   req.ResponseSchema,
		},
	}

	body, err := json.Marshal(genReq)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Could not build AI request").
			Mark(ierr.ErrValidation)
	}

	apiURL, err := geminiGenerateURL(model, apiKey)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid AI service configuration").
			Mark(ierr.ErrSystem)
	}

	for attempt := 1; attempt <= maxGeminiParseAttempts; attempt++ {
		httpResp, err := s.client.Send(ctx, &httpclient.Request{
			Method: http.MethodPost,
			URL:    apiURL,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: body,
		})
		if err != nil {
			if httpErr, ok := httpclient.IsHTTPError(err); ok {
				s.log.WarnwCtx(ctx, "gemini non-success status", "status", httpErr.StatusCode)
				return nil, mapGeminiUpstreamHTTPError(err, httpErr.StatusCode)
			}
			s.log.WarnwCtx(ctx, "gemini request failed", "error", err)
			return nil, ierr.WithError(err).
				WithHint("AI service temporarily unavailable").
				Mark(ierr.ErrHTTPClient)
		}

		if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
			s.log.WarnwCtx(ctx, "gemini non-success status",
				"status", httpResp.StatusCode,
				"body_len", len(httpResp.Body),
			)
			return nil, ierr.NewErrorf("gemini returned status %d", httpResp.StatusCode).
				WithHint("AI service temporarily unavailable").
				Mark(ierr.ErrHTTPClient)
		}

		var genResp geminiGenerateResponse
		if err := json.Unmarshal(httpResp.Body, &genResp); err != nil {
			s.log.WarnwCtx(ctx, "gemini response not json", "error", err)
			return nil, ierr.NewError("invalid response from AI service").
				WithHint("AI service returned an unexpected response").
				Mark(ierr.ErrHTTPClient)
		}

		if genResp.Error != nil && genResp.Error.Message != "" {
			s.log.WarnwCtx(ctx, "gemini error object present",
				"code", genResp.Error.Code,
				"status", genResp.Error.Status,
			)
			return nil, ierr.NewError("AI provider rejected the request").
				WithHint("AI service temporarily unavailable").
				Mark(ierr.ErrHTTPClient)
		}

		if genResp.PromptFeedback != nil && genResp.PromptFeedback.BlockReason != "" {
			return nil, ierr.NewError("prompt was blocked").
				WithHint("The request could not be processed by the AI service").
				Mark(ierr.ErrInvalidOperation)
		}

		if len(genResp.Candidates) == 0 {
			return nil, ierr.NewError("no candidates in AI response").
				WithHint("AI service returned an empty result").
				Mark(ierr.ErrHTTPClient)
		}

		cand := genResp.Candidates[0]
		if cand.Content == nil || len(cand.Content.Parts) == 0 {
			return nil, ierr.NewError("no content parts in AI response").
				WithHint("AI service returned an empty result").
				Mark(ierr.ErrHTTPClient)
		}

		text := normalizeGeminiJSONText(cand.Content.Parts[0].Text)
		if text == "" {
			if attempt < maxGeminiParseAttempts {
				s.log.WarnwCtx(ctx, "gemini returned non-json text, retrying", "attempt", attempt)
				continue
			}
			return nil, ierr.NewError("AI output is not valid JSON").
				WithHint("AI service returned invalid data").
				Mark(ierr.ErrHTTPClient)
		}

		var rawObj json.RawMessage
		if err := json.Unmarshal([]byte(text), &rawObj); err != nil {
			if attempt < maxGeminiParseAttempts {
				s.log.WarnwCtx(ctx, "gemini invalid json payload, retrying", "attempt", attempt)
				continue
			}
			return nil, ierr.WithError(err).
				WithHint("AI service returned invalid data").
				Mark(ierr.ErrHTTPClient)
		}

		// Reject non-objects at top level (pricing schema must be an object).
		var typeCheck map[string]json.RawMessage
		if err := json.Unmarshal(rawObj, &typeCheck); err != nil {
			if attempt < maxGeminiParseAttempts {
				s.log.WarnwCtx(ctx, "gemini non-object json payload, retrying", "attempt", attempt)
				continue
			}
			return nil, ierr.WithError(err).
				WithHint("AI service returned invalid data").
				Mark(ierr.ErrHTTPClient)
		}

		out, err := json.Marshal(rawObj)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Could not normalize AI response").
				Mark(ierr.ErrInternal)
		}

		return out, nil
	}

	return nil, ierr.NewError("AI output is not valid JSON").
		WithHint("AI service returned invalid data").
		Mark(ierr.ErrHTTPClient)
}

// mapGeminiUpstreamHTTPError maps Google Generative Language API HTTP errors to client-facing errors.
// err should be the value returned by httpclient when status >= 400.
func mapGeminiUpstreamHTTPError(err error, status int) error {
	switch status {
	case http.StatusTooManyRequests:
		return ierr.WithError(err).
			WithHint("Google Gemini rate limit exceeded. Wait and retry, or reduce usage.").
			Mark(ierr.ErrTooManyRequests)
	case http.StatusUnauthorized, http.StatusForbidden:
		return ierr.WithError(err).
			WithHint("AI service rejected the request. Check Gemini API key configuration and access.").
			Mark(ierr.ErrInvalidOperation)
	case http.StatusBadRequest:
		return ierr.WithError(err).
			WithHint("AI service rejected the request.").
			Mark(ierr.ErrInvalidOperation)
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		return ierr.WithError(err).
			WithHint("AI service is temporarily overloaded. Try again shortly.").
			Mark(ierr.ErrServiceUnavailable)
	default:
		return ierr.WithError(err).
			WithHint("AI service temporarily unavailable.").
			Mark(ierr.ErrHTTPClient)
	}
}

func geminiGenerateURL(model, apiKey string) (string, error) {
	base := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", url.PathEscape(model))
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("key", apiKey)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// normalizeGeminiJSONText attempts to recover JSON object text from model output.
// Handles plain JSON, markdown fenced code blocks, and leading/trailing commentary.
func normalizeGeminiJSONText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}

	// Case 1: already valid JSON object text.
	if json.Valid([]byte(trimmed)) {
		return trimmed
	}

	// Case 2: markdown fenced payload.
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```JSON")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(strings.TrimSpace(trimmed), "```")
		trimmed = strings.TrimSpace(trimmed)
		if json.Valid([]byte(trimmed)) {
			return trimmed
		}
	}

	// Case 3: extract first object-looking span.
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		candidate := strings.TrimSpace(trimmed[start : end+1])
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}

	return ""
}
