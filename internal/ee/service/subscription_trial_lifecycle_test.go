package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	repository "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

type trialLifecycleInvoice struct {
	InvoiceService
	t      *testing.T
	store  *lifecycleTestStore
	calls  int
	verify func(context.Context, *dto.CreateSubscriptionInvoiceRequest)
}

func (s *trialLifecycleInvoice) CreateSubscriptionInvoice(ctx context.Context, request *dto.CreateSubscriptionInvoiceRequest, _ *dto.PaymentParameters, _ types.InvoiceFlowType, _ bool) (*dto.InvoiceResponse, *subscription.Subscription, error) {
	s.calls++
	require.True(s.t, types.HasInvoiceLockAdmission(ctx))
	require.NotEmpty(s.t, s.store.scopes)
	if s.verify != nil {
		s.verify(ctx, request)
	}
	return &dto.InvoiceResponse{Invoice: invoice.Invoice{ID: "invoice_trial_guard", SubscriptionID: &request.SubscriptionID, PeriodStart: &request.PeriodStart, PeriodEnd: &request.PeriodEnd, InvoiceStatus: types.InvoiceStatusDraft}}, nil, nil
}

func newTrialLifecycleFixture(t *testing.T) (*SubscriptionPauseSuite, *subscription.Subscription, *subscriptionService, *lifecycleTestStore) {
	t.Helper()
	s, sub := newSubscriptionUpdateFixture(t)
	start, end := time.Now().UTC().Add(-2*time.Hour), time.Now().UTC().Add(-time.Hour)
	sub.StartDate = start
	sub.CurrentPeriodStart = start
	sub.CurrentPeriodEnd = end
	sub.BillingAnchor = start
	sub.SubscriptionType = types.SubscriptionTypeStandalone
	sub.SubscriptionStatus = types.SubscriptionStatusTrialing
	sub.TrialStart = &start
	sub.TrialEnd = &end
	sub.BillingCadence = types.BILLING_CADENCE_RECURRING
	store := &lifecycleTestStore{}
	svc := s.service.(*subscriptionService)
	svc.BillingTermsStore = store
	return s, sub, svc, store
}

func TestTrialLifecycleConversionUsesFreshState(t *testing.T) {
	for _, status := range []types.SubscriptionStatus{types.SubscriptionStatusPaused, types.SubscriptionStatusCancelled, types.SubscriptionStatusActive} {
		t.Run(string(status), func(t *testing.T) {
			s, sub, svc, store := newTrialLifecycleFixture(t)
			stale := subscription.NewSubscriptionBuilder(sub).Build()
			sub.SubscriptionStatus = status
			invoices := &trialLifecycleInvoice{t: t, store: store}
			_, err := svc.processSubscriptionTrialEnd(s.GetContext(), stale, invoices, time.Now().UTC())
			require.NoError(t, err)
			require.Zero(t, invoices.calls)
			current, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
			require.NoError(t, err)
			require.Equal(t, status, current.SubscriptionStatus)
		})
	}
}

func TestTrialLifecycleConversionLocksBeforeInvoice(t *testing.T) {
	s, sub, svc, store := newTrialLifecycleFixture(t)
	invoices := &trialLifecycleInvoice{t: t, store: store}
	_, err := svc.processSubscriptionTrialEnd(s.GetContext(), sub, invoices, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, 1, invoices.calls)
	current, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusIncomplete, current.SubscriptionStatus)
	store.err = ierr.NewError("busy guard").Mark(ierr.ErrVersionConflict)
	_, err = svc.processSubscriptionTrialEnd(s.GetContext(), sub, invoices, time.Now().UTC())
	require.True(t, ierr.IsVersionConflict(err))
	require.Equal(t, 1, invoices.calls)
}

func TestTrialLifecycleModificationCannotUndoPause(t *testing.T) {
	s, sub, svc, _ := newTrialLifecycleFixture(t)
	stale := subscription.NewSubscriptionBuilder(sub).Build()
	params := svc.ServiceParams
	modifier := NewSubscriptionModificationService(params).(*subscriptionModificationService)
	sub.SubscriptionStatus = types.SubscriptionStatusPaused
	_, err := modifier.executeTrialEndModifyDate(s.GetContext(), stale, time.Now().UTC().Add(time.Hour))
	require.Error(t, err)
	current, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusPaused, current.SubscriptionStatus)
}

func TestCancellationRestorationPreservesFreshPause(t *testing.T) {
	s, sub, svc, _ := newTrialLifecycleFixture(t)
	sub.SubscriptionStatus = types.SubscriptionStatusPaused
	sub.PauseStatus = types.PauseStatusActive
	sub.ActivePauseID = lo.ToPtr("pause_existing")
	until := time.Now().UTC().Add(24 * time.Hour)
	sub.CancelAt = &until
	sub.EndDate = &until
	sub.CancelAtPeriodEnd = true
	sub.CancelledAt = &until
	schedule := &subscription.SubscriptionSchedule{ID: "cancel_restore_guard", SubscriptionID: sub.ID, ScheduleType: types.SubscriptionScheduleChangeTypeCancellation,
		ScheduledAt: until, Status: types.ScheduleStatusPending, TenantID: types.GetTenantID(s.GetContext()), EnvironmentID: types.GetEnvironmentID(s.GetContext()), StatusColumn: types.StatusPublished}
	require.NoError(t, schedule.SetCancellationConfig(&subscription.CancellationConfiguration{}))
	require.NoError(t, svc.SubScheduleRepo.Create(s.GetContext(), schedule))
	schedules := NewSubscriptionScheduleService(svc.ServiceParams, nil)
	require.NoError(t, schedules.Cancel(s.GetContext(), schedule.ID))
	current, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusPaused, current.SubscriptionStatus)
	require.Equal(t, types.PauseStatusActive, current.PauseStatus)
	require.Equal(t, "pause_existing", *current.ActivePauseID)
	require.Nil(t, current.CancelAt)
	require.Nil(t, current.CancelledAt)
}

type failingTrialLifecycleInvoice struct{ InvoiceService }

func (s *failingTrialLifecycleInvoice) CreateSubscriptionInvoice(context.Context, *dto.CreateSubscriptionInvoiceRequest, *dto.PaymentParameters, types.InvoiceFlowType, bool) (*dto.InvoiceResponse, *subscription.Subscription, error) {
	return nil, nil, errors.New("injected invoice failure")
}

func TestTrialLifecycleGuardPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	s, sub, service, _ := newTrialLifecycleFixture(t)
	ctx, cancel := context.WithTimeout(s.GetContext(), 15*time.Second)
	defer cancel()
	params := service.ServiceParams
	params.DB = db
	params.SubRepo = repository.NewSubscriptionRepository(db, params.Logger, testutil.NewInMemoryRedis())
	params.InvoiceRepo = repository.NewInvoiceRepository(db, params.Logger)
	params.BillingTermsStore = repository.NewBillingTermsExecutionStore(db)
	require.NoError(t, params.SubRepo.Create(ctx, sub))
	svc := NewSubscriptionService(params).(*subscriptionService)
	before, err := params.SubRepo.Get(ctx, sub.ID)
	require.NoError(t, err)
	invoices := &trialLifecycleInvoice{t: t, store: &lifecycleTestStore{scopes: []domain.EligibilityScope{{Type: "subscription", ID: sub.ID}}}}
	require.NoError(t, db.WithTx(ctx, func(held context.Context) error {
		if err := params.BillingTermsStore.LockEligibility(held, []domain.EligibilityScope{{Type: "subscription", ID: sub.ID}}); err != nil {
			return err
		}
		err := db.WithTx(ctx, func(caller context.Context) error {
			_, err := svc.processSubscriptionTrialEnd(caller, sub, invoices, time.Now().UTC())
			return err
		})
		require.True(t, ierr.IsVersionConflict(err), "nested conversion must retry while a collector owns eligibility: %v", err)
		require.Zero(t, invoices.calls)
		return nil
	}))
	_, err = svc.processSubscriptionTrialEnd(ctx, sub, &failingTrialLifecycleInvoice{}, time.Now().UTC())
	require.ErrorContains(t, err, "injected invoice failure")
	after, err := params.SubRepo.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusTrialing, after.SubscriptionStatus)
	require.Equal(t, before.CurrentPeriodStart, after.CurrentPeriodStart)
	require.Equal(t, before.CurrentPeriodEnd, after.CurrentPeriodEnd)
	invoices.verify = func(tx context.Context, req *dto.CreateSubscriptionInvoiceRequest) {
		fresh, err := params.SubRepo.Get(tx, sub.ID)
		require.NoError(t, err)
		destination := invoice.Invoice{ID: "trial_precision_draft", SubscriptionID: &fresh.ID, CustomerID: fresh.GetInvoicingCustomerID(), Currency: fresh.Currency,
			InvoiceType: types.InvoiceTypeSubscription, InvoiceStatus: types.InvoiceStatusDraft, BillingReason: string(types.InvoiceBillingReasonSubscriptionTrialEnd),
			PeriodStart: &req.PeriodStart, PeriodEnd: &req.PeriodEnd, EnvironmentID: fresh.EnvironmentID, BaseModel: types.GetDefaultBaseModel(tx)}
		bound := bindPendingCollection(tx, &destination)
		require.True(t, pendingCollectionAllowed(bound, fresh, types.BillingTermSubscriptionInvoice), "actual draft authorization must match the microsecond-precision stored transition")
	}
	preciseNow := time.Now().UTC().Truncate(time.Second).Add(123456789 * time.Nanosecond)
	_, err = svc.processGuardedTrialEnd(ctx, sub, invoices, preciseNow, true)
	require.NoError(t, err)
	require.Equal(t, 1, invoices.calls)
	after, err = params.SubRepo.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusIncomplete, after.SubscriptionStatus)
}

func TestCancellationRestorationRejectsExecutedState(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		s, sub, svc, _ := newTrialLifecycleFixture(t)
		until := time.Now().UTC().Add(time.Hour)
		sub.SubscriptionStatus = types.SubscriptionStatusActive
		sub.CancelAt = &until
		sub.CancelledAt = &until
		sub.EndDate = &until
		schedule := &subscription.SubscriptionSchedule{ID: "restore_executed", SubscriptionID: sub.ID, ScheduleType: types.SubscriptionScheduleChangeTypeCancellation, ScheduledAt: until, Status: types.ScheduleStatusPending, TenantID: types.GetTenantID(s.GetContext()), EnvironmentID: types.GetEnvironmentID(s.GetContext()), StatusColumn: types.StatusPublished}
		require.NoError(t, schedule.SetCancellationConfig(&subscription.CancellationConfiguration{}))
		require.NoError(t, svc.SubScheduleRepo.Create(s.GetContext(), schedule))
		stale := *schedule
		if cancelled {
			sub.SubscriptionStatus = types.SubscriptionStatusCancelled
		} else {
			schedule.Status = types.ScheduleStatusExecuted
		}
		schedules := NewSubscriptionScheduleService(svc.ServiceParams, nil).(*subscriptionScheduleService)
		err := schedules.restoreCancellationState(s.GetContext(), &stale)
		require.True(t, ierr.IsVersionConflict(err), "executed cancellation cannot be restored from an old request: %v", err)
		current, err := svc.SubRepo.Get(s.GetContext(), sub.ID)
		require.NoError(t, err)
		require.Equal(t, until, *current.CancelledAt)
		require.Equal(t, until, *current.EndDate)
	}
}

func TestTrialLifecycleExplicitEndRejectsFreshPause(t *testing.T) {
	s, sub, svc, store := newTrialLifecycleFixture(t)
	stale := subscription.NewSubscriptionBuilder(sub).Build()
	sub.SubscriptionStatus = types.SubscriptionStatusPaused
	invoices := &trialLifecycleInvoice{t: t, store: store}
	_, err := svc.processGuardedTrialEnd(s.GetContext(), stale, invoices, time.Now().UTC(), true)
	require.True(t, ierr.IsVersionConflict(err), "an explicit immediate request must not report a converted trial after a concurrent pause: %v", err)
	require.Zero(t, invoices.calls)
}
