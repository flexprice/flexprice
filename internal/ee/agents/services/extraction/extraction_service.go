package extraction

import (
	"context"
)

type ExtractionServiceInterface interface {
	Extract(ctx context.Context, req *ExtractRequest) (*Extraction, error)
}
