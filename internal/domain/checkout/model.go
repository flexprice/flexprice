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

	SourceSubscriptionID *string // upgrades only: old sub to cancel on completion

	CheckoutType types.CheckoutType
	Objective    types.CheckoutObjective
	Status       types.CheckoutStatus

	Amount   decimal.Decimal
	Currency string

	Provider          string
	ProviderSessionID *string
	CheckoutURL       *string
	SuccessURL        *string
	CancelURL         *string

	Configuration *CheckoutConfiguration // reserved; nil in v1 create/upgrade flows

	ExpiresAt    time.Time
	CompletedAt  *time.Time
	CancelledAt  *time.Time
	ErrorMessage *string

	EnvironmentID string
	types.BaseModel
}

// CheckoutConfiguration is reserved for a future "create nothing until paid" mode.
// Kept generic to avoid a domain -> api/dto dependency; the typed deferred request
// is marshaled into Payload by the service layer if/when that mode is built.
type CheckoutConfiguration struct {
	SaveCard bool            `json:"save_card,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
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

// SetConfiguration stores cfg on the checkout.
func (c *Checkout) SetConfiguration(cfg *CheckoutConfiguration) error {
	c.Configuration = cfg
	return nil
}

// GetConfiguration returns the stored configuration (nil if none).
func (c *Checkout) GetConfiguration() (*CheckoutConfiguration, error) {
	return c.Configuration, nil
}

// GetConfigurationMap serializes the reserved configuration to a JSONB map
// (nil when no configuration is set). Used by the repository.
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
		ID:                   e.ID,
		CustomerID:           e.CustomerID,
		EntityType:           e.EntityType,
		EntityID:             e.EntityID,
		SourceSubscriptionID: e.SourceSubscriptionID,
		CheckoutType:         e.CheckoutType,
		Objective:            e.Objective,
		Status:               e.CheckoutStatus,
		Amount:               e.Amount,
		Currency:             e.Currency,
		Provider:             e.Provider,
		ProviderSessionID:    e.ProviderSessionID,
		CheckoutURL:          e.CheckoutURL,
		SuccessURL:           e.SuccessURL,
		CancelURL:            e.CancelURL,
		ExpiresAt:            e.ExpiresAt,
		CompletedAt:          e.CompletedAt,
		CancelledAt:          e.CancelledAt,
		ErrorMessage:         e.ErrorMessage,
		EnvironmentID:        e.EnvironmentID,
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
