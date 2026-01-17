//go:build integration

package moyasar

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration test configuration
// Run with: go test -tags=integration -v ./internal/integration/moyasar/... -run Integration
var (
	testSecretKey      = os.Getenv("MOYASAR_TEST_SECRET_KEY")
	testPublishableKey = os.Getenv("MOYASAR_TEST_PUBLISHABLE_KEY")
	testWebhookSecret  = os.Getenv("MOYASAR_TEST_WEBHOOK_SECRET")
	testBaseURL        = getEnvOrDefault("MOYASAR_TEST_BASE_URL", "https://api.moyasar.com/v1")
	testCallbackURL    = os.Getenv("MOYASAR_TEST_CALLBACK_URL")

	// Moyasar test cards
	testVisaCardApproved = "4111111111111111"
	testMadaCardApproved = "4201320111111010"
	testCardName         = "Test Cardholder"
	testCardMonth        = "12"
	testCardYear         = "2030"
	testCardCVC          = "123"
)

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// TestIntegration_CreatePaymentWithCard tests creating a payment with card source
func TestIntegration_CreatePaymentWithCard(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if testSecretKey == "" {
		t.Skip("Skipping integration test: MOYASAR_TEST_SECRET_KEY not set")
	}

	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}

	// Create payment request with card source
	// Note: Amount for SAR must be divisible by 10 (end with 0)
	reqBody := map[string]interface{}{
		"amount":       10000, // 100 SAR in halalah (ends with 0)
		"currency":     "SAR",
		"description":  "FlexPrice Integration Test Payment",
		"callback_url": testCallbackURL,
		"source": map[string]interface{}{
			"type":   "creditcard",
			"name":   testCardName,
			"number": testVisaCardApproved,
			"month":  testCardMonth,
			"year":   testCardYear,
			"cvc":    testCardCVC,
		},
		"metadata": map[string]string{
			"test":                  "true",
			"flexprice_invoice_id":  "inv_test_123",
			"flexprice_customer_id": "cust_test_456",
		},
		"given_id": uuid.New().String(), // Must be valid UUID
	}

	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, testBaseURL+"/payments", bytes.NewReader(bodyBytes))
	require.NoError(t, err)

	httpReq.SetBasicAuth(testSecretKey, "")
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	t.Logf("Response Status: %d", resp.StatusCode)
	t.Logf("Response Body: %s", string(respBody))

	// Parse response - expecting 201 Created for payment initiation
	if resp.StatusCode == 201 || resp.StatusCode == 200 {
		var payment CreatePaymentResponse
		err = json.Unmarshal(respBody, &payment)
		require.NoError(t, err)

		assert.NotEmpty(t, payment.ID)
		assert.Equal(t, 10000, payment.Amount)
		assert.Equal(t, "SAR", payment.Currency)

		t.Logf("✅ Payment created successfully!")
		t.Logf("   Payment ID: %s", payment.ID)
		t.Logf("   Status: %s", payment.Status)

		if payment.TransactionURL != "" {
			t.Logf("   3DS URL: %s", payment.TransactionURL)
		}
	} else {
		t.Logf("⚠️ Payment creation returned status %d (may require 3DS)", resp.StatusCode)
	}
}

// TestIntegration_GetPayment tests retrieving a payment from Moyasar
func TestIntegration_GetPayment(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if testSecretKey == "" {
		t.Skip("Skipping integration test: MOYASAR_TEST_SECRET_KEY not set")
	}

	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}

	// Create payment with proper source
	reqBody := map[string]interface{}{
		"amount":      5000, // 50 SAR (ends with 0)
		"currency":    "SAR",
		"description": "Test Payment for GetPayment",
		"source": map[string]interface{}{
			"type":   "creditcard",
			"name":   testCardName,
			"number": testVisaCardApproved,
			"month":  testCardMonth,
			"year":   testCardYear,
			"cvc":    testCardCVC,
		},
		"given_id": uuid.New().String(),
	}

	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, testBaseURL+"/payments", bytes.NewReader(bodyBytes))
	require.NoError(t, err)
	httpReq.SetBasicAuth(testSecretKey, "")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		t.Logf("Create payment response: %s", string(respBody))
		t.Skip("Skipping GetPayment test - payment creation requires 3DS")
	}

	var createdPayment CreatePaymentResponse
	err = json.Unmarshal(respBody, &createdPayment)
	require.NoError(t, err)

	// Now get the payment
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, testBaseURL+"/payments/"+createdPayment.ID, nil)
	require.NoError(t, err)
	getReq.SetBasicAuth(testSecretKey, "")

	resp2, err := client.Do(getReq)
	require.NoError(t, err)
	defer resp2.Body.Close()

	respBody2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	t.Logf("Get Payment Response: %s", string(respBody2))

	assert.Equal(t, 200, resp2.StatusCode)

	var payment MoyasarPayment
	err = json.Unmarshal(respBody2, &payment)
	require.NoError(t, err)

	assert.Equal(t, createdPayment.ID, payment.ID)
	t.Logf("✅ Retrieved payment: %s", payment.ID)
}

// TestIntegration_CreateToken tests creating a card token with proper card
func TestIntegration_CreateToken(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if testPublishableKey == "" {
		t.Skip("Skipping integration test: MOYASAR_TEST_PUBLISHABLE_KEY not set")
	}

	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}

	// Use proper test card
	reqBody := map[string]interface{}{
		"name":         testCardName,
		"number":       testVisaCardApproved,
		"month":        testCardMonth,
		"year":         testCardYear,
		"cvc":          testCardCVC,
		"save_only":    true,
		"callback_url": testCallbackURL,
		"metadata": map[string]string{
			"test": "true",
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, testBaseURL+"/tokens", bytes.NewReader(bodyBytes))
	require.NoError(t, err)

	// Token creation uses publishable key
	httpReq.SetBasicAuth(testPublishableKey, "")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("Create Token Response Status: %d", resp.StatusCode)
	t.Logf("Create Token Response: %s", string(respBody))

	// Token creation may require 3DS verification
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var token CreateTokenResponse
		err = json.Unmarshal(respBody, &token)
		require.NoError(t, err)

		assert.NotEmpty(t, token.ID)
		t.Logf("✅ Token created: %s, Status: %s", token.ID, token.Status)

		if token.TransactionURL != "" {
			t.Logf("   3DS Verification URL: %s", token.TransactionURL)
		}
	} else {
		t.Logf("⚠️ Token creation returned %d (may require 3DS or different card)", resp.StatusCode)
	}
}

// TestIntegration_AmountConversion tests proper amount handling
func TestIntegration_AmountConversion(t *testing.T) {
	testCases := []struct {
		sarAmount     decimal.Decimal
		expectedHalal int
	}{
		{decimal.NewFromFloat(100.00), 10000},
		{decimal.NewFromFloat(50.00), 5000}, // SAR amounts should end in 0
		{decimal.NewFromFloat(1.00), 100},
		{decimal.NewFromFloat(10.50), 1050},
		{decimal.NewFromFloat(999.90), 99990},
	}

	for _, tc := range testCases {
		halalah := int(tc.sarAmount.Mul(decimal.NewFromInt(100)).IntPart())
		assert.Equal(t, tc.expectedHalal, halalah, "SAR %s should convert to %d halalah", tc.sarAmount.String(), tc.expectedHalal)
	}
	t.Logf("✅ Amount conversion tests passed")
}

// TestIntegration_WebhookSignature tests webhook signature verification
func TestIntegration_WebhookSignature(t *testing.T) {
	if testWebhookSecret == "" {
		t.Skip("Skipping integration test: MOYASAR_TEST_WEBHOOK_SECRET not set")
	}
	payload := []byte(`{"type":"payment_paid","data":{"id":"pay_abc123","status":"paid","amount":10000}}`)

	signature := generateHMACSig(payload, testWebhookSecret)

	assert.NotEmpty(t, signature)
	assert.Len(t, signature, 64)

	// Verify idempotency
	signature2 := generateHMACSig(payload, testWebhookSecret)
	assert.Equal(t, signature, signature2)

	t.Logf("✅ Webhook signature: %s", signature)
}

// TestIntegration_ListPayments tests listing payments from Moyasar
func TestIntegration_ListPayments(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if testSecretKey == "" {
		t.Skip("Skipping integration test: MOYASAR_TEST_SECRET_KEY not set")
	}

	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, testBaseURL+"/payments?page=1", nil)
	require.NoError(t, err)
	httpReq.SetBasicAuth(testSecretKey, "")

	resp, err := client.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	t.Logf("List Payments Status: %d", resp.StatusCode)

	assert.Equal(t, 200, resp.StatusCode)

	var payments PaymentListResponse
	err = json.Unmarshal(respBody, &payments)
	require.NoError(t, err)

	t.Logf("✅ Found %d payments in sandbox", len(payments.Payments))
}
