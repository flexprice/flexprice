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
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

const ToolTypeFunction = "function"

const (
	ToolChoiceAuto     = "auto"
	ToolChoiceRequired = "required"
	ToolChoiceNone     = "none"
)

const (
	FinishReasonStop      = "stop"
	FinishReasonToolCalls = "tool_calls"
	FinishReasonLength    = "length"
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
