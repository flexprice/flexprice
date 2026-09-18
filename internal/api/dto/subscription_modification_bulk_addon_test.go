package dto

import (
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

func validBulkAddonAdd() *AddAddonToSubscriptionRequest {
	return &AddAddonToSubscriptionRequest{
		AddonID:           "addon_123",
		Cadence:           types.AddonCadenceRecurring,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}
}

func validBulkAddonRemove() *RemoveAddonRequest {
	return &RemoveAddonRequest{
		AddonAssociationID: "addon_assoc_123",
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
	}
}

func TestSubModifyBulkAddonParams_Validate(t *testing.T) {
	manyAdds := make([]*AddAddonToSubscriptionRequest, maxAddonBatchEntries+1)
	for i := range manyAdds {
		manyAdds[i] = validBulkAddonAdd()
	}

	withAdd := func(mutate func(*AddAddonToSubscriptionRequest)) *SubModifyBulkAddonParams {
		add := validBulkAddonAdd()
		mutate(add)
		return &SubModifyBulkAddonParams{Adds: []*AddAddonToSubscriptionRequest{add}}
	}
	withRemove := func(mutate func(*RemoveAddonRequest)) *SubModifyBulkAddonParams {
		remove := validBulkAddonRemove()
		mutate(remove)
		return &SubModifyBulkAddonParams{Removes: []*RemoveAddonRequest{remove}}
	}

	tests := []struct {
		name    string
		params  *SubModifyBulkAddonParams
		wantErr bool
	}{
		{name: "nil params", params: nil, wantErr: true},
		{name: "no entries", params: &SubModifyBulkAddonParams{}, wantErr: true},
		{
			name:    "adds only",
			params:  &SubModifyBulkAddonParams{Adds: []*AddAddonToSubscriptionRequest{validBulkAddonAdd()}},
			wantErr: false,
		},
		{
			name:    "removes only",
			params:  &SubModifyBulkAddonParams{Removes: []*RemoveAddonRequest{validBulkAddonRemove()}},
			wantErr: false,
		},
		{
			name: "mixed swap",
			params: &SubModifyBulkAddonParams{
				Adds:    []*AddAddonToSubscriptionRequest{validBulkAddonAdd()},
				Removes: []*RemoveAddonRequest{validBulkAddonRemove()},
			},
			wantErr: false,
		},
		{
			// Two instances of one addon is a legitimate request.
			name: "same addon added twice",
			params: &SubModifyBulkAddonParams{
				Adds: []*AddAddonToSubscriptionRequest{validBulkAddonAdd(), validBulkAddonAdd()},
			},
			wantErr: false,
		},
		{
			name:    "over the entry cap",
			params:  &SubModifyBulkAddonParams{Adds: manyAdds},
			wantErr: true,
		},
		{
			name: "duplicate association in removes",
			params: &SubModifyBulkAddonParams{
				Removes: []*RemoveAddonRequest{validBulkAddonRemove(), validBulkAddonRemove()},
			},
			wantErr: true,
		},
		{
			name:    "nil add entry",
			params:  &SubModifyBulkAddonParams{Adds: []*AddAddonToSubscriptionRequest{nil}},
			wantErr: true,
		},
		{
			name:    "nil remove entry",
			params:  &SubModifyBulkAddonParams{Removes: []*RemoveAddonRequest{nil}},
			wantErr: true,
		},
		{
			name:    "add missing addon id",
			params:  withAdd(func(a *AddAddonToSubscriptionRequest) { a.AddonID = "" }),
			wantErr: true,
		},
		{
			name:    "add invalid proration behavior",
			params:  withAdd(func(a *AddAddonToSubscriptionRequest) { a.ProrationBehavior = "always" }),
			wantErr: true,
		},
		{
			name:    "remove missing association id",
			params:  withRemove(func(r *RemoveAddonRequest) { r.AddonAssociationID = "" }),
			wantErr: true,
		},
		{
			name:    "add with change_at immediate",
			params:  withAdd(func(a *AddAddonToSubscriptionRequest) { a.ChangeAt = lo.ToPtr(types.ScheduleTypeImmediate) }),
			wantErr: false,
		},
		{
			name:    "remove with change_at end of period",
			params:  withRemove(func(r *RemoveAddonRequest) { r.ChangeAt = lo.ToPtr(types.ScheduleTypePeriodEnd) }),
			wantErr: false,
		},
		{
			name:    "add with unknown change_at",
			params:  withAdd(func(a *AddAddonToSubscriptionRequest) { a.ChangeAt = lo.ToPtr(types.ScheduleType("tomorrow")) }),
			wantErr: true,
		},
		{
			name:    "add with empty change_at",
			params:  withAdd(func(a *AddAddonToSubscriptionRequest) { a.ChangeAt = lo.ToPtr(types.ScheduleType("")) }),
			wantErr: true,
		},
		{
			// Two ways to say when would let one request contradict itself.
			name: "add with both change_at and start_date",
			params: withAdd(func(a *AddAddonToSubscriptionRequest) {
				a.ChangeAt = lo.ToPtr(types.ScheduleTypeImmediate)
				a.StartDate = lo.ToPtr(time.Now())
			}),
			wantErr: true,
		},
		{
			name: "remove with both change_at and effective_date",
			params: withRemove(func(r *RemoveAddonRequest) {
				r.ChangeAt = lo.ToPtr(types.ScheduleTypePeriodEnd)
				r.EffectiveDate = lo.ToPtr(time.Now())
			}),
			wantErr: true,
		},
		{
			// nil means "use the default"; an explicit zero timestamp is a client bug.
			name:    "explicit zero start date on add",
			params:  withAdd(func(a *AddAddonToSubscriptionRequest) { a.StartDate = lo.ToPtr(time.Time{}) }),
			wantErr: true,
		},
		{
			name:    "explicit zero effective date on remove",
			params:  withRemove(func(r *RemoveAddonRequest) { r.EffectiveDate = lo.ToPtr(time.Time{}) }),
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

// Entries are the single-addon requests, so their server-only fields must not be settable
// from the wire.
func TestSubModifyBulkAddonParams_ServerOnlyFieldsAreNotClientSettable(t *testing.T) {
	add := validBulkAddonAdd()
	assert.False(t, add.PreviewOnly)
	assert.False(t, add.SkipEntityValidation)
	assert.False(t, validBulkAddonRemove().PreviewOnly)
}

func TestExecuteSubscriptionModifyRequest_BulkAddon(t *testing.T) {
	valid := &SubModifyBulkAddonParams{Adds: []*AddAddonToSubscriptionRequest{validBulkAddonAdd()}}

	t.Run("params required for type addons", func(t *testing.T) {
		req := ExecuteSubscriptionModifyRequest{Type: SubscriptionModifyTypeAddon}
		assert.Error(t, req.Validate())
	})

	t.Run("valid batch accepted", func(t *testing.T) {
		req := ExecuteSubscriptionModifyRequest{Type: SubscriptionModifyTypeAddon, BulkAddonParams: valid}
		assert.NoError(t, req.Validate())
	})

	t.Run("checkout accepted when the batch can charge", func(t *testing.T) {
		req := ExecuteSubscriptionModifyRequest{
			Type:            SubscriptionModifyTypeAddon,
			BulkAddonParams: valid,
			Checkout:        &CheckoutParams{PaymentParams: PaymentParams{PaymentProvider: types.CheckoutPaymentProviderRazorpay}},
		}
		assert.NoError(t, req.Validate())
	})

	// A removes-only batch can only ever credit: the change service finds nothing to collect
	// and applies immediately, so checkout is accepted and ignored rather than rejected.
	// type "addon" carries either shape, so the request has to say which one it means.
	t.Run("addon_params and addon_bulk_params together are rejected", func(t *testing.T) {
		req := ExecuteSubscriptionModifyRequest{
			Type:            SubscriptionModifyTypeAddon,
			AddonParams:     &SubModifyAddonParams{Action: SubscriptionModificationActionAdd, Add: validBulkAddonAdd()},
			BulkAddonParams: valid,
		}
		err := req.Validate()
		assert.Error(t, err)
		assert.True(t, ierr.IsValidation(err))
	})

	t.Run("neither params block is rejected", func(t *testing.T) {
		req := ExecuteSubscriptionModifyRequest{Type: SubscriptionModifyTypeAddon}
		assert.Error(t, req.Validate())
	})

	t.Run("addon_params alone still works", func(t *testing.T) {
		req := ExecuteSubscriptionModifyRequest{
			Type:        SubscriptionModifyTypeAddon,
			AddonParams: &SubModifyAddonParams{Action: SubscriptionModificationActionAdd, Add: validBulkAddonAdd()},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("checkout accepted for a removes-only batch", func(t *testing.T) {
		req := ExecuteSubscriptionModifyRequest{
			Type:            SubscriptionModifyTypeAddon,
			BulkAddonParams: &SubModifyBulkAddonParams{Removes: []*RemoveAddonRequest{validBulkAddonRemove()}},
			Checkout:        &CheckoutParams{PaymentParams: PaymentParams{PaymentProvider: types.CheckoutPaymentProviderRazorpay}},
		}
		assert.NoError(t, req.Validate())
	})
}
