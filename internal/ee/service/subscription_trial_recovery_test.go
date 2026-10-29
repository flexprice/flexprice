package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type trialRecoverySubscriptions struct {
	subscription.Repository
	order      *[]string
	beforeLock func(string)
	onList     func(context.Context)
}

func (r *trialRecoverySubscriptions) List(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	if r.onList != nil {
		r.onList(ctx)
	}
	return r.Repository.List(ctx, filter)
}

func (r *trialRecoverySubscriptions) GetForUpdateNowait(ctx context.Context, id string) (*subscription.Subscription, error) {
	if r.order != nil {
		*r.order = append(*r.order, "subscription:"+id)
	}
	if r.beforeLock != nil {
		r.beforeLock(id)
	}
	return r.Repository.GetForUpdateNowait(ctx, id)
}

type trialRecoveryInvoices struct {
	invoice.Repository
	order      *[]string
	beforeLock func(string)
	lockErr    error
}

func (r *trialRecoveryInvoices) GetForUpdateNowait(ctx context.Context, id string) (*invoice.Invoice, error) {
	if r.order != nil {
		*r.order = append(*r.order, "invoice:"+id)
	}
	if r.beforeLock != nil {
		r.beforeLock(id)
	}
	if r.lockErr != nil {
		return nil, r.lockErr
	}
	return r.Repository.GetForUpdateNowait(ctx, id)
}

type trialRecoveryStore struct {
	domain.ExecutionStore
	scopes []domain.EligibilityScope
	order  *[]string
	hook   func()
	err    error
	nested bool
}

func (s *trialRecoveryStore) InTransaction(context.Context) bool { return s.nested }

func (s *trialRecoveryStore) LockEligibility(_ context.Context, scopes []domain.EligibilityScope) error {
	s.scopes = append([]domain.EligibilityScope(nil), scopes...)
	if s.order != nil {
		*s.order = append(*s.order, "eligibility")
	}
	if s.hook != nil {
		s.hook()
	}
	return s.err
}

type trialRecoveryDB struct {
	postgres.IClient
	inTx bool
}

func (d *trialRecoveryDB) WithTx(ctx context.Context, fn func(context.Context) error) error {
	d.inTx = true
	err := d.IClient.WithTx(ctx, fn)
	d.inTx = false
	return err
}

type trialRecoveryGrants struct {
	creditgrantapplication.Repository
	t     *testing.T
	db    *trialRecoveryDB
	reads int
}

type trialRecoveryCappedSubscriptions struct {
	subscription.Repository
}

func (r *trialRecoveryCappedSubscriptions) List(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	if len(filter.ParentSubscriptionIDs) > 0 {
		return make([]*subscription.Subscription, domain.MaxEligibilityScopes), nil
	}
	return r.Repository.List(ctx, filter)
}

func (r *trialRecoveryGrants) List(ctx context.Context, filter *types.CreditGrantApplicationFilter) ([]*creditgrantapplication.CreditGrantApplication, error) {
	require.False(r.t, r.db.inTx, "grant processing must follow the outer commit")
	require.Nil(r.t, ctx.Value(types.CtxDBTransaction))
	r.reads++
	return nil, nil
}

func newTrialRecoveryFixture(t *testing.T) (*SubscriptionPauseSuite, *subscriptionService, *subscription.Subscription, *invoice.Invoice, time.Time) {
	t.Helper()
	s, sub := newSubscriptionUpdateFixture(t)
	sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
	sub.TrialEnd = lo.ToPtr(sub.CurrentPeriodStart)
	sub.TrialStart = lo.ToPtr(sub.CurrentPeriodStart.AddDate(0, -1, 0))
	asOf := sub.CurrentPeriodStart.Add(24 * time.Hour)
	paid := &invoice.Invoice{
		ID: "invoice_trial_recovery", CustomerID: sub.GetInvoicingCustomerID(), SubscriptionID: &sub.ID,
		InvoiceType: types.InvoiceTypeSubscription, InvoiceStatus: types.InvoiceStatusFinalized,
		PaymentStatus: types.PaymentStatusSucceeded, BillingReason: string(types.InvoiceBillingReasonSubscriptionTrialEnd),
		Currency: sub.Currency, AmountDue: decimal.NewFromInt(100), AmountPaid: decimal.NewFromInt(100),
		Subtotal: decimal.NewFromInt(100), Total: decimal.NewFromInt(100), AmountRemaining: decimal.Zero,
		PeriodStart: &sub.CurrentPeriodStart, PeriodEnd: &sub.CurrentPeriodEnd, LastComputedAt: &asOf,
		EnvironmentID: sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	require.NoError(t, s.params.InvoiceRepo.Create(s.GetContext(), paid))
	svc := s.service.(*subscriptionService)
	svc.BillingTermsStore = &trialRecoveryStore{}
	return s, svc, sub, paid, asOf
}

func TestTrialActivationRecoveryPaidAndSkipped(t *testing.T) {
	for _, status := range []types.InvoiceStatus{types.InvoiceStatusFinalized, types.InvoiceStatusSkipped} {
		t.Run(string(status), func(t *testing.T) {
			s, svc, sub, source, asOf := newTrialRecoveryFixture(t)
			if status == types.InvoiceStatusSkipped {
				source.InvoiceStatus = status
				source.PaymentStatus = types.PaymentStatusPending
				source.AmountDue, source.AmountPaid, source.Subtotal, source.Total = decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
				require.NoError(t, svc.InvoiceRepo.Update(s.GetContext(), source))
			}
			order := []string{}
			svc.SubRepo = &trialRecoverySubscriptions{Repository: svc.SubRepo, order: &order}
			svc.InvoiceRepo = &trialRecoveryInvoices{Repository: svc.InvoiceRepo, order: &order}
			store := &trialRecoveryStore{order: &order}
			svc.BillingTermsStore = store
			db := &trialRecoveryDB{IClient: svc.DB}
			svc.DB = db
			grants := &trialRecoveryGrants{t: t, db: db}
			svc.CreditGrantApplicationRepo = grants
			changed, err := svc.reconcileTrialActivation(s.GetContext(), sub.ID, asOf)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, []string{"subscription:" + sub.ID, "invoice:" + source.ID, "eligibility"}, order)
			require.Equal(t, []domain.EligibilityScope{{Type: "subscription", ID: sub.ID}}, store.scopes)
			stored, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
			require.Equal(t, sub.CurrentPeriodStart, stored.CurrentPeriodStart)
			require.Equal(t, sub.CurrentPeriodEnd, stored.CurrentPeriodEnd)
			require.Equal(t, 1, grants.reads)
			require.Len(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events(), 1)
			changed, err = svc.reconcileTrialActivation(s.GetContext(), sub.ID, asOf)
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, 1, grants.reads)
			require.Len(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events(), 1)
		})
	}
}

func TestTrialActivationRecoveryRejectsIneligibleState(t *testing.T) {
	for _, scenario := range []string{"paused", "cancelled", "due cancellation", "due period cancellation", "ended", "later period", "trialing", "inherited", "grouped", "linked standalone", "missing customer", "missing currency", "unpaid", "draft", "wrong reason", "wrong recipient", "wrong currency", "wrong period", "uncomputed skipped", "invalid computed date", "nonzero skipped denomination", "ambiguous invoices"} {
		t.Run(scenario, func(t *testing.T) {
			s, svc, sub, source, asOf := newTrialRecoveryFixture(t)
			switch scenario {
			case "paused":
				sub.PauseStatus = types.PauseStatusActive
			case "cancelled":
				sub.CancelledAt = &asOf
			case "due cancellation":
				sub.CancelAt = &asOf
			case "due period cancellation":
				sub.CancelAtPeriodEnd = true
				asOf = sub.CurrentPeriodEnd
			case "ended":
				sub.EndDate = &asOf
			case "later period":
				sub.CurrentPeriodStart = sub.CurrentPeriodStart.AddDate(0, 1, 0)
				sub.CurrentPeriodEnd = sub.CurrentPeriodEnd.AddDate(0, 1, 0)
			case "trialing":
				sub.SubscriptionStatus = types.SubscriptionStatusTrialing
			case "inherited":
				sub.SubscriptionType = types.SubscriptionTypeInherited
			case "grouped":
				sub.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
			case "linked standalone":
				sub.ParentSubscriptionID = lo.ToPtr("other_parent")
			case "missing customer":
				sub.CustomerID = ""
			case "missing currency":
				sub.Currency = ""
			case "unpaid":
				source.PaymentStatus = types.PaymentStatusPending
				source.AmountRemaining = decimal.NewFromInt(100)
			case "draft":
				source.InvoiceStatus = types.InvoiceStatusDraft
			case "wrong reason":
				source.BillingReason = string(types.InvoiceBillingReasonSubscriptionTrialStart)
			case "wrong recipient":
				source.CustomerID = "other_customer"
			case "wrong currency":
				source.Currency = "eur"
			case "wrong period":
				source.PeriodEnd = lo.ToPtr(source.PeriodEnd.Add(time.Hour))
			case "uncomputed skipped":
				source.InvoiceStatus = types.InvoiceStatusSkipped
				source.AmountDue, source.AmountPaid, source.Subtotal, source.Total = decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
				source.LastComputedAt = nil
			case "invalid computed date", "nonzero skipped denomination":
				source.InvoiceStatus = types.InvoiceStatusSkipped
				source.AmountDue, source.AmountPaid, source.Subtotal, source.Total = decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
				if scenario == "invalid computed date" {
					source.LastComputedAt = lo.ToPtr(time.Time{})
				} else {
					source.CustomCurrency = &types.CustomCurrency{Code: source.Currency, AmountDue: decimal.NewFromInt(1)}
				}
			case "ambiguous invoices":
				duplicate := *source
				duplicate.ID = "another_trial_invoice"
				require.NoError(t, svc.InvoiceRepo.Create(s.GetContext(), &duplicate))
			}
			require.NoError(t, svc.InvoiceRepo.Update(s.GetContext(), source))
			before := sub.SubscriptionStatus
			changed, err := svc.reconcileTrialActivation(s.GetContext(), sub.ID, asOf)
			if scenario == "ambiguous invoices" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.False(t, changed)
			stored, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			require.Equal(t, before, stored.SubscriptionStatus)
			require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
		})
	}
}

func TestTrialActivationRecoveryParentAndMembershipDrift(t *testing.T) {
	for _, drift := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete hierarchy", true: "membership drift"}[drift], func(t *testing.T) {
			s, svc, parent, _, asOf := newTrialRecoveryFixture(t)
			parent.SubscriptionType = types.SubscriptionTypeParent
			for _, id := range []string{"a_incomplete", "z_paused", "z_cancelled"} {
				child := *parent
				child.ID, child.SubscriptionType, child.ParentSubscriptionID = id, types.SubscriptionTypeInherited, &parent.ID
				if id == "z_paused" {
					child.SubscriptionStatus = types.SubscriptionStatusPaused
				}
				if id == "z_cancelled" {
					child.SubscriptionStatus = types.SubscriptionStatusCancelled
				}
				require.NoError(t, svc.SubRepo.Create(s.GetContext(), &child))
			}
			order := []string{}
			svc.SubRepo = &trialRecoverySubscriptions{Repository: svc.SubRepo, order: &order}
			store := &trialRecoveryStore{order: &order}
			svc.BillingTermsStore = store
			if drift {
				store.hook = func() {
					child := *parent
					child.ID, child.SubscriptionType, child.ParentSubscriptionID = "late_child", types.SubscriptionTypeInherited, &parent.ID
					require.NoError(t, svc.SubRepo.Create(s.GetContext(), &child))
				}
			}
			changed, err := svc.reconcileTrialActivation(s.GetContext(), parent.ID, asOf)
			if drift {
				require.True(t, ierr.IsVersionConflict(err), "%+v", err)
				require.False(t, changed)
				require.NotContains(t, order, "subscription:late_child")
				require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
				return
			}
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, []string{"subscription:a_incomplete", "subscription:" + parent.ID, "subscription:z_cancelled", "subscription:z_paused", "eligibility"}, order)
			for id, status := range map[string]types.SubscriptionStatus{parent.ID: types.SubscriptionStatusActive, "a_incomplete": types.SubscriptionStatusActive, "z_paused": types.SubscriptionStatusPaused, "z_cancelled": types.SubscriptionStatusCancelled} {
				stored, err := svc.SubRepo.Get(s.GetContext(), id)
				require.NoError(t, err)
				require.Equal(t, status, stored.SubscriptionStatus)
			}
			require.Len(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events(), 2)
		})
	}
}

func TestTrialActivationRecoveryFreshLocksAndFailures(t *testing.T) {
	for _, scenario := range []string{"fresh pause", "fresh void", "missing store", "nested transaction", "guard error", "missing scope", "invalid date"} {
		t.Run(scenario, func(t *testing.T) {
			s, svc, sub, _, asOf := newTrialRecoveryFixture(t)
			ctx := s.GetContext()
			switch scenario {
			case "fresh pause":
				svc.SubRepo = &trialRecoverySubscriptions{Repository: svc.SubRepo, beforeLock: func(string) { sub.PauseStatus = types.PauseStatusActive }}
			case "fresh void":
				svc.InvoiceRepo = &trialRecoveryInvoices{Repository: svc.InvoiceRepo, beforeLock: func(id string) {
					source, err := s.params.InvoiceRepo.Get(ctx, id)
					require.NoError(t, err)
					source.InvoiceStatus = types.InvoiceStatusVoided
					require.NoError(t, s.params.InvoiceRepo.Update(ctx, source))
				}}
			case "missing store":
				svc.BillingTermsStore = nil
			case "nested transaction":
				svc.BillingTermsStore = &trialRecoveryStore{nested: true}
			case "guard error":
				svc.BillingTermsStore = &trialRecoveryStore{err: errors.New("guard failed")}
			case "missing scope":
				ctx = types.SetEnvironmentID(ctx, "")
			case "invalid date":
				asOf = time.Time{}
			}
			changed, err := svc.reconcileTrialActivation(ctx, sub.ID, asOf)
			if scenario == "fresh pause" || scenario == "fresh void" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.False(t, changed)
			stored, err := s.params.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			require.Equal(t, types.SubscriptionStatusIncomplete, stored.SubscriptionStatus)
			require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
		})
	}
}

func TestTrialActivationRecoveryHierarchyLimit(t *testing.T) {
	s, svc, parent, _, asOf := newTrialRecoveryFixture(t)
	parent.SubscriptionType = types.SubscriptionTypeParent
	svc.SubRepo = &trialRecoveryCappedSubscriptions{Repository: svc.SubRepo}
	changed, err := svc.reconcileTrialActivation(s.GetContext(), parent.ID, asOf)
	require.True(t, ierr.IsValidation(err), "%+v", err)
	require.False(t, changed)
	require.Empty(t, svc.BillingTermsStore.(*trialRecoveryStore).scopes)
	require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
}

func TestTrialActivationRecoveryDelegatedCustomCurrency(t *testing.T) {
	s, svc, sub, source, asOf := newTrialRecoveryFixture(t)
	sub.SubscriptionType = types.SubscriptionTypeDelegatedInvoicing
	sub.InvoicingCustomerID = lo.ToPtr("delegated_payer")
	sub.Currency = "credits"
	source.CustomerID = *sub.InvoicingCustomerID
	source.Currency = "usd"
	source.CustomCurrency = &types.CustomCurrency{Code: "credits", Rate: decimal.NewFromInt(1), AmountDue: source.AmountDue, Subtotal: source.Subtotal, Total: source.Total}
	source.PaymentStatus = types.PaymentStatusOverpaid
	source.AmountPaid = source.AmountDue.Add(decimal.NewFromInt(10))
	require.NoError(t, svc.InvoiceRepo.Update(s.GetContext(), source))
	changed, err := svc.reconcileTrialActivation(s.GetContext(), sub.ID, asOf)
	require.NoError(t, err)
	require.True(t, changed)
	stored, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
	require.Equal(t, "credits", stored.Currency)
}

func TestTrialActivationRequestCommitBoundary(t *testing.T) {
	for _, mode := range []string{"commit", "rollback", "immediate", "missing hooks", "closed hooks"} {
		t.Run(mode, func(t *testing.T) {
			s, svc, sub, source, _ := newTrialRecoveryFixture(t)
			setTrialRecoveryPastPeriod(t, s, sub, source)
			ctx, cancel := context.WithCancel(s.GetContext())
			defer cancel()
			if mode != "immediate" {
				ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
				if mode != "missing hooks" {
					ctx = types.WithPostCommitHooks(ctx)
				}
				if mode == "closed hooks" {
					types.RunPostCommitHooks(ctx)
				}
			}
			reads := 0
			svc.SubRepo = &trialRecoverySubscriptions{Repository: svc.SubRepo, onList: func(read context.Context) {
				reads++
				require.Nil(t, svc.DB.TxFromContext(read))
				require.NoError(t, read.Err())
				require.Equal(t, sub.TenantID, types.GetTenantID(read))
				require.Equal(t, sub.EnvironmentID, types.GetEnvironmentID(read))
				if mode == "commit" {
					deadline, ok := read.Deadline()
					require.True(t, ok)
					require.Positive(t, time.Until(deadline))
					require.LessOrEqual(t, time.Until(deadline), 10*time.Second)
				}
			}}
			err := svc.completeTrialConversionToActive(ctx, sub)
			if mode == "missing hooks" || mode == "closed hooks" {
				require.True(t, ierr.IsInvalidOperation(err), "%+v", err)
				require.Zero(t, reads)
				return
			}
			require.NoError(t, err)
			if mode != "immediate" {
				require.Zero(t, reads, "invoice transaction must not take subscription locks")
				require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
				cancel()
				if mode == "rollback" {
					types.DiscardPostCommitHooks(ctx)
				}
				types.RunPostCommitHooks(ctx)
			}
			stored, err := s.params.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			if mode == "rollback" {
				require.Zero(t, reads)
				require.Equal(t, types.SubscriptionStatusIncomplete, stored.SubscriptionStatus)
				require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
				return
			}
			require.Positive(t, reads)
			require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
			require.Len(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events(), 1)
			types.RunPostCommitHooks(ctx)
			require.Len(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events(), 1)
		})
	}
}

func TestTrialActivationRequestFailureRemainsRecoverable(t *testing.T) {
	s, svc, sub, source, asOf := newTrialRecoveryFixture(t)
	setTrialRecoveryPastPeriod(t, s, sub, source)
	ctx := types.WithPostCommitHooks(context.WithValue(s.GetContext(), types.CtxDBTransaction, &ent.Tx{}))
	busy := &trialRecoveryInvoices{Repository: svc.InvoiceRepo, lockErr: ierr.NewError("invoice is busy").Mark(ierr.ErrVersionConflict)}
	svc.InvoiceRepo = busy
	require.NoError(t, svc.requestTrialActivation(ctx, sub.ID, asOf))
	types.RunPostCommitHooks(ctx)
	stored, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusIncomplete, stored.SubscriptionStatus)
	require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
	busy.lockErr = nil
	changed, err := svc.reconcileTrialActivation(s.GetContext(), sub.ID, asOf)
	require.NoError(t, err)
	require.True(t, changed)
}

func TestTrialActivationRequestObservation(t *testing.T) {
	for _, scenario := range []string{"supplied future observation", "cancellation after supplied observation"} {
		t.Run(scenario, func(t *testing.T) {
			s, svc, sub, source, asOf := newTrialRecoveryFixture(t)
			if scenario == "cancellation after supplied observation" {
				setTrialRecoveryPastPeriod(t, s, sub, source)
				asOf = sub.CurrentPeriodStart.Add(time.Hour)
				sub.CancelAt = lo.ToPtr(asOf.Add(time.Hour))
			}
			ctx := types.WithPostCommitHooks(context.WithValue(s.GetContext(), types.CtxDBTransaction, &ent.Tx{}))
			require.NoError(t, svc.requestTrialActivation(ctx, sub.ID, asOf))
			types.RunPostCommitHooks(ctx)
			stored, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			if scenario == "cancellation after supplied observation" {
				require.Equal(t, types.SubscriptionStatusIncomplete, stored.SubscriptionStatus)
				require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
			} else {
				require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
			}
		})
	}
}

func setTrialRecoveryPastPeriod(t *testing.T, s *SubscriptionPauseSuite, sub *subscription.Subscription, source *invoice.Invoice) {
	t.Helper()
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	sub.CurrentPeriodStart, sub.CurrentPeriodEnd = start, start.AddDate(0, 1, 0)
	sub.TrialStart, sub.TrialEnd = lo.ToPtr(start.AddDate(0, -1, 0)), &start
	source.PeriodStart, source.PeriodEnd = &sub.CurrentPeriodStart, &sub.CurrentPeriodEnd
	source.LastComputedAt = &start
	require.NoError(t, s.params.InvoiceRepo.Update(s.GetContext(), source))
}
