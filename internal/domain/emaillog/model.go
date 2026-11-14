package emaillog

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// EmailLog records one email sent to a customer and its delivery status.
type EmailLog struct {
	ID                string                    `json:"id"`
	EnvironmentID     string                    `json:"environment_id"`
	EntityType        types.SystemEntityType    `json:"entity_type"`
	EntityID          string                    `json:"entity_id"`
	CustomerID        string                    `json:"customer_id"`
	TemplateKey       types.EmailTemplateKey    `json:"template_key"`
	Trigger           types.EmailTrigger        `json:"trigger"`
	FromAddress       string                    `json:"from_address"`
	ReplyTo           string                    `json:"reply_to"`
	ToAddresses       []string                  `json:"to_addresses" activity:"drop"`
	CCAddresses       []string                  `json:"cc_addresses" activity:"drop"`
	Subject           string                    `json:"subject"`
	Provider          types.EmailProvider       `json:"provider"`
	ProviderMessageID *string                   `json:"provider_message_id,omitempty"`
	DeliveryStatus    types.EmailDeliveryStatus `json:"delivery_status"`
	StatusReason      *string                   `json:"status_reason,omitempty" activity:"drop"`
	SentAt            *time.Time                `json:"sent_at,omitempty"`
	DeliveredAt       *time.Time                `json:"delivered_at,omitempty"`
	Metadata          types.Metadata            `json:"metadata,omitempty" activity:"drop"`
	types.BaseModel
}

// FromEnt converts an Ent EmailLog to a domain EmailLog.
func FromEnt(e *ent.EmailLog) *EmailLog {
	if e == nil {
		return nil
	}
	return &EmailLog{
		ID:                e.ID,
		EnvironmentID:     e.EnvironmentID,
		EntityType:        e.EntityType,
		EntityID:          e.EntityID,
		CustomerID:        e.CustomerID,
		TemplateKey:       e.TemplateKey,
		Trigger:           e.Trigger,
		FromAddress:       e.FromAddress,
		ReplyTo:           e.ReplyTo,
		ToAddresses:       e.ToAddresses,
		CCAddresses:       e.CcAddresses,
		Subject:           e.Subject,
		Provider:          e.Provider,
		ProviderMessageID: e.ProviderMessageID,
		DeliveryStatus:    e.DeliveryStatus,
		StatusReason:      e.StatusReason,
		SentAt:            e.SentAt,
		DeliveredAt:       e.DeliveredAt,
		Metadata:          e.Metadata,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

// FromEntList converts a list of Ent EmailLogs to domain EmailLogs.
func FromEntList(list []*ent.EmailLog) []*EmailLog {
	if list == nil {
		return nil
	}
	return lo.Map(list, func(item *ent.EmailLog, _ int) *EmailLog {
		return FromEnt(item)
	})
}
