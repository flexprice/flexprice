package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	repository "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionBareEndSettlesManualBillingPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	for _, scenario := range []string{"invoice_before_manual_fallback", "manual_fallback_before_invoice", "scheduled_cancellation"} {
		fallbackFirst := scenario == "manual_fallback_before_invoice"
		t.Run(scenario, func(t *testing.T) {
			f := newInvoiceCollectionFixture(t, db)
			params := f.service.ServiceParams
			params.SubScheduleRepo = testutil.NewInMemorySubscriptionScheduleStore()
			ctx := f.ctx
			old, err := params.SubRepo.Get(ctx, *f.invoice.SubscriptionID)
			require.NoError(t, err)
			if scenario == "scheduled_cancellation" {
				params.SubRepo = repository.NewSubscriptionRepository(db, params.Logger, testutil.NewInMemoryRedis())
				params.CommitmentRepo = repository.NewCommitmentRepository(db, params.Logger)
				resolver, err := core.NewOwnerResolver(NewCommitmentTermsOwner(params.CommitmentRepo, params.SubRepo))
				require.NoError(t, err)
				params.BillingTerms = core.NewService(repository.NewBillingTermRepository(db), f.items, params.BillingTermsStore, resolver)
			}
			start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			end := start.AddDate(0, 1, 0)
			sub := subscription.NewSubscriptionBuilder(old).WithEndDate(&end).WithCurrentPeriodStart(start).WithCurrentPeriodEnd(end).Build()
			sub.ID = types.GenerateUUIDWithPrefix("sub")
			sub.StartDate = start
			sub.BillingAnchor = start
			sub.BillingCycle = types.BillingCycleAnniversary
			sub.ProrationBehavior = types.ProrationBehaviorNone
			sub.CollectionMethod = string(types.CollectionMethodSendInvoice)
			sub.PaymentBehavior = string(types.PaymentBehaviorDefaultActive)
			require.NoError(t, params.SubRepo.Create(ctx, sub))
			owner := &commitment.Commitment{ID: types.GenerateUUIDWithPrefix("comm"), SubscriptionID: sub.ID, CustomerID: sub.CustomerID, Name: "Fixed term", InvoiceLabel: "Capacity", Currency: sub.Currency,
				Type: types.COMMITMENT_TYPE_AMOUNT, Value: decimal.NewFromInt(100), Measurement: types.CommitmentMeasurementBillingPeriod,
				ScopeEntityType: types.CommitmentScopeSubscription, ScopeEntityID: sub.ID, StartDate: start, EndDate: end, Timezone: "UTC", Binding: true,
				CollectionMethod: lo.ToPtr(types.CommitmentCollectionBillingTerms), TotalAmount: lo.ToPtr(decimal.NewFromInt(100)), CommitmentStatus: types.CommitmentStatusActive, EnvironmentID: sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx)}
			_, err = params.CommitmentRepo.Create(ctx, owner)
			require.NoError(t, err)
			legacy := commitment.NewCommitmentBuilder(owner).Build()
			legacy.ID = types.GenerateUUIDWithPrefix("comm")
			legacy.CollectionMethod = lo.ToPtr(types.CommitmentCollectionPerPeriod)
			legacy.TotalAmount = nil
			_, err = params.CommitmentRepo.Create(ctx, legacy)
			require.NoError(t, err)
			state, err := params.BillingTerms.Initialize(ctx, core.InitializeRequest{Owner: commitmentBillingOwner(owner.ID), Rows: []core.RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, OnTrigger: true}}})
			require.NoError(t, err)
			manualID := state.Items()[0].ID
			now := time.Now().UTC()
			if fallbackFirst {
				require.NoError(t, params.BillingTerms.ResolveFallback(ctx, core.FallbackRequest{Owner: commitmentBillingOwner(owner.ID), ItemID: manualID, AsOf: now}))
			}
			svc := NewSubscriptionService(params).(*subscriptionService)
			if scenario == "scheduled_cancellation" {
				abort := errors.New("abort cancellation")
				err := db.WithTx(ctx, func(tx context.Context) error {
					cancelled, err := svc.CompleteScheduledCancellation(tx, sub.ID, end)
					if err != nil {
						return err
					}
					require.True(t, cancelled)
					return abort
				})
				require.ErrorIs(t, err, abort)
				current, err := params.SubRepo.Get(types.WithForceWriter(ctx), sub.ID)
				require.NoError(t, err)
				require.Equal(t, types.SubscriptionStatusActive, current.SubscriptionStatus)
				manual, err := f.items.Get(ctx, manualID)
				require.NoError(t, err)
				require.Equal(t, types.StatusPublished, manual.Status)
				cancelled, err := svc.CompleteScheduledCancellation(ctx, sub.ID, end)
				require.NoError(t, err)
				require.True(t, cancelled)
			} else {
				require.NoError(t, svc.processSubscriptionPeriod(ctx, sub, now))
			}
			closed, err := params.SubRepo.Get(ctx, sub.ID)
			require.NoError(t, err)
			require.Equal(t, types.SubscriptionStatusCancelled, closed.SubscriptionStatus)
			require.Equal(t, end, *closed.CancelledAt)
			legacy, err = params.CommitmentRepo.Get(ctx, legacy.ID)
			require.NoError(t, err)
			require.Equal(t, types.CommitmentStatusActive, legacy.CommitmentStatus)
			require.Nil(t, legacy.TerminatedAt)
			current, err := params.BillingTerms.Get(ctx, commitmentBillingOwner(owner.ID))
			require.NoError(t, err)
			require.Equal(t, types.BillingTermSeparateInvoice, current.Owner().Delivery)
			require.True(t, current.Owner().CanCollect)
			require.Equal(t, "100", current.Summary().TargetAmount.String())
			require.False(t, current.Summary().RequiresReplan)
			if fallbackFirst {
				require.Equal(t, "100", current.Summary().AssignedAmount.String())
				require.True(t, current.Summary().ScheduledAmount.IsZero())
				return
			}
			require.True(t, current.Summary().AssignedAmount.IsZero())
			require.Equal(t, "100", current.Summary().ScheduledAmount.String())
			require.Len(t, current.Items(), 1)
			item := current.Items()[0]
			require.Equal(t, types.BillingScheduleSettlement, item.Kind)
			require.Equal(t, end, *item.BillOn)
			oldManual, err := f.items.Get(ctx, manualID)
			require.NoError(t, err)
			require.Equal(t, types.StatusArchived, oldManual.Status)
			require.Equal(t, types.BillingScheduleCanceled, oldManual.ScheduleStatus)
			due, err := NewBillingScheduleService(params, f.items).CreateOrRecover(ctx, core.DueInvoiceRequest{ItemID: item.ID, AsOf: now})
			require.NoError(t, err)
			require.NotEmpty(t, due.InvoiceID)
			final, err := params.BillingTerms.Get(ctx, commitmentBillingOwner(owner.ID))
			require.NoError(t, err)
			require.Equal(t, "100", final.Summary().AssignedAmount.String())
		})
	}
}
