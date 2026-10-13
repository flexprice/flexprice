package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

var paymentAddonPrice = decimal.NewFromInt(5)

// gatewaysRefundingToSource are the gateways the server can refund back to the
// card (integration factory GetRefundProvider). Elsewhere a refund settles to
// the customer's wallet instead.
var gatewaysRefundingToSource = map[string]bool{
	"chargebee": true,
	"razorpay":  true,
}

// modifySubscriptionAutoCharge raises the plan line item's quantity with
// checkout and asserts the change is gated on, then applied by, the payment.
func (p *PaymentAutoChargeProbe) modifySubscriptionAutoCharge(ctx context.Context, f *paymentFlow, subID string) error {
	ids := map[string]string{"subscription_id": subID}
	item, err := p.planLineItem(ctx, f, subID)
	if err != nil {
		return err
	}
	ids["line_item_id"] = derefStr(item.ID)
	ids["price_id"] = derefStr(item.PriceID)

	resp, err := p.client.Payments().ExecuteSubscriptionModify(ctx, subID, types.ExecuteSubscriptionModifyRequest{
		Type: types.SubscriptionModifyTypeQuantityChange,
		QuantityChangeParams: &types.SubModifyQuantityChangeRequest{
			LineItems: []types.LineItemQuantityChange{{ID: derefStr(item.ID), Quantity: strPtr("2")}},
		},
		Checkout: f.checkoutParams(types.CollectionMethodChargeAutomatically, false),
	})
	if err != nil {
		return f.fail("modify_autocharge_start", ids, "execute quantity change: %w", err)
	}
	if resp == nil || resp.SubscriptionModifyResponse == nil || resp.SubscriptionModifyResponse.CheckoutSession == nil {
		return f.fail("modify_autocharge_assert_gated", ids, "quantity increase on an in-advance price was applied without a checkout session")
	}
	sessionID := derefStr(resp.SubscriptionModifyResponse.CheckoutSession.ID)

	final, err := f.waitTerminal(ctx, "modify_autocharge_session", sessionID)
	if err != nil {
		return err
	}
	if err := f.expectCompleted("modify_autocharge_session", final); err != nil {
		return err
	}

	sub, err := p.client.Subscriptions().Get(ctx, subID)
	if err != nil {
		return f.fail("modify_autocharge_read_sub", ids, "get subscription: %w", err)
	}
	for _, li := range subscriptionLineItems(sub) {
		if derefStr(li.PriceID) != derefStr(item.PriceID) || !lineItemOpen(li) {
			continue
		}
		if q, _ := decimal.NewFromString(derefStr(li.Quantity)); q.Equal(decimal.NewFromInt(2)) {
			return nil
		}
		ids["quantity"] = derefStr(li.Quantity)
	}
	return f.fail("modify_autocharge_assert_quantity", ids, "no open line item for the price carries quantity 2 after the paid change")
}

// planLineItem returns the subscription's open, in-advance fixed line item.
func (p *PaymentAutoChargeProbe) planLineItem(ctx context.Context, f *paymentFlow, subID string) (types.SubscriptionSubscriptionLineItem, error) {
	sub, err := p.client.Subscriptions().Get(ctx, subID)
	if err != nil {
		return types.SubscriptionSubscriptionLineItem{}, f.fail("modify_read_sub", map[string]string{"subscription_id": subID}, "get subscription: %w", err)
	}
	for _, li := range subscriptionLineItems(sub) {
		if lineItemOpen(li) && li.PriceType != nil && *li.PriceType == types.PriceTypeFixed &&
			li.InvoiceCadence != nil && *li.InvoiceCadence == types.InvoiceCadenceAdvance {
			return li, nil
		}
	}
	return types.SubscriptionSubscriptionLineItem{}, f.fail("modify_read_sub", map[string]string{"subscription_id": subID}, "subscription has no open in-advance fixed line item")
}

// lineItemOpen reports an un-ended line item; the API sends an open end_date as the zero time.
func lineItemOpen(li types.SubscriptionSubscriptionLineItem) bool {
	return li.EndDate == nil || li.EndDate.IsZero()
}

func subscriptionLineItems(resp *dtos.GetSubscriptionResponse) []types.SubscriptionSubscriptionLineItem {
	if resp == nil || resp.SubscriptionResponse == nil {
		return nil
	}
	return resp.SubscriptionResponse.LineItems
}

// addonAutoCharge attaches an in-advance addon with prorations and checkout, and
// asserts it stays pending until the payment completes, then turns active.
func (p *PaymentAutoChargeProbe) addonAutoCharge(ctx context.Context, f *paymentFlow, subID string) error {
	addonID, err := p.ensureAddon(ctx, f)
	if err != nil {
		return err
	}
	ids := map[string]string{"subscription_id": subID, "addon_id": addonID}

	resp, err := p.client.Payments().AddSubscriptionAddon(ctx, types.AddAddonRequest{
		SubscriptionID:    subID,
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring.ToPointer(),
		ProrationBehavior: types.ProrationBehaviorCreateProrations.ToPointer(),
		Checkout:          f.checkoutParams(types.CollectionMethodChargeAutomatically, false),
	})
	if err != nil {
		return f.fail("addon_autocharge_start", ids, "attach addon: %w", err)
	}
	if resp == nil || resp.AddAddonToSubscriptionResponse == nil || resp.AddAddonToSubscriptionResponse.CheckoutSession == nil {
		return f.fail("addon_autocharge_assert_gated", ids, "prorated in-advance addon was attached without a checkout session")
	}
	assoc := resp.AddAddonToSubscriptionResponse
	ids["association_id"] = derefStr(assoc.ID)
	if assoc.AddonStatus == nil || *assoc.AddonStatus != types.AddonStatusPending {
		return f.fail("addon_autocharge_assert_pending", ids, "addon is %v before payment, want pending", derefAddonStatus(assoc.AddonStatus))
	}
	sessionID := derefStr(assoc.CheckoutSession.ID)

	final, err := f.waitTerminal(ctx, "addon_autocharge_session", sessionID)
	if err != nil {
		return err
	}
	if err := f.expectCompleted("addon_autocharge_session", final); err != nil {
		return err
	}

	list, err := p.client.Payments().GetSubscriptionAddonAssociations(ctx, subID)
	if err != nil {
		return f.fail("addon_autocharge_read", ids, "list addon associations: %w", err)
	}
	if list != nil && list.ListAddonAssociationsResponse != nil {
		for _, a := range list.ListAddonAssociationsResponse.Items {
			if derefStr(a.ID) == derefStr(assoc.ID) && a.AddonStatus != nil && *a.AddonStatus == types.AddonStatusActive {
				return nil
			}
		}
	}
	return f.fail("addon_autocharge_assert_active", ids, "addon association not active after the paid attach")
}

// ensureAddon returns the per-currency payments addon, creating it with a single
// in-advance fixed price so a prorated attach charges something.
func (p *PaymentAutoChargeProbe) ensureAddon(ctx context.Context, f *paymentFlow) (string, error) {
	lookupKey := "e2eprobe_payments_addon_" + strings.ToLower(f.opts.Provider.Currency)
	ids := map[string]string{"addon_lookup_key": lookupKey}

	addonID := ""
	got, err := p.client.Payments().GetAddonByLookupKey(ctx, lookupKey)
	switch {
	case err != nil && !isNotFound(err):
		return "", f.fail("ensure_addon", ids, "get addon: %w", err)
	case err == nil && got != nil && got.AddonResponse != nil:
		addonID = derefStr(got.AddonResponse.ID)
	}
	if addonID == "" {
		created, err := p.client.Payments().CreateAddon(ctx, types.CreateAddonRequest{
			Name:        "E2EProbe Payments Addon " + f.opts.Provider.Currency,
			LookupKey:   lookupKey,
			Description: strPtr("In-advance addon the e2eprobe payment probes attach"),
			Metadata:    map[string]any{"e2eprobe": "true", "e2eprobe_role": "seed"},
		})
		if err != nil {
			return "", f.fail("ensure_addon", ids, "create addon: %w", err)
		}
		if created == nil || created.CreateAddonResponse == nil || created.CreateAddonResponse.ID == nil {
			return "", f.fail("ensure_addon", ids, "create addon: empty response")
		}
		addonID = *created.CreateAddonResponse.ID
	}
	ids["addon_id"] = addonID

	entityType := types.PriceEntityTypeAddon
	prices, err := p.client.Prices().Query(ctx, types.PriceFilter{EntityIds: []string{addonID}, EntityType: &entityType})
	if err != nil {
		return "", f.fail("ensure_addon_price", ids, "query addon prices: %w", err)
	}
	if prices != nil && prices.ListPricesResponse != nil && len(prices.ListPricesResponse.Items) > 0 {
		return addonID, nil
	}
	if _, err := p.client.Prices().Create(ctx, types.CreatePriceRequest{
		EntityID:           addonID,
		EntityType:         types.PriceEntityTypeAddon,
		Type:               types.PriceTypeFixed,
		BillingModel:       types.BillingModelFlatFee,
		BillingPeriod:      types.BillingPeriodMonthly,
		BillingPeriodCount: int64Ptr(1),
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		PriceUnitType:      types.PriceUnitTypeFiat,
		Amount:             strPtr(paymentAddonPrice.StringFixed(2)),
		Currency:           f.opts.Provider.Currency,
		DisplayName:        strPtr("E2EProbe Payments Addon Fee"),
	}); err != nil {
		return "", f.fail("ensure_addon_price", ids, "create addon price: %w", err)
	}
	return addonID, nil
}

// refundPaidInvoice credit-notes the full paid invoice back to source and
// asserts the invoice is refunded and the money settles: to the card on gateways
// that refund, to the customer's wallet everywhere else.
func (p *PaymentAutoChargeProbe) refundPaidInvoice(ctx context.Context, f *paymentFlow, invoiceID string) error {
	ids := map[string]string{"invoice_id": invoiceID}
	inv, err := p.client.Invoices().Get(ctx, invoiceID)
	if err != nil {
		return f.fail("refund_read_invoice", ids, "get invoice: %w", err)
	}
	if inv == nil || inv.InvoiceResponse == nil || len(inv.InvoiceResponse.LineItems) == 0 {
		return f.fail("refund_read_invoice", ids, "paid invoice has no line items to credit")
	}
	lineItemID := derefStr(inv.InvoiceResponse.LineItems[0].ID)

	before, err := f.credits(ctx)
	if err != nil {
		return err
	}
	cn, err := p.client.Payments().CreateCreditNote(ctx, types.CreateCreditNoteRequest{
		InvoiceID:         invoiceID,
		Reason:            types.CreditNoteReasonServiceIssue,
		ProcessCreditNote: boolPtr(true),
		RefundTarget:      types.RefundTargetBackToSource.ToPointer(),
		IdempotencyKey:    strPtr(fmt.Sprintf("e2eprobe-refund-%s-%d", f.runID, time.Now().UnixNano())),
		Memo:              strPtr("e2eprobe payments refund"),
		LineItems: []types.CreateCreditNoteLineItemRequest{{
			InvoiceLineItemID: lineItemID,
			Amount:            paymentInvoiceAmount.StringFixed(2),
			// Optional in the API, but the server fails with a 500 without it.
			DisplayName: strPtr("E2EProbe refund"),
		}},
	})
	if err != nil {
		return f.fail("refund_create_credit_note", ids, "create credit note: %w", err)
	}
	if cn == nil || cn.CreditNoteResponse == nil || cn.CreditNoteResponse.ID == nil {
		return f.fail("refund_create_credit_note", ids, "create credit note: empty response")
	}
	creditNoteID := *cn.CreditNoteResponse.ID
	ids["credit_note_id"] = creditNoteID

	after, err := p.client.Invoices().Get(ctx, invoiceID)
	if err != nil {
		return f.fail("refund_read_invoice_after", ids, "get invoice: %w", err)
	}
	if after == nil || after.InvoiceResponse == nil {
		return f.fail("refund_read_invoice_after", ids, "get invoice: empty response")
	}
	if status := derefPaymentStatus(after.InvoiceResponse.PaymentStatus); status != types.PaymentStatusRefunded {
		ids["payment_status"] = string(status)
		return f.fail("refund_assert_invoice_refunded", ids, "invoice payment_status is %s after a full refund, want REFUNDED", status)
	}

	refunds, err := p.waitRefundsSettled(ctx, f, creditNoteID)
	if err != nil {
		return err
	}
	toSource := gatewaysRefundingToSource[f.provider()]
	settled := decimal.Zero
	for _, r := range refunds {
		if derefRefundStatus(r.RefundStatus) != types.RefundStatusSucceeded {
			continue
		}
		dest := derefRefundDestination(r.RefundDestination)
		if toSource && dest != types.RefundDestinationGateway {
			ids["refund_destination"] = string(dest)
			return f.fail("refund_assert_to_source", ids, "%s supports refunds but the refund settled to %s, want GATEWAY", f.provider(), dest)
		}
		if toSource && derefStr(r.GatewayRefundID) == "" {
			return f.fail("refund_assert_to_source", ids, "gateway refund settled without a gateway_refund_id")
		}
		amt, _ := decimal.NewFromString(derefStr(r.Amount))
		settled = settled.Add(amt)
	}
	if !settled.Equal(paymentInvoiceAmount) {
		ids["settled"] = settled.String()
		return f.fail("refund_assert_settled", ids, "refunds settled %s, want %s", settled, paymentInvoiceAmount)
	}
	if !toSource {
		return f.expectCredits(ctx, "refund_assert_wallet_credit", before.Add(paymentInvoiceAmount))
	}
	return nil
}

// waitRefundsSettled polls the credit note's refunds until none is still in flight.
func (p *PaymentAutoChargeProbe) waitRefundsSettled(ctx context.Context, f *paymentFlow, creditNoteID string) ([]types.RefundResponse, error) {
	ids := map[string]string{"credit_note_id": creditNoteID}
	deadline := time.Now().Add(f.opts.settleTimeout())
	for {
		resp, err := p.client.Payments().ListRefunds(ctx, dtos.ListRefundsRequest{CreditNoteIds: []string{creditNoteID}})
		if err != nil {
			return nil, f.fail("refund_list", ids, "list refunds: %w", err)
		}
		var items []types.RefundResponse
		if resp != nil && resp.ListRefundsResponse != nil {
			items = resp.ListRefundsResponse.Items
		}
		inFlight := len(items) == 0
		for _, r := range items {
			switch derefRefundStatus(r.RefundStatus) {
			case types.RefundStatusPending, types.RefundStatusProcessing:
				inFlight = true
			}
		}
		if !inFlight {
			return items, nil
		}
		if time.Now().After(deadline) {
			return nil, f.fail("refund_assert_settles", ids, "refunds still in flight after %s (%d rows)", f.opts.settleTimeout(), len(items))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.opts.pollInterval()):
		}
	}
}

func derefAddonStatus(s *types.AddonStatus) types.AddonStatus {
	if s == nil {
		return ""
	}
	return *s
}

func derefRefundStatus(s *types.RefundStatus) types.RefundStatus {
	if s == nil {
		return ""
	}
	return *s
}

func derefRefundDestination(d *types.RefundDestination) types.RefundDestination {
	if d == nil {
		return ""
	}
	return *d
}
