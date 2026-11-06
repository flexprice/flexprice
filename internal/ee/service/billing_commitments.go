package service

import (
	"context"
	"slices"
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
	commitments       []*stackedCommitment    // sorted for fill: priority, then start, then ID
	scopeByCommitment map[string]*loadedScope // keyed by commitment ID
	handledLineIDs    map[string]bool
	cachedExternalIDs []string
}

// loadedScope is what the loader resolves for a commitment beyond its stacked form.
type loadedScope struct {
	lineItems    []*subscription.SubscriptionLineItem // in fill order, limited to the billed line items
	bucket       types.WindowSize
	overagePrice *price.Price
	amountOf     amountFn
	accrualStart time.Time      // where the commitment period's capacity starts accruing
	location     *time.Location // the commitment's timezone, loaded once for its active windows
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
		handledLineIDs:    map[string]bool{},
	}

	svc := newCommitmentService(s.ServiceParams)
	enabled, err := svc.IsEnabled(ctx)
	if err != nil {
		return nil, err
	}

	if !enabled {
		return l, nil
	}

	active, err := svc.ListOverlapping(ctx, sub.ID, periodStart, periodEnd)
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
	billedItems := lo.KeyBy(sub.LineItems, func(item *subscription.SubscriptionLineItem) string { return item.ID })
	for _, c := range active {
		if err := l.load(ctx, c, billedItems); err != nil {
			return nil, err
		}
	}

	sortCommitmentStack(l.commitments)
	if err := l.checkSharedLines(); err != nil {
		return nil, err
	}

	return l, nil
}

// newProjectedCommitmentLoader loads commitments for [start, end) priced through the end of the billing period
// holding end, reading usage only up to end.
func (s *billingService) newProjectedCommitmentLoader(
	ctx context.Context,
	sub *subscription.Subscription,
	start, end time.Time,
	charges map[string][]*dto.SubscriptionUsageByMetersResponse,
	meters map[string]*meter.Meter,
	source types.UsageSource,
) (*commitmentLoader, error) {
	// An end inside the open billing period projects to its end, unless the window starts after that period does
	// and so misses its earlier usage.
	periodEnd := end
	if !start.After(sub.CurrentPeriodStart) && end.After(sub.CurrentPeriodStart) && end.Before(sub.CurrentPeriodEnd) {
		periodEnd = sub.CurrentPeriodEnd
	}

	l, err := s.newCommitmentLoader(ctx, sub, start, periodEnd, periodEnd, charges, meters, source)
	if err != nil {
		return nil, err
	}

	l.usageEnd = end
	return l, nil
}

// load adds c with the billed lines it scopes, its prices and its period clipped to the window; it skips a
// commitment that has ended, scopes no billed line, or whose billing period does not hold the window.
func (l *commitmentLoader) load(
	ctx context.Context,
	c *commitment.Commitment,
	billedItems map[string]*subscription.SubscriptionLineItem,
) error {
	if c.EndedBy(l.periodStart) {
		return nil
	}

	// Skip a commitment none of whose scoped lines this calculation bills.
	scopeLineItems := scopeLinesInPeriod(
		c,
		l.subContext,
		maxTime(l.periodStart, c.StartDate),
		lo.ToPtr(c.Until(l.periodEnd)),
	)
	scopedItems := lo.FilterMap(
		scopeLineItems,
		func(li *subscription.SubscriptionLineItem, _ int) (*subscription.SubscriptionLineItem, bool) {
			item, ok := billedItems[li.ID]
			return item, ok
		},
	)
	if len(scopedItems) == 0 {
		return nil
	}

	var committedPrice *price.Price
	if c.Type != types.COMMITMENT_TYPE_AMOUNT && c.CommittedPriceID != nil {
		p, err := l.billing.PriceRepo.Get(ctx, *c.CommittedPriceID)
		if err != nil {
			return err
		}

		committedPrice = p
	}

	// A quantity commitment without a committed price values to zero.
	var amountOf amountFn = identityAmount
	if c.Type != types.COMMITMENT_TYPE_AMOUNT {
		amountOf = priceAmountFn(ctx, l.billing.ServiceParams, committedPrice)
	}

	var overagePrice *price.Price
	if c.OveragePriceID != nil {
		p, err := l.billing.PriceRepo.Get(ctx, *c.OveragePriceID)
		if err != nil {
			return err
		}

		overagePrice = p
	}

	bucket := bucketFor(c, l.subContext)
	if c.Measurement == types.CommitmentMeasurementWindow && bucket == "" {
		return ierr.NewErrorf("commitment %s measures by window but its scope has no bucketed price", c.ID).
			WithHint("Window commitments need bucketed usage prices").
			Mark(ierr.ErrInvalidOperation)
	}

	invoiced := types.Period{Start: l.periodStart, End: l.periodEnd}
	periods, err := commitmentPeriods(c, l.sub, amountOf, bucket, invoiced)
	if err != nil {
		return err
	}

	period, ok := containingPeriod(periods, invoiced)
	if !ok {
		l.billing.Logger.Debug(ctx, "commitment.skipped",
			"operation", "commitment.skipped",
			"reason", "window outside its billing periods",
			"commitment_id", c.ID,
			"subscription_id", l.sub.ID,
			"period_start", l.periodStart,
			"period_end", l.periodEnd,
		)
		return nil
	}

	// A partial invoice or a mid-period termination bills a prorated share of the period's value.
	billed := invoiced
	if c.TerminatedAt != nil {
		billed.End = minTime(billed.End, *c.TerminatedAt)
	}
	clipped := clipCommitmentPeriod(c, period, billed, amountOf, bucket, l.sub.BillingAnchor)

	for _, item := range scopedItems {
		l.handledLineIDs[item.ID] = true
	}

	l.scopeByCommitment[c.ID] = &loadedScope{
		lineItems:    scopedItems,
		bucket:       bucket,
		overagePrice: overagePrice,
		amountOf:     amountOf,
		accrualStart: period.Start,
		location:     c.Location(),
	}
	l.commitments = append(l.commitments, &stackedCommitment{
		Commitment:     c,
		CommittedPrice: committedPrice,
		Period:         clipped,
	})
	return nil
}

// checkSharedLines rejects a line scoped by commitments of different stacks, which would bill its usage twice.
func (l *commitmentLoader) checkSharedLines() error {
	claimedBy := map[string]*stackedCommitment{}
	for _, sc := range l.commitments {
		for _, item := range l.scopeByCommitment[sc.ID].lineItems {
			other, claimed := claimedBy[item.ID]
			if !claimed {
				claimedBy[item.ID] = sc
				continue
			}

			if l.stackKeyOf(other) != l.stackKeyOf(sc) {
				return ierr.NewErrorf(
					"line item %s is scoped by commitments %s and %s, which differ in type, measurement or bucket size",
					item.ID, other.ID, sc.ID,
				).
					WithHint("Overlapping commitments on one line must share type and measurement").
					Mark(ierr.ErrInvalidOperation)
			}
		}
	}

	return nil
}

func (l *commitmentLoader) stackKeyOf(sc *stackedCommitment) stackKey {
	return stackKey{
		typ:         sc.Type,
		measurement: sc.Measurement,
		bucket:      l.scopeByCommitment[sc.ID].bucket,
	}
}

// HandledLineIDs returns the line items commitments price; normal pricing skips them.
func (l *commitmentLoader) HandledLineIDs() map[string]bool {
	return l.handledLineIDs
}

// Stacks builds one stack per type, measurement and bucket size from the loaded commitments and allocates each.
func (l *commitmentLoader) Stacks(ctx context.Context) ([]*commitmentStack, []*allocation, error) {
	var keys []stackKey
	groups := map[stackKey][]*stackedCommitment{}
	for _, loaded := range l.commitments {
		key := l.stackKeyOf(loaded)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}

		// Each call fills copies, so the loaded commitments stay unfilled and Stacks can run again.
		sc := *loaded
		groups[key] = append(groups[key], &sc)
	}

	stacks := make([]*commitmentStack, 0, len(keys))
	allocs := make([]*allocation, 0, len(keys))
	for _, key := range keys {
		stack := &commitmentStack{
			Type:        key.typ,
			Measurement: key.measurement,
			Commitments: groups[key],
		}
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

// usageLines returns the commitment lines at usage value, with true-ups, before any billing terms cover them. Invoices
// and usage analytics read BillingLines instead.
func (l *commitmentLoader) usageLines(ctx context.Context) ([]dto.CreateInvoiceLineItemRequest, error) {
	stacks, allocs, err := l.Stacks(ctx)
	if err != nil {
		return nil, err
	}

	cost := func(p *price.Price, qty decimal.Decimal) decimal.Decimal {
		return l.priceSvc.CalculateCost(ctx, p, qty)
	}
	return commitmentInvoiceLines(stacks, allocs, l.sub.Currency, l.periodStart, l.periodEnd, cost), nil
}

// BillingLines returns the commitment lines an invoice bills and their total: the usage lines, with those of
// billing-terms commitments covered by their instalments.
func (l *commitmentLoader) BillingLines(ctx context.Context) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	usageLines, err := l.usageLines(ctx)
	if err != nil {
		return nil, decimal.Zero, err
	}

	lines := coverWithBillingTerms(usageLines, l.commitments)
	total := sumAmounts(lines)

	l.billing.Logger.Debug(ctx, "applied commitments to usage charges",
		"subscription_id", l.sub.ID,
		"commitment_ids", lo.Map(l.commitments, func(sc *stackedCommitment, _ int) string { return sc.ID }),
		"period_start", l.periodStart,
		"period_end", l.periodEnd,
		"lines", len(lines),
		"total", total.String(),
	)
	return lines, total, nil
}

// coverWithBillingTerms zeroes the utilized rows and drops the true-up rows of commitments collected through
// billing terms: their instalments carry the commitment's value, so usage within it is shown at no charge.
func coverWithBillingTerms(
	lines []dto.CreateInvoiceLineItemRequest,
	commitments []*stackedCommitment,
) []dto.CreateInvoiceLineItemRequest {
	covered := map[string]*commitment.Commitment{}
	for _, sc := range commitments {
		if lo.FromPtr(sc.CollectionMethod) == types.CommitmentCollectionBillingTerms {
			covered[sc.ID] = sc.Commitment
		}
	}

	if len(covered) == 0 {
		return lines
	}

	out := make([]dto.CreateInvoiceLineItemRequest, 0, len(lines))
	for _, line := range lines {
		c, ok := covered[lo.FromPtr(line.CommitmentID)]
		if !ok {
			out = append(out, line)
			continue
		}

		switch types.CommitmentLineKind(line.Metadata[types.MetadataKeyCommitmentLineKind]) {
		case types.CommitmentLineTrueUp:
			continue
		case types.CommitmentLineUtilized:
			line.Amount = decimal.Zero
			line.DisplayName = lo.ToPtr(c.InvoiceLabel + " (covered by commitment)")
		}

		out = append(out, line)
	}

	return out
}

// UsageBreakdown reports each loaded commitment's committed, used and unused capacity and their values for the
// loaded window.
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
			alloc := allocs[si].Commitments[ci]
			b := &dto.CommitmentUsageBreakdown{
				CommitmentID: sc.ID,
				InvoiceLabel: sc.InvoiceLabel,
				Type:         sc.Type,
				Measurement:  sc.Measurement,
				Binding:      sc.Binding,
				Currency:     l.sub.Currency,
				AppliesFrom:  maxTime(l.periodStart, sc.StartDate),
				AppliesTo:    sc.Until(l.periodEnd),
				Committed: dto.CommitmentUsageValue{
					Quantity: alloc.Capacity,
					Amount:   types.RoundToCurrencyPrecision(sc.Period.Amount, l.sub.Currency),
				},
				Used:   dto.CommitmentUsageValue{Quantity: alloc.Used},
				Unused: dto.CommitmentUsageValue{Quantity: alloc.Unused},
			}
			byID[sc.ID] = b
			out = append(out, b)
		}
	}

	for _, line := range lines {
		b := byID[lo.FromPtr(line.CommitmentID)]
		kind := types.CommitmentLineKind(line.Metadata[types.MetadataKeyCommitmentLineKind])
		if b != nil && kind == types.CommitmentLineUtilized {
			b.Used.Amount = b.Used.Amount.Add(line.Amount)
		}
	}

	// Billed amounts follow the usage invoice, which covers billing-terms commitments.
	for _, line := range coverWithBillingTerms(lines, l.commitments) {
		b := byID[lo.FromPtr(line.CommitmentID)]
		if b == nil {
			continue
		}

		switch types.CommitmentLineKind(line.Metadata[types.MetadataKeyCommitmentLineKind]) {
		case types.CommitmentLineUtilized, types.CommitmentLineTrueUp:
			b.BilledAmount = b.BilledAmount.Add(line.Amount)
		}
	}

	for _, b := range out {
		b.Unused.Amount = decimal.Max(b.Committed.Amount.Sub(b.Used.Amount), decimal.Zero)
	}

	return out, nil
}

// commitmentUsageBreakdown reports each commitment's usage for [start, end) as the period invoice would value it;
// nil when none applies.
func (s *billingService) commitmentUsageBreakdown(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	start, end time.Time,
) ([]*dto.CommitmentUsageBreakdown, error) {
	loader, err := s.newProjectedCommitmentLoader(ctx, sub, start, end, rawChargesByLine(sub, usage), nil, "")
	if err != nil || len(loader.commitments) == 0 {
		return nil, err
	}

	return loader.UsageBreakdown(ctx)
}

// buildStackLines creates the stack's usage lines, links each to the commitments scoping it and returns the
// lines' meters, aligned with stack.Lines.
func (l *commitmentLoader) buildStackLines(ctx context.Context, stack *commitmentStack) ([]*meter.Meter, error) {
	lineByItemID := map[string]*usageLine{}
	firstScopedBy := map[string]string{} // line item ID to the first commitment scoping it, for errors
	var lineMeters []*meter.Meter
	for _, sc := range stack.Commitments {
		scope := l.scopeByCommitment[sc.ID]
		for _, item := range scope.lineItems {
			line, ok := lineByItemID[item.ID]
			if ok && !sameOveragePrice(line.OveragePrice, scope.overagePrice) {
				return nil, ierr.NewErrorf(
					"commitments %s and %s cover line item %s with different overage prices",
					firstScopedBy[item.ID], sc.ID, item.ID,
				).
					WithHint("Overlapping commitments on one line must share an overage price").
					Mark(ierr.ErrInvalidOperation)
			}

			if !ok {
				p, m, err := l.priceAndMeter(ctx, item)
				if err != nil {
					return nil, err
				}

				line = &usageLine{
					Item:         item,
					Price:        p,
					OveragePrice: scope.overagePrice,
				}
				lineByItemID[item.ID] = line
				firstScopedBy[item.ID] = sc.ID
				stack.Lines = append(stack.Lines, line)
				lineMeters = append(lineMeters, m)
			}

			sc.Lines = append(sc.Lines, line)
		}
	}

	return lineMeters, nil
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

	// The floor counts the buckets the pools hold over the whole window, accruing from the commitment period's start.
	for _, sc := range stack.Commitments {
		sc.PoolOf = make([]int, len(stack.Windows))
		for w, t := range stack.Windows {
			// A bucket starting before the period belongs to the period before it, which counted its capacity.
			if t.Before(l.periodStart) || !l.appliesAt(sc.Commitment, t) {
				sc.PoolOf[w] = noPool
				continue
			}

			sc.PoolOf[w] = len(sc.Pools)
			sc.Pools = append(sc.Pools, sc.Value)
		}

		scope := l.scopeByCommitment[sc.ID]
		elapsed := types.Period{Start: scope.accrualStart, End: l.periodStart}
		current := types.Period{Start: l.periodStart, End: l.periodEnd}
		before := l.activeBuckets(sc.Commitment, elapsed, bucket)
		sc.Period.ActiveBuckets = l.activeBuckets(sc.Commitment, current, bucket)
		sc.Period.Capacity = sc.Value.Mul(decimal.NewFromInt(sc.Period.ActiveBuckets))
		sc.Period.Amount = accruedAmount(
			scope.amountOf,
			sc.Value.Mul(decimal.NewFromInt(before)),
			sc.Value.Mul(decimal.NewFromInt(before+sc.Period.ActiveBuckets)),
			sc.Currency,
		)
	}

	return nil
}

// activeBuckets counts the buckets of the usage grid starting within span that the commitment's capacity applies to.
func (l *commitmentLoader) activeBuckets(c *commitment.Commitment, span types.Period, bucket types.WindowSize) int64 {
	anchor := &l.sub.BillingAnchor
	var count int64
	for start := truncateToBucketStart(span.Start.UTC(), bucket, anchor); start.Before(span.End); start = nextBucketStart(start, bucket, anchor) {
		if !start.Before(span.Start) && l.appliesAt(c, start) {
			count++
		}
	}

	return count
}

// appliesAt reports whether the commitment's capacity applies at t: within its term, which ends with the subscription
// when it is open-ended, and inside its active windows.
func (l *commitmentLoader) appliesAt(c *commitment.Commitment, t time.Time) bool {
	return !t.Before(c.StartDate) && !endReached(termEnd(l.sub, c), t) && c.ActiveWindows.Contains(t, l.scopeByCommitment[c.ID].location)
}

// windowOf returns the index of the window holding max(t, line start), or of the first window for an earlier one.
// A bucket opened before its line (ClickHouse weeks start on Sunday) draws capacity from the line's start.
func (l *usageLine) windowOf(windows []time.Time, t time.Time) int {
	start := maxTime(t, l.Item.StartDate)
	return max(sort.Search(len(windows), func(i int) bool { return windows[i].After(start) })-1, 0)
}

// splitLineIntoBuckets lays a bucketed line's usage out one bucket per window, each priced on its own as normal
// billing prices it; windowOf places each bucket, so one off the grid lands rather than vanishes.
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

		at := line.windowOf(stack.Windows, t)
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
	commitmentEdges := lo.FlatMap(stack.Commitments, func(sc *stackedCommitment, _ int) []time.Time {
		// An end past the range, or none, lands at end and adds no boundary.
		return []time.Time{sc.StartDate, sc.Until(l.periodEnd)}
	})
	boundaryStarts := periodSplits(l.periodStart, l.periodEnd, commitmentEdges)
	lineMeters, err := l.buildStackLines(ctx, stack)
	if err != nil {
		return err
	}

	versionEdges := lo.FlatMap(stack.Lines, func(line *usageLine, _ int) []time.Time {
		return []time.Time{line.Item.StartDate, line.Item.EndDate}
	})
	windowStarts := slices.Concat(boundaryStarts, periodSplits(l.periodStart, l.periodEnd, versionEdges))

	// Windows start at the buckets' own keys, which can sit off a generated grid; a bucket before the period joins
	// the first window.
	bucketedUsage := make([]map[time.Time]decimal.Decimal, len(stack.Lines))
	for i, line := range stack.Lines {
		m := lineMeters[i]
		if !price.IsBucketedMax(line.Price, m) && !price.IsBucketedSum(line.Price, m) {
			continue
		}

		if bucketedUsage[i], err = l.bucketUsage(ctx, line, m, price.ResolveBucketSize(line.Price, m)); err != nil {
			return err
		}

		for t := range bucketedUsage[i] {
			// A bucket opened before its line starts its window at the line's start, as windowOf places it.
			if at := maxTime(t, line.Item.StartDate); at.After(l.periodStart) {
				windowStarts = append(windowStarts, at)
			}
		}
	}

	slices.SortFunc(windowStarts, time.Time.Compare)
	stack.Windows = slices.CompactFunc(windowStarts, time.Time.Equal)

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
		sc.Pools = []decimal.Decimal{sc.Period.Capacity}
		sc.PoolOf = make([]int, len(stack.Windows))
		for w, t := range stack.Windows {
			if !l.appliesAt(sc.Commitment, t) {
				sc.PoolOf[w] = noPool
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
	for _, charge := range l.activeCharges(line.Item) {
		line.Quantity = line.Quantity.Add(charge.QuantityDecimal())
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
		at := line.windowOf(stack.Windows, t)
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

	slices.SortFunc(boundaries, time.Time.Compare)
	return slices.CompactFunc(boundaries, time.Time.Equal)
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

	for i := range last {
		from, to := line.Item.GetPeriodStart(boundaryStarts[i]), line.Item.GetPeriodEnd(boundaryStarts[i+1])
		if !from.Before(to) {
			continue
		}

		result, err := l.lineUsage(ctx, line, m, boundaryStarts[i], boundaryStarts[i+1])
		if err != nil {
			return nil, err
		}

		out[i] = result.Value
	}

	remaining := line.Quantity
	for i := range last {
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
		if result, err = l.lineUsage(ctx, line, m, l.periodStart, end); err != nil {
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

// priceAndMeter returns a scoped line item's price and meter, preferring the invoice's own copies.
func (l *commitmentLoader) priceAndMeter(
	ctx context.Context,
	item *subscription.SubscriptionLineItem,
) (*price.Price, *meter.Meter, error) {
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
		return nil, nil, ierr.NewErrorf(
			"line item %s has a price bucketed per %q group, which commitments cannot cover", item.ID, groupBy,
		).
			WithHint("Commitments cannot cover bucketed prices whose meter groups usage by a property").
			Mark(ierr.ErrInvalidOperation)
	}

	return p, m, nil
}

// lineUsage reads the line's usage in [start, end) from meter_usage, bucketed when its price is.
func (l *commitmentLoader) lineUsage(
	ctx context.Context,
	line *usageLine,
	m *meter.Meter,
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

	if price.IsBucketed(line.Price, m) {
		return l.billing.queryBucketedMeterUsageDirect(ctx, m, line.Price, line.Item, l.sub, extIDs, start, end, l.source)
	}

	result, err := l.billing.MeterUsageRepo.GetUsage(ctx, &events.MeterUsageQueryParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: extIDs,
		MeterID:             line.Item.MeterID,
		StartTime:           line.Item.GetPeriodStart(start),
		EndTime:             line.Item.GetPeriodEnd(end),
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
	for i := range last {
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

// containingPeriod returns the commitment period whose regular billing period contains window, comparing whole seconds
// so an older subscription's mid-second first period still matches.
func containingPeriod(periods []commitmentPeriod, window types.Period) (commitmentPeriod, bool) {
	start, end := window.Start.Truncate(time.Second), window.End.Truncate(time.Second)
	for _, p := range periods {
		if !p.FullStart.Truncate(time.Second).After(start) && !end.After(p.FullEnd.Truncate(time.Second)) {
			return p, true
		}
	}

	return commitmentPeriod{}, false
}
