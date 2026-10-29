package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/metrics/metricstest"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/stretchr/testify/require"
)

type refundPostCommitPublisher struct {
	events   []*types.WebhookEvent
	contexts []context.Context
}

func (p *refundPostCommitPublisher) PublishWebhook(ctx context.Context, event *types.WebhookEvent) error {
	p.contexts = append(p.contexts, ctx)
	p.events = append(p.events, event)
	return nil
}

func (p *refundPostCommitPublisher) Close() error { return nil }

func TestRefundWebhooks_WaitForOutermostCommit(t *testing.T) {
	for _, outcome := range []string{"outside transaction", "commit", "rollback"} {
		t.Run(outcome, func(t *testing.T) {
			ctx := types.SetTenantID(context.Background(), "tenant_refund_event")
			ctx = types.SetEnvironmentID(ctx, "env_refund_event")
			ctx = types.SetUserID(ctx, "user_refund_event")
			if outcome != "outside transaction" {
				ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
				ctx = types.WithPostCommitHooks(ctx)
			}
			publisher := &refundPostCommitPublisher{}
			s := &refundService{ServiceParams: ServiceParams{Logger: logger.NewNoopLogger(), WebhookPublisher: publisher}}
			for _, event := range []types.WebhookEventName{types.WebhookEventRefundCreated, types.WebhookEventRefundSucceeded} {
				s.publishSystemEvent(ctx, event, "refund_event")
			}
			if outcome != "outside transaction" {
				require.Empty(t, publisher.events)
				if outcome == "rollback" {
					types.DiscardPostCommitHooks(ctx)
				} else {
					types.RunPostCommitHooks(ctx)
				}
			}
			if outcome == "rollback" {
				types.RunPostCommitHooks(ctx)
				require.Empty(t, publisher.events)
				return
			}
			require.Len(t, publisher.events, 2)
			require.Equal(t, types.WebhookEventRefundCreated, publisher.events[0].EventName)
			require.Equal(t, types.WebhookEventRefundSucceeded, publisher.events[1].EventName)
			for i, event := range publisher.events {
				require.Nil(t, publisher.contexts[i].Value(types.CtxDBTransaction))
				require.Equal(t, "tenant_refund_event", types.GetTenantID(publisher.contexts[i]))
				require.Equal(t, "env_refund_event", types.GetEnvironmentID(publisher.contexts[i]))
				require.Equal(t, "user_refund_event", types.GetUserID(publisher.contexts[i]))
				require.Equal(t, "refund_event", event.EntityID)
				require.Equal(t, "tenant_refund_event", event.TenantID)
				var payload webhookDto.InternalRefundEvent
				require.NoError(t, json.Unmarshal(event.Payload, &payload))
				require.Equal(t, "refund_event", payload.RefundID)
			}
			types.RunPostCommitHooks(ctx)
			require.Len(t, publisher.events, 2)
		})
	}
}

func TestRefundTransitions_CountOnlyCommittedStates(t *testing.T) {
	reader := metricstest.Install(t)
	for _, outcome := range []string{"outside transaction", "commit", "rollback"} {
		t.Run(outcome, func(t *testing.T) {
			ctx := context.Background()
			if outcome != "outside transaction" {
				ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
				ctx = types.WithPostCommitHooks(ctx)
			}
			count := func(status types.RefundStatus) int64 {
				return reader.Sum("refund.transitions", map[string]string{"provider": "none", "destination": string(types.RefundDestinationWallet), "reason": string(types.RefundReasonOrderChange), "status": string(status)})
			}
			beforePending, beforeSucceeded := count(types.RefundStatusPending), count(types.RefundStatusSucceeded)
			row := &refund.Refund{RefundStatus: types.RefundStatusPending, RefundDestination: types.RefundDestinationWallet, RefundReason: types.RefundReasonOrderChange}
			recordRefundTransition(ctx, row)
			row.RefundStatus = types.RefundStatusSucceeded
			recordRefundTransition(ctx, row)
			if outcome != "outside transaction" {
				require.Equal(t, beforePending, count(types.RefundStatusPending))
				require.Equal(t, beforeSucceeded, count(types.RefundStatusSucceeded))
				if outcome == "rollback" {
					types.DiscardPostCommitHooks(ctx)
				} else {
					types.RunPostCommitHooks(ctx)
				}
			}
			increment := int64(1)
			if outcome == "rollback" {
				increment = 0
			}
			require.Equal(t, beforePending+increment, count(types.RefundStatusPending))
			require.Equal(t, beforeSucceeded+increment, count(types.RefundStatusSucceeded))
		})
	}
}
