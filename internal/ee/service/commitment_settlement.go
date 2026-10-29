package service

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const commitmentSettlementKey = "flexprice.billing_terms.settlement"

type commitmentSettlement struct {
	Policy          string                    `json:"policy"`
	Currency        string                    `json:"currency"`
	Target          decimal.Decimal           `json:"target_amount"`
	Accrued         decimal.Decimal           `json:"accrued_amount"`
	Retained        decimal.Decimal           `json:"retained_principal"`
	EffectiveAt     time.Time                 `json:"effective_at"`
	Delivery        types.BillingTermDelivery `json:"invoice_delivery"`
	FeeInvoiceID    *string                   `json:"fee_invoice_id,omitempty"`
	CancellationFee *decimal.Decimal          `json:"cancellation_fee,omitempty"`
}

func commitmentUsesTerms(c *commitment.Commitment) bool {
	return c != nil && c.Binding && c.CollectionMethod != nil && *c.CollectionMethod == types.CommitmentCollectionBillingTerms
}

func readCommitmentSettlement(c *commitment.Commitment) (*commitmentSettlement, error) {
	raw, exists := c.Metadata[commitmentSettlementKey]
	if !exists {
		return nil, nil
	}
	if len(raw) == 0 || len(raw) > 4096 {
		return nil, ierr.Validation("invalid protected commitment settlement")
	}
	var decision commitmentSettlement
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decision); err != nil {
		return nil, ierr.Validation("invalid protected commitment settlement")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, ierr.Validation("invalid protected commitment settlement")
	}
	if decision.Policy != "accrued_or_assigned" || !strings.EqualFold(decision.Currency, c.Currency) || !commitmentTermsDate(decision.EffectiveAt) || decision.Delivery.Validate() != nil {
		return nil, ierr.Validation("invalid protected commitment settlement policy")
	}
	for _, amount := range []decimal.Decimal{decision.Target, decision.Accrued, decision.Retained} {
		if amount.Exponent() < -8 || amount.Exponent() > 12 || amount.IsNegative() || (amount.IsPositive() && !validCommitmentTermsTotal(&amount, c.Currency)) {
			return nil, ierr.Validation("invalid protected commitment settlement amount")
		}
	}
	if !decision.Target.Equal(decimal.Max(decision.Accrued, decision.Retained)) || c.TotalAmount == nil || decision.Target.GreaterThan(*c.TotalAmount) {
		return nil, ierr.Validation("protected commitment settlement does not match its economics")
	}
	if c.TerminatedAt != nil {
		if c.CommitmentStatus != types.CommitmentStatusTerminated || !c.TerminatedAt.Equal(decision.EffectiveAt) {
			return nil, ierr.Validation("protected settlement termination date is inconsistent")
		}
	} else if c.CommitmentStatus != types.CommitmentStatusEnded || !c.EndDate.Equal(decision.EffectiveAt) {
		return nil, ierr.Validation("protected settlement requires a terminal commitment")
	}
	if decision.FeeInvoiceID != nil && (strings.TrimSpace(*decision.FeeInvoiceID) == "" || len(*decision.FeeInvoiceID) > 50) {
		return nil, ierr.Validation("invalid settlement fee invoice")
	}
	if decision.CancellationFee != nil && (decision.CancellationFee.IsNegative() || (decision.CancellationFee.IsPositive() && !validCommitmentTermsTotal(decision.CancellationFee, c.Currency))) {
		return nil, ierr.Validation("invalid settlement cancellation fee")
	}
	return &decision, nil
}

func settlementMetadata(c *commitment.Commitment, decision commitmentSettlement) (types.Metadata, error) {
	encoded, err := json.Marshal(decision)
	if err != nil || len(encoded) > 4096 {
		return nil, ierr.Validation("invalid commitment settlement decision")
	}
	metadata := lo.Assign(types.Metadata{}, c.Metadata)
	metadata[commitmentSettlementKey] = string(encoded)
	return metadata, nil
}

func (s *commitmentService) accruedTermsPrincipal(ctx context.Context, c *commitment.Commitment, effectiveAt time.Time) (decimal.Decimal, error) {
	if !effectiveAt.After(c.StartDate) {
		return decimal.Zero, nil
	}
	if !effectiveAt.Before(c.EndDate) {
		return *c.TotalAmount, nil
	}
	cc, err := s.loadCommitmentContext(ctx, c.SubscriptionID)
	if err != nil {
		return decimal.Zero, err
	}
	amountOf, err := s.committedAmountFn(ctx, c)
	if err != nil {
		return decimal.Zero, err
	}
	periods, err := commitmentPeriods(c, cc.Sub, amountOf, bucketFor(c, cc))
	if err != nil {
		return decimal.Zero, err
	}
	amount := decimal.Zero
	for _, period := range periods {
		if !period.Start.Before(effectiveAt) {
			continue
		}
		amount = amount.Add(clipCommitmentPeriod(c, period, c.StartDate, effectiveAt, amountOf, bucketFor(c, cc), cc.Sub.BillingAnchor).Amount)
	}
	return decimal.Min(*c.TotalAmount, types.RoundToCurrencyPrecision(amount, c.Currency)), nil
}

func (s *commitmentService) settleCommitment(ctx context.Context, id string, effectiveAt time.Time, fee *decimal.Decimal, cancellation bool) (*commitment.Commitment, *string, error) {
	if s.BillingTerms == nil {
		return nil, nil, ierr.NewError("billing terms are unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	var out *commitment.Commitment
	var invoiceID *string
	_, err := s.BillingTerms.WithSettlement(ctx, core.SettlementRequest{Owner: commitmentBillingOwner(id), BillOn: effectiveAt}, func(tx context.Context, current core.State) (bool, error) {
		c, err := s.CommitmentRepo.Get(tx, id)
		if err != nil {
			return false, err
		}
		previous, err := readCommitmentSettlement(c)
		if err != nil {
			return false, err
		}
		if previous != nil {
			out, invoiceID = c, previous.FeeInvoiceID
			if !cancellation && (!effectiveAt.UTC().Truncate(time.Microsecond).Equal(previous.EffectiveAt) || (fee != nil && (previous.CancellationFee == nil || !types.RoundToCurrencyPrecision(*fee, c.Currency).Equal(*previous.CancellationFee)))) {
				return false, ierr.NewError("commitment already has a different settlement decision").Mark(ierr.ErrVersionConflict)
			}
			if cancellation && !effectiveAt.Before(previous.EffectiveAt) && previous.Delivery != types.BillingTermSeparateInvoice {
				decision := *previous
				decision.Delivery = types.BillingTermSeparateInvoice
				metadata, err := settlementMetadata(c, decision)
				if err != nil {
					return false, err
				}
				out, err = s.CommitmentRepo.Update(tx, commitment.NewCommitmentBuilder(c).WithMetadata(metadata).Build())
				return err == nil, err
			}
			if !cancellation || !effectiveAt.Before(previous.EffectiveAt) {
				return false, nil
			}
		}
		if c.CommitmentStatus != types.CommitmentStatusActive && c.CommitmentStatus != types.CommitmentStatusTerminated && !(cancellation && c.CommitmentStatus == types.CommitmentStatusEnded) {
			return false, ierr.Validation("commitment cannot be settled in its current status")
		}
		if !cancellation && !effectiveAt.Before(c.EndDate) {
			return false, ierr.Validation("effective_at must be before the commitment end")
		}
		decisionAt := effectiveAt.UTC().Truncate(time.Microsecond)
		if !decisionAt.Before(c.EndDate) {
			decisionAt = c.EndDate
		}
		accrued, err := s.accruedTermsPrincipal(tx, c, decisionAt)
		if err != nil {
			return false, err
		}
		decision := commitmentSettlement{Policy: "accrued_or_assigned", Currency: strings.ToLower(c.Currency), EffectiveAt: decisionAt, Accrued: accrued, Retained: current.Summary().AssignedAmount, Delivery: current.Owner().Delivery}
		decision.Target = decimal.Max(decision.Accrued, decision.Retained)
		if cancellation {
			decision.Delivery = types.BillingTermSeparateInvoice
		}
		if !cancellation {
			adapter := &commitmentTermsOwner{subscriptions: s.SubRepo}
			sub, err := adapter.subscription(tx, c.SubscriptionID)
			if err != nil {
				return false, err
			}
			if sub.SubscriptionType == types.SubscriptionTypeGroupedInvoicing {
				sub, err = adapter.subscription(tx, lo.FromPtr(sub.ParentSubscriptionID))
				if err != nil {
					return false, err
				}
			}
			if sub.SubscriptionStatus == types.SubscriptionStatusCancelled || sub.CancelledAt != nil || (sub.EndDate != nil && !sub.EndDate.After(effectiveAt)) {
				decision.Delivery = types.BillingTermSeparateInvoice
			}
		}
		if previous != nil {
			decision.FeeInvoiceID, decision.CancellationFee = previous.FeeInvoiceID, previous.CancellationFee
		}
		if previous == nil && fee != nil {
			invoiceID, err = s.billCancellationFee(tx, c, effectiveAt, *fee)
			if err != nil {
				return false, err
			}
			decision.FeeInvoiceID, decision.CancellationFee = invoiceID, lo.ToPtr(types.RoundToCurrencyPrecision(*fee, c.Currency))
		}
		metadata, err := settlementMetadata(c, decision)
		if err != nil {
			return false, err
		}
		builder := commitment.NewCommitmentBuilder(c).WithMetadata(metadata)
		if decisionAt.Before(c.EndDate) {
			builder.WithStatus(types.CommitmentStatusTerminated).WithTerminatedAt(decisionAt)
		} else {
			builder.WithStatus(types.CommitmentStatusEnded)
		}
		out, err = s.CommitmentRepo.Update(tx, builder.Build())
		return err == nil, err
	})
	return out, invoiceID, err
}
