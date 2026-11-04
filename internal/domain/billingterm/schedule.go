package billingterm

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	// MaxTerms is the most billing terms an owner can have.
	MaxTerms = 100

	// MaxInstalments is the most instalments an owner's terms can expand to, and the most its plan can hold with the
	// invoiced ones.
	MaxInstalments = 500
)

// DefaultInvoiceLabel is the invoice label of an instalment given none. NumberLabels fills {n} and {of}.
const DefaultInvoiceLabel = "Instalment {n} of {of}"

// termErrorFormat names the term's index, then the field, then the reason.
const termErrorFormat = "billing_terms[%d].%s: %s"

// resolvedTerm is a validated term with its defaults applied, its steps cut and its total resolved.
type resolvedTerm struct {
	index int
	term  *Term

	// Once terms.
	billTrigger   types.InstalmentBillTrigger
	billOn        time.Time
	servicePeriod types.Period

	// Recurring terms.
	cadence types.InvoiceCadence
	split   types.BillingTermSplit
	steps   []PeriodValue

	// total is the term's share of the owner's total, which its instalments split.
	total decimal.Decimal

	delivery types.InstalmentDelivery
}

// Expand validates the terms against the owner and its period values and returns their instalments in billing order.
// Their labels keep {n} and {of} until NumberLabels numbers them among the owner's other instalments.
func Expand(ctx context.Context, owner Owner, periods Periods, terms []*Term) ([]*Instalment, error) {
	collection := owner.CollectionPeriod()
	total := owner.CollectionTotal()
	currency := owner.BillingCurrency()

	if !collection.End.After(collection.Start) {
		return nil, ierr.Validationf("billing_terms.owner: the collection period needs an end after its start")
	}

	if !total.IsPositive() || !total.Equal(types.RoundToCurrencyPrecision(total, currency)) {
		return nil, ierr.Validationf("billing_terms.owner: the collection total must be greater than 0 and rounded to %s", currency)
	}

	delivery := owner.InstalmentDelivery()
	if delivery != types.InstalmentDeliverySubscriptionInvoice && delivery != types.InstalmentDeliverySeparateInvoice {
		return nil, ierr.Validationf("billing_terms.owner: the instalment delivery must be subscription_invoice or separate_invoice")
	}

	if len(terms) == 0 || len(terms) > MaxTerms {
		return nil, ierr.Validationf("billing_terms: must hold 1 to %d terms", MaxTerms)
	}

	resolved := make([]*resolvedTerm, 0, len(terms))
	for i, t := range terms {
		r, err := resolveTerm(i, t, collection, owner.TermStart(), currency, delivery, periods)
		if err != nil {
			return nil, err
		}

		resolved = append(resolved, r)
	}

	if err := resolveAmounts(resolved, total); err != nil {
		return nil, err
	}

	count := lo.SumBy(resolved, func(r *resolvedTerm) int { return r.instalmentCount() })
	if count > MaxInstalments {
		return nil, ierr.Validationf("billing_terms: expand to %d instalments, more than the %d allowed", count, MaxInstalments)
	}

	instalments := make([]*Instalment, 0, count)
	for _, r := range resolved {
		rendered, err := r.instalments(ctx, owner)
		if err != nil {
			return nil, err
		}

		instalments = append(instalments, rendered...)
	}

	// A stable sort keeps term then step order among equal bill_on dates; the time-sortable IDs then follow it.
	slices.SortStableFunc(instalments, func(a, b *Instalment) int { return a.BillOn.Compare(b.BillOn) })
	for _, i := range instalments {
		i.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_INSTALMENT)
	}

	return instalments, nil
}

// NumberLabels fills {n} and {of} in planned, new instalments in billing order, counting on from numberedBefore; labels
// without them take no number. Within MaxInstalments a label never outgrows its template.
func NumberLabels(planned []*Instalment, numberedBefore int) {
	numbered := lo.Filter(planned, func(i *Instalment, _ int) bool { return IsNumbered(i.InvoiceLabel) })
	of := strconv.Itoa(numberedBefore + len(numbered))
	for n, i := range numbered {
		i.InvoiceLabel = strings.NewReplacer("{n}", strconv.Itoa(numberedBefore+n+1), "{of}", of).Replace(i.InvoiceLabel)
	}
}

// IsNumbered reports whether a label template takes a number: it uses {n} or {of}.
func IsNumbered(template string) bool {
	return strings.Contains(template, "{n}") || strings.Contains(template, "{of}")
}

// resolveTerm validates one term against the collection period and applies the defaults of its kind; a term without
// dates starts at termStart.
func resolveTerm(
	index int,
	t *Term,
	collection types.Period,
	termStart time.Time,
	currency string,
	ownerDelivery types.InstalmentDelivery,
	periods Periods,
) (*resolvedTerm, error) {
	switch t.ValueType {
	case types.BillingTermValueFixed:
		if t.Value == nil || !t.Value.IsPositive() {
			return nil, ierr.Validationf(termErrorFormat, index, "value", "must be greater than 0")
		}

		if !t.Value.Equal(types.RoundToCurrencyPrecision(*t.Value, currency)) {
			return nil, ierr.Validationf(termErrorFormat, index, "value", fmt.Sprintf("must be rounded to %s precision", currency))
		}
	case types.BillingTermValueRemainder:
		if t.Value != nil {
			return nil, ierr.Validationf(termErrorFormat, index, "value", "a remainder term takes no value")
		}
	case types.BillingTermValuePercentage:
		return nil, ierr.Validationf(termErrorFormat, index, "value_type", "percentage terms are not supported yet")
	default:
		return nil, ierr.Validationf(termErrorFormat, index, "value_type", "must be fixed or remainder")
	}

	delivery := lo.FromPtrOr(t.InstalmentInvoicing, ownerDelivery)
	if delivery != types.InstalmentDeliverySubscriptionInvoice && delivery != types.InstalmentDeliverySeparateInvoice {
		return nil, ierr.Validationf(termErrorFormat, index, "instalment_invoicing", "must be subscription_invoice or separate_invoice")
	}

	var r *resolvedTerm
	var err error
	switch t.Kind {
	case types.BillingTermKindOnce:
		r, err = resolveOnce(index, t, collection, termStart)
	case types.BillingTermKindRecurring:
		steps, unit := periods.stepsFor(delivery)
		r, err = resolveRecurring(index, t, collection, termStart, steps, unit)
	default:
		return nil, ierr.Validationf(termErrorFormat, index, "kind", "must be once or recurring")
	}
	if err != nil {
		return nil, err
	}

	r.delivery = delivery
	return r, nil
}

// resolveOnce checks a once term's dates; bill_on defaults to termStart. The collection period is the service period of
// its instalment.
func resolveOnce(index int, t *Term, collection types.Period, termStart time.Time) (*resolvedTerm, error) {
	if field := t.recurringOnlyField(); field != "" {
		return nil, ierr.Validationf(termErrorFormat, index, field, "is not used by once terms")
	}

	r := &resolvedTerm{
		index: index,
		term:  t,

		billTrigger:   lo.FromPtrOr(t.BillTrigger, types.InstalmentBillTriggerDate),
		billOn:        lo.FromPtrOr(t.BillOn, termStart),
		servicePeriod: collection,
	}

	switch r.billTrigger {
	case types.InstalmentBillTriggerDate:
		if r.billOn.Before(collection.Start) || r.billOn.After(collection.End) {
			return nil, ierr.Validationf(termErrorFormat, index, "bill_on", "must fall within the collection period")
		}
	case types.InstalmentBillTriggerManual:
		if t.BillOn != nil {
			return nil, ierr.Validationf(termErrorFormat, index, "bill_on", "is set by triggering a manual term, not on the term")
		}

		// An untriggered milestone waits at the end of the collection period.
		r.billOn = collection.End
	default:
		return nil, ierr.Validationf(termErrorFormat, index, "bill_trigger", "must be date or manual")
	}

	return r, nil
}

// resolveRecurring checks a recurring term and cuts its steps from periods, named unit in errors; start_date defaults to
// the start of the period holding termStart and end_date to the end of the collection period.
func resolveRecurring(
	index int,
	t *Term,
	collection types.Period,
	termStart time.Time,
	periods []PeriodValue,
	unit string,
) (*resolvedTerm, error) {
	if field := t.onceOnlyField(); field != "" {
		return nil, ierr.Validationf(termErrorFormat, index, field, "is not used by recurring terms")
	}

	cadence := lo.FromPtr(t.InvoiceCadence)
	if cadence != types.InvoiceCadenceAdvance && cadence != types.InvoiceCadenceArrear {
		return nil, ierr.Validationf(termErrorFormat, index, "invoice_cadence", "must be ADVANCE or ARREAR")
	}

	periodCount := lo.FromPtrOr(t.PeriodCount, 1)
	if periodCount < 1 {
		return nil, ierr.Validationf(termErrorFormat, index, "period_count", "must be at least 1")
	}

	if t.Split != nil && t.ValueType != types.BillingTermValueRemainder {
		return nil, ierr.Validationf(termErrorFormat, index, "split", "is only used by remainder terms")
	}

	split := lo.FromPtrOr(t.Split, types.BillingTermSplitEven)
	if split != types.BillingTermSplitEven && split != types.BillingTermSplitProportional {
		return nil, ierr.Validationf(termErrorFormat, index, "split", "must be even or proportional")
	}

	start := termStart
	if holding, ok := lo.Find(periods, func(p PeriodValue) bool { return !p.Start.After(termStart) && p.End.After(termStart) }); ok {
		start = holding.Start
	}

	span := types.Period{
		Start: lo.FromPtrOr(t.StartDate, start),
		End:   lo.FromPtrOr(t.EndDate, collection.End),
	}

	if !lo.ContainsBy(periods, func(p PeriodValue) bool { return p.Start.Equal(span.Start) }) {
		return nil, ierr.Validationf(termErrorFormat, index, "start_date", "must be the start of "+unit)
	}

	if !lo.ContainsBy(periods, func(p PeriodValue) bool { return p.End.Equal(span.End) }) {
		return nil, ierr.Validationf(termErrorFormat, index, "end_date", "must be the end of "+unit)
	}

	if !span.Start.Before(span.End) {
		return nil, ierr.Validationf(termErrorFormat, index, "start_date", "must be before end_date")
	}

	steps := cutSteps(periods, span, periodCount)
	if split == types.BillingTermSplitProportional && !sumValues(steps).IsPositive() {
		return nil, ierr.Validationf(termErrorFormat, index, "split", "proportional needs periods with a value")
	}

	return &resolvedTerm{
		index: index,
		term:  t,

		cadence: cadence,
		split:   split,
		steps:   steps,
	}, nil
}

// cutSteps groups the period values inside span into steps of count consecutive periods; the last may be shorter.
func cutSteps(periods []PeriodValue, span types.Period, count int) []PeriodValue {
	inside := lo.Filter(periods, func(p PeriodValue, _ int) bool {
		return !p.Start.Before(span.Start) && !p.End.After(span.End)
	})

	groups := lo.Chunk(inside, count)
	steps := make([]PeriodValue, 0, len(groups))
	for _, group := range groups {
		steps = append(steps, PeriodValue{
			Period: types.Period{
				Start: group[0].Start,
				End:   group[len(group)-1].End,
			},
			Amount: sumValues(group),
		})
	}

	return steps
}

// resolveAmounts gives the remainder term whatever the fixed terms leave of total.
func resolveAmounts(terms []*resolvedTerm, total decimal.Decimal) error {
	var remainder *resolvedTerm
	others := decimal.Zero
	for _, r := range terms {
		if r.term.ValueType == types.BillingTermValueRemainder {
			if remainder != nil {
				return ierr.Validationf(termErrorFormat, r.index, "value_type", "only one term can be the remainder")
			}

			remainder = r
			continue
		}

		r.total = lo.FromPtr(r.term.Value).Mul(decimal.NewFromInt(int64(r.instalmentCount())))
		others = others.Add(r.total)
	}

	if remainder == nil {
		if !others.Equal(total) {
			return ierr.Validationf(
				termErrorFormat, len(terms)-1, "value",
				fmt.Sprintf("the terms add up to %s, not the total of %s; adjust them or add a remainder term", others, total),
			)
		}

		return nil
	}

	remainder.total = total.Sub(others)
	if !remainder.total.IsPositive() {
		return ierr.Validationf(
			termErrorFormat, remainder.index, "value_type",
			fmt.Sprintf("the other terms add up to %s and leave nothing of the total of %s for the remainder", others, total),
		)
	}

	return nil
}

func (r *resolvedTerm) instalmentCount() int {
	if r.term.Kind == types.BillingTermKindOnce {
		return 1
	}

	return len(r.steps)
}

// instalments renders the term's instalments of the owner, unnumbered and without IDs.
func (r *resolvedTerm) instalments(ctx context.Context, owner Owner) ([]*Instalment, error) {
	var builders []*instalmentBuilder
	if r.term.Kind == types.BillingTermKindOnce {
		builders = append(builders, NewOwnerInstalment(ctx, owner).
			WithDelivery(r.delivery).
			WithBillTrigger(r.billTrigger).
			WithBillOn(r.billOn).
			WithServicePeriod(r.servicePeriod))
	} else {
		for _, step := range r.steps {
			billOn := step.End
			if r.cadence == types.InvoiceCadenceAdvance {
				billOn = step.Start
			}

			builders = append(builders, NewOwnerInstalment(ctx, owner).
				WithDelivery(r.delivery).
				WithBillOn(billOn).
				WithServicePeriod(step.Period))
		}
	}

	label := r.term.LabelTemplate()
	amounts := r.instalmentAmounts(owner.BillingCurrency())
	out := make([]*Instalment, 0, len(builders))
	for n, b := range builders {
		if !amounts[n].IsPositive() {
			return nil, ierr.Validationf(
				termErrorFormat, r.index, "value",
				fmt.Sprintf("instalment %d of %d comes to %s; every instalment must be greater than 0", n+1, len(amounts), amounts[n]),
			)
		}

		out = append(out, b.WithBillingTermID(r.term.ID).WithAmount(amounts[n]).WithInvoiceLabel(label).Build())
	}

	return out, nil
}

// instalmentAmounts splits the term's total over its instalments: the term's value each on a fixed term; evenly or in
// proportion to the steps' values on a remainder term, the last absorbing the rounding.
func (r *resolvedTerm) instalmentAmounts(currency string) []decimal.Decimal {
	count := r.instalmentCount()
	if r.term.ValueType == types.BillingTermValueFixed {
		return lo.Times(count, func(int) decimal.Decimal { return lo.FromPtr(r.term.Value) })
	}

	stepsValue := sumValues(r.steps)
	amounts := make([]decimal.Decimal, count)
	allocated := decimal.Zero
	for n := range count - 1 {
		share := r.total.Div(decimal.NewFromInt(int64(count)))
		if r.split == types.BillingTermSplitProportional {
			share = r.total.Mul(r.steps[n].Amount).Div(stepsValue)
		}

		amounts[n] = types.RoundToCurrencyPrecision(share, currency)
		allocated = allocated.Add(amounts[n])
	}

	amounts[count-1] = r.total.Sub(allocated)
	return amounts
}

func sumValues(values []PeriodValue) decimal.Decimal {
	sum := decimal.Zero
	for _, v := range values {
		sum = sum.Add(v.Amount)
	}

	return sum
}
