package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/config"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/ee/agents/services/customermatch"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	contractagent "github.com/flexprice/flexprice/internal/ee/agents/services/extraction/contract"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
	"github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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
	DeriveTerms(ctx context.Context, contractID string, req *dto.DeriveContractTermsRequest) (*extraction.Derived, error)
	GetReview(ctx context.Context, contractID string) (*dto.ContractReviewResponse, error)
	SaveReviewStep(ctx context.Context, contractID, step string, req *dto.SaveContractReviewRequest) (*dto.ContractReviewResponse, error)
	// CompleteDraft attaches a draft subscription and draft POBs to the draft contract, and activates the subscription when asked.
	CompleteDraft(ctx context.Context, contractID string, req *dto.CompleteDraftRequest) (*dto.ContractResponse, error)
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
	db              postgres.IClient
	subscriptions   interfaces.SubscriptionService
	contractService ContractService
	pobs            pobsvc.PerformanceObligationService
	customers       interfaces.CustomerService
	products        interfaces.ProductService
	storage         storage.Resolver
	extractor       extraction.ExtractionServiceInterface
	matcher         customermatch.CustomerMatchServiceInterface
	directory       *customerDirectory
	catalog         *productCatalog
	config          *config.Configuration
	logger          *logger.Logger
	starter         WorkflowStarter
	reviewThreshold int
}

func NewContractIngestService(
	contracts contractRepo.Repository,
	db postgres.IClient,
	subscriptions interfaces.SubscriptionService,
	contractService ContractService,
	pobs pobsvc.PerformanceObligationService,
	storageResolver storage.Resolver,
	extractor extraction.ExtractionServiceInterface,
	matcher customermatch.CustomerMatchServiceInterface,
	customers interfaces.CustomerService,
	products interfaces.ProductService,
	cfg *config.Configuration,
	logger *logger.Logger,
) ContractIngestService {
	return &contractIngestService{
		contracts:       contracts,
		db:              db,
		subscriptions:   subscriptions,
		contractService: contractService,
		pobs:            pobs,
		customers:       customers,
		products:        products,
		storage:         storageResolver,
		extractor:       extractor,
		matcher:         matcher,
		directory:       newCustomerDirectory(customers),
		catalog:         newProductCatalog(products),
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
		if delErr := store.Delete(ctx, key); delErr != nil {
			c.logger.Error(ctx, "contract upload cleanup failed", "error", delErr, "contract_id", id, "key", key)
		}
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

func (c *contractIngestService) CompleteDraft(ctx context.Context, contractID string, req *dto.CompleteDraftRequest) (*dto.ContractResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	contract, err := c.contracts.GetByID(ctx, contractID)
	if err != nil {
		return nil, err
	}
	if err := c.canCompleteDraft(ctx, contract, req); err != nil {
		return nil, err
	}

	var subID string
	err = c.db.WithTx(ctx, func(ctx context.Context) error {
		customerID := req.Contract.CustomerID
		if req.Customer != nil {
			customer, err := c.customers.CreateCustomer(ctx, *req.Customer)
			if err != nil {
				return err
			}
			customerID = customer.ID
		}

		productIDs, lineItems, err := c.createCatalog(ctx, req.Products)
		if err != nil {
			return err
		}

		subReq := req.Subscriptions[0]
		subReq.CustomerID = customerID
		subReq.Currency = req.Contract.Currency
		subReq.StartDate = req.Contract.TermStart
		subReq.EndDate = lo.ToPtr(req.Contract.TermEnd)
		subReq.SubscriptionStatus = types.SubscriptionStatusDraft
		subReq.LineItems = append(subReq.LineItems, lineItems...)
		sub, err := c.subscriptions.CreateSubscription(ctx, subReq)
		if err != nil {
			return err
		}
		subID = sub.ID

		if err := c.contracts.CreateAssociation(ctx, domainContract.NewAssociationBuilder(nil).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONTRACT_ASSOCIATION)).
			WithContractID(contract.ID()).
			WithEntityType(types.RevRecEntityTypeSubscription).
			WithEntityID(sub.ID).
			WithStatus(types.StatusPublished).
			WithCreatedAt(clock.Now()).
			Build()); err != nil {
			return err
		}

		completed := domainContract.NewBuilder(contract).
			WithCustomerID(customerID).
			WithCurrency(req.Contract.Currency).
			WithTermStart(req.Contract.TermStart).
			WithTermEnd(lo.ToPtr(req.Contract.TermEnd)).
			Build()
		if err := c.contracts.Update(ctx, completed); err != nil {
			return err
		}

		pobs, err := c.resolvePOBProducts(req.PerformanceObligations, productIDs)
		if err != nil {
			return err
		}
		_, err = c.pobs.CreateForContract(ctx, completed, pobs)
		return err
	})
	if err != nil {
		return nil, err
	}

	if req.Activate {
		if _, err := c.subscriptions.ActivateDraftSubscription(ctx, subID, dto.ActivateDraftSubscriptionRequest{StartDate: req.Contract.TermStart}); err != nil {
			return nil, err
		}
	}
	return c.contractService.GetContract(ctx, contractID)
}

// canCompleteDraft checks the contract is a draft with no subscription yet, and agrees with the requested currency.
func (c *contractIngestService) canCompleteDraft(ctx context.Context, contract *domainContract.Contract, req *dto.CompleteDraftRequest) error {
	if contract.ContractStatus() != types.ContractStatusDraft {
		return ierr.NewError("contract is not a draft").
			WithHint("Only a draft contract can be completed").
			WithReportableDetails(map[string]any{"contract_status": contract.ContractStatus()}).
			Mark(ierr.ErrInvalidOperation)
	}

	if contract.Currency() != "" && !strings.EqualFold(contract.Currency(), req.Contract.Currency) {
		return ierr.NewError("currency does not match the contract").
			WithHintf("Use the contract's currency %s", contract.Currency()).
			Mark(ierr.ErrValidation)
	}

	linked, err := c.contracts.ListAssociations(ctx, contract.ID())
	if err != nil {
		return err
	}

	if len(linked) > 0 {
		return ierr.NewError("contract is already completed").
			WithHint("The contract already has a subscription").
			Mark(ierr.ErrInvalidOperation)
	}

	return nil
}

// createCatalog creates each product and returns its ID by lookup key, with its prices as subscription line items.
func (c *contractIngestService) createCatalog(ctx context.Context, catalogs []dto.CreateProductCatalogRequest) (map[string]string, []dto.CreateSubscriptionLineItemRequest, error) {
	productIDs := make(map[string]string, len(catalogs))
	var lineItems []dto.CreateSubscriptionLineItemRequest
	for _, catalog := range catalogs {
		product, err := c.products.CreateProduct(ctx, catalog.Product)
		if err != nil {
			return nil, nil, err
		}
		productIDs[product.LookupKey] = product.ID

		for _, price := range catalog.Prices {
			price.ProductID = product.ID
			lineItems = append(lineItems, dto.CreateSubscriptionLineItemRequest{Price: &price})
		}
	}

	return productIDs, lineItems, nil
}

// resolvePOBProducts fills in the product ID of each POB named by the lookup key of a product created in the request.
func (c *contractIngestService) resolvePOBProducts(reqs []dto.CreatePOBRequest, productIDs map[string]string) ([]dto.CreatePOBRequest, error) {
	resolved := make([]dto.CreatePOBRequest, 0, len(reqs))
	for _, req := range reqs {
		if req.ProductLookupKey != "" {
			id, ok := productIDs[req.ProductLookupKey]
			if !ok {
				return nil, ierr.NewError("no product created with the lookup key").
					WithHint("product_lookup_key must match a product in the request's products").
					WithReportableDetails(map[string]any{"product_lookup_key": req.ProductLookupKey}).
					Mark(ierr.ErrValidation)
			}
			req.ProductID = id
		}
		resolved = append(resolved, req)
	}

	return resolved, nil
}
