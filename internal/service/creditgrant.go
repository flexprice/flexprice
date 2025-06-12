package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// CreditGrantService defines the interface for credit grant service
type CreditGrantService interface {
	// CreateCreditGrant creates a new credit grant
	CreateCreditGrant(ctx context.Context, req dto.CreateCreditGrantRequest) (*dto.CreditGrantResponse, error)

	// GetCreditGrant retrieves a credit grant by ID
	GetCreditGrant(ctx context.Context, id string) (*dto.CreditGrantResponse, error)

	// ListCreditGrants retrieves credit grants based on filter
	ListCreditGrants(ctx context.Context, filter *types.CreditGrantFilter) (*dto.ListCreditGrantsResponse, error)

	// UpdateCreditGrant updates an existing credit grant
	UpdateCreditGrant(ctx context.Context, id string, req dto.UpdateCreditGrantRequest) (*dto.CreditGrantResponse, error)

	// DeleteCreditGrant deletes a credit grant by ID
	DeleteCreditGrant(ctx context.Context, id string) error

	// GetCreditGrantsByPlan retrieves credit grants for a specific plan
	GetCreditGrantsByPlan(ctx context.Context, planID string) (*dto.ListCreditGrantsResponse, error)

	// GetCreditGrantsBySubscription retrieves credit grants for a specific subscription
	GetCreditGrantsBySubscription(ctx context.Context, subscriptionID string) (*dto.ListCreditGrantsResponse, error)

	ProcessScheduledCreditGrantApplications(ctx context.Context) error

	// ApplyRecurringGrant applies a recurring credit grant to a subscription
	ApplyRecurringGrant(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription) error
}

type creditGrantService struct {
	ServiceParams
}

func NewCreditGrantService(
	serviceParams ServiceParams,
) CreditGrantService {
	return &creditGrantService{
		ServiceParams: serviceParams,
	}
}

func (s *creditGrantService) CreateCreditGrant(ctx context.Context, req dto.CreateCreditGrantRequest) (*dto.CreditGrantResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Validate plan exists if plan_id is provided
	if req.PlanID != nil && *req.PlanID != "" {
		plan, err := s.PlanRepo.Get(ctx, *req.PlanID)
		if err != nil {
			return nil, err
		}
		if plan == nil {
			return nil, ierr.NewError("plan not found").
				WithHint(fmt.Sprintf("Plan with ID %s does not exist", *req.PlanID)).
				WithReportableDetails(map[string]interface{}{
					"plan_id": *req.PlanID,
				}).
				Mark(ierr.ErrNotFound)
		}
	}

	// Validate subscription exists if subscription_id is provided
	if req.SubscriptionID != nil && *req.SubscriptionID != "" {
		sub, err := s.SubRepo.Get(ctx, *req.SubscriptionID)
		if err != nil {
			return nil, err
		}
		if sub == nil {
			return nil, ierr.NewError("subscription not found").
				WithHint(fmt.Sprintf("Subscription with ID %s does not exist", *req.SubscriptionID)).
				WithReportableDetails(map[string]interface{}{
					"subscription_id": *req.SubscriptionID,
				}).
				Mark(ierr.ErrNotFound)
		}
	}

	// Create credit grant
	cg := req.ToCreditGrant(ctx)

	cg, err := s.CreditGrantRepo.Create(ctx, cg)
	if err != nil {
		return nil, err
	}

	response := &dto.CreditGrantResponse{CreditGrant: cg}

	return response, nil
}

func (s *creditGrantService) GetCreditGrant(ctx context.Context, id string) (*dto.CreditGrantResponse, error) {
	result, err := s.CreditGrantRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	response := &dto.CreditGrantResponse{CreditGrant: result}
	return response, nil
}

func (s *creditGrantService) ListCreditGrants(ctx context.Context, filter *types.CreditGrantFilter) (*dto.ListCreditGrantsResponse, error) {
	if filter == nil {
		filter = types.NewDefaultCreditGrantFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	// Set default sort order if not specified
	if filter.QueryFilter.Sort == nil {
		filter.QueryFilter.Sort = lo.ToPtr("created_at")
		filter.QueryFilter.Order = lo.ToPtr("desc")
	}

	creditGrants, err := s.CreditGrantRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.CreditGrantRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &dto.ListCreditGrantsResponse{
		Items: make([]*dto.CreditGrantResponse, len(creditGrants)),
	}

	for i, cg := range creditGrants {
		response.Items[i] = &dto.CreditGrantResponse{CreditGrant: cg}
	}

	response.Pagination = types.NewPaginationResponse(
		count,
		filter.GetLimit(),
		filter.GetOffset(),
	)

	return response, nil
}

func (s *creditGrantService) UpdateCreditGrant(ctx context.Context, id string, req dto.UpdateCreditGrantRequest) (*dto.CreditGrantResponse, error) {
	existing, err := s.CreditGrantRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// TODO: add checks for not updating

	// Update fields if provided
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Metadata != nil {
		existing.Metadata = *req.Metadata
	}

	// Validate updated credit grant
	if err := existing.Validate(); err != nil {
		return nil, err
	}

	updated, err := s.CreditGrantRepo.Update(ctx, existing)
	if err != nil {
		return nil, err
	}

	response := &dto.CreditGrantResponse{CreditGrant: updated}
	return response, nil
}

func (s *creditGrantService) DeleteCreditGrant(ctx context.Context, id string) error {
	return s.CreditGrantRepo.Delete(ctx, id)
}

func (s *creditGrantService) GetCreditGrantsByPlan(ctx context.Context, planID string) (*dto.ListCreditGrantsResponse, error) {
	// Create a filter for the plan's credit grants
	filter := types.NewNoLimitCreditGrantFilter()
	filter.PlanIDs = []string{planID}
	filter.WithStatus(types.StatusPublished)

	// Use the standard list function to get the credit grants with expansion
	return s.ListCreditGrants(ctx, filter)
}

func (s *creditGrantService) GetCreditGrantsBySubscription(ctx context.Context, subscriptionID string) (*dto.ListCreditGrantsResponse, error) {
	// Create a filter for the subscription's credit grants
	filter := types.NewNoLimitCreditGrantFilter()
	filter.SubscriptionIDs = []string{subscriptionID}
	filter.WithStatus(types.StatusPublished)

	// Use the standard list function to get the credit grants with expansion
	resp, err := s.ListCreditGrants(ctx, filter)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *creditGrantService) ProcessScheduledCreditGrantApplications(ctx context.Context) error {
	// Find eligible subscriptions for this grant
	applications, err := s.CreditGrantApplicationRepo.FindAllScheduledApplications(ctx)
	if err != nil {
		return err
	}

	subscriptionService := NewSubscriptionService(s.ServiceParams)
	creditGrantService := NewCreditGrantService(s.ServiceParams)
	// add tenant_id and env_id in context for each application
	for _, cga := range applications {

		// we check if the application is alre	ady applied
		if cga.ApplicationStatus == types.ApplicationStatusApplied {
			// we skip the application if it is already applied
			continue
		}

		ctxWithTenant := context.WithValue(ctx, types.CtxTenantID, cga.TenantID)
		ctxWithEnv := context.WithValue(ctxWithTenant, types.CtxEnvironmentID, cga.EnvironmentID)

		// we validate subscription state
		subscription, err := subscriptionService.GetSubscription(ctxWithEnv, cga.SubscriptionID)
		if err != nil {
			return err
		}

		// we check if the credit grant is active or not
		creditGrant, err := creditGrantService.GetCreditGrant(ctxWithEnv, cga.CreditGrantID)
		if err != nil {
			return err
		}

		err = s.ProcessGrantForSubscription(ctxWithEnv, creditGrant.CreditGrant, subscription.Subscription, cga)
		if err != nil {
			return err
		}

	}

	return nil
}

func (s *creditGrantService) ProcessGrantForSubscription(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, cga *creditgrantapplication.CreditGrantApplication) error {
	// Determine if grant should be applied based on subscription state
	stateHandler := NewSubscriptionStateHandler(subscription, grant)

	action, reason := stateHandler.DetermineAction()

	// Apply the determined action
	switch action {
	case StateActionApply:
		return s.ApplyRecurringGrant(ctx, grant, subscription)
	case StateActionSkip:
		s.Logger.Debugw("skipping grant application", "reason", reason, "grant_id", grant.ID, "subscription_id", subscription.ID)
		return nil
	case StateActionDefer:
		s.Logger.Debugw("deferring grant application", "reason", reason, "grant_id", grant.ID, "subscription_id", subscription.ID)
		return nil
	case StateActionCancel:
		s.Logger.Debugw("cancelling grant application", "reason", reason, "grant_id", grant.ID, "subscription_id", subscription.ID)
		return nil
	default:
		return ierr.NewError("unknown state action").
			WithHint("Unknown state action").
			WithReportableDetails(map[string]interface{}{
				"action": action,
				"reason": reason,
			}).
			Mark(ierr.ErrInternal)
	}
}

func (s *creditGrantService) ApplyRecurringGrant(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription) error {
	// Use wallet service from the service params
	walletService := NewWalletService(s.ServiceParams)

	// Find or create wallet (reuse logic from subscription service)
	wallets, err := walletService.GetWalletsByCustomerID(ctx, subscription.CustomerID)
	if err != nil {
		return err
	}

	var selectedWallet *dto.WalletResponse
	for _, w := range wallets {
		if types.IsMatchingCurrency(w.Currency, subscription.Currency) {
			selectedWallet = w
			break
		}
	}

	if selectedWallet == nil {
		// Create new wallet
		walletReq := &dto.CreateWalletRequest{
			Name:       "Subscription Wallet",
			CustomerID: subscription.CustomerID,
			Currency:   subscription.Currency,
		}
		selectedWallet, err = walletService.CreateWallet(ctx, walletReq)
		if err != nil {
			return err
		}
	}

	// Calculate expiry date based on grant settings
	var expiryDate *time.Time
	if grant.ExpirationType == types.CreditGrantExpiryTypeBillingCycle {
		expiryDate = &subscription.CurrentPeriodEnd
	} else if grant.ExpirationType == types.CreditGrantExpiryTypeDuration && grant.ExpirationDuration != nil {
		expiry := time.Now().AddDate(0, 0, *grant.ExpirationDuration)
		expiryDate = &expiry
	}

	// Apply credit to wallet
	topupReq := &dto.TopUpWalletRequest{
		CreditsToAdd:      grant.Credits,
		TransactionReason: types.TransactionReasonSubscriptionCredit,
		ExpiryDateUTC:     expiryDate,
		Priority:          grant.Priority,
		IdempotencyKey:    lo.ToPtr(fmt.Sprintf("recurring_%s_%s_%d", grant.ID, subscription.ID, time.Now().Unix())),
		Metadata: map[string]string{
			"grant_id":        grant.ID,
			"subscription_id": subscription.ID,
			"reason":          "recurring_credit_grant",
			"period_start":    subscription.CurrentPeriodStart.Format(time.RFC3339),
			"period_end":      subscription.CurrentPeriodEnd.Format(time.RFC3339),
		},
	}

	_, err = walletService.TopUpWallet(ctx, selectedWallet.ID, topupReq)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to apply recurring credit grant to wallet").
			WithReportableDetails(map[string]interface{}{
				"grant_id":        grant.ID,
				"subscription_id": subscription.ID,
				"wallet_id":       selectedWallet.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	s.Logger.Infow("successfully applied recurring credit grant",
		"grant_id", grant.ID,
		"subscription_id", subscription.ID,
		"wallet_id", selectedWallet.ID,
		"amount", grant.Credits,
	)

	return nil
}
