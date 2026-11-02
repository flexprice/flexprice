package payload

import (
	"context"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// BillingTermsPayloadBuilder builds billing_terms.* payloads from the owner's current terms.
type BillingTermsPayloadBuilder struct {
	services *Services
}

// NewBillingTermsPayloadBuilder returns the builder for billing_terms.* events.
func NewBillingTermsPayloadBuilder(services *Services) PayloadBuilder {
	return &BillingTermsPayloadBuilder{services: services}
}

func (b *BillingTermsPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var parsed webhookDto.InternalBillingTermsEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal billing terms event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	// Commitments are the only owner type with a service to read terms through.
	if parsed.OwnerType != types.BillingTermOwnerCommitment || parsed.OwnerID == "" || parsed.TenantID == "" {
		return nil, ierr.NewError("invalid data type for billing terms event").
			WithHint("Please provide a valid owner type, owner ID and tenant ID").
			WithReportableDetails(map[string]any{
				"owner_type": parsed.OwnerType,
				"owner_id":   parsed.OwnerID,
				"tenant_id":  parsed.TenantID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	current, err := b.services.CommitmentService.GetBillingTerms(ctx, parsed.OwnerID)
	if err != nil {
		return nil, err
	}

	return json.Marshal(webhookDto.NewBillingTermsWebhookPayload(parsed.OwnerType, parsed.OwnerID, current.BillingTerms, eventType))
}

// BillingInstalmentPayloadBuilder builds billing_instalment.* payloads from the stored instalment.
type BillingInstalmentPayloadBuilder struct {
	services *Services
}

// NewBillingInstalmentPayloadBuilder returns the builder for billing_instalment.* events.
func NewBillingInstalmentPayloadBuilder(services *Services) PayloadBuilder {
	return &BillingInstalmentPayloadBuilder{services: services}
}

func (b *BillingInstalmentPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var parsed webhookDto.InternalBillingInstalmentEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal billing instalment event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	if parsed.InstalmentID == "" || parsed.TenantID == "" {
		return nil, ierr.NewError("invalid data type for billing instalment event").
			WithHint("Please provide a valid instalment ID and tenant ID").
			WithReportableDetails(map[string]any{
				"instalment_id": parsed.InstalmentID,
				"tenant_id":     parsed.TenantID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	instalment, err := b.services.CommitmentService.GetInstalment(ctx, parsed.InstalmentID)
	if err != nil {
		return nil, err
	}

	return json.Marshal(webhookDto.NewBillingInstalmentWebhookPayload(instalment, eventType))
}
