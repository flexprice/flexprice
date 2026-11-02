package payload

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// fakeBillingTermsService serves GetBillingTerms and GetInstalment; any other call panics.
type fakeBillingTermsService struct {
	service.CommitmentService

	terms      *dto.BillingTermsResponse
	instalment *billingterm.Instalment

	gotOwnerID      string
	gotInstalmentID string
}

func (f *fakeBillingTermsService) GetBillingTerms(_ context.Context, id string) (*dto.BillingTermsResponse, error) {
	f.gotOwnerID = id
	return f.terms, nil
}

func (f *fakeBillingTermsService) GetInstalment(_ context.Context, id string) (*billingterm.Instalment, error) {
	f.gotInstalmentID = id
	return f.instalment, nil
}

func TestBillingTermsPayloadBuilder(t *testing.T) {
	terms := []*billingterm.Term{
		{
			ID:        "bterm_1",
			OwnerType: types.BillingTermOwnerCommitment,
			OwnerID:   "cmt_1",
			Kind:      types.BillingTermKindOnce,
			ValueType: types.BillingTermValueFixed,
		},
		{
			ID:        "bterm_2",
			OwnerType: types.BillingTermOwnerCommitment,
			OwnerID:   "cmt_1",
			Kind:      types.BillingTermKindRecurring,
			ValueType: types.BillingTermValueRemainder,
		},
	}

	tests := []struct {
		name    string
		event   webhookDto.InternalBillingTermsEvent
		raw     json.RawMessage
		wantErr bool
	}{
		{
			name: "carries the stored terms",
			event: webhookDto.InternalBillingTermsEvent{
				OwnerType: types.BillingTermOwnerCommitment,
				OwnerID:   "cmt_1",
				TenantID:  "ten_1",
			},
		},
		{
			name:    "malformed data",
			raw:     json.RawMessage(`{`),
			wantErr: true,
		},
		{
			name:    "missing owner id",
			event:   webhookDto.InternalBillingTermsEvent{OwnerType: types.BillingTermOwnerCommitment, TenantID: "ten_1"},
			wantErr: true,
		},
		{
			name:    "missing tenant id",
			event:   webhookDto.InternalBillingTermsEvent{OwnerType: types.BillingTermOwnerCommitment, OwnerID: "cmt_1"},
			wantErr: true,
		},
		{
			name:    "missing owner type",
			event:   webhookDto.InternalBillingTermsEvent{OwnerID: "cmt_1", TenantID: "ten_1"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeBillingTermsService{terms: &dto.BillingTermsResponse{BillingTerms: terms}}
			b := NewBillingTermsPayloadBuilder(&Services{CommitmentService: svc})

			data := tt.raw
			if data == nil {
				var err error
				data, err = json.Marshal(tt.event)
				require.NoError(t, err)
			}

			out, err := b.BuildPayload(context.Background(), types.WebhookEventBillingTermsCreated, data)
			if tt.wantErr {
				require.Error(t, err)
				require.True(t, ierr.IsInvalidOperation(err), "got %v", err)
				require.Empty(t, svc.gotOwnerID)
				return
			}

			require.NoError(t, err)
			require.Equal(t, "cmt_1", svc.gotOwnerID)

			var payload webhookDto.BillingTermsWebhookPayload
			require.NoError(t, json.Unmarshal(out, &payload))
			require.Equal(t, types.WebhookEventBillingTermsCreated, payload.EventType)
			require.Equal(t, types.BillingTermOwnerCommitment, payload.OwnerType)
			require.Equal(t, "cmt_1", payload.OwnerID)
			require.Len(t, payload.BillingTerms, 2)
			require.Equal(t, "bterm_1", payload.BillingTerms[0].ID)
			require.Equal(t, "bterm_2", payload.BillingTerms[1].ID)
		})
	}
}

func TestBillingInstalmentPayloadBuilder(t *testing.T) {
	invoiceID := "inv_1"
	instalment := &billingterm.Instalment{
		ID:               "binst_1",
		OwnerType:        types.BillingTermOwnerCommitment,
		OwnerID:          "cmt_1",
		Amount:           decimal.NewFromInt(300),
		BillOn:           time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		InstalmentStatus: types.InstalmentStatusInvoiced,
		InvoiceID:        &invoiceID,
	}

	tests := []struct {
		name    string
		event   webhookDto.InternalBillingInstalmentEvent
		raw     json.RawMessage
		wantErr bool
	}{
		{
			name:  "carries the stored instalment",
			event: webhookDto.InternalBillingInstalmentEvent{InstalmentID: "binst_1", TenantID: "ten_1"},
		},
		{
			name:    "malformed data",
			raw:     json.RawMessage(`{`),
			wantErr: true,
		},
		{
			name:    "missing instalment id",
			event:   webhookDto.InternalBillingInstalmentEvent{TenantID: "ten_1"},
			wantErr: true,
		},
		{
			name:    "missing tenant id",
			event:   webhookDto.InternalBillingInstalmentEvent{InstalmentID: "binst_1"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeBillingTermsService{instalment: instalment}
			b := NewBillingInstalmentPayloadBuilder(&Services{CommitmentService: svc})

			data := tt.raw
			if data == nil {
				var err error
				data, err = json.Marshal(tt.event)
				require.NoError(t, err)
			}

			out, err := b.BuildPayload(context.Background(), types.WebhookEventBillingInstalmentInvoiced, data)
			if tt.wantErr {
				require.Error(t, err)
				require.True(t, ierr.IsInvalidOperation(err), "got %v", err)
				require.Empty(t, svc.gotInstalmentID)
				return
			}

			require.NoError(t, err)
			require.Equal(t, "binst_1", svc.gotInstalmentID)

			var payload map[string]any
			require.NoError(t, json.Unmarshal(out, &payload))
			require.Equal(t, string(types.WebhookEventBillingInstalmentInvoiced), payload["event_type"])

			got, ok := payload["instalment"].(map[string]any)
			require.True(t, ok, "payload carries the instalment: %s", out)
			require.Equal(t, "binst_1", got["id"])
			require.Equal(t, "cmt_1", got["owner_id"])
			require.Equal(t, "300", got["amount"])
			require.Equal(t, "inv_1", got["invoice_id"])
		})
	}
}

func TestPayloadBuilderFactory_RegistersBillingTermEvents(t *testing.T) {
	f := NewPayloadBuilderFactory(&Services{})

	for _, e := range []types.WebhookEventName{
		types.WebhookEventBillingTermsCreated,
		types.WebhookEventBillingTermsUpdated,
	} {
		b, err := f.GetBuilder(e)
		require.NoError(t, err, "event %s", e)
		require.IsType(t, &BillingTermsPayloadBuilder{}, b)
	}

	for _, e := range []types.WebhookEventName{
		types.WebhookEventBillingInstalmentCreated,
		types.WebhookEventBillingInstalmentUpdated,
		types.WebhookEventBillingInstalmentCancelled,
		types.WebhookEventBillingInstalmentInvoiced,
	} {
		b, err := f.GetBuilder(e)
		require.NoError(t, err, "event %s", e)
		require.IsType(t, &BillingInstalmentPayloadBuilder{}, b)
	}
}
