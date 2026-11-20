package scenarios

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// perUnit is what every usage price in the plan charges per unit.
var perUnit = decimal.RequireFromString("0.01")

// commitmentPreview alternates legs on clones of the committed persona: under
// the commitment a true-up must bill usage up to it, over it the excess must
// carry the overage factor.
type commitmentPreview struct {
	under usageBatch
	over  usageBatch
	poll  pollWindow
	runs  atomic.Int64
}

func newCommitmentPreview(scenario.Deps) *commitmentPreview {
	return &commitmentPreview{
		under: usageBatch{meter: "sum_commit", events: 100, value: "1"},
		over:  usageBatch{meter: "sum_commit", events: 700, value: "1"},
		poll:  pollWindow{every: 5 * time.Second, within: 8 * time.Minute},
	}
}

func (*commitmentPreview) Name() string        { return "commitment-preview" }
func (*commitmentPreview) Kind() e2eprobe.Kind { return e2eprobe.KindFlow }
func (*commitmentPreview) Requires() []e2eprobe.Trait {
	return []e2eprobe.Trait{e2eprobe.TraitCommitment}
}

func (s *commitmentPreview) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	p := sc.Persona()
	spec := p.Spec().Commitment
	if spec == nil {
		return sc.Skip("spec", "persona declares no commitment")
	}
	commitment, err := decimal.NewFromString(spec.Amount)
	if err != nil {
		return sc.Fail("spec", fmt.Errorf("commitment amount %q: %w", spec.Amount, err))
	}
	factor, err := decimal.NewFromString(spec.OverageFactor)
	if err != nil {
		return sc.Fail("spec", fmt.Errorf("overage factor %q: %w", spec.OverageFactor, err))
	}
	batch, leg := s.under, "under"
	if s.runs.Add(1)%2 == 0 {
		batch, leg = s.over, "over"
	}
	sc.Attr("leg", leg)

	eph, err := sc.Clone(ctx)
	if err != nil {
		return sc.Fail("clone", err)
	}
	noteClone(sc, eph)
	events, err := batch.build(sc, eph.ExternalID())
	if err != nil {
		return sc.Fail("batch", err)
	}
	if err := sc.Client().Events().IngestBulk(ctx, types.BulkIngestEventRequest{Events: events}); err != nil {
		return sc.Fail("ingest_bulk", err)
	}
	// Usage is read back before the preview; a preview computed early would bill too little.
	raw := perUnit.Mul(decimal.NewFromInt(int64(batch.events)))
	seen := decimal.Zero
	err = s.poll.until(ctx, func() error {
		resp, err := sc.Client().Subscriptions().GetUsage(ctx, types.GetUsageBySubscriptionRequest{SubscriptionID: eph.SubscriptionID()})
		if err != nil {
			return err
		}
		if resp.GetUsageBySubscriptionResponse == nil || resp.GetUsageBySubscriptionResponse.Amount == nil {
			return errors.New("usage read has no amount")
		}
		seen = decimal.NewFromFloat(*resp.GetUsageBySubscriptionResponse.Amount)
		if seen.Round(2).LessThan(raw) {
			return fmt.Errorf("usage %s has not reached %s", seen.StringFixed(2), raw.StringFixed(2))
		}
		return nil
	})
	if err != nil {
		sc.Attr("usage_amount", seen.StringFixed(2))
		return sc.Fail("poll_usage", err)
	}
	inv, err := previewOf(ctx, sc, eph.SubscriptionID())
	if err != nil {
		return sc.Fail("preview", err)
	}
	assertCommitment(sc, inv, commitment, factor, raw)
	return sc.Result("assert_commitment_preview")
}

// assertCommitment checks what the preview bills beyond the fixed fees: the
// commitment itself under it (with a true-up line), commitment plus factored
// excess over it.
func assertCommitment(sc *scenario.Context, inv *types.InvoiceResponse, commitment, factor, raw decimal.Decimal) {
	total := decimalOf(inv.Total)
	if inv.Total == nil {
		total = decimalOf(inv.AmountDue)
	}
	fixed, trueUp := decimal.Zero, false
	for _, li := range inv.LineItems {
		if isTrueUpLine(li) {
			trueUp = true
			continue
		}
		if li.PriceType != nil && *li.PriceType == string(types.PriceTypeFixed) {
			fixed = fixed.Add(decimalOf(li.Amount))
		}
	}
	billed := total.Sub(fixed)
	if raw.GreaterThan(commitment) {
		sc.Money("usage billed with overage", commitment.Add(raw.Sub(commitment).Mul(factor)), billed)
		return
	}
	sc.Money("usage billed to commitment", commitment, billed)
	if raw.LessThan(commitment) {
		sc.Truth("preview carries a commitment true-up line", trueUp)
	}
}
