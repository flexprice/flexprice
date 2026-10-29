package service

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/activity"
	domainBillingTerms "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *subscriptionService) reconcileTrialActivation(ctx context.Context, subscriptionID string, asOf time.Time) (bool, error) {
	if err := s.validateTrialActivation(ctx, subscriptionID, asOf); err != nil {
		return false, err
	}
	if s.DB.TxFromContext(ctx) != nil || s.BillingTermsStore.InTransaction(ctx) {
		return false, ierr.NewError("trial activation requires its own transaction").Mark(ierr.ErrInvalidOperation)
	}

	var activated []*subscription.Subscription
	err := s.DB.WithTx(types.WithForceWriter(ctx), func(tx context.Context) error {
		rows, err := s.SubRepo.List(tx, &types.SubscriptionFilter{
			QueryFilter:        &types.QueryFilter{Limit: lo.ToPtr(2), Status: lo.ToPtr(types.StatusPublished)},
			SubscriptionIDs:    []string{subscriptionID},
			SubscriptionStatus: types.AllSubscriptionStatuses(),
		})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		if len(rows) != 1 || !subscriptionUpdateScoped(tx, rows[0], subscriptionID) {
			return ierr.NewError("trial activation discovery returned invalid scope").Mark(ierr.ErrDatabase)
		}
		discovered := rows[0]
		if !trialRecoveryRoot(discovered) || !trialRecoveryPending(discovered, asOf) {
			return nil
		}
		participantIDs := []string{subscriptionID}
		if discovered.SubscriptionType == types.SubscriptionTypeParent {
			children, err := s.trialRecoveryChildren(tx, subscriptionID)
			if err != nil {
				return err
			}
			for _, child := range children {
				participantIDs = append(participantIDs, child.ID)
			}
		}
		slices.Sort(participantIDs)
		locked := make(map[string]*subscription.Subscription, len(participantIDs))
		scopes := make([]domainBillingTerms.EligibilityScope, 0, len(participantIDs))
		for _, participantID := range participantIDs {
			current, err := s.SubRepo.GetForUpdateNowait(tx, participantID)
			if err != nil {
				return err
			}
			if !subscriptionUpdateScoped(tx, current, participantID) {
				return ierr.NewError("trial activation lock returned invalid scope").Mark(ierr.ErrDatabase)
			}
			locked[participantID] = current
			scopes = append(scopes, domainBillingTerms.EligibilityScope{Type: "subscription", ID: participantID})
		}
		current := locked[subscriptionID]
		if current.SubscriptionType != discovered.SubscriptionType || lo.FromPtr(current.ParentSubscriptionID) != lo.FromPtr(discovered.ParentSubscriptionID) {
			return trialRecoveryMembershipChanged()
		}
		if !trialRecoveryRoot(current) || !trialRecoveryPending(current, asOf) {
			return nil
		}
		invoices, err := s.InvoiceRepo.List(tx, &types.InvoiceFilter{
			QueryFilter:    &types.QueryFilter{Limit: lo.ToPtr(2), Status: lo.ToPtr(types.StatusPublished)},
			SubscriptionID: subscriptionID, InvoiceType: types.InvoiceTypeSubscription,
			BillingReason:  types.InvoiceBillingReasonSubscriptionTrialEnd,
			InvoiceStatus:  []types.InvoiceStatus{types.InvoiceStatusDraft, types.InvoiceStatusFinalized, types.InvoiceStatusSkipped},
			PeriodStartGTE: &current.CurrentPeriodStart, PeriodStartLTE: &current.CurrentPeriodStart,
			PeriodEndGTE: &current.CurrentPeriodEnd, PeriodEndLTE: &current.CurrentPeriodEnd,
			SkipLineItems: true,
		})
		if err != nil {
			return err
		}
		if len(invoices) == 0 {
			return nil
		}
		if len(invoices) != 1 || invoices[0] == nil || invoices[0].ID == "" {
			return ierr.NewError("trial activation requires one unambiguous source invoice").Mark(ierr.ErrInvalidOperation)
		}
		sourceID := invoices[0].ID
		// NOWAIT avoids a cycle with legacy payment callbacks that hold the invoice first.
		source, err := s.InvoiceRepo.GetForUpdateNowait(tx, sourceID)
		if err != nil {
			return err
		}
		if source == nil || source.ID != sourceID {
			return ierr.NewError("trial activation lock returned an invalid invoice").Mark(ierr.ErrDatabase)
		}
		if !trialRecoverySourceReady(tx, current, source) {
			return nil
		}
		if err := s.BillingTermsStore.LockEligibility(tx, scopes); err != nil {
			return err
		}
		if current.SubscriptionType == types.SubscriptionTypeParent {
			children, err := s.trialRecoveryChildren(tx, subscriptionID)
			if err != nil {
				return err
			}
			freshIDs := []string{subscriptionID}
			for _, child := range children {
				freshIDs = append(freshIDs, child.ID)
			}
			slices.Sort(freshIDs)
			if !slices.Equal(participantIDs, freshIDs) {
				return trialRecoveryMembershipChanged()
			}
		}
		observation := trialRecoveryObservation(asOf)
		if !trialRecoveryPending(current, observation) {
			return nil
		}
		for _, participantID := range participantIDs {
			participant := locked[participantID]
			if participantID != subscriptionID && (!trialRecoveryChild(participant, current) || !trialRecoveryPending(participant, observation)) {
				continue
			}
			updated := subscription.NewSubscriptionBuilder(participant).WithStatus(types.SubscriptionStatusActive).Build()
			if err := s.SubRepo.Update(activity.WithAction(tx, participantID, activity.SubscriptionActivated), updated); err != nil {
				return err
			}
			activated = append(activated, updated)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	ctx = withoutPendingCollection(types.WithoutDBTransaction(types.WithForceWriter(ctx)))
	for _, sub := range activated {
		// Pending grant applications remain persisted for the existing grant cron after a crash.
		if err := s.processPendingCreditGrantsForSubscription(ctx, sub); err != nil {
			s.Logger.Error(ctx, "trial activation grant processing deferred", "error", err, "subscription_id", sub.ID)
		}
		s.publishSystemEvent(ctx, types.WebhookEventSubscriptionActivated, sub.ID)
		NotifySubscriptionChanged(ctx, s.ServiceParams, sub.ID)
	}
	return len(activated) > 0, nil
}

func (s *subscriptionService) trialRecoveryChildren(ctx context.Context, parentID string) ([]*subscription.Subscription, error) {
	rows, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{
		QueryFilter:           &types.QueryFilter{Limit: lo.ToPtr(domainBillingTerms.MaxEligibilityScopes), Status: lo.ToPtr(types.StatusPublished)},
		ParentSubscriptionIDs: []string{parentID},
		SubscriptionTypes:     []types.SubscriptionType{types.SubscriptionTypeInherited},
		SubscriptionStatus:    types.AllSubscriptionStatuses(),
	})
	if err != nil {
		return nil, err
	}
	if len(rows) >= domainBillingTerms.MaxEligibilityScopes {
		return nil, ierr.Validation("trial activation supports at most 500 subscriptions in one hierarchy")
	}
	seen := map[string]bool{parentID: true}
	for _, child := range rows {
		if child == nil || strings.TrimSpace(child.ID) == "" || len(child.ID) > 50 || seen[child.ID] || !subscriptionUpdateScoped(ctx, child, child.ID) ||
			child.SubscriptionType != types.SubscriptionTypeInherited || lo.FromPtr(child.ParentSubscriptionID) != parentID {
			return nil, ierr.NewError("trial activation membership returned invalid scope").Mark(ierr.ErrDatabase)
		}
		seen[child.ID] = true
	}
	return rows, nil
}

func trialRecoveryRoot(sub *subscription.Subscription) bool {
	if lo.FromPtr(sub.ParentSubscriptionID) != "" {
		return false
	}
	return sub.SubscriptionType == types.SubscriptionTypeStandalone || sub.SubscriptionType == types.SubscriptionTypeDelegatedInvoicing || sub.SubscriptionType == types.SubscriptionTypeParent
}

func trialRecoveryPending(sub *subscription.Subscription, asOf time.Time) bool {
	if sub.SubscriptionStatus != types.SubscriptionStatusIncomplete || sub.PauseStatus == types.PauseStatusActive || sub.ActivePauseID != nil || sub.CancelledAt != nil {
		return false
	}
	if strings.TrimSpace(sub.CustomerID) == "" || strings.TrimSpace(sub.GetInvoicingCustomerID()) == "" || strings.TrimSpace(sub.Currency) == "" {
		return false
	}
	if (sub.CancelAt != nil && !sub.CancelAt.After(asOf)) || (sub.EndDate != nil && !sub.EndDate.After(asOf)) ||
		(sub.CancelAtPeriodEnd && !sub.CurrentPeriodEnd.After(asOf)) {
		return false
	}
	if sub.TrialStart == nil || sub.TrialEnd == nil || !trialRecoveryDate(*sub.TrialStart) || !trialRecoveryDate(*sub.TrialEnd) || !trialRecoveryDate(sub.CurrentPeriodEnd) {
		return false
	}
	return sub.TrialStart.Before(*sub.TrialEnd) && !sub.TrialEnd.After(asOf) &&
		sub.CurrentPeriodStart.Equal(*sub.TrialEnd) && sub.CurrentPeriodStart.Before(sub.CurrentPeriodEnd)
}

func trialRecoveryChild(child, parent *subscription.Subscription) bool {
	return child.SubscriptionType == types.SubscriptionTypeInherited && lo.FromPtr(child.ParentSubscriptionID) == parent.ID &&
		strings.EqualFold(child.Currency, parent.Currency) && child.TrialStart != nil && child.TrialEnd != nil &&
		child.TrialStart.Equal(*parent.TrialStart) && child.TrialEnd.Equal(*parent.TrialEnd) &&
		child.CurrentPeriodStart.Equal(parent.CurrentPeriodStart) && child.CurrentPeriodEnd.Equal(parent.CurrentPeriodEnd)
}

func trialRecoverySourceReady(ctx context.Context, sub *subscription.Subscription, source *invoice.Invoice) bool {
	if source == nil || source.TenantID != types.GetTenantID(ctx) || source.EnvironmentID != types.GetEnvironmentID(ctx) || source.Status != types.StatusPublished ||
		source.InvoiceType != types.InvoiceTypeSubscription || source.BillingReason != string(types.InvoiceBillingReasonSubscriptionTrialEnd) ||
		source.SubscriptionID == nil || *source.SubscriptionID != sub.ID || source.CustomerID != sub.GetInvoicingCustomerID() || !strings.EqualFold(source.DenominationCurrency(), sub.Currency) ||
		source.PeriodStart == nil || source.PeriodEnd == nil || !source.PeriodStart.Equal(sub.CurrentPeriodStart) || !source.PeriodEnd.Equal(sub.CurrentPeriodEnd) ||
		source.AmountRemaining.IsNegative() || source.AmountDue.IsNegative() || source.AmountPaid.IsNegative() {
		return false
	}
	if source.PaymentStatus == types.PaymentStatusVoided || source.PaymentStatus == types.PaymentStatusRefunded || source.PaymentStatus == types.PaymentStatusPartiallyRefunded {
		return false
	}
	switch source.InvoiceStatus {
	case types.InvoiceStatusFinalized:
		if source.AmountRemaining.IsZero() && (source.PaymentStatus == types.PaymentStatusSucceeded || source.PaymentStatus == types.PaymentStatusOverpaid) && !source.AmountPaid.LessThan(source.AmountDue) {
			return true
		}
		if trialRecoveryZero(source) {
			return true
		}
		// Durable activation uses the stored policy, never an invocation-only payment override.
		return sub.PaymentBehavior == string(types.PaymentBehaviorDefaultActive) && source.LastComputedAt != nil && trialRecoveryDate(*source.LastComputedAt) &&
			(sub.CollectionMethod == string(types.CollectionMethodSendInvoice) || sub.CollectionMethod == string(types.CollectionMethodChargeAutomatically)) &&
			source.AmountDue.GreaterThan(source.AmountPaid) &&
			source.AmountRemaining.Equal(source.AmountDue.Sub(source.AmountPaid)) &&
			(source.PaymentStatus == types.PaymentStatusPending || source.PaymentStatus == types.PaymentStatusFailed || source.PaymentStatus == types.PaymentStatusInitiated || source.PaymentStatus == types.PaymentStatusProcessing)
	case types.InvoiceStatusSkipped:
		return trialRecoveryZero(source)
	default:
		return false
	}
}

func trialRecoveryZero(source *invoice.Invoice) bool {
	return source.LastComputedAt != nil && trialRecoveryDate(*source.LastComputedAt) && source.AmountRemaining.IsZero() &&
		source.AmountDue.IsZero() && source.AmountPaid.IsZero() && source.Subtotal.IsZero() && source.Total.IsZero() &&
		(source.CustomCurrency == nil || (source.CustomCurrency.AmountDue.IsZero() && source.CustomCurrency.Subtotal.IsZero() && source.CustomCurrency.Total.IsZero()))
}

func trialRecoveryDate(date time.Time) bool {
	return !date.IsZero() && date.Year() >= 1 && date.Year() <= 9999 && date.UTC().Year() >= 1 && date.UTC().Year() <= 9999
}

func trialRecoveryMembershipChanged() error {
	return ierr.NewError("trial activation membership changed; retry the request").Mark(ierr.ErrVersionConflict)
}

func trialRecoveryObservation(asOf time.Time) time.Time {
	now := time.Now().UTC()
	if asOf.After(now) {
		return asOf
	}
	return now
}

func (s *subscriptionService) validateTrialActivation(ctx context.Context, id string, asOf time.Time) error {
	if ctx == nil || (reflect.ValueOf(ctx).Kind() == reflect.Pointer && reflect.ValueOf(ctx).IsNil()) {
		return ierr.Validation("trial activation requires a context")
	}
	if strings.TrimSpace(id) == "" || len(id) > 50 || types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" || !trialRecoveryDate(asOf) {
		return ierr.Validation("trial activation requires a subscription, scope and valid observation date")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return ierr.NewError("trial activation is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	for _, dependency := range []any{s.DB, s.SubRepo, s.InvoiceRepo, s.BillingTermsStore, s.CreditGrantApplicationRepo, s.Logger, s.WebhookPublisher} {
		value := reflect.ValueOf(dependency)
		if !value.IsValid() || (value.Kind() == reflect.Pointer && value.IsNil()) {
			return ierr.NewError("trial activation is unavailable").Mark(ierr.ErrServiceUnavailable)
		}
	}
	return nil
}

func (s *subscriptionService) requestTrialActivation(ctx context.Context, id string, asOf time.Time) error {
	if err := s.validateTrialActivation(ctx, id, asOf); err != nil {
		return err
	}
	if s.DB.TxFromContext(ctx) == nil && !s.BillingTermsStore.InTransaction(ctx) {
		_, err := s.reconcileTrialActivation(ctx, id, trialRecoveryObservation(asOf))
		return err
	}
	requested := func() {
		detached := withoutPendingCollection(types.WithoutDBTransaction(context.WithoutCancel(ctx)))
		detached, cancel := context.WithTimeout(detached, 10*time.Second)
		defer cancel()
		if _, err := s.reconcileTrialActivation(detached, id, trialRecoveryObservation(asOf)); err != nil {
			s.Logger.Error(detached, "trial activation deferred to recovery sweep", "error", err, "subscription_id", id)
		}
	}
	if !types.RegisterPostCommit(ctx, requested) {
		return ierr.NewError("trial activation requires an active postcommit queue").Mark(ierr.ErrInvalidOperation)
	}
	return nil
}
