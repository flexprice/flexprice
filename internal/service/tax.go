package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
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
	LinkTaxRatesToEntity(ctx context.Context, req dto.LinkTaxRateToEntityRequest) error

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

	// Set tax rate status to active by default
	taxRate.TaxRateStatus = types.TaxRateStatusActive

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
		Pagination: pagination,
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
	taxAssociationFilter := types.NewTaxAssociationFilter()
	taxAssociationFilter.TaxRateIDs = []string{id}
	taxAssociationFilter.Limit = lo.ToPtr(1)
	taxAssociations, err := s.TaxAssociationRepo.List(ctx, taxAssociationFilter)
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

	// also check if the tax rate is being used in any tax applied records
	taxAppliedFilter := types.NewTaxAppliedFilter()
	taxAppliedFilter.TaxRateIDs = []string{id}
	taxAppliedFilter.Limit = lo.ToPtr(1)
	taxAppliedRecords, err := s.TaxAppliedRepo.List(ctx, taxAppliedFilter)
	if err != nil {
		s.Logger.Errorw("failed to get tax applied records for tax rate",
			"error", err,
			"tax_rate_id", id,
		)
		return nil, err
	}

	if len(taxAppliedRecords) > 0 {
		s.Logger.Warnw("tax rate is being used in tax applied records, cannot update",
			"tax_rate_id", id,
		)
		return nil, ierr.NewError("tax rate is being used in tax applied records, cannot update").
			WithHint("Tax rate is being used in tax applied records, cannot update").
			Mark(ierr.ErrValidation)
	}

	// Get the existing tax rate
	taxRate, err := s.TaxRateRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

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

	if len(req.Metadata) > 0 {
		taxRate.Metadata = req.Metadata
	}

	if req.TaxRateStatus != nil {
		taxRate.TaxRateStatus = lo.FromPtr(req.TaxRateStatus)
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

		// TODO: check if the invoice is a one time invoice, if so, skip tax recalculation
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

	// validate tax rate exists and is valid
	taxRate, err := s.TaxRateRepo.GetByCode(ctx, req.TaxRateCode)
	if err != nil {
		return nil, err
	}

	if taxRate.TaxRateStatus != types.TaxRateStatusActive {
		return nil, ierr.NewError("tax rate is not active").
			WithHint("Tax rate is not active").
			Mark(ierr.ErrValidation)
	}

	// Convert request to domain model
	tc := req.ToTaxAssociation(ctx, taxRate.ID)

	s.Logger.Infow("creating tax association",
		"tax_rate_id", tc.TaxRateID,
		"entity_type", tc.EntityType,
		"entity_id", tc.EntityID,
		"priority", tc.Priority,
		"auto_apply", tc.AutoApply)

	// Create tax config
	err = s.TaxAssociationRepo.Create(ctx, tc)
	if err != nil {
		s.Logger.Errorw("failed to create tax association",
			"error", err,
			"tax_rate_id", tc.TaxRateID,
			"entity_type", tc.EntityType,
			"entity_id", tc.EntityID)
		return nil, err
	}

	s.Logger.Infow("tax association created successfully",
		"tax_config_id", tc.ID,
		"tax_rate_id", tc.TaxRateID,
		"entity_type", tc.EntityType,
		"entity_id", tc.EntityID)

	return dto.ToTaxAssociationResponse(tc), nil
}

// GetTaxAssociation retrieves a tax association by ID
func (s *taxService) GetTaxAssociation(ctx context.Context, id string) (*dto.TaxAssociationResponse, error) {
	if id == "" {
		return nil, ierr.NewError("tax association ID is required").
			WithHint("Tax association ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	s.Logger.Debugw("getting tax association", "tax_association_id", id)

	tc, err := s.TaxAssociationRepo.Get(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to get tax association",
			"error", err,
			"tax_association_id", id)
		return nil, err
	}

	taxRate, err := s.GetTaxRate(ctx, tc.TaxRateID)
	if err != nil {
		return nil, err
	}

	response := dto.ToTaxAssociationResponse(tc)
	response.TaxRate = taxRate

	return response, nil
}

// UpdateTaxAssociation updates a tax association
func (s *taxService) UpdateTaxAssociation(ctx context.Context, id string, req *dto.TaxAssociationUpdateRequest) (*dto.TaxAssociationResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if id == "" {
		return nil, ierr.NewError("tax association ID is required").
			WithHint("Tax association ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Get existing tax association to ensure it exists
	existing, err := s.TaxAssociationRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields if provided
	if req.Priority != nil {
		existing.Priority = lo.FromPtr(req.Priority)
	}

	if req.AutoApply != nil {
		existing.AutoApply = lo.FromPtr(req.AutoApply)
	}

	if req.Metadata != nil {
		existing.Metadata = lo.FromPtr(req.Metadata)
	}

	s.Logger.Infow("updating tax association",
		"tax_association_id", id,
		"tax_rate_id", existing.TaxRateID,
		"entity_type", existing.EntityType,
		"entity_id", existing.EntityID)

	// Update tax config
	err = s.TaxAssociationRepo.Update(ctx, existing)
	if err != nil {
		s.Logger.Errorw("failed to update tax association",
			"error", err,
			"tax_association_id", id)
		return nil, err
	}

	s.Logger.Infow("tax association updated successfully",
		"tax_association_id", id,
		"tax_rate_id", existing.TaxRateID,
		"entity_type", existing.EntityType,
		"entity_id", existing.EntityID)

	return dto.ToTaxAssociationResponse(existing), nil
}

// DeleteTaxAssociation deletes a tax association
func (s *taxService) DeleteTaxAssociation(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("tax association ID is required").
			WithHint("Tax association ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Get existing tax association to ensure it exists
	existing, err := s.TaxAssociationRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Delete tax association
	err = s.TaxAssociationRepo.Delete(ctx, existing)
	if err != nil {
		s.Logger.Errorw("failed to delete tax association",
			"error", err,
			"tax_association_id", id)
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

	s.Logger.Debugw("listing tax associations",
		"entity_type", filter.EntityType,
		"entity_id", filter.EntityID)

	// List tax associations
	taxAssociations, err := s.TaxAssociationRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to list tax associations",
			"error", err,
			"filter", filter)
		return nil, err
	}

	// Get total count for pagination
	total, err := s.TaxAssociationRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &dto.ListTaxAssociationsResponse{
		Items: make([]*dto.TaxAssociationResponse, len(taxAssociations)),
	}

	for i, tc := range taxAssociations {
		response.Items[i] = dto.ToTaxAssociationResponse(tc)

	}

	response.Pagination = types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset())

	return response, nil
}

// LinkTaxRatesToEntity links tax rates to any entity in a single transaction
// It is only used while linking tax rates to an entity during creation
// It is not used while updating an entity
func (s *taxService) LinkTaxRatesToEntity(ctx context.Context, req dto.LinkTaxRateToEntityRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}

	entityType := req.EntityType
	entityID := req.EntityID
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if len(req.TaxRateOverrides) > 0 {
			// Validate all overrides first
			for _, taxOverride := range req.TaxRateOverrides {
				if err := taxOverride.Validate(); err != nil {
					return err
				}
			}

			// Create tax associations from overrides
			for _, taxOverride := range req.TaxRateOverrides {
				taxAssociationReq := taxOverride.ToTaxAssociationRequest(ctx, entityID, entityType)

				s.Logger.Infow("creating tax association from override",
					"taxrate_code", taxOverride.TaxRateCode,
					"entity_type", entityType,
					"entity_id", entityID,
					"priority", taxOverride.Priority,
					"auto_apply", taxOverride.AutoApply,
				)

				if _, err := s.CreateTaxAssociation(ctx, taxAssociationReq); err != nil {
					return err
				}
			}

			s.Logger.Infow("successfully created tax associations from overrides",
				"entity_type", entityType,
				"entity_id", entityID,
				"associations_count", len(req.TaxRateOverrides))
		}

		if len(req.ExistingTaxAssociations) > 0 {
			for _, taxAssociation := range req.ExistingTaxAssociations {
				// Get the tax rate to get its code
				taxRate, err := s.GetTaxRate(ctx, taxAssociation.TaxRateID)
				if err != nil {
					s.Logger.Errorw("failed to get tax rate for association",
						"error", err,
						"tax_rate_id", taxAssociation.TaxRateID)
					return err
				}

				// Create tax association request for the target entity
				taxAssociationReq := &dto.CreateTaxAssociationRequest{
					TaxRateCode: taxRate.Code,
					EntityType:  entityType,
					EntityID:    entityID,
					Priority:    taxAssociation.Priority,
					AutoApply:   taxAssociation.AutoApply,
					Currency:    taxAssociation.Currency,
					Metadata:    taxAssociation.Metadata,
				}

				s.Logger.Infow("creating tax association",
					"taxrate_code", taxRate.Code,
					"entity_type", entityType,
					"entity_id", entityID,
					"priority", taxAssociation.Priority,
					"auto_apply", taxAssociation.AutoApply,
				)

				if _, err := s.CreateTaxAssociation(ctx, taxAssociationReq); err != nil {
					s.Logger.Errorw("failed to create tax association",
						"error", err,
						"taxrate_code", taxRate.Code)
					return err
				}
			}

		}

		return nil
	})
}

// PrepareTaxRatesForInvoice prepares tax rates for an invoice based on the request
// This method handles both tax rate overrides and subscription tax rates
func (s *taxService) PrepareTaxRatesForInvoice(ctx context.Context, req dto.CreateInvoiceRequest) ([]*dto.TaxRateResponse, error) {
	if len(req.TaxRateOverrides) > 0 {
		s.Logger.Infow("processing tax rate overrides for invoice",
			"overrides_count", len(req.TaxRateOverrides))

		taxRateCodes := make([]string, len(req.TaxRateOverrides))
		for i, override := range req.TaxRateOverrides {
			taxRateCodes[i] = override.TaxRateCode
		}

		filter := types.NewNoLimitTaxRateFilter()
		filter.TaxRateCodes = taxRateCodes

		taxRatesResponse, err := s.ListTaxRates(ctx, filter)
		if err != nil {
			s.Logger.Errorw("failed to resolve tax rates from overrides",
				"error", err,
				"tax_rate_codes", taxRateCodes)
			return nil, err
		}

		return taxRatesResponse.Items, nil
	}

	if req.SubscriptionID != nil {
		filter := types.NewNoLimitTaxAssociationFilter()
		filter.EntityType = types.TaxrateEntityTypeSubscription
		filter.EntityID = lo.FromPtr(req.SubscriptionID)
		filter.AutoApply = lo.ToPtr(true)

		taxAssociations, err := s.ListTaxAssociations(ctx, filter)
		if err != nil {
			s.Logger.Errorw("failed to get tax associations for subscription",
				"error", err,
				"subscription_id", lo.FromPtr(req.SubscriptionID),
			)
			return nil, err
		}

		if len(taxAssociations.Items) == 0 {
			return []*dto.TaxRateResponse{}, nil
		}

		// Get tax rates for the associations
		taxRateIDs := make([]string, len(taxAssociations.Items))
		for i, association := range taxAssociations.Items {
			taxRateIDs[i] = association.TaxRateID
		}

		taxRateFilter := types.NewNoLimitTaxRateFilter()
		taxRateFilter.TaxRateIDs = taxRateIDs

		taxRatesResponse, err := s.ListTaxRates(ctx, taxRateFilter)
		if err != nil {
			s.Logger.Errorw("failed to fetch subscription tax rates",
				"error", err,
				"subscription_id", lo.FromPtr(req.SubscriptionID),
				"tax_rate_ids", taxRateIDs)
			return nil, err
		}

		return taxRatesResponse.Items, nil
	}

	return []*dto.TaxRateResponse{}, nil
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
		return s.createEmptyTaxCalculationResult(taxRates), nil
	}

	s.Logger.Infow("applying taxes to invoice",
		"invoice_id", inv.ID,
		"tax_rates_count", len(taxRates))

	taxableAmount := inv.Subtotal
	totalTaxAmount := decimal.Zero
	taxAppliedRecords := make([]*dto.TaxAppliedResponse, 0, len(taxRates))

	// Process each tax rate
	for _, taxRate := range taxRates {
		taxAmount := s.calculateTaxAmount(taxRate, taxableAmount)
		if taxAmount == nil {
			continue // Skip invalid tax rate types
		}

		totalTaxAmount = totalTaxAmount.Add(*taxAmount)
		taxAppliedRecord, err := s.processTaxApplication(ctx, inv, taxRate, taxableAmount, *taxAmount)
		if err != nil {
			return nil, err
		}

		taxAppliedRecords = append(taxAppliedRecords, taxAppliedRecord)
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

// createEmptyTaxCalculationResult creates an empty tax calculation result
func (s *taxService) createEmptyTaxCalculationResult(taxRates []*dto.TaxRateResponse) *TaxCalculationResult {
	return &TaxCalculationResult{
		TotalTaxAmount:    decimal.Zero,
		TaxAppliedRecords: []*dto.TaxAppliedResponse{},
		TaxRates:          taxRates,
	}
}

// calculateTaxAmount calculates the tax amount for a given tax rate and taxable amount
func (s *taxService) calculateTaxAmount(taxRate *dto.TaxRateResponse, taxableAmount decimal.Decimal) *decimal.Decimal {
	var taxAmount decimal.Decimal

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
		return nil
	}

	return &taxAmount
}

// processTaxApplication handles the creation or update of tax applied records
func (s *taxService) processTaxApplication(ctx context.Context, inv *invoice.Invoice, taxRate *dto.TaxRateResponse, taxableAmount, taxAmount decimal.Decimal) (*dto.TaxAppliedResponse, error) {
	idempGen := idempotency.NewGenerator()
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

	if existingTaxApplied != nil {
		existingTaxApplied.TaxableAmount = taxableAmount
		existingTaxApplied.TaxAmount = taxAmount
		existingTaxApplied.AppliedAt = time.Now().UTC()

		if err := s.TaxAppliedRepo.Update(ctx, existingTaxApplied); err != nil {
			s.Logger.Errorw("failed to update existing tax applied record",
				"error", err,
				"tax_applied_id", existingTaxApplied.ID,
				"tax_rate_id", taxRate.ID)
			return nil, err
		}

		s.Logger.Infow("updated existing tax applied record",
			"tax_applied_id", existingTaxApplied.ID,
			"tax_rate_id", taxRate.ID,
			"tax_rate_code", taxRate.Code,
			"tax_amount", taxAmount,
			"taxable_amount", taxableAmount)

		return &dto.TaxAppliedResponse{TaxApplied: *existingTaxApplied}, nil
	}

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
	taxApplied.AppliedAt = time.Now().UTC()

	// Create the tax applied record
	if err := s.TaxAppliedRepo.Create(ctx, taxApplied); err != nil {
		s.Logger.Errorw("failed to create tax applied record",
			"error", err,
			"tax_rate_id", taxRate.ID,
			"invoice_id", inv.ID,
			"idempotency_key", idempotencyKey)
		return nil, err
	}

	s.Logger.Infow("created new tax applied record",
		"tax_applied_id", taxApplied.ID,
		"tax_rate_id", taxRate.ID,
		"tax_rate_code", taxRate.Code,
		"tax_amount", taxAmount,
		"taxable_amount", taxableAmount)

	return &dto.TaxAppliedResponse{TaxApplied: *taxApplied}, nil
}
