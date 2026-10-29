package billingterms

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestPlannerAnnualCollection(t *testing.T) {
	req := testRequest(t)
	req.Rows = []RuleInput{
		{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: ptr(decimal.RequireFromString("300"))},
		recurringRule(),
	}
	plan, err := NewPlanner().Plan(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, plan.Rules(), 2)
	require.Len(t, plan.Items(), 13)
	require.Len(t, plan.ItemsForRule(0), 1)
	require.Len(t, plan.ItemsForRule(1), 12)
	require.Equal(t, "1200", plan.Budget().String())
	require.Equal(t, "300", plan.Items()[0].Amount.String())
	for i, item := range plan.ItemsForRule(1) {
		require.Equal(t, "75", item.Amount.String())
		require.Equal(t, req.Owner.StartDate.AddDate(0, i+1, 0), *item.BillOn)
		require.Equal(t, req.Owner.StartDate.AddDate(0, i, 0), item.ServicePeriodStart)
		require.Equal(t, types.BillingScheduleScheduled, item.ScheduleStatus)
		require.Empty(t, item.ID)
		require.Nil(t, item.BillingTermID)
		require.Equal(t, req.Owner.TenantID, item.TenantID)
		require.Equal(t, req.Owner.EnvironmentID, item.EnvironmentID)
	}
	require.Equal(t, req.Owner.StartDate, plan.Items()[0].ServicePeriodStart)
	require.Equal(t, req.Owner.EndDate, plan.Items()[0].ServicePeriodEnd)
	require.Equal(t, "1200", sumItems(plan.Items()).String())
}

func TestPlannerReturnsDetachedModels(t *testing.T) {
	req := testRequest(t)
	req.Rows = []RuleInput{
		{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: ptr(decimal.NewFromInt(300)), Metadata: types.Metadata{"note": "original"}},
		recurringRule(),
	}
	planned, err := NewPlanner().Plan(context.Background(), req)
	require.NoError(t, err)
	rules := planned.Rules()
	*rules[0].Value = decimal.NewFromInt(1)
	*rules[0].BillOn = rules[0].BillOn.Add(time.Hour)
	rules[0].Metadata["note"] = "changed"
	items := planned.Items()
	items[0].Amount = decimal.NewFromInt(1)
	*items[0].BillOn = items[0].BillOn.Add(time.Hour)
	group := planned.ItemsForRule(1)
	group[0].Amount = decimal.NewFromInt(1)

	require.Equal(t, "300", planned.Rules()[0].Value.String())
	require.Equal(t, req.Owner.StartDate, *planned.Rules()[0].BillOn)
	require.Equal(t, "original", planned.Rules()[0].Metadata["note"])
	require.Equal(t, "300", planned.Items()[0].Amount.String())
	require.Equal(t, req.Owner.StartDate, *planned.Items()[0].BillOn)
	require.Equal(t, "75", planned.ItemsForRule(1)[0].Amount.String())
}

type nilPlannerContext struct{ context.Context }

func TestPlannerRejectsNilContext(t *testing.T) {
	for _, ctx := range []context.Context{nil, (*nilPlannerContext)(nil)} {
		_, err := NewPlanner().Plan(ctx, testRequest(t))
		require.True(t, ierr.IsValidation(err), "%+v", err)
	}
}

func TestPlannerSplitting(t *testing.T) {
	tests := []struct {
		name     string
		currency string
		amount   string
		start    string
		end      string
		anchor   string
		timezone string
		interval types.BillingPeriod
		split    types.BillingTermSplit
		want     []string
	}{
		{"even residual", "usd", "1000", "2027-01-01T00:00:00Z", "2027-04-01T00:00:00Z", "2027-01-01T00:00:00Z", "UTC", types.BILLING_PERIOD_MONTHLY, types.BillingTermEven, []string{"333.33", "333.33", "333.34"}},
		{"partial month elapsed", "usd", "900", "2027-01-15T00:00:00Z", "2027-03-01T00:00:00Z", "2027-01-01T00:00:00Z", "UTC", types.BILLING_PERIOD_MONTHLY, types.BillingTermProportional, []string{"340", "560"}},
		{"partial month even", "usd", "900", "2027-01-15T00:00:00Z", "2027-03-01T00:00:00Z", "2027-01-01T00:00:00Z", "UTC", types.BILLING_PERIOD_MONTHLY, types.BillingTermEven, []string{"450", "450"}},
		{"zero decimals", "jpy", "1000", "2027-01-01T00:00:00Z", "2027-04-01T00:00:00Z", "2027-01-01T00:00:00Z", "UTC", types.BILLING_PERIOD_MONTHLY, types.BillingTermEven, []string{"333", "333", "334"}},
		{"three decimals", "kwd", "1", "2027-01-01T00:00:00Z", "2027-04-01T00:00:00Z", "2027-01-01T00:00:00Z", "UTC", types.BILLING_PERIOD_MONTHLY, types.BillingTermEven, []string{"0.333", "0.333", "0.334"}},
		{"DST elapsed days", "usd", "71", "2027-03-13T05:00:00Z", "2027-03-16T04:00:00Z", "2027-03-13T05:00:00Z", "America/New_York", types.BILLING_PERIOD_DAILY, types.BillingTermProportional, []string{"24", "23", "24"}},
		{"DST even days", "usd", "90", "2027-03-13T05:00:00Z", "2027-03-16T04:00:00Z", "2027-03-13T05:00:00Z", "America/New_York", types.BILLING_PERIOD_DAILY, types.BillingTermEven, []string{"30", "30", "30"}},
		{"month end anchor", "usd", "300", "2027-01-31T00:00:00Z", "2027-04-30T00:00:00Z", "2027-01-31T00:00:00Z", "UTC", types.BILLING_PERIOD_MONTHLY, types.BillingTermEven, []string{"100", "100", "100"}},
		{"large elapsed periods", "usd", "1000", "1000-01-01T00:00:00Z", "2001-01-01T00:00:00Z", "1000-01-01T00:00:00Z", "UTC", types.BILLING_PERIOD_ANNUAL, types.BillingTermProportional, []string{"999", "1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := testRequest(t)
			req.Owner.Currency = tt.currency
			req.Owner.OriginalTotal = decimal.RequireFromString(tt.amount)
			req.Owner.CollectionTarget = req.Owner.OriginalTotal
			req.Owner.StartDate, req.Owner.EndDate = date(t, tt.start), date(t, tt.end)
			req.Owner.Timezone = tt.timezone
			req.Calendar = testCalendar(t, date(t, tt.anchor), tt.timezone)
			rule := recurringRule()
			rule.Interval, rule.Split = ptr(tt.interval), ptr(tt.split)
			if tt.name == "large elapsed periods" {
				rule.IntervalCount = ptr(1000)
			}
			req.Rows = []RuleInput{rule}
			result, err := NewPlanner().Plan(context.Background(), req)
			require.NoError(t, err)
			var amounts []string
			for _, item := range result.Items() {
				amounts = append(amounts, item.Amount.String())
			}
			require.Equal(t, tt.want, amounts)
			require.True(t, sumItems(result.Items()).Equal(req.Owner.CollectionTarget))
		})
	}
}

func TestPlannerEditsAndPercent(t *testing.T) {
	req := testRequest(t)
	req.Edit = true
	req.Owner.AssignedAmount, req.Owner.RetainedItemCount = decimal.NewFromInt(600), 5
	start := date(t, "2027-05-01T00:00:00Z")
	rule := recurringRule()
	rule.StartDate, rule.IntervalCount = &start, ptr(2)
	req.Rows = []RuleInput{rule}
	result, err := NewPlanner().Plan(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Items(), 4)
	for _, item := range result.Items() {
		require.Equal(t, "150", item.Amount.String())
	}
	require.Equal(t, date(t, "2027-07-01T00:00:00Z"), *result.Items()[0].BillOn)

	// The replacement percent still refers to $1,200, not the $600 balance.
	req.Rows = []RuleInput{
		{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Percent: ptr(decimal.NewFromInt(25)), BillOn: &start},
		{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: ptr(req.Owner.EndDate)},
	}
	result, err = NewPlanner().Plan(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "300", result.Items()[0].Amount.String())
	require.Equal(t, "300", result.Items()[1].Amount.String())
	require.Equal(t, "25", result.Rules()[0].Metadata["input_percent"])
	require.Nil(t, req.Rows[0].Value)
	require.Nil(t, req.Rows[0].Metadata)

	req.Owner.AssignedAmount, req.Owner.RetainedItemCount = decimal.Zero, 0
	req.Rows = []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: ptr(req.Owner.StartDate)}}
	result, err = NewPlanner().Plan(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "1200", result.Items()[0].Amount.String())
}

func TestPlannerManualAndStandaloneOwner(t *testing.T) {
	req := testRequest(t)
	req.Owner.Owner.Type = "fixed_fee"
	req.Owner.SubscriptionID, req.Calendar = nil, nil
	req.Owner.Delivery = types.BillingTermSeparateInvoice
	req.Owner.CanCollect = false
	req.Rows = []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, OnTrigger: true}}
	result, err := NewPlanner().Plan(context.Background(), req)
	require.NoError(t, err)
	item := result.Items()[0]
	require.Nil(t, item.SubscriptionID)
	require.Nil(t, item.BillOn)
	require.Equal(t, types.BillingScheduleManual, item.BillTrigger)
	require.Equal(t, "1200", item.Amount.String())
	require.Equal(t, req.Owner.StartDate, item.ServicePeriodStart)
	require.Equal(t, req.Owner.EndDate, item.ServicePeriodEnd)
	result, err = NewPlanner().Plan(context.Background(), PlanRequest{Owner: req.Owner, Rows: resultToInputs(result)})
	require.NoError(t, err)
	require.Nil(t, result.Items()[0].BillOn)
}

func TestPlannerFixedOccurrencesAndLimits(t *testing.T) {
	for _, n := range []int{365, 500, 501} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			req := testRequest(t)
			req.Owner.Owner.Type = "fee"
			req.Owner.SubscriptionID = nil
			req.Owner.Delivery = types.BillingTermSeparateInvoice
			req.Owner.EndDate = req.Owner.StartDate.AddDate(0, 0, n)
			req.Owner.OriginalTotal, req.Owner.CollectionTarget = decimal.NewFromInt(int64(n)), decimal.NewFromInt(int64(n))
			rule := recurringRule()
			rule.Interval, rule.Split, rule.ValueType, rule.Value = ptr(types.BILLING_PERIOD_DAILY), nil, types.BillingTermFixed, ptr(decimal.NewFromInt(1))
			req.Rows = []RuleInput{rule}
			result, err := NewPlanner().Plan(context.Background(), req)
			if n > MaxItems {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, result.Items(), n)
		})
	}

	req := testRequest(t)
	req.Owner.AssignedAmount, req.Owner.RetainedItemCount = decimal.NewFromInt(1199), 499
	req.Rows = []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder}}
	result, err := NewPlanner().Plan(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Items(), 1)
	req.Rows = append(req.Rows, RuleInput{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: ptr(decimal.RequireFromString("0.50"))})
	_, err = NewPlanner().Plan(context.Background(), req)
	require.Error(t, err)

	req = testRequest(t)
	req.Owner.CollectionTarget = decimal.NewFromInt(20)
	req.Rows = make([]RuleInput, MaxRules)
	for i := range req.Rows {
		req.Rows[i] = RuleInput{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: ptr(decimal.NewFromInt(1))}
	}
	_, err = NewPlanner().Plan(context.Background(), req)
	require.NoError(t, err)
	req.Rows = append(req.Rows, req.Rows[0])
	_, err = NewPlanner().Plan(context.Background(), req)
	require.Error(t, err)
}

func TestPlannerValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*PlanRequest)
	}{
		{"empty plan", func(r *PlanRequest) { r.Rows = nil }},
		{"missing scope", func(r *PlanRequest) { r.Owner.EnvironmentID = "" }},
		{"missing recipient", func(r *PlanRequest) { r.Owner.CustomerID = "" }},
		{"invalid timezone", func(r *PlanRequest) { r.Owner.Timezone = "Unknown/Zone" }},
		{"no subscription", func(r *PlanRequest) { r.Owner.SubscriptionID = nil }},
		{"no budget", func(r *PlanRequest) { r.Owner.AssignedAmount = r.Owner.CollectionTarget; r.Owner.RetainedItemCount = 1 }},
		{"negative budget", func(r *PlanRequest) { r.Owner.AssignedAmount = decimal.NewFromInt(1201); r.Owner.RetainedItemCount = 1 }},
		{"missing retained count", func(r *PlanRequest) { r.Owner.AssignedAmount = decimal.NewFromInt(1) }},
		{"unrounded assigned", func(r *PlanRequest) {
			r.Owner.AssignedAmount = decimal.RequireFromString("1.001")
			r.Owner.RetainedItemCount = 1
		}},
		{"edit forbidden", func(r *PlanRequest) { r.Edit = true; r.Owner.CanEdit = false }},
		{"implicit edit once date", func(r *PlanRequest) { r.Edit = true }},
		{"invalid kind", func(r *PlanRequest) { r.Rows[0].Kind = "daily" }},
		{"invalid value type", func(r *PlanRequest) { r.Rows[0].ValueType = "percent" }},
		{"remainder value", func(r *PlanRequest) { r.Rows[0].Value = ptr(decimal.NewFromInt(1)) }},
		{"two remainders", func(r *PlanRequest) { r.Rows = append(r.Rows, r.Rows[0]) }},
		{"zero fixed", func(r *PlanRequest) {
			r.Rows[0].ValueType = types.BillingTermFixed
			r.Rows[0].Value = ptr(decimal.Zero)
		}},
		{"sub minor amount", func(r *PlanRequest) {
			r.Rows[0].ValueType = types.BillingTermFixed
			r.Rows[0].Value = ptr(decimal.RequireFromString("0.001"))
		}},
		{"fixed mismatch", func(r *PlanRequest) {
			r.Rows[0].ValueType = types.BillingTermFixed
			r.Rows[0].Value = ptr(decimal.NewFromInt(1))
		}},
		{"fixed double input", func(r *PlanRequest) {
			r.Rows[0].ValueType = types.BillingTermFixed
			r.Rows[0].Value = ptr(decimal.NewFromInt(1))
			r.Rows[0].Percent = ptr(decimal.NewFromInt(1))
		}},
		{"percent over hundred", func(r *PlanRequest) {
			r.Rows[0].ValueType = types.BillingTermFixed
			r.Rows[0].Percent = ptr(decimal.NewFromInt(101))
		}},
		{"tiny percent exponent", func(r *PlanRequest) {
			r.Rows[0].ValueType = types.BillingTermFixed
			r.Rows[0].Percent = ptr(decimal.New(1, math.MinInt32))
		}},
		{"huge amount exponent", func(r *PlanRequest) {
			r.Rows[0].ValueType = types.BillingTermFixed
			r.Rows[0].Value = ptr(decimal.New(1, math.MaxInt32))
		}},
		{"rounded storage overflow", func(r *PlanRequest) {
			r.Owner.OriginalTotal = decimal.RequireFromString("999999999999.999")
			r.Owner.CollectionTarget = r.Owner.OriginalTotal
		}},
		{"date out of term", func(r *PlanRequest) { r.Rows[0].BillOn = ptr(r.Owner.StartDate.AddDate(0, 0, -1)) }},
		{"trigger plus date", func(r *PlanRequest) { r.Rows[0].OnTrigger = true; r.Rows[0].BillOn = ptr(r.Owner.StartDate) }},
		{"once recurring field", func(r *PlanRequest) { r.Rows[0].IntervalCount = ptr(1) }},
		{"once split", func(r *PlanRequest) { r.Rows[0].Split = ptr(types.BillingTermEven) }},
		{"long description", func(r *PlanRequest) { r.Rows[0].Description = strings.Repeat("x", 256) }},
		{"missing calendar", func(r *PlanRequest) { r.Rows = []RuleInput{recurringRule()}; r.Calendar = nil }},
		{"implicit edit recurring date", func(r *PlanRequest) { r.Rows = []RuleInput{recurringRule()}; r.Edit = true }},
		{"recurring trigger", func(r *PlanRequest) { r.Rows = []RuleInput{recurringRule()}; r.Rows[0].OnTrigger = true }},
		{"recurring timing", func(r *PlanRequest) { r.Rows = []RuleInput{recurringRule()}; r.Rows[0].BillAt = nil }},
		{"bad interval", func(r *PlanRequest) {
			r.Rows = []RuleInput{recurringRule()}
			r.Rows[0].Interval = ptr(types.BILLING_PERIOD_ONETIME)
		}},
		{"zero interval count", func(r *PlanRequest) { r.Rows = []RuleInput{recurringRule()}; r.Rows[0].IntervalCount = ptr(0) }},
		{"bad split", func(r *PlanRequest) {
			r.Rows = []RuleInput{recurringRule()}
			r.Rows[0].Split = ptr(types.BillingTermSplit("priced"))
		}},
		{"fixed split", func(r *PlanRequest) {
			r.Rows = []RuleInput{recurringRule()}
			r.Rows[0].ValueType = types.BillingTermFixed
			r.Rows[0].Value = ptr(decimal.NewFromInt(100))
		}},
		{"reversed range", func(r *PlanRequest) {
			r.Rows = []RuleInput{recurringRule()}
			r.Rows[0].EndDate = ptr(r.Owner.StartDate)
		}},
		{"rounded zero occurrence", func(r *PlanRequest) {
			r.Rows = []RuleInput{recurringRule()}
			r.Owner.CollectionTarget = decimal.RequireFromString("0.01")
		}},
		{"non advancing calendar", func(r *PlanRequest) { r.Rows = []RuleInput{recurringRule()}; r.Calendar = stalledCalendar{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := testRequest(t)
			req.Rows = []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder}}
			tt.change(&req)
			result, err := NewPlanner().Plan(context.Background(), req)
			require.Error(t, err)
			require.True(t, ierr.IsValidation(err), err.Error())
			require.Nil(t, result)
		})
	}
}

func TestPlannerContextAndNilResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewPlanner().Plan(ctx, testRequest(t))
	require.ErrorIs(t, err, context.Canceled)
	var p *plan
	require.Nil(t, p.Rules())
	require.Nil(t, p.Items())
	require.Nil(t, p.ItemsForRule(0))
	require.True(t, p.Budget().IsZero())
}

type stalledCalendar struct{}

func (stalledCalendar) Next(start, _ time.Time, _ types.BillingPeriod, _ int) (time.Time, error) {
	return start, nil
}

func testRequest(t *testing.T) PlanRequest {
	t.Helper()
	start := date(t, "2027-01-01T00:00:00Z")
	return PlanRequest{
		Owner: domain.OwnerSnapshot{
			Owner:    domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: "comm_test"},
			TenantID: "tenant", EnvironmentID: "env", CustomerID: "cust",
			OriginalTotal: decimal.NewFromInt(1200), CollectionTarget: decimal.NewFromInt(1200),
			StartDate: start, EndDate: date(t, "2028-01-01T00:00:00Z"), Timezone: "UTC", Currency: "usd",
			SubscriptionID: ptr("sub"), Delivery: types.BillingTermSubscriptionInvoice, CanEdit: true, CanCollect: true,
		},
		Calendar: testCalendar(t, start, "UTC"),
	}
}

func recurringRule() RuleInput {
	return RuleInput{Kind: types.BillingTermRecurring, ValueType: types.BillingTermRemainder,
		Interval: ptr(types.BILLING_PERIOD_MONTHLY), IntervalCount: ptr(1), BillAt: ptr(types.BillingTermArrear), Split: ptr(types.BillingTermEven)}
}

func testCalendar(t *testing.T, anchor time.Time, timezone string) Calendar {
	t.Helper()
	calendar, err := NewCalendar(anchor, timezone)
	require.NoError(t, err)
	return calendar
}

func date(t *testing.T, value string) time.Time {
	t.Helper()
	result, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)
	return result
}

func ptr[T any](v T) *T { return &v }

func sumItems(items []*domain.ScheduleItem) decimal.Decimal {
	sum := decimal.Zero
	for _, item := range items {
		sum = sum.Add(item.Amount)
	}
	return sum
}

func resultToInputs(result Plan) []RuleInput {
	var inputs []RuleInput
	for _, rule := range result.Rules() {
		inputs = append(inputs, RuleInput{Kind: rule.Kind, ValueType: rule.ValueType, OnTrigger: rule.OnTrigger, Value: rule.Value})
	}
	return inputs
}
