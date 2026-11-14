package openrouter

import "time"

const (
	ProviderName = "openrouter"

	defaultBaseURL = "https://openrouter.ai/api/v1"
	defaultTimeout = 300 * time.Second
	appTitle       = "Flexprice"

	chatCompletionsEndpoint = "/chat/completions"
)

const (
	RoleSystem = "system"
	RoleUser   = "user"
)

const (
	ContentTypeText     = "text"
	ContentTypeFile     = "file"
	ContentTypeImageURL = "image_url"
)

const ResponseFormatJSONSchema = "json_schema"

const (
	PluginFileParser    = "file-parser"
	PDFEngineNative     = "native"
	PDFEngineMistralOCR = "mistral-ocr"
	PDFEngineCloudflare = "cloudflare-ai"
)

const dataCollectionDeny = "deny"
