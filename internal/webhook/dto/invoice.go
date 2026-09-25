package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type InternalInvoiceEvent struct {
	InvoiceID string `json:"invoice_id"`
	TenantID  string `json:"tenant_id"`
}

// Invoice is the wire shape of an invoice on a webhook. It is deliberately not
// dto.InvoiceResponse: the API response carries per-row bookkeeping (tenant_id,
// created_by/updated_by, status) and whole subtrees consumers never read, which
// pushed large invoices past the Svix per-message payload limit.
type Invoice struct {
	ID              string              `json:"id"`
	CustomerID      string              `json:"customer_id"`
	SubscriptionID  *string             `json:"subscription_id,omitempty"`
	EnvironmentID   string              `json:"environment_id"`
	InvoiceNumber   *string             `json:"invoice_number,omitempty"`
	InvoiceType     types.InvoiceType   `json:"invoice_type"`
	InvoiceStatus   types.InvoiceStatus `json:"invoice_status"`
	PaymentStatus   types.PaymentStatus `json:"payment_status"`
	BillingReason   string              `json:"billing_reason,omitempty"`
	Currency        string              `json:"currency"`
	IdempotencyKey  *string             `json:"idempotency_key,omitempty"`
	BillingSequence *int                `json:"billing_sequence,omitempty"`
	BillingPeriod   *string             `json:"billing_period,omitempty"`
	Description     string              `json:"description,omitempty"`

	AmountDue                  decimal.Decimal `json:"amount_due" swaggertype:"string"`
	AmountPaid                 decimal.Decimal `json:"amount_paid" swaggertype:"string"`
	AmountRemaining            decimal.Decimal `json:"amount_remaining" swaggertype:"string"`
	Subtotal                   decimal.Decimal `json:"subtotal" swaggertype:"string"`
	Total                      decimal.Decimal `json:"total" swaggertype:"string"`
	TotalTax                   decimal.Decimal `json:"total_tax" swaggertype:"string"`
	TotalDiscount              decimal.Decimal `json:"total_discount" swaggertype:"string"`
	TotalPrepaidCreditsApplied decimal.Decimal `json:"total_prepaid_credits_applied" swaggertype:"string"`

	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	PeriodStart *time.Time `json:"period_start,omitempty"`
	PeriodEnd   *time.Time `json:"period_end,omitempty"`
	DueDate     *time.Time `json:"due_date,omitempty"`
	PaidAt      *time.Time `json:"paid_at,omitempty"`
	VoidedAt    *time.Time `json:"voided_at,omitempty"`
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`

	InvoicePDFURL *string        `json:"invoice_pdf_url,omitempty"`
	Metadata      types.Metadata `json:"metadata,omitempty"`

	Customer           *Customer            `json:"customer,omitempty"`
	Subscription       *Subscription        `json:"subscription,omitempty"`
	LineItems          []*InvoiceLineItem   `json:"line_items,omitempty"`
	Taxes              []*TaxApplied        `json:"taxes,omitempty"`
	TaxSummary         *dto.TaxSummary      `json:"tax_summary,omitempty"`
	CouponApplications []*CouponApplication `json:"coupon_applications,omitempty"`
}

type InvoiceLineItem struct {
	ID                          string                `json:"id"`
	PriceID                     *string               `json:"price_id,omitempty"`
	PriceType                   *string               `json:"price_type,omitempty"`
	MeterID                     *string               `json:"meter_id,omitempty"`
	MeterDisplayName            *string               `json:"meter_display_name,omitempty"`
	EntityID                    *string               `json:"entity_id,omitempty"`
	EntityType                  *string               `json:"entity_type,omitempty"`
	DisplayName                 *string               `json:"display_name,omitempty"`
	PlanDisplayName             *string               `json:"plan_display_name,omitempty"`
	Amount                      decimal.Decimal       `json:"amount" swaggertype:"string"`
	Quantity                    decimal.Decimal       `json:"quantity" swaggertype:"string"`
	Currency                    string                `json:"currency"`
	PeriodStart                 *time.Time            `json:"period_start,omitempty"`
	PeriodEnd                   *time.Time            `json:"period_end,omitempty"`
	SubscriptionLineItemID      *string               `json:"subscription_line_item_id,omitempty"`
	PriceUnit                   *string               `json:"price_unit,omitempty"`
	PriceUnitAmount             *decimal.Decimal      `json:"price_unit_amount,omitempty" swaggertype:"string"`
	PrepaidCreditsApplied       decimal.Decimal       `json:"prepaid_credits_applied" swaggertype:"string"`
	LineItemDiscount            decimal.Decimal       `json:"line_item_discount" swaggertype:"string"`
	InvoiceLevelDiscount        decimal.Decimal       `json:"invoice_level_discount" swaggertype:"string"`
	AdjustedEntitlementQuantity *decimal.Decimal      `json:"adjusted_entitlement_quantity,omitempty" swaggertype:"string"`
	CommitmentInfo              *types.CommitmentInfo `json:"commitment_info,omitempty"`
	Metadata                    types.Metadata        `json:"metadata,omitempty"`
}

type Plan struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	LookupKey   string         `json:"lookup_key,omitempty"`
	Description string         `json:"description,omitempty"`
	Metadata    types.Metadata `json:"metadata,omitempty"`
	Prices      []*Price       `json:"prices,omitempty"`
}

type Price struct {
	ID                     string                       `json:"id"`
	Amount                 decimal.Decimal              `json:"amount" swaggertype:"string"`
	DisplayAmount          string                       `json:"display_amount"`
	Currency               string                       `json:"currency"`
	Type                   types.PriceType              `json:"type"`
	BillingPeriod          types.BillingPeriod          `json:"billing_period"`
	BillingPeriodCount     int                          `json:"billing_period_count"`
	BillingModel           types.BillingModel           `json:"billing_model"`
	BillingCadence         types.BillingCadence         `json:"billing_cadence"`
	InvoiceCadence         types.InvoiceCadence         `json:"invoice_cadence"`
	DisplayName            string                       `json:"display_name,omitempty"`
	LookupKey              string                       `json:"lookup_key,omitempty"`
	Description            string                       `json:"description,omitempty"`
	TierMode               types.BillingTier            `json:"tier_mode,omitempty"`
	Tiers                  price.JSONBTiers             `json:"tiers,omitempty"`
	TransformQuantity      price.JSONBTransformQuantity `json:"transform_quantity,omitempty"`
	MeterID                string                       `json:"meter_id,omitempty"`
	PriceUnit              *string                      `json:"price_unit,omitempty"`
	PriceUnitAmount        *decimal.Decimal             `json:"price_unit_amount,omitempty" swaggertype:"string"`
	DisplayPriceUnitAmount string                       `json:"display_price_unit_amount,omitempty"`
	Metadata               price.JSONBMetadata          `json:"metadata,omitempty"`
	Meter                  *Meter                       `json:"meter,omitempty"`
}

type Meter struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	EventName   string            `json:"event_name"`
	Aggregation meter.Aggregation `json:"aggregation"`
	ResetUsage  types.ResetUsage  `json:"reset_usage,omitempty"`
}

type TaxApplied struct {
	ID            string            `json:"id"`
	TaxRateID     string            `json:"tax_rate_id,omitempty"`
	TaxableAmount decimal.Decimal   `json:"taxable_amount" swaggertype:"string"`
	TaxAmount     decimal.Decimal   `json:"tax_amount" swaggertype:"string"`
	TaxBehavior   types.TaxBehavior `json:"tax_behavior,omitempty"`
	Currency      string            `json:"currency,omitempty"`
	AppliedAt     time.Time         `json:"applied_at"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	TaxRate       *TaxRate          `json:"tax_rate,omitempty"`
}

type TaxRate struct {
	ID              string            `json:"id"`
	Name            string            `json:"name,omitempty"`
	Code            string            `json:"code,omitempty"`
	Description     string            `json:"description,omitempty"`
	TaxRateType     types.TaxRateType `json:"tax_rate_type,omitempty"`
	PercentageValue *decimal.Decimal  `json:"percentage_value,omitempty" swaggertype:"string"`
}

type CouponApplication struct {
	ID                  string                 `json:"id"`
	CouponID            string                 `json:"coupon_id"`
	CouponAssociationID string                 `json:"coupon_association_id,omitempty"`
	InvoiceLineItemID   *string                `json:"invoice_line_item_id,omitempty"`
	SubscriptionID      *string                `json:"subscription_id,omitempty"`
	AppliedAt           time.Time              `json:"applied_at"`
	OriginalPrice       decimal.Decimal        `json:"original_price" swaggertype:"string"`
	FinalPrice          decimal.Decimal        `json:"final_price" swaggertype:"string"`
	DiscountedAmount    decimal.Decimal        `json:"discounted_amount" swaggertype:"string"`
	DiscountType        types.CouponType       `json:"discount_type,omitempty"`
	DiscountPercentage  *decimal.Decimal       `json:"discount_percentage,omitempty" swaggertype:"string"`
	Currency            string                 `json:"currency,omitempty"`
	CouponSnapshot      map[string]interface{} `json:"coupon_snapshot,omitempty"`
	Metadata            map[string]string      `json:"metadata,omitempty"`
}

type CouponAssociation struct {
	ID                     string            `json:"id"`
	CouponID               string            `json:"coupon_id"`
	SubscriptionID         string            `json:"subscription_id"`
	SubscriptionLineItemID *string           `json:"subscription_line_item_id,omitempty"`
	StartDate              time.Time         `json:"start_date"`
	EndDate                *time.Time        `json:"end_date,omitempty"`
	Metadata               map[string]string `json:"metadata,omitempty"`
}

// isBillableLineItem reports whether a line item is worth putting on the wire.
// Period fan-out emits one line item per sub-window per price whether or not any
// usage landed in it, so an invoice accumulates rows that carry no information.
// A zero amount alone is not enough to drop a row: entitlement-covered and fully
// discounted usage is still usage the customer expects to see itemised.
func isBillableLineItem(item *dto.InvoiceLineItemResponse) bool {
	if item == nil {
		return false
	}
	return !item.Amount.IsZero() || !item.Quantity.IsZero()
}

func newInvoiceLineItem(item *dto.InvoiceLineItemResponse) *InvoiceLineItem {
	if item == nil {
		return nil
	}
	return &InvoiceLineItem{
		ID:                          item.ID,
		PriceID:                     item.PriceID,
		PriceType:                   item.PriceType,
		MeterID:                     item.MeterID,
		MeterDisplayName:            item.MeterDisplayName,
		EntityID:                    item.EntityID,
		EntityType:                  item.EntityType,
		DisplayName:                 item.DisplayName,
		PlanDisplayName:             item.PlanDisplayName,
		Amount:                      item.Amount,
		Quantity:                    item.Quantity,
		Currency:                    item.Currency,
		PeriodStart:                 item.PeriodStart,
		PeriodEnd:                   item.PeriodEnd,
		SubscriptionLineItemID:      item.SubscriptionLineItemID,
		PriceUnit:                   item.PriceUnit,
		PriceUnitAmount:             item.PriceUnitAmount,
		PrepaidCreditsApplied:       item.PrepaidCreditsApplied,
		LineItemDiscount:            item.LineItemDiscount,
		InvoiceLevelDiscount:        item.InvoiceLevelDiscount,
		AdjustedEntitlementQuantity: item.AdjustedEntitlementQuantity,
		CommitmentInfo:              item.CommitmentInfo,
		Metadata:                    item.Metadata,
	}
}

func newMeter(resp *dto.MeterResponse) *Meter {
	if resp == nil {
		return nil
	}
	return &Meter{
		ID:          resp.ID,
		Name:        resp.Name,
		EventName:   resp.EventName,
		Aggregation: resp.Aggregation,
		ResetUsage:  resp.ResetUsage,
	}
}

func newPriceFromDomain(p *price.Price, m *Meter) *Price {
	if p == nil {
		return nil
	}
	return &Price{
		ID:                     p.ID,
		Amount:                 p.Amount,
		DisplayAmount:          p.DisplayAmount,
		Currency:               p.Currency,
		Type:                   p.Type,
		BillingPeriod:          p.BillingPeriod,
		BillingPeriodCount:     p.BillingPeriodCount,
		BillingModel:           p.BillingModel,
		BillingCadence:         p.BillingCadence,
		InvoiceCadence:         p.InvoiceCadence,
		DisplayName:            p.DisplayName,
		LookupKey:              p.LookupKey,
		Description:            p.Description,
		TierMode:               p.TierMode,
		Tiers:                  p.Tiers,
		TransformQuantity:      p.TransformQuantity,
		MeterID:                p.MeterID,
		PriceUnit:              p.PriceUnit,
		PriceUnitAmount:        p.PriceUnitAmount,
		DisplayPriceUnitAmount: p.DisplayPriceUnitAmount,
		Metadata:               p.Metadata,
		Meter:                  m,
	}
}

func newPrice(resp *dto.PriceResponse) *Price {
	if resp == nil {
		return nil
	}
	return newPriceFromDomain(resp.Price, newMeter(resp.Meter))
}

// newPlan keeps only the prices in referencedPriceIDs. A plan's full price
// catalogue is fixed overhead unrelated to the invoice being sent, and consumers
// resolve prices by the price_id on a line item.
func newPlan(resp *dto.PlanResponse, referencedPriceIDs map[string]struct{}) *Plan {
	if resp == nil || resp.Plan == nil {
		return nil
	}

	prices := make([]*Price, 0, len(referencedPriceIDs))
	for _, p := range resp.Prices {
		if p == nil || p.Price == nil {
			continue
		}
		if _, ok := referencedPriceIDs[p.ID]; !ok {
			continue
		}
		prices = append(prices, newPrice(p))
	}

	return &Plan{
		ID:          resp.ID,
		Name:        resp.Name,
		LookupKey:   resp.LookupKey,
		Description: resp.Description,
		Metadata:    resp.Metadata,
		Prices:      prices,
	}
}

func newSubscriptionLineItem(item *subscription.SubscriptionLineItem) *SubscriptionLineItem {
	if item == nil {
		return nil
	}
	return &SubscriptionLineItem{
		ID:                 item.ID,
		SubscriptionID:     item.SubscriptionID,
		EntityID:           item.EntityID,
		EntityType:         item.EntityType,
		PlanDisplayName:    item.PlanDisplayName,
		PriceID:            item.PriceID,
		PriceType:          item.PriceType,
		MeterID:            item.MeterID,
		MeterDisplayName:   item.MeterDisplayName,
		DisplayName:        item.DisplayName,
		Quantity:           item.Quantity,
		Currency:           item.Currency,
		BillingPeriod:      item.BillingPeriod,
		BillingPeriodCount: item.BillingPeriodCount,
		InvoiceCadence:     item.InvoiceCadence,
		StartDate:          item.StartDate,
		EndDate:            item.EndDate,
		PriceUnit:          item.PriceUnit,
		Metadata:           item.Metadata,
		Price:              newPriceFromDomain(item.Price, nil),
	}
}

func newCouponAssociations(resp *dto.SubscriptionResponse) []*CouponAssociation {
	if resp == nil {
		return nil
	}
	associations := make([]*CouponAssociation, 0, len(resp.CouponAssociations))
	for _, ca := range resp.CouponAssociations {
		if ca == nil || ca.CouponAssociation == nil {
			continue
		}
		associations = append(associations, &CouponAssociation{
			ID:                     ca.ID,
			CouponID:               ca.CouponID,
			SubscriptionID:         ca.SubscriptionID,
			SubscriptionLineItemID: ca.SubscriptionLineItemID,
			StartDate:              ca.StartDate,
			EndDate:                ca.EndDate,
			Metadata:               ca.Metadata,
		})
	}
	if len(associations) == 0 {
		return nil
	}
	return associations
}

// newInvoiceSubscription is the invoice-webhook view of a subscription: the base
// subscription fields plus the plan, line items and coupon associations an invoice
// consumer needs to price a line. NewSubscription stays untouched so subscription.*
// webhooks keep their existing payload.
func newInvoiceSubscription(resp *dto.SubscriptionResponse, referencedPriceIDs map[string]struct{}) *Subscription {
	sub := NewSubscription(resp)
	if sub == nil {
		return nil
	}

	sub.Plan = newPlan(resp.Plan, referencedPriceIDs)
	sub.CouponAssociations = newCouponAssociations(resp)

	lineItems := make([]*SubscriptionLineItem, 0, len(resp.LineItems))
	for _, item := range resp.LineItems {
		if li := newSubscriptionLineItem(item); li != nil {
			lineItems = append(lineItems, li)
		}
	}
	if len(lineItems) > 0 {
		sub.LineItems = lineItems
	}

	return sub
}

func newTaxes(taxes []*dto.TaxAppliedResponse) []*TaxApplied {
	applied := make([]*TaxApplied, 0, len(taxes))
	for _, tax := range taxes {
		if tax == nil {
			continue
		}
		t := &TaxApplied{
			ID:            tax.ID,
			TaxRateID:     tax.GetTaxRateID(),
			TaxableAmount: tax.TaxableAmount,
			TaxAmount:     tax.TaxAmount,
			TaxBehavior:   tax.TaxBehavior,
			Currency:      tax.Currency,
			AppliedAt:     tax.AppliedAt,
			Metadata:      tax.Metadata,
		}
		if tax.TaxRate != nil && tax.TaxRate.TaxRate != nil {
			t.TaxRate = &TaxRate{
				ID:              tax.TaxRate.ID,
				Name:            tax.TaxRate.Name,
				Code:            tax.TaxRate.Code,
				Description:     tax.TaxRate.Description,
				TaxRateType:     tax.TaxRate.TaxRateType,
				PercentageValue: tax.TaxRate.PercentageValue,
			}
		}
		applied = append(applied, t)
	}
	if len(applied) == 0 {
		return nil
	}
	return applied
}

func newCouponApplications(applications []*dto.CouponApplicationResponse) []*CouponApplication {
	applied := make([]*CouponApplication, 0, len(applications))
	for _, ca := range applications {
		if ca == nil || ca.CouponApplication == nil {
			continue
		}
		applied = append(applied, &CouponApplication{
			ID:                  ca.ID,
			CouponID:            ca.CouponID,
			CouponAssociationID: ca.CouponAssociationID,
			InvoiceLineItemID:   ca.InvoiceLineItemID,
			SubscriptionID:      ca.SubscriptionID,
			AppliedAt:           ca.AppliedAt,
			OriginalPrice:       ca.OriginalPrice,
			FinalPrice:          ca.FinalPrice,
			DiscountedAmount:    ca.DiscountedAmount,
			DiscountType:        ca.DiscountType,
			DiscountPercentage:  ca.DiscountPercentage,
			Currency:            ca.Currency,
			CouponSnapshot:      ca.CouponSnapshot,
			Metadata:            ca.Metadata,
		})
	}
	if len(applied) == 0 {
		return nil
	}
	return applied
}

// referencedPriceIDs collects the prices a consumer can actually reach from this
// payload: those on the line items being sent, plus those on the subscription's
// own line items.
func referencedPriceIDs(lineItems []*InvoiceLineItem, sub *dto.SubscriptionResponse) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, item := range lineItems {
		if id := lo.FromPtr(item.PriceID); id != "" {
			ids[id] = struct{}{}
		}
	}
	if sub != nil {
		for _, item := range sub.LineItems {
			if item != nil && item.PriceID != "" {
				ids[item.PriceID] = struct{}{}
			}
		}
	}
	return ids
}

func NewInvoice(resp *dto.InvoiceResponse) *Invoice {
	if resp == nil {
		return nil
	}

	lineItems := make([]*InvoiceLineItem, 0, len(resp.LineItems))
	for _, item := range resp.LineItems {
		if !isBillableLineItem(item) {
			continue
		}
		if li := newInvoiceLineItem(item); li != nil {
			lineItems = append(lineItems, li)
		}
	}

	customer := NewCustomer(resp.Customer)
	if customer == nil && resp.Subscription != nil {
		customer = NewCustomer(resp.Subscription.Customer)
	}

	return &Invoice{
		ID:                         resp.ID,
		CustomerID:                 resp.CustomerID,
		SubscriptionID:             resp.SubscriptionID,
		EnvironmentID:              resp.EnvironmentID,
		InvoiceNumber:              resp.InvoiceNumber,
		InvoiceType:                resp.InvoiceType,
		InvoiceStatus:              resp.InvoiceStatus,
		PaymentStatus:              resp.PaymentStatus,
		BillingReason:              resp.BillingReason,
		Currency:                   resp.Currency,
		IdempotencyKey:             resp.IdempotencyKey,
		BillingSequence:            resp.BillingSequence,
		BillingPeriod:              resp.BillingPeriod,
		Description:                resp.Description,
		AmountDue:                  resp.AmountDue,
		AmountPaid:                 resp.AmountPaid,
		AmountRemaining:            resp.AmountRemaining,
		Subtotal:                   resp.Subtotal,
		Total:                      resp.Total,
		TotalTax:                   resp.TotalTax,
		TotalDiscount:              resp.TotalDiscount,
		TotalPrepaidCreditsApplied: resp.TotalPrepaidCreditsApplied,
		CreatedAt:                  resp.CreatedAt,
		UpdatedAt:                  resp.UpdatedAt,
		PeriodStart:                resp.PeriodStart,
		PeriodEnd:                  resp.PeriodEnd,
		DueDate:                    resp.DueDate,
		PaidAt:                     resp.PaidAt,
		VoidedAt:                   resp.VoidedAt,
		FinalizedAt:                resp.FinalizedAt,
		InvoicePDFURL:              resp.InvoicePDFURL,
		Metadata:                   resp.Metadata,
		Customer:                   customer,
		Subscription:               newInvoiceSubscription(resp.Subscription, referencedPriceIDs(lineItems, resp.Subscription)),
		LineItems:                  lineItems,
		Taxes:                      newTaxes(resp.Taxes),
		TaxSummary:                 resp.TaxSummary,
		CouponApplications:         newCouponApplications(resp.CouponApplications),
	}
}

type InvoiceWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Invoice   *Invoice               `json:"invoice"`
}

func NewInvoiceWebhookPayload(invoice *dto.InvoiceResponse, eventType types.WebhookEventName) *InvoiceWebhookPayload {
	return &InvoiceWebhookPayload{EventType: eventType, Invoice: NewInvoice(invoice)}
}
