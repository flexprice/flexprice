package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// ── Input types ──────────────────────────────────────────────────────────────

type CalculateFixedChargesInput struct {
	Sub         *subscription.Subscription
	PeriodStart time.Time
	PeriodEnd   time.Time
}

func (r *CalculateFixedChargesInput) Validate() error {
	if r.Sub == nil {
		return ierr.NewError("sub is required").Mark(ierr.ErrValidation)
	}
	return nil
}

type CalculateUsageChargesInput struct {
	Sub         *subscription.Subscription
	Usage       *dto.GetUsageBySubscriptionResponse
	PeriodStart time.Time
	PeriodEnd   time.Time
}

func (r *CalculateUsageChargesInput) Validate() error {
	if r.Sub == nil {
		return ierr.NewError("sub is required").Mark(ierr.ErrValidation)
	}
	return nil
}

type CalculateAllChargesInput struct {
	Sub         *subscription.Subscription
	Usage       *dto.GetUsageBySubscriptionResponse
	PeriodStart time.Time
	PeriodEnd   time.Time
}

func (r *CalculateAllChargesInput) Validate() error {
	if r.Sub == nil {
		return ierr.NewError("sub is required").Mark(ierr.ErrValidation)
	}
	return nil
}

type PrepareSubscriptionInvoiceInput struct {
	Sub              *subscription.Subscription
	PeriodStart      time.Time
	PeriodEnd        time.Time
	ReferencePoint   types.InvoiceReferencePoint
	ExcludeInvoiceID string
}

func (r *PrepareSubscriptionInvoiceInput) Validate() error {
	if r.Sub == nil {
		return ierr.NewError("sub is required").Mark(ierr.ErrValidation)
	}
	if r.ReferencePoint == "" {
		return ierr.NewError("reference_point is required").Mark(ierr.ErrValidation)
	}
	return nil
}

type ClassifyLineItemsInput struct {
	Sub                *subscription.Subscription
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
	NextPeriodStart    time.Time
	NextPeriodEnd      time.Time
}

func (r *ClassifyLineItemsInput) Validate() error {
	if r.Sub == nil {
		return ierr.NewError("sub is required").Mark(ierr.ErrValidation)
	}
	return nil
}

type FilterLineItemsToBeInvoicedInput struct {
	Sub              *subscription.Subscription
	PeriodStart      time.Time
	PeriodEnd        time.Time
	LineItems        []*subscription.SubscriptionLineItem
	ExcludeInvoiceID string
}

func (r *FilterLineItemsToBeInvoicedInput) Validate() error {
	if r.Sub == nil {
		return ierr.NewError("sub is required").Mark(ierr.ErrValidation)
	}
	return nil
}

type CalculateChargesInput struct {
	Sub          *subscription.Subscription
	LineItems    []*subscription.SubscriptionLineItem
	PeriodStart  time.Time
	PeriodEnd    time.Time
	IncludeUsage bool
}

func (r *CalculateChargesInput) Validate() error {
	if r.Sub == nil {
		return ierr.NewError("sub is required").Mark(ierr.ErrValidation)
	}
	return nil
}

type CreateInvoiceRequestForChargesInput struct {
	Sub         *subscription.Subscription
	Result      *BillingCalculationResult // nil produces a zero-amount invoice
	PeriodStart time.Time
	PeriodEnd   time.Time
	Description string
	Metadata    types.Metadata
}

func (r *CreateInvoiceRequestForChargesInput) Validate() error {
	if r.Sub == nil {
		return ierr.NewError("sub is required").Mark(ierr.ErrValidation)
	}
	return nil
}

type CalculateFeatureUsageChargesInput struct {
	Sub         *subscription.Subscription
	Usage       *dto.GetUsageBySubscriptionResponse
	PeriodStart time.Time
	PeriodEnd   time.Time
	// Source controls which ClickHouse table / query mode is used.
	// Pass types.UsageSourceInvoiceCreation to use FINAL on feature_usage.
	// Zero value means no special source (same as the old nil opts).
	Source types.UsageSource
}

func (r *CalculateFeatureUsageChargesInput) Validate() error {
	if r.Sub == nil {
		return ierr.NewError("sub is required").Mark(ierr.ErrValidation)
	}
	return nil
}

type GetCustomerEntitlementsInput struct {
	CustomerID string
	Filters    *dto.GetCustomerEntitlementsRequest
}

func (r *GetCustomerEntitlementsInput) Validate() error {
	if r.CustomerID == "" {
		return ierr.NewError("customer_id is required").Mark(ierr.ErrValidation)
	}
	return nil
}

type AggregateEntitlementsInput struct {
	Entitlements   []*dto.EntitlementResponse
	SubscriptionID string
}

func (r *AggregateEntitlementsInput) Validate() error {
	return nil
}

type GetCustomerUsageSummaryInput struct {
	CustomerID string
	Filters    *dto.GetCustomerUsageSummaryRequest
}

func (r *GetCustomerUsageSummaryInput) Validate() error {
	if r.CustomerID == "" {
		return ierr.NewError("customer_id is required").Mark(ierr.ErrValidation)
	}
	return nil
}

// ── Result types ─────────────────────────────────────────────────────────────

// CalculateFixedChargesResult holds the output of CalculateFixedCharges.
type CalculateFixedChargesResult struct {
	LineItems   []dto.CreateInvoiceLineItemRequest
	TotalAmount decimal.Decimal
}

// CalculateUsageChargesResult holds the output of CalculateUsageCharges.
type CalculateUsageChargesResult struct {
	LineItems   []dto.CreateInvoiceLineItemRequest
	TotalAmount decimal.Decimal
}

// CalculateFeatureUsageChargesResult holds the output of CalculateFeatureUsageCharges.
type CalculateFeatureUsageChargesResult struct {
	LineItems   []dto.CreateInvoiceLineItemRequest
	TotalAmount decimal.Decimal
}
