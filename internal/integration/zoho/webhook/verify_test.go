package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"testing"
)

func TestZohoWebhookInvoiceStatusVoid(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{ZohoBooksInvoiceWebhookStatusVoid, true},
		{"VOID", true},
		{" voided ", true},
		{ZohoBooksInvoiceWebhookStatusPaid, false},
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
