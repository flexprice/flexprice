package stripe

import "testing"

// Caller-supplied metadata reaches Stripe from the create-payment request body,
// and the keys FlexPrice sets itself are what a returning webhook uses to decide
// which payment, invoice and environment the Stripe object belongs to. If a
// caller could set them, a webhook would reconcile against a record of their
// choosing.
func TestReservedStripeMetadataKeys(t *testing.T) {
	reserved := []string{
		"flexprice_payment_id",
		"flexprice_invoice_id",
		"flexprice_customer_id",
		"stripe_invoice_id",
		"environment_id",
		"customer_id",
		"payment_source",
		"payment_type",
		"connection_id",
		"connection_name",
	}
	for _, key := range reserved {
		if !isReservedStripeMetadataKey(key) {
			t.Errorf("%q must be reserved: caller metadata could otherwise overwrite it", key)
		}
	}

	allowed := []string{"order_ref", "team", "flexprice_payment_id_note", "", "Flexprice_Payment_ID"}
	for _, key := range allowed {
		if isReservedStripeMetadataKey(key) {
			t.Errorf("%q must not be reserved: callers may set their own metadata", key)
		}
	}
}

// The merge that applies req.Metadata runs after the trusted block is built, so
// skipping reserved keys is the only thing keeping the FlexPrice-set values
// authoritative.
func TestReservedKeysSurviveCallerMetadataMerge(t *testing.T) {
	metadata := map[string]string{
		"flexprice_payment_id": "pay_trusted",
		"payment_source":       "flexprice",
	}

	callerMetadata := map[string]string{
		"flexprice_payment_id": "pay_attacker",
		"payment_source":       "spoofed",
		"order_ref":            "ref_123",
	}

	for k, v := range callerMetadata {
		if isReservedStripeMetadataKey(k) {
			continue
		}
		metadata[k] = v
	}

	if got := metadata["flexprice_payment_id"]; got != "pay_trusted" {
		t.Fatalf("caller metadata overwrote the payment anchor: got %q, want %q", got, "pay_trusted")
	}
	if got := metadata["payment_source"]; got != "flexprice" {
		t.Fatalf("caller metadata overwrote payment_source: got %q, want %q", got, "flexprice")
	}
	if got := metadata["order_ref"]; got != "ref_123" {
		t.Fatalf("non-reserved caller metadata must pass through: got %q", got)
	}
}
