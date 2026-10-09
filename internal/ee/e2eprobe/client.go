package e2eprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	flexprice "github.com/flexprice/go-sdk/v2"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// GrantEntitlementInput mirrors internal/api/dto/entitlement.go:CreateEntitlementRequest
// for the grant subset that SDK v2.0.24 doesn't cover. When the SDK is
// regenerated with grant fields on CreateEntitlementRequest, delete this
// and use the SDK's typed Create.
type GrantEntitlementInput struct {
	FeatureID          string
	FeatureType        string // "metered"
	PlanID             string
	EntityType         string // "plan"
	EntityID           string // same value as PlanID for plan-level
	IsEnabled          bool
	GrantMeasure       string // "quantity" | "amount"
	GrantQuota         string // decimal string
	GrantDurationValue int
	GrantDurationUnit  string // "hour" | "day" | "week"
	AggregationMode    string // "additive" | "parallel"
}

// GrantEntitlementResponse is the minimal decode of GET /entitlements/{id}
// carrying only the fields the seed's config-echo assertion needs. The
// server's full EntitlementResponse has many more fields; we ignore them.
type GrantEntitlementResponse struct {
	ID                 string `json:"id"`
	FeatureID          string `json:"feature_id"`
	PlanID             string `json:"plan_id"`
	GrantMeasure       string `json:"grant_measure"`
	GrantQuota         string `json:"grant_quota"`
	GrantDurationValue *int   `json:"grant_duration_value"`
	GrantDurationUnit  string `json:"grant_duration_unit"`
	AggregationMode    string `json:"aggregation_mode"`
	IsEnabled          bool   `json:"is_enabled"`
}

type Client interface {
	Customers() CustomerOps
	Plans() PlanOps
	Prices() PriceOps
	Features() FeatureOps
	Subscriptions() SubscriptionOps
	Wallets() WalletOps
	Events() EventOps
	Invoices() InvoiceOps
	NewAsyncEventClient() AsyncEventClient
	Entitlements() EntitlementOps
	Coupons() CouponOps
	CouponAssociations() CouponAssociationOps
	TaxRates() TaxRateOps
	TaxAssociations() TaxAssociationOps
	Payments() PaymentOps
}

type CustomerOps interface {
	Create(ctx context.Context, req types.CreateCustomerRequest) (*dtos.CreateCustomerResponse, error)
	GetByExternalID(ctx context.Context, externalID string) (*dtos.GetCustomerByExternalIDResponse, error)
	Get(ctx context.Context, id string) (*dtos.GetCustomerResponse, error)
	GetEntitlements(ctx context.Context, id string) (*dtos.GetCustomerEntitlementsResponse, error)
	GetUsageSummary(ctx context.Context, req dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error)
	Update(ctx context.Context, body types.UpdateCustomerRequest, id, externalID *string) (*dtos.UpdateCustomerResponse, error)
	Delete(ctx context.Context, id string) (*dtos.DeleteCustomerResponse, error)
	Query(ctx context.Context, filter types.CustomerFilter) (*dtos.QueryCustomerResponse, error)
}

type PlanOps interface {
	Create(ctx context.Context, req types.CreatePlanRequest) (*dtos.CreatePlanResponse, error)
	Query(ctx context.Context, filter types.PlanFilter) (*dtos.QueryPlanResponse, error)
	Get(ctx context.Context, id string) (*dtos.GetPlanResponse, error)

	// SyncPrices pushes prices added to the plan after a subscription was
	// created onto that subscription's line items. Line items snapshot the
	// plan at create time, so a seed meter added later is invisible to every
	// existing subscription — and to every read path that filters usage by
	// active-subscription line items — until this runs.
	SyncPrices(ctx context.Context, planID string) (*dtos.SyncPlanPricesResponse, error)
}

type PriceOps interface {
	Create(ctx context.Context, req types.CreatePriceRequest) (*dtos.CreatePriceResponse, error)
	// CreateBucketed is Create plus bucket_size, which the published SDK's
	// CreatePriceRequest does not carry yet. Falls back to the typed Create
	// when bucketSize is empty.
	CreateBucketed(ctx context.Context, req types.CreatePriceRequest, bucketSize string) (id string, err error)
	Query(ctx context.Context, filter types.PriceFilter) (*dtos.QueryPriceResponse, error)
}

type FeatureOps interface {
	Create(ctx context.Context, req types.CreateFeatureRequest) (*dtos.CreateFeatureResponse, error)
	Query(ctx context.Context, filter types.FeatureFilter) (*dtos.QueryFeatureResponse, error)
}

type SubscriptionOps interface {
	Create(ctx context.Context, req types.CreateSubscriptionRequest) (*dtos.CreateSubscriptionResponse, error)
	Get(ctx context.Context, id string) (*dtos.GetSubscriptionResponse, error)
	Cancel(ctx context.Context, id string, body types.CancelSubscriptionRequest) (*dtos.CancelSubscriptionResponse, error)
	Query(ctx context.Context, filter types.SubscriptionFilter) (*dtos.QuerySubscriptionResponse, error)
	ActivateSubscription(ctx context.Context, id string, body types.ActivateDraftSubscriptionRequest) (*dtos.ActivateSubscriptionResponse, error)
	GetEntitlements(ctx context.Context, id string, featureIDs []string) (*dtos.GetSubscriptionEntitlementsResponse, error)
	GetUsage(ctx context.Context, req types.GetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error)
	CreateLineItem(ctx context.Context, id string, body types.CreateSubscriptionLineItemRequest) (*dtos.CreateSubscriptionLineItemResponse, error)
	UpdateLineItem(ctx context.Context, id string, body types.UpdateSubscriptionLineItemRequest) (*dtos.UpdateSubscriptionLineItemResponse, error)
}

type WalletOps interface {
	Create(ctx context.Context, req types.CreateWalletRequest) (*dtos.CreateWalletResponse, error)
	Query(ctx context.Context, filter types.WalletFilter) (*dtos.QueryWalletResponse, error)
	GetWalletsByCustomerID(ctx context.Context, customerID string) (*dtos.GetWalletsByCustomerIDResponse, error)
	GetBalance(ctx context.Context, id string) (*dtos.GetWalletBalanceResponse, error)
	TopUp(ctx context.Context, id string, body types.TopUpWalletRequest) (*dtos.TopUpWalletResponse, error)
}

type EventOps interface {
	Ingest(ctx context.Context, req types.IngestEventRequest) (*dtos.IngestEventResponse, error)
	GetUsageAnalytics(ctx context.Context, req types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error)
	// ListRaw queries the raw events table. The probe uses this to verify
	// synchronously-ingested events actually landed before polling the
	// aggregation pipeline — separating "ingest dropped" from "aggregation
	// dropped" in failure attribution.
	ListRaw(ctx context.Context, req types.GetEventsRequest) (*dtos.ListRawEventsResponse, error)
}

type InvoiceOps interface {
	Query(ctx context.Context, filter types.InvoiceFilter) (*dtos.QueryInvoiceResponse, error)
	Get(ctx context.Context, id string) (*dtos.GetInvoiceResponse, error)
	GetPreview(ctx context.Context, req types.GetPreviewInvoiceRequest) (*dtos.GetInvoicePreviewResponse, error)
}

type EntitlementOps interface {
	Create(ctx context.Context, req types.CreateEntitlementRequest) (*dtos.CreateEntitlementResponse, error)
	Query(ctx context.Context, req types.EntitlementFilter) (*dtos.QueryEntitlementResponse, error)
	Delete(ctx context.Context, id string) (*dtos.DeleteEntitlementResponse, error)

	// CreateWithGrant sends a grant-config-enabled entitlement create via
	// raw HTTP. SDK v2.0.24's CreateEntitlementRequest doesn't expose grant
	// fields; when the SDK regenerates, delete this and use Create.
	CreateWithGrant(ctx context.Context, req GrantEntitlementInput) (id string, err error)

	// GetRaw fetches an entitlement by ID via raw HTTP so callers can
	// inspect grant fields the SDK's typed EntitlementResponse drops.
	GetRaw(ctx context.Context, id string) (*GrantEntitlementResponse, error)
}

type CouponOps interface {
	Create(ctx context.Context, req types.CreateCouponRequest) (*dtos.CreateCouponResponse, error)
	Query(ctx context.Context, req types.CouponFilter) (*dtos.QueryCouponResponse, error)
	GetByCode(ctx context.Context, code string) (*dtos.GetCouponByCodeResponse, error)
	Delete(ctx context.Context, id string) (*dtos.DeleteCouponResponse, error)
}

type CouponAssociationOps interface {
	List(ctx context.Context, req dtos.ListCouponAssociationsRequest) (*dtos.ListCouponAssociationsResponse, error)
}

type TaxRateOps interface {
	Create(ctx context.Context, req types.CreateTaxRateRequest) (*dtos.CreateTaxRateResponse, error)
	Get(ctx context.Context, id string) (*dtos.GetTaxRateResponse, error)
	List(ctx context.Context, req dtos.GetTaxRatesRequest) (*dtos.GetTaxRatesResponse, error)
	Delete(ctx context.Context, id string) (*dtos.DeleteTaxRateResponse, error)
}

type TaxAssociationOps interface {
	Create(ctx context.Context, req types.CreateTaxAssociationRequest) (*dtos.CreateTaxAssociationResponse, error)
	List(ctx context.Context, entityType, entityID, externalCustomerID, taxRateID *string) (*dtos.ListTaxAssociationsResponse, error)
	Delete(ctx context.Context, id string) (*dtos.DeleteTaxAssociationResponse, error)
}

// SavedPaymentMethod is the subset of a saved-methods listing the payment probes read.
type SavedPaymentMethod struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	IsDefault     bool   `json:"is_default"`
	CanAutoCharge bool   `json:"can_auto_charge"`
	Card          *struct {
		Last4 string `json:"last4"`
	} `json:"card,omitempty"`
}

// Last4 returns the card's last four digits, "" for a non-card method.
func (m SavedPaymentMethod) Last4() string {
	if m.Card == nil {
		return ""
	}
	return m.Card.Last4
}

// PaymentOps covers checkout sessions and saved payment methods. Methods named
// Portal* authenticate with a customer-portal session token instead of the API
// key. The saved-method, setup, mapping and portal endpoints are not in the SDK
// and go over raw HTTP.
type PaymentOps interface {
	CreateCheckoutSession(ctx context.Context, req types.CreateCheckoutSessionRequest) (*dtos.CreateCheckoutSessionResponse, error)
	GetCheckoutSession(ctx context.Context, id string) (*dtos.GetCheckoutSessionResponse, error)
	CancelCheckoutSession(ctx context.Context, id string) (*dtos.CancelCheckoutSessionResponse, error)
	// CreateCheckoutInvoice creates a one-off invoice; with req.Checkout set it is
	// gated on a checkout session.
	CreateCheckoutInvoice(ctx context.Context, req types.CreateInvoiceRequest) (*dtos.CreateInvoiceResponse, error)
	// ListPayments reads payments without reconciling with the gateway, unlike
	// GET /payments/{id}, so it shows only what webhooks have delivered.
	ListPayments(ctx context.Context, req dtos.ListPaymentsRequest) (*dtos.ListPaymentsResponse, error)

	ListSavedMethods(ctx context.Context, customerID, provider string) ([]SavedPaymentMethod, error)
	// CreateSetupLink returns the hosted page URL for adding a card (Stripe setup intent).
	CreateSetupLink(ctx context.Context, customerID, provider, returnURL string) (string, error)
	// GetGatewayCustomerID returns the customer's id at the gateway, "" when not yet synced.
	GetGatewayCustomerID(ctx context.Context, customerID, provider string) (string, error)
	CreatePortalSession(ctx context.Context, externalCustomerID string) (token string, err error)

	PortalListSavedMethods(ctx context.Context, token, provider string) ([]SavedPaymentMethod, error)
	// PortalAddMethod returns the hosted page URL the customer adds a method on.
	PortalAddMethod(ctx context.Context, token, provider, returnURL string) (string, error)
	PortalSetDefaultMethod(ctx context.Context, token, provider, methodID string) ([]SavedPaymentMethod, error)
	PortalDeleteMethod(ctx context.Context, token, provider, methodID string) ([]SavedPaymentMethod, error)

	// ExecuteSubscriptionModify applies a subscription change; with req.Checkout set
	// and a net charge, it is gated on a checkout session.
	ExecuteSubscriptionModify(ctx context.Context, subscriptionID string, req types.ExecuteSubscriptionModifyRequest) (*dtos.ExecuteSubscriptionModifyResponse, error)
	// AddSubscriptionAddon attaches an addon; with req.Checkout set and prorations
	// requested, it is gated on a checkout session.
	AddSubscriptionAddon(ctx context.Context, req types.AddAddonRequest) (*dtos.AddSubscriptionAddonResponse, error)
	GetSubscriptionAddonAssociations(ctx context.Context, subscriptionID string) (*dtos.GetSubscriptionAddonAssociationsResponse, error)
	GetAddonByLookupKey(ctx context.Context, lookupKey string) (*dtos.GetAddonByLookupKeyResponse, error)
	CreateAddon(ctx context.Context, req types.CreateAddonRequest) (*dtos.CreateAddonResponse, error)

	// CreateCreditNote issues a credit note; against a paid invoice it is a refund.
	CreateCreditNote(ctx context.Context, req types.CreateCreditNoteRequest) (*dtos.CreateCreditNoteResponse, error)
	ListRefunds(ctx context.Context, req dtos.ListRefundsRequest) (*dtos.ListRefundsResponse, error)
}

type AsyncEventClient interface {
	Enqueue(eventName, externalCustomerID string, properties map[string]any) error
	EnqueueWithOptions(opts flexprice.EventOptions) error
	Flush() error
	Close() error
}

func NewSDKClient(apiHost, apiKey string) Client {
	sdk := flexprice.New(
		flexprice.WithServerURL(apiHost),
		flexprice.WithSecurity(apiKey),
	)
	return &sdkClient{sdk: sdk, apiHost: apiHost, apiKey: apiKey}
}

type sdkClient struct {
	sdk *flexprice.Flexprice
	// apiHost + apiKey are captured here so raw-HTTP methods
	// (EntitlementOps.CreateWithGrant + GetRaw, PriceOps.CreateBucketed)
	// can bypass the SDK's
	// generated typed request/response for endpoints where the SDK
	// doesn't cover fields the server accepts. When SDK v2.0.24 is
	// regenerated with those fields, delete the raw-HTTP methods and
	// remove these captures.
	apiHost string
	apiKey  string
}

func (c *sdkClient) Customers() CustomerOps         { return customerOps{c.sdk.Customers} }
func (c *sdkClient) Plans() PlanOps                 { return planOps{c.sdk.Plans} }
func (c *sdkClient) Prices() PriceOps               { return priceOps{s: c.sdk.Prices, parent: c} }
func (c *sdkClient) Features() FeatureOps           { return featureOps{c.sdk.Features} }
func (c *sdkClient) Subscriptions() SubscriptionOps { return subscriptionOps{c.sdk.Subscriptions} }
func (c *sdkClient) Wallets() WalletOps             { return walletOps{c.sdk.Wallets} }
func (c *sdkClient) Events() EventOps               { return eventOps{c.sdk.Events} }
func (c *sdkClient) Invoices() InvoiceOps           { return invoiceOps{c.sdk.Invoices} }
func (c *sdkClient) NewAsyncEventClient() AsyncEventClient {
	return c.sdk.NewAsyncClient()
}
func (c *sdkClient) Entitlements() EntitlementOps {
	return entitlementOps{s: c.sdk.Entitlements, parent: c}
}
func (c *sdkClient) Coupons() CouponOps { return couponOps{c.sdk.Coupons} }
func (c *sdkClient) CouponAssociations() CouponAssociationOps {
	return couponAssociationOps{c.sdk.CouponAssociations}
}
func (c *sdkClient) TaxRates() TaxRateOps { return taxRateOps{c.sdk.TaxRates} }
func (c *sdkClient) TaxAssociations() TaxAssociationOps {
	return taxAssociationOps{c.sdk.TaxAssociations}
}
func (c *sdkClient) Payments() PaymentOps {
	return paymentOps{
		checkout:      c.sdk.Checkout,
		payments:      c.sdk.Payments,
		invoices:      c.sdk.Invoices,
		subscriptions: c.sdk.Subscriptions,
		addons:        c.sdk.Addons,
		creditNotes:   c.sdk.CreditNotes,
		refunds:       c.sdk.Refunds,
		parent:        c,
	}
}

// --- adapters ---

type customerOps struct{ s *flexprice.Customers }

func (o customerOps) Create(ctx context.Context, req types.CreateCustomerRequest) (*dtos.CreateCustomerResponse, error) {
	return o.s.CreateCustomer(ctx, req)
}
func (o customerOps) GetByExternalID(ctx context.Context, externalID string) (*dtos.GetCustomerByExternalIDResponse, error) {
	return o.s.GetCustomerByExternalID(ctx, externalID)
}
func (o customerOps) Get(ctx context.Context, id string) (*dtos.GetCustomerResponse, error) {
	return o.s.GetCustomer(ctx, id)
}
func (o customerOps) GetEntitlements(ctx context.Context, id string) (*dtos.GetCustomerEntitlementsResponse, error) {
	return o.s.GetCustomerEntitlements(ctx, id)
}
func (o customerOps) GetUsageSummary(ctx context.Context, req dtos.GetCustomerUsageSummaryRequest) (*dtos.GetCustomerUsageSummaryResponse, error) {
	return o.s.GetCustomerUsageSummary(ctx, req)
}
func (o customerOps) Update(ctx context.Context, body types.UpdateCustomerRequest, id, externalID *string) (*dtos.UpdateCustomerResponse, error) {
	return o.s.UpdateCustomer(ctx, body, id, externalID)
}
func (o customerOps) Delete(ctx context.Context, id string) (*dtos.DeleteCustomerResponse, error) {
	return o.s.DeleteCustomer(ctx, id)
}
func (o customerOps) Query(ctx context.Context, filter types.CustomerFilter) (*dtos.QueryCustomerResponse, error) {
	return o.s.QueryCustomer(ctx, filter)
}

type planOps struct{ s *flexprice.Plans }

func (o planOps) Create(ctx context.Context, req types.CreatePlanRequest) (*dtos.CreatePlanResponse, error) {
	return o.s.CreatePlan(ctx, req)
}
func (o planOps) Query(ctx context.Context, f types.PlanFilter) (*dtos.QueryPlanResponse, error) {
	return o.s.QueryPlan(ctx, f)
}
func (o planOps) Get(ctx context.Context, id string) (*dtos.GetPlanResponse, error) {
	return o.s.GetPlan(ctx, id)
}

func (o planOps) SyncPrices(ctx context.Context, planID string) (*dtos.SyncPlanPricesResponse, error) {
	return o.s.SyncPlanPrices(ctx, planID)
}

type priceOps struct {
	s      *flexprice.Prices
	parent *sdkClient
}

func (o priceOps) Create(ctx context.Context, req types.CreatePriceRequest) (*dtos.CreatePriceResponse, error) {
	return o.s.CreatePrice(ctx, req)
}

// CreateBucketed posts the price with bucket_size attached. The published SDK's
// CreatePriceRequest has no bucket_size field, so the typed request is marshalled
// and the key spliced in — same escape hatch as EntitlementOps.CreateWithGrant.
// Delete this once the SDK is regenerated and set the field on the typed request.
func (o priceOps) CreateBucketed(ctx context.Context, req types.CreatePriceRequest, bucketSize string) (string, error) {
	if bucketSize == "" {
		resp, err := o.Create(ctx, req)
		if err != nil {
			return "", err
		}
		if resp == nil || resp.PriceResponse == nil || resp.PriceResponse.ID == nil {
			return "", fmt.Errorf("create price: empty response")
		}
		return *resp.PriceResponse.ID, nil
	}

	// Round-trip the typed request so every field the SDK knows about is carried
	// verbatim; only bucket_size is added on top.
	typed, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal price request: %w", err)
	}
	body := map[string]any{}
	if err := json.Unmarshal(typed, &body); err != nil {
		return "", fmt.Errorf("unmarshal price request: %w", err)
	}
	body["bucket_size"] = bucketSize

	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal bucketed price request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		strings.TrimRight(o.parent.apiHost, "/")+"/prices",
		bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-api-key", o.parent.apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var out struct {
			ID         string `json:"id"`
			BucketSize string `json:"bucket_size"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return "", fmt.Errorf("decode response: %w", err)
		}
		// Echo check: a server that silently drops bucket_size would otherwise
		// leave the probe asserting bucketed behaviour against an unbucketed price.
		if out.BucketSize != bucketSize {
			return "", fmt.Errorf("create bucketed price: server echoed bucket_size %q, want %q", out.BucketSize, bucketSize)
		}
		return out.ID, nil
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	return "", errorFromRawHTTPResponse(resp.StatusCode, bodyBytes)
}

func (o priceOps) Query(ctx context.Context, f types.PriceFilter) (*dtos.QueryPriceResponse, error) {
	return o.s.QueryPrice(ctx, f)
}

type featureOps struct{ s *flexprice.Features }

func (o featureOps) Create(ctx context.Context, req types.CreateFeatureRequest) (*dtos.CreateFeatureResponse, error) {
	return o.s.CreateFeature(ctx, req)
}
func (o featureOps) Query(ctx context.Context, f types.FeatureFilter) (*dtos.QueryFeatureResponse, error) {
	return o.s.QueryFeature(ctx, f)
}

type subscriptionOps struct{ s *flexprice.Subscriptions }

func (o subscriptionOps) Create(ctx context.Context, req types.CreateSubscriptionRequest) (*dtos.CreateSubscriptionResponse, error) {
	return o.s.CreateSubscription(ctx, req)
}
func (o subscriptionOps) Get(ctx context.Context, id string) (*dtos.GetSubscriptionResponse, error) {
	return o.s.GetSubscription(ctx, id)
}
func (o subscriptionOps) Cancel(ctx context.Context, id string, body types.CancelSubscriptionRequest) (*dtos.CancelSubscriptionResponse, error) {
	return o.s.CancelSubscription(ctx, id, body)
}
func (o subscriptionOps) Query(ctx context.Context, f types.SubscriptionFilter) (*dtos.QuerySubscriptionResponse, error) {
	return o.s.QuerySubscription(ctx, f)
}
func (o subscriptionOps) ActivateSubscription(ctx context.Context, id string, body types.ActivateDraftSubscriptionRequest) (*dtos.ActivateSubscriptionResponse, error) {
	return o.s.ActivateSubscription(ctx, id, body)
}
func (o subscriptionOps) GetEntitlements(ctx context.Context, id string, featureIDs []string) (*dtos.GetSubscriptionEntitlementsResponse, error) {
	return o.s.GetSubscriptionEntitlements(ctx, id, featureIDs)
}
func (o subscriptionOps) GetUsage(ctx context.Context, req types.GetUsageBySubscriptionRequest) (*dtos.GetSubscriptionUsageResponse, error) {
	return o.s.GetSubscriptionUsage(ctx, req)
}
func (o subscriptionOps) CreateLineItem(ctx context.Context, id string, body types.CreateSubscriptionLineItemRequest) (*dtos.CreateSubscriptionLineItemResponse, error) {
	return o.s.CreateSubscriptionLineItem(ctx, id, body)
}
func (o subscriptionOps) UpdateLineItem(ctx context.Context, id string, body types.UpdateSubscriptionLineItemRequest) (*dtos.UpdateSubscriptionLineItemResponse, error) {
	return o.s.UpdateSubscriptionLineItem(ctx, id, body)
}

type walletOps struct{ s *flexprice.Wallets }

func (o walletOps) Create(ctx context.Context, req types.CreateWalletRequest) (*dtos.CreateWalletResponse, error) {
	return o.s.CreateWallet(ctx, req)
}
func (o walletOps) Query(ctx context.Context, f types.WalletFilter) (*dtos.QueryWalletResponse, error) {
	return o.s.QueryWallet(ctx, f)
}
func (o walletOps) GetWalletsByCustomerID(ctx context.Context, customerID string) (*dtos.GetWalletsByCustomerIDResponse, error) {
	return o.s.GetWalletsByCustomerID(ctx, customerID)
}

// GetBalance passes nil for the optional expand parameter (not exposed in the interface).
func (o walletOps) GetBalance(ctx context.Context, id string) (*dtos.GetWalletBalanceResponse, error) {
	return o.s.GetWalletBalance(ctx, id, nil)
}
func (o walletOps) TopUp(ctx context.Context, id string, body types.TopUpWalletRequest) (*dtos.TopUpWalletResponse, error) {
	return o.s.TopUpWallet(ctx, id, body)
}

type eventOps struct{ s *flexprice.Events }

func (o eventOps) Ingest(ctx context.Context, req types.IngestEventRequest) (*dtos.IngestEventResponse, error) {
	return o.s.IngestEvent(ctx, req)
}
func (o eventOps) GetUsageAnalytics(ctx context.Context, req types.GetUsageAnalyticsRequest) (*dtos.GetUsageAnalyticsResponse, error) {
	return o.s.GetUsageAnalytics(ctx, req)
}
func (o eventOps) ListRaw(ctx context.Context, req types.GetEventsRequest) (*dtos.ListRawEventsResponse, error) {
	return o.s.ListRawEvents(ctx, req)
}

type invoiceOps struct{ s *flexprice.Invoices }

func (o invoiceOps) Query(ctx context.Context, f types.InvoiceFilter) (*dtos.QueryInvoiceResponse, error) {
	return o.s.QueryInvoice(ctx, f)
}

// Get passes nil for optional expandBySource, groupBy, and expand parameters (not exposed in the interface).
func (o invoiceOps) Get(ctx context.Context, id string) (*dtos.GetInvoiceResponse, error) {
	return o.s.GetInvoice(ctx, id, nil, nil, nil)
}
func (o invoiceOps) GetPreview(ctx context.Context, req types.GetPreviewInvoiceRequest) (*dtos.GetInvoicePreviewResponse, error) {
	return o.s.GetInvoicePreview(ctx, req)
}

type entitlementOps struct {
	s      *flexprice.Entitlements
	parent *sdkClient // read-only reference — holds apiHost + apiKey for raw HTTP
}

func (o entitlementOps) Create(ctx context.Context, req types.CreateEntitlementRequest) (*dtos.CreateEntitlementResponse, error) {
	return o.s.CreateEntitlement(ctx, req)
}
func (o entitlementOps) Query(ctx context.Context, f types.EntitlementFilter) (*dtos.QueryEntitlementResponse, error) {
	return o.s.QueryEntitlement(ctx, f)
}
func (o entitlementOps) Delete(ctx context.Context, id string) (*dtos.DeleteEntitlementResponse, error) {
	return o.s.DeleteEntitlement(ctx, id)
}

// CreateWithGrant POSTs a hand-rolled JSON body to /entitlements matching
// the server's dto.CreateEntitlementRequest schema, including grant fields
// that SDK v2.0.24's CreateEntitlementRequest doesn't expose.
func (o entitlementOps) CreateWithGrant(ctx context.Context, req GrantEntitlementInput) (string, error) {
	body := map[string]any{
		"feature_id":           req.FeatureID,
		"feature_type":         req.FeatureType,
		"plan_id":              req.PlanID,
		"entity_type":          req.EntityType,
		"entity_id":            req.EntityID,
		"is_enabled":           req.IsEnabled,
		"grant_measure":        req.GrantMeasure,
		"grant_quota":          req.GrantQuota,
		"grant_duration_value": req.GrantDurationValue,
		"grant_duration_unit":  req.GrantDurationUnit,
		"aggregation_mode":     req.AggregationMode,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal grant entitlement input: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		strings.TrimRight(o.parent.apiHost, "/")+"/entitlements",
		bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-api-key", o.parent.apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var out GrantEntitlementResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return "", fmt.Errorf("decode response: %w", err)
		}
		return out.ID, nil
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	return "", errorFromRawHTTPResponse(resp.StatusCode, bodyBytes)
}

// GetRaw fetches an entitlement's full JSON body and decodes into
// GrantEntitlementResponse — includes grant_* fields the SDK's typed
// EntitlementResponse silently drops.
func (o entitlementOps) GetRaw(ctx context.Context, id string) (*GrantEntitlementResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET",
		strings.TrimRight(o.parent.apiHost, "/")+"/entitlements/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-api-key", o.parent.apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var out GrantEntitlementResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		return &out, nil
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	return nil, errorFromRawHTTPResponse(resp.StatusCode, bodyBytes)
}

// errorFromRawHTTPResponse converts a non-2xx HTTP response into an SDK-shaped
// error so callers can reuse isNotFound / isAlreadyExists helpers unchanged.
func errorFromRawHTTPResponse(status int, body []byte) error {
	var er sdkerrors.ErrorResponse
	if err := json.Unmarshal(body, &er); err == nil && er.HTTPStatusCode != nil {
		return &er
	}
	return sdkerrors.NewAPIError("raw HTTP error", status, string(body), nil)
}

type couponOps struct{ s *flexprice.Coupons }

func (o couponOps) Create(ctx context.Context, req types.CreateCouponRequest) (*dtos.CreateCouponResponse, error) {
	return o.s.CreateCoupon(ctx, req)
}
func (o couponOps) Query(ctx context.Context, f types.CouponFilter) (*dtos.QueryCouponResponse, error) {
	return o.s.QueryCoupon(ctx, f)
}
func (o couponOps) GetByCode(ctx context.Context, code string) (*dtos.GetCouponByCodeResponse, error) {
	return o.s.GetCouponByCode(ctx, code)
}
func (o couponOps) Delete(ctx context.Context, id string) (*dtos.DeleteCouponResponse, error) {
	return o.s.DeleteCoupon(ctx, id)
}

type couponAssociationOps struct{ s *flexprice.CouponAssociations }

func (o couponAssociationOps) List(ctx context.Context, req dtos.ListCouponAssociationsRequest) (*dtos.ListCouponAssociationsResponse, error) {
	return o.s.ListCouponAssociations(ctx, req)
}

type taxRateOps struct{ s *flexprice.TaxRates }

func (o taxRateOps) Create(ctx context.Context, req types.CreateTaxRateRequest) (*dtos.CreateTaxRateResponse, error) {
	return o.s.CreateTaxRate(ctx, req)
}
func (o taxRateOps) Get(ctx context.Context, id string) (*dtos.GetTaxRateResponse, error) {
	return o.s.GetTaxRate(ctx, id)
}
func (o taxRateOps) List(ctx context.Context, req dtos.GetTaxRatesRequest) (*dtos.GetTaxRatesResponse, error) {
	return o.s.GetTaxRates(ctx, req)
}
func (o taxRateOps) Delete(ctx context.Context, id string) (*dtos.DeleteTaxRateResponse, error) {
	return o.s.DeleteTaxRate(ctx, id)
}

type taxAssociationOps struct{ s *flexprice.TaxAssociations }

func (o taxAssociationOps) Create(ctx context.Context, req types.CreateTaxAssociationRequest) (*dtos.CreateTaxAssociationResponse, error) {
	return o.s.CreateTaxAssociation(ctx, req)
}
func (o taxAssociationOps) List(ctx context.Context, entityType, entityID, externalCustomerID, taxRateID *string) (*dtos.ListTaxAssociationsResponse, error) {
	return o.s.ListTaxAssociations(ctx, entityType, entityID, externalCustomerID, taxRateID)
}
func (o taxAssociationOps) Delete(ctx context.Context, id string) (*dtos.DeleteTaxAssociationResponse, error) {
	return o.s.DeleteTaxAssociation(ctx, id)
}

type paymentOps struct {
	checkout      *flexprice.Checkout
	payments      *flexprice.Payments
	invoices      *flexprice.Invoices
	subscriptions *flexprice.Subscriptions
	addons        *flexprice.Addons
	creditNotes   *flexprice.CreditNotes
	refunds       *flexprice.Refunds
	parent        *sdkClient
}

func (o paymentOps) CreateCheckoutSession(ctx context.Context, req types.CreateCheckoutSessionRequest) (*dtos.CreateCheckoutSessionResponse, error) {
	return o.checkout.CreateCheckoutSession(ctx, req)
}
func (o paymentOps) GetCheckoutSession(ctx context.Context, id string) (*dtos.GetCheckoutSessionResponse, error) {
	return o.checkout.GetCheckoutSession(ctx, id)
}
func (o paymentOps) CancelCheckoutSession(ctx context.Context, id string) (*dtos.CancelCheckoutSessionResponse, error) {
	return o.checkout.CancelCheckoutSession(ctx, id)
}
func (o paymentOps) CreateCheckoutInvoice(ctx context.Context, req types.CreateInvoiceRequest) (*dtos.CreateInvoiceResponse, error) {
	return o.invoices.CreateInvoice(ctx, req)
}
func (o paymentOps) ListPayments(ctx context.Context, req dtos.ListPaymentsRequest) (*dtos.ListPaymentsResponse, error) {
	return o.payments.ListPayments(ctx, req)
}

func (o paymentOps) ExecuteSubscriptionModify(ctx context.Context, subscriptionID string, req types.ExecuteSubscriptionModifyRequest) (*dtos.ExecuteSubscriptionModifyResponse, error) {
	return o.subscriptions.ExecuteSubscriptionModify(ctx, subscriptionID, req)
}
func (o paymentOps) AddSubscriptionAddon(ctx context.Context, req types.AddAddonRequest) (*dtos.AddSubscriptionAddonResponse, error) {
	return o.subscriptions.AddSubscriptionAddon(ctx, req)
}
func (o paymentOps) GetSubscriptionAddonAssociations(ctx context.Context, subscriptionID string) (*dtos.GetSubscriptionAddonAssociationsResponse, error) {
	return o.subscriptions.GetSubscriptionAddonAssociations(ctx, subscriptionID)
}
func (o paymentOps) GetAddonByLookupKey(ctx context.Context, lookupKey string) (*dtos.GetAddonByLookupKeyResponse, error) {
	return o.addons.GetAddonByLookupKey(ctx, lookupKey)
}
func (o paymentOps) CreateAddon(ctx context.Context, req types.CreateAddonRequest) (*dtos.CreateAddonResponse, error) {
	return o.addons.CreateAddon(ctx, req)
}
func (o paymentOps) CreateCreditNote(ctx context.Context, req types.CreateCreditNoteRequest) (*dtos.CreateCreditNoteResponse, error) {
	return o.creditNotes.CreateCreditNote(ctx, req)
}
func (o paymentOps) ListRefunds(ctx context.Context, req dtos.ListRefundsRequest) (*dtos.ListRefundsResponse, error) {
	return o.refunds.ListRefunds(ctx, req)
}

// savedMethodsResponse is the server's SavedPaymentMethodsResponse: one block per gateway.
type savedMethodsResponse struct {
	Providers []struct {
		Provider string               `json:"provider"`
		Items    []SavedPaymentMethod `json:"items"`
		Error    *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"providers"`
}

// methodsFor picks provider's block, failing when it is missing or reports an error.
func (r savedMethodsResponse) methodsFor(provider string) ([]SavedPaymentMethod, error) {
	for _, block := range r.Providers {
		if block.Provider != provider {
			continue
		}
		if block.Error != nil {
			return nil, fmt.Errorf("%s reported: %s", provider, block.Error.Message)
		}
		return block.Items, nil
	}
	return nil, fmt.Errorf("saved-methods response has no %s block", provider)
}

func (o paymentOps) ListSavedMethods(ctx context.Context, customerID, provider string) ([]SavedPaymentMethod, error) {
	var out savedMethodsResponse
	if err := o.parent.doRaw(ctx, http.MethodGet, "/customers/"+url.PathEscape(customerID)+"/payment-methods?providers="+url.QueryEscape(provider), "", nil, &out); err != nil {
		return nil, err
	}
	return out.methodsFor(provider)
}

func (o paymentOps) CreateSetupLink(ctx context.Context, customerID, provider, returnURL string) (string, error) {
	var out struct {
		CheckoutURL string `json:"checkout_url"`
	}
	body := map[string]any{"provider": provider, "success_url": returnURL, "cancel_url": returnURL}
	if err := o.parent.doRaw(ctx, http.MethodPost, "/payments/customers/"+url.PathEscape(customerID)+"/setup/intent", "", body, &out); err != nil {
		return "", err
	}
	return out.CheckoutURL, nil
}

func (o paymentOps) GetGatewayCustomerID(ctx context.Context, customerID, provider string) (string, error) {
	var out struct {
		Items []struct {
			ProviderType     string `json:"provider_type"`
			ProviderEntityID string `json:"provider_entity_id"`
		} `json:"items"`
	}
	if err := o.parent.doRaw(ctx, http.MethodGet, "/integrations/mappings?entity_type=customer&entity_id="+url.QueryEscape(customerID), "", nil, &out); err != nil {
		return "", err
	}
	for _, m := range out.Items {
		if m.ProviderType == provider && m.ProviderEntityID != "" {
			return m.ProviderEntityID, nil
		}
	}
	return "", nil
}

func (o paymentOps) CreatePortalSession(ctx context.Context, externalCustomerID string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	if err := o.parent.doRaw(ctx, http.MethodGet, "/customers/portal/"+url.PathEscape(externalCustomerID), "", nil, &out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", fmt.Errorf("portal session returned no token")
	}
	return out.Token, nil
}

func (o paymentOps) PortalListSavedMethods(ctx context.Context, token, provider string) ([]SavedPaymentMethod, error) {
	var out savedMethodsResponse
	if err := o.parent.doRaw(ctx, http.MethodGet, "/customer/portal/payment-methods?providers="+url.QueryEscape(provider), token, nil, &out); err != nil {
		return nil, err
	}
	return out.methodsFor(provider)
}

func (o paymentOps) PortalAddMethod(ctx context.Context, token, provider, returnURL string) (string, error) {
	var out struct {
		Action struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"action"`
	}
	body := map[string]any{"payment_provider": provider, "success_url": returnURL, "cancel_url": returnURL}
	if err := o.parent.doRaw(ctx, http.MethodPost, "/customer/portal/payment-methods", token, body, &out); err != nil {
		return "", err
	}
	return out.Action.URL, nil
}

func (o paymentOps) PortalSetDefaultMethod(ctx context.Context, token, provider, methodID string) ([]SavedPaymentMethod, error) {
	return o.portalMutateMethod(ctx, "/customer/portal/payment-methods/default", token, provider, methodID)
}

func (o paymentOps) PortalDeleteMethod(ctx context.Context, token, provider, methodID string) ([]SavedPaymentMethod, error) {
	return o.portalMutateMethod(ctx, "/customer/portal/payment-methods/delete", token, provider, methodID)
}

func (o paymentOps) portalMutateMethod(ctx context.Context, path, token, provider, methodID string) ([]SavedPaymentMethod, error) {
	var out savedMethodsResponse
	body := map[string]any{"payment_provider": provider, "payment_method_id": methodID}
	if err := o.parent.doRaw(ctx, http.MethodPost, path, token, body, &out); err != nil {
		return nil, err
	}
	return out.methodsFor(provider)
}

// doRaw sends a JSON request to the Flexprice API and decodes a 2xx body into out.
// A non-empty sessionToken authenticates as a portal customer instead of the API key.
func (c *sdkClient) doRaw(ctx context.Context, method, path, sessionToken string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.apiHost, "/")+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if sessionToken != "" {
		httpReq.Header.Set("X-Session-Token", sessionToken)
	} else {
		httpReq.Header.Set("x-api-key", c.apiKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return errorFromRawHTTPResponse(resp.StatusCode, bodyBytes)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
