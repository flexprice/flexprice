package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/metrics"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type RefundService interface {
	// PrepareRefundsForCreditNote and PrepareRefundsForVoidedInvoice persist PENDING refund rows in the caller's
	// transaction. They touch no gateway; Dispatch does that after the commit.
	PrepareRefundsForCreditNote(ctx context.Context, cn *creditnote.CreditNote, inv *invoice.Invoice, target *types.RefundTarget) ([]*refund.Refund, error)
	PrepareRefundsForVoidedInvoice(ctx context.Context, inv *invoice.Invoice, amount decimal.Decimal) ([]*refund.Refund, error)

	// Dispatch moves one planned row towards settlement. Must run outside a transaction.
	Dispatch(ctx context.Context, refundID string) error

	Settle(ctx context.Context, req *dto.SettleRefundRequest) error
	Fail(ctx context.Context, refundID, reason string) error

	GetRefund(ctx context.Context, id string) (*dto.RefundResponse, error)
	GetRefundByGatewayRefundID(ctx context.Context, gateway, gatewayRefundID string) (*dto.RefundResponse, error)
	ListRefunds(ctx context.Context, filter *types.RefundFilter) (*dto.ListRefundsResponse, error)
	RetryRefund(ctx context.Context, id string) (*dto.RefundResponse, error)
}

type refundService struct {
	ServiceParams
}

func NewRefundService(params ServiceParams) RefundService {
	return &refundService{ServiceParams: params}
}

// gatewayRefundableMethods are the payment methods whose money left through a
// gateway and so can be returned the same way.
var gatewayRefundableMethods = []types.PaymentMethodType{
	types.PaymentMethodTypeCard,
	types.PaymentMethodTypePaymentLink,
	types.PaymentMethodTypeUPI,
}

func (s *refundService) PrepareRefundsForCreditNote(ctx context.Context, cn *creditnote.CreditNote, inv *invoice.Invoice, target *types.RefundTarget) ([]*refund.Refund, error) {
	if cn == nil || inv == nil {
		return nil, ierr.NewError("missing credit note or invoice").
			WithHint("A refund plan needs both a credit note and its invoice.").
			Mark(ierr.ErrValidation)
	}

	if target != nil {
		if err := target.Validate(); err != nil {
			return nil, err
		}
	}

	rows, err := s.allocateAcrossPayments(ctx, inv, cn.TotalAmount, allocationContext{
		creditNoteID:   lo.ToPtr(cn.ID),
		reason:         refundReasonFromCreditNote(cn.Reason),
		idempotencyKey: cn.ID,
		allowGateway:   target.AllowsBackToSource(),
	})
	if err != nil {
		return nil, err
	}

	return s.persist(ctx, rows)
}

func (s *refundService) PrepareRefundsForVoidedInvoice(ctx context.Context, inv *invoice.Invoice, amount decimal.Decimal) ([]*refund.Refund, error) {
	if inv == nil {
		return nil, ierr.NewError("missing invoice").
			WithHint("A void refund plan needs an invoice.").
			Mark(ierr.ErrValidation)
	}

	rows, err := s.allocateAcrossPayments(ctx, inv, amount, allocationContext{
		reason:         types.RefundReasonOrderChange,
		idempotencyKey: fmt.Sprintf("%s-void", inv.ID),
		allowGateway:   false,
	})
	if err != nil {
		return nil, err
	}

	return s.persist(ctx, rows)
}

func (s *refundService) persist(ctx context.Context, rows []*refund.Refund) ([]*refund.Refund, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	if err := s.RefundRepo.CreateBulk(ctx, rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		recordRefundTransition(ctx, row)
		s.publishSystemEvent(ctx, types.WebhookEventRefundCreated, row.ID)
	}
	return rows, nil
}

type allocationContext struct {
	creditNoteID   *string
	reason         types.RefundReason
	idempotencyKey string
	allowGateway   bool
}

// allocateAcrossPayments splits amount over the invoice's succeeded payments, bounded
// by what each payment can still give back: what it took in, less what has already
// gone back out of it, settled or still in flight. Anything no payment can cover
// becomes a single wallet row.
func (s *refundService) allocateAcrossPayments(
	ctx context.Context,
	inv *invoice.Invoice,
	amount decimal.Decimal,
	alloc allocationContext,
) ([]*refund.Refund, error) {
	if amount.IsZero() || amount.IsNegative() {
		return nil, nil
	}

	payments, err := s.succeededPayments(ctx, inv.ID)
	if err != nil {
		return nil, err
	}

	settled := map[string]decimal.Decimal{}
	inFlight := map[string]decimal.Decimal{}
	if len(payments) > 0 {
		paymentIDs := lo.Map(payments, func(p *payment.Payment, _ int) string { return p.ID })

		settled, err = s.RefundRepo.SumSettledByPaymentIDs(ctx, inv.ID, paymentIDs)
		if err != nil {
			return nil, err
		}

		inFlight, err = s.RefundRepo.SumInFlightByPaymentIDs(ctx, inv.ID, paymentIDs)
		if err != nil {
			return nil, err
		}
	}

	rows := make([]*refund.Refund, 0, len(payments)+1)
	remainingAmountToRefund := amount

	for _, p := range payments {
		if !remainingAmountToRefund.IsPositive() {
			break
		}
		if p.Currency != inv.Currency {
			continue
		}

		remainingPaymentRefundCapacity := p.Amount.Sub(settled[p.ID]).Sub(inFlight[p.ID])
		if !remainingPaymentRefundCapacity.IsPositive() {
			continue
		}

		refundForThisPayment := decimal.Min(remainingPaymentRefundCapacity, remainingAmountToRefund)
		remainingAmountToRefund = remainingAmountToRefund.Sub(refundForThisPayment)

		row := s.newRow(ctx, inv, refundForThisPayment, alloc, len(rows))
		row.PaymentID = lo.ToPtr(p.ID)

		if alloc.allowGateway && isGatewayRefundable(p) {
			row.RefundDestination = types.RefundDestinationGateway
			row.PaymentGateway = p.PaymentGateway
			// The gateway's own payment id is read off the payment at dispatch time,
			// so it is never duplicated here and cannot go stale.
			row.GatewayIdempotencyToken = lo.ToPtr(row.IdempotencyKey)
		}

		rows = append(rows, row)
	}

	if remainingAmountToRefund.IsPositive() {
		rows = append(rows, s.newRow(ctx, inv, remainingAmountToRefund, alloc, len(rows)))
	}

	return rows, nil
}

func (s *refundService) succeededPayments(ctx context.Context, invoiceID string) ([]*payment.Payment, error) {
	filter := types.NewNoLimitPaymentFilter()
	filter.DestinationType = lo.ToPtr(string(types.PaymentDestinationTypeInvoice))
	filter.DestinationID = lo.ToPtr(invoiceID)
	filter.PaymentStatus = lo.ToPtr(string(types.PaymentStatusSucceeded))

	payments, err := s.PaymentRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Oldest first, so a partial refund draws down the earliest payment and the
	// allocation is reproducible regardless of repository ordering.
	sort.SliceStable(payments, func(i, j int) bool {
		if payments[i].CreatedAt.Equal(payments[j].CreatedAt) {
			return payments[i].ID < payments[j].ID
		}
		return payments[i].CreatedAt.Before(payments[j].CreatedAt)
	})

	return payments, nil
}

func (s *refundService) newRow(
	ctx context.Context,
	inv *invoice.Invoice,
	amount decimal.Decimal,
	alloc allocationContext,
	index int,
) *refund.Refund {
	return refund.NewRefundBuilder(nil).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_REFUND)).
		WithInvoiceID(inv.ID).
		WithCreditNoteID(alloc.creditNoteID).
		WithAmount(amount).
		WithSettledAmount(decimal.Zero).
		WithCurrency(inv.Currency).
		WithStatus(types.RefundStatusPending).
		WithRefundReason(alloc.reason).
		WithDestination(types.RefundDestinationWallet). // default to wallet, 100% success rate
		WithAttempt(1).
		WithIdempotencyKey(fmt.Sprintf("%s-%d", alloc.idempotencyKey, index)).
		WithEnvironmentID(types.GetEnvironmentID(ctx)).
		WithBaseModel(types.GetDefaultBaseModel(ctx)).
		Build()
}

func isGatewayRefundable(p *payment.Payment) bool {
	return lo.Contains(gatewayRefundableMethods, p.PaymentMethodType) &&
		p.PaymentGateway != nil &&
		p.GatewayPaymentID != nil &&
		*p.GatewayPaymentID != ""
}

func refundReasonFromCreditNote(reason types.CreditNoteReason) types.RefundReason {
	switch reason {
	case types.CreditNoteReasonDuplicate:
		return types.RefundReasonDuplicate
	case types.CreditNoteReasonFraudulent:
		return types.RefundReasonFraudulent
	case types.CreditNoteReasonOrderChange, types.CreditNoteReasonSubscriptionCancellation:
		return types.RefundReasonOrderChange
	case types.CreditNoteReasonUnsatisfactory, types.CreditNoteReasonService:
		return types.RefundReasonServiceIssue
	default:
		return types.RefundReasonOther
	}
}

func (s *refundService) Dispatch(ctx context.Context, refundID string) error {
	row, err := s.RefundRepo.Get(ctx, refundID)
	if err != nil {
		return err
	}
	if row.RefundStatus.IsTerminal() {
		return nil
	}

	switch row.RefundDestination {
	case types.RefundDestinationWallet:
		return s.settleToWallet(ctx, row)
	case types.RefundDestinationGateway:
		return s.dispatchToGateway(ctx, row)
	default:
		return nil
	}
}

func (s *refundService) settleToWallet(ctx context.Context, row *refund.Refund) error {
	walletService := NewWalletService(s.ServiceParams)

	return s.DB.WithTx(ctx, func(tx context.Context) error {
		// Refunds on one invoice settle one at a time, so a converted invoice's running total stays
		// consistent. The invoice is locked before the refund row, the same order void takes.
		inv, err := s.InvoiceRepo.GetForUpdate(tx, row.InvoiceID)
		if err != nil {
			return err
		}

		locked, err := s.RefundRepo.GetForUpdate(tx, row.ID)
		if err != nil {
			return err
		}
		if locked.RefundStatus.IsTerminal() {
			return nil
		}

		reason := types.TransactionReasonInvoiceVoidRefund
		metadata := types.Metadata{"refund_id": row.ID, "invoice_id": row.InvoiceID}
		if row.CreditNoteID != nil {
			reason = types.TransactionReasonCreditNote
			metadata["credit_note_id"] = *row.CreditNoteID
		}

		walletCurrency, credit := row.Currency, row.Amount
		// A converted invoice's refund credits the charge-currency wallet, at the frozen rate.
		if fx := inv.FxConversion; fx != nil && types.IsMatchingCurrency(row.Currency, fx.BillingCurrency) {
			walletCurrency = fx.ChargeCurrency
			credit, err = s.chargeCurrencyCreditFor(tx, inv, row)
			if err != nil {
				return err
			}
			metadata["fx_rate"] = fx.Rate.String()
			metadata["fx_charge_currency"] = fx.ChargeCurrency
			metadata["fx_billing_currency"] = fx.BillingCurrency
			metadata["fx_billing_amount"] = row.Amount.String()
		}

		// Worth less than the charge currency's smallest unit: nothing to credit.
		if !credit.IsPositive() {
			s.Logger.Info(tx, "refund settled without a wallet credit: converted amount rounds to zero",
				"refund_id", row.ID, "invoice_id", row.InvoiceID, "amount", row.Amount.String())
			return s.Settle(tx, &dto.SettleRefundRequest{RefundID: row.ID, SettledAmount: row.Amount})
		}

		w, err := walletService.EnsurePrepaidWallet(tx, inv.CustomerID, walletCurrency)
		if err != nil {
			return err
		}

		// Keyed on the refund row, not the credit note: one credit note can fan out
		// into several rows and they must each top up.
		topUp, err := walletService.TopUpWallet(tx, w.ID, &dto.TopUpWalletRequest{
			Amount:            credit,
			TransactionReason: reason,
			Metadata:          metadata,
			IdempotencyKey:    lo.ToPtr(row.ID),
			Description:       fmt.Sprintf("Refund for invoice %s", lo.FromPtrOr(inv.InvoiceNumber, inv.ID)),
		})
		if err != nil {
			return err
		}

		var walletTxnID *string
		if topUp != nil && topUp.WalletTransaction != nil {
			walletTxnID = lo.ToPtr(topUp.WalletTransaction.ID)
		}

		return s.Settle(tx, &dto.SettleRefundRequest{
			RefundID:      row.ID,
			SettledAmount: row.Amount,
			DestinationID: walletTxnID,
		})
	})
}

// chargeCurrencyCreditFor converts a converted invoice's wallet refund at the frozen rate, as a step in
// a running total, so everything the invoice returns to the wallet adds up to one rounding.
func (s *refundService) chargeCurrencyCreditFor(ctx context.Context, inv *invoice.Invoice, row *refund.Refund) (decimal.Decimal, error) {
	fx := inv.FxConversion
	prior, err := s.RefundRepo.SumSettledToWalletByInvoice(ctx, inv.ID, fx.BillingCurrency)
	if err != nil {
		return decimal.Zero, err
	}

	toCharge := func(billing decimal.Decimal) decimal.Decimal {
		return types.RoundToCurrencyPrecision(billing.Div(fx.Rate), fx.ChargeCurrency)
	}
	return toCharge(prior.Add(row.Amount)).Sub(toCharge(prior)), nil
}

func (s *refundService) dispatchToGateway(ctx context.Context, row *refund.Refund) error {
	provider, err := s.IntegrationFactory.GetRefundProvider(ctx, types.PaymentGatewayType(lo.FromPtr(row.PaymentGateway)))
	if err != nil {
		if ierr.IsNotImplemented(err) {
			s.Logger.Info(ctx, "gateway cannot refund, falling back to wallet",
				"refund_id", row.ID,
				"gateway", lo.FromPtr(row.PaymentGateway))
			return s.Fail(ctx, row.ID, "gateway does not support refunds")
		}
		return err
	}

	gatewayPaymentID, err := s.gatewayPaymentIDFor(ctx, row)
	if err != nil {
		return err
	}

	// Claim the row before the call so a concurrent dispatch cannot issue a second
	// gateway refund for the same money.
	claimed, err := s.claimForGateway(ctx, row.ID)
	if err != nil || !claimed {
		return err
	}

	resp, err := provider.RefundPayment(ctx, interfaces.RefundProviderRequest{
		GatewayPaymentID: gatewayPaymentID,
		Amount:           row.Amount,
		Currency:         row.Currency,
		IdempotencyKey:   lo.FromPtrOr(row.GatewayIdempotencyToken, row.IdempotencyKey),
	})
	if err != nil {
		s.Logger.Error(ctx, "gateway refund call failed",
			"error", err,
			"refund_id", row.ID,
			"gateway", lo.FromPtr(row.PaymentGateway))
		return s.Fail(ctx, row.ID, err.Error())
	}

	switch resp.Status {
	case types.RefundStatusSucceeded:
		return s.Settle(ctx, &dto.SettleRefundRequest{
			RefundID:        row.ID,
			SettledAmount:   resp.SettledAmount,
			DestinationID:   lo.ToPtr(resp.GatewayRefundID),
			GatewayMetadata: resp.Metadata,
		})
	case types.RefundStatusFailed:
		return s.Fail(ctx, row.ID, "gateway rejected the refund")
	default:
		return s.recordGatewayAcceptance(ctx, row.ID, resp)
	}
}

// gatewayPaymentIDFor reads the provider's payment identifier off the payment the
// refund draws from, rather than copying it onto the refund row at plan time.
func (s *refundService) gatewayPaymentIDFor(ctx context.Context, row *refund.Refund) (string, error) {
	if row.PaymentID == nil {
		return "", ierr.NewError("gateway refund has no payment to refund against").
			WithHint("A gateway refund must be linked to a payment.").
			WithReportableDetails(map[string]any{"refund_id": row.ID}).
			Mark(ierr.ErrValidation)
	}

	p, err := s.PaymentRepo.Get(ctx, *row.PaymentID)
	if err != nil {
		return "", err
	}
	if p.GatewayPaymentID == nil || *p.GatewayPaymentID == "" {
		return "", ierr.NewError("payment has no gateway payment id").
			WithHint("The payment this refund draws from was never recorded against a gateway.").
			WithReportableDetails(map[string]any{"refund_id": row.ID, "payment_id": p.ID}).
			Mark(ierr.ErrValidation)
	}

	return *p.GatewayPaymentID, nil
}

func (s *refundService) claimForGateway(ctx context.Context, refundID string) (bool, error) {
	var claimed *refund.Refund
	err := s.DB.WithTx(ctx, func(tx context.Context) error {
		locked, err := s.RefundRepo.GetForUpdate(tx, refundID)
		if err != nil {
			return err
		}
		if err := locked.RefundStatus.ValidateTransitionTo(types.RefundStatusProcessing); err != nil {
			return nil
		}

		now := time.Now().UTC()
		updated := refund.NewRefundBuilder(locked).
			WithStatus(types.RefundStatusProcessing).
			WithInitiatedAt(&now).
			Build()
		if err := s.RefundRepo.Update(tx, updated); err != nil {
			return err
		}
		claimed = updated
		return nil
	})
	if err != nil || claimed == nil {
		return false, err
	}
	recordRefundTransition(ctx, claimed)
	return true, nil
}

func (s *refundService) recordGatewayAcceptance(ctx context.Context, refundID string, resp *interfaces.RefundProviderResponse) error {
	return s.DB.WithTx(ctx, func(tx context.Context) error {
		locked, err := s.RefundRepo.GetForUpdate(tx, refundID)
		if err != nil {
			return err
		}
		if locked.RefundStatus.IsTerminal() {
			return nil
		}

		updated := refund.NewRefundBuilder(locked).
			WithGatewayRefundID(lo.ToPtr(resp.GatewayRefundID)).
			WithDestinationID(lo.ToPtr(resp.GatewayRefundID)).
			WithGatewayMetadata(resp.Metadata).
			Build()
		return s.RefundRepo.Update(tx, updated)
	})
}

func (s *refundService) Settle(ctx context.Context, req *dto.SettleRefundRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}

	var settled *refund.Refund
	err := s.DB.WithTx(ctx, func(tx context.Context) error {
		row, err := s.RefundRepo.GetForUpdate(tx, req.RefundID)
		if err != nil {
			return err
		}

		// A redelivered webhook lands here; the transition guard is what makes
		// settlement happen at most once.
		if err := row.RefundStatus.ValidateTransitionTo(types.RefundStatusSucceeded); err != nil {
			s.Logger.Info(ctx, "refund settlement ignored",
				"refund_id", row.ID,
				"current_status", row.RefundStatus)
			return nil
		}

		// The gateway reports the settled amount; it can never exceed what was planned.
		if req.SettledAmount.GreaterThan(row.Amount) {
			return ierr.NewError("settled amount exceeds the refund amount").
				WithHint("A refund cannot settle for more than it was planned for.").
				WithReportableDetails(map[string]any{
					"refund_id":      row.ID,
					"refund_amount":  row.Amount,
					"settled_amount": req.SettledAmount,
				}).
				Mark(ierr.ErrValidation)
		}

		now := time.Now().UTC()
		builder := refund.NewRefundBuilder(row).
			WithStatus(types.RefundStatusSucceeded).
			WithSettledAmount(req.SettledAmount).
			WithSucceededAt(&now)
		if req.DestinationID != nil && *req.DestinationID != "" {
			builder = builder.WithDestinationID(req.DestinationID)
			if row.RefundDestination == types.RefundDestinationGateway {
				builder = builder.WithGatewayRefundID(req.DestinationID)
			}
		}
		if req.GatewayMetadata != nil {
			builder = builder.WithGatewayMetadata(req.GatewayMetadata)
		}

		settled = builder.Build()
		return s.RefundRepo.Update(tx, settled)
	})
	if err != nil || settled == nil {
		return err
	}
	recordRefundTransition(ctx, settled)

	s.publishSystemEvent(ctx, types.WebhookEventRefundSucceeded, req.RefundID)
	return nil
}

func (s *refundService) Fail(ctx context.Context, refundID, reason string) error {
	var failed *refund.Refund

	err := s.DB.WithTx(ctx, func(tx context.Context) error {
		row, err := s.RefundRepo.GetForUpdate(tx, refundID)
		if err != nil {
			return err
		}
		if err := row.RefundStatus.ValidateTransitionTo(types.RefundStatusFailed); err != nil {
			s.Logger.Info(ctx, "refund failure ignored",
				"refund_id", row.ID,
				"current_status", row.RefundStatus)
			return nil
		}

		now := time.Now().UTC()
		failed = refund.NewRefundBuilder(row).
			WithStatus(types.RefundStatusFailed).
			WithFailureReason(lo.ToPtr(reason)).
			WithFailedAt(&now).
			Build()
		return s.RefundRepo.Update(tx, failed)
	})
	if err != nil || failed == nil {
		return err
	}
	recordRefundTransition(ctx, failed)

	s.publishSystemEvent(ctx, types.WebhookEventRefundFailed, failed.ID)

	if failed.RefundDestination != types.RefundDestinationGateway {
		return nil
	}

	fallback, err := s.refundToWalletAsFallbackToFailure(ctx, failed.ID)
	if err != nil {
		return err
	}
	return s.Dispatch(ctx, fallback.ID)
}

// refundToWalletAsFallbackToFailure creates the one wallet row that replaces a failed
// gateway refund. It is keyed on the failed row's metadata so a retry never mints a second one.
func (s *refundService) refundToWalletAsFallbackToFailure(ctx context.Context, failedID string) (*refund.Refund, error) {
	var fallback *refund.Refund
	var created bool

	err := s.DB.WithTx(ctx, func(tx context.Context) error {
		row, err := s.RefundRepo.GetForUpdate(tx, failedID)
		if err != nil {
			return err
		}

		if existingID, ok := row.Metadata["fallback_refund_id"]; ok && existingID != "" {
			fallback, err = s.RefundRepo.Get(tx, existingID)
			return err
		}

		fallback = refund.NewRefundBuilder(row).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_REFUND)).
			WithStatus(types.RefundStatusPending).
			WithDestination(types.RefundDestinationWallet).
			WithDestinationID(nil).
			WithPaymentGateway(nil).
			WithGatewayRefundID(nil).
			WithGatewayTrackingID(nil).
			WithGatewayIdempotencyToken(nil).
			WithGatewayMetadata(nil).
			WithFailureReason(nil).
			WithSettledAmount(decimal.Zero).
			WithAttempt(row.Attempt + 1).
			WithIdempotencyKey(fmt.Sprintf("%s-fb-%d", row.IdempotencyKey, row.Attempt+1)).
			WithMetadata(types.Metadata{"retry_of": row.ID}).
			WithInitiatedAt(nil).
			WithSucceededAt(nil).
			WithFailedAt(nil).
			WithCancelledAt(nil).
			WithBaseModel(types.GetDefaultBaseModel(tx)).
			Build()

		if err := s.RefundRepo.Create(tx, fallback); err != nil {
			return err
		}
		created = true
		s.publishSystemEvent(tx, types.WebhookEventRefundCreated, fallback.ID)

		metadata := types.Metadata{}
		for k, v := range row.Metadata {
			metadata[k] = v
		}
		metadata["fallback_refund_id"] = fallback.ID

		return s.RefundRepo.Update(tx, refund.NewRefundBuilder(row).WithMetadata(metadata).Build())
	})
	if err != nil {
		return nil, err
	}
	if created {
		recordRefundTransition(ctx, fallback)
	}
	return fallback, nil
}

func recordRefundTransition(ctx context.Context, r *refund.Refund) {
	metrics.RecordCounter(ctx, metrics.RefundTransitions, 1,
		metrics.L(metrics.KeyProvider, lo.FromPtrOr(r.PaymentGateway, "none")),
		metrics.L(metrics.KeyDestination, string(r.RefundDestination)),
		metrics.L(metrics.KeyReason, string(r.RefundReason)),
		metrics.L(metrics.KeyStatus, string(r.RefundStatus)),
	)
}

func (s *refundService) GetRefund(ctx context.Context, id string) (*dto.RefundResponse, error) {
	row, err := s.RefundRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return dto.NewRefundResponse(row), nil
}

func (s *refundService) GetRefundByGatewayRefundID(ctx context.Context, gateway, gatewayRefundID string) (*dto.RefundResponse, error) {
	row, err := s.RefundRepo.GetByGatewayRefundID(ctx, gateway, gatewayRefundID)
	if err != nil {
		return nil, err
	}
	return dto.NewRefundResponse(row), nil
}

func (s *refundService) publishSystemEvent(ctx context.Context, eventName types.WebhookEventName, refundID string) {
	payload, err := json.Marshal(webhookDto.InternalRefundEvent{
		RefundID: refundID,
		TenantID: types.GetTenantID(ctx),
	})
	if err != nil {
		s.Logger.Error(ctx, "failed to marshal refund webhook payload", "error", err, "refund_id", refundID)
		return
	}

	event := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SYSTEM_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(payload),
		EntityType:    types.SystemEntityTypeRefund,
		EntityID:      refundID,
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, event); err != nil {
		s.Logger.Error(ctx, "failed to publish refund webhook event",
			"error", err,
			"event_name", eventName,
			"refund_id", refundID)
	}
}

func (s *refundService) ListRefunds(ctx context.Context, filter *types.RefundFilter) (*dto.ListRefundsResponse, error) {
	if filter == nil {
		filter = types.NewRefundFilter()
	}
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}
	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	rows, err := s.RefundRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	total, err := s.RefundRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	items := lo.Map(rows, func(r *refund.Refund, _ int) *dto.RefundResponse {
		return dto.NewRefundResponse(r)
	})
	resp := types.NewListResponse(items, total, filter.GetLimit(), filter.GetOffset())
	return &resp, nil
}

func (s *refundService) RetryRefund(ctx context.Context, id string) (*dto.RefundResponse, error) {
	row, err := s.RefundRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	switch row.RefundStatus {
	case types.RefundStatusPending:
		if err := s.Dispatch(ctx, row.ID); err != nil {
			return nil, err
		}
		return s.GetRefund(ctx, row.ID)

	case types.RefundStatusFailed:
		fallback, err := s.refundToWalletAsFallbackToFailure(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		if err := s.Dispatch(ctx, fallback.ID); err != nil {
			return nil, err
		}
		return s.GetRefund(ctx, fallback.ID)

	default:
		return nil, ierr.NewError("refund cannot be retried").
			WithHintf("A refund in status %s cannot be retried.", row.RefundStatus).
			WithReportableDetails(map[string]any{
				"refund_id":     row.ID,
				"refund_status": row.RefundStatus,
			}).
			Mark(ierr.ErrValidation)
	}
}
