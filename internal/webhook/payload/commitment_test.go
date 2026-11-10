package payload

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// fakeCommitmentService serves Get only; any other call panics.
type fakeCommitmentService struct {
	service.CommitmentService
	resp  *dto.CommitmentResponse
	gotID string
}

func (f *fakeCommitmentService) Get(_ context.Context, id string) (*dto.CommitmentResponse, error) {
	f.gotID = id
	return f.resp, nil
}

func TestCommitmentPayloadBuilder_BuildPayload(t *testing.T) {
	value := decimal.NewFromInt(1200)
	svc := &fakeCommitmentService{resp: &dto.CommitmentResponse{
		Commitment:      &commitment.Commitment{ID: "cmt_1", SubscriptionID: "subs_1", Name: "Base"},
		CommitmentValue: &value,
	}}
	b := NewCommitmentPayloadBuilder(&Services{CommitmentService: svc})

	data, err := json.Marshal(webhookDto.InternalCommitmentEvent{CommitmentID: "cmt_1", SubscriptionID: "subs_1", TenantID: "ten_1"})
	require.NoError(t, err)

	out, err := b.BuildPayload(context.Background(), types.WebhookEventCommitmentCreated, data)
	require.NoError(t, err)
	require.Equal(t, "cmt_1", svc.gotID)

	var payload struct {
		EventType  types.WebhookEventName `json:"event_type"`
		Commitment map[string]any         `json:"commitment"`
	}
	require.NoError(t, json.Unmarshal(out, &payload))
	require.Equal(t, types.WebhookEventCommitmentCreated, payload.EventType)
	require.Equal(t, "cmt_1", payload.Commitment["id"])
	require.Equal(t, "subs_1", payload.Commitment["subscription_id"])
	require.Equal(t, "1200", payload.Commitment["commitment_value"])
}

func TestCommitmentPayloadBuilder_BuildPayload_InvalidData(t *testing.T) {
	b := NewCommitmentPayloadBuilder(&Services{CommitmentService: &fakeCommitmentService{}})

	_, err := b.BuildPayload(context.Background(), types.WebhookEventCommitmentCreated, json.RawMessage(`{`))
	require.Error(t, err)

	data, err := json.Marshal(webhookDto.InternalCommitmentEvent{TenantID: "ten_1"})
	require.NoError(t, err)
	_, err = b.BuildPayload(context.Background(), types.WebhookEventCommitmentCreated, data)
	require.Error(t, err)
}

func TestPayloadBuilderFactory_RegistersCommitmentEvents(t *testing.T) {
	f := NewPayloadBuilderFactory(&Services{})
	for _, e := range []types.WebhookEventName{
		types.WebhookEventCommitmentCreated,
		types.WebhookEventCommitmentUpdated,
		types.WebhookEventCommitmentTerminated,
	} {
		b, err := f.GetBuilder(e)
		require.NoError(t, err, "event %s", e)
		require.IsType(t, &CommitmentPayloadBuilder{}, b)
	}
}
