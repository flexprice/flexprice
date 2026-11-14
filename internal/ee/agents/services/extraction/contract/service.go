package contract

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/flexprice/flexprice/internal/ee/agents/providers/reducto"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

type ContractExtractionService struct {
	client reducto.ReductoClient
	logger *logger.Logger
	schema json.RawMessage
}

func NewContractExtractionService(client reducto.ReductoClient, logger *logger.Logger) extraction.ExtractionServiceInterface {
	s := &ContractExtractionService{
		client: client,
		logger: logger,
	}
	s.schema = s.responseSchema()
	return s
}

func (s *ContractExtractionService) Extract(ctx context.Context, req *extraction.ExtractRequest) (*extraction.Extraction, error) {
	if err := s.validateRequest(req); err != nil {
		return nil, err
	}

	upload, err := s.client.Upload(ctx, s.fileName(req.FileName), req.MediaType, req.Data)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Extract(ctx, s.extractRequest(upload.FileID))
	if err != nil {
		return nil, err
	}

	result, err := s.toResult(resp.Result)
	if err != nil {
		return nil, err
	}
	s.sanitize(result)

	usage := s.usage(resp)
	s.logger.Info(ctx, "contract extracted",
		"job_id", resp.JobID,
		"charges", len(result.Charges),
		"pages", usage.Pages,
		"fields", usage.Fields,
		"duration_seconds", usage.DurationSeconds)
	return &extraction.Extraction{
		Result: result,
		Model:  s.model(resp),
		JobID:  resp.JobID,
		Usage:  usage,
	}, nil
}

func (s *ContractExtractionService) validateRequest(req *extraction.ExtractRequest) error {
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

func (s *ContractExtractionService) extractRequest(fileID string) *reducto.ExtractRequest {
	return &reducto.ExtractRequest{
		Input: fileID,
		Instructions: reducto.Instructions{
			Schema:       s.schema,
			SystemPrompt: systemPrompt,
		},
		Settings: &reducto.ExtractSettings{
			Citations: &reducto.CitationSettings{Enabled: true},
		},
	}
}

func (s *ContractExtractionService) usage(resp *reducto.ExtractResponse) *extraction.Usage {
	u := &extraction.Usage{DurationSeconds: resp.Duration}
	if resp.Usage != nil {
		u.Pages = resp.Usage.NumPages
		u.Fields = resp.Usage.NumFields
		u.Credits = resp.Usage.Credits
	}
	return u
}

func (s *ContractExtractionService) model(resp *reducto.ExtractResponse) string {
	if resp.Usage != nil && resp.Usage.ExtractMode != "" {
		return extractModelPrefix + resp.Usage.ExtractMode
	}
	return strings.TrimSuffix(extractModelPrefix, "/")
}

func (s *ContractExtractionService) fileName(name string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return defaultFileName
}
