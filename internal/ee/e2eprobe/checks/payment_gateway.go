package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
)

// TestCard names the card behavior a probe needs, independent of gateway.
type TestCard string

const (
	// TestCardSuccess vaults and charges cleanly.
	TestCardSuccess TestCard = "success"
	// TestCardDecline vaults cleanly but every charge against it is declined.
	TestCardDecline TestCard = "decline"
)

// ErrTestCardUnsupported means the driver has no card for the requested behavior.
var ErrTestCardUnsupported = errors.New("test card not available for this gateway")

// GatewayDriver does, directly against a gateway's test mode, what a customer
// would otherwise do on a hosted page.
type GatewayDriver interface {
	// AttachCard vaults a test card on the gateway customer and returns the
	// gateway's id for the saved method.
	AttachCard(ctx context.Context, gatewayCustomerID string, card TestCard) (string, error)
}

// NewGatewayDriver returns the driver for cfg, or nil when no gateway test
// credentials are configured and card-dependent flows must be skipped.
func NewGatewayDriver(cfg e2eprobe.PaymentProviderConfig) GatewayDriver {
	client := &http.Client{Timeout: 30 * time.Second}
	switch cfg.Provider {
	case "stripe":
		if cfg.StripeSecretKey == "" {
			return nil
		}
		return &stripeDriver{baseURL: "https://api.stripe.com", secretKey: cfg.StripeSecretKey, http: client}
	case "chargebee":
		if cfg.ChargebeeAPIKey == "" {
			return nil
		}
		return &chargebeeDriver{
			baseURL:     fmt.Sprintf("https://%s.chargebee.com", cfg.ChargebeeSite),
			apiKey:      cfg.ChargebeeAPIKey,
			declineCard: cfg.ChargebeeDeclineCard,
			http:        client,
		}
	}
	return nil
}

// stripeDriver attaches Stripe's predefined test payment methods.
type stripeDriver struct {
	baseURL   string
	secretKey string
	http      *http.Client
}

// stripeTestMethods maps behaviors to Stripe test tokens. chargeCustomerFail
// attaches cleanly and fails every later charge, which an off-session decline needs.
var stripeTestMethods = map[TestCard]string{
	TestCardSuccess: "pm_card_visa",
	TestCardDecline: "pm_card_chargeCustomerFail",
}

func (d *stripeDriver) AttachCard(ctx context.Context, gatewayCustomerID string, card TestCard) (string, error) {
	token, ok := stripeTestMethods[card]
	if !ok {
		return "", ErrTestCardUnsupported
	}
	var out struct {
		ID string `json:"id"`
	}
	endpoint := d.baseURL + "/v1/payment_methods/" + token + "/attach"
	err := postGatewayForm(ctx, d.http, endpoint, url.Values{"customer": {gatewayCustomerID}}, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+d.secretKey)
	}, &out)
	if err != nil {
		return "", fmt.Errorf("stripe attach %s: %w", token, err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("stripe attach %s returned no payment method id", token)
	}
	return out.ID, nil
}

// chargebeeDriver creates card payment sources on a Chargebee test site.
type chargebeeDriver struct {
	baseURL     string
	apiKey      string
	declineCard string
	http        *http.Client
}

// chargebeeSuccessCard is the Chargebee test gateway's always-approve card.
const chargebeeSuccessCard = "4111111111111111"

func (d *chargebeeDriver) AttachCard(ctx context.Context, gatewayCustomerID string, card TestCard) (string, error) {
	number := chargebeeSuccessCard
	if card == TestCardDecline {
		if d.declineCard == "" {
			return "", ErrTestCardUnsupported
		}
		number = d.declineCard
	}
	form := url.Values{
		"customer_id":        {gatewayCustomerID},
		"card[number]":       {number},
		"card[cvv]":          {"123"},
		"card[expiry_month]": {"12"},
		"card[expiry_year]":  {fmt.Sprint(time.Now().Year() + 3)},
	}
	var out struct {
		PaymentSource struct {
			ID string `json:"id"`
		} `json:"payment_source"`
	}
	endpoint := d.baseURL + "/api/v2/payment_sources/create_card"
	err := postGatewayForm(ctx, d.http, endpoint, form, func(r *http.Request) {
		r.SetBasicAuth(d.apiKey, "")
	}, &out)
	if err != nil {
		return "", fmt.Errorf("chargebee create_card: %w", err)
	}
	if out.PaymentSource.ID == "" {
		return "", fmt.Errorf("chargebee create_card returned no payment source id")
	}
	return out.PaymentSource.ID, nil
}

// postGatewayForm sends a form-encoded POST to a gateway API and decodes the JSON reply.
func postGatewayForm(ctx context.Context, client *http.Client, endpoint string, form url.Values, auth func(*http.Request), out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	auth(req)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		if len(raw) > 500 {
			raw = raw[:500]
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, raw)
	}
	return json.Unmarshal(raw, out)
}
