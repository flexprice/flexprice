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

func TestGetValidAccessToken_RejectsRefreshedAPIDomainBeforeStore(t *testing.T) {
	c, rt, repo, conn := newRefreshClient("https://accounts.zoho.com", `{"access_token":"a","api_domain":"https://evil.example.com","expires_in":3600}`)
	if _, err := c.getValidAccessToken(context.Background(), conn); err == nil {
		t.Fatal("evil api_domain accepted")
	}
	if len(rt.urls) != 1 {
		t.Fatalf("urls=%v", rt.urls)
	}
	if repo.updates != 0 || conn.EncryptedSecretData.ZohoBooks.APIDomain != "https://www.zohoapis.com" {
		t.Fatal("api_domain stored")
	}
}

func TestGetValidAccessToken_StoresValidRefreshedAPIDomain(t *testing.T) {
	c, _, repo, conn := newRefreshClient("https://accounts.zoho.com", `{"access_token":"a","api_domain":"https://www.zohoapis.ca/","expires_in":3600}`)
	if _, err := c.getValidAccessToken(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	if repo.updates != 1 || conn.EncryptedSecretData.ZohoBooks.APIDomain != "https://www.zohoapis.ca" {
		t.Fatalf("updates=%d domain=%q", repo.updates, conn.EncryptedSecretData.ZohoBooks.APIDomain)
	}
}
