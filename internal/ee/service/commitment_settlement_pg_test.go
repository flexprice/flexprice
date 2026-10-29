package service

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	repository "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCommitmentSettlementAtomicPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	f := newInvoiceCollectionFixture(t, db)
	params := f.service.ServiceParams
	owners, err := params.CommitmentRepo.List(f.ctx, commitment.NewNoLimitFilter())
	require.NoError(t, err)
	require.Len(t, owners, 1)
	owner := commitment.NewCommitmentBuilder(owners[0]).Build()
	owner.InvoiceLabel = "Contract payment"
	owner.EndDate = owner.StartDate.AddDate(0, 1, 0)
	sub, err := params.SubRepo.Get(f.ctx, owner.SubscriptionID)
	require.NoError(t, err)
	sub.CurrentPeriodEnd = owner.EndDate
	require.NoError(t, params.SubRepo.Update(f.ctx, sub))
	owner.Type = types.COMMITMENT_TYPE_AMOUNT
	owner.Measurement = types.CommitmentMeasurementBillingPeriod
	owner.Value = decimal.NewFromInt(100)
	owner.ScopeEntityType = types.CommitmentScopeSubscription
	owner.ScopeEntityID = owner.SubscriptionID
	params.CommitmentRepo = repository.NewCommitmentRepository(db, params.Logger)
	_, err = params.CommitmentRepo.Create(f.ctx, owner)
	require.NoError(t, err)
	resolver, err := core.NewOwnerResolver(NewCommitmentTermsOwner(params.CommitmentRepo, params.SubRepo))
	require.NoError(t, err)
	params.BillingTerms = core.NewService(repository.NewBillingTermRepository(db), f.items, params.BillingTermsStore, resolver)
	svc := newCommitmentService(params)
	ref := commitmentBillingOwner(owner.ID)
	initial, err := params.BillingTerms.Get(f.ctx, ref)
	require.NoError(t, err)
	firstDate, secondDate := owner.StartDate, owner.EndDate
	state, err := params.BillingTerms.Replace(f.ctx, core.ReplaceRequest{Owner: ref, ExpectedState: initial.StateToken(), Reason: "retain part of principal", Rows: []core.RuleInput{
		{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: lo.ToPtr(decimal.NewFromInt(30)), BillOn: &firstDate},
		{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: &secondDate},
	}})
	require.NoError(t, err)
	var claimed *domain.ScheduleItem
	for _, item := range state.Items() {
		if item.Amount.Equal(decimal.NewFromInt(30)) {
			claimed = item
		}
	}
	require.NotNil(t, claimed)
	inv := invoice.NewInvoiceBuilder(&invoice.Invoice{ID: types.GenerateUUIDWithPrefix("inv"), CustomerID: claimed.CustomerID, SubscriptionID: claimed.SubscriptionID,
		InvoiceType: types.InvoiceTypeSubscription, InvoiceStatus: types.InvoiceStatusDraft, PaymentStatus: types.PaymentStatusPending, Currency: claimed.Currency,
		Subtotal: claimed.Amount, Total: claimed.Amount, AmountDue: claimed.Amount, AmountRemaining: claimed.Amount,
		EnvironmentID: owner.EnvironmentID, BaseModel: types.GetDefaultBaseModel(f.ctx)}).Build()
	line := invoice.NewInvoiceLineItemBuilder(&invoice.InvoiceLineItem{ID: types.GenerateUUIDWithPrefix("invli"), InvoiceID: inv.ID, CustomerID: claimed.CustomerID, SubscriptionID: claimed.SubscriptionID,
		CommitmentID: &owner.ID, Amount: claimed.Amount, Quantity: decimal.NewFromInt(1), Currency: claimed.Currency, PriceType: lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
		PeriodStart: &claimed.ServicePeriodStart, PeriodEnd: &claimed.ServicePeriodEnd, EnvironmentID: owner.EnvironmentID, BaseModel: types.GetDefaultBaseModel(f.ctx)}).WithBillingScheduleItemID(&claimed.ID).Build()
	inv = invoice.NewInvoiceBuilder(inv).WithIdempotencyKey(&inv.ID).WithLineItems([]*invoice.InvoiceLineItem{line}).Build()
	require.NoError(t, db.WithTx(f.ctx, func(tx context.Context) error {
		if err := params.InvoiceRepo.CreateWithLineItems(tx, inv); err != nil {
			return err
		}
		accepted, err := f.items.Claim(tx, claimed.ID, inv.ID, line.ID)
		if err != nil {
			return err
		}
		if !accepted {
			return errors.New("fixture claim rejected")
		}
		return nil
	}))
	at := owner.StartDate.Add(owner.EndDate.Sub(owner.StartDate) / 2).Truncate(time.Microsecond)
	before, err := params.BillingTerms.Get(f.ctx, ref)
	require.NoError(t, err)
	rollback := errors.New("force outer rollback")
	err = db.WithTx(f.ctx, func(tx context.Context) error {
		_, _, err := svc.terminate(tx, owner.ID, at, nil)
		if err != nil {
			return err
		}
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	after, err := params.BillingTerms.Get(f.ctx, ref)
	require.NoError(t, err)
	require.Equal(t, before.StateToken(), after.StateToken())
	persisted, err := params.CommitmentRepo.Get(f.ctx, owner.ID)
	require.NoError(t, err)
	require.Equal(t, types.CommitmentStatusActive, persisted.CommitmentStatus)
	require.Empty(t, persisted.Metadata[commitmentSettlementKey])
	require.NoError(t, db.WithTx(f.ctx, func(tx context.Context) error { _, _, err := svc.terminate(tx, owner.ID, at, nil); return err }))
	after, err = params.BillingTerms.Get(f.ctx, ref)
	require.NoError(t, err)
	require.Equal(t, "50", after.Summary().TargetAmount.String())
	require.Equal(t, "30", after.Summary().AssignedAmount.String())
	require.Equal(t, "20", after.Summary().ScheduledAmount.String())
	var settlementID string
	for _, item := range after.Items() {
		if item.ScheduleStatus == types.BillingScheduleScheduled {
			require.Equal(t, types.BillingScheduleSettlement, item.Kind)
			require.Nil(t, item.BillingTermID)
			settlementID = item.ID
		} else {
			require.Equal(t, claimed.ID, item.ID)
			require.Equal(t, types.StatusArchived, item.Status)
			require.Equal(t, inv.ID, *item.InvoiceID)
		}
	}
	require.NotEmpty(t, settlementID)
	require.NoError(t, db.WithTx(f.ctx, func(tx context.Context) error { _, _, err := svc.terminate(tx, owner.ID, at, nil); return err }))
	again, err := params.BillingTerms.Get(f.ctx, ref)
	require.NoError(t, err)
	require.Equal(t, after.StateToken(), again.StateToken())
	require.NoError(t, svc.TerminateForSubscription(f.ctx, owner.SubscriptionID, at))
	converted, err := params.BillingTerms.Get(f.ctx, ref)
	require.NoError(t, err)
	require.Equal(t, types.BillingTermSeparateInvoice, converted.Owner().Delivery)
	require.Nil(t, owner.InstalmentInvoicing, "configured destination remains the subscription default")
	invoiceService := NewInvoiceService(params).(*invoiceService)
	computed, skipped, err := invoiceService.ComputeInvoice(f.ctx, f.invoice.ID, nil)
	require.NoError(t, err, "a final subscription invoice must not attempt the separately authorized settlement")
	require.False(t, skipped)
	for _, line := range computed.LineItems {
		require.Nil(t, line.BillingScheduleItemID)
	}
	remaining, err := params.BillingTerms.Get(f.ctx, ref)
	require.NoError(t, err)
	require.Equal(t, "30", remaining.Summary().AssignedAmount.String())
	require.Equal(t, "20", remaining.Summary().ScheduledAmount.String())
}
