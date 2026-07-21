package usagerecord

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// UsageRecord is one usage snapshot for a subscription over a reporting window (table
// usage_records), written by the marketplace snapshot cron and reported by the marketplace reporting
// cron. ConnectionID pins which marketplace connection — and therefore which provider — this row
// reports through; a subscription only ever belongs to one marketplace, so there is no fan-out to
// track here. Synced is the single retry signal: false means the reporting cron will pick this row
// up again on its next run. Failure detail is never persisted on the row — only logged.
type UsageRecord struct {
	ID                  string          `db:"id" json:"id"`
	CustomerID          string          `db:"customer_id" json:"customer_id"`
	CustomerExternalID  string          `db:"customer_external_id" json:"customer_external_id"`
	SubscriptionID      string          `db:"subscription_id" json:"subscription_id"`
	PlanID              string          `db:"plan_id" json:"plan_id"`
	Quantity            decimal.Decimal `db:"quantity" json:"quantity"`
	Amount              decimal.Decimal `db:"amount" json:"amount"`
	Currency            string          `db:"currency" json:"currency"`
	PeriodStart         time.Time       `db:"period_start" json:"period_start"`
	PeriodEnd           time.Time       `db:"period_end" json:"period_end"`
	ConnectionID        string          `db:"connection_id" json:"connection_id"`
	Synced              bool            `db:"synced" json:"synced"`
	SyncedAt            *time.Time      `db:"synced_at" json:"synced_at,omitempty"`
	MarketplaceReportID string          `db:"marketplace_report_id" json:"marketplace_report_id,omitempty"`
	EnvironmentID       string          `db:"environment_id" json:"environment_id"`
	types.BaseModel
}

// FromEnt converts an ent.UsageRecord to the domain UsageRecord.
func FromEnt(e *ent.UsageRecord) *UsageRecord {
	if e == nil {
		return nil
	}
	return &UsageRecord{
		ID:                  e.ID,
		CustomerID:          e.CustomerID,
		CustomerExternalID:  e.CustomerExternalID,
		SubscriptionID:      e.SubscriptionID,
		PlanID:              e.PlanID,
		Quantity:            e.Quantity,
		Amount:              e.Amount,
		Currency:            e.Currency,
		PeriodStart:         e.PeriodStart,
		PeriodEnd:           e.PeriodEnd,
		ConnectionID:        e.ConnectionID,
		Synced:              e.Synced,
		SyncedAt:            e.SyncedAt,
		MarketplaceReportID: e.MarketplaceReportID,
		EnvironmentID:       e.EnvironmentID,
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
