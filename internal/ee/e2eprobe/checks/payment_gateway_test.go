package checks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
)

func TestStripeDriver_AttachCard(t *testing.T) {
	tests := []struct {
		name      string
		card      TestCard
		wantToken string
	}{
		{name: "success card", card: TestCardSuccess, wantToken: "pm_card_visa"},
		{name: "decline card", card: TestCardDecline, wantToken: "pm_card_chargeCustomerFail"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/payment_methods/"+tt.wantToken+"/attach" {
					t.Errorf("path = %s", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer sk_test_x" {
					t.Errorf("Authorization = %q", got)
				}
				_ = r.ParseForm()
				if got := r.PostForm.Get("customer"); got != "cus_1" {
					t.Errorf("customer = %q", got)
				}
				_, _ = w.Write([]byte(`{"id":"pm_123"}`))
			}))
			defer srv.Close()

			d := &stripeDriver{baseURL: srv.URL, secretKey: "sk_test_x", http: srv.Client()}
			id, err := d.AttachCard(context.Background(), "cus_1", tt.card)
			if err != nil {
				t.Fatalf("AttachCard: %v", err)
			}
			if id != "pm_123" {
				t.Errorf("id = %q, want pm_123", id)
			}
		})
	}
}

func TestStripeDriver_GatewayErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"code":"card_declined"}}`))
	}))
	defer srv.Close()

	d := &stripeDriver{baseURL: srv.URL, secretKey: "sk_test_x", http: srv.Client()}
	if _, err := d.AttachCard(context.Background(), "cus_1", TestCardSuccess); err == nil {
		t.Fatal("want error for HTTP 402")
	}
}

func TestChargebeeDriver_AttachCard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/payment_sources/create_card" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if user, _, ok := r.BasicAuth(); !ok || user != "test_key" {
			t.Errorf("basic auth user = %q", user)
		}
		_ = r.ParseForm()
		if got := r.PostForm.Get("card[number]"); got != chargebeeSuccessCard {
			t.Errorf("card number = %q", got)
		}
		if got := r.PostForm.Get("customer_id"); got != "cb_cust" {
			t.Errorf("customer_id = %q", got)
		}
		_, _ = w.Write([]byte(`{"payment_source":{"id":"pm_cb_1"}}`))
	}))
	defer srv.Close()

	d := &chargebeeDriver{baseURL: srv.URL, apiKey: "test_key", http: srv.Client()}
	id, err := d.AttachCard(context.Background(), "cb_cust", TestCardSuccess)
	if err != nil {
		t.Fatalf("AttachCard: %v", err)
	}
	if id != "pm_cb_1" {
		t.Errorf("id = %q, want pm_cb_1", id)
	}
}

func TestChargebeeDriver_DeclineNeedsConfiguredCard(t *testing.T) {
	d := &chargebeeDriver{baseURL: "http://unused", apiKey: "test_key", http: http.DefaultClient}
	if _, err := d.AttachCard(context.Background(), "cb_cust", TestCardDecline); !errors.Is(err, ErrTestCardUnsupported) {
		t.Fatalf("err = %v, want ErrTestCardUnsupported", err)
	}
}

func TestNewGatewayDriver(t *testing.T) {
	tests := []struct {
		name    string
		cfg     e2eprobe.PaymentProviderConfig
		wantNil bool
	}{
		{name: "stripe with key", cfg: e2eprobe.PaymentProviderConfig{Provider: "stripe", StripeSecretKey: "sk_test_x"}},
		{name: "stripe without key", cfg: e2eprobe.PaymentProviderConfig{Provider: "stripe"}, wantNil: true},
		{name: "chargebee with key", cfg: e2eprobe.PaymentProviderConfig{Provider: "chargebee", ChargebeeSite: "acme-test", ChargebeeAPIKey: "test_x"}},
		{name: "razorpay has no driver", cfg: e2eprobe.PaymentProviderConfig{Provider: "razorpay"}, wantNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewGatewayDriver(tt.cfg); (got == nil) != tt.wantNil {
				t.Errorf("NewGatewayDriver nil = %v, want %v", got == nil, tt.wantNil)
			}
		})
	}
}
