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
	pathInvoiceSearch        = "/invoices/search"     // as QueryInvoice builds it
	pathCustomerUsage        = "/customers/usage"     // as GetCustomerUsageSummary builds it
	pathCheckoutSessions     = "/checkout/sessions/"  // as GetCheckoutSession builds it
	pathPlan                 = "/plans/plan_1"        // as GetPlan builds it for the configured plan
)

type cannedResponse struct {
	status int
	body   string
}

// notFoundBody is the JSON error envelope the Flexprice API returns for a 404.
const notFoundBody = `{"code":"not_found","message":"customer not found","http_status_code":404}`

// fakeMumbai serves just the customer endpoints the remote client touches.
type fakeMumbai struct {
	mu        sync.Mutex
	byExtID   map[string]string            // external_id -> customer id
	metadata  map[string]map[string]string // customer id -> metadata
	subs      map[string][]map[string]any  // external_id -> subscriptions
	invoices  []map[string]any
	created   []string
	rekeyedTo []string
	updates   []map[string]any
	checkouts map[string]cannedResponse // checkout session id -> response
}

func (f *fakeMumbai) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
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
			if md, ok := body["metadata"].(map[string]any); ok { // Mumbai replaces metadata wholesale
				f.metadata[id] = lo.MapValues(md, func(v any, _ string) string { return v.(string) })
			}
			newExt, renamed := body["external_id"].(string)
			if renamed {
				f.byExtID[newExt] = id
				delete(f.byExtID, old)
				f.rekeyedTo = append(f.rekeyedTo, newExt)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "external_id": lo.Ternary(renamed, newExt, old)})
		case r.Method == http.MethodPost && r.URL.Path == pathInvoiceSearch:
			_ = json.NewEncoder(w).Encode(map[string]any{"items": f.invoices})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, pathCheckoutSessions):
			resp, ok := f.checkouts[strings.TrimPrefix(r.URL.Path, pathCheckoutSessions)]
			if !ok {
				t.Errorf("unexpected checkout lookup %s", r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(resp.status)
			_, _ = w.Write([]byte(resp.body))
		case r.Method == http.MethodGet && r.URL.Path == pathPlan:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "plan_1", "prices": []map[string]any{{"id": "price_1", "type": "FIXED", "currency": "usd", "billing_period": "MONTHLY"}}})
		case r.Method == http.MethodGet && r.URL.Path == pathCustomerUsage:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
}

func newFakeMumbai(existing map[string]string) *fakeMumbai {
	return &fakeMumbai{byExtID: existing, metadata: map[string]map[string]string{}, subs: map[string][]map[string]any{}, checkouts: map[string]cannedResponse{}}
}

func newTestRemoteClient(t *testing.T, f *fakeMumbai) *remoteClient {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return newRemoteClient(config.BillingConfig{Region: "in", APIHost: srv.URL, APIKey: "sk", PlanID: "plan_1"})
}

func TestEnsureCustomer(t *testing.T) {
	tests := []struct {
		name        string
		existing    map[string]string // external_id -> customer id on Mumbai
		legacyExtID string            // set only in the region hosting the billing tenant
		wantCreated []string
		wantRenamed []string
		wantFinal   map[string]string
	}{
		{"legacy_bare_id_is_renamed", map[string]string{"t1": "cust_legacy"}, "t1", nil, []string{"in:t1"}, map[string]string{"in:t1": "cust_legacy"}},
		{"already_renamed_is_noop", map[string]string{"in:t1": "cust_legacy"}, "t1", nil, nil, map[string]string{"in:t1": "cust_legacy"}},
		{"not_found_anywhere_is_created", map[string]string{}, "t1", []string{"in:t1"}, nil, map[string]string{"in:t1": "cust_in:t1"}},
		{"non_mumbai_region_never_looks_up_legacy", map[string]string{"t1": "cust_other"}, "", []string{"in:t1"}, nil, map[string]string{"t1": "cust_other", "in:t1": "cust_in:t1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeMumbai(tt.existing)
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
	c := newTestRemoteClient(t, newFakeMumbai(map[string]string{}))
	_, err := c.sdk.Customers.GetCustomerByExternalID(context.Background(), "missing")
	require.Error(t, err)
	var er *sdkerrors.ErrorResponse
	assert.True(t, errors.As(err, &er), "a JSON 404 surfaces as ErrorResponse, got %T", err)
	assert.True(t, isNotFound(err))
}

func TestLatestSubscription(t *testing.T) {
	at := func(h int) string { return time.Date(2026, 10, 1, h, 0, 0, 0, time.UTC).Format(time.RFC3339) }
	sub := func(id, plan, status string, created int) map[string]any {
		return map[string]any{"id": id, "plan_id": plan, "subscription_status": status, "created_at": at(created)}
	}
	tests := []struct {
		name       string
		subs       []map[string]any
		wantID     string
		wantStatus string
	}{
		{"no_subscriptions", nil, "", ""},
		{"newest_live_wins", []map[string]any{sub("s1", "plan_1", "active", 1), sub("s2", "plan_1", "trialing", 2)}, "s2", "trialing"},
		{"live_beats_newer_cancelled", []map[string]any{sub("s1", "plan_1", "active", 1), sub("s2", "plan_1", "cancelled", 2)}, "s1", "active"},
		{"only_cancelled_is_returned", []map[string]any{sub("s1", "plan_1", "cancelled", 1)}, "s1", "cancelled"},
		{"newest_cancelled_when_all_cancelled", []map[string]any{sub("s1", "plan_1", "cancelled", 1), sub("s2", "plan_1", "cancelled", 3)}, "s2", "cancelled"},
		{"other_plans_ignored", []map[string]any{sub("s1", "plan_other", "active", 5)}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeMumbai(map[string]string{"in:t1": "cust_1"})
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
	c := newTestRemoteClient(t, newFakeMumbai(map[string]string{}))
	got, err := c.LatestSubscription(context.Background(), "in:missing")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestUpdateBillingDetails_PreservesMetadata(t *testing.T) {
	tests := []struct {
		name     string
		existing map[string]string
		taxID    string
		want     map[string]string
	}{
		{"tax_id_merged_into_existing", map[string]string{"tenant_id": "t1", "old_external_ids": "t1", "crm": "x"}, "GB123", map[string]string{"tenant_id": "t1", "old_external_ids": "t1", "crm": "x", "tax_id": "GB123"}},
		{"tax_id_overwrites_previous", map[string]string{"tenant_id": "t1", "tax_id": "OLD"}, "NEW", map[string]string{"tenant_id": "t1", "tax_id": "NEW"}},
		{"no_tax_id_leaves_metadata_untouched", map[string]string{"tenant_id": "t1"}, "", map[string]string{"tenant_id": "t1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeMumbai(map[string]string{"in:t1": "cust_1"})
			f.metadata["cust_1"] = tt.existing
			c := newTestRemoteClient(t, f)

			require.NoError(t, c.UpdateBillingDetails(context.Background(), "in:t1", billingDetails{name: "Acme", taxID: tt.taxID}))

			assert.Equal(t, tt.want, f.metadata["cust_1"])
			require.Len(t, f.updates, 1)
			assert.Equal(t, "Acme", f.updates[0]["name"])
		})
	}
}

func TestSummary_InvoiceTotal(t *testing.T) {
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
			f := newFakeMumbai(map[string]string{"in:t1": "cust_1"})
			f.invoices = []map[string]any{{"id": "inv_1", "total": tt.total, "currency": "USD"}}
			c := newTestRemoteClient(t, f)

			got, err := c.Summary(context.Background(), "in:t1")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "inv_1")
				return
			}
			require.NoError(t, err)
			require.Len(t, got.invoices, 1)
			assert.Equal(t, tt.wantTotal, got.invoices[0].total.String())
		})
	}
}

func TestGetCheckout_MapsMumbaiErrors(t *testing.T) {
	tests := []struct {
		name     string
		resp     cannedResponse
		wantKind func(error) bool
		wantHint string
	}{
		{"json_400_is_validation", cannedResponse{http.StatusBadRequest, `{"code":"validation_error","message":"checkout session id is malformed","http_status_code":400}`}, ierr.IsValidation, "checkout session id is malformed"},
		{"json_404_is_not_found", cannedResponse{http.StatusNotFound, `{"code":"not_found","message":"checkout session not found","http_status_code":404}`}, ierr.IsNotFound, ""},
		{"json_500_is_http_client", cannedResponse{http.StatusInternalServerError, `{"code":"internal_error","message":"boom","http_status_code":500}`}, ierr.IsHTTPClient, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeMumbai(map[string]string{})
			f.checkouts["cs_1"] = tt.resp
			c := newTestRemoteClient(t, f)

			_, err := c.GetCheckout(context.Background(), "cs_1")
			require.Error(t, err)
			assert.True(t, tt.wantKind(err), "got %v", err)
			assert.True(t, isRemoteFailure(err))
			if tt.wantHint != "" {
				assert.Contains(t, cockroach.FlattenHints(err), tt.wantHint)
			}
		})
	}
}

func TestMumbaiErr(t *testing.T) {
	conflict := sdkerrors.NewAPIError("x", http.StatusConflict, `{"message":"checkout open","details":{"checkout_session_id":"cs_open"}}`, nil)
	tests := []struct {
		name     string
		err      error
		wantKind func(error) bool
	}{
		{"conflict_is_already_exists", conflict, ierr.IsAlreadyExists},
		{"unauthorized_is_validation", sdkerrors.NewAPIError("x", http.StatusUnauthorized, `{"message":"bad key"}`, nil), ierr.IsValidation},
		{"server_error_is_http_client", sdkerrors.NewAPIError("x", http.StatusBadGateway, "", nil), ierr.IsHTTPClient},
		{"transport_error_is_http_client", errors.New("dial tcp: connection refused"), ierr.IsHTTPClient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mumbaiErr(tt.err)
			assert.True(t, tt.wantKind(got), "got %v", got)
			assert.True(t, isRemoteFailure(got))
			assert.Equal(t, httpStatus(tt.err), httpStatus(got), "the SDK status stays reachable")
		})
	}
	assert.NoError(t, mumbaiErr(nil))
	assert.Equal(t, "cs_open", conflictCheckoutID(mumbaiErr(conflict)))
}

func TestStartTrial_SubscriptionCreatedMeanwhileIsNotDuplicated(t *testing.T) {
	f := newFakeMumbai(map[string]string{"in:t1": "cust_1"})
	f.subs["in:t1"] = []map[string]any{{"id": "s1", "plan_id": "plan_1", "subscription_status": "trialing"}}
	c := newTestRemoteClient(t, f)

	// The fake fails the test on any POST /subscriptions.
	require.NoError(t, c.StartTrial(context.Background(), "in:t1"))
}
