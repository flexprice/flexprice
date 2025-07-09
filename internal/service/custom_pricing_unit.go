package service

import (
	"context"
	"fmt"
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
	client *ent.Client
}

// NewCustomPricingUnitService creates a new instance of CustomPricingUnitService
func NewCustomPricingUnitService(client *ent.Client) *CustomPricingUnitService {
	return &CustomPricingUnitService{
		client: client,
	}
}

// Create creates a new custom pricing unit
func (s *CustomPricingUnitService) Create(ctx context.Context, req *dto.CreateCustomPricingUnitRequest) (*dto.CustomPricingUnitResponse, error) {
	// Get tenant and environment IDs from context
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant id is required").
			WithMessage("missing tenant id in context").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}

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
			custompricingunit.CodeEQ(req.Code),
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

	// Validate conversion rate is provided (since it's required)
	if req.ConversionRate == nil {
		return nil, ierr.NewError("conversion rate is required").
			WithMessage("missing required conversion rate").
			WithHint("Conversion rate is required").
			Mark(ierr.ErrValidation)
	}

	// Create the custom pricing unit with prefixed UUID
	unit, err := s.client.CustomPricingUnit.Create().
		SetID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOM_PRICING_UNIT)).
		SetTenantID(tenantID).
		SetEnvironmentID(environmentID).
		SetName(req.Name).
		SetCode(req.Code).
		SetSymbol(req.Symbol).
		SetBaseCurrency(req.BaseCurrency).
		SetConversionRate(*req.ConversionRate).
		SetPrecision(req.Precision).
		Save(ctx)
	if err != nil {
		// Preserve database errors without overwriting
		if ierr.IsDatabase(err) {
			return nil, err
		}
		return nil, ierr.WithError(err).
			WithMessage("failed to create custom pricing unit").
			WithHint("Failed to create custom pricing unit").
			Mark(ierr.ErrDatabase)
	}

	return s.toResponse(unit), nil
}

// List returns a paginated list of custom pricing units
func (s *CustomPricingUnitService) List(ctx context.Context, filter *dto.CustomPricingUnitFilter) (*dto.ListCustomPricingUnitsResponse, error) {
	query := s.client.CustomPricingUnit.Query()

	// Apply filters
	if filter.Status != "" {
		query = query.Where(custompricingunit.StatusEQ(string(filter.Status)))
	}

	// Get total count
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, err
	}

	// Get paginated results
	units, err := query.All(ctx)
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

// Get retrieves a custom pricing unit by ID
func (s *CustomPricingUnitService) Get(ctx context.Context, id string) (*dto.CustomPricingUnitResponse, error) {
	unit, err := s.client.CustomPricingUnit.Get(ctx, id)
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

// Update updates a custom pricing unit
func (s *CustomPricingUnitService) Update(ctx context.Context, id string, req *dto.UpdateCustomPricingUnitRequest) (*dto.CustomPricingUnitResponse, error) {
	// Get the existing unit first
	existingUnit, err := s.client.CustomPricingUnit.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithMessage("custom pricing unit not found").
				WithHint("Custom pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		// Preserve database errors without overwriting
		if ierr.IsDatabase(err) {
			return nil, err
		}
		return nil, ierr.WithError(err).
			WithMessage("failed to get custom pricing unit").
			WithHint("Failed to get custom pricing unit").
			Mark(ierr.ErrDatabase)
	}

	// Create domain model for validation
	domainUnit := &domainCPU.CustomPricingUnit{
		ID:             existingUnit.ID,
		Name:           existingUnit.Name,
		Code:           existingUnit.Code,
		Symbol:         existingUnit.Symbol,
		BaseCurrency:   existingUnit.BaseCurrency,
		ConversionRate: existingUnit.ConversionRate,
		Precision:      existingUnit.Precision,
		Status:         types.Status(existingUnit.Status),
		TenantID:       existingUnit.TenantID,
		EnvironmentID:  existingUnit.EnvironmentID,
		CreatedAt:      existingUnit.CreatedAt,
		UpdatedAt:      existingUnit.UpdatedAt,
	}

	// Build update query
	update := s.client.CustomPricingUnit.UpdateOneID(id)

	// Handle status update if provided
	if req.Status != "" {
		// Check if status is already the requested status
		if existingUnit.Status == string(req.Status) {
			return nil, ierr.NewError("invalid status transition").
				WithMessage("status already set").
				WithHint(fmt.Sprintf("Custom pricing unit is already in %s status", req.Status)).
				WithReportableDetails(map[string]interface{}{
					"current_status":   existingUnit.Status,
					"requested_status": req.Status,
				}).
				Mark(ierr.ErrValidation)
		}

		// Validate status transitions
		switch types.Status(existingUnit.Status) {
		case types.StatusPublished:
			// Published can only transition to Archived
			if req.Status != types.StatusArchived {
				return nil, ierr.NewError("invalid status transition").
					WithMessage("invalid status transition from published").
					WithHint("Published pricing unit can only be archived").
					WithReportableDetails(map[string]interface{}{
						"current_status":      existingUnit.Status,
						"requested_status":    req.Status,
						"allowed_transitions": []string{string(types.StatusArchived)},
					}).
					Mark(ierr.ErrValidation)
			}
		case types.StatusArchived:
			// Archived can transition to Published or Deleted
			if req.Status != types.StatusPublished && req.Status != types.StatusDeleted {
				return nil, ierr.NewError("invalid status transition").
					WithMessage("invalid status transition from archived").
					WithHint("Archived pricing unit can only be published or deleted").
					WithReportableDetails(map[string]interface{}{
						"current_status":   existingUnit.Status,
						"requested_status": req.Status,
						"allowed_transitions": []string{
							string(types.StatusPublished),
							string(types.StatusDeleted),
						},
					}).
					Mark(ierr.ErrValidation)
			}

			// If transitioning to Published, check code uniqueness
			if req.Status == types.StatusPublished {
				exists, err := s.client.CustomPricingUnit.Query().
					Where(
						custompricingunit.And(
							custompricingunit.CodeEQ(existingUnit.Code),
							custompricingunit.TenantIDEQ(existingUnit.TenantID),
							custompricingunit.EnvironmentIDEQ(existingUnit.EnvironmentID),
							custompricingunit.StatusEQ(string(types.StatusPublished)),
							custompricingunit.IDNEQ(id), // Exclude current unit
						),
					).
					Exist(ctx)
				if err != nil {
					// Preserve database errors without overwriting
					if ierr.IsDatabase(err) {
						return nil, err
					}
					return nil, ierr.WithError(err).
						WithMessage("failed to check code uniqueness").
						WithHint("Failed to check if code already exists").
						Mark(ierr.ErrDatabase)
				}
				if exists {
					return nil, ierr.NewError("duplicate code").
						WithMessage("cannot publish due to code conflict").
						WithHint("Cannot change status to published. Another unit with the same code is already published.").
						WithReportableDetails(map[string]interface{}{
							"code":               existingUnit.Code,
							"conflicting_status": types.StatusPublished,
						}).
						Mark(ierr.ErrValidation)
				}
			}
		case types.StatusDeleted:
			// Deleted is a terminal state
			return nil, ierr.NewError("invalid status transition").
				WithMessage("cannot transition from deleted status").
				WithHint("Deleted pricing unit cannot be updated").
				WithReportableDetails(map[string]interface{}{
					"current_status":   existingUnit.Status,
					"requested_status": req.Status,
				}).
				Mark(ierr.ErrValidation)
		}

		domainUnit.Status = req.Status
		if err := domainUnit.Validate(); err != nil {
			return nil, err
		}
		update.SetStatus(string(req.Status))
	}

	// Handle other field updates
	if req.Name != "" {
		domainUnit.Name = req.Name
		if err := domainUnit.Validate(); err != nil {
			return nil, err
		}
		update.SetName(req.Name)
	}

	if req.Symbol != "" {
		domainUnit.Symbol = req.Symbol
		if err := domainUnit.Validate(); err != nil {
			return nil, err
		}
		update.SetSymbol(req.Symbol)
	}

	if req.Precision != 0 {
		domainUnit.Precision = req.Precision
		if err := domainUnit.Validate(); err != nil {
			return nil, err
		}
		update.SetPrecision(req.Precision)
	}

	// Perform update
	unit, err := update.Save(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithMessage("failed to update custom pricing unit").
			WithHint("Failed to update custom pricing unit").
			Mark(ierr.ErrDatabase)
	}

	return &dto.CustomPricingUnitResponse{
		ID:             unit.ID,
		Name:           unit.Name,
		Code:           unit.Code,
		Symbol:         unit.Symbol,
		BaseCurrency:   unit.BaseCurrency,
		ConversionRate: unit.ConversionRate,
		Precision:      unit.Precision,
		Status:         types.Status(unit.Status),
		CreatedAt:      unit.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      unit.UpdatedAt.Format(time.RFC3339),
	}, nil
}

// Delete archives a custom pricing unit
func (s *CustomPricingUnitService) Delete(ctx context.Context, id string) error {
	// Get the existing unit first
	existingUnit, err := s.client.CustomPricingUnit.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithMessage("custom pricing unit not found").
				WithHint("Custom pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		// Preserve database errors without overwriting
		if ierr.IsDatabase(err) {
			return err
		}
		return ierr.WithError(err).
			WithMessage("failed to get custom pricing unit").
			WithHint("Failed to get custom pricing unit").
			Mark(ierr.ErrDatabase)
	}

	// Check if already archived
	if existingUnit.Status == string(types.StatusArchived) {
		return ierr.NewError("custom pricing unit is already archived").
			WithMessage("cannot archive unit in archived state").
			WithHint("The custom pricing unit is already in archived state").
			WithReportableDetails(map[string]interface{}{
				"id":                  id,
				"status":              existingUnit.Status,
				"requested_operation": "archive",
			}).
			Mark(ierr.ErrValidation)
	}

	// Check if the unit is being used by any prices
	exists, err := s.client.Price.Query().
		Where(price.CustomPricingUnitIDEQ(id)).
		Exist(ctx)
	if err != nil {
		// Preserve database errors without overwriting
		if ierr.IsDatabase(err) {
			return err
		}
		return ierr.WithError(err).
			WithMessage("failed to check if custom pricing unit is in use").
			WithHint("Failed to check if custom pricing unit is in use").
			Mark(ierr.ErrDatabase)
	}
	if exists {
		return ierr.NewError("custom pricing unit is in use").
			WithMessage("cannot archive unit that is referenced by prices").
			WithHint("Cannot archive a custom pricing unit that is being used by one or more prices").
			WithReportableDetails(map[string]interface{}{
				"id":                id,
				"status":            existingUnit.Status,
				"operation_blocked": "archive",
			}).
			Mark(ierr.ErrValidation)
	}

	// Archive the unit by updating its status
	_, err = s.client.CustomPricingUnit.UpdateOneID(id).
		SetStatus(string(types.StatusArchived)).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithMessage("custom pricing unit not found").
				WithHint("Custom pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		// Preserve database errors without overwriting
		if ierr.IsDatabase(err) {
			return err
		}
		return ierr.WithError(err).
			WithMessage("failed to archive custom pricing unit").
			WithHint("Failed to archive custom pricing unit").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// toResponse converts an ent.CustomPricingUnit to a dto.CustomPricingUnitResponse
func (s *CustomPricingUnitService) toResponse(unit *ent.CustomPricingUnit) *dto.CustomPricingUnitResponse {
	return &dto.CustomPricingUnitResponse{
		ID:             unit.ID,
		Name:           unit.Name,
		Code:           unit.Code,
		Symbol:         unit.Symbol,
		BaseCurrency:   unit.BaseCurrency,
		ConversionRate: unit.ConversionRate,
		Precision:      unit.Precision,
		Status:         types.Status(unit.Status),
		CreatedAt:      unit.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      unit.UpdatedAt.Format(time.RFC3339),
	}
}
