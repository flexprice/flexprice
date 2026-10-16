package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// setCreateSubscriptionTrialWindow fills in trial start/end. Precedence: explicit dates, then
// subscription trial_period_days, then plan prices.
func setCreateSubscriptionTrialWindow(req *dto.CreateSubscriptionRequest, sub *subscription.Subscription, planPrices []*dto.PriceResponse) error {
	if req.TrialStart != nil && req.TrialEnd != nil {
		sub.TrialStart = req.TrialStart
		sub.TrialEnd = req.TrialEnd
		return nil
	}

	// Precedence: request trial_period_days, else uniform trial_period_days on recurring fixed plan prices
	// (usage/tiered prices are ignored so trial is not "inherited" from a metered line item).
	effectiveTrialDays := 0
	if req.TrialPeriodDays != nil {
		effectiveTrialDays = lo.FromPtr(req.TrialPeriodDays)
	} else {
		recurringFixed := lo.Filter(planPrices, func(p *dto.PriceResponse, _ int) bool {
			return p.BillingCadence == types.BILLING_CADENCE_RECURRING && p.Type == types.PRICE_TYPE_FIXED
		})
		if len(recurringFixed) == 0 {
			return nil
		}
		unique := lo.Uniq(lo.Map(recurringFixed, func(p *dto.PriceResponse, _ int) int {
			return p.TrialPeriodDays
		}))
		if len(unique) == 0 {
			return nil
		}
		if len(unique) > 1 {
			return ierr.NewError("all recurring fixed plan prices must have the same trial_period_days").
				WithHint("Align trial_period_days on plan prices or override with subscription trial_period_days").
				Mark(ierr.ErrValidation)
		}
		effectiveTrialDays = unique[0]
	}

	if effectiveTrialDays <= 0 {
		sub.TrialStart, sub.TrialEnd = nil, nil
		return nil
	}

	// Window starts on subscription start, N local days.
	sub.TrialStart = lo.ToPtr(sub.StartDate)
	sub.TrialEnd = lo.ToPtr(types.AdvanceDays(sub.StartDate, effectiveTrialDays, sub.Timezone))
	return nil
}

// ProcessTrialEndDue picks up trialing subs past trial end, moves them to the first real period, and creates
// the trial-end invoice. Safe to run repeatedly for the same subscription.
func (s *subscriptionService) ProcessTrialEndDue(ctx context.Context) (*dto.SubscriptionUpdatePeriodResponse, error) {
	// Re-query with offset 0 each time: processSubscriptionTrialEnd flips trialing → incomplete, so
	// offset pagination would skip rows that slide into the first page after earlier rows are updated.
	const batchSize = 100
	now := time.Now().UTC()
	listCtx := ctx

	s.Logger.Info(ctx, "starting trial end processing", "current_time", now)

	response := &dto.SubscriptionUpdatePeriodResponse{
		Items:        make([]*dto.SubscriptionUpdatePeriodResponseItem, 0),
		TotalFailed:  0,
		TotalSuccess: 0,
		StartAt:      now,
	}

	invoiceService := NewInvoiceService(s.ServiceParams)

	for {
		filter := &types.SubscriptionFilter{
			QueryFilter: &types.QueryFilter{
				Limit:  lo.ToPtr(batchSize),
				Offset: lo.ToPtr(0),
				Status: lo.ToPtr(types.StatusPublished),
			},
			SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusTrialing},
			TrialEndDueLTE:     &now,
		}

		subs, err := s.SubRepo.GetSubscriptionsForBillingPeriodUpdate(listCtx, filter)
		if err != nil {
			return response, err
		}

		if len(subs) == 0 {
			break
		}

		s.Logger.Info(listCtx, "processing trial end batch", "batch_size", len(subs))

		// Derive per-sub context from listCtx (no WithValue chain growth); rows can span envs/tenants.
		for _, trialingSubscription := range subs {
			subCtx := derivePerSubscriptionCtx(listCtx, trialingSubscription)

			responseItem := &dto.SubscriptionUpdatePeriodResponseItem{
				SubscriptionID: trialingSubscription.ID,
				PeriodStart:    trialingSubscription.CurrentPeriodStart,
				PeriodEnd:      trialingSubscription.CurrentPeriodEnd,
			}

			_, err := s.processSubscriptionTrialEnd(subCtx, trialingSubscription, invoiceService, now)
			if err != nil {
				s.Logger.Error(subCtx, "failed to process trial end for subscription",
					"subscription_id", trialingSubscription.ID,
					"error", err)
				response.TotalFailed++
				responseItem.Error = err.Error()
			} else {
				response.TotalSuccess++
				responseItem.Success = true
			}
			response.Items = append(response.Items, responseItem)
		}

		if len(subs) < batchSize {
			break
		}
	}

	return response, nil
}

// derivePerSubscriptionCtx scopes tenant, environment, and user onto parentCtx for a single subscription.
// Callers must not reuse one subscription's context for the next; use a shared parent and derive per row.
func derivePerSubscriptionCtx(parentCtx context.Context, sub *subscription.Subscription) context.Context {
	ctx := context.WithValue(parentCtx, types.CtxTenantID, sub.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, sub.EnvironmentID)
	ctx = context.WithValue(ctx, types.CtxUserID, sub.CreatedBy)
	return ctx
}

// ProcessSingleSubscriptionTrialEnd is the public entry point for ending a single subscription's
// trial on demand (e.g. from the modification API). It delegates to the private batch-oriented
// processSubscriptionTrialEnd with now = time.Now().
func (s *subscriptionService) ProcessSingleSubscriptionTrialEnd(ctx context.Context, sub *subscription.Subscription, now time.Time) (*dto.InvoiceResponse, error) {
	invService := NewInvoiceService(s.ServiceParams)
	return s.processSubscriptionTrialEnd(ctx, sub, invService, now)
}

func (s *subscriptionService) processSubscriptionTrialEnd(ctx context.Context, sub *subscription.Subscription, invoiceService InvoiceService, now time.Time) (*dto.InvoiceResponse, error) {
	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		return nil, nil
	}
	if sub.SubscriptionStatus == types.SubscriptionStatusPaused {
		return nil, nil
	}
	if sub.SubscriptionStatus != types.SubscriptionStatusTrialing {
		return nil, nil
	}
	if sub.TrialStart == nil || sub.TrialEnd == nil {
		s.Logger.Info(ctx, "trialing subscription missing trial bounds, skipping",
			"subscription_id", sub.ID)
		return nil, nil
	}
	if sub.TrialEnd.After(now) {
		return nil, nil
	}

	// A pending checkout (of any kind) defers trial end to a later cron run; sessions expire within hours.
	// TODO: narrow to trial_end checkouts if this delays conversions in practice.
	pending, err := pendingCheckoutSessions(ctx, s.ServiceParams, sub.ID)
	if err != nil || len(pending) > 0 {
		return nil, err
	}

	firstPeriodStart := lo.FromPtr(sub.TrialEnd)
	paymentParams := dto.NewPaymentParametersFromSubscription(sub.CollectionMethod, sub.PaymentBehavior, sub.GatewayPaymentMethodID)
	paymentParams = paymentParams.NormalizePaymentParameters()

	var trialEndInvoice *dto.InvoiceResponse
	var firstPeriodEnd time.Time

	// Subscription update, cascade, and trial-end invoice creation commit together.
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		var errConv error
		if firstPeriodEnd, errConv = s.convertTrialToFirstPaidPeriod(txCtx, sub, firstPeriodStart); errConv != nil {
			return errConv
		}

		var errInv error
		trialEndInvoice, _, errInv = invoiceService.CreateSubscriptionInvoice(txCtx, &dto.CreateSubscriptionInvoiceRequest{
			SubscriptionID: sub.ID,
			PeriodStart:    firstPeriodStart,
			PeriodEnd:      firstPeriodEnd,
			ReferencePoint: types.ReferencePointPeriodStart,
			BillingReason:  types.InvoiceBillingReasonSubscriptionTrialEnd,
		}, paymentParams, types.InvoiceFlowRenewal, false)
		return errInv
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "subscription period advanced and moved to incomplete after trial end",
		"subscription_id", sub.ID,
		"first_period_start", firstPeriodStart,
		"first_period_end", firstPeriodEnd)

	// Runs after commit: webhooks and credit grants must not fire if the tx above rolled back.
	if trialEndInvoice == nil {
		if err := s.completeTrialConversionToActive(ctx, sub); err != nil {
			return nil, err
		}
		s.Logger.Info(ctx, "subscription activated after zero-amount trial end",
			"subscription_id", sub.ID)
	}
	return trialEndInvoice, nil
}

// convertTrialToFirstPaidPeriod moves sub out of trial into incomplete with its first paid period starting at start.
// Calendar subs bill a short first period; anniversary subs re-anchor at start for a full one.
func (s *subscriptionService) convertTrialToFirstPaidPeriod(ctx context.Context, sub *subscription.Subscription, start time.Time) (time.Time, error) {
	anchor, end, err := trialConversionPeriod(sub, start)
	if err != nil {
		return time.Time{}, err
	}
	sub.BillingAnchor = anchor
	sub.TrialEnd = lo.ToPtr(start)
	// The trial has ended, but activation waits for invoice settlement or the zero-amount completion hook.
	sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
	sub.CurrentPeriodStart = start
	sub.CurrentPeriodEnd = end
	if err := s.SubRepo.Update(ctx, sub); err != nil {
		return time.Time{}, err
	}
	return end, s.cascadeTrialEndToInherited(ctx, sub)
}

// cascadeTrialEndToInherited propagates the trial-end state (incomplete + advanced period) to all
// inherited subscriptions under a parent. Mirrors cascadePauseToInherited.
func (s *subscriptionService) cascadeTrialEndToInherited(ctx context.Context, parentSub *subscription.Subscription) error {
	if parentSub.SubscriptionType != types.SubscriptionTypeParent {
		return nil
	}
	children, err := s.getInheritedSubscriptions(ctx, parentSub.ID)
	if err != nil {
		return err
	}
	for _, child := range children {
		child.TrialEnd = parentSub.TrialEnd
		child.SubscriptionStatus = types.SubscriptionStatusIncomplete
		child.BillingAnchor = parentSub.BillingAnchor
		child.CurrentPeriodStart = parentSub.CurrentPeriodStart
		child.CurrentPeriodEnd = parentSub.CurrentPeriodEnd
		if err := s.SubRepo.Update(ctx, child); err != nil {
			return err
		}
	}
	return nil
}

// cascadeTrialActivationToInherited propagates active status to all inherited subscriptions
// once the parent's trial-end invoice is paid (or was zero-amount). Mirrors cascadeResumeToInherited.
func (s *subscriptionService) cascadeTrialActivationToInherited(ctx context.Context, parentSub *subscription.Subscription) error {
	if parentSub.SubscriptionType != types.SubscriptionTypeParent {
		return nil
	}
	children, err := s.getInheritedSubscriptions(ctx, parentSub.ID)
	if err != nil {
		return err
	}
	for _, child := range children {
		child.SubscriptionStatus = types.SubscriptionStatusActive
		if err := s.SubRepo.Update(ctx, child); err != nil {
			return err
		}
	}
	return nil
}

// trialConversionPeriod returns the billing anchor and first paid period end for a trial that ends at start.
func trialConversionPeriod(sub *subscription.Subscription, start time.Time) (time.Time, time.Time, error) {
	anchor := start
	if sub.BillingCycle == types.BillingCycleCalendar {
		anchor = types.CalculateCalendarBillingAnchor(start, sub.BillingPeriod, sub.Timezone)
	}
	end, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart:  start,
		BillingAnchor:       anchor,
		Unit:                sub.BillingPeriodCount,
		Period:              sub.BillingPeriod,
		SubscriptionEndDate: sub.EndDate,
		Timezone:            sub.Timezone,
	})
	return anchor, end, err
}
