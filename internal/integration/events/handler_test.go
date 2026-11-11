package events

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/logger"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"go.uber.org/zap"
)

func testLogger() *logger.Logger {
	return &logger.Logger{SugaredLogger: zap.NewNop().Sugar()}
}

func TestProcessMessage_IgnoresUnknownEvent(t *testing.T) {
	h := &handler{
		deps: Deps{
			Logger: testLogger(),
			Config: &config.Configuration{},
		},
		processors: map[types.WebhookEventName]eventProcessor{},
	}

	ev := types.WebhookEvent{
		EventName: "unknown.event",
		TenantID:  "ten_1",
		Payload:   json.RawMessage(`{"x":"y"}`),
	}
	b, _ := json.Marshal(ev)
	msg := message.NewMessage("msg-1", b)

	if err := h.processMessage(context.Background(), msg); err != nil {
		t.Fatalf("expected nil error for unknown event, got %v", err)
	}
}

func TestProcessMessage_CustomerCreatedDispatchError(t *testing.T) {
	h, ok := NewHandler(Deps{
		Logger: testLogger(),
		Config: &config.Configuration{
			IntegrationEvents: config.IntegrationEventsConfig{Enabled: true},
		},
	}).(*handler)
	if !ok {
		t.Fatalf("expected concrete *handler")
	}

	ev := types.WebhookEvent{
		EventName:     types.WebhookEventCustomerCreated,
		TenantID:      "ten_1",
		EnvironmentID: "env_1",
		UserID:        "usr_1",
		Payload:       json.RawMessage(`{"customer_id":"cus_1"}`),
	}
	b, _ := json.Marshal(ev)
	msg := message.NewMessage("msg-2", b)

	if err := h.processMessage(context.Background(), msg); err == nil {
		t.Fatalf("expected error when temporal service is unavailable")
	}
}

type fakeConnectionRepo struct {
	connection.Repository
}

func (fakeConnectionRepo) GetByProvider(_ context.Context, _ types.SecretProvider) (*connection.Connection, error) {
	return &connection.Connection{SyncConfig: &types.SyncConfig{Invoice: &types.EntitySyncConfig{Outbound: true}}}, nil
}

type fakeMappedEIMRepo struct {
	entityintegrationmapping.Repository
}

func (fakeMappedEIMRepo) Count(_ context.Context, _ *types.EntityIntegrationMappingFilter) (int, error) {
	return 1, nil
}

type fakeWorkflowRun struct {
	temporalmodels.WorkflowRun
}

func (fakeWorkflowRun) GetID() string    { return "wf_1" }
func (fakeWorkflowRun) GetRunID() string { return "run_1" }

type fakeTemporalService struct {
	temporalservice.TemporalService
	inputs []interface{}
}

func (f *fakeTemporalService) ExecuteWorkflow(_ context.Context, _ types.TemporalWorkflowType, params interface{}) (temporalmodels.WorkflowRun, error) {
	f.inputs = append(f.inputs, params)
	return fakeWorkflowRun{}, nil
}

func TestTriggerZohoBooksIfEnabled_MappedInvoice(t *testing.T) {
	tests := []struct {
		name         string
		resync       bool
		wantWorkflow bool
	}{
		{name: "automatic sync skips a mapped invoice"},
		{name: "resync starts the workflow", resync: true, wantWorkflow: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			temporalSvc := &fakeTemporalService{}
			in := invoiceVendorSyncInput{TenantID: "ten_1", EnvironmentID: "env_1", InvoiceID: "inv_1", Resync: tc.resync}

			err := triggerZohoBooksIfEnabled(context.Background(), fakeConnectionRepo{}, fakeMappedEIMRepo{}, temporalSvc, testLogger(), in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !tc.wantWorkflow {
				if len(temporalSvc.inputs) != 0 {
					t.Fatalf("expected no workflow, got %d", len(temporalSvc.inputs))
				}
				return
			}
			if len(temporalSvc.inputs) != 1 {
				t.Fatalf("expected one workflow, got %d", len(temporalSvc.inputs))
			}
			input, ok := temporalSvc.inputs[0].(*temporalmodels.ZohoBooksInvoiceSyncWorkflowInput)
			if !ok || !input.Resync {
				t.Fatalf("expected Zoho input with Resync set, got %#v", temporalSvc.inputs[0])
			}
		})
	}
}
