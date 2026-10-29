package billingterms

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestRuleBuilderCopiesDefinition(t *testing.T) {
	amount := decimal.NewFromInt(75)
	date := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	interval := types.BILLING_PERIOD_MONTHLY
	count := 1
	timing := types.BillingTermArrear
	split := types.BillingTermEven
	original := &Rule{
		ID: "bterm_original", Value: &amount, BillOn: &date,
		StartDate: &date, EndDate: &date, Interval: &interval,
		IntervalCount: &count, BillAt: &timing, Split: &split,
		Metadata:  types.Metadata{"label": "original"},
		BaseModel: types.BaseModel{Status: types.StatusPublished},
	}
	copy := NewRuleBuilder(original).WithStatus(types.StatusArchived).Build()
	*copy.Value = decimal.NewFromInt(100)
	*copy.BillOn = date.Add(time.Hour)
	*copy.StartDate = date.Add(time.Hour)
	*copy.EndDate = date.Add(time.Hour)
	*copy.Interval = types.BILLING_PERIOD_ANNUAL
	*copy.IntervalCount = 2
	*copy.BillAt = types.BillingTermAdvance
	*copy.Split = types.BillingTermProportional
	copy.Metadata["label"] = "changed"
	require.Equal(t, "bterm_original", copy.ID)
	require.True(t, original.Value.Equal(decimal.NewFromInt(75)))
	require.Equal(t, date, *original.BillOn)
	require.Equal(t, date, *original.StartDate)
	require.Equal(t, date, *original.EndDate)
	require.Equal(t, types.BILLING_PERIOD_MONTHLY, *original.Interval)
	require.Equal(t, 1, *original.IntervalCount)
	require.Equal(t, types.BillingTermArrear, *original.BillAt)
	require.Equal(t, types.BillingTermEven, *original.Split)
	require.Equal(t, "original", original.Metadata["label"])
	require.Equal(t, types.StatusPublished, original.Status)
}

func TestScheduleItemBuilderPreservesSourceWhileCanceling(t *testing.T) {
	termID, invoiceID, lineID, subscriptionID := "bterm_original", "inv_original", "line_original", "sub_original"
	billOn := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	reason := types.BillingScheduleInvoiceVoided
	original := &ScheduleItem{
		ID: "bsi_original", BillingTermID: &termID, InvoiceID: &invoiceID,
		InvoiceLineItemID: &lineID, SubscriptionID: &subscriptionID,
		BillOn: &billOn, Amount: decimal.NewFromInt(75),
		ScheduleStatus: types.BillingScheduleInvoiced,
		Metadata:       types.Metadata{"note": "original"},
		BaseModel:      types.BaseModel{Status: types.StatusPublished},
	}
	copy := NewScheduleItemBuilder(original).
		WithStatus(types.StatusArchived).
		WithScheduleStatus(types.BillingScheduleCanceled).
		WithCancellationReason(&reason).Build()
	require.Equal(t, original.ID, copy.ID)
	require.Equal(t, original.Amount, copy.Amount)
	require.Equal(t, termID, *copy.BillingTermID)
	require.Equal(t, invoiceID, *copy.InvoiceID)
	require.Equal(t, lineID, *copy.InvoiceLineItemID)
	*copy.BillingTermID = "changed"
	*copy.InvoiceID = "changed"
	*copy.InvoiceLineItemID = "changed"
	*copy.SubscriptionID = "changed"
	*copy.BillOn = billOn.Add(time.Hour)
	copy.Metadata["note"] = "changed"
	require.Equal(t, termID, *original.BillingTermID)
	require.Equal(t, invoiceID, *original.InvoiceID)
	require.Equal(t, lineID, *original.InvoiceLineItemID)
	require.Equal(t, subscriptionID, *original.SubscriptionID)
	require.Equal(t, billOn, *original.BillOn)
	require.Equal(t, "original", original.Metadata["note"])
	require.Nil(t, original.CancellationReason)
	require.Equal(t, types.BillingScheduleInvoiced, original.ScheduleStatus)
}

func TestBuildersDetachMetadataArguments(t *testing.T) {
	metadata := types.Metadata{"key": "original"}
	rule := NewRuleBuilder(nil).WithMetadata(metadata).Build()
	item := NewScheduleItemBuilder(nil).WithMetadata(metadata).Build()
	metadata["key"] = "changed"
	require.Equal(t, "original", rule.Metadata["key"])
	require.Equal(t, "original", item.Metadata["key"])
}

func TestBuildersHandleMissingModels(t *testing.T) {
	for _, test := range []struct {
		name string
		rule *ruleBuilder
		item *scheduleItemBuilder
	}{
		{name: "nil builders"},
		{name: "empty builders", rule: &ruleBuilder{}, item: &scheduleItemBuilder{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Nil(t, test.rule.WithID("rule").WithStatus(types.StatusArchived).WithMetadata(nil).Build())
			require.Nil(t, test.item.WithID("item").WithBillingTermID(nil).WithAmount(decimal.Zero).
				WithStatus(types.StatusArchived).WithBillOn(nil).WithScheduleStatus(types.BillingScheduleCanceled).
				WithInvoiceID(nil).WithInvoiceLineItemID(nil).WithCancellationReason(nil).WithMetadata(nil).Build())
		})
	}
	require.NotNil(t, NewRuleBuilder(nil).WithID("rule").Build())
	require.NotNil(t, NewScheduleItemBuilder(nil).WithID("item").Build())
}
