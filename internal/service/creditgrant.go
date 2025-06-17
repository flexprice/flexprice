package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	domainCreditGrantApplication "github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
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

	// ApplyCreditGrant applies a credit grant to a subscription and creates CGA tracking records
	// This method handles both one-time and recurring credit grants
	ApplyCreditGrant(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, reason string, metadata types.Metadata) (*domainCreditGrantApplication.CreditGrantApplication, error)

	// CheckDuplicateApplication checks if a credit grant application already exists for the given period
	CheckDuplicateApplication(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, periodStart, periodEnd time.Time) (*domainCreditGrantApplication.CreditGrantApplication, error)
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

// ApplyCreditGrant applies a credit grant to a subscription and creates CGA tracking records
// This method handles both one-time and recurring credit grants
func (s *creditGrantService) ApplyCreditGrant(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, reason string, metadata types.Metadata) (*domainCreditGrantApplication.CreditGrantApplication, error) {

	// check if the credit grant is already applied for this period
	_, periodEnd, err := s.calculateNextPeriod(grant, subscription, subscription.CurrentPeriodStart)
	if err != nil {
		return nil, err
	}

	// Generate idempotency key first to check for duplicates
	idempotencyKey := s.generateIdempotencyKey(grant, subscription, subscription.CurrentPeriodStart, periodEnd)

	// Check if already exists for this period (idempotency protection)
	existing, err := s.CreditGrantApplicationRepo.FindByIdempotencyKey(ctx, idempotencyKey)
	if err == nil && existing != nil {
		s.Logger.Debugw("credit grant application already exists for this period",
			"grant_id", grant.ID,
			"subscription_id", subscription.ID,
			"application_id", existing.ID,
			"status", existing.ApplicationStatus)
		return existing, nil
	}

	// Create CGA record for tracking FIRST (in PENDING status)
	cga := &domainCreditGrantApplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   grant.ID,
		SubscriptionID:                  subscription.ID,
		ScheduledFor:                    time.Now().UTC(),
		PeriodStart:                     subscription.CurrentPeriodStart,
		PeriodEnd:                       periodEnd,
		ApplicationStatus:               types.ApplicationStatusPending,
		Currency:                        subscription.Currency,
		ApplicationReason:               reason,
		SubscriptionStatusAtApplication: string(subscription.SubscriptionStatus),
		RetryCount:                      0,
		CreditsApplied:                  decimal.Zero,
		Metadata:                        metadata,
		IdempotencyKey:                  idempotencyKey,
		EnvironmentID:                   types.GetEnvironmentID(ctx),
		BaseModel:                       types.GetDefaultBaseModel(ctx),
	}

	// Create the CGA record FIRST to ensure tracking
	createErr := s.CreditGrantApplicationRepo.Create(ctx, cga)
	if createErr != nil {
		s.Logger.Errorw("failed to create CGA record", "error", createErr)
		return nil, createErr
	}

	// Now try to apply the credit grant with proper transaction tracking
	err = s.applyCreditToWallet(ctx, grant, subscription, cga.ID)
	now := time.Now().UTC()

	// Update the CGA record with the result
	if err != nil {
		// Mark as failed
		cga.ApplicationStatus = types.ApplicationStatusFailed
		failureReason := err.Error()
		cga.FailureReason = &failureReason
		nextRetry := now.Add(15 * time.Minute)
		cga.NextRetryAt = &nextRetry
	} else {
		// Mark as applied successfully
		cga.ApplicationStatus = types.ApplicationStatusApplied
		cga.AppliedAt = &now
		cga.CreditsApplied = grant.Credits
	}

	// Update the CGA record with final status
	updateErr := s.CreditGrantApplicationRepo.Update(ctx, cga)
	if updateErr != nil {
		s.Logger.Errorw("failed to update CGA record", "error", updateErr)
		// Don't return error here as credit was already applied/failed
	}

	// If this is a recurring grant and successfully applied, create next period application
	if err == nil && grant.Cadence == types.CreditGrantCadenceRecurring {
		nextErr := s.createNextPeriodApplication(ctx, grant, subscription, cga.PeriodEnd)
		if nextErr != nil {
			s.Logger.Errorw("failed to create next period application", "error", nextErr)
			// Don't fail the current application for this
		}
	}

	return cga, err
}

// applyCreditToWallet applies credit to the customer's wallet
func (s *creditGrantService) applyCreditToWallet(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, cgaID string) error {
	walletService := NewWalletService(s.ServiceParams)

	// Find or create wallet
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

	// Calculate expiry date
	var expiryDate *time.Time
	if grant.ExpirationType == types.CreditGrantExpiryTypeBillingCycle {
		expiryDate = &subscription.CurrentPeriodEnd
	} else if grant.ExpirationType == types.CreditGrantExpiryTypeDuration && grant.ExpirationDuration != nil {
		expiry := time.Now().AddDate(0, 0, *grant.ExpirationDuration)
		expiryDate = &expiry
	}

	// Apply credit to wallet using CGA ID as idempotency key
	topupReq := &dto.TopUpWalletRequest{
		CreditsToAdd:      grant.Credits,
		TransactionReason: types.TransactionReasonSubscriptionCredit,
		ExpiryDateUTC:     expiryDate,
		Priority:          grant.Priority,
		IdempotencyKey:    &cgaID, // Use CGA ID as idempotency key
		Metadata: map[string]string{
			"grant_id":        grant.ID,
			"subscription_id": subscription.ID,
			"cga_id":          cgaID,
			"reason":          "credit_grant_application",
		},
	}

	_, err = walletService.TopUpWallet(ctx, selectedWallet.ID, topupReq)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to apply credit grant to wallet").
			WithReportableDetails(map[string]interface{}{
				"grant_id":        grant.ID,
				"subscription_id": subscription.ID,
				"wallet_id":       selectedWallet.ID,
				"cga_id":          cgaID,
			}).
			Mark(ierr.ErrDatabase)
	}

	s.Logger.Infow("successfully applied credit grant",
		"grant_id", grant.ID,
		"subscription_id", subscription.ID,
		"wallet_id", selectedWallet.ID,
		"credits_applied", grant.Credits,
		"cga_id", cgaID,
	)

	return nil
}

// NOTE: this is the main function that will be used to process scheduled credit grant applications
// this function will be called by the scheduler every 15 minutes and should not be used for other purposes
func (s *creditGrantService) ProcessScheduledCreditGrantApplications(ctx context.Context) error {
	// Find all scheduled applications
	applications, err := s.CreditGrantApplicationRepo.FindAllScheduledApplications(ctx)
	if err != nil {
		return err
	}

	s.Logger.Infow("found %d scheduled credit grant applications to process", "count", len(applications))

	// Process each application
	for _, cga := range applications {
		// Skip if already applied
		if cga.ApplicationStatus == types.ApplicationStatusApplied {
			continue
		}

		// Set tenant and environment context
		ctxWithTenant := context.WithValue(ctx, types.CtxTenantID, cga.TenantID)
		ctxWithEnv := context.WithValue(ctxWithTenant, types.CtxEnvironmentID, cga.EnvironmentID)

		err := s.processScheduledApplication(ctxWithEnv, cga)
		if err != nil {
			s.Logger.Errorw("failed to process scheduled application",
				"application_id", cga.ID,
				"grant_id", cga.CreditGrantID,
				"subscription_id", cga.SubscriptionID,
				"error", err)
		}
	}

	return nil
}

// processScheduledApplication processes a single scheduled credit grant application
func (s *creditGrantService) processScheduledApplication(
	ctx context.Context,
	cga *domainCreditGrantApplication.CreditGrantApplication,
) error {
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	creditGrantService := NewCreditGrantService(s.ServiceParams)

	// Get subscription
	subscription, err := subscriptionService.GetSubscription(ctx, cga.SubscriptionID)
	if err != nil {
		s.Logger.Errorw("failed to get subscription", "subscription_id", cga.SubscriptionID, "error", err)
		return err
	}

	// Get credit grant
	creditGrant, err := creditGrantService.GetCreditGrant(ctx, cga.CreditGrantID)
	if err != nil {
		s.Logger.Errorw("failed to get credit grant", "credit_grant_id", cga.CreditGrantID, "error", err)
		return err
	}

	// Check if credit grant is published
	if creditGrant.CreditGrant.Status != types.StatusPublished {
		s.Logger.Debugw("credit grant is not published, skipping", "credit_grant_id", cga.CreditGrantID)
		return nil
	}

	// If exists and applied successfully, skip
	if cga.ApplicationStatus == types.ApplicationStatusApplied {
		s.Logger.Debugw("grant already applied successfully, skipping", "application_id", cga.ID)
		return nil
	}

	// If exists and failed, retry
	if cga.ApplicationStatus == types.ApplicationStatusFailed {
		return s.retryFailedApplication(ctx, cga, creditGrant.CreditGrant, subscription.Subscription)
	}

	// check if the credit grant is already applied for this period using proper CGA tracking
	existingApp, err := s.CheckDuplicateApplication(ctx, creditGrant.CreditGrant, subscription.Subscription, cga.PeriodStart, cga.PeriodEnd)
	if err != nil {
		s.Logger.Errorw("failed to check for duplicate application", "error", err)
		return err
	}

	if existingApp != nil && existingApp.ApplicationStatus == types.ApplicationStatusApplied {
		s.Logger.Debugw("grant already applied for this period, skipping",
			"application_id", cga.ID,
			"existing_application_id", existingApp.ID)
		return nil
	}

	// Apply the grant
	return s.applyScheduledGrant(ctx, creditGrant.CreditGrant, subscription.Subscription, cga)
}

// applyScheduledGrant applies a scheduled credit grant
func (s *creditGrantService) applyScheduledGrant(
	ctx context.Context,
	grant *creditgrant.CreditGrant,
	subscription *subscription.Subscription,
	cga *domainCreditGrantApplication.CreditGrantApplication,
) error {
	// Check subscription state
	stateHandler := NewSubscriptionStateHandler(subscription, grant)
	action, reason := stateHandler.DetermineAction()

	if action != StateActionApply {
		s.Logger.Debugw("skipping grant application due to subscription state",
			"subscription_id", subscription.ID,
			"subscription_status", subscription.SubscriptionStatus,
			"grant_id", grant.ID,
			"reason", reason)
		return nil
	}

	// Apply the credit using the scheduled CGA's idempotency key
	err := s.applyCreditToWallet(ctx, grant, subscription, cga.ID)
	now := time.Now().UTC()

	// Update the original CGA
	if err != nil {
		cga.ApplicationStatus = types.ApplicationStatusFailed
		failureReason := err.Error()
		cga.FailureReason = &failureReason
		nextRetry := now.Add(15 * time.Minute)
		cga.NextRetryAt = lo.ToPtr(nextRetry)
	} else {
		cga.ApplicationStatus = types.ApplicationStatusApplied
		cga.AppliedAt = lo.ToPtr(now)
		cga.CreditsApplied = grant.Credits
	}

	updateErr := s.CreditGrantApplicationRepo.Update(ctx, cga)
	if updateErr != nil {
		s.Logger.Errorw("failed to update CGA", "application_id", cga.ID, "error", updateErr)
	}

	// If successful and recurring, create next period application
	if err == nil && grant.Cadence == types.CreditGrantCadenceRecurring {
		nextErr := s.createNextPeriodApplication(ctx, grant, subscription, cga.PeriodEnd)
		if nextErr != nil {
			s.Logger.Errorw("failed to create next period application", "error", nextErr)
		}
	}

	return err
}

// retryFailedApplication retries a failed credit grant application
func (s *creditGrantService) retryFailedApplication(ctx context.Context, cga *domainCreditGrantApplication.CreditGrantApplication, grant *creditgrant.CreditGrant, subscription *subscription.Subscription) error {
	// Update retry count
	cga.RetryCount++
	cga.ApplicationStatus = types.ApplicationStatusPending
	cga.FailureReason = nil
	cga.NextRetryAt = nil

	// Try to apply the grant
	err := s.applyCreditToWallet(ctx, grant, subscription, cga.ID)
	now := time.Now().UTC()

	if err != nil {
		// Mark as failed and set next retry time with exponential backoff
		cga.ApplicationStatus = types.ApplicationStatusFailed
		failureReason := err.Error()
		cga.FailureReason = &failureReason

		// Improved exponential backoff: 15min, 30min, 1hr, 2hr, 4hr (max)
		retryCount := cga.RetryCount
		if retryCount > 4 {
			retryCount = 4 // Cap at 4 retries = 4 hours max backoff
		}
		backoffMinutes := 15 * (1 << retryCount)
		nextRetry := now.Add(time.Duration(backoffMinutes) * time.Minute)
		cga.NextRetryAt = &nextRetry

		s.Logger.Errorw("credit grant application failed, scheduling retry",
			"application_id", cga.ID,
			"retry_count", cga.RetryCount,
			"next_retry_at", nextRetry,
			"error", err)
	} else {
		// Mark as applied successfully
		cga.ApplicationStatus = types.ApplicationStatusApplied
		cga.AppliedAt = &now
		cga.CreditsApplied = grant.Credits

		s.Logger.Infow("credit grant application succeeded",
			"application_id", cga.ID,
			"grant_id", grant.ID,
			"subscription_id", cga.SubscriptionID,
			"credits_applied", grant.Credits)
	}

	// Update the application
	updateErr := s.CreditGrantApplicationRepo.Update(ctx, cga)
	if updateErr != nil {
		s.Logger.Errorw("failed to update application", "application_id", cga.ID, "error", updateErr)
		return updateErr
	}

	// If successful and recurring, create next period application
	if err == nil && grant.Cadence == types.CreditGrantCadenceRecurring {
		nextErr := s.createNextPeriodApplication(ctx, grant, subscription, cga.PeriodEnd)
		if nextErr != nil {
			s.Logger.Errorw("failed to create next period application", "error", nextErr)
		}
	}

	return err
}

// createNextPeriodApplication creates a new CGA entry with scheduled status for the next period
func (s *creditGrantService) createNextPeriodApplication(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, currentPeriodEnd time.Time) error {
	// Calculate next period dates
	nextPeriodStart, nextPeriodEnd, err := s.calculateNextPeriod(grant, subscription, currentPeriodEnd)
	if err != nil {
		s.Logger.Errorw("failed to calculate next period",
			"grant_id", grant.ID,
			"subscription_id", subscription.ID,
			"current_period_end", currentPeriodEnd,
			"error", err)
		return err
	}

	// Create next period CGA
	nextPeriodCGA := &domainCreditGrantApplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   grant.ID,
		SubscriptionID:                  subscription.ID,
		ScheduledFor:                    nextPeriodStart,
		PeriodStart:                     nextPeriodStart,
		PeriodEnd:                       nextPeriodEnd,
		ApplicationStatus:               types.ApplicationStatusScheduled,
		CreditsApplied:                  decimal.Zero,
		Currency:                        subscription.Currency,
		ApplicationReason:               "recurring_credit_grant_next_period",
		SubscriptionStatusAtApplication: string(subscription.SubscriptionStatus),
		RetryCount:                      0,
		IdempotencyKey:                  s.generateIdempotencyKey(grant, subscription, nextPeriodStart, nextPeriodEnd),
		EnvironmentID:                   types.GetEnvironmentID(ctx),
		BaseModel:                       types.GetDefaultBaseModel(ctx),
	}

	err = s.CreditGrantApplicationRepo.Create(ctx, nextPeriodCGA)
	if err != nil {
		s.Logger.Errorw("failed to create next period CGA",
			"next_period_start", nextPeriodStart,
			"next_period_end", nextPeriodEnd,
			"error", err)
		return err
	}

	s.Logger.Infow("created next period credit grant application",
		"grant_id", grant.ID,
		"subscription_id", subscription.ID,
		"next_period_start", nextPeriodStart,
		"next_period_end", nextPeriodEnd,
		"application_id", nextPeriodCGA.ID)

	return nil
}

// calculateNextPeriod calculates the next credit grant period using simplified logic
func (s *creditGrantService) calculateNextPeriod(grant *creditgrant.CreditGrant, subscription *subscription.Subscription, currentPeriodEnd time.Time) (time.Time, time.Time, error) {
	nextPeriodStart := currentPeriodEnd

	creditGrantPeriodConfig := map[types.CreditGrantPeriod]types.BillingPeriod{
		types.CREDIT_GRANT_PERIOD_ANNUAL:      types.BILLING_PERIOD_ANNUAL,
		types.CREDIT_GRANT_PERIOD_HALF_YEARLY: types.BILLING_PERIOD_HALF_YEAR,
		types.CREDIT_GRANT_PERIOD_QUARTER:     types.BILLING_PERIOD_QUARTER,
		types.CREDIT_GRANT_PERIOD_MONTHLY:     types.BILLING_PERIOD_MONTHLY,
		types.CREDIT_GRANT_PERIOD_WEEKLY:      types.BILLING_PERIOD_WEEKLY,
		types.CREDIT_GRANT_PERIOD_DAILY:       types.BILLING_PERIOD_DAILY,
	}

	creditGrantPeriod := creditGrantPeriodConfig[lo.FromPtr(grant.Period)]

	// Use credit grant-specific period if defined, otherwise use billing period
	if grant.Period != nil && grant.PeriodCount != nil {
		// get the anchor date
		anchor := s.getAnchorDate(grant, subscription)

		// get the next period end
		nextPeriodEnd, err := types.NextBillingDate(nextPeriodStart, anchor, *grant.PeriodCount, creditGrantPeriod)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}

		// return the next period start and end
		return nextPeriodStart, nextPeriodEnd, nil
	}

	// Fall back to billing alignment
	// this is the case where the credit grant period is not defined
	nextPeriodEnd, err := types.NextBillingDate(nextPeriodStart, subscription.BillingAnchor, subscription.BillingPeriodCount, subscription.BillingPeriod)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return nextPeriodStart, nextPeriodEnd, nil
}

// getAnchorDate determines the anchor date for credit grant calculations
func (s *creditGrantService) getAnchorDate(grant *creditgrant.CreditGrant, subscription *subscription.Subscription) time.Time {
	// If grant period matches billing period, use billing anchor
	if s.isAlignedWithBilling(grant, subscription) {
		return subscription.BillingAnchor
	}
	// Otherwise use grant creation date
	return grant.CreatedAt
}

// isAlignedWithBilling checks if credit grant should align with billing cycles
func (s *creditGrantService) isAlignedWithBilling(grant *creditgrant.CreditGrant, subscription *subscription.Subscription) bool {
	if grant.Period == nil {
		return true
	}

	// Simple mapping between billing and credit grant periods
	periodMap := map[types.BillingPeriod]types.CreditGrantPeriod{
		types.BILLING_PERIOD_DAILY:     types.CREDIT_GRANT_PERIOD_DAILY,
		types.BILLING_PERIOD_WEEKLY:    types.CREDIT_GRANT_PERIOD_WEEKLY,
		types.BILLING_PERIOD_MONTHLY:   types.CREDIT_GRANT_PERIOD_MONTHLY,
		types.BILLING_PERIOD_QUARTER:   types.CREDIT_GRANT_PERIOD_QUARTER,
		types.BILLING_PERIOD_HALF_YEAR: types.CREDIT_GRANT_PERIOD_HALF_YEARLY,
		types.BILLING_PERIOD_ANNUAL:    types.CREDIT_GRANT_PERIOD_ANNUAL,
	}

	expectedPeriod, exists := periodMap[subscription.BillingPeriod]
	return exists && *grant.Period == expectedPeriod
}

// generateIdempotencyKey creates a unique key for the credit grant application based on grant, subscription, and period
func (s *creditGrantService) generateIdempotencyKey(grant *creditgrant.CreditGrant, subscription *subscription.Subscription, periodStart, periodEnd time.Time) string {

	generator := idempotency.NewGenerator()

	return generator.GenerateKey(idempotency.ScopeCreditGrant, map[string]interface{}{
		"grant_id":        grant.ID,
		"subscription_id": subscription.ID,
		"period_start":    periodStart.UTC(),
		"period_end":      periodEnd.UTC(),
	})
}

// CheckDuplicateApplication checks if a credit grant application already exists for the given period
func (s *creditGrantService) CheckDuplicateApplication(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, periodStart, periodEnd time.Time) (*domainCreditGrantApplication.CreditGrantApplication, error) {
	// Generate idempotency key
	idempotencyKey := s.generateIdempotencyKey(grant, subscription, periodStart, periodEnd)

	// Check by idempotency key first
	existing, err := s.CreditGrantApplicationRepo.FindByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		s.Logger.Errorw("failed to check for existing application", "error", err)
		return nil, err
	}

	if existing != nil {
		return existing, nil
	}

	// Additional check using period overlap
	exists, err := s.CreditGrantApplicationRepo.ExistsForPeriod(ctx, grant.ID, subscription.ID, periodStart, periodEnd)
	if err != nil {
		s.Logger.Errorw("failed to check period existence", "error", err)
		return nil, err
	}

	if exists {
		s.Logger.Warnw("duplicate application detected but idempotency key mismatch",
			"grant_id", grant.ID,
			"subscription_id", subscription.ID,
			"period_start", periodStart,
			"period_end", periodEnd)
	}

	return nil, nil
}
