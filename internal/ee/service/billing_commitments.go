package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// commitmentLoader turns the commitments overlapping one invoice calculation and the subscription's usage into
// allocated commitment stacks. It is empty when commitments are off or nothing overlaps the period.
type commitmentLoader struct {
	billing     *billingService
	sub         *subscription.Subscription
	periodStart time.Time
	periodEnd   time.Time
	asOf        time.Time
	charges     map[string][]*dto.SubscriptionUsageByMetersResponse
	meters      map[string]*meter.Meter
	source      types.UsageSource
	// usageEnd caps usage reads when periodEnd is projected past the queried window; zero means periodEnd.
	usageEnd          time.Time
	subContext        *commitmentContext
	priceSvc          PriceService
	commitments       []*stackedCommitment    // in D5 order
	scopeByCommitment map[string]*loadedScope // keyed by commitment ID
	ownedLineIDs      map[string]bool         // line items the commitments price; normal pricing skips them
	cachedExternalIDs []string
}

// loadedScope is what the loader resolves for a commitment beyond its stacked form.
type loadedScope struct {
	lineItems    []*subscription.SubscriptionLineItem // in fill order, limited to this call's line items
	bucket       types.WindowSize
	overagePrice *price.Price
	amountOf     func(capacity decimal.Decimal) decimal.Decimal
	periodStart  time.Time // where the commitment period's capacity starts accruing, before any clip to the invoice
}

// stackKey groups commitments that can share lines: same type and measurement, and bucket size for windows.
type stackKey struct {
	typ         types.CommitmentType
	measurement types.CommitmentMeasurement
	bucket      types.WindowSize
}

func (s *billingService) newCommitmentLoader(
	ctx context.Context,
	sub *subscription.Subscription,
	periodStart, periodEnd, asOf time.Time,
	charges map[string][]*dto.SubscriptionUsageByMetersResponse,
	meters map[string]*meter.Meter,
	source types.UsageSource,
) (*commitmentLoader, error) {
	l := &commitmentLoader{
		billing:           s,
		sub:               sub,
		periodStart:       periodStart,
		periodEnd:         periodEnd,
		asOf:              asOf,
		charges:           charges,
		meters:            meters,
		source:            source,
		scopeByCommitment: map[string]*loadedScope{},
		ownedLineIDs:      map[string]bool{},
	}

	svc := &commitmentService{ServiceParams: s.ServiceParams}
	enabled, err := svc.IsEnabled(ctx)
	if err != nil {
		return nil, err
	}

	if !enabled {
		return l, nil
	}

	active, err := svc.ActiveForPeriod(ctx, sub.ID, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}

	if len(active) == 0 {
		return l, nil
	}

	l.subContext, err = svc.loadCommitmentContext(ctx, sub.ID)
	if err != nil {
		return nil, err
	}

	l.priceSvc = NewPriceService(s.ServiceParams)
	callLineItemByID := lo.SliceToMap(sub.LineItems, func(li *subscription.SubscriptionLineItem) (string, *subscription.SubscriptionLineItem) {
		return li.ID, li
	})

	for _, c := range active {
		if c.EndedBy(periodStart) {
			continue
		}

		// The call must carry one of the commitment's lines; other cadence groups and filtered items leave it alone.
		scopeLineItems := scopeLinesInPeriod(
			c.ScopeEntityType,
			c.ScopeEntityID,
			l.subContext,
			maxTime(periodStart, c.StartDate),
			lo.ToPtr(c.Until(periodEnd)),
		)
		callScopeLineItems := lo.FilterMap(scopeLineItems, func(li *subscription.SubscriptionLineItem, _ int) (*subscription.SubscriptionLineItem, bool) {
			item, ok := callLineItemByID[li.ID]
			return item, ok
		})
		if len(callScopeLineItems) == 0 {
			continue
		}

		amountOf := identityAmount
		var committedPrice *price.Price
		if c.Type != types.COMMITMENT_TYPE_AMOUNT {
			amountOf = func(decimal.Decimal) decimal.Decimal { return decimal.Zero }
			if c.CommittedPriceID != nil {
				p, err := s.PriceRepo.Get(ctx, *c.CommittedPriceID)
				if err != nil {
					return nil, err
				}

				committedPrice = p
				amountOf = priceAmountFn(ctx, s.ServiceParams, p)
			}
		}

		var overagePrice *price.Price
		if c.OveragePriceID != nil {
			p, err := s.PriceRepo.Get(ctx, *c.OveragePriceID)
			if err != nil {
				return nil, err
			}

			overagePrice = p
		}

		bucket := bucketFor(c, l.subContext)
		if c.Measurement == types.CommitmentMeasurementWindow && bucket == "" {
			return nil, ierr.NewErrorf("commitment %s measures by window but its scope has no bucketed price", c.ID).
				WithHint("Window commitments need bucketed usage prices").
				Mark(ierr.ErrInvalidOperation)
		}

		periods, err := commitmentPeriods(c, sub, amountOf, bucket, periodStart, periodEnd)
		if err != nil {
			return nil, err
		}

		period, ok := containingPeriod(periods, periodStart, periodEnd)
		if !ok {
			s.Logger.Debug(ctx, "commitment.skipped",
				"operation", "commitment.skipped",
				"reason", "window outside its billing periods",
				"commitment_id", c.ID, "subscription_id", sub.ID, "period_start", periodStart, "period_end", periodEnd)
			continue
		}

		// An invoice for part of the period, or a mid-period termination, prorates the period's value, so the
		// partial invoices of one period sum to its full value.
		until := periodEnd
		if c.TerminatedAt != nil {
			until = minTime(until, *c.TerminatedAt)
		}
		clipped := clipCommitmentPeriod(c, period, periodStart, until, amountOf, bucket, sub.BillingAnchor)

		for _, item := range callScopeLineItems {
			l.ownedLineIDs[item.ID] = true
		}

		l.scopeByCommitment[c.ID] = &loadedScope{
			lineItems:    callScopeLineItems,
			bucket:       bucket,
			overagePrice: overagePrice,
			amountOf:     amountOf,
			periodStart:  period.Start,
		}
		l.commitments = append(l.commitments, &stackedCommitment{C: c, CommittedPrice: committedPrice, Period: clipped})
	}

	sortCommitmentStack(l.commitments)

	// A line priced by two stacks would bill its usage twice, which terminated commitments of another type can cause.
	claimedBy := map[string]*stackedCommitment{}
	for _, sc := range l.commitments {
		for _, item := range l.scopeByCommitment[sc.C.ID].lineItems {
			other, claimed := claimedBy[item.ID]
			if !claimed {
				claimedBy[item.ID] = sc
				continue
			}

			if l.stackKeyOf(other) != l.stackKeyOf(sc) {
				return nil, ierr.NewErrorf(
					"line item %s is scoped by commitments %s and %s, which differ in type, measurement or bucket size",
					item.ID, other.C.ID, sc.C.ID,
				).
					WithHint("Overlapping commitments on one line must share type and measurement").
					Mark(ierr.ErrInvalidOperation)
			}
		}
	}

	return l, nil
}

func (l *commitmentLoader) stackKeyOf(sc *stackedCommitment) stackKey {
	return stackKey{typ: sc.C.Type, measurement: sc.C.Measurement, bucket: l.scopeByCommitment[sc.C.ID].bucket}
}

// HandledLineIDs returns the line items commitments price; normal pricing skips them.
func (l *commitmentLoader) HandledLineIDs() map[string]bool {
	return l.ownedLineIDs
}

// Stacks builds one stack per type, measurement and bucket size from the loaded commitments and allocates each.
func (l *commitmentLoader) Stacks(ctx context.Context) ([]*commitmentStack, []*allocation, error) {
	var keys []stackKey
	groups := map[stackKey][]*stackedCommitment{}
	for _, proto := range l.commitments {
		key := l.stackKeyOf(proto)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}

		member := *proto
		groups[key] = append(groups[key], &member)
	}

	stacks := make([]*commitmentStack, 0, len(keys))
	allocs := make([]*allocation, 0, len(keys))
	for _, key := range keys {
		stack := &commitmentStack{Type: key.typ, Measurement: key.measurement, Commitments: groups[key]}
		var err error
		if key.measurement == types.CommitmentMeasurementWindow {
			err = l.buildWindowStack(ctx, stack, key.bucket)
		} else {
			err = l.buildPeriodStack(ctx, stack)
		}

		if err != nil {
			return nil, nil, err
		}

		alloc, err := allocate(stack)
		if err != nil {
			return nil, nil, err
		}

		stacks = append(stacks, stack)
		allocs = append(allocs, alloc)
	}

	return stacks, allocs, nil
}

// InvoiceLines returns the commitment invoice lines and their total.
func (l *commitmentLoader) InvoiceLines(ctx context.Context) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	stacks, allocs, err := l.Stacks(ctx)
	if err != nil {
		return nil, decimal.Zero, err
	}

	cost := func(p *price.Price, qty decimal.Decimal) decimal.Decimal {
		return l.priceSvc.CalculateCost(ctx, p, qty)
	}
	lines := commitmentInvoiceLines(stacks, allocs, l.sub.Currency, l.periodStart, l.periodEnd, cost)

	total := decimal.Zero
	for _, line := range lines {
		total = total.Add(line.Amount)
	}

	l.billing.Logger.Debug(ctx, "applied commitments to usage charges", "subscription_id", l.sub.ID,
		"commitment_ids", lo.Map(l.commitments, func(sc *stackedCommitment, _ int) string { return sc.C.ID }),
		"period_start", l.periodStart, "period_end", l.periodEnd, "lines", len(lines), "total", total.String())
	return lines, total, nil
}

// sameOveragePrice reports whether two overage prices agree: both absent, or both flat with equal amounts.
func sameOveragePrice(a, b *price.Price) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return a.BillingModel == types.BILLING_MODEL_FLAT_FEE &&
		b.BillingModel == types.BILLING_MODEL_FLAT_FEE &&
		a.Amount.Equal(b.Amount)
}

// UsageBreakdown reports each commitment's committed, used and unused capacity with their money
// values for the loaded window, one entry per commitment summed across its sub-window stacks.
func (l *commitmentLoader) UsageBreakdown(ctx context.Context) ([]*dto.CommitmentUsageBreakdown, error) {
	stacks, allocs, err := l.Stacks(ctx)
	if err != nil {
		return nil, err
	}

	cost := func(p *price.Price, qty decimal.Decimal) decimal.Decimal {
		return l.priceSvc.CalculateCost(ctx, p, qty)
	}
	lines := commitmentInvoiceLines(stacks, allocs, l.sub.Currency, l.periodStart, l.periodEnd, cost)

	byID := map[string]*dto.CommitmentUsageBreakdown{}
	var out []*dto.CommitmentUsageBreakdown
	for si, stack := range stacks {
		for ci, sc := range stack.Commitments {
			used := allocs[si].Commitments[ci]
			b := byID[sc.C.ID]
			if b == nil {
				b = &dto.CommitmentUsageBreakdown{
					CommitmentID: sc.C.ID,
					InvoiceLabel: sc.C.InvoiceLabel,
					Type:         sc.C.Type,
					Measurement:  sc.C.Measurement,
					Binding:      sc.C.Binding,
					Currency:     l.sub.Currency,
					AppliesFrom:  maxTime(l.periodStart, sc.C.StartDate),
					AppliesTo:    sc.C.Until(l.periodEnd),
				}
				byID[sc.C.ID] = b
				out = append(out, b)
			}

			b.Committed.Quantity = b.Committed.Quantity.Add(used.Capacity)
			b.Committed.Amount = b.Committed.Amount.Add(types.RoundToCurrencyPrecision(sc.Period.Amount, l.sub.Currency))
			b.Used.Quantity = b.Used.Quantity.Add(used.Used)
			b.Unused.Quantity = b.Unused.Quantity.Add(used.Unused)
		}
	}

	for _, line := range lines {
		b := byID[lo.FromPtr(line.CommitmentID)]
		if b == nil {
			continue
		}

		switch types.CommitmentLineKind(line.Metadata[types.MetadataKeyCommitmentLineKind]) {
		case types.CommitmentLineUtilized:
			b.Used.Amount = b.Used.Amount.Add(line.Amount)
			b.BilledAmount = b.BilledAmount.Add(line.Amount)
		case types.CommitmentLineTrueUp:
			b.BilledAmount = b.BilledAmount.Add(line.Amount)
		}
	}

	for _, b := range out {
		b.Unused.Amount = decimal.Max(b.Committed.Amount.Sub(b.Used.Amount), decimal.Zero)
	}

	return out, nil
}

// projectedCommitmentEnd extends an end inside the subscription's open billing period to that period's
// end: a to-date window must not prorate capacity and report covered usage as overage. A window
// starting mid-period misses the period's earlier usage and cannot project coherently, so it keeps
// its own end (clipped-preview semantics).
func projectedCommitmentEnd(sub *subscription.Subscription, start, end time.Time) time.Time {
	if start.After(sub.CurrentPeriodStart) {
		return end
	}

	if end.After(sub.CurrentPeriodStart) && end.Before(sub.CurrentPeriodEnd) {
		return sub.CurrentPeriodEnd
	}

	return end
}

// commitmentUsageBreakdown reports each commitment's usage for the window from the usage response's charges; nil when none applies.
// The loader projects to the end of the billing period holding the window's end, so capacities and
// floors are the ones the period invoice bills even when the query stops mid-period.
func (s *billingService) commitmentUsageBreakdown(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	start, end time.Time,
) ([]*dto.CommitmentUsageBreakdown, error) {
	projectedEnd := projectedCommitmentEnd(sub, start, end)
	loader, err := s.newCommitmentLoader(
		ctx, sub, start, projectedEnd, projectedEnd, rawChargesByLine(sub, usage), nil, "",
	)
	if err != nil || len(loader.commitments) == 0 {
		return nil, err
	}

	loader.usageEnd = end

	return loader.UsageBreakdown(ctx)
}

// buildStackLines creates the stack's usage lines, links each to the commitments scoping it and returns the
// lines' meters, aligned with stack.Lines.
func (l *commitmentLoader) buildStackLines(ctx context.Context, stack *commitmentStack) ([]*meter.Meter, error) {
	lineByItemID := map[string]*usageLine{}
	overageSetBy := map[string]*stackedCommitment{}
	var lineMeters []*meter.Meter
	for _, sc := range stack.Commitments {
		scope := l.scopeByCommitment[sc.C.ID]
		for _, item := range scope.lineItems {
			line, ok := lineByItemID[item.ID]
			if !ok {
				p, m, err := l.lineDeps(ctx, item)
				if err != nil {
					return nil, err
				}

				line = &usageLine{Item: item, Price: p}
				lineByItemID[item.ID] = line
				stack.Lines = append(stack.Lines, line)
				lineMeters = append(lineMeters, m)
			}

			// Commitments covering a line must agree on its overage price: none, or flat with equal amounts.
			setter, isSet := overageSetBy[item.ID]
			if !isSet {
				overageSetBy[item.ID] = sc
				line.OveragePrice = scope.overagePrice
			} else if !sameOveragePrice(l.scopeByCommitment[setter.C.ID].overagePrice, scope.overagePrice) {
				return nil, ierr.NewErrorf(
					"commitments %s and %s cover line item %s with different overage prices",
					setter.C.ID, sc.C.ID, item.ID,
				).
					WithHint("Overlapping commitments on one line must share an overage price").
					Mark(ierr.ErrInvalidOperation)
			}

			sc.Lines = append(sc.Lines, line)
		}
	}

	return lineMeters, nil
}

// buildWindowStack gives the stack one window per bucket in the elapsed period, so empty buckets still count as
// capacity, and one pool per bucket each commitment is active in.
func (l *commitmentLoader) buildWindowStack(ctx context.Context, stack *commitmentStack, bucket types.WindowSize) error {
	stack.Windows = generateBucketStarts(l.periodStart.UTC(), minTime(l.periodEnd, l.asOf).UTC(), bucket, &l.sub.BillingAnchor)
	lineMeters, err := l.buildStackLines(ctx, stack)
	if err != nil {
		return err
	}

	for i, line := range stack.Lines {
		usage, err := l.bucketUsage(ctx, line, lineMeters[i], bucket)
		if err != nil {
			return err
		}

		l.splitLineIntoBuckets(ctx, stack, line, usage)
	}

	// The floor counts the same buckets as the pools, over the whole window, so it never differs from the
	// capacity the allocation grants once the window has elapsed; it accrues from the commitment period's start.
	for _, sc := range stack.Commitments {
		sc.PoolOf = make([]int, len(stack.Windows))
		for w, t := range stack.Windows {
			if !accrues(sc.C, t, l.periodStart) {
				sc.PoolOf[w] = -1
				continue
			}

			sc.PoolOf[w] = len(sc.Pools)
			sc.Pools = append(sc.Pools, capacityPool{Capacity: sc.C.Value})
		}

		scope := l.scopeByCommitment[sc.C.ID]
		before := l.activeBuckets(sc.C, scope.periodStart, l.periodStart, bucket)
		sc.Period.ActiveBuckets = l.activeBuckets(sc.C, l.periodStart, l.periodEnd, bucket)
		sc.Period.Capacity = sc.C.Value.Mul(decimal.NewFromInt(sc.Period.ActiveBuckets))
		sc.Period.Amount = accruedAmount(
			scope.amountOf,
			sc.C.Value.Mul(decimal.NewFromInt(before)),
			sc.C.Value.Mul(decimal.NewFromInt(before+sc.Period.ActiveBuckets)),
			sc.C.Currency,
		)
	}

	return nil
}

// activeBuckets counts the buckets of the usage grid starting in [from, to) that hold the commitment's capacity.
func (l *commitmentLoader) activeBuckets(c *commitment.Commitment, from, to time.Time, bucket types.WindowSize) int64 {
	starts := generateBucketStarts(from.UTC(), to.UTC(), bucket, &l.sub.BillingAnchor)
	return int64(lo.CountBy(starts, func(t time.Time) bool { return accrues(c, t, from) }))
}

// accrues reports whether the bucket starting at t holds the commitment's capacity in a span starting at from.
// A bucket starting before the span belongs to the span before it, which already counted it.
func accrues(c *commitment.Commitment, t, from time.Time) bool {
	return !t.Before(from) && c.IsActiveAt(t) && c.ActiveWindows.Contains(t, c.Location())
}

// splitLineIntoBuckets lays a bucketed line's usage out one bucket per window, each priced on its own as normal billing
// prices it. A bucket joins the window containing max(its key, line start), so an instant off the grid lands rather than
// vanishes; one before the first window joins it.
func (l *commitmentLoader) splitLineIntoBuckets(
	ctx context.Context,
	stack *commitmentStack,
	line *usageLine,
	usage map[time.Time]decimal.Decimal,
) {
	quantities := make([]decimal.Decimal, len(stack.Windows))
	for t, v := range usage {
		if len(quantities) == 0 {
			break
		}

		// A bucket opened before its line (ClickHouse weeks start on Sunday) draws capacity from the line's start.
		start := maxTime(t, line.Item.StartDate)
		at := max(sort.Search(len(stack.Windows), func(i int) bool { return stack.Windows[i].After(start) })-1, 0)
		quantities[at] = quantities[at].Add(v)
	}

	costs := make([]decimal.Decimal, len(quantities))
	raw := decimal.Zero
	for w, q := range quantities {
		line.Quantity = line.Quantity.Add(q)
		costs[w] = l.priceSvc.CalculateCost(ctx, line.Price, q)
		raw = raw.Add(costs[w])
	}

	line.Cost = types.RoundToCurrencyPrecision(raw, line.Price.Currency)
	line.Usage = quantities
	line.PerWindow = true
	if stack.Type == types.COMMITMENT_TYPE_AMOUNT {
		// Each bucket keeps its own cost; spreading only reconciles them with the rounded total.
		line.Usage = spreadCost(costs, line.Cost)
	}
}

// buildPeriodStack splits a billing period at commitment boundaries, line versions, and usage buckets.
// Each commitment shares one capacity pool across the windows it applies to.
func (l *commitmentLoader) buildPeriodStack(ctx context.Context, stack *commitmentStack) error {
	boundaryStarts := periodSplits(l.periodStart, l.periodEnd, lo.FlatMap(stack.Commitments, func(sc *stackedCommitment, _ int) []time.Time {
		// An end past the range, or none, lands at end and adds no boundary.
		return []time.Time{sc.C.StartDate, sc.C.Until(l.periodEnd)}
	}))
	lineMeters, err := l.buildStackLines(ctx, stack)
	if err != nil {
		return err
	}

	// Windows start at the usage buckets' own keys, not a generated grid, which may disagree with them (ClickHouse
	// weeks start on Sunday); a bucket starting before the period joins the first window, as it joins the first span.
	bucketedUsage := make([]map[time.Time]decimal.Decimal, len(stack.Lines))
	versionStarts := periodSplits(l.periodStart, l.periodEnd, lo.FlatMap(stack.Lines, func(line *usageLine, _ int) []time.Time {
		return []time.Time{line.Item.StartDate, line.Item.EndDate}
	}))
	windowStarts := append(append([]time.Time(nil), boundaryStarts...), versionStarts...)
	for i, line := range stack.Lines {
		m := lineMeters[i]
		if !price.IsBucketedMax(line.Price, m) && !price.IsBucketedSum(line.Price, m) {
			continue
		}

		if bucketedUsage[i], err = l.bucketUsage(ctx, line, m, price.ResolveBucketSize(line.Price, m)); err != nil {
			return err
		}

		for t := range bucketedUsage[i] {
			// A bucket opened before its line (ClickHouse weeks start on Sunday) draws capacity from the line's start.
			if at := maxTime(t, line.Item.StartDate); at.After(l.periodStart) {
				windowStarts = append(windowStarts, at)
			}
		}
	}

	sort.Slice(windowStarts, func(i, j int) bool { return windowStarts[i].Before(windowStarts[j]) })
	stack.Windows = lo.UniqBy(windowStarts, func(t time.Time) int64 { return t.UnixNano() })

	for i, line := range stack.Lines {
		if bucketedUsage[i] != nil {
			l.splitLineIntoBuckets(ctx, stack, line, bucketedUsage[i])
			continue
		}

		if err := l.splitLineAcrossBoundaries(ctx, stack, line, lineMeters[i], boundaryStarts); err != nil {
			return err
		}
	}

	for _, sc := range stack.Commitments {
		sc.Pools = []capacityPool{{Capacity: sc.Period.Capacity}}
		sc.PoolOf = make([]int, len(stack.Windows))
		for w, t := range stack.Windows {
			// Start is compared at whole seconds, as the period edges are; the subscription's own end
			// bounds an open-ended commitment (termEnd), so no capacity survives the subscription.
			if sc.C.StartDate.Truncate(time.Second).After(t) || endReached(termEnd(l.sub, sc.C), t) {
				sc.PoolOf[w] = -1
			}
		}
	}

	return nil
}

// splitLineAcrossBoundaries prices an unbucketed line's period quantity the way the normal path does and lays its
// usage out per span, each at the window where the line's version starts within the span.
func (l *commitmentLoader) splitLineAcrossBoundaries(
	ctx context.Context,
	stack *commitmentStack,
	line *usageLine,
	m *meter.Meter,
	boundaryStarts []time.Time,
) error {
	for _, c := range l.activeCharges(line.Item) {
		line.Quantity = line.Quantity.Add(c.QuantityDecimal())
	}

	line.Cost = types.RoundToCurrencyPrecision(l.priceSvc.CalculateCost(ctx, line.Price, line.Quantity), line.Price.Currency)
	spanQuantities := []decimal.Decimal{line.Quantity}
	if len(boundaryStarts) > 1 {
		var err error
		if spanQuantities, err = l.quantitiesPerSpan(ctx, line, m, boundaryStarts); err != nil {
			return err
		}
	}

	spanValues := spanQuantities
	if stack.Type == types.COMMITMENT_TYPE_AMOUNT {
		// Pricing each span alone would walk tiers per span; spreading the period cost conserves it.
		spanValues = spreadCost(spanQuantities, line.Cost)
	}

	line.Usage = make([]decimal.Decimal, len(stack.Windows))
	for i, t := range boundaryStarts {
		from := maxTime(t, line.Item.StartDate)
		at := max(sort.Search(len(stack.Windows), func(w int) bool { return stack.Windows[w].After(from) })-1, 0)
		line.Usage[at] = line.Usage[at].Add(spanValues[i])
	}

	return nil
}

// periodSplits returns the starts of the sub-ranges [start, end) is split into at the given instants.
// Times in the same second as the period edges do not split it, since computed billing dates drop sub-second precision.
func periodSplits(start time.Time, end time.Time, instants []time.Time) []time.Time {
	from, to := start.Truncate(time.Second), end.Truncate(time.Second)
	boundaries := []time.Time{start}
	for _, t := range instants {
		sec := t.Truncate(time.Second)
		if sec.After(from) && sec.Before(to) {
			boundaries = append(boundaries, t)
		}
	}

	sort.Slice(boundaries, func(i, j int) bool { return boundaries[i].Before(boundaries[j]) })
	return lo.UniqBy(boundaries, func(t time.Time) int64 { return t.UnixNano() })
}

// quantitiesPerSpan returns the line's usage in each span starting at boundaryStarts, one read per span but the last.
// Spans are capped in time order so they sum to the period quantity; the last takes the remainder.
func (l *commitmentLoader) quantitiesPerSpan(
	ctx context.Context,
	line *usageLine,
	m *meter.Meter,
	boundaryStarts []time.Time,
) ([]decimal.Decimal, error) {
	out := make([]decimal.Decimal, len(boundaryStarts))
	last := len(boundaryStarts) - 1
	if !line.Quantity.IsPositive() {
		out[last] = line.Quantity
		return out, nil
	}

	for i := 0; i < last; i++ {
		from, to := line.Item.GetPeriodStart(boundaryStarts[i]), line.Item.GetPeriodEnd(boundaryStarts[i+1])
		if !from.Before(to) {
			continue
		}

		result, err := l.lineUsage(ctx, m, line, boundaryStarts[i], boundaryStarts[i+1])
		if err != nil {
			return nil, err
		}

		out[i] = result.Value
	}

	remaining := line.Quantity
	for i := 0; i < last; i++ {
		out[i] = decimal.Min(decimal.Max(out[i], decimal.Zero), remaining)
		remaining = remaining.Sub(out[i])
	}

	out[last] = remaining
	return out, nil
}

// bucketUsage returns the line's usage per bucket in the elapsed period, keyed at each bucket's start.
func (l *commitmentLoader) bucketUsage(
	ctx context.Context,
	line *usageLine,
	m *meter.Meter,
	bucket types.WindowSize,
) (map[time.Time]decimal.Decimal, error) {
	out := map[time.Time]decimal.Decimal{}
	charges := l.activeCharges(line.Item)
	if len(charges) == 0 {
		return out, nil
	}

	if got := price.ResolveBucketSize(line.Price, m); got != bucket {
		return nil, ierr.NewErrorf("line item %s has bucket size %q, its commitment measures %q", line.Item.ID, got, bucket).
			WithHint("All prices in a window commitment must share one bucket size").
			Mark(ierr.ErrInvalidOperation)
	}

	end := minTime(l.periodEnd, l.asOf)
	result := charges[0].BucketedUsageResult
	if result == nil {
		var err error
		if result, err = l.lineUsage(ctx, m, line, l.periodStart, end); err != nil {
			return nil, err
		}
	}

	for t, v := range aggregateUsageResultsByWindow(result.Results, m.Aggregation.Type) {
		if t.Before(end) {
			out[t] = v
		}
	}

	return out, nil
}

// activeCharges returns the line's charges, or none when the line has not run during the elapsed period.
func (l *commitmentLoader) activeCharges(item *subscription.SubscriptionLineItem) []*dto.SubscriptionUsageByMetersResponse {
	if _, _, active := elapsedLineItemWindow(item, l.periodStart, l.periodEnd, l.asOf); !active {
		return nil
	}

	return l.charges[item.ID]
}

// lineDeps returns a scoped line item's price and meter, preferring the invoice's own copies.
func (l *commitmentLoader) lineDeps(ctx context.Context, item *subscription.SubscriptionLineItem) (*price.Price, *meter.Meter, error) {
	var p *price.Price
	if charges := l.charges[item.ID]; len(charges) > 0 && charges[0].Price != nil {
		p = charges[0].Price
	}

	if p == nil {
		p = l.subContext.Prices[item.PriceID]
	}

	if p == nil {
		var err error
		if p, err = l.billing.PriceRepo.Get(ctx, item.PriceID); err != nil {
			return nil, nil, err
		}
	}

	m := l.meters[item.MeterID]
	if m == nil {
		m = l.subContext.Meters[item.MeterID]
	}

	if m == nil {
		return nil, nil, ierr.NewErrorf("meter %s not found for line item %s", item.MeterID, item.ID).
			WithReportableDetails(map[string]interface{}{"meter_id": item.MeterID}).
			Mark(ierr.ErrNotFound)
	}

	// Allocation reads one value per bucket, so per-group bucket pricing would silently collapse.
	if groupBy := price.BucketedGroupBy(p, m); groupBy != "" {
		return nil, nil, ierr.NewErrorf("line item %s has a price bucketed per %q group, which commitments cannot cover", item.ID, groupBy).
			WithHint("Commitments cannot cover bucketed prices whose meter groups usage by a property").
			Mark(ierr.ErrInvalidOperation)
	}

	return p, m, nil
}

// lineUsage reads the line's usage in [start, end) from meter_usage, bucketed when its price is.
func (l *commitmentLoader) lineUsage(
	ctx context.Context,
	m *meter.Meter,
	line *usageLine,
	start time.Time,
	end time.Time,
) (*events.AggregationResult, error) {
	extIDs, err := l.externalIDs(ctx)
	if err != nil {
		return nil, err
	}

	if !l.usageEnd.IsZero() && end.After(l.usageEnd) {
		end = l.usageEnd
	}

	if !start.Before(end) {
		return &events.AggregationResult{}, nil
	}

	item, p, s := line.Item, line.Price, l.billing
	if price.IsBucketed(p, m) {
		return s.queryBucketedMeterUsageDirect(ctx, m, p, item, l.sub, extIDs, start, end, l.source)
	}

	result, err := s.MeterUsageRepo.GetUsage(ctx, &events.MeterUsageQueryParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: extIDs,
		MeterID:             item.MeterID,
		StartTime:           item.GetPeriodStart(start),
		EndTime:             item.GetPeriodEnd(end),
		AggregationType:     m.Aggregation.Type,
		UseFinal:            l.source.UseFinal(),
	})
	if err != nil {
		return nil, err
	}

	return &events.AggregationResult{Value: result.TotalValue}, nil
}

func (l *commitmentLoader) externalIDs(ctx context.Context) ([]string, error) {
	if l.cachedExternalIDs != nil {
		return l.cachedExternalIDs, nil
	}

	ids, err := NewSubscriptionService(l.billing.ServiceParams).ExternalCustomerIDsForSubscription(ctx, l.sub)
	if err != nil {
		return nil, err
	}

	l.cachedExternalIDs = ids
	return ids, nil
}

// rawChargesByLine keys usage charges by line item: on the charge's line item ID when set, else on price.
func rawChargesByLine(
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
) map[string][]*dto.SubscriptionUsageByMetersResponse {
	out := map[string][]*dto.SubscriptionUsageByMetersResponse{}
	if usage == nil {
		return out
	}

	for _, item := range sub.LineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}

		for _, charge := range usage.Charges {
			if charge.SubscriptionLineItemID != "" {
				if charge.SubscriptionLineItemID == item.ID {
					out[item.ID] = append(out[item.ID], charge)
				}
				continue
			}

			if charge.Price != nil && charge.Price.ID == item.PriceID {
				out[item.ID] = append(out[item.ID], charge)
			}
		}
	}

	return out
}

// spreadCost splits total across windows in proportion to their weights, summing to it exactly and never
// negative; with no positive weight it all lands on the last window.
func spreadCost(weights []decimal.Decimal, total decimal.Decimal) []decimal.Decimal {
	out := make([]decimal.Decimal, len(weights))
	if len(weights) == 0 {
		return out
	}

	sum, last := decimal.Zero, len(weights)-1
	for i, w := range weights {
		if w.IsPositive() {
			sum, last = sum.Add(w), i
		}
	}

	if !sum.IsPositive() {
		out[last] = total
		return out
	}

	// Shares are taken off a running total, so each is non-negative and the last positive one ends at total.
	cum, given := decimal.Zero, decimal.Zero
	for i := 0; i < last; i++ {
		if !weights[i].IsPositive() {
			continue
		}

		cum = cum.Add(weights[i])
		upTo := total.Mul(cum).Div(sum)
		out[i] = upTo.Sub(given)
		given = upTo
	}

	out[last] = total.Sub(given)
	return out
}

// containingPeriod returns the commitment period whose uncut billing period contains [start, end).
// Compared at whole seconds, because computed billing dates drop sub-second precision.
func containingPeriod(periods []commitmentPeriod, start, end time.Time) (commitmentPeriod, bool) {
	start, end = start.Truncate(time.Second), end.Truncate(time.Second)
	for _, p := range periods {
		if !p.FullStart.Truncate(time.Second).After(start) && !end.After(p.FullEnd.Truncate(time.Second)) {
			return p, true
		}
	}

	return commitmentPeriod{}, false
}
