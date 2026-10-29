package subscription

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionBuilderEmpty(t *testing.T) {
	for _, test := range []struct {
		name    string
		builder *subscriptionBuilder
	}{
		{name: "nil"},
		{name: "zero", builder: &subscriptionBuilder{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			date := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
			parentID := "parent"
			updated := test.builder.WithStatus(types.SubscriptionStatusActive).
				WithParentSubscriptionID(&parentID).WithCancelAt(&date).WithEndDate(&date).
				WithCurrentPeriodEnd(date).WithCancelAtPeriodEnd(true)
			require.Same(t, test.builder, updated)
			require.Nil(t, updated.Build())
		})
	}

	created := NewSubscriptionBuilder(nil).WithStatus(types.SubscriptionStatusActive).Build()
	require.NotNil(t, created)
	require.Equal(t, types.SubscriptionStatusActive, created.SubscriptionStatus)
}

func TestSubscriptionBuilderCopiesUpdates(t *testing.T) {
	originalEnd := time.Date(2027, time.February, 1, 0, 0, 0, 0, time.UTC)
	originalParentID := "old_parent"
	amount := decimal.NewFromInt(100)
	original := &Subscription{
		ID: "sub", CustomerID: "customer", PlanID: "plan", Currency: "usd",
		SubscriptionStatus: types.SubscriptionStatusIncomplete,
		CurrentPeriodStart: originalEnd.AddDate(0, -1, 0), CurrentPeriodEnd: originalEnd,
		ParentSubscriptionID: &originalParentID, CancelAt: &originalEnd, EndDate: &originalEnd,
		CommitmentAmount: &amount, Metadata: types.Metadata{"source": "original"},
		BaseModel: types.BaseModel{TenantID: "tenant", Status: types.StatusPublished}, EnvironmentID: "env",
	}
	parentID := "new_parent"
	date := originalEnd.AddDate(0, 1, 0)
	updated := NewSubscriptionBuilder(original).WithStatus(types.SubscriptionStatusActive).
		WithParentSubscriptionID(&parentID).WithCancelAt(&date).WithEndDate(&date).
		WithCurrentPeriodEnd(date).WithCancelAtPeriodEnd(true).Build()

	parentID = "changed_input"
	date = date.AddDate(0, 1, 0)
	updated.Metadata["source"] = "updated"
	require.Equal(t, "new_parent", *updated.ParentSubscriptionID)
	require.Equal(t, originalEnd.AddDate(0, 1, 0), *updated.CancelAt)
	require.Equal(t, *updated.CancelAt, *updated.EndDate)
	require.Equal(t, *updated.CancelAt, updated.CurrentPeriodEnd)
	require.True(t, updated.CancelAtPeriodEnd)
	require.Equal(t, types.SubscriptionStatusActive, updated.SubscriptionStatus)
	require.Equal(t, types.SubscriptionStatusIncomplete, original.SubscriptionStatus)
	require.Equal(t, originalEnd, *original.CancelAt)
	require.Equal(t, originalEnd, *original.EndDate)
	require.Equal(t, "old_parent", *original.ParentSubscriptionID)
	require.Equal(t, "original", original.Metadata["source"])
	require.Equal(t, original.ID, updated.ID)
	require.Equal(t, original.CustomerID, updated.CustomerID)
	require.Equal(t, original.PlanID, updated.PlanID)
	require.Equal(t, original.Currency, updated.Currency)
	require.Equal(t, original.CurrentPeriodStart, updated.CurrentPeriodStart)
	require.Equal(t, original.CommitmentAmount, updated.CommitmentAmount)
	require.Equal(t, original.BaseModel, updated.BaseModel)
	require.Equal(t, original.EnvironmentID, updated.EnvironmentID)

	cleared := NewSubscriptionBuilder(updated).WithParentSubscriptionID(nil).WithCancelAt(nil).WithEndDate(nil).Build()
	require.Nil(t, cleared.ParentSubscriptionID)
	require.Nil(t, cleared.CancelAt)
	require.Nil(t, cleared.EndDate)
	require.NotNil(t, updated.ParentSubscriptionID)
	require.NotNil(t, updated.CancelAt)
	require.NotNil(t, updated.EndDate)
}
