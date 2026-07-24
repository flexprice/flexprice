// Package azuremarketplace wraps the Azure calls the marketplace integration needs: an OAuth2
// client_credentials token exchange against a tenant's own Entra app, and the Marketplace SaaS API's
// usageEvent call (to report usage). Unlike AWS/GCP, Flexprice holds no static identity of its own
// here — every call authenticates as the tenant's Entra app, using credentials the tenant pastes in.
package azuremarketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
)

const (
	tokenURLTemplate      = "https://login.microsoftonline.com/%s/oauth2/v2.0/token"
	marketplaceAPIBaseURL = "https://marketplaceapi.microsoft.com/api"
	apiVersion            = "2018-08-31"

	// marketplaceSaaSResourceID identifies the Marketplace SaaS/metering API itself in Microsoft
	// Entra ID — the same value for every publisher, never tenant-specific, never a secret. It is
	// used below as the OAuth2 "scope" (as "{id}/.default") when requesting a token, telling Entra ID
	// which API the token should be valid for; without it Entra would have no API to scope the token
	// to. Before the tenant's app can request a token scoped to it, the tenant must have registered a
	// service principal for this ID in their own directory (a one-time step on their side —
	// `az ad sp create --id 20e940b3-4c77-4b0b-9a53-9e16a1b010a7`, per Microsoft's SaaS registration
	// guide). Flexprice never creates or touches this service principal itself.
	marketplaceSaaSResourceID = "20e940b3-4c77-4b0b-9a53-9e16a1b010a7"

	// defaultTokenTTL is used if the token response omits expires_in.
	defaultTokenTTL = time.Hour

	// azureTimeLayout is the timestamp format Azure's usage event API expects: UTC, no zone suffix.
	azureTimeLayout = "2006-01-02T15:04:05"
)

// Token is a bearer token for the Marketplace SaaS API, scoped to one tenant's Entra app.
type Token struct {
	AccessToken string
	ExpiresAt   time.Time
}

// UsageEventInput is one usage event to report.
type UsageEventInput struct {
	ResourceID         string
	Dimension          string
	PlanID             string
	Quantity           float64
	EffectiveStartTime time.Time
}

// UsageEventResult is Azure's outcome for one usageEvent call. Unlike the AWS/GCP clients, a nil
// error here always means accepted: Microsoft's own docs guarantee HTTP 200 carries "Accepted" as
// the only possible status for the single-event endpoint, and every rejection (Duplicate, Expired,
// invalid resource, etc.) comes back as a distinct non-2xx status, which httpclient.Client.Send
// already turns into an error.
type UsageEventResult struct {
	UsageEventID string
}

// Client is the set of Azure Marketplace operations the integration uses.
type Client interface {
	// GetToken exchanges the tenant's Entra app credentials for a bearer token via client_credentials.
	// Used both to verify a connection at creation time and to authenticate each report.
	GetToken(ctx context.Context, tenantID, clientID, clientSecret string) (Token, error)

	// ReportUsageEvent reports a single usage event. A nil error means Azure accepted it; any
	// rejection (Duplicate, Expired, invalid resource, malformed request) surfaces as a non-nil error.
	ReportUsageEvent(ctx context.Context, token Token, record UsageEventInput) (*UsageEventResult, error)
}

type client struct {
	httpClient httpclient.Client
	logger     *logger.Logger
}

// NewClient builds a stateless Azure Marketplace client.
func NewClient(log *logger.Logger) Client {
	return &client{httpClient: httpclient.NewDefaultClient(), logger: log}
}

// GetToken requests a client_credentials token scoped to the Marketplace SaaS API resource ID. The
// tenant's own Entra app and client secret sign the request; on failure the raw error is not logged,
// since it may echo request parameters.
func (c *client) GetToken(ctx context.Context, tenantID, clientID, clientSecret string) (Token, error) {
	if tenantID == "" || clientID == "" || clientSecret == "" {
		return Token{}, ierr.NewError("tenant_id, client_id and client_secret are required").
			WithHint("Azure Marketplace connection requires tenant_id, client_id and client_secret").
			Mark(ierr.ErrValidation)
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("scope", marketplaceSaaSResourceID+"/.default")

	resp, err := c.httpClient.Send(ctx, &httpclient.Request{
		Method:  http.MethodPost,
		URL:     fmt.Sprintf(tokenURLTemplate, tenantID),
		Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		Body:    []byte(form.Encode()),
	})
	if err != nil {
		c.logger.Error(ctx, "azure marketplace token request failed",
			"error", "redacted: error response may echo request parameters")
		return Token{}, ierr.NewError("azure marketplace token request failed").
			WithHint("Failed to authenticate with the provided tenant_id, client_id and client_secret.").
			Mark(ierr.ErrValidation)
	}

	// expires_in's JSON type is not consistent across documentation and live responses — Microsoft's
	// own registration guide shows it quoted ("3600") while the real v2.0 endpoint returns a bare
	// number, matching the OAuth2 RFC 6749 §5.1 shape. Decoding into interface{} accepts either.
	var parsed struct {
		AccessToken string      `json:"access_token"`
		ExpiresIn   interface{} `json:"expires_in"`
	}
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return Token{}, ierr.WithError(err).
			WithHint("Azure Marketplace token response could not be parsed").
			Mark(ierr.ErrHTTPClient)
	}
	if parsed.AccessToken == "" {
		return Token{}, ierr.NewError("azure marketplace token response has no access_token").
			Mark(ierr.ErrHTTPClient)
	}

	ttl := defaultTokenTTL
	switch v := parsed.ExpiresIn.(type) {
	case string:
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			ttl = time.Duration(seconds) * time.Second
		}
	case float64:
		if v > 0 {
			ttl = time.Duration(v) * time.Second
		}
	}

	return Token{AccessToken: parsed.AccessToken, ExpiresAt: time.Now().UTC().Add(ttl)}, nil
}

// azureUsageEventWire is the usageEvent wire format.
type azureUsageEventWire struct {
	ResourceID         string  `json:"resourceId"`
	Quantity           float64 `json:"quantity"`
	Dimension          string  `json:"dimension"`
	EffectiveStartTime string  `json:"effectiveStartTime"`
	PlanID             string  `json:"planId"`
}

// azureUsageEventResultWire is the usageEvent response body. status is always "Accepted" on a 200
// (Microsoft's own doc: "this is the only value in case of single usage event"), so it isn't read.
type azureUsageEventResultWire struct {
	UsageEventID string `json:"usageEventId"`
}

// ReportUsageEvent reports a single usage event to Azure Marketplace.
func (c *client) ReportUsageEvent(ctx context.Context, token Token, record UsageEventInput) (*UsageEventResult, error) {
	wire := azureUsageEventWire{
		ResourceID:         record.ResourceID,
		Quantity:           record.Quantity,
		Dimension:          record.Dimension,
		EffectiveStartTime: record.EffectiveStartTime.UTC().Format(azureTimeLayout),
		PlanID:             record.PlanID,
	}

	body, err := json.Marshal(wire)
	if err != nil {
		return nil, ierr.WithError(err).Mark(ierr.ErrValidation)
	}

	resp, err := c.httpClient.Send(ctx, &httpclient.Request{
		Method: http.MethodPost,
		URL:    fmt.Sprintf("%s/usageEvent?api-version=%s", marketplaceAPIBaseURL, apiVersion),
		Headers: map[string]string{
			"Authorization": "Bearer " + token.AccessToken,
			"Content-Type":  "application/json",
		},
		Body: body,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("usageEvent call failed").
			Mark(ierr.ErrHTTPClient)
	}

	var parsed azureUsageEventResultWire
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Azure Marketplace usageEvent response could not be parsed").
			Mark(ierr.ErrHTTPClient)
	}

	return &UsageEventResult{UsageEventID: parsed.UsageEventID}, nil
}
