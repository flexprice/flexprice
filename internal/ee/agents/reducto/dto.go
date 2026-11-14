package reducto

import "encoding/json"

type UploadResponse struct {
	FileID       string `json:"file_id"`
	PresignedURL string `json:"presigned_url,omitempty"`
}

type ExtractRequest struct {
	Input        string           `json:"input"`
	Instructions Instructions     `json:"instructions"`
	Settings     *ExtractSettings `json:"settings,omitempty"`
}

type Instructions struct {
	Schema       json.RawMessage `json:"schema"`
	SystemPrompt string          `json:"system_prompt,omitempty"`
}

type ExtractSettings struct {
	Citations *CitationSettings `json:"citations,omitempty"`
}

type CitationSettings struct {
	Enabled bool `json:"enabled"`
}

type ExtractResponse struct {
	ResponseType string          `json:"response_type"`
	Result       json.RawMessage `json:"result"`
	Usage        *ExtractUsage   `json:"usage"`
	JobID        string          `json:"job_id"`
	Duration     float64         `json:"duration"`
	StudioLink   string          `json:"studio_link"`
}

type ExtractUsage struct {
	NumPages    int      `json:"num_pages"`
	NumFields   int      `json:"num_fields"`
	Credits     *float64 `json:"credits"`
	ExtractMode string   `json:"extract_mode"`
}

type Field struct {
	Value     json.RawMessage `json:"value"`
	Citations []Citation      `json:"citations"`
}

type Citation struct {
	Type               string              `json:"type"`
	BBox               *BoundingBox        `json:"bbox"`
	Content            string              `json:"content"`
	Confidence         string              `json:"confidence"`
	GranularConfidence *GranularConfidence `json:"granular_confidence"`
}

type BoundingBox struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Page   int     `json:"page"`
}

type GranularConfidence struct {
	ExtractConfidence *float64 `json:"extract_confidence"`
	ParseConfidence   *float64 `json:"parse_confidence"`
}

type errorResponse struct {
	Detail json.RawMessage `json:"detail"`
}
