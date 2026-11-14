package reducto

import "time"

const (
	ProviderName = "reducto"

	defaultBaseURL = "https://platform.reducto.ai"
	defaultTimeout = 300 * time.Second

	uploadEndpoint  = "/upload"
	extractEndpoint = "/extract"

	uploadFormField = "file"
)

const (
	ConfidenceHigh = "high"
	ConfidenceLow  = "low"
)
