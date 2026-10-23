package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type CommitmentScopeInput struct {
	EntityType types.CommitmentScopeType `json:"entity_type" validate:"required"`
	EntityID   string                    `json:"entity_id" validate:"required"`
}

type CreateCommitmentRequest struct {
	SubscriptionID     string                            `json:"subscription_id" validate:"required"`
	IdempotencyKey     *string                           `json:"idempotency_key,omitempty" validate:"omitempty,max=128"`
	Name               string                            `json:"name" validate:"required,max=255"`
	InvoiceLabel       string                            `json:"invoice_label,omitempty" validate:"omitempty,max=255"`
	InvoiceDescription *string                           `json:"invoice_description,omitempty" validate:"omitempty,max=500"`
	Scope              CommitmentScopeInput              `json:"scope" validate:"required"`
	Type               types.CommitmentType              `json:"type" validate:"required"`
	Value              decimal.Decimal                   `json:"value" swaggertype:"string"`
	CommittedRate      *decimal.Decimal                  `json:"committed_rate,omitempty" swaggertype:"string"`
	OverageRate        *decimal.Decimal                  `json:"overage_rate,omitempty" swaggertype:"string"`
	Measurement        types.CommitmentMeasurement       `json:"measurement" validate:"required"`
	ActiveWindows      types.ActiveWindows               `json:"active_windows,omitempty"`
	Timezone           string                            `json:"timezone,omitempty"`
	Priority           int                               `json:"priority"`
	Binding            *bool                             `json:"binding,omitempty"` // default false
	CollectionMethod   *types.CommitmentCollectionMethod `json:"collection_method,omitempty"`
	StartDate          time.Time                         `json:"start_date" validate:"required"`
	EndDate            time.Time                         `json:"end_date" validate:"required"`
	Metadata           types.Metadata                    `json:"metadata,omitempty"`
}

// ApplyDefaults fills label, timezone and the binding-dependent settings.
func (r *CreateCommitmentRequest) ApplyDefaults(subTimezone string) {
	if r.InvoiceLabel == "" {
		r.InvoiceLabel = r.Name
	}

	if r.Timezone == "" {
		r.Timezone = subTimezone
	}

	if r.Binding == nil {
		r.Binding = lo.ToPtr(false)
	}

	if *r.Binding && r.CollectionMethod == nil {
		r.CollectionMethod = lo.ToPtr(types.CommitmentCollectionPerPeriod)
	}
}

func invalid(msg string) error {
	return ierr.NewError(msg).
		WithHint(msg).
		Mark(ierr.ErrValidation)
}

// Validate checks the rules that need only the request. Call ApplyDefaults first.
func (r *CreateCommitmentRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	validators := []interface{ Validate() error }{
		r.Scope.EntityType,
		r.Measurement,
		r.ActiveWindows,
	}
	for _, v := range validators {
		if err := v.Validate(); err != nil {
			return err
		}
	}

	switch {
	case !r.Value.IsPositive():
		return invalid("value must be greater than zero")
	case !r.StartDate.Before(r.EndDate):
		return invalid("start_date must be before end_date")
	case r.Priority < 0:
		return invalid("priority must be zero or greater")
	case r.Type != types.COMMITMENT_TYPE_AMOUNT && r.Type != types.COMMITMENT_TYPE_QUANTITY:
		return invalid("type must be amount or quantity")
	case r.Type == types.COMMITMENT_TYPE_QUANTITY && (r.CommittedRate == nil || !r.CommittedRate.IsPositive()):
		return invalid("committed_rate greater than zero is required for quantity commitments")
	case r.Type == types.COMMITMENT_TYPE_AMOUNT && r.CommittedRate != nil:
		return invalid("committed_rate is not allowed on amount commitments")
	case r.OverageRate != nil && r.Type != types.COMMITMENT_TYPE_QUANTITY:
		return invalid("overage_rate is supported only on quantity commitments")
	case r.OverageRate != nil && r.OverageRate.IsNegative():
		return invalid("overage_rate must be zero or greater")
	case len(r.ActiveWindows) > 0 && r.Measurement != types.CommitmentMeasurementWindow:
		return invalid("active_windows require measurement=window")
	}

	if _, err := time.LoadLocation(r.Timezone); err != nil || r.Timezone == "" {
		return invalid("timezone must be a valid IANA name")
	}

	binding := lo.FromPtrOr(r.Binding, false)
	if !binding && r.CollectionMethod != nil {
		return invalid("non-binding commitments take no collection_method")
	}

	if r.CollectionMethod != nil {
		if err := r.CollectionMethod.Validate(); err != nil {
			return err
		}

		if *r.CollectionMethod == types.CommitmentCollectionBillingTerms {
			return invalid("collection_method billing_terms is not yet supported")
		}
	}

	if len(r.Name) == 0 || len(r.Name) > 255 || len(r.InvoiceLabel) > 255 {
		return invalid("name and invoice_label must be 1-255 characters")
	}

	if r.InvoiceDescription != nil && len(*r.InvoiceDescription) > 500 {
		return invalid("invoice_description must be at most 500 characters")
	}

	return nil
}

// ToCommitment builds the domain commitment for sub. Price IDs are set by the caller.
func (r *CreateCommitmentRequest) ToCommitment(
	ctx context.Context,
	sub *subscription.Subscription,
) *commitment.Commitment {
	return &commitment.Commitment{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COMMITMENT),
		SubscriptionID:     r.SubscriptionID,
		CustomerID:         sub.CustomerID,
		Name:               r.Name,
		InvoiceLabel:       r.InvoiceLabel,
		InvoiceDescription: r.InvoiceDescription,
		Currency:           sub.Currency,
		ScopeEntityType:    r.Scope.EntityType,
		ScopeEntityID:      r.Scope.EntityID,
		Type:               r.Type,
		Value:              r.Value,
		Measurement:        r.Measurement,
		ActiveWindows:      r.ActiveWindows,
		Timezone:           r.Timezone,
		Priority:           r.Priority,
		Binding:            lo.FromPtrOr(r.Binding, false),
		CollectionMethod:   r.CollectionMethod,
		StartDate:          r.StartDate,
		EndDate:            r.EndDate,
		CommitmentStatus:   types.CommitmentStatusActive,
		IdempotencyKey:     r.IdempotencyKey,
		Metadata:           r.Metadata,
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
}

type UpdateCommitmentRequest struct {
	Name               *string        `json:"name,omitempty" validate:"omitempty,max=255"`
	InvoiceLabel       *string        `json:"invoice_label,omitempty" validate:"omitempty,max=255"`
	InvoiceDescription *string        `json:"invoice_description,omitempty" validate:"omitempty,max=500"`
	Metadata           types.Metadata `json:"metadata,omitempty"`
}

type TerminateCommitmentRequest struct {
	EffectiveAt     *time.Time                 `json:"effective_at,omitempty"`
	CancellationFee *CommitmentCancellationFee `json:"cancellation_fee,omitempty"`
}

// CommitmentCancellationFee is charged on early termination; value is a percent for percent_of_remaining.
type CommitmentCancellationFee struct {
	Type  types.CommitmentCancellationFeeType `json:"type" validate:"required"`
	Value decimal.Decimal                     `json:"value" swaggertype:"string"`
}

func (r *TerminateCommitmentRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	fee := r.CancellationFee
	if fee == nil {
		return nil
	}

	if err := fee.Type.Validate(); err != nil {
		return err
	}

	switch {
	case !fee.Value.IsPositive():
		return invalid("cancellation_fee.value must be greater than zero")
	case fee.Type == types.CommitmentCancellationFeePercentOfRemaining && fee.Value.GreaterThan(decimal.NewFromInt(100)):
		return invalid("cancellation_fee.value must be at most 100 for percent_of_remaining")
	}

	return nil
}

type CommitmentResponse struct {
	*commitment.Commitment `json:",inline"`
	CommitmentValue        *decimal.Decimal `json:"commitment_value,omitempty" swaggertype:"string"`
}

type ListCommitmentsResponse = types.ListResponse[*CommitmentResponse] // @name ListCommitmentsResponse
