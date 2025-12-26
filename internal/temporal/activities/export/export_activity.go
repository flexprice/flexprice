package export

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	syncExport "github.com/flexprice/flexprice/internal/service/sync/export"
	"github.com/flexprice/flexprice/internal/types"
)

// ExportActivity handles the actual export operations
type ExportActivity struct {
	featureUsageRepo    events.FeatureUsageRepository
	invoiceRepo         invoice.Repository
	walletRepo          wallet.Repository
	walletBalanceGetter syncExport.WalletBalanceGetter
	customerRepo        customer.Repository
	connectionRepo      connection.Repository
	integrationFactory  *integration.Factory
	logger              *logger.Logger
}

// NewExportActivity creates a new export activity
func NewExportActivity(
	featureUsageRepo events.FeatureUsageRepository,
	invoiceRepo invoice.Repository,
	walletRepo wallet.Repository,
	walletBalanceGetter syncExport.WalletBalanceGetter,
	customerRepo customer.Repository,
	connectionRepo connection.Repository,
	integrationFactory *integration.Factory,
	logger *logger.Logger,
) *ExportActivity {
	return &ExportActivity{
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

// ExportDataInput represents input for exporting data
type ExportDataInput struct {
	EntityType   types.ScheduledTaskEntityType
	ConnectionID string
	TenantID     string
	EnvID        string
	StartTime    time.Time
	EndTime      time.Time
	JobConfig    *types.S3JobConfig
}

// ExportDataOutput represents output from export
type ExportDataOutput struct {
	FileURL       string
	RecordCount   int
	FileSizeBytes int64
}

// ExportData performs the complete export: prepare data, generate CSV, upload to provider
func (a *ExportActivity) ExportData(ctx context.Context, input ExportDataInput) (*ExportDataOutput, error) {
	a.logger.Infow("starting data export",
		"entity_type", input.EntityType,
		"tenant_id", input.TenantID,
		"env_id", input.EnvID,
		"start_time", input.StartTime,
		"end_time", input.EndTime)

	// Add tenant and environment to context for repository queries
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvID)

	// Create export request
	request := &dto.ExportRequest{
		EntityType:   input.EntityType,
		ConnectionID: input.ConnectionID,
		TenantID:     input.TenantID,
		EnvID:        input.EnvID,
		StartTime:    input.StartTime,
		EndTime:      input.EndTime,
		JobConfig:    input.JobConfig,
	}

	// Use the ExportService which handles routing to the correct exporter
	exportService := syncExport.NewExportServiceWithWallet(a.featureUsageRepo, a.invoiceRepo, a.walletRepo, a.walletBalanceGetter, a.customerRepo, a.connectionRepo, a.integrationFactory, a.logger)
	response, err := exportService.Export(ctx, request)
	if err != nil {
		a.logger.Errorw("export failed", "error", err, "entity_type", input.EntityType)
		return nil, err
	}

	a.logger.Infow("export completed successfully",
		"entity_type", input.EntityType,
		"file_url", response.FileURL,
		"record_count", response.RecordCount,
		"file_size", response.FileSizeBytes)

	return &ExportDataOutput{
		FileURL:       response.FileURL,
		RecordCount:   response.RecordCount,
		FileSizeBytes: response.FileSizeBytes,
	}, nil
}
