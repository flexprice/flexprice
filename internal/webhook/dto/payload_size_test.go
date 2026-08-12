package webhookDto

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestPayloadSize_NewIsSmallerThanOld(t *testing.T) {
	type result struct {
		entity, tier       string
		oldBytes, newBytes int
	}

	marshal := func(t *testing.T, v interface{}) int {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		return len(data)
	}

	var results []result

	fullCustomer := buildCustomerResponse(500)
	results = append(results, result{
		entity:   "customer",
		tier:     "huge",
		oldBytes: marshal(t, fullCustomer),
		newBytes: marshal(t, NewCustomerWebhookPayload(fullCustomer, types.WebhookEventCustomerUpdated)),
	})

	fullSubscription := buildSubscriptionResponse(500, 500, 500)
	results = append(results, result{
		entity:   "subscription",
		tier:     "huge",
		oldBytes: marshal(t, fullSubscription),
		newBytes: marshal(t, NewSubscriptionWebhookPayload(fullSubscription, types.WebhookEventSubscriptionUpdated)),
	})

	fullInvoice := buildInvoiceResponse(500)
	results = append(results, result{
		entity:   "invoice",
		tier:     "huge",
		oldBytes: marshal(t, fullInvoice),
		newBytes: marshal(t, NewInvoiceWebhookPayload(fullInvoice, types.WebhookEventInvoiceUpdateFinalized)),
	})

	fullCreditNote := buildCreditNoteResponse(500)
	results = append(results, result{
		entity:   "credit_note",
		tier:     "huge",
		oldBytes: marshal(t, fullCreditNote),
		newBytes: marshal(t, NewCreditNoteWebhookPayload(fullCreditNote, types.WebhookEventCreditNoteUpdated)),
	})

	fullPayment := buildPaymentResponse(500)
	results = append(results, result{
		entity:   "payment",
		tier:     "huge",
		oldBytes: marshal(t, fullPayment),
		newBytes: marshal(t, NewPaymentWebhookPayload(fullPayment, types.WebhookEventPaymentSuccess)),
	})

	fullWallet := buildWalletResponse()
	results = append(results, result{
		entity:   "wallet",
		tier:     "fixed",
		oldBytes: marshal(t, fullWallet),
		newBytes: marshal(t, NewWalletWebhookPayload(fullWallet, nil, types.WebhookEventWalletUpdated)),
	})

	fullTransaction := buildWalletTransactionResponse()
	results = append(results, result{
		entity:   "wallet_transaction",
		tier:     "fixed",
		oldBytes: marshal(t, fullTransaction),
		newBytes: marshal(t, NewTransactionWebhookPayload(fullTransaction, types.WebhookEventWalletTransactionCreated)),
	})

	fullFeature := buildFeatureResponse()
	results = append(results, result{
		entity:   "feature",
		tier:     "fixed",
		oldBytes: marshal(t, fullFeature),
		newBytes: marshal(t, NewFeatureWebhookPayload(fullFeature, types.WebhookEventFeatureUpdated)),
	})

	fullEntitlement := buildEntitlementResponse()
	results = append(results, result{
		entity:   "entitlement",
		tier:     "fixed",
		oldBytes: marshal(t, fullEntitlement),
		newBytes: marshal(t, NewEntitlementWebhookPayload(fullEntitlement, types.WebhookEventEntitlementUpdated)),
	})

	fullCheckoutSession := buildCheckoutSessionResponse()
	results = append(results, result{
		entity:   "checkout_session",
		tier:     "fixed",
		oldBytes: marshal(t, fullCheckoutSession),
		newBytes: marshal(t, NewCheckoutSessionWebhookPayload(fullCheckoutSession, types.WebhookEventCheckoutSessionCompleted)),
	})

	fullPhase := buildSubscriptionPhaseResponse()
	results = append(results, result{
		entity:   "subscription_phase",
		tier:     "fixed",
		oldBytes: marshal(t, fullPhase),
		newBytes: marshal(t, NewSubscriptionPhaseWebhookPayload(NewSubscriptionPhase(fullPhase), types.WebhookEventSubscriptionPhaseUpdated)),
	})

	results = append(results, result{
		entity:   "alert",
		tier:     "fixed",
		oldBytes: marshal(t, buildOldAlertPayload()),
		newBytes: marshal(t, NewAlertWebhookPayload(
			buildFeatureResponse(), buildWalletResponse(), buildCustomerResponse(3),
			types.AlertTypeFeatureWalletBalance, types.AlertStateInAlarm, types.WebhookEventFeatureWalletBalanceAlert,
		)),
	})

	fullInvoiceForComm := buildInvoiceResponse(50)
	results = append(results, result{
		entity:   "communication",
		tier:     "fixed",
		oldBytes: marshal(t, fullInvoiceForComm),
		newBytes: marshal(t, NewCommunicationWebhookPayload(fullInvoiceForComm, types.WebhookEventInvoiceCommunicationTriggered)),
	})

	for _, r := range results {
		reduction := 100 * (1 - float64(r.newBytes)/float64(r.oldBytes))
		t.Logf("%-20s %-8s old=%8d bytes  new=%8d bytes  reduction=%.1f%%",
			r.entity, r.tier, r.oldBytes, r.newBytes, reduction)
		assert.Less(t, r.newBytes, r.oldBytes, "%s/%s: new payload did not shrink", r.entity, r.tier)
	}
}
