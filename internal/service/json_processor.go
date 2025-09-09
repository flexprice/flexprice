package service

import (
	"bytes"
	"encoding/json"
	"io"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

// JSONProcessor handles JSON-specific operations
type JSONProcessor struct {
	Logger *logger.Logger
}

// NewJSONProcessor creates a new JSON processor
func NewJSONProcessor(logger *logger.Logger) *JSONProcessor {
	return &JSONProcessor{
		Logger: logger,
	}
}

// PrepareJSONReader creates a configured JSON decoder from the file content
func (jp *JSONProcessor) PrepareJSONReader(fileContent []byte) (*json.Decoder, error) {
	// Check for and remove BOM if present
	if len(fileContent) >= 3 && fileContent[0] == 0xEF && fileContent[1] == 0xBB && fileContent[2] == 0xBF {
		fileContent = fileContent[3:]
		jp.Logger.Debug("DEBUG: BOM detected and removed from file content")
	}

	// Create a decoder with strict JSON validation
	decoder := json.NewDecoder(bytes.NewReader(fileContent))
	decoder.DisallowUnknownFields()

	// Validate that the content starts with an array
	t, err := decoder.Token()
	if err != nil {
		return nil, ierr.NewErrorf("invalid JSON content: %v", err).
			WithHint("Invalid JSON content").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrValidation)
	}

	delim, ok := t.(json.Delim)
	if !ok || delim != '[' {
		return nil, ierr.NewError("JSON content must start with an array").
			WithHint("Invalid JSON format").
			Mark(ierr.ErrValidation)
	}

	// Reset the decoder to start from the beginning
	decoder = json.NewDecoder(bytes.NewReader(fileContent))
	decoder.DisallowUnknownFields()

	return decoder, nil
}

// ValidateJSONStructure validates that the JSON content is an array of objects
func (jp *JSONProcessor) ValidateJSONStructure(decoder *json.Decoder) error {
	// Read opening bracket
	t, err := decoder.Token()
	if err != nil {
		return ierr.NewErrorf("invalid JSON content: %v", err).
			WithHint("Invalid JSON content").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrValidation)
	}

	delim, ok := t.(json.Delim)
	if !ok || delim != '[' {
		return ierr.NewError("JSON content must start with an array").
			WithHint("Invalid JSON format").
			Mark(ierr.ErrValidation)
	}

	// Check first element is an object
	t, err = decoder.Token()
	if err == io.EOF {
		return ierr.NewError("JSON array is empty").
			WithHint("Empty JSON array").
			Mark(ierr.ErrValidation)
	}
	if err != nil {
		return ierr.NewErrorf("invalid JSON content: %v", err).
			WithHint("Invalid JSON content").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrValidation)
	}

	delim, ok = t.(json.Delim)
	if !ok || delim != '{' {
		return ierr.NewError("JSON array must contain objects").
			WithHint("Invalid JSON format").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ExtractHeaders extracts the field names from the first object in the JSON array
func (jp *JSONProcessor) ExtractHeaders(decoder *json.Decoder) ([]string, error) {
	// Skip the opening bracket of the array
	_, err := decoder.Token()
	if err != nil {
		return nil, ierr.NewErrorf("failed to read array start: %v", err).
			WithHint("Invalid JSON format").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrValidation)
	}

	// Read the first object's opening brace
	_, err = decoder.Token()
	if err != nil {
		return nil, ierr.NewErrorf("failed to read object start: %v", err).
			WithHint("Invalid JSON format").
			WithReportableDetails(map[string]interface{}{
				"error": err.Error(),
			}).
			Mark(ierr.ErrValidation)
	}

	var headers []string
	for {
		// Read field names until we hit the closing brace
		key, err := decoder.Token()
		if err != nil {
			return nil, ierr.NewErrorf("failed to read field name: %v", err).
				WithHint("Invalid JSON format").
				WithReportableDetails(map[string]interface{}{
					"error": err.Error(),
				}).
				Mark(ierr.ErrValidation)
		}

		// Check if we've hit the end of the object
		if delim, ok := key.(json.Delim); ok && delim == '}' {
			break
		}

		// Add the field name to our headers
		if str, ok := key.(string); ok {
			headers = append(headers, str)
		}

		// Skip the value
		var v interface{}
		if err := decoder.Decode(&v); err != nil {
			return nil, ierr.NewErrorf("failed to skip value: %v", err).
				WithHint("Invalid JSON format").
				WithReportableDetails(map[string]interface{}{
					"error": err.Error(),
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return headers, nil
}
