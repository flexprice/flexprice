package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestDraftActivationRejectsConcurrentLifecycleChange(t *testing.T) {
	for _, status := range []types.SubscriptionStatus{types.SubscriptionStatusCancelled, types.SubscriptionStatusPaused, types.SubscriptionStatusActive} {
		t.Run(string(status), func(t *testing.T) {
			fixture, svc, sub := newLifecycleFixture(t)
			ctx := fixture.GetContext()
			draft := subscription.NewSubscriptionBuilder(sub).WithStatus(types.SubscriptionStatusDraft).Build()
			require.NoError(t, svc.SubRepo.Update(ctx, draft))
			repo := svc.SubRepo
			svc.SubRepo = &cancellationRaceRepository{Repository: repo, beforeLock: func(tx context.Context, id string) {
				require.NoError(t, repo.Update(tx, subscription.NewSubscriptionBuilder(draft).WithStatus(status).Build()))
			}}
			_, err := svc.ActivateDraftSubscription(ctx, sub.ID, dto.ActivateDraftSubscriptionRequest{StartDate: lo.ToPtr(time.Now().UTC())})
			require.Error(t, err)
			current, err := repo.Get(ctx, sub.ID)
			require.NoError(t, err)
			require.Equal(t, status, current.SubscriptionStatus)
			require.Equal(t, sub.StartDate, current.StartDate)
			invoices, err := svc.InvoiceRepo.List(ctx, types.NewNoLimitInvoiceFilter())
			require.NoError(t, err)
			require.Empty(t, invoices)
			require.Empty(t, fixture.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
		})
	}
}

func TestDraftActivationRetryDoesNotRebillOrShiftDates(t *testing.T) {
	fixture, svc, sub := newLifecycleFixture(t)
	ctx := fixture.GetContext()
	draft := subscription.NewSubscriptionBuilder(sub).WithStatus(types.SubscriptionStatusDraft).Build()
	require.NoError(t, svc.SubRepo.Update(ctx, draft))
	activationDate := time.Now().UTC().Truncate(time.Second)
	response, err := svc.ActivateDraftSubscription(ctx, sub.ID, dto.ActivateDraftSubscriptionRequest{StartDate: &activationDate})
	require.NoError(t, err)
	require.NotEqual(t, types.SubscriptionStatusDraft, response.SubscriptionStatus)
	invoices, err := svc.InvoiceRepo.List(ctx, types.NewNoLimitInvoiceFilter())
	require.NoError(t, err)
	firstEvents := len(fixture.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events())
	_, err = svc.ActivateDraftSubscription(ctx, sub.ID, dto.ActivateDraftSubscriptionRequest{StartDate: lo.ToPtr(activationDate.Add(time.Hour))})
	require.Error(t, err)
	current, err := svc.SubRepo.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, activationDate, current.StartDate)
	retriedInvoices, err := svc.InvoiceRepo.List(ctx, types.NewNoLimitInvoiceFilter())
	require.NoError(t, err)
	require.Len(t, retriedInvoices, len(invoices))
	require.Len(t, fixture.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher).Events(), firstEvents)
}
