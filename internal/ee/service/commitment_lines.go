package service

import (
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// scopedLine is an in-scope usage line item and the totals the normal path would have billed.
type scopedLine struct {
	Item     *subscription.SubscriptionLineItem
	Price    *price.Price
	Quantity decimal.Decimal
	Cost     decimal.Decimal
}

// onDemandTotal is a line's on-demand usage and its price under one commitment's overage rate.
type onDemandTotal struct {
	Line     *scopedLine
	Owner    *stackedCommitment
	Quantity decimal.Decimal
	Amount   decimal.Decimal
}

type commitmentLineInput struct {
	Stack       []*stackedCommitment
	Lines       map[string]*scopedLine
	Alloc       *allocationResult
	PeriodByCmt map[string]commitmentPeriod
	PeriodStart time.Time
	PeriodEnd   time.Time
	Currency    string
	PriceCost   func(p *price.Price, qty decimal.Decimal) decimal.Decimal
}

// buildCommitmentLines turns an allocation into within, true-up and on-demand invoice lines (per_period only).
func buildCommitmentLines(in commitmentLineInput) ([]dto.CreateInvoiceLineItemRequest, error) {
	var out []dto.CreateInvoiceLineItemRequest
	allocatedByLine := map[string]decimal.Decimal{}
	for _, sc := range in.Stack {
		alloc := in.Alloc.ByCommitment[sc.C.ID]
		if alloc == nil {
			alloc = &commitmentAllocation{UsedByLine: map[string]decimal.Decimal{}}
		}
		withinValue := decimal.Zero
		for _, lineID := range sc.LineIDs {
			used := alloc.UsedByLine[lineID]
			if !used.IsPositive() {
				continue
			}
			sl, err := lookupScopedLine(in.Lines, lineID)
			if err != nil {
				return nil, err
			}
			allocatedByLine[lineID] = allocatedByLine[lineID].Add(used)
			qty, amount := used, used.Mul(sc.Rates.Committed)
			if sc.C.Type == types.COMMITMENT_TYPE_AMOUNT {
				qty = proportionalQuantity(sl, used)
				amount = used
			}
			withinValue = withinValue.Add(amount)
			out = append(out, commitmentLine(in, sc, sl, types.CommitmentLineWithin, sc.C.InvoiceLabel, qty, amount))
		}
		if !sc.C.Binding {
			continue
		}
		trueUp := alloc.Unused.Mul(sc.Rates.Committed)
		trueUpQty := alloc.Unused
		if sc.C.Type == types.COMMITMENT_TYPE_AMOUNT {
			trueUp = decimal.Max(in.PeriodByCmt[sc.C.ID].Amount.Sub(withinValue), decimal.Zero)
			trueUpQty = decimal.NewFromInt(1)
		}
		if trueUp.IsPositive() {
			line := commitmentLine(in, sc, nil, types.CommitmentLineTrueUp, sc.C.InvoiceLabel+" (unused)", trueUpQty, trueUp)
			line.PriceType = lo.ToPtr(string(types.PRICE_TYPE_FIXED))
			line.PriceID = sc.C.CommittedPriceID
			out = append(out, line)
		}
	}

	// On-demand is priced window by window, so bucketed tiers match the normal path, at the overage
	// of the commitment in force in that window.
	var onDemand []*onDemandTotal
	totalByKey := map[string]*onDemandTotal{}
	for _, od := range in.Alloc.OnDemand {
		sl, err := lookupScopedLine(in.Lines, od.LineID)
		if err != nil {
			return nil, err
		}
		allocatedByLine[od.LineID] = allocatedByLine[od.LineID].Add(od.Quantity)
		key := od.LineID + "/" + od.Owner.C.ID
		total, ok := totalByKey[key]
		if !ok {
			total = &onDemandTotal{Line: sl, Owner: od.Owner}
			totalByKey[key] = total
			onDemand = append(onDemand, total)
		}
		amount := od.Quantity
		if od.Owner.C.Type == types.COMMITMENT_TYPE_QUANTITY {
			amount = in.PriceCost(sl.Price, od.Quantity)
			if od.Owner.Rates.Overage != nil {
				amount = od.Quantity.Mul(*od.Owner.Rates.Overage)
			}
		}
		total.Quantity = total.Quantity.Add(od.Quantity)
		total.Amount = total.Amount.Add(amount)
	}
	sort.SliceStable(onDemand, func(i, j int) bool { return onDemand[i].Line.Item.ID < onDemand[j].Line.Item.ID })
	for _, total := range onDemand {
		showQty := total.Quantity
		if total.Owner.C.Type == types.COMMITMENT_TYPE_AMOUNT {
			showQty = proportionalQuantity(total.Line, total.Quantity)
		}
		out = append(out, commitmentLine(in, total.Owner, total.Line, types.CommitmentLineOnDemand, lineDisplayName(total.Line), showQty, total.Amount))
	}

	lineIDs := lo.Keys(in.Lines)
	sort.Strings(lineIDs)
	for _, lineID := range lineIDs {
		sl := in.Lines[lineID]
		owner := ownerOf(in.Stack, lineID)
		expected := sl.Quantity
		if owner.C.Type == types.COMMITMENT_TYPE_AMOUNT {
			expected = sl.Cost
		}
		if !allocatedByLine[lineID].Equal(expected) {
			return nil, ierr.NewErrorf("commitment allocation mismatch for line item %s", lineID).
				WithHint("within plus on-demand must equal the line's usage").
				WithReportableDetails(map[string]interface{}{"allocated": allocatedByLine[lineID].String(), "expected": expected.String()}).
				Mark(ierr.ErrInternal)
		}
		// Free usage takes no amount capacity, but its quantity still shows on the invoice.
		if owner.C.Type == types.COMMITMENT_TYPE_AMOUNT && sl.Cost.IsZero() && sl.Quantity.IsPositive() {
			out = append(out, commitmentLine(in, owner, sl, types.CommitmentLineOnDemand, lineDisplayName(sl), sl.Quantity, decimal.Zero))
		}
	}
	return out, nil
}

func lookupScopedLine(lines map[string]*scopedLine, lineID string) (*scopedLine, error) {
	sl, ok := lines[lineID]
	if !ok || sl == nil {
		return nil, ierr.NewErrorf("commitment allocation references unknown line item %s", lineID).
			Mark(ierr.ErrInternal)
	}
	return sl, nil
}

func commitmentLine(in commitmentLineInput, sc *stackedCommitment, sl *scopedLine, kind types.CommitmentLineKind, name string, qty, amount decimal.Decimal) dto.CreateInvoiceLineItemRequest {
	line := dto.CreateInvoiceLineItemRequest{
		DisplayName:  lo.ToPtr(name),
		Quantity:     qty,
		Amount:       types.RoundToCurrencyPrecision(amount, in.Currency),
		PeriodStart:  lo.ToPtr(in.PeriodStart),
		PeriodEnd:    lo.ToPtr(in.PeriodEnd),
		CommitmentID: lo.ToPtr(sc.C.ID),
		Metadata: types.Metadata{
			types.MetadataKeyCommitmentLineKind: string(kind),
			types.MetadataKeyCommitmentID:       sc.C.ID,
		},
	}
	if sc.C.InvoiceDescription != nil {
		line.Metadata[types.MetadataKeyDescription] = *sc.C.InvoiceDescription
	}
	if sl != nil {
		line.PriceID = lo.ToPtr(sl.Item.PriceID)
		line.PriceType = lo.ToPtr(string(sl.Item.PriceType))
		line.MeterID = lo.ToPtr(sl.Item.MeterID)
		line.MeterDisplayName = lo.ToPtr(sl.Item.MeterDisplayName)
		line.EntityID = lo.ToPtr(sl.Item.EntityID)
		line.EntityType = lo.ToPtr(string(sl.Item.EntityType))
		line.PlanDisplayName = lo.ToPtr(sl.Item.PlanDisplayName)
		line.SubscriptionLineItemID = lo.ToPtr(sl.Item.ID)
	} else {
		line.EntityType = lo.ToPtr(string(types.InvoiceLineItemEntityTypeCommitment))
		line.EntityID = lo.ToPtr(sc.C.ID)
	}
	return line
}

// ownerOf returns the last stacked commitment scoping the line, falling back to the first.
func ownerOf(stack []*stackedCommitment, lineID string) *stackedCommitment {
	for i := len(stack) - 1; i >= 0; i-- {
		if lo.Contains(stack[i].LineIDs, lineID) {
			return stack[i]
		}
	}
	return stack[0]
}

// proportionalQuantity converts an allocated money value into units at the line's average rate.
func proportionalQuantity(sl *scopedLine, value decimal.Decimal) decimal.Decimal {
	if sl.Cost.IsZero() {
		return decimal.Zero
	}
	return sl.Quantity.Mul(value).Div(sl.Cost)
}

func lineDisplayName(sl *scopedLine) string {
	if sl.Item.DisplayName != "" {
		return sl.Item.DisplayName
	}
	return sl.Price.DisplayName
}
