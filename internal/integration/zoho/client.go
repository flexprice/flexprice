package zoho

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

type ZohoClient interface {
	HasZohoBooksConnection(ctx context.Context) bool
	QueryContactByEmail(ctx context.Context, email string) (*ContactResponse, error)
	CreateContact(ctx context.Context, req *ContactCreateRequest) (*ContactResponse, error)
	CreateInvoice(ctx context.Context, req *InvoiceCreateRequest) (*InvoiceResponse, error)
	// ResolveInvoiceCurrency returns currency_code and exchange_rate for Zoho create-invoice (base-currency conversion per Zoho Books).
	ResolveInvoiceCurrency(ctx context.Context, invoiceCurrency string) (currencyCode string, exchangeRate float64, err error)
	// GetZohoBooksWebhookConfig loads the published connection and returns the decrypted webhook signing secret (empty if unset).
	GetZohoBooksWebhookConfig(ctx context.Context) (*connection.Connection, string, error)
}

type Client struct {
	connectionRepo    connection.Repository
	encryptionService security.EncryptionService
	httpClient        *http.Client
	logger            *logger.Logger
}

func NewClient(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
) ZohoClient {
	return &Client{
		connectionRepo:    connectionRepo,
		encryptionService: encryptionService,
		httpClient:        &http.Client{Timeout: 30 * time.Second},
		logger:            logger,
	}
}

func (c *Client) HasZohoBooksConnection(ctx context.Context) bool {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderZohoBooks)
	return err == nil && conn != nil && conn.Status == types.StatusPublished
}

// GetZohoBooksWebhookConfig implements ZohoClient (used by HTTP webhooks, parallel to Stripe GetStripeClient).
func (c *Client) GetZohoBooksWebhookConfig(ctx context.Context) (*connection.Connection, string, error) {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderZohoBooks)
	if err != nil {
		return nil, "", err
	}
	if conn == nil {
		return nil, "", ierr.NewError("Zoho Books connection not configured").Mark(ierr.ErrNotFound)
	}
	zb := conn.EncryptedSecretData.ZohoBooks
	if zb == nil {
		return nil, "", ierr.NewError("Zoho Books metadata missing").Mark(ierr.ErrNotFound)
	}
	plain := ""
	if zb.WebhookSecret != "" {
		dec, decErr := c.encryptionService.Decrypt(zb.WebhookSecret)
		if decErr != nil {
			return nil, "", ierr.WithError(decErr).WithHint("failed to decrypt Zoho webhook secret").Mark(ierr.ErrInternal)
		}
		plain = strings.TrimSpace(dec)
	}
	return conn, plain, nil
}

func (c *Client) QueryContactByEmail(ctx context.Context, email string) (*ContactResponse, error) {
	if strings.TrimSpace(email) == "" {
		return nil, nil
	}
	var resp struct {
		Contacts []ContactResponse `json:"contacts"`
	}
	err := c.doBooksRequest(ctx, http.MethodGet, "/books/v3/contacts", map[string]string{"email": email}, nil, &resp)
	if err != nil {
		return nil, err
	}
	if len(resp.Contacts) == 0 {
		return nil, nil
	}
	return &resp.Contacts[0], nil
}

func (c *Client) CreateContact(ctx context.Context, req *ContactCreateRequest) (*ContactResponse, error) {
	var resp struct {
		Contact ContactResponse `json:"contact"`
	}
	if err := c.doBooksRequest(ctx, http.MethodPost, "/books/v3/contacts", nil, req, &resp); err != nil {
		return nil, err
	}
	return &resp.Contact, nil
}

func (c *Client) CreateInvoice(ctx context.Context, req *InvoiceCreateRequest) (*InvoiceResponse, error) {
	var resp struct {
		Invoice InvoiceResponse `json:"invoice"`
	}
	if err := c.doBooksRequest(ctx, http.MethodPost, "/books/v3/invoices", nil, req, &resp); err != nil {
		return nil, err
	}
	return &resp.Invoice, nil
}

// ResolveInvoiceCurrency maps a FlexPrice invoice currency to Zoho Books invoice fields.
// If the invoice currency matches the Zoho organization base currency, exchange_rate is 1.
// Otherwise the rate comes from Zoho Settings → Currencies (invoice currency must be enabled there).
func (c *Client) ResolveInvoiceCurrency(ctx context.Context, invoiceCurrency string) (string, float64, error) {
	code := strings.TrimSpace(strings.ToUpper(invoiceCurrency))
	if code == "" {
		return "", 0, ierr.NewError("invoice currency is empty").
			WithHint("FlexPrice invoices must have a currency before syncing to Zoho Books").
			Mark(ierr.ErrValidation)
	}

	base, err := c.getOrganizationBaseCurrency(ctx)
	if err != nil {
		return "", 0, err
	}
	base = strings.TrimSpace(strings.ToUpper(base))
	if base == "" {
		return "", 0, ierr.NewError("Zoho organization base currency is empty").
			WithHint("Check Zoho Books organization settings").
			Mark(ierr.ErrInternal)
	}

	if code == base {
		return code, 1, nil
	}

	rate, err := c.getBooksExchangeRate(ctx, code)
	if err != nil {
		return "", 0, err
	}
	if rate <= 0 {
		return "", 0, ierr.NewError("invalid Zoho exchange rate for invoice currency").
			WithHintf("Set a positive exchange rate for %s under Zoho Books → Settings → Currencies", code).
			Mark(ierr.ErrValidation)
	}
	return code, rate, nil
}

func (c *Client) getOrganizationBaseCurrency(ctx context.Context) (string, error) {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderZohoBooks)
	if err != nil {
		return "", err
	}
	if conn == nil || conn.EncryptedSecretData.ZohoBooks == nil {
		return "", ierr.NewError("Zoho Books connection not configured").Mark(ierr.ErrNotFound)
	}
	orgID := strings.TrimSpace(conn.EncryptedSecretData.ZohoBooks.OrganizationID)
	if orgID == "" {
		return "", ierr.NewError("Zoho organization_id missing on connection").Mark(ierr.ErrValidation)
	}

	var resp struct {
		Organization struct {
			CurrencyCode string `json:"currency_code"`
		} `json:"organization"`
	}
	path := fmt.Sprintf("/books/v3/organizations/%s", orgID)
	if err := c.doBooksRequest(ctx, http.MethodGet, path, nil, nil, &resp); err != nil {
		return "", err
	}
	return resp.Organization.CurrencyCode, nil
}

func (c *Client) getBooksExchangeRate(ctx context.Context, invoiceCurrency string) (float64, error) {
	var resp struct {
		Currencies []struct {
			CurrencyCode string  `json:"currency_code"`
			ExchangeRate float64 `json:"exchange_rate"`
		} `json:"currencies"`
	}
	if err := c.doBooksRequest(ctx, http.MethodGet, "/books/v3/settings/currencies", nil, nil, &resp); err != nil {
		return 0, err
	}
	want := strings.TrimSpace(strings.ToUpper(invoiceCurrency))
	for _, cur := range resp.Currencies {
		if strings.EqualFold(strings.TrimSpace(cur.CurrencyCode), want) {
			return cur.ExchangeRate, nil
		}
	}
	return 0, ierr.NewError(fmt.Sprintf("currency %s is not enabled in Zoho Books", want)).
		WithHint("Add it under Zoho Books → Settings → Currencies with an exchange rate vs your base currency, then retry.").
		Mark(ierr.ErrValidation)
}

func (c *Client) doBooksRequest(
	ctx context.Context,
	method string,
	path string,
	query map[string]string,
	reqBody interface{},
	respBody interface{},
) error {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderZohoBooks)
	if err != nil {
		return err
	}
	if conn == nil || conn.EncryptedSecretData.ZohoBooks == nil {
		return ierr.NewError("Zoho Books connection not configured").Mark(ierr.ErrNotFound)
	}
	md := conn.EncryptedSecretData.ZohoBooks

	accessToken, err := c.getValidAccessToken(ctx, conn)
	if err != nil {
		return err
	}

	baseURL := strings.TrimRight(md.APIDomain, "/")
	if baseURL == "" {
		baseURL = "https://www.zohoapis.com"
	}
	u, err := url.Parse(baseURL + path)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("organization_id", md.OrganizationID)
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	var bodyReader io.Reader
	if reqBody != nil {
		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Zoho-oauthtoken "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ierr.WithError(err).WithHint("Zoho API request failed").Mark(ierr.ErrHTTPClient)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ierr.NewError("Zoho API request failed").
			WithHintf("Zoho API returned status %d", resp.StatusCode).
			WithReportableDetails(map[string]interface{}{"response_body": string(bodyBytes)}).
			Mark(ierr.ErrHTTPClient)
	}
	if respBody != nil && len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, respBody); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) getValidAccessToken(ctx context.Context, conn *connection.Connection) (string, error) {
	md := conn.EncryptedSecretData.ZohoBooks
	if md == nil {
		return "", ierr.NewError("Zoho Books metadata missing").Mark(ierr.ErrNotFound)
	}

	if md.AccessToken != "" && md.AccessTokenExpiresAt != "" {
		exp, err := time.Parse(time.RFC3339, md.AccessTokenExpiresAt)
		if err == nil && time.Now().UTC().Before(exp.Add(-1*time.Minute)) {
			token, err := c.encryptionService.Decrypt(md.AccessToken)
			if err == nil && token != "" {
				return token, nil
			}
		}
	}

	refreshToken, err := c.encryptionService.Decrypt(md.RefreshToken)
	if err != nil {
		return "", err
	}
	clientID, err := c.encryptionService.Decrypt(md.ClientID)
	if err != nil {
		return "", err
	}
	clientSecret, err := c.encryptionService.Decrypt(md.ClientSecret)
	if err != nil {
		return "", err
	}

	accountsServer := strings.TrimRight(md.AccountsURL, "/")
	if accountsServer == "" {
		accountsServer = "https://accounts.zoho.com"
	}
	form := url.Values{}
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("grant_type", "refresh_token")

	tokenURL := fmt.Sprintf("%s/oauth/v2/token", accountsServer)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", ierr.WithError(err).WithHint("Zoho token refresh request failed").Mark(ierr.ErrHTTPClient)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", ierr.NewError("Zoho token refresh failed").
			WithHintf("Zoho token endpoint returned status %d", resp.StatusCode).
			WithReportableDetails(map[string]interface{}{"response_body": string(bodyBytes)}).
			Mark(ierr.ErrHTTPClient)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		APIDomain   string `json:"api_domain"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return "", err
	}

	encAccess, err := c.encryptionService.Encrypt(tokenResp.AccessToken)
	if err != nil {
		return "", err
	}

	md.AccessToken = encAccess
	if tokenResp.APIDomain != "" {
		md.APIDomain = tokenResp.APIDomain
	}
	md.AccessTokenExpiresAt = time.Now().UTC().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339)
	conn.EncryptedSecretData.ZohoBooks = md
	conn.UpdatedAt = time.Now().UTC()

	if err := c.connectionRepo.Update(ctx, conn); err != nil {
		return "", err
	}
	return tokenResp.AccessToken, nil
}
