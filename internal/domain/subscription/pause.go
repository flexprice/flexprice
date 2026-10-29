package subscription

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// SubscriptionPause represents a pause configuration for a subscription
type SubscriptionPause struct {
	// ID is the unique identifier for the subscription pause
	ID string `json:"id"`

	// SubscriptionID is the identifier for the subscription
	SubscriptionID string `json:"subscription_id"`

	PauseStatus types.PauseStatus `json:"pause_status"`
	PauseMode   types.PauseMode   `json:"pause_mode"`
	ResumeMode  types.ResumeMode  `json:"resume_mode,omitempty"`

	// PauseStart is when the pause actually started
	PauseStart time.Time `json:"pause_start"`

	// PauseEnd is when the pause will end (null for indefinite)
	PauseEnd *time.Time `json:"pause_end,omitempty"`

	// ResumedAt is when the pause was actually ended (if manually resumed)
	ResumedAt *time.Time `json:"resumed_at,omitempty"`

	// OriginalPeriodStart is the start of the billing period when the pause was created
	OriginalPeriodStart time.Time `json:"original_period_start"`

	// OriginalPeriodEnd is the end of the billing period when the pause was created
	OriginalPeriodEnd time.Time `json:"original_period_end"`

	// Reason is the reason for pausing
	Reason string `json:"reason,omitempty"`

	Metadata types.Metadata `json:"metadata,omitempty"`

	// EnvironmentID is the environment identifier for the pause
	EnvironmentID string `json:"environment_id"`

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
