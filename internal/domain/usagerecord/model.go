package usagerecord

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// UsageRecord is one usage snapshot for a subscription over a reporting window (table
// usage_records), written by the marketplace snapshot cron and reported by the marketplace
// reporting cron. It is provider-agnostic: Syncs tracks one outcome per connection this record has
// been reported to, keyed by connection_id, so the same subscription's usage can reach every
// marketplace it's mapped to. Synced is true only once every connection currently relevant to this
// record has a Syncs entry. Failure detail is never persisted on the row — only logged.
type UsageRecord struct {
	ID                 string                                `db:"id" json:"id"`
	CustomerID         string                                `db:"customer_id" json:"customer_id"`
	CustomerExternalID string                                `db:"customer_external_id" json:"customer_external_id"`
	SubscriptionID     string                                `db:"subscription_id" json:"subscription_id"`
	PlanID             string                                `db:"plan_id" json:"plan_id"`
	Quantity           decimal.Decimal                       `db:"quantity" json:"quantity"`
	Amount             decimal.Decimal                       `db:"amount" json:"amount"`
	Currency           string                                `db:"currency" json:"currency"`
	PeriodStart        time.Time                             `db:"period_start" json:"period_start"`
	PeriodEnd          time.Time                             `db:"period_end" json:"period_end"`
	Synced             bool                                  `db:"synced" json:"synced"`
	Syncs              map[string]types.UsageRecordSyncEntry `db:"syncs" json:"syncs"`
	EnvironmentID      string                                `db:"environment_id" json:"environment_id"`
	types.BaseModel
}

// FromEnt converts an ent.UsageRecord to the domain UsageRecord.
func FromEnt(e *ent.UsageRecord) *UsageRecord {
	if e == nil {
		return nil
	}
	syncs := e.Syncs
	if syncs == nil {
		syncs = map[string]types.UsageRecordSyncEntry{}
	}
	return &UsageRecord{
		ID:                 e.ID,
		CustomerID:         e.CustomerID,
		CustomerExternalID: e.CustomerExternalID,
		SubscriptionID:     e.SubscriptionID,
		PlanID:             e.PlanID,
		Quantity:           e.Quantity,
		Amount:             e.Amount,
		Currency:           e.Currency,
		PeriodStart:        e.PeriodStart,
		PeriodEnd:          e.PeriodEnd,
		Synced:             e.Synced,
		Syncs:              syncs,
		EnvironmentID:      e.EnvironmentID,
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

// FromEntList converts a list of ent.UsageRecord to domain UsageRecord.
func FromEntList(list []*ent.UsageRecord) []*UsageRecord {
	result := make([]*UsageRecord, len(list))
	for i, e := range list {
		result[i] = FromEnt(e)
	}
	return result
}
