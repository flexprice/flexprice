package invoice

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestListPastPeriodDraftsActivity(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1")
	invoiceRepo := testutil.NewInMemoryInvoiceStore()
	now := time.Now().UTC()

	seed := func(id, subscriptionID string, reason types.InvoiceBillingReason, status types.InvoiceStatus, periodEnd time.Time) {
		periodStart := periodEnd.AddDate(0, -1, 0)
		require.NoError(t, invoiceRepo.Create(ctx, &invoice.Invoice{
			ID:             id,
			CustomerID:     "cust_1",
			SubscriptionID: lo.ToPtr(subscriptionID),
			InvoiceType:    types.InvoiceTypeSubscription,
			InvoiceStatus:  status,
			BillingReason:  string(reason),
			Currency:       "usd",
			PeriodStart:    &periodStart,
			PeriodEnd:      &periodEnd,
			EnvironmentID:  "env_1",
			BaseModel:      types.BaseModel{TenantID: "tenant_1", Status: types.StatusPublished, CreatedAt: now, UpdatedAt: now},
		}))
	}
	seed("inv_past_cycle", "subs_1", types.InvoiceBillingReasonSubscriptionCycle, types.InvoiceStatusDraft, now.Add(-24*time.Hour))
	seed("inv_open_cycle", "subs_1", types.InvoiceBillingReasonSubscriptionCycle, types.InvoiceStatusDraft, now.Add(24*time.Hour))
	seed("inv_past_proration", "subs_1", types.InvoiceBillingReasonProration, types.InvoiceStatusDraft, now.Add(-24*time.Hour))
	seed("inv_past_finalized", "subs_1", types.InvoiceBillingReasonSubscriptionCycle, types.InvoiceStatusFinalized, now.Add(-24*time.Hour))
	seed("inv_other_sub", "subs_2", types.InvoiceBillingReasonSubscriptionCycle, types.InvoiceStatusDraft, now.Add(-24*time.Hour))

	acts := NewInvoiceActivities(service.ServiceParams{InvoiceRepo: invoiceRepo}, logger.NewNoopLogger())
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
	env.RegisterActivity(acts.ListPastPeriodDraftsActivity)

	val, err := env.ExecuteActivity(acts.ListPastPeriodDraftsActivity, invoiceModels.ListPastPeriodDraftsActivityInput{
		SubscriptionID: "subs_1",
		TenantID:       "tenant_1",
		EnvironmentID:  "env_1",
		UserID:         "user_1",
	})
	require.NoError(t, err)

	var out invoiceModels.ListPastPeriodDraftsActivityOutput
	require.NoError(t, val.Get(&out))
	require.Equal(t, []string{"inv_past_cycle"}, out.InvoiceIDs)
}

func TestFinalizeInvoiceActivity_SkipsWhenAutoFinalizeOff(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1")
	invoiceRepo := testutil.NewInMemoryInvoiceStore()
	settingsRepo := testutil.NewInMemorySettingsStore()
	now := time.Now().UTC()
	baseModel := types.BaseModel{TenantID: "tenant_1", Status: types.StatusPublished, CreatedAt: now, UpdatedAt: now}

	require.NoError(t, settingsRepo.Create(ctx, &settings.Setting{
		ID:            "setting_1",
		Key:           types.SettingKeyInvoiceConfig,
		Value:         map[string]interface{}{"auto_finalize": false, "finalization_delay_seconds": 1800},
		EnvironmentID: "env_1",
		BaseModel:     baseModel,
	}))

	computedAt := now.Add(-time.Hour)
	require.NoError(t, invoiceRepo.Create(ctx, &invoice.Invoice{
		ID:             "inv_1",
		CustomerID:     "cust_1",
		InvoiceType:    types.InvoiceTypeOneOff,
		InvoiceStatus:  types.InvoiceStatusDraft,
		Currency:       "usd",
		LastComputedAt: &computedAt,
		EnvironmentID:  "env_1",
		BaseModel:      baseModel,
	}))

	acts := NewInvoiceActivities(service.ServiceParams{InvoiceRepo: invoiceRepo, SettingsRepo: settingsRepo}, logger.NewNoopLogger())
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
	env.RegisterActivity(acts.FinalizeInvoiceActivity)

	val, err := env.ExecuteActivity(acts.FinalizeInvoiceActivity, invoiceModels.FinalizeInvoiceActivityInput{
		InvoiceID:     "inv_1",
		TenantID:      "tenant_1",
		EnvironmentID: "env_1",
		UserID:        "user_1",
	})
	require.NoError(t, err)

	var out invoiceModels.FinalizeInvoiceActivityOutput
	require.NoError(t, val.Get(&out))
	require.True(t, out.Skipped)

	inv, err := invoiceRepo.Get(ctx, "inv_1")
	require.NoError(t, err)
	require.Equal(t, types.InvoiceStatusDraft, inv.InvoiceStatus)
}
