package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/domain/taxassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
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
	RecalculateInvoiceTaxes(ctx context.Context, invoiceId string) error

	// tax association operations
	CreateTaxAssociation(ctx context.Context, ta *dto.CreateTaxAssociationRequest) (*dto.TaxAssociationResponse, error)
	GetTaxAssociation(ctx context.Context, id string) (*dto.TaxAssociationResponse, error)
	UpdateTaxAssociation(ctx context.Context, id string, ta *dto.TaxAssociationUpdateRequest) (*dto.TaxAssociationResponse, error)
	DeleteTaxAssociation(ctx context.Context, id string) error
	ListTaxAssociations(ctx context.Context, filter *types.TaxAssociationFilter) (*dto.ListTaxAssociationsResponse, error)

	// LinkTaxRatesToEntity links tax rates to any entity type
	LinkTaxRatesToEntity(ctx context.Context, entityType types.TaxrateEntityType, entityID string, taxRateLinks []*dto.CreateEntityTaxAssociation) (*dto.EntityTaxAssociationResponse, error)

	// New methods for handling tax rate creation and association separately
	ResolveTaxOverrides(ctx context.Context, taxRateOverrides []*dto.TaxRateOverride) ([]*dto.ResolvedTaxRateInfo, error)

	// tax application operations
	CreateTaxApplied(ctx context.Context, req dto.CreateTaxAppliedRequest) (*dto.TaxAppliedResponse, error)
	GetTaxApplied(ctx context.Context, id string) (*dto.TaxAppliedResponse, error)
	ListTaxApplied(ctx context.Context, filter *types.TaxAppliedFilter) (*dto.ListTaxAppliedResponse, error)
	DeleteTaxApplied(ctx context.Context, id string) error

	// Invoice tax operations
	PrepareTaxRatesForInvoice(ctx context.Context, req dto.CreateInvoiceRequest) ([]*dto.TaxRateResponse, error)
	ApplyTaxesOnInvoice(ctx context.Context, inv *invoice.Invoice, taxRates []*dto.TaxRateResponse) (*TaxCalculationResult, error)
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
	if err := req.Validate(); err != nil {
		s.Logger.Warnw("tax rate update validation failed",
			"error", err,
			"tax_rate_id", id,
		)
		return nil, err
	}

	// check is tax rate is being used in any tax assignments
	taxAssociations, err := s.TaxAssociationRepo.List(ctx, &types.TaxAssociationFilter{
		TaxRateIDs: []string{id},
	})
	if err != nil {
		s.Logger.Errorw("failed to get tax associations for tax rate",
			"error", err,
			"tax_rate_id", id,
		)
		return nil, err
	}

	if len(taxAssociations) > 0 {
		s.Logger.Warnw("tax rate is being used in tax assignments, cannot update",
			"tax_rate_id", id,
		)
		return nil, ierr.NewError("tax rate is being used in tax assignments, cannot update").
			WithHint("Tax rate is being used in tax assignments, cannot update").
			Mark(ierr.ErrValidation)
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

func (s *taxService) RecalculateInvoiceTaxes(ctx context.Context, invoiceId string) error {
	// Use database transaction to ensure all operations succeed or fail together
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Get the invoice
		invoice, err := s.InvoiceRepo.Get(txCtx, invoiceId)
		if err != nil {
			s.Logger.Errorw("failed to get invoice for tax recalculation",
				"error", err,
				"invoice_id", invoiceId,
			)
			return err
		}

		// Check if invoice has a subscription ID
		if invoice.SubscriptionID == nil {
			s.Logger.Warnw("invoice has no subscription ID, skipping tax recalculation",
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

		s.Logger.Infow("tax associations found for subscription",
			"invoice_id", invoiceId,
			"entity_type", types.TaxrateEntityTypeSubscription,
			"subscription_id", lo.FromPtr(invoice.SubscriptionID),
			"tax_associations", taxAssociations,
		)

		if len(taxAssociations) == 0 {
			s.Logger.Infow("no tax associations found for subscription, skipping tax recalculation",
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
			taxAppliedRecord := &dto.CreateTaxAppliedRequest{
				TaxRateID:        taxRate.ID,
				EntityType:       types.TaxrateEntityTypeInvoice,
				EntityID:         invoiceId,
				TaxAssociationID: lo.ToPtr(taxAssociation.ID),
				TaxableAmount:    taxableAmount,
				TaxAmount:        taxAmount,
				Currency:         invoice.Currency,
			}

			taxApplied := taxAppliedRecord.ToTaxApplied(txCtx)

			// set applied at to the invoice due date
			taxApplied.AppliedAt = time.Now().UTC()

			// Create the tax applied record
			if err := s.TaxAppliedRepo.Create(txCtx, taxApplied); err != nil {
				s.Logger.Errorw("failed to create tax applied record",
					"error", err,
					"tax_rate_id", taxRate.ID,
				)
				return err
			}

			s.Logger.Infow("created tax applied record",
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

		return nil
	})
}

// CreateTaxApplied creates a new tax applied record
func (s *taxService) CreateTaxApplied(ctx context.Context, req dto.CreateTaxAppliedRequest) (*dto.TaxAppliedResponse, error) {
	// Validate the request
	if err := req.Validate(); err != nil {
		s.Logger.Warnw("tax applied creation validation failed",
			"error", err,
			"tax_rate_id", req.TaxRateID,
			"entity_type", req.EntityType,
			"entity_id", req.EntityID,
		)
		return nil, err
	}

	// Convert the request to a domain model
	taxApplied := req.ToTaxApplied(ctx)

	// Create the tax applied record in the repository
	if err := s.TaxAppliedRepo.Create(ctx, taxApplied); err != nil {
		s.Logger.Errorw("failed to create tax applied record",
			"error", err,
			"tax_applied_id", taxApplied.ID,
			"tax_rate_id", taxApplied.TaxRateID,
			"entity_type", taxApplied.EntityType,
			"entity_id", taxApplied.EntityID,
		)
		return nil, err
	}

	s.Logger.Infow("tax applied record created successfully",
		"tax_applied_id", taxApplied.ID,
		"tax_rate_id", taxApplied.TaxRateID,
		"entity_type", taxApplied.EntityType,
		"entity_id", taxApplied.EntityID,
		"tax_amount", taxApplied.TaxAmount,
	)

	// Return the created tax applied record
	return &dto.TaxAppliedResponse{TaxApplied: *taxApplied}, nil
}

// GetTaxApplied retrieves a tax applied record by ID
func (s *taxService) GetTaxApplied(ctx context.Context, id string) (*dto.TaxAppliedResponse, error) {
	if id == "" {
		return nil, ierr.NewError("tax_applied_id is required").
			WithHint("Tax applied ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get the tax applied record from the repository
	taxApplied, err := s.TaxAppliedRepo.Get(ctx, id)
	if err != nil {
		s.Logger.Warnw("failed to get tax applied record",
			"error", err,
			"tax_applied_id", id,
		)
		return nil, err
	}

	// Return the tax applied record
	return &dto.TaxAppliedResponse{TaxApplied: *taxApplied}, nil
}

// ListTaxApplied lists tax applied records based on the provided filter
func (s *taxService) ListTaxApplied(ctx context.Context, filter *types.TaxAppliedFilter) (*dto.ListTaxAppliedResponse, error) {
	if filter == nil {
		filter = types.NewDefaultTaxAppliedFilter()
	}

	// Validate the filter
	if err := filter.Validate(); err != nil {
		s.Logger.Warnw("tax applied filter validation failed",
			"error", err,
			"filter", filter,
		)
		return nil, err
	}

	// Get tax applied records from the repository
	taxAppliedRecords, err := s.TaxAppliedRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to list tax applied records",
			"error", err,
			"filter", filter,
		)
		return nil, err
	}

	// Build response items
	items := make([]*dto.TaxAppliedResponse, len(taxAppliedRecords))
	for i, ta := range taxAppliedRecords {
		items[i] = &dto.TaxAppliedResponse{TaxApplied: *ta}
	}

	// Get the total count of tax applied records
	count, err := s.TaxAppliedRepo.Count(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to count tax applied records",
			"error", err,
			"filter", filter,
		)
		return nil, err
	}

	// Return the response with pagination
	// Note: Since the repository doesn't have a Count method, we'll use the length of items
	// This is a limitation, but it's consistent with how other services handle this
	return &dto.ListTaxAppliedResponse{
		Items:      items,
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

// DeleteTaxApplied deletes a tax applied record
func (s *taxService) DeleteTaxApplied(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("tax_applied_id is required").
			WithHint("Tax applied ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get the tax applied record to ensure it exists
	taxApplied, err := s.TaxAppliedRepo.Get(ctx, id)
	if err != nil {
		s.Logger.Warnw("failed to get tax applied record for deletion",
			"error", err,
			"tax_applied_id", id,
		)
		return err
	}

	// Delete the tax applied record
	if err := s.TaxAppliedRepo.Delete(ctx, id); err != nil {
		s.Logger.Errorw("failed to delete tax applied record",
			"error", err,
			"tax_applied_id", id,
		)
		return err
	}

	s.Logger.Infow("tax applied record deleted successfully",
		"tax_applied_id", id,
		"tax_rate_id", taxApplied.TaxRateID,
		"entity_type", taxApplied.EntityType,
		"entity_id", taxApplied.EntityID,
	)

	return nil
}

// CreateTaxAssociation creates a new tax association
func (s *taxService) CreateTaxAssociation(ctx context.Context, req *dto.CreateTaxAssociationRequest) (*dto.TaxAssociationResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	taxRate, err := s.TaxRateRepo.Get(ctx, req.TaxRateID)
	if err != nil {
		return nil, err
	}

	// Convert request to domain model
	tc := req.ToTaxAssociation(ctx, lo.FromPtr(taxRate))

	s.Logger.Infow("creating tax config",
		"tax_rate_id", tc.TaxRateID,
		"entity_type", tc.EntityType,
		"entity_id", tc.EntityID,
		"priority", tc.Priority,
		"auto_apply", tc.AutoApply)

	// Create tax config
	err = s.TaxAssociationRepo.Create(ctx, tc)
	if err != nil {
		s.Logger.Errorw("failed to create tax config",
			"error", err,
			"tax_rate_id", tc.TaxRateID,
			"entity_type", tc.EntityType,
			"entity_id", tc.EntityID)
		return nil, err
	}

	s.Logger.Infow("tax config created successfully",
		"tax_config_id", tc.ID,
		"tax_rate_id", tc.TaxRateID,
		"entity_type", tc.EntityType,
		"entity_id", tc.EntityID)

	return dto.ToTaxAssociationResponse(tc), nil
}

// GetTaxAssociation retrieves a tax association by ID
func (s *taxService) GetTaxAssociation(ctx context.Context, id string) (*dto.TaxAssociationResponse, error) {
	if id == "" {
		return nil, ierr.NewError("tax config ID is required").
			WithHint("Tax config ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	s.Logger.Debugw("getting tax config", "tax_config_id", id)

	tc, err := s.TaxAssociationRepo.Get(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to get tax config",
			"error", err,
			"tax_config_id", id)
		return nil, err
	}

	if tc == nil {
		return nil, ierr.NewError("tax config not found").
			WithHint(fmt.Sprintf("Tax config with ID %s does not exist", id)).
			WithReportableDetails(map[string]interface{}{
				"tax_config_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	return dto.ToTaxAssociationResponse(tc), nil
}

// UpdateTaxAssociation updates a tax association
func (s *taxService) UpdateTaxAssociation(ctx context.Context, id string, req *dto.TaxAssociationUpdateRequest) (*dto.TaxAssociationResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if id == "" {
		return nil, ierr.NewError("tax config ID is required").
			WithHint("Tax config ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Get existing tax config to ensure it exists
	existing, err := s.TaxAssociationRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		return nil, ierr.NewError("tax config not found").
			WithHint(fmt.Sprintf("Tax config with ID %s does not exist", id)).
			WithReportableDetails(map[string]interface{}{
				"tax_config_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Update fields if provided
	if req.Priority >= 0 {
		existing.Priority = req.Priority
	}
	existing.AutoApply = req.AutoApply

	if req.Metadata != nil {
		existing.Metadata = req.Metadata
	}

	s.Logger.Infow("updating tax config",
		"tax_config_id", id,
		"tax_rate_id", existing.TaxRateID,
		"entity_type", existing.EntityType,
		"entity_id", existing.EntityID)

	// Update tax config
	err = s.TaxAssociationRepo.Update(ctx, existing)
	if err != nil {
		s.Logger.Errorw("failed to update tax config",
			"error", err,
			"tax_config_id", id)
		return nil, err
	}

	s.Logger.Infow("tax config updated successfully",
		"tax_config_id", id,
		"tax_rate_id", existing.TaxRateID,
		"entity_type", existing.EntityType,
		"entity_id", existing.EntityID)

	return dto.ToTaxAssociationResponse(existing), nil
}

// DeleteTaxAssociation deletes a tax association
func (s *taxService) DeleteTaxAssociation(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("tax config ID is required").
			WithHint("Tax config ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Get existing tax config to ensure it exists
	existing, err := s.TaxAssociationRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Delete tax config
	err = s.TaxAssociationRepo.Delete(ctx, existing)
	if err != nil {
		s.Logger.Errorw("failed to delete tax config",
			"error", err,
			"tax_config_id", id)
		return err
	}

	return nil
}

// ListTaxAssociations lists tax associations
func (s *taxService) ListTaxAssociations(ctx context.Context, filter *types.TaxAssociationFilter) (*dto.ListTaxAssociationsResponse, error) {
	if filter == nil {
		filter = types.NewTaxAssociationFilter()
	}

	// Validate filter
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	s.Logger.Debugw("listing tax configs",
		"limit", filter.GetLimit(),
		"offset", filter.GetOffset(),
		"entity_type", filter.EntityType,
		"entity_id", filter.EntityID,
		"tax_rate_ids_count", len(filter.TaxRateIDs))

	// List tax configs
	taxConfigs, err := s.TaxAssociationRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to list tax configs",
			"error", err,
			"limit", filter.GetLimit(),
			"offset", filter.GetOffset())
		return nil, err
	}

	// Get total count for pagination
	total, err := s.TaxAssociationRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &dto.ListTaxAssociationsResponse{
		Items: make([]*dto.TaxAssociationResponse, len(taxConfigs)),
	}

	for i, tc := range taxConfigs {
		response.Items[i] = dto.ToTaxAssociationResponse(tc)
	}

	response.Pagination = types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset())

	s.Logger.Debugw("successfully listed tax configs",
		"count", len(taxConfigs),
		"total", total,
		"limit", filter.GetLimit(),
		"offset", filter.GetOffset())

	return response, nil
}

// LinkTaxRatesToEntity links tax rates to any entity in a single transaction
func (s *taxService) LinkTaxRatesToEntity(ctx context.Context, entityType types.TaxrateEntityType, entityID string, taxRateLinks []*dto.CreateEntityTaxAssociation) (*dto.EntityTaxAssociationResponse, error) {
	// Early return for empty input
	if len(taxRateLinks) == 0 {
		return &dto.EntityTaxAssociationResponse{
			EntityID:       entityID,
			EntityType:     entityType,
			LinkedTaxRates: []*dto.LinkedTaxRateInfo{},
		}, nil
	}

	// Pre-validate all tax rate links before starting transaction
	for _, taxRateLink := range taxRateLinks {
		if err := taxRateLink.Validate(); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Invalid tax rate configuration").
				Mark(ierr.ErrValidation)
		}
	}

	// Execute all operations within a single transaction
	var response *dto.EntityTaxAssociationResponse
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Step 1: Convert entity tax associations to tax overrides for resolution
		taxRateOverrides := make([]*dto.TaxRateOverride, 0, len(taxRateLinks))
		for _, taxRateLink := range taxRateLinks {
			taxRateOverride := &dto.TaxRateOverride{
				CreateTaxRateRequest: taxRateLink.CreateTaxRateRequest,
				TaxRateID:            taxRateLink.TaxRateID,
				Priority:             taxRateLink.Priority,
				AutoApply:            taxRateLink.AutoApply,
			}
			taxRateOverrides = append(taxRateOverrides, taxRateOverride)
		}

		// Step 2: Resolve or create tax rates
		resolvedTaxRates, err := s.ResolveTaxOverrides(txCtx, taxRateOverrides)
		if err != nil {
			return err
		}

		// Step 3: Create tax associations
		linkedTaxRates := make([]*dto.LinkedTaxRateInfo, 0, len(resolvedTaxRates))
		for _, resolvedTaxRate := range resolvedTaxRates {
			// Create tax association request
			taxConfigReq := &dto.CreateTaxAssociationRequest{
				TaxRateID:  resolvedTaxRate.TaxRateID,
				EntityType: entityType,
				EntityID:   entityID,
				Priority:   resolvedTaxRate.Priority,
				AutoApply:  resolvedTaxRate.AutoApply,
			}

			// Create tax association
			taxConfig, err := s.CreateTaxAssociation(txCtx, taxConfigReq)
			if err != nil {
				return err
			}

			// Add to results
			linkedTaxRates = append(linkedTaxRates, &dto.LinkedTaxRateInfo{
				TaxRateID:        resolvedTaxRate.TaxRateID,
				TaxAssociationID: taxConfig.ID,
				WasCreated:       resolvedTaxRate.WasCreated,
				Priority:         taxConfig.Priority,
				AutoApply:        taxConfig.AutoApply,
			})
		}

		s.Logger.Infow("successfully created tax associations",
			"entity_type", entityType,
			"entity_id", entityID,
			"associations_count", len(linkedTaxRates))

		response = &dto.EntityTaxAssociationResponse{
			EntityID:       entityID,
			EntityType:     entityType,
			LinkedTaxRates: linkedTaxRates,
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	s.Logger.Infow("successfully linked tax rates to entity",
		"entity_type", entityType,
		"entity_id", entityID,
		"links_count", len(response.LinkedTaxRates))

	return response, nil
}

// ResolveTaxOverrides handles the creation or resolution of tax rates from tax overrides
// This method focuses purely on ensuring tax rates exist and returning resolved information
// without entity-specific concerns, making it more reusable and purposeful
func (s *taxService) ResolveTaxOverrides(ctx context.Context, taxRateOverrides []*dto.TaxRateOverride) ([]*dto.ResolvedTaxRateInfo, error) {
	if len(taxRateOverrides) == 0 {
		return []*dto.ResolvedTaxRateInfo{}, nil
	}

	resolvedTaxRates := make([]*dto.ResolvedTaxRateInfo, 0, len(taxRateOverrides))

	for _, taxRateOverride := range taxRateOverrides {
		var taxRateToUse *taxrate.TaxRate
		var wasCreated bool

		// Resolve or create tax rate
		if taxRateOverride.TaxRateID != nil {
			// Use existing tax rate
			taxRateID := *taxRateOverride.TaxRateID
			existingTaxRate, err := s.TaxRateRepo.Get(ctx, taxRateID)
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Tax rate not found").
					WithReportableDetails(map[string]interface{}{
						"tax_rate_id": taxRateID,
					}).
					Mark(ierr.ErrNotFound)
			}
			taxRateToUse = existingTaxRate
			wasCreated = false
		} else {
			// Create new tax rate using the service's methods directly
			taxRateResponse, err := s.CreateTaxRate(ctx, taxRateOverride.CreateTaxRateRequest)
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to create tax rate").
					WithReportableDetails(map[string]interface{}{
						"tax_rate_code": taxRateOverride.CreateTaxRateRequest.Code,
					}).
					Mark(ierr.ErrDatabase)
			}
			taxRateToUse = taxRateResponse.TaxRate
			wasCreated = true
		}

		// Add to results
		resolvedTaxRates = append(resolvedTaxRates, &dto.ResolvedTaxRateInfo{
			TaxRateID:  taxRateToUse.ID,
			WasCreated: wasCreated,
			Priority:   taxRateOverride.Priority,
			AutoApply:  taxRateOverride.AutoApply,
		})
	}

	s.Logger.Infow("successfully resolved or created tax rates",
		"resolved_count", len(resolvedTaxRates),
		"created_count", len(lo.Filter(resolvedTaxRates, func(r *dto.ResolvedTaxRateInfo, _ int) bool {
			return r.WasCreated
		})))

	return resolvedTaxRates, nil
}

// PrepareTaxRatesForInvoice prepares tax rates for an invoice based on the request
// This method handles both tax rate overrides and subscription tax rates
// Following clean architecture principles - only depends on the request data
func (s *taxService) PrepareTaxRatesForInvoice(ctx context.Context, req dto.CreateInvoiceRequest) ([]*dto.TaxRateResponse, error) {
	var taxRates []*dto.TaxRateResponse

	if len(req.TaxRateOverrides) > 0 {
		// Handle tax rate overrides for the invoice
		s.Logger.Infow("processing tax rate overrides for invoice",
			"customer_id", req.CustomerID,
			"subscription_id", req.SubscriptionID,
			"overrides_count", len(req.TaxRateOverrides))

		// Step 1: Resolve or create tax rates directly from overrides
		resolvedTaxRates, err := s.ResolveTaxOverrides(ctx, req.TaxRateOverrides)
		if err != nil {
			s.Logger.Errorw("failed to resolve or create tax rates",
				"error", err,
				"customer_id", req.CustomerID)
			return nil, err
		}

		// Step 2: Collect all tax rate IDs
		taxRateIDs := make([]string, 0, len(resolvedTaxRates))
		for _, resolvedTaxRate := range resolvedTaxRates {
			taxRateIDs = append(taxRateIDs, resolvedTaxRate.TaxRateID)
		}

		// Step 3: Fetch all tax rates using the collected IDs
		taxRatesResponse, err := s.ListTaxRates(ctx, &types.TaxRateFilter{
			TaxRateIDs: taxRateIDs,
		})
		if err != nil {
			s.Logger.Errorw("failed to fetch tax rates",
				"error", err,
				"customer_id", req.CustomerID,
				"tax_rate_ids", taxRateIDs)
			return nil, err
		}

		taxRates = taxRatesResponse.Items
	} else {
		// Handle subscription tax rate overrides (existing logic)
		// For subscription invoices, we need to get the associated tax rates
		if req.SubscriptionID != nil {
			// Get tax associations for the subscription
			filter := types.NewNoLimitTaxAssociationFilter()
			filter.EntityType = types.TaxrateEntityTypeSubscription
			filter.EntityID = *req.SubscriptionID
			filter.AutoApply = lo.ToPtr(true)

			taxAssociations, err := s.ListTaxAssociations(ctx, filter)
			if err != nil {
				s.Logger.Errorw("failed to get tax associations for subscription",
					"error", err,
					"subscription_id", *req.SubscriptionID)
				return nil, err
			}

			// Get tax rates for the associations
			if len(taxAssociations.Items) > 0 {
				taxRateIDs := make([]string, 0, len(taxAssociations.Items))
				for _, association := range taxAssociations.Items {
					taxRateIDs = append(taxRateIDs, association.TaxRateID)
				}

				taxRatesResponse, err := s.ListTaxRates(ctx, &types.TaxRateFilter{
					TaxRateIDs: taxRateIDs,
				})
				if err != nil {
					s.Logger.Errorw("failed to fetch subscription tax rates",
						"error", err,
						"subscription_id", *req.SubscriptionID,
						"tax_rate_ids", taxRateIDs)
					return nil, err
				}

				taxRates = taxRatesResponse.Items
			}
		}
	}

	return taxRates, nil
}

// TaxCalculationResult represents the result of tax calculations
type TaxCalculationResult struct {
	TotalTaxAmount    decimal.Decimal
	TaxAppliedRecords []*dto.TaxAppliedResponse
	TaxRates          []*dto.TaxRateResponse
}

// ApplyTaxesOnInvoice applies taxes to an invoice and creates/updates tax applied records
// This method handles idempotency by checking for existing tax applied records
// Returns calculated tax data instead of directly updating the invoice
func (s *taxService) ApplyTaxesOnInvoice(ctx context.Context, inv *invoice.Invoice, taxRates []*dto.TaxRateResponse) (*TaxCalculationResult, error) {
	if len(taxRates) == 0 {
		s.Logger.Infow("no tax rates to apply to invoice", "invoice_id", inv.ID)
		return &TaxCalculationResult{
			TotalTaxAmount:    decimal.Zero,
			TaxAppliedRecords: []*dto.TaxAppliedResponse{},
			TaxRates:          taxRates,
		}, nil
	}

	s.Logger.Infow("applying taxes to invoice",
		"invoice_id", inv.ID,
		"tax_rates_count", len(taxRates))

	// Initialize tax calculation variables
	taxableAmount := inv.Subtotal
	totalTaxAmount := decimal.Zero
	idempGen := idempotency.NewGenerator()
	taxAppliedRecords := make([]*dto.TaxAppliedResponse, 0, len(taxRates))

	// Process each tax rate
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

		// Generate idempotency key for this tax application
		idempotencyKey := idempGen.GenerateKey(idempotency.ScopeTaxApplication, map[string]interface{}{
			"tax_rate_id": taxRate.ID,
			"entity_id":   inv.ID,
			"entity_type": string(types.TaxrateEntityTypeInvoice),
		})

		// Check if tax applied record already exists
		existingTaxApplied, err := s.TaxAppliedRepo.GetByIdempotencyKey(ctx, idempotencyKey)
		if err != nil && !ierr.IsNotFound(err) {
			s.Logger.Errorw("failed to check existing tax applied record",
				"error", err,
				"tax_rate_id", taxRate.ID,
				"invoice_id", inv.ID,
				"idempotency_key", idempotencyKey)
			return nil, err
		}

		var taxAppliedResponse *dto.TaxAppliedResponse

		if existingTaxApplied != nil {
			// Update existing tax applied record
			existingTaxApplied.TaxableAmount = taxableAmount
			existingTaxApplied.TaxAmount = taxAmount
			existingTaxApplied.AppliedAt = time.Now().UTC()

			if err := s.TaxAppliedRepo.Update(ctx, existingTaxApplied); err != nil {
				s.Logger.Errorw("failed to update existing tax applied record",
					"error", err,
					"tax_applied_id", existingTaxApplied.ID,
					"tax_rate_id", taxRate.ID,
					"invoice_id", inv.ID)
				return nil, err
			}

			taxAppliedResponse = &dto.TaxAppliedResponse{TaxApplied: *existingTaxApplied}

			s.Logger.Infow("updated existing tax applied record",
				"tax_applied_id", existingTaxApplied.ID,
				"tax_rate_id", taxRate.ID,
				"tax_rate_code", taxRate.Code,
				"tax_amount", taxAmount,
				"taxable_amount", taxableAmount,
				"invoice_id", inv.ID,
			)
		} else {
			// Create new tax applied record
			taxAppliedRecord := &dto.CreateTaxAppliedRequest{
				TaxRateID:     taxRate.ID,
				EntityType:    types.TaxrateEntityTypeInvoice,
				EntityID:      inv.ID,
				TaxableAmount: taxableAmount,
				TaxAmount:     taxAmount,
				Currency:      inv.Currency,
			}

			// Convert to domain model and set idempotency key
			taxApplied := taxAppliedRecord.ToTaxApplied(ctx)
			taxApplied.IdempotencyKey = &idempotencyKey

			// Create the tax applied record
			if err := s.TaxAppliedRepo.Create(ctx, taxApplied); err != nil {
				s.Logger.Errorw("failed to create tax applied record",
					"error", err,
					"tax_rate_id", taxRate.ID,
					"invoice_id", inv.ID,
					"idempotency_key", idempotencyKey)
				return nil, err
			}

			taxAppliedResponse = &dto.TaxAppliedResponse{TaxApplied: *taxApplied}

			s.Logger.Infow("created new tax applied record",
				"tax_applied_id", taxApplied.ID,
				"tax_rate_id", taxRate.ID,
				"tax_rate_code", taxRate.Code,
				"tax_amount", taxAmount,
				"taxable_amount", taxableAmount,
				"invoice_id", inv.ID,
			)
		}

		taxAppliedRecords = append(taxAppliedRecords, taxAppliedResponse)
	}

	s.Logger.Infow("successfully calculated taxes for invoice",
		"invoice_id", inv.ID,
		"total_tax", totalTaxAmount,
		"tax_rates_processed", len(taxRates))

	return &TaxCalculationResult{
		TotalTaxAmount:    totalTaxAmount,
		TaxAppliedRecords: taxAppliedRecords,
		TaxRates:          taxRates,
	}, nil
}
