package zoho

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

type idRepo struct {
	connection.Repository
	conn *connection.Connection
}

func (r *idRepo) GetByProvider(context.Context, types.SecretProvider) (*connection.Connection, error) {
	return r.conn, nil
}

type idEnc struct{ security.EncryptionService }

func (idEnc) Decrypt(s string) (string, error) { return s, nil }

type captureRT struct{ urls []string }

func (c *captureRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.urls = append(c.urls, r.URL.String())
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}}, nil
}

func newIDClient(apiDomain, org string) (*Client, *captureRT) {
	rt := &captureRT{}
	conn := &connection.Connection{EncryptedSecretData: types.ConnectionMetadata{ZohoBooks: &types.ZohoBooksConnectionMetadata{
		AccessToken:          "tok",
		AccessTokenExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
		APIDomain:            apiDomain,
		OrganizationID:       org,
	}}}
	return &Client{
		connectionRepo:    &idRepo{conn: conn},
		encryptionService: idEnc{},
		httpClient:        &http.Client{Transport: rt},
		logger:            logger.NewNoopLogger(),
	}, rt
}

func TestDoBooksRequest_RejectsNonZohoAPIDomain(t *testing.T) {
	for _, d := range []string{"https://evil.example.com", "http://www.zohoapis.com", "https://www.zohoapis.com.evil.io"} {
		c, rt := newIDClient(d, "123")
		if err := c.doBooksRequest(context.Background(), http.MethodGet, "/books/v3/x", nil, nil, nil); err == nil {
			t.Fatalf("%q accepted", d)
		}
		if len(rt.urls) != 0 {
			t.Fatalf("%q: request sent", d)
		}
	}
}

func TestDoBooksRequest_AcceptsCAAndAEDomains(t *testing.T) {
	for _, d := range []string{"https://www.zohoapis.ca", "https://www.zohoapis.ae"} {
		c, rt := newIDClient(d, "123")
		if err := c.doBooksRequest(context.Background(), http.MethodGet, "/books/v3/x", nil, nil, nil); err != nil {
			t.Fatalf("%q: %v", d, err)
		}
		if len(rt.urls) != 1 {
			t.Fatalf("%q: urls=%v", d, rt.urls)
		}
	}
}

func TestDoBooksRequest_AcceptsStoredDomainWithTrailingSlash(t *testing.T) {
	c, rt := newIDClient("https://www.zohoapis.eu/", "123")
	if err := c.doBooksRequest(context.Background(), http.MethodGet, "/books/v3/x", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(rt.urls) != 1 || !strings.HasPrefix(rt.urls[0], "https://www.zohoapis.eu/books/v3/x?") {
		t.Fatalf("urls=%v", rt.urls)
	}
}

func TestGetOrganizationBaseCurrency_EscapesOrgID(t *testing.T) {
	c, rt := newIDClient("https://www.zohoapis.com", "../x?y=z")
	if _, err := c.getOrganizationBaseCurrency(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "https://www.zohoapis.com/books/v3/organizations/..%2Fx%3Fy=z?"
	if len(rt.urls) != 1 || !strings.HasPrefix(rt.urls[0], want) {
		t.Fatalf("urls=%v want prefix %s", rt.urls, want)
	}
}

func TestResourceIDsArePathEscaped(t *testing.T) {
	const id = "../x?y=z"
	const base = "https://www.zohoapis.com/books/v3/"
	tests := []struct {
		name string
		call func(*Client) error
		want string
	}{
		{"update contact", func(c *Client) error {
			_, err := c.UpdateContact(context.Background(), id, &ContactUpdateRequest{})
			return err
		}, base + "contacts/..%2Fx%3Fy=z?"},
		{"get contact", func(c *Client) error {
			_, err := c.GetContact(context.Background(), id)
			return err
		}, base + "contacts/..%2Fx%3Fy=z?"},
		{"update invoice", func(c *Client) error {
			_, err := c.UpdateInvoice(context.Background(), id, &InvoiceCreateRequest{})
			return err
		}, base + "invoices/..%2Fx%3Fy=z?"},
		{"get invoice", func(c *Client) error {
			_, err := c.GetInvoice(context.Background(), id)
			return err
		}, base + "invoices/..%2Fx%3Fy=z?"},
		{"submit invoice", func(c *Client) error {
			return c.SubmitInvoiceForApproval(context.Background(), id)
		}, base + "invoices/..%2Fx%3Fy=z/submit?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rt := newIDClient("https://www.zohoapis.com", "123")
			if err := tt.call(c); err != nil {
				t.Fatal(err)
			}
			if len(rt.urls) != 1 || !strings.HasPrefix(rt.urls[0], tt.want) {
				t.Fatalf("urls=%v want prefix %s", rt.urls, tt.want)
			}
		})
	}
}
