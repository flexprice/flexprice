package service

import (
	"sync"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func replanCorrection(t *testing.T, f *invoiceCollectionFixture, originalID string, item *domain.ScheduleItem) []*domain.ScheduleItem {
	t.Helper()
	f.service.BillingScheduleItems = f.items
	_, err := f.service.VoidInvoice(f.ctx, originalID, dto.InvoiceVoidRequest{})
	require.NoError(t, err)
	owner := domain.OwnerRef{Type: item.OwnerType, ID: item.OwnerID}
	state, err := f.service.BillingTerms.Get(f.ctx, owner)
	require.NoError(t, err)
	state, err = f.service.BillingTerms.Replace(f.ctx, core.ReplaceRequest{Owner: owner, ExpectedState: state.StateToken(), Reason: "Correct payment arrangement", Rows: []core.RuleInput{
		{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: lo.ToPtr(decimal.NewFromInt(40)), BillOn: &item.ServicePeriodStart},
		{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: &item.ServicePeriodStart},
	}})
	require.NoError(t, err)
	items := state.Items()
	if items[0].Amount.Equal(decimal.NewFromInt(60)) {
		items[0], items[1] = items[1], items[0]
	}
	return items
}

func TestInvoiceBillingCorrectionPostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	t.Run("concurrent identical correction recovers one replacement", func(t *testing.T) {
		f := newInvoiceCollectionFixture(t, db)
		_, _, err := f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil)
		require.NoError(t, err)
		item, err := f.items.Get(f.ctx, f.itemID)
		require.NoError(t, err)
		items := replanCorrection(t, f, f.invoice.ID, item)
		request := dto.RecalculateInvoiceRequest{BillingScheduleItemIDs: []string{items[0].ID}}
		results := make([]*dto.InvoiceResponse, 2)
		failures := make([]error, 2)
		var calls sync.WaitGroup
		for index := range results {
			calls.Add(1)
			go func(index int) {
				defer calls.Done()
				results[index], failures[index] = f.service.RecalculateInvoiceWithSchedule(f.ctx, f.invoice.ID, request)
			}(index)
		}
		calls.Wait()
		for _, err := range failures {
			require.NoError(t, err)
		}
		require.Equal(t, results[0].ID, results[1].ID)
		original, err := f.service.InvoiceRepo.Get(f.ctx, f.invoice.ID)
		require.NoError(t, err)
		require.Equal(t, results[0].ID, lo.FromPtr(original.RecalculatedInvoiceID))
	})
	t.Run("foreign archived and paused selections cannot be collected", func(t *testing.T) {
		f := newInvoiceCollectionFixture(t, db)
		_, _, err := f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil)
		require.NoError(t, err)
		item, err := f.items.Get(f.ctx, f.itemID)
		require.NoError(t, err)
		items := replanCorrection(t, f, f.invoice.ID, item)
		_, _, foreign := newBillingDueFixture(t, db, false)
		request := dto.RecalculateInvoiceRequest{BillingScheduleItemIDs: []string{items[0].ID}}
		err = f.service.ValidateInvoiceCorrection(types.SetEnvironmentID(f.ctx, "another_environment"), f.invoice.ID, request)
		require.True(t, ierr.IsNotFound(err), "%v", err)
		err = f.service.ValidateInvoiceCorrection(f.ctx, f.invoice.ID, dto.RecalculateInvoiceRequest{BillingScheduleItemIDs: []string{foreign.ID}})
		require.True(t, ierr.IsNotFound(err), "%v", err)
		err = f.service.ValidateInvoiceCorrection(f.ctx, f.invoice.ID, dto.RecalculateInvoiceRequest{BillingScheduleItemIDs: []string{item.ID}})
		require.True(t, ierr.IsVersionConflict(err), "%v", err)
		sub, err := f.service.SubRepo.Get(f.ctx, *item.SubscriptionID)
		require.NoError(t, err)
		sub.SubscriptionStatus = types.SubscriptionStatusPaused
		require.NoError(t, f.service.SubRepo.Update(f.ctx, sub))
		err = f.service.ValidateInvoiceCorrection(f.ctx, f.invoice.ID, request)
		require.True(t, ierr.IsVersionConflict(err), "%v", err)
		sub.SubscriptionStatus = types.SubscriptionStatusActive
		require.NoError(t, f.service.SubRepo.Update(f.ctx, sub))
		require.NoError(t, f.service.ValidateInvoiceCorrection(f.ctx, f.invoice.ID, request))
		current, err := f.items.Get(f.ctx, items[0].ID)
		require.NoError(t, err)
		require.Nil(t, current.InvoiceID)
	})
	t.Run("mixed invoice preserves ordinary charges and never sweeps other due items", func(t *testing.T) {
		f := newInvoiceCollectionFixture(t, db)
		_, _, err := f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil)
		require.NoError(t, err)
		item, err := f.items.Get(f.ctx, f.itemID)
		require.NoError(t, err)
		items := replanCorrection(t, f, f.invoice.ID, item)
		request := dto.RecalculateInvoiceRequest{BillingScheduleItemIDs: []string{items[0].ID}}
		require.NoError(t, f.service.ValidateInvoiceCorrection(f.ctx, f.invoice.ID, request))
		replacement, err := f.service.RecalculateInvoiceWithSchedule(f.ctx, f.invoice.ID, request)
		require.NoError(t, err)
		require.Equal(t, "80", replacement.Subtotal.String())
		require.Len(t, replacement.LineItems, 2)
		for range 2 {
			again, err := f.service.RecalculateInvoiceWithSchedule(f.ctx, f.invoice.ID, request)
			require.NoError(t, err)
			require.Equal(t, replacement.ID, again.ID)
			computed, _, err := f.service.ComputeInvoice(f.ctx, replacement.ID, nil)
			require.NoError(t, err)
			require.Equal(t, "80", computed.Subtotal.String())
		}
		remaining, err := f.items.Get(f.ctx, items[1].ID)
		require.NoError(t, err)
		require.Equal(t, types.BillingScheduleScheduled, remaining.ScheduleStatus)
		require.Nil(t, remaining.InvoiceID)
		_, err = f.service.RecalculateInvoiceWithSchedule(f.ctx, f.invoice.ID, dto.RecalculateInvoiceRequest{BillingScheduleItemIDs: []string{items[1].ID}})
		require.True(t, ierr.IsVersionConflict(err), "%v", err)
	})
	t.Run("separate source supports explicit reissue and competing pickup conflicts", func(t *testing.T) {
		f, due, item := newBillingDueFixture(t, db, false)
		first, err := due.CreateOrRecover(f.ctx, core.DueInvoiceRequest{ItemID: item.ID, AsOf: time.Now().UTC()})
		require.NoError(t, err)
		f.service.BillingScheduleItems = f.items
		err = f.service.ValidateInvoiceCorrection(f.ctx, first.InvoiceID, dto.RecalculateInvoiceRequest{BillingScheduleItemIDs: []string{item.ID}})
		require.True(t, ierr.IsVersionConflict(err))
		items := replanCorrection(t, f, first.InvoiceID, item)
		request := dto.RecalculateInvoiceRequest{BillingScheduleItemIDs: []string{items[0].ID}}
		replacement, err := f.service.RecalculateInvoiceWithSchedule(f.ctx, first.InvoiceID, request)
		require.NoError(t, err)
		require.Equal(t, types.InvoiceTypeOneOff, replacement.InvoiceType)
		require.Equal(t, "40", replacement.Subtotal.String())
		current, err := f.items.Get(f.ctx, items[0].ID)
		require.NoError(t, err)
		fresh := replanCorrection(t, f, replacement.ID, current)
		picked, err := due.CreateOrRecover(f.ctx, core.DueInvoiceRequest{ItemID: fresh[0].ID, AsOf: time.Now().UTC()})
		require.NoError(t, err)
		require.NotEmpty(t, picked.InvoiceID)
		err = f.service.ValidateInvoiceCorrection(f.ctx, replacement.ID, dto.RecalculateInvoiceRequest{BillingScheduleItemIDs: []string{fresh[0].ID}})
		require.True(t, ierr.IsVersionConflict(err), "%v", err)
	})
	t.Run("failed replacement leaves no link and no claim", func(t *testing.T) {
		f := newInvoiceCollectionFixture(t, db)
		_, _, err := f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil)
		require.NoError(t, err)
		item, err := f.items.Get(f.ctx, f.itemID)
		require.NoError(t, err)
		items := replanCorrection(t, f, f.invoice.ID, item)
		f.service.InvoiceRepo = &failCollectionInvoiceUpdate{f.service.InvoiceRepo}
		_, err = f.service.RecalculateInvoiceWithSchedule(f.ctx, f.invoice.ID, dto.RecalculateInvoiceRequest{BillingScheduleItemIDs: []string{items[0].ID}})
		require.ErrorContains(t, err, "injected total persistence failure")
		original, err := f.service.InvoiceRepo.Get(f.ctx, f.invoice.ID)
		require.NoError(t, err)
		require.Nil(t, original.RecalculatedInvoiceID)
		current, err := f.items.Get(f.ctx, items[0].ID)
		require.NoError(t, err)
		require.Nil(t, current.InvoiceID)
	})
}
