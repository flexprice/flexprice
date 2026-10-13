package httpclient

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fake credentials, split so secret scanners don't flag the test file.
const (
	fakeStripeKey     = "sk_" + "live_abc123def456"
	fakeWebhookSecret = "whsec_" + "abcdefgh12345678"
)

func TestRedactor_Body(t *testing.T) {
	const jsonType = "application/json"
	const formType = "application/x-www-form-urlencoded"

	tests := []struct {
		name        string
		body        string
		contentType string
		want        string
	}{
		{
			name:        "nested card fields",
			body:        `{"source":{"type":"creditcard","number":"4111111111111111","cvc":"123","month":12,"name":"Jane Doe"}}`,
			contentType: jsonType,
			want:        `{"source":{"cvc":"[redacted]","month":"[redacted]","name":"[redacted]","number":"[redacted]","type":"creditcard"}}`,
		},
		{
			name:        "camelCase keys",
			body:        `{"cardNumber":"4242424242424242","securityCode":"123","accountNumber":"000123456789"}`,
			contentType: jsonType,
			want:        `{"accountNumber":"[redacted]","cardNumber":"[redacted]","securityCode":"[redacted]"}`,
		},
		{
			name:        "sensitive key with an object value is redacted whole",
			body:        `{"token":{"id":"tok_live_abc"},"billing_details":{"line1":"1 Main St"}}`,
			contentType: jsonType,
			want:        `{"billing_details":"[redacted]","token":"[redacted]"}`,
		},
		{
			name:        "JSON inside a string is redacted whole",
			body:        `{"payload":"{\"password\":\"hunter2\"}"}`,
			contentType: jsonType,
			want:        `{"payload":"[redacted]"}`,
		},
		{
			name:        "custom field values",
			body:        `{"properties":[{"label":"Owner","value":"Jane Doe"}]}`,
			contentType: jsonType,
			want:        `{"properties":[{"label":"Owner","value":"[redacted]"}]}`,
		},
		{
			name:        "error codes, ids and amounts are kept",
			body:        `{"error":{"type":"card_error","code":"card_declined","param":"source"},"id":"ch_1","amount":500,"currency":"usd"}`,
			contentType: jsonType,
			want:        `{"amount":500,"currency":"usd","error":{"code":"card_declined","param":"source","type":"card_error"},"id":"ch_1"}`,
		},
		{
			name:        "free-text and link fields are redacted",
			body:        `{"description":"Invoice for Jane","hosted_invoice_url":"https://x","invoice_pdf":"https://y"}`,
			contentType: jsonType,
			want:        `{"description":"[redacted]","hosted_invoice_url":"[redacted]","invoice_pdf":"[redacted]"}`,
		},
		{
			name:        "metadata keeps only our exact flexprice_ ids, and still scans them",
			body:        `{"metadata":{"flexprice_customer_id":"jane@acme.com","flexprice_customer_phone":"+15551234567"}}`,
			contentType: jsonType,
			want:        `{"metadata":{"flexprice_customer_id":"[email]","flexprice_customer_phone":"[redacted]"}}`,
		},
		{
			name:        "metadata keeps only our flexprice_ ids",
			body:        `{"metadata":{"flexprice_invoice_id":"inv_1","owner":"Jane"},"meta_data":"{\"x\":1}"}`,
			contentType: jsonType,
			want:        `{"meta_data":"[redacted]","metadata":{"flexprice_invoice_id":"inv_1","owner":"[redacted]"}}`,
		},
		{
			name:        "an email under an unlisted key is masked",
			body:        `{"footer":"Questions? Write to jane@acme.com","ts":"1791075612"}`,
			contentType: jsonType,
			want:        `{"footer":"Questions? Write to [email]","ts":"1791075612"}`,
		},
		{
			name:        "a card number is masked",
			body:        `{"message":"card 4111 1111 1111 1111 declined"}`,
			contentType: jsonType,
			want:        `{"message":"card [card] declined"}`,
		},
		{
			name:        "an API key is masked",
			body:        `{"message":"Invalid API Key provided: ` + fakeStripeKey + `"}`,
			contentType: jsonType,
			want:        `{"message":"Invalid API Key provided: [secret]"}`,
		},
		{
			name:        "plain error text is kept",
			body:        `{"message":"Your card was declined."}`,
			contentType: jsonType,
			want:        `{"message":"Your card was declined."}`,
		},
		{
			name:        "luhn-invalid digits are kept",
			body:        `{"ref":"1234567890123"}`,
			contentType: jsonType,
			want:        `{"ref":"1234567890123"}`,
		},
		{
			name:        "unparseable or truncated JSON is omitted",
			body:        `{"password":"hunter2hunter2`,
			contentType: jsonType,
			want:        "[omitted: invalid JSON]",
		},
		{
			name:        "form body with bracketed keys",
			body:        "customer[billing_address][line1]=1+Main+St&customer[id]=cus_1&card[expiry_month]=12&card[brand]=visa",
			contentType: formType,
			want:        `{"card[brand]":"visa","card[expiry_month]":"[redacted]","customer[billing_address][line1]":"[redacted]","customer[id]":"cus_1"}`,
		},
		{
			name:        "form metadata exemption needs a metadata parent and an exact id",
			body:        "client_secret[flexprice_invoice_id]=hunter2&metadata[flexprice_phone]=%2B15551234567&subscription_data[metadata][flexprice_customer_id]=cust_1",
			contentType: formType,
			want:        `{"client_secret[flexprice_invoice_id]":"[redacted]","metadata[flexprice_phone]":"[redacted]","subscription_data[metadata][flexprice_customer_id]":"cust_1"}`,
		},
		{
			name:        "form metadata keeps only our flexprice_ ids",
			body:        "metadata[flexprice_invoice_id]=inv_1&metadata[sync_source]=flexprice",
			contentType: formType,
			want:        `{"metadata[flexprice_invoice_id]":"inv_1","metadata[sync_source]":"[redacted]"}`,
		},
		{
			name:        "OAuth code in a form body",
			body:        "code=1000.abcdef&client_id=x&client_secret=y&grant_type=authorization_code",
			contentType: formType,
			want:        `{"client_id":"x","client_secret":"[redacted]","code":"[redacted]","grant_type":"authorization_code"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, (&redactor{}).body([]byte(tt.body), tt.contentType, "request"))
		})
	}
}

func TestSkipReason(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		wantSkip string
	}{
		{name: "json", headers: map[string]string{"Content-Type": "application/json; charset=utf-8"}},
		{name: "json suffix type", headers: map[string]string{"Content-Type": "application/problem+json"}},
		{name: "form", headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"}},
		{name: "xml", headers: map[string]string{"Content-Type": "application/xml"}, wantSkip: "[omitted: application/xml body]"},
		{name: "html", headers: map[string]string{"Content-Type": "text/html; charset=utf-8"}, wantSkip: "[omitted: text/html body]"},
		{name: "no content type", headers: map[string]string{}, wantSkip: "[omitted: no content type]"},
		{name: "compressed", headers: map[string]string{"Content-Type": "application/json", "Content-Encoding": "gzip"}, wantSkip: "[omitted: gzip-encoded body]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tt.headers {
				h.Set(k, v)
			}
			assert.Equal(t, tt.wantSkip, skipReason(h))
		})
	}
}

func TestRedactor_TruncatesAfterRedacting(t *testing.T) {
	body := `{"pad":"` + strings.Repeat("x", maxLoggedBodyBytes-20) + `","password":"hunter2hunter2hunter2"}`
	got := (&redactor{}).body([]byte(body), "application/json", "request")

	assert.True(t, strings.HasSuffix(got, "…[truncated at 4 KB]"))
	assert.NotContains(t, got, "hunter2")
}

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{"email", "customer_email", "PrimaryEmailAddr", "cardNumber", "card-number", "BillAddr",
		"billing_address", "vat_number", "gst_no", "contact", "first_name", "customer_name", "hosted_invoice_url",
		"meta_data", "client_secret"}
	safe := []string{"id", "status", "amount", "currency", "code", "type", "param", "decline_code", "created", "plan_id", "last4"}

	for _, key := range sensitive {
		assert.True(t, isSensitiveKey(key), key)
	}
	for _, key := range safe {
		assert.False(t, isSensitiveKey(key), key)
	}
}

// TestRedactor_ProviderCorpus runs realistic provider payloads with planted PII through
// the redactor. Add a fixture here for every new provider or endpoint shape.
func TestRedactor_ProviderCorpus(t *testing.T) {
	planted := []string{
		"Jane", "Doe", "jane@acme.com", "+15551234567", "9876543210", "1 Main St", "Springfield", "94107",
		"4111111111111111", "000123456789", "110000000", "DE89370400440532013000", "HDFC0001234",
		"jane@okicici", "GB123456789", "27AAPFU0939F1ZV", "203.0.113.7", "invoice.stripe.com", "rzp.io",
		"1000.authcode", fakeWebhookSecret, "Acme Widgets", "2028",
	}

	tests := []struct {
		name        string
		contentType string
		body        string
		mustKeep    []string
	}{
		{
			name:        "stripe customer create (form)",
			contentType: "application/x-www-form-urlencoded",
			body: "email=jane%40acme.com&name=Jane+Doe&phone=%2B15551234567&address[line1]=1+Main+St&address[city]=Springfield" +
				"&address[postal_code]=94107&tax_id_data[0][type]=eu_vat&tax_id_data[0][value]=GB123456789&metadata[flexprice_customer_id]=cust_1",
		},
		{
			name:        "stripe invoice response",
			contentType: "application/json",
			body: `{"id":"in_1","object":"invoice","status":"open","amount_due":1000,"currency":"usd","customer":"cus_1",
				"customer_email":"jane@acme.com","customer_name":"Jane Doe","customer_address":{"line1":"1 Main St","city":"Springfield"},
				"customer_phone":"+15551234567","customer_tax_ids":[{"type":"eu_vat","value":"GB123456789"}],
				"hosted_invoice_url":"https://invoice.stripe.com/i/acct_1/live_x","account_name":"Acme Widgets",
				"lines":{"data":[{"id":"il_1","amount":1000,"description":"Pro plan for Jane Doe"}]}}`,
			mustKeep: []string{"in_1", "open", "1000", "cus_1", "il_1"},
		},
		{
			name:        "stripe card error with payment method",
			contentType: "application/json",
			body: `{"error":{"type":"card_error","code":"card_declined","decline_code":"insufficient_funds","request_log_url":"https://dashboard.stripe.com/x",
				"payment_method":{"id":"pm_1","billing_details":{"email":"jane@acme.com","name":"Jane Doe"},"card":{"last4":"4242","exp_month":12,"exp_year":2028}}}}`,
			mustKeep: []string{"card_declined", "insufficient_funds", "pm_1"},
		},
		{
			name:        "stripe webhook endpoint create",
			contentType: "application/json",
			body:        `{"id":"we_1","secret":"` + fakeWebhookSecret + `","url":"https://api.flexprice.io/webhooks"}`,
			mustKeep:    []string{"we_1"},
		},
		{
			name:        "chargebee customer and card (form)",
			contentType: "application/x-www-form-urlencoded",
			body: "first_name=Jane&last_name=Doe&email=jane%40acme.com&company=Acme+Widgets&vat_number=GB123456789" +
				"&billing_address[line1]=1+Main+St&card[number]=4111111111111111&card[cvv]=123&card[expiry_month]=12&auto_collection=off",
			mustKeep: []string{"auto_collection"},
		},
		{
			name:        "chargebee invoice response with meta_data",
			contentType: "application/json",
			body: `{"invoice":{"id":"inv_1","status":"payment_due","total":1000,"billing_address":{"first_name":"Jane","line1":"1 Main St"},
				"meta_data":"{\"owner\":\"jane@acme.com\"}"}}`,
			mustKeep: []string{"inv_1", "payment_due"},
		},
		{
			name:        "razorpay payment",
			contentType: "application/json",
			body: `{"id":"pay_1","status":"captured","amount":50000,"method":"upi","email":"jane@acme.com","contact":"+15551234567",
				"vpa":"jane@okicici","bank_account":{"account_number":"000123456789","ifsc":"HDFC0001234"},"notes":{"customer":"Jane Doe"},
				"acquirer_data":{"rrn":"123456789012"},"short_url":"https://rzp.io/i/abc"}`,
			mustKeep: []string{"pay_1", "captured", "50000", "upi"},
		},
		{
			name:        "moyasar raw card payment",
			contentType: "application/json",
			body: `{"amount":1000,"currency":"SAR","source":{"type":"creditcard","name":"Jane Doe","number":"4111111111111111","month":"12","year":"2028","cvc":"123"},
				"callback_url":"https://x","ip":"203.0.113.7"}`,
			mustKeep: []string{"creditcard", "SAR"},
		},
		{
			name:        "quickbooks customer",
			contentType: "application/json",
			body: `{"Customer":{"Id":"58","DisplayName":"Jane Doe","GivenName":"Jane","PrimaryEmailAddr":{"Address":"jane@acme.com"},
				"PrimaryPhone":{"FreeFormNumber":"9876543210"},"BillAddr":{"Line1":"1 Main St","City":"Springfield"},"CompanyName":"Acme Widgets"}}`,
			mustKeep: []string{`"Id":"58"`},
		},
		{
			name:        "hubspot contact",
			contentType: "application/json",
			body: `{"id":"501","properties":{"email":"jane@acme.com","firstname":"Jane","lastname":"Doe","phone":"+15551234567","company":"Acme Widgets",
				"hs_object_id":"501"}}`,
			mustKeep: []string{"501"},
		},
		{
			name:        "zoho contact with GST and custom fields",
			contentType: "application/json",
			body: `{"contact":{"contact_id":"460000000026049","contact_name":"Jane Doe","gst_no":"27AAPFU0939F1ZV",
				"custom_fields":[{"label":"Owner","value":"Jane Doe"}],"billing_address":{"street":"1 Main St"}}}`,
		},
		{
			name:        "zoho OAuth token exchange (form)",
			contentType: "application/x-www-form-urlencoded",
			body:        "code=1000.authcode&client_id=1000.CLIENT&client_secret=s3cr3t&grant_type=authorization_code&redirect_uri=https%3A%2F%2Fapp",
			mustKeep:    []string{"authorization_code"},
		},
		{
			name:        "azure marketplace resolve",
			contentType: "application/json",
			body: `{"id":"sub_1","planId":"pro","purchaser":{"emailId":"jane@acme.com","objectId":"o1","tenantId":"t1"},
				"beneficiary":{"emailId":"jane@acme.com"}}`,
			mustKeep: []string{"sub_1", "pro"},
		},
		{
			name:        "paddle customer",
			contentType: "application/json",
			body:        `{"data":{"id":"ctm_1","email":"jane@acme.com","name":"Jane Doe","status":"active"}}`,
			mustKeep:    []string{"ctm_1", "active"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (&redactor{}).body([]byte(tt.body), tt.contentType, "response")
			require.NotContains(t, got, "omitted", "fixture must parse")
			for _, value := range planted {
				assert.NotContains(t, got, value, "planted PII survived redaction")
			}
			for _, value := range tt.mustKeep {
				assert.Contains(t, got, value, "debugging field was redacted")
			}
		})
	}
}
