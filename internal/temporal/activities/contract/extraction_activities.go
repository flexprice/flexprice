package contract

import (
	"context"

	cerrors "github.com/cockroachdb/errors"
	contractsvc "github.com/flexprice/flexprice/internal/ee/service/contract"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

const (
	errTypeContractExtraction = "ContractExtractionError"
	errTypeCustomerMatch      = "ContractCustomerMatchError"
	defaultFailureReason      = "Contract extraction failed"
)

type ExtractionActivities struct {
	ingest contractsvc.ContractIngestService
	logger *logger.Logger
}

func NewExtractionActivities(ingest contractsvc.ContractIngestService, logger *logger.Logger) *ExtractionActivities {
	return &ExtractionActivities{ingest: ingest, logger: logger}
}

func (a *ExtractionActivities) ExtractContract(ctx context.Context, input models.ContractExtractionWorkflowInput) error {
	if err := input.Validate(); err != nil {
		return temporal.NewNonRetryableApplicationError(reason(err), errTypeContractExtraction, err)
	}
	ctx = withScope(ctx, input)

	if err := a.ingest.Process(ctx, input.ContractID); err != nil {
		a.logger.Error(ctx, "ExtractContract activity failed",
			"error", err,
			"contract_id", input.ContractID,
			"tenant_id", input.TenantID,
			"environment_id", input.EnvironmentID,
		)
		if isPermanent(err) {
			return temporal.NewNonRetryableApplicationError(reason(err), errTypeContractExtraction, err)
		}
		return temporal.NewApplicationErrorWithCause(reason(err), errTypeContractExtraction, err)
	}
	return nil
}

func (a *ExtractionActivities) MarkContractExtractionFailed(ctx context.Context, input models.MarkContractExtractionFailedInput) error {
	if err := input.Validate(); err != nil {
		return temporal.NewNonRetryableApplicationError(reason(err), errTypeContractExtraction, err)
	}
	ctx = withScope(ctx, input.ContractExtractionWorkflowInput)

	if err := a.ingest.MarkFailed(ctx, input.ContractID, input.Reason); err != nil {
		a.logger.Error(ctx, "MarkContractExtractionFailed activity failed", "error", err, "contract_id", input.ContractID)
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError(reason(err), errTypeContractExtraction, err)
		}
		return err
	}
	return nil
}

func (a *ExtractionActivities) MatchContractCustomer(ctx context.Context, input models.ContractExtractionWorkflowInput) error {
	if err := input.Validate(); err != nil {
		return temporal.NewNonRetryableApplicationError(reason(err), errTypeCustomerMatch, err)
	}
	ctx = withScope(ctx, input)

	if err := a.ingest.MatchCustomer(ctx, input.ContractID); err != nil {
		a.logger.Error(ctx, "MatchContractCustomer activity failed",
			"error", err,
			"contract_id", input.ContractID,
			"tenant_id", input.TenantID,
			"environment_id", input.EnvironmentID,
		)
		if isPermanent(err) || ierr.IsServiceUnavailable(err) {
			return temporal.NewNonRetryableApplicationError(reason(err), errTypeCustomerMatch, err)
		}
		return temporal.NewApplicationErrorWithCause(reason(err), errTypeCustomerMatch, err)
	}
	return nil
}

func (a *ExtractionActivities) MarkCustomerMatchUnavailable(ctx context.Context, input models.MarkCustomerMatchUnavailableInput) error {
	if err := input.Validate(); err != nil {
		return temporal.NewNonRetryableApplicationError(reason(err), errTypeCustomerMatch, err)
	}
	ctx = withScope(ctx, input.ContractExtractionWorkflowInput)

	if err := a.ingest.MarkCustomerMatchUnavailable(ctx, input.ContractID, input.Reason); err != nil {
		a.logger.Error(ctx, "MarkCustomerMatchUnavailable activity failed", "error", err, "contract_id", input.ContractID)
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError(reason(err), errTypeCustomerMatch, err)
		}
		return err
	}
	return nil
}

func withScope(ctx context.Context, input models.ContractExtractionWorkflowInput) context.Context {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	if input.UserID != "" {
		ctx = types.SetUserID(ctx, input.UserID)
	}
	return ctx
}

func isPermanent(err error) bool {
	return ierr.IsValidation(err) || ierr.IsInvalidOperation(err) || ierr.IsNotFound(err)
}

func reason(err error) string {
	if hint := cerrors.FlattenHints(err); hint != "" {
		return hint
	}
	return defaultFailureReason
}
