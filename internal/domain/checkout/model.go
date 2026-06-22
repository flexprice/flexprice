package checkout

import (
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Checkout is a deferred, user-gated activation of a subject (a subscription).
type Checkout struct {
	ID         string
	CustomerID string

	EntityType types.CheckoutEntityType
	EntityID   string

	CheckoutAction types.CheckoutAction
	Mode           types.CheckoutMode
	Status         types.CheckoutStatus

	Amount   *decimal.Decimal
	Currency string

	Provider          types.CheckoutProvider
	ProviderSessionID *string
	CheckoutURL       *string
	SuccessURL        *string
	CancelURL         *string

	Configuration *CheckoutConfiguration // deferred-operation payload; nil in v1

	ExpiresAt      time.Time
	CompletedAt    *time.Time
	CancelledAt    *time.Time
	FailureMessage *string

	EnvironmentID string
	types.BaseModel
}

// CheckoutConfiguration is the deferred-operation payload stored on the checkout.
// Each action type uses a dedicated key so callers can unmarshal into a typed struct.
type CheckoutConfiguration struct {
	// SubscriptionCreateParams carries the deferred subscription creation spec
	// for checkout_action = subscription_creation checkouts.
	SubscriptionCreateParams json.RawMessage `json:"subscription_create_params,omitempty"`
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
	if len(e.Configuration) > 0 {
		b, err := json.Marshal(e.Configuration)
		if err == nil {
			var cfg CheckoutConfiguration
			if json.Unmarshal(b, &cfg) == nil {
				c.Configuration = &cfg
			}
		}
	}
	return c
}

// FromEntList converts a list of Ent entities to domain models.
func FromEntList(entities []*ent.Checkout) []*Checkout {
	out := make([]*Checkout, 0, len(entities))
	for _, e := range entities {
		out = append(out, FromEnt(e))
	}
	return out
}
