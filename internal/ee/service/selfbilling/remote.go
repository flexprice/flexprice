package selfbilling

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	cockroach "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	flexprice "github.com/flexprice/go-sdk/v2"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	remoteTimeout = 15 * time.Second

	// trialCurrency and trialBillingPeriod are the self-serve plan's defaults on Flexprice Cloud.
	trialCurrency      = "usd"
	trialBillingPeriod = sdktypes.BillingPeriodMonthly

	// metaExternalID tags a checkout session with the customer it belongs to.
	metaExternalID = "self_billing_external_id"
	metaTaxID      = "tax_id"

	summaryInvoiceLimit = 12

	usageEventSource = "self_billing"
)

type remoteClient struct {
	sdk      *flexprice.Flexprice
	planID   string
	provider sdktypes.CheckoutPaymentProvider
}

func newRemoteClient(cfg config.BillingConfig) *remoteClient {
	sdk := flexprice.New(
		flexprice.WithServerURL(cfg.APIHost),
		flexprice.WithSecurity(cfg.APIKey),
		flexprice.WithTimeout(remoteTimeout),
	)

	return &remoteClient{
		sdk:      sdk,
		planID:   cfg.PlanID,
		provider: sdktypes.CheckoutPaymentProvider(cfg.CheckoutProvider()),
	}
}

// EnsureCustomer finds or creates the customer; a legacy bare-tenant-id customer is re-keyed, never duplicated.
func (c *remoteClient) EnsureCustomer(ctx context.Context, in customerInput) error {
	_, err := c.sdk.Customers.GetCustomerByExternalID(ctx, in.extID)
	if !isNotFound(err) {
		return cloudErr(err)
	}

	if in.legacyExtID != "" {
		_, err := c.sdk.Customers.GetCustomerByExternalID(ctx, in.legacyExtID)
		if err == nil {
			rekey := sdktypes.UpdateCustomerRequest{ExternalID: lo.ToPtr(in.extID)}
			_, err = c.sdk.Customers.UpdateCustomer(ctx, rekey, nil, lo.ToPtr(in.legacyExtID))
			return cloudErr(err)
		}
		if !isNotFound(err) {
			return cloudErr(err)
		}
	}

	_, err = c.sdk.Customers.CreateCustomer(ctx, sdktypes.CreateCustomerRequest{
		ExternalID: in.extID,
		Name:       in.name,
		Email:      lo.EmptyableToPtr(in.email),
		Metadata:   map[string]string{"tenant_id": in.tenantID},
	})
	return cloudErr(err)
}

// StartTrial re-checks the subscription right before creating it, since CreateSubscription takes no idempotency key.
func (c *remoteClient) StartTrial(ctx context.Context, extID string) (bool, error) {
	sub, err := c.LatestSubscription(ctx, extID)
	if err != nil || sub != nil {
		return false, err
	}

	_, err = c.sdk.Subscriptions.CreateSubscription(ctx, sdktypes.CreateSubscriptionRequest{
		ExternalCustomerID: lo.ToPtr(extID),
		PlanID:             c.planID,
		Currency:           trialCurrency,
		BillingPeriod:      trialBillingPeriod,
	})
	if err != nil {
		return false, cloudErr(err)
	}
	return true, nil
}

func (c *remoteClient) LatestSubscription(ctx context.Context, extID string) (*remoteSubscription, error) {
	resp, err := c.sdk.Customers.GetSubscriptionsForCustomer(ctx, extID, nil)
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, cloudErr(err)
	}

	var live, cancelled *sdktypes.SubscriptionResponse
	newer := func(s, than *sdktypes.SubscriptionResponse) bool {
		return than == nil || lo.FromPtr(s.CreatedAt).After(lo.FromPtr(than.CreatedAt))
	}

	// Every plan counts, not just self-serve: a tenant moved to an Enterprise plan is driven by that subscription.
	for i := range resp.ListSubscriptionsResponse.Items {
		s := &resp.ListSubscriptionsResponse.Items[i]
		if lo.FromPtr(s.SubscriptionStatus) == sdktypes.SubscriptionStatusCancelled {
			if newer(s, cancelled) {
				cancelled = s
			}
			continue
		}

		if newer(s, live) {
			live = s
		}
	}

	latest := lo.Ternary(live != nil, live, cancelled)
	if latest == nil {
		return nil, nil
	}
	return toRemoteSubscription(latest), nil
}

// UpdateBillingDetails merges the tax ID into the existing metadata, since Flexprice Cloud replaces metadata wholesale.
func (c *remoteClient) UpdateBillingDetails(ctx context.Context, extID string, details billingDetails) error {
	var metadata map[string]string
	if details.taxID != "" {
		cust, err := c.sdk.Customers.GetCustomerByExternalID(ctx, extID)
		if err != nil {
			return cloudErr(err)
		}
		metadata = lo.Assign(cust.CustomerResponse.GetMetadata(), map[string]string{metaTaxID: details.taxID})
	}

	req := sdktypes.UpdateCustomerRequest{
		Name:     lo.EmptyableToPtr(details.name),
		Email:    lo.EmptyableToPtr(details.email),
		Metadata: metadata,
	}

	// An address is sent whole, empty parts included, so a cleared line or state does not linger on Flexprice Cloud.
	if details.hasAddress() {
		req.AddressLine1 = lo.ToPtr(details.line1)
		req.AddressLine2 = lo.ToPtr(details.line2)
		req.AddressCity = lo.ToPtr(details.city)
		req.AddressState = lo.ToPtr(details.state)
		req.AddressCountry = lo.ToPtr(details.country)
		req.AddressPostalCode = lo.ToPtr(details.postalCode)
	}

	_, err := c.sdk.Customers.UpdateCustomer(ctx, req, nil, lo.ToPtr(extID))
	return cloudErr(err)
}

// StartTrialEndCheckout ends the trial immediately once paid; a $0 first period completes with no session.
func (c *remoteClient) StartTrialEndCheckout(ctx context.Context, in checkoutInput) (*remoteCheckout, error) {
	resp, err := c.sdk.Subscriptions.ExecuteSubscriptionModify(ctx, in.subscriptionID, sdktypes.ExecuteSubscriptionModifyRequest{
		Type:           sdktypes.SubscriptionModifyTypeTrialEnd,
		TrialEndParams: &sdktypes.SubModifyTrialEndRequest{Action: sdktypes.TrialEndActionImmediate},
		Checkout: &sdktypes.CheckoutParams{
			PaymentProvider: c.provider,
			SuccessURL:      lo.ToPtr(in.successURL),
			FailureURL:      lo.ToPtr(in.failureURL),
			Metadata:        map[string]string{metaExternalID: in.extID},
			IdempotencyKey:  lo.ToPtr("trial_end:" + in.subscriptionID),
		},
	})
	if id := conflictCheckoutID(err); id != "" {
		return c.GetCheckout(ctx, id)
	}
	if err != nil {
		return nil, cloudErr(err)
	}

	cs := resp.SubscriptionModifyResponse.CheckoutSession
	if cs == nil {
		return &remoteCheckout{
			status:   types.CheckoutStatusCompleted,
			extID:    in.extID,
			terminal: true,
		}, nil
	}
	return toRemoteCheckout(cs), nil
}

func (c *remoteClient) GetCheckout(ctx context.Context, id string) (*remoteCheckout, error) {
	resp, err := c.sdk.Checkout.GetCheckoutSession(ctx, id)
	if err != nil {
		return nil, cloudErr(err)
	}
	return toRemoteCheckout(resp.CheckoutSessionResponse), nil
}

func (c *remoteClient) CancelCheckout(ctx context.Context, id string) error {
	_, err := c.sdk.Checkout.CancelCheckoutSession(ctx, id)
	return cloudErr(err)
}

func (c *remoteClient) PortalSession(ctx context.Context, extID string) (*portalSession, error) {
	resp, err := c.sdk.Customers.CreateCustomerPortalSession(ctx, extID)
	if err != nil {
		return nil, cloudErr(err)
	}

	p := resp.PortalSessionResponse
	return &portalSession{
		url:       lo.FromPtr(p.GetURL()),
		expiresAt: lo.FromPtr(p.GetExpiresAt()),
	}, nil
}

// Summary fails on an unparseable invoice total rather than showing a wrong amount.
func (c *remoteClient) Summary(ctx context.Context, extID string) (*remoteSummary, error) {
	sub, err := c.LatestSubscription(ctx, extID)
	if err != nil {
		return nil, err
	}

	invoices, err := c.sdk.Invoices.QueryInvoice(ctx, sdktypes.InvoiceFilter{
		ExternalCustomerID: lo.ToPtr(extID),
		Limit:              lo.ToPtr(int64(summaryInvoiceLimit)),
	})
	if err != nil {
		return nil, cloudErr(err)
	}

	usage, err := c.sdk.Customers.GetCustomerUsageSummary(ctx, sdkdtos.GetCustomerUsageSummaryRequest{CustomerLookupKey: lo.ToPtr(extID)})
	if err != nil {
		return nil, cloudErr(err)
	}

	usageSummary, err := toUsageSummary(usage.CustomerUsageSummaryResponse)
	if err != nil {
		return nil, err
	}

	out := &remoteSummary{
		subscription: sub,
		usage:        usageSummary,
	}
	for _, inv := range invoices.ListInvoicesResponse.Items {
		mapped, err := toRemoteInvoice(inv)
		if err != nil {
			return nil, err
		}
		out.invoices = append(out.invoices, mapped)
	}
	return out, nil
}

func (c *remoteClient) IngestBulk(ctx context.Context, events []usageEvent) error {
	req := sdktypes.BulkIngestEventRequest{
		Events: lo.Map(events, func(e usageEvent, _ int) sdktypes.IngestEventRequest {
			return sdktypes.IngestEventRequest{
				EventID:            lo.ToPtr(e.id),
				EventName:          e.name,
				ExternalCustomerID: e.extID,
				Properties:         e.properties,
				Source:             lo.ToPtr(usageEventSource),
				Timestamp:          lo.ToPtr(e.timestamp.UTC().Format(time.RFC3339)),
			}
		}),
	}

	_, err := c.sdk.Events.IngestEventsBulk(ctx, req)
	return cloudErr(err)
}

// toRemoteSubscription maps the SDK subscription onto local types; Flexprice Cloud shares our status values.
func toRemoteSubscription(s *sdktypes.SubscriptionResponse) *remoteSubscription {
	return &remoteSubscription{
		id:       lo.FromPtr(s.ID),
		status:   types.SubscriptionStatus(lo.FromPtr(s.SubscriptionStatus)),
		trialEnd: s.TrialEnd,
	}
}

func toRemoteCheckout(cs *sdktypes.CheckoutSessionResponse) *remoteCheckout {
	co := &remoteCheckout{
		id:       lo.FromPtr(cs.ID),
		status:   types.CheckoutStatus(lo.FromPtr(cs.CheckoutStatus)),
		extID:    cs.Metadata[metaExternalID],
		terminal: lo.FromPtr(cs.Terminal),
	}
	if cs.PaymentAction != nil {
		co.url = lo.FromPtr(cs.PaymentAction.URL)
	}
	return co
}

func toRemoteInvoice(inv sdktypes.InvoiceResponse) (remoteInvoice, error) {
	total := decimal.Zero
	if raw := lo.FromPtr(inv.Total); raw != "" {
		parsed, err := decimal.NewFromString(raw)
		if err != nil {
			return remoteInvoice{}, ierr.WithError(err).
				WithMessagef("flexprice cloud invoice %s has an unparseable total %q", lo.FromPtr(inv.ID), raw).
				WithHint("The billing invoice total could not be read").
				Mark(ierr.ErrInternal)
		}
		total = parsed
	}

	return remoteInvoice{
		id:            lo.FromPtr(inv.ID),
		number:        lo.FromPtr(inv.InvoiceNumber),
		status:        types.InvoiceStatus(lo.FromPtr(inv.InvoiceStatus)),
		paymentStatus: types.PaymentStatus(lo.FromPtr(inv.PaymentStatus)),
		total:         total,
		currency:      lo.FromPtr(inv.Currency),
		dueDate:       inv.DueDate,
		pdfURL:        lo.FromPtr(inv.InvoicePdfURL),
	}, nil
}

// errRemote marks errors that came from a Flexprice Cloud call.
var errRemote = cockroach.New("flexprice cloud call failed")

// isRemoteFailure reports whether err came from a Flexprice Cloud call.
func isRemoteFailure(err error) bool { return cockroach.Is(err, errRemote) }

// cloudErr classifies a Flexprice Cloud SDK failure; the SDK error stays in the chain.
// A 401 or 403 is our own credential fault, so it reads as unavailable rather than as the caller's mistake.
func cloudErr(err error) error {
	if err == nil {
		return nil
	}

	status := httpStatus(err)
	hint := remoteMessage(err)

	var kind error
	switch {
	case status == http.StatusNotFound:
		kind = ierr.ErrNotFound
	case status == http.StatusConflict:
		kind = ierr.ErrAlreadyExists
	case status >= 400 && status < 500 && status != http.StatusUnauthorized && status != http.StatusForbidden:
		kind = ierr.ErrValidation
	default:
		kind = ierr.ErrHTTPClient
		hint = "Flexprice Cloud is unavailable; try again shortly"
	}

	if hint == "" {
		hint = "Flexprice Cloud rejected the request"
	}

	marked := ierr.WithError(err).
		WithMessagef("flexprice cloud call failed with status %d", status).
		WithHint(hint).
		Mark(kind)
	return cockroach.Mark(marked, errRemote)
}

// remoteMessage returns the user-facing message in a Flexprice Cloud error body, if any.
func remoteMessage(err error) string {
	var errResp *sdkerrors.ErrorResponse
	if errors.As(err, &errResp) {
		return strings.TrimSpace(lo.FromPtr(errResp.Message))
	}

	var body string
	var apiErr *sdkerrors.APIError
	var validationErr *sdkerrors.ResponseValidationError
	switch {
	case errors.As(err, &apiErr):
		body = apiErr.Body
	case errors.As(err, &validationErr):
		body = validationErr.Body
	default:
		return ""
	}

	var parsed struct {
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(body), &parsed) != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Message)
}

// toUsageSummary reads the SDK's usage summary back into the API's own DTO, since Flexprice Cloud serves that same shape.
func toUsageSummary(in *sdktypes.CustomerUsageSummaryResponse) (*dto.CustomerUsageSummaryResponse, error) {
	if in == nil {
		return nil, nil
	}

	raw, err := json.Marshal(in)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Flexprice Cloud returned an unreadable usage summary").Mark(ierr.ErrInternal)
	}

	var out dto.CustomerUsageSummaryResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, ierr.WithError(err).WithHint("Flexprice Cloud returned an unreadable usage summary").Mark(ierr.ErrInternal)
	}
	return &out, nil
}

// conflictCheckoutID returns the open checkout session ID a Flexprice Cloud 409 body names, if any.
func conflictCheckoutID(err error) string {
	var apiErr *sdkerrors.APIError
	if !errors.As(err, &apiErr) {
		return ""
	}

	var body struct {
		Details struct {
			CheckoutSessionID string `json:"checkout_session_id"`
		} `json:"details"`
	}
	if json.Unmarshal([]byte(apiErr.Body), &body) != nil {
		return ""
	}
	return body.Details.CheckoutSessionID
}

// isNotFound reports whether an SDK call failed with HTTP 404, whichever error type the SDK used for it.
func isNotFound(err error) bool {
	return httpStatus(err) == http.StatusNotFound
}

// httpStatus returns the HTTP status of a failed SDK call, or 0 when err did not come from an HTTP response.
func httpStatus(err error) int {
	if err == nil {
		return 0
	}

	var apiErr *sdkerrors.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}

	var errResp *sdkerrors.ErrorResponse
	if errors.As(err, &errResp) {
		if errResp.HTTPMeta.Response == nil {
			return 0
		}
		return errResp.HTTPMeta.Response.StatusCode
	}

	var validationErr *sdkerrors.ResponseValidationError
	if errors.As(err, &validationErr) {
		return validationErr.StatusCode
	}
	return 0
}
