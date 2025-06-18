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
	"github.com/flexprice/flexprice/internal/sentry"
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

	// ProcessScheduledCreditGrantApplications processes scheduled credit grant applications
	ProcessScheduledCreditGrantApplications(ctx context.Context) (*dto.ProcessScheduledCreditGrantApplicationsResponse, error)

	// ApplyCreditGrant applies a credit grant to a subscription and creates CGA tracking records
	// This method handles both one-time and recurring credit grants
	ApplyCreditGrant(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, metadata types.Metadata) error
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
func (s *creditGrantService) ApplyCreditGrant(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, metadata types.Metadata) error {
	// Calculate credit grant period
	// We are using the subscription start date instead of the current period start
	// because the current period start is the start of the current billing cycle
	periodStart, periodEnd, err := s.calculateNextPeriod(grant, subscription.StartDate)
	if err != nil {
		return err
	}

	// Create CGA record for tracking
	cga := &domainCreditGrantApplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   grant.ID,
		SubscriptionID:                  subscription.ID,
		ScheduledFor:                    time.Now().UTC(),
		PeriodStart:                     periodStart,
		PeriodEnd:                       periodEnd,
		ApplicationStatus:               types.ApplicationStatusPending,
		ApplicationReason:               types.ApplicationReasonFirstTimeRecurringCreditGrant,
		SubscriptionStatusAtApplication: subscription.SubscriptionStatus,
		RetryCount:                      0,
		CreditsApplied:                  decimal.Zero,
		Metadata:                        metadata,
		IdempotencyKey:                  s.generateIdempotencyKey(grant, subscription, periodStart, periodEnd),
		EnvironmentID:                   types.GetEnvironmentID(ctx),
		BaseModel:                       types.GetDefaultBaseModel(ctx),
	}

	// Create CGA record first
	if err = s.CreditGrantApplicationRepo.Create(ctx, cga); err != nil {
		s.Logger.Errorw("failed to create CGA record", "error", err)
		return err
	}

	// Apply credit grant transaction (handles wallet, status update, and next period creation atomically)
	err = s.applyCreditGrantToWallet(ctx, grant, subscription, cga)

	return err
}

// applyCreditGrantToWallet applies credit grant in a complete transaction
// This function performs 3 main tasks atomically:
// 1. Apply credits to wallet
// 2. Update CGA status to applied
// 3. Create next period CGA if recurring
// If any task fails, all changes are rolled back and CGA is marked as failed
func (s *creditGrantService) applyCreditGrantToWallet(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, cga *domainCreditGrantApplication.CreditGrantApplication) error {
	walletService := NewWalletService(s.ServiceParams)

	// Find or create wallet outside of transaction for better error handling
	wallets, err := walletService.GetWalletsByCustomerID(ctx, subscription.CustomerID)
	if err != nil {
		return s.handleCreditGrantFailure(ctx, cga, err, "Failed to get wallet for top up")
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
			return s.handleCreditGrantFailure(ctx, cga, err, "Failed to create wallet for top up")
		}
	}

	// Calculate expiry date
	var expiryDate *time.Time

	if grant.ExpirationType == types.CreditGrantExpiryTypeNever {
		expiryDate = nil
	}

	if grant.ExpirationType == types.CreditGrantExpiryTypeDuration {
		if grant.ExpirationDurationUnit != nil && grant.ExpirationDuration != nil && *grant.ExpirationDuration > 0 {
			switch *grant.ExpirationDurationUnit {
			case types.CreditGrantExpiryDurationUnitDays:
				expiry := subscription.StartDate.AddDate(0, 0, *grant.ExpirationDuration)
				expiryDate = &expiry
			case types.CreditGrantExpiryDurationUnitWeeks:
				expiry := subscription.StartDate.AddDate(0, 0, *grant.ExpirationDuration*7)
				expiryDate = &expiry
			case types.CreditGrantExpiryDurationUnitMonths:
				expiry := subscription.StartDate.AddDate(0, *grant.ExpirationDuration, 0)
				expiryDate = &expiry
			case types.CreditGrantExpiryDurationUnitYears:
				expiry := subscription.StartDate.AddDate(*grant.ExpirationDuration, 0, 0)
				expiryDate = &expiry
			}
		}
	}

	if grant.ExpirationType == types.CreditGrantExpiryTypeBillingCycle {
		expiryDate = &subscription.CurrentPeriodEnd
	}

	// Prepare top-up request
	topupReq := &dto.TopUpWalletRequest{
		CreditsToAdd:      grant.Credits,
		TransactionReason: types.TransactionReasonSubscriptionCredit,
		ExpiryDateUTC:     expiryDate,
		Priority:          grant.Priority,
		IdempotencyKey:    &cga.ID,
		Metadata: map[string]string{
			"grant_id":        grant.ID,
			"subscription_id": subscription.ID,
			"cga_id":          cga.ID,
		},
	}

	// Execute all tasks in a single transaction
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Task 1: Apply credit to wallet
		_, err := walletService.TopUpWallet(txCtx, selectedWallet.ID, topupReq)
		if err != nil {
			return err
		}

		// Task 2: Update CGA status to applied
		cga.ApplicationStatus = types.ApplicationStatusApplied
		cga.AppliedAt = lo.ToPtr(time.Now().UTC())
		cga.CreditsApplied = grant.Credits
		cga.FailureReason = nil // Clear any previous failure reason

		if err := s.CreditGrantApplicationRepo.Update(txCtx, cga); err != nil {
			return err
		}

		// Task 3: Create next period application if recurring
		if grant.Cadence == types.CreditGrantCadenceRecurring {
			if err := s.createNextPeriodApplication(txCtx, grant, subscription, cga.PeriodEnd); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to create next period credit grant application").
					WithReportableDetails(map[string]interface{}{
						"grant_id":        grant.ID,
						"subscription_id": subscription.ID,
						"current_cga_id":  cga.ID,
					}).
					Mark(ierr.ErrDatabase)
			}
		}

		return nil
	})

	// Handle transaction failure - rollback is automatic, but we need to update CGA status
	if err != nil {
		return s.handleCreditGrantFailure(ctx, cga, err, "Transaction failed during credit grant application")
	}

	// Log success
	s.Logger.Infow("Successfully applied credit grant transaction",
		"grant_id", grant.ID,
		"subscription_id", subscription.ID,
		"wallet_id", selectedWallet.ID,
		"credits_applied", grant.Credits,
		"cga_id", cga.ID,
		"is_recurring", grant.Cadence == types.CreditGrantCadenceRecurring,
	)

	return nil
}

// handleCreditGrantFailure handles failure by updating CGA status and logging
func (s *creditGrantService) handleCreditGrantFailure(
	ctx context.Context,
	cga *domainCreditGrantApplication.CreditGrantApplication,
	err error,
	hint string,
) error {
	// Log the primary error early for visibility
	s.Logger.Errorw("Credit grant application failed",
		"cga_id", cga.ID,
		"grant_id", cga.CreditGrantID,
		"subscription_id", cga.SubscriptionID,
		"hint", hint,
		"error", err)

	// Send to Sentry early
	sentrySvc := sentry.NewSentryService(s.Config, s.Logger)
	sentrySvc.CaptureException(err)

	// Prepare status update
	cga.ApplicationStatus = types.ApplicationStatusFailed
	cga.FailureReason = lo.ToPtr(err.Error())

	// Update in DB (log secondary error but return original)
	if updateErr := s.CreditGrantApplicationRepo.Update(ctx, cga); updateErr != nil {
		s.Logger.Errorw("Failed to update CGA after failure",
			"cga_id", cga.ID,
			"original_error", err.Error(),
			"update_error", updateErr.Error())
		return err // Preserve original context
	}

	// Return original error
	return err
}

// NOTE: this is the main function that will be used to process scheduled credit grant applications
// this function will be called by the scheduler every 15 minutes and should not be used for other purposes
func (s *creditGrantService) ProcessScheduledCreditGrantApplications(ctx context.Context) (*dto.ProcessScheduledCreditGrantApplicationsResponse, error) {
	// Find all scheduled applications
	applications, err := s.CreditGrantApplicationRepo.FindAllScheduledApplications(ctx)
	if err != nil {
		return nil, err
	}

	response := &dto.ProcessScheduledCreditGrantApplicationsResponse{
		SuccessApplicationsCount: 0,
		FailedApplicationsCount:  0,
		TotalApplicationsCount:   len(applications),
	}

	s.Logger.Infow("found %d scheduled credit grant applications to process", "count", len(applications))

	// Process each application
	for _, cga := range applications {
		// Set tenant and environment context
		ctxWithTenant := context.WithValue(ctx, types.CtxTenantID, cga.TenantID)
		ctxWithEnv := context.WithValue(ctxWithTenant, types.CtxEnvironmentID, cga.EnvironmentID)

		err := s.processScheduledApplication(ctxWithEnv, cga)
		if err != nil {
			s.Logger.Errorw("Failed to process scheduled application",
				"application_id", cga.ID,
				"grant_id", cga.CreditGrantID,
				"subscription_id", cga.SubscriptionID,
				"error", err)
			response.FailedApplicationsCount++
			continue
		}

		response.SuccessApplicationsCount++
		s.Logger.Debugw("Successfully processed scheduled application",
			"application_id", cga.ID,
			"grant_id", cga.CreditGrantID,
			"subscription_id", cga.SubscriptionID)
	}

	return response, nil
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
		s.Logger.Errorw("Failed to get subscription", "subscription_id", cga.SubscriptionID, "error", err)
		return err
	}

	// Get credit grant
	creditGrant, err := creditGrantService.GetCreditGrant(ctx, cga.CreditGrantID)
	if err != nil {
		s.Logger.Errorw("Failed to get credit grant", "credit_grant_id", cga.CreditGrantID, "error", err)
		return err
	}

	// Check if credit grant is published
	if creditGrant.CreditGrant.Status != types.StatusPublished {
		s.Logger.Debugw("Credit grant is not published, skipping", "credit_grant_id", cga.CreditGrantID)
		return nil
	}

	// If exists and failed, retry
	if cga.ApplicationStatus == types.ApplicationStatusFailed {
		s.Logger.Infow("Retrying failed credit grant application",
			"application_id", cga.ID,
			"grant_id", creditGrant.CreditGrant.ID,
			"subscription_id", subscription.ID)

		// Only increment retry count if application is failed as it applyCreditGrantToWallet will handle the status update as well as reset the failure reason
		cga.RetryCount++

		if err := s.CreditGrantApplicationRepo.Update(ctx, cga); err != nil {
			s.Logger.Errorw("Failed to update application after retry", "application_id", cga.ID, "error", err)
			return err
		}
	}

	// Apply the grant
	// Check subscription state
	stateHandler := NewSubscriptionStateHandler(subscription.Subscription, creditGrant.CreditGrant)
	action, reason := stateHandler.DetermineAction()

	switch action {
	case StateActionApply:
		// Apply credit grant transaction (handles wallet, status update, and next period creation atomically)
		err := s.applyCreditGrantToWallet(ctx, creditGrant.CreditGrant, subscription.Subscription, cga)
		if err != nil {
			s.Logger.Errorw("Failed to apply credit grant transaction", "application_id", cga.ID, "error", err)
			return err
		}

	case StateActionSkip:
		// Skip current period and create next period application if recurring
		err := s.skipCreditGrantApplication(ctx, cga, creditGrant.CreditGrant, subscription.Subscription)
		if err != nil {
			s.Logger.Errorw("Failed to skip credit grant application", "application_id", cga.ID, "error", err)
			return err
		}

	case StateActionDefer:
		// Defer until state changes - reschedule for later
		err := s.deferCreditGrantApplication(ctx, cga)
		if err != nil {
			s.Logger.Errorw("Failed to defer credit grant application", "application_id", cga.ID, "error", err)
			return err
		}

	case StateActionCancel:
		// Cancel all future applications for this grant and subscription
		err := s.cancelFutureCreditGrantApplications(ctx, creditGrant.CreditGrant, subscription.Subscription, cga, reason)
		if err != nil {
			s.Logger.Errorw("Failed to cancel future credit grant applications", "application_id", cga.ID, "error", err)
			return err
		}
	}

	return nil
}

// createNextPeriodApplication creates a new CGA entry with scheduled status for the next period
func (s *creditGrantService) createNextPeriodApplication(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, currentPeriodEnd time.Time) error {
	// Calculate next period dates
	nextPeriodStart, nextPeriodEnd, err := s.calculateNextPeriod(grant, currentPeriodEnd)
	if err != nil {
		s.Logger.Errorw("Failed to calculate next period",
			"grant_id", grant.ID,
			"subscription_id", subscription.ID,
			"current_period_end", currentPeriodEnd,
			"error", err)
		return err
	}

	// check if this cga is valid for the next period
	// for this subscription, is the next period end after the subscription end?
	if subscription.EndDate != nil && nextPeriodEnd.After(*subscription.EndDate) {
		s.Logger.Infow("Next period end is after subscription end, skipping", "grant_id", grant.ID, "subscription_id", subscription.ID)
		return nil
	}

	// Create next period CGA
	nextPeriodCGA := &domainCreditGrantApplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   grant.ID,
		SubscriptionID:                  subscription.ID,
		ScheduledFor:                    nextPeriodStart,
		PeriodStart:                     nextPeriodStart,
		PeriodEnd:                       nextPeriodEnd,
		ApplicationStatus:               types.ApplicationStatusPending,
		CreditsApplied:                  decimal.Zero,
		ApplicationReason:               types.ApplicationReasonRecurringCreditGrant,
		SubscriptionStatusAtApplication: subscription.SubscriptionStatus,
		RetryCount:                      0,
		IdempotencyKey:                  s.generateIdempotencyKey(grant, subscription, nextPeriodStart, nextPeriodEnd),
		EnvironmentID:                   types.GetEnvironmentID(ctx),
		BaseModel:                       types.GetDefaultBaseModel(ctx),
	}

	err = s.CreditGrantApplicationRepo.Create(ctx, nextPeriodCGA)
	if err != nil {
		s.Logger.Errorw("Failed to create next period CGA",
			"next_period_start", nextPeriodStart,
			"next_period_end", nextPeriodEnd,
			"error", err)
		return err
	}

	s.Logger.Infow("Created next period credit grant application",
		"grant_id", grant.ID,
		"subscription_id", subscription.ID,
		"next_period_start", nextPeriodStart,
		"next_period_end", nextPeriodEnd,
		"application_id", nextPeriodCGA.ID)

	return nil
}

// calculateNextPeriod calculates the next credit grant period using simplified logic
func (s *creditGrantService) calculateNextPeriod(grant *creditgrant.CreditGrant, nextPeriodStart time.Time) (time.Time, time.Time, error) {
	billingPeriod, err := types.GetBillingPeriodFromCreditGrantPeriod(lo.FromPtr(grant.Period))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	// Calculate next period end using the grant's creation date as anchor
	nextPeriodEnd, err := types.NextBillingDate(nextPeriodStart, grant.CreatedAt, lo.FromPtr(grant.PeriodCount), billingPeriod)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	return nextPeriodStart, nextPeriodEnd, nil
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

// skipCreditGrantApplication skips the current period and creates next period application if recurring
func (s *creditGrantService) skipCreditGrantApplication(
	ctx context.Context,
	cga *domainCreditGrantApplication.CreditGrantApplication,
	grant *creditgrant.CreditGrant,
	subscription *subscription.Subscription,
) error {
	// Log skip reason
	s.Logger.Infow("Skipping credit grant application",
		"application_id", cga.ID,
		"grant_id", cga.CreditGrantID,
		"subscription_id", cga.SubscriptionID,
		"subscription_status", cga.SubscriptionStatusAtApplication,
		"reason", cga.FailureReason)

	// Update current CGA status to skipped
	cga.ApplicationStatus = types.ApplicationStatusSkipped
	cga.FailureReason = cga.FailureReason

	err := s.CreditGrantApplicationRepo.Update(ctx, cga)
	if err != nil {
		s.Logger.Errorw("Failed to update CGA status to skipped", "application_id", cga.ID, "error", err)
		return err
	}

	// Create next period application if recurring
	if grant.Cadence == types.CreditGrantCadenceRecurring {
		return s.createNextPeriodApplication(ctx, grant, subscription, cga.PeriodEnd)
	}

	return nil
}

// deferCreditGrantApplication defers the application until subscription state changes
func (s *creditGrantService) deferCreditGrantApplication(
	ctx context.Context,
	cga *domainCreditGrantApplication.CreditGrantApplication,
) error {
	// Log defer reason
	s.Logger.Infow("Deferring credit grant application",
		"application_id", cga.ID,
		"grant_id", cga.CreditGrantID,
		"subscription_id", cga.SubscriptionID,
		"subscription_status", cga.SubscriptionStatusAtApplication,
		"reason", cga.FailureReason)

	// Calculate next retry time with exponential backoff (defer for 30 minutes initially)
	backoffMinutes := 30 * (1 << min(cga.RetryCount, 4))
	nextRetry := time.Now().UTC().Add(time.Duration(backoffMinutes) * time.Minute)

	// Update CGA with deferred status and next retry time
	cga.ScheduledFor = nextRetry

	err := s.CreditGrantApplicationRepo.Update(ctx, cga)
	if err != nil {
		s.Logger.Errorw("Failed to update CGA for deferral", "application_id", cga.ID, "error", err)
		return err
	}

	s.Logger.Infow("Credit grant application deferred",
		"application_id", cga.ID,
		"next_retry", nextRetry,
		"backoff_minutes", backoffMinutes)

	return nil
}

// cancelFutureCreditGrantApplications cancels all future applications for this grant and subscription
func (s *creditGrantService) cancelFutureCreditGrantApplications(
	ctx context.Context,
	grant *creditgrant.CreditGrant,
	subscription *subscription.Subscription,
	cga *domainCreditGrantApplication.CreditGrantApplication,
	reason string,
) error {
	// Log cancellation reason
	s.Logger.Infow("Cancelling future credit grant applications",
		"application_id", cga.ID,
		"grant_id", grant.ID,
		"subscription_id", subscription.ID,
		"subscription_status", subscription.SubscriptionStatus,
		"reason", reason)

	// Update current CGA status to cancelled
	cga.ApplicationStatus = types.ApplicationStatusCancelled
	cga.FailureReason = &reason

	err := s.CreditGrantApplicationRepo.Update(ctx, cga)
	if err != nil {
		s.Logger.Errorw("Failed to update CGA status to cancelled", "application_id", cga.ID, "error", err)
		return err
	}

	// Get all future pending applications
	pendingFilter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{grant.ID},
		SubscriptionIDs: []string{subscription.ID},
		ApplicationStatuses: []types.ApplicationStatus{
			types.ApplicationStatusPending,
			types.ApplicationStatusFailed,
		},
		QueryFilter: types.NewNoLimitQueryFilter(),
	}

	applications, err := s.CreditGrantApplicationRepo.List(ctx, pendingFilter)
	if err != nil {
		s.Logger.Errorw("Failed to fetch pending future applications", "error", err)
		return err
	}

	// Cancel each future application
	for _, app := range applications {
		app.ApplicationStatus = types.ApplicationStatusCancelled
		app.FailureReason = &reason

		err := s.CreditGrantApplicationRepo.Update(ctx, app)
		if err != nil {
			s.Logger.Errorw("Failed to cancel future application", "application_id", app.ID, "error", err)
			// Continue with other applications even if one fails
			continue
		}

		s.Logger.Infow("Cancelled future credit grant application",
			"application_id", app.ID,
			"scheduled_for", app.ScheduledFor)
	}

	s.Logger.Infow("Successfully cancelled future credit grant applications",
		"grant_id", grant.ID,
		"subscription_id", subscription.ID,
		"cancelled_count", len(applications))

	return nil
}
