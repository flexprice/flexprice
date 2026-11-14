package webhook

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// fakeRefundService mirrors the real service's one guarantee that matters here:
// a row settles or fails at most once, however many times the webhook arrives.
type fakeRefundService struct {
	row           *refund.Refund
	settleCalls   []*dto.SettleRefundRequest
	failReasons   []string
	appliedSettle int
	appliedFail   int
}

func (f *fakeRefundService) GetRefundByGatewayRefundID(_ context.Context, _, gatewayRefundID string) (*dto.RefundResponse, error) {
	if f.row == nil || f.row.GatewayRefundID == nil || *f.row.GatewayRefundID != gatewayRefundID {
		return nil, ierr.NewError("refund not found").Mark(ierr.ErrNotFound)
	}
	return dto.NewRefundResponse(f.row), nil
}

func (f *fakeRefundService) Settle(_ context.Context, req *dto.SettleRefundRequest) error {
	f.settleCalls = append(f.settleCalls, req)
	if f.row.RefundStatus.IsTerminal() {
		return nil
	}
	f.row.RefundStatus = types.RefundStatusSucceeded
	f.row.SettledAmount = req.SettledAmount
	f.appliedSettle++
	return nil
}

func (f *fakeRefundService) Fail(_ context.Context, _, reason string) error {
	f.failReasons = append(f.failReasons, reason)
	if f.row.RefundStatus.IsTerminal() {
		return nil
	}
	f.row.RefundStatus = types.RefundStatusFailed
	f.appliedFail++
	return nil
}

type ChargebeeRefundWebhookSuite struct {
	suite.Suite
	ctx      context.Context
	handler  *Handler
	refundSv *fakeRefundService
	services *ServiceDependencies
}

func TestChargebeeRefundWebhook(t *testing.T) {
	suite.Run(t, new(ChargebeeRefundWebhookSuite))
}

func (s *ChargebeeRefundWebhookSuite) SetupTest() {
	s.ctx = types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_test"), "env_test")

	s.refundSv = &fakeRefundService{
		row: &refund.Refund{
			ID:                "refund_001",
			InvoiceID:         "inv_001",
			Amount:            decimal.NewFromInt(25),
			Currency:          "USD",
			RefundStatus:      types.RefundStatusProcessing,
			RefundDestination: types.RefundDestinationGateway,
			GatewayRefundID:   lo.ToPtr("txn_refund_001"),
		},
	}
	s.handler = NewHandler(nil, nil, nil, logger.NewNoopLogger())
	s.services = &ServiceDependencies{RefundService: s.refundSv}
}

func (s *ChargebeeRefundWebhookSuite) event(eventType ChargebeeEventType, txnID, status string, amountMinor int64) *ChargebeeWebhookEvent {
	content, err := json.Marshal(ChargebeeWebhookContent{
		Transaction: &ChargebeeTransaction{
			ID:           txnID,
			Type:         "refund",
			Status:       status,
			Amount:       amountMinor,
			CurrencyCode: "USD",
		},
	})
	s.Require().NoError(err)
	return &ChargebeeWebhookEvent{ID: "evt_001", EventType: string(eventType), Content: content}
}

func (s *ChargebeeRefundWebhookSuite) TestPaymentRefundedSettlesOnceOnReplay() {
	event := s.event(EventPaymentRefunded, "txn_refund_001", "success", 2500)

	s.NoError(s.handler.HandleWebhookEvent(s.ctx, event, "env_test", s.services))
	s.NoError(s.handler.HandleWebhookEvent(s.ctx, event, "env_test", s.services))

	s.Len(s.refundSv.settleCalls, 2)
	s.Equal(1, s.refundSv.appliedSettle)
	s.True(decimal.NewFromInt(25).Equal(s.refundSv.settleCalls[0].SettledAmount))
	s.Equal("txn_refund_001", *s.refundSv.settleCalls[0].DestinationID)
	s.Equal("refund_001", s.refundSv.settleCalls[0].RefundID)
}

func (s *ChargebeeRefundWebhookSuite) TestFailedTransactionFailsTheRow() {
	event := s.event(EventPaymentRefunded, "txn_refund_001", "failure", 2500)

	s.NoError(s.handler.HandleWebhookEvent(s.ctx, event, "env_test", s.services))

	s.Len(s.refundSv.failReasons, 1)
	s.Equal(1, s.refundSv.appliedFail)
	s.Empty(s.refundSv.settleCalls)
}

func (s *ChargebeeRefundWebhookSuite) TestUnknownTransactionIsIgnored() {
	s.NoError(s.handler.HandleWebhookEvent(s.ctx, s.event(EventPaymentRefunded, "txn_other", "success", 2500), "env_test", s.services))

	s.Empty(s.refundSv.settleCalls)
	s.Empty(s.refundSv.failReasons)
}

func (s *ChargebeeRefundWebhookSuite) TestRefundInitiatedIsANoOp() {
	s.NoError(s.handler.HandleWebhookEvent(s.ctx, s.event(EventRefundInitiated, "txn_refund_001", "success", 2500), "env_test", s.services))

	s.Empty(s.refundSv.settleCalls)
	s.Empty(s.refundSv.failReasons)
}
