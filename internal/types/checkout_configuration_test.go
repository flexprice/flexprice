package types

import (
	"testing"
	"time"

	cockroachErrors "github.com/cockroachdb/errors"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validAddAddonRef() AddAddonRef {
	return AddAddonRef{
		AssociationID:     "addon_assoc_123",
		AddonID:           "addon_123",
		Cadence:           AddonCadenceRecurring,
		ProrationBehavior: ProrationBehaviorCreateProrations,
		StartDate:         time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
	}
}

func TestAddAddonParams_Validate(t *testing.T) {
	withRef := func(mutate func(*AddAddonRef)) *AddAddonParams {
		ref := validAddAddonRef()
		mutate(&ref)
		return &AddAddonParams{SubscriptionID: "subs_123", Addons: []AddAddonRef{ref}}
	}

	tests := []struct {
		name    string
		params  *AddAddonParams
		wantErr bool
	}{
		{
			name:    "nil params",
			params:  nil,
			wantErr: true,
		},
		{
			name:    "valid single addon",
			params:  withRef(func(*AddAddonRef) {}),
			wantErr: false,
		},
		{
			// Unset proration_behavior is legal: it mirrors the pay-later path, where an
			// unset behavior means no partial-period charge rather than an invalid request.
			name:    "valid with unset proration behavior",
			params:  withRef(func(r *AddAddonRef) { r.ProrationBehavior = "" }),
			wantErr: false,
		},
		{
			name:    "valid onetime cadence",
			params:  withRef(func(r *AddAddonRef) { r.Cadence = AddonCadenceOnetime }),
			wantErr: false,
		},
		{
			name:    "empty subscription id",
			params:  &AddAddonParams{SubscriptionID: "", Addons: []AddAddonRef{validAddAddonRef()}},
			wantErr: true,
		},
		{
			name:    "zero addons",
			params:  &AddAddonParams{SubscriptionID: "subs_123", Addons: nil},
			wantErr: true,
		},
		{
			// The blob is list-shaped so batching is additive; completion loops the list.
			name: "multiple addons allowed",
			params: &AddAddonParams{
				SubscriptionID: "subs_123",
				Addons:         []AddAddonRef{validAddAddonRef(), validAddAddonRef()},
			},
			wantErr: false,
		},
		{
			name:    "empty association id",
			params:  withRef(func(r *AddAddonRef) { r.AssociationID = "" }),
			wantErr: true,
		},
		{
			name:    "empty addon id",
			params:  withRef(func(r *AddAddonRef) { r.AddonID = "" }),
			wantErr: true,
		},
		{
			name:    "empty cadence",
			params:  withRef(func(r *AddAddonRef) { r.Cadence = "" }),
			wantErr: true,
		},
		{
			name:    "invalid cadence",
			params:  withRef(func(r *AddAddonRef) { r.Cadence = "quarterly" }),
			wantErr: true,
		},
		{
			name:    "invalid proration behavior",
			params:  withRef(func(r *AddAddonRef) { r.ProrationBehavior = "always" }),
			wantErr: true,
		},
		{
			// A zero start date would replay as time.Now() and rebuild line items for a
			// different day than the draft invoice was priced for.
			name:    "zero start date",
			params:  withRef(func(r *AddAddonRef) { r.StartDate = time.Time{} }),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, ierr.IsValidation(err), "expected a validation error, got %v", err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

// CheckoutConfiguration.Validate defaults unknown actions to nil, so a missing add_addon
// case would silently pass every malformed session through.
func TestCheckoutConfiguration_Validate_AddAddon(t *testing.T) {
	t.Run("missing params rejected", func(t *testing.T) {
		cfg := &CheckoutConfiguration{}
		err := cfg.Validate(CheckoutActionAddAddon)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "add_addon_params")
	})

	t.Run("invalid params propagate", func(t *testing.T) {
		cfg := &CheckoutConfiguration{
			AddAddonParams: &AddAddonParams{SubscriptionID: "", Addons: []AddAddonRef{validAddAddonRef()}},
		}
		assert.Error(t, cfg.Validate(CheckoutActionAddAddon))
	})

	t.Run("valid params accepted", func(t *testing.T) {
		cfg := &CheckoutConfiguration{
			AddAddonParams: &AddAddonParams{
				SubscriptionID: "subs_123",
				Addons:         []AddAddonRef{validAddAddonRef()},
			},
		}
		assert.NoError(t, cfg.Validate(CheckoutActionAddAddon))
	})
}

func TestCheckoutAction_Validate_AddAddon(t *testing.T) {
	assert.NoError(t, CheckoutActionAddAddon.Validate())
	assert.Equal(t, "add_addon", CheckoutActionAddAddon.String())

	// The hint listing the allowed values is a hardcoded literal, not a render of the
	// `allowed` slice, so it drifts silently unless something checks it.
	err := CheckoutAction("not_an_action").Validate()
	assert.Error(t, err)
	hints := cockroachErrors.GetAllHints(err)
	assert.NotEmpty(t, hints)
	assert.Contains(t, hints[0], "add_addon",
		"the hardcoded allowed-values hint must list every CheckoutAction constant")
}

// A builder that quietly dropped a field would be worse than the struct literal it
// replaces, since callers can no longer see the whole shape at the call site.
func TestCheckoutProviderResultBuilder(t *testing.T) {
	expires := time.Now().UTC()
	action := &PaymentAction{Type: PaymentActionTypePaymentLink, URL: "https://rzp.io/x"}

	got := NewCheckoutProviderResult().
		WithProviderSessionID("plink_1").
		WithProviderPaymentIntentID("pay_1").
		WithExpiresAt(&expires).
		WithProviderMetadata(map[string]string{"k": "v"}).
		WithNextAction(action).
		Build()

	require.NotNil(t, got)
	assert.Equal(t, "plink_1", got.ProviderSessionID)
	assert.Equal(t, "pay_1", got.ProviderPaymentIntentID)
	assert.Equal(t, &expires, got.ExpiresAt)
	assert.Equal(t, map[string]string{"k": "v"}, got.ProviderMetadata)
	assert.Equal(t, action, got.NextAction)
}

// Fragments are the common case — a webhook knows the payment id and nothing else —
// and everything unset must stay zero so MergeOnto can tell it apart from a value.
func TestCheckoutProviderResultBuilder_Fragment(t *testing.T) {
	got := NewCheckoutProviderResult().WithProviderPaymentIntentID("pay_1").Build()

	require.NotNil(t, got)
	assert.Equal(t, "pay_1", got.ProviderPaymentIntentID)
	assert.Empty(t, got.ProviderSessionID)
	assert.Nil(t, got.NextAction)
	assert.Nil(t, got.ExpiresAt)
	assert.Nil(t, got.ProviderMetadata)
}

func TestCheckoutProviderResultBuilder_NilBuilder(t *testing.T) {
	var b *CheckoutProviderResultBuilder
	assert.Nil(t, b.Build())
}
