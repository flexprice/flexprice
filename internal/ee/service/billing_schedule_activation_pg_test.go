package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
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

func TestDraftActivationBillingCollectionPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	for _, scenario := range []string{"paid activation", "trial activation", "arbitrary draft"} {
		t.Run(scenario, func(t *testing.T) {
			f := newInvoiceCollectionFixture(t, db)
			ctx := f.ctx
			params := f.service.ServiceParams
			template, err := params.SubRepo.Get(ctx, *f.invoice.SubscriptionID)
			require.NoError(t, err)
			params.SubRepo = repository.NewSubscriptionRepository(db, params.Logger, testutil.NewInMemoryRedis())
			params.CommitmentRepo = repository.NewCommitmentRepository(db, params.Logger)
			params.SubscriptionLineItemRepo = repository.NewSubscriptionLineItemRepository(db, params.Logger)
			params.SubScheduleRepo = testutil.NewInMemorySubscriptionScheduleStore()
			resolver, err := core.NewOwnerResolver(NewCommitmentTermsOwner(params.CommitmentRepo, params.SubRepo))
			require.NoError(t, err)
			params.BillingTerms = core.NewService(repository.NewBillingTermRepository(db), f.items, params.BillingTermsStore, resolver)
			activationDate := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
			start := activationDate.AddDate(0, 0, -10)
			end := start.AddDate(0, 1, 0)
			sub := subscription.NewSubscriptionBuilder(template).WithStatus(types.SubscriptionStatusDraft).WithStartDate(start).
				WithBillingAnchor(start).WithCurrentPeriodStart(start).WithCurrentPeriodEnd(end).WithEndDate(nil).Build()
			sub.ID = types.GenerateUUIDWithPrefix("sub")
			sub.BillingCycle = types.BillingCycleAnniversary
			sub.ProrationBehavior = types.ProrationBehaviorNone
			sub.CollectionMethod = string(types.CollectionMethodSendInvoice)
			sub.PaymentBehavior = string(types.PaymentBehaviorDefaultActive)
			if scenario == "trial activation" {
				sub = subscription.NewSubscriptionBuilder(sub).WithTrialStart(&start).WithTrialEnd(lo.ToPtr(activationDate.AddDate(0, 0, 2))).Build()
			}
			require.NoError(t, params.SubRepo.Create(ctx, sub))
			owner := &commitment.Commitment{
				ID: types.GenerateUUIDWithPrefix("comm"), SubscriptionID: sub.ID, CustomerID: sub.CustomerID,
				Name: "Prepaid capacity", InvoiceLabel: "Capacity", Currency: sub.Currency,
				Type: types.COMMITMENT_TYPE_AMOUNT, Value: decimal.NewFromInt(100), Measurement: types.CommitmentMeasurementBillingPeriod,
				ScopeEntityType: types.CommitmentScopeSubscription, ScopeEntityID: sub.ID, StartDate: activationDate, EndDate: end, Timezone: "UTC", Binding: true,
				CollectionMethod: lo.ToPtr(types.CommitmentCollectionBillingTerms), TotalAmount: lo.ToPtr(decimal.NewFromInt(100)),
				CommitmentStatus: types.CommitmentStatusActive, EnvironmentID: sub.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx),
			}
			_, err = params.CommitmentRepo.Create(ctx, owner)
			require.NoError(t, err)
			state, err := params.BillingTerms.Initialize(ctx, core.InitializeRequest{Owner: commitmentBillingOwner(owner.ID), Rows: []core.RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: &activationDate}}})
			require.NoError(t, err)
			itemID := state.Items()[0].ID
			svc := NewSubscriptionService(params)
			request := dto.ActivateDraftSubscriptionRequest{StartDate: &activationDate}
			if scenario == "paid activation" {
				abort := errors.New("abort activation")
				err := db.WithTx(ctx, func(tx context.Context) error {
					response, err := svc.ActivateDraftSubscription(tx, sub.ID, request)
					if err != nil {
						return err
					}
					require.NotNil(t, response.LatestInvoice)
					require.Equal(t, "100", response.LatestInvoice.Subtotal.String())
					return abort
				})
				require.ErrorIs(t, err, abort)
				item, err := f.items.Get(ctx, itemID)
				require.NoError(t, err)
				require.Nil(t, item.InvoiceID)
				current, err := params.SubRepo.Get(types.WithForceWriter(ctx), sub.ID)
				require.NoError(t, err)
				require.Equal(t, types.SubscriptionStatusDraft, current.SubscriptionStatus)
			}
			var response *dto.SubscriptionResponse
			if scenario == "arbitrary draft" {
				_, _, err = NewInvoiceService(params).CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
					SubscriptionID: sub.ID, PeriodStart: start, PeriodEnd: end, ReferencePoint: types.ReferencePointPeriodStart,
				}, dto.NewPaymentParametersFromSubscription(sub.CollectionMethod, sub.PaymentBehavior, nil), types.InvoiceFlowSubscriptionCreation, true)
			} else {
				response, err = svc.ActivateDraftSubscription(ctx, sub.ID, request)
			}
			require.NoError(t, err)
			item, err := f.items.Get(ctx, itemID)
			require.NoError(t, err)
			if scenario == "paid activation" {
				require.NotNil(t, response.LatestInvoice)
				require.Equal(t, response.LatestInvoice.ID, lo.FromPtr(item.InvoiceID))
				require.Equal(t, types.InvoiceStatusFinalized, response.LatestInvoice.InvoiceStatus)
				require.Equal(t, "100", response.LatestInvoice.Subtotal.String())
			} else {
				require.Nil(t, item.InvoiceID)
				require.Equal(t, types.BillingScheduleScheduled, item.ScheduleStatus)
			}
		})
	}
}
