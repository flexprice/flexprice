package extraction

import (
	"context"
)

type ExtractionServiceInterface interface {
	Extract(ctx context.Context, req *ExtractRequest) (*Extraction, error)
	Enrich(ctx context.Context, result *Result) error
}
