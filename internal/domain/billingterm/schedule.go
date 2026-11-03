package billingterm

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	// MaxTerms is the most billing terms an owner can have.
	MaxTerms = 20

	// MaxInstalments is the most instalments an owner's terms can expand to.
	MaxInstalments = 500
)

// DefaultInvoiceLabel is the invoice label of an instalment given none. NumberLabels fills {n} and {of}.
const DefaultInvoiceLabel = "Instalment {n} of {of}"

const (
	maxInvoiceLabelLength = 255

	// termErrorFormat names the term's index, then the field, then the reason.
	termErrorFormat = "billing_terms[%d].%s: %s"
)

// resolvedTerm is a validated term with its defaults applied, its steps cut and its amount resolved.
type resolvedTerm struct {
	index int
	term  *Term

	billTrigger   types.InstalmentBillTrigger
	billOn        time.Time
	servicePeriod types.Period

	cadence types.InvoiceCadence
	split   types.BillingTermSplit
	steps   []PeriodValue

	// amount is the term's share of the total; instalmentAmount is the amount of each instalment of a fixed term.
	amount           decimal.Decimal
	instalmentAmount decimal.Decimal
}

// Expand validates the terms against the owner and its period values and returns their instalments in billing order.
// Their labels keep {n} and {of} until NumberLabels numbers them among the owner's other instalments.
func Expand(ctx context.Context, owner Owner, periods []PeriodValue, terms []*Term) ([]*Instalment, error) {
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
		r, err := resolveTerm(i, t, collection, owner.TermStart(), currency, periods)
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

	template := InstalmentTemplate(ctx, owner)

	instalments := make([]*Instalment, 0, count)
	for _, r := range resolved {
		rendered, err := r.instalments(template, currency)
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

// NumberLabels fills {n} and {of} in each invoice label with the instalment's position in plan and the plan's length.
// plan is all of an owner's instalments in billing order; labels already numbered are left as they are.
func NumberLabels(plan []*Instalment) {
	of := strconv.Itoa(len(plan))
	for n, i := range plan {
		i.InvoiceLabel = strings.NewReplacer("{n}", strconv.Itoa(n+1), "{of}", of).Replace(i.InvoiceLabel)
	}
}

// InstalmentTemplate is a scheduled instalment of the owner with the owner's fields filled in. The caller sets the ID,
// amount, dates, trigger, invoice label and service period.
func InstalmentTemplate(ctx context.Context, owner Owner) Instalment {
	return Instalment{
		OwnerType: owner.OwnerType(),
		OwnerID:   owner.OwnerID(),

		CustomerID:     owner.BilledCustomerID(),
		SubscriptionID: lo.EmptyableToPtr(owner.BilledSubscriptionID()),
		Currency:       owner.BillingCurrency(),

		Kind:             types.InstalmentKindInstalment,
		Delivery:         owner.InstalmentDelivery(),
		InstalmentStatus: types.InstalmentStatusScheduled,

		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

// resolveTerm validates one term against the collection period and applies the defaults of its kind; a term without
// dates starts at termStart.
func resolveTerm(
	index int,
	t *Term,
	collection types.Period,
	termStart time.Time,
	currency string,
	periods []PeriodValue,
) (*resolvedTerm, error) {
	if utf8.RuneCountInString(lo.FromPtr(t.InvoiceLabel)) > maxInvoiceLabelLength {
		return nil, ierr.Validationf(termErrorFormat, index, "invoice_label", "must be at most 255 characters")
	}

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

	switch t.Kind {
	case types.BillingTermKindOnce:
		return resolveOnce(index, t, collection, termStart)
	case types.BillingTermKindRecurring:
		return resolveRecurring(index, t, collection, termStart, periods)
	default:
		return nil, ierr.Validationf(termErrorFormat, index, "kind", "must be once or recurring")
	}
}

// resolveOnce checks a once term's dates; bill_on defaults to termStart. The collection period is the service period of
// its instalment.
func resolveOnce(index int, t *Term, collection types.Period, termStart time.Time) (*resolvedTerm, error) {
	unused := []lo.Entry[string, bool]{
		{Key: "start_date", Value: t.StartDate != nil},
		{Key: "end_date", Value: t.EndDate != nil},
		{Key: "period_count", Value: t.PeriodCount != nil},
		{Key: "invoice_cadence", Value: t.InvoiceCadence != nil},
		{Key: "split", Value: t.Split != nil},
	}
	for _, field := range unused {
		if field.Value {
			return nil, ierr.Validationf(termErrorFormat, index, field.Key, "is not used by once terms")
		}
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

// resolveRecurring checks a recurring term and cuts its steps; start_date defaults to termStart and end_date to the end
// of the collection period.
func resolveRecurring(
	index int,
	t *Term,
	collection types.Period,
	termStart time.Time,
	periods []PeriodValue,
) (*resolvedTerm, error) {
	unused := []lo.Entry[string, bool]{
		{Key: "bill_trigger", Value: t.BillTrigger != nil},
		{Key: "bill_on", Value: t.BillOn != nil},
	}
	for _, field := range unused {
		if field.Value {
			return nil, ierr.Validationf(termErrorFormat, index, field.Key, "is not used by recurring terms")
		}
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

	span := types.Period{
		Start: lo.FromPtrOr(t.StartDate, termStart),
		End:   lo.FromPtrOr(t.EndDate, collection.End),
	}

	if !lo.ContainsBy(periods, func(p PeriodValue) bool { return p.Start.Equal(span.Start) }) {
		return nil, ierr.Validationf(termErrorFormat, index, "start_date", "must be the start of a billing period")
	}

	if !lo.ContainsBy(periods, func(p PeriodValue) bool { return p.End.Equal(span.End) }) {
		return nil, ierr.Validationf(termErrorFormat, index, "end_date", "must be the end of a billing period")
	}

	if !span.Start.Before(span.End) {
		return nil, ierr.Validationf(termErrorFormat, index, "start_date", "must be before end_date")
	}

	steps := cutSteps(periods, span, periodCount)
	if split == types.BillingTermSplitProportional && !sumValues(steps).IsPositive() {
		return nil, ierr.Validationf(termErrorFormat, index, "split", "proportional needs billing periods with a value")
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

		r.instalmentAmount = lo.FromPtr(r.term.Value)
		r.amount = r.instalmentAmount.Mul(decimal.NewFromInt(int64(r.instalmentCount())))
		others = others.Add(r.amount)
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

	remainder.amount = total.Sub(others)
	if !remainder.amount.IsPositive() {
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

// instalments renders the term's instalments onto copies of template, which carries the owner's fields.
func (r *resolvedTerm) instalments(template Instalment, currency string) ([]*Instalment, error) {
	out := make([]*Instalment, 0, r.instalmentCount())
	if r.term.Kind == types.BillingTermKindOnce {
		i := template
		i.BillTrigger = r.billTrigger
		i.BillOn = r.billOn
		i.Amount = r.amount
		i.ServicePeriodStart = r.servicePeriod.Start
		i.ServicePeriodEnd = r.servicePeriod.End
		out = append(out, &i)
	} else {
		amounts := r.stepAmounts(currency)
		for n, step := range r.steps {
			i := template
			i.BillTrigger = types.InstalmentBillTriggerDate
			i.BillOn = step.End
			if r.cadence == types.InvoiceCadenceAdvance {
				i.BillOn = step.Start
			}

			i.Amount = amounts[n]
			i.ServicePeriodStart = step.Start
			i.ServicePeriodEnd = step.End
			out = append(out, &i)
		}
	}

	for n, i := range out {
		i.BillingTermID = lo.ToPtr(r.term.ID)
		i.InvoiceLabel = lo.CoalesceOrEmpty(lo.FromPtr(r.term.InvoiceLabel), DefaultInvoiceLabel)
		if !i.Amount.IsPositive() {
			return nil, ierr.Validationf(
				termErrorFormat, r.index, "value",
				fmt.Sprintf("instalment %d of %d comes to %s; every instalment must be greater than 0", n+1, len(out), i.Amount),
			)
		}
	}

	return out, nil
}

// stepAmounts splits the term's amount over its steps; on a remainder term the last step absorbs the rounding.
func (r *resolvedTerm) stepAmounts(currency string) []decimal.Decimal {
	if r.term.ValueType == types.BillingTermValueFixed {
		return lo.Times(len(r.steps), func(int) decimal.Decimal { return r.instalmentAmount })
	}

	termValue := sumValues(r.steps)
	stepCount := decimal.NewFromInt(int64(len(r.steps)))
	amounts := make([]decimal.Decimal, len(r.steps))
	allocated := decimal.Zero
	for n, step := range r.steps[:len(r.steps)-1] {
		share := r.amount.Div(stepCount)
		if r.split == types.BillingTermSplitProportional {
			share = r.amount.Mul(step.Amount).Div(termValue)
		}

		amounts[n] = types.RoundToCurrencyPrecision(share, currency)
		allocated = allocated.Add(amounts[n])
	}

	amounts[len(amounts)-1] = r.amount.Sub(allocated)

	return amounts
}

func sumValues(values []PeriodValue) decimal.Decimal {
	sum := decimal.Zero
	for _, v := range values {
		sum = sum.Add(v.Amount)
	}

	return sum
}
