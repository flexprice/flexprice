package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type CommitmentScopeInput struct {
	EntityType types.CommitmentScopeType `json:"entity_type" validate:"required"`
	// EntityID may be omitted on an inline subscription-scoped commitment; the new subscription's ID is used.
	EntityID string `json:"entity_id"`
}

// CommitmentPriceInput prices a commitment's committed or overage rate; billing_model defaults to FLAT_FEE.
type CommitmentPriceInput struct {
	BillingModel      types.BillingModel       `json:"billing_model,omitempty"`
	Amount            *decimal.Decimal         `json:"amount,omitempty" swaggertype:"string"`
	TierMode          types.BillingTier        `json:"tier_mode,omitempty"`
	Tiers             []CreatePriceTier        `json:"tiers,omitempty"`
	TransformQuantity *price.TransformQuantity `json:"transform_quantity,omitempty"`
}

// ToCreatePriceRequest builds the subscription-scoped usage price backing the rate; kind is committed or overage.
func (in *CommitmentPriceInput) ToCreatePriceRequest(
	c *commitment.Commitment,
	sub *subscription.Subscription,
	meterID string,
	kind string,
) CreatePriceRequest {
	return CreatePriceRequest{
		Amount:               in.Amount,
		Currency:             c.Currency,
		EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:             c.SubscriptionID,
		Type:                 types.PRICE_TYPE_USAGE,
		PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
		BillingPeriod:        sub.BillingPeriod,
		BillingPeriodCount:   sub.BillingPeriodCount,
		BillingModel:         in.BillingModel,
		TierMode:             in.TierMode,
		Tiers:                in.Tiers,
		TransformQuantity:    in.TransformQuantity,
		InvoiceCadence:       types.InvoiceCadenceArrear,
		MeterID:              meterID,
		DisplayName:          c.Name + " (" + kind + ")",
		SkipEntityValidation: true,
		Metadata: map[string]string{
			"commitment_id":   c.ID,
			"commitment_rate": kind,
		},
	}
}

// ToPrice materialises the rate as a transient price, for valuing a commitment before its price rows exist.
func (in *CommitmentPriceInput) ToPrice(currency string) *price.Price {
	transform := price.JSONBTransformQuantity{DivideBy: 1}
	if in.TransformQuantity != nil {
		transform = price.JSONBTransformQuantity(*in.TransformQuantity)
	}

	tiers, _ := (&CreatePriceRequest{}).convertTiers(in.Tiers)
	return &price.Price{
		Amount:            lo.FromPtrOr(in.Amount, decimal.Zero),
		Currency:          currency,
		PriceUnitType:     types.PRICE_UNIT_TYPE_FIAT,
		Type:              types.PRICE_TYPE_USAGE,
		BillingModel:      in.BillingModel,
		TierMode:          in.TierMode,
		Tiers:             tiers,
		TransformQuantity: transform,
	}
}

type CreateCommitmentRequest struct {
	// SubscriptionID is required on POST /commitments and set server-side on inline commitments.
	SubscriptionID     string                            `json:"subscription_id"`
	IdempotencyKey     *string                           `json:"idempotency_key,omitempty" validate:"omitempty,max=128"`
	Name               string                            `json:"name" validate:"required,max=255"`
	InvoiceLabel       string                            `json:"invoice_label,omitempty" validate:"omitempty,max=255"`
	InvoiceDescription *string                           `json:"invoice_description,omitempty" validate:"omitempty,max=500"`
	Scope              CommitmentScopeInput              `json:"scope" validate:"required"`
	Type               types.CommitmentType              `json:"type" validate:"required"`
	Value              decimal.Decimal                   `json:"value" swaggertype:"string"`
	CommittedPrice     *CommitmentPriceInput             `json:"committed_price,omitempty"`
	OveragePrice       *CommitmentPriceInput             `json:"overage_price,omitempty"`
	Measurement        types.CommitmentMeasurement       `json:"measurement" validate:"required"`
	ActiveWindows      types.ActiveWindows               `json:"active_windows,omitempty"`
	Timezone           string                            `json:"timezone,omitempty"`
	Priority           int                               `json:"priority"`
	Binding            *bool                             `json:"binding,omitempty"` // default false
	CollectionMethod   *types.CommitmentCollectionMethod `json:"collection_method,omitempty"`
	StartDate          time.Time                         `json:"start_date" validate:"required"`
	// EndDate is exclusive; omit it for an open-ended commitment, which bills every period until terminated.
	EndDate  *time.Time     `json:"end_date,omitempty"`
	Metadata types.Metadata `json:"metadata,omitempty"`
}

// ApplyDefaults fills label, timezone and the binding-dependent settings.
func (r *CreateCommitmentRequest) ApplyDefaults(subTimezone string) {
	// Term edges align to whole seconds, like every computed billing date; the loader's window grid
	// and pool checks compare at that granularity.
	r.StartDate = r.StartDate.Truncate(time.Second)
	if r.EndDate != nil {
		r.EndDate = lo.ToPtr(r.EndDate.Truncate(time.Second))
	}

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

	for _, p := range []*CommitmentPriceInput{r.CommittedPrice, r.OveragePrice} {
		if p != nil && p.BillingModel == "" {
			p.BillingModel = types.BILLING_MODEL_FLAT_FEE
		}
	}
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
	case r.SubscriptionID == "":
		return ierr.Validation("subscription_id is required")
	case r.Scope.EntityID == "":
		return ierr.Validation("scope.entity_id is required")
	case !r.Value.IsPositive():
		return ierr.Validation("value must be greater than zero")
	case r.EndDate != nil && !r.StartDate.Before(*r.EndDate):
		return ierr.Validation("start_date must be before end_date")
	case r.Priority < 0:
		return ierr.Validation("priority must be zero or greater")
	case r.Type != types.COMMITMENT_TYPE_AMOUNT && r.Type != types.COMMITMENT_TYPE_QUANTITY:
		return ierr.Validation("type must be amount or quantity")
	case r.Type == types.COMMITMENT_TYPE_QUANTITY && r.CommittedPrice == nil:
		return ierr.Validation("committed_price is required for quantity commitments")
	case r.Type == types.COMMITMENT_TYPE_AMOUNT && r.CommittedPrice != nil:
		return ierr.Validation("committed_price is not allowed on amount commitments")
	case r.OveragePrice != nil && r.Type != types.COMMITMENT_TYPE_QUANTITY:
		return ierr.Validation("overage_price is supported only on quantity commitments")
	case len(r.ActiveWindows) > 0 && r.Measurement != types.CommitmentMeasurementWindow:
		return ierr.Validation("active_windows require measurement=window")
	}

	if err := r.validatePrices(); err != nil {
		return err
	}

	if _, err := time.LoadLocation(r.Timezone); err != nil || r.Timezone == "" {
		return ierr.Validation("timezone must be a valid IANA name")
	}

	binding := lo.FromPtrOr(r.Binding, false)
	if !binding && r.CollectionMethod != nil {
		return ierr.Validation("non-binding commitments take no collection_method")
	}

	if r.CollectionMethod != nil {
		if err := r.CollectionMethod.Validate(); err != nil {
			return err
		}

		if *r.CollectionMethod == types.CommitmentCollectionBillingTerms {
			return ierr.Validation("collection_method billing_terms is not yet supported")
		}
	}

	if len(r.Name) == 0 || len(r.Name) > 255 || len(r.InvoiceLabel) > 255 {
		return ierr.Validation("name and invoice_label must be 1-255 characters")
	}

	if r.InvoiceDescription != nil && len(*r.InvoiceDescription) > 500 {
		return ierr.Validation("invoice_description must be at most 500 characters")
	}

	return nil
}

// validatePrices checks each rate against its billing model; the price request's own validation covers tier and
// package details once the scope meter is known.
func (r *CreateCommitmentRequest) validatePrices() error {
	if r.CommittedPrice != nil {
		if err := r.CommittedPrice.validate("committed_price", true); err != nil {
			return err
		}
	}

	if r.OveragePrice != nil {
		return r.OveragePrice.validate("overage_price", false)
	}

	return nil
}

// validate checks the rate against its billing model; positive requires a FLAT_FEE or PACKAGE amount above zero.
func (in *CommitmentPriceInput) validate(field string, positive bool) error {
	if in.BillingModel != types.BILLING_MODEL_TIERED && len(in.Tiers) > 0 {
		return ierr.Validationf("%s.tiers can only be set on a TIERED price", field)
	}

	switch in.BillingModel {
	case types.BILLING_MODEL_FLAT_FEE, types.BILLING_MODEL_PACKAGE:
		if in.Amount == nil || in.Amount.IsNegative() {
			return ierr.Validationf("%s.amount must be zero or greater", field)
		}

		if positive && !in.Amount.IsPositive() {
			return ierr.Validationf("%s.amount must be greater than zero", field)
		}

		if in.BillingModel == types.BILLING_MODEL_PACKAGE && in.TransformQuantity == nil {
			return ierr.Validationf("%s.transform_quantity is required for PACKAGE", field)
		}
	case types.BILLING_MODEL_TIERED:
		if in.TierMode == "" {
			return ierr.Validationf("%s.tier_mode is required for TIERED", field)
		}

		if err := in.TierMode.Validate(); err != nil {
			return err
		}

		if positive && in.TierMode == types.BILLING_TIER_VOLUME {
			return ierr.Validationf("%s.tier_mode must be SLAB: committed prices must price marginally so usage never exceeds the commitment's value", field)
		}

		if len(in.Tiers) == 0 {
			return ierr.Validationf("%s.tiers are required for TIERED", field)
		}
	default:
		return ierr.Validationf("%s.billing_model must be FLAT_FEE, TIERED or PACKAGE", field)
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

// TerminateCommitmentRequest ends a commitment early; cancellation_fee is a flat amount in the commitment currency.
type TerminateCommitmentRequest struct {
	EffectiveAt     *time.Time       `json:"effective_at,omitempty"`
	CancellationFee *decimal.Decimal `json:"cancellation_fee,omitempty" swaggertype:"string"`
}

func (r *TerminateCommitmentRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.CancellationFee != nil && !r.CancellationFee.IsPositive() {
		return ierr.Validation("cancellation_fee must be greater than zero")
	}

	return nil
}

type CommitmentResponse struct {
	*commitment.Commitment `json:",inline"`
	CommitmentValue        *decimal.Decimal `json:"commitment_value,omitempty" swaggertype:"string"`
	TerminationInvoiceID   *string          `json:"termination_invoice_id,omitempty"`
}

type ListCommitmentsResponse = types.ListResponse[*CommitmentResponse] // @name ListCommitmentsResponse
