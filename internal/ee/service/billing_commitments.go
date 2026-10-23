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
	fromEvents  bool // read usage from raw events instead of meter_usage
	context     *commitmentContext
	priceSvc    PriceService
	commitments []*stackedCommitment // in D5 order
	scopes      map[string]*loadedScope
	handled     map[string]bool
	extIDs      []string
}

// loadedScope is what the loader resolves for a commitment beyond its stacked form.
type loadedScope struct {
	items       []*subscription.SubscriptionLineItem // in fill order, limited to this call's line items
	bucket      types.WindowSize
	overageRate *decimal.Decimal
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
	fromEvents bool,
) (*commitmentLoader, error) {
	l := &commitmentLoader{
		billing:     s,
		sub:         sub,
		periodStart: periodStart,
		periodEnd:   periodEnd,
		asOf:        asOf,
		charges:     charges,
		meters:      meters,
		source:      source,
		fromEvents:  fromEvents,
		scopes:      map[string]*loadedScope{},
		handled:     map[string]bool{},
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

	l.context, err = svc.loadCommitmentContext(ctx, sub.ID)
	if err != nil {
		return nil, err
	}

	l.priceSvc = NewPriceService(s.ServiceParams)
	callItems := lo.SliceToMap(sub.LineItems, func(li *subscription.SubscriptionLineItem) (string, *subscription.SubscriptionLineItem) {
		return li.ID, li
	})

	for _, c := range active {
		effectiveEnd := commitmentEffectiveEnd(c)
		if !effectiveEnd.After(periodStart) {
			continue
		}

		// The call must carry one of the commitment's lines; other cadence groups and filtered items leave it alone.
		inScope := scopeLinesInPeriod(
			c.ScopeEntityType,
			c.ScopeEntityID,
			l.context,
			maxTime(periodStart, c.StartDate),
			minTime(periodEnd, effectiveEnd),
		)
		items := lo.FilterMap(inScope, func(li *subscription.SubscriptionLineItem, _ int) (*subscription.SubscriptionLineItem, bool) {
			item, ok := callItems[li.ID]
			return item, ok
		})
		if len(items) == 0 {
			continue
		}

		committedRate := decimal.NewFromInt(1)
		if c.Type != types.COMMITMENT_TYPE_AMOUNT {
			committedRate = decimal.Zero
			if c.CommittedPriceID != nil {
				p, err := s.PriceRepo.Get(ctx, *c.CommittedPriceID)
				if err != nil {
					return nil, err
				}

				committedRate = p.Amount
			}
		}

		var overageRate *decimal.Decimal
		if c.OveragePriceID != nil {
			p, err := s.PriceRepo.Get(ctx, *c.OveragePriceID)
			if err != nil {
				return nil, err
			}

			overageRate = lo.ToPtr(p.Amount)
		}

		bucket := bucketFor(c, l.context)
		if c.Measurement == types.CommitmentMeasurementWindow && bucket == "" {
			return nil, ierr.NewErrorf("commitment %s measures by window but its scope has no bucketed price", c.ID).
				WithHint("Window commitments need bucketed usage prices").
				Mark(ierr.ErrInvalidOperation)
		}

		periods, err := commitmentPeriods(c, sub, committedRate, bucket)
		if err != nil {
			return nil, err
		}

		period, ok := containingPeriod(periods, periodStart, periodEnd)
		if !ok {
			s.Logger.Info(ctx, "skipping commitment for a window outside its billing periods",
				"commitment_id", c.ID, "subscription_id", sub.ID, "period_start", periodStart, "period_end", periodEnd)
			continue
		}

		// A mid-period termination prorates this period; the invoice still covers the whole subscription period.
		if c.TerminatedAt != nil {
			period = clipCommitmentPeriod(c, period, *c.TerminatedAt, committedRate, bucket, sub.BillingAnchor)
		}

		for _, item := range items {
			l.handled[item.ID] = true
		}

		l.scopes[c.ID] = &loadedScope{items: items, bucket: bucket, overageRate: overageRate}
		l.commitments = append(l.commitments, &stackedCommitment{C: c, CommittedRate: committedRate, Period: period})
	}

	sortCommitmentStack(l.commitments)

	// A line priced by two stacks would bill its usage twice, which terminated commitments of another type can cause.
	claimedBy := map[string]*stackedCommitment{}
	for _, sc := range l.commitments {
		for _, item := range l.scopes[sc.C.ID].items {
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
	return stackKey{typ: sc.C.Type, measurement: sc.C.Measurement, bucket: l.scopes[sc.C.ID].bucket}
}

// HandledLineIDs returns the line items commitments price; normal pricing skips them.
func (l *commitmentLoader) HandledLineIDs() map[string]bool {
	return l.handled
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
			err = l.fillWindowStack(ctx, stack, key.bucket)
		} else {
			err = l.fillPeriodStack(ctx, stack)
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

// UsageBreakdown reports each commitment's capacity, usage and billed amount for the loaded window.
// When usage exceeds capacity, ValueWithin reaches PeriodAmount and ValueUnused is 0.
func (l *commitmentLoader) UsageBreakdown(ctx context.Context) ([]*dto.CommitmentUsageBreakdown, error) {
	stacks, allocs, err := l.Stacks(ctx)
	if err != nil {
		return nil, err
	}

	cost := func(p *price.Price, qty decimal.Decimal) decimal.Decimal {
		return l.priceSvc.CalculateCost(ctx, p, qty)
	}
	lines := commitmentInvoiceLines(stacks, allocs, l.sub.Currency, l.periodStart, l.periodEnd, cost)

	var out []*dto.CommitmentUsageBreakdown
	for si, stack := range stacks {
		for ci, sc := range stack.Commitments {
			used := allocs[si].Commitments[ci]
			b := &dto.CommitmentUsageBreakdown{
				CommitmentID: sc.C.ID,
				InvoiceLabel: sc.C.InvoiceLabel,
				PeriodAmount: types.RoundToCurrencyPrecision(sc.Period.Amount, l.sub.Currency),
				Capacity:     used.Capacity,
				Used:         used.Used,
				Unused:       used.Unused,
			}

			trueUp := decimal.Zero
			for _, line := range lines {
				if lo.FromPtr(line.CommitmentID) != sc.C.ID {
					continue
				}

				switch types.CommitmentLineKind(line.Metadata[types.MetadataKeyCommitmentLineKind]) {
				case types.CommitmentLineWithin:
					b.ValueWithin = b.ValueWithin.Add(line.Amount)
				case types.CommitmentLineTrueUp:
					trueUp = trueUp.Add(line.Amount)
				}
			}

			b.ValueUnused = decimal.Max(b.PeriodAmount.Sub(b.ValueWithin), decimal.Zero)
			b.BilledAmount = b.ValueWithin.Add(trueUp)
			out = append(out, b)
		}
	}

	return out, nil
}

// commitmentUsageBreakdown reports each commitment's usage for the window from the raw-events usage; nil when none applies.
func (s *billingService) commitmentUsageBreakdown(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	start, end time.Time,
) ([]*dto.CommitmentUsageBreakdown, error) {
	loader, err := s.newCommitmentLoader(
		ctx, sub, start, end, time.Now().UTC(), rawChargesByLine(sub, usage), nil, "", true,
	)
	if err != nil || len(loader.commitments) == 0 {
		return nil, err
	}

	return loader.UsageBreakdown(ctx)
}

// linkLines creates the stack's usage lines, links them to the commitments scoping them and returns each
// line's meter, aligned with stack.Lines.
func (l *commitmentLoader) linkLines(ctx context.Context, stack *commitmentStack) ([]*meter.Meter, error) {
	byItem := map[string]*usageLine{}
	overageFrom := map[string]*stackedCommitment{}
	var meters []*meter.Meter
	for _, sc := range stack.Commitments {
		scope := l.scopes[sc.C.ID]
		for _, item := range scope.items {
			line, ok := byItem[item.ID]
			if !ok {
				p, m, err := l.lineDeps(ctx, item)
				if err != nil {
					return nil, err
				}

				line = &usageLine{Item: item, Price: p}
				byItem[item.ID] = line
				stack.Lines = append(stack.Lines, line)
				meters = append(meters, m)
			}

			// Commitments covering a line must agree on its overage rate.
			first, seen := overageFrom[item.ID]
			if !seen {
				overageFrom[item.ID] = sc
				line.OverageRate = scope.overageRate
			} else if prior := l.scopes[first.C.ID].overageRate; (prior == nil) != (scope.overageRate == nil) ||
				(prior != nil && !prior.Equal(*scope.overageRate)) {
				return nil, ierr.NewErrorf(
					"commitments %s and %s cover line item %s with different overage rates",
					first.C.ID, sc.C.ID, item.ID,
				).
					WithHint("Overlapping commitments on one line must share an overage rate").
					Mark(ierr.ErrInvalidOperation)
			}

			sc.Lines = append(sc.Lines, line)
		}
	}

	return meters, nil
}

// fillWindowStack gives the stack one window per bucket in the elapsed period, so empty buckets still count as
// capacity, and one pool per bucket each commitment is active in.
func (l *commitmentLoader) fillWindowStack(ctx context.Context, stack *commitmentStack, bucket types.WindowSize) error {
	stack.Windows = generateBucketStarts(l.periodStart, minTime(l.periodEnd, l.asOf), bucket, &l.sub.BillingAnchor)
	meters, err := l.linkLines(ctx, stack)
	if err != nil {
		return err
	}

	for i, line := range stack.Lines {
		quantities, err := l.bucketQuantities(ctx, line, meters[i], bucket, stack.Windows)
		if err != nil {
			return err
		}

		unitCost := func(q decimal.Decimal) decimal.Decimal { return l.priceSvc.CalculateCost(ctx, line.Price, q) }
		raw := decimal.Zero
		for _, q := range quantities {
			line.Quantity = line.Quantity.Add(q)
			raw = raw.Add(unitCost(q))
		}

		line.Cost = types.RoundToCurrencyPrecision(raw, line.Price.Currency)
		line.Usage = quantities
		if stack.Type == types.COMMITMENT_TYPE_AMOUNT {
			line.Usage = spreadWindowCosts(quantities, line.Cost, unitCost)
		}
	}

	for _, sc := range stack.Commitments {
		loc := sc.C.Location()
		sc.PoolOf = make([]int, len(stack.Windows))
		for w, t := range stack.Windows {
			if !sc.C.IsActiveAt(t) || !sc.C.ActiveWindows.Contains(t, loc) {
				sc.PoolOf[w] = -1
				continue
			}

			sc.PoolOf[w] = len(sc.Pools)
			sc.Pools = append(sc.Pools, capacityPool{Capacity: sc.C.Value})
		}
	}

	return nil
}

// fillPeriodStack gives the stack one window for the period, or one per stretch between commitment starts and
// ends inside it. Each commitment has a single pool spanning the stretches it is in force.
func (l *commitmentLoader) fillPeriodStack(ctx context.Context, stack *commitmentStack) error {
	starts := periodSplits(stack.Commitments, l.periodStart, l.periodEnd)
	stack.Windows = starts
	meters, err := l.linkLines(ctx, stack)
	if err != nil {
		return err
	}

	for i, line := range stack.Lines {
		bucketed, err := l.periodTotals(ctx, line, meters[i])
		if err != nil {
			return err
		}

		line.Usage = []decimal.Decimal{line.Quantity}
		if stack.Type == types.COMMITMENT_TYPE_AMOUNT {
			line.Usage = []decimal.Decimal{line.Cost}
		}

		if len(starts) == 1 {
			continue
		}

		quantities, err := l.splitQuantities(ctx, line, meters[i], bucketed, starts)
		if err != nil {
			return err
		}

		line.Usage = quantities
		if stack.Type == types.COMMITMENT_TYPE_AMOUNT {
			unitCost := func(q decimal.Decimal) decimal.Decimal { return l.priceSvc.CalculateCost(ctx, line.Price, q) }
			line.Usage = spreadWindowCosts(quantities, line.Cost, unitCost)
		}
	}

	for _, sc := range stack.Commitments {
		sc.Pools = []capacityPool{{Capacity: sc.Period.Capacity}}
		sc.PoolOf = make([]int, len(starts))
		for w, t := range starts {
			// Start is compared at whole seconds, as the period edges are.
			inForce := !sc.C.StartDate.Truncate(time.Second).After(t) && commitmentEffectiveEnd(sc.C).After(t)
			if len(starts) > 1 && !inForce {
				sc.PoolOf[w] = -1
			}
		}
	}

	return nil
}

// periodSplits returns the start of each stretch of [start, end) between the commitments' starts and ends.
// Times in the same second as the period edges do not split it, since computed billing dates drop sub-second precision.
func periodSplits(commitments []*stackedCommitment, start time.Time, end time.Time) []time.Time {
	from, to := start.Truncate(time.Second), end.Truncate(time.Second)
	out := []time.Time{start}
	for _, sc := range commitments {
		for _, t := range []time.Time{sc.C.StartDate, commitmentEffectiveEnd(sc.C)} {
			sec := t.Truncate(time.Second)
			if sec.After(from) && sec.Before(to) {
				out = append(out, t)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return lo.UniqBy(out, func(t time.Time) int64 { return t.UnixNano() })
}

// splitQuantities returns the line's usage in each stretch from its buckets, or one read per stretch but the last.
// Stretches are capped in time order so they sum to the period quantity; the last takes the remainder.
func (l *commitmentLoader) splitQuantities(
	ctx context.Context,
	line *usageLine,
	m *meter.Meter,
	bucketed *events.AggregationResult,
	starts []time.Time,
) ([]decimal.Decimal, error) {
	out := make([]decimal.Decimal, len(starts))
	last := len(starts) - 1
	if !line.Quantity.IsPositive() {
		out[last] = line.Quantity
		return out, nil
	}

	if bucketed != nil {
		// A bucket belongs to the stretch it starts in; buckets starting before the period go to the first.
		for t, v := range aggregateUsageResultsByWindow(bucketed.Results, m.Aggregation.Type) {
			next := sort.Search(len(starts), func(i int) bool { return starts[i].After(t) })
			at := max(next-1, 0)
			out[at] = out[at].Add(v)
		}
	} else {
		for i := 0; i < last; i++ {
			from, to := line.Item.GetPeriodStart(starts[i]), line.Item.GetPeriodEnd(starts[i+1])
			if !from.Before(to) {
				continue
			}

			result, err := l.lineUsage(ctx, m, line, starts[i], starts[i+1])
			if err != nil {
				return nil, err
			}

			out[i] = result.Value
		}
	}

	remaining := line.Quantity
	for i := 0; i < last; i++ {
		out[i] = decimal.Min(decimal.Max(out[i], decimal.Zero), remaining)
		remaining = remaining.Sub(out[i])
	}

	out[last] = remaining
	return out, nil
}

// periodTotals sets the line's period quantity and cost the way the normal meter-usage path prices them,
// and returns the bucketed usage it priced, if any.
func (l *commitmentLoader) periodTotals(
	ctx context.Context,
	line *usageLine,
	m *meter.Meter,
) (*events.AggregationResult, error) {
	charges := l.activeCharges(line.Item)
	if len(charges) == 0 {
		return nil, nil
	}

	if price.IsBucketedMax(line.Price, m) || price.IsBucketedSum(line.Price, m) {
		result := charges[0].BucketedUsageResult
		if result == nil {
			var err error
			if result, err = l.lineUsage(ctx, m, line, l.periodStart, l.periodEnd); err != nil {
				return nil, err
			}
		}

		cost := calculateBucketedMeterCost(ctx, l.priceSvc, line.Price, result, price.BucketedGroupBy(line.Price, m) != "")
		line.Quantity = cost.Quantity
		line.Cost = types.RoundToCurrencyPrecision(cost.Amount, line.Price.Currency)
		return result, nil
	}

	for _, c := range charges {
		line.Quantity = line.Quantity.Add(c.QuantityDecimal())
	}

	line.Cost = types.RoundToCurrencyPrecision(l.priceSvc.CalculateCost(ctx, line.Price, line.Quantity), line.Price.Currency)
	return nil, nil
}

// bucketQuantities returns the line's usage for each bucket start; buckets without usage are zero.
func (l *commitmentLoader) bucketQuantities(
	ctx context.Context,
	line *usageLine,
	m *meter.Meter,
	bucket types.WindowSize,
	starts []time.Time,
) ([]decimal.Decimal, error) {
	out := make([]decimal.Decimal, len(starts))
	charges := l.activeCharges(line.Item)
	if len(charges) == 0 || len(starts) == 0 {
		return out, nil
	}

	if got := price.ResolveBucketSize(line.Price, m); got != bucket {
		return nil, ierr.NewErrorf("line item %s has bucket size %q, its commitment measures %q", line.Item.ID, got, bucket).
			WithHint("All prices in a window commitment must share one bucket size").
			Mark(ierr.ErrInvalidOperation)
	}

	result := charges[0].BucketedUsageResult
	if result == nil {
		var err error
		if result, err = l.lineUsage(ctx, m, line, l.periodStart, minTime(l.periodEnd, l.asOf)); err != nil {
			return nil, err
		}
	}

	byWindow := map[int64]decimal.Decimal{}
	for t, v := range aggregateUsageResultsByWindow(result.Results, m.Aggregation.Type) {
		byWindow[t.Unix()] = v
	}

	for i, t := range starts {
		out[i] = byWindow[t.Unix()]
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
		p = l.context.Prices[item.PriceID]
	}

	if p == nil {
		var err error
		if p, err = l.billing.PriceRepo.Get(ctx, item.PriceID); err != nil {
			return nil, nil, err
		}
	}

	m := l.meters[item.MeterID]
	if m == nil {
		m = l.context.Meters[item.MeterID]
	}

	if m == nil {
		return nil, nil, ierr.NewErrorf("meter %s not found for line item %s", item.MeterID, item.ID).
			WithReportableDetails(map[string]interface{}{"meter_id": item.MeterID}).
			Mark(ierr.ErrNotFound)
	}

	return p, m, nil
}

// lineUsage reads the line's usage in [start, end), bucketed when its price is, from raw events on the raw-events path.
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

	item, p, s := line.Item, line.Price, l.billing
	if !l.fromEvents {
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

	eventSvc := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger, s.Config, s.TracingSvc)
	return eventSvc.GetUsageByMeter(ctx, &dto.GetUsageByMeterRequest{
		MeterID:             item.MeterID,
		PriceID:             item.PriceID,
		Price:               p,
		ExternalCustomerIDs: extIDs,
		StartTime:           item.GetPeriodStart(start),
		EndTime:             item.GetPeriodEnd(end),
		WindowSize:          price.ResolveBucketSize(p, m),
		BillingAnchor:       &l.sub.BillingAnchor,
		Filters:             m.ToFilterMap(),
		Meter:               m,
		Timezone:            l.sub.Timezone,
	})
}

func (l *commitmentLoader) externalIDs(ctx context.Context) ([]string, error) {
	if l.extIDs != nil {
		return l.extIDs, nil
	}

	ids, err := NewSubscriptionService(l.billing.ServiceParams).ExternalCustomerIDsForSubscription(ctx, l.sub)
	if err != nil {
		return nil, err
	}

	l.extIDs = ids
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

// spreadWindowCosts prices each window and puts the rounding residue on the last window with usage,
// so the windows sum exactly to the line's period cost.
func spreadWindowCosts(
	quantities []decimal.Decimal,
	total decimal.Decimal,
	unitCost func(decimal.Decimal) decimal.Decimal,
) []decimal.Decimal {
	out := make([]decimal.Decimal, len(quantities))
	if len(quantities) == 0 {
		return out
	}

	sum := decimal.Zero
	last := len(quantities) - 1
	for i, q := range quantities {
		if q.IsZero() {
			out[i] = decimal.Zero
			continue
		}

		out[i] = unitCost(q)
		sum = sum.Add(out[i])
		last = i
	}

	out[last] = out[last].Add(total.Sub(sum))
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

// commitmentEffectiveEnd is when the commitment stops running: its end, or termination if earlier.
func commitmentEffectiveEnd(c *commitment.Commitment) time.Time {
	if c.TerminatedAt != nil {
		return minTime(c.EndDate, *c.TerminatedAt)
	}

	return c.EndDate
}
