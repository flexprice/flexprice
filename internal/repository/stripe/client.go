package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/stripe"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
)

// StripeAPIClient implements the stripe.Client interface
type StripeAPIClient struct {
	httpClient httpclient.Client
	apiKey     string
	baseURL    string
	log        *logger.Logger
}

// NewStripeAPIClient creates a new Stripe API client
func NewStripeAPIClient(httpClient httpclient.Client, apiKey string, log *logger.Logger) stripe.Client {
	return &StripeAPIClient{
		httpClient: httpClient,
		apiKey:     apiKey,
		baseURL:    "https://api.stripe.com/v1",
		log:        log,
	}
}

// Customer operations
func (c *StripeAPIClient) CreateCustomer(ctx context.Context, req *stripe.CreateCustomerRequest) (*stripe.StripeCustomer, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	c.log.Debugw("creating stripe customer", "email", req.Email, "name", req.Name)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to marshal customer request").
			Mark(ierr.ErrValidation)
	}

	httpReq := &httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/customers",
		Headers: map[string]string{
			"Authorization": "Bearer " + c.apiKey,
			"Content-Type":  "application/json",
		},
		Body: body,
	}

	resp, err := c.httpClient.Send(ctx, httpReq)
	if err != nil {
		return nil, c.handleAPIError(err, "create customer")
	}

	var customer stripe.StripeCustomer
	if err := json.Unmarshal(resp.Body, &customer); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse customer response").
			Mark(ierr.ErrHTTPClient)
	}

	return &customer, nil
}

func (c *StripeAPIClient) GetCustomer(ctx context.Context, customerID string) (*stripe.StripeCustomer, error) {
	if customerID == "" {
		return nil, ierr.NewError("customer ID is required").
			WithHint("Customer ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	c.log.Debugw("getting stripe customer", "customer_id", customerID)

	httpReq := &httpclient.Request{
		Method: http.MethodGet,
		URL:    c.baseURL + "/customers/" + customerID,
		Headers: map[string]string{
			"Authorization": "Bearer " + c.apiKey,
		},
	}

	resp, err := c.httpClient.Send(ctx, httpReq)
	if err != nil {
		return nil, c.handleAPIError(err, "get customer")
	}

	var customer stripe.StripeCustomer
	if err := json.Unmarshal(resp.Body, &customer); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse customer response").
			Mark(ierr.ErrHTTPClient)
	}

	return &customer, nil
}

func (c *StripeAPIClient) UpdateCustomer(ctx context.Context, customerID string, req *stripe.UpdateCustomerRequest) (*stripe.StripeCustomer, error) {
	if customerID == "" {
		return nil, ierr.NewError("customer ID is required").
			WithHint("Customer ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	c.log.Debugw("updating stripe customer", "customer_id", customerID)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to marshal customer update request").
			Mark(ierr.ErrValidation)
	}

	httpReq := &httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/customers/" + customerID,
		Headers: map[string]string{
			"Authorization": "Bearer " + c.apiKey,
			"Content-Type":  "application/json",
		},
		Body: body,
	}

	resp, err := c.httpClient.Send(ctx, httpReq)
	if err != nil {
		return nil, c.handleAPIError(err, "update customer")
	}

	var customer stripe.StripeCustomer
	if err := json.Unmarshal(resp.Body, &customer); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse customer response").
			Mark(ierr.ErrHTTPClient)
	}

	return &customer, nil
}

// Meter operations
func (c *StripeAPIClient) CreateMeter(ctx context.Context, req *stripe.CreateMeterRequest) (*stripe.StripeMeter, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	c.log.Debugw("creating stripe meter", "display_name", req.DisplayName, "event_name", req.EventName)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to marshal meter request").
			Mark(ierr.ErrValidation)
	}

	httpReq := &httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/billing/meters",
		Headers: map[string]string{
			"Authorization": "Bearer " + c.apiKey,
			"Content-Type":  "application/json",
		},
		Body: body,
	}

	resp, err := c.httpClient.Send(ctx, httpReq)
	if err != nil {
		return nil, c.handleAPIError(err, "create meter")
	}

	var meter stripe.StripeMeter
	if err := json.Unmarshal(resp.Body, &meter); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse meter response").
			Mark(ierr.ErrHTTPClient)
	}

	return &meter, nil
}

func (c *StripeAPIClient) GetMeter(ctx context.Context, meterID string) (*stripe.StripeMeter, error) {
	if meterID == "" {
		return nil, ierr.NewError("meter ID is required").
			WithHint("Meter ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	c.log.Debugw("getting stripe meter", "meter_id", meterID)

	httpReq := &httpclient.Request{
		Method: http.MethodGet,
		URL:    c.baseURL + "/billing/meters/" + meterID,
		Headers: map[string]string{
			"Authorization": "Bearer " + c.apiKey,
		},
	}

	resp, err := c.httpClient.Send(ctx, httpReq)
	if err != nil {
		return nil, c.handleAPIError(err, "get meter")
	}

	// Stripe returns numeric timestamps; to avoid time.UnmarshalJSON issues, unmarshal only required fields.
	var light struct {
		ID        string `json:"id"`
		EventName string `json:"event_name"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(resp.Body, &light); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse meter response (lightweight)").
			Mark(ierr.ErrHTTPClient)
	}

	return &stripe.StripeMeter{
		ID:        light.ID,
		EventName: light.EventName,
		Status:    light.Status,
	}, nil
}

func (c *StripeAPIClient) UpdateMeter(ctx context.Context, meterID string, req *stripe.UpdateMeterRequest) (*stripe.StripeMeter, error) {
	if meterID == "" {
		return nil, ierr.NewError("meter ID is required").
			WithHint("Meter ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	c.log.Debugw("updating stripe meter", "meter_id", meterID)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to marshal meter update request").
			Mark(ierr.ErrValidation)
	}

	httpReq := &httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/billing/meters/" + meterID,
		Headers: map[string]string{
			"Authorization": "Bearer " + c.apiKey,
			"Content-Type":  "application/json",
		},
		Body: body,
	}

	resp, err := c.httpClient.Send(ctx, httpReq)
	if err != nil {
		return nil, c.handleAPIError(err, "update meter")
	}

	var meter stripe.StripeMeter
	if err := json.Unmarshal(resp.Body, &meter); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse meter response").
			Mark(ierr.ErrHTTPClient)
	}

	return &meter, nil
}

func (c *StripeAPIClient) ListMeters(ctx context.Context, req *stripe.ListMetersRequest) ([]*stripe.StripeMeter, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	c.log.Debugw("listing stripe meters")

	url := c.baseURL + "/billing/meters"
	if req.Limit != nil || req.StartingAfter != nil || req.EndingBefore != nil || req.Status != nil {
		params := []string{}
		if req.Limit != nil {
			params = append(params, fmt.Sprintf("limit=%d", *req.Limit))
		}
		if req.StartingAfter != nil {
			params = append(params, fmt.Sprintf("starting_after=%s", *req.StartingAfter))
		}
		if req.EndingBefore != nil {
			params = append(params, fmt.Sprintf("ending_before=%s", *req.EndingBefore))
		}
		if req.Status != nil {
			params = append(params, fmt.Sprintf("status=%s", *req.Status))
		}
		if len(params) > 0 {
			url += "?" + strings.Join(params, "&")
		}
	}

	httpReq := &httpclient.Request{
		Method: http.MethodGet,
		URL:    url,
		Headers: map[string]string{
			"Authorization": "Bearer " + c.apiKey,
		},
	}

	resp, err := c.httpClient.Send(ctx, httpReq)
	if err != nil {
		return nil, c.handleAPIError(err, "list meters")
	}

	var listResp struct {
		Data []*stripe.StripeMeter `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &listResp); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse meters list response").
			Mark(ierr.ErrHTTPClient)
	}

	return listResp.Data, nil
}

// Usage/Event operations
func (c *StripeAPIClient) CreateMeterEvent(ctx context.Context, req *stripe.CreateMeterEventRequest) (*stripe.StripeAPIResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	c.log.Debugw("creating stripe meter event", "event_name", req.EventName, "identifier", req.Identifier)

	// Stripe expects application/x-www-form-urlencoded by default.
	values := url.Values{}
	values.Set("event_name", req.EventName)
	values.Set("identifier", req.Identifier)
	values.Set("timestamp", strconv.FormatInt(req.Timestamp.Unix(), 10))

	// Encode payload as nested form fields e.g. payload[value]=10 & payload[stripe_customer_id]=cus_123
	for k, v := range req.Payload {
		var s string
		switch vv := v.(type) {
		case string:
			s = vv
		case fmt.Stringer:
			s = vv.String()
		case float64:
			s = strconv.FormatFloat(vv, 'f', -1, 64)
		case float32:
			s = strconv.FormatFloat(float64(vv), 'f', -1, 32)
		case int:
			s = strconv.Itoa(vv)
		case int64:
			s = strconv.FormatInt(vv, 10)
		case uint64:
			s = strconv.FormatUint(vv, 10)
		default:
			// Fallback to JSON encoding
			b, _ := json.Marshal(v)
			s = string(b)
		}
		values.Set(fmt.Sprintf("payload[%s]", k), s)
	}

	httpReq := &httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/billing/meter_events",
		Headers: map[string]string{
			"Authorization": "Bearer " + c.apiKey,
			"Content-Type":  "application/x-www-form-urlencoded",
		},
		Body: []byte(values.Encode()),
	}

	resp, err := c.httpClient.Send(ctx, httpReq)
	if err != nil {
		return nil, c.handleAPIError(err, "create meter event")
	}

	// Log raw response for easier debugging (truncated to avoid huge logs)
	truncatedBody := string(resp.Body)
	if len(truncatedBody) > 1000 {
		truncatedBody = truncatedBody[:1000] + "..."
	}
	c.log.Debugw("stripe meter event response", "status_code", resp.StatusCode, "body", truncatedBody)

	var apiResp stripe.StripeAPIResponse
	if err := json.Unmarshal(resp.Body, &apiResp); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse meter event response").
			Mark(ierr.ErrHTTPClient)
	}

	return &apiResp, nil
}

func (c *StripeAPIClient) CreateMeterEventBatch(ctx context.Context, req *stripe.CreateMeterEventBatchRequest) (*stripe.StripeAPIResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	c.log.Debugw("creating stripe meter event batch", "event_count", len(req.Events))

	body, err := json.Marshal(req)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to marshal meter event batch request").
			Mark(ierr.ErrValidation)
	}

	httpReq := &httpclient.Request{
		Method: http.MethodPost,
		URL:    c.baseURL + "/billing/meter_events/batch",
		Headers: map[string]string{
			"Authorization": "Bearer " + c.apiKey,
			"Content-Type":  "application/json",
		},
		Body: body,
	}

	resp, err := c.httpClient.Send(ctx, httpReq)
	if err != nil {
		return nil, c.handleAPIError(err, "create meter event batch")
	}

	var apiResp stripe.StripeAPIResponse
	if err := json.Unmarshal(resp.Body, &apiResp); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse meter event batch response").
			Mark(ierr.ErrHTTPClient)
	}

	return &apiResp, nil
}

func (c *StripeAPIClient) GetUsageRecordSummary(ctx context.Context, req *stripe.GetUsageRecordSummaryRequest) (*stripe.StripeUsageRecordSummary, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	c.log.Debugw("getting stripe usage record summary", "subscription_item", req.SubscriptionItem)

	url := c.baseURL + "/subscription_items/" + req.SubscriptionItem + "/usage_record_summaries"
	params := []string{}
	if req.StartTime != nil {
		params = append(params, fmt.Sprintf("starting_after=%d", req.StartTime.Unix()))
	}
	if req.EndTime != nil {
		params = append(params, fmt.Sprintf("ending_before=%d", req.EndTime.Unix()))
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	httpReq := &httpclient.Request{
		Method: http.MethodGet,
		URL:    url,
		Headers: map[string]string{
			"Authorization": "Bearer " + c.apiKey,
		},
	}

	resp, err := c.httpClient.Send(ctx, httpReq)
	if err != nil {
		return nil, c.handleAPIError(err, "get usage record summary")
	}

	var summary stripe.StripeUsageRecordSummary
	if err := json.Unmarshal(resp.Body, &summary); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse usage record summary response").
			Mark(ierr.ErrHTTPClient)
	}

	return &summary, nil
}

// Webhook operations
func (c *StripeAPIClient) ValidateWebhookSignature(ctx context.Context, payload []byte, signature string, secret string) error {
	if len(payload) == 0 {
		return ierr.NewError("payload is empty").
			WithHint("Webhook payload must not be empty").
			Mark(ierr.ErrValidation)
	}

	if signature == "" {
		return ierr.NewError("signature is empty").
			WithHint("Webhook signature must not be empty").
			Mark(ierr.ErrValidation)
	}

	if secret == "" {
		return ierr.NewError("secret is empty").
			WithHint("Webhook secret must not be empty").
			Mark(ierr.ErrValidation)
	}

	c.log.Debugw("validating stripe webhook signature")

	// Parse signature header
	parts := strings.Split(signature, ",")
	var timestamp string
	var v1Signature string

	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			timestamp = kv[1]
		case "v1":
			v1Signature = kv[1]
		}
	}

	if timestamp == "" || v1Signature == "" {
		return ierr.NewError("invalid signature format").
			WithHint("Signature must contain timestamp and v1 hash").
			Mark(ierr.ErrValidation)
	}

	// Compute expected signature
	signedPayload := timestamp + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	// Compare signatures
	if !hmac.Equal([]byte(v1Signature), []byte(expectedSignature)) {
		return ierr.NewError("signature verification failed").
			WithHint("Webhook signature does not match expected value").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (c *StripeAPIClient) ParseWebhookPayload(ctx context.Context, payload []byte, eventType string) (interface{}, error) {
	if len(payload) == 0 {
		return nil, ierr.NewError("payload is empty").
			WithHint("Webhook payload must not be empty").
			Mark(ierr.ErrValidation)
	}

	if eventType == "" {
		return nil, ierr.NewError("event type is empty").
			WithHint("Event type must not be empty").
			Mark(ierr.ErrValidation)
	}

	c.log.Debugw("parsing stripe webhook payload", "event_type", eventType)

	// Parse the generic event structure first
	var event struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse webhook event").
			Mark(ierr.ErrValidation)
	}

	// Parse based on event type
	switch event.Type {
	case "customer.created", "customer.updated":
		var customerData struct {
			Object stripe.StripeCustomer `json:"object"`
		}
		if err := json.Unmarshal(event.Data, &customerData); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to parse customer webhook data").
				Mark(ierr.ErrValidation)
		}
		return &customerData.Object, nil

	default:
		// Return raw data for unknown event types
		var data interface{}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to parse webhook data").
				Mark(ierr.ErrValidation)
		}
		return data, nil
	}
}

// Helper method to handle API errors
func (c *StripeAPIClient) handleAPIError(err error, operation string) error {
	// If this is an HTTP error, extract more context for logs
	if httpErr, ok := httpclient.IsHTTPError(err); ok {
		respBody := string(httpErr.Response)
		if len(respBody) > 1000 {
			respBody = respBody[:1000] + "..."
		}
		c.log.Errorw("stripe api error", "operation", operation, "status_code", httpErr.StatusCode, "response", respBody)
	} else {
		c.log.Errorw("stripe api error", "operation", operation, "error", err)
	}

	return ierr.WithError(err).
		WithHint("Stripe API operation failed").
		WithReportableDetails(map[string]interface{}{
			"operation": operation,
		}).
		Mark(ierr.ErrHTTPClient)
}
