package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	repository "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestTrialRecovery_Postgres(t *testing.T) {
	for _, scenario := range []string{"invoice lock contention", "activation rollback", "postcommit activation", "outer rollback"} {
		t.Run(scenario, func(t *testing.T) {
			db := newInvoiceVoidPostgresClient(t)
			s, sub := newSubscriptionUpdateFixture(t)
			ctx, cancel := context.WithTimeout(s.GetContext(), 15*time.Second)
			defer cancel()
			params := s.params
			params.DB = db
			params.SubRepo = repository.NewSubscriptionRepository(db, params.Logger, testutil.NewInMemoryRedis())
			params.InvoiceRepo = repository.NewInvoiceRepository(db, params.Logger)
			params.BillingTermsStore = repository.NewBillingTermsExecutionStore(db)
			sub.SubscriptionStatus = types.SubscriptionStatusIncomplete
			sub.CollectionMethod = string(types.CollectionMethodChargeAutomatically)
			sub.PaymentBehavior = string(types.PaymentBehaviorAllowIncomplete)
			sub.StartDate = time.Now().UTC().Truncate(time.Microsecond).AddDate(0, 0, -21)
			sub.TrialStart = lo.ToPtr(sub.StartDate)
			sub.TrialEnd = lo.ToPtr(sub.StartDate.AddDate(0, 0, 14))
			sub.CurrentPeriodStart = *sub.TrialEnd
			sub.CurrentPeriodEnd = sub.CurrentPeriodStart.AddDate(0, 1, 0)
			expectedEvents := 1
			if scenario == "postcommit activation" || scenario == "outer rollback" {
				sub.SubscriptionType = types.SubscriptionTypeParent
				expectedEvents = 2
			}
			require.NoError(t, params.SubRepo.Create(ctx, sub))
			if sub.SubscriptionType == types.SubscriptionTypeParent {
				child := *sub
				child.ID = "sub_recovery_child"
				child.SubscriptionType = types.SubscriptionTypeInherited
				child.ParentSubscriptionID = &sub.ID
				require.NoError(t, params.SubRepo.Create(ctx, &child))
			}
			asOf := sub.TrialEnd.Add(time.Hour)
			source := invoice.NewInvoiceBuilder(&invoice.Invoice{
				ID: "inv_recovery", CustomerID: sub.GetInvoicingCustomerID(), SubscriptionID: &sub.ID,
				InvoiceType: types.InvoiceTypeSubscription, InvoiceStatus: types.InvoiceStatusFinalized,
				BillingReason: string(types.InvoiceBillingReasonSubscriptionTrialEnd), PaymentStatus: types.PaymentStatusSucceeded,
				Currency: sub.Currency, AmountDue: decimal.NewFromInt(100), AmountPaid: decimal.NewFromInt(100),
				Subtotal: decimal.NewFromInt(100), Total: decimal.NewFromInt(100),
				PeriodStart: &sub.CurrentPeriodStart, PeriodEnd: &sub.CurrentPeriodEnd,
				FinalizedAt: &asOf, LastComputedAt: &asOf,
				EnvironmentID: sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx),
			}).WithIdempotencyKey(lo.ToPtr("trial_recovery")).Build()
			svc := NewSubscriptionService(params).(*subscriptionService)
			pub := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)

			if scenario == "postcommit activation" || scenario == "outer rollback" {
				rollback := errors.New("rollback outer invoice transaction")
				err := db.WithTx(ctx, func(tx context.Context) error {
					if err := params.InvoiceRepo.Create(tx, source); err != nil {
						return err
					}
					if err := db.WithTx(tx, func(nested context.Context) error {
						processor := NewSubscriptionPaymentProcessor(&params)
						return processor.HandlePaymentBehavior(nested, sub, dto.NewInvoiceResponse(source), types.PaymentBehaviorAllowIncomplete, types.InvoiceFlowRenewal)
					}); err != nil {
						return err
					}
					stored, err := params.SubRepo.Get(tx, sub.ID)
					if err != nil {
						return err
					}
					require.Equal(t, types.SubscriptionStatusIncomplete, stored.SubscriptionStatus)
					child, err := params.SubRepo.Get(tx, "sub_recovery_child")
					if err != nil {
						return err
					}
					require.Equal(t, types.SubscriptionStatusIncomplete, child.SubscriptionStatus)
					require.Empty(t, pub.Events(), "nested WithTx must not run the activation hook")
					if scenario == "outer rollback" {
						return rollback
					}
					return nil
				})
				if scenario == "postcommit activation" {
					require.NoError(t, err)
					stored, err := params.SubRepo.Get(types.WithForceWriter(ctx), sub.ID)
					require.NoError(t, err)
					require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
					child, err := params.SubRepo.Get(types.WithForceWriter(ctx), "sub_recovery_child")
					require.NoError(t, err)
					require.Equal(t, types.SubscriptionStatusActive, child.SubscriptionStatus)
					require.Len(t, pub.Events(), expectedEvents)
					activated, err := svc.reconcileTrialActivation(ctx, sub.ID, asOf)
					require.NoError(t, err)
					require.False(t, activated)
					require.Len(t, pub.Events(), expectedEvents)
					return
				}
				require.ErrorIs(t, err, rollback)
				_, err = params.InvoiceRepo.Get(types.WithForceWriter(ctx), source.ID)
				require.True(t, ierr.IsNotFound(err), "%+v", err)
				activated, err := svc.reconcileTrialActivation(ctx, sub.ID, asOf)
				require.NoError(t, err)
				require.False(t, activated, "rolled-back invoices cannot activate subscriptions")
			}
			require.NoError(t, params.InvoiceRepo.Create(ctx, source))

			if scenario == "invoice lock contention" {
				done := make(chan error, 1)
				err := db.WithTx(ctx, func(tx context.Context) error {
					if _, err := params.InvoiceRepo.GetForUpdate(tx, source.ID); err != nil {
						return err
					}
					go func() {
						_, err := svc.reconcileTrialActivation(ctx, sub.ID, asOf)
						done <- err
					}()
					select {
					case err := <-done:
						require.True(t, ierr.IsVersionConflict(err), "%+v", err)
					case <-ctx.Done():
						return ctx.Err()
					}
					locked, err := params.SubRepo.GetForUpdate(tx, sub.ID)
					if err != nil {
						return err
					}
					require.Equal(t, types.SubscriptionStatusIncomplete, locked.SubscriptionStatus)
					return nil
				})
				require.NoError(t, err)
			} else if scenario == "activation rollback" {
				svc.SubRepo = &subscriptionUpdateFailingRepository{Repository: params.SubRepo}
				activated, err := svc.reconcileTrialActivation(ctx, sub.ID, asOf)
				require.ErrorContains(t, err, "failure after database update")
				require.False(t, activated)
				svc.SubRepo = params.SubRepo
			}
			require.Empty(t, pub.Events())
			stored, err := params.SubRepo.Get(types.WithForceWriter(ctx), sub.ID)
			require.NoError(t, err)
			require.Equal(t, types.SubscriptionStatusIncomplete, stored.SubscriptionStatus)
			if sub.SubscriptionType == types.SubscriptionTypeParent {
				child, err := params.SubRepo.Get(types.WithForceWriter(ctx), "sub_recovery_child")
				require.NoError(t, err)
				require.Equal(t, types.SubscriptionStatusIncomplete, child.SubscriptionStatus)
			}

			activated, err := svc.reconcileTrialActivation(ctx, sub.ID, asOf)
			require.NoError(t, err)
			require.True(t, activated)
			activated, err = svc.reconcileTrialActivation(ctx, sub.ID, asOf)
			require.NoError(t, err)
			require.False(t, activated)
			require.Len(t, pub.Events(), expectedEvents)
			require.Equal(t, types.WebhookEventSubscriptionActivated, pub.Events()[0].EventName)
			stored, err = params.SubRepo.Get(types.WithForceWriter(ctx), sub.ID)
			require.NoError(t, err)
			require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
		})
	}
}
