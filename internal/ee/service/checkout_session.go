package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type CheckoutSessionService interface {
	CreateCheckoutSession(ctx context.Context, req dto.CreateCheckoutSessionRequest) (*dto.CheckoutSessionResponse, error)
	GetCheckoutSession(ctx context.Context, id string) (*dto.CheckoutSessionResponse, error)
	ListCheckoutSessions(ctx context.Context, filter *types.CheckoutSessionFilter) (*dto.ListCheckoutSessionsResponse, error)
	UpdateCheckoutSession(ctx context.Context, id string, req dto.UpdateCheckoutSessionRequest) (*dto.CheckoutSessionResponse, error)
	DeleteCheckoutSession(ctx context.Context, id string) error
}

type checkoutSessionService struct {
	ServiceParams
}

func NewCheckoutSessionService(params ServiceParams) CheckoutSessionService {
	return &checkoutSessionService{ServiceParams: params}
}

func (s *checkoutSessionService) CreateCheckoutSession(ctx context.Context, req dto.CreateCheckoutSessionRequest) (*dto.CheckoutSessionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	session := req.ToCheckoutSession(ctx)

	if err := s.CheckoutSessionRepo.Create(ctx, session); err != nil {
		return nil, err
	}

	return dto.ToCheckoutSessionResponse(session), nil
}

func (s *checkoutSessionService) GetCheckoutSession(ctx context.Context, id string) (*dto.CheckoutSessionResponse, error) {
	if id == "" {
		return nil, ierr.NewError("id is required").
			WithHint("checkout session ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	session, err := s.CheckoutSessionRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return dto.ToCheckoutSessionResponse(session), nil
}

func (s *checkoutSessionService) ListCheckoutSessions(ctx context.Context, filter *types.CheckoutSessionFilter) (*dto.ListCheckoutSessionsResponse, error) {
	if filter == nil {
		filter = types.NewDefaultCheckoutSessionFilter()
	}
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	sessions, err := s.CheckoutSessionRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.CheckoutSessionRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	items := make([]*dto.CheckoutSessionResponse, len(sessions))
	for i, sess := range sessions {
		items[i] = dto.ToCheckoutSessionResponse(sess)
	}

	result := types.NewListResponse(items, count, filter.GetLimit(), filter.GetOffset())
	return &result, nil
}

func (s *checkoutSessionService) UpdateCheckoutSession(ctx context.Context, id string, req dto.UpdateCheckoutSessionRequest) (*dto.CheckoutSessionResponse, error) {
	if id == "" {
		return nil, ierr.NewError("id is required").
			WithHint("checkout session ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	session, err := s.CheckoutSessionRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.CheckoutStatus != nil {
		if err := req.CheckoutStatus.Validate(); err != nil {
			return nil, err
		}
		session.CheckoutStatus = *req.CheckoutStatus
	}
	if req.CheckoutInvoiceID != nil {
		session.CheckoutInvoiceID = req.CheckoutInvoiceID
	}
	if req.CheckoutPaymentID != nil {
		session.CheckoutPaymentID = req.CheckoutPaymentID
	}
	if req.Result != nil {
		session.Result = (*domainCheckout.JSONBCheckoutResult)(req.Result)
	}
	if req.ProviderResult != nil {
		session.ProviderResult = (*domainCheckout.JSONBCheckoutProviderResult)(req.ProviderResult)
	}
	if req.CompletedAt != nil {
		session.CompletedAt = req.CompletedAt
	}
	if req.CancelledAt != nil {
		session.CancelledAt = req.CancelledAt
	}
	if req.FailureReason != nil {
		session.FailureReason = req.FailureReason
	}

	session.UpdatedBy = types.GetUserID(ctx)

	if err := s.CheckoutSessionRepo.Update(ctx, session); err != nil {
		return nil, err
	}

	return dto.ToCheckoutSessionResponse(session), nil
}

func (s *checkoutSessionService) DeleteCheckoutSession(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("id is required").
			WithHint("checkout session ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	session, err := s.CheckoutSessionRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	session.Status = types.StatusArchived
	session.UpdatedBy = types.GetUserID(ctx)

	return s.CheckoutSessionRepo.Update(ctx, session)
}

// ensure interface compliance at compile time
var _ CheckoutSessionService = (*checkoutSessionService)(nil)
