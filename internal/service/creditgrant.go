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

	// InitializeCreditGrantWorkflow initializes the workflow for a credit grant
	// This creates the first CGA and triggers eager application if applicable
	InitializeCreditGrantWorkflow(ctx context.Context, cg creditgrant.CreditGrant) (*creditgrant.CreditGrant, error)

	// ProcessCreditGrantApplication processes a single credit grant application
	// Use this to manually trigger processing of a pending/failed application
	ProcessCreditGrantApplication(ctx context.Context, applicationID string) error

	// CancelFutureSubscriptionGrants cancels all future credit grants for this subscription
	// This is typically used when a subscription is cancelled
	CancelFutureSubscriptionGrants(ctx context.Context, subscriptionID string) error

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

	// Validate plan if plan_id is provided (plan_id presence is validated in DTO)
	if req.PlanID != nil && lo.FromPtr(req.PlanID) != "" {
		plan, err := s.PlanRepo.Get(ctx, lo.FromPtr(req.PlanID))
		if err != nil {
			return nil, err
		}
		if plan == nil {
			return nil, ierr.NewError("plan not found").
				WithHint(fmt.Sprintf("Plan with ID %s does not exist", lo.FromPtr(req.PlanID))).
				WithReportableDetails(map[string]interface{}{
					"plan_id": lo.FromPtr(req.PlanID),
				}).
				Mark(ierr.ErrNotFound)
		}

		if plan.Status != types.StatusPublished {
			return nil, ierr.NewError("plan is not published").
				WithHint("Plan is not published").
				WithReportableDetails(map[string]interface{}{
					"plan_id": lo.FromPtr(req.PlanID),
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Validate based on scope
	switch req.Scope {
	case types.CreditGrantScopeSubscription:
		// Validate subscription exists and is not cancelled (subscription_id presence is validated in DTO)
		sub, err := s.SubRepo.Get(ctx, lo.FromPtr(req.SubscriptionID))
		if err != nil {
			return nil, err
		}
		if sub == nil {
			return nil, ierr.NewError("subscription not found").
				WithHint(fmt.Sprintf("Subscription with ID %s does not exist", lo.FromPtr(req.SubscriptionID))).
				WithReportableDetails(map[string]interface{}{
					"subscription_id": lo.FromPtr(req.SubscriptionID),
				}).
				Mark(ierr.ErrNotFound)
		}

		if sub.SubscriptionStatus == types.SubscriptionStatusCancelled {
			return nil, ierr.NewError("subscription is cancelled").
				WithHint("Subscription is cancelled").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": lo.FromPtr(req.SubscriptionID),
				}).
				Mark(ierr.ErrValidation)
		}

		// Validate credit grant dates against subscription dates (start_date presence is validated in DTO)
		if req.StartDate.Before(sub.StartDate) {
			return nil, ierr.NewError("credit grant start_date must be greater than or equal to subscription start_date").
				WithHint("Credit grant start date cannot be before subscription start date").
				WithReportableDetails(map[string]interface{}{
					"credit_grant_start_date": req.StartDate,
					"subscription_start_date": sub.StartDate,
				}).
				Mark(ierr.ErrValidation)
		}

		if req.EndDate != nil {
			if sub.EndDate != nil && req.EndDate.After(lo.FromPtr(sub.EndDate)) {
				return nil, ierr.NewError("credit grant end_date must be less than or equal to subscription end_date").
					WithHint("Credit grant end date cannot be after subscription end date").
					WithReportableDetails(map[string]interface{}{
						"credit_grant_end_date": req.EndDate,
						"subscription_end_date": sub.EndDate,
					}).
					Mark(ierr.ErrValidation)
			}
		}

		// Set credit grant anchor to start date if not provided
		if req.CreditGrantAnchor == nil && req.StartDate != nil {
			req.CreditGrantAnchor = req.StartDate
		}

		// Validate billing anchor against subscription dates
		if req.CreditGrantAnchor != nil {
			if req.CreditGrantAnchor.Before(sub.StartDate) {
				return nil, ierr.NewError("credit_grant_anchor must be greater than or equal to subscription start_date").
					WithHint("Credit grant anchor cannot be before subscription start date").
					WithReportableDetails(map[string]interface{}{
						"credit_grant_anchor":     req.CreditGrantAnchor,
						"subscription_start_date": sub.StartDate,
					}).
					Mark(ierr.ErrValidation)
			}
			if sub.EndDate != nil && req.CreditGrantAnchor.After(lo.FromPtr(sub.EndDate)) {
				return nil, ierr.NewError("credit_grant_anchor must be less than or equal to subscription end_date").
					WithHint("Credit grant anchor cannot be after subscription end date").
					WithReportableDetails(map[string]interface{}{
						"credit_grant_anchor":   req.CreditGrantAnchor,
						"subscription_end_date": sub.EndDate,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	}

	// Create credit grant
	cg := req.ToCreditGrant(ctx)
	cg, err := s.CreditGrantRepo.Create(ctx, cg)
	if err != nil {
		return nil, err
	}

	// Initialize workflow
	if cg.Scope == types.CreditGrantScopeSubscription {
		cg, err = s.InitializeCreditGrantWorkflow(ctx, lo.FromPtr(cg))
		if err != nil {
			return nil, err
		}
	}

	response := &dto.CreditGrantResponse{CreditGrant: cg}
	return response, nil
}

func (s *creditGrantService) InitializeCreditGrantWorkflow(ctx context.Context, cg creditgrant.CreditGrant) (*creditgrant.CreditGrant, error) {

	// 3. Calculate period for the first application
	periodStart := lo.FromPtr(cg.StartDate)
	var periodEnd *time.Time = nil

	if cg.Cadence == types.CreditGrantCadenceRecurring {
		var err error
		var calculatedPeriodEnd time.Time
		_, calculatedPeriodEnd, err = CalculateNextCreditGrantPeriod(cg, periodStart)
		if err != nil {
			return nil, err
		}
		periodEnd = lo.ToPtr(calculatedPeriodEnd)
	}

	// 4. Fetch subscription to get current status for subscription_status_at_application
	var subscriptionStatus types.SubscriptionStatus
	if cg.SubscriptionID != nil && lo.FromPtr(cg.SubscriptionID) != "" {
		sub, err := s.SubRepo.Get(ctx, lo.FromPtr(cg.SubscriptionID))
		if err != nil {
			return nil, err
		}
		subscriptionStatus = sub.SubscriptionStatus
	}

	// 5. Create the first CGA record in Pending status
	var applicationReason types.CreditGrantApplicationReason
	if cg.Cadence == types.CreditGrantCadenceRecurring {
		applicationReason = types.ApplicationReasonFirstTimeRecurringCreditGrant
	} else {
		applicationReason = types.ApplicationReasonOnetimeCreditGrant
	}

	cgaReq := dto.CreateCreditGrantApplicationRequest{
		CreditGrantID:                   cg.ID,
		SubscriptionID:                  lo.FromPtr(cg.SubscriptionID),
		ScheduledFor:                    periodStart,
		PeriodStart:                     lo.ToPtr(periodStart),
		PeriodEnd:                       periodEnd,
		ApplicationReason:               applicationReason,
		Credits:                         cg.Credits,
		SubscriptionStatusAtApplication: subscriptionStatus,
		IdempotencyKey:                  s.generateIdempotencyKey(lo.ToPtr(cg), lo.ToPtr(periodStart), periodEnd),
	}

	if err := cgaReq.Validate(); err != nil {
		return nil, err
	}

	cga := cgaReq.ToCreditGrantApplication(ctx)
	if err := s.CreditGrantApplicationRepo.Create(ctx, cga); err != nil {
		s.Logger.Errorw("failed to create initial CGA record", "error", err, "grant_id", cg.ID)
		return nil, err
	}

	// 6. Eager application: if the grant is due now or in the past, process it immediately
	if cg.CreditGrantAnchor.Before(time.Now()) || cg.CreditGrantAnchor.Equal(time.Now()) {
		// Use a background context or a sub-context to avoid blocking the main creation if processing fails
		// Though for initialization, we might want it to be synchronous if it fails due to validation
		if err := s.processScheduledApplication(ctx, cga); err != nil {
			s.Logger.Errorw("failed to process initial CGA eagerly", "error", err, "cga_id", cga.ID)
		}
	}

	return lo.ToPtr(cg), nil
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
		// Only cancel future applications for this specific grant
		if err := s.cancelFutureGrantApplications(ctx, grant); err != nil {
			return err
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

func (s *creditGrantService) ProcessCreditGrantApplication(ctx context.Context, applicationID string) error {
	cga, err := s.CreditGrantApplicationRepo.Get(ctx, applicationID)
	if err != nil {
		return err
	}
	return s.processScheduledApplication(ctx, cga)
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
	// IMPORTANT: For recurring grants, expiry should be calculated from when the credit is actually granted,
	// not from subscription start date. This ensures each grant has its own expiry timeline.
	var expiryDate *time.Time
	effectiveDate := cga.PeriodStart

	if grant.ExpirationType == types.CreditGrantExpiryTypeNever {
		expiryDate = nil
	}

	if grant.ExpirationType == types.CreditGrantExpiryTypeDuration {
		if grant.ExpirationDurationUnit != nil && grant.ExpirationDuration != nil && lo.FromPtr(grant.ExpirationDuration) > 0 {

			switch lo.FromPtr(grant.ExpirationDurationUnit) {
			case types.CreditGrantExpiryDurationUnitDays:
				expiry := effectiveDate.AddDate(0, 0, lo.FromPtr(grant.ExpirationDuration))
				expiryDate = lo.ToPtr(expiry)
			case types.CreditGrantExpiryDurationUnitWeeks:
				expiry := effectiveDate.AddDate(0, 0, lo.FromPtr(grant.ExpirationDuration)*7)
				expiryDate = lo.ToPtr(expiry)
			case types.CreditGrantExpiryDurationUnitMonths:
				expiry := effectiveDate.AddDate(0, lo.FromPtr(grant.ExpirationDuration), 0)
				expiryDate = lo.ToPtr(expiry)
			case types.CreditGrantExpiryDurationUnitYears:
				expiry := effectiveDate.AddDate(lo.FromPtr(grant.ExpirationDuration), 0, 0)
				expiryDate = lo.ToPtr(expiry)
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
		// For billing cycle expiry, use the current period end when the grant is applied
		// For recurring grants, this will be the period end for the current billing period
		expiryDate = lo.ToPtr(subscription.CurrentPeriodEnd)
	}

	// Prepare top-up request
	topupReq := &dto.TopUpWalletRequest{
		CreditsToAdd:      cga.Credits,
		TransactionReason: types.TransactionReasonSubscriptionCredit,
		ExpiryDateUTC:     expiryDate,
		Priority:          grant.Priority,
		IdempotencyKey:    lo.ToPtr(cga.ID),
		Metadata: map[string]string{
			"grant_id":        grant.ID,
			"subscription_id": subscription.ID,
			"cga_id":          cga.ID,
		},
	}

	// Execute all tasks in a single transaction
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Task 0: Expiry Check
		// If the grant has an expiration duration, check if it has already expired relative to current time
		if expiryDate != nil {
			if expiryDate.Before(time.Now().UTC()) {
				s.Logger.Infow("Credit grant application period has already expired, skipping application",
					"cga_id", cga.ID,
					"grant_id", grant.ID,
					"expiry_date", expiryDate)

				cga.ApplicationStatus = types.ApplicationStatusSkipped
				cga.FailureReason = lo.ToPtr("Expired")
				cga.AppliedAt = nil // Ensure applied_at is nil for skipped grants
				if err := s.CreditGrantApplicationRepo.Update(txCtx, cga); err != nil {
					return err
				}

				// Even if skipped due to expiry, we create the next period application for recurring grants
				if grant.Cadence == types.CreditGrantCadenceRecurring {
					if err := s.createNextPeriodApplication(txCtx, grant, subscription, lo.FromPtr(cga.PeriodEnd)); err != nil {
						return err
					}
				}
				return nil
			}
		}

		// Task 1: Apply credit to wallet
		_, err := walletService.TopUpWallet(txCtx, selectedWallet.ID, topupReq)
		if err != nil {
			return err
		}

		// Task 2: Update CGA status to applied
		cga.ApplicationStatus = types.ApplicationStatusApplied
		cga.AppliedAt = lo.ToPtr(time.Now().UTC())
		// Note: We do NOT clear failure reason even on success - preserve it for audit/history purposes

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

		// Only increment retry count if application is failed
		// Note: We do NOT clear the failure reason here - it will only be cleared when successfully applied
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
		// Cancel current application and stop further ones
		cga.ApplicationStatus = types.ApplicationStatusCancelled
		cga.AppliedAt = nil // Ensure applied_at is nil for cancelled grants
		if err := s.CreditGrantApplicationRepo.Update(ctx, cga); err != nil {
			s.Logger.Errorw("Failed to cancel credit grant application", "application_id", cga.ID, "error", err)
			return err
		}

		// Cancel all future applications for this grant and subscription
		err := s.cancelFutureGrantApplications(ctx, creditGrant.CreditGrant)
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
	nextPeriodStart, nextPeriodEnd, err := CalculateNextCreditGrantPeriod(lo.FromPtr(grant), currentPeriodEnd)
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
	if grant.EndDate != nil && nextPeriodEnd.After(lo.FromPtr(grant.EndDate)) {
		s.Logger.Infow("Next period end is after grant end, skipping", "grant_id", grant.ID, "subscription_id", subscription.ID)
		return nil
	}

	nextPeriodCGAAReq := dto.CreateCreditGrantApplicationRequest{
		CreditGrantID:                   grant.ID,
		SubscriptionID:                  subscription.ID,
		ScheduledFor:                    nextPeriodStart,
		PeriodStart:                     lo.ToPtr(nextPeriodStart),
		PeriodEnd:                       lo.ToPtr(nextPeriodEnd),
		Credits:                         grant.Credits,
		ApplicationReason:               types.ApplicationReasonRecurringCreditGrant,
		SubscriptionStatusAtApplication: subscription.SubscriptionStatus,
		IdempotencyKey:                  s.generateIdempotencyKey(grant, lo.ToPtr(nextPeriodStart), lo.ToPtr(nextPeriodEnd)),
	}

	if err := nextPeriodCGAAReq.Validate(); err != nil {
		return err
	}

	nextPeriodCGA := nextPeriodCGAAReq.ToCreditGrantApplication(ctx)
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

func CalculateNextCreditGrantPeriod(grant creditgrant.CreditGrant, nextPeriodStart time.Time) (time.Time, time.Time, error) {
	billingPeriod, err := types.GetBillingPeriodFromCreditGrantPeriod(lo.FromPtr(grant.Period))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	// Calculate next period end using the anchor for consistent cycle calculation
	// We pass nil for subscriptionEndDate because we handle that at a higher level during application
	nextPeriodEnd, err := types.NextBillingDate(nextPeriodStart, lo.FromPtr(grant.CreditGrantAnchor), lo.FromPtr(grant.PeriodCount), billingPeriod, nil)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	return nextPeriodStart, nextPeriodEnd, nil
}

// generateIdempotencyKey creates a unique key for the credit grant application based on grant and period
func (s *creditGrantService) generateIdempotencyKey(grant *creditgrant.CreditGrant, periodStart, periodEnd *time.Time) string {
	generator := idempotency.NewGenerator()

	keyData := map[string]interface{}{
		"grant_id": grant.ID,
	}

	if periodStart != nil {
		keyData["period_start"] = periodStart.UTC()
	}

	if periodEnd != nil {
		keyData["period_end"] = periodEnd.UTC()
	}

	return generator.GenerateKey(idempotency.ScopeCreditGrant, keyData)
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
	cga.AppliedAt = nil

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

// cancelFutureGrantApplications cancels all future applications for a specific grant
func (s *creditGrantService) cancelFutureGrantApplications(ctx context.Context, grant *creditgrant.CreditGrant) error {
	if grant.Scope != types.CreditGrantScopeSubscription || grant.SubscriptionID == nil {
		return nil
	}

	// Get all future pending or failed applications
	pendingFilter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:  []string{grant.ID},
		SubscriptionIDs: []string{lo.FromPtr(grant.SubscriptionID)},
		ApplicationStatuses: []types.ApplicationStatus{
			types.ApplicationStatusPending,
			types.ApplicationStatusFailed,
		},
		QueryFilter: types.NewNoLimitQueryFilter(),
	}

	applications, err := s.CreditGrantApplicationRepo.List(ctx, pendingFilter)
	if err != nil {
		return err
	}

	// Cancel each application
	for _, app := range applications {
		app.ApplicationStatus = types.ApplicationStatusCancelled
		if err := s.CreditGrantApplicationRepo.Update(ctx, app); err != nil {
			s.Logger.Errorw("Failed to cancel future application", "application_id", app.ID, "error", err)
			continue
		}
	}

	return nil
}

func (s *creditGrantService) CancelFutureSubscriptionGrants(ctx context.Context, subscriptionID string) error {
	// get all credit grants for this subscription
	// use List with filter directly
	filter := types.NewNoLimitCreditGrantFilter()
	filter.SubscriptionIDs = []string{subscriptionID}
	filter.WithStatus(types.StatusPublished)

	creditGrants, err := s.CreditGrantRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Errorw("Failed to fetch credit grants for subscription", "subscription_id", subscriptionID, "error", err)
		return err
	}

	for _, grant := range creditGrants {
		if err := s.cancelFutureGrantApplications(ctx, grant); err != nil {
			s.Logger.Errorw("Failed to void future applications", "grant_id", grant.ID, "error", err)
			return err
		}

		// archive the grant
		if err := s.CreditGrantRepo.Delete(ctx, grant.ID); err != nil {
			s.Logger.Errorw("Failed to archive credit grant", "grant_id", grant.ID, "error", err)
			return err
		}
	}

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
