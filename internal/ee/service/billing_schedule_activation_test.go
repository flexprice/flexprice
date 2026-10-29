package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestDraftCollectionAuthorization(t *testing.T) {
	for _, scenario := range []string{"authorized", "unbound", "cleared", "invoice changed", "period changed", "reason changed", "scope changed", "fresh pause", "fresh cancellation", "trial", "separate delivery"} {
		t.Run(scenario, func(t *testing.T) {
			f, inv, _ := trialEndCollectionFixture(t)
			f.sub.SubscriptionStatus = types.SubscriptionStatusDraft
			f.sub.TrialStart, f.sub.TrialEnd = nil, nil
			inv.BillingReason = string(types.InvoiceBillingReasonSubscriptionCreate)
			asOf := f.sub.CurrentPeriodStart
			ctx := withDraftActivationCollection(f.ctx, f.sub, asOf)
			if scenario != "unbound" {
				ctx = bindPendingCollection(ctx, inv)
			}
			switch scenario {
			case "cleared":
				ctx = bindPendingCollection(withoutPendingCollection(ctx), inv)
			case "invoice changed":
				inv = invoice.NewInvoiceBuilder(inv).Build()
				inv.ID = "another_invoice"
				ctx = bindPendingCollection(ctx, inv)
			case "period changed":
				inv.PeriodEnd = lo.ToPtr(inv.PeriodEnd.Add(time.Hour))
			case "reason changed":
				inv.BillingReason = string(types.InvoiceBillingReasonSubscriptionCycle)
			case "scope changed":
				ctx = types.SetEnvironmentID(ctx, "another_env")
			case "fresh pause":
				f.sub.PauseStatus = types.PauseStatusActive
			case "fresh cancellation":
				f.sub.CancelledAt = &asOf
			case "trial":
				f.sub.TrialStart = &asOf
				f.sub.TrialEnd = lo.ToPtr(asOf.Add(time.Hour))
			case "separate delivery":
				f.commitment.InstalmentInvoicing = lo.ToPtr(string(types.BillingTermSeparateInvoice))
			}
			ctx = revalidatePendingCollection(ctx, inv)
			owner, err := f.adapter.Snapshot(ctx, f.owner)
			if scenario == "scope changed" {
				require.Error(t, err)
				require.Nil(t, owner)
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario == "authorized", owner.CanCollect)
		})
	}
}
