package outbound

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// SubscriptionWebhookPayload is the minimal webhook representation of a subscription.
// Built from either dto.SubscriptionResponse or dto.SubscriptionResponseV2 — the two
// constructors below — so every subscription payload has this one shape regardless of
// which internal API response variant produced it.
type SubscriptionWebhookPayload struct {
	ID                   string                   `json:"id"`
	CustomerID           string                   `json:"customer_id"`
	PlanID               string                   `json:"plan_id"`
	LookupKey            string                   `json:"lookup_key,omitempty"`
	SubscriptionStatus   types.SubscriptionStatus `json:"subscription_status"`
	Currency             string                   `json:"currency"`
	StartDate            time.Time                `json:"start_date"`
	EndDate              *time.Time               `json:"end_date,omitempty"`
	CurrentPeriodStart   time.Time                `json:"current_period_start"`
	CurrentPeriodEnd     time.Time                `json:"current_period_end"`
	CancelledAt          *time.Time               `json:"cancelled_at,omitempty"`
	CancelAt             *time.Time               `json:"cancel_at,omitempty"`
	CancelAtPeriodEnd    bool                     `json:"cancel_at_period_end"`
	TrialStart           *time.Time               `json:"trial_start,omitempty"`
	TrialEnd             *time.Time               `json:"trial_end,omitempty"`
	BillingCadence       types.BillingCadence     `json:"billing_cadence"`
	BillingPeriod        types.BillingPeriod      `json:"billing_period"`
	BillingPeriodCount   int                      `json:"billing_period_count"`
	PauseStatus          types.PauseStatus        `json:"pause_status"`
	SubscriptionType     types.SubscriptionType   `json:"subscription_type"`
	ParentSubscriptionID *string                  `json:"parent_subscription_id,omitempty"`
	Plan                 *PlanWebhookPayload      `json:"plan,omitempty"`
}

// NewSubscriptionWebhookPayload builds the minimal payload from the V1 subscription response
// (used by every subscription.* event today).
func NewSubscriptionWebhookPayload(resp *dto.SubscriptionResponse) *SubscriptionWebhookPayload {
	if resp == nil || resp.Subscription == nil {
		return nil
	}
	return &SubscriptionWebhookPayload{
		ID:                   resp.ID,
		CustomerID:           resp.CustomerID,
		PlanID:               resp.PlanID,
		LookupKey:            resp.LookupKey,
		SubscriptionStatus:   resp.SubscriptionStatus,
		Currency:             resp.Currency,
		StartDate:            resp.StartDate,
		EndDate:              resp.EndDate,
		CurrentPeriodStart:   resp.CurrentPeriodStart,
		CurrentPeriodEnd:     resp.CurrentPeriodEnd,
		CancelledAt:          resp.CancelledAt,
		CancelAt:             resp.CancelAt,
		CancelAtPeriodEnd:    resp.CancelAtPeriodEnd,
		TrialStart:           resp.TrialStart,
		TrialEnd:             resp.TrialEnd,
		BillingCadence:       resp.BillingCadence,
		BillingPeriod:        resp.BillingPeriod,
		BillingPeriodCount:   resp.BillingPeriodCount,
		PauseStatus:          resp.PauseStatus,
		SubscriptionType:     resp.SubscriptionType,
		ParentSubscriptionID: resp.ParentSubscriptionID,
		Plan:                 NewPlanWebhookPayload(resp.Plan),
	}
}

// NewSubscriptionWebhookPayloadFromV2 builds the minimal payload from the V2 subscription
// response (used by the entitlement-grant-exhaustion alert path, which calls GetSubscriptionV2).
func NewSubscriptionWebhookPayloadFromV2(resp *dto.SubscriptionResponseV2) *SubscriptionWebhookPayload {
	if resp == nil || resp.Subscription == nil {
		return nil
	}
	return &SubscriptionWebhookPayload{
		ID:                   resp.ID,
		CustomerID:           resp.CustomerID,
		PlanID:               resp.PlanID,
		LookupKey:            resp.LookupKey,
		SubscriptionStatus:   resp.SubscriptionStatus,
		Currency:             resp.Currency,
		StartDate:            resp.StartDate,
		EndDate:              resp.EndDate,
		CurrentPeriodStart:   resp.CurrentPeriodStart,
		CurrentPeriodEnd:     resp.CurrentPeriodEnd,
		CancelledAt:          resp.CancelledAt,
		CancelAt:             resp.CancelAt,
		CancelAtPeriodEnd:    resp.CancelAtPeriodEnd,
		TrialStart:           resp.TrialStart,
		TrialEnd:             resp.TrialEnd,
		BillingCadence:       resp.BillingCadence,
		BillingPeriod:        resp.BillingPeriod,
		BillingPeriodCount:   resp.BillingPeriodCount,
		PauseStatus:          resp.PauseStatus,
		SubscriptionType:     resp.SubscriptionType,
		ParentSubscriptionID: resp.ParentSubscriptionID,
		Plan:                 NewPlanWebhookPayload(resp.Plan),
	}
}
