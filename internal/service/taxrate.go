package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/taxrate"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type TaxRateService interface {
	// Core CRUD operations
	CreateTaxRate(ctx context.Context, req dto.CreateTaxRateRequest) (*dto.TaxRateResponse, error)
	GetTaxRate(ctx context.Context, id string) (*dto.TaxRateResponse, error)
	ListTaxRates(ctx context.Context, filter *types.TaxRateFilter) (*dto.ListTaxRatesResponse, error)
	UpdateTaxRate(ctx context.Context, id string, req dto.UpdateTaxRateRequest) (*dto.TaxRateResponse, error)
	GetTaxRateByCode(ctx context.Context, code string) (*dto.TaxRateResponse, error)

	// Tax resolution and calculation
	ResolveTaxRates(ctx context.Context, tenantID, customerID, planID, invoiceID, lineItemID string) ([]*taxrate.TaxRate, error)
	CalculateLineTaxes(ctx context.Context, netAmount decimal.Decimal, rates []*taxrate.TaxRate) ([]dto.AppliedTax, decimal.Decimal, error)
}

type taxRateService struct {
	repo   taxrate.Repository
	logger *logger.Logger
}

// NewTaxRateService creates a new instance of TaxRateService
func NewTaxRateService(repo taxrate.Repository, logger *logger.Logger) TaxRateService {
	return &taxRateService{
		repo:   repo,
		logger: logger,
	}
}

// CreateTaxRate creates a new tax rate
func (s *taxRateService) CreateTaxRate(ctx context.Context, req dto.CreateTaxRateRequest) (*dto.TaxRateResponse, error) {
	// Validate the request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Convert the request to a domain model
	taxRate, err := req.ToTaxRate(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse tax rate data").
			Mark(ierr.ErrValidation)
	}

	// Create the tax rate in the repository
	if err := s.repo.Create(ctx, taxRate); err != nil {
		return nil, err
	}

	// Return the created tax rate
	return &dto.TaxRateResponse{TaxRate: taxRate}, nil
}

// GetTaxRate retrieves a tax rate by ID
func (s *taxRateService) GetTaxRate(ctx context.Context, id string) (*dto.TaxRateResponse, error) {
	if id == "" {
		return nil, ierr.NewError("tax_rate_id is required").
			WithHint("Tax rate ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get the tax rate from the repository
	taxRate, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Return the tax rate
	return &dto.TaxRateResponse{TaxRate: taxRate}, nil
}

// ListTaxRates lists tax rates based on the provided filter
func (s *taxRateService) ListTaxRates(ctx context.Context, filter *types.TaxRateFilter) (*dto.ListTaxRatesResponse, error) {
	// Get tax rates from the repository
	taxRates, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Get the total count of tax rates
	count, err := s.repo.Count(ctx, filter)
	if err != nil {
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

// UpdateTaxRate creates a new version of a tax rate instead of modifying the existing one
// This preserves historical data while allowing changes to tax rates
func (s *taxRateService) UpdateTaxRate(ctx context.Context, id string, req dto.UpdateTaxRateRequest) (*dto.TaxRateResponse, error) {
	if id == "" {
		return nil, ierr.NewError("tax_rate_id is required").
			WithHint("Tax rate ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get the existing tax rate
	oldTaxRate, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Archive the old tax rate
	if err := s.DeleteTaxRate(ctx, id); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to delete the old tax rate version").
			Mark(ierr.ErrDatabase)
	}

	// Create a new tax rate with updated values
	newID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE)
	now := time.Now().UTC()

	newTaxRate := &taxrate.TaxRate{
		ID:            newID,
		Name:          oldTaxRate.Name,
		Code:          oldTaxRate.Code,
		Description:   oldTaxRate.Description,
		Percentage:    oldTaxRate.Percentage,
		FixedValue:    oldTaxRate.FixedValue,
		IsCompound:    oldTaxRate.IsCompound,
		ValidFrom:     oldTaxRate.ValidFrom,
		ValidTo:       oldTaxRate.ValidTo,
		EnvironmentID: oldTaxRate.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  oldTaxRate.TenantID,
			Status:    oldTaxRate.Status,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: types.GetUserID(ctx),
			UpdatedBy: types.GetUserID(ctx),
		},
	}

	// Update fields based on the request
	if req.Name != "" {
		newTaxRate.Name = req.Name
	}

	if req.Code != "" {
		newTaxRate.Code = req.Code
	}

	if req.Description != "" {
		newTaxRate.Description = req.Description
	}

	if req.Percentage != nil {
		newTaxRate.Percentage = req.Percentage
	}

	if req.FixedValue != nil {
		newTaxRate.FixedValue = req.FixedValue
	}

	if req.IsCompound != nil {
		newTaxRate.IsCompound = *req.IsCompound
	}

	if req.ValidFrom != nil {
		newTaxRate.ValidFrom = req.ValidFrom
	}

	if req.ValidTo != nil {
		newTaxRate.ValidTo = req.ValidTo
	}

	// Create the new tax rate in the repository
	if err := s.repo.Create(ctx, newTaxRate); err != nil {
		return nil, err
	}

	// TODO: Update all active tax assignments to point to the new tax rate ID
	// This will be implemented as part of the tax assignment service

	// Return the updated tax rate
	return &dto.TaxRateResponse{TaxRate: newTaxRate}, nil
}

// DeleteTaxRate is maintained for backward compatibility but now calls ArchiveTaxRate
func (s *taxRateService) DeleteTaxRate(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("tax_rate_id is required").
			WithHint("Tax rate ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get the tax rate to archive
	taxRate, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Call the repository's Delete method which handles archiving
	return s.repo.Delete(ctx, taxRate)
}

// GetTaxRateByCode retrieves a tax rate by its code
func (s *taxRateService) GetTaxRateByCode(ctx context.Context, code string) (*dto.TaxRateResponse, error) {
	if code == "" {
		return nil, ierr.NewError("tax_rate_code is required").
			WithHint("Tax rate code is required").
			Mark(ierr.ErrValidation)
	}

	// Get the tax rate by code from the repository
	taxRate, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}

	// Return the tax rate
	return &dto.TaxRateResponse{TaxRate: taxRate}, nil
}

// ResolveTaxRates implements the tax rate resolution algorithm based on the provided hierarchy
// Precedence: Line-item > Invoice > Customer > Plan > Tenant
func (s *taxRateService) ResolveTaxRates(ctx context.Context, tenantID, customerID, planID, invoiceID, lineItemID string) ([]*taxrate.TaxRate, error) {
	// TODO: This will be implemented when tax assignment functionality is added
	// The implementation will follow the resolution algorithm described in section 4.2 of the PRD

	// For now, just return an empty array
	return []*taxrate.TaxRate{}, nil
}

// CalculateLineTaxes calculates taxes for a line item
// It handles both compound and non-compound taxes
func (s *taxRateService) CalculateLineTaxes(ctx context.Context, netAmount decimal.Decimal, rates []*taxrate.TaxRate) ([]dto.AppliedTax, decimal.Decimal, error) {
	taxBase := netAmount
	totalTax := decimal.Zero
	appliedTaxes := []dto.AppliedTax{}

	// Sort rates by compound flag (non-compound first, then compound)
	// This ensures that non-compound taxes are calculated on the base amount
	// while compound taxes include previous taxes
	sortedRates := make([]*taxrate.TaxRate, len(rates))
	copy(sortedRates, rates)

	sort.SliceStable(sortedRates, func(i, j int) bool {
		return !sortedRates[i].IsCompound && sortedRates[j].IsCompound // non-compound taxes first
	})

	// Calculate each tax
	for _, rate := range sortedRates {
		// Skip archived or invalid tax rates
		if rate.Status == types.StatusArchived {
			continue
		}

		// Get percentage value, defaulting to 0 if nil
		percentage := decimal.Zero
		if rate.Percentage != nil {
			percentage = decimal.NewFromFloat(*rate.Percentage)
		}

		// Get fixed value, defaulting to 0 if nil
		fixedValue := decimal.Zero
		if rate.FixedValue != nil {
			fixedValue = decimal.NewFromFloat(*rate.FixedValue)
		}

		// Calculate tax amount (percentage-based + fixed)
		percentageTax := taxBase.Mul(percentage)
		taxAmount := percentageTax.Add(fixedValue)

		// Create applied tax entry
		appliedTaxes = append(appliedTaxes, dto.AppliedTax{
			TaxRateID:  rate.ID,
			Code:       rate.Code,
			Percentage: percentage,
			FixedValue: fixedValue,
			Amount:     taxAmount,
		})

		// Add to total tax
		totalTax = totalTax.Add(taxAmount)

		// If compound, add to the base for next tax calculation
		if rate.IsCompound {
			taxBase = taxBase.Add(taxAmount)
		}
	}

	return appliedTaxes, totalTax, nil
}
