package dto

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type SubscriptionPhaseCreateRequest struct {
	StartDate time.Time  `json:"start_date" validate:"required"`
	EndDate   *time.Time `json:"end_date,omitempty"`

	// SubscriptionCoupons is the preferred way to attach coupons to this phase.
	SubscriptionCoupons []SubscriptionCouponInput `json:"subscription_coupons,omitempty"`

	// Deprecated: use SubscriptionCoupons instead.
	Coupons []string `json:"coupons,omitempty"`

	// Deprecated: use SubscriptionCoupons instead.
	LineItemCoupons map[string][]string `json:"line_item_coupons,omitempty"`

	// OverrideLineItems overrides specific plan prices for this phase.
	OverrideLineItems []OverrideLineItemRequest `json:"override_line_items,omitempty" validate:"omitempty,dive"`

	// LineItems are extra (non-plan) line items for this phase; start_date defaults to phase start.
	LineItems []CreateSubscriptionLineItemRequest `json:"line_items,omitempty" validate:"omitempty,dive"`

	Metadata map[string]string `json:"metadata,omitempty"`
}

func (r *SubscriptionPhaseCreateRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.EndDate != nil && r.EndDate.Before(r.StartDate) {
		return ierr.NewError("end_date cannot be before start_date").
			WithHint("Ensure the phase end date is on or after the start date").
			Mark(ierr.ErrValidation)
	}

	// Full business-rule validation (price context, dates) is deferred to the service layer.
	for i, li := range r.LineItems {
		if err := li.Validate(nil, nil); err != nil {
			return ierr.NewErrorf("invalid line_item at index %d: %s", i, err.Error()).
				WithHint("Check the line item fields at the given index").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

func (r *SubscriptionPhaseCreateRequest) ToSubscriptionPhase(ctx context.Context, subscriptionID string) *subscription.SubscriptionPhase {
	return &subscription.SubscriptionPhase{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_PHASE),
		SubscriptionID: subscriptionID,
		StartDate:      r.StartDate,
		EndDate:        r.EndDate,
		Metadata:       r.Metadata,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
}

type LineItemCommitmentConfig struct {
	CommitmentAmount   *decimal.Decimal     `json:"commitment_amount,omitempty"`
	CommitmentQuantity *decimal.Decimal     `json:"commitment_quantity,omitempty"`
	CommitmentType     types.CommitmentType `json:"commitment_type,omitempty"`
	// OverageFactor is a multiplier on usage beyond commitment; 1.0 = base rate.
	OverageFactor *decimal.Decimal `json:"overage_factor,omitempty"`
	// EnableTrueUp charges the shortfall when usage is below commitment.
	EnableTrueUp *bool `json:"enable_true_up,omitempty"`
	// IsWindowCommitment applies commitment per window (e.g. per day) rather than per billing period.
	IsWindowCommitment *bool `json:"is_window_commitment,omitempty"`
	// CommitmentDuration allows a different period than billing (e.g. ANNUAL commitment on MONTHLY billing).
	CommitmentDuration *types.BillingPeriod `json:"commitment_duration,omitempty"`
	// CommitmentTimeBuckets scopes commitment to specific UTC-hour windows; requires IsWindowCommitment=true.
	CommitmentTimeBuckets []CommitmentBucketRequest `json:"commitment_time_buckets,omitempty"`
}

func (c *LineItemCommitmentConfig) ToDomainBuckets() types.TimeOfDayBuckets {
	return bucketRequestsToDomain(c.CommitmentTimeBuckets)
}

func validateLineItemCommitments(commitments map[string]*LineItemCommitmentConfig) error {
	if len(commitments) == 0 {
		return nil
	}

	for priceID, commitmentConfig := range commitments {
		if priceID == "" {
			return ierr.NewError("price_id cannot be empty in line_item_commitments").
				WithHint("Each entry in line_item_commitments must have a valid price_id as the key").
				Mark(ierr.ErrValidation)
		}

		if commitmentConfig == nil {
			return ierr.NewError("commitment config cannot be nil").
				WithHint(fmt.Sprintf("Commitment configuration for price_id %s cannot be nil", priceID)).
				WithReportableDetails(map[string]interface{}{
					"price_id": priceID,
				}).
				Mark(ierr.ErrValidation)
		}

		if err := commitmentConfig.Validate(); err != nil {
			return ierr.NewError(fmt.Sprintf("invalid commitment config for price_id %s", priceID)).
				WithHint("Line item commitment validation failed").
				WithReportableDetails(map[string]interface{}{
					"price_id": priceID,
					"error":    err.Error(),
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

func validateNoDuplicateOverridePriceIDs(overrides []OverrideLineItemRequest) error {
	priceIDsSeen := make(map[string]bool)
	for i, override := range overrides {
		if priceIDsSeen[override.PriceID] {
			return ierr.NewError(fmt.Sprintf("duplicate price_id in override line items at index %d", i)).
				WithHint("Each price can only be overridden once per subscription").
				WithReportableDetails(map[string]interface{}{
					"price_id": override.PriceID,
					"index":    i,
				}).
				Mark(ierr.ErrValidation)
		}
		priceIDsSeen[override.PriceID] = true
	}
	return nil
}

func (c *LineItemCommitmentConfig) Validate() error {
	hasAmountCommitment := c.CommitmentAmount != nil && c.CommitmentAmount.GreaterThan(decimal.Zero)
	hasQuantityCommitment := c.CommitmentQuantity != nil && c.CommitmentQuantity.GreaterThan(decimal.Zero)
	hasCommitment := hasAmountCommitment || hasQuantityCommitment

	if !hasCommitment {
		return nil
	}

	if hasAmountCommitment && hasQuantityCommitment {
		return ierr.NewError("cannot set both commitment_amount and commitment_quantity").
			WithHint("Specify either commitment_amount or commitment_quantity, not both").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount":   c.CommitmentAmount,
				"commitment_quantity": c.CommitmentQuantity,
			}).
			Mark(ierr.ErrValidation)
	}

	if c.CommitmentType != "" && !c.CommitmentType.Validate() {
		return ierr.NewError("invalid commitment_type").
			WithHint("Commitment type must be either 'amount' or 'quantity'").
			WithReportableDetails(map[string]interface{}{
				"commitment_type": c.CommitmentType,
			}).
			Mark(ierr.ErrValidation)
	}

	if c.CommitmentType == "" {
		if hasAmountCommitment {
			c.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
		} else if hasQuantityCommitment {
			c.CommitmentType = types.COMMITMENT_TYPE_QUANTITY
		}
	} else {
		if hasAmountCommitment && c.CommitmentType != types.COMMITMENT_TYPE_AMOUNT {
			return ierr.NewError("commitment_type mismatch").
				WithHint("When commitment_amount is set, commitment_type must be 'amount'").
				WithReportableDetails(map[string]interface{}{
					"commitment_type":   c.CommitmentType,
					"commitment_amount": c.CommitmentAmount,
				}).
				Mark(ierr.ErrValidation)
		}

		if hasQuantityCommitment && c.CommitmentType != types.COMMITMENT_TYPE_QUANTITY {
			return ierr.NewError("commitment_type mismatch").
				WithHint("When commitment_quantity is set, commitment_type must be 'quantity'").
				WithReportableDetails(map[string]interface{}{
					"commitment_type":     c.CommitmentType,
					"commitment_quantity": c.CommitmentQuantity,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// overage_factor is optional; omitting it defaults to 1.0, meaning usage beyond
	// the commitment bills at the base rate.
	if c.OverageFactor == nil {
		c.OverageFactor = types.DefaultOverageFactor()
	}

	if c.OverageFactor.LessThan(decimal.NewFromInt(1)) {
		return ierr.NewError("overage_factor must be at least 1.0").
			WithHint("Overage factor determines the multiplier for usage beyond commitment").
			WithReportableDetails(map[string]interface{}{
				"overage_factor": c.OverageFactor,
			}).
			Mark(ierr.ErrValidation)
	}

	if hasAmountCommitment && c.CommitmentAmount.IsNegative() {
		return ierr.NewError("commitment_amount must be non-negative").
			WithHint("Commitment amount cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount": c.CommitmentAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	if hasQuantityCommitment && c.CommitmentQuantity.IsNegative() {
		return ierr.NewError("commitment_quantity must be non-negative").
			WithHint("Commitment quantity cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"commitment_quantity": c.CommitmentQuantity,
			}).
			Mark(ierr.ErrValidation)
	}

	if len(c.CommitmentTimeBuckets) > 0 {
		if c.IsWindowCommitment == nil || !*c.IsWindowCommitment {
			return ierr.NewError("commitment_time_buckets requires is_window_commitment=true").
				WithHint("Set is_window_commitment=true to apply commitment only during the configured hours").
				Mark(ierr.ErrValidation)
		}
		if err := validateTimeOfDayBuckets(c.CommitmentTimeBuckets); err != nil {
			return err
		}
	}

	return nil
}

// SubscriptionCouponRequest applies a coupon at subscription level or to a specific line item.
type SubscriptionCouponRequest struct {
	CouponID            string     `json:"coupon_id" validate:"required"`
	StartDate           time.Time  `json:"start_date" validate:"required"`
	EndDate             *time.Time `json:"end_date,omitempty"`
	LineItemID          *string    `json:"line_item_id,omitempty"`
	SubscriptionPhaseID *string    `json:"subscription_phase_id,omitempty"`
}

func (r *SubscriptionCouponRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.EndDate != nil {
		if r.EndDate.Before(r.StartDate) {
			return ierr.NewError("end_date cannot be before start_date").
				WithHint("Ensure the coupon end date is on or after the start date").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// SubscriptionCouponInput is the preferred coupon attachment request (by coupon_code, not ID).
// Optionally targets a specific line item via price_id; omit for subscription-level.
type SubscriptionCouponInput struct {
	CouponCode string     `json:"coupon_code" validate:"required"`
	StartDate  *time.Time `json:"start_date,omitempty"`
	EndDate    *time.Time `json:"end_date,omitempty"`
	PriceID    *string    `json:"price_id,omitempty"`
}

func (r *SubscriptionCouponInput) Validate() error {
	if r.CouponCode == "" {
		return ierr.NewError("coupon_code is required").
			WithHint("Provide a valid coupon code").
			Mark(ierr.ErrValidation)
	}
	if r.EndDate != nil && r.StartDate != nil && r.EndDate.Before(*r.StartDate) {
		return ierr.NewError("end_date cannot be before start_date").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// GroupedInvoicingChildRequest creates a grouped_invoicing child inline with the parent request.
// Billing schedule fields (period, cycle, anchor, currency, start_date) are inherited from the parent.
type GroupedInvoicingChildRequest struct {
	PlanID             string `json:"plan_id" validate:"required"`
	ExternalCustomerID string `json:"external_customer_id" validate:"required"`
	SubscriptionCreationConfig
}

// SubscriptionInheritanceConfig holds all customer-hierarchy and invoicing-routing fields.
type SubscriptionInheritanceConfig struct {
	// ExternalCustomerIDsToInheritSubscription creates inherited skeleton subscriptions for child customers. Parent only.
	ExternalCustomerIDsToInheritSubscription []string `json:"external_customer_ids_to_inherit_subscription,omitempty"`

	// ParentSubscriptionID links to an existing parent. Required for inherited/grouped_invoicing; rejected for standalone/delegated/parent.
	ParentSubscriptionID string `json:"parent_subscription_id,omitempty"`

	// InvoicingCustomerExternalID routes invoices to a different customer. Required for delegated; rejected for inherited.
	InvoicingCustomerExternalID *string `json:"invoicing_customer_external_id,omitempty"`

	// SubscriptionsIDsForGroupedInvoicing converts existing standalone subscriptions to grouped_invoicing under this parent. Parent only.
	SubscriptionsIDsForGroupedInvoicing []string `json:"subscriptions_ids_for_grouped_invoicing,omitempty"`

	GroupedInvoicingChildrenToCreate []GroupedInvoicingChildRequest `json:"grouped_invoicing_children_to_create,omitempty" validate:"omitempty,dive"`
}

func (c *SubscriptionInheritanceConfig) checkoutUnsupportedFields() bool {
	if c == nil {
		return false
	}

	if len(c.ExternalCustomerIDsToInheritSubscription) > 0 {
		return true
	}
	if c.ParentSubscriptionID != "" {
		return true
	}
	if c.InvoicingCustomerExternalID != nil {
		return true
	}
	if len(c.SubscriptionsIDsForGroupedInvoicing) > 0 {
		return true
	}

	return false
}

func (c *SubscriptionInheritanceConfig) Validate() error {
	if c == nil {
		return nil
	}

	if c.ParentSubscriptionID != "" && len(c.ExternalCustomerIDsToInheritSubscription) > 0 {
		return ierr.NewError("cannot set parent_subscription_id together with external_customer_ids_to_inherit_subscription").
			WithHint("Use either a parent subscription link or child customers to inherit, not both").
			Mark(ierr.ErrValidation)
	}

	if c.InvoicingCustomerExternalID != nil && len(c.ExternalCustomerIDsToInheritSubscription) > 0 {
		return ierr.NewError("cannot set invoicing_customer_external_id together with external_customer_ids_to_inherit_subscription").
			WithHint("Use either invoicing_customer_external_id or external_customer_ids_to_inherit_subscription, not both").
			Mark(ierr.ErrValidation)
	}

	if len(c.SubscriptionsIDsForGroupedInvoicing) > 0 && c.ParentSubscriptionID != "" {
		return ierr.NewError("cannot set subscriptions_ids_for_grouped_invoicing together with parent_subscription_id").
			WithHint("subscriptions_ids_for_grouped_invoicing can only be used when creating a parent subscription").
			Mark(ierr.ErrValidation)
	}

	if len(c.SubscriptionsIDsForGroupedInvoicing) > 0 && c.InvoicingCustomerExternalID != nil {
		return ierr.NewError("cannot set subscriptions_ids_for_grouped_invoicing together with invoicing_customer_external_id").
			WithHint("Use either grouped invoicing conversion or delegated invoicing, not both").
			Mark(ierr.ErrValidation)
	}

	if len(c.SubscriptionsIDsForGroupedInvoicing) > 0 && len(c.ExternalCustomerIDsToInheritSubscription) > 0 {
		return ierr.NewError("cannot set subscriptions_ids_for_grouped_invoicing together with external_customer_ids_to_inherit_subscription").
			WithHint("Use either grouped invoicing conversion or inherited child creation, not both").
			Mark(ierr.ErrValidation)
	}

	if len(c.GroupedInvoicingChildrenToCreate) > 0 && len(c.SubscriptionsIDsForGroupedInvoicing) > 0 {
		return ierr.NewError("cannot set grouped_invoicing_children_to_create together with subscriptions_ids_for_grouped_invoicing").
			WithHint("Use either inline child creation or existing-subscription conversion, not both").
			Mark(ierr.ErrValidation)
	}

	// InvoicingCustomerExternalID is still allowed here (delegated payer for seat fees).
	if len(c.GroupedInvoicingChildrenToCreate) > 0 && c.ParentSubscriptionID != "" {
		return ierr.NewError("cannot set grouped_invoicing_children_to_create together with parent_subscription_id").
			WithHint("grouped_invoicing_children_to_create can only be used when creating a parent subscription").
			Mark(ierr.ErrValidation)
	}

	for i, id := range c.SubscriptionsIDsForGroupedInvoicing {
		if id == "" {
			return ierr.NewError("subscriptions_ids_for_grouped_invoicing cannot contain empty values").
				WithHint(fmt.Sprintf("Subscription ID at index %d is empty", i)).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// SubscriptionCreationConfig holds optional configuration shared by CreateSubscriptionRequest and
// GroupedInvoicingChildRequest, using identical JSON fields and validation for both.
type SubscriptionCreationConfig struct {
	// TrialPeriodDays: nil = inherit from plan prices, 0 = no trial, >0 = override in days.
	TrialPeriodDays *int `json:"trial_period_days,omitempty"`

	CreditGrants []CreateCreditGrantRequest `json:"credit_grants,omitempty"`

	CommitmentAmount *decimal.Decimal `json:"commitment_amount,omitempty" swaggertype:"string"`

	// CommitmentDuration allows a different commitment period than billing (e.g. ANNUAL on MONTHLY).
	CommitmentDuration *types.BillingPeriod `json:"commitment_duration,omitempty"`

	OverageFactor    *decimal.Decimal   `json:"overage_factor,omitempty" swaggertype:"string"`
	EnableTrueUp     bool               `json:"enable_true_up"`
	TaxRateOverrides []*TaxRateOverride `json:"tax_rate_overrides,omitempty"`

	// SubscriptionCoupons is the preferred way to attach coupons at creation.
	// Accepts coupon_code; optionally targets a line item via price_id.
	SubscriptionCoupons []SubscriptionCouponInput `json:"subscription_coupons,omitempty"`

	// Deprecated: use SubscriptionCoupons instead.
	Coupons []string `json:"coupons,omitempty"`

	// Deprecated: use SubscriptionCoupons instead.
	LineItemCoupons map[string][]string `json:"line_item_coupons,omitempty"`

	// LineItemCommitments sets per-line-item commitment config, keyed by price_id.
	LineItemCommitments map[string]*LineItemCommitmentConfig `json:"line_item_commitments,omitempty" validate:"omitempty,dive"`

	// OverrideLineItems overrides specific plan prices for this subscription.
	OverrideLineItems    []OverrideLineItemRequest       `json:"override_line_items,omitempty" validate:"omitempty,dive"`
	OverrideEntitlements []OverrideEntitlementRequest    `json:"override_entitlements,omitempty" validate:"omitempty,dive"`
	Addons               []AddAddonToSubscriptionRequest `json:"addons,omitempty" validate:"omitempty,dive"`
	// LineItems are extra (non-plan) line items added at creation.
	LineItems []CreateSubscriptionLineItemRequest `json:"line_items,omitempty" validate:"omitempty,dive"`
	Phases    []SubscriptionPhaseCreateRequest    `json:"phases,omitempty" validate:"omitempty,dive"`
}

func (c *SubscriptionCreationConfig) Validate() error {
	if c.CommitmentAmount != nil && c.CommitmentAmount.LessThan(decimal.Zero) {
		return ierr.NewError("commitment_amount must be non-negative").
			WithHint("Commitment amount must be greater than or equal to 0").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount": *c.CommitmentAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	if c.OverageFactor != nil && c.OverageFactor.LessThan(decimal.NewFromInt(1)) {
		return ierr.NewError("overage_factor must be at least 1.0").
			WithHint("Overage factor must be greater than or equal to 1.0").
			WithReportableDetails(map[string]interface{}{
				"overage_factor": *c.OverageFactor,
			}).
			Mark(ierr.ErrValidation)
	}

	if len(c.CreditGrants) > 0 {
		for i, grant := range c.CreditGrants {
			// Grants created with a subscription must have SUBSCRIPTION scope.
			if grant.Scope != types.CreditGrantScopeSubscription {
				return ierr.NewError("invalid credit grant scope").
					WithHint("Credit grants created with a subscription must have SUBSCRIPTION scope").
					WithReportableDetails(map[string]interface{}{
						"grant_scope": grant.Scope,
						"grant_index": i,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	}

	if len(c.TaxRateOverrides) > 0 {
		for _, taxRateOverride := range c.TaxRateOverrides {
			if err := taxRateOverride.Validate(); err != nil {
				return ierr.NewError("invalid tax rate override").
					WithHint("Tax rate override validation failed").
					WithReportableDetails(map[string]interface{}{
						"error":             err.Error(),
						"tax_rate_override": taxRateOverride,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	}

	if err := validateLineItemCommitments(c.LineItemCommitments); err != nil {
		return err
	}

	if err := validateNoDuplicateOverridePriceIDs(c.OverrideLineItems); err != nil {
		return err
	}

	const maxLineItems = 100
	if len(c.LineItems) > maxLineItems {
		return ierr.NewError("line_items exceeds maximum allowed").
			WithHint(fmt.Sprintf("At most %d line items can be added at subscription creation", maxLineItems)).
			WithReportableDetails(map[string]interface{}{
				"count": len(c.LineItems),
				"max":   maxLineItems,
			}).
			Mark(ierr.ErrValidation)
	}
	for i, item := range c.LineItems {
		if err := item.Validate(nil, nil); err != nil {
			return ierr.WithError(err).
				WithHint(fmt.Sprintf("Line item validation failed at index %d", i)).
				WithReportableDetails(map[string]interface{}{"line_item_index": i}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

type CreateSubscriptionRequest struct {
	// ID is pre-generated for internal use (e.g. Paddle mapping before write). Never from external JSON.
	// TODO: Remove once plan-change integration carryover is handled generically.
	ID string `json:"-"`

	// CustomerID takes priority over ExternalCustomerID when both are provided.
	CustomerID         string `json:"customer_id"`
	ExternalCustomerID string `json:"external_customer_id"`

	PlanID    string     `json:"plan_id" validate:"required"`
	Currency  string     `json:"currency" validate:"required,len=3"`
	LookupKey string     `json:"lookup_key"`
	StartDate *time.Time `json:"start_date,omitempty"`
	EndDate   *time.Time `json:"end_date,omitempty"`

	// TrialStart/TrialEnd are for internal integrations (e.g. Stripe sync); not accepted from public JSON.
	TrialStart *time.Time `json:"-"`
	TrialEnd   *time.Time `json:"-"`

	BillingCadence     types.BillingCadence `json:"-"`
	BillingPeriod      types.BillingPeriod  `json:"billing_period" validate:"required"`
	BillingPeriodCount int                  `json:"billing_period_count" default:"1"`
	Metadata           map[string]string    `json:"metadata,omitempty"`

	// BillingCycle controls the billing anchor. anniversary = start_date; calendar = period boundary
	// (e.g. monthly with start 2025-04-15 → calendar anchor is 2025-05-01).
	BillingCycle types.BillingCycle `json:"billing_cycle"`

	PaymentBehavior        *types.PaymentBehavior  `json:"payment_behavior,omitempty"`
	GatewayPaymentMethodID *string                 `json:"gateway_payment_method_id,omitempty"`
	CollectionMethod       *types.CollectionMethod `json:"collection_method,omitempty"`
	// ProrationBehavior: create_prorations or none (default). Ignored for anniversary billing.
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`
	Timezone          string                  `json:"timezone" validate:"omitempty,timezone"`
	// BillingAnchor overrides the derived anchor for anniversary billing. For monthly billing,
	// the day-of-month defines cycle boundaries (shorter first period if start is before that day).
	BillingAnchor *time.Time `json:"billing_anchor,omitempty"`

	Workflow *types.TemporalWorkflowType `json:"-"`

	// SubscriptionStatus: set to "draft" to skip invoice creation and payment on create.
	SubscriptionStatus types.SubscriptionStatus `json:"subscription_status,omitempty"`
	PaymentTerms       *types.PaymentTerms      `json:"payment_terms,omitempty"`

	// AutoInvoiceThreshold triggers a mid-period invoice when usage (in subscription currency) exceeds this amount.
	// Standalone subscriptions only; all plan prices must be usage-based. Immutable after creation.
	AutoInvoiceThreshold *decimal.Decimal `json:"auto_invoice_threshold,omitempty" swaggertype:"string"`

	// IncludePriceIDs selects which plan prices to attach. Nil/omitted attaches matching-cadence
	// prices plus ONETIME; [] attaches none (LineItems extras still apply); a non-empty list
	// attaches only those IDs. Each listed ID must belong to the plan, match the subscription
	// currency, and have a cadence that equals or strictly divides the subscription cadence.
	// Pointer-slice distinguishes nil from [].
	// NOTE: no `dive,required` on this tag — swaggo misinterprets `required`
	// inside `dive` as marking the whole field required, which then shows up
	// in the OpenAPI schema and breaks callers that omit the field. Per-element
	// non-emptiness is enforced explicitly in Validate() below.
	IncludePriceIDs *[]string `json:"include_price_ids,omitempty"`

	// Inheritance groups customer-hierarchy fields; providing child IDs makes this a PARENT subscription.
	Inheritance *SubscriptionInheritanceConfig `json:"inheritance,omitempty"`

	// SubscriptionType is set internally by the service layer.
	SubscriptionType types.SubscriptionType `json:"-"`

	// OpeningInvoiceAdjustmentAmount nets the opening invoice against a proration/cancel credit (e.g. on plan change). Internal only.
	OpeningInvoiceAdjustmentAmount *decimal.Decimal `json:"-"`

	Checkout *CheckoutParams `json:"checkout,omitempty"`

	SubscriptionCreationConfig
}

type AddAddonRequest struct {
	SubscriptionID                string          `json:"subscription_id" validate:"required"`
	Checkout                      *CheckoutParams `json:"checkout,omitempty"`
	AddAddonToSubscriptionRequest `json:",inline"`
}

func (r *AddAddonRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if err := r.AddAddonToSubscriptionRequest.Validate(); err != nil {
		return err
	}

	if err := r.Checkout.Validate(); err != nil {
		return err
	}

	if r.Checkout != nil {
		if len(r.OverrideLineItems) > 0 {
			return ierr.NewError("override_line_items is not supported with checkout").
				WithHint("Remove checkout to use override_line_items, or remove override_line_items to gate this addon behind payment").
				Mark(ierr.ErrValidation)
		}

		if len(r.LineItemCommitments) > 0 {
			return ierr.NewError("line_item_commitments is not supported with checkout").
				WithHint("Remove checkout to use line_item_commitments, or remove line_item_commitments to gate this addon behind payment").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

type RemoveAddonRequest struct {
	AddonAssociationID string                  `json:"addon_association_id" validate:"required"`
	Reason             string                  `json:"reason,omitempty"`
	ProrationBehavior  types.ProrationBehavior `json:"proration_behavior,omitempty"`
	// EffectiveDate defaults to period end when nil; mid-period with create_prorations issues a wallet credit.
	EffectiveDate *time.Time `json:"effective_date,omitempty"`

	// PreviewOnly quotes the removal without writing anything. Server-set: callers reach it
	// through the preview endpoint, never by sending it.
	PreviewOnly bool `json:"-"`
}

func (r *RemoveAddonRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	if r.ProrationBehavior != "" {
		if err := r.ProrationBehavior.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type UpdateSubscriptionRequest struct {
	Status            types.SubscriptionStatus `json:"status"`
	CancelAt          *time.Time               `json:"cancel_at,omitempty"`
	CancelAtPeriodEnd bool                     `json:"cancel_at_period_end,omitempty"`

	// ParentSubscriptionID sets or clears the parent subscription. Omit to leave unchanged; send "" to clear.
	ParentSubscriptionID *string `json:"parent_subscription_id,omitempty"`
}

// Validate checks UpdateSubscriptionRequest fields for basic structural validity.
func (r *UpdateSubscriptionRequest) Validate() error {

	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	return nil
}

// CancelSubscriptionRequest represents the enhanced cancellation request
type CancelSubscriptionRequest struct {

	// ProrationBehavior controls how proration is handled.
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`

	// CancellationType determines when the cancellation takes effect
	CancellationType types.CancellationType `json:"cancellation_type" validate:"required"`

	// CancelImmediatelyInvoicePolicy controls whether to generate a final invoice on immediate cancellation. Defaults to skip.
	CancelImmediatelyInvoicePolicy types.CancelImmediatelyInvoicePolicy `json:"cancel_immediately_inovice_policy,omitempty"`

	// Reason for cancellation (for audit and business intelligence)
	Reason string `json:"reason,omitempty"`

	// CancelAt is the exact date/time when the subscription should be cancelled.
	// Required for cancellation_type "scheduled_date"; optional for "immediate" (past dates only — backdated cancellation).
	// For "scheduled_date", accepts both future dates (deferred cancellation) and past dates (backdated cancellation).
	// For "immediate", accepts past/current dates only; use "scheduled_date" for future dates.
	CancelAt *time.Time `json:"cancel_at,omitempty"`

	//SuppressWebhook is an internal flag to suppress webhook events during cancellation.
	SuppressWebhook bool `json:"-,omitempty"`

	// SkipProrationWalletCredit skips TopUpWalletForProratedCharge when the same proration is settled on the new subscription's opening invoice (immediate plan change).
	SkipProrationWalletCredit bool `json:"-"`
}

// CancelSubscriptionResponse represents the enhanced cancellation response
type CancelSubscriptionResponse struct {
	// Basic cancellation info
	SubscriptionID   string                   `json:"subscription_id"`
	CancellationType types.CancellationType   `json:"cancellation_type"`
	EffectiveDate    time.Time                `json:"effective_date"`
	Status           types.SubscriptionStatus `json:"status"`
	Reason           string                   `json:"reason,omitempty"`

	// Proration details
	ProrationInvoice  *InvoiceResponse  `json:"proration_invoice,omitempty"`
	ProrationDetails  []ProrationDetail `json:"proration_details"`
	TotalCreditAmount decimal.Decimal   `json:"total_credit_amount" swaggertype:"string"`

	// Response metadata
	Message     string    `json:"message"`
	ProcessedAt time.Time `json:"processed_at"`
}

// ProrationDetail provides line-item level proration information
type ProrationDetail struct {
	LineItemID     string          `json:"line_item_id"`
	PriceID        string          `json:"price_id"`
	PlanName       string          `json:"plan_name,omitempty"`
	OriginalAmount decimal.Decimal `json:"original_amount" swaggertype:"string"`
	CreditAmount   decimal.Decimal `json:"credit_amount" swaggertype:"string"`
	ChargeAmount   decimal.Decimal `json:"charge_amount" swaggertype:"string"`
	ProrationDays  int             `json:"proration_days"`
	Description    string          `json:"description,omitempty"`
}

type TerminateSubscriptionResourcesRequest struct {
	SubscriptionID     string    `json:"subscription_id" binding:"required"`
	EffectiveDate      time.Time `json:"effective_date" binding:"required"`
	CancellationReason string    `json:"cancellation_reason,omitempty"`
}

// Validate validates the terminate subscription resources request
func (r *TerminateSubscriptionResourcesRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// Validate validates the cancellation request
func (r *CancelSubscriptionRequest) Validate() error {
	// Validate cancellation type
	if err := r.CancellationType.Validate(); err != nil {
		return err
	}

	// Set default proration behavior if not provided
	if r.ProrationBehavior == "" {
		r.ProrationBehavior = types.ProrationBehaviorNone
	}
	// Set default cancel immediately invoice policy if not provided or empty (default: skip = do not generate invoice)
	if r.CancelImmediatelyInvoicePolicy == "" {
		r.CancelImmediatelyInvoicePolicy = types.CancelImmediatelyInvoicePolicySkip
	} else if err := r.CancelImmediatelyInvoicePolicy.Validate(); err != nil {
		return err
	}

	// CancellationType-specific validation
	if r.CancellationType == types.CancellationTypeScheduledDate {
		if r.CancelAt == nil {
			return ierr.NewError("cancel_at is required for scheduled_date").
				WithHint("Provide a cancel_at date (past for backdated cancellation, future for scheduled cancellation)").
				Mark(ierr.ErrValidation)
		}
	}

	if r.CancellationType == types.CancellationTypeImmediate && r.CancelAt != nil {
		now := time.Now().UTC()
		if r.CancelAt.After(now) {
			return ierr.NewError("cancel_at cannot be a future date for immediate cancellation").
				WithHint("Use cancellation_type 'scheduled_date' to schedule a future cancellation").
				WithReportableDetails(map[string]interface{}{
					"cancel_at": r.CancelAt.UTC().Format(time.RFC3339),
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// ActivateDraftSubscriptionRequest represents the request to activate a draft subscription
type ActivateDraftSubscriptionRequest struct {
	// start_date is the new start date for the subscription when activating
	StartDate *time.Time `json:"start_date" validate:"required"`
}

// Validate validates the activation request
func (r *ActivateDraftSubscriptionRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.StartDate == nil {
		return ierr.NewError("start_date is required").
			WithHint("Start date is required to activate a draft subscription").
			Mark(ierr.ErrValidation)
	}

	return nil
}

type SubscriptionResponse struct {
	*subscription.Subscription
	Plan     *PlanResponse     `json:"plan"`
	Customer *CustomerResponse `json:"customer"`
	// CouponAssociations are the coupon associations for this subscription
	CouponAssociations []*CouponAssociationResponse `json:"coupon_associations,omitempty"`

	// Phases are the subscription phases for this subscription
	Phases []*SubscriptionPhaseResponse `json:"phases,omitempty"`

	// Credit grants are the credit grants for this subscription
	CreditGrants []*CreditGrantResponse `json:"credit_grants,omitempty"`

	// Latest invoice information for incomplete subscriptions
	LatestInvoice *InvoiceResponse `json:"latest_invoice,omitempty"`

	// Entitlements is populated only when the caller adds "entitlements" to
	// the search filter's expand string. Each entry is a feature with its
	// aggregated entitlement info (same shape as CustomerEntitlementsResponse.Features).
	Entitlements []*AggregatedFeature `json:"entitlements,omitempty"`

	CheckoutSession *CheckoutSessionResponse `json:"checkout_session,omitempty"`
}

// ListSubscriptionsResponse represents the response for listing subscriptions
type ListSubscriptionsResponse = types.ListResponse[*SubscriptionResponse] // @name ListSubscriptionsResponse

// SubscriptionResponseV2 represents the V2 response for a subscription
// with optional expanded fields based on the request expand parameter
type SubscriptionResponseV2 struct {
	*subscription.Subscription

	// Plan is expanded only if "plan" is in expand parameter
	Plan *PlanResponse `json:"plan,omitempty"`

	// Customer is expanded only if "customer" is in expand parameter
	Customer *CustomerResponse `json:"customer,omitempty"`

	// LineItems is expanded only if "subscription_line_items" is in expand parameter
	// Each line item can optionally include expanded price data
	LineItems []*SubscriptionLineItemResponse `json:"line_items,omitempty"`

	// CouponAssociations are included when "coupon_associations" is in expand parameter
	CouponAssociations []*CouponAssociationResponse `json:"coupon_associations,omitempty"`

	// Phases are included when "phases" is in expand parameter
	Phases []*SubscriptionPhaseResponse `json:"phases,omitempty"`

	// CreditGrants are included when "credit_grants" is in expand parameter
	CreditGrants []*CreditGrantResponse `json:"credit_grants,omitempty"`

	// Pauses are included when subscription has pause status
	Pauses []*subscription.SubscriptionPause `json:"pauses,omitempty"`

	// PlanPricesOutOfSync is true when the subscription's synced_price_sequence
	// is behind the plan's current max prices.sequence — i.e. plan-price
	// changes have not yet been reconciled into this subscription's line items.
	PlanPricesOutOfSync bool `json:"plan_prices_out_of_sync"`
}

func (r *CreateSubscriptionRequest) validateCheckoutCompatibility() error {
	if err := r.Checkout.Validate(); err != nil {
		return err
	}

	if r.Checkout == nil {
		return nil
	}

	// A grouped_invoicing child is built by createGroupedInvoicingChildren, which
	// sets parent_subscription_id and passes the parent's checkout down. That
	// combination is rejected by checkoutUnsupportedFields below, so the
	// inheritance check alone is skipped for this type. The status and phase
	// checks still apply: the generated child sets neither, and a request that
	// does set them is as unsupported here as anywhere else.
	isGroupedInvoicingChild := r.SubscriptionType == types.SubscriptionTypeGroupedInvoicing

	if r.SubscriptionStatus != "" {
		return ierr.NewError("subscription_status is not supported with checkout").
			WithHint("Remove subscription_status; a checkout-gated subscription is created as draft and activated once payment succeeds").
			WithReportableDetails(map[string]any{"subscription_status": r.SubscriptionStatus}).
			Mark(ierr.ErrValidation)
	}

	if len(r.Phases) > 0 {
		return ierr.NewError("phases is not supported with checkout").
			WithHint("Remove checkout to use phases, or remove phases to gate this subscription behind payment").
			Mark(ierr.ErrValidation)
	}

	if !isGroupedInvoicingChild && r.Inheritance.checkoutUnsupportedFields() {
		return ierr.NewError("inheritance field is not supported with checkout").
			WithHint("Only grouped_invoicing_children_to_create is supported with checkout; remove the other inheritance fields, or remove checkout").
			Mark(ierr.ErrValidation)
	}

	if r.Inheritance != nil {
		for i, child := range r.Inheritance.GroupedInvoicingChildrenToCreate {
			if len(child.Phases) > 0 {
				return ierr.NewError("phases is not supported with checkout").
					WithHintf("Remove phases from grouped_invoicing_children_to_create[%d], or remove checkout", i).
					Mark(ierr.ErrValidation)
			}
		}
	}

	return nil
}

func (r *CreateSubscriptionRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	if err := r.validateCheckoutCompatibility(); err != nil {
		return err
	}

	if r.OpeningInvoiceAdjustmentAmount != nil && r.OpeningInvoiceAdjustmentAmount.IsNegative() {
		return ierr.NewError("opening invoice adjustment amount must be >= 0").
			Mark(ierr.ErrValidation)
	}

	if r.AutoInvoiceThreshold != nil && r.AutoInvoiceThreshold.IsNegative() {
		return ierr.NewError("auto_invoice_threshold must be zero or greater").
			WithReportableDetails(map[string]any{
				"auto_invoice_threshold": r.AutoInvoiceThreshold.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	if r.IncludePriceIDs != nil {
		seen := make(map[string]struct{}, len(*r.IncludePriceIDs))
		for _, id := range *r.IncludePriceIDs {
			if id == "" {
				return ierr.NewError("include_price_ids contains empty id").
					WithHint("Each price id in include_price_ids must be a non-empty string.").
					Mark(ierr.ErrValidation)
			}
			if _, dup := seen[id]; dup {
				return ierr.NewError("include_price_ids contains duplicate id").
					WithHint("Each price id in include_price_ids must appear at most once.").
					WithReportableDetails(map[string]any{"duplicate_id": id}).
					Mark(ierr.ErrValidation)
			}
			seen[id] = struct{}{}
		}
	}

	// Case- Both are absent
	if r.CustomerID == "" && r.ExternalCustomerID == "" {
		return ierr.NewError("either customer_id or external_customer_id is required").
			WithHint("Please provide either customer_id or external_customer_id").
			Mark(ierr.ErrValidation)
	}

	if r.Inheritance != nil {
		if err := r.Inheritance.Validate(); err != nil {
			return err
		}
	}

	// Validate currency
	if err := types.ValidateCurrencyCode(r.Currency); err != nil {
		return err
	}

	if r.BillingCadence == "" {
		r.BillingCadence = types.BILLING_CADENCE_RECURRING
	}
	if err := r.BillingCadence.Validate(); err != nil {
		return err
	}

	if err := r.BillingPeriod.Validate(); err != nil {
		return err
	}

	if err := r.BillingCycle.Validate(); err != nil {
		return err
	}
	if r.BillingAnchor != nil && r.BillingCycle != types.BillingCycleAnniversary {
		return ierr.NewError("invalid billing_anchor for billing_cycle").
			WithHint("billing_anchor can only be passed when billing_cycle is anniversary").
			WithReportableDetails(map[string]any{
				"billing_cycle":  r.BillingCycle,
				"billing_anchor": r.BillingAnchor,
			}).
			Mark(ierr.ErrValidation)
	}

	// Handle legacy collection method conversion and validation
	if r.CollectionMethod != nil {
		// Handle legacy default_incomplete collection method
		if string(*r.CollectionMethod) == "default_incomplete" {
			// Convert to charge_automatically + allow_incomplete for backward compatibility
			chargeAutomaticallyMethod := types.CollectionMethodChargeAutomatically
			r.CollectionMethod = &chargeAutomaticallyMethod
			if r.PaymentBehavior == nil || *r.PaymentBehavior == types.PaymentBehaviorDefaultIncomplete {
				allowIncomplete := types.PaymentBehaviorAllowIncomplete
				r.PaymentBehavior = &allowIncomplete
			}
		}

		if err := r.CollectionMethod.Validate(); err != nil {
			return err
		}
	}

	// Set defaults for collection method and payment behavior if not provided
	if r.CollectionMethod == nil {
		defaultCollectionMethod := types.CollectionMethodChargeAutomatically
		r.CollectionMethod = &defaultCollectionMethod
	}
	if r.PaymentBehavior == nil {
		defaultPaymentBehavior := types.PaymentBehaviorDefaultActive
		r.PaymentBehavior = &defaultPaymentBehavior
	}

	if r.PaymentTerms != nil {
		if err := (*r.PaymentTerms).Validate(); err != nil {
			return err
		}
	}

	// Set default value to Billing Period Count if not provided
	if r.BillingPeriodCount == 0 {
		r.BillingPeriodCount = 1
	} else if r.BillingPeriodCount < 0 {
		return ierr.NewError("invalid billing period count").
			WithHint("Billing Period must be a valid positive number").
			WithReportableDetails(map[string]interface{}{
				"billing_period_count": r.BillingPeriodCount,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate payment behavior and collection method combination
	if err := r.validatePaymentBehaviorForCollectionMethod(*r.CollectionMethod, *r.PaymentBehavior); err != nil {
		return err
	}

	// Set default start date if not provided
	if r.StartDate == nil {
		now := time.Now().UTC()
		r.StartDate = &now
	}

	if r.EndDate != nil && r.EndDate.Before(*r.StartDate) {
		return ierr.NewError("end_date cannot be before start_date").
			WithHint("End date must be after start date").
			WithReportableDetails(map[string]interface{}{
				"start_date": *r.StartDate,
				"end_date":   *r.EndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.ProrationBehavior != "" {
		if err := r.ProrationBehavior.Validate(); err != nil {
			return err
		}
	} else {
		r.ProrationBehavior = types.ProrationBehaviorNone
	}
	if r.Workflow == nil {
		r.Workflow = lo.ToPtr(types.TemporalSubscriptionChangeWorkflow)
	}

	if r.ProrationBehavior != "" && r.ProrationBehavior == types.ProrationBehaviorCreateProrations {
		if err := r.validateShouldAllowProrationOnStartDate(r); err != nil {
			return err
		}

		// Validate that proration and entitlement overrides are not both enabled
		if len(r.OverrideEntitlements) > 0 {
			return ierr.NewError("cannot enable proration with custom entitlement overrides").
				WithHint("Proration automatically calculates entitlements. Remove override_entitlements or disable proration.").
				WithReportableDetails(map[string]interface{}{
					"proration_behavior":          r.ProrationBehavior,
					"override_entitlements_count": len(r.OverrideEntitlements),
				}).
				Mark(ierr.ErrValidation)
		}
	}

	if r.BillingPeriodCount < 1 {
		return ierr.NewError("billing_period_count must be greater than 0").
			WithHint("Billing period count must be at least 1").
			WithReportableDetails(map[string]interface{}{
				"billing_period_count": r.BillingPeriodCount,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.PlanID == "" {
		return ierr.NewError("plan_id is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	if r.TrialPeriodDays != nil && lo.FromPtr(r.TrialPeriodDays) < 0 {
		return ierr.NewError("trial_period_days must be non-negative").
			WithHint("Use 0 to disable trial or omit to inherit from plan prices").
			WithReportableDetails(map[string]interface{}{
				"trial_period_days": lo.FromPtr(r.TrialPeriodDays),
			}).
			Mark(ierr.ErrValidation)
	}

	if (r.TrialStart != nil) != (r.TrialEnd != nil) {
		return ierr.NewError("trial_start and trial_end must both be set").
			WithHint("Internal use only: provide both bounds or neither").
			Mark(ierr.ErrValidation)
	}

	if r.TrialPeriodDays != nil && lo.FromPtr(r.TrialPeriodDays) > 0 && (r.TrialStart != nil || r.TrialEnd != nil) {
		return ierr.NewError("cannot set trial_period_days together with trial_start/trial_end").
			WithHint("Use trial_period_days for API creates, or trial_start/trial_end for gateway sync only").
			Mark(ierr.ErrValidation)
	}

	if r.StartDate != nil && r.TrialStart != nil && r.TrialStart.After(*r.StartDate) {
		return ierr.NewError("trial_start cannot be after start_date").
			WithHint("Trial start date must be before or equal to start date").
			WithReportableDetails(map[string]interface{}{
				"start_date":  *r.StartDate,
				"trial_start": *r.TrialStart,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.StartDate != nil && r.TrialEnd != nil && r.TrialEnd.Before(*r.StartDate) {
		return ierr.NewError("trial_end cannot be before start_date").
			WithHint("Trial end date must be after or equal to start date").
			WithReportableDetails(map[string]interface{}{
				"start_date": *r.StartDate,
				"trial_end":  *r.TrialEnd,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.TrialStart != nil && r.TrialEnd != nil && r.TrialEnd.Before(*r.TrialStart) {
		return ierr.NewError("trial_end cannot be before trial_start").
			WithHint("trial_end must be on or after trial_start").
			Mark(ierr.ErrValidation)
	}

	// Validate draft subscription constraints
	if r.SubscriptionStatus == types.SubscriptionStatusDraft {
		if len(r.Phases) > 0 {
			return ierr.NewError("phases are not allowed for draft subscriptions").
				WithHint("Draft subscriptions cannot have phases").
				Mark(ierr.ErrValidation)
		}
	}

	if err := r.SubscriptionCreationConfig.Validate(); err != nil {
		return err
	}

	// Validate phases continuity if provided
	if len(r.Phases) > 0 {
		// First, validate each individual phase
		for i, phase := range r.Phases {
			if err := phase.Validate(); err != nil {
				return ierr.WithError(err).
					WithHint(fmt.Sprintf("Phase validation failed at index %d", i)).
					WithReportableDetails(map[string]interface{}{
						"phase_index": i,
					}).
					Mark(ierr.ErrValidation)
			}
		}

		// Validate phase continuity
		for i := 0; i < len(r.Phases); i++ {
			currentPhase := r.Phases[i]
			isLastPhase := i == len(r.Phases)-1

			// All phases except the last must have an end date
			if !isLastPhase && currentPhase.EndDate == nil {
				return ierr.NewError("phase end_date is required for all phases except the last").
					WithHint(fmt.Sprintf("Phase at index %d must have an end_date set", i)).
					WithReportableDetails(map[string]interface{}{
						"phase_index": i,
						"phase_start": currentPhase.StartDate,
					}).
					Mark(ierr.ErrValidation)
			}

			// If not the last phase, validate that current phase's end date equals next phase's start date
			if !isLastPhase {
				nextPhase := r.Phases[i+1]

				// Check if end date of current phase equals start date of next phase
				if !currentPhase.EndDate.Equal(nextPhase.StartDate) {
					return ierr.NewError("phases must be continuous").
						WithHint(fmt.Sprintf("Phase %d end_date must equal phase %d start_date for continuous phases", i, i+1)).
						WithReportableDetails(map[string]interface{}{
							"phase_index":       i,
							"current_phase_end": *currentPhase.EndDate,
							"next_phase_index":  i + 1,
							"next_phase_start":  nextPhase.StartDate,
						}).
						Mark(ierr.ErrValidation)
				}
			}
		}

		// Validate that subscription dates match phase dates when both are provided
		if r.StartDate != nil && !r.StartDate.Equal(r.Phases[0].StartDate) {
			return ierr.NewError("subscription start_date must match first phase start_date when both are provided").
				WithHint("Ensure subscription start_date equals the first phase start_date").
				WithReportableDetails(map[string]interface{}{
					"subscription_start_date": *r.StartDate,
					"first_phase_start_date":  r.Phases[0].StartDate,
				}).
				Mark(ierr.ErrValidation)
		}

		// Check last phase end date validation
		lastPhase := r.Phases[len(r.Phases)-1]
		if r.EndDate != nil && lastPhase.EndDate != nil && !r.EndDate.Equal(*lastPhase.EndDate) {
			return ierr.NewError("subscription end_date must match last phase end_date when both are provided").
				WithHint("Ensure subscription end_date equals the last phase end_date").
				WithReportableDetails(map[string]interface{}{
					"subscription_end_date": *r.EndDate,
					"last_phase_end_date":   *lastPhase.EndDate,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// validatePaymentBehaviorForCollectionMethod validates that payment behavior is compatible with collection method
func (r *CreateSubscriptionRequest) validatePaymentBehaviorForCollectionMethod(collectionMethod types.CollectionMethod, paymentBehavior types.PaymentBehavior) error {
	switch collectionMethod {
	case types.CollectionMethodChargeAutomatically:
		// For charge_automatically, allow_incomplete, error_if_incomplete, and default_active are allowed
		if paymentBehavior != types.PaymentBehaviorAllowIncomplete &&
			paymentBehavior != types.PaymentBehaviorErrorIfIncomplete &&
			paymentBehavior != types.PaymentBehaviorDefaultActive {
			return ierr.NewError("invalid payment behavior for charge_automatically collection method").
				WithHint("Only allow_incomplete, error_if_incomplete, and default_active are supported for charge_automatically collection method").
				WithReportableDetails(map[string]interface{}{
					"collection_method": collectionMethod,
					"payment_behavior":  paymentBehavior,
					"allowed_behaviors": []types.PaymentBehavior{
						types.PaymentBehaviorAllowIncomplete,
						types.PaymentBehaviorErrorIfIncomplete,
						types.PaymentBehaviorDefaultActive,
					},
				}).
				Mark(ierr.ErrValidation)
		}
	case types.CollectionMethodSendInvoice:
		// For send_invoice, only default_active and default_incomplete are allowed
		if paymentBehavior != types.PaymentBehaviorDefaultActive && paymentBehavior != types.PaymentBehaviorDefaultIncomplete {
			return ierr.NewError("invalid payment behavior for send_invoice collection method").
				WithHint("Only default_active and default_incomplete are supported for send_invoice collection method").
				WithReportableDetails(map[string]interface{}{
					"collection_method": collectionMethod,
					"payment_behavior":  paymentBehavior,
					"allowed_behaviors": []types.PaymentBehavior{
						types.PaymentBehaviorDefaultActive,
						types.PaymentBehaviorDefaultIncomplete,
					},
				}).
				Mark(ierr.ErrValidation)
		}

	default:
		return ierr.NewError("unsupported collection method").
			WithHint("Only charge_automatically and send_invoice collection methods are supported").
			WithReportableDetails(map[string]interface{}{
				"collection_method": collectionMethod,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (r *CreateSubscriptionRequest) validateShouldAllowProrationOnStartDate(request *CreateSubscriptionRequest) error {
	// If the start date is before the current date and proration mode is active, return an error
	// This prevents creating subscriptions with backdated start dates that would trigger proration

	if request.Workflow == lo.ToPtr(types.TemporalSubscriptionCreationWorkflow) {
		return nil
	}

	// Compare only the date portions (ignore time) - don't allow before current day
	now := time.Now().UTC()
	startDateOnly := time.Date(request.StartDate.Year(), request.StartDate.Month(), request.StartDate.Day(), 0, 0, 0, 0, time.UTC)
	nowDateOnly := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	if startDateOnly.Before(nowDateOnly) {
		return ierr.NewError("cannot create subscription with past start date when proration is active and workflow is subscription change").
			WithHint("Either set start date to current day or later, or disable proration behavior").
			WithReportableDetails(map[string]interface{}{
				"start_date":   request.StartDate.Format("2006-01-02"),
				"current_date": nowDateOnly.Format("2006-01-02"),
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (r *CreateSubscriptionRequest) ToSubscription(ctx context.Context) *subscription.Subscription {
	// Handle legacy collection method and set defaults
	paymentBehavior := types.PaymentBehaviorDefaultActive
	collectionMethod := types.CollectionMethodChargeAutomatically

	// Handle legacy default_incomplete collection method conversion
	if r.CollectionMethod != nil && string(*r.CollectionMethod) == "default_incomplete" {
		// Convert legacy default_incomplete collection method to charge_automatically + allow_incomplete
		collectionMethod = types.CollectionMethodChargeAutomatically
		paymentBehavior = types.PaymentBehaviorAllowIncomplete
	} else {
		// Normal flow - use provided values or defaults
		if r.CollectionMethod != nil {
			collectionMethod = *r.CollectionMethod
		}
		if r.PaymentBehavior != nil {
			paymentBehavior = *r.PaymentBehavior
		}
	}

	// Set initial status based on payment behavior
	var initialStatus types.SubscriptionStatus
	switch paymentBehavior {
	case types.PaymentBehaviorDefaultActive:
		// Default active behavior - subscription starts as active
		initialStatus = types.SubscriptionStatusActive
	case types.PaymentBehaviorDefaultIncomplete:
		// Default incomplete behavior - subscription starts as incomplete
		initialStatus = types.SubscriptionStatusIncomplete
	case types.PaymentBehaviorAllowIncomplete:
		// Allow incomplete behavior - subscription starts as incomplete (will be updated based on payment result)
		initialStatus = types.SubscriptionStatusIncomplete
	case types.PaymentBehaviorErrorIfIncomplete:
		// Error if incomplete behavior - subscription starts as incomplete (will fail if payment fails)
		initialStatus = types.SubscriptionStatusIncomplete
	default:
		// Fallback to active for unknown behaviors
		initialStatus = types.SubscriptionStatusActive
	}

	if r.Timezone == "" {
		r.Timezone = types.DefaultTimezone
	}

	// Determine subscription start and end dates based on phases
	var startDate time.Time
	var endDate *time.Time
	var billingAnchor time.Time

	if len(r.Phases) > 0 {
		// If phases are provided, use first phase start date as subscription start date
		startDate = r.Phases[0].StartDate
		billingAnchor = startDate

		// Use last phase end date as subscription end date (if last phase has end date)
		lastPhase := r.Phases[len(r.Phases)-1]
		if lastPhase.EndDate != nil {
			endDate = lastPhase.EndDate
		}
	} else {
		// If no phases, use provided start/end dates
		if r.StartDate != nil {
			startDate = *r.StartDate
			billingAnchor = startDate
		}
		endDate = r.EndDate
	}
	if r.BillingAnchor != nil {
		billingAnchor = *r.BillingAnchor
	}

	subscriptionType := r.SubscriptionType
	if subscriptionType == "" {
		subscriptionType = types.SubscriptionTypeStandalone
	}

	var parentSubscriptionID *string
	if r.Inheritance != nil && r.Inheritance.ParentSubscriptionID != "" {
		parentSubscriptionID = lo.ToPtr(r.Inheritance.ParentSubscriptionID)
	}

	var trialStart, trialEnd *time.Time
	if r.TrialStart != nil && r.TrialEnd != nil {
		trialStart, trialEnd = r.TrialStart, r.TrialEnd
	}

	subID := r.ID
	if subID == "" {
		subID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION)
	}

	sub := &subscription.Subscription{
		ID:                 subID,
		CustomerID:         r.CustomerID,
		PlanID:             r.PlanID,
		Currency:           strings.ToLower(r.Currency),
		LookupKey:          r.LookupKey,
		SubscriptionStatus: initialStatus,
		StartDate:          startDate,
		EndDate:            endDate,
		TrialStart:         trialStart,
		TrialEnd:           trialEnd,
		BillingCadence:     r.BillingCadence,
		BillingPeriod:      r.BillingPeriod,
		BillingPeriodCount: r.BillingPeriodCount,
		BillingAnchor:      billingAnchor,
		Metadata:           r.Metadata,
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
		BillingCycle:       r.BillingCycle,
		Timezone:           r.Timezone,
		ProrationBehavior:  r.ProrationBehavior,

		// New payment behavior fields
		PaymentBehavior:        string(paymentBehavior),
		CollectionMethod:       string(collectionMethod),
		GatewayPaymentMethodID: r.GatewayPaymentMethodID,
		ParentSubscriptionID:   parentSubscriptionID,
		PaymentTerms:           r.PaymentTerms,
		SubscriptionType:       subscriptionType,
	}

	// Set commitment amount, duration, and overage factor if provided
	if r.CommitmentAmount != nil {
		sub.CommitmentAmount = r.CommitmentAmount
	}

	if r.CommitmentDuration != nil {
		sub.CommitmentDuration = r.CommitmentDuration
	}

	if r.OverageFactor != nil {
		sub.OverageFactor = r.OverageFactor
	} else {
		sub.OverageFactor = lo.ToPtr(decimal.NewFromInt(1)) // Default value
	}

	if r.AutoInvoiceThreshold != nil {
		sub.AutoInvoiceThreshold = r.AutoInvoiceThreshold
	}

	return sub
}

// SubscriptionLineItemRequest represents the request to create a subscription line item
type SubscriptionLineItemRequest struct {
	PriceID     string            `json:"price_id" validate:"required"`
	Quantity    decimal.Decimal   `json:"quantity" validate:"required" swaggertype:"string"`
	DisplayName string            `json:"display_name,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`

	// Commitment fields
	CommitmentAmount        *decimal.Decimal     `json:"commitment_amount,omitempty"`
	CommitmentQuantity      *decimal.Decimal     `json:"commitment_quantity,omitempty"`
	CommitmentType          types.CommitmentType `json:"commitment_type,omitempty"`
	CommitmentOverageFactor *decimal.Decimal     `json:"commitment_overage_factor,omitempty"`
	CommitmentTrueUpEnabled bool                 `json:"commitment_true_up_enabled,omitempty"`
	CommitmentWindowed      bool                 `json:"commitment_windowed,omitempty"`
	CommitmentDuration      *types.BillingPeriod `json:"commitment_duration,omitempty"`
	// CommitmentTimeBuckets - if provided, applies commitment only in specified time buckets
	CommitmentTimeBuckets types.TimeOfDayBuckets `json:"commitment_time_buckets,omitempty"`
}

// SubscriptionLineItemResponse represents the response for a subscription line item
type SubscriptionLineItemResponse struct {
	*subscription.SubscriptionLineItem
	Price *PriceResponse `json:"price,omitempty"`
}

// ListSubscriptionLineItemsResponse represents the response for listing subscription line items
type ListSubscriptionLineItemsResponse = types.ListResponse[*SubscriptionLineItemResponse] // @name ListSubscriptionLineItemsResponse

// OverrideLineItemRequest represents a price override for a specific subscription
type OverrideLineItemRequest struct {
	// PriceID references the plan price to override
	PriceID string `json:"price_id" validate:"required"`

	// Quantity for this line item (optional)
	Quantity *decimal.Decimal `json:"quantity,omitempty" swaggertype:"string"`

	BillingModel types.BillingModel `json:"billing_model,omitempty"`

	// Amount is the new price amount that overrides the original price (optional)
	Amount *decimal.Decimal `json:"amount,omitempty" swaggertype:"string"`

	// TierMode determines how to calculate the price for a given quantity
	TierMode types.BillingTier `json:"tier_mode,omitempty"`

	// BucketSize overrides the windowing used to turn this meter's usage into
	// billable units for this subscription. See CreatePriceRequest.BucketSize.
	BucketSize types.WindowSize `json:"bucket_size,omitempty"`

	// Tiers determines the pricing tiers for this line item
	Tiers []CreatePriceTier `json:"tiers,omitempty"`

	// TransformQuantity determines how to transform the quantity for this line item
	TransformQuantity *price.TransformQuantity `json:"transform_quantity,omitempty"`

	// PriceUnitAmount is the amount of the price unit (for CUSTOM type, FLAT_FEE/PACKAGE billing models)
	PriceUnitAmount *decimal.Decimal `json:"price_unit_amount,omitempty" swaggertype:"string"`

	// PriceUnitTiers are the tiers for the price unit (for CUSTOM type, TIERED billing model)
	PriceUnitTiers []CreatePriceTier `json:"price_unit_tiers,omitempty"`
}

// OverrideEntitlementRequest allows overriding entitlement values for a subscription
type OverrideEntitlementRequest struct {
	// EntitlementID references the plan/addon entitlement to override
	EntitlementID string `json:"entitlement_id" validate:"required"`

	// UsageLimit is the new usage limit (only these 3 fields can be overridden)
	// For metered features, nil means unlimited usage
	UsageLimit *int64 `json:"usage_limit"`

	// IsEnabled determines if the entitlement is enabled or disabled
	IsEnabled *bool `json:"is_enabled,omitempty"`

	// StaticValue is the static value for static features
	StaticValue *string `json:"static_value,omitempty"`

	// ConfigValue is the config value for config features
	ConfigValue map[string]interface{} `json:"config_value,omitempty"`
}

// Validate validates the entitlement override request
func (r *OverrideEntitlementRequest) Validate() error {
	if r.EntitlementID == "" {
		return ierr.NewError("entitlement_id is required").
			WithHint("Please provide the entitlement ID to override").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the override line item request with additional context
// This method should be called after basic validation to check business rules
func (r *OverrideLineItemRequest) Validate(
	priceMap map[string]*PriceResponse,
	lineItemsByPriceID map[string]*subscription.SubscriptionLineItem,
	EntityId string,
) error {
	if r.PriceID == "" {
		return ierr.NewError("price_id is required for override line items").
			WithHint("Price ID must be specified for price overrides").
			Mark(ierr.ErrValidation)
	}

	// At least one override field must be provided
	if r.Quantity == nil && r.Amount == nil && r.BillingModel == "" && r.TierMode == "" && r.BucketSize == "" && len(r.Tiers) == 0 && r.TransformQuantity == nil && r.PriceUnitAmount == nil && len(r.PriceUnitTiers) == 0 {
		return ierr.NewError("at least one override field must be provided").
			WithHint("Specify at least one of: quantity, amount, billing_model, tier_mode, bucket_size, tiers, transform_quantity, price_unit_amount, or price_unit_tiers for price override").
			Mark(ierr.ErrValidation)
	}

	// Mutual exclusivity: cannot provide both FIAT fields and custom price unit fields
	if (r.Amount != nil || len(r.Tiers) > 0) && (r.PriceUnitAmount != nil || len(r.PriceUnitTiers) > 0) {
		return ierr.NewError("cannot provide both FIAT fields and custom price unit fields").
			WithHint("Cannot use both FIAT fields (amount, tiers) and custom price unit fields (price_unit_amount, price_unit_tiers) in the same override request").
			Mark(ierr.ErrValidation)
	}

	// Validate amount if provided
	if r.Amount != nil && r.Amount.IsNegative() {
		return ierr.NewError("invalid override line item: amount must be non-negative").
			WithHint("Override amount cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"amount": r.Amount.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate price_unit_amount if provided
	if r.PriceUnitAmount != nil && r.PriceUnitAmount.IsNegative() {
		return ierr.NewError("price_unit_amount must be non-negative").
			WithHint("Override price unit amount cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"price_unit_amount": r.PriceUnitAmount.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate quantity if provided
	if err := price.ValidateQuantityNonNegative(r.Quantity); err != nil {
		return err
	}

	// Get original price - it must be available for validation
	if priceMap == nil {
		return ierr.NewError("price map is required for validation").
			WithHint("Price map must be provided to validate override requests").
			WithReportableDetails(map[string]interface{}{
				"price_id": r.PriceID,
			}).
			Mark(ierr.ErrValidation)
	}

	originalPrice, exists := priceMap[r.PriceID]
	if !exists || originalPrice == nil {
		return ierr.NewError("price not found in plan").
			WithHint("Could not find the original price for the specified price ID. The price must exist in the plan to create an override").
			WithReportableDetails(map[string]interface{}{
				"price_id": r.PriceID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Quantity can only be set for fixed prices, not usage-based prices
	if r.Quantity != nil && originalPrice.Type == types.PRICE_TYPE_USAGE && r.Quantity.GreaterThan(decimal.Zero) {
		return ierr.NewError("quantity cannot be set for usage-based prices").
			WithHint("Quantity overrides are only allowed for fixed prices. Usage-based prices track quantity automatically from meter events").
			WithReportableDetails(map[string]interface{}{
				"price_id":   r.PriceID,
				"price_type": originalPrice.Type,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate billing model if provided
	if r.BillingModel != "" {
		if err := r.BillingModel.Validate(); err != nil {
			return err
		}

		// Billing model specific validations
		switch r.BillingModel {
		case types.BILLING_MODEL_TIERED:
			// Validate tiers based on original price's price unit type
			// Default to FIAT if PriceUnitType is empty (for backward compatibility)
			priceUnitType := originalPrice.PriceUnitType
			if priceUnitType == "" {
				priceUnitType = types.PRICE_UNIT_TYPE_FIAT
			}

			switch priceUnitType {
			case types.PRICE_UNIT_TYPE_CUSTOM:
				// For CUSTOM price unit, require price_unit_tiers
				if len(r.PriceUnitTiers) == 0 {
					return ierr.NewError("invalid override line item: price_unit_tiers are required when billing model is TIERED and using custom pricing unit").
						WithHint("Price unit tiers are required to set up tiered pricing with custom pricing units").
						WithReportableDetails(map[string]interface{}{
							"price_id":        r.PriceID,
							"price_unit_type": originalPrice.PriceUnitType,
						}).
						Mark(ierr.ErrValidation)
				}
			case types.PRICE_UNIT_TYPE_FIAT:
				// For FIAT price unit, require tiers
				if len(r.Tiers) == 0 {
					return ierr.NewError("invalid override line item: tiers are required when billing model is TIERED").
						WithHint("Price tiers are required to set up tiered pricing").
						WithReportableDetails(map[string]interface{}{
							"price_id":        r.PriceID,
							"price_unit_type": originalPrice.PriceUnitType,
						}).
						Mark(ierr.ErrValidation)
				}
			default:
				// Invalid or unknown price unit type (not empty, not FIAT, not CUSTOM)
				return ierr.NewError("invalid override line item: invalid or unknown price unit type for tiered billing model").
					WithHint("Price unit type must be either FIAT or CUSTOM for tiered billing model").
					WithReportableDetails(map[string]interface{}{
						"price_id":        r.PriceID,
						"price_unit_type": originalPrice.PriceUnitType,
					}).
					Mark(ierr.ErrValidation)
			}

			// Validate tier mode if provided
			if r.TierMode != "" {
				if err := r.TierMode.Validate(); err != nil {
					return err
				}
			}

			// Validate tiers if provided (for FIAT)
			if len(r.Tiers) > 0 {
				for i, tier := range r.Tiers {
					// Validate tier unit amount is not negative (allows zero)
					if tier.UnitAmount.IsNegative() {
						return ierr.NewError("tier unit amount cannot be negative").
							WithHint("Tier unit amount cannot be negative").
							WithReportableDetails(map[string]interface{}{
								"tier_index":  i,
								"unit_amount": tier.UnitAmount.String(),
							}).
							Mark(ierr.ErrValidation)
					}

					// Validate flat amount if provided
					if tier.FlatAmount != nil && tier.FlatAmount.IsNegative() {
						return ierr.NewError("tier flat amount cannot be negative").
							WithHint("Tier flat amount cannot be negative").
							WithReportableDetails(map[string]interface{}{
								"tier_index":  i,
								"flat_amount": tier.FlatAmount.String(),
							}).
							Mark(ierr.ErrValidation)
					}
				}
			}

			// Validate price_unit_tiers if provided (for CUSTOM)
			if len(r.PriceUnitTiers) > 0 {
				for i, tier := range r.PriceUnitTiers {
					// Validate tier unit amount is not negative (allows zero)
					if tier.UnitAmount.IsNegative() {
						return ierr.NewError("price unit tier unit amount cannot be negative").
							WithHint("Price unit tier unit amount cannot be negative").
							WithReportableDetails(map[string]interface{}{
								"tier_index":  i,
								"unit_amount": tier.UnitAmount.String(),
							}).
							Mark(ierr.ErrValidation)
					}

					// Validate flat amount if provided
					if tier.FlatAmount != nil && tier.FlatAmount.IsNegative() {
						return ierr.NewError("price unit tier flat amount cannot be negative").
							WithHint("Price unit tier flat amount cannot be negative").
							WithReportableDetails(map[string]interface{}{
								"tier_index":  i,
								"flat_amount": tier.FlatAmount.String(),
							}).
							Mark(ierr.ErrValidation)
					}
				}
			}

		case types.BILLING_MODEL_PACKAGE:
			// TransformQuantity is always required for PACKAGE
			if r.TransformQuantity == nil {
				return ierr.NewError("transform_quantity is required when billing model is PACKAGE").
					WithHint("Please provide the number of units to set up package pricing override").
					Mark(ierr.ErrValidation)
			}

			if err := r.TransformQuantity.Validate(); err != nil {
				return err
			}

			// Validate amount based on original price's price unit type
			switch originalPrice.PriceUnitType {
			case types.PRICE_UNIT_TYPE_CUSTOM:
				// For CUSTOM price unit, require price_unit_amount
				if r.PriceUnitAmount == nil {
					return ierr.NewError("price_unit_amount is required when billing model is PACKAGE and using custom pricing unit").
						WithHint("Price unit amount is required to set up package pricing with custom pricing units").
						WithReportableDetails(map[string]interface{}{
							"price_id":        r.PriceID,
							"price_unit_type": originalPrice.PriceUnitType,
						}).
						Mark(ierr.ErrValidation)
				}
			case types.PRICE_UNIT_TYPE_FIAT:
				// For FIAT price unit, require amount
				if r.Amount == nil {
					return ierr.NewError("amount is required when billing model is PACKAGE and using fiat pricing unit").
						WithHint("Amount is required to set up package pricing with fiat pricing units").
						WithReportableDetails(map[string]interface{}{
							"price_id":        r.PriceID,
							"price_unit_type": originalPrice.PriceUnitType,
						}).
						Mark(ierr.ErrValidation)
				}
			}

		case types.BILLING_MODEL_FLAT_FEE:
			// Validate amount based on original price's price unit type
			switch originalPrice.PriceUnitType {
			case types.PRICE_UNIT_TYPE_CUSTOM:
				// For CUSTOM price unit, require price_unit_amount
				if r.PriceUnitAmount == nil {
					return ierr.NewError("price_unit_amount is required when billing model is FLAT_FEE and using custom pricing unit").
						WithHint("Price unit amount is required to set up flat fee pricing with custom pricing units").
						WithReportableDetails(map[string]interface{}{
							"price_id":        r.PriceID,
							"price_unit_type": originalPrice.PriceUnitType,
						}).
						Mark(ierr.ErrValidation)
				}
			case types.PRICE_UNIT_TYPE_FIAT:
				// For FIAT price unit, require amount
				if r.Amount == nil {
					return ierr.NewError("amount is required when billing model is FLAT_FEE and using fiat pricing unit").
						WithHint("Amount is required to set up flat fee pricing with fiat pricing units").
						WithReportableDetails(map[string]interface{}{
							"price_id":        r.PriceID,
							"price_unit_type": originalPrice.PriceUnitType,
						}).
						Mark(ierr.ErrValidation)
				}
			}
		}
	}

	// Validate tier mode if provided (independent of billing model)
	if r.TierMode != "" {
		if err := r.TierMode.Validate(); err != nil {
			return err
		}
	}

	// Validate transform quantity if provided (independent of billing model)
	if r.TransformQuantity != nil {
		if err := r.TransformQuantity.Validate(); err != nil {
			return err
		}
	}

	// Additional context validation
	if lineItemsByPriceID != nil && EntityId != "" {
		// Validate that the line item exists for this price
		_, exists := lineItemsByPriceID[r.PriceID]
		if !exists {
			return ierr.NewError("line item not found for price").
				WithHint("Could not find line item for the specified price").
				WithReportableDetails(map[string]interface{}{
					"price_id": r.PriceID,
				}).
				Mark(ierr.ErrNotFound)
		}
	}

	// Validate that override fields match the original price's PriceUnitType
	switch originalPrice.PriceUnitType {
	case types.PRICE_UNIT_TYPE_FIAT:
		if r.PriceUnitAmount != nil || len(r.PriceUnitTiers) > 0 {
			return ierr.NewError("cannot use custom price unit fields on a FIAT price").
				WithHint("Price unit type cannot be changed during override. Use FIAT fields (amount or tiers) instead.").
				WithReportableDetails(map[string]interface{}{
					"price_id":        r.PriceID,
					"price_unit_type": originalPrice.PriceUnitType,
				}).
				Mark(ierr.ErrValidation)
		}
	case types.PRICE_UNIT_TYPE_CUSTOM:
		if r.Amount != nil || len(r.Tiers) > 0 {
			return ierr.NewError("cannot use FIAT fields on a CUSTOM price").
				WithHint("Price unit type cannot be changed during override. Use price_unit_amount or price_unit_tiers instead.").
				WithReportableDetails(map[string]interface{}{
					"price_id":        r.PriceID,
					"price_unit_type": originalPrice.PriceUnitType,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// ToSubscriptionLineItem converts a request to a domain subscription line item
func (r *SubscriptionLineItemRequest) ToSubscriptionLineItem(ctx context.Context) *subscription.SubscriptionLineItem {
	lineItem := &subscription.SubscriptionLineItem{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		PriceID:       r.PriceID,
		Quantity:      r.Quantity,
		DisplayName:   r.DisplayName,
		Metadata:      r.Metadata,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	// Set commitment fields if provided
	if r.CommitmentAmount != nil {
		lineItem.CommitmentAmount = r.CommitmentAmount
	}
	if r.CommitmentQuantity != nil {
		lineItem.CommitmentQuantity = r.CommitmentQuantity
	}
	if r.CommitmentType != "" {
		lineItem.CommitmentType = r.CommitmentType
	}
	if r.CommitmentOverageFactor != nil {
		lineItem.CommitmentOverageFactor = r.CommitmentOverageFactor
	}
	lineItem.CommitmentTrueUpEnabled = r.CommitmentTrueUpEnabled
	lineItem.CommitmentWindowed = r.CommitmentWindowed
	if r.CommitmentDuration != nil {
		lineItem.CommitmentDuration = r.CommitmentDuration
	}
	if len(r.CommitmentTimeBuckets) > 0 {
		lineItem.CommitmentTimeBuckets = r.CommitmentTimeBuckets
	}

	return lineItem
}

type GetUsageBySubscriptionRequest struct {
	SubscriptionID string    `json:"subscription_id" binding:"required" example:"123"`
	StartTime      time.Time `json:"start_time" example:"2024-03-13T00:00:00Z"`
	EndTime        time.Time `json:"end_time" example:"2024-03-20T00:00:00Z"`
	LifetimeUsage  bool      `json:"lifetime_usage" example:"false"`
	// Source indicates the caller context. When "invoice_creation", ClickHouse queries use FINAL.
	// Optional; omit for default (no FINAL).
	Source string `json:"-"`
}

type GetUsageBySubscriptionResponse struct {
	Amount             float64                              `json:"amount"`
	Currency           string                               `json:"currency"`
	DisplayAmount      string                               `json:"display_amount"`
	StartTime          time.Time                            `json:"start_time"`
	EndTime            time.Time                            `json:"end_time"`
	Charges            []*SubscriptionUsageByMetersResponse `json:"charges"`
	CommitmentAmount   float64                              `json:"commitment_amount,omitempty"`
	OverageFactor      float64                              `json:"overage_factor,omitempty"`
	CommitmentUtilized float64                              `json:"commitment_utilized,omitempty"` // Amount of commitment used
	OverageAmount      float64                              `json:"overage_amount,omitempty"`      // Amount charged at overage rate
	HasOverage         bool                                 `json:"has_overage"`                   // Whether any usage exceeded commitment
}

type SubscriptionUsageByMetersResponse struct {
	SubscriptionLineItemID string             `json:"subscription_line_item_id,omitempty"` // For feature_usage: direct match by sub_line_item_id
	Amount                 float64            `json:"amount"`
	Currency               string             `json:"currency"`
	DisplayAmount          string             `json:"display_amount"`
	Quantity               float64            `json:"quantity"`
	FilterValues           price.JSONBFilters `json:"filter_values"`
	MeterID                string             `json:"meter_id"`
	MeterDisplayName       string             `json:"meter_display_name"`
	Price                  *price.Price       `json:"price"`
	IsOverage              bool               `json:"is_overage"`               // Whether this charge is at overage rate
	OverageFactor          float64            `json:"overage_factor,omitempty"` // Factor applied to this charge if in overage

	// BucketedUsageResult holds per-bucket usage data for bucketed meters (MAX/SUM with bucket_size).
	// Populated by GetMeterUsageBySubscription so CalculateMeterUsageCharges doesn't re-query ClickHouse.
	BucketedUsageResult *events.AggregationResult `json:"-"`

	// Exact carriers for billing math. JSON amount/quantity remain float64 for API
	// compatibility; invoice calculation must use AmountDecimal/QuantityDecimal so
	// values never round-trip through float64 (can flip currency rounding at boundaries).
	amountExact      decimal.Decimal
	hasAmountExact   bool
	quantityExact    decimal.Decimal
	hasQuantityExact bool
}

// SetAmountDecimal stores the billing amount exactly and mirrors float64 for the API.
func (c *SubscriptionUsageByMetersResponse) SetAmountDecimal(amount decimal.Decimal) {
	if c == nil {
		return
	}
	c.amountExact = amount
	c.hasAmountExact = true
	c.Amount = amount.InexactFloat64()
}

// SetAmountWithCurrencyPrecision rounds to currency precision, then SetAmountDecimal.
func (c *SubscriptionUsageByMetersResponse) SetAmountWithCurrencyPrecision(amount decimal.Decimal, currency string) {
	if c == nil {
		return
	}
	c.SetAmountDecimal(types.RoundToCurrencyPrecision(amount, currency))
}

// AmountDecimal returns the exact billing amount when set; otherwise reconstructs from the API float.
func (c *SubscriptionUsageByMetersResponse) AmountDecimal() decimal.Decimal {
	if c == nil {
		return decimal.Zero
	}
	if c.hasAmountExact {
		return c.amountExact
	}
	return decimal.NewFromFloat(c.Amount)
}

// SetQuantityDecimal stores the billing quantity exactly and mirrors float64 for the API.
func (c *SubscriptionUsageByMetersResponse) SetQuantityDecimal(quantity decimal.Decimal) {
	if c == nil {
		return
	}
	c.quantityExact = quantity
	c.hasQuantityExact = true
	c.Quantity = quantity.InexactFloat64()
}

// QuantityDecimal returns the exact billing quantity when set; otherwise reconstructs from the API float.
func (c *SubscriptionUsageByMetersResponse) QuantityDecimal() decimal.Decimal {
	if c == nil {
		return decimal.Zero
	}
	if c.hasQuantityExact {
		return c.quantityExact
	}
	return decimal.NewFromFloat(c.Quantity)
}

type SubscriptionUpdatePeriodResponse struct {
	TotalSuccess int                                     `json:"total_success"`
	TotalFailed  int                                     `json:"total_failed"`
	Items        []*SubscriptionUpdatePeriodResponseItem `json:"items"`
	StartAt      time.Time                               `json:"start_at"`
}

type SubscriptionUpdatePeriodResponseItem struct {
	SubscriptionID string    `json:"subscription_id"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
	Success        bool      `json:"success"`
	Error          string    `json:"error"`
}

// AutoInvoiceThresholdBillingResult is the result of a single ProcessAutoInvoiceThresholdBilling run.
type AutoInvoiceThresholdBillingResult struct {
	TotalChecked  int                                      `json:"total_checked"`
	TotalInvoiced int                                      `json:"total_invoiced"`
	TotalSkipped  int                                      `json:"total_skipped"`
	TotalFailed   int                                      `json:"total_failed"`
	Items         []*AutoInvoiceThresholdBillingResultItem `json:"items,omitempty"`
}

// AutoInvoiceThresholdBillingResultItem is the per-subscription outcome for auto invoice threshold billing.
type AutoInvoiceThresholdBillingResultItem struct {
	SubscriptionID string `json:"subscription_id"`
	Invoiced       bool   `json:"invoiced"`
	InvoiceID      string `json:"invoice_id,omitempty"`
	Error          string `json:"error,omitempty"`
}

// GetUpcomingCreditGrantApplicationsRequest represents the request to get upcoming credit grant applications
type GetUpcomingCreditGrantApplicationsRequest struct {
	// SubscriptionIDs is a list of subscription IDs to get upcoming credit grant applications for
	// This allows querying multiple subscriptions at once, useful for customer-level queries
	SubscriptionIDs []string `json:"subscription_ids" binding:"required,min=1" validate:"required,min=1"`
}

// Validate validates the GetUpcomingCreditGrantApplicationsRequest
func (r *GetUpcomingCreditGrantApplicationsRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if len(r.SubscriptionIDs) == 0 {
		return ierr.NewError("subscription_ids is required").
			WithHint("Please provide at least one subscription ID").
			Mark(ierr.ErrValidation)
	}

	// Validate that all subscription IDs are non-empty
	for i, id := range r.SubscriptionIDs {
		if id == "" {
			return ierr.NewError("subscription_ids cannot contain empty values").
				WithHint(fmt.Sprintf("Subscription ID at index %d is empty", i)).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

type Period struct {
	Start time.Time
	End   time.Time
}

func (p *Period) Validate() error {
	if p.Start.IsZero() {
		return ierr.NewError("start_date is required").
			WithHint("Please provide a start date").
			Mark(ierr.ErrValidation)
	}
	if p.End.IsZero() {
		return ierr.NewError("end_date is required").
			WithHint("Please provide an end date").
			Mark(ierr.ErrValidation)
	}
	if p.Start.After(p.End) {
		return ierr.NewError("start_date cannot be after end_date").
			WithHint("Ensure the period start date is on or before the end date").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// TriggerSubscriptionWorkflowRequest represents the request to trigger a subscription billing workflow
type TriggerSubscriptionWorkflowRequest struct {
	SubscriptionID string `json:"subscription_id" validate:"required"`
}

// Validate validates the TriggerSubscriptionWorkflowRequest
func (r *TriggerSubscriptionWorkflowRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	return nil
}

// TriggerSubscriptionWorkflowResponse represents the response for triggering a subscription billing workflow
type TriggerSubscriptionWorkflowResponse struct {
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
	Message    string `json:"message"`
}
