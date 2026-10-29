package service

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
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
