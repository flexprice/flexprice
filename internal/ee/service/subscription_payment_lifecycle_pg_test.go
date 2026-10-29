package service

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	repository "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestPaymentLifecycle_Postgres(t *testing.T) {
	for _, scenario := range []string{"invoice lock contention", "status rollback", "postcommit activation", "outer rollback", "eligibility before invoice"} {
		t.Run(scenario, func(t *testing.T) {
			db := newInvoiceVoidPostgresClient(t)
			s, _, sub, source, _ := newPaymentLifecycleFixture(t)
			ctx, cancel := context.WithTimeout(s.GetContext(), 15*time.Second)
			defer cancel()
			params := s.params
			params.DB = db
			params.SubRepo = repository.NewSubscriptionRepository(db, params.Logger, testutil.NewInMemoryRedis())
			params.InvoiceRepo = repository.NewInvoiceRepository(db, params.Logger)
			store := repository.NewBillingTermsExecutionStore(db)
			params.BillingTermsStore = store
			now := time.Now().UTC().Truncate(time.Microsecond)
			sub.StartDate = now.AddDate(0, 0, -7)
			sub.CurrentPeriodStart = sub.StartDate
			sub.CurrentPeriodEnd = sub.StartDate.AddDate(0, 1, 0)
			source = invoice.NewInvoiceBuilder(source).WithIdempotencyKey(lo.ToPtr("payment_lifecycle_source")).Build()
			source.PeriodStart = &sub.CurrentPeriodStart
			source.PeriodEnd = &sub.CurrentPeriodEnd
			source.LastComputedAt = &now
			source.FinalizedAt = &now
			require.NoError(t, params.SubRepo.Create(ctx, sub))
			svc := NewSubscriptionService(params).(*subscriptionService)
			pub := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
			if scenario == "postcommit activation" || scenario == "outer rollback" {
				abort := errors.New("abort outer payment")
				err := db.WithTx(ctx, func(tx context.Context) error {
					if err := params.InvoiceRepo.Create(tx, source); err != nil {
						return err
					}
					if err := svc.HandleSubscriptionActivatingInvoicePaid(tx, source); err != nil {
						return err
					}
					current, err := params.SubRepo.Get(tx, sub.ID)
					if err != nil {
						return err
					}
					require.Equal(t, types.SubscriptionStatusIncomplete, current.SubscriptionStatus)
					require.Empty(t, pub.Events())
					if scenario == "outer rollback" {
						return abort
					}
					return nil
				})
				if scenario == "outer rollback" {
					require.ErrorIs(t, err, abort)
				} else {
					require.NoError(t, err)
				}
				current, err := params.SubRepo.Get(types.WithForceWriter(ctx), sub.ID)
				require.NoError(t, err)
				if scenario == "outer rollback" {
					require.Equal(t, types.SubscriptionStatusIncomplete, current.SubscriptionStatus)
					require.Empty(t, pub.Events())
				} else {
					require.Equal(t, types.SubscriptionStatusActive, current.SubscriptionStatus)
					require.Len(t, pub.Events(), 1)
				}
				return
			}
			require.NoError(t, params.InvoiceRepo.Create(ctx, source))
			request := paymentLifecycleRequest{subscriptionID: sub.ID, invoiceID: source.ID, asOf: now}
			switch scenario {
			case "invoice lock contention":
				err := db.WithTx(ctx, func(tx context.Context) error {
					if _, err := params.InvoiceRepo.GetForUpdate(tx, source.ID); err != nil {
						return err
					}
					done := make(chan error, 1)
					go func() { done <- svc.reconcilePaymentLifecycle(ctx, request) }()
					select {
					case err := <-done:
						require.True(t, ierr.IsVersionConflict(err), "%+v", err)
					case <-ctx.Done():
						return ctx.Err()
					}
					current, err := params.SubRepo.GetForUpdateNowait(tx, sub.ID)
					if err != nil {
						return err
					}
					require.Equal(t, types.SubscriptionStatusIncomplete, current.SubscriptionStatus)
					return nil
				})
				require.NoError(t, err)
			case "status rollback":
				svc.SubRepo = &subscriptionUpdateFailingRepository{Repository: params.SubRepo}
				require.ErrorContains(t, svc.reconcilePaymentLifecycle(ctx, request), "failure after database update")
				current, err := params.SubRepo.Get(types.WithForceWriter(ctx), sub.ID)
				require.NoError(t, err)
				require.Equal(t, types.SubscriptionStatusIncomplete, current.SubscriptionStatus)
			case "eligibility before invoice":
				waiting := make(chan struct{})
				svc.BillingTermsStore = &subscriptionUpdateObservedStore{ExecutionStore: store, waiting: waiting}
				done := make(chan error, 1)
				err := db.WithTx(ctx, func(tx context.Context) error {
					if err := store.LockEligibility(tx, []domain.EligibilityScope{{Type: "subscription", ID: sub.ID}}); err != nil {
						return err
					}
					go func() { done <- svc.reconcilePaymentLifecycle(ctx, request) }()
					select {
					case <-waiting:
					case <-ctx.Done():
						return ctx.Err()
					}
					if _, err := params.InvoiceRepo.GetForUpdateNowait(tx, source.ID); err != nil {
						return err
					}
					current, err := params.SubRepo.GetForUpdateNowait(tx, sub.ID)
					if err != nil {
						return err
					}
					current.SubscriptionStatus = types.SubscriptionStatusPaused
					return params.SubRepo.Update(tx, current)
				})
				require.NoError(t, err)
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				current, err := params.SubRepo.Get(types.WithForceWriter(ctx), sub.ID)
				require.NoError(t, err)
				require.Equal(t, types.SubscriptionStatusPaused, current.SubscriptionStatus)
			}
			require.Empty(t, pub.Events())
		})
	}
}

func TestPaymentLifecycle_PostgresCandidatePaging(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	s, _, template, _, _ := newPaymentLifecycleFixture(t)
	repo := repository.NewSubscriptionRepository(db, s.params.Logger, testutil.NewInMemoryRedis())
	ctx := s.GetContext()
	for _, tc := range []struct {
		id     string
		tenant string
		env    string
		status types.SubscriptionStatus
		gated  bool
	}{
		{"a", types.GetTenantID(ctx), types.GetEnvironmentID(ctx), types.SubscriptionStatusIncomplete, false},
		{"b", types.GetTenantID(ctx), types.GetEnvironmentID(ctx), types.SubscriptionStatusActive, true},
		{"c", types.GetTenantID(ctx), types.GetEnvironmentID(ctx), types.SubscriptionStatusActive, false},
		{"d", "another_tenant", types.GetEnvironmentID(ctx), types.SubscriptionStatusIncomplete, true},
		{"e", types.GetTenantID(ctx), "another_environment", types.SubscriptionStatusIncomplete, true},
		{"f", types.GetTenantID(ctx), types.GetEnvironmentID(ctx), types.SubscriptionStatusPaused, true},
	} {
		owner := *template
		owner.ID, owner.TenantID, owner.EnvironmentID = tc.id, tc.tenant, tc.env
		owner.SubscriptionStatus = tc.status
		owner.PaymentBehavior = string(types.PaymentBehaviorDefaultActive)
		if tc.gated {
			owner.PaymentBehavior = string(types.PaymentBehaviorAllowIncomplete)
		}
		createCtx := types.SetEnvironmentID(types.SetTenantID(ctx, tc.tenant), tc.env)
		require.NoError(t, repo.Create(createCtx, &owner))
	}
	first, err := repo.ListPaymentLifecycleCandidates(ctx, "", 1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, "a", first[0].ID)
	second, err := repo.ListPaymentLifecycleCandidates(ctx, first[0].ID, 100)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, "b", second[0].ID)
	_, err = repo.ListPaymentLifecycleCandidates(types.SetTenantID(ctx, ""), "", 100)
	require.Error(t, err)
	_, err = repo.ListPaymentLifecycleCandidates(ctx, "", 101)
	require.Error(t, err)
}
