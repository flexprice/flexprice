package export

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// Exporter defines the interface for entity-specific exporters
type Exporter interface {
	// PrepareData fetches and prepares data for export
	PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error)

	// GetFilenamePrefix returns the prefix for the exported file
	GetFilenamePrefix() string
}

// StreamingExporter optionally supports writing CSV incrementally to a writer.
type StreamingExporter interface {
	Exporter
	WriteData(ctx context.Context, request *dto.ExportRequest, w io.Writer) (int, error)
}

// ExportService handles export operations for different entity types
type ExportService struct {
	meterUsageRepo           events.MeterUsageRepository
	priceRepo                price.Repository
	invoiceRepo              invoice.Repository
	walletRepo               wallet.Repository
	walletBalanceGetter      WalletBalanceGetter
	customerRepo             customer.Repository
	usageAnalyticsGetter     UsageAnalyticsGetter
	connectionRepo           connection.Repository
	integrationFactory       *integration.Factory
	storageResolver          storage.Resolver
	config                   *config.Configuration
	logger                   *logger.Logger
	eventRepo                events.Repository
	subscriptionLineItemRepo subscription.LineItemRepository
	revenueFactRepo          revenuefact.Repository
}

// NewExportService creates a new export service
func NewExportService(
	meterUsageRepo events.MeterUsageRepository,
	priceRepo price.Repository,
	invoiceRepo invoice.Repository,
	connectionRepo connection.Repository,
	integrationFactory *integration.Factory,
	storageResolver storage.Resolver,
	cfg *config.Configuration,
	logger *logger.Logger,
	eventRepo events.Repository,
) *ExportService {
	return &ExportService{
		meterUsageRepo:     meterUsageRepo,
		priceRepo:          priceRepo,
		invoiceRepo:        invoiceRepo,
		walletRepo:         nil,
		connectionRepo:     connectionRepo,
		integrationFactory: integrationFactory,
		storageResolver:    storageResolver,
		config:             cfg,
		logger:             logger,
		eventRepo:          eventRepo,
	}
}

// NewExportServiceWithWallet creates a new export service with wallet repository
func NewExportServiceWithWallet(
	meterUsageRepo events.MeterUsageRepository,
	priceRepo price.Repository,
	invoiceRepo invoice.Repository,
	walletRepo wallet.Repository,
	walletBalanceGetter WalletBalanceGetter,
	customerRepo customer.Repository,
	connectionRepo connection.Repository,
	integrationFactory *integration.Factory,
	storageResolver storage.Resolver,
	cfg *config.Configuration,
	logger *logger.Logger,
	usageAnalyticsGetter UsageAnalyticsGetter,
	eventRepo events.Repository,
	subscriptionLineItemRepo subscription.LineItemRepository,
	revenueFactRepo revenuefact.Repository,
) *ExportService {
	return &ExportService{
		meterUsageRepo:           meterUsageRepo,
		priceRepo:                priceRepo,
		invoiceRepo:              invoiceRepo,
		walletRepo:               walletRepo,
		walletBalanceGetter:      walletBalanceGetter,
		customerRepo:             customerRepo,
		connectionRepo:           connectionRepo,
		integrationFactory:       integrationFactory,
		storageResolver:          storageResolver,
		config:                   cfg,
		logger:                   logger,
		usageAnalyticsGetter:     usageAnalyticsGetter,
		eventRepo:                eventRepo,
		subscriptionLineItemRepo: subscriptionLineItemRepo,
		revenueFactRepo:          revenueFactRepo,
	}
}

// Export routes the export request to the appropriate entity exporter
func (s *ExportService) Export(ctx context.Context, request *dto.ExportRequest) (*dto.ExportResponse, error) {
	s.logger.Info(ctx, "starting export",
		"entity_type", request.EntityType,
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"start_time", request.StartTime,
		"end_time", request.EndTime)

	ctx = types.SetTenantID(ctx, request.TenantID)
	ctx = types.SetEnvironmentID(ctx, request.EnvID)

	if request.JobConfig == nil {
		return nil, ierr.NewError("job configuration is required").
			WithHint("job configuration must be provided for exports").
			Mark(ierr.ErrValidation)
	}

	if len(request.JobConfig.GetExportMetadataFields()) > 0 {
		if err := request.JobConfig.GetExportMetadataFields().ValidateAndDefault(request.EntityType); err != nil {
			return nil, err
		}
	}

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

func (s *ExportService) executeExport(ctx context.Context, request *dto.ExportRequest, exporter Exporter) (*dto.ExportResponse, error) {
	ctx = types.SetTenantID(ctx, request.TenantID)
	ctx = types.SetEnvironmentID(ctx, request.EnvID)

	conn, err := s.connectionRepo.Get(ctx, request.ConnectionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get connection for export").
			Mark(ierr.ErrDatabase)
	}

	switch conn.ProviderType {
	case types.SecretProviderS3, types.SecretProviderGCS:
	default:
		return nil, ierr.NewError("unsupported provider type").
			WithHintf("Provider type '%s' is not supported for exports", conn.ProviderType).
			Mark(ierr.ErrValidation)
	}

	store, err := s.resolveExportStore(ctx, request)
	if err != nil {
		if ierr.IsValidation(err) {
			return nil, err
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get storage provider from factory").
			Mark(ierr.ErrHTTPClient)
	}

	if streaming, ok := exporter.(StreamingExporter); ok {
		return s.streamToStorage(ctx, request, streaming, store)
	}

	csvBytes, recordCount, err := exporter.PrepareData(ctx, request)
	if err != nil {
		return nil, err
	}

	return s.uploadToStorage(ctx, request, exporter, store, csvBytes, recordCount)
}

// streamToStorage stages CSV on disk for upload and removes the temporary file.
func (s *ExportService) streamToStorage(ctx context.Context, request *dto.ExportRequest, exporter StreamingExporter, store storage.Storage) (*dto.ExportResponse, error) {
	tmp, err := os.CreateTemp("", "flexprice-export-*.csv")
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create temp file for export").
			Mark(ierr.ErrSystem)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	gzipped := request.JobConfig.Compression == types.S3CompressionTypeGzip
	recordCount, csvSize, err := writeExportCSV(ctx, request, exporter, tmp, gzipped)
	if err != nil {
		return nil, err
	}

	key := exportObjectKey(request, exporter)
	s.logger.Info(ctx, "uploading export",
		"connection_id", request.ConnectionID,
		"provider", store.Provider(),
		"destination", store.FileURL(key))

	uploadResponse, err := store.UploadStream(ctx, &storage.StreamUploadRequest{
		Key:              key,
		Body:             tmp,
		Format:           storage.UploadFormatCSV,
		Gzipped:          gzipped,
		UncompressedSize: csvSize,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to upload CSV export").
			Mark(ierr.ErrHTTPClient)
	}

	return s.exportResponse(ctx, request, recordCount, uploadResponse), nil
}

// writeExportCSV writes the export to w, gzipped when asked, and returns the record count and CSV size.
func writeExportCSV(ctx context.Context, request *dto.ExportRequest, exporter StreamingExporter, w io.Writer, gzipped bool) (int, int64, error) {
	if !gzipped {
		csv := &countingWriter{w: w}
		recordCount, err := exporter.WriteData(ctx, request, csv)
		return recordCount, csv.n, err
	}

	gz := gzip.NewWriter(w)
	csv := &countingWriter{w: gz}
	recordCount, err := exporter.WriteData(ctx, request, csv)
	if err != nil {
		return 0, 0, err
	}
	if err := gz.Close(); err != nil {
		return 0, 0, ierr.WithError(err).
			WithHint("Failed to compress export").
			Mark(ierr.ErrSystem)
	}
	return recordCount, csv.n, nil
}

// countingWriter counts the bytes written through it.
type countingWriter struct {
	w io.Writer
	n int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	return n, err
}

// exportObjectKey builds <key_prefix>/<entity>/<entity>-<start>-<end>[-customers-<digest>].csv[.gz].
// A customer-filtered run gets its own key so it never replaces the full export of the same window.
func exportObjectKey(request *dto.ExportRequest, exporter Exporter) string {
	startTimeStr := request.StartTime.Format("060102150405")
	endTimeStr := request.EndTime.Format("060102150405")
	filenamePrefix := exporter.GetFilenamePrefix()
	filename := fmt.Sprintf("%s-%s-%s", filenamePrefix, startTimeStr, endTimeStr)
	if len(request.CustomerExternalIDs) > 0 {
		filename += "-customers-" + customerFilterDigest(request.CustomerExternalIDs)
	}
	return storage.ObjectKey(request.JobConfig.KeyPrefix, filenamePrefix, filename, "csv", request.JobConfig.Compression == types.S3CompressionTypeGzip)
}

func (s *ExportService) exportResponse(ctx context.Context, request *dto.ExportRequest, recordCount int, uploadResponse *storage.UploadResponse) *dto.ExportResponse {
	s.logger.Info(ctx, "successfully uploaded export",
		"file_url", uploadResponse.FileURL,
		"file_size_bytes", uploadResponse.FileSizeBytes)

	return &dto.ExportResponse{
		EntityType:    request.EntityType,
		RecordCount:   recordCount,
		FileURL:       uploadResponse.FileURL,
		FileSizeBytes: uploadResponse.FileSizeBytes,
		ExportedAt:    uploadResponse.UploadedAt,
	}
}

func (s *ExportService) resolveExportStore(ctx context.Context, request *dto.ExportRequest) (storage.Storage, error) {
	if request.JobConfig == nil {
		return nil, ierr.NewError("job configuration is required").
			WithHint("job configuration must be provided for storage uploads").
			Mark(ierr.ErrValidation)
	}

	return s.storageResolver.ForConnectionExport(ctx, request.ConnectionID, request.JobConfig)
}

func (s *ExportService) uploadToStorage(ctx context.Context, request *dto.ExportRequest, exporter Exporter, store storage.Storage, csvBytes []byte, recordCount int) (*dto.ExportResponse, error) {
	key := exportObjectKey(request, exporter)

	// Use resolved destination, not JobConfig.Bucket.
	s.logger.Info(ctx, "uploading export",
		"connection_id", request.ConnectionID,
		"provider", store.Provider(),
		"destination", store.FileURL(key))

	uploadResponse, err := store.Upload(ctx, &storage.UploadRequest{
		Key:      key,
		Data:     csvBytes,
		Format:   storage.UploadFormatCSV,
		Compress: request.JobConfig.Compression == types.S3CompressionTypeGzip,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to upload CSV export").
			Mark(ierr.ErrHTTPClient)
	}

	return s.exportResponse(ctx, request, recordCount, uploadResponse), nil
}

// getExporter returns the appropriate exporter for the given entity type
func (s *ExportService) getExporter(entityType types.ScheduledTaskEntityType) Exporter {
	switch entityType {
	case types.ScheduledTaskEntityTypeEvents:
		return NewEventExporter(s.meterUsageRepo, s.priceRepo, s.integrationFactory, s.config, s.logger)
	case types.ScheduledTaskEntityTypeInvoice:
		return NewInvoiceExporter(s.invoiceRepo, s.integrationFactory, s.logger)
	case types.ScheduledTaskEntityTypeCreditTopups:
		if s.walletRepo == nil {
			s.logger.Info(context.Background(), "wallet repository not configured for credit topup export")
			return nil
		}
		return NewCreditTopupExporter(s.walletRepo, s.integrationFactory, s.logger)
	case types.ScheduledTaskEntityTypeWalletDebits:
		if s.walletRepo == nil {
			s.logger.Info(context.Background(), "wallet repository not configured for wallet debit export")
			return nil
		}
		return NewWalletDebitExporter(s.walletRepo, s.logger)
	case types.ScheduledTaskEntityTypeWalletLedger:
		if s.walletRepo == nil {
			s.logger.Info(context.Background(), "wallet repository not configured for wallet ledger export")
			return nil
		}
		return NewWalletLedgerExporter(s.walletRepo, s.logger)
	case types.ScheduledTaskEntityTypeCreditUsage:
		if s.walletRepo == nil || s.walletBalanceGetter == nil || s.customerRepo == nil {
			s.logger.Info(context.Background(), "wallet or customer repository not configured for credit usage export",
				"wallet_repo_nil", s.walletRepo == nil,
				"wallet_balance_getter_nil", s.walletBalanceGetter == nil,
				"customer_repo_nil", s.customerRepo == nil)
			return nil
		}
		return NewCreditUsageExporter(s.walletRepo, s.customerRepo, s.walletBalanceGetter, s.integrationFactory, s.logger)
	case types.ScheduledTaskEntityTypeRevenueFacts:
		if s.revenueFactRepo == nil {
			s.logger.Info(context.Background(), "revenue fact repository not configured for revenue facts export")
			return nil
		}
		return NewRevenueFactsExporter(s.revenueFactRepo, s.logger)
	case types.ScheduledTaskEntityTypeUsageAnalytics:
		if s.customerRepo == nil || s.subscriptionLineItemRepo == nil {
			s.logger.Info(context.Background(), "customer or subscription line item repository not configured for usage analytics export",
				"customer_repo_nil", s.customerRepo == nil,
				"subscription_line_item_repo_nil", s.subscriptionLineItemRepo == nil)
			return nil
		}
		return NewUsageAnalyticsExporter(s.customerRepo, s.eventRepo, s.subscriptionLineItemRepo, s.usageAnalyticsGetter, s.logger)
	default:
		return nil
	}
}

// customerFilterDigest names a customer selection independently of its order.
func customerFilterDigest(customerExternalIDs []string) string {
	ids := lo.Uniq(customerExternalIDs)
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(sum[:6])
}
