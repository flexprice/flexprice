package selfbilling

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	cockroach "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	pathCustomers            = "/customers"           // as CreateCustomer / UpdateCustomer build it
	pathCustomerByExternalID = "/customers/external/" // as GetCustomerByExternalID builds it
	pathSubscriptionsSuffix  = "/subscriptions"       // as GetSubscriptionsForCustomer appends it
	pathSubscriptions        = "/subscriptions"       // as CreateSubscription builds it
	pathModifySuffix         = "/modify/execute"      // as ExecuteSubscriptionModify appends it
	pathInvoiceSearch        = "/invoices/search"     // as QueryInvoice builds it
	pathCustomerUsage        = "/customers/usage"     // as GetCustomerUsageSummary builds it
	pathCustomerPortal       = "/customers/portal/"   // as CreateCustomerPortalSession builds it
	pathCheckoutSessions     = "/checkout/sessions/"  // as GetCheckoutSession builds it
	pathEventsBulk           = "/events/bulk"         // as IngestEventsBulk builds it
)

// notFoundBody is the JSON error envelope the Flexprice API returns for a 404.
const notFoundBody = `{"code":"not_found","message":"customer not found","http_status_code":404}`

type cannedResponse struct {
	status int
	body   string
}

// fakeCloud serves just the Flexprice Cloud endpoints the remote client touches.
type fakeCloud struct {
	mu sync.Mutex

	byExtID   map[string]string            // external_id -> customer id
	metadata  map[string]map[string]string // customer id -> metadata
	subs      map[string][]map[string]any  // external_id -> subscriptions
	invoices  []map[string]any
	checkouts map[string]cannedResponse // checkout session id -> response
	modify    *cannedResponse           // ExecuteSubscriptionModify response
	failAll   *cannedResponse           // when set, every request gets this response

	created       []string
	rekeyedTo     []string
	updates       []map[string]any // UpdateCustomer request bodies
	subscriptions []map[string]any // CreateSubscription request bodies
	modifications []map[string]any // ExecuteSubscriptionModify request bodies
	cancelled     []string
	portals       []string
	bulkIngests   []map[string]any // IngestEventsBulk request bodies
}

func newFakeCloud(existing map[string]string) *fakeCloud {
	return &fakeCloud{
		byExtID:   existing,
		metadata:  map[string]map[string]string{},
		subs:      map[string][]map[string]any{},
		checkouts: map[string]cannedResponse{},
	}
}

func (f *fakeCloud) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if f.failAll != nil {
			w.WriteHeader(f.failAll.status)
			_, _ = w.Write([]byte(f.failAll.body))
			return
		}

		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, pathCustomerByExternalID) && strings.HasSuffix(r.URL.Path, pathSubscriptionsSuffix):
			ext := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, pathCustomerByExternalID), pathSubscriptionsSuffix)
			if _, ok := f.byExtID[ext]; !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(notFoundBody))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": f.subs[ext]})

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, pathCustomerByExternalID):
			ext := strings.TrimPrefix(r.URL.Path, pathCustomerByExternalID)
			id, ok := f.byExtID[ext]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(notFoundBody))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "external_id": ext, "metadata": f.metadata[id]})

		case r.Method == http.MethodPost && r.URL.Path == pathCustomers:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			ext := body["external_id"].(string)
			f.byExtID[ext] = "cust_" + ext
			f.created = append(f.created, ext)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "cust_" + ext, "external_id": ext})

		case r.Method == http.MethodPut && r.URL.Path == pathCustomers:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.updates = append(f.updates, body)
			old := r.URL.Query().Get("external_customer_id")
			id := f.byExtID[old]
			if md, ok := body["metadata"].(map[string]any); ok { // metadata is replaced wholesale
				f.metadata[id] = lo.MapValues(md, func(v any, _ string) string { return v.(string) })
			}
			newExt, renamed := body["external_id"].(string)
			if renamed {
				f.byExtID[newExt] = id
				delete(f.byExtID, old)
				f.rekeyedTo = append(f.rekeyedTo, newExt)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "external_id": lo.Ternary(renamed, newExt, old)})

		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, pathSubscriptions+"/") && strings.HasSuffix(r.URL.Path, pathModifySuffix):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.modifications = append(f.modifications, body)
			w.WriteHeader(f.modify.status)
			_, _ = w.Write([]byte(f.modify.body))

		case r.Method == http.MethodPost && r.URL.Path == pathInvoiceSearch:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": f.invoices})

		case r.Method == http.MethodGet && r.URL.Path == pathCustomerUsage:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"customer_id": "cust_1",
				"features":    []map[string]any{{"current_usage": "12.5", "is_enabled": true}},
			})

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, pathCustomerPortal):
			ext := strings.TrimPrefix(r.URL.Path, pathCustomerPortal)
			f.portals = append(f.portals, ext)
			_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://portal.test/" + ext, "expires_at": "2026-10-01T01:00:00Z"})

		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, pathCheckoutSessions) && strings.HasSuffix(r.URL.Path, "/cancel"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, pathCheckoutSessions), "/cancel")
			f.cancelled = append(f.cancelled, id)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "checkout_status": "failed", "terminal": true})

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, pathCheckoutSessions):
			resp, ok := f.checkouts[strings.TrimPrefix(r.URL.Path, pathCheckoutSessions)]
			if !ok {
				t.Errorf("unexpected checkout lookup %s", r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(resp.status)
			_, _ = w.Write([]byte(resp.body))

		case r.Method == http.MethodPost && r.URL.Path == pathSubscriptions:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.subscriptions = append(f.subscriptions, body)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "sub_new"})

		case r.Method == http.MethodPost && r.URL.Path == pathEventsBulk:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.bulkIngests = append(f.bulkIngests, body)
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "accepted"})

		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
}

func newTestRemoteClient(t *testing.T, f *fakeCloud) *remoteClient {
	t.Helper()
	return newTestRemoteClientWith(t, f, config.BillingConfig{})
}

func newTestRemoteClientWith(t *testing.T, f *fakeCloud, extra config.BillingConfig) *remoteClient {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return newRemoteClient(config.BillingConfig{
		Region:  "in",
		APIHost: srv.URL,
		APIKey:  "sk",
		PlanID:  "plan_1",

		PaymentProvider: extra.PaymentProvider,
	})
}

func TestEnsureCustomer(t *testing.T) {
	tests := []struct {
		name        string
		existing    map[string]string // external_id -> customer id on Flexprice Cloud
		legacyExtID string            // set only in the region hosting the billing tenant
		wantCreated []string
		wantRenamed []string
		wantFinal   map[string]string
	}{
		{
			name:        "legacy_bare_id_is_renamed",
			existing:    map[string]string{"t1": "cust_legacy"},
			legacyExtID: "t1",
			wantRenamed: []string{"in:t1"},
			wantFinal:   map[string]string{"in:t1": "cust_legacy"},
		},
		{
			name:        "already_renamed_is_noop",
			existing:    map[string]string{"in:t1": "cust_legacy"},
			legacyExtID: "t1",
			wantFinal:   map[string]string{"in:t1": "cust_legacy"},
		},
		{
			name:        "not_found_anywhere_is_created",
			existing:    map[string]string{},
			legacyExtID: "t1",
			wantCreated: []string{"in:t1"},
			wantFinal:   map[string]string{"in:t1": "cust_in:t1"},
		},
		{
			name:        "other_region_never_looks_up_legacy",
			existing:    map[string]string{"t1": "cust_other"},
			wantCreated: []string{"in:t1"},
			wantFinal:   map[string]string{"t1": "cust_other", "in:t1": "cust_in:t1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(tt.existing)
			c := newTestRemoteClient(t, f)
			in := customerInput{extID: "in:t1", legacyExtID: tt.legacyExtID, name: "Acme", tenantID: "t1"}

			require.NoError(t, c.EnsureCustomer(context.Background(), in))
			require.NoError(t, c.EnsureCustomer(context.Background(), in), "second call must be a no-op")

			assert.Equal(t, tt.wantCreated, f.created, "never duplicate; create at most once")
			assert.Equal(t, tt.wantRenamed, f.rekeyedTo, "rename at most once")
			assert.Equal(t, tt.wantFinal, f.byExtID)
		})
	}
}

func TestStartTrial(t *testing.T) {
	tests := []struct {
		name        string
		existing    []map[string]any
		wantSubs    []map[string]any
		wantCreated bool
	}{
		{
			name:        "creates_monthly_usd_subscription_on_the_plan",
			wantCreated: true,
			wantSubs: []map[string]any{{
				"external_customer_id": "in:t1",
				"plan_id":              "plan_1",
				"currency":             "usd",
				"billing_period":       "MONTHLY",
			}},
		},
		{
			name:     "subscription_created_meanwhile_is_not_duplicated",
			existing: []map[string]any{{"id": "s1", "plan_id": "plan_1", "subscription_status": "trialing"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{"in:t1": "cust_1"})
			f.subs["in:t1"] = tt.existing
			c := newTestRemoteClient(t, f)

			created, err := c.StartTrial(context.Background(), "in:t1")
			require.NoError(t, err)
			assert.Equal(t, tt.wantCreated, created)

			require.Len(t, f.subscriptions, len(tt.wantSubs))
			for i, want := range tt.wantSubs {
				for k, v := range want {
					assert.Equal(t, v, f.subscriptions[i][k], k)
				}
			}
		})
	}
}

func TestLatestSubscription(t *testing.T) {
	at := func(h int) string {
		return time.Date(2026, 10, 1, h, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}
	sub := func(id, plan string, status types.SubscriptionStatus, created int) map[string]any {
		return map[string]any{"id": id, "plan_id": plan, "subscription_status": status, "created_at": at(created)}
	}
	active, trialing, cancelled := types.SubscriptionStatusActive, types.SubscriptionStatusTrialing, types.SubscriptionStatusCancelled

	tests := []struct {
		name       string
		subs       []map[string]any
		wantID     string
		wantStatus types.SubscriptionStatus
	}{
		{"no_subscriptions", nil, "", ""},
		{"newest_live_wins", []map[string]any{sub("s1", "plan_1", active, 1), sub("s2", "plan_1", trialing, 2)}, "s2", trialing},
		{"live_beats_newer_cancelled", []map[string]any{sub("s1", "plan_1", active, 1), sub("s2", "plan_1", cancelled, 2)}, "s1", active},
		{"only_cancelled_is_returned", []map[string]any{sub("s1", "plan_1", cancelled, 1)}, "s1", cancelled},
		{"newest_cancelled_when_all_cancelled", []map[string]any{sub("s1", "plan_1", cancelled, 1), sub("s2", "plan_1", cancelled, 3)}, "s2", cancelled},
		{"other_plans_ignored", []map[string]any{sub("s1", "plan_other", active, 5)}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{"in:t1": "cust_1"})
			f.subs["in:t1"] = tt.subs
			c := newTestRemoteClient(t, f)

			got, err := c.LatestSubscription(context.Background(), "in:t1")
			require.NoError(t, err)

			if tt.wantID == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.wantID, got.id)
			assert.Equal(t, tt.wantStatus, got.status)
		})
	}
}

func TestLatestSubscription_UnknownCustomerIsNil(t *testing.T) {
	c := newTestRemoteClient(t, newFakeCloud(map[string]string{}))

	got, err := c.LatestSubscription(context.Background(), "in:missing")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestRemoteClient_MapsCloudErrors(t *testing.T) {
	tests := []struct {
		name     string
		resp     cannedResponse
		wantKind func(error) bool
		wantHint string
	}{
		{
			name:     "json_400_is_validation",
			resp:     cannedResponse{http.StatusBadRequest, `{"code":"validation_error","message":"external id is malformed","http_status_code":400}`},
			wantKind: ierr.IsValidation,
			wantHint: "external id is malformed",
		},
		{
			name:     "json_409_is_already_exists",
			resp:     cannedResponse{http.StatusConflict, `{"code":"conflict","message":"customer exists","http_status_code":409}`},
			wantKind: ierr.IsAlreadyExists,
			wantHint: "customer exists",
		},
		{
			name:     "json_401_is_http_client",
			resp:     cannedResponse{http.StatusUnauthorized, `{"code":"unauthorized","message":"invalid api key","http_status_code":401}`},
			wantKind: ierr.IsHTTPClient,
			wantHint: "Flexprice Cloud is unavailable",
		},
		{
			name:     "json_403_is_http_client",
			resp:     cannedResponse{http.StatusForbidden, `{"code":"forbidden","message":"forbidden","http_status_code":403}`},
			wantKind: ierr.IsHTTPClient,
			wantHint: "Flexprice Cloud is unavailable",
		},
		{
			name:     "json_500_is_http_client",
			resp:     cannedResponse{http.StatusInternalServerError, `{"code":"internal_error","message":"boom","http_status_code":500}`},
			wantKind: ierr.IsHTTPClient,
			wantHint: "Flexprice Cloud is unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{})
			f.failAll = &tt.resp
			c := newTestRemoteClient(t, f)

			err := c.EnsureCustomer(context.Background(), customerInput{extID: "in:t1", name: "Acme", tenantID: "t1"})
			require.Error(t, err)
			assert.True(t, tt.wantKind(err), "got %v", err)
			assert.Contains(t, cockroach.FlattenHints(err), tt.wantHint)
			assert.Equal(t, tt.resp.status, httpStatus(err), "the SDK status stays reachable")
		})
	}
}

func TestCloudErr(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantKind func(error) bool
	}{
		{"not_found", sdkerrors.NewAPIError("x", http.StatusNotFound, "", nil), ierr.IsNotFound},
		{"conflict", sdkerrors.NewAPIError("x", http.StatusConflict, `{"message":"exists"}`, nil), ierr.IsAlreadyExists},
		{"bad_request_is_validation", sdkerrors.NewAPIError("x", http.StatusBadRequest, `{"message":"bad input"}`, nil), ierr.IsValidation},
		{"unauthorized_is_http_client", sdkerrors.NewAPIError("x", http.StatusUnauthorized, `{"message":"bad key"}`, nil), ierr.IsHTTPClient},
		{"forbidden_is_http_client", sdkerrors.NewAPIError("x", http.StatusForbidden, `{"message":"no access"}`, nil), ierr.IsHTTPClient},
		{"server_error_is_http_client", sdkerrors.NewAPIError("x", http.StatusBadGateway, "", nil), ierr.IsHTTPClient},
		{"transport_error_is_http_client", errors.New("dial tcp: connection refused"), ierr.IsHTTPClient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cloudErr(tt.err)
			assert.True(t, tt.wantKind(got), "got %v", got)
			assert.Equal(t, isNotFound(tt.err), isNotFound(got), "not-found branching survives the mapping")
		})
	}
	assert.NoError(t, cloudErr(nil))
}

func TestIsNotFound(t *testing.T) {
	resp := func(code int) sdktypes.HTTPMetadata {
		return sdktypes.HTTPMetadata{Response: &http.Response{StatusCode: code}}
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain_error", errors.New("boom"), false},
		{"api_error_404", sdkerrors.NewAPIError("x", http.StatusNotFound, "", nil), true},
		{"api_error_500", sdkerrors.NewAPIError("x", http.StatusInternalServerError, "", nil), false},
		{"error_response_404", &sdkerrors.ErrorResponse{HTTPMeta: resp(http.StatusNotFound)}, true},
		{"error_response_400", &sdkerrors.ErrorResponse{HTTPMeta: resp(http.StatusBadRequest)}, false},
		{"error_response_without_http_meta", &sdkerrors.ErrorResponse{}, false},
		{"validation_error_404", sdkerrors.NewResponseValidationError("x", http.StatusNotFound, "", nil, errors.New("bad")), true},
		{"validation_error_500", sdkerrors.NewResponseValidationError("x", http.StatusInternalServerError, "", nil, errors.New("bad")), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isNotFound(tt.err))
		})
	}
}

func TestIsNotFound_SDKJSON404(t *testing.T) {
	c := newTestRemoteClient(t, newFakeCloud(map[string]string{}))

	_, err := c.sdk.Customers.GetCustomerByExternalID(context.Background(), "missing")
	require.Error(t, err)

	var errResp *sdkerrors.ErrorResponse
	assert.True(t, errors.As(err, &errResp), "a JSON 404 surfaces as ErrorResponse, got %T", err)
	assert.True(t, isNotFound(err))
	assert.Equal(t, "customer not found", lo.FromPtr(errResp.Message))
}

func TestUpdateBillingDetails_PreservesMetadata(t *testing.T) {
	tests := []struct {
		name     string
		existing map[string]string
		taxID    string
		want     map[string]string
	}{
		{
			name:     "tax_id_merged_into_existing",
			existing: map[string]string{"tenant_id": "t1", "old_external_ids": "t1", "crm": "x"},
			taxID:    "GB123",
			want:     map[string]string{"tenant_id": "t1", "old_external_ids": "t1", "crm": "x", "tax_id": "GB123"},
		},
		{
			name:     "tax_id_overwrites_previous",
			existing: map[string]string{"tenant_id": "t1", "tax_id": "OLD"},
			taxID:    "NEW",
			want:     map[string]string{"tenant_id": "t1", "tax_id": "NEW"},
		},
		{
			name:     "no_tax_id_leaves_metadata_untouched",
			existing: map[string]string{"tenant_id": "t1"},
			want:     map[string]string{"tenant_id": "t1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{"in:t1": "cust_1"})
			f.metadata["cust_1"] = tt.existing
			c := newTestRemoteClient(t, f)

			details := billingDetails{name: "Acme", city: "Pune", taxID: tt.taxID}
			require.NoError(t, c.UpdateBillingDetails(context.Background(), "in:t1", details))

			assert.Equal(t, tt.want, f.metadata["cust_1"])
			require.Len(t, f.updates, 1)
			assert.Equal(t, "Acme", f.updates[0]["name"])
			assert.Equal(t, "Pune", f.updates[0]["address_city"])
		})
	}
}

func TestStartTrialEndCheckout(t *testing.T) {
	const pendingBody = `{"checkout_session":{"id":"cs_1","checkout_status":"pending","terminal":false,` +
		`"metadata":{"self_billing_external_id":"in:t1"},"payment_action":{"url":"https://pay.test/cs_1"}}}`

	tests := []struct {
		name string
		resp cannedResponse
		want remoteCheckout
	}{
		{
			name: "pending_session_returned",
			resp: cannedResponse{http.StatusOK, pendingBody},
			want: remoteCheckout{id: "cs_1", status: types.CheckoutStatusPending, url: "https://pay.test/cs_1", extID: "in:t1"},
		},
		{
			name: "free_first_period_completes_without_session",
			resp: cannedResponse{http.StatusOK, `{"subscription":{"id":"sub_1"}}`},
			want: remoteCheckout{status: types.CheckoutStatusCompleted, extID: "in:t1", terminal: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{})
			f.modify = &tt.resp
			c := newTestRemoteClient(t, f)

			got, err := c.StartTrialEndCheckout(context.Background(), checkoutInput{
				extID:          "in:t1",
				subscriptionID: "sub_1",
				successURL:     "https://app.test/ok",
				failureURL:     "https://app.test/fail",
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, *got)

			require.Len(t, f.modifications, 1)
			body := f.modifications[0]
			assert.Equal(t, "trial_end", body["type"])
			assert.Equal(t, "immediate", body["trial_end_params"].(map[string]any)["action"])
			checkout := body["checkout"].(map[string]any)
			assert.Equal(t, "trial_end:sub_1", checkout["idempotency_key"])
			assert.Equal(t, "in:t1", checkout["metadata"].(map[string]any)[metaExternalID])
		})
	}
}

func TestStartTrialEndCheckout_SendsConfiguredProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider types.CheckoutPaymentProvider
		want     string
	}{
		{"default_is_stripe", "", "stripe"},
		{"configured_provider", types.CheckoutPaymentProviderRazorpay, "razorpay"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{})
			f.modify = &cannedResponse{http.StatusOK, `{"subscription":{"id":"sub_1"}}`}
			c := newTestRemoteClientWith(t, f, config.BillingConfig{PaymentProvider: tt.provider})

			_, err := c.StartTrialEndCheckout(context.Background(), checkoutInput{extID: "in:t1", subscriptionID: "sub_1"})
			require.NoError(t, err)

			require.Len(t, f.modifications, 1)
			checkout := f.modifications[0]["checkout"].(map[string]any)
			assert.Equal(t, tt.want, checkout["payment_provider"])
		})
	}
}

func TestStartTrialEndCheckout_Conflict(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		wantID string // "" = the 409 is returned
	}{
		{
			name:   "resumes_open_session",
			body:   `{"code":"already_exists","http_status_code":409,"details":{"checkout_session_id":"cs_open"}}`,
			wantID: "cs_open",
		},
		{
			name: "no_session_id_returns_conflict",
			body: `{"code":"already_exists","message":"Complete or cancel the existing checkout","http_status_code":409}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{})
			f.modify = &cannedResponse{http.StatusConflict, tt.body}
			f.checkouts["cs_open"] = cannedResponse{http.StatusOK, `{"id":"cs_open","checkout_status":"pending","payment_action":{"url":"https://checkout.test/open"},"metadata":{"` + metaExternalID + `":"in:t1"}}`}
			c := newTestRemoteClient(t, f)

			co, err := c.StartTrialEndCheckout(context.Background(), checkoutInput{extID: "in:t1", subscriptionID: "sub_1"})
			if tt.wantID == "" {
				require.Error(t, err)
				assert.True(t, ierr.IsAlreadyExists(err), "got %v", err)
				assert.Contains(t, cockroach.FlattenHints(err), "Complete or cancel the existing checkout")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, co.id)
			assert.Equal(t, "https://checkout.test/open", co.url)
			assert.Equal(t, types.CheckoutStatusPending, co.status)
		})
	}
}

func TestGetCheckout_MapsCloudErrors(t *testing.T) {
	tests := []struct {
		name     string
		resp     cannedResponse
		wantKind func(error) bool
		wantHint string
	}{
		{
			name:     "json_400_is_validation",
			resp:     cannedResponse{http.StatusBadRequest, `{"code":"validation_error","message":"checkout session id is malformed","http_status_code":400}`},
			wantKind: ierr.IsValidation,
			wantHint: "checkout session id is malformed",
		},
		{
			name:     "json_404_is_not_found",
			resp:     cannedResponse{http.StatusNotFound, `{"code":"not_found","message":"checkout session not found","http_status_code":404}`},
			wantKind: ierr.IsNotFound,
		},
		{
			name:     "json_500_is_http_client",
			resp:     cannedResponse{http.StatusInternalServerError, `{"code":"internal_error","message":"boom","http_status_code":500}`},
			wantKind: ierr.IsHTTPClient,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{})
			f.checkouts["cs_1"] = tt.resp
			c := newTestRemoteClient(t, f)

			_, err := c.GetCheckout(context.Background(), "cs_1")
			require.Error(t, err)
			assert.True(t, tt.wantKind(err), "got %v", err)
			if tt.wantHint != "" {
				assert.Contains(t, cockroach.FlattenHints(err), tt.wantHint)
			}
		})
	}
}

func TestGetCheckout_MapsStatus(t *testing.T) {
	f := newFakeCloud(map[string]string{})
	f.checkouts["cs_1"] = cannedResponse{http.StatusOK, `{"id":"cs_1","checkout_status":"completed","terminal":true,"metadata":{"self_billing_external_id":"in:t1"}}`}
	c := newTestRemoteClient(t, f)

	got, err := c.GetCheckout(context.Background(), "cs_1")
	require.NoError(t, err)
	assert.Equal(t, remoteCheckout{id: "cs_1", status: types.CheckoutStatusCompleted, extID: "in:t1", terminal: true}, *got)
}

func TestRemoteClient_CancelCheckout(t *testing.T) {
	f := newFakeCloud(map[string]string{})
	c := newTestRemoteClient(t, f)

	require.NoError(t, c.CancelCheckout(context.Background(), "cs_1"))
	assert.Equal(t, []string{"cs_1"}, f.cancelled)
}

func TestRemoteClient_PortalSession(t *testing.T) {
	f := newFakeCloud(map[string]string{})
	c := newTestRemoteClient(t, f)

	got, err := c.PortalSession(context.Background(), "in:t1")
	require.NoError(t, err)
	assert.Equal(t, "https://portal.test/in:t1", got.url)
	assert.Equal(t, time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC), got.expiresAt.UTC())
	assert.Equal(t, []string{"in:t1"}, f.portals)
}

func TestRemoteClient_SummaryInvoices(t *testing.T) {
	tests := []struct {
		name      string
		total     string
		wantTotal string
		wantErr   bool
	}{
		{"valid_total_parsed", "12.50", "12.5", false},
		{"empty_total_is_zero", "", "0", false},
		{"malformed_total_fails", "12,50", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{"in:t1": "cust_1"})
			f.invoices = []map[string]any{{
				"id":             "inv_1",
				"total":          tt.total,
				"currency":       "USD",
				"invoice_status": "FINALIZED",
				"payment_status": "PENDING",
			}}
			c := newTestRemoteClient(t, f)

			got, err := c.Summary(context.Background(), "in:t1")
			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, ierr.IsInternal(err), "got %v", err)
				assert.Contains(t, err.Error(), "inv_1")
				return
			}

			require.NoError(t, err)
			require.Len(t, got.invoices, 1)
			assert.Equal(t, tt.wantTotal, got.invoices[0].total.String())
			assert.Equal(t, types.InvoiceStatusFinalized, got.invoices[0].status)
			assert.Equal(t, types.PaymentStatusPending, got.invoices[0].paymentStatus)

			require.NotNil(t, got.usage)
			assert.Equal(t, "cust_1", got.usage.CustomerID)
			require.Len(t, got.usage.Features, 1)
			assert.Equal(t, "12.5", got.usage.Features[0].CurrentUsage.String())
			assert.True(t, got.usage.Features[0].IsEnabled)
		})
	}
}

func TestRemoteClient_IngestBulk(t *testing.T) {
	f := newFakeCloud(map[string]string{})
	c := newTestRemoteClient(t, f)
	hour := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	err := c.IngestBulk(context.Background(), []usageEvent{
		{
			id:         "evt:in:t1:env_1:2026100109",
			name:       "events_ingested",
			extID:      "in:t1",
			timestamp:  hour,
			properties: map[string]string{"count": "3"},
		},
	})
	require.NoError(t, err)

	require.Len(t, f.bulkIngests, 1)
	events := f.bulkIngests[0]["events"].([]any)
	require.Len(t, events, 1)
	assert.Equal(t, map[string]any{
		"event_id":             "evt:in:t1:env_1:2026100109",
		"event_name":           "events_ingested",
		"external_customer_id": "in:t1",
		"properties":           map[string]any{"count": "3"},
		"source":               "self_billing",
		"timestamp":            "2026-10-01T09:00:00Z",
	}, events[0])
}

func TestRemoteClient_IngestBulkMapsCloudErrors(t *testing.T) {
	f := newFakeCloud(map[string]string{})
	f.failAll = &cannedResponse{http.StatusInternalServerError, `{"code":"internal_error","message":"boom","http_status_code":500}`}
	c := newTestRemoteClient(t, f)

	err := c.IngestBulk(context.Background(), []usageEvent{{id: "evt:1", name: "events_ingested", extID: "in:t1"}})
	require.Error(t, err)
	assert.True(t, ierr.IsHTTPClient(err), "got %v", err)
	assert.True(t, isRemoteFailure(err))
}
