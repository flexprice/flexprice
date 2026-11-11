package selfbilling

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var activeSub = &remoteSubscription{id: "sub_us:t1", status: types.SubscriptionStatusActive}

func TestTargetStatus(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	sub := func(status types.SubscriptionStatus) *remoteSubscription {
		return &remoteSubscription{id: "s", status: status}
	}
	trialEndedAt := func(trialEnd time.Time) *remoteSubscription {
		return &remoteSubscription{id: "s", status: types.SubscriptionStatusTrialing, trialEnd: &trialEnd}
	}
	activeEndedAt := func(trialEnd time.Time) *remoteSubscription {
		return &remoteSubscription{id: "s", status: types.SubscriptionStatusActive, trialEnd: &trialEnd}
	}
	trialing, active, suspended := types.TenantInternalStatusTrialing, types.TenantInternalStatusActive, types.TenantInternalStatusSuspended

	tests := []struct {
		name       string
		current    types.TenantInternalStatus
		sub        *remoteSubscription
		want       types.TenantInternalStatus
		wantReason suspensionReason
	}{
		{"no_subscription_keeps_trialing", trialing, nil, trialing, ""},
		{"no_subscription_keeps_active", active, nil, active, ""},
		{"no_subscription_keeps_suspended", suspended, nil, suspended, ""},
		{"trialing_subscription_keeps_trialing", trialing, sub(types.SubscriptionStatusTrialing), trialing, ""},
		{"paid_activates_trialing", trialing, sub(types.SubscriptionStatusActive), active, ""},
		{"paid_restores_suspended", suspended, sub(types.SubscriptionStatusActive), active, ""},
		{"paid_keeps_active", active, sub(types.SubscriptionStatusActive), active, ""},
		{"unpaid_trial_end_suspends", trialing, sub(types.SubscriptionStatusIncomplete), suspended, suspensionReasonPaymentIncomplete},
		{"cancelled_trial_suspends", trialing, sub(types.SubscriptionStatusCancelled), suspended, suspensionReasonSubscriptionCancelled},
		{"active_never_suspended_on_cancel", active, sub(types.SubscriptionStatusCancelled), active, ""},
		{"active_never_suspended_on_unpaid", active, sub(types.SubscriptionStatusIncomplete), active, ""},
		{"suspended_stays_on_cancel", suspended, sub(types.SubscriptionStatusCancelled), suspended, ""},
		{"paused_ignored", trialing, sub(types.SubscriptionStatusPaused), trialing, ""},
		{"trial_not_yet_ended_keeps_trialing", trialing, trialEndedAt(now.Add(24 * time.Hour)), trialing, ""},
		{"trial_ended_within_grace_keeps_trialing", trialing, trialEndedAt(now.Add(-30 * time.Minute)), trialing, ""},
		{"trial_ended_exactly_at_grace_keeps_trialing", trialing, trialEndedAt(now.Add(-trialExpiryGrace)), trialing, ""},
		{"trial_expired_past_grace_suspends", trialing, trialEndedAt(now.Add(-2 * time.Hour)), suspended, suspensionReasonTrialExpired},
		{"expired_trial_never_suspends_active", active, trialEndedAt(now.Add(-48 * time.Hour)), active, ""},
		{"expired_trial_keeps_suspended", suspended, trialEndedAt(now.Add(-48 * time.Hour)), suspended, ""},
		{"converted_after_expiry_activates", trialing, activeEndedAt(now.Add(-48 * time.Hour)), active, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, reason := targetStatus(tt.current, tt.sub, now)
			assert.Equal(t, tt.want, status)
			assert.Equal(t, tt.wantReason, reason)
		})
	}
}

func TestUpdateTenantStatus_ActiveUnlocksProduction(t *testing.T) {
	tests := []struct {
		name          string
		from          types.TenantInternalStatus
		prodLimit     int // 0 = leave the default
		wantProdLimit int
	}{
		{"trialing_to_active", types.TenantInternalStatusTrialing, 0, 1},
		{"suspended_to_active", types.TenantInternalStatusSuspended, 0, 1},
		{"already_active_is_repaired", types.TenantInternalStatusActive, 0, 1},
		{"higher_limit_kept", types.TenantInternalStatusTrialing, 3, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, d := newTestService(newFakeClient())
			seedTenant(t, d, "t1", tt.from)
			ctx := tenantCtx("t1")
			if tt.prodLimit > 0 {
				_, err := s.settings.UpdateSettingByKey(ctx, types.SettingKeyTenantConfig, &dto.UpdateSettingRequest{
					Value: map[string]interface{}{"production": tt.prodLimit},
				})
				require.NoError(t, err)
			}

			for i := 0; i < 2; i++ {
				got, err := d.tenants.GetByID(ctx, "t1")
				require.NoError(t, err)
				status, err := s.updateTenantStatus(ctx, got, activeSub)
				require.NoError(t, err)
				assert.Equal(t, types.TenantInternalStatusActive, status)
			}

			got, err := d.tenants.GetByID(ctx, "t1")
			require.NoError(t, err)
			assert.Equal(t, types.TenantInternalStatusActive, got.InternalStatus)

			setting, err := s.settings.GetSettingByKeyUnchecked(ctx, types.SettingKeyTenantConfig)
			require.NoError(t, err)
			assert.EqualValues(t, tt.wantProdLimit, setting.Value["production"])
			assert.Equal(t, 1, productionEnvs(t, d, "t1"))
		})
	}
}

// racingEnvStore inserts a production environment right after the first count, as a concurrent activation would.
type racingEnvStore struct {
	*testutil.InMemoryEnvironmentStore
	counts int
}

func (r *racingEnvStore) CountByType(ctx context.Context, envType types.EnvironmentType) (int, error) {
	r.counts++
	n, err := r.InMemoryEnvironmentStore.CountByType(ctx, envType)
	if r.counts == 1 {
		winner := &environment.Environment{
			ID:        "env_winner",
			Name:      "Production",
			Type:      types.EnvironmentProduction,
			BaseModel: types.GetDefaultBaseModel(ctx),
		}
		if cerr := r.InMemoryEnvironmentStore.Create(ctx, winner); cerr != nil {
			return 0, cerr
		}
	}
	return n, err
}

func TestUpdateTenantStatus_EnvCreationRaceTreatedAsDone(t *testing.T) {
	s, d := newTestService(newFakeClient())
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	racing := &racingEnvStore{InMemoryEnvironmentStore: d.envs}
	s.EnvironmentRepo = racing
	s.envs = service.NewEnvironmentService(racing, service.NewEnvAccessService(s.Config), s.settings, s.ServiceParams)

	tn, err := d.tenants.GetByID(context.Background(), "t1")
	require.NoError(t, err)
	_, err = s.updateTenantStatus(tenantCtx("t1"), tn, activeSub)
	require.NoError(t, err)

	// The first read saw 0, CreateEnvironment's limit check refused ours, the recount found the winner.
	assert.Equal(t, 3, racing.counts)
	assert.Equal(t, 1, productionEnvs(t, d, "t1"))

	got, err := d.tenants.GetByID(context.Background(), "t1")
	require.NoError(t, err)
	assert.Equal(t, types.TenantInternalStatusActive, got.InternalStatus)
}

func TestUpdateTenantStatus_NotActiveLeavesProductionLocked(t *testing.T) {
	s, d := newTestService(newFakeClient())
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	tn, err := d.tenants.GetByID(context.Background(), "t1")
	require.NoError(t, err)

	status, err := s.updateTenantStatus(tenantCtx("t1"), tn, &remoteSubscription{id: "sub_us:t1", status: types.SubscriptionStatusIncomplete})
	require.NoError(t, err)

	assert.Equal(t, types.TenantInternalStatusSuspended, status)
	assert.Equal(t, 0, productionEnvs(t, d, "t1"))
}

func TestUpdateTenantStatus_Alerts(t *testing.T) {
	sub := func(status types.SubscriptionStatus) *remoteSubscription {
		return &remoteSubscription{id: "sub_us:t1", status: status}
	}
	trialing, active, suspended := types.TenantInternalStatusTrialing, types.TenantInternalStatusActive, types.TenantInternalStatusSuspended

	expiredTrial := &remoteSubscription{
		id:       "sub_us:t1",
		status:   types.SubscriptionStatusTrialing,
		trialEnd: lo.ToPtr(time.Now().Add(-2 * trialExpiryGrace)),
	}

	tests := []struct {
		name       string
		from       types.TenantInternalStatus
		sub        *remoteSubscription
		wantEvent  alertEvent // empty = no alert
		wantTo     types.TenantInternalStatus
		wantReason suspensionReason
	}{
		{"trialing_to_active", trialing, sub(types.SubscriptionStatusActive), alertTenantActivated, active, ""},
		{"suspended_to_active", suspended, sub(types.SubscriptionStatusActive), alertTenantActivated, active, ""},
		{"trialing_to_suspended", trialing, sub(types.SubscriptionStatusIncomplete), alertTenantSuspended, suspended, suspensionReasonPaymentIncomplete},
		{"cancelled_to_suspended", trialing, sub(types.SubscriptionStatusCancelled), alertTenantSuspended, suspended, suspensionReasonSubscriptionCancelled},
		{"expired_trial_to_suspended", trialing, expiredTrial, alertTenantSuspended, suspended, suspensionReasonTrialExpired},
		{"active_unchanged", active, sub(types.SubscriptionStatusActive), "", active, ""},
		{"trialing_unchanged", trialing, sub(types.SubscriptionStatusTrialing), "", trialing, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, d := newTestService(newFakeClient())
			sink := newAlertSink(t, s)
			seedTenant(t, d, "t1", tt.from)
			tn, err := d.tenants.GetByID(context.Background(), "t1")
			require.NoError(t, err)

			status, err := s.updateTenantStatus(tenantCtx("t1"), tn, tt.sub)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTo, status)

			if tt.wantEvent == "" {
				assert.Empty(t, sink.waitFor(t, 0))
				return
			}

			got := sink.waitFor(t, 1)
			assert.Equal(t, alert{
				Event:      tt.wantEvent,
				TenantID:   "t1",
				TenantName: "t1",
				Region:     "us",
				FromStatus: tt.from,
				ToStatus:   tt.wantTo,
				Reason:     tt.wantReason,
				OccurredAt: got[0].OccurredAt,
			}, got[0])
		})
	}
}
