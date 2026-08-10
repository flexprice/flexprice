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
		"set_default",
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

// SetupIntent writes set_default after merging caller metadata, so the key is
// absent rather than overwritten when the caller supplies it. The setup-intent
// success webhook makes the payment method the customer's default on seeing
// "true", so a caller that could set it would promote its own card without
// asking for it through req.SetDefault.
func TestCallerCannotSetSetupIntentDefaultFlag(t *testing.T) {
	metadata := map[string]string{
		"customer_id":    "cust_1",
		"environment_id": "env_1",
	}

	callerMetadata := map[string]string{
		"set_default": "true",
		"order_ref":   "ref_123",
	}

	for k, v := range callerMetadata {
		if isReservedStripeMetadataKey(k) {
			continue
		}
		metadata[k] = v
	}

	// Mirrors the req.SetDefault == false path, which writes nothing.
	if _, present := metadata["set_default"]; present {
		t.Fatal("caller metadata must not be able to set set_default")
	}
	if got := metadata["order_ref"]; got != "ref_123" {
		t.Fatalf("non-reserved caller metadata must pass through: got %q", got)
	}
}
