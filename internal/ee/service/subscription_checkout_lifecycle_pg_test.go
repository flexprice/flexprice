package service

import (
	"context"
	"errors"
	"testing"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	repository "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCheckoutLifecycle_PostgresActivationCommitBoundary(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			db := newInvoiceVoidPostgresClient(t)
			s, svc, sub, _, _ := newPaymentLifecycleFixture(t)
			svc.DB = db
			svc.SubRepo = repository.NewSubscriptionRepository(db, svc.Logger, testutil.NewInMemoryRedis())
			svc.BillingTermsStore = repository.NewBillingTermsExecutionStore(db)
			sub.SubscriptionStatus = types.SubscriptionStatusDraft
			require.NoError(t, svc.SubRepo.Create(s.GetContext(), sub))
			pub := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
			abort := errors.New("abort checkout")
			err := db.WithTx(s.GetContext(), func(tx context.Context) error {
				if err := svc.activateDraftSubscription(tx, sub); err != nil {
					return err
				}
				require.Empty(t, pub.Events())
				if !commit {
					return abort
				}
				return nil
			})
			if commit {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, abort)
			}
			fresh, err := svc.SubRepo.Get(types.WithForceWriter(s.GetContext()), sub.ID)
			require.NoError(t, err)
			if commit {
				require.Equal(t, types.SubscriptionStatusActive, fresh.SubscriptionStatus)
				require.Len(t, pub.Events(), 1)
				require.NoError(t, svc.activateDraftSubscription(s.GetContext(), sub))
				require.Len(t, pub.Events(), 1)
			} else {
				require.Equal(t, types.SubscriptionStatusDraft, fresh.SubscriptionStatus)
				require.Empty(t, pub.Events())
			}
		})
	}
}

func TestCheckoutLifecycle_PostgresCommitmentCleanup(t *testing.T) {
	for _, assigned := range []bool{false, true} {
		t.Run(map[bool]string{false: "unassigned schedule", true: "assigned principal"}[assigned], func(t *testing.T) {
			db := newInvoiceVoidPostgresClient(t)
			f := newInvoiceCollectionFixture(t, db)
			if assigned {
				_, _, err := f.service.ComputeInvoice(f.ctx, f.invoice.ID, nil)
				require.NoError(t, err)
			}
			sub, err := f.service.SubRepo.Get(f.ctx, *f.invoice.SubscriptionID)
			require.NoError(t, err)
			owners, err := f.service.CommitmentRepo.List(f.ctx, commitment.NewNoLimitFilter())
			require.NoError(t, err)
			require.Len(t, owners, 1)
			owner := owners[0]
			owner.InvoiceLabel = owner.Name
			owner.ScopeEntityType, owner.ScopeEntityID = types.CommitmentScopeSubscription, sub.ID
			owner.Type, owner.Value, owner.Measurement = types.COMMITMENT_TYPE_AMOUNT, decimal.NewFromInt(100), types.CommitmentMeasurementBillingPeriod
			sub.SubscriptionStatus = types.SubscriptionStatusDraft
			sub.BillingCycle = types.BillingCycleAnniversary
			sub.ProrationBehavior = types.ProrationBehaviorNone
			params := f.service.ServiceParams
			params.SubRepo = repository.NewSubscriptionRepository(db, params.Logger, testutil.NewInMemoryRedis())
			params.CommitmentRepo = repository.NewCommitmentRepository(db, params.Logger)
			require.NoError(t, params.SubRepo.Create(f.ctx, sub))
			_, err = params.CommitmentRepo.Create(f.ctx, owner)
			require.NoError(t, err)
			resolver, err := core.NewOwnerResolver(NewCommitmentTermsOwner(params.CommitmentRepo, params.SubRepo))
			require.NoError(t, err)
			params.BillingTerms = core.NewService(repository.NewBillingTermRepository(db), f.items, params.BillingTermsStore, resolver)
			svc := NewSubscriptionService(params).(*subscriptionService)
			err = svc.archiveDraftCheckoutSubscription(f.ctx, sub.ID)
			if assigned {
				require.True(t, ierr.IsVersionConflict(err), "%+v", err)
				fresh, err := params.SubRepo.Get(types.WithForceWriter(f.ctx), sub.ID)
				require.NoError(t, err)
				require.Equal(t, types.StatusPublished, fresh.Status)
				_, err = params.CommitmentRepo.Get(f.ctx, owner.ID)
				require.NoError(t, err)
			} else {
				require.NoError(t, err)
				_, err = params.CommitmentRepo.Get(f.ctx, owner.ID)
				require.True(t, ierr.IsNotFound(err))
			}
			items, err := f.items.List(f.ctx, &domain.ScheduleItemFilter{Owner: lo.ToPtr(commitmentBillingOwner(owner.ID)), IncludeArchived: true, Limit: 500})
			require.NoError(t, err)
			require.Len(t, items, 1)
			if assigned {
				require.Equal(t, types.BillingScheduleInvoiced, items[0].ScheduleStatus)
				require.Equal(t, types.StatusPublished, items[0].Status)
			} else {
				require.Equal(t, types.BillingScheduleCanceled, items[0].ScheduleStatus)
				require.Equal(t, types.StatusArchived, items[0].Status)
			}
		})
	}
}
