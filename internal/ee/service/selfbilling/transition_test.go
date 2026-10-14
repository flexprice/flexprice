package selfbilling

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestDecide(t *testing.T) {
	sub := func(status string) *remoteSubscription { return &remoteSubscription{id: "s", status: status} }
	trialing, active, suspended := types.TenantInternalStatusTrialing, types.TenantInternalStatusActive, types.TenantInternalStatusSuspended

	tests := []struct {
		name          string
		local         types.TenantInternalStatus
		remote        *remoteSubscription
		wantNext      types.TenantInternalStatus
		wantChanged   bool
		wantProvision bool
	}{
		{"no_remote_trialing_provisions", trialing, nil, trialing, false, true},
		{"no_remote_active_left_alone", active, nil, active, false, false},
		{"no_remote_suspended_left_alone", suspended, nil, suspended, false, false},
		{"remote_trialing_stays", trialing, sub("trialing"), trialing, false, false},
		{"remote_active_activates_trialing", trialing, sub("active"), active, true, false},
		{"remote_active_restores_suspended", suspended, sub("active"), active, true, false},
		{"remote_active_active_noop", active, sub("active"), active, false, false},
		{"incomplete_trialing_suspends", trialing, sub("incomplete"), suspended, true, false},
		{"cancelled_trialing_suspends", trialing, sub("cancelled"), suspended, true, false},
		{"never_auto_suspend_active_cancelled", active, sub("cancelled"), active, false, false},
		{"never_auto_suspend_active_incomplete", active, sub("incomplete"), active, false, false},
		{"suspended_cancelled_stays", suspended, sub("cancelled"), suspended, false, false},
		{"paused_ignored", trialing, sub("paused"), trialing, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := decide(tt.local, tt.remote)
			assert.Equal(t, tt.wantNext, d.next)
			assert.Equal(t, tt.wantChanged, d.changed)
			assert.Equal(t, tt.wantProvision, d.provision)
		})
	}
}
