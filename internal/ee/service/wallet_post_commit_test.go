package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/alertlogs"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type walletPostCommitPublisher struct {
	contexts []context.Context
	events   []*types.WebhookEvent
}

func (p *walletPostCommitPublisher) PublishWebhook(ctx context.Context, event *types.WebhookEvent) error {
	p.contexts = append(p.contexts, ctx)
	p.events = append(p.events, event)
	return nil
}

func (p *walletPostCommitPublisher) Close() error { return nil }

type walletPostCommitAlerts struct {
	alertlogs.Repository
	contexts []context.Context
}

func (r *walletPostCommitAlerts) GetLatestAlert(ctx context.Context, entityType types.AlertEntityType, entityID string, alertType *types.AlertType, parentType, parentID, settingID *string, periodStart *time.Time) (*alertlogs.AlertLog, error) {
	r.contexts = append(r.contexts, ctx)
	return r.Repository.GetLatestAlert(ctx, entityType, entityID, alertType, parentType, parentID, settingID, periodStart)
}

func TestWalletAfterCommit(t *testing.T) {
	for _, mode := range []string{"commit", "rollback", "immediate", "finished"} {
		t.Run(mode, func(t *testing.T) {
			ctx := types.SetEnvironmentID(context.WithValue(context.Background(), types.CtxTenantID, "tenant-postcommit"), "env-postcommit")
			ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
			if mode != "immediate" {
				ctx = types.WithPostCommitHooks(ctx)
			}
			if mode == "finished" {
				types.RunPostCommitHooks(ctx)
			}
			calls := 0
			walletAfterCommit(ctx, func(effect context.Context) {
				calls++
				require.Nil(t, effect.Value(types.CtxDBTransaction))
				require.Equal(t, "tenant-postcommit", types.GetTenantID(effect))
				require.Equal(t, "env-postcommit", types.GetEnvironmentID(effect))
			})
			if mode == "commit" || mode == "rollback" {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
			if mode == "rollback" {
				types.DiscardPostCommitHooks(ctx)
			}
			types.RunPostCommitHooks(ctx)
			if mode == "rollback" {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
			types.RunPostCommitHooks(ctx)
			if mode == "rollback" {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}

func (s *WalletServiceSuite) TestPostCommitWalletCreated() {
	for _, mode := range []string{"commit", "rollback", "immediate"} {
		s.Run(mode, func() {
			service := s.service.(*walletService)
			publisher := &walletPostCommitPublisher{}
			service.WebhookPublisher = publisher
			ctx := s.GetContext()
			if mode != "immediate" {
				ctx = types.WithPostCommitHooks(context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{}))
			}
			response, err := service.EnsurePrepaidWallet(ctx, "postcommit-customer-"+mode, "usd")
			s.Require().NoError(err)
			s.Require().NotNil(response)
			if mode != "immediate" {
				s.Empty(publisher.events, "new wallet must not be published before the outer transaction commits")
			}
			if mode == "rollback" {
				types.DiscardPostCommitHooks(ctx)
			}
			types.RunPostCommitHooks(ctx)
			if mode == "rollback" {
				s.Empty(publisher.events)
				return
			}
			s.Require().Len(publisher.events, 1)
			s.Equal(types.WebhookEventWalletCreated, publisher.events[0].EventName)
			s.Equal(response.ID, publisher.events[0].EntityID)
			s.assertWalletPostCommitContexts(publisher.contexts)
		})
	}
}

func (s *WalletServiceSuite) TestPostCommitRefundWalletCredit() {
	s.checkRefundWalletPostCommit("commit")
}
func (s *WalletServiceSuite) TestPostCommitRefundWalletRollback() {
	s.checkRefundWalletPostCommit("rollback")
}
func (s *WalletServiceSuite) TestPostCommitRefundWalletImmediate() {
	s.checkRefundWalletPostCommit("immediate")
}

func (s *WalletServiceSuite) checkRefundWalletPostCommit(mode string) {
	service := s.service.(*walletService)
	publisher := &walletPostCommitPublisher{}
	alerts := &walletPostCommitAlerts{Repository: service.AlertLogsRepo}
	service.WebhookPublisher = publisher
	service.AlertLogsRepo = alerts
	w, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.Require().NoError(err)
	w.AlertSettings = &types.AlertSettings{AlertEnabled: lo.ToPtr(true), Critical: &types.AlertThreshold{Threshold: decimal.NewFromInt(10000), Condition: types.AlertConditionBelow}}
	s.Require().NoError(s.GetStores().WalletRepo.UpdateWallet(s.GetContext(), w.ID, w))
	ctx := s.GetContext()
	if mode != "immediate" {
		ctx = types.WithPostCommitHooks(context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{}))
	}
	response, err := service.TopUpWallet(ctx, w.ID, &dto.TopUpWalletRequest{Amount: decimal.NewFromInt(25), TransactionReason: types.TransactionReasonInvoiceVoidRefund, IdempotencyKey: lo.ToPtr("postcommit-refund-" + mode), SourceType: types.WalletTxSourceTypeInvoice, SourceID: "invoice-refund"})
	s.Require().NoError(err)
	s.Require().NotNil(response.WalletTransaction)
	s.True(response.Wallet.CreditBalance.Equal(decimal.NewFromInt(1025)), "balance update stays inside the caller's transaction")
	if mode != "immediate" {
		s.Empty(publisher.events)
		s.Empty(alerts.contexts, "alerts must wait for the outer transaction")
	}
	if mode == "rollback" {
		types.DiscardPostCommitHooks(ctx)
	}
	types.RunPostCommitHooks(ctx)
	if mode == "rollback" {
		s.Empty(publisher.events)
		s.Empty(alerts.contexts)
		return
	}
	transactionEvents := 0
	for _, event := range publisher.events {
		if event.EventName == types.WebhookEventWalletTransactionCreated {
			transactionEvents++
			s.Equal(response.WalletTransaction.ID, event.EntityID)
		}
	}
	s.Equal(1, transactionEvents)
	s.NotEmpty(alerts.contexts, "committed credits evaluate their alerts")
	s.assertWalletPostCommitContexts(publisher.contexts)
	s.assertWalletPostCommitContexts(alerts.contexts)
}

func (s *WalletServiceSuite) assertWalletPostCommitContexts(contexts []context.Context) {
	for _, ctx := range contexts {
		s.Nil(ctx.Value(types.CtxDBTransaction))
		s.Equal(types.GetTenantID(s.GetContext()), types.GetTenantID(ctx))
		s.Equal(types.GetEnvironmentID(s.GetContext()), types.GetEnvironmentID(ctx))
	}
}
