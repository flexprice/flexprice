package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/price"
	"github.com/flexprice/flexprice/ent/priceunit"
	"github.com/flexprice/flexprice/internal/api/dto"
	domainPriceUnit "github.com/flexprice/flexprice/internal/domain/priceunit"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// PriceUnitService handles business logic for price units
type PriceUnitService struct {
	repo   domainPriceUnit.Repository
	client *ent.Client // Keep client for price checks during deletion
	log    *logger.Logger
}

// NewPriceUnitService creates a new instance of PriceUnitService
func NewPriceUnitService(repo domainPriceUnit.Repository, client *ent.Client, log *logger.Logger) *PriceUnitService {
	return &PriceUnitService{
		repo:   repo,
		client: client,
		log:    log,
	}
}

func (s *PriceUnitService) Create(ctx context.Context, req *dto.CreatePriceUnitRequest) (*domainPriceUnit.PriceUnit, error) {
	// Validate tenant ID
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant id is required").
			WithMessage("missing tenant id in context").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}

	// Validate environment ID
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID == "" {
		return nil, ierr.NewError("environment id is required").
			WithMessage("missing environment id in context").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}

	// Check if code already exists (only consider published records)
	exists, err := s.client.PriceUnit.Query().
		Where(
			priceunit.CodeEQ(strings.ToLower(req.Code)),
			priceunit.TenantIDEQ(tenantID),
			priceunit.EnvironmentIDEQ(environmentID),
			priceunit.StatusEQ(string(types.StatusPublished)),
		).
		Exist(ctx)
	if err != nil {
		// Preserve database errors without overwriting
		if ierr.IsDatabase(err) {
			return nil, err
		}
		return nil, ierr.WithError(err).
			WithMessage("failed to check if code exists").
			WithHint("Failed to check if code exists").
			Mark(ierr.ErrDatabase)
	}
	if exists {
		return nil, ierr.NewError("code already exists").
			WithMessage("duplicate code found for published unit").
			WithHint("A published custom pricing unit with this code already exists").
			WithReportableDetails(map[string]interface{}{
				"code":   req.Code,
				"status": types.StatusPublished,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate conversion rate is provided
	if req.ConversionRate == nil {
		return nil, ierr.NewError("conversion rate is required").
			WithMessage("missing required conversion rate").
			WithHint("Conversion rate is required").
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()

	// Generate ID with prefix
	id := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE_UNIT)

	unit := &domainPriceUnit.PriceUnit{
		ID:             id,
		Name:           req.Name,
		Code:           strings.ToLower(req.Code),
		Symbol:         req.Symbol,
		BaseCurrency:   strings.ToLower(req.BaseCurrency),
		ConversionRate: *req.ConversionRate,
		Precision:      req.Precision,
		Status:         types.StatusPublished,
		TenantID:       tenantID,
		EnvironmentID:  environmentID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.repo.Create(ctx, unit); err != nil {
		return nil, err
	}

	return unit, nil
}

// List returns a paginated list of pricing units
func (s *PriceUnitService) List(ctx context.Context, filter *dto.PriceUnitFilter) (*dto.ListPriceUnitsResponse, error) {
	// Convert DTO filter to domain filter
	domainFilter := &domainPriceUnit.PriceUnitFilter{
		Status:        filter.Status,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
	}

	// Get paginated results
	units, err := s.repo.List(ctx, domainFilter)
	if err != nil {
		return nil, err
	}

	// Convert to response
	response := &dto.ListPriceUnitsResponse{
		Items: make([]*dto.PriceUnitResponse, len(units)),
	}

	response.Items = make([]*dto.PriceUnitResponse, len(units))
	for i, unit := range units {
		response.Items[i] = s.toResponse(unit)

	}

	return response, nil
}

// GetByID retrieves a pricing unit by ID
func (s *PriceUnitService) GetByID(ctx context.Context, id string) (*dto.PriceUnitResponse, error) {
	unit, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithMessage("pricing unit not found").
				WithHint("Pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return nil, err
	}

	return s.toResponse(unit), nil
}

func (s *PriceUnitService) GetByCode(ctx context.Context, code, tenantID, environmentID string) (*dto.PriceUnitResponse, error) {
	unit, err := s.repo.GetByCode(ctx, strings.ToLower(code), tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return nil, err
	}
	return s.toResponse(unit), nil
}

func (s *PriceUnitService) Update(ctx context.Context, id string, req *dto.UpdatePriceUnitRequest) (*dto.PriceUnitResponse, error) {
	// Get existing unit
	existingUnit, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Track if any changes were made
	hasChanges := false
	changes := make(map[string]interface{})

	// Update fields if provided and different from current values
	if req.Name != "" {
		if req.Name != existingUnit.Name {
			existingUnit.Name = req.Name
			hasChanges = true
			changes["name"] = req.Name
		}
	}
	if req.Symbol != "" {
		if req.Symbol != existingUnit.Symbol {
			existingUnit.Symbol = req.Symbol
			hasChanges = true
			changes["symbol"] = req.Symbol
		}
	}
	if req.Precision != 0 {
		if req.Precision != existingUnit.Precision {
			existingUnit.Precision = req.Precision
			hasChanges = true
			changes["precision"] = req.Precision
		}
	}
	if req.ConversionRate != nil {
		if !req.ConversionRate.Equal(existingUnit.ConversionRate) {
			existingUnit.ConversionRate = *req.ConversionRate
			hasChanges = true
			changes["conversion_rate"] = req.ConversionRate.String()
		}
	}

	// Check if any changes were actually made
	if !hasChanges {
		return nil, ierr.NewError("no changes detected").
			WithMessage("provided values are the same as current values").
			WithHint("Provide different values to update the price unit").
			WithReportableDetails(map[string]interface{}{
				"id":              id,
				"name":            existingUnit.Name,
				"symbol":          existingUnit.Symbol,
				"precision":       existingUnit.Precision,
				"conversion_rate": existingUnit.ConversionRate,
			}).
			Mark(ierr.ErrValidation)
	}

	existingUnit.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx, existingUnit); err != nil {
		return nil, err
	}

	return s.toResponse(existingUnit), nil
}

func (s *PriceUnitService) Delete(ctx context.Context, id string) error {
	// Get the existing unit first
	existingUnit, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// Check the current status and handle accordingly
	switch existingUnit.Status {
	case types.StatusPublished:
		// Check if the unit is being used by any prices
		exists, err := s.client.Price.Query().
			Where(price.PriceUnitIDEQ(id)).
			Exist(ctx)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to check if price unit is in use").
				Mark(ierr.ErrDatabase)
		}
		if exists {
			return ierr.NewError("price unit is in use").
				WithMessage("cannot archive unit that is in use").
				WithHint("This price unit is being used by one or more prices").
				Mark(ierr.ErrValidation)
		}

		// Archive the unit (set status to archived)
		existingUnit.Status = types.StatusArchived
		existingUnit.UpdatedAt = time.Now().UTC()

		return s.repo.Update(ctx, existingUnit)

	case types.StatusArchived:
		return ierr.NewError("price unit is already archived").
			WithMessage("cannot archive unit that is already archived").
			WithHint("The price unit is already in archived status").
			WithReportableDetails(map[string]interface{}{
				"id":     id,
				"status": existingUnit.Status,
			}).
			Mark(ierr.ErrValidation)

	case types.StatusDeleted:
		return ierr.NewError("price unit has been hard deleted").
			WithMessage("cannot archive unit that has been hard deleted").
			WithHint("The price unit has been permanently deleted and cannot be modified").
			WithReportableDetails(map[string]interface{}{
				"id":     id,
				"status": existingUnit.Status,
			}).
			Mark(ierr.ErrValidation)

	default:
		return ierr.NewError("invalid price unit status").
			WithMessage("price unit has an invalid status").
			WithHint("Price unit status must be one of: published, archived, deleted").
			WithReportableDetails(map[string]interface{}{
				"id":     id,
				"status": existingUnit.Status,
			}).
			Mark(ierr.ErrValidation)
	}
}

// ConvertToBaseCurrency converts an amount from pricing unit to base currency
// amount in fiat currency = amount in pricing unit * conversion_rate
func (s *PriceUnitService) ConvertToBaseCurrency(ctx context.Context, code, tenantID, environmentID string, priceUnitAmount decimal.Decimal) (decimal.Decimal, error) {
	if priceUnitAmount.IsZero() {
		return decimal.Zero, nil
	}

	if priceUnitAmount.IsNegative() {
		return decimal.Zero, ierr.NewError("amount must be positive").
			WithMessage("negative amount provided for conversion").
			WithHint("Amount must be greater than zero").
			WithReportableDetails(map[string]interface{}{
				"amount": priceUnitAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	return s.repo.ConvertToBaseCurrency(ctx, strings.ToLower(code), tenantID, environmentID, priceUnitAmount)
}

// ConvertToPriceUnit converts an amount from base currency to pricing unit
// amount in pricing unit = amount in fiat currency / conversion_rate
func (s *PriceUnitService) ConvertToPriceUnit(ctx context.Context, code, tenantID, environmentID string, fiatAmount decimal.Decimal) (decimal.Decimal, error) {
	if fiatAmount.IsZero() {
		return decimal.Zero, nil
	}

	if fiatAmount.IsNegative() {
		return decimal.Zero, ierr.NewError("amount must be positive").
			WithMessage("negative amount provided for conversion").
			WithHint("Amount must be greater than zero").
			WithReportableDetails(map[string]interface{}{
				"amount": fiatAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	return s.repo.ConvertToPriceUnit(ctx, strings.ToLower(code), tenantID, environmentID, fiatAmount)
}

// toResponse converts a domain PricingUnit to a dto.PriceUnitResponse
func (s *PriceUnitService) toResponse(unit *domainPriceUnit.PriceUnit) *dto.PriceUnitResponse {
	return &dto.PriceUnitResponse{
		ID:             unit.ID,
		Name:           unit.Name,
		Code:           unit.Code,
		Symbol:         unit.Symbol,
		BaseCurrency:   unit.BaseCurrency,
		ConversionRate: unit.ConversionRate,
		Precision:      unit.Precision,
		Status:         unit.Status,
		CreatedAt:      unit.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      unit.UpdatedAt.Format(time.RFC3339),
	}
}
