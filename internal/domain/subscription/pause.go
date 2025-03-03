package subscription

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// SubscriptionPause represents a pause configuration for a subscription
type SubscriptionPause struct {
	// ID is the unique identifier for the subscription pause
	ID string `db:"id" json:"id"`

	// SubscriptionID is the identifier for the subscription
	SubscriptionID string `db:"subscription_id" json:"subscription_id"`

	// PauseStatus is the status of the pause
	PauseStatus types.PauseStatus `db:"pause_status" json:"pause_status"` // none, active, scheduled, completed, cancelled

	// PauseMode indicates how the pause was applied
	PauseMode types.PauseMode `db:"pause_mode" json:"pause_mode"` // immediate, scheduled, period_end

	// ResumeMode indicates how the resume will be applied
	ResumeMode types.ResumeMode `db:"resume_mode" json:"resume_mode,omitempty"` // immediate, scheduled, auto

	// PauseStart is when the pause actually started
	PauseStart time.Time `db:"pause_start" json:"pause_start"`

	// PauseEnd is when the pause will end (null for indefinite)
	PauseEnd *time.Time `db:"pause_end" json:"pause_end,omitempty"`

	// ResumedAt is when the pause was actually ended (if manually resumed)
	ResumedAt *time.Time `db:"resumed_at" json:"resumed_at,omitempty"`

	// OriginalPeriodStart is the start of the billing period when the pause was created
	OriginalPeriodStart time.Time `db:"original_period_start" json:"original_period_start"`

	// OriginalPeriodEnd is the end of the billing period when the pause was created
	OriginalPeriodEnd time.Time `db:"original_period_end" json:"original_period_end"`

	// Reason is the reason for pausing
	Reason string `db:"reason" json:"reason,omitempty"`

	// Metadata is a map of key-value pairs that can be attached to the pause
	Metadata types.Metadata `db:"metadata" json:"metadata,omitempty"`

	// EnvironmentID is the environment identifier for the pause
	EnvironmentID string `db:"environment_id" json:"environment_id"`

	types.BaseModel
}

// SubscriptionPauseFromEnt converts an ent.SubscriptionPause to a domain SubscriptionPause
func SubscriptionPauseFromEnt(p *ent.SubscriptionPause) *SubscriptionPause {
	if p == nil {
		return nil
	}

	return &SubscriptionPause{
		ID:                  p.ID,
		SubscriptionID:      p.SubscriptionID,
		PauseStatus:         types.PauseStatus(p.PauseStatus),
		PauseMode:           types.PauseMode(p.PauseMode),
		ResumeMode:          types.ResumeMode(p.ResumeMode),
		PauseStart:          p.PauseStart,
		PauseEnd:            p.PauseEnd,
		ResumedAt:           p.ResumedAt,
		OriginalPeriodStart: p.OriginalPeriodStart,
		OriginalPeriodEnd:   p.OriginalPeriodEnd,
		Reason:              p.Reason,
		Metadata:            p.Metadata,
		EnvironmentID:       p.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  p.TenantID,
			Status:    types.Status(p.Status),
			CreatedAt: p.CreatedAt,
			CreatedBy: p.CreatedBy,
			UpdatedAt: p.UpdatedAt,
			UpdatedBy: p.UpdatedBy,
		},
	}
}

// SubscriptionPauseListFromEnt converts a list of ent.SubscriptionPause to a list of domain SubscriptionPause
func SubscriptionPauseListFromEnt(pauses []*ent.SubscriptionPause) []*SubscriptionPause {
	if pauses == nil {
		return nil
	}

	result := make([]*SubscriptionPause, len(pauses))
	for i, p := range pauses {
		result[i] = SubscriptionPauseFromEnt(p)
	}
	return result
}
