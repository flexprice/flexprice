package cron

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// razorpayReconciliationStuckClaimAge is how long a claim must sit in
// "claimed" before the sweep will attempt to resolve it — see design spec
// §8 ("query claims status=claimed older than ~1hr"). Kept short of the
// 20-minute sweep interval's multiples so a claim gets several sweep passes
// worth of grace before being touched, in case the original request is still
// genuinely in flight at the gateway.
const razorpayReconciliationStuckClaimAge = 1 * time.Hour

// razorpayReconciliationEntityTypes are the two idempotency-claim entity
// types AutoChargeInvoice creates (see internal/ee/service/invoice.go).
var razorpayReconciliationEntityTypes = []types.IntegrationEntityType{
	types.IntegrationEntityTypeInvoiceCharge,
	types.IntegrationEntityTypeTokenCycleCharge,
}

// RazorpayReconciliationSweepActivities implements design spec §8's
// reconciliation sweep for stuck Razorpay autocharge idempotency claims. This
// is the ONLY place a stuck claim resolves — internal/ee/service/invoice.go's
// AutoChargeInvoice deliberately leaves an ambiguous claim "claimed" for this
// sweep to pick up later.
type RazorpayReconciliationSweepActivities struct {
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	integrationFactory           *integration.Factory
	logger                       *logger.Logger
}

// NewRazorpayReconciliationSweepActivities constructs RazorpayReconciliationSweepActivities.
func NewRazorpayReconciliationSweepActivities(
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	integrationFactory *integration.Factory,
	log *logger.Logger,
) *RazorpayReconciliationSweepActivities {
	return &RazorpayReconciliationSweepActivities{
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		integrationFactory:           integrationFactory,
		logger:                       log,
	}
}

// mapRazorpayPaymentStatusToClaimStatus maps a Razorpay Payment entity's
// "status" field (per Payment.Fetch) to the claim status it should resolve
// to. An empty return means "still ambiguous — leave the claim 'claimed' and
// let a later sweep pass retry it."
func mapRazorpayPaymentStatusToClaimStatus(razorpayPaymentStatus string) string {
	switch razorpayPaymentStatus {
	case "captured":
		return "succeeded"
	case "failed":
		return "failed"
	default:
		// e.g. "created", "authorized", "refunded" or any other in-flight /
		// unexpected state — not a final outcome we can safely act on yet.
		return ""
	}
}

// ResolveStuckClaimsActivity implements design spec §8's reconciliation
// sweep: find InvoiceCharge/TokenCycleCharge claims stuck in "claimed" past
// razorpayReconciliationStuckClaimAge, resolve each via Razorpay's read-only
// Payment.Fetch, and update the claim accordingly.
func (a *RazorpayReconciliationSweepActivities) ResolveStuckClaimsActivity(ctx context.Context) (*cronModels.RazorpayReconciliationSweepWorkflowResult, error) {
	result := &cronModels.RazorpayReconciliationSweepWorkflowResult{}

	claims, err := a.entityIntegrationMappingRepo.ListScopedClaimedByEntityTypesAndProvider(
		ctx,
		razorpayReconciliationEntityTypes,
		string(types.SecretProviderRazorpay),
	)
	if err != nil {
		a.logger.Error(ctx, "failed to list claimed razorpay autocharge claims", "error", err)
		return nil, err
	}

	cutoff := time.Now().UTC().Add(-razorpayReconciliationStuckClaimAge)

	for _, claim := range claims {
		if !claim.CreatedAt.Before(cutoff) {
			// Not stuck yet — still within the grace window, leave it alone.
			continue
		}

		result.Total++

		scopedCtx := types.SetTenantID(ctx, claim.TenantID)
		scopedCtx = types.SetEnvironmentID(scopedCtx, claim.EnvironmentID)

		paymentID, _ := claim.Metadata["payment_id"].(string)
		if paymentID == "" {
			// Nothing was ever recorded for this claim — either the process
			// crashed before ChargeSavedPaymentMethod returned, or the charge
			// submission itself failed before any provider payment ID existed.
			// Nothing to Payment.Fetch against: treat as abandoned.
			if err := a.resolveClaim(scopedCtx, claim.MappingID, "failed"); err != nil {
				a.logger.Error(scopedCtx, "failed to mark abandoned razorpay autocharge claim as failed",
					"mapping_id", claim.MappingID, "entity_type", claim.EntityType, "entity_id", claim.EntityID, "error", err)
				result.Errors++
				continue
			}
			a.logger.Info(scopedCtx, "resolved abandoned razorpay autocharge claim (no payment_id) to failed",
				"mapping_id", claim.MappingID, "entity_type", claim.EntityType, "entity_id", claim.EntityID)
			result.AbandonedNoRef++
			result.Failed++
			continue
		}

		razorpayIntegration, err := a.integrationFactory.GetRazorpayIntegration(scopedCtx)
		if err != nil {
			a.logger.Error(scopedCtx, "razorpay integration not found, skipping claim",
				"mapping_id", claim.MappingID, "tenant_id", claim.TenantID, "environment_id", claim.EnvironmentID, "error", err)
			result.Errors++
			continue
		}

		sdkClient, _, err := razorpayIntegration.Client.GetRazorpaySDKClient(scopedCtx)
		if err != nil {
			a.logger.Error(scopedCtx, "failed to get razorpay sdk client, skipping claim",
				"mapping_id", claim.MappingID, "payment_id", paymentID, "error", err)
			result.Errors++
			continue
		}

		payment, err := sdkClient.Payment.Fetch(paymentID, nil, nil)
		if err != nil {
			// Read-only lookup failed (network blip, rate limit, etc.) — leave
			// the claim "claimed" and retry on the next sweep pass rather than
			// guessing a resolution.
			a.logger.Error(scopedCtx, "razorpay Payment.Fetch failed, will retry next sweep",
				"mapping_id", claim.MappingID, "payment_id", paymentID, "error", err)
			result.Errors++
			continue
		}

		razorpayStatus, _ := payment["status"].(string)
		mappedStatus := mapRazorpayPaymentStatusToClaimStatus(razorpayStatus)
		if mappedStatus == "" {
			a.logger.Info(scopedCtx, "razorpay payment still ambiguous, leaving claim claimed",
				"mapping_id", claim.MappingID, "payment_id", paymentID, "razorpay_status", razorpayStatus)
			result.StillPending++
			continue
		}

		if err := a.resolveClaim(scopedCtx, claim.MappingID, mappedStatus); err != nil {
			a.logger.Error(scopedCtx, "failed to update resolved razorpay autocharge claim",
				"mapping_id", claim.MappingID, "payment_id", paymentID, "resolved_status", mappedStatus, "error", err)
			result.Errors++
			continue
		}

		a.logger.Info(scopedCtx, "resolved razorpay autocharge claim",
			"mapping_id", claim.MappingID, "payment_id", paymentID, "razorpay_status", razorpayStatus, "resolved_status", mappedStatus)

		if mappedStatus == "succeeded" {
			result.Succeeded++
		} else {
			result.Failed++
		}
	}

	a.logger.Info(ctx, "completed razorpay autocharge claim reconciliation sweep",
		"total", result.Total,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
		"still_pending", result.StillPending,
		"abandoned_no_payment_ref", result.AbandonedNoRef,
		"errors", result.Errors,
	)

	return result, nil
}

// resolveClaim re-reads the full mapping row (scopedCtx must already carry
// the claim's tenant/environment) and persists the new status onto its
// Metadata, preserving every other field Update requires.
func (a *RazorpayReconciliationSweepActivities) resolveClaim(scopedCtx context.Context, mappingID string, newStatus string) error {
	mapping, err := a.entityIntegrationMappingRepo.Get(scopedCtx, mappingID)
	if err != nil {
		return err
	}
	if mapping.Metadata == nil {
		mapping.Metadata = map[string]interface{}{}
	}
	mapping.Metadata["status"] = newStatus
	return a.entityIntegrationMappingRepo.Update(scopedCtx, mapping)
}
