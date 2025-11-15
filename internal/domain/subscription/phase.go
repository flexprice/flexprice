package subscription

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// SubscriptionPhase represents a phase in a subscription lifecycle
type SubscriptionPhase struct {
	// ID is the unique identifier for the subscription phase
	ID string `db:"id" json:"id"`

	// SubscriptionID is the identifier for the subscription
	SubscriptionID string `db:"subscription_id" json:"subscription_id"`

	// StartDate is when the phase starts
	StartDate time.Time `db:"start_date" json:"start_date"`

	// EndDate is when the phase ends (nil if phase is still active or indefinite)
	EndDate *time.Time `db:"end_date" json:"end_date,omitempty"`

	// Metadata contains additional key-value pairs
	Metadata types.Metadata `db:"metadata" json:"metadata,omitempty"`

	// EnvironmentID is the environment identifier for the phase
	EnvironmentID string `db:"environment_id" json:"environment_id"`

	types.BaseModel
}

// SubscriptionPhaseFromEnt converts an ent.SubscriptionPhase to domain SubscriptionPhase
func SubscriptionPhaseFromEnt(e *ent.SubscriptionPhase) *SubscriptionPhase {
	if e == nil {
		return nil
	}

	return &SubscriptionPhase{
		ID:             e.ID,
		SubscriptionID: e.SubscriptionID,
		StartDate:      e.StartDate,
		EndDate:        e.EndDate,
		Metadata:       e.Metadata,
		EnvironmentID:  e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		},
	}
}

// SubscriptionPhaseListFromEnt converts a list of ent.SubscriptionPhase to a list of domain SubscriptionPhase
func SubscriptionPhaseListFromEnt(phases []*ent.SubscriptionPhase) []*SubscriptionPhase {
	if phases == nil {
		return nil
	}

	result := make([]*SubscriptionPhase, len(phases))
	for i, p := range phases {
		result[i] = SubscriptionPhaseFromEnt(p)
	}
	return result
}

// IsActive returns true if the phase is currently active at the given time
// A phase is active if:
// - Status is Published
// - StartDate has passed
// - EndDate is nil or has not yet passed
func (sp *SubscriptionPhase) IsActive(t time.Time) bool {
	if sp.Status != types.StatusPublished {
		return false
	}
	if sp.StartDate.After(t) {
		return false
	}
	if sp.EndDate != nil && sp.EndDate.Before(t) {
		return false
	}
	return true
}
