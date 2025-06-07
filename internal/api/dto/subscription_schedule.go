package dto

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// SubscriptionSchedulePhaseInput represents the input for creating a subscription schedule phase
type SubscriptionSchedulePhaseInput struct {
	BillingCycle     types.BillingCycle            `json:"billing_cycle"`
	StartDate        time.Time                     `json:"start_date" validate:"required"`
	EndDate          *time.Time                    `json:"end_date,omitempty"`
	LineItems        []SubscriptionLineItemRequest `json:"line_items"`
	CreditGrants     []CreateCreditGrantRequest    `json:"credit_grants"`
	CommitmentAmount decimal.Decimal               `json:"commitment_amount"`
	OverageFactor    decimal.Decimal               `json:"overage_factor"`
	Metadata         map[string]string             `json:"metadata,omitempty"`
}

// AddSchedulePhaseRequest represents the input for adding a new phase to an existing subscription schedule
type AddSchedulePhaseRequest struct {
	Phase SubscriptionSchedulePhaseInput `json:"phase" validate:"required"`
}

// SubscriptionScheduleResponse represents the response for a subscription schedule
type SubscriptionScheduleResponse struct {
	ID                string                               `json:"id"`
	SubscriptionID    string                               `json:"subscription_id"`
	ScheduleStatus    types.SubscriptionScheduleStatus     `json:"status"`
	CurrentPhaseIndex int                                  `json:"current_phase_index"`
	EndBehavior       types.ScheduleEndBehavior            `json:"end_behavior"`
	StartDate         time.Time                            `json:"start_date"`
	Phases            []*SubscriptionSchedulePhaseResponse `json:"phases,omitempty"`
	CreatedAt         time.Time                            `json:"created_at"`
	UpdatedAt         time.Time                            `json:"updated_at"`
}

// SubscriptionSchedulePhaseResponse represents the response for a subscription schedule phase
type SubscriptionSchedulePhaseResponse struct {
	ID               string                         `json:"id"`
	ScheduleID       string                         `json:"schedule_id"`
	PhaseIndex       int                            `json:"phase_index"`
	StartDate        time.Time                      `json:"start_date"`
	EndDate          *time.Time                     `json:"end_date,omitempty"`
	CommitmentAmount *decimal.Decimal               `json:"commitment_amount"`
	OverageFactor    *decimal.Decimal               `json:"overage_factor"`
	CreditGrants     []CreditGrantResponse          `json:"credit_grants,omitempty"`
	LineItems        []SubscriptionLineItemResponse `json:"line_items,omitempty"`
	CreatedAt        time.Time                      `json:"created_at"`
	UpdatedAt        time.Time                      `json:"updated_at"`
}

// CreateSubscriptionScheduleRequest represents the request to create a subscription schedule
type CreateSubscriptionScheduleRequest struct {
	SubscriptionID string                           `json:"subscription_id" validate:"required"`
	EndBehavior    types.ScheduleEndBehavior        `json:"end_behavior"`
	Phases         []SubscriptionSchedulePhaseInput `json:"phases" validate:"required,min=1,dive"`
}

// UpdateSubscriptionScheduleRequest represents the request to update a subscription schedule
type UpdateSubscriptionScheduleRequest struct {
	Status      types.SubscriptionScheduleStatus `json:"status,omitempty"`
	EndBehavior types.ScheduleEndBehavior        `json:"end_behavior,omitempty"`
}

// Validate validates the subscription schedule phase input
func (p *SubscriptionSchedulePhaseInput) Validate() error {
	if p.StartDate.IsZero() {
		return ierr.NewError("start_date is required").
			WithHint("Start date is required for a schedule phase").
			Mark(ierr.ErrValidation)
	}

	if p.CommitmentAmount.LessThan(decimal.Zero) {
		return ierr.NewError("commitment_amount must be non-negative").
			WithHint("Commitment amount must be greater than or equal to 0").
			Mark(ierr.ErrValidation)
	}

	if p.OverageFactor.LessThan(decimal.NewFromInt(1)) {
		return ierr.NewError("overage_factor must be at least 1.0").
			WithHint("Overage factor must be greater than or equal to 1.0").
			Mark(ierr.ErrValidation)
	}

	// Validate credit grants
	for i, grant := range p.CreditGrants {
		if err := grant.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("Credit grant validation failed").
				WithReportableDetails(map[string]interface{}{
					"index": i,
					"error": err.Error(),
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// Validate validates the create subscription schedule request
func (r *CreateSubscriptionScheduleRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	if len(r.Phases) == 0 {
		return ierr.NewError("at least one phase is required").
			WithHint("A subscription schedule must have at least one phase").
			Mark(ierr.ErrValidation)
	}

	// Validate each phase
	for i, phase := range r.Phases {
		if err := phase.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("Phase validation failed").
				WithReportableDetails(map[string]interface{}{
					"index": i,
					"error": err.Error(),
				}).
				Mark(ierr.ErrValidation)
		}

		// Validate phase continuity
		if i > 0 {
			prevPhase := r.Phases[i-1]
			if prevPhase.EndDate == nil {
				return ierr.NewError(fmt.Sprintf("phase at index %d must have an end date", i-1)).
					WithHint("All phases except the last one must have an end date").
					Mark(ierr.ErrValidation)
			}

			if !prevPhase.EndDate.Equal(phase.StartDate) {
				return ierr.NewError(fmt.Sprintf("phase at index %d does not start immediately after previous phase", i)).
					WithHint("Phases must be contiguous").
					WithReportableDetails(map[string]interface{}{
						"previous_phase_end":  prevPhase.EndDate,
						"current_phase_start": phase.StartDate,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	}

	return nil
}

// Validate validates the add schedule phase request
func (r *AddSchedulePhaseRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	return r.Phase.Validate()
}

// FromDomain converts a domain subscription schedule to a DTO response
func SubscriptionScheduleResponseFromDomain(schedule *subscription.SubscriptionSchedule) *SubscriptionScheduleResponse {
	if schedule == nil {
		return nil
	}

	phases := make([]*SubscriptionSchedulePhaseResponse, 0, len(schedule.Phases))
	for _, phase := range schedule.Phases {
		phases = append(phases, SubscriptionSchedulePhaseResponseFromDomain(phase))
	}

	return &SubscriptionScheduleResponse{
		ID:                schedule.ID,
		SubscriptionID:    schedule.SubscriptionID,
		ScheduleStatus:    schedule.ScheduleStatus,
		CurrentPhaseIndex: schedule.CurrentPhaseIndex,
		EndBehavior:       schedule.EndBehavior,
		StartDate:         schedule.StartDate,
		Phases:            phases,
		CreatedAt:         schedule.BaseModel.CreatedAt,
		UpdatedAt:         schedule.BaseModel.UpdatedAt,
	}
}

// FromDomain converts a domain subscription schedule phase to a DTO response
func SubscriptionSchedulePhaseResponseFromDomain(phase *subscription.SchedulePhase) *SubscriptionSchedulePhaseResponse {
	if phase == nil {
		return nil
	}

	// Convert credit grants and line items from JSON
	var creditGrants []CreditGrantResponse
	var lineItems []SubscriptionLineItemResponse

	// TODO: Add deserialization of credit grants and line items from JSON

	return &SubscriptionSchedulePhaseResponse{
		ID:               phase.ID,
		ScheduleID:       phase.ScheduleID,
		PhaseIndex:       phase.PhaseIndex,
		StartDate:        phase.StartDate,
		EndDate:          phase.EndDate,
		CommitmentAmount: phase.CommitmentAmount,
		OverageFactor:    phase.OverageFactor,
		CreditGrants:     creditGrants,
		LineItems:        lineItems,
		CreatedAt:        phase.BaseModel.CreatedAt,
		UpdatedAt:        phase.BaseModel.UpdatedAt,
	}
}
