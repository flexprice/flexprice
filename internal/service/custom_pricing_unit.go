package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/custompricingunit"
	"github.com/flexprice/flexprice/ent/price"
	"github.com/flexprice/flexprice/internal/api/dto"
	domainCPU "github.com/flexprice/flexprice/internal/domain/custompricingunit"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CustomPricingUnitService handles business logic for custom pricing units
type CustomPricingUnitService struct {
	repo   domainCPU.Repository
	client *ent.Client // Keep client for price checks during deletion
}

// NewCustomPricingUnitService creates a new instance of CustomPricingUnitService
func NewCustomPricingUnitService(repo domainCPU.Repository, client *ent.Client) *CustomPricingUnitService {
	return &CustomPricingUnitService{
		repo:   repo,
		client: client,
	}
}

func (s *CustomPricingUnitService) Create(ctx context.Context, req *dto.CreateCustomPricingUnitRequest) (*domainCPU.CustomPricingUnit, error) {
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
	exists, err := s.client.CustomPricingUnit.Query().
		Where(
			custompricingunit.CodeEQ(strings.ToLower(req.Code)),
			custompricingunit.TenantIDEQ(tenantID),
			custompricingunit.EnvironmentIDEQ(environmentID),
			custompricingunit.StatusEQ(string(types.StatusPublished)),
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
	id := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOM_PRICING_UNIT)

	unit := &domainCPU.CustomPricingUnit{
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

// List returns a paginated list of custom pricing units
func (s *CustomPricingUnitService) List(ctx context.Context, filter *dto.CustomPricingUnitFilter) (*dto.ListCustomPricingUnitsResponse, error) {
	// Convert DTO filter to domain filter
	domainFilter := &domainCPU.CustomPricingUnitFilter{
		Status:        filter.Status,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
	}

	// Get total count
	total, err := s.repo.Count(ctx, domainFilter)
	if err != nil {
		return nil, err
	}

	// Get paginated results
	units, err := s.repo.List(ctx, domainFilter)
	if err != nil {
		return nil, err
	}

	// Convert to response
	response := &dto.ListCustomPricingUnitsResponse{
		Units:      make([]dto.CustomPricingUnitResponse, len(units)),
		TotalCount: total,
	}

	for i, unit := range units {
		response.Units[i] = *s.toResponse(unit)
	}

	return response, nil
}

// GetByID retrieves a custom pricing unit by ID
func (s *CustomPricingUnitService) GetByID(ctx context.Context, id string) (*dto.CustomPricingUnitResponse, error) {
	unit, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithMessage("custom pricing unit not found").
				WithHint("Custom pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return nil, err
	}

	return s.toResponse(unit), nil
}

func (s *CustomPricingUnitService) GetByCode(ctx context.Context, code, tenantID, environmentID string) (*dto.CustomPricingUnitResponse, error) {
	unit, err := s.repo.GetByCode(ctx, strings.ToLower(code), tenantID, environmentID, string(types.StatusPublished))
	if err != nil {
		return nil, err
	}
	return s.toResponse(unit), nil
}

func (s *CustomPricingUnitService) Update(ctx context.Context, id string, req *dto.UpdateCustomPricingUnitRequest) (*dto.CustomPricingUnitResponse, error) {
	// Get existing unit
	existingUnit, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Validate status transition
	if req.Status != "" {
		if req.Status == existingUnit.Status {
			return nil, ierr.NewError("invalid status transition").
				WithMessage("unit is already in the requested status").
				WithHint(fmt.Sprintf("The custom pricing unit is already in %s status", req.Status)).
				WithReportableDetails(map[string]interface{}{
					"current_status":   existingUnit.Status,
					"requested_status": req.Status,
				}).
				Mark(ierr.ErrValidation)
		}

		// Check valid transitions
		switch existingUnit.Status {
		case types.StatusPublished:
			if req.Status != types.StatusArchived {
				return nil, ierr.NewError("invalid status transition").
					WithMessage("cannot transition from published to requested status").
					WithHint("Published units can only be archived").
					WithReportableDetails(map[string]interface{}{
						"current_status":   existingUnit.Status,
						"requested_status": req.Status,
					}).
					Mark(ierr.ErrValidation)
			}
		case types.StatusArchived:
			if req.Status != types.StatusPublished && req.Status != types.StatusDeleted {
				return nil, ierr.NewError("invalid status transition").
					WithMessage("cannot transition from archived to requested status").
					WithHint("Archived units can only be published or deleted").
					WithReportableDetails(map[string]interface{}{
						"current_status":   existingUnit.Status,
						"requested_status": req.Status,
					}).
					Mark(ierr.ErrValidation)
			}

			// If transitioning from archived to published, check code uniqueness
			if req.Status == types.StatusPublished {
				exists, err := s.client.CustomPricingUnit.Query().
					Where(
						custompricingunit.CodeEQ(existingUnit.Code),
						custompricingunit.TenantIDEQ(existingUnit.TenantID),
						custompricingunit.EnvironmentIDEQ(existingUnit.EnvironmentID),
						custompricingunit.StatusEQ(string(types.StatusPublished)),
						custompricingunit.IDNEQ(id),
					).
					Exist(ctx)
				if err != nil {
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
							"code":   existingUnit.Code,
							"status": types.StatusPublished,
						}).
						Mark(ierr.ErrValidation)
				}
			}
		default:
			return nil, ierr.NewError("invalid status transition").
				WithMessage("current status does not allow transitions").
				WithHint("Only published units can be archived, and archived units can be published or deleted").
				WithReportableDetails(map[string]interface{}{
					"current_status":   existingUnit.Status,
					"requested_status": req.Status,
				}).
				Mark(ierr.ErrValidation)
		}

		existingUnit.Status = req.Status
	}

	// Update other fields if provided
	if req.Name != "" {
		existingUnit.Name = req.Name
	}
	if req.Symbol != "" {
		existingUnit.Symbol = req.Symbol
	}
	if req.Precision != 0 {
		existingUnit.Precision = req.Precision
	}

	existingUnit.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx, existingUnit); err != nil {
		return nil, err
	}

	return s.toResponse(existingUnit), nil
}

func (s *CustomPricingUnitService) Delete(ctx context.Context, id string) error {
	// Get the existing unit first
	existingUnit, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// Check if already archived
	if existingUnit.Status == types.StatusArchived {
		return ierr.NewError("custom pricing unit is already archived").
			WithMessage("cannot archive unit in archived state").
			WithHint("The custom pricing unit is already in archived state").
			WithReportableDetails(map[string]interface{}{
				"id":     id,
				"status": existingUnit.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	// Check if the unit is being used by any prices
	exists, err := s.client.Price.Query().
		Where(price.CustomPricingUnitIDEQ(id)).
		Exist(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to check if custom pricing unit is in use").
			Mark(ierr.ErrDatabase)
	}
	if exists {
		return ierr.NewError("custom pricing unit is in use").
			WithMessage("cannot archive unit that is in use").
			WithHint("This custom pricing unit is being used by one or more prices").
			Mark(ierr.ErrValidation)
	}

	// Archive the unit by updating its status
	existingUnit.Status = types.StatusArchived
	existingUnit.UpdatedAt = time.Now().UTC()

	return s.repo.Update(ctx, existingUnit)
}

// toResponse converts a domain CustomPricingUnit to a dto.CustomPricingUnitResponse
func (s *CustomPricingUnitService) toResponse(unit *domainCPU.CustomPricingUnit) *dto.CustomPricingUnitResponse {
	return &dto.CustomPricingUnitResponse{
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
