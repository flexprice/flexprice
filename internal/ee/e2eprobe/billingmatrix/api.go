package billingmatrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/shopspring/decimal"
)

// API sends exact JSON to the Flexprice API; e2eprobe.RawOps satisfies it.
type API interface {
	Do(ctx context.Context, method, path string, body, out any) error
}

// statusOf extracts the HTTP status of an API error, or 0 when err is not an HTTP error.
func statusOf(err error) int {
	var er *sdkerrors.ErrorResponse
	if errors.As(err, &er) && er.HTTPStatusCode != nil {
		return int(*er.HTTPStatusCode)
	}
	var ae *sdkerrors.APIError
	if errors.As(err, &ae) {
		return ae.StatusCode
	}
	return 0
}

func isClientError(err error) bool {
	s := statusOf(err)
	return s >= 400 && s < 500
}

type idOnly struct {
	ID string `json:"id"`
}

type lineItem struct {
	ID                 string          `json:"id"`
	PriceID            string          `json:"price_id"`
	DisplayName        string          `json:"display_name"`
	EntityType         string          `json:"entity_type"`
	BillingPeriod      string          `json:"billing_period"`
	BillingPeriodCount int             `json:"billing_period_count"`
	InvoiceCadence     string          `json:"invoice_cadence"`
	Quantity           decimal.Decimal `json:"quantity"`
	StartDate          time.Time       `json:"start_date"`
	EndDate            *time.Time      `json:"end_date"`
}

type invoiceLine struct {
	ID                     string          `json:"id"`
	PriceID                *string         `json:"price_id"`
	SubscriptionLineItemID *string         `json:"subscription_line_item_id"`
	Amount                 decimal.Decimal `json:"amount"`
	Quantity               decimal.Decimal `json:"quantity"`
	PeriodStart            *time.Time      `json:"period_start"`
	PeriodEnd              *time.Time      `json:"period_end"`
	DisplayName            *string         `json:"display_name"`
}

type invoice struct {
	ID            string          `json:"id"`
	InvoiceType   string          `json:"invoice_type"`
	InvoiceStatus string          `json:"invoice_status"`
	BillingReason string          `json:"billing_reason"`
	Subtotal      decimal.Decimal `json:"subtotal"`
	AmountDue     decimal.Decimal `json:"amount_due"`
	PeriodStart   *time.Time      `json:"period_start"`
	PeriodEnd     *time.Time      `json:"period_end"`
	LineItems     []invoiceLine   `json:"line_items"`
}

// byPrice sums line amounts per price and counts the lines.
func (inv *invoice) byPrice() (map[string]decimal.Decimal, map[string]int) {
	sum, n := map[string]decimal.Decimal{}, map[string]int{}
	if inv == nil {
		return sum, n
	}
	for _, l := range inv.LineItems {
		if l.PriceID == nil {
			continue
		}
		sum[*l.PriceID] = sum[*l.PriceID].Add(l.Amount)
		n[*l.PriceID]++
	}
	return sum, n
}

type subscriptionResp struct {
	ID                 string     `json:"id"`
	CustomerID         string     `json:"customer_id"`
	Status             string     `json:"subscription_status"`
	BillingAnchor      time.Time  `json:"billing_anchor"`
	StartDate          time.Time  `json:"start_date"`
	CurrentPeriodStart time.Time  `json:"current_period_start"`
	CurrentPeriodEnd   time.Time  `json:"current_period_end"`
	TrialStart         *time.Time `json:"trial_start"`
	TrialEnd           *time.Time `json:"trial_end"`
	Timezone           string     `json:"timezone"`
	LineItems          []lineItem `json:"line_items"`
	LatestInvoice      *invoice   `json:"latest_invoice"`
}

func (s *subscriptionResp) lineItemFor(priceID string) *lineItem {
	for i := range s.LineItems {
		if s.LineItems[i].PriceID == priceID {
			return &s.LineItems[i]
		}
	}
	return nil
}

type walletTxn struct {
	ID                string            `json:"id"`
	Type              string            `json:"type"`
	Amount            decimal.Decimal   `json:"amount"`
	CreditAmount      decimal.Decimal   `json:"credit_amount"`
	TransactionReason string            `json:"transaction_reason"`
	Metadata          map[string]string `json:"metadata"`
	CreatedAt         time.Time         `json:"created_at"`
}

type changedInvoice struct {
	ID                string     `json:"id"`
	Action            string     `json:"action"`
	Status            string     `json:"status"`
	Invoice           *invoice   `json:"invoice"`
	WalletTransaction *walletTxn `json:"wallet_transaction"`
}

type changedAddonAssociation struct {
	ID           string    `json:"id"`
	AddonID      string    `json:"addon_id"`
	StartDate    time.Time `json:"start_date"`
	ChangeAction string    `json:"change_action"`
}

type changedLineItem struct {
	ID           string     `json:"id"`
	PriceID      string     `json:"price_id"`
	StartDate    *time.Time `json:"start_date"`
	EndDate      *time.Time `json:"end_date"`
	ChangeAction string     `json:"change_action"`
}

type modifyResp struct {
	ChangedResources struct {
		LineItems         []changedLineItem         `json:"line_items"`
		AddonAssociations []changedAddonAssociation `json:"addon_associations"`
		Invoices          []changedInvoice          `json:"invoices"`
	} `json:"changed_resources"`
}

// charges sums created charge invoices; credits sums wallet credits.
func (m *modifyResp) charges() (decimal.Decimal, []*invoice) {
	total, out := decimal.Zero, []*invoice{}
	for _, c := range m.ChangedResources.Invoices {
		if c.Invoice != nil && c.Action != "wallet_credit" {
			total = total.Add(c.Invoice.Subtotal)
			out = append(out, c.Invoice)
		}
	}
	return total, out
}

func (m *modifyResp) credits() decimal.Decimal {
	total := decimal.Zero
	for _, c := range m.ChangedResources.Invoices {
		if c.Action == "wallet_credit" && c.WalletTransaction != nil {
			total = total.Add(c.WalletTransaction.Amount)
		}
	}
	return total
}

type prorationDetail struct {
	LineItemID   string          `json:"line_item_id"`
	PriceID      string          `json:"price_id"`
	CreditAmount decimal.Decimal `json:"credit_amount"`
	ChargeAmount decimal.Decimal `json:"charge_amount"`
}

type cancelResp struct {
	EffectiveDate     time.Time         `json:"effective_date"`
	Status            string            `json:"status"`
	ProrationDetails  []prorationDetail `json:"proration_details"`
	TotalCreditAmount decimal.Decimal   `json:"total_credit_amount"`
}

type allowance struct {
	EntitlementID string          `json:"entitlement_id"`
	Quota         decimal.Decimal `json:"quota"`
	ValidFrom     *time.Time      `json:"valid_from"`
	ValidTo       *time.Time      `json:"valid_to"`
	IsActive      bool            `json:"is_active"`
}

type subEntitlements struct {
	Features []struct {
		Feature struct {
			ID string `json:"id"`
		} `json:"feature"`
		Entitlement struct {
			GrantState *struct {
				Allowances []allowance `json:"allowances"`
			} `json:"grant_state"`
		} `json:"entitlement"`
	} `json:"features"`
}

type creditGrantApplication struct {
	ID           string          `json:"id"`
	ScheduledFor time.Time       `json:"scheduled_for"`
	PeriodStart  *time.Time      `json:"period_start"`
	PeriodEnd    *time.Time      `json:"period_end"`
	Credits      decimal.Decimal `json:"credits"`
	Status       string          `json:"application_status"`
}

// client is the typed surface the scenarios use; creates are recorded in track for cleanup.
type client struct {
	api   API
	track *tracker
}

func (c client) post(ctx context.Context, path string, body, out any) error {
	return c.api.Do(ctx, http.MethodPost, path, body, out)
}

func (c client) get(ctx context.Context, path string, out any) error {
	return c.api.Do(ctx, http.MethodGet, path, nil, out)
}

// decodeItems decodes either a bare array or a {"items": [...]} page into out.
func decodeItems(raw json.RawMessage, out any) error {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		return json.Unmarshal(raw, out)
	}
	var page struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return err
	}
	if len(page.Items) == 0 {
		return nil
	}
	return json.Unmarshal(page.Items, out)
}

func (c client) createCustomer(ctx context.Context, ext, tz, runID, role string) (string, error) {
	var out idOnly
	err := c.post(ctx, "/customers", map[string]any{
		"external_id":              ext,
		"name":                     "E2EProbe Billing Matrix",
		"timezone":                 tz,
		"skip_onboarding_workflow": true,
		"metadata": map[string]string{
			"e2eprobe": "true", "e2eprobe_cohort": "ephemeral", "e2eprobe_role": role, "e2eprobe_run_id": runID,
		},
	}, &out)
	c.track.add(kindCustomer, out.ID)
	return out.ID, err
}

func (c client) createPlan(ctx context.Context, name string) (string, error) {
	var out idOnly
	err := c.post(ctx, "/plans", map[string]any{"name": name, "metadata": map[string]string{"e2eprobe": "true"}}, &out)
	c.track.add(kindPlan, out.ID)
	return out.ID, err
}

func (c client) createAddon(ctx context.Context, name, lookupKey string) (string, error) {
	var out idOnly
	err := c.post(ctx, "/addons", map[string]any{"name": name, "lookup_key": lookupKey}, &out)
	c.track.add(kindAddon, out.ID)
	return out.ID, err
}

func (c client) createPrice(ctx context.Context, body map[string]any) (string, error) {
	var out idOnly
	err := c.post(ctx, "/prices", body, &out)
	c.track.add(kindPrice, out.ID)
	return out.ID, err
}

func (c client) createSubscription(ctx context.Context, body map[string]any) (*subscriptionResp, error) {
	var out subscriptionResp
	if err := c.post(ctx, "/subscriptions", body, &out); err != nil {
		return nil, err
	}
	c.track.add(kindSubscription, out.ID)
	return &out, nil
}

func (c client) getSubscription(ctx context.Context, id string) (*subscriptionResp, error) {
	var out subscriptionResp
	if err := c.get(ctx, "/subscriptions/"+url.PathEscape(id), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c client) preview(ctx context.Context, subID string, p *span) (*invoice, error) {
	body := map[string]any{"subscription_id": subID}
	if p != nil {
		body["period_start"] = p.start.UTC().Format(time.RFC3339)
		body["period_end"] = p.end.UTC().Format(time.RFC3339)
	}
	var out invoice
	if err := c.post(ctx, "/invoices/preview", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c client) subscriptionInvoices(ctx context.Context, subID string) ([]invoice, error) {
	var raw json.RawMessage
	if err := c.post(ctx, "/invoices/search", map[string]any{"subscription_id": subID, "limit": 100}, &raw); err != nil {
		return nil, err
	}
	var out []invoice
	return out, decodeItems(raw, &out)
}

func (c client) modify(ctx context.Context, subID string, body map[string]any, preview bool) (*modifyResp, error) {
	op := "execute"
	if preview {
		op = "preview"
	}
	var out modifyResp
	if err := c.post(ctx, fmt.Sprintf("/subscriptions/%s/modify/%s", url.PathEscape(subID), op), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c client) cancel(ctx context.Context, subID string, body map[string]any) (*cancelResp, error) {
	var out cancelResp
	if err := c.post(ctx, "/subscriptions/"+url.PathEscape(subID)+"/cancel", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c client) walletCredits(ctx context.Context, customerID string) ([]walletTxn, error) {
	var raw json.RawMessage
	if err := c.get(ctx, "/customers/"+url.PathEscape(customerID)+"/wallets", &raw); err != nil {
		return nil, err
	}
	var wallets []idOnly
	if err := decodeItems(raw, &wallets); err != nil {
		return nil, err
	}
	var all []walletTxn
	for _, w := range wallets {
		var txRaw json.RawMessage
		if err := c.get(ctx, "/wallets/"+url.PathEscape(w.ID)+"/transactions?limit=100", &txRaw); err != nil {
			return nil, err
		}
		var txns []walletTxn
		if err := decodeItems(txRaw, &txns); err != nil {
			return nil, err
		}
		all = append(all, txns...)
	}
	return all, nil
}

func (c client) upcomingGrants(ctx context.Context, subID string) ([]creditGrantApplication, error) {
	var raw json.RawMessage
	if err := c.get(ctx, "/subscriptions/"+url.PathEscape(subID)+"/grants/upcoming", &raw); err != nil {
		return nil, err
	}
	var out []creditGrantApplication
	return out, decodeItems(raw, &out)
}

func (c client) subscriptionEntitlements(ctx context.Context, subID, featureID string) (*subEntitlements, error) {
	var out subEntitlements
	path := "/subscriptions/" + url.PathEscape(subID) + "/entitlements?feature_ids=" + url.QueryEscape(featureID)
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
