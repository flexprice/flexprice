package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
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
	PrepareTaxRatesForInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceTaxRates, error)
	ApplyTaxesOnInvoice(ctx context.Context, inv *invoice.Invoice, taxRates *dto.InvoiceTaxRates) (*TaxCalculationResult, error)
	CalculateTaxesOnInvoice(ctx context.Context, inv *invoice.Invoice, taxRates *dto.InvoiceTaxRates) *TaxCalculationResult
}

type taxService struct {
	ServiceParams
}

// NewTaxService creates a new instance of TaxService
func NewTaxService(params ServiceParams) TaxService {
	return &taxService{
		ServiceParams: params,
	}
}

// CreateTaxRate creates a new tax rate
func (s *taxService) CreateTaxRate(ctx context.Context, req dto.CreateTaxRateRequest) (*dto.TaxRateResponse, error) {
	// Validate the request
	if err := req.Validate(); err != nil {
		s.Logger.Info(ctx, "tax rate creation validation failed",
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
		s.Logger.Error(ctx, "failed to create tax rate",
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
		s.Logger.Info(ctx, "failed to get tax rate",
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
		s.Logger.Error(ctx, "failed to list tax rates",
			"error", err,
			"filter", filter,
		)
		return nil, err
	}

	// Get the total count of tax rates
	count, err := s.TaxRateRepo.Count(ctx, filter)
	if err != nil {
		s.Logger.Error(ctx, "failed to count tax rates",
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
		s.Logger.Info(ctx, "tax rate update validation failed",
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
		s.Logger.Error(ctx, "failed to get tax associations for tax rate",
			"error", err,
			"tax_rate_id", id,
		)
		return nil, err
	}

	if len(taxAssociations) > 0 {
		s.Logger.Info(ctx, "tax rate is being used in tax assignments, cannot update",
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
		s.Logger.Error(ctx, "failed to get tax applied records for tax rate",
			"error", err,
			"tax_rate_id", id,
		)
		return nil, err
	}

	if len(taxAppliedRecords) > 0 {
		s.Logger.Info(ctx, "tax rate is being used in tax applied records, cannot update",
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
		s.Logger.Error(ctx, "failed to update tax rate",
			"error", err,
			"tax_rate_id", id,
		)
		return nil, err
	}

	s.Logger.Info(ctx, "tax rate updated successfully",
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
		s.Logger.Info(ctx, "failed to get tax rate for deletion",
			"error", err,
			"tax_rate_id", id,
		)
		return err
	}

	// Block delete when the tax rate has any active association
	taxAssociationFilter := types.NewTaxAssociationFilter()
	taxAssociationFilter.TaxRateIDs = []string{id}
	taxAssociationFilter.Status = lo.ToPtr(types.StatusPublished)
	taxAssociationFilter.Limit = lo.ToPtr(1)
	taxAssociations, err := s.TaxAssociationRepo.List(ctx, taxAssociationFilter)
	if err != nil {
		s.Logger.Error(ctx, "failed to get tax associations for tax rate",
			"error", err,
			"tax_rate_id", id,
		)
		return err
	}

	if len(taxAssociations) > 0 {
		s.Logger.Info(ctx, "tax rate has active associations, cannot delete",
			"tax_rate_id", id,
		)
		return ierr.NewError("tax rate has active associations, cannot delete").
			WithHint("This tax rate has active associations. Please remove all associations before deleting it.").
			Mark(ierr.ErrValidation)
	}

	// Call the repository's Delete method which handles archiving
	if err := s.TaxRateRepo.Delete(ctx, taxRate); err != nil {
		s.Logger.Error(ctx, "failed to delete tax rate",
			"error", err,
			"tax_rate_id", id,
		)
		return err
	}

	s.Logger.Info(ctx, "tax rate deleted successfully",
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
		s.Logger.Info(ctx, "failed to get tax rate by code",
			"error", err,
			"code", code,
		)
		return nil, err
	}

	// Return the tax rate
	return &dto.TaxRateResponse{TaxRate: taxRate}, nil
}

// CreateTaxApplied creates a new tax applied record
func (s *taxService) CreateTaxApplied(ctx context.Context, req dto.CreateTaxAppliedRequest) (*dto.TaxAppliedResponse, error) {
	// Validate the request
	if err := req.Validate(); err != nil {
		s.Logger.Info(ctx, "tax applied creation validation failed",
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
		s.Logger.Error(ctx, "failed to create tax applied record",
			"error", err,
			"tax_applied_id", taxApplied.ID,
			"tax_rate_id", taxApplied.TaxRateID,
			"entity_type", taxApplied.EntityType,
			"entity_id", taxApplied.EntityID,
		)
		return nil, err
	}

	s.Logger.Info(ctx, "tax applied record created successfully",
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
		s.Logger.Info(ctx, "failed to get tax applied record",
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
		s.Logger.Info(ctx, "tax applied filter validation failed",
			"error", err,
			"filter", filter,
		)
		return nil, err
	}

	// Get tax applied records from the repository
	taxAppliedRecords, err := s.TaxAppliedRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Error(ctx, "failed to list tax applied records",
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

	// Fetch tax rates if requested
	if filter.GetExpand().Has(types.ExpandTaxRate) {
		taxRateIDs := lo.Map(taxAppliedRecords, func(ta *taxapplied.TaxApplied, _ int) string {
			return ta.TaxRateID
		})

		taxRateFilter := types.NewNoLimitTaxRateFilter()
		taxRateFilter.TaxRateIDs = taxRateIDs

		taxRatesResponse, err := s.ListTaxRates(ctx, taxRateFilter)
		if err != nil {
			s.Logger.Error(ctx, "failed to list tax rates for expansion",
				"error", err,
				"tax_rate_ids", taxRateIDs)
			return nil, err
		}

		// Create a map for quick lookup
		taxRatesByID := make(map[string]*dto.TaxRateResponse)
		for _, taxRate := range taxRatesResponse.Items {
			taxRatesByID[taxRate.ID] = taxRate
		}

		// Assign tax rates to the appropriate tax applied records
		for i, ta := range taxAppliedRecords {
			if taxRate, exists := taxRatesByID[ta.TaxRateID]; exists {
				items[i].TaxRate = taxRate
			}
		}
	}

	// Get the total count of tax applied records
	count, err := s.TaxAppliedRepo.Count(ctx, filter)
	if err != nil {
		s.Logger.Error(ctx, "failed to count tax applied records",
			"error", err,
			"filter", filter,
		)
		return nil, err
	}

	// Return the response with pagination
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
		s.Logger.Info(ctx, "failed to get tax applied record for deletion",
			"error", err,
			"tax_applied_id", id,
		)
		return err
	}

	// Delete the tax applied record
	if err := s.TaxAppliedRepo.Delete(ctx, id); err != nil {
		s.Logger.Error(ctx, "failed to delete tax applied record",
			"error", err,
			"tax_applied_id", id,
		)
		return err
	}

	s.Logger.Info(ctx, "tax applied record deleted successfully",
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

	// Resolve external_customer_id if provided
	if req.ExternalCustomerID != "" {
		customer, err := s.CustomerRepo.GetByLookupKey(ctx, req.ExternalCustomerID)
		if err != nil {
			s.Logger.Error(ctx, "failed to resolve external customer ID",
				"error", err,
				"external_customer_id", req.ExternalCustomerID)
			return nil, ierr.WithError(err).
				WithHintf("Customer with external ID '%s' not found", req.ExternalCustomerID).
				WithReportableDetails(map[string]interface{}{
					"external_customer_id": req.ExternalCustomerID,
				}).
				Mark(ierr.ErrNotFound)
		}

		// If both entity_id and external_customer_id are provided, validate they match
		if req.EntityID != "" && req.EntityID != customer.ID {
			s.Logger.Info(ctx, "entity_id and external_customer_id point to different customers",
				"entity_id", req.EntityID,
				"external_customer_id", req.ExternalCustomerID,
				"resolved_customer_id", customer.ID)
			return nil, ierr.NewError("entity_id and external_customer_id point to different customers").
				WithHint("When both entity_id and external_customer_id are provided, they must point to the same customer").
				WithReportableDetails(map[string]interface{}{
					"entity_id":            req.EntityID,
					"external_customer_id": req.ExternalCustomerID,
					"resolved_customer_id": customer.ID,
				}).
				Mark(ierr.ErrValidation)
		}

		// Set entity_type to customer and entity_id to resolved customer ID
		req.EntityType = types.TaxRateEntityTypeCustomer
		req.EntityID = customer.ID

		s.Logger.Debug(ctx, "resolved external customer ID to internal customer ID",
			"external_customer_id", req.ExternalCustomerID,
			"customer_id", customer.ID)
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

	// Only a subscription-level association needs anything looked up: the subscription for its
	// currency, and its customer for the exemption check. The other three entity types need
	// neither and must not pay for the fetches.
	var sub *subscription.Subscription
	if req.EntityType == types.TaxRateEntityTypeSubscription && req.EntityID != "" {
		sub, err = s.SubRepo.Get(ctx, req.EntityID)
		if err != nil {
			return nil, err
		}

		cust, err := s.CustomerRepo.Get(ctx, sub.CustomerID)
		if err != nil {
			return nil, err
		}

		// An exempt customer is never taxed, so a subscription-level association would be
		// dead configuration. Skip it rather than fail so subscription creation still
		// succeeds, just with zero tax associations.
		if cust.TaxTreatment == types.TaxTreatmentExempt {
			s.Logger.Info(ctx, "skipping subscription tax association — exempt customer",
				"subscription_id", sub.ID,
				"customer_id", cust.ID,
				"tax_rate_id", taxRate.ID)
			return nil, nil
		}
	}

	tc.TaxBehavior, err = s.resolveEffectiveTaxBehavior(ctx, req, taxRate, sub)
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "creating tax association",
		"tax_rate_id", tc.TaxRateID,
		"entity_type", tc.EntityType,
		"entity_id", tc.EntityID,
		"priority", tc.Priority,
		"auto_apply", tc.AutoApply)

	// Create tax config
	err = s.TaxAssociationRepo.Create(ctx, tc)
	if err != nil {
		s.Logger.Error(ctx, "failed to create tax association",
			"error", err,
			"tax_rate_id", tc.TaxRateID,
			"entity_type", tc.EntityType,
			"entity_id", tc.EntityID)
		return nil, err
	}

	s.Logger.Info(ctx, "tax association created successfully",
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

	s.Logger.Debug(ctx, "getting tax association", "tax_association_id", id)

	tc, err := s.TaxAssociationRepo.Get(ctx, id)
	if err != nil {
		s.Logger.Error(ctx, "failed to get tax association",
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

	if req.TaxBehavior != nil {
		existing.TaxBehavior = req.TaxBehavior
	}

	s.Logger.Info(ctx, "updating tax association",
		"tax_association_id", id,
		"tax_rate_id", existing.TaxRateID,
		"entity_type", existing.EntityType,
		"entity_id", existing.EntityID)

	// Update tax config
	err = s.TaxAssociationRepo.Update(ctx, existing)
	if err != nil {
		s.Logger.Error(ctx, "failed to update tax association",
			"error", err,
			"tax_association_id", id)
		return nil, err
	}

	s.Logger.Info(ctx, "tax association updated successfully",
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
		s.Logger.Error(ctx, "failed to delete tax association",
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

	// Resolve external_customer_id if provided
	if filter.ExternalCustomerID != "" {
		customer, err := s.CustomerRepo.GetByLookupKey(ctx, filter.ExternalCustomerID)
		if err != nil {
			s.Logger.Error(ctx, "failed to resolve external customer ID",
				"error", err,
				"external_customer_id", filter.ExternalCustomerID)
			return nil, ierr.WithError(err).
				WithHintf("Customer with external ID '%s' not found", filter.ExternalCustomerID).
				WithReportableDetails(map[string]interface{}{
					"external_customer_id": filter.ExternalCustomerID,
				}).
				Mark(ierr.ErrNotFound)
		}

		// If both entity_id and external_customer_id are provided, validate they match
		if filter.EntityID != "" && filter.EntityID != customer.ID {
			s.Logger.Info(ctx, "entity_id and external_customer_id point to different customers",
				"entity_id", filter.EntityID,
				"external_customer_id", filter.ExternalCustomerID,
				"resolved_customer_id", customer.ID)
			return nil, ierr.NewError("entity_id and external_customer_id point to different customers").
				WithHint("When both entity_id and external_customer_id are provided, they must point to the same customer").
				WithReportableDetails(map[string]interface{}{
					"entity_id":            filter.EntityID,
					"external_customer_id": filter.ExternalCustomerID,
					"resolved_customer_id": customer.ID,
				}).
				Mark(ierr.ErrValidation)
		}

		// Set entity_type to customer and entity_id to resolved customer ID
		filter.EntityType = types.TaxRateEntityTypeCustomer
		filter.EntityID = customer.ID

		s.Logger.Debug(ctx, "resolved external customer ID to internal customer ID",
			"external_customer_id", filter.ExternalCustomerID,
			"customer_id", customer.ID)
	}

	s.Logger.Debug(ctx, "listing tax associations",
		"entity_type", filter.EntityType,
		"entity_id", filter.EntityID)

	// List tax associations
	taxAssociations, err := s.TaxAssociationRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Error(ctx, "failed to list tax associations",
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

	// Initialize response items
	for i, tc := range taxAssociations {
		response.Items[i] = dto.ToTaxAssociationResponse(tc)
	}

	// Fetch tax rates if requested
	if filter.GetExpand().Has(types.ExpandTaxRate) {
		taxRateIDs := lo.Map(taxAssociations, func(tc *taxassociation.TaxAssociation, _ int) string {
			return tc.TaxRateID
		})

		taxRateFilter := types.NewNoLimitTaxRateFilter()
		taxRateFilter.TaxRateIDs = taxRateIDs

		taxRatesResponse, err := s.ListTaxRates(ctx, taxRateFilter)
		if err != nil {
			s.Logger.Error(ctx, "failed to list tax rates",
				"error", err,
				"tax_rate_ids", taxRateIDs)
			return nil, err
		}

		// Create a map for quick lookup
		taxRatesByID := make(map[string]*dto.TaxRateResponse)
		for _, taxRate := range taxRatesResponse.Items {
			taxRatesByID[taxRate.ID] = taxRate
		}

		// Assign tax rates to the appropriate associations
		for i, tc := range taxAssociations {
			if taxRate, exists := taxRatesByID[tc.TaxRateID]; exists {
				response.Items[i].TaxRate = taxRate
			}
		}
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

				s.Logger.Info(ctx, "creating tax association from override",
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

			s.Logger.Info(ctx, "successfully created tax associations from overrides",
				"entity_type", entityType,
				"entity_id", entityID,
				"associations_count", len(req.TaxRateOverrides))
		}

		if len(req.ExistingTaxAssociations) > 0 {
			for _, taxAssociation := range req.ExistingTaxAssociations {
				// Get the tax rate to get its code
				taxRate, err := s.GetTaxRate(ctx, taxAssociation.TaxRateID)
				if err != nil {
					s.Logger.Error(ctx, "failed to get tax rate for association",
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
					TaxBehavior: taxAssociation.TaxBehavior,
				}

				s.Logger.Info(ctx, "creating tax association",
					"taxrate_code", taxRate.Code,
					"entity_type", entityType,
					"entity_id", entityID,
					"priority", taxAssociation.Priority,
					"auto_apply", taxAssociation.AutoApply,
				)

				if _, err := s.CreateTaxAssociation(ctx, taxAssociationReq); err != nil {
					s.Logger.Error(ctx, "failed to create tax association",
						"error", err,
						"taxrate_code", taxRate.Code)
					return err
				}
			}

		}

		return nil
	})
}

// PrepareTaxRatesForInvoice resolves everything invoice tax computation needs about this
// customer: which rates apply, what behavior each carries, and whether the customer is
// exempt. Rate overrides win over raw tax_rate IDs, which win over the subscription's own
// associations. Exemption is resolved here, alongside the rates, so no caller has to pair
// the two up itself.
func (s *taxService) PrepareTaxRatesForInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceTaxRates, error) {
	// Read once and reused by every branch below, so the rates and the exemption flag always
	// come from the same moment.
	cust, err := s.CustomerRepo.Get(ctx, req.CustomerID)
	if err != nil {
		return nil, err
	}

	if len(req.TaxRateOverrides) > 0 {
		s.Logger.Info(ctx, "processing tax rate overrides for invoice",
			"overrides_count", len(req.TaxRateOverrides))

		taxRateCodes := make([]string, len(req.TaxRateOverrides))
		behaviorByCode := make(map[string]types.TaxBehavior, len(req.TaxRateOverrides))
		for i, override := range req.TaxRateOverrides {
			taxRateCodes[i] = override.TaxRateCode
			if override.TaxBehavior != nil {
				behaviorByCode[override.TaxRateCode] = *override.TaxBehavior
			} else {
				behaviorByCode[override.TaxRateCode] = types.DefaultTaxBehaviorForCurrency(override.Currency)
			}
		}

		filter := types.NewNoLimitTaxRateFilter()
		filter.TaxRateCodes = taxRateCodes

		taxRatesResponse, err := s.ListTaxRates(ctx, filter)
		if err != nil {
			s.Logger.Error(ctx, "failed to resolve tax rates from overrides",
				"error", err,
				"tax_rate_codes", taxRateCodes)
			return nil, err
		}

		resolved := make([]*dto.TaxRateWithBehavior, len(taxRatesResponse.Items))
		for i, tr := range taxRatesResponse.Items {
			resolved[i] = &dto.TaxRateWithBehavior{TaxRateResponse: tr, TaxBehavior: behaviorByCode[tr.Code]}
		}
		return dto.NewInvoiceTaxRates(resolved, cust), nil
	}

	if len(req.TaxRates) > 0 {
		// Raw rate IDs carry no association and no behavior, so only the invoice currency is
		// left to resolve against. Known gap: the tenant/customer hierarchy is not consulted
		// here — pass tax_rate_overrides instead, which take an explicit behavior.
		behavior := types.DefaultTaxBehaviorForCurrency(req.Currency)
		resolved := make([]*dto.TaxRateWithBehavior, 0, len(req.TaxRates))
		for _, taxRateID := range req.TaxRates {
			taxRate, err := s.GetTaxRate(ctx, taxRateID)
			if err != nil {
				return nil, err
			}
			resolved = append(resolved, &dto.TaxRateWithBehavior{TaxRateResponse: taxRate, TaxBehavior: behavior})
		}
		return dto.NewInvoiceTaxRates(resolved, cust), nil
	}

	if req.SubscriptionID != nil {
		filter := types.NewNoLimitTaxAssociationFilter()
		filter.EntityType = types.TaxRateEntityTypeSubscription
		filter.EntityID = lo.FromPtr(req.SubscriptionID)
		filter.AutoApply = lo.ToPtr(true)
		if req.PeriodStart != nil {
			filter.StartDate = lo.ToPtr(req.PeriodStart.UTC())
		}
		if req.PeriodEnd != nil {
			filter.EndDate = lo.ToPtr(req.PeriodEnd.UTC())
		}

		taxAssociations, err := s.ListTaxAssociations(ctx, filter)
		if err != nil {
			s.Logger.Error(ctx, "failed to get tax associations for subscription",
				"error", err,
				"subscription_id", lo.FromPtr(req.SubscriptionID),
			)
			return nil, err
		}

		if len(taxAssociations.Items) == 0 {
			return dto.NewInvoiceTaxRates(nil, cust), nil
		}

		// Keep each association's behavior paired with its rate: a bare rate ID list would
		// lose it as soon as two associations share a rate.
		taxRateIDs := make([]string, len(taxAssociations.Items))
		behaviorByRateID := make(map[string]types.TaxBehavior, len(taxAssociations.Items))
		for i, association := range taxAssociations.Items {
			taxRateIDs[i] = association.TaxRateID
			behavior := lo.FromPtr(association.TaxBehavior)
			if behavior == "" {
				// Creation always stamps one, so a null here should not happen. Fall back to
				// the same currency default every other unstamped resolution uses.
				behavior = types.DefaultTaxBehaviorForCurrency(req.Currency)
				s.Logger.Error(ctx, "subscription tax association missing tax_behavior, defaulting from currency",
					"error", "tax_behavior is null on a subscription-level association",
					"tax_association_id", association.ID,
					"tax_rate_id", association.TaxRateID,
					"currency", req.Currency,
					"resolved_behavior", behavior)
			}
			behaviorByRateID[association.TaxRateID] = behavior
		}

		taxRateFilter := types.NewNoLimitTaxRateFilter()
		taxRateFilter.TaxRateIDs = taxRateIDs

		taxRatesResponse, err := s.ListTaxRates(ctx, taxRateFilter)
		if err != nil {
			s.Logger.Error(ctx, "failed to fetch subscription tax rates",
				"error", err,
				"subscription_id", lo.FromPtr(req.SubscriptionID),
				"tax_rate_ids", taxRateIDs)
			return nil, err
		}

		resolved := make([]*dto.TaxRateWithBehavior, len(taxRatesResponse.Items))
		for i, tr := range taxRatesResponse.Items {
			resolved[i] = &dto.TaxRateWithBehavior{TaxRateResponse: tr, TaxBehavior: behaviorByRateID[tr.ID]}
		}
		return dto.NewInvoiceTaxRates(resolved, cust), nil
	}

	return dto.NewInvoiceTaxRates(nil, cust), nil
}

// TaxCalculationResult represents the result of tax calculations
type TaxCalculationResult struct {
	// InclusiveTax is the tax already contained in the taxable amount, recovered by working
	// backwards from it. Because it is already inside the subtotal it is never added to the
	// invoice total; it exists to report how much of the listed price was tax. Computed the
	// same way whether or not the customer is exempt.
	InclusiveTax decimal.Decimal

	// ExclusiveTax is added on top of the taxable amount — the only tax that moves the total.
	// Computed the same way whether or not the customer is exempt.
	ExclusiveTax decimal.Decimal

	// TotalTaxAmount is what is actually charged: InclusiveTax + ExclusiveTax, or zero when
	// the customer is exempt. This is what lands on invoice.total_tax.
	TotalTaxAmount decimal.Decimal

	Exempt bool

	TaxAppliedRecords []*dto.TaxAppliedResponse
}

func taxableAmount(inv *invoice.Invoice) decimal.Decimal {
	amount := inv.Subtotal.Sub(inv.TotalDiscount)
	if amount.IsNegative() {
		return decimal.Zero
	}

	return amount
}

// CalculateTaxesOnInvoice computes what the resolved rates would charge and writes nothing.
// TaxAppliedRecords are built in memory, so it is safe for a preview of an invoice that will
// never exist. ApplyTaxesOnInvoice calls this and then persists them.
func (s *taxService) CalculateTaxesOnInvoice(ctx context.Context, inv *invoice.Invoice, taxRates *dto.InvoiceTaxRates) *TaxCalculationResult {
	taxableAmt := taxableAmount(inv)
	rateLines, rateByID := s.buildRateLines(ctx, inv, taxRates.GetRates())

	breakdown := calculateTaxBreakdown(taxableAmt, rateLines, inv.Currency)

	s.Logger.Info(ctx, "tax computed for invoice",
		"invoice_id", inv.ID, "taxable_amount", taxableAmt,
		"inclusive_tax", breakdown.inclusiveTax, "exclusive_tax", breakdown.exclusiveTax)

	// Tax is computed the same way for everyone; exemption only zeroes what is charged. One
	// override at the end, rather than a branch inside the maths.
	exempt := taxRates.IsExempt()
	totalTaxCharged := breakdown.inclusiveTax.Add(breakdown.exclusiveTax)
	if exempt {
		s.Logger.Info(ctx, "exemption applied at compute",
			"invoice_id", inv.ID, "customer_id", inv.CustomerID,
			"waived_inclusive_tax", breakdown.inclusiveTax, "waived_exclusive_tax", breakdown.exclusiveTax)
		totalTaxCharged = decimal.Zero
	}

	taxAppliedRecords := make([]*dto.TaxAppliedResponse, 0, len(breakdown.lines))
	for _, line := range breakdown.lines {
		chargedAmount := line.taxAmount
		if exempt {
			chargedAmount = decimal.Zero
		}
		rate := rateByID[line.rateID]

		taxAppliedRecords = append(taxAppliedRecords, &dto.TaxAppliedResponse{
			TaxApplied: taxapplied.TaxApplied{
				TaxRateID:     rate.ID,
				EntityType:    types.TaxRateEntityTypeInvoice,
				EntityID:      inv.ID,
				TaxableAmount: line.taxableAmount,
				TaxAmount:     chargedAmount,
				TaxBehavior:   line.taxBehavior,
				Currency:      inv.Currency,
				AppliedAt:     time.Now().UTC(),
			},
			TaxRate: rate.TaxRateResponse,
		})
	}

	return &TaxCalculationResult{
		InclusiveTax:      breakdown.inclusiveTax,
		ExclusiveTax:      breakdown.exclusiveTax,
		TotalTaxAmount:    totalTaxCharged,
		Exempt:            exempt,
		TaxAppliedRecords: taxAppliedRecords,
	}
}

// ApplyTaxesOnInvoice applies taxes to an invoice and creates/updates tax applied records
// This method handles idempotency by checking for existing tax applied records
// Returns calculated tax data instead of directly updating the invoice
func (s *taxService) ApplyTaxesOnInvoice(ctx context.Context, inv *invoice.Invoice, taxRates *dto.InvoiceTaxRates) (*TaxCalculationResult, error) {
	if len(taxRates.GetRates()) == 0 {
		s.Logger.Info(ctx, "no tax rates to apply to invoice", "invoice_id", inv.ID)
		return &TaxCalculationResult{
			TotalTaxAmount:    decimal.Zero,
			TaxAppliedRecords: []*dto.TaxAppliedResponse{},
		}, nil
	}

	s.Logger.Info(ctx, "applying taxes to invoice",
		"invoice_id", inv.ID,
		"tax_rates_count", len(taxRates.GetRates()))

	result := s.CalculateTaxesOnInvoice(ctx, inv, taxRates)

	rateByID := lo.SliceToMap(taxRates.GetRates(), func(r *dto.TaxRateWithBehavior) (string, *dto.TaxRateWithBehavior) {
		return r.ID, r
	})
	persisted := make([]*dto.TaxAppliedResponse, 0, len(result.TaxAppliedRecords))
	for _, record := range result.TaxAppliedRecords {
		applied, err := s.processTaxApplication(ctx, inv, rateByID[record.TaxRateID], record.TaxableAmount, record.TaxAmount)
		if err != nil {
			return nil, err
		}
		persisted = append(persisted, applied)
	}
	result.TaxAppliedRecords = persisted

	s.Logger.Info(ctx, "successfully calculated taxes for invoice",
		"invoice_id", inv.ID,
		"total_tax", result.TotalTaxAmount,
		"tax_rates_processed", len(taxRates.GetRates()))

	return result, nil
}

// processTaxApplication handles the creation or update of tax applied records
func (s *taxService) processTaxApplication(ctx context.Context, inv *invoice.Invoice, taxRate *dto.TaxRateWithBehavior, taxableAmount, taxAmount decimal.Decimal) (*dto.TaxAppliedResponse, error) {
	idempGen := idempotency.NewGenerator()
	idempotencyKey := idempGen.GenerateKey(idempotency.ScopeTaxApplication, map[string]interface{}{
		"tax_rate_id": taxRate.ID,
		"entity_id":   inv.ID,
		"entity_type": string(types.TaxRateEntityTypeInvoice),
	})

	// Check if tax applied record already exists
	existingTaxApplied, err := s.TaxAppliedRepo.GetByIdempotencyKey(ctx, idempotencyKey)
	if err != nil && !ierr.IsNotFound(err) {
		s.Logger.Error(ctx, "failed to check existing tax applied record",
			"error", err,
			"tax_rate_id", taxRate.ID,
			"invoice_id", inv.ID,
			"idempotency_key", idempotencyKey)
		return nil, err
	}

	if existingTaxApplied != nil {
		existingTaxApplied.TaxableAmount = taxableAmount
		existingTaxApplied.TaxAmount = taxAmount
		existingTaxApplied.TaxBehavior = taxRate.TaxBehavior
		existingTaxApplied.AppliedAt = time.Now().UTC()

		if err := s.TaxAppliedRepo.Update(ctx, existingTaxApplied); err != nil {
			s.Logger.Error(ctx, "failed to update existing tax applied record",
				"error", err,
				"tax_applied_id", existingTaxApplied.ID,
				"tax_rate_id", taxRate.ID)
			return nil, err
		}

		s.Logger.Info(ctx, "updated existing tax applied record",
			"tax_applied_id", existingTaxApplied.ID,
			"tax_rate_id", taxRate.ID,
			"tax_rate_code", taxRate.Code,
			"tax_amount", taxAmount,
			"taxable_amount", taxableAmount)

		return &dto.TaxAppliedResponse{TaxApplied: *existingTaxApplied}, nil
	}

	taxAppliedRecord := &dto.CreateTaxAppliedRequest{
		TaxRateID:     taxRate.ID,
		EntityType:    types.TaxRateEntityTypeInvoice,
		EntityID:      inv.ID,
		TaxableAmount: taxableAmount,
		TaxAmount:     taxAmount,
		TaxBehavior:   taxRate.TaxBehavior,
		Currency:      inv.Currency,
	}

	// Convert to domain model and set idempotency key
	taxApplied := taxAppliedRecord.ToTaxApplied(ctx)
	taxApplied.IdempotencyKey = &idempotencyKey
	taxApplied.AppliedAt = time.Now().UTC()

	// Create the tax applied record
	if err := s.TaxAppliedRepo.Create(ctx, taxApplied); err != nil {
		s.Logger.Error(ctx, "failed to create tax applied record",
			"error", err,
			"tax_rate_id", taxRate.ID,
			"invoice_id", inv.ID,
			"idempotency_key", idempotencyKey)
		return nil, err
	}

	s.Logger.Info(ctx, "created new tax applied record",
		"tax_applied_id", taxApplied.ID,
		"tax_rate_id", taxRate.ID,
		"tax_rate_code", taxRate.Code,
		"tax_amount", taxAmount,
		"taxable_amount", taxableAmount)

	return &dto.TaxAppliedResponse{TaxApplied: *taxApplied}, nil
}

// buildRateLines drops rates with no percentage_value and reshapes the rest for the calculation.
func (s *taxService) buildRateLines(ctx context.Context, inv *invoice.Invoice, taxRates []*dto.TaxRateWithBehavior) ([]taxRateLine, map[string]*dto.TaxRateWithBehavior) {
	lines := make([]taxRateLine, 0, len(taxRates))
	byID := make(map[string]*dto.TaxRateWithBehavior, len(taxRates))

	for _, rate := range taxRates {
		if rate.PercentageValue == nil {
			s.Logger.Info(ctx, "rate skipped — missing percentage_value",
				"invoice_id", inv.ID, "tax_rate_id", rate.ID)
			continue
		}

		byID[rate.ID] = rate
		lines = append(lines, taxRateLine{
			id:              rate.ID,
			percentageValue: rate.PercentageValue,
			taxBehavior:     rate.TaxBehavior,
		})
	}

	return lines, byID
}

// resolveEffectiveTaxBehavior stamps the tax_behavior a new association will carry, and rejects
// an inclusive rate above 100%. sub is the already-fetched subscription for a subscription-level
// association, and nil for tenant/customer-level requests — the caller fetches it once, for
// both this and its own exemption check, rather than this function fetching it again.
func (s *taxService) resolveEffectiveTaxBehavior(ctx context.Context, req *dto.CreateTaxAssociationRequest, taxRate *taxrate.TaxRate, sub *subscription.Subscription) (*types.TaxBehavior, error) {
	behavior := req.TaxBehavior

	// sub is nil for tenant/customer-level associations: they span several currencies, so
	// there is no single default to resolve from. They keep whatever the request gave —
	// including nothing — and are resolved when copied down to a subscription.
	if sub != nil {
		resolvedBehavior := lo.FromPtr(req.TaxBehavior)
		source := types.TaxBehaviorSourceExplicit
		if req.TaxBehavior == nil {
			resolvedBehavior = types.DefaultTaxBehaviorForCurrency(sub.Currency)
			source = types.TaxBehaviorSourceCurrencyDefault
		}

		behavior = &resolvedBehavior
		s.Logger.Info(ctx, "tax behavior resolved for subscription association",
			"subscription_id", sub.ID,
			"tax_rate_id", taxRate.ID,
			"currency", sub.Currency,
			"resolved_behavior", resolvedBehavior,
			"source", source)
	}

	// Above 100% inclusive, the tax exceeds the tax-free price it is derived from. The maths
	// still works, so this is rejected as a configuration mistake. Checked against the
	// resolved value so a row that became inclusive via the currency default is caught too.
	if behavior != nil && *behavior == types.TaxBehaviorInclusive &&
		taxRate.PercentageValue != nil && taxRate.PercentageValue.GreaterThan(decimal.NewFromInt(100)) {
		return nil, ierr.NewError("a percentage rate above 100% cannot be inclusive").
			WithHint("An inclusive tax rate above 100% would mean the tax exceeds the tax-free price it is derived from").
			WithReportableDetails(map[string]interface{}{
				"tax_rate_id":      taxRate.ID,
				"percentage_value": taxRate.PercentageValue.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	return behavior, nil
}

// taxRateLine is what calculateTaxBreakdown needs from a rate. Callers must have already
// dropped rates with no percentage_value.
type taxRateLine struct {
	id              string
	percentageValue *decimal.Decimal
	taxBehavior     types.TaxBehavior
}

// taxLineResult is what one rate line charges after calculateTaxBreakdown.
type taxLineResult struct {
	rateID      string
	taxBehavior types.TaxBehavior
	// taxableAmount is what this rate was computed against: the invoice's full taxable amount
	// for an inclusive line, or that amount less the inclusive tax for an exclusive line. The
	// two are not independent — see calculateTaxBreakdown.
	taxableAmount decimal.Decimal
	taxAmount     decimal.Decimal
}

// taxCalculationBreakdown is what calculateTaxBreakdown produces. Whether the customer is
// exempt never changes these amounts; the caller applies exemption afterward as a single
// override, never inside the calculation.
type taxCalculationBreakdown struct {
	inclusiveTax decimal.Decimal
	exclusiveTax decimal.Decimal
	lines        []*taxLineResult
}

// inclusiveShare is one rate's rounded slice of the combined inclusive tax, kept with its rate
// so the largest share can absorb the rounding remainder.
type inclusiveShare struct {
	rate   taxRateLine
	amount decimal.Decimal
}

// calculateTaxBreakdown splits rates by behavior and computes what each one charges. Both run
// on the discounted amount, matching Stripe's tax-rate ordering:
//
//  1. Inclusive tax is recovered from taxableAmount (already post-discount). Discounting a
//     tax-inclusive price reduces the tax inside it too.
//  2. Exclusive tax runs on taxableAmount less that inclusive portion, never on taxableAmount
//     directly, which would tax money the inclusive portion already accounts for.
//  3. Total is taxableAmount plus the exclusive tax.
//
// Several simultaneous inclusive rates cannot each be extracted independently: each would claim
// the whole gap between the amount and its tax-free price, giving as many contradictory
// tax-free prices as there are rates. They are combined, extracted once, then split
// proportionally.
//
// Inclusive tax can never reach taxableAmount, since rate/(100+rate) is below 1 for any rate at
// or above zero, so netTaxableAmount is never negative.
func calculateTaxBreakdown(taxableAmount decimal.Decimal, rates []taxRateLine, currency string) *taxCalculationBreakdown {
	precision := types.GetCurrencyPrecision(currency)
	hundred := decimal.NewFromInt(100)

	var combinedInclusiveRate decimal.Decimal
	var inclusiveLines, exclusiveLines []taxRateLine
	for _, r := range rates {
		if r.taxBehavior == types.TaxBehaviorInclusive {
			inclusiveLines = append(inclusiveLines, r)
			combinedInclusiveRate = combinedInclusiveRate.Add(*r.percentageValue)
		} else {
			exclusiveLines = append(exclusiveLines, r)
		}
	}

	// combinedInclusiveRate can only be zero when every inclusive line's own rate is zero too
	// (e.g. a single inclusive rate at 0%%) — guard the division rather than let a legitimate
	// 0%% rate panic.
	var unroundedInclusiveTax decimal.Decimal
	if !combinedInclusiveRate.IsZero() {
		// amount * rate / (100 + rate) — the tax already inside amount at this combined rate.
		unroundedInclusiveTax = taxableAmount.Mul(combinedInclusiveRate).Div(hundred.Add(combinedInclusiveRate))
	}
	inclusiveTax := unroundedInclusiveTax.Round(precision)

	lines := make([]*taxLineResult, 0, len(rates))

	if len(inclusiveLines) > 0 {
		shares := make([]inclusiveShare, len(inclusiveLines))
		var sumRounded decimal.Decimal
		largestIdx := 0
		for i, r := range inclusiveLines {
			var amount decimal.Decimal
			if !combinedInclusiveRate.IsZero() {
				// this rate's share of the combined tax, proportional to its own rate.
				amount = unroundedInclusiveTax.Mul(*r.percentageValue).Div(combinedInclusiveRate).Round(precision)
			}
			shares[i] = inclusiveShare{rate: r, amount: amount}
			sumRounded = sumRounded.Add(amount)
			if amount.GreaterThan(shares[largestIdx].amount) {
				largestIdx = i
			}
		}

		// Independently-rounded shares can land a cent off the rounded whole; assign the
		// stray remainder to the largest share so the lines still sum to inclusiveTax exactly.
		if remainder := inclusiveTax.Sub(sumRounded); !remainder.IsZero() {
			shares[largestIdx].amount = shares[largestIdx].amount.Add(remainder)
		}

		for _, sh := range shares {
			lines = append(lines, &taxLineResult{
				rateID:        sh.rate.id,
				taxBehavior:   types.TaxBehaviorInclusive,
				taxableAmount: taxableAmount,
				taxAmount:     sh.amount,
			})
		}
	}

	// What is left to be taxed on top, once the inclusive portion is accounted for.
	netTaxableAmount := taxableAmount.Sub(inclusiveTax)

	var exclusiveTax decimal.Decimal
	for _, r := range exclusiveLines {
		amount := netTaxableAmount.Mul(*r.percentageValue).Div(hundred).Round(precision)
		lines = append(lines, &taxLineResult{
			rateID:        r.id,
			taxBehavior:   types.TaxBehaviorExclusive,
			taxableAmount: netTaxableAmount,
			taxAmount:     amount,
		})
		exclusiveTax = exclusiveTax.Add(amount)
	}

	return &taxCalculationBreakdown{
		inclusiveTax: inclusiveTax,
		exclusiveTax: exclusiveTax,
		lines:        lines,
	}
}
