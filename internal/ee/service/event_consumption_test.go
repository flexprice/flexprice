package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingEventRepo struct {
	events.Repository
	inserted []*events.Event
}

func (r *recordingEventRepo) BulkInsertEvents(_ context.Context, evs []*events.Event) error {
	r.inserted = append(r.inserted, evs...)
	return nil
}

// TestEventConsumption_NoBillingCopy checks only the tenant's own event is stored, even where the billing tenant is local.
func TestEventConsumption_NoBillingCopy(t *testing.T) {
	cfg := &config.Configuration{Billing: config.BillingConfig{
		TenantID:      "t_billing",
		EnvironmentID: "env_billing",
	}}
	ev := events.NewEvent("api_call", "t1", "cust_1", map[string]interface{}{}, time.Now().UTC(), "evt_1", "", "api", "env_1")
	payload, err := json.Marshal(ev)
	require.NoError(t, err)

	tests := []struct {
		name string
		run  func(s *eventConsumptionService) error
	}{
		{"processMessage", func(s *eventConsumptionService) error {
			return s.processMessage(context.Background(), message.NewMessage("m1", payload))
		}},
		{"ProcessRawEvent", func(s *eventConsumptionService) error {
			return s.ProcessRawEvent(context.Background(), payload)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &recordingEventRepo{}
			s := &eventConsumptionService{
				ServiceParams: ServiceParams{Logger: logger.NewNoopLogger(), Config: cfg},
				eventRepo:     repo,
			}

			require.NoError(t, tt.run(s))
			require.Len(t, repo.inserted, 1, "only the tenant's own event is stored")
			assert.Equal(t, "t1", repo.inserted[0].TenantID)
		})
	}
}

type scopeRecordingWindow struct {
	tenants []string
}

func (w *scopeRecordingWindow) IsSuppressed(ctx context.Context, _ string, _ time.Time) bool {
	w.tenants = append(w.tenants, types.GetTenantID(ctx))
	return false
}

func scopedMsg(t *testing.T, batch events.EventBatch) *message.Message {
	t.Helper()
	payload, err := json.Marshal(batch)
	require.NoError(t, err)
	return message.NewMessage("m1", payload)
}

func mkEvt(id, tenant, env string) *events.Event {
	return events.NewEvent("api_call", tenant, "cust_1", map[string]interface{}{}, time.Now().UTC(), id, "", "api", env)
}

func TestEventConsumption_BulkScope(t *testing.T) {
	tests := []struct {
		name      string
		batch     events.EventBatch
		mdTenant  string
		wantIDs   []string
		wantTenat []string
	}{
		{"consistent", events.EventBatch{TenantID: "tA", EnvironmentID: "e1", Events: []*events.Event{mkEvt("a", "tA", "e1")}}, "", []string{"a"}, []string{"tA"}},
		{"empty falls back", events.EventBatch{TenantID: "tA", EnvironmentID: "e1", Events: []*events.Event{mkEvt("a", "", "")}}, "", []string{"a"}, []string{"tA"}},
		{"other tenant dropped", events.EventBatch{TenantID: "tA", EnvironmentID: "e1", Events: []*events.Event{mkEvt("a", "tB", "e1"), mkEvt("b", "tA", "e1")}}, "", []string{"b"}, []string{"tA"}},
		{"other env dropped", events.EventBatch{TenantID: "tA", EnvironmentID: "e1", Events: []*events.Event{mkEvt("a", "tA", "e2")}}, "", nil, nil},
		{"metadata mismatch dropped", events.EventBatch{TenantID: "tA", EnvironmentID: "e1", Events: []*events.Event{mkEvt("a", "tA", "e1")}}, "tB", nil, nil},
		{"no scope anywhere dropped", events.EventBatch{Events: []*events.Event{mkEvt("a", "tB", "e1")}}, "", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &recordingEventRepo{}
			win := &scopeRecordingWindow{}
			s := &eventConsumptionService{
				ServiceParams:  ServiceParams{Logger: logger.NewNoopLogger(), Config: &config.Configuration{}},
				eventRepo:      repo,
				deletionWindow: win,
			}
			ctx := context.WithValue(context.Background(), types.CtxTenantID, tt.mdTenant)

			require.NoError(t, s.processBulkMessage(ctx, scopedMsg(t, tt.batch)))

			var ids []string
			for _, e := range repo.inserted {
				ids = append(ids, e.ID)
			}
			assert.Equal(t, tt.wantIDs, ids)
			assert.Equal(t, tt.wantTenat, win.tenants)
		})
	}
}

func TestEventConsumption_SingleScope(t *testing.T) {
	tests := []struct {
		name     string
		mdTenant string
		mdEnv    string
		evt      *events.Event
		want     int
	}{
		{"consistent", "tA", "e1", mkEvt("a", "tA", "e1"), 1},
		{"empty metadata uses payload", "", "", mkEvt("a", "tA", "e1"), 1},
		{"tenant mismatch", "tA", "e1", mkEvt("a", "tB", "e1"), 0},
		{"env mismatch", "tA", "e1", mkEvt("a", "tA", "e2"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &recordingEventRepo{}
			s := &eventConsumptionService{
				ServiceParams: ServiceParams{Logger: logger.NewNoopLogger(), Config: &config.Configuration{}},
				eventRepo:     repo,
			}
			payload, err := json.Marshal(tt.evt)
			require.NoError(t, err)
			ctx := context.WithValue(context.Background(), types.CtxTenantID, tt.mdTenant)
			ctx = context.WithValue(ctx, types.CtxEnvironmentID, tt.mdEnv)

			require.NoError(t, s.processMessage(ctx, message.NewMessage("m1", payload)))
			assert.Len(t, repo.inserted, tt.want)
		})
	}
}

func TestMeterUsageTracking_ScopeMismatchDropped(t *testing.T) {
	newSvc := func() *meterUsageTrackingService {
		return &meterUsageTrackingService{ServiceParams: ServiceParams{
			Logger: logger.NewNoopLogger(),
			Config: &config.Configuration{},
		}}
	}
	ctx := context.WithValue(context.Background(), types.CtxTenantID, "tA")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "e1")

	// nil MeterRepo panics if a mismatched event is processed.
	t.Run("bulk event tenant", func(t *testing.T) {
		b := events.EventBatch{TenantID: "tA", EnvironmentID: "e1", Events: []*events.Event{mkEvt("a", "tB", "e1")}}
		require.NoError(t, newSvc().processBulkMessage(ctx, scopedMsg(t, b)))
	})
	t.Run("bulk metadata vs envelope", func(t *testing.T) {
		b := events.EventBatch{TenantID: "tB", EnvironmentID: "e1", Events: []*events.Event{mkEvt("a", "tB", "e1")}}
		require.NoError(t, newSvc().processBulkMessage(ctx, scopedMsg(t, b)))
	})
	t.Run("single payload vs metadata", func(t *testing.T) {
		payload, err := json.Marshal(mkEvt("a", "tB", "e1"))
		require.NoError(t, err)
		require.NoError(t, newSvc().processMessage(ctx, message.NewMessage("m1", payload)))
	})
}

type lookupRecordingMeterRepo struct {
	meter.Repository
	scopes []string
}

func (r *lookupRecordingMeterRepo) GetMatchingMetersByEventName(ctx context.Context, _ string) ([]*meter.Meter, error) {
	r.scopes = append(r.scopes, types.GetTenantID(ctx)+"|"+types.GetEnvironmentID(ctx))
	return nil, nil
}

func scopedCtx(tenant, env string) context.Context {
	ctx := context.WithValue(context.Background(), types.CtxTenantID, tenant)
	return context.WithValue(ctx, types.CtxEnvironmentID, env)
}

func TestBulkScope_EffectiveScope(t *testing.T) {
	tests := []struct {
		name    string
		batch   events.EventBatch
		mdTen   string
		mdEnv   string
		wantIDs []string
		// Tenant and env seen by the meter lookup.
		wantScope string
	}{
		{"empty batch tenant, ctx A, event B", events.EventBatch{EnvironmentID: "e1", Events: []*events.Event{mkEvt("a", "tB", "e1")}}, "tA", "e1", nil, ""},
		{"empty batch env, ctx e1, event e2", events.EventBatch{TenantID: "tA", Events: []*events.Event{mkEvt("a", "tA", "e2")}}, "tA", "e1", nil, ""},
		{"fully empty envelope, ctx A, event B", events.EventBatch{Events: []*events.Event{mkEvt("a", "tB", "e2")}}, "tA", "e1", nil, ""},
		{"empty batch tenant, ctx A, event A", events.EventBatch{EnvironmentID: "e1", Events: []*events.Event{mkEvt("a", "tA", "e1")}}, "tA", "e1", []string{"a"}, "tA|e1"},
		{"empty batch, event empty falls back to ctx", events.EventBatch{Events: []*events.Event{mkEvt("a", "", "")}}, "tA", "e1", []string{"a"}, "tA|e1"},
		{"consistent", events.EventBatch{TenantID: "tA", EnvironmentID: "e1", Events: []*events.Event{mkEvt("a", "tA", "e1")}}, "tA", "e1", []string{"a"}, "tA|e1"},
		{"no scope anywhere, event scoped dropped", events.EventBatch{Events: []*events.Event{mkEvt("a", "tB", "e2")}}, "", "", nil, ""},
		{"no envelope, two tenants same id dropped", events.EventBatch{Events: []*events.Event{mkEvt("a", "tA", "e1"), mkEvt("a", "tB", "e1")}}, "", "", nil, ""},
		{"envelope env only, tenant from ctx", events.EventBatch{EnvironmentID: "e1", Events: []*events.Event{mkEvt("a", "", "")}}, "tA", "", []string{"a"}, "tA|e1"},
	}
	for _, tt := range tests {
		t.Run("events/"+tt.name, func(t *testing.T) {
			repo := &recordingEventRepo{}
			s := &eventConsumptionService{
				ServiceParams: ServiceParams{Logger: logger.NewNoopLogger(), Config: &config.Configuration{}},
				eventRepo:     repo,
			}
			require.NoError(t, s.processBulkMessage(scopedCtx(tt.mdTen, tt.mdEnv), scopedMsg(t, tt.batch)))
			var ids []string
			for _, e := range repo.inserted {
				ids = append(ids, e.ID)
			}
			assert.Equal(t, tt.wantIDs, ids)
		})
		t.Run("meter/"+tt.name, func(t *testing.T) {
			mr := &lookupRecordingMeterRepo{}
			s := &meterUsageTrackingService{ServiceParams: ServiceParams{
				Logger:    logger.NewNoopLogger(),
				Config:    &config.Configuration{},
				MeterRepo: mr,
			}}
			require.NoError(t, s.processBulkMessage(scopedCtx(tt.mdTen, tt.mdEnv), scopedMsg(t, tt.batch)))
			if tt.wantIDs == nil {
				assert.Empty(t, mr.scopes)
				return
			}
			assert.Equal(t, []string{tt.wantScope}, mr.scopes)
		})
	}
}

func TestMeterUsageBulk_EmptyScopeSkipped(t *testing.T) {
	mr := &lookupRecordingMeterRepo{}
	s := &meterUsageTrackingService{ServiceParams: ServiceParams{
		Logger:    logger.NewNoopLogger(),
		Config:    &config.Configuration{},
		MeterRepo: mr,
	}}
	b := events.EventBatch{Events: []*events.Event{mkEvt("a", "", "")}}
	require.NoError(t, s.processBulkMessage(context.Background(), scopedMsg(t, b)))
	assert.Empty(t, mr.scopes)
}
