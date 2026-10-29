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

// ProcessTrialEndDue converts due trials and recovers activation from settled first-period invoices.
func (s *subscriptionService) ProcessTrialEndDue(ctx context.Context) (*dto.SubscriptionUpdatePeriodResponse, error) {
	// Keyset traversal advances past unpaid or failed candidates without skipping rows changed by this run.
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
	afterID := ""

	for {
		filter := &types.SubscriptionFilter{
			QueryFilter: &types.QueryFilter{
				Limit:  lo.ToPtr(batchSize),
				Sort:   lo.ToPtr("id"),
				Order:  lo.ToPtr(types.OrderAsc),
				Status: lo.ToPtr(types.StatusPublished),
			},
			SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusTrialing, types.SubscriptionStatusIncomplete},
			TrialEndDueLTE:     &now,
			AfterID:            afterID,
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
		for _, candidate := range subs {
			if candidate == nil || candidate.ID == "" || candidate.ID == afterID {
				return response, ierr.NewError("trial-end traversal did not advance").Mark(ierr.ErrDatabase)
			}
			afterID = candidate.ID
			subCtx := derivePerSubscriptionCtx(listCtx, candidate)

			responseItem := &dto.SubscriptionUpdatePeriodResponseItem{
				SubscriptionID: candidate.ID,
				PeriodStart:    candidate.CurrentPeriodStart,
				PeriodEnd:      candidate.CurrentPeriodEnd,
			}

			var processErr error
			if candidate.SubscriptionStatus == types.SubscriptionStatusIncomplete {
				_, processErr = s.reconcileTrialActivation(subCtx, candidate.ID, now)
			} else {
				_, processErr = s.processSubscriptionTrialEnd(subCtx, candidate, invoiceService, now)
			}
			if processErr != nil {
				s.Logger.Error(subCtx, "failed to process trial end for subscription",
					"subscription_id", candidate.ID,
					"error", processErr)
				response.TotalFailed++
				responseItem.Error = processErr.Error()
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
	return s.processGuardedTrialEnd(ctx, sub, invService, now, true)
}

func (s *subscriptionService) processSubscriptionTrialEnd(ctx context.Context, sub *subscription.Subscription, invoiceService InvoiceService, now time.Time) (*dto.InvoiceResponse, error) {
	return s.processGuardedTrialEnd(ctx, sub, invoiceService, now, false)
}

func (s *subscriptionService) processGuardedTrialEnd(ctx context.Context, sub *subscription.Subscription, invoiceService InvoiceService, now time.Time, endNow bool) (*dto.InvoiceResponse, error) {
	if sub == nil || !commitmentTermsDate(now) || invoiceService == nil {
		return nil, ierr.Validation("trial conversion requires a subscription, invoice service and finite date")
	}
	if err := s.validateSubscriptionLifecycle(ctx, sub.ID); err != nil {
		return nil, err
	}
	nested := s.DB.TxFromContext(ctx) != nil || s.BillingTermsStore.InTransaction(ctx)
	var current *subscription.Subscription
	var inv *dto.InvoiceResponse
	err := s.DB.WithTx(types.WithForceWriter(ctx), func(tx context.Context) error {
		lifecycle, err := s.lockSubscriptionLifecycle(tx, sub.ID, nested)
		if err != nil {
			return err
		}
		current = subscription.NewSubscriptionBuilder(lifecycle.current).Build()
		if endNow && (current.SubscriptionStatus != types.SubscriptionStatusTrialing || current.PauseStatus == types.PauseStatusActive || current.CancelledAt != nil || (current.CancelAt != nil && !current.CancelAt.After(now)) || (current.EndDate != nil && !current.EndDate.After(now))) {
			return ierr.NewError("trial lifecycle changed; refresh the subscription").Mark(ierr.ErrVersionConflict)
		}
		if endNow && current.SubscriptionStatus == types.SubscriptionStatusTrialing && current.TrialStart != nil && current.TrialEnd != nil {
			if now.Before(*current.TrialStart) {
				return ierr.Validation("trial cannot end before it starts")
			}
			current = subscription.NewSubscriptionBuilder(current).WithTrialEnd(&now).Build()
		}
		inv, err = s.processLockedTrialEnd(lifecycle.ctx, current, invoiceService, now)
		return err
	})
	if err == nil && current != nil {
		*sub = *current
	}
	return inv, err
}

func (s *subscriptionService) processLockedTrialEnd(ctx context.Context, sub *subscription.Subscription, invoiceService InvoiceService, now time.Time) (*dto.InvoiceResponse, error) {
	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		return nil, nil
	}
	if sub.SubscriptionStatus == types.SubscriptionStatusPaused || sub.PauseStatus == types.PauseStatusActive || sub.CancelledAt != nil ||
		(sub.CancelAt != nil && !sub.CancelAt.After(now)) || (sub.EndDate != nil && !sub.EndDate.After(now)) {
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

	firstPeriodStart := lo.FromPtr(sub.TrialEnd).UTC().Truncate(time.Microsecond)
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
		collectionCtx := withTrialEndCollection(txCtx, sub, now)
		trialEndInvoice, _, errInv = invoiceService.CreateSubscriptionInvoice(collectionCtx, &dto.CreateSubscriptionInvoiceRequest{
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

	// Activation waits for the outermost commit when conversion runs inside a caller transaction.
	if trialEndInvoice == nil {
		if err := s.requestTrialActivation(ctx, sub.ID, now); err != nil {
			return nil, err
		}
		s.Logger.Info(ctx, "requested trial activation after zero-amount invoice",
			"subscription_id", sub.ID)
	}
	return trialEndInvoice, nil
}

// convertTrialToFirstPaidPeriod moves sub out of trial into incomplete with its first paid period starting at start.
// Calendar subs bill a short first period; anniversary subs re-anchor at start for a full one.
func (s *subscriptionService) convertTrialToFirstPaidPeriod(ctx context.Context, sub *subscription.Subscription, start time.Time) (time.Time, error) {
	start = start.UTC().Truncate(time.Microsecond)
	anchor, end, err := trialConversionPeriod(sub, start)
	if err != nil {
		return time.Time{}, err
	}
	updated := subscription.NewSubscriptionBuilder(sub).WithBillingAnchor(anchor).WithTrialEnd(&start).
		WithStatus(types.SubscriptionStatusIncomplete).WithCurrentPeriodStart(start).WithCurrentPeriodEnd(end).Build()
	if err := s.SubRepo.Update(ctx, updated); err != nil {
		return time.Time{}, err
	}
	*sub = *updated
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
		child = subscription.NewSubscriptionBuilder(child).WithTrialEnd(parentSub.TrialEnd).WithStatus(types.SubscriptionStatusIncomplete).
			WithBillingAnchor(parentSub.BillingAnchor).WithCurrentPeriodStart(parentSub.CurrentPeriodStart).WithCurrentPeriodEnd(parentSub.CurrentPeriodEnd).Build()
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
