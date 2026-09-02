package stripe

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

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

// mergeCallerMetadata is the merge both Stripe call sites use, so exercising it
// here covers the filtering that keeps the FlexPrice-set values authoritative.
func TestMergeCallerMetadataKeepsReservedKeys(t *testing.T) {
	trusted := map[string]string{
		"flexprice_payment_id": "pay_trusted",
		"payment_source":       "flexprice",
	}

	merged := mergeCallerMetadata(trusted, types.Metadata{
		"flexprice_payment_id": "pay_attacker",
		"payment_source":       "spoofed",
		"order_ref":            "ref_123",
	})

	if got := merged["flexprice_payment_id"]; got != "pay_trusted" {
		t.Fatalf("caller metadata overwrote the payment anchor: got %q, want %q", got, "pay_trusted")
	}
	if got := merged["payment_source"]; got != "flexprice" {
		t.Fatalf("caller metadata overwrote payment_source: got %q, want %q", got, "flexprice")
	}
	if got := merged["order_ref"]; got != "ref_123" {
		t.Fatalf("non-reserved caller metadata must pass through: got %q", got)
	}
}

// SetupIntent builds its trusted block, merges caller metadata, and only then
// writes set_default from req.SetDefault. The setup-intent success webhook makes
// the payment method the customer's default on seeing "true", so a caller able to
// set the key would promote its own card without asking through req.SetDefault.
//
// Reproduces that ordering around the shared merge rather than calling
// SetupIntent, which would need a live Stripe client and a synced customer.
func TestSetupIntentDefaultFlagComesOnlyFromRequest(t *testing.T) {
	setupIntentMetadata := func(setDefault bool, caller types.Metadata) map[string]string {
		metadata := map[string]string{
			"customer_id":    "cust_1",
			"environment_id": "env_1",
			"usage":          "off_session",
		}
		metadata = mergeCallerMetadata(metadata, caller)
		if setDefault {
			metadata["set_default"] = "true"
		}
		return metadata
	}

	spoofed := types.Metadata{"set_default": "true", "order_ref": "ref_123"}

	metadata := setupIntentMetadata(false, spoofed)
	if _, present := metadata["set_default"]; present {
		t.Fatal("caller metadata must not be able to set set_default")
	}
	if got := metadata["order_ref"]; got != "ref_123" {
		t.Fatalf("non-reserved caller metadata must pass through: got %q", got)
	}

	// The trusted path must still write the flag when the request asks for it.
	metadata = setupIntentMetadata(true, spoofed)
	if got := metadata["set_default"]; got != "true" {
		t.Fatalf("req.SetDefault must still set the flag: got %q, want %q", got, "true")
	}
}
