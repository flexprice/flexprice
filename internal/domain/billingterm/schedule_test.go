package billingterm

import (
	"context"
	"strings"
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type testOwner struct {
	currency string
	delivery types.InstalmentDelivery
	period   types.Period
	total    decimal.Decimal
}

func (o testOwner) OwnerType() types.BillingTermOwnerType { return types.BillingTermOwnerCommitment }

func (o testOwner) OwnerID() string { return "cmt_1" }

func (o testOwner) BilledCustomerID() string { return "cust_1" }

func (o testOwner) BilledSubscriptionID() string { return "subs_1" }

func (o testOwner) BillingCurrency() string { return o.currency }

func (o testOwner) InstalmentDelivery() types.InstalmentDelivery { return o.delivery }

func (o testOwner) CollectionPeriod() types.Period { return o.period }

func (o testOwner) CollectionTotal() decimal.Decimal { return o.total }

type instalmentWant struct {
	billOn  time.Time
	amount  string
	service types.Period
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func period(start, end time.Time) types.Period {
	return types.Period{Start: start, End: end}
}

func dec(s string) decimal.Decimal {
	return decimal.RequireFromString(s)
}

// monthly returns n monthly period values from start, each worth amount.
func monthly(start time.Time, n int, amount string) []PeriodValue {
	out := make([]PeriodValue, 0, n)
	for i := range n {
		out = append(out, PeriodValue{
			Period: period(start.AddDate(0, i, 0), start.AddDate(0, i+1, 0)),
			Amount: dec(amount),
		})
	}

	return out
}

// monthlyWants returns one instalment per month from start, billed at the month's start or end.
func monthlyWants(start time.Time, amounts []string, cadence types.InvoiceCadence) []instalmentWant {
	out := make([]instalmentWant, 0, len(amounts))
	for i, amount := range amounts {
		service := period(start.AddDate(0, i, 0), start.AddDate(0, i+1, 0))
		billOn := service.End
		if cadence == types.InvoiceCadenceAdvance {
			billOn = service.Start
		}

		out = append(out, instalmentWant{billOn: billOn, amount: amount, service: service})
	}

	return out
}

func usd(total string, start, end time.Time) testOwner {
	return testOwner{
		currency: "USD",
		delivery: types.InstalmentDeliverySubscriptionInvoice,
		period:   period(start, end),
		total:    dec(total),
	}
}

func onceFixed(amount string) *Term {
	return &Term{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_TERM),
		Kind:      types.BillingTermKindOnce,
		ValueType: types.BillingTermValueFixed,
		Amount:    lo.ToPtr(dec(amount)),
	}
}

func oncePercentage(percentage string) *Term {
	return &Term{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_TERM),
		Kind:       types.BillingTermKindOnce,
		ValueType:  types.BillingTermValueFixed,
		Percentage: lo.ToPtr(dec(percentage)),
	}
}

func onceRemainder() *Term {
	return &Term{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_TERM),
		Kind:      types.BillingTermKindOnce,
		ValueType: types.BillingTermValueRemainder,
	}
}

func recurringFixed(amount string, cadence types.InvoiceCadence) *Term {
	return &Term{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_TERM),
		Kind:           types.BillingTermKindRecurring,
		ValueType:      types.BillingTermValueFixed,
		Amount:         lo.ToPtr(dec(amount)),
		InvoiceCadence: lo.ToPtr(cadence),
	}
}

func recurringRemainder(cadence types.InvoiceCadence) *Term {
	return &Term{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_TERM),
		Kind:           types.BillingTermKindRecurring,
		ValueType:      types.BillingTermValueRemainder,
		InvoiceCadence: lo.ToPtr(cadence),
	}
}

func with(term *Term, change func(t *Term)) *Term {
	change(term)
	return term
}

func repeat(amount string, n int) []string {
	return lo.Times(n, func(int) string { return amount })
}

func TestExpand(t *testing.T) {
	jan2027 := date(2027, time.January, 1)
	jan2028 := date(2028, time.January, 1)

	// A14 starts mid-January: the first period is worth 17/31 of a month.
	a14Start := date(2027, time.January, 15)
	a14Periods := append([]PeriodValue{{
		Period: period(a14Start, date(2027, time.February, 1)),
		Amount: dec("100").Mul(dec("17")).Div(dec("31")),
	}}, monthly(date(2027, time.February, 1), 11, "100")...)
	a14Once := instalmentWant{billOn: a14Start, amount: "300", service: period(a14Start, jan2028)}
	a14First := period(a14Start, date(2027, time.February, 1))

	tests := []struct {
		name    string
		owner   testOwner
		periods []PeriodValue
		terms   []*Term
		want    []instalmentWant
		check   func(t *testing.T, terms []*Term, got []*Instalment)
	}{
		{
			name:    "A3 once fixed then monthly remainder in arrear",
			owner:   usd("1000", jan2027, date(2027, time.November, 1)),
			periods: monthly(jan2027, 10, "100"),
			terms: []*Term{
				onceFixed("300"),
				recurringRemainder(types.InvoiceCadenceArrear),
			},
			want: append(
				[]instalmentWant{{billOn: jan2027, amount: "300", service: period(jan2027, date(2027, time.November, 1))}},
				monthlyWants(jan2027, repeat("70", 10), types.InvoiceCadenceArrear)...,
			),
			check: func(t *testing.T, terms []*Term, got []*Instalment) {
				require.Equal(t, "Commitment payment", got[0].Description)
				require.Equal(t, "Instalment 2 of 10", got[2].Description)
				require.Equal(t, "Instalment 10 of 10", got[10].Description)
				require.Equal(t, lo.ToPtr(terms[0].ID), got[0].BillingTermID)
				require.Equal(t, lo.ToPtr(terms[1].ID), got[1].BillingTermID)
			},
		},
		{
			name:    "A3 with a custom description",
			owner:   usd("1000", jan2027, date(2027, time.November, 1)),
			periods: monthly(jan2027, 10, "100"),
			terms: []*Term{
				onceFixed("300"),
				with(recurringRemainder(types.InvoiceCadenceArrear), func(t *Term) {
					t.Description = lo.ToPtr("Payment {n}/{of}")
				}),
			},
			want: append(
				[]instalmentWant{{billOn: jan2027, amount: "300", service: period(jan2027, date(2027, time.November, 1))}},
				monthlyWants(jan2027, repeat("70", 10), types.InvoiceCadenceArrear)...,
			),
			check: func(t *testing.T, _ []*Term, got []*Instalment) {
				require.Equal(t, "Payment 1/10", got[1].Description)
				require.Equal(t, "Payment 10/10", got[10].Description)
			},
		},
		{
			name:    "A4 percentages and a remainder at term end",
			owner:   usd("1200", jan2027, jan2028),
			periods: monthly(jan2027, 12, "100"),
			terms: []*Term{
				oncePercentage("30"),
				with(oncePercentage("50"), func(t *Term) { t.BillOn = lo.ToPtr(date(2027, time.July, 1)) }),
				with(onceRemainder(), func(t *Term) { t.BillOn = lo.ToPtr(jan2028) }),
			},
			want: []instalmentWant{
				{billOn: jan2027, amount: "360", service: period(jan2027, jan2028)},
				{billOn: date(2027, time.July, 1), amount: "600", service: period(jan2027, jan2028)},
				{billOn: jan2028, amount: "240", service: period(jan2027, jan2028)},
			},
			check: func(t *testing.T, terms []*Term, _ []*Instalment) {
				require.True(t, terms[0].Amount.Equal(dec("360")), "got %s", terms[0].Amount)
				require.True(t, terms[1].Amount.Equal(dec("600")), "got %s", terms[1].Amount)
				require.Nil(t, terms[2].Amount)
			},
		},
		{
			name:    "A6 deposit then monthly fixed from April",
			owner:   usd("1200", jan2027, jan2028),
			periods: monthly(jan2027, 12, "100"),
			terms: []*Term{
				with(onceFixed("300"), func(t *Term) {
					t.CoversFrom = lo.ToPtr(jan2027)
					t.CoversTo = lo.ToPtr(date(2027, time.April, 1))
				}),
				with(recurringFixed("100", types.InvoiceCadenceArrear), func(t *Term) {
					t.StartsOn = lo.ToPtr(date(2027, time.April, 1))
				}),
			},
			want: append(
				[]instalmentWant{{billOn: jan2027, amount: "300", service: period(jan2027, date(2027, time.April, 1))}},
				monthlyWants(date(2027, time.April, 1), repeat("100", 9), types.InvoiceCadenceArrear)...,
			),
		},
		{
			name:    "A7 quarterly in advance",
			owner:   usd("1200", jan2027, jan2028),
			periods: monthly(jan2027, 12, "100"),
			terms: []*Term{
				with(recurringFixed("300", types.InvoiceCadenceAdvance), func(t *Term) { t.PeriodCount = lo.ToPtr(3) }),
			},
			want: []instalmentWant{
				{billOn: jan2027, amount: "300", service: period(jan2027, date(2027, time.April, 1))},
				{billOn: date(2027, time.April, 1), amount: "300", service: period(date(2027, time.April, 1), date(2027, time.July, 1))},
				{billOn: date(2027, time.July, 1), amount: "300", service: period(date(2027, time.July, 1), date(2027, time.October, 1))},
				{billOn: date(2027, time.October, 1), amount: "300", service: period(date(2027, time.October, 1), jan2028)},
			},
		},
		{
			name:    "A14 mid-period start, even split",
			owner:   usd("1154.84", a14Start, jan2028),
			periods: a14Periods,
			terms: []*Term{
				onceFixed("300"),
				recurringRemainder(types.InvoiceCadenceArrear),
			},
			want: append(
				[]instalmentWant{a14Once, {billOn: a14First.End, amount: "71.24", service: a14First}},
				monthlyWants(date(2027, time.February, 1), append(repeat("71.24", 10), "71.20"), types.InvoiceCadenceArrear)...,
			),
		},
		{
			name:    "A14 mid-period start, proportional split",
			owner:   usd("1154.84", a14Start, jan2028),
			periods: a14Periods,
			terms: []*Term{
				onceFixed("300"),
				with(recurringRemainder(types.InvoiceCadenceArrear), func(t *Term) {
					t.Split = lo.ToPtr(types.BillingTermSplitProportional)
				}),
			},
			want: append(
				[]instalmentWant{a14Once, {billOn: a14First.End, amount: "40.59", service: a14First}},
				monthlyWants(date(2027, time.February, 1), append(repeat("74.02", 10), "74.05"), types.InvoiceCadenceArrear)...,
			),
		},
		{
			name:    "B2 half upfront, the rest on a milestone",
			owner:   usd("1200", jan2027, jan2028),
			periods: monthly(jan2027, 12, "100"),
			terms: []*Term{
				oncePercentage("50"),
				with(onceRemainder(), func(t *Term) { t.BillTrigger = lo.ToPtr(types.InstalmentBillTriggerManual) }),
			},
			want: []instalmentWant{
				{billOn: jan2027, amount: "600", service: period(jan2027, jan2028)},
				{billOn: jan2028, amount: "600", service: period(jan2027, jan2028)},
			},
			check: func(t *testing.T, _ []*Term, got []*Instalment) {
				require.Equal(t, types.InstalmentBillTriggerDate, got[0].BillTrigger)
				require.Equal(t, types.InstalmentBillTriggerManual, got[1].BillTrigger)
				require.Nil(t, got[1].TriggeredAt)
			},
		},
		{
			name:    "B8 rounding, last instalment absorbs",
			owner:   usd("1000", jan2027, date(2027, time.April, 1)),
			periods: monthly(jan2027, 3, "100"),
			terms:   []*Term{recurringRemainder(types.InvoiceCadenceArrear)},
			want:    monthlyWants(jan2027, []string{"333.33", "333.33", "333.34"}, types.InvoiceCadenceArrear),
		},
		{
			name: "JPY rounds to whole yen",
			owner: testOwner{
				currency: "JPY",
				delivery: types.InstalmentDeliverySeparateInvoice,
				period:   period(jan2027, date(2027, time.April, 1)),
				total:    dec("1000"),
			},
			periods: monthly(jan2027, 3, "100"),
			terms:   []*Term{recurringRemainder(types.InvoiceCadenceArrear)},
			want:    monthlyWants(jan2027, []string{"333", "333", "334"}, types.InvoiceCadenceArrear),
		},
		{
			name:    "ties on bill_on keep term order",
			owner:   usd("1200", jan2027, jan2028),
			periods: monthly(jan2027, 12, "100"),
			terms: []*Term{
				with(recurringFixed("50", types.InvoiceCadenceAdvance), func(t *Term) { t.PeriodCount = lo.ToPtr(6) }),
				onceRemainder(),
			},
			want: []instalmentWant{
				{billOn: jan2027, amount: "50", service: period(jan2027, date(2027, time.July, 1))},
				{billOn: jan2027, amount: "1100", service: period(jan2027, jan2028)},
				{billOn: date(2027, time.July, 1), amount: "50", service: period(date(2027, time.July, 1), jan2028)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), types.CtxEnvironmentID, "env_1")
			ctx = context.WithValue(ctx, types.CtxTenantID, "tenant_1")

			got, err := Expand(ctx, tt.owner, tt.periods, tt.terms)
			require.NoError(t, err)

			gotWants := lo.Map(got, func(i *Instalment, _ int) instalmentWant {
				return instalmentWant{
					billOn:  i.BillOn,
					amount:  i.Amount.String(),
					service: period(i.ServicePeriodStart, i.ServicePeriodEnd),
				}
			})
			for i := range tt.want {
				tt.want[i].amount = dec(tt.want[i].amount).String()
			}
			require.Equal(t, tt.want, gotWants)

			sum := decimal.Zero
			for _, i := range got {
				sum = sum.Add(i.Amount)
				require.True(t, strings.HasPrefix(i.ID, types.UUID_PREFIX_BILLING_INSTALMENT+"_"), i.ID)
				require.Equal(t, types.BillingTermOwnerCommitment, i.OwnerType)
				require.Equal(t, "cmt_1", i.OwnerID)
				require.Equal(t, "cust_1", i.CustomerID)
				require.Equal(t, "subs_1", i.SubscriptionID)
				require.Equal(t, tt.owner.currency, i.Currency)
				require.Equal(t, types.InstalmentKindInstalment, i.Kind)
				require.Equal(t, tt.owner.delivery, i.Delivery)
				require.Equal(t, types.InstalmentStatusScheduled, i.InstalmentStatus)
				require.Equal(t, "env_1", i.EnvironmentID)
				require.Equal(t, "tenant_1", i.TenantID)
				require.Equal(t, types.StatusPublished, i.Status)
				require.NotNil(t, i.BillingTermID)
			}
			require.True(t, sum.Equal(tt.owner.total), "instalments add up to %s, want %s", sum, tt.owner.total)
			require.Len(t, lo.UniqBy(got, func(i *Instalment) string { return i.ID }), len(got))
			require.IsIncreasing(t, lo.Map(got, func(i *Instalment, _ int) string { return i.ID }))

			if tt.check != nil {
				tt.check(t, tt.terms, got)
			}
		})
	}
}

func TestExpand_Validation(t *testing.T) {
	jan2027 := date(2027, time.January, 1)
	jan2028 := date(2028, time.January, 1)
	year := usd("1200", jan2027, jan2028)
	months := monthly(jan2027, 12, "100")

	daily := make([]PeriodValue, 0, 501)
	for i := range 501 {
		daily = append(daily, PeriodValue{
			Period: period(jan2027.AddDate(0, 0, i), jan2027.AddDate(0, 0, i+1)),
			Amount: dec("1"),
		})
	}

	tests := []struct {
		name    string
		owner   testOwner
		periods []PeriodValue
		terms   []*Term
		wantMsg string
	}{
		{
			name:    "owner without an end",
			owner:   usd("1200", jan2027, time.Time{}),
			periods: months,
			terms:   []*Term{onceRemainder()},
			wantMsg: "billing_terms.owner:",
		},
		{
			name:    "owner without an instalment delivery",
			owner:   testOwner{currency: "USD", period: period(jan2027, jan2028), total: dec("1200")},
			periods: months,
			terms:   []*Term{onceRemainder()},
			wantMsg: "billing_terms.owner:",
		},
		{
			name:    "owner with a zero total",
			owner:   usd("0", jan2027, jan2028),
			periods: months,
			terms:   []*Term{onceRemainder()},
			wantMsg: "billing_terms.owner:",
		},
		{
			name:    "owner total below currency precision",
			owner:   usd("1200.005", jan2027, jan2028),
			periods: months,
			terms:   []*Term{onceRemainder()},
			wantMsg: "billing_terms.owner:",
		},
		{
			name:    "no terms",
			owner:   year,
			periods: months,
			wantMsg: "billing_terms: must hold 1 to 20 terms",
		},
		{
			name:    "21 terms",
			owner:   year,
			periods: months,
			terms:   lo.Times(21, func(int) *Term { return onceFixed("1") }),
			wantMsg: "billing_terms: must hold 1 to 20 terms",
		},
		{
			name:    "unknown kind",
			owner:   year,
			periods: months,
			terms:   []*Term{with(onceRemainder(), func(t *Term) { t.Kind = "weekly" })},
			wantMsg: "billing_terms[0].kind:",
		},
		{
			name:    "percentage value type is input only",
			owner:   year,
			periods: months,
			terms: []*Term{with(oncePercentage("100"), func(t *Term) {
				t.ValueType = types.BillingTermValuePercentage
			})},
			wantMsg: "billing_terms[0].value_type:",
		},
		{
			name:    "recurring with bill_on",
			owner:   year,
			periods: months,
			terms: []*Term{with(recurringRemainder(types.InvoiceCadenceArrear), func(t *Term) {
				t.BillOn = lo.ToPtr(jan2027)
			})},
			wantMsg: "billing_terms[0].bill_on:",
		},
		{
			name:    "once with period_count",
			owner:   year,
			periods: months,
			terms:   []*Term{with(onceRemainder(), func(t *Term) { t.PeriodCount = lo.ToPtr(1) })},
			wantMsg: "billing_terms[0].period_count:",
		},
		{
			name:    "fixed with neither amount nor percentage",
			owner:   year,
			periods: months,
			terms:   []*Term{with(onceFixed("1200"), func(t *Term) { t.Amount = nil })},
			wantMsg: "billing_terms[0].amount:",
		},
		{
			name:    "fixed with both amount and percentage",
			owner:   year,
			periods: months,
			terms:   []*Term{with(onceFixed("1200"), func(t *Term) { t.Percentage = lo.ToPtr(dec("100")) })},
			wantMsg: "billing_terms[0].amount:",
		},
		{
			name:    "fixed amount of 0",
			owner:   year,
			periods: months,
			terms:   []*Term{onceFixed("0"), onceRemainder()},
			wantMsg: "billing_terms[0].amount:",
		},
		{
			name:    "fixed amount finer than the currency precision",
			owner:   usd("1200.12", jan2027, jan2028),
			periods: months,
			terms:   []*Term{recurringFixed("100.005", types.InvoiceCadenceArrear)},
			wantMsg: "billing_terms[0].amount: must be rounded to USD precision",
		},
		{
			name:    "percentage 0",
			owner:   year,
			periods: months,
			terms:   []*Term{oncePercentage("0"), onceRemainder()},
			wantMsg: "billing_terms[0].percentage:",
		},
		{
			name:    "percentage 100.01",
			owner:   year,
			periods: months,
			terms:   []*Term{oncePercentage("100.01")},
			wantMsg: "billing_terms[0].percentage:",
		},
		{
			name:    "remainder with an amount",
			owner:   year,
			periods: months,
			terms:   []*Term{with(onceRemainder(), func(t *Term) { t.Amount = lo.ToPtr(dec("1200")) })},
			wantMsg: "billing_terms[0].amount:",
		},
		{
			name:    "two remainders",
			owner:   year,
			periods: months,
			terms:   []*Term{onceRemainder(), recurringRemainder(types.InvoiceCadenceArrear)},
			wantMsg: "billing_terms[1].value_type:",
		},
		{
			name:    "fixed sum reaches the total with a remainder",
			owner:   year,
			periods: months,
			terms:   []*Term{onceFixed("1200"), onceRemainder()},
			wantMsg: "billing_terms[1].value_type:",
		},
		{
			name:    "fixed sum differs from the total without a remainder",
			owner:   year,
			periods: months,
			terms:   []*Term{onceFixed("300"), recurringFixed("70", types.InvoiceCadenceArrear)},
			wantMsg: "billing_terms[1].amount:",
		},
		{
			name:    "once bill_on after the period end",
			owner:   year,
			periods: months,
			terms:   []*Term{with(onceRemainder(), func(t *Term) { t.BillOn = lo.ToPtr(jan2028.Add(time.Second)) })},
			wantMsg: "billing_terms[0].bill_on:",
		},
		{
			name:    "once bill_on before the period start",
			owner:   year,
			periods: months,
			terms:   []*Term{with(onceRemainder(), func(t *Term) { t.BillOn = lo.ToPtr(jan2027.Add(-time.Second)) })},
			wantMsg: "billing_terms[0].bill_on:",
		},
		{
			name:    "manual with bill_on",
			owner:   year,
			periods: months,
			terms: []*Term{with(onceRemainder(), func(t *Term) {
				t.BillTrigger = lo.ToPtr(types.InstalmentBillTriggerManual)
				t.BillOn = lo.ToPtr(jan2027)
			})},
			wantMsg: "billing_terms[0].bill_on:",
		},
		{
			name:    "unknown bill_trigger",
			owner:   year,
			periods: months,
			terms: []*Term{with(onceRemainder(), func(t *Term) {
				t.BillTrigger = lo.ToPtr(types.InstalmentBillTrigger("someday"))
			})},
			wantMsg: "billing_terms[0].bill_trigger:",
		},
		{
			name:    "covers_from not before covers_to",
			owner:   year,
			periods: months,
			terms: []*Term{with(onceRemainder(), func(t *Term) {
				t.CoversFrom = lo.ToPtr(date(2027, time.April, 1))
				t.CoversTo = lo.ToPtr(date(2027, time.April, 1))
			})},
			wantMsg: "billing_terms[0].covers_from:",
		},
		{
			name:    "covers_to past the period end",
			owner:   year,
			periods: months,
			terms:   []*Term{with(onceRemainder(), func(t *Term) { t.CoversTo = lo.ToPtr(jan2028.AddDate(0, 1, 0)) })},
			wantMsg: "billing_terms[0].covers_to:",
		},
		{
			name:    "recurring without invoice_cadence",
			owner:   year,
			periods: months,
			terms: []*Term{with(recurringRemainder(types.InvoiceCadenceArrear), func(t *Term) {
				t.InvoiceCadence = nil
			})},
			wantMsg: "billing_terms[0].invoice_cadence:",
		},
		{
			name:    "recurring with an empty invoice_cadence",
			owner:   year,
			periods: months,
			terms: []*Term{with(recurringRemainder(types.InvoiceCadenceArrear), func(t *Term) {
				t.InvoiceCadence = lo.ToPtr(types.InvoiceCadence(""))
			})},
			wantMsg: "billing_terms[0].invoice_cadence:",
		},
		{
			name:    "recurring with an empty split",
			owner:   year,
			periods: months,
			terms: []*Term{with(recurringRemainder(types.InvoiceCadenceArrear), func(t *Term) {
				t.Split = lo.ToPtr(types.BillingTermSplit(""))
			})},
			wantMsg: "billing_terms[0].split:",
		},
		{
			name:    "starts_on not a period start",
			owner:   year,
			periods: months,
			terms: []*Term{with(recurringRemainder(types.InvoiceCadenceArrear), func(t *Term) {
				t.StartsOn = lo.ToPtr(date(2027, time.January, 15))
			})},
			wantMsg: "billing_terms[0].starts_on:",
		},
		{
			name:    "ends_on not a period end",
			owner:   year,
			periods: months,
			terms: []*Term{with(recurringRemainder(types.InvoiceCadenceArrear), func(t *Term) {
				t.EndsOn = lo.ToPtr(date(2027, time.June, 15))
			})},
			wantMsg: "billing_terms[0].ends_on:",
		},
		{
			name:    "starts_on not before ends_on",
			owner:   year,
			periods: months,
			terms: []*Term{with(recurringRemainder(types.InvoiceCadenceArrear), func(t *Term) {
				t.StartsOn = lo.ToPtr(date(2027, time.June, 1))
				t.EndsOn = lo.ToPtr(date(2027, time.June, 1))
			})},
			wantMsg: "billing_terms[0].starts_on:",
		},
		{
			name:    "period_count 0",
			owner:   year,
			periods: months,
			terms: []*Term{with(recurringRemainder(types.InvoiceCadenceArrear), func(t *Term) {
				t.PeriodCount = lo.ToPtr(0)
			})},
			wantMsg: "billing_terms[0].period_count:",
		},
		{
			name:    "split on a fixed term",
			owner:   year,
			periods: months,
			terms: []*Term{with(recurringFixed("100", types.InvoiceCadenceArrear), func(t *Term) {
				t.Split = lo.ToPtr(types.BillingTermSplitEven)
			})},
			wantMsg: "billing_terms[0].split:",
		},
		{
			name:    "proportional split over periods without value",
			owner:   year,
			periods: monthly(jan2027, 12, "0"),
			terms: []*Term{with(recurringRemainder(types.InvoiceCadenceArrear), func(t *Term) {
				t.Split = lo.ToPtr(types.BillingTermSplitProportional)
			})},
			wantMsg: "billing_terms[0].split:",
		},
		{
			name:    "description over 255 characters",
			owner:   year,
			periods: months,
			terms:   []*Term{with(onceRemainder(), func(t *Term) { t.Description = lo.ToPtr(strings.Repeat("a", 256)) })},
			wantMsg: "billing_terms[0].description:",
		},
		{
			name:    "an instalment rounds to 0",
			owner:   usd("0.10", jan2027, date(2027, time.July, 1)),
			periods: monthly(jan2027, 6, "100"),
			terms:   []*Term{recurringRemainder(types.InvoiceCadenceArrear)},
			wantMsg: "billing_terms[0].amount:",
		},
		{
			name:    "501 instalments",
			owner:   usd("501", jan2027, jan2027.AddDate(0, 0, 501)),
			periods: daily,
			terms:   []*Term{recurringRemainder(types.InvoiceCadenceArrear)},
			wantMsg: "billing_terms: expand to 501 instalments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Expand(context.Background(), tt.owner, tt.periods, tt.terms)

			require.Error(t, err)
			require.True(t, ierr.IsValidation(err), "want a validation error, got %v", err)
			require.Contains(t, err.Error(), tt.wantMsg)
			require.Nil(t, got)
		})
	}
}

func TestExpand_LeavesPercentageTermsUntouchedOnError(t *testing.T) {
	jan2027 := date(2027, time.January, 1)
	terms := []*Term{oncePercentage("30"), onceFixed("10")}

	_, err := Expand(context.Background(), usd("1200", jan2027, date(2028, time.January, 1)), monthly(jan2027, 12, "100"), terms)

	require.True(t, ierr.IsValidation(err))
	require.Nil(t, terms[0].Amount)
}
