package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func newBillingDueFixture(t *testing.T, db postgres.IClient, manual bool) (*invoiceCollectionFixture, BillingScheduleService, *domain.ScheduleItem) {
	t.Helper()
	f := newInvoiceCollectionFixture(t, db)
	setBillingDueFinalizationDelay(t, f.ctx, f.service.ServiceParams, 0)
	old, err := f.items.Get(f.ctx, f.itemID)
	require.NoError(t, err)
	owner, err := f.service.CommitmentRepo.Get(f.ctx, old.OwnerID)
	require.NoError(t, err)
	copied := *owner
	copied.ID = types.GenerateUUIDWithPrefix("comm")
	copied.InstalmentInvoicing = lo.ToPtr(string(types.BillingTermSeparateInvoice))
	_, err = f.service.CommitmentRepo.Create(f.ctx, &copied)
	require.NoError(t, err)
	row := core.RuleInput{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, OnTrigger: manual}
	if !manual {
		row.BillOn = &copied.StartDate
	}
	state, err := f.service.BillingTerms.Initialize(f.ctx, core.InitializeRequest{Owner: domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: copied.ID}, Rows: []core.RuleInput{row}})
	require.NoError(t, err)
	return f, NewBillingScheduleService(f.service.ServiceParams, f.items), state.Items()[0]
}

func setBillingDueFinalizationDelay(t *testing.T, ctx context.Context, params ServiceParams, seconds int) {
	t.Helper()
	settings := NewSettingsService(params).(*settingsService)
	configuration, err := GetSetting[types.InvoiceConfig](settings, ctx, types.SettingKeyInvoiceConfig)
	require.NoError(t, err)
	configuration.FinalizationDelaySeconds = seconds
	require.NoError(t, UpdateSetting(settings, ctx, types.SettingKeyInvoiceConfig, configuration))
	stored, err := params.SettingsRepo.GetByKey(ctx, types.SettingKeyInvoiceConfig)
	require.NoError(t, err)
	stored.Value["finalization_delay_seconds"] = seconds
	require.NoError(t, params.SettingsRepo.Update(ctx, stored))
}

func TestBillingScheduleDuePostgres(t *testing.T) {
	db := newInvoiceVoidPostgresClient(t)
	t.Run("concurrent delivery converges on one invoice", func(t *testing.T) {
		f, service, item := newBillingDueFixture(t, db, false)
		request := core.DueInvoiceRequest{ItemID: item.ID, AsOf: time.Now().UTC()}
		results := make([]*core.DueInvoice, 2)
		failures := make([]error, 2)
		var workers sync.WaitGroup
		for index := range results {
			workers.Add(1)
			go func(index int) {
				defer workers.Done()
				results[index], failures[index] = service.CreateOrRecover(f.ctx, request)
				if ierr.IsVersionConflict(failures[index]) {
					results[index], failures[index] = service.CreateOrRecover(f.ctx, request)
				}
			}(index)
		}
		workers.Wait()
		for _, err := range failures {
			require.NoError(t, err)
		}
		require.NotEmpty(t, results[0].InvoiceID)
		require.Equal(t, results[0].InvoiceID, results[1].InvoiceID)
	})
	t.Run("create retry archive and void retain one exact source", func(t *testing.T) {
		f, service, item := newBillingDueFixture(t, db, false)
		request := core.DueInvoiceRequest{ItemID: item.ID, AsOf: time.Now().UTC()}
		first, err := service.CreateOrRecover(f.ctx, request)
		require.NoError(t, err)
		require.NotEmpty(t, first.InvoiceID)
		inv, err := f.service.InvoiceRepo.Get(f.ctx, first.InvoiceID)
		require.NoError(t, err)
		require.Equal(t, types.InvoiceTypeOneOff, inv.InvoiceType)
		require.Equal(t, types.InvoiceStatusDraft, inv.InvoiceStatus)
		require.Equal(t, "100", inv.Subtotal.String())
		require.NotNil(t, inv.LastComputedAt)
		require.Len(t, inv.LineItems, 1)
		require.Nil(t, inv.LineItems[0].PriceID)
		require.Equal(t, item.ID, lo.FromPtr(inv.LineItems[0].BillingScheduleItemID))
		require.NoError(t, db.WithTx(f.ctx, func(tx context.Context) error {
			owner := domain.OwnerRef{Type: item.OwnerType, ID: item.OwnerID}
			if err := f.service.BillingTermsStore.LockOwner(tx, owner); err != nil {
				return err
			}
			return f.items.Archive(tx, owner)
		}))
		again, err := service.CreateOrRecover(f.ctx, request)
		require.NoError(t, err)
		require.Equal(t, first.InvoiceID, again.InvoiceID)
		filter := &domain.ScheduleItemFilter{IDs: []string{item.ID}, IncludeArchived: true, RecoverableInvoicesOnly: true}
		recoverable, err := f.items.List(f.ctx, filter)
		require.NoError(t, err)
		require.Len(t, recoverable, 1)
		_, skipped, err := f.service.ComputeInvoice(f.ctx, first.InvoiceID, nil)
		require.NoError(t, err)
		require.False(t, skipped)
		_, err = f.service.VoidInvoice(f.ctx, first.InvoiceID, dto.InvoiceVoidRequest{})
		require.NoError(t, err)
		afterVoid, err := service.CreateOrRecover(f.ctx, request)
		require.NoError(t, err)
		require.Empty(t, afterVoid.InvoiceID)
		recoverable, err = f.items.List(f.ctx, filter)
		require.NoError(t, err)
		require.Empty(t, recoverable)
	})
	t.Run("recovery excludes settled payment history", func(t *testing.T) {
		f, service, item := newBillingDueFixture(t, db, false)
		result, err := service.CreateOrRecover(f.ctx, core.DueInvoiceRequest{ItemID: item.ID, AsOf: time.Now().UTC()})
		require.NoError(t, err)
		inv, err := f.service.InvoiceRepo.Get(f.ctx, result.InvoiceID)
		require.NoError(t, err)
		filter := &domain.ScheduleItemFilter{IDs: []string{item.ID}, IncludeArchived: true, RecoverableInvoicesOnly: true}
		for _, status := range []types.PaymentStatus{types.PaymentStatusPending, types.PaymentStatusProcessing, types.PaymentStatusFailed, types.PaymentStatusSucceeded, types.PaymentStatusOverpaid, types.PaymentStatusRefunded} {
			updated := invoice.NewInvoiceBuilder(inv).WithInvoiceStatus(types.InvoiceStatusFinalized).WithPaymentStatus(status).Build()
			require.NoError(t, f.service.InvoiceRepo.Update(f.ctx, updated))
			items, err := f.items.List(f.ctx, filter)
			require.NoError(t, err)
			if status == types.PaymentStatusPending || status == types.PaymentStatusProcessing || status == types.PaymentStatusFailed {
				require.Len(t, items, 1, status)
			} else {
				require.Empty(t, items, status)
			}
		}
	})
	t.Run("failed totals roll back draft line and claim", func(t *testing.T) {
		f, _, item := newBillingDueFixture(t, db, false)
		params := f.service.ServiceParams
		params.InvoiceRepo = &failCollectionInvoiceUpdate{params.InvoiceRepo}
		service := NewBillingScheduleService(params, f.items)
		_, err := service.CreateOrRecover(f.ctx, core.DueInvoiceRequest{ItemID: item.ID, AsOf: time.Now().UTC()})
		require.ErrorContains(t, err, "injected total persistence failure")
		current, err := f.items.Get(f.ctx, item.ID)
		require.NoError(t, err)
		require.Equal(t, types.BillingScheduleScheduled, current.ScheduleStatus)
		require.Nil(t, current.InvoiceID)
		_, err = f.service.InvoiceRepo.GetByIdempotencyKey(f.ctx, "billing_schedule:"+item.ID)
		require.True(t, ierr.IsNotFound(err), "%v", err)
	})
	t.Run("pause defers original date and resume collects", func(t *testing.T) {
		f, service, item := newBillingDueFixture(t, db, false)
		sub, err := f.service.SubRepo.Get(f.ctx, *item.SubscriptionID)
		require.NoError(t, err)
		sub.SubscriptionStatus = types.SubscriptionStatusPaused
		require.NoError(t, f.service.SubRepo.Update(f.ctx, sub))
		request := core.DueInvoiceRequest{ItemID: item.ID, AsOf: time.Now().UTC()}
		paused, err := service.CreateOrRecover(f.ctx, request)
		require.NoError(t, err)
		require.Empty(t, paused.InvoiceID)
		require.Equal(t, "subscription_paused", paused.DeferredReason)
		sub.SubscriptionStatus = types.SubscriptionStatusActive
		require.NoError(t, f.service.SubRepo.Update(f.ctx, sub))
		resumed, err := service.CreateOrRecover(f.ctx, request)
		require.NoError(t, err)
		require.NotEmpty(t, resumed.InvoiceID)
		current, err := f.items.Get(f.ctx, item.ID)
		require.NoError(t, err)
		require.True(t, current.BillOn.Equal(*item.BillOn))
	})
	t.Run("bounded phases resolve manual fallback and preserve scope", func(t *testing.T) {
		f, service, item := newBillingDueFixture(t, db, true)
		request := core.DueBatchRequest{AsOf: item.ServicePeriodEnd, Limit: 1}
		var found bool
		for page := 0; page < 20; page++ {
			batch, err := service.Prepare(f.ctx, request)
			require.NoError(t, err)
			require.LessOrEqual(t, batch.Scanned, 1)
			for _, candidate := range batch.Items {
				found = found || candidate.ItemID == item.ID
			}
			if batch.NextCursor == "" {
				break
			}
			request.Cursor = batch.NextCursor
			_, err = service.Prepare(types.SetEnvironmentID(f.ctx, "another_env"), request)
			require.True(t, ierr.IsValidation(err))
		}
		require.True(t, found)
		current, err := f.items.Get(f.ctx, item.ID)
		require.NoError(t, err)
		require.True(t, current.BillOn.Equal(item.ServicePeriodEnd))
		owners, err := f.service.CommitmentRepo.List(f.ctx, &commitment.Filter{IDs: []string{item.OwnerID}})
		require.NoError(t, err)
		require.Len(t, owners, 1)
	})
}
