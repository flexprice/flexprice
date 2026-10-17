package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/logger"
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

func TestEventConsumption_NoBillingCopy(t *testing.T) {
	cfg := &config.Configuration{Billing: config.BillingConfig{TenantID: "t_billing", EnvironmentID: "env_billing"}}
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
			s := &eventConsumptionService{ServiceParams: ServiceParams{Logger: logger.NewNoopLogger(), Config: cfg}, eventRepo: repo}
			require.NoError(t, tt.run(s))
			require.Len(t, repo.inserted, 1, "only the tenant's own event is stored")
			assert.Equal(t, "t1", repo.inserted[0].TenantID)
		})
	}
}
