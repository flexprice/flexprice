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

const (
	maxDescriptionLength = 255

	defaultOnceDescription      = "Commitment payment"
	defaultRecurringDescription = "Instalment {n} of {of}"

	// termErrorFormat names the term's index, then the field, then the reason.
	termErrorFormat = "billing_terms[%d].%s: %s"
)

// resolvedTerm is a validated term with its defaults applied, its steps cut and its amount resolved.
type resolvedTerm struct {
	index int
	term  *Term

	billTrigger types.InstalmentBillTrigger
	billOn      time.Time
	covers      types.Period

	cadence types.InvoiceCadence
	split   types.BillingTermSplit
	steps   []PeriodValue

	// amount is the term's share of the total; instalmentAmount is the amount of each instalment of a fixed term.
	amount           decimal.Decimal
	instalmentAmount decimal.Decimal
}

// Expand validates the terms against the owner and its period values and returns their instalments in billing order.
// It fills Amount on terms entered as a percentage.
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
		r, err := resolveTerm(i, t, collection, periods)
		if err != nil {
			return nil, err
		}

		resolved = append(resolved, r)
	}

	if err := resolveAmounts(resolved, total, currency); err != nil {
		return nil, err
	}

	count := lo.SumBy(resolved, func(r *resolvedTerm) int { return r.instalmentCount() })
	if count > MaxInstalments {
		return nil, ierr.Validationf("billing_terms: expand to %d instalments, more than the %d allowed", count, MaxInstalments)
	}

	template := Instalment{
		OwnerType: owner.OwnerType(),
		OwnerID:   owner.OwnerID(),

		CustomerID:     owner.BilledCustomerID(),
		SubscriptionID: owner.BilledSubscriptionID(),
		Currency:       currency,

		Kind:             types.InstalmentKindInstalment,
		Delivery:         delivery,
		InstalmentStatus: types.InstalmentStatusScheduled,

		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

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

	for _, r := range resolved {
		if r.term.Percentage != nil {
			r.term.Amount = lo.ToPtr(r.instalmentAmount)
		}
	}

	return instalments, nil
}

// resolveTerm validates one term against the collection period and applies the defaults of its kind.
func resolveTerm(index int, t *Term, collection types.Period, periods []PeriodValue) (*resolvedTerm, error) {
	switch t.ValueType {
	case types.BillingTermValueFixed:
		if (t.Amount == nil) == (t.Percentage == nil) {
			return nil, ierr.Validationf(termErrorFormat, index, "amount", "a fixed term needs exactly one of amount or percentage")
		}

		if t.Amount != nil && !t.Amount.IsPositive() {
			return nil, ierr.Validationf(termErrorFormat, index, "amount", "must be greater than 0")
		}

		if t.Percentage != nil && (!t.Percentage.IsPositive() || t.Percentage.GreaterThan(decimal.NewFromInt(100))) {
			return nil, ierr.Validationf(termErrorFormat, index, "percentage", "must be greater than 0 and at most 100")
		}
	case types.BillingTermValueRemainder:
		if t.Amount != nil {
			return nil, ierr.Validationf(termErrorFormat, index, "amount", "a remainder term takes no amount")
		}

		if t.Percentage != nil {
			return nil, ierr.Validationf(termErrorFormat, index, "percentage", "a remainder term takes no percentage")
		}
	default:
		return nil, ierr.Validationf(termErrorFormat, index, "value_type", "must be fixed or remainder")
	}

	switch t.Kind {
	case types.BillingTermKindOnce:
		return resolveOnce(index, t, collection)
	case types.BillingTermKindRecurring:
		return resolveRecurring(index, t, collection, periods)
	default:
		return nil, ierr.Validationf(termErrorFormat, index, "kind", "must be once or recurring")
	}
}

// resolveOnce checks a once term's dates; bill_on and coverage default to the collection period.
func resolveOnce(index int, t *Term, collection types.Period) (*resolvedTerm, error) {
	unused := []lo.Entry[string, bool]{
		{Key: "starts_on", Value: t.StartsOn != nil},
		{Key: "ends_on", Value: t.EndsOn != nil},
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

		billTrigger: lo.FromPtrOr(t.BillTrigger, types.InstalmentBillTriggerDate),
		billOn:      lo.FromPtrOr(t.BillOn, collection.Start),
		covers: types.Period{
			Start: lo.FromPtrOr(t.CoversFrom, collection.Start),
			End:   lo.FromPtrOr(t.CoversTo, collection.End),
		},
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

	if !r.covers.Start.Before(r.covers.End) {
		return nil, ierr.Validationf(termErrorFormat, index, "covers_from", "must be before covers_to")
	}

	if r.covers.Start.Before(collection.Start) {
		return nil, ierr.Validationf(termErrorFormat, index, "covers_from", "must fall within the collection period")
	}

	if r.covers.End.After(collection.End) {
		return nil, ierr.Validationf(termErrorFormat, index, "covers_to", "must fall within the collection period")
	}

	return r, nil
}

// resolveRecurring checks a recurring term and cuts its steps; starts_on and ends_on default to the collection period.
func resolveRecurring(index int, t *Term, collection types.Period, periods []PeriodValue) (*resolvedTerm, error) {
	unused := []lo.Entry[string, bool]{
		{Key: "bill_trigger", Value: t.BillTrigger != nil},
		{Key: "bill_on", Value: t.BillOn != nil},
		{Key: "covers_from", Value: t.CoversFrom != nil},
		{Key: "covers_to", Value: t.CoversTo != nil},
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
		Start: lo.FromPtrOr(t.StartsOn, collection.Start),
		End:   lo.FromPtrOr(t.EndsOn, collection.End),
	}

	if !lo.ContainsBy(periods, func(p PeriodValue) bool { return p.Start.Equal(span.Start) }) {
		return nil, ierr.Validationf(termErrorFormat, index, "starts_on", "must be the start of a billing period")
	}

	if !lo.ContainsBy(periods, func(p PeriodValue) bool { return p.End.Equal(span.End) }) {
		return nil, ierr.Validationf(termErrorFormat, index, "ends_on", "must be the end of a billing period")
	}

	if !span.Start.Before(span.End) {
		return nil, ierr.Validationf(termErrorFormat, index, "starts_on", "must be before ends_on")
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

// resolveAmounts rounds fixed amounts to the currency and gives the remainder term whatever the others leave of total.
func resolveAmounts(terms []*resolvedTerm, total decimal.Decimal, currency string) error {
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

		perInstalment := lo.FromPtr(r.term.Amount)
		if r.term.Percentage != nil {
			perInstalment = total.Mul(*r.term.Percentage).Div(decimal.NewFromInt(100))
		}

		r.instalmentAmount = types.RoundToCurrencyPrecision(perInstalment, currency)
		r.amount = r.instalmentAmount.Mul(decimal.NewFromInt(int64(r.instalmentCount())))
		others = others.Add(r.amount)
	}

	if remainder == nil {
		if !others.Equal(total) {
			return ierr.Validationf(
				termErrorFormat, len(terms)-1, "amount",
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
	description := lo.FromPtr(r.term.Description)
	if r.term.Kind == types.BillingTermKindOnce {
		i := template
		i.BillTrigger = r.billTrigger
		i.BillOn = r.billOn
		i.Amount = r.amount
		i.ServicePeriodStart = r.covers.Start
		i.ServicePeriodEnd = r.covers.End
		out = append(out, &i)

		description = lo.CoalesceOrEmpty(description, defaultOnceDescription)
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

		description = lo.CoalesceOrEmpty(description, defaultRecurringDescription)
	}

	for n, i := range out {
		i.BillingTermID = lo.ToPtr(r.term.ID)
		i.Description = strings.NewReplacer("{n}", strconv.Itoa(n+1), "{of}", strconv.Itoa(len(out))).Replace(description)
		if utf8.RuneCountInString(i.Description) > maxDescriptionLength {
			return nil, ierr.Validationf(termErrorFormat, r.index, "description", "must be at most 255 characters")
		}

		if !i.Amount.IsPositive() {
			return nil, ierr.Validationf(
				termErrorFormat, r.index, "amount",
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
