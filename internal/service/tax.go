package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/domain/taxassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type TaxService interface {
	// Core CRUD operations
	CreateTaxRate(ctx context.Context, req dto.CreateTaxRateRequest) (*dto.TaxRateResponse, error)
	GetTaxRate(ctx context.Context, id string) (*dto.TaxRateResponse, error)
	ListTaxRates(ctx context.Context, filter *types.TaxRateFilter) (*dto.ListTaxRatesResponse, error)
	UpdateTaxRate(ctx context.Context, id string, req dto.UpdateTaxRateRequest) (*dto.TaxRateResponse, error)
	GetTaxRateByCode(ctx context.Context, code string) (*dto.TaxRateResponse, error)
	DeleteTaxRate(ctx context.Context, id string) error

	// Tax Applied operations
	ApplyTaxOnInvoice(ctx context.Context, invoiceId string) error
}

type taxService struct {
	ServiceParams
}

// NewTaxRateService creates a new instance of TaxRateService
func NewTaxService(params ServiceParams) TaxService {
	return &taxService{
		ServiceParams: params,
	}
}

// CreateTaxRate creates a new tax rate
func (s *taxService) CreateTaxRate(ctx context.Context, req dto.CreateTaxRateRequest) (*dto.TaxRateResponse, error) {
	// Validate the request
	if err := req.Validate(); err != nil {
		s.Logger.Warnw("tax rate creation validation failed",
			"error", err,
			"name", req.Name,
			"code", req.Code,
		)
		return nil, err
	}

	// Convert the request to a domain model
	taxRate := req.ToTaxRate(ctx)

	// Set tax rate status based on validity period
	now := time.Now().UTC()
	taxRate.TaxRateStatus = s.calculateTaxRateStatus(taxRate, now)

	// Create the tax rate in the repository
	if err := s.TaxRateRepo.Create(ctx, taxRate); err != nil {
		s.Logger.Errorw("failed to create tax rate",
			"error", err,
			"tax_rate_id", taxRate.ID,
			"name", taxRate.Name,
			"code", taxRate.Code,
		)
		return nil, err
	}

	s.Logger.Infow("tax rate created successfully",
		"tax_rate_id", taxRate.ID,
		"name", taxRate.Name,
		"code", taxRate.Code,
		"status", taxRate.TaxRateStatus,
	)

	// Return the created tax rate
	return &dto.TaxRateResponse{TaxRate: taxRate}, nil
}

// GetTaxRate retrieves a tax rate by ID
func (s *taxService) GetTaxRate(ctx context.Context, id string) (*dto.TaxRateResponse, error) {
	if id == "" {
		return nil, ierr.NewError("tax_rate_id is required").
			WithHint("Tax rate ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get the tax rate from the repository
	taxRate, err := s.TaxRateRepo.Get(ctx, id)
	if err != nil {
		s.Logger.Warnw("failed to get tax rate",
			"error", err,
			"tax_rate_id", id,
		)
		return nil, err
	}

	// Return the tax rate
	return &dto.TaxRateResponse{TaxRate: taxRate}, nil
}

// ListTaxRates lists tax rates based on the provided filter
func (s *taxService) ListTaxRates(ctx context.Context, filter *types.TaxRateFilter) (*dto.ListTaxRatesResponse, error) {
	if filter == nil {
		filter = types.NewDefaultTaxRateFilter()
	}

	// Get tax rates from the repository
	taxRates, err := s.TaxRateRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to list tax rates",
			"error", err,
			"filter", filter,
		)
		return nil, err
	}

	// Get the total count of tax rates
	count, err := s.TaxRateRepo.Count(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to count tax rates",
			"error", err,
			"filter", filter,
		)
		return nil, err
	}

	// Build response items
	items := make([]*dto.TaxRateResponse, len(taxRates))
	for i, t := range taxRates {
		items[i] = &dto.TaxRateResponse{TaxRate: t}
	}

	// Create pagination response
	pagination := types.NewPaginationResponse(
		count,
		filter.GetLimit(),
		filter.GetOffset(),
	)

	// Return the response
	return &dto.ListTaxRatesResponse{
		Items:      items,
		Pagination: &pagination,
	}, nil
}

// UpdateTaxRate updates an existing tax rate in place
func (s *taxService) UpdateTaxRate(ctx context.Context, id string, req dto.UpdateTaxRateRequest) (*dto.TaxRateResponse, error) {
	if id == "" {
		return nil, ierr.NewError("tax_rate_id is required").
			WithHint("Tax rate ID is required").
			Mark(ierr.ErrValidation)
	}

	// Validate the update request
	if err := s.validateUpdateRequest(req); err != nil {
		s.Logger.Warnw("tax rate update validation failed",
			"error", err,
			"tax_rate_id", id,
		)
		return nil, err
	}

	// Get the existing tax rate
	taxRate, err := s.TaxRateRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// TODO: check if tax is being used in any tax assignments then dont allow update
	// Apply updates only for non-empty fields
	if req.Name != "" {
		taxRate.Name = req.Name
	}

	if req.Code != "" {
		taxRate.Code = req.Code
	}

	if req.Description != "" {
		taxRate.Description = req.Description
	}

	if req.ValidFrom != nil {
		taxRate.ValidFrom = req.ValidFrom
	}

	if req.ValidTo != nil {
		taxRate.ValidTo = req.ValidTo
	}

	if len(req.Metadata) > 0 {
		taxRate.Metadata = req.Metadata
	}

	// Update status based on validity period if dates were updated
	if req.ValidFrom != nil || req.ValidTo != nil {
		taxRate.TaxRateStatus = s.calculateTaxRateStatus(taxRate, time.Now().UTC())
	}

	// Perform the update in the repository
	if err := s.TaxRateRepo.Update(ctx, taxRate); err != nil {
		s.Logger.Errorw("failed to update tax rate",
			"error", err,
			"tax_rate_id", id,
		)
		return nil, err
	}

	s.Logger.Infow("tax rate updated successfully",
		"tax_rate_id", id,
		"name", taxRate.Name,
		"code", taxRate.Code,
		"status", taxRate.TaxRateStatus,
	)

	// Return the updated tax rate
	return &dto.TaxRateResponse{TaxRate: taxRate}, nil
}

// DeleteTaxRate archives a tax rate by setting its status to archived
func (s *taxService) DeleteTaxRate(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("tax_rate_id is required").
			WithHint("Tax rate ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get the tax rate to archive
	taxRate, err := s.TaxRateRepo.Get(ctx, id)
	if err != nil {
		s.Logger.Warnw("failed to get tax rate for deletion",
			"error", err,
			"tax_rate_id", id,
		)
		return err
	}

	// Call the repository's Delete method which handles archiving
	if err := s.TaxRateRepo.Delete(ctx, taxRate); err != nil {
		s.Logger.Errorw("failed to delete tax rate",
			"error", err,
			"tax_rate_id", id,
		)
		return err
	}

	s.Logger.Infow("tax rate deleted successfully",
		"tax_rate_id", id,
		"name", taxRate.Name,
		"code", taxRate.Code,
	)

	return nil
}

// GetTaxRateByCode retrieves a tax rate by its code
func (s *taxService) GetTaxRateByCode(ctx context.Context, code string) (*dto.TaxRateResponse, error) {
	if code == "" {
		return nil, ierr.NewError("tax_rate_code is required").
			WithHint("Tax rate code is required").
			Mark(ierr.ErrValidation)
	}

	// Get the tax rate by code from the repository
	taxRate, err := s.TaxRateRepo.GetByCode(ctx, code)
	if err != nil {
		s.Logger.Warnw("failed to get tax rate by code",
			"error", err,
			"code", code,
		)
		return nil, err
	}

	// Return the tax rate
	return &dto.TaxRateResponse{TaxRate: taxRate}, nil
}

// validateUpdateRequest validates the update request
func (s *taxService) validateUpdateRequest(req dto.UpdateTaxRateRequest) error {
	// Validate that at least one field is being updated
	if req.Name == "" && req.Code == "" && req.Description == "" &&
		req.ValidFrom == nil && req.ValidTo == nil &&
		len(req.Metadata) == 0 {
		return ierr.NewError("at least one field must be provided for update").
			WithHint("Please provide at least one field to update").
			Mark(ierr.ErrValidation)
	}

	// Validate date range if both dates are provided
	if req.ValidFrom != nil && req.ValidTo != nil && req.ValidFrom.After(*req.ValidTo) {
		return ierr.NewError("valid_from cannot be after valid_to").
			WithHint("Valid from date cannot be after valid to date").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// calculateTaxRateStatus determines the appropriate status based on validity dates
func (s *taxService) calculateTaxRateStatus(taxRate *taxrate.TaxRate, now time.Time) types.TaxRateStatus {
	// If ValidFrom is in the future, tax rate should be inactive
	if taxRate.ValidFrom != nil && taxRate.ValidFrom.After(now) {
		return types.TaxRateStatusInactive
	}

	// If ValidTo is in the past, tax rate should be inactive
	if taxRate.ValidTo != nil && taxRate.ValidTo.Before(now) {
		return types.TaxRateStatusInactive
	}

	// If ValidFrom is nil or in the past, and ValidTo is nil or in the future, tax rate should be active
	return types.TaxRateStatusActive
}

func (s *taxService) ApplyTaxOnInvoice(ctx context.Context, invoiceId string) error {
	// Use database transaction to ensure all operations succeed or fail together
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Get the invoice
		invoice, err := s.InvoiceRepo.Get(txCtx, invoiceId)
		if err != nil {
			s.Logger.Errorw("failed to get invoice for tax application",
				"error", err,
				"invoice_id", invoiceId,
			)
			return err
		}

		// Check if invoice has a subscription ID
		if invoice.SubscriptionID == nil {
			s.Logger.Warnw("invoice has no subscription ID, skipping tax application",
				"invoice_id", invoiceId,
			)
			return nil
		}

		// Get all the taxes associated with the subscription of this invoice
		taxAssociations, err := s.TaxAssociationRepo.List(txCtx, &types.TaxAssociationFilter{
			EntityID:   lo.FromPtr(invoice.SubscriptionID),
			EntityType: types.TaxrateEntityTypeSubscription,
		})
		if err != nil {
			s.Logger.Errorw("failed to get tax associations for subscription",
				"error", err,
				"invoice_id", invoiceId,
				"subscription_id", lo.FromPtr(invoice.SubscriptionID),
			)
			return err
		}

		if len(taxAssociations) == 0 {
			s.Logger.Infow("no tax associations found for subscription, skipping tax application",
				"invoice_id", invoiceId,
				"subscription_id", lo.FromPtr(invoice.SubscriptionID),
			)
			return nil
		}

		// Get all the tax rate IDs associated with the tax associations
		subscriptionTaxRateIds := lo.Map(taxAssociations, func(taxAssociation *taxassociation.TaxAssociation, _ int) string {
			return taxAssociation.TaxRateID
		})

		// Get all the tax rates associated with the tax associations
		taxRates, err := s.TaxRateRepo.List(txCtx, &types.TaxRateFilter{
			TaxRateIDs: subscriptionTaxRateIds,
		})
		if err != nil {
			s.Logger.Errorw("failed to get tax rates for tax associations",
				"error", err,
				"invoice_id", invoiceId,
				"tax_rate_ids", subscriptionTaxRateIds,
			)
			return err
		}

		// Taxable amount is the subtotal of the invoice
		taxableAmount := invoice.Subtotal
		totalTaxAmount := decimal.Zero

		// Create a map to store tax association by tax rate ID for quick lookup
		taxAssociationMap := make(map[string]*taxassociation.TaxAssociation)
		for _, ta := range taxAssociations {
			taxAssociationMap[ta.TaxRateID] = ta
		}

		// Apply each tax rate to the taxable amount
		for _, taxRate := range taxRates {
			var taxAmount decimal.Decimal

			// Calculate tax amount based on tax rate type
			switch taxRate.TaxRateType {
			case types.TaxRateTypePercentage:
				// For percentage tax: taxable_amount * (percentage / 100)
				taxAmount = taxableAmount.Mul(*taxRate.PercentageValue).Div(decimal.NewFromInt(100))
			case types.TaxRateTypeFixed:
				// For fixed tax: use the fixed value directly
				taxAmount = *taxRate.FixedValue
			default:
				s.Logger.Warnw("unknown tax rate type, skipping",
					"tax_rate_id", taxRate.ID,
					"tax_rate_type", taxRate.TaxRateType,
				)
				continue
			}

			// Add tax amount to total
			totalTaxAmount = totalTaxAmount.Add(taxAmount)

			// Get the tax association for this tax rate
			taxAssociation := taxAssociationMap[taxRate.ID]

			// Create a tax applied record
			taxApplied := &taxapplied.TaxApplied{
				ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_APPLIED),
				TaxRateID:        taxRate.ID,
				EntityType:       types.TaxrateEntityTypeInvoice,
				EntityID:         invoiceId,
				TaxAssociationID: lo.ToPtr(taxAssociation.ID),
				TaxableAmount:    taxableAmount,
				TaxAmount:        taxAmount,
				Currency:         invoice.Currency,
				AppliedAt:        time.Now().UTC(),
				BaseModel:        types.GetDefaultBaseModel(txCtx),
			}

			// Set environment ID
			if invoice.EnvironmentID != "" {
				taxApplied.EnvironmentID = invoice.EnvironmentID
			} else {
				taxApplied.EnvironmentID = types.GetEnvironmentID(txCtx)
			}

			// Create the tax applied record
			if err := s.TaxAppliedRepo.Create(txCtx, taxApplied); err != nil {
				s.Logger.Errorw("failed to create tax applied record",
					"error", err,
					"tax_applied_id", taxApplied.ID,
					"tax_rate_id", taxRate.ID,
				)
				return err
			}

			s.Logger.Infow("created tax applied record",
				"tax_applied_id", taxApplied.ID,
				"tax_rate_id", taxRate.ID,
				"tax_rate_code", taxRate.Code,
				"tax_amount", taxAmount,
				"taxable_amount", taxableAmount,
				"invoice_id", invoiceId,
			)
		}

		// Update the invoice with the total tax and recalculate the total
		invoice.TotalTax = totalTaxAmount
		invoice.Total = invoice.Subtotal.Add(totalTaxAmount)

		// Update the invoice
		if err := s.InvoiceRepo.Update(txCtx, invoice); err != nil {
			s.Logger.Errorw("failed to update invoice with tax amounts",
				"error", err,
				"invoice_id", invoiceId,
				"total_tax", totalTaxAmount,
				"new_total", invoice.Total,
			)
			return err
		}

		s.Logger.Infow("successfully applied taxes to invoice",
			"invoice_id", invoiceId,
			"subtotal", invoice.Subtotal,
			"total_tax", totalTaxAmount,
			"total", invoice.Total,
			"tax_rates_applied", len(taxRates),
		)

		return nil
	})
}
