package enterprise

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
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// CreditGrantService defines the enterprise credit grant service (prepaid usage with credits).
// Only enterprise methods: CreateCreditGrant, GetCreditGrant, ListCreditGrants.
type CreditGrantService interface {
	CreateCreditGrant(ctx context.Context, req dto.CreateCreditGrantRequest) (*dto.CreditGrantResponse, error)
	GetCreditGrant(ctx context.Context, id string) (*dto.CreditGrantResponse, error)
	ListCreditGrants(ctx context.Context, filter *types.CreditGrantFilter) (*dto.ListCreditGrantsResponse, error)
}

type creditGrantService struct {
	EnterpriseParams
}

// NewCreditGrantService creates a new enterprise credit grant service.
func NewCreditGrantService(p EnterpriseParams) CreditGrantService {
	return &creditGrantService{EnterpriseParams: p}
}

// getCreditGrantsByPlan is used by CreateCreditGrant for plan-scope validation.
func (s *creditGrantService) getCreditGrantsByPlan(ctx context.Context, planID string) (*dto.ListCreditGrantsResponse, error) {
	filter := types.NewNoLimitCreditGrantFilter()
	filter.PlanIDs = []string{planID}
	filter.WithStatus(types.StatusPublished)
	filter.Scope = lo.ToPtr(types.CreditGrantScopePlan)
	return s.ListCreditGrants(ctx, filter)
}

func (s *creditGrantService) CreateCreditGrant(ctx context.Context, req dto.CreateCreditGrantRequest) (*dto.CreditGrantResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if req.PlanID != nil && lo.FromPtr(req.PlanID) != "" {
		plan, err := s.PlanRepo.Get(ctx, lo.FromPtr(req.PlanID))
		if err != nil {
			return nil, err
		}
		if plan == nil {
			return nil, ierr.NewError("plan not found").
				WithHint(fmt.Sprintf("Plan with ID %s does not exist", lo.FromPtr(req.PlanID))).
				WithReportableDetails(map[string]interface{}{"plan_id": lo.FromPtr(req.PlanID)}).
				Mark(ierr.ErrNotFound)
		}
		if plan.Status != types.StatusPublished {
			return nil, ierr.NewError("plan is not published").
				WithHint("Plan is not published").
				WithReportableDetails(map[string]interface{}{"plan_id": lo.FromPtr(req.PlanID)}).
				Mark(ierr.ErrValidation)
		}
	}

	switch req.Scope {
	case types.CreditGrantScopePlan:
		existingGrants, err := s.getCreditGrantsByPlan(ctx, lo.FromPtr(req.PlanID))
		if err != nil {
			return nil, err
		}
		if len(existingGrants.Items) > 0 {
			validationError := ierr.NewError("all credit grants for a plan must have the same conversion_rate and topup_conversion_rate").
				WithHint("All credit grants for a plan must have the same conversion rates").
				Mark(ierr.ErrValidation)
			for _, existingGrant := range existingGrants.Items {
				if (req.ConversionRate == nil) != (existingGrant.ConversionRate == nil) ||
					(req.ConversionRate != nil && !req.ConversionRate.Equal(lo.FromPtr(existingGrant.ConversionRate))) {
					return nil, validationError
				}
				if (req.TopupConversionRate == nil) != (existingGrant.TopupConversionRate == nil) ||
					(req.TopupConversionRate != nil && !req.TopupConversionRate.Equal(lo.FromPtr(existingGrant.TopupConversionRate))) {
					return nil, validationError
				}
			}
		}
	case types.CreditGrantScopeSubscription:
		sub, err := s.SubRepo.Get(ctx, lo.FromPtr(req.SubscriptionID))
		if err != nil {
			return nil, err
		}
		if sub == nil {
			return nil, ierr.NewError("subscription not found").
				WithHint(fmt.Sprintf("Subscription with ID %s does not exist", lo.FromPtr(req.SubscriptionID))).
				WithReportableDetails(map[string]interface{}{"subscription_id": lo.FromPtr(req.SubscriptionID)}).
				Mark(ierr.ErrNotFound)
		}
		if sub.SubscriptionStatus == types.SubscriptionStatusCancelled {
			return nil, ierr.NewError("subscription is cancelled").
				WithHint("Subscription is cancelled").
				Mark(ierr.ErrValidation)
		}
		if req.StartDate.Before(sub.StartDate) {
			return nil, ierr.NewError("credit grant start_date must be greater than or equal to subscription start_date").
				WithHint("Credit grant start date cannot be before subscription start date").
				Mark(ierr.ErrValidation)
		}
		if req.EndDate != nil && sub.EndDate != nil && req.EndDate.After(lo.FromPtr(sub.EndDate)) {
			return nil, ierr.NewError("credit grant end_date must be less than or equal to subscription end_date").
				WithHint("Credit grant end date cannot be after subscription end date").
				Mark(ierr.ErrValidation)
		}
		if req.CreditGrantAnchor == nil && req.StartDate != nil {
			req.CreditGrantAnchor = req.StartDate
		}
		if req.CreditGrantAnchor != nil {
			if req.CreditGrantAnchor.Before(sub.StartDate) {
				return nil, ierr.NewError("credit_grant_anchor must be greater than or equal to subscription start_date").
					WithHint("Credit grant anchor cannot be before subscription start date").
					Mark(ierr.ErrValidation)
			}
			if sub.EndDate != nil && req.CreditGrantAnchor.After(lo.FromPtr(sub.EndDate)) {
				return nil, ierr.NewError("credit_grant_anchor must be less than or equal to subscription end_date").
					WithHint("Credit grant anchor cannot be after subscription end date").
					Mark(ierr.ErrValidation)
			}
		}
	}

	cg := req.ToCreditGrant(ctx)
	var err error
	cg, err = s.CreditGrantRepo.Create(ctx, cg)
	if err != nil {
		return nil, err
	}

	if cg.Scope == types.CreditGrantScopeSubscription {
		cg, err = s.initializeCreditGrantWorkflow(ctx, lo.FromPtr(cg))
		if err != nil {
			return nil, err
		}
	}

	return &dto.CreditGrantResponse{CreditGrant: cg}, nil
}

func (s *creditGrantService) initializeCreditGrantWorkflow(ctx context.Context, cg creditgrant.CreditGrant) (*creditgrant.CreditGrant, error) {
	periodStart := lo.FromPtr(cg.StartDate)
	var periodEnd *time.Time

	if cg.Cadence == types.CreditGrantCadenceRecurring {
		_, calculatedPeriodEnd, err := service.CalculateNextCreditGrantPeriod(cg, periodStart)
		if err != nil {
			return nil, err
		}
		periodEnd = lo.ToPtr(calculatedPeriodEnd)
	}

	var subscriptionStatus types.SubscriptionStatus
	if cg.SubscriptionID != nil && lo.FromPtr(cg.SubscriptionID) != "" {
		sub, err := s.SubRepo.Get(ctx, lo.FromPtr(cg.SubscriptionID))
		if err != nil {
			return nil, err
		}
		subscriptionStatus = sub.SubscriptionStatus
	}

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
		PeriodStart:                     periodStart,
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

	if cg.CreditGrantAnchor.Before(time.Now()) || cg.CreditGrantAnchor.Equal(time.Now()) {
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
	return &dto.CreditGrantResponse{CreditGrant: result}, nil
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
	response.Pagination = types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset())
	return response, nil
}

func (s *creditGrantService) applyCreditGrantToWallet(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, cga *domainCreditGrantApplication.CreditGrantApplication) error {
	walletSvc := service.NewWalletService(s.ServiceParams)

	wallets, err := walletSvc.GetWalletsByCustomerID(ctx, subscription.CustomerID)
	if err != nil {
		return s.handleCreditGrantFailure(ctx, cga, err, "Failed to get wallet for top up")
	}

	var selectedWallet *dto.WalletResponse
	for _, w := range wallets {
		if !types.IsMatchingCurrency(w.Currency, subscription.Currency) {
			continue
		}
		if grant.ConversionRate != nil && !w.ConversionRate.Equal(lo.FromPtr(grant.ConversionRate)) {
			continue
		}
		if grant.TopupConversionRate != nil && !w.TopupConversionRate.Equal(lo.FromPtr(grant.TopupConversionRate)) {
			continue
		}
		selectedWallet = w
		break
	}
	if selectedWallet == nil {
		walletReq := &dto.CreateWalletRequest{
			Name:       "Subscription Wallet",
			CustomerID: subscription.CustomerID,
			Currency:   subscription.Currency,
			Config: &types.WalletConfig{
				AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeUsage},
			},
		}
		if grant.ConversionRate != nil {
			walletReq.ConversionRate = lo.FromPtr(grant.ConversionRate)
		}
		if grant.TopupConversionRate != nil {
			walletReq.TopupConversionRate = grant.TopupConversionRate
		}
		var err error
		selectedWallet, err = walletSvc.CreateWallet(ctx, walletReq)
		if err != nil {
			return s.handleCreditGrantFailure(ctx, cga, err, "Failed to create wallet for top up")
		}
	}

	var expiryDate *time.Time
	effectiveDate := cga.PeriodStart

	if grant.ExpirationType == types.CreditGrantExpiryTypeNever {
		expiryDate = nil
	}
	if grant.ExpirationType == types.CreditGrantExpiryTypeDuration &&
		grant.ExpirationDurationUnit != nil && grant.ExpirationDuration != nil && lo.FromPtr(grant.ExpirationDuration) > 0 {
		duration := lo.FromPtr(grant.ExpirationDuration)
		switch lo.FromPtr(grant.ExpirationDurationUnit) {
		case types.CreditGrantExpiryDurationUnitDays:
			expiryDate = lo.ToPtr(effectiveDate.Add(time.Duration(duration) * 24 * time.Hour))
		case types.CreditGrantExpiryDurationUnitWeeks:
			expiryDate = lo.ToPtr(effectiveDate.Add(time.Duration(duration) * 7 * 24 * time.Hour))
		case types.CreditGrantExpiryDurationUnitMonths:
			expiryDate = lo.ToPtr(effectiveDate.AddDate(0, duration, 0))
		case types.CreditGrantExpiryDurationUnitYears:
			expiryDate = lo.ToPtr(effectiveDate.AddDate(duration, 0, 0))
		default:
			return ierr.NewError("invalid expiration duration unit").Mark(ierr.ErrValidation)
		}
	}
	if grant.ExpirationType == types.CreditGrantExpiryTypeBillingCycle {
		if (effectiveDate.Equal(subscription.CurrentPeriodStart) || effectiveDate.After(subscription.CurrentPeriodStart)) &&
			effectiveDate.Before(subscription.CurrentPeriodEnd) {
			expiryDate = lo.ToPtr(subscription.CurrentPeriodEnd)
		} else {
			periods, err := types.CalculateBillingPeriods(
				subscription.StartDate,
				subscription.EndDate,
				subscription.BillingAnchor,
				subscription.BillingPeriodCount,
				subscription.BillingPeriod,
			)
			if err != nil {
				return err
			}
			for _, subPeriod := range periods {
				if (effectiveDate.Equal(subPeriod.Start) || effectiveDate.After(subPeriod.Start)) && effectiveDate.Before(subPeriod.End) {
					expiryDate = lo.ToPtr(subPeriod.End)
					break
				}
			}
		}
	}

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

	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if expiryDate != nil && expiryDate.Before(time.Now().UTC()) {
			cga.ApplicationStatus = types.ApplicationStatusSkipped
			cga.FailureReason = lo.ToPtr(fmt.Sprintf("Credit grant expired before application. Expiry date: %s", expiryDate.Format(time.RFC3339)))
			cga.AppliedAt = nil
			if err := s.CreditGrantApplicationRepo.Update(txCtx, cga); err != nil {
				return err
			}
			if grant.Cadence == types.CreditGrantCadenceRecurring {
				return s.createNextPeriodApplication(txCtx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
			}
			return nil
		}

		if _, err := walletSvc.TopUpWallet(txCtx, selectedWallet.ID, topupReq); err != nil {
			return err
		}
		cga.ApplicationStatus = types.ApplicationStatusApplied
		cga.AppliedAt = lo.ToPtr(time.Now().UTC())
		if err := s.CreditGrantApplicationRepo.Update(txCtx, cga); err != nil {
			return err
		}
		if grant.Cadence == types.CreditGrantCadenceRecurring {
			return s.createNextPeriodApplication(txCtx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
		}
		return nil
	})
	if err != nil {
		return s.handleCreditGrantFailure(ctx, cga, err, "Transaction failed during credit grant application")
	}
	return nil
}

func (s *creditGrantService) handleCreditGrantFailure(ctx context.Context, cga *domainCreditGrantApplication.CreditGrantApplication, err error, hint string) error {
	s.Logger.Errorw("Credit grant application failed", "cga_id", cga.ID, "grant_id", cga.CreditGrantID, "hint", hint, "error", err)
	sentrySvc := sentry.NewSentryService(s.Config, s.Logger)
	sentrySvc.CaptureException(err)
	cga.ApplicationStatus = types.ApplicationStatusFailed
	cga.FailureReason = lo.ToPtr(fmt.Sprintf("%s: %s", hint, err.Error()))
	if updateErr := s.CreditGrantApplicationRepo.Update(ctx, cga); updateErr != nil {
		s.Logger.Errorw("Failed to update CGA after failure", "cga_id", cga.ID, "original_error", err.Error())
		return err
	}
	return err
}

func (s *creditGrantService) processScheduledApplication(ctx context.Context, cga *domainCreditGrantApplication.CreditGrantApplication) error {
	sp := s.ServiceParams
	subscriptionSvc := service.NewSubscriptionService(sp)
	creditGrantSvc := service.NewCreditGrantService(sp)

	subscription, err := subscriptionSvc.GetSubscription(ctx, cga.SubscriptionID)
	if err != nil {
		s.Logger.Errorw("Failed to get subscription", "subscription_id", cga.SubscriptionID, "error", err)
		return err
	}
	creditGrant, err := creditGrantSvc.GetCreditGrant(ctx, cga.CreditGrantID)
	if err != nil {
		s.Logger.Errorw("Failed to get credit grant", "credit_grant_id", cga.CreditGrantID, "error", err)
		return err
	}
	if creditGrant.CreditGrant.Status != types.StatusPublished {
		return nil
	}
	if cga.ApplicationStatus == types.ApplicationStatusFailed {
		cga.RetryCount++
	}

	stateHandler := service.NewSubscriptionStateHandler(subscription.Subscription, creditGrant.CreditGrant)
	action, err := stateHandler.DetermineCreditGrantAction()
	if err != nil {
		s.Logger.Errorw("Failed to determine action", "application_id", cga.ID, "error", err)
		return err
	}

	switch action {
	case service.StateActionApply:
		return s.applyCreditGrantToWallet(ctx, creditGrant.CreditGrant, subscription.Subscription, cga)
	case service.StateActionSkip:
		return s.skipCreditGrantApplication(ctx, cga, creditGrant.CreditGrant, subscription.Subscription)
	case service.StateActionDefer:
		return s.deferCreditGrantApplication(ctx, cga)
	case service.StateActionCancel:
		cga.ApplicationStatus = types.ApplicationStatusCancelled
		cga.AppliedAt = nil
		if err := s.CreditGrantApplicationRepo.Update(ctx, cga); err != nil {
			return err
		}
		return s.cancelFutureGrantApplications(ctx, creditGrant.CreditGrant)
	}
	return nil
}

func (s *creditGrantService) createNextPeriodApplication(ctx context.Context, grant *creditgrant.CreditGrant, subscription *subscription.Subscription, currentPeriodEnd time.Time) error {
	nextPeriodStart, nextPeriodEnd, err := service.CalculateNextCreditGrantPeriod(lo.FromPtr(grant), currentPeriodEnd)
	if err != nil {
		return err
	}
	if grant.EndDate != nil && nextPeriodEnd.After(lo.FromPtr(grant.EndDate)) {
		return nil
	}
	nextPeriodCGAReq := dto.CreateCreditGrantApplicationRequest{
		CreditGrantID:                   grant.ID,
		SubscriptionID:                  subscription.ID,
		ScheduledFor:                    nextPeriodStart,
		PeriodStart:                     nextPeriodStart,
		PeriodEnd:                       lo.ToPtr(nextPeriodEnd),
		Credits:                         grant.Credits,
		ApplicationReason:               types.ApplicationReasonRecurringCreditGrant,
		SubscriptionStatusAtApplication: subscription.SubscriptionStatus,
		IdempotencyKey:                  s.generateIdempotencyKey(grant, lo.ToPtr(nextPeriodStart), lo.ToPtr(nextPeriodEnd)),
	}
	if err := nextPeriodCGAReq.Validate(); err != nil {
		return err
	}
	nextPeriodCGA := nextPeriodCGAReq.ToCreditGrantApplication(ctx)
	return s.CreditGrantApplicationRepo.Create(ctx, nextPeriodCGA)
}

func (s *creditGrantService) generateIdempotencyKey(grant *creditgrant.CreditGrant, periodStart, periodEnd *time.Time) string {
	generator := idempotency.NewGenerator()
	keyData := map[string]interface{}{"grant_id": grant.ID}
	if periodStart != nil {
		keyData["period_start"] = periodStart.UTC()
	}
	if periodEnd != nil {
		keyData["period_end"] = periodEnd.UTC()
	}
	return generator.GenerateKey(idempotency.ScopeCreditGrant, keyData)
}

func (s *creditGrantService) skipCreditGrantApplication(ctx context.Context, cga *domainCreditGrantApplication.CreditGrantApplication, grant *creditgrant.CreditGrant, subscription *subscription.Subscription) error {
	cga.ApplicationStatus = types.ApplicationStatusSkipped
	cga.AppliedAt = nil
	if err := s.CreditGrantApplicationRepo.Update(ctx, cga); err != nil {
		return err
	}
	if grant.Cadence == types.CreditGrantCadenceRecurring {
		return s.createNextPeriodApplication(ctx, grant, subscription, lo.FromPtr(cga.PeriodEnd))
	}
	return nil
}

func (s *creditGrantService) deferCreditGrantApplication(ctx context.Context, cga *domainCreditGrantApplication.CreditGrantApplication) error {
	retryCount := cga.RetryCount
	if retryCount > 4 {
		retryCount = 4
	}
	backoffMinutes := 30 * (1 << retryCount)
	nextRetry := time.Now().UTC().Add(time.Duration(backoffMinutes) * time.Minute)
	cga.ScheduledFor = nextRetry
	cga.RetryCount++
	return s.CreditGrantApplicationRepo.Update(ctx, cga)
}

func (s *creditGrantService) cancelFutureGrantApplications(ctx context.Context, grant *creditgrant.CreditGrant) error {
	if grant.Scope != types.CreditGrantScopeSubscription || grant.SubscriptionID == nil {
		return nil
	}
	pendingFilter := &types.CreditGrantApplicationFilter{
		CreditGrantIDs:      []string{grant.ID},
		SubscriptionIDs:     []string{lo.FromPtr(grant.SubscriptionID)},
		ApplicationStatuses: []types.ApplicationStatus{types.ApplicationStatusPending, types.ApplicationStatusFailed},
		QueryFilter:         types.NewNoLimitQueryFilter(),
	}
	applications, err := s.CreditGrantApplicationRepo.List(ctx, pendingFilter)
	if err != nil {
		return err
	}
	for _, app := range applications {
		app.ApplicationStatus = types.ApplicationStatusCancelled
		_ = s.CreditGrantApplicationRepo.Update(ctx, app)
	}
	return nil
}
