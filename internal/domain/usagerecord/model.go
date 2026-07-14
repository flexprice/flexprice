package usagerecord

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Marketplace identifies a marketplace a UsageRecord may be synced to. This is a separate,
// narrower enum from entity_integration_mapping.provider_type ("aws_marketplace") — it is only
// ever used as a key in UsageRecord.Syncs.
type Marketplace string

const (
	MarketplaceAWS   Marketplace = "aws"
	MarketplaceAzure Marketplace = "azure"
	MarketplaceGCP   Marketplace = "gcp"
)

// MarketplaceSyncEntry is one marketplace's sync outcome for a usage record. Stored as a value
// in the record's Syncs map, keyed by Marketplace. Presence + a non-nil SyncedAt IS the
// "already synced" signal — there is no separate status field. An entry that's absent (or
// present with SyncedAt nil) means "not yet successfully synced," and Cron B retries it on
// every run. Failure detail is never persisted on the row — only logged — so this struct only
// ever represents a SUCCESS.
type MarketplaceSyncEntry struct {
	ConnectionID        string     `json:"connection_id"`
	SyncedAt            *time.Time `json:"synced_at,omitempty"`
	MarketplaceReportID string     `json:"marketplace_report_id,omitempty"` // AWS: MeteringRecordId
}

// UsageRecord is one usage snapshot for a subscription over a reporting window (table
// usage_records). Syncs holds one entry per marketplace the record has been successfully reported to
// A record is fully synced once every connected marketplace has an entry in the Syncs map.
type UsageRecord struct {
	ID                 string                               `db:"id" json:"id"`
	CustomerID         string                               `db:"customer_id" json:"customer_id"`
	CustomerExternalID string                               `db:"customer_external_id" json:"customer_external_id"`
	SubscriptionID     string                               `db:"subscription_id" json:"subscription_id"`
	PlanID             string                               `db:"plan_id" json:"plan_id"`
	Quantity           decimal.Decimal                      `db:"quantity" json:"quantity"`
	Amount             decimal.Decimal                      `db:"amount" json:"amount"`
	PeriodStart        time.Time                            `db:"period_start" json:"period_start"`
	PeriodEnd          time.Time                            `db:"period_end" json:"period_end"`
	Syncs              map[Marketplace]MarketplaceSyncEntry `db:"syncs" json:"syncs"`
	AllProvidersSynced bool                                 `db:"all_providers_synced" json:"all_providers_synced"`
	EnvironmentID      string                               `db:"environment_id" json:"environment_id"`
	types.BaseModel
}

// FromEnt converts an ent.UsageRecord to the domain UsageRecord.
func FromEnt(e *ent.UsageRecord) *UsageRecord {
	if e == nil {
		return nil
	}
	return &UsageRecord{
		ID:                 e.ID,
		CustomerID:         e.CustomerID,
		CustomerExternalID: e.CustomerExternalID,
		SubscriptionID:     e.SubscriptionID,
		PlanID:             e.PlanID,
		Quantity:           e.Quantity,
		Amount:             e.Amount,
		PeriodStart:        e.PeriodStart,
		PeriodEnd:          e.PeriodEnd,
		Syncs:              syncsFromMap(e.Syncs),
		AllProvidersSynced: e.AllProvidersSynced,
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

// syncsFromMap converts the generic JSON map stored by Ent into the typed Syncs map. Values are
// re-marshaled/unmarshaled through JSON since Ent hands back map[string]interface{}.
func syncsFromMap(raw map[string]interface{}) map[Marketplace]MarketplaceSyncEntry {
	if raw == nil {
		return nil
	}
	result := make(map[Marketplace]MarketplaceSyncEntry, len(raw))
	for k, v := range raw {
		entryMap, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		entry := MarketplaceSyncEntry{}
		if connID, ok := entryMap["connection_id"].(string); ok {
			entry.ConnectionID = connID
		}
		if reportID, ok := entryMap["marketplace_report_id"].(string); ok {
			entry.MarketplaceReportID = reportID
		}
		if syncedAtRaw, ok := entryMap["synced_at"].(string); ok && syncedAtRaw != "" {
			if t, err := time.Parse(time.RFC3339, syncedAtRaw); err == nil {
				entry.SyncedAt = &t
			}
		}
		result[Marketplace(k)] = entry
	}
	return result
}

// SyncsToMap converts the typed Syncs map into the generic JSON map Ent stores.
func SyncsToMap(syncs map[Marketplace]MarketplaceSyncEntry) map[string]interface{} {
	if syncs == nil {
		return map[string]interface{}{}
	}
	result := make(map[string]interface{}, len(syncs))
	for k, v := range syncs {
		entry := map[string]interface{}{
			"connection_id": v.ConnectionID,
		}
		if v.MarketplaceReportID != "" {
			entry["marketplace_report_id"] = v.MarketplaceReportID
		}
		if v.SyncedAt != nil {
			entry["synced_at"] = v.SyncedAt.Format(time.RFC3339)
		}
		result[string(k)] = entry
	}
	return result
}
