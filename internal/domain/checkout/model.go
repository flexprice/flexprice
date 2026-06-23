package checkout

import (
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Checkout is a deferred, user-gated activation of a subject (a subscription).
type Checkout struct {
	ID         string `json:"id"`
	CustomerID string `json:"customer_id"`

	EntityType types.CheckoutEntityType `json:"entity_type"`
	EntityID   string                   `json:"entity_id"`

	CheckoutAction types.CheckoutAction `json:"checkout_action"`
	Mode           types.CheckoutMode   `json:"mode"`
	Status         types.CheckoutStatus `json:"checkout_status"`

	Amount   *decimal.Decimal `json:"amount,omitempty" swaggertype:"string"`
	Currency string           `json:"currency"`

	Provider          types.CheckoutProvider `json:"provider"`
	ProviderSessionID *string                `json:"provider_session_id,omitempty"`
	CheckoutURL       *string                `json:"checkout_url,omitempty"`
	SuccessURL        *string                `json:"success_url,omitempty"`
	CancelURL         *string                `json:"cancel_url,omitempty"`

	Configuration *CheckoutConfiguration `json:"configuration,omitempty"` // deferred-operation payload; nil in v1

	ExpiresAt      time.Time  `json:"expires_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CancelledAt    *time.Time `json:"cancelled_at,omitempty"`
	FailureMessage *string    `json:"failure_message,omitempty"`

	EnvironmentID string `json:"environment_id"`
	types.BaseModel
}

// CheckoutConfiguration is the deferred-operation payload stored on the checkout.
// Each action type uses a dedicated key so callers can unmarshal into a typed struct.
type CheckoutConfiguration struct {
	// SubscriptionCreationParams carries the deferred subscription creation spec
	// for checkout_action = subscription_creation checkouts.
	SubscriptionCreationParams json.RawMessage `json:"subscription_creation_params,omitempty"`
}

func (c *Checkout) IsPending() bool {
	return c.Status == types.CheckoutStatusPending
}

func (c *Checkout) IsTerminal() bool {
	switch c.Status {
	case types.CheckoutStatusCompleted, types.CheckoutStatusExpired,
		types.CheckoutStatusCancelled, types.CheckoutStatusFailed:
		return true
	default:
		return false
	}
}

// GetConfigurationMap serializes the configuration to a JSONB map (nil when none set).
func (c *Checkout) GetConfigurationMap() (map[string]interface{}, error) {
	if c.Configuration == nil {
		return nil, nil
	}
	b, err := json.Marshal(c.Configuration)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// FromEnt converts an Ent entity to the domain model.
func FromEnt(e *ent.Checkout) *Checkout {
	if e == nil {
		return nil
	}
	c := &Checkout{
		ID:                e.ID,
		CustomerID:        e.CustomerID,
		EntityType:        e.EntityType,
		EntityID:          e.EntityID,
		CheckoutAction:    e.CheckoutAction,
		Mode:              e.Mode,
		Status:            e.CheckoutStatus,
		Amount:            e.Amount,
		Currency:          e.Currency,
		Provider:          e.Provider,
		ProviderSessionID: e.ProviderSessionID,
		CheckoutURL:       e.CheckoutURL,
		SuccessURL:        e.SuccessURL,
		CancelURL:         e.CancelURL,
		ExpiresAt:         e.ExpiresAt,
		CompletedAt:       e.CompletedAt,
		CancelledAt:       e.CancelledAt,
		FailureMessage:    e.FailureMessage,
		EnvironmentID:     e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
	c.Configuration = configurationFromMap(e.Configuration)
	return c
}

func configurationFromMap(m map[string]interface{}) *CheckoutConfiguration {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	var cfg CheckoutConfiguration
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil
	}
	return lo.ToPtr(cfg)
}

// FromEntList converts a list of Ent entities to domain models.
func FromEntList(entities []*ent.Checkout) []*Checkout {
	return lo.Map(entities, func(e *ent.Checkout, _ int) *Checkout {
		return FromEnt(e)
	})
}
