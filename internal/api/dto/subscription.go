package dto

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// SubscriptionPhaseCreateRequest represents the request to create a subscription phase
type SubscriptionPhaseCreateRequest struct {
	StartDate time.Time  `json:"start_date" validate:"required"`
	EndDate   *time.Time `json:"end_date,omitempty"`

	// Coupons represents subscription-level coupons to be applied to this phase
	Coupons []string `json:"coupons,omitempty"`

	// LineItemCoupons represents line item-level coupons (map of line_item_id to coupon IDs)
	LineItemCoupons map[string][]string `json:"line_item_coupons,omitempty"`

	// OverrideLineItems allows customizing specific prices for this phase
	// If not provided, phase will use the same line items as the subscription (plan prices)
	OverrideLineItems []OverrideLineItemRequest `json:"override_line_items,omitempty" validate:"omitempty,dive"`

	Metadata map[string]string `json:"metadata,omitempty"`
}

// Validate validates the SubscriptionPhaseCreateRequest
func (r *SubscriptionPhaseCreateRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.EndDate != nil && r.EndDate.Before(r.StartDate) {
		return ierr.NewError("end_date cannot be before start_date").
			WithHint("Ensure the phase end date is on or after the start date").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ToSubscriptionPhase converts the request to a domain SubscriptionPhase

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

// LineItemCommitmentConfig represents commitment configuration for a line item
type LineItemCommitmentConfig struct {
	// CommitmentAmount is the minimum amount committed for this line item
	CommitmentAmount *decimal.Decimal `json:"commitment_amount,omitempty"`

	// CommitmentQuantity is the minimum quantity committed for this line item
	CommitmentQuantity *decimal.Decimal `json:"commitment_quantity,omitempty"`

	// CommitmentType specifies whether commitment is based on amount or quantity
	CommitmentType types.CommitmentType `json:"commitment_type,omitempty"`

	// OverageFactor is a multiplier applied to usage beyond the commitment
	OverageFactor *decimal.Decimal `json:"overage_factor,omitempty"`

	// EnableTrueUp determines if true-up fee should be applied when usage is below commitment
	EnableTrueUp *bool `json:"enable_true_up,omitempty"`

	// IsWindowCommitment determines if commitment is applied per window (e.g., per day) rather than per billing period
	IsWindowCommitment *bool `json:"is_window_commitment,omitempty"`
}

// Validate validates the line item commitment configuration
func (c *LineItemCommitmentConfig) Validate() error {
	hasAmountCommitment := c.CommitmentAmount != nil && c.CommitmentAmount.GreaterThan(decimal.Zero)
	hasQuantityCommitment := c.CommitmentQuantity != nil && c.CommitmentQuantity.GreaterThan(decimal.Zero)
	hasCommitment := hasAmountCommitment || hasQuantityCommitment

	if !hasCommitment {
		// No commitment configured, nothing to validate
		return nil
	}

	// Rule 1: Cannot set both commitment_amount and commitment_quantity
	if hasAmountCommitment && hasQuantityCommitment {
		return ierr.NewError("cannot set both commitment_amount and commitment_quantity").
			WithHint("Specify either commitment_amount or commitment_quantity, not both").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount":   c.CommitmentAmount,
				"commitment_quantity": c.CommitmentQuantity,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 2: Commitment type must be valid and match the provided field
	if c.CommitmentType != "" && !c.CommitmentType.Validate() {
		return ierr.NewError("invalid commitment_type").
			WithHint("Commitment type must be either 'amount' or 'quantity'").
			WithReportableDetails(map[string]interface{}{
				"commitment_type": c.CommitmentType,
			}).
			Mark(ierr.ErrValidation)
	}

	// Auto-set commitment type if not provided
	if c.CommitmentType == "" {
		if hasAmountCommitment {
			c.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
		} else if hasQuantityCommitment {
			c.CommitmentType = types.COMMITMENT_TYPE_QUANTITY
		}
	} else {
		// Validate commitment type matches the provided field
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

	// Rule 3: Overage factor is required and must be greater than 1.0 when commitment is set
	if c.OverageFactor == nil {
		return ierr.NewError("overage_factor is required when commitment is set").
			WithHint("Specify an overage_factor greater than 1.0").
			Mark(ierr.ErrValidation)
	}

	if c.OverageFactor.LessThanOrEqual(decimal.NewFromInt(1)) {
		return ierr.NewError("overage_factor must be greater than 1.0").
			WithHint("Overage factor determines the multiplier for usage beyond commitment").
			WithReportableDetails(map[string]interface{}{
				"overage_factor": c.OverageFactor,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 4: Validate commitment values are positive
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

	return nil
}

// SubscriptionCouponRequest represents a coupon to be applied to a subscription
// If LineItemID is provided, the coupon is applied to that specific line item
// If LineItemID is omitted, the coupon is applied at the subscription level
type SubscriptionCouponRequest struct {
	CouponID            string     `json:"coupon_id" validate:"required"`
	StartDate           time.Time  `json:"start_date" validate:"required"`
	EndDate             *time.Time `json:"end_date,omitempty"`
	LineItemID          *string    `json:"line_item_id,omitempty"`
	SubscriptionPhaseID *string    `json:"subscription_phase_id,omitempty"`
}

// Validate validates the SubscriptionCouponRequest
func (r *SubscriptionCouponRequest) Validate() error {

	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Validate date range if EndDate is provided
	if r.EndDate != nil {
		if r.EndDate.Before(r.StartDate) {
			return ierr.NewError("end_date cannot be before start_date").
				WithHint("Ensure the coupon end date is on or after the start date").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

type CreateSubscriptionRequest struct {

	// customer_id is the flexprice customer id
	// and it is prioritized over external_customer_id in case both are provided.
	CustomerID string `json:"customer_id"`

	// external_customer_id is the customer id in your DB
	// and must be same as what you provided as external_id while creating the customer in flexprice.
	ExternalCustomerID string `json:"external_customer_id"`

	// invoicing_customer_id is the customer ID to use for invoicing
	// This can differ from the subscription customer (e.g., parent company invoicing for child company)
	// This field is set internally based on InvoiceBillingConfig and is not exposed in the API
	InvoicingCustomerID *string `json:"-"`

	// invoice_billing determines which customer should receive invoices for a subscription
	// "invoice_to_parent" - Invoices are sent to the parent customer
	// "invoice_to_self" - Invoices are sent to the subscription's customer
	InvoiceBilling *types.InvoiceBilling `json:"invoice_billing,omitempty"`

	PlanID             string               `json:"plan_id" validate:"required"`
	Currency           string               `json:"currency" validate:"required,len=3"`
	LookupKey          string               `json:"lookup_key"`
	StartDate          *time.Time           `json:"start_date,omitempty"`
	EndDate            *time.Time           `json:"end_date,omitempty"`
	TrialStart         *time.Time           `json:"trial_start,omitempty"`
	TrialEnd           *time.Time           `json:"trial_end,omitempty"`
	BillingCadence     types.BillingCadence `json:"billing_cadence" validate:"required"`
	BillingPeriod      types.BillingPeriod  `json:"billing_period" validate:"required"`
	BillingPeriodCount int                  `json:"billing_period_count" default:"1"`
	Metadata           map[string]string    `json:"metadata,omitempty"`

	// BillingCycle is the cycle of the billing anchor.
	// This is used to determine the billing date for the subscription (i.e set the billing anchor)
	// If not set, the default value is anniversary. Possible values are anniversary and calendar.
	// Anniversary billing means the billing anchor will be the start date of the subscription.
	// Calendar billing means the billing anchor will be the appropriate date based on the billing period.
	// For example, if the billing period is month and the start date is 2025-04-15 then in case of
	// calendar billing the billing anchor will be 2025-05-01 vs 2025-04-15 for anniversary billing.
	BillingCycle types.BillingCycle `json:"billing_cycle"`

	// Credit grants to be applied when subscription is created
	CreditGrants []CreateCreditGrantRequest `json:"credit_grants,omitempty"`

	// CommitmentAmount is the minimum amount a customer commits to paying for a billing period
	CommitmentAmount *decimal.Decimal `json:"commitment_amount,omitempty" swaggertype:"string"`

	// OverageFactor is a multiplier applied to usage beyond the commitment amount
	OverageFactor *decimal.Decimal `json:"overage_factor,omitempty" swaggertype:"string"`

	// tax_rate_overrides is the tax rate overrides	to be applied to the subscription
	TaxRateOverrides []*TaxRateOverride `json:"tax_rate_overrides,omitempty"`

	Coupons []string `json:"coupons,omitempty"`

	LineItemCoupons map[string][]string `json:"line_item_coupons,omitempty"`

	// LineItemCommitments allows setting commitment configuration per line item (keyed by price_id)
	LineItemCommitments map[string]*LineItemCommitmentConfig `json:"line_item_commitments,omitempty" validate:"omitempty,dive"`

	// OverrideLineItems allows customizing specific prices for this subscription
	OverrideLineItems []OverrideLineItemRequest `json:"override_line_items,omitempty" validate:"omitempty,dive"`
	// OverrideEntitlements allows customizing specific entitlements for this subscription
	OverrideEntitlements []OverrideEntitlementRequest `json:"override_entitlements,omitempty" validate:"omitempty,dive"`
	// Addons represents addons to be added to the subscription during creation
	Addons []AddAddonToSubscriptionRequest `json:"addons,omitempty" validate:"omitempty,dive"`

	// Phases represents subscription phases to be created with the subscription
	Phases []SubscriptionPhaseCreateRequest `json:"phases,omitempty" validate:"omitempty,dive"`

	// Payment behavior configuration
	PaymentBehavior        *types.PaymentBehavior `json:"payment_behavior,omitempty"`
	GatewayPaymentMethodID *string                `json:"gateway_payment_method_id,omitempty"`

	// collection_method determines how invoices are collected
	// "default_incomplete" - subscription waits for payment confirmation before activation
	// "send_invoice" - subscription activates immediately, invoice is sent for payment
	CollectionMethod *types.CollectionMethod `json:"collection_method,omitempty"`

	// ProrationBehavior controls how proration is handled.
	// If not set, the default value is none. Possible values are create_prorations and none.
	// create_prorations means the proration will be calculated and applied.
	// none means the proration will not be calculated.
	// This is IGNORED when the billing cycle is anniversary.
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`

	// Timezone of the customer.
	// If not set, the default value is UTC.
	CustomerTimezone string `json:"customer_timezone" validate:"omitempty,timezone"`

	//Billing Anchor
	BillingAnchor *time.Time `json:"-"`

	// Workflow
	Workflow *types.TemporalWorkflowType `json:"-"`

	// SubscriptionStatus determines the initial status of the subscription
	// If set to "draft", the subscription will be created as a draft (skips invoice creation and payment processing)
	SubscriptionStatus types.SubscriptionStatus `json:"subscription_status,omitempty"`

	// Enable Commitment True Up Fee
	EnableTrueUp bool `json:"enable_true_up"`
}

// AddAddonRequest is used by body-based endpoint /subscriptions/addon
type AddAddonRequest struct {
	SubscriptionID                string `json:"subscription_id" validate:"required"`
	AddAddonToSubscriptionRequest `json:",inline"`
}

// RemoveAddonRequest is used by body-based endpoint /subscriptions/addon (DELETE)
type RemoveAddonRequest struct {
	AddonAssociationID string `json:"addon_association_id" validate:"required"`
	Reason             string `json:"reason,omitempty"`
}

func (r *RemoveAddonRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	return nil
}

type UpdateSubscriptionRequest struct {
	Status            types.SubscriptionStatus `json:"status"`
	CancelAt          *time.Time               `json:"cancel_at,omitempty"`
	CancelAtPeriodEnd bool                     `json:"cancel_at_period_end,omitempty"`
}

// CancelSubscriptionRequest represents the enhanced cancellation request
type CancelSubscriptionRequest struct {

	// ProrationBehavior controls how proration is handled.
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`

	// CancellationType determines when the cancellation takes effect
	CancellationType types.CancellationType `json:"cancellation_type" validate:"required"`

	// Reason for cancellation (for audit and business intelligence)
	Reason string `json:"reason,omitempty"`

	//SuppressWebhook is an internal flag to suppress webhook events during cancellation.
	SuppressWebhook bool `json:"-,omitempty"`
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
}

// ListSubscriptionsResponse represents the response for listing subscriptions
type ListSubscriptionsResponse = types.ListResponse[*SubscriptionResponse]

func (r *CreateSubscriptionRequest) Validate() error {
	// Case- Both are absent
	if r.CustomerID == "" && r.ExternalCustomerID == "" {
		return ierr.NewError("either customer_id or external_customer_id is required").
			WithHint("Please provide either customer_id or external_customer_id").
			Mark(ierr.ErrValidation)
	}

	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	// Validate currency
	if err := types.ValidateCurrencyCode(r.Currency); err != nil {
		return err
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

	// Set default for invoice billing if not provided
	if r.InvoiceBilling == nil {
		r.InvoiceBilling = lo.ToPtr(types.InvoiceBillingInvoiceToSelf)
	} else {
		// Validate invoice billing if provided
		if err := r.InvoiceBilling.Validate(); err != nil {
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

	if r.StartDate != nil && r.StartDate.After(time.Now().UTC()) {
		return ierr.NewError("start_date cannot be in the future").
			WithHint("Start date must be in the past or present").
			WithReportableDetails(map[string]interface{}{
				"start_date": *r.StartDate,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.TrialStart != nil && r.TrialStart.After(*r.StartDate) {
		return ierr.NewError("trial_start cannot be after start_date").
			WithHint("Trial start date must be before or equal to start date").
			WithReportableDetails(map[string]interface{}{
				"start_date":  *r.StartDate,
				"trial_start": *r.TrialStart,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.TrialEnd != nil && r.TrialEnd.Before(*r.StartDate) {
		return ierr.NewError("trial_end cannot be before start_date").
			WithHint("Trial end date must be after or equal to start date").
			WithReportableDetails(map[string]interface{}{
				"start_date": *r.StartDate,
				"trial_end":  *r.TrialEnd,
			}).
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

	// Validate commitment amount and overage factor
	if r.CommitmentAmount != nil && r.CommitmentAmount.LessThan(decimal.Zero) {
		return ierr.NewError("commitment_amount must be non-negative").
			WithHint("Commitment amount must be greater than or equal to 0").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount": *r.CommitmentAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.OverageFactor != nil && r.OverageFactor.LessThan(decimal.NewFromInt(1)) {
		return ierr.NewError("overage_factor must be at least 1.0").
			WithHint("Overage factor must be greater than or equal to 1.0").
			WithReportableDetails(map[string]interface{}{
				"overage_factor": *r.OverageFactor,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate credit grants if provided
	if len(r.CreditGrants) > 0 {
		for i, grant := range r.CreditGrants {

			// Force scope to SUBSCRIPTION for all grants added this way
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

	// taxrate overrides validation
	if len(r.TaxRateOverrides) > 0 {
		for _, taxRateOverride := range r.TaxRateOverrides {
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

	// Validate line item commitments if provided
	if len(r.LineItemCommitments) > 0 {
		for priceID, commitmentConfig := range r.LineItemCommitments {
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
	}

	// Validate override line items if provided
	if len(r.OverrideLineItems) > 0 {
		priceIDsSeen := make(map[string]bool)
		for i, override := range r.OverrideLineItems {
			if err := override.Validate(nil, nil, r.PlanID); err != nil {
				return ierr.NewError(fmt.Sprintf("invalid override line item at index %d", i)).
					WithHint("Override line item validation failed").
					WithReportableDetails(map[string]interface{}{
						"index": i,
						"error": err.Error(),
					}).
					Mark(ierr.ErrValidation)
			}

			// Check for duplicate price IDs
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

	if r.CustomerTimezone == "" {
		r.CustomerTimezone = "UTC"
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

	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         r.CustomerID,
		PlanID:             r.PlanID,
		Currency:           strings.ToLower(r.Currency),
		LookupKey:          r.LookupKey,
		SubscriptionStatus: initialStatus,
		StartDate:          startDate,
		EndDate:            endDate,
		TrialStart:         r.TrialStart,
		TrialEnd:           r.TrialEnd,
		BillingCadence:     r.BillingCadence,
		BillingPeriod:      r.BillingPeriod,
		BillingPeriodCount: r.BillingPeriodCount,
		BillingAnchor:      billingAnchor,
		Metadata:           r.Metadata,
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
		BillingCycle:       r.BillingCycle,
		CustomerTimezone:   r.CustomerTimezone,
		ProrationBehavior:  r.ProrationBehavior,

		// New payment behavior fields
		PaymentBehavior:        string(paymentBehavior),
		CollectionMethod:       string(collectionMethod),
		GatewayPaymentMethodID: r.GatewayPaymentMethodID,
		InvoicingCustomerID:    r.InvoicingCustomerID,
	}

	// Set commitment amount and overage factor if provided
	if r.CommitmentAmount != nil {
		sub.CommitmentAmount = r.CommitmentAmount
	}

	if r.OverageFactor != nil {
		sub.OverageFactor = r.OverageFactor
	} else {
		sub.OverageFactor = lo.ToPtr(decimal.NewFromInt(1)) // Default value
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
}

// SubscriptionLineItemResponse represents the response for a subscription line item
type SubscriptionLineItemResponse struct {
	*subscription.SubscriptionLineItem
	Price *PriceResponse `json:"price,omitempty"`
}

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
	if r.Quantity == nil && r.Amount == nil && r.BillingModel == "" && r.TierMode == "" && len(r.Tiers) == 0 && r.TransformQuantity == nil && r.PriceUnitAmount == nil && len(r.PriceUnitTiers) == 0 {
		return ierr.NewError("at least one override field must be provided").
			WithHint("Specify at least one of: quantity, amount, billing_model, tier_mode, tiers, transform_quantity, price_unit_amount, or price_unit_tiers for price override").
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
		return ierr.NewError("amount must be non-negative").
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
	if r.Quantity != nil {
		if r.Quantity.IsNegative() {
			return ierr.NewError("quantity must be non-negative").
				WithHint("Override quantity cannot be negative").
				WithReportableDetails(map[string]interface{}{
					"quantity": r.Quantity.String(),
				}).
				Mark(ierr.ErrValidation)
		}

		// Quantity can only be set for fixed prices, not usage-based prices
		if priceMap != nil {
			if price, exists := priceMap[r.PriceID]; exists && price != nil {
				if price.Type == types.PRICE_TYPE_USAGE && r.Quantity.GreaterThan(decimal.Zero) {
					return ierr.NewError("quantity cannot be set for usage-based prices").
						WithHint("Quantity overrides are only allowed for fixed prices. Usage-based prices track quantity automatically from meter events").
						WithReportableDetails(map[string]interface{}{
							"price_id":   r.PriceID,
							"price_type": price.Type,
						}).
						Mark(ierr.ErrValidation)
				}
			}
		}
	}

	// Validate billing model if provided
	if r.BillingModel != "" {
		if err := r.BillingModel.Validate(); err != nil {
			return err
		}

		// Billing model specific validations
		switch r.BillingModel {
		case types.BILLING_MODEL_TIERED:
			// Check for tiers in either tier_mode or tiers
			hasTierMode := r.TierMode != ""
			hasTiers := len(r.Tiers) > 0

			if !hasTierMode && !hasTiers {
				return ierr.NewError("tier_mode or tiers are required when billing model is TIERED").
					WithHint("Please provide either tier_mode or tiers for tiered pricing override").
					Mark(ierr.ErrValidation)
			}

			// Validate tier mode if provided
			if r.TierMode != "" {
				if err := r.TierMode.Validate(); err != nil {
					return err
				}
			}

			// Validate tiers if provided
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

		case types.BILLING_MODEL_PACKAGE:
			if r.TransformQuantity == nil {
				return ierr.NewError("transform_quantity is required when billing model is PACKAGE").
					WithHint("Please provide the number of units to set up package pricing override").
					Mark(ierr.ErrValidation)
			}

			if err := r.TransformQuantity.Validate(); err != nil {
				return err
			}

		case types.BILLING_MODEL_FLAT_FEE:
			// For flat fee, amount is typically required unless quantity is being overridden
			if r.Amount == nil && r.Quantity == nil {
				return ierr.NewError("amount or quantity is required when billing model is FLAT_FEE").
					WithHint("Please provide either amount or quantity for flat fee pricing override").
					Mark(ierr.ErrValidation)
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

	// If context is provided, do additional validation
	if priceMap != nil && lineItemsByPriceID != nil && EntityId != "" {
		// Validate that the price exists in the plan
		_, exists := priceMap[r.PriceID]
		if !exists {
			return ierr.NewError("price not found in plan").
				WithHint("Override price must be a valid price from the selected plan").
				WithReportableDetails(map[string]interface{}{
					"price_id": r.PriceID,
					"plan_id":  EntityId,
				}).
				Mark(ierr.ErrValidation)
		}

		// Validate that the line item exists for this price
		_, exists = lineItemsByPriceID[r.PriceID]
		if !exists {
			return ierr.NewError("line item not found for price").
				WithHint("Could not find line item for the specified price").
				WithReportableDetails(map[string]interface{}{
					"price_id": r.PriceID,
				}).
				Mark(ierr.ErrInternal)
		}

		// Validate that override fields match the original price's PriceUnitType
		if originalPrice, exists := priceMap[r.PriceID]; exists && originalPrice != nil {
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

	return lineItem
}

type GetUsageBySubscriptionRequest struct {
	SubscriptionID string    `json:"subscription_id" binding:"required" example:"123"`
	StartTime      time.Time `json:"start_time" example:"2024-03-13T00:00:00Z"`
	EndTime        time.Time `json:"end_time" example:"2024-03-20T00:00:00Z"`
	LifetimeUsage  bool      `json:"lifetime_usage" example:"false"`
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
	Amount           float64            `json:"amount"`
	Currency         string             `json:"currency"`
	DisplayAmount    string             `json:"display_amount"`
	Quantity         float64            `json:"quantity"`
	FilterValues     price.JSONBFilters `json:"filter_values"`
	MeterID          string             `json:"meter_id"`
	MeterDisplayName string             `json:"meter_display_name"`
	Price            *price.Price       `json:"price"`
	IsOverage        bool               `json:"is_overage"`               // Whether this charge is at overage rate
	OverageFactor    float64            `json:"overage_factor,omitempty"` // Factor applied to this charge if in overage
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
