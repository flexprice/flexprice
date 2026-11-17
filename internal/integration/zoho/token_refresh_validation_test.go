package zoho

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

type refreshRepo struct {
	idRepo
	updates int
}

func (r *refreshRepo) Update(context.Context, *connection.Connection) error {
	r.updates++
	return nil
}

type refreshEnc struct{ idEnc }

func (refreshEnc) Encrypt(s string) (string, error) { return s, nil }

type bodyRT struct {
	urls   []string
	bodies []string
	resp   string
}

func (b *bodyRT) RoundTrip(r *http.Request) (*http.Response, error) {
	b.urls = append(b.urls, r.URL.String())
	if r.Body != nil {
		d, _ := io.ReadAll(r.Body)
		b.bodies = append(b.bodies, string(d))
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(b.resp)), Header: http.Header{}}, nil
}

func newRefreshClient(accounts, tokenResp string) (*Client, *bodyRT, *refreshRepo, *connection.Connection) {
	rt := &bodyRT{resp: tokenResp}
	conn := &connection.Connection{EncryptedSecretData: types.ConnectionMetadata{ZohoBooks: &types.ZohoBooksConnectionMetadata{
		RefreshToken:   "rt",
		ClientID:       "cid",
		ClientSecret:   "SECRET",
		AccountsURL:    accounts,
		APIDomain:      "https://www.zohoapis.com",
		OrganizationID: "123",
	}}}
	repo := &refreshRepo{idRepo: idRepo{conn: conn}}
	return &Client{
		connectionRepo:    repo,
		encryptionService: refreshEnc{},
		httpClient:        &http.Client{Transport: rt},
		logger:            logger.NewNoopLogger(),
	}, rt, repo, conn
}

func TestGetValidAccessToken_RejectsAccountsServerBeforeSend(t *testing.T) {
	for _, a := range []string{"https://evil.example.com", "http://accounts.zoho.com", "https://accounts.zoho.com.evil.io"} {
		c, rt, repo, conn := newRefreshClient(a, `{}`)
		if _, err := c.getValidAccessToken(context.Background(), conn); err == nil {
			t.Fatalf("%q accepted", a)
		}
		if len(rt.urls) != 0 || repo.updates != 0 {
			t.Fatalf("%q: request sent or stored", a)
		}
	}
}

func TestGetValidAccessToken_RefreshedAPIDomain(t *testing.T) {
	const stored = "https://www.zohoapis.com"
	tests := []struct {
		name       string
		apiDomain  string
		wantDomain string
	}{
		{"valid domain stored without slash", "https://www.zohoapis.ca/", "https://www.zohoapis.ca"},
		{"absent domain keeps stored", "", stored},
		{"non-zoho domain keeps stored", "https://evil.example.com", stored},
		{"http domain keeps stored", "http://www.zohoapis.ca", stored},
		{"domain with path keeps stored", "https://www.zohoapis.ca/x", stored},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"access_token":"new","api_domain":"` + tt.apiDomain + `","expires_in":3600}`
			c, rt, repo, conn := newRefreshClient("https://accounts.zoho.com", body)
			token, err := c.getValidAccessToken(context.Background(), conn)
			if err != nil {
				t.Fatal(err)
			}
			if token != "new" || len(rt.urls) != 1 || repo.updates != 1 {
				t.Fatalf("token=%q urls=%v updates=%d", token, rt.urls, repo.updates)
			}
			md := conn.EncryptedSecretData.ZohoBooks
			if md.APIDomain != tt.wantDomain || md.AccessToken != "new" {
				t.Fatalf("domain=%q access=%q", md.APIDomain, md.AccessToken)
			}
		})
	}
}
