package service

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

type lifecycleTestDB struct{ postgres.IClient }

func (d *lifecycleTestDB) TryLock(ctx context.Context, req postgres.LockRequest) error {
	locker, ok := d.IClient.(postgres.NonblockingLocker)
	if !ok {
		return ierr.NewError("nonblocking locks unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	return locker.TryLock(ctx, req)
}

func (d *lifecycleTestDB) WithTx(ctx context.Context, fn func(context.Context) error) error {
	if d.TxFromContext(ctx) != nil {
		return fn(ctx)
	}
	tx := types.WithForceWriter(context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{}))
	tx = types.WithTxStore(types.WithPostCommitHooks(tx))
	if err := fn(tx); err != nil {
		types.DiscardPostCommitHooks(tx)
		return err
	}
	types.RunPostCommitHooks(tx)
	return nil
}

type lifecycleTestStore struct {
	domain.ExecutionStore
	order  *[]string
	scopes []domain.EligibilityScope
	hook   func(context.Context)
	err    error
	tries  int
}

func (s *subscriptionUpdateStore) TryLockEligibility(ctx context.Context, scopes []domain.EligibilityScope) error {
	return s.LockEligibility(ctx, scopes)
}

func (s *lifecycleTestStore) TryLockEligibility(ctx context.Context, scopes []domain.EligibilityScope) error {
	s.tries++
	return s.LockEligibility(ctx, scopes)
}

type lifecycleBlockingStore struct{ domain.ExecutionStore }

func (s *lifecycleBlockingStore) InTransaction(ctx context.Context) bool {
	return ctx.Value(types.CtxDBTransaction) != nil
}

func (s *lifecycleBlockingStore) LockEligibility(context.Context, []domain.EligibilityScope) error {
	return nil
}

func (s *lifecycleTestStore) InTransaction(ctx context.Context) bool {
	return ctx.Value(types.CtxDBTransaction) != nil
}

func (s *lifecycleTestStore) LockEligibility(ctx context.Context, scopes []domain.EligibilityScope) error {
	s.scopes = append([]domain.EligibilityScope(nil), scopes...)
	if s.order != nil {
		*s.order = append(*s.order, "eligibility")
	}
	if s.hook != nil {
		s.hook(ctx)
	}
	return s.err
}

type lifecycleTestSubscriptions struct {
	subscription.Repository
	order *[]string
}

func (r *lifecycleTestSubscriptions) GetForUpdateNowait(ctx context.Context, id string) (*subscription.Subscription, error) {
	*r.order = append(*r.order, "subscription:"+id)
	return r.Repository.GetForUpdateNowait(ctx, id)
}

type lifecycleTestInvoices struct {
	invoice.Repository
	order *[]string
}

func (r *lifecycleTestInvoices) GetForUpdateNowait(ctx context.Context, id string) (*invoice.Invoice, error) {
	*r.order = append(*r.order, "invoice:"+id)
	return r.Repository.GetForUpdateNowait(ctx, id)
}

func newLifecycleFixture(t *testing.T) (*SubscriptionPauseSuite, *subscriptionService, *subscription.Subscription) {
	t.Helper()
	s := &SubscriptionPauseSuite{}
	s.SetT(t)
	s.SetupSuite()
	s.SetupTest()
	t.Cleanup(s.TearDownTest)
	start, end := openPeriod()
	sub := s.subscriptionWithCharges("lifecycle_root", start, end)
	return s, s.service.(*subscriptionService), sub
}

func TestSubscriptionLifecycleLocksBeforeEligibility(t *testing.T) {
	s, svc, parent := newLifecycleFixture(t)
	parent.SubscriptionType = types.SubscriptionTypeParent
	child := *parent
	child.ID, child.SubscriptionType, child.ParentSubscriptionID = "a_child", types.SubscriptionTypeInherited, &parent.ID
	require.NoError(t, svc.SubRepo.Create(s.GetContext(), &child))
	grouped := child
	grouped.ID, grouped.SubscriptionType = "b_grouped", types.SubscriptionTypeGroupedInvoicing
	require.NoError(t, svc.SubRepo.Create(s.GetContext(), &grouped))
	draft := &invoice.Invoice{
		ID: "invoice_prefix", SubscriptionID: &parent.ID, CustomerID: parent.CustomerID,
		InvoiceType: types.InvoiceTypeSubscription, InvoiceStatus: types.InvoiceStatusDraft,
		BillingReason: string(types.InvoiceBillingReasonSubscriptionCycle), Currency: parent.Currency,
		PeriodStart: &parent.CurrentPeriodStart, PeriodEnd: &parent.CurrentPeriodEnd,
		EnvironmentID: parent.EnvironmentID, BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	require.NoError(t, svc.InvoiceRepo.Create(s.GetContext(), draft))
	order := []string{}
	svc.SubRepo = &lifecycleTestSubscriptions{Repository: svc.SubRepo, order: &order}
	svc.InvoiceRepo = &lifecycleTestInvoices{Repository: svc.InvoiceRepo, order: &order}
	store := &lifecycleTestStore{order: &order}
	svc.BillingTermsStore = store
	_, err := svc.PauseSubscription(s.GetContext(), parent.ID, &dto.PauseSubscriptionRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{"subscription:a_child", "subscription:b_grouped", "subscription:" + parent.ID, "invoice:" + draft.ID, "eligibility"}, order)
	require.Equal(t, []domain.EligibilityScope{{Type: "subscription", ID: child.ID}, {Type: "subscription", ID: grouped.ID}, {Type: "subscription", ID: parent.ID}}, store.scopes)
	for _, id := range []string{parent.ID, child.ID} {
		stored, err := s.params.SubRepo.Get(s.GetContext(), id)
		require.NoError(t, err)
		require.Equal(t, types.SubscriptionStatusPaused, stored.SubscriptionStatus)
	}
	stored, err := s.params.SubRepo.Get(s.GetContext(), grouped.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
}

func TestSubscriptionLifecycleFailClosed(t *testing.T) {
	for _, scenario := range []string{"guard error", "membership drift", "missing store", "missing scope", "nested pause", "nil request"} {
		t.Run(scenario, func(t *testing.T) {
			s, svc, sub := newLifecycleFixture(t)
			ctx := s.GetContext()
			request := &dto.PauseSubscriptionRequest{}
			store := &lifecycleTestStore{}
			svc.BillingTermsStore = store
			switch scenario {
			case "guard error":
				store.err = errors.New("eligibility unavailable")
			case "membership drift":
				sub.SubscriptionType = types.SubscriptionTypeParent
				store.hook = func(tx context.Context) {
					child := *sub
					child.ID, child.SubscriptionType, child.ParentSubscriptionID = "late_child", types.SubscriptionTypeInherited, &sub.ID
					require.NoError(t, svc.SubRepo.Create(tx, &child))
				}
			case "missing store":
				svc.BillingTermsStore = nil
			case "missing scope":
				ctx = types.SetEnvironmentID(ctx, "")
			case "nested pause":
				ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
			case "nil request":
				request = nil
			}
			_, err := svc.PauseSubscription(ctx, sub.ID, request)
			require.Error(t, err)
			if scenario == "membership drift" {
				require.True(t, ierr.IsVersionConflict(err), "%+v", err)
			}
			stored, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
			require.Nil(t, stored.ActivePauseID)
			require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
		})
	}
}

func TestSubscriptionLifecycleResumeKeepsSchedule(t *testing.T) {
	s, svc, sub := newLifecycleFixture(t)
	originalAnchor := sub.BillingAnchor
	_, err := svc.PauseSubscription(s.GetContext(), sub.ID, &dto.PauseSubscriptionRequest{})
	require.NoError(t, err)
	result, err := svc.ResumeSubscription(s.GetContext(), sub.ID, &dto.ResumeSubscriptionRequest{ProrationBehavior: lo.ToPtr(types.ProrationBehaviorNone)})
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusActive, result.Subscription.SubscriptionStatus)
	require.Equal(t, originalAnchor, result.Subscription.BillingAnchor)
	require.Equal(t, sub.CurrentPeriodEnd, result.Subscription.CurrentPeriodEnd)
	require.Nil(t, result.Subscription.ActivePauseID)
}

func TestSubscriptionLifecycleCancellationUsesOuterCommit(t *testing.T) {
	for _, scenario := range []string{"commit", "rollback", "busy guard", "missing try capability"} {
		t.Run(scenario, func(t *testing.T) {
			s, svc, sub := newLifecycleFixture(t)
			store := &lifecycleTestStore{}
			svc.BillingTermsStore = store
			if scenario == "busy guard" {
				store.err = ierr.NewError("busy eligibility").Mark(ierr.ErrVersionConflict)
			}
			if scenario == "missing try capability" {
				svc.BillingTermsStore = &lifecycleBlockingStore{}
			}
			rollback := errors.New("outer rollback")
			err := svc.DB.WithTx(s.GetContext(), func(tx context.Context) error {
				_, err := svc.CancelSubscription(tx, sub.ID, &dto.CancelSubscriptionRequest{CancellationType: types.CancellationTypeImmediate})
				if err != nil {
					return err
				}
				require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
				if scenario == "rollback" {
					return rollback
				}
				return nil
			})
			if scenario == "commit" {
				require.NoError(t, err)
				require.GreaterOrEqual(t, store.tries, 1)
				require.NotEmpty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
				return
			}
			require.Error(t, err)
			if scenario == "busy guard" {
				require.True(t, ierr.IsVersionConflict(err), "%+v", err)
			}
			require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
		})
	}
}

func TestSubscriptionLifecycleRejectsLateInvoice(t *testing.T) {
	s, svc, sub := newLifecycleFixture(t)
	svc.BillingTermsStore = &lifecycleTestStore{hook: func(context.Context) {
		late := &invoice.Invoice{
			ID: "invoice_late", SubscriptionID: &sub.ID, CustomerID: sub.CustomerID,
			InvoiceType: types.InvoiceTypeSubscription, InvoiceStatus: types.InvoiceStatusDraft,
			BillingReason: string(types.InvoiceBillingReasonSubscriptionCycle), Currency: sub.Currency,
			PeriodStart: &sub.CurrentPeriodStart, PeriodEnd: &sub.CurrentPeriodEnd,
			EnvironmentID: sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(s.GetContext()),
		}
		require.NoError(t, s.params.InvoiceRepo.Create(s.GetContext(), late))
	}}
	_, err := svc.PauseSubscription(s.GetContext(), sub.ID, &dto.PauseSubscriptionRequest{})
	require.True(t, ierr.IsVersionConflict(err), "%+v", err)
	stored, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
	require.Nil(t, stored.ActivePauseID)
	require.Empty(t, s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
}
