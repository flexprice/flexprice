package scenarios

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
	itypes "github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// sharedTaxRate is the fixture tax rate's percentage, 10%.
var sharedTaxRate = decimal.RequireFromString("0.10")

// ── invoice-tax ───────────────────────────────────────────────────────

// invoiceTax checks the newest finalized cycle invoice carries the shared tax
// rate. Tax is stamped at finalize and never backfilled, so invoices finalized
// before the association was active are left out.
type invoiceTax struct{}

func newInvoiceTax(scenario.Deps) *invoiceTax { return &invoiceTax{} }

func (*invoiceTax) Name() string               { return "invoice-tax" }
func (*invoiceTax) Kind() e2eprobe.Kind        { return e2eprobe.KindProbe }
func (*invoiceTax) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitTax} }

func (*invoiceTax) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	p := sc.Persona()
	if gap := p.Gap(e2eprobe.TraitTax); gap != "" {
		return sc.Skip("gap", gap)
	}
	activeFrom := p.TaxActiveFrom()
	if activeFrom.IsZero() {
		return sc.Skip("activation", "tax association activation time unknown")
	}
	sc.Attr("subscription_id", p.SubscriptionID())
	invoices, err := subscriptionInvoices(ctx, sc, p.SubscriptionID(), types.InvoiceStatusFinalized)
	if err != nil {
		return sc.Fail("invoices", err)
	}
	var taxable []types.InvoiceResponse
	for _, inv := range invoices {
		if taxableSince(inv, activeFrom) {
			taxable = append(taxable, inv)
		}
	}
	listed := latestInvoice(taxable)
	if listed == nil || listed.ID == nil {
		return sc.Skip("no_invoice", "no finalized invoice since the tax association became active")
	}
	sc.Attr("invoice_id", *listed.ID)
	// Search responses omit tax_applied; only GET carries Taxes.
	got, err := sc.Client().Invoices().Get(ctx, *listed.ID)
	if err != nil {
		return sc.Fail("invoice", err)
	}
	inv := listed
	if got.InvoiceResponse != nil {
		inv = got.InvoiceResponse
	}
	fx := sc.World().Fixtures()
	var applied *types.TaxAppliedResponse
	for i := range inv.Taxes {
		if taxMatches(inv.Taxes[i], fx) {
			applied = &inv.Taxes[i]
			break
		}
	}
	sc.Truth("latest finalized invoice carries the shared tax rate", applied != nil)
	if applied != nil {
		sc.Money("tax amount", invoiceDecimal(applied.TaxableAmount).Mul(sharedTaxRate), invoiceDecimal(applied.TaxAmount))
	}
	return sc.Result("assert_tax")
}

// taxableSince reports whether the invoice was finalized once the association
// was active; with no timestamp it is taken to predate it.
func taxableSince(inv types.InvoiceResponse, activeFrom time.Time) bool {
	stamped := inv.FinalizedAt
	if stamped == nil {
		stamped = inv.CreatedAt
	}
	return stamped != nil && !stamped.Before(activeFrom)
}

// taxMatches accepts the shared rate by id or by code.
func taxMatches(tx types.TaxAppliedResponse, fx *world.Fixtures) bool {
	if fx.TaxRateID() != "" && tx.TaxRateID != nil && *tx.TaxRateID == fx.TaxRateID() {
		return true
	}
	return tx.TaxRate != nil && tx.TaxRate.Code != nil && *tx.TaxRate.Code == fx.TaxRateCode()
}

// ── invoice-coupon ────────────────────────────────────────────────────

// invoiceCoupon checks the first cycle invoice references the shared coupon.
// The coupon is ONCE cadence, so only the oldest invoice can carry it.
type invoiceCoupon struct{}

func newInvoiceCoupon(scenario.Deps) *invoiceCoupon { return &invoiceCoupon{} }

func (*invoiceCoupon) Name() string               { return "invoice-coupon" }
func (*invoiceCoupon) Kind() e2eprobe.Kind        { return e2eprobe.KindProbe }
func (*invoiceCoupon) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitCoupon} }

func (*invoiceCoupon) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	p := sc.Persona()
	if gap := p.Gap(e2eprobe.TraitCoupon); gap != "" {
		return sc.Skip("gap", gap)
	}
	sc.Attr("subscription_id", p.SubscriptionID())
	inv, err := oldestInvoice(ctx, sc, p.SubscriptionID())
	if err != nil {
		return sc.Fail("invoices", err)
	}
	if inv == nil {
		return sc.Skip("no_invoice", "no invoice for the subscription yet")
	}
	if inv.ID != nil {
		sc.Attr("invoice_id", *inv.ID)
	}
	couponID := sc.World().Fixtures().CouponID()
	found := false
	for _, ca := range inv.CouponApplications {
		if ca.CouponID != nil && *ca.CouponID == couponID {
			found = true
		}
	}
	sc.Truth("oldest invoice references the shared coupon", found)
	return sc.Result("assert_coupon")
}

// oldestInvoice asks for the subscription's first invoice. The server-side
// subscription filter has returned nothing for subscriptions that do have
// invoices, so an empty page falls back to the customer's invoices filtered here.
func oldestInvoice(ctx context.Context, sc *scenario.Context, subID string) (*types.InvoiceResponse, error) {
	order, limit := types.InvoiceFilterOrderAsc, int64(1)
	resp, err := sc.Client().Invoices().Query(ctx, types.InvoiceFilter{SubscriptionID: &subID, Order: &order, Limit: &limit})
	if err != nil {
		return nil, err
	}
	if items := invoiceItems(resp); len(items) > 0 {
		return &items[0], nil
	}
	customerID := sc.Persona().CustomerID()
	resp, err = sc.Client().Invoices().Query(ctx, types.InvoiceFilter{CustomerID: &customerID, Order: &order})
	if err != nil {
		return nil, err
	}
	items := invoiceItems(resp)
	for i := range items {
		if items[i].SubscriptionID != nil && *items[i].SubscriptionID == subID {
			return &items[i], nil
		}
	}
	return nil, nil
}

// ── invoice-commitment ────────────────────────────────────────────────

// invoiceCommitment checks the newest finalized invoice honours the persona's
// commitment: under it, a true-up line lifts billed usage to the commitment;
// over it, the total covers the commitment.
type invoiceCommitment struct{}

func newInvoiceCommitment(scenario.Deps) *invoiceCommitment { return &invoiceCommitment{} }

func (*invoiceCommitment) Name() string        { return "invoice-commitment" }
func (*invoiceCommitment) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }
func (*invoiceCommitment) Requires() []e2eprobe.Trait {
	return []e2eprobe.Trait{e2eprobe.TraitCommitment}
}

func (*invoiceCommitment) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	p := sc.Persona()
	if gap := p.Gap(e2eprobe.TraitCommitment); gap != "" {
		return sc.Skip("gap", gap)
	}
	spec := p.Spec().Commitment
	if spec == nil {
		return sc.Skip("spec", "persona declares no commitment amount")
	}
	commitment, err := decimal.NewFromString(spec.Amount)
	if err != nil {
		return sc.Fail("spec", fmt.Errorf("commitment amount %q: %w", spec.Amount, err))
	}
	subID := p.SubscriptionID()
	sc.Attr("subscription_id", subID)
	invoices, err := subscriptionInvoices(ctx, sc, subID, types.InvoiceStatusFinalized)
	if err != nil {
		return sc.Fail("invoices", err)
	}
	inv := latestInvoice(invoices)
	if inv == nil {
		return sc.Skip("no_invoice", "no finalized invoice for the subscription yet")
	}
	if inv.ID != nil {
		sc.Attr("invoice_id", *inv.ID)
	}
	usage, trueUp, hasTrueUp := commitmentParts(inv.LineItems)
	sc.Attr("usage", usage.StringFixed(2))
	sc.Attr("true_up", trueUp.StringFixed(2))
	if usage.GreaterThanOrEqual(commitment) {
		total := invoiceTotal(*inv)
		sc.Truth(fmt.Sprintf("invoice total %s covers the commitment %s", total.StringFixed(2), commitment.StringFixed(2)), total.GreaterThanOrEqual(commitment))
		return sc.Result("assert_commitment")
	}
	// The server bills no true-up unless the subscription enabled it at create.
	sub, err := subscriptionOf(ctx, sc, subID)
	if err != nil {
		return sc.Fail("subscription", err)
	}
	if sub.EnableTrueUp == nil || !*sub.EnableTrueUp {
		return sc.Skip("true_up", "subscription was created without enable_true_up, so an under-commitment invoice carries no true-up line")
	}
	sc.Truth("under-commitment invoice carries a true-up line", hasTrueUp)
	sc.Money("usage plus true-up", commitment, usage.Add(trueUp))
	return sc.Result("assert_commitment")
}

// commitmentParts splits line items into billed usage and true-up. A
// subscription-level true-up is its own FIXED line flagged is_commitment_trueup;
// a line-level commitment folds it into the usage line's CommitmentInfo instead.
func commitmentParts(items []types.InvoiceLineItemResponse) (usage, trueUp decimal.Decimal, hasTrueUp bool) {
	for _, li := range items {
		amount := invoiceDecimal(li.Amount)
		if isTrueUpLine(li) {
			trueUp = trueUp.Add(amount)
			hasTrueUp = true
			continue
		}
		if li.PriceType == nil || *li.PriceType != string(types.PriceTypeUsage) {
			continue
		}
		folded := decimal.Zero
		if li.CommitmentInfo != nil {
			folded = invoiceDecimal(li.CommitmentInfo.ComputedTrueUpAmount)
		}
		if folded.IsPositive() {
			hasTrueUp = true
		}
		usage = usage.Add(amount.Sub(folded))
		trueUp = trueUp.Add(folded)
	}
	return usage, trueUp, hasTrueUp
}

// isTrueUpLine matches the server's true-up line by its metadata flag, or by
// the "True Up" display name older invoices carry.
func isTrueUpLine(li types.InvoiceLineItemResponse) bool {
	if li.Metadata[itypes.MetadataKeyIsCommitmentTrueup] == itypes.MetadataValueTrue {
		return true
	}
	if li.DisplayName == nil {
		return false
	}
	name := strings.ToLower(*li.DisplayName)
	return strings.Contains(name, "true up") || strings.Contains(name, "true-up")
}

// ── invoice-multi-cadence ─────────────────────────────────────────────

// invoiceMultiCadence checks the quarterly subscription's newest invoice fans
// its monthly prices out: every source line item must bill once per month of
// the cycle.
type invoiceMultiCadence struct{}

func newInvoiceMultiCadence(scenario.Deps) *invoiceMultiCadence { return &invoiceMultiCadence{} }

func (*invoiceMultiCadence) Name() string        { return "invoice-multi-cadence" }
func (*invoiceMultiCadence) Kind() e2eprobe.Kind { return e2eprobe.KindProbe }
func (*invoiceMultiCadence) Requires() []e2eprobe.Trait {
	return []e2eprobe.Trait{e2eprobe.TraitMultiCadence}
}

func (*invoiceMultiCadence) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	p := sc.Persona()
	if gap := p.Gap(e2eprobe.TraitMultiCadence); gap != "" {
		return sc.Skip("gap", gap)
	}
	subID := p.MultiCadenceSubID()
	if subID == "" {
		return sc.Skip("subscription", "persona has no quarterly subscription yet")
	}
	sc.Attr("subscription_id", subID)
	sub, err := subscriptionOf(ctx, sc, subID)
	switch {
	case world.IsNotFound(err):
		return sc.Skip("subscription", "quarterly subscription not found yet")
	case err != nil:
		return sc.Fail("subscription", err)
	}
	months := monthsPerCycle(sub)
	invoices, err := subscriptionInvoices(ctx, sc, subID)
	if err != nil {
		return sc.Fail("invoices", err)
	}
	inv := latestInvoice(invoices)
	if inv == nil {
		return sc.Skip("no_invoice", "no invoice for the quarterly subscription yet")
	}
	if inv.ID != nil {
		sc.Attr("invoice_id", *inv.ID)
	}
	sc.Attr("line_items", strconv.Itoa(len(inv.LineItems)))
	sc.Truth("latest invoice has line items", len(inv.LineItems) > 0)
	bySource := lineItemsBySource(inv.LineItems)
	sources := make([]string, 0, len(bySource))
	for source := range bySource {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		sc.Truth(fmt.Sprintf("%s bills at least %d line items for %d months (got %d)", source, months, months, bySource[source]), bySource[source] >= months)
	}
	return sc.Result("assert_fan_out")
}

// monthsPerCycle is how many monthly plan prices one billing cycle spans.
func monthsPerCycle(sub types.SubscriptionResponse) int {
	months := int64(1)
	if sub.BillingPeriod != nil {
		switch *sub.BillingPeriod {
		case types.BillingPeriodQuarterly:
			months = 3
		case types.BillingPeriodHalfYearly:
			months = 6
		case types.BillingPeriodAnnual:
			months = 12
		}
	}
	return int(months * periodCount(sub))
}

// lineItemsBySource counts invoice lines per subscription line item, falling
// back to the price and then to the invoice as a whole.
func lineItemsBySource(items []types.InvoiceLineItemResponse) map[string]int {
	out := map[string]int{}
	for _, li := range items {
		switch {
		case li.SubscriptionLineItemID != nil && *li.SubscriptionLineItemID != "":
			out["subscription line item "+*li.SubscriptionLineItemID]++
		case li.PriceID != nil && *li.PriceID != "":
			out["price "+*li.PriceID]++
		default:
			out["invoice"]++
		}
	}
	return out
}

// ── shared ────────────────────────────────────────────────────────────

// subscriptionOf reads one subscription; a nil body reads as empty.
func subscriptionOf(ctx context.Context, sc *scenario.Context, subID string) (types.SubscriptionResponse, error) {
	resp, err := sc.Client().Subscriptions().Get(ctx, subID)
	if err != nil {
		return types.SubscriptionResponse{}, err
	}
	if resp == nil || resp.SubscriptionResponse == nil {
		return types.SubscriptionResponse{}, nil
	}
	return *resp.SubscriptionResponse, nil
}

// subscriptionInvoices lists the persona's invoices of one subscription, in
// the given statuses (any when none). The query is by customer and filtered
// here: the server-side subscription filter has returned nothing for
// subscriptions that do have invoices.
func subscriptionInvoices(ctx context.Context, sc *scenario.Context, subID string, statuses ...types.InvoiceStatus) ([]types.InvoiceResponse, error) {
	customerID := sc.Persona().CustomerID()
	resp, err := sc.Client().Invoices().Query(ctx, types.InvoiceFilter{CustomerID: &customerID, InvoiceStatus: statuses})
	if err != nil {
		return nil, err
	}
	var out []types.InvoiceResponse
	for _, inv := range invoiceItems(resp) {
		if inv.SubscriptionID != nil && *inv.SubscriptionID == subID {
			out = append(out, inv)
		}
	}
	return out, nil
}

func invoiceItems(resp *dtos.QueryInvoiceResponse) []types.InvoiceResponse {
	if resp == nil || resp.ListInvoicesResponse == nil {
		return nil
	}
	return resp.ListInvoicesResponse.Items
}

// latestInvoice is the invoice with the latest period end, else creation time.
func latestInvoice(items []types.InvoiceResponse) *types.InvoiceResponse {
	var best *types.InvoiceResponse
	for i := range items {
		if best == nil || invoiceTime(items[i]).After(invoiceTime(*best)) {
			best = &items[i]
		}
	}
	return best
}

// invoiceTime orders invoices by period end, falling back to creation time.
func invoiceTime(inv types.InvoiceResponse) time.Time {
	switch {
	case inv.PeriodEnd != nil:
		return *inv.PeriodEnd
	case inv.CreatedAt != nil:
		return *inv.CreatedAt
	}
	return time.Time{}
}

// invoiceTotal prefers total and falls back to amount_due.
func invoiceTotal(inv types.InvoiceResponse) decimal.Decimal {
	if inv.Total != nil {
		return invoiceDecimal(inv.Total)
	}
	return invoiceDecimal(inv.AmountDue)
}

// invoiceDecimal parses a money string; nil or malformed reads as zero.
func invoiceDecimal(s *string) decimal.Decimal {
	if s == nil {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return decimal.Zero
	}
	return d
}
