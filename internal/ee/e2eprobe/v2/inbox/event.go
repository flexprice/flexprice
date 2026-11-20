// Package inbox receives the webhooks Flexprice delivers to the probe and lets
// scenarios wait for the one they caused.
package inbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Event is one delivered webhook with the ids scenarios match on.
type Event struct {
	ReceivedAt     time.Time      `json:"received_at"`
	EventType      string         `json:"event_type"`
	CustomerID     string         `json:"customer_id,omitempty"`
	WalletID       string         `json:"wallet_id,omitempty"`
	SubscriptionID string         `json:"subscription_id,omitempty"`
	InvoiceID      string         `json:"invoice_id,omitempty"`
	EntityID       string         `json:"entity_id,omitempty"`
	Raw            map[string]any `json:"-"`
}

// Parse decodes a Flexprice webhook envelope. Ids are read from the nested
// object (customer.id) or the flat key (customer_id), whichever is present.
func Parse(received time.Time, body []byte) (Event, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return Event{}, fmt.Errorf("inbox: invalid JSON: %w", err)
	}
	eventType, _ := raw["event_type"].(string)
	if eventType == "" {
		return Event{}, errors.New("inbox: envelope has no event_type")
	}
	ev := Event{
		ReceivedAt:     received,
		EventType:      eventType,
		CustomerID:     idOf(raw, "customer"),
		WalletID:       idOf(raw, "wallet"),
		SubscriptionID: idOf(raw, "subscription"),
		InvoiceID:      idOf(raw, "invoice"),
		Raw:            raw,
	}
	for _, id := range []string{ev.CustomerID, ev.WalletID, ev.SubscriptionID, ev.InvoiceID} {
		if id != "" {
			ev.EntityID = id
			break
		}
	}
	return ev, nil
}

func idOf(raw map[string]any, entity string) string {
	if nested, ok := raw[entity].(map[string]any); ok {
		if id, ok := nested["id"].(string); ok && id != "" {
			return id
		}
	}
	if id, ok := raw[entity+"_id"].(string); ok {
		return id
	}
	return ""
}

// Match selects events; empty fields match anything and After is exclusive.
type Match struct {
	EventType      string
	CustomerID     string
	WalletID       string
	SubscriptionID string
	After          time.Time
}

func (m Match) Matches(e Event) bool {
	if m.EventType != "" && e.EventType != m.EventType {
		return false
	}
	if m.CustomerID != "" && e.CustomerID != m.CustomerID {
		return false
	}
	if m.WalletID != "" && e.WalletID != m.WalletID {
		return false
	}
	if m.SubscriptionID != "" && e.SubscriptionID != m.SubscriptionID {
		return false
	}
	return m.After.IsZero() || e.ReceivedAt.After(m.After)
}
