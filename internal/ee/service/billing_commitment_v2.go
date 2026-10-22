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

// commitmentPass is the per-invoice commitment step; empty when the env flag is off or no commitment overlaps the period.
type commitmentPass struct {
	stack   []*stackedCommitment
	lineIDs map[string]bool // subscription line items handled by commitments; skipped by normal pricing
	cc      *commitmentContext
	periods map[string]commitmentPeriod // each commitment's invoice period, cut at termination
	buckets map[string]types.WindowSize
	settle  map[string]decimal.Decimal // bill_remaining amount owed by commitments terminated in the period
	fees    map[string]decimal.Decimal // cancellation fees of commitments terminated in the period
	// fromEvents reads bucketed usage from raw events instead of meter_usage, for the raw-events path.
	fromEvents bool
}

// prepareCommitmentPass loads commitments overlapping the period and the line items they handle.
func (s *billingService) prepareCommitmentPass(
	ctx context.Context,
	sub *subscription.Subscription,
	periodStart time.Time,
	periodEnd time.Time,
) (*commitmentPass, error) {
	pass := &commitmentPass{
		lineIDs: map[string]bool{},
		periods: map[string]commitmentPeriod{},
		buckets: map[string]types.WindowSize{},
		settle:  map[string]decimal.Decimal{},
		fees:    map[string]decimal.Decimal{},
	}

	svc := &commitmentService{ServiceParams: s.ServiceParams}
	enabled, err := svc.IsEnabled(ctx)
	if err != nil {
		return nil, err
	}

	if !enabled {
		return pass, nil
	}

	commitments, err := s.commitmentsForInvoice(ctx, sub, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}

	if len(commitments) == 0 {
		return pass, nil
	}

	cc, err := svc.loadCommitmentContext(ctx, sub.ID)
	if err != nil {
		return nil, err
	}

	pass.cc = cc

	callLines := lo.SliceToMap(sub.LineItems, func(li *subscription.SubscriptionLineItem) (string, bool) { return li.ID, true })
	for _, c := range commitments {
		effectiveEnd := commitmentEffectiveEnd(c)
		settles := owesRemaining(c, sub, periodStart, periodEnd)
		feeDue := c.CancellationFeeAmount != nil && terminatesOnInvoice(c, sub, periodStart, periodEnd)
		if !effectiveEnd.After(periodStart) && !settles && !feeDue {
			continue
		}

		// The call must carry one of the commitment's lines; other cadence groups and filtered items leave it alone.
		// A commitment that never runs in the period only settles, on the call for the subscription's own cadence.
		from, termEnd := maxTime(periodStart, c.StartDate), minTime(periodEnd, c.EndDate)
		settleOnly := !from.Before(termEnd)
		switch {
		case settleOnly && !carriesSubscriptionCadence(sub):
			continue
		case !settleOnly && len(inCall(scopeLinesInPeriod(c.ScopeEntityType, c.ScopeEntityID, cc, from, termEnd), callLines)) == 0:
			continue
		}

		rates, err := s.resolveCommitmentRates(ctx, c)
		if err != nil {
			return nil, err
		}

		bucket := bucketFor(c, cc)
		if c.Measurement == types.CommitmentMeasurementWindow && bucket == "" {
			return nil, ierr.NewErrorf("commitment %s measures by window but its scope has no bucketed price", c.ID).
				WithHint("Window commitments need bucketed usage prices").
				Mark(ierr.ErrInvalidOperation)
		}

		periods, err := commitmentPeriods(c, sub, rates, bucket)
		if err != nil {
			return nil, err
		}

		var current commitmentPeriod
		if !settleOnly {
			var ok bool
			if current, ok = containingPeriod(periods, periodStart, periodEnd); !ok {
				s.Logger.Info(ctx, "skipping commitment for a window outside its billing periods",
					"commitment_id", c.ID, "subscription_id", sub.ID, "period_start", periodStart, "period_end", periodEnd)
				continue
			}

			// A mid-period termination prorates this period; the invoice still covers the whole subscription period.
			if c.TerminatedAt != nil {
				current = clipCommitmentPeriod(c, current, *c.TerminatedAt, rates, bucket, sub.BillingAnchor)
			}
		}

		pass.periods[c.ID] = current
		pass.buckets[c.ID] = bucket
		if settles {
			pass.settle[c.ID] = remainingValue(c, periods, rates, bucket, sub.BillingAnchor)
		}

		if feeDue {
			pass.fees[c.ID] = *c.CancellationFeeAmount
		}

		var ids []string
		if !settleOnly {
			lines := inCall(
				scopeLinesInPeriod(c.ScopeEntityType, c.ScopeEntityID, cc, from, minTime(periodEnd, effectiveEnd)),
				callLines,
			)
			ids = lo.Map(lines, func(li *subscription.SubscriptionLineItem, _ int) string { return li.ID })
		}

		for _, id := range ids {
			pass.lineIDs[id] = true
		}

		pass.stack = append(pass.stack, &stackedCommitment{
			C:       c,
			Rates:   rates,
			LineIDs: ids,
		})
	}

	sortCommitmentStack(pass.stack)
	return pass, nil
}

// commitmentsForInvoice returns the commitments running in [periodStart, periodEnd), plus those that have not started
// but settle or charge a cancellation fee on this invoice; subscription end dates are applied as terminations.
func (s *billingService) commitmentsForInvoice(
	ctx context.Context,
	sub *subscription.Subscription,
	periodStart time.Time,
	periodEnd time.Time,
) ([]*commitment.Commitment, error) {
	f := commitment.NewNoLimitFilter()
	f.SubscriptionIDs = []string{sub.ID}
	f.Statuses = []types.CommitmentStatus{types.CommitmentStatusActive, types.CommitmentStatusTerminated}
	f.ActiveFrom = &periodStart
	all, err := s.CommitmentRepo.List(ctx, f)
	if err != nil {
		return nil, err
	}

	out := make([]*commitment.Commitment, 0, len(all))
	for _, c := range all {
		c = withSubscriptionEnd(c, sub, periodStart, periodEnd)
		feeDue := c.CancellationFeeAmount != nil && terminatesOnInvoice(c, sub, periodStart, periodEnd)
		if c.StartDate.Before(periodEnd) || owesRemaining(c, sub, periodStart, periodEnd) || feeDue {
			out = append(out, c)
		}
	}

	return out, nil
}

// runCommitmentPass allocates in-scope usage across the stack and returns the commitment invoice lines and their total.
func (s *billingService) runCommitmentPass(
	ctx context.Context,
	pass *commitmentPass,
	sub *subscription.Subscription,
	charges map[string][]*dto.SubscriptionUsageByMetersResponse,
	periodStart, periodEnd, asOf time.Time,
	source types.UsageSource,
	meters map[string]*meter.Meter,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	out, _, err := s.applyCommitmentPass(ctx, pass, sub, charges, periodStart, periodEnd, asOf, source, meters)
	if err != nil {
		return nil, decimal.Zero, err
	}

	total := decimal.Zero
	for _, l := range out {
		total = total.Add(l.Amount)
	}

	s.Logger.Debug(ctx, "applied commitments to usage charges", "subscription_id", sub.ID,
		"commitment_ids", lo.Map(pass.stack, func(sc *stackedCommitment, _ int) string { return sc.C.ID }),
		"period_start", periodStart, "period_end", periodEnd, "lines", len(out), "total", total.String())
	return out, total, nil
}

// applyCommitmentPass returns the commitment invoice lines with the allocation they were built from.
func (s *billingService) applyCommitmentPass(
	ctx context.Context,
	pass *commitmentPass,
	sub *subscription.Subscription,
	charges map[string][]*dto.SubscriptionUsageByMetersResponse,
	periodStart, periodEnd, asOf time.Time,
	source types.UsageSource,
	meters map[string]*meter.Meter,
) ([]dto.CreateInvoiceLineItemRequest, *allocationResult, error) {
	r := &commitmentRun{
		s:           s,
		pass:        pass,
		sub:         sub,
		charges:     charges,
		periodStart: periodStart,
		periodEnd:   periodEnd,
		asOf:        asOf,
		source:      source,
		meters:      meters,
		priceSvc:    NewPriceService(s.ServiceParams),
		lines:       map[string]*scopedLine{},
	}

	alloc := &allocationResult{ByCommitment: map[string]*commitmentAllocation{}}
	for _, group := range groupCommitmentStack(pass.stack) {
		var windows []windowUsage
		var err error
		if group[0].C.Measurement == types.CommitmentMeasurementWindow {
			windows, err = r.bucketWindows(ctx, group)
		} else {
			windows, err = r.periodWindow(ctx, group)
		}

		if err != nil {
			return nil, nil, err
		}

		res := allocateCommitments(group, windows)
		for id, a := range res.ByCommitment {
			alloc.ByCommitment[id] = a
		}

		alloc.OnDemand = append(alloc.OnDemand, res.OnDemand...)
	}

	in := commitmentLineInput{
		Stack:       pass.stack,
		Lines:       r.lines,
		Alloc:       alloc,
		PeriodByCmt: pass.periods,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		Currency:    sub.Currency,
		PriceCost: func(p *price.Price, qty decimal.Decimal) decimal.Decimal {
			return r.priceSvc.CalculateCost(ctx, p, qty)
		},
	}

	out, err := buildCommitmentLines(in)
	if err != nil {
		return nil, nil, err
	}

	for _, sc := range pass.stack {
		amount := types.RoundToCurrencyPrecision(pass.settle[sc.C.ID], sub.Currency)
		if amount.IsPositive() {
			line := commitmentLine(
				in,
				sc,
				nil,
				types.CommitmentLineSettlement,
				sc.C.InvoiceLabel+" (remaining commitment)",
				decimal.NewFromInt(1),
				amount,
			)
			line.PriceType = lo.ToPtr(string(types.PRICE_TYPE_FIXED))
			line.PriceID = sc.C.CommittedPriceID
			out = append(out, line)
		}

		fee := pass.fees[sc.C.ID]
		if fee.IsPositive() {
			line := commitmentLine(
				in,
				sc,
				nil,
				types.CommitmentLineCancellationFee,
				sc.C.InvoiceLabel+" (cancellation fee)",
				decimal.NewFromInt(1),
				fee,
			)
			line.PriceType = lo.ToPtr(string(types.PRICE_TYPE_FIXED))
			out = append(out, line)
		}
	}

	return out, alloc, nil
}

// commitmentUsageBreakdown runs the invoice preview's commitment pass for the usage window and reports each
// commitment's usage value and billed amount; nil when no commitment applies.
func (s *billingService) commitmentUsageBreakdown(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	start time.Time,
	end time.Time,
) ([]*dto.CommitmentUsageBreakdown, error) {
	pass, err := s.prepareCommitmentPass(ctx, sub, start, end)
	if err != nil || len(pass.stack) == 0 {
		return nil, err
	}

	pass.fromEvents = true
	lines, alloc, err := s.applyCommitmentPass(
		ctx,
		pass,
		sub,
		rawChargesByLine(sub, usage),
		start,
		end,
		time.Now().UTC(),
		"",
		nil,
	)
	if err != nil {
		return nil, err
	}

	return commitmentBreakdown(pass, lines, alloc, sub.Currency), nil
}

// commitmentBreakdown maps the allocation and lines of one pass to the usage API breakdown.
// When usage exceeds capacity, ValueWithin reaches PeriodAmount and ValueUnused is 0.
func commitmentBreakdown(
	pass *commitmentPass,
	lines []dto.CreateInvoiceLineItemRequest,
	alloc *allocationResult,
	currency string,
) []*dto.CommitmentUsageBreakdown {
	out := make([]*dto.CommitmentUsageBreakdown, 0, len(pass.stack))
	for _, sc := range pass.stack {
		b := &dto.CommitmentUsageBreakdown{
			CommitmentID: sc.C.ID,
			InvoiceLabel: sc.C.InvoiceLabel,
			PeriodAmount: types.RoundToCurrencyPrecision(pass.periods[sc.C.ID].Amount, currency),
		}

		if a := alloc.ByCommitment[sc.C.ID]; a != nil {
			b.Capacity, b.Unused = a.Capacity, a.Unused
			for _, used := range a.UsedByLine {
				b.Used = b.Used.Add(used)
			}
		}

		trueUp := decimal.Zero
		for _, l := range lines {
			if lo.FromPtr(l.CommitmentID) != sc.C.ID {
				continue
			}

			switch types.CommitmentLineKind(l.Metadata[types.MetadataKeyCommitmentLineKind]) {
			case types.CommitmentLineWithin:
				b.ValueWithin = b.ValueWithin.Add(l.Amount)
			case types.CommitmentLineTrueUp:
				trueUp = trueUp.Add(l.Amount)
			}
		}

		b.ValueUnused = decimal.Max(b.PeriodAmount.Sub(b.ValueWithin), decimal.Zero)
		b.BilledAmount = b.ValueWithin.Add(trueUp)
		out = append(out, b)
	}

	return out
}

// rawChargesByLine keys raw-events usage charges by line item, matching on price as the raw-events path does.
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
			if charge.Price != nil && charge.Price.ID == item.PriceID {
				out[item.ID] = append(out[item.ID], charge)
			}
		}
	}

	return out
}

// resolveCommitmentRates loads the committed and overage rates from the commitment's prices.
func (s *billingService) resolveCommitmentRates(
	ctx context.Context,
	c *commitment.Commitment,
) (commitmentRates, error) {
	rates := commitmentRates{Committed: decimal.NewFromInt(1)}
	if c.Type != types.COMMITMENT_TYPE_AMOUNT {
		rates.Committed = decimal.Zero
		if c.CommittedPriceID != nil {
			p, err := s.PriceRepo.Get(ctx, *c.CommittedPriceID)
			if err != nil {
				return rates, err
			}

			rates.Committed = p.Amount
		}
	}

	if c.OveragePriceID != nil {
		p, err := s.PriceRepo.Get(ctx, *c.OveragePriceID)
		if err != nil {
			return rates, err
		}

		rates.Overage = lo.ToPtr(p.Amount)
	}

	return rates, nil
}

// commitmentRun holds the per-invoice state shared while building windows for each commitment group.
type commitmentRun struct {
	s           *billingService
	pass        *commitmentPass
	sub         *subscription.Subscription
	charges     map[string][]*dto.SubscriptionUsageByMetersResponse
	periodStart time.Time
	periodEnd   time.Time
	asOf        time.Time
	source      types.UsageSource
	meters      map[string]*meter.Meter
	priceSvc    PriceService
	extIDs      []string
	lines       map[string]*scopedLine
}

// periodWindow builds the billing-period windows: one for the period, or one per stretch between commitment
// starts and ends inside it, with the quantity or cost per line.
func (r *commitmentRun) periodWindow(ctx context.Context, group []*stackedCommitment) ([]windowUsage, error) {
	starts := periodSplits(group, r.periodStart, r.periodEnd)
	windows := make([]windowUsage, len(starts))
	for i, t := range starts {
		windows[i] = windowUsage{Start: t, ByLine: map[string]decimal.Decimal{}}
	}

	for _, lineID := range groupLineIDs(group) {
		item, p, m, err := r.lineDeps(ctx, lineID)
		if err != nil {
			return nil, err
		}

		sl := &scopedLine{Item: item, Price: p}
		bucketed, err := r.periodTotals(ctx, sl, m)
		if err != nil {
			return nil, err
		}

		r.lines[lineID] = sl
		values := []decimal.Decimal{sl.Quantity}
		if group[0].C.Type == types.COMMITMENT_TYPE_AMOUNT {
			values = []decimal.Decimal{sl.Cost}
		}

		if len(starts) > 1 {
			quantities, err := r.splitQuantities(ctx, sl, m, bucketed, starts)
			if err != nil {
				return nil, err
			}

			values = quantities
			if group[0].C.Type == types.COMMITMENT_TYPE_AMOUNT {
				unitCost := func(q decimal.Decimal) decimal.Decimal { return r.priceSvc.CalculateCost(ctx, p, q) }
				values = spreadWindowCosts(quantities, sl.Cost, unitCost)
			}
		}

		for i, v := range values {
			windows[i].ByLine[lineID] = v
		}
	}

	for _, sc := range group {
		c := sc.C
		capacity := r.pass.periods[c.ID].Capacity
		sc.Capacity = func(t time.Time) decimal.Decimal {
			// Start is compared at whole seconds, as the period edges are.
			inForce := !c.StartDate.Truncate(time.Second).After(t) && commitmentEffectiveEnd(c).After(t)
			if len(starts) > 1 && !inForce {
				return decimal.Zero
			}

			return capacity
		}
	}

	return windows, nil
}

// periodSplits returns the start of each stretch of [start, end) between the group's commitment starts and ends.
// Times in the same second as the period edges do not split it, since computed billing dates drop sub-second precision.
func periodSplits(group []*stackedCommitment, start time.Time, end time.Time) []time.Time {
	from, to := start.Truncate(time.Second), end.Truncate(time.Second)
	out := []time.Time{start}
	for _, sc := range group {
		if len(sc.LineIDs) == 0 {
			continue
		}

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
func (r *commitmentRun) splitQuantities(
	ctx context.Context,
	sl *scopedLine,
	m *meter.Meter,
	bucketed *events.AggregationResult,
	starts []time.Time,
) ([]decimal.Decimal, error) {
	out := make([]decimal.Decimal, len(starts))
	last := len(starts) - 1
	if !sl.Quantity.IsPositive() {
		out[last] = sl.Quantity
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
		extIDs, err := r.externalIDs(ctx)
		if err != nil {
			return nil, err
		}

		for i := 0; i < last; i++ {
			from, to := sl.Item.GetPeriodStart(starts[i]), sl.Item.GetPeriodEnd(starts[i+1])
			if !from.Before(to) {
				continue
			}

			result, err := r.lineUsage(ctx, m, sl.Price, sl.Item, extIDs, starts[i], starts[i+1])
			if err != nil {
				return nil, err
			}

			out[i] = result.Value
		}
	}

	remaining := sl.Quantity
	for i := 0; i < last; i++ {
		out[i] = decimal.Min(decimal.Max(out[i], decimal.Zero), remaining)
		remaining = remaining.Sub(out[i])
	}

	out[last] = remaining
	return out, nil
}

// periodTotals sets the line's period quantity and cost the way the normal meter-usage path prices them,
// and returns the bucketed usage it priced, if any.
func (r *commitmentRun) periodTotals(
	ctx context.Context,
	sl *scopedLine,
	m *meter.Meter,
) (*events.AggregationResult, error) {
	charges := r.activeCharges(sl.Item)
	if len(charges) == 0 {
		return nil, nil
	}

	if price.IsBucketedMax(sl.Price, m) || price.IsBucketedSum(sl.Price, m) {
		result := charges[0].BucketedUsageResult
		if result == nil {
			extIDs, err := r.externalIDs(ctx)
			if err != nil {
				return nil, err
			}

			if result, err = r.lineUsage(ctx, m, sl.Price, sl.Item, extIDs, r.periodStart, r.periodEnd); err != nil {
				return nil, err
			}
		}

		cost := calculateBucketedMeterCost(ctx, r.priceSvc, sl.Price, result, price.BucketedGroupBy(sl.Price, m) != "")
		sl.Quantity = cost.Quantity
		sl.Cost = types.RoundToCurrencyPrecision(cost.Amount, sl.Price.Currency)
		return result, nil
	}

	for _, c := range charges {
		sl.Quantity = sl.Quantity.Add(c.QuantityDecimal())
	}

	sl.Cost = types.RoundToCurrencyPrecision(r.priceSvc.CalculateCost(ctx, sl.Price, sl.Quantity), sl.Price.Currency)
	return nil, nil
}

// bucketWindows builds one window per bucket in the elapsed period, so empty buckets still count as capacity.
func (r *commitmentRun) bucketWindows(ctx context.Context, group []*stackedCommitment) ([]windowUsage, error) {
	bucket := r.pass.buckets[group[0].C.ID]
	starts := generateBucketStarts(r.periodStart, minTime(r.periodEnd, r.asOf), bucket, &r.sub.BillingAnchor)
	windows := make([]windowUsage, len(starts))
	for i, t := range starts {
		windows[i] = windowUsage{Start: t, ByLine: map[string]decimal.Decimal{}}
	}

	for _, lineID := range groupLineIDs(group) {
		item, p, m, err := r.lineDeps(ctx, lineID)
		if err != nil {
			return nil, err
		}

		quantities, err := r.bucketQuantities(ctx, item, p, m, bucket, starts)
		if err != nil {
			return nil, err
		}

		sl := &scopedLine{Item: item, Price: p}
		unitCost := func(q decimal.Decimal) decimal.Decimal { return r.priceSvc.CalculateCost(ctx, p, q) }
		raw := decimal.Zero
		for _, q := range quantities {
			sl.Quantity = sl.Quantity.Add(q)
			raw = raw.Add(unitCost(q))
		}

		sl.Cost = types.RoundToCurrencyPrecision(raw, p.Currency)
		r.lines[lineID] = sl

		values := quantities
		if group[0].C.Type == types.COMMITMENT_TYPE_AMOUNT {
			values = spreadWindowCosts(quantities, sl.Cost, unitCost)
		}

		for i, v := range values {
			if !v.IsZero() {
				windows[i].ByLine[lineID] = v
			}
		}
	}

	for _, sc := range group {
		c := sc.C
		loc := c.Location()
		sc.Capacity = func(t time.Time) decimal.Decimal {
			if c.IsActiveAt(t) && c.ActiveWindows.Contains(t, loc) {
				return c.Value
			}

			return decimal.Zero
		}
	}

	return windows, nil
}

// bucketQuantities returns the line's usage for each bucket start; buckets without usage are zero.
func (r *commitmentRun) bucketQuantities(
	ctx context.Context,
	item *subscription.SubscriptionLineItem,
	p *price.Price,
	m *meter.Meter,
	bucket types.WindowSize,
	starts []time.Time,
) ([]decimal.Decimal, error) {
	out := make([]decimal.Decimal, len(starts))
	charges := r.activeCharges(item)
	if len(charges) == 0 || len(starts) == 0 {
		return out, nil
	}

	if got := price.ResolveBucketSize(p, m); got != bucket {
		return nil, ierr.NewErrorf("line item %s has bucket size %q, its commitment measures %q", item.ID, got, bucket).
			WithHint("All prices in a window commitment must share one bucket size").
			Mark(ierr.ErrInvalidOperation)
	}

	result := charges[0].BucketedUsageResult
	if result == nil {
		extIDs, err := r.externalIDs(ctx)
		if err != nil {
			return nil, err
		}

		if result, err = r.lineUsage(ctx, m, p, item, extIDs, r.periodStart, minTime(r.periodEnd, r.asOf)); err != nil {
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
func (r *commitmentRun) activeCharges(
	item *subscription.SubscriptionLineItem,
) []*dto.SubscriptionUsageByMetersResponse {
	if _, _, active := elapsedLineItemWindow(item, r.periodStart, r.periodEnd, r.asOf); !active {
		return nil
	}

	return r.charges[item.ID]
}

// lineDeps returns a scoped line item with its price and meter, preferring the invoice's own copies.
func (r *commitmentRun) lineDeps(
	ctx context.Context,
	lineID string,
) (*subscription.SubscriptionLineItem, *price.Price, *meter.Meter, error) {
	item, ok := lo.Find(r.sub.LineItems, func(li *subscription.SubscriptionLineItem) bool { return li.ID == lineID })
	if !ok {
		item, ok = lo.Find(r.pass.cc.Items, func(li *subscription.SubscriptionLineItem) bool { return li.ID == lineID })
	}

	if !ok {
		return nil, nil, nil, ierr.NewErrorf("commitment scope references unknown line item %s", lineID).
			Mark(ierr.ErrNotFound)
	}

	var p *price.Price
	if charges := r.charges[lineID]; len(charges) > 0 && charges[0].Price != nil {
		p = charges[0].Price
	}

	if p == nil {
		p = r.pass.cc.Prices[item.PriceID]
	}

	if p == nil {
		var err error
		if p, err = r.s.PriceRepo.Get(ctx, item.PriceID); err != nil {
			return nil, nil, nil, err
		}
	}

	m := r.meters[item.MeterID]
	if m == nil {
		m = r.pass.cc.Meters[item.MeterID]
	}

	if m == nil {
		return nil, nil, nil, ierr.NewErrorf("meter %s not found for line item %s", item.MeterID, item.ID).
			WithReportableDetails(map[string]interface{}{"meter_id": item.MeterID}).
			Mark(ierr.ErrNotFound)
	}

	return item, p, m, nil
}

// lineUsage reads the line's usage in [start, end), bucketed when its price is, from raw events on the raw-events path.
func (r *commitmentRun) lineUsage(
	ctx context.Context,
	m *meter.Meter,
	p *price.Price,
	item *subscription.SubscriptionLineItem,
	extIDs []string,
	start time.Time,
	end time.Time,
) (*events.AggregationResult, error) {
	if !r.pass.fromEvents {
		if price.IsBucketed(p, m) {
			return r.s.queryBucketedMeterUsageDirect(ctx, m, p, item, r.sub, extIDs, start, end, r.source)
		}

		result, err := r.s.MeterUsageRepo.GetUsage(ctx, &events.MeterUsageQueryParams{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: extIDs,
			MeterID:             item.MeterID,
			StartTime:           item.GetPeriodStart(start),
			EndTime:             item.GetPeriodEnd(end),
			AggregationType:     m.Aggregation.Type,
			UseFinal:            r.source.UseFinal(),
		})
		if err != nil {
			return nil, err
		}

		return &events.AggregationResult{Value: result.TotalValue}, nil
	}

	eventSvc := NewEventService(
		r.s.EventRepo,
		r.s.MeterRepo,
		r.s.EventPublisher,
		r.s.Logger,
		r.s.Config,
		r.s.TracingSvc,
	)
	return eventSvc.GetUsageByMeter(ctx, &dto.GetUsageByMeterRequest{
		MeterID:             item.MeterID,
		PriceID:             item.PriceID,
		Price:               p,
		ExternalCustomerIDs: extIDs,
		StartTime:           item.GetPeriodStart(start),
		EndTime:             item.GetPeriodEnd(end),
		WindowSize:          price.ResolveBucketSize(p, m),
		BillingAnchor:       &r.sub.BillingAnchor,
		Filters:             m.ToFilterMap(),
		Meter:               m,
		Timezone:            r.sub.Timezone,
	})
}

func (r *commitmentRun) externalIDs(ctx context.Context) ([]string, error) {
	if r.extIDs != nil {
		return r.extIDs, nil
	}

	ids, err := NewSubscriptionService(r.s.ServiceParams).ExternalCustomerIDsForSubscription(ctx, r.sub)
	if err != nil {
		return nil, err
	}

	r.extIDs = ids
	return ids, nil
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

// groupCommitmentStack splits the stack by type and measurement, keeping stack order; commitments that share a
// line always agree on both, so groups never share lines.
func groupCommitmentStack(stack []*stackedCommitment) [][]*stackedCommitment {
	var keys []string
	groups := map[string][]*stackedCommitment{}
	for _, sc := range stack {
		key := string(sc.C.Type) + "/" + string(sc.C.Measurement)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}

		groups[key] = append(groups[key], sc)
	}

	return lo.Map(keys, func(k string, _ int) []*stackedCommitment { return groups[k] })
}

func groupLineIDs(group []*stackedCommitment) []string {
	var ids []string
	for _, sc := range group {
		ids = append(ids, sc.LineIDs...)
	}

	return lo.Uniq(ids)
}

// scopeLinesInPeriod returns the line items in scope at any point of [from, to), ordered for fill.
func scopeLinesInPeriod(
	scopeType types.CommitmentScopeType,
	scopeID string,
	cc *commitmentContext,
	from time.Time,
	to time.Time,
) []*subscription.SubscriptionLineItem {
	if !from.Before(to) {
		return nil
	}

	at := []time.Time{from}
	for _, li := range cc.Items {
		if li.StartDate.After(from) && li.StartDate.Before(to) {
			at = append(at, li.StartDate)
		}
	}

	seen := map[string]bool{}
	var out []*subscription.SubscriptionLineItem
	for _, t := range at {
		for _, li := range resolveScopeLines(scopeType, scopeID, cc, t) {
			if !seen[li.ID] {
				seen[li.ID] = true
				out = append(out, li)
			}
		}
	}

	sortLinesForFill(out)
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

func inCall(
	lines []*subscription.SubscriptionLineItem,
	callLines map[string]bool,
) []*subscription.SubscriptionLineItem {
	return lo.Filter(lines, func(li *subscription.SubscriptionLineItem, _ int) bool { return callLines[li.ID] })
}

// withSubscriptionEnd treats a subscription end or cancel date in (start, end] as the termination of an active
// commitment that would otherwise run past it, so the final invoice settles it.
func withSubscriptionEnd(
	c *commitment.Commitment,
	sub *subscription.Subscription,
	start time.Time,
	end time.Time,
) *commitment.Commitment {
	endAt := sub.EndDate
	if sub.CancelAt != nil && (endAt == nil || sub.CancelAt.Before(*endAt)) {
		endAt = sub.CancelAt
	}

	if c.TerminatedAt != nil || endAt == nil || !endAt.After(start) || endAt.After(end) || !endAt.Before(c.EndDate) {
		return c
	}

	terminated := *c
	terminated.TerminatedAt = lo.ToPtr(*endAt)
	return &terminated
}

// carriesSubscriptionCadence reports whether the call prices usage on the subscription's own cadence.
func carriesSubscriptionCadence(sub *subscription.Subscription) bool {
	return lo.SomeBy(sub.LineItems, func(li *subscription.SubscriptionLineItem) bool {
		return li.PriceType == types.PRICE_TYPE_USAGE && billsEverySubscriptionPeriod(
			sub,
			li.BillingPeriod,
			li.BillingPeriodCount,
		)
	})
}

func commitmentEffectiveEnd(c *commitment.Commitment) time.Time {
	if c.TerminatedAt != nil {
		return minTime(c.EndDate, *c.TerminatedAt)
	}

	return c.EndDate
}

// owesRemaining reports whether a binding per_period bill_remaining commitment settles on the invoice for
// (start, end].
func owesRemaining(c *commitment.Commitment, sub *subscription.Subscription, start, end time.Time) bool {
	if !c.Binding || lo.FromPtr(c.OnEarlyTermination) != types.CommitmentTerminationBillRemaining {
		return false
	}

	if c.CollectionMethod != nil && *c.CollectionMethod != types.CommitmentCollectionPerPeriod {
		return false
	}

	return terminatesOnInvoice(c, sub, start, end)
}

// terminatesOnInvoice reports whether the invoice for (start, end] holds terminated_at; a termination at or before
// the subscription start lands on the first period.
func terminatesOnInvoice(c *commitment.Commitment, sub *subscription.Subscription, start, end time.Time) bool {
	if c.TerminatedAt == nil {
		return false
	}

	at := maxTime(*c.TerminatedAt, sub.StartDate)
	if at.Equal(start) && start.Equal(sub.StartDate) {
		return true
	}

	return at.After(start) && !at.After(end) && end.After(sub.StartDate)
}

// remainingValue is the commitment value minus what was earned up to termination.
func remainingValue(
	c *commitment.Commitment,
	periods []commitmentPeriod,
	rates commitmentRates,
	bucket types.WindowSize,
	anchor time.Time,
) decimal.Decimal {
	remaining := decimal.Zero
	for _, p := range periods {
		earned := clipCommitmentPeriod(c, p, *c.TerminatedAt, rates, bucket, anchor)
		remaining = remaining.Add(p.Amount.Sub(earned.Amount))
	}

	return remaining
}
