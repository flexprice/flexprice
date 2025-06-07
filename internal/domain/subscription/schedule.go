package subscription

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// SubscriptionSchedule represents a timeline of phases for a subscription
type SubscriptionSchedule struct {
	ID                string                           `json:"id"`
	SubscriptionID    string                           `json:"subscription_id"`
	ScheduleStatus    types.SubscriptionScheduleStatus `json:"schedule_status"`
	CurrentPhaseIndex int                              `json:"current_phase_index"`
	EndBehavior       types.ScheduleEndBehavior        `json:"end_behavior"`
	StartDate         time.Time                        `json:"start_date"`
	Metadata          types.Metadata                   `json:"metadata"`
	Phases            []*SchedulePhase                 `json:"phases,omitempty"`
	EnvironmentID     string                           `json:"environment_id"`
	types.BaseModel
}

// SchedulePhase represents a time-boxed configuration of a subscription
type SchedulePhase struct {
	ID               string                           `json:"id"`
	ScheduleID       string                           `json:"schedule_id"`
	PhaseIndex       int                              `json:"phase_index"`
	StartDate        time.Time                        `json:"start_date"`
	EndDate          *time.Time                       `json:"end_date,omitempty"`
	CommitmentAmount *decimal.Decimal                 `json:"commitment_amount"`
	OverageFactor    *decimal.Decimal                 `json:"overage_factor"`
	LineItems        []types.SchedulePhaseLineItem    `json:"line_items,omitempty"`
	CreditGrants     []types.SchedulePhaseCreditGrant `json:"credit_grants,omitempty"`
	Metadata         types.Metadata                   `json:"metadata,omitempty"`
	EnvironmentID    string                           `json:"environment_id"`
	types.BaseModel
}

// GetCurrentPhase returns the current phase based on the current phase index
func (s *SubscriptionSchedule) GetCurrentPhase() *SchedulePhase {
	if len(s.Phases) == 0 || s.CurrentPhaseIndex >= len(s.Phases) {
		return nil
	}
	return s.Phases[s.CurrentPhaseIndex]
}

// GetNextPhase returns the next phase or nil if this is the last phase
func (s *SubscriptionSchedule) GetNextPhase() *SchedulePhase {
	if len(s.Phases) == 0 || s.CurrentPhaseIndex+1 >= len(s.Phases) {
		return nil
	}
	return s.Phases[s.CurrentPhaseIndex+1]
}

// IsActive returns true if the schedule is active
func (s *SubscriptionSchedule) IsActive() bool {
	return s.ScheduleStatus == types.ScheduleStatusActive
}

// HasFuturePhases returns true if there are phases after the current one
func (s *SubscriptionSchedule) HasFuturePhases() bool {
	return s.GetNextPhase() != nil
}

func GetSubscriptionScheduleFromEnt(ent *ent.SubscriptionSchedule) *SubscriptionSchedule {
	var phases []*SchedulePhase
	if ent.Edges.Phases != nil {
		phases = GetSchedulePhasesFromEnt(ent.Edges.Phases)
	}

	return &SubscriptionSchedule{
		ID:                ent.ID,
		SubscriptionID:    ent.SubscriptionID,
		ScheduleStatus:    types.SubscriptionScheduleStatus(ent.ScheduleStatus),
		CurrentPhaseIndex: ent.CurrentPhaseIndex,
		EndBehavior:       types.ScheduleEndBehavior(ent.EndBehavior),
		StartDate:         ent.StartDate,
		Metadata:          ent.Metadata,
		Phases:            phases,
		EnvironmentID:     ent.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  ent.TenantID,
			CreatedAt: ent.CreatedAt,
			UpdatedAt: ent.UpdatedAt,
			CreatedBy: ent.CreatedBy,
			UpdatedBy: ent.UpdatedBy,
		},
	}
}

func GetSchedulePhasesFromEnt(phases []*ent.SubscriptionSchedulePhase) []*SchedulePhase {
	if phases == nil {
		return nil
	}
	return lo.Map(phases, func(phase *ent.SubscriptionSchedulePhase, _ int) *SchedulePhase {
		return GetSchedulePhaseFromEnt(phase)
	})
}

func GetSchedulePhaseFromEnt(ent *ent.SubscriptionSchedulePhase) *SchedulePhase {
	var lineItems []types.SchedulePhaseLineItem
	if ent.LineItems != nil {
		lineItems = lo.Map(ent.LineItems, func(item types.SchedulePhaseLineItem, _ int) types.SchedulePhaseLineItem {
			return item
		})
	}

	var creditGrants []types.SchedulePhaseCreditGrant
	if ent.CreditGrants != nil {
		creditGrants = lo.Map(ent.CreditGrants, func(item types.SchedulePhaseCreditGrant, _ int) types.SchedulePhaseCreditGrant {
			return item
		})
	}
	return &SchedulePhase{
		ID:               ent.ID,
		ScheduleID:       ent.ScheduleID,
		PhaseIndex:       ent.PhaseIndex,
		StartDate:        ent.StartDate,
		EndDate:          ent.EndDate,
		CommitmentAmount: ent.CommitmentAmount,
		OverageFactor:    ent.OverageFactor,
		LineItems:        lineItems,
		CreditGrants:     creditGrants,
		Metadata:         ent.Metadata,
		EnvironmentID:    ent.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  ent.TenantID,
			CreatedAt: ent.CreatedAt,
			UpdatedAt: ent.UpdatedAt,
			CreatedBy: ent.CreatedBy,
			UpdatedBy: ent.UpdatedBy,
		},
	}
}
