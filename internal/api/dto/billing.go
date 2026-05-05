package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// ── Shared result types ───────────────────────────────────────────────────────

// BillingCalculationResult holds all calculated charges for a billing period.
type BillingCalculationResult struct {
	FixedCharges []CreateInvoiceLineItemRequest
	UsageCharges []CreateInvoiceLineItemRequest
	TotalAmount  decimal.Decimal
	Currency     string
}

// LineItemClassification represents the classification of line items based on cadence and type.
type LineItemClassification struct {
	CurrentPeriodAdvance []*subscription.SubscriptionLineItem
	CurrentPeriodArrear  []*subscription.SubscriptionLineItem
	NextPeriodAdvance    []*subscription.SubscriptionLineItem
	HasUsageCharges      bool
}

// ── Request / Params types ────────────────────────────────────────────────────

// CalculateFixedChargesParams holds inputs for CalculateFixedCharges.
type CalculateFixedChargesParams struct {
	Subscription *subscription.Subscription `validate:"required"`
	PeriodStart  time.Time                  `validate:"required"`
	PeriodEnd    time.Time                  `validate:"required"`
	// FixedChargeAdjustment is an optional credit (e.g. opening/plan-change) applied only to
	// fixed line amounts after rounding.
	FixedChargeAdjustment *decimal.Decimal `json:"-"`
}

// Validate enforces struct tags and a non-negative optional adjustment.
func (p *CalculateFixedChargesParams) Validate() error {
	if err := validator.ValidateRequest(p); err != nil {
		return err
	}
	if p.FixedChargeAdjustment != nil && p.FixedChargeAdjustment.IsNegative() {
		return ierr.NewError("fixed_charge_adjustment must not be negative").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CalculateUsageChargesParams holds inputs for CalculateUsageCharges.
type CalculateUsageChargesParams struct {
	Subscription *subscription.Subscription `validate:"required"`
	Usage        *GetUsageBySubscriptionResponse
	PeriodStart  time.Time `validate:"required"`
	PeriodEnd    time.Time `validate:"required"`
}

// Validate enforces struct tags.
func (p *CalculateUsageChargesParams) Validate() error {
	return validator.ValidateRequest(p)
}

// CalculateAllChargesParams holds inputs for CalculateAllCharges.
type CalculateAllChargesParams struct {
	Subscription *subscription.Subscription `validate:"required"`
	Usage        *GetUsageBySubscriptionResponse
	PeriodStart  time.Time `validate:"required"`
	PeriodEnd    time.Time `validate:"required"`
	// FixedChargeAdjustment is forwarded to CalculateFixedCharges only (usage unchanged).
	FixedChargeAdjustment *decimal.Decimal `json:"-"`
}

// Validate enforces struct tags and a non-negative optional adjustment.
func (p *CalculateAllChargesParams) Validate() error {
	if err := validator.ValidateRequest(p); err != nil {
		return err
	}
	if p.FixedChargeAdjustment != nil && p.FixedChargeAdjustment.IsNegative() {
		return ierr.NewError("fixed_charge_adjustment must not be negative").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// PrepareSubscriptionInvoiceRequestParams holds inputs for PrepareSubscriptionInvoiceRequest.
type PrepareSubscriptionInvoiceRequestParams struct {
	Subscription   *subscription.Subscription  `validate:"required"`
	PeriodStart    time.Time                   `validate:"required"`
	PeriodEnd      time.Time                   `validate:"required"`
	ReferencePoint types.InvoiceReferencePoint `validate:"required"`
	// ExcludeInvoiceID excludes lines already invoiced for the same price/period (empty = no exclusion).
	ExcludeInvoiceID string `json:"-"`
	// OpeningInvoiceAdjustmentAmount is the credit from the cancelled subscription to apply to the
	// first invoice. Applied as FixedChargeAdjustment (reduces fixed line item amounts before coupons).
	OpeningInvoiceAdjustmentAmount *decimal.Decimal `json:"-"`
}

// Validate enforces struct tags and the reference point.
func (p *PrepareSubscriptionInvoiceRequestParams) Validate() error {
	if err := validator.ValidateRequest(p); err != nil {
		return err
	}
	if err := p.ReferencePoint.Validate(); err != nil {
		return err
	}
	return nil
}

// ClassifyLineItemsParams holds inputs for ClassifyLineItems.
type ClassifyLineItemsParams struct {
	Subscription       *subscription.Subscription `validate:"required"`
	CurrentPeriodStart time.Time                  `validate:"required"`
	CurrentPeriodEnd   time.Time                  `validate:"required"`
	NextPeriodStart    time.Time
	NextPeriodEnd      time.Time
}

// Validate enforces struct tags.
func (p *ClassifyLineItemsParams) Validate() error {
	return validator.ValidateRequest(p)
}

// FilterLineItemsToBeInvoicedParams holds inputs for FilterLineItemsToBeInvoiced.
type FilterLineItemsToBeInvoicedParams struct {
	Subscription     *subscription.Subscription `validate:"required"`
	PeriodStart      time.Time                  `validate:"required"`
	PeriodEnd        time.Time                  `validate:"required"`
	LineItems        []*subscription.SubscriptionLineItem
	ExcludeInvoiceID string `json:"-"`
}

// Validate enforces struct tags.
func (p *FilterLineItemsToBeInvoicedParams) Validate() error {
	return validator.ValidateRequest(p)
}

// CalculateChargesParams holds inputs for CalculateCharges.
type CalculateChargesParams struct {
	Subscription *subscription.Subscription `validate:"required"`
	LineItems    []*subscription.SubscriptionLineItem
	PeriodStart  time.Time `validate:"required"`
	PeriodEnd    time.Time `validate:"required"`
	IncludeUsage bool
	// FixedChargeAdjustment is forwarded to fixed-charge calculation only.
	FixedChargeAdjustment *decimal.Decimal `json:"-"`
}

// Validate enforces struct tags and a non-negative optional adjustment.
func (p *CalculateChargesParams) Validate() error {
	if err := validator.ValidateRequest(p); err != nil {
		return err
	}
	if p.FixedChargeAdjustment != nil && p.FixedChargeAdjustment.IsNegative() {
		return ierr.NewError("fixed_charge_adjustment must not be negative").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CreateInvoiceRequestForChargesParams holds inputs for CreateInvoiceRequestForCharges.
type CreateInvoiceRequestForChargesParams struct {
	Subscription *subscription.Subscription `validate:"required"`
	Result       *BillingCalculationResult  // nil produces a zero-amount invoice
	PeriodStart  time.Time                  `validate:"required"`
	PeriodEnd    time.Time                  `validate:"required"`
	Description  string
	Metadata     types.Metadata
}

// Validate enforces struct tags.
func (p *CreateInvoiceRequestForChargesParams) Validate() error {
	return validator.ValidateRequest(p)
}

// CalculateFeatureUsageChargesParams holds inputs for CalculateFeatureUsageCharges.
type CalculateFeatureUsageChargesParams struct {
	Subscription *subscription.Subscription `validate:"required"`
	Usage        *GetUsageBySubscriptionResponse
	PeriodStart  time.Time `validate:"required"`
	PeriodEnd    time.Time `validate:"required"`
	// Source controls which ClickHouse table / query mode is used.
	// Set to types.UsageSourceInvoiceCreation to use FINAL on feature_usage.
	// Zero value means no special source.
	Source types.UsageSource
}

// Validate enforces struct tags.
func (p *CalculateFeatureUsageChargesParams) Validate() error {
	return validator.ValidateRequest(p)
}

// AggregateEntitlementsParams holds inputs for AggregateEntitlements.
type AggregateEntitlementsParams struct {
	Entitlements   []*EntitlementResponse
	SubscriptionID string
}

// Validate is a no-op (no required fields).
func (p *AggregateEntitlementsParams) Validate() error {
	return nil
}

// ── Result types ─────────────────────────────────────────────────────────────

// CalculateFixedChargesResult holds the output of CalculateFixedCharges.
type CalculateFixedChargesResult struct {
	LineItems   []CreateInvoiceLineItemRequest
	TotalAmount decimal.Decimal
}

// CalculateUsageChargesResult holds the output of CalculateUsageCharges.
type CalculateUsageChargesResult struct {
	LineItems   []CreateInvoiceLineItemRequest
	TotalAmount decimal.Decimal
}

// CalculateFeatureUsageChargesResult holds the output of CalculateFeatureUsageCharges.
type CalculateFeatureUsageChargesResult struct {
	LineItems   []CreateInvoiceLineItemRequest
	TotalAmount decimal.Decimal
}
