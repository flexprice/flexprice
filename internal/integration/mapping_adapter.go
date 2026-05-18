package integration

import (
	"context"
	"time"

	apidto "github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
)

// entityIntegrationMappingServiceAdapter wraps an entityintegrationmapping.Repository and
// implements interfaces.EntityIntegrationMappingService.
// It is used by the integration factory so that sub-packages (e.g. Paddle) can call the
// service interface instead of the raw repository.
type entityIntegrationMappingServiceAdapter struct {
	repo entityintegrationmapping.Repository
}

// newEntityIntegrationMappingServiceAdapter creates the adapter.
func newEntityIntegrationMappingServiceAdapter(repo entityintegrationmapping.Repository) interfaces.EntityIntegrationMappingService {
	return &entityIntegrationMappingServiceAdapter{repo: repo}
}

func (a *entityIntegrationMappingServiceAdapter) CreateEntityIntegrationMapping(ctx context.Context, req apidto.CreateEntityIntegrationMappingRequest) (*apidto.EntityIntegrationMappingResponse, error) {
	mapping := req.ToEntityIntegrationMapping(ctx)
	if err := entityintegrationmapping.Validate(mapping); err != nil {
		return nil, ierr.WithError(err).WithHint("Invalid entity integration mapping data").Mark(ierr.ErrValidation)
	}
	if err := a.repo.Create(ctx, mapping); err != nil {
		return nil, err
	}
	return toMappingResponse(mapping), nil
}

func (a *entityIntegrationMappingServiceAdapter) GetEntityIntegrationMapping(ctx context.Context, id string) (*apidto.EntityIntegrationMappingResponse, error) {
	if id == "" {
		return nil, ierr.NewError("entity integration mapping ID is required").Mark(ierr.ErrValidation)
	}
	mapping, err := a.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return toMappingResponse(mapping), nil
}

func (a *entityIntegrationMappingServiceAdapter) GetEntityIntegrationMappings(ctx context.Context, filter *types.EntityIntegrationMappingFilter) (*apidto.ListEntityIntegrationMappingsResponse, error) {
	if filter == nil {
		filter = &types.EntityIntegrationMappingFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}
	mappings, err := a.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	total, err := a.repo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}
	items := make([]*apidto.EntityIntegrationMappingResponse, 0, len(mappings))
	for _, m := range mappings {
		items = append(items, toMappingResponse(m))
	}
	return &apidto.ListEntityIntegrationMappingsResponse{
		Items:      items,
		Pagination: types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

func (a *entityIntegrationMappingServiceAdapter) UpdateEntityIntegrationMapping(ctx context.Context, id string, req apidto.UpdateEntityIntegrationMappingRequest) (*apidto.EntityIntegrationMappingResponse, error) {
	if id == "" {
		return nil, ierr.NewError("entity integration mapping ID is required").Mark(ierr.ErrValidation)
	}
	mapping, err := a.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.ProviderEntityID != nil {
		mapping.ProviderEntityID = *req.ProviderEntityID
	}
	if req.Metadata != nil {
		mapping.Metadata = req.Metadata
	}
	mapping.UpdatedAt = time.Now().UTC()
	mapping.UpdatedBy = types.GetUserID(ctx)
	if err := a.repo.Update(ctx, mapping); err != nil {
		return nil, err
	}
	return toMappingResponse(mapping), nil
}

func (a *entityIntegrationMappingServiceAdapter) DeleteEntityIntegrationMapping(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("entity integration mapping ID is required").Mark(ierr.ErrValidation)
	}
	mapping, err := a.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	return a.repo.Delete(ctx, mapping)
}

func (a *entityIntegrationMappingServiceAdapter) LinkIntegrationMapping(ctx context.Context, req apidto.LinkIntegrationMappingRequest) (*apidto.LinkIntegrationMappingResponse, error) {
	return nil, ierr.NewError("LinkIntegrationMapping not supported via integration factory adapter").
		WithHint("Use the service layer directly for this operation").
		Mark(ierr.ErrValidation)
}

// toMappingResponse converts a domain mapping to a DTO response.
func toMappingResponse(m *entityintegrationmapping.EntityIntegrationMapping) *apidto.EntityIntegrationMappingResponse {
	return &apidto.EntityIntegrationMappingResponse{
		ID:               m.ID,
		EntityID:         m.EntityID,
		EntityType:       m.EntityType,
		ProviderType:     m.ProviderType,
		ProviderEntityID: m.ProviderEntityID,
		EnvironmentID:    m.EnvironmentID,
		TenantID:         m.TenantID,
		Status:           m.Status,
		Metadata:         m.Metadata,
		CreatedAt:        m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:        m.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		CreatedBy:        m.CreatedBy,
		UpdatedBy:        m.UpdatedBy,
	}
}
