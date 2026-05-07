package dto

import (
	"encoding/json"
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

const (
	maxGeminiSystemPromptBytes   = 256 * 1024
	maxGeminiUserPromptBytes     = 256 * 1024
	maxGeminiResponseSchemaBytes = 512 * 1024
)

// ParseGeminiPricingRequest is the body for POST /v1/ai/pricing/parse-gemini.
type ParseGeminiPricingRequest struct {
	SystemPrompt   string          `json:"systemPrompt" swaggertype:"string" example:"You are a pricing architect..."`
	UserPrompt     string          `json:"userPrompt" swaggertype:"string" example:"Describe pricing for..."`
	ResponseSchema json.RawMessage `json:"responseSchema" swaggertype:"object"`
}

// Validate checks required fields and size limits.
func (r *ParseGeminiPricingRequest) Validate() error {
	if r == nil {
		return ierr.NewError("nil request").WithHint("Invalid request").Mark(ierr.ErrValidation)
	}
	if len(r.SystemPrompt) == 0 {
		return ierr.NewError("systemPrompt required").WithHint("systemPrompt is required").Mark(ierr.ErrValidation)
	}
	if len(r.UserPrompt) == 0 {
		return ierr.NewError("userPrompt required").WithHint("userPrompt is required").Mark(ierr.ErrValidation)
	}
	if len(r.ResponseSchema) == 0 {
		return ierr.NewError("responseSchema required").WithHint("responseSchema is required").Mark(ierr.ErrValidation)
	}
	if len(r.SystemPrompt) > maxGeminiSystemPromptBytes {
		return ierr.NewError("systemPrompt too large").WithHint(fmt.Sprintf("systemPrompt exceeds %d bytes", maxGeminiSystemPromptBytes)).Mark(ierr.ErrValidation)
	}
	if len(r.UserPrompt) > maxGeminiUserPromptBytes {
		return ierr.NewError("userPrompt too large").WithHint(fmt.Sprintf("userPrompt exceeds %d bytes", maxGeminiUserPromptBytes)).Mark(ierr.ErrValidation)
	}
	if len(r.ResponseSchema) > maxGeminiResponseSchemaBytes {
		return ierr.NewError("responseSchema too large").WithHint(fmt.Sprintf("responseSchema exceeds %d bytes", maxGeminiResponseSchemaBytes)).Mark(ierr.ErrValidation)
	}
	if !json.Valid(r.ResponseSchema) {
		return ierr.NewError("responseSchema invalid json").WithHint("responseSchema must be valid JSON").Mark(ierr.ErrValidation)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(r.ResponseSchema, &obj); err != nil {
		return ierr.NewError("responseSchema must be a JSON object").WithHint("responseSchema must be a JSON object").Mark(ierr.ErrValidation)
	}
	return nil
}
