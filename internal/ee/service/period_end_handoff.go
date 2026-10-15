package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

// periodEndHandoff decides which line item bills a meter that is billed once per
// window, when the window holds several line items for it back to back (plan change,
// price update). The last one bills the whole window; the ones it replaced bill nothing.
type periodEndHandoff struct {
	closingStart map[string]time.Time
	superseded   map[string]struct{}
}

// newPeriodEndHandoff builds the handoff for [windowStart, windowEnd). billsOnce
// reports whether a usage line item's meter is billed once per window. A meter whose
// line items overlap in time is left alone: they are concurrent charges, not a replacement.
func newPeriodEndHandoff(
	items []*subscription.SubscriptionLineItem,
	billsOnce func(*subscription.SubscriptionLineItem) bool,
	windowStart, windowEnd time.Time,
) *periodEndHandoff {
	h := &periodEndHandoff{
		closingStart: make(map[string]time.Time),
		superseded:   make(map[string]struct{}),
	}

	byMeter := make(map[string][]*subscription.SubscriptionLineItem)
	for _, item := range items {
		if item == nil || item.PriceType != types.PRICE_TYPE_USAGE || item.MeterID == "" || !billsOnce(item) {
			continue
		}
		if !item.GetPeriodEnd(windowEnd).After(item.GetPeriodStart(windowStart)) {
			continue
		}
		byMeter[item.MeterID] = append(byMeter[item.MeterID], item)
	}

	for _, meterItems := range byMeter {
		if len(meterItems) < 2 {
			continue
		}

		closing := meterItems[0]
		for _, item := range meterItems[1:] {
			itemEnd, closingEnd := item.GetPeriodEnd(windowEnd), closing.GetPeriodEnd(windowEnd)
			if itemEnd.After(closingEnd) ||
				(itemEnd.Equal(closingEnd) && item.GetPeriodStart(windowStart).After(closing.GetPeriodStart(windowStart))) {
				closing = item
			}
		}

		closingFrom := closing.GetPeriodStart(windowStart)
		earliest := closingFrom
		sequential := true
		for _, item := range meterItems {
			if item.ID == closing.ID {
				continue
			}
			if item.GetPeriodEnd(windowEnd).After(closingFrom) {
				sequential = false
				break
			}
			if start := item.GetPeriodStart(windowStart); start.Before(earliest) {
				earliest = start
			}
		}
		if !sequential {
			continue
		}

		h.closingStart[closing.ID] = earliest
		for _, item := range meterItems {
			if item.ID != closing.ID {
				h.superseded[item.ID] = struct{}{}
			}
		}
	}
	return h
}

// IsSuperseded reports whether a later line item on the same meter bills this one's usage.
func (h *periodEndHandoff) IsSuperseded(lineItemID string) bool {
	if h == nil {
		return false
	}
	_, ok := h.superseded[lineItemID]
	return ok
}

// UsageStart is where this line item's usage query starts: the earliest replaced line
// item's start for a closing item, its own start otherwise.
func (h *periodEndHandoff) UsageStart(item *subscription.SubscriptionLineItem, windowStart time.Time) time.Time {
	if h != nil {
		if start, ok := h.closingStart[item.ID]; ok {
			return start
		}
	}
	return item.GetPeriodStart(windowStart)
}
