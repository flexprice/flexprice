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

	// NOTE: THIS IS ONLY FOR CRON JOB SHOULD NOT BE USED ELSEWHERE IN OTHER WORKFLOWS
	// This runs every 15 mins
	// ProcessScheduledCreditGrantApplications processes scheduled credit grant applications
	ProcessScheduledCreditGrantApplications(ctx context.Context) (*dto.ProcessScheduledCreditGrantApplicationsResponse, error)

	// ApplyCreditGrant applies a credit grant to a subscription and creates CGA tracking records
	// This method handles both one-time and recurring credit grants
	ApplyCreditGrant(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, metadata types.Metadata) error

	// CreateScheduledCreditGrantApplication creates a CGA record without applying it
	// This is used when credit grants need to be scheduled for later processing (e.g., when subscription is incomplete)
	CreateScheduledCreditGrantApplication(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, metadata types.Metadata) (*domainCreditGrantApplication.CreditGrantApplication, error)

	// ApplyCreditGrantToWallet applies credit grant to wallet atomically
	// This handles wallet top-up, CGA status update, and next period creation
	ApplyCreditGrantToWallet(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, cga *domainCreditGrantApplication.CreditGrantApplication) error

	// CancelFutureCreditGrantsOfSubscription cancels all future credit grants for this subscription
	CancelFutureCreditGrantsOfSubscription(ctx context.Context, subscriptionID string) error

	// ListCreditGrantApplications retrieves credit grant applications based on filter
	ListCreditGrantApplications(ctx context.Context, filter *types.CreditGrantApplicationFilter) (*dto.ListCreditGrantApplicationsResponse, error)
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

		// check if subscription is cancelled
		if sub.SubscriptionStatus == types.SubscriptionStatusCancelled {
			return nil, ierr.NewError("subscription is cancelled").
				WithHint("Subscription is cancelled").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": *req.SubscriptionID,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// plan validation if plan_id is provided
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

		// check if plan is published
		if plan.Status != types.StatusPublished {
			return nil, ierr.NewError("plan is not published").
				WithHint("Plan is not published").
				WithReportableDetails(map[string]interface{}{
					"plan_id": *req.PlanID,
				}).
				Mark(ierr.ErrValidation)
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

	grant, err := s.CreditGrantRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	if grant.Status != types.StatusPublished {
		return ierr.NewError("credit grant is not in published status").
			WithHint("Credit grant is already archived").
			WithReportableDetails(map[string]interface{}{
				"credit_grant_id": id,
				"status":          grant.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	if err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		if grant.Scope == types.CreditGrantScopeSubscription && grant.SubscriptionID != nil {
			err = s.CancelFutureCreditGrantsOfSubscription(ctx, *grant.SubscriptionID)
			if err != nil {
				return err
			}
		}

		err = s.CreditGrantRepo.Delete(ctx, id)
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (s *creditGrantService) GetCreditGrantsByPlan(ctx context.Context, planID string) (*dto.ListCreditGrantsResponse, error) {
	// Create a filter for the plan's credit grants
	filter := types.NewNoLimitCreditGrantFilter()
	filter.PlanIDs = []string{planID}
	filter.WithStatus(types.StatusPublished)
	filter.Scope = lo.ToPtr(types.CreditGrantScopePlan)

	// Use the standard list function to get the credit grants with expansion
	return s.ListCreditGrants(ctx, filter)
}

func (s *creditGrantService) GetCreditGrantsBySubscription(ctx context.Context, subscriptionID string) (*dto.ListCreditGrantsResponse, error) {
	// Create a filter for the subscription's credit grants
	filter := types.NewNoLimitCreditGrantFilter()
	filter.SubscriptionIDs = []string{subscriptionID}
	filter.WithStatus(types.StatusPublished)
	filter.Scope = lo.ToPtr(types.CreditGrantScopeSubscription)

	// Use the standard list function to get the credit grants with expansion
	resp, err := s.ListCreditGrants(ctx, filter)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// CreateScheduledCreditGrantApplication creates a CGA record without applying it
// This is used when credit grants need to be scheduled for later processing (e.g., when subscription is incomplete)
func (s *creditGrantService) CreateScheduledCreditGrantApplication(
	ctx context.Context,
	grant *creditgrant.CreditGrant,
	subscription *subscription.Subscription,
	metadata types.Metadata,
) (*domainCreditGrantApplication.CreditGrantApplication, error) {
	// Calculate credit grant period based on cadence
	var periodStart, periodEnd time.Time
	var err error

	if grant.Cadence == types.CreditGrantCadenceRecurring {
		// For recurring grants, calculate proper period dates
		periodStart, periodEnd, err = s.calculateNextPeriod(grant, subscription.StartDate, subscription.EndDate)
		if err != nil {
			return nil, err
		}
	}

	// Create CGA record for tracking
	var applicationReason types.CreditGrantApplicationReason
	if grant.Cadence == types.CreditGrantCadenceRecurring {
		applicationReason = types.ApplicationReasonFirstTimeRecurringCreditGrant
	} else {
		applicationReason = types.ApplicationReasonOnetimeCreditGrant
	}

	// Schedule for subscription start date, or now if subscription start is in the past
	scheduledFor := subscription.StartDate
	if scheduledFor.Before(time.Now().UTC()) {
		scheduledFor = time.Now().UTC()
	}

	cga := &domainCreditGrantApplication.CreditGrantApplication{
		ID:                              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CREDIT_GRANT_APPLICATION),
		CreditGrantID:                   grant.ID,
		SubscriptionID:                  subscription.ID,
		ScheduledFor:                    scheduledFor,
		PeriodStart:                     lo.ToPtr(periodStart),
		PeriodEnd:                       lo.ToPtr(periodEnd),
		ApplicationStatus:               types.ApplicationStatusPending,
		ApplicationReason:               applicationReason,
		SubscriptionStatusAtApplication: subscription.SubscriptionStatus,
		RetryCount:                      0,
		Credits:                         grant.Credits,
		Metadata:                        metadata,
		IdempotencyKey:                  s.generateIdempotencyKey(grant, subscription, periodStart, periodEnd),
		EnvironmentID:                   types.GetEnvironmentID(ctx),
		BaseModel:                       types.GetDefaultBaseModel(ctx),
	}

	// Create CGA record
	if err = s.CreditGrantApplicationRepo.Create(ctx, cga); err != nil {
		s.Logger.Errorw("failed to create scheduled CGA record", "error", err)
		return nil, err
	}

	return cga, nil
}

// ApplyCreditGrant applies a credit grant to a subscription and creates CGA tracking records
// This method handles both one-time and recurring credit grants
func (s *creditGrantService) ApplyCreditGrant(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, metadata types.Metadata) error {

	// Validate credit grant
	if err := grant.Validate(); err != nil {
		return err
	}

	// Create CGA record for tracking
	cga, err := s.CreateScheduledCreditGrantApplication(ctx, grant, subscription, metadata)
	if err != nil {
		return err
	}

	// Apply credit grant transaction (handles wallet, status update, and next period creation atomically)
	err = s.ApplyCreditGrantToWallet(ctx, grant, subscription, cga)

	return err
}

// ApplyCreditGrantToWallet applies credit grant in a complete transaction
// This function performs 3 main tasks atomically:
// 1. Apply credits to wallet
// 2. Update CGA status to applied
// 3. Create next period CGA if recurring
// If any task fails, all changes are rolled back and CGA is marked as failed
func (s *creditGrantService) ApplyCreditGrantToWallet(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, cga *domainCreditGrantApplication.CreditGrantApplication) error {
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
			Config: &types.WalletConfig{
				AllowedPriceTypes: []types.WalletConfigPriceType{
					types.WalletConfigPriceTypeUsage,
				},
			},
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
		if grant.ExpirationDurationUnit != nil && grant.ExpirationDuration != nil && lo.FromPtr(grant.ExpirationDuration) > 0 {
			switch lo.FromPtr(grant.ExpirationDurationUnit) {
			case types.CreditGrantExpiryDurationUnitDays:
				expiry := subscription.StartDate.AddDate(0, 0, lo.FromPtr(grant.ExpirationDuration))
				expiryDate = &expiry
			case types.CreditGrantExpiryDurationUnitWeeks:
				expiry := subscription.StartDate.AddDate(0, 0, lo.FromPtr(grant.ExpirationDuration)*7)
				expiryDate = &expiry
			case types.CreditGrantExpiryDurationUnitMonths:
				expiry := subscription.StartDate.AddDate(0, lo.FromPtr(grant.ExpirationDuration), 0)
				expiryDate = &expiry
			case types.CreditGrantExpiryDurationUnitYears:
				expiry := subscription.StartDate.AddDate(lo.FromPtr(grant.ExpirationDuration), 0, 0)
				expiryDate = &expiry
			default:
				return ierr.NewError("invalid expiration duration unit").
					WithHint("Please provide a valid expiration duration unit").
					WithReportableDetails(map[string]interface{}{
						"expiration_duration_unit": grant.ExpirationDurationUnit,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	}

	if grant.ExpirationType == types.CreditGrantExpiryTypeBillingCycle {
		expiryDate = &subscription.CurrentPeriodEnd
	}

	// Prepare top-up request
	topupReq := &dto.TopUpWalletRequest{
		CreditsToAdd:      cga.Credits,
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
		cga.FailureReason = nil // Clear any previous failure reason

		if err := s.CreditGrantApplicationRepo.Update(txCtx, cga); err != nil {
			return err
		}

		// Task 3: Create next period application if recurring
		if grant.Cadence == types.CreditGrantCadenceRecurring {
			if err := s.createNextPeriodApplication(txCtx, grant, subscription, lo.FromPtr(cga.PeriodEnd)); err != nil {
				return err
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
		"credits_applied", cga.Credits,
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
		// We are not updating the CGA status here as it will be updated by methods following every step

	}

	// Apply the grant
	// Check subscription state
	stateHandler := NewSubscriptionStateHandler(subscription.Subscription, creditGrant.CreditGrant)
	action, err := stateHandler.DetermineCreditGrantAction()

	if err != nil {
		s.Logger.Errorw("Failed to determine action", "application_id", cga.ID, "error", err)
		return err
	}

	switch action {
	case StateActionApply:
		// Apply credit grant transaction (handles wallet, status update, and next period creation atomically)
		err := s.ApplyCreditGrantToWallet(ctx, creditGrant.CreditGrant, subscription.Subscription, cga)
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
		err := s.cancelFutureCreditGrantApplications(ctx, creditGrant.CreditGrant, subscription.Subscription, cga)
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
	nextPeriodStart, nextPeriodEnd, err := s.calculateNextPeriod(grant, currentPeriodEnd, subscription.EndDate)
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
		PeriodStart:                     lo.ToPtr(nextPeriodStart),
		PeriodEnd:                       lo.ToPtr(nextPeriodEnd),
		ApplicationStatus:               types.ApplicationStatusPending,
		Credits:                         grant.Credits,
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
func (s *creditGrantService) calculateNextPeriod(grant *creditgrant.CreditGrant, nextPeriodStart time.Time, subscriptionEndDate *time.Time) (time.Time, time.Time, error) {
	billingPeriod, err := types.GetBillingPeriodFromCreditGrantPeriod(lo.FromPtr(grant.Period))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	// Calculate next period end using the grant's creation date as anchor
	nextPeriodEnd, err := types.NextBillingDate(nextPeriodStart, grant.CreatedAt, lo.FromPtr(grant.PeriodCount), billingPeriod, subscriptionEndDate)
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

	err := s.CreditGrantApplicationRepo.Update(ctx, cga)
	if err != nil {
		s.Logger.Errorw("Failed to update CGA status to skipped", "application_id", cga.ID, "error", err)
		return err
	}

	// Create next period application if recurring
	if grant.Cadence == types.CreditGrantCadenceRecurring {
		// Create next period application if recurring
		err := s.createNextPeriodApplication(ctx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
		if err != nil {
			s.Logger.Errorw("Failed to create next period application", "application_id", cga.ID, "error", err)
			return err
		}
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

	// Update CGA with deferred status and next retry time and increment retry count so that next time it will be deferred for longer
	cga.ScheduledFor = nextRetry
	cga.RetryCount++

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
) error {
	// Log cancellation reason
	s.Logger.Infow("Cancelling future credit grant applications",
		"application_id", cga.ID,
		"grant_id", grant.ID,
		"subscription_id", subscription.ID,
		"subscription_status", subscription.SubscriptionStatus,
		"reason", cga.FailureReason)

	// Update current CGA status to cancelled
	cga.ApplicationStatus = types.ApplicationStatusCancelled

	if err := s.CreditGrantApplicationRepo.Update(ctx, cga); err != nil {
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

	// Cancel each future application in a transaction
	if err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Cancel each future application
		for _, app := range applications {
			app.ApplicationStatus = types.ApplicationStatusCancelled

			err := s.CreditGrantApplicationRepo.Update(ctx, app)
			if err != nil {
				s.Logger.Errorw("Failed to cancel future application", "application_id", app.ID, "error", err)
				// Continue with other applications even if one fails
				// As even if one fails to update when it gets picked up by the scheduler it will be retried and cancelled
				continue
			}

			s.Logger.Infow("Cancelled future credit grant application",
				"application_id", app.ID,
				"scheduled_for", app.ScheduledFor)
		}

		return nil
	}); err != nil {
		return err
	}

	s.Logger.Infow("Successfully cancelled future credit grant applications",
		"grant_id", grant.ID,
		"subscription_id", subscription.ID,
		"cancelled_count", len(applications))

	return nil
}

func (s *creditGrantService) CancelFutureCreditGrantsOfSubscription(ctx context.Context, subscriptionID string) error {

	// get subscription
	subscription, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		s.Logger.Errorw("Failed to fetch subscription", "error", err)
		return err
	}

	// get all credit grants for this subscription
	creditGrants, err := s.CreditGrantRepo.GetBySubscription(ctx, subscriptionID)
	if err != nil {
		s.Logger.Errorw("Failed to fetch credit grants", "error", err)
		return err
	}

	// Collect all applications to cancel
	applicationsToCancel := make([]*domainCreditGrantApplication.CreditGrantApplication, 0)

	for _, creditGrant := range creditGrants {
		// get all pending future applications for this credit grant
		pendingFilter := &types.CreditGrantApplicationFilter{
			CreditGrantIDs:  []string{creditGrant.ID},
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

		applicationsToCancel = append(applicationsToCancel, applications...)

		// finally archive the credit grant
		err = s.CreditGrantRepo.Delete(ctx, creditGrant.ID)
		if err != nil {
			s.Logger.Errorw("Failed to archive credit grant", "error", err)
			return err
		}
	}

	// Cancel all applications within a transaction
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		for _, app := range applicationsToCancel {
			app.ApplicationStatus = types.ApplicationStatusCancelled
			app.Status = types.StatusArchived

			err := s.CreditGrantApplicationRepo.Update(txCtx, app)
			if err != nil {
				s.Logger.Errorw("Failed to cancel application",
					"application_id", app.ID,
					"grant_id", app.CreditGrantID,
					"subscription_id", app.SubscriptionID,
					"error", err)
				return err
			}

			s.Logger.Infow("Cancelled credit grant application",
				"application_id", app.ID,
				"grant_id", app.CreditGrantID,
				"subscription_id", app.SubscriptionID,
				"scheduled_for", app.ScheduledFor)
		}
		return nil
	})

	if err != nil {
		s.Logger.Errorw("Failed to cancel future credit grant applications", "error", err)
		return err
	}

	s.Logger.Infow("Successfully cancelled all future credit grant applications for subscription",
		"subscription_id", subscriptionID,
		"cancelled_count", len(applicationsToCancel),
		"subscription_status", subscription.SubscriptionStatus)

	return nil
}

func (s *creditGrantService) ListCreditGrantApplications(ctx context.Context, filter *types.CreditGrantApplicationFilter) (*dto.ListCreditGrantApplicationsResponse, error) {
	if filter == nil {
		filter = types.NewCreditGrantApplicationFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	applications, err := s.CreditGrantApplicationRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.CreditGrantApplicationRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &dto.ListCreditGrantApplicationsResponse{
		Items: make([]*dto.CreditGrantApplicationResponse, len(applications)),
	}

	for i, app := range applications {
		response.Items[i] = &dto.CreditGrantApplicationResponse{CreditGrantApplication: app}
	}

	response.Pagination = types.NewPaginationResponse(
		count,
		filter.GetLimit(),
		filter.GetOffset(),
	)

	return response, nil
}
