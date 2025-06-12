package types

import (
	"github.com/shopspring/decimal"
)

// CostsheetStatus represents the possible states of a costsheet record
type CostsheetStatus string

const (
	// CostsheetStatusPublished indicates the costsheet is active and can be used for cost calculations
	CostsheetStatusPublished CostsheetStatus = "published"
	// CostsheetStatusDraft indicates the costsheet is still being configured
	CostsheetStatusDraft CostsheetStatus = "draft"
	// CostsheetStatusArchived indicates the costsheet is no longer active but preserved for historical reference
	CostsheetStatusArchived CostsheetStatus = "archived"
)

// IsValid checks if the status is one of the defined constants
func (s CostsheetStatus) IsValid() bool {
	switch s {
	case CostsheetStatusPublished, CostsheetStatusDraft, CostsheetStatusArchived:
		return true
	}
	return false
}

// String returns the string representation of the status
func (s CostsheetStatus) String() string {
	return string(s)
}

// CostBreakdown represents the calculated cost for a specific meter
// This is used across different parts of the business logic for cost calculations
type CostBreakdown struct {
	MeterID string          `json:"meter_id"`
	Usage   decimal.Decimal `json:"usage"`
	Cost    decimal.Decimal `json:"cost"`
}
