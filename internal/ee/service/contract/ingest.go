package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/config"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/ee/agents/services/customermatch"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	contractagent "github.com/flexprice/flexprice/internal/ee/agents/services/extraction/contract"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
	"github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	contractKeyPrefix = "contracts"
	maxUploadBytes    = 20 << 20
	maxErrorLength    = 2000
)

type ContractIngestService interface {
	Upload(ctx context.Context, req *UploadRequest) (*domainContract.Contract, error)
	Process(ctx context.Context, contractID string) error
	MarkFailed(ctx context.Context, contractID, reason string) error
	MatchCustomer(ctx context.Context, contractID string) error
	MarkCustomerMatchUnavailable(ctx context.Context, contractID, reason string) error
}

type UploadRequest struct {
	FileName string
	Data     []byte
}

type WorkflowStarter interface {
	ExecuteWorkflow(ctx context.Context, workflowType types.TemporalWorkflowType, params interface{}) (models.WorkflowRun, error)
}

type contractIngestService struct {
	contracts       contractRepo.Repository
	storage         storage.Resolver
	extractor       extraction.ExtractionServiceInterface
	matcher         customermatch.CustomerMatchServiceInterface
	directory       *customerDirectory
	config          *config.Configuration
	logger          *logger.Logger
	starter         WorkflowStarter
	reviewThreshold int
}

func NewContractIngestService(
	contracts contractRepo.Repository,
	storageResolver storage.Resolver,
	extractor extraction.ExtractionServiceInterface,
	matcher customermatch.CustomerMatchServiceInterface,
	customers interfaces.CustomerService,
	cfg *config.Configuration,
	logger *logger.Logger,
) ContractIngestService {
	return &contractIngestService{
		contracts:       contracts,
		storage:         storageResolver,
		extractor:       extractor,
		matcher:         matcher,
		directory:       newCustomerDirectory(customers),
		config:          cfg,
		logger:          logger,
		reviewThreshold: DefaultReviewThreshold,
	}
}

func (c *contractIngestService) Upload(ctx context.Context, req *UploadRequest) (*domainContract.Contract, error) {
	mediaType, err := c.validateUpload(req)
	if err != nil {
		return nil, err
	}

	store, err := c.storage.ForPlatform(ctx, storage.PurposeImport)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Contract uploads are not configured on this deployment").
			Mark(ierr.ErrServiceUnavailable)
	}

	id := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONTRACT)
	key := c.objectKey(ctx, id, mediaType)
	if _, err := store.Upload(ctx, &storagetypes.UploadRequest{
		Key:         key,
		Data:        req.Data,
		Format:      c.uploadFormat(mediaType),
		ContentType: mediaType,
	}); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to store the contract file").
			Mark(ierr.ErrHTTPClient)
	}

	sum := sha256.Sum256(req.Data)
	now := clock.Now()
	contract := domainContract.NewBuilder(nil).
		WithID(id).
		WithTenantID(types.GetTenantID(ctx)).
		WithEnvironmentID(types.GetEnvironmentID(ctx)).
		WithContractStatus(types.ContractStatusDraft).
		WithStatus(types.StatusPublished).
		WithDocument(&types.ContractDocument{
			Key:       key,
			FileName:  c.uploadFileName(req.FileName),
			MediaType: mediaType,
			SizeBytes: int64(len(req.Data)),
			SHA256:    hex.EncodeToString(sum[:]),
		}).
		WithExtractionStatus(types.ContractExtractionStatusPending).
		WithCreatedAt(now).
		WithUpdatedAt(now).
		Build()
	if err := c.contracts.Create(ctx, contract); err != nil {
		return nil, err
	}

	if err := c.enqueue(ctx, id); err != nil {
		c.logger.Error(ctx, "contract extraction enqueue failed", "error", err, "contract_id", id)
		if markErr := c.MarkFailed(ctx, id, "Could not start processing the contract"); markErr != nil {
			c.logger.Error(ctx, "contract extraction mark failed", "error", markErr, "contract_id", id)
		}
		return nil, err
	}

	c.logger.Info(ctx, "contract uploaded", "contract_id", id, "media_type", mediaType, "size_bytes", len(req.Data))
	return contract, nil
}

func (c *contractIngestService) Process(ctx context.Context, contractID string) error {
	contract, err := c.contracts.GetByID(ctx, contractID)
	if err != nil {
		return err
	}
	if contract.ExtractionStatus() == types.ContractExtractionStatusCompleted {
		return nil
	}
	doc := contract.Document()
	if doc == nil || doc.Key == "" {
		return ierr.NewError("contract has no document").
			WithHint("Upload a contract file before extracting it").
			Mark(ierr.ErrInvalidOperation)
	}

	contract = domainContract.NewBuilder(contract).
		WithExtractionStatus(types.ContractExtractionStatusProcessing).
		WithExtractionError("").
		Build()
	if err := c.contracts.Update(ctx, contract); err != nil {
		return err
	}

	store, err := c.storage.ForPlatform(ctx, storage.PurposeImport)
	if err != nil {
		return err
	}
	data, err := store.Download(ctx, doc.Key)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to read the contract file").
			Mark(ierr.ErrHTTPClient)
	}

	out, err := c.extractor.Extract(ctx, &extraction.ExtractRequest{
		FileName:  doc.FileName,
		MediaType: doc.MediaType,
		Data:      data,
	})
	if err != nil {
		return err
	}
	if err := c.enrich(out.Result); err != nil {
		return err
	}
	out.Result.CustomerMatch = c.initialMatch()

	raw, err := json.Marshal(out)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to save the extraction").
			Mark(ierr.ErrInternal)
	}

	contract = domainContract.NewBuilder(contract).
		WithExtraction(raw).
		WithExtractionStatus(types.ContractExtractionStatusCompleted).
		Build()
	if err := c.contracts.Update(ctx, contract); err != nil {
		return err
	}

	c.logger.Info(ctx, "contract extraction completed", "contract_id", contractID, "model", out.Model)
	return nil
}

func (c *contractIngestService) MarkFailed(ctx context.Context, contractID, reason string) error {
	contract, err := c.contracts.GetByID(ctx, contractID)
	if err != nil {
		return err
	}
	if contract.ExtractionStatus() == types.ContractExtractionStatusCompleted {
		return nil
	}
	return c.contracts.Update(ctx, domainContract.NewBuilder(contract).
		WithExtractionStatus(types.ContractExtractionStatusFailed).
		WithExtractionError(c.truncate(reason, maxErrorLength)).
		Build())
}

func (c *contractIngestService) enqueue(ctx context.Context, contractID string) error {
	starter := c.starter
	if starter == nil {
		if global := temporalservice.GetGlobalTemporalService(); global != nil {
			starter = global
		}
	}
	if starter == nil {
		return ierr.NewError("temporal service not available").
			WithHint("Contract processing is not available on this deployment").
			Mark(ierr.ErrServiceUnavailable)
	}
	_, err := starter.ExecuteWorkflow(ctx, types.TemporalContractExtractionWorkflow, models.ContractExtractionWorkflowInput{
		ContractID: contractID,
	})
	return err
}

func (c *contractIngestService) objectKey(ctx context.Context, contractID, mediaType string) string {
	name := path.Join(contractKeyPrefix, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), contractID+c.extensionFor(mediaType))
	if prefix := strings.Trim(c.config.FlexpriceS3Imports.KeyPrefix, "/"); prefix != "" {
		return prefix + "/" + name
	}
	return name
}

func (c *contractIngestService) validateUpload(req *UploadRequest) (string, error) {
	if req == nil || len(req.Data) == 0 {
		return "", ierr.NewError("file is required").
			WithHint("Upload a contract file").
			Mark(ierr.ErrValidation)
	}
	if len(req.Data) > maxUploadBytes {
		return "", ierr.NewError("file too large").
			WithHintf("Contract files must be %d MB or smaller", maxUploadBytes>>20).
			Mark(ierr.ErrValidation)
	}
	mediaType, _, _ := strings.Cut(http.DetectContentType(req.Data), ";")
	switch mediaType {
	case contractagent.MediaTypePDF, contractagent.MediaTypePNG, contractagent.MediaTypeJPEG:
		return mediaType, nil
	default:
		return "", ierr.NewErrorf("unsupported file type %q", mediaType).
			WithHint("Upload a PDF, PNG or JPG file").
			Mark(ierr.ErrValidation)
	}
}

func (c *contractIngestService) extensionFor(mediaType string) string {
	switch mediaType {
	case contractagent.MediaTypePNG:
		return ".png"
	case contractagent.MediaTypeJPEG:
		return ".jpg"
	default:
		return ".pdf"
	}
}

func (c *contractIngestService) uploadFormat(mediaType string) storagetypes.UploadFormat {
	if mediaType == contractagent.MediaTypePDF {
		return storagetypes.UploadFormatPDF
	}
	return ""
}

func (c *contractIngestService) truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

func (c *contractIngestService) uploadFileName(name string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return "contract.pdf"
}
