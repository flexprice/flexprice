package invoice

import (
	"context"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"github.com/flexprice/flexprice/internal/types"
)

// InvoiceActivities contains all invoice-related activities
type InvoiceActivities struct {
	serviceParams service.ServiceParams
	logger        *logger.Logger
}

// NewInvoiceActivities creates a new InvoiceActivities instance
func NewInvoiceActivities(
	serviceParams service.ServiceParams,
	logger *logger.Logger,
) *InvoiceActivities {
	return &InvoiceActivities{
		serviceParams: serviceParams,
		logger:        logger,
	}
}

// CalculateInvoiceActivity computes usage for a draft invoice and either
// populates it with line items + invoice number, or marks it as SKIPPED.
func (s *InvoiceActivities) CalculateInvoiceActivity(
	ctx context.Context,
	input invoiceModels.CalculateInvoiceActivityInput,
) (*invoiceModels.CalculateInvoiceActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invoiceService := service.NewInvoiceService(s.serviceParams)

	skipped, err := invoiceService.CalculateAndPopulateInvoice(ctx, input.InvoiceID)
	if err != nil {
		s.logger.Errorw("failed to calculate and populate invoice",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	if skipped {
		s.logger.Infow("invoice skipped (zero usage)",
			"invoice_id", input.InvoiceID)
	} else {
		s.logger.Infow("invoice calculated and populated successfully",
			"invoice_id", input.InvoiceID)
	}

	return &invoiceModels.CalculateInvoiceActivityOutput{
		Skipped: skipped,
	}, nil
}

// FinalizeInvoiceActivity finalizes an invoice
func (s *InvoiceActivities) FinalizeInvoiceActivity(
	ctx context.Context,
	input invoiceModels.FinalizeInvoiceActivityInput,
) (*invoiceModels.FinalizeInvoiceActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invoiceService := service.NewInvoiceService(s.serviceParams)

	if err := invoiceService.FinalizeInvoice(ctx, input.InvoiceID); err != nil {
		s.logger.Errorw("failed to finalize invoice",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	s.logger.Infow("finalized invoice successfully",
		"invoice_id", input.InvoiceID)

	return &invoiceModels.FinalizeInvoiceActivityOutput{
		Success: true,
	}, nil
}

// SyncInvoiceToVendorActivity syncs an invoice to external vendors
func (s *InvoiceActivities) SyncInvoiceToVendorActivity(
	ctx context.Context,
	input invoiceModels.SyncInvoiceActivityInput,
) (*invoiceModels.SyncInvoiceActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invoiceService := service.NewInvoiceService(s.serviceParams)

	if err := invoiceService.SyncInvoiceToExternalVendors(ctx, input.InvoiceID); err != nil {
		s.logger.Errorw("failed to sync invoice to external vendor",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	s.logger.Infow("synced invoice to external vendor successfully",
		"invoice_id", input.InvoiceID)

	return &invoiceModels.SyncInvoiceActivityOutput{
		Success: true,
	}, nil
}

// AttemptInvoicePaymentActivity attempts to collect payment for an invoice
func (s *InvoiceActivities) AttemptInvoicePaymentActivity(
	ctx context.Context,
	input invoiceModels.PaymentActivityInput,
) (*invoiceModels.PaymentActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invoiceService := service.NewInvoiceService(s.serviceParams)

	if err := invoiceService.AttemptPayment(ctx, input.InvoiceID); err != nil {
		s.logger.Errorw("failed to attempt payment for invoice",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	s.logger.Infow("attempted payment for invoice successfully",
		"invoice_id", input.InvoiceID)

	return &invoiceModels.PaymentActivityOutput{
		Success: true,
	}, nil
}

// RecalculateInvoiceActivity recalculates a voided subscription invoice by creating a replacement invoice (same billing period).
func (s *InvoiceActivities) RecalculateInvoiceActivity(
	ctx context.Context,
	input invoiceModels.RecalculateInvoiceActivityInput,
) (*invoiceModels.RecalculateInvoiceActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invSvc := service.NewInvoiceService(s.serviceParams)

	newInv, err := invSvc.RecalculateInvoice(ctx, input.InvoiceID)
	if err != nil {
		s.logger.Errorw("failed to recalculate invoice",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	outID := input.InvoiceID
	if newInv != nil {
		outID = newInv.ID
	}
	s.logger.Infow("recalculated invoice successfully",
		"invoice_id", input.InvoiceID,
		"new_invoice_id", outID)

	return &invoiceModels.RecalculateInvoiceActivityOutput{
		Success:   true,
		InvoiceID: outID,
	}, nil
}
