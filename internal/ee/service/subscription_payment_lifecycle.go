package service

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/activity"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type paymentLifecycleRequest struct {
	subscriptionID string
	invoiceID      string
	asOf           time.Time
	overdueOnly    bool
	graceDays      int
}

func (s *subscriptionService) requestPaymentLifecycle(ctx context.Context, subscriptionID, invoiceID string) error {
	request := paymentLifecycleRequest{subscriptionID: subscriptionID, invoiceID: invoiceID, asOf: time.Now().UTC()}
	if err := s.validatePaymentLifecycle(ctx, request); err != nil {
		return err
	}
	if s.DB.TxFromContext(ctx) == nil && !s.BillingTermsStore.InTransaction(ctx) {
		return s.reconcilePaymentLifecycle(ctx, request)
	}
	reconcile := func() {
		detached := withoutPendingCollection(types.WithoutDBTransaction(context.WithoutCancel(ctx)))
		detached, cancel := context.WithTimeout(detached, 10*time.Second)
		defer cancel()
		request.asOf = time.Now().UTC()
		if err := s.reconcilePaymentLifecycle(detached, request); err != nil {
			s.Logger.Error(detached, "subscription payment status deferred to recovery sweep", "error", err, "subscription_id", subscriptionID, "invoice_id", invoiceID)
		}
	}
	if !types.RegisterPostCommit(ctx, reconcile) {
		return ierr.NewError("payment status reconciliation requires an active postcommit queue").Mark(ierr.ErrInvalidOperation)
	}
	return nil
}

func (s *subscriptionService) validatePaymentLifecycle(ctx context.Context, request paymentLifecycleRequest) error {
	if err := s.validateSubscriptionLifecycle(ctx, request.subscriptionID); err != nil {
		return err
	}
	if (request.invoiceID != "" && strings.TrimSpace(request.invoiceID) == "") || len(request.invoiceID) > 50 || !trialRecoveryDate(request.asOf) || request.graceDays < 0 {
		return ierr.Validation("payment lifecycle requires a valid invoice, observation date and grace period")
	}
	for _, dependency := range []any{s.Logger, s.WebhookPublisher, s.CreditGrantApplicationRepo} {
		value := reflect.ValueOf(dependency)
		if !value.IsValid() || (value.Kind() == reflect.Pointer && value.IsNil()) {
			return ierr.NewError("payment lifecycle is unavailable").Mark(ierr.ErrServiceUnavailable)
		}
	}
	return nil
}

func (s *subscriptionService) reconcilePaymentLifecycle(ctx context.Context, request paymentLifecycleRequest) error {
	if err := s.validatePaymentLifecycle(ctx, request); err != nil {
		return err
	}
	if s.DB.TxFromContext(ctx) != nil || s.BillingTermsStore.InTransaction(ctx) {
		return ierr.NewError("payment status reconciliation requires its own transaction").Mark(ierr.ErrInvalidOperation)
	}
	var changed, grantOwners []*subscription.Subscription
	err := s.DB.WithTx(types.WithForceWriter(ctx), func(tx context.Context) error {
		discovered, err := s.lifecycleSubscription(tx, request.subscriptionID)
		if ierr.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !paymentLifecycleOwner(discovered, request.asOf) {
			return nil
		}
		current, participants, err := s.lockPaymentSubscriptions(tx, discovered, false)
		if err != nil {
			return err
		}
		if !paymentLifecycleOwner(current, request.asOf) {
			return nil
		}
		sources, err := s.paymentLifecycleInvoices(tx, current, request.invoiceID)
		if err != nil {
			return err
		}
		target, ready := paymentLifecycleStatus(current, sources, request)
		if target == current.SubscriptionStatus {
			if ready && target == types.SubscriptionStatusActive {
				grantOwners = append(grantOwners, current)
			}
			return nil
		}
		for _, participant := range participants {
			id := participant.ID
			if id != current.ID && !paymentLifecycleChild(current, participant, request.asOf) {
				continue
			}
			updated := subscription.NewSubscriptionBuilder(participant).WithStatus(target).Build()
			writeCtx := tx
			if target == types.SubscriptionStatusActive {
				writeCtx = activity.WithAction(tx, id, activity.SubscriptionActivated)
			}
			if err := s.SubRepo.Update(writeCtx, updated); err != nil {
				return err
			}
			changed = append(changed, updated)
			if target == types.SubscriptionStatusActive {
				grantOwners = append(grantOwners, updated)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, owner := range grantOwners {
		if err := s.processPendingCreditGrantsForSubscription(ctx, owner); err != nil {
			s.Logger.Error(ctx, "subscription payment grants deferred to recovery", "error", err, "subscription_id", owner.ID)
		}
	}
	for _, owner := range changed {
		event := types.WebhookEventSubscriptionUpdated
		if owner.SubscriptionStatus == types.SubscriptionStatusActive {
			event = types.WebhookEventSubscriptionActivated
		}
		s.publishSystemEvent(ctx, event, owner.ID)
		NotifySubscriptionChanged(ctx, s.ServiceParams, owner.ID)
		s.Logger.Info(ctx, "subscription payment status reconciled", "subscription_id", owner.ID, "status", owner.SubscriptionStatus)
	}
	return nil
}

func (s *subscriptionService) lockPaymentSubscriptions(ctx context.Context, discovered *subscription.Subscription, nonblocking bool) (*subscription.Subscription, []*subscription.Subscription, error) {
	ids, err := s.paymentLifecycleParticipants(ctx, discovered)
	if err != nil {
		return nil, nil, err
	}
	scopes := make([]domain.EligibilityScope, 0, len(ids))
	for _, id := range ids {
		scopes = append(scopes, domain.EligibilityScope{Type: "subscription", ID: id})
	}
	if nonblocking {
		store, ok := s.BillingTermsStore.(domain.NonblockingEligibilityStore)
		if !ok {
			return nil, nil, ierr.NewError("nested payment lifecycle requires nonblocking eligibility locks").Mark(ierr.ErrServiceUnavailable)
		}
		err = store.TryLockEligibility(ctx, scopes)
	} else {
		err = s.BillingTermsStore.LockEligibility(ctx, scopes)
	}
	if err != nil {
		return nil, nil, err
	}
	var current *subscription.Subscription
	participants := make([]*subscription.Subscription, 0, len(ids))
	for _, id := range ids {
		participant, err := s.SubRepo.GetForUpdateNowait(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if !subscriptionUpdateScoped(ctx, participant, id) {
			return nil, nil, ierr.NewError("payment lifecycle returned invalid subscription scope").Mark(ierr.ErrDatabase)
		}
		participants = append(participants, participant)
		if id == discovered.ID {
			current = participant
		}
	}
	freshIDs, err := s.paymentLifecycleParticipants(ctx, current)
	if err != nil {
		return nil, nil, err
	}
	if !slices.Equal(ids, freshIDs) {
		return nil, nil, lifecycleMembershipChanged()
	}
	return current, participants, nil
}

func paymentLifecycleChild(parent, child *subscription.Subscription, asOf time.Time) bool {
	return child.SubscriptionType == types.SubscriptionTypeInherited &&
		lo.FromPtr(child.ParentSubscriptionID) == parent.ID && child.SubscriptionStatus == parent.SubscriptionStatus &&
		paymentLifecycleOwner(child, asOf) && child.CurrentPeriodStart.Equal(parent.CurrentPeriodStart) && child.CurrentPeriodEnd.Equal(parent.CurrentPeriodEnd)
}

func (s *subscriptionService) paymentLifecycleParticipants(ctx context.Context, owner *subscription.Subscription) ([]string, error) {
	ids := []string{owner.ID}
	if parent := lo.FromPtr(owner.ParentSubscriptionID); parent != "" {
		ids = append(ids, parent)
	}
	if owner.SubscriptionType == types.SubscriptionTypeParent {
		children, err := s.lifecycleChildren(ctx, owner.ID)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			ids = append(ids, child.ID)
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > domain.MaxEligibilityScopes {
		return nil, ierr.Validation("payment lifecycle supports at most 500 subscriptions")
	}
	return ids, nil
}

func (s *subscriptionService) paymentLifecycleInvoices(ctx context.Context, owner *subscription.Subscription, requestedID string) ([]*invoice.Invoice, error) {
	currentFilter := &types.InvoiceFilter{QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(501), Status: lo.ToPtr(types.StatusPublished)}, SubscriptionID: owner.ID, InvoiceType: types.InvoiceTypeSubscription, InvoiceStatus: []types.InvoiceStatus{types.InvoiceStatusDraft, types.InvoiceStatusFinalized, types.InvoiceStatusSkipped}, PeriodEndGTE: &owner.CurrentPeriodStart, SkipLineItems: true}
	current, err := s.InvoiceRepo.List(ctx, currentFilter)
	if err != nil {
		return nil, err
	}
	unpaid, err := s.InvoiceRepo.List(ctx, &types.InvoiceFilter{QueryFilter: &types.QueryFilter{Limit: lo.ToPtr(501), Status: lo.ToPtr(types.StatusPublished)}, SubscriptionID: owner.ID, InvoiceType: types.InvoiceTypeSubscription, InvoiceStatus: []types.InvoiceStatus{types.InvoiceStatusFinalized}, AmountRemainingGt: lo.ToPtr(decimal.Zero), SkipLineItems: true})
	if err != nil {
		return nil, err
	}
	ids := []string{}
	if requestedID != "" {
		ids = append(ids, requestedID)
	}
	for _, rows := range [][]*invoice.Invoice{current, unpaid} {
		if len(rows) > 500 {
			return nil, ierr.Validation("payment lifecycle supports at most 500 relevant invoices")
		}
		for _, source := range rows {
			if !paymentInvoiceScoped(ctx, owner, source) {
				return nil, ierr.NewError("payment lifecycle returned invalid invoice scope").Mark(ierr.ErrDatabase)
			}
			ids = append(ids, source.ID)
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > 500 {
		return nil, ierr.Validation("payment lifecycle supports at most 500 relevant invoices")
	}
	sources := make([]*invoice.Invoice, 0, len(ids))
	for _, id := range ids {
		source, err := s.InvoiceRepo.GetForUpdateNowait(ctx, id)
		if err != nil {
			return nil, err
		}
		if !paymentInvoiceScoped(ctx, owner, source) || source.ID != id {
			return nil, ierr.NewError("payment lifecycle returned invalid locked invoice scope").Mark(ierr.ErrDatabase)
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func paymentInvoiceScoped(ctx context.Context, owner *subscription.Subscription, source *invoice.Invoice) bool {
	return source != nil && source.ID != "" && source.TenantID == types.GetTenantID(ctx) && source.EnvironmentID == types.GetEnvironmentID(ctx) && source.Status == types.StatusPublished && source.SubscriptionID != nil && *source.SubscriptionID == owner.ID && source.InvoiceType == types.InvoiceTypeSubscription && source.CustomerID == owner.GetInvoicingCustomerID() && strings.EqualFold(source.DenominationCurrency(), owner.Currency)
}

func paymentLifecycleOwner(owner *subscription.Subscription, asOf time.Time) bool {
	if owner == nil || (owner.SubscriptionStatus != types.SubscriptionStatusActive && owner.SubscriptionStatus != types.SubscriptionStatusIncomplete) || owner.PauseStatus == types.PauseStatusActive || owner.ActivePauseID != nil || owner.CancelledAt != nil {
		return false
	}
	if (owner.EndDate != nil && !owner.EndDate.After(asOf)) || (owner.CancelAt != nil && !owner.CancelAt.After(asOf)) || (owner.CancelAtPeriodEnd && !owner.CurrentPeriodEnd.After(asOf)) {
		return false
	}
	if !trialRecoveryDate(owner.CurrentPeriodStart) || !trialRecoveryDate(owner.CurrentPeriodEnd) || !owner.CurrentPeriodStart.Before(owner.CurrentPeriodEnd) {
		return false
	}
	if owner.TrialEnd != nil && (owner.TrialEnd.After(asOf) || owner.TrialEnd.Equal(owner.CurrentPeriodStart)) {
		return false
	}
	return owner.CustomerID != "" && owner.GetInvoicingCustomerID() != "" && owner.Currency != ""
}

func paymentLifecycleStatus(owner *subscription.Subscription, sources []*invoice.Invoice, request paymentLifecycleRequest) (types.SubscriptionStatus, bool) {
	gated := types.PaymentBehavior(owner.PaymentBehavior).IsIncompleteType()
	ready, blocked, downgrade := false, false, false
	for _, source := range sources {
		reason := types.InvoiceBillingReason(source.BillingReason)
		if reason != types.InvoiceBillingReasonSubscriptionCreate && reason != types.InvoiceBillingReasonSubscriptionCycle {
			continue
		}
		pending := paymentInvoicePending(source)
		awaiting := paymentInvoiceAwaitingPayment(source)
		due := source.DueDate != nil && !source.DueDate.After(request.asOf)
		automatic := owner.CollectionMethod == string(types.CollectionMethodChargeAutomatically)
		if gated && awaiting {
			if reason == types.InvoiceBillingReasonSubscriptionCreate || automatic || due {
				blocked = true
			}
			if reason == types.InvoiceBillingReasonSubscriptionCycle && pending {
				if request.overdueOnly {
					downgrade = downgrade || (due && source.DueDate.After(request.asOf.AddDate(0, 0, -request.graceDays)))
				} else {
					downgrade = downgrade || (due || (automatic && source.ID == request.invoiceID))
				}
			}
		}
		if !paymentInvoiceCurrent(owner, source) {
			continue
		}
		if source.InvoiceStatus == types.InvoiceStatusDraft {
			blocked = true
			continue
		}
		ready = ready || paymentInvoiceSettled(source)
		if owner.PaymentBehavior == string(types.PaymentBehaviorDefaultActive) && source.InvoiceStatus == types.InvoiceStatusFinalized && source.LastComputedAt != nil && trialRecoveryDate(*source.LastComputedAt) && awaiting {
			ready = true
		}
	}
	if owner.SubscriptionStatus == types.SubscriptionStatusActive && gated && downgrade {
		return types.SubscriptionStatusIncomplete, false
	}
	if owner.SubscriptionStatus == types.SubscriptionStatusIncomplete && ready && !blocked {
		return types.SubscriptionStatusActive, true
	}
	return owner.SubscriptionStatus, ready && !blocked
}

func paymentInvoiceCurrent(owner *subscription.Subscription, source *invoice.Invoice) bool {
	return source.PeriodStart != nil && source.PeriodEnd != nil && trialRecoveryDate(*source.PeriodStart) && trialRecoveryDate(*source.PeriodEnd) && source.PeriodStart.Before(*source.PeriodEnd) && !source.PeriodStart.After(owner.CurrentPeriodStart) && !source.PeriodEnd.Before(owner.CurrentPeriodStart)
}

func paymentInvoicePending(source *invoice.Invoice) bool {
	return (source.PaymentStatus == types.PaymentStatusPending || source.PaymentStatus == types.PaymentStatusFailed) && paymentInvoiceAwaitingPayment(source)
}

func paymentInvoiceSettled(source *invoice.Invoice) bool {
	if source.InvoiceStatus == types.InvoiceStatusSkipped {
		return trialRecoveryZero(source)
	}
	if source.InvoiceStatus != types.InvoiceStatusFinalized {
		return false
	}
	if trialRecoveryZero(source) {
		return true
	}
	return !source.AmountDue.IsNegative() && !source.AmountPaid.IsNegative() && source.AmountRemaining.IsZero() && !source.AmountPaid.LessThan(source.AmountDue) && (source.PaymentStatus == types.PaymentStatusSucceeded || source.PaymentStatus == types.PaymentStatusOverpaid)
}

func paymentInvoiceAwaitingPayment(source *invoice.Invoice) bool {
	return source.InvoiceStatus == types.InvoiceStatusFinalized &&
		(source.PaymentStatus == types.PaymentStatusPending || source.PaymentStatus == types.PaymentStatusFailed || source.PaymentStatus == types.PaymentStatusInitiated || source.PaymentStatus == types.PaymentStatusProcessing) &&
		source.AmountDue.IsPositive() && !source.AmountPaid.IsNegative() && source.AmountRemaining.IsPositive() && source.AmountRemaining.Equal(source.AmountDue.Sub(source.AmountPaid))
}
