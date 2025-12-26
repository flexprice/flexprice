package export

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// Exporter defines the interface for entity-specific exporters
type Exporter interface {
	// PrepareData fetches and prepares data for export
	PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error)

	// GetFilenamePrefix returns the prefix for the exported file
	GetFilenamePrefix() string
}

// ExportService handles export operations for different entity types
type ExportService struct {
	featureUsageRepo    events.FeatureUsageRepository
	invoiceRepo         invoice.Repository
	walletRepo          wallet.Repository
	walletBalanceGetter WalletBalanceGetter
	customerRepo        customer.Repository
	connectionRepo      connection.Repository
	integrationFactory  *integration.Factory
	logger              *logger.Logger
}

// NewExportService creates a new export service
func NewExportService(
	featureUsageRepo events.FeatureUsageRepository,
	invoiceRepo invoice.Repository,
	connectionRepo connection.Repository,
	integrationFactory *integration.Factory,
	logger *logger.Logger,
) *ExportService {
	return &ExportService{
		featureUsageRepo:   featureUsageRepo,
		invoiceRepo:        invoiceRepo,
		walletRepo:         nil, // Will be set when needed
		connectionRepo:     connectionRepo,
		integrationFactory: integrationFactory,
		logger:             logger,
	}
}

// NewExportServiceWithWallet creates a new export service with wallet repository
func NewExportServiceWithWallet(
	featureUsageRepo events.FeatureUsageRepository,
	invoiceRepo invoice.Repository,
	walletRepo wallet.Repository,
	walletBalanceGetter WalletBalanceGetter,
	customerRepo customer.Repository,
	connectionRepo connection.Repository,
	integrationFactory *integration.Factory,
	logger *logger.Logger,
) *ExportService {
	return &ExportService{
		featureUsageRepo:    featureUsageRepo,
		invoiceRepo:         invoiceRepo,
		walletRepo:          walletRepo,
		walletBalanceGetter: walletBalanceGetter,
		customerRepo:        customerRepo,
		connectionRepo:      connectionRepo,
		integrationFactory:  integrationFactory,
		logger:              logger,
	}
}

// Export routes the export request to the appropriate entity exporter
func (s *ExportService) Export(ctx context.Context, request *dto.ExportRequest) (*dto.ExportResponse, error) {
	s.logger.Infow("starting export",
		"entity_type", request.EntityType,
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"start_time", request.StartTime,
		"end_time", request.EndTime)

	ctx = types.SetTenantID(ctx, request.TenantID)
	ctx = types.SetEnvironmentID(ctx, request.EnvID)

	// Get the appropriate exporter for the entity type
	exporter := s.getExporter(request.EntityType)
	if exporter == nil {
		return nil, ierr.NewError("unknown entity type").
			WithHintf("entity type '%s' is not supported", request.EntityType).
			Mark(ierr.ErrValidation)
	}

	// Execute the export workflow: PrepareData -> Upload to provider
	return s.executeExport(ctx, request, exporter)
}

// executeExport performs the common export workflow: validate -> prepare data -> upload to provider
func (s *ExportService) executeExport(ctx context.Context, request *dto.ExportRequest, exporter Exporter) (*dto.ExportResponse, error) {
	// Step 1: Prepare data (fetch + convert to CSV) - entity-specific logic
	csvBytes, recordCount, err := exporter.PrepareData(ctx, request)
	if err != nil {
		return nil, err
	}

	// Step 2: Get connection to determine provider type
	conn, err := s.connectionRepo.Get(ctx, request.ConnectionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get connection for export").
			Mark(ierr.ErrDatabase)
	}

	// Add tenant and environment to context for connection lookup
	ctx = types.SetTenantID(ctx, request.TenantID)
	ctx = types.SetEnvironmentID(ctx, request.EnvID)

	// Step 3: Route to appropriate provider based on connection type
	switch conn.ProviderType {
	case types.SecretProviderS3:
		return s.uploadToS3(ctx, request, exporter, csvBytes, recordCount)
	default:
		return nil, ierr.NewError("unsupported provider type").
			WithHintf("Provider type '%s' is not supported for exports", conn.ProviderType).
			Mark(ierr.ErrValidation)
	}
}

// uploadToS3 handles S3-specific upload logic
func (s *ExportService) uploadToS3(ctx context.Context, request *dto.ExportRequest, exporter Exporter, csvBytes []byte, recordCount int) (*dto.ExportResponse, error) {
	// Validate S3 job config is provided
	if request.JobConfig == nil {
		return nil, ierr.NewError("S3 job configuration is required").
			WithHint("S3 job configuration must be provided for S3 uploads").
			Mark(ierr.ErrValidation)
	}

	s.logger.Infow("uploading to S3",
		"connection_id", request.ConnectionID,
		"bucket", request.JobConfig.Bucket,
		"region", request.JobConfig.Region)

	// Get the S3 integration client from factory
	s3IntegrationClient, err := s.integrationFactory.GetS3Client(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get S3 integration client from factory").
			Mark(ierr.ErrHTTPClient)
	}

	// Get the configured S3 client with job config
	s3Client, _, err := s3IntegrationClient.GetS3Client(ctx, request.JobConfig, request.ConnectionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get configured S3 client").
			Mark(ierr.ErrHTTPClient)
	}

	// Generate filename with start and end times
	// Format: {prefix}-start_time_{YYMMDDHHMMSS}-end_time_{YYMMDDHHMMSS}.csv
	startTimeStr := request.StartTime.Format("060102150405") // YYMMDDHHMMSS
	endTimeStr := request.EndTime.Format("060102150405")     // YYMMDDHHMMSS
	filenamePrefix := exporter.GetFilenamePrefix()
	filename := fmt.Sprintf("%s-%s-%s", filenamePrefix, startTimeStr, endTimeStr)

	uploadResponse, err := s3Client.UploadCSV(ctx, filename, csvBytes, filenamePrefix)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to upload CSV to S3").
			Mark(ierr.ErrHTTPClient)
	}

	s.logger.Infow("successfully uploaded to S3",
		"file_url", uploadResponse.FileURL,
		"file_size_bytes", uploadResponse.FileSizeBytes)

	return &dto.ExportResponse{
		EntityType:    request.EntityType,
		RecordCount:   recordCount,
		FileURL:       uploadResponse.FileURL,
		FileSizeBytes: uploadResponse.FileSizeBytes,
		ExportedAt:    uploadResponse.UploadedAt,
	}, nil
}

// getExporter returns the appropriate exporter for the given entity type
func (s *ExportService) getExporter(entityType types.ScheduledTaskEntityType) Exporter {
	switch entityType {
	case types.ScheduledTaskEntityTypeEvents:
		return NewEventExporter(s.featureUsageRepo, s.integrationFactory, s.logger)
	case types.ScheduledTaskEntityTypeInvoice:
		return NewInvoiceExporter(s.invoiceRepo, s.integrationFactory, s.logger)
	case types.ScheduledTaskEntityTypeCreditTopups:
		if s.walletRepo == nil {
			s.logger.Errorw("wallet repository not configured for credit topup export")
			return nil
		}
		return NewCreditTopupExporter(s.walletRepo, s.integrationFactory, s.logger)
	case types.ScheduledTaskEntityTypeCreditUsage:
		if s.walletRepo == nil || s.walletBalanceGetter == nil || s.customerRepo == nil {
			s.logger.Errorw("wallet or customer repository not configured for credit usage export",
				"wallet_repo_nil", s.walletRepo == nil,
				"wallet_balance_getter_nil", s.walletBalanceGetter == nil,
				"customer_repo_nil", s.customerRepo == nil)
			return nil
		}
		return NewCreditUsageExporter(s.walletRepo, s.customerRepo, s.walletBalanceGetter, s.integrationFactory, s.logger)
	default:
		return nil
	}
}
