package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
)

type SubscriptionPhaseService interface {
	CreateSubscriptionPhase(ctx context.Context, req dto.CreateSubscriptionPhaseRequest) (*dto.SubscriptionPhaseResponse, error)
	GetSubscriptionPhase(ctx context.Context, id string) (*dto.SubscriptionPhaseResponse, error)
	GetSubscriptionPhases(ctx context.Context, filter *types.SubscriptionPhaseFilter) (*dto.ListSubscriptionPhasesResponse, error)
	UpdateSubscriptionPhase(ctx context.Context, id string, req dto.UpdateSubscriptionPhaseRequest) (*dto.SubscriptionPhaseResponse, error)
	DeleteSubscriptionPhase(ctx context.Context, id string) error
}

type subscriptionPhaseService struct {
	ServiceParams
}

func NewSubscriptionPhaseService(params ServiceParams) SubscriptionPhaseService {
	return &subscriptionPhaseService{
		ServiceParams: params,
	}
}

func (s *subscriptionPhaseService) CreateSubscriptionPhase(ctx context.Context, req dto.CreateSubscriptionPhaseRequest) (*dto.SubscriptionPhaseResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Validate subscription exists
	sub, err := s.SubRepo.Get(ctx, req.SubscriptionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Subscription not found").
			Mark(ierr.ErrNotFound)
	}

	// Validate subscription status
	if sub.Status != types.StatusPublished {
		return nil, ierr.NewError("subscription is not published").
			WithHint("Cannot create phase for unpublished subscription").
			Mark(ierr.ErrValidation)
	}

	phaseModel := req.ToSubscriptionPhase(ctx)

	if err := s.SubscriptionPhaseRepo.Create(ctx, phaseModel); err != nil {
		return nil, err
	}

	// Publish webhook event
	s.publishWebhookEvent(ctx, types.WebhookEventSubscriptionPhaseCreated, phaseModel.ID)

	return &dto.SubscriptionPhaseResponse{SubscriptionPhase: phaseModel}, nil
}

func (s *subscriptionPhaseService) GetSubscriptionPhase(ctx context.Context, id string) (*dto.SubscriptionPhaseResponse, error) {
	if id == "" {
		return nil, ierr.NewError("subscription phase ID is required").
			WithHint("Subscription phase ID is required").
			Mark(ierr.ErrValidation)
	}

	phase, err := s.SubscriptionPhaseRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionPhaseResponse{SubscriptionPhase: phase}, nil
}

func (s *subscriptionPhaseService) GetSubscriptionPhases(ctx context.Context, filter *types.SubscriptionPhaseFilter) (*dto.ListSubscriptionPhasesResponse, error) {
	if filter == nil {
		filter = types.NewSubscriptionPhaseFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	// Set default sort order if not specified
	if filter.QueryFilter.Sort == nil {
		filter.QueryFilter.Sort = lo.ToPtr("start_date")
		filter.QueryFilter.Order = lo.ToPtr("asc")
	}

	// Validate filters
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	phases, err := s.SubscriptionPhaseRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	phaseCount, err := s.SubscriptionPhaseRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &dto.ListSubscriptionPhasesResponse{
		Items: make([]*dto.SubscriptionPhaseResponse, len(phases)),
	}

	for i, phase := range phases {
		response.Items[i] = &dto.SubscriptionPhaseResponse{SubscriptionPhase: phase}
	}

	response.Pagination = types.NewPaginationResponse(
		phaseCount,
		filter.GetLimit(),
		filter.GetOffset(),
	)

	return response, nil
}

func (s *subscriptionPhaseService) UpdateSubscriptionPhase(ctx context.Context, id string, req dto.UpdateSubscriptionPhaseRequest) (*dto.SubscriptionPhaseResponse, error) {
	if id == "" {
		return nil, ierr.NewError("subscription phase ID is required").
			WithHint("Subscription phase ID is required").
			Mark(ierr.ErrValidation)
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	phase, err := s.SubscriptionPhaseRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Only metadata can be updated - start_date and end_date are immutable
	if req.Metadata != nil {
		phase.Metadata = *req.Metadata
	}

	phase.UpdatedAt = time.Now().UTC()
	phase.UpdatedBy = types.GetUserID(ctx)

	if err := s.SubscriptionPhaseRepo.Update(ctx, phase); err != nil {
		return nil, err
	}

	// Publish webhook event
	s.publishWebhookEvent(ctx, types.WebhookEventSubscriptionPhaseUpdated, phase.ID)

	return &dto.SubscriptionPhaseResponse{SubscriptionPhase: phase}, nil
}

func (s *subscriptionPhaseService) DeleteSubscriptionPhase(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("subscription phase ID is required").
			WithHint("Subscription phase ID is required").
			Mark(ierr.ErrValidation)
	}

	phase, err := s.SubscriptionPhaseRepo.Get(ctx, id)
	if err != nil {
		return ierr.WithError(err).
			WithHint(fmt.Sprintf("Subscription phase with ID %s not found", id)).
			Mark(ierr.ErrNotFound)
	}

	if phase.Status != types.StatusPublished {
		return ierr.NewError("subscription phase is not published").
			WithHint("Cannot delete unpublished subscription phase").
			Mark(ierr.ErrNotFound)
	}

	if err := s.SubscriptionPhaseRepo.Delete(ctx, id); err != nil {
		return err
	}

	// Publish webhook event
	s.publishWebhookEvent(ctx, types.WebhookEventSubscriptionPhaseDeleted, id)

	return nil
}

func (s *subscriptionPhaseService) publishWebhookEvent(ctx context.Context, eventName string, phaseID string) {
	webhookPayload, err := json.Marshal(webhookDto.InternalSubscriptionPhaseEvent{
		PhaseID:  phaseID,
		TenantID: types.GetTenantID(ctx),
	})
	if err != nil {
		s.Logger.Errorw("failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Errorf("failed to publish %s event: %v", webhookEvent.EventName, err)
	}
}
