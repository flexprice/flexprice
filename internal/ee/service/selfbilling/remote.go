package selfbilling

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	flexprice "github.com/flexprice/go-sdk/v2"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	stripeProvider      = sdktypes.CheckoutPaymentProvider("stripe")
	summaryInvoiceLimit = 12
	metaTaxID           = "tax_id"
)

type remoteClient struct {
	sdk    *flexprice.Flexprice
	planID string
}

func newRemoteClient(cfg config.BillingConfig) *remoteClient {
	return &remoteClient{
		sdk:    flexprice.New(flexprice.WithServerURL(cfg.APIHost), flexprice.WithSecurity(cfg.APIKey), flexprice.WithTimeout(15*time.Second)),
		planID: cfg.PlanID,
	}
}

// EnsureCustomer finds or creates the customer; a legacy bare-tenant-id customer is re-keyed, never duplicated.
func (c *remoteClient) EnsureCustomer(ctx context.Context, in customerInput) error {
	_, err := c.sdk.Customers.GetCustomerByExternalID(ctx, in.extID)
	if !isNotFound(err) {
		return mumbaiErr(err)
	}
	if in.legacyExtID != "" {
		_, err := c.sdk.Customers.GetCustomerByExternalID(ctx, in.legacyExtID)
		if err == nil {
			_, err = c.sdk.Customers.UpdateCustomer(ctx, sdktypes.UpdateCustomerRequest{ExternalID: lo.ToPtr(in.extID)}, nil, lo.ToPtr(in.legacyExtID))
			return mumbaiErr(err)
		}
		if !isNotFound(err) {
			return mumbaiErr(err)
		}
	}
	_, err = c.sdk.Customers.CreateCustomer(ctx, sdktypes.CreateCustomerRequest{
		ExternalID: in.extID,
		Name:       in.name,
		Email:      lo.EmptyableToPtr(in.email),
		Metadata:   map[string]string{"tenant_id": in.tenantID},
	})
	return mumbaiErr(err)
}

// StartTrial subscribes the customer to the self-serve plan; the plan's prices carry the trial days.
// CreateSubscription takes no idempotency key, so the subscription is re-checked right before creating it.
func (c *remoteClient) StartTrial(ctx context.Context, extID string) error {
	resp, err := c.sdk.Plans.GetPlan(ctx, c.planID)
	if err != nil {
		return mumbaiErr(err)
	}
	prices := resp.PlanResponse.Prices
	if len(prices) == 0 {
		return ierr.NewError("self-serve plan has no prices").Mark(ierr.ErrInvalidOperation)
	}
	fixed, ok := lo.Find(prices, func(p sdktypes.PriceResponse) bool {
		return lo.FromPtr(p.Type) == sdktypes.PriceTypeFixed
	})
	if !ok {
		fixed = prices[0]
	}
	if sub, err := c.LatestSubscription(ctx, extID); err != nil || sub != nil {
		return err
	}
	_, err = c.sdk.Subscriptions.CreateSubscription(ctx, sdktypes.CreateSubscriptionRequest{
		ExternalCustomerID: lo.ToPtr(extID),
		PlanID:             c.planID,
		Currency:           lo.FromPtr(fixed.Currency),
		BillingPeriod:      lo.FromPtr(fixed.BillingPeriod),
	})
	return mumbaiErr(err)
}

func (c *remoteClient) LatestSubscription(ctx context.Context, extID string) (*remoteSubscription, error) {
	resp, err := c.sdk.Customers.GetSubscriptionsForCustomer(ctx, extID, nil)
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, mumbaiErr(err)
	}
	var live, cancelled *sdktypes.SubscriptionResponse
	newer := func(s, than *sdktypes.SubscriptionResponse) bool {
		return than == nil || lo.FromPtr(s.CreatedAt).After(lo.FromPtr(than.CreatedAt))
	}
	for i := range resp.ListSubscriptionsResponse.Items {
		s := &resp.ListSubscriptionsResponse.Items[i]
		if lo.FromPtr(s.PlanID) != c.planID {
			continue
		}
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

func toRemoteSubscription(s *sdktypes.SubscriptionResponse) *remoteSubscription {
	return &remoteSubscription{id: lo.FromPtr(s.ID), status: string(lo.FromPtr(s.SubscriptionStatus)), trialEnd: s.TrialEnd}
}

// UpdateBillingDetails sets the address and tax ID; Mumbai replaces metadata wholesale, so existing keys are merged in.
func (c *remoteClient) UpdateBillingDetails(ctx context.Context, extID string, d billingDetails) error {
	var metadata map[string]string
	if d.taxID != "" {
		cust, err := c.sdk.Customers.GetCustomerByExternalID(ctx, extID)
		if err != nil {
			return mumbaiErr(err)
		}
		metadata = lo.Assign(cust.CustomerResponse.GetMetadata(), map[string]string{metaTaxID: d.taxID})
	}
	_, err := c.sdk.Customers.UpdateCustomer(ctx, sdktypes.UpdateCustomerRequest{
		Name:              lo.EmptyableToPtr(d.name),
		Email:             lo.EmptyableToPtr(d.email),
		AddressLine1:      lo.EmptyableToPtr(d.line1),
		AddressLine2:      lo.EmptyableToPtr(d.line2),
		AddressCity:       lo.EmptyableToPtr(d.city),
		AddressState:      lo.EmptyableToPtr(d.state),
		AddressCountry:    lo.EmptyableToPtr(d.country),
		AddressPostalCode: lo.EmptyableToPtr(d.postalCode),
		Metadata:          metadata,
	}, nil, lo.ToPtr(extID))
	return mumbaiErr(err)
}

func (c *remoteClient) StartTrialEndCheckout(ctx context.Context, in checkoutInput) (*remoteCheckout, error) {
	resp, err := c.sdk.Subscriptions.ExecuteSubscriptionModify(ctx, in.subscriptionID, sdktypes.ExecuteSubscriptionModifyRequest{
		Type:           sdktypes.SubscriptionModifyTypeTrialEnd,
		TrialEndParams: &sdktypes.SubModifyTrialEndRequest{Action: sdktypes.TrialEndActionImmediate},
		Checkout: &sdktypes.CheckoutParams{
			PaymentProvider: stripeProvider,
			SuccessURL:      lo.ToPtr(in.successURL),
			FailureURL:      lo.ToPtr(in.failureURL),
			Metadata:        map[string]string{metaExternalID: in.extID},
			IdempotencyKey:  lo.ToPtr("trial_end:" + in.subscriptionID),
		},
	})
	if err != nil {
		return nil, mumbaiErr(err)
	}
	cs := resp.SubscriptionModifyResponse.CheckoutSession
	if cs == nil { // $0 first period: Mumbai ended the trial with nothing to collect
		return &remoteCheckout{status: string(sdktypes.CheckoutStatusCompleted), terminal: true, extID: in.extID}, nil
	}
	return toRemoteCheckout(cs), nil
}

func (c *remoteClient) GetCheckout(ctx context.Context, id string) (*remoteCheckout, error) {
	resp, err := c.sdk.Checkout.GetCheckoutSession(ctx, id)
	if err != nil {
		return nil, mumbaiErr(err)
	}
	return toRemoteCheckout(resp.CheckoutSessionResponse), nil
}

func (c *remoteClient) CancelCheckout(ctx context.Context, id string) error {
	_, err := c.sdk.Checkout.CancelCheckoutSession(ctx, id)
	return mumbaiErr(err)
}

func toRemoteCheckout(cs *sdktypes.CheckoutSessionResponse) *remoteCheckout {
	co := &remoteCheckout{
		id:       lo.FromPtr(cs.ID),
		status:   string(lo.FromPtr(cs.CheckoutStatus)),
		terminal: lo.FromPtr(cs.Terminal),
		extID:    cs.Metadata[metaExternalID],
	}
	if cs.PaymentAction != nil {
		co.url = lo.FromPtr(cs.PaymentAction.URL)
	}
	return co
}

// errPortalSDKPending is returned until Task 17 swaps in the generated go-sdk portal call.
var errPortalSDKPending = ierr.NewError("billing portal needs a newer go-sdk").
	WithHint("Bump github.com/flexprice/go-sdk/v2 to the release that includes createCustomerPortalSession").
	Mark(ierr.ErrInternal)

func (c *remoteClient) PortalSession(context.Context, string) (*portalSession, error) {
	return nil, errPortalSDKPending
}

func (c *remoteClient) Summary(ctx context.Context, extID string) (*remoteSummary, error) {
	sub, err := c.LatestSubscription(ctx, extID)
	if err != nil {
		return nil, err
	}
	invs, err := c.sdk.Invoices.QueryInvoice(ctx, sdktypes.InvoiceFilter{ExternalCustomerID: lo.ToPtr(extID), Limit: lo.ToPtr(int64(summaryInvoiceLimit))})
	if err != nil {
		return nil, mumbaiErr(err)
	}
	usage, err := c.sdk.Customers.GetCustomerUsageSummary(ctx, sdkdtos.GetCustomerUsageSummaryRequest{CustomerLookupKey: lo.ToPtr(extID)})
	if err != nil {
		return nil, mumbaiErr(err)
	}
	out := &remoteSummary{subscription: sub, usage: usage.CustomerUsageSummaryResponse}
	for _, i := range invs.ListInvoicesResponse.Items {
		total := decimal.Zero
		if t := lo.FromPtr(i.Total); t != "" {
			total, err = decimal.NewFromString(t)
			if err != nil {
				return nil, ierr.WithError(err).
					WithMessagef("billing invoice %s has an unparseable total %q", lo.FromPtr(i.ID), t).
					WithHint("The billing invoice total could not be read").
					Mark(ierr.ErrInternal)
			}
		}
		out.invoices = append(out.invoices, remoteInvoice{
			id: lo.FromPtr(i.ID), number: lo.FromPtr(i.InvoiceNumber),
			status: string(lo.FromPtr(i.InvoiceStatus)), paymentStatus: string(lo.FromPtr(i.PaymentStatus)),
			currency: lo.FromPtr(i.Currency), pdfURL: lo.FromPtr(i.InvoicePdfURL), total: total, dueDate: i.DueDate,
		})
	}
	return out, nil
}

func (c *remoteClient) IngestBulk(ctx context.Context, events []usageEvent) error {
	_, err := c.sdk.Events.IngestEventsBulk(ctx, sdktypes.BulkIngestEventRequest{Events: lo.Map(events, func(e usageEvent, _ int) sdktypes.IngestEventRequest {
		return sdktypes.IngestEventRequest{
			EventID:            lo.ToPtr(e.id),
			EventName:          e.name,
			ExternalCustomerID: e.extID,
			Properties:         e.properties,
			Source:             lo.ToPtr("self_billing"),
			Timestamp:          lo.ToPtr(e.timestamp.UTC().Format(time.RFC3339)),
		}
	})})
	return mumbaiErr(err)
}

// errRemote marks every failure that came back from a billing tenant call.
var errRemote = errors.New("billing tenant call failed")

// mumbaiErr classifies a billing tenant SDK failure for API callers; the SDK error stays in the chain.
func mumbaiErr(err error) error {
	if err == nil {
		return nil
	}
	status := httpStatus(err)
	var kind error
	hint := remoteMessage(err)
	switch {
	case status == http.StatusNotFound:
		kind = ierr.ErrNotFound
	case status == http.StatusConflict:
		kind = ierr.ErrAlreadyExists
	case status >= 400 && status < 500:
		kind = ierr.ErrValidation
	default:
		kind, hint = ierr.ErrHTTPClient, "The billing service is unavailable; try again shortly"
	}
	if hint == "" {
		hint = "The billing service rejected the request"
	}
	out := ierr.WithError(err).
		WithMessagef("billing tenant call failed with status %d", status).
		WithHint(hint).
		Mark(kind)
	return errors.Mark(out, errRemote)
}

// isRemoteFailure reports whether err came from a billing tenant call.
func isRemoteFailure(err error) bool { return errors.Is(err, errRemote) }

// remoteMessage returns the user-facing message in a Flexprice API error body, if any.
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

// isNotFound reports whether an SDK call failed with HTTP 404, whichever error type the SDK used for it.
func isNotFound(err error) bool { return httpStatus(err) == http.StatusNotFound }

// isConflict reports whether an SDK call failed with HTTP 409.
func isConflict(err error) bool { return httpStatus(err) == http.StatusConflict }

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

// conflictCheckoutID returns the open checkout session id Mumbai names in a 409 body, if any.
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
