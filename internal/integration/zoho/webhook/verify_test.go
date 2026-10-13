package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	domainconn "github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/metrics/metricstest"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"net/url"
	"testing"

	"github.com/flexprice/flexprice/internal/integration/zoho"
)

func TestZohoWebhookInvoiceStatusVoid(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{zoho.InvoiceStatusVoid, true},
		{"VOID", true},
		{" voided ", true},
		{zoho.InvoiceStatusPaid, false},
		{"", false},
	} {
		if got := zohoWebhookInvoiceStatusVoid(tc.in); got != tc.want {
			t.Fatalf("zohoWebhookInvoiceStatusVoid(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPayload_UnmarshalInvoiceNumericAmountsAndID(t *testing.T) {
	raw := `{"organization_id":60069194202,"invoice":{"invoice_id":987654321,"status":"paid","total":100.5,"balance":0,"payment_made":100.5}}`
	var p Payload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Invoice == nil {
		t.Fatal("expected invoice")
	}
	if p.OrganizationID.String() != "60069194202" {
		t.Fatalf("organization_id: got %q", p.OrganizationID.String())
	}
	if p.Invoice.InvoiceID.String() != "987654321" {
		t.Fatalf("invoice_id: got %q", p.Invoice.InvoiceID.String())
	}
	if p.Invoice.Total.String() != "100.5" {
		t.Fatalf("total: got %q", p.Invoice.Total.String())
	}
	if p.Invoice.Balance.String() != "0" {
		t.Fatalf("balance: got %q", p.Invoice.Balance.String())
	}
	if p.Invoice.PaymentMade.String() != "100.5" {
		t.Fatalf("payment_made: got %q", p.Invoice.PaymentMade.String())
	}
}

func TestBuildSigningString_NoQuery_AppendsRawBody(t *testing.T) {
	u, _ := url.Parse("https://example.com/v1/webhooks/zoho_books/t/e")
	body := []byte(`{"invoice":{"invoice_id":"1","status":"paid"}}`)
	got := BuildSigningString(u, body)
	if got != string(body) {
		t.Fatalf("expected signing string == raw body, got %q want %q", got, string(body))
	}
}

func TestBuildSigningString_WithQuery_SortedThenBody(t *testing.T) {
	// Mirrors Zoho example: subscription_id=90343, name=basic → namebasic + subscription_id90343 + JSON
	u, _ := url.Parse("https://example.com/hook?subscription_id=90343&name=basic")
	body := []byte(`{"created_date":"2019-03-06","event_id":"5675"}`)
	got := BuildSigningString(u, body)
	want := "namebasicsubscription_id90343" + string(body)
	if got != want {
		t.Fatalf("signing string mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestVerifySignature_KnownVector(t *testing.T) {
	secret := "mysecretkey12"
	body := []byte(`{"test":true}`)
	u, _ := url.Parse("https://x")
	signing := BuildSigningString(u, body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signing))
	sig := hex.EncodeToString(mac.Sum(nil))
	if !VerifySignature(sig, signing, secret) {
		t.Fatal("expected signature to verify")
	}
	if VerifySignature("deadbeef", signing, secret) {
		t.Fatal("expected bad signature to fail")
	}
}

// Only verified webhooks are counted; a verified one that fails processing counts as failed.
func TestHandle_CountsOnlyVerifiedWebhooks(t *testing.T) {
	r := metricstest.Install(t)
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "ten_gw_zoho"), "env_gw")
	h := NewHandler(logger.NewNoopLogger())
	conn := &domainconn.Connection{EncryptedSecretData: types.ConnectionMetadata{ZohoBooks: &types.ZohoBooksConnectionMetadata{OrganizationID: "org_1"}}}
	body := []byte(`{"organization_id":"org_other"}`)
	u, _ := url.Parse("https://example.com/hook")
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte(BuildSigningString(u, body)))
	match := map[string]string{"tenant_id": "ten_gw_zoho", "provider": "zoho_books"}

	require.Error(t, h.Handle(ctx, conn, u, body, "bad-signature", "secret", &ServiceDeps{}))
	require.Equal(t, int64(0), r.Sum("gateway.webhooks", match))

	require.Error(t, h.Handle(ctx, conn, u, body, hex.EncodeToString(mac.Sum(nil)), "secret", &ServiceDeps{}))
	require.Equal(t, int64(1), r.Sum("gateway.webhooks", map[string]string{"tenant_id": "ten_gw_zoho", "event_type": "other", "outcome": "failed"}))
}
