package selfbilling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	cockroach "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	pathCustomers            = "/customers"           // as CreateCustomer / UpdateCustomer build it
	pathCustomerByExternalID = "/customers/external/" // as GetCustomerByExternalID builds it
	pathSubscriptionsSuffix  = "/subscriptions"       // as GetSubscriptionsForCustomer appends it
	pathSubscriptions        = "/subscriptions"       // as CreateSubscription builds it
	pathSubscriptionSearch   = "/subscriptions/search"
	pathModifySuffix         = "/modify/execute"     // as ExecuteSubscriptionModify appends it
	pathInvoiceSearch        = "/invoices/search"    // as QueryInvoice builds it
	pathCustomerUsage        = "/customers/usage"    // as GetCustomerUsageSummary builds it
	pathCustomerPortal       = "/customers/portal/"  // as CreateCustomerPortalSession builds it
	pathCheckoutSessions     = "/checkout/sessions/" // as GetCheckoutSession builds it
	pathEventsBulk           = "/events/bulk"        // as IngestEventsBulk builds it
	pathFXRateQuery          = "/forex/query"        // as QueryFXRates builds it
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

	byExtID    map[string]string            // external_id -> customer id
	metadata   map[string]map[string]string // customer id -> metadata
	subs       map[string][]map[string]any  // external_id -> subscriptions
	plans      map[string]map[string]any
	prices     map[string]map[string]any
	subDetails map[string]map[string]any
	requests   []string
	searches   []map[string]any
	failPaths  map[string]cannedResponse
	invoices   []map[string]any
	checkouts  map[string]cannedResponse // checkout session id -> response
	modify     *cannedResponse           // ExecuteSubscriptionModify response
	failAll    *cannedResponse           // when set, every request gets this response
	fxRates    []map[string]any          // QueryFXRates items

	created       []string
	rekeyedTo     []string
	updates       []map[string]any // UpdateCustomer request bodies
	subscriptions []map[string]any // CreateSubscription request bodies
	modifications []map[string]any // ExecuteSubscriptionModify request bodies
	cancelled     []string
	subCancels    []map[string]any // CancelSubscription request bodies, with the subscription id under "id"
	voided        []string         // VoidInvoice ids
	portals       []string
	bulkIngests   []map[string]any // IngestEventsBulk request bodies
	fxQueries     []map[string]any // QueryFXRates request bodies
}

func newFakeCloud(existing map[string]string) *fakeCloud {
	return &fakeCloud{
		byExtID:    existing,
		metadata:   map[string]map[string]string{},
		subs:       map[string][]map[string]any{},
		plans:      map[string]map[string]any{"plan_1": {"id": "plan_1", "name": "Self Serve", "prices": []any{}}},
		subDetails: map[string]map[string]any{},
		prices:     map[string]map[string]any{},
		failPaths:  map[string]cannedResponse{},
		checkouts:  map[string]cannedResponse{},
	}
}

func (f *fakeCloud) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
		if failure, ok := f.failPaths[r.URL.Path]; ok {
			w.WriteHeader(failure.status)
			_, _ = w.Write([]byte(failure.body))
			return
		}
		if f.failAll != nil {
			w.WriteHeader(f.failAll.status)
			_, _ = w.Write([]byte(f.failAll.body))
			return
		}

		switch {
		case r.Method == http.MethodPost && r.URL.Path == pathSubscriptionSearch:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("invalid subscription search body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.searches = append(f.searches, body)
			ext := body["external_customer_id"].(string)
			if _, ok := f.byExtID[ext]; !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(notFoundBody))
				return
			}
			statuses := make(map[string]bool)
			for _, status := range body["subscription_status"].([]any) {
				statuses[status.(string)] = true
			}

			var matching []map[string]any
			for _, sub := range f.subs[ext] {
				if statuses[fmt.Sprint(sub["subscription_status"])] {
					matching = append(matching, sub)
				}
			}

			createdAt := func(row map[string]any) time.Time {
				if value, ok := row["created_at"].(time.Time); ok {
					return value
				}

				value, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(row["created_at"]))
				return value
			}

			sort.SliceStable(matching, func(i, j int) bool {
				left, right := createdAt(matching[i]), createdAt(matching[j])
				if left.Equal(right) {
					return fmt.Sprint(matching[i]["id"]) > fmt.Sprint(matching[j]["id"])
				}

				return left.After(right)
			})

			limit := len(matching)
			if l, ok := body["limit"].(float64); ok {
				limit = min(int(l), limit)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": matching[:limit]})

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/plans/"):
			plan, ok := f.plans[strings.TrimPrefix(r.URL.Path, "/plans/")]
			if !ok {
				t.Errorf("unexpected plan lookup %s", r.URL.Path)
				w.WriteHeader(http.StatusNotImplemented)
				return
			}
			_ = json.NewEncoder(w).Encode(plan)

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/prices/"):
			p, ok := f.prices[strings.TrimPrefix(r.URL.Path, "/prices/")]
			if !ok {
				t.Errorf("unexpected price lookup %s", r.URL.Path)
				w.WriteHeader(http.StatusNotImplemented)
				return
			}

			_ = json.NewEncoder(w).Encode(p)

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/subscriptions/") && strings.HasSuffix(r.URL.Path, "/v2"):
			assert.Equal(t, "plan,subscription_line_items.prices", r.URL.Query().Get("expand"))
			sub, ok := f.subDetails[strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/subscriptions/"), "/v2")]
			if !ok {
				t.Errorf("unexpected subscription lookup %s", r.URL.Path)
				w.WriteHeader(http.StatusNotImplemented)
				return
			}
			for _, rows := range f.subs {
				for _, row := range rows {
					if row["id"] == sub["id"] {
						row["subscription_status"] = sub["subscription_status"]
					}
				}
			}

			_ = json.NewEncoder(w).Encode(sub)

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
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, ok := f.byExtID[fmt.Sprint(body["external_customer_id"])]; !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(notFoundBody))
				return
			}
			items := f.invoices
			if subID, ok := body["subscription_id"].(string); ok {
				items = lo.Filter(items, func(inv map[string]any, _ int) bool { return inv["subscription_id"] == subID })
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})

		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/invoices/") && strings.HasSuffix(r.URL.Path, "/void"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/invoices/"), "/void")
			f.voided = append(f.voided, id)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "invoice_status": "VOIDED"})

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
			created := map[string]any{"id": "sub_new", "plan_id": body["plan_id"], "subscription_status": "trialing"}
			if body["checkout"] != nil {
				created["subscription_status"] = "draft"
				created["checkout_session"] = map[string]any{
					"id": "cs_new", "checkout_status": "pending",
					"payment_action": map[string]any{"url": "https://checkout.test/new"},
					"metadata":       map[string]any{metaExternalID: body["external_customer_id"]},
				}
			}
			_ = json.NewEncoder(w).Encode(created)

		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, pathSubscriptions+"/") && strings.HasSuffix(r.URL.Path, "/cancel"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			body["id"] = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, pathSubscriptions+"/"), "/cancel")
			f.subCancels = append(f.subCancels, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"subscription_id": body["id"], "status": "cancelled"})

		case r.Method == http.MethodPost && r.URL.Path == pathEventsBulk:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.bulkIngests = append(f.bulkIngests, body)
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "accepted"})

		case r.Method == http.MethodPost && r.URL.Path == pathFXRateQuery:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.fxQueries = append(f.fxQueries, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"items": f.fxRates})

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
		Region:        "in",
		APIHost:       srv.URL,
		APIKey:        "sk",
		DefaultPlanID: "plan_1",
		PlanIDs:       lo.CoalesceSliceOrEmpty(extra.PlanIDs, []string{"plan_1"}),

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
				"payment_behavior":     "allow_incomplete",
			}},
		},
		{
			name:     "subscription_created_meanwhile_is_not_duplicated",
			existing: []map[string]any{{"id": "s1", "plan_id": "plan_1", "subscription_status": "trialing"}},
		},
		{
			name:     "enterprise_subscription_gets_no_self_serve_trial",
			existing: []map[string]any{{"id": "s1", "plan_id": "plan_enterprise", "subscription_status": "active"}},
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
		{"enterprise_plan_counts", []map[string]any{sub("s1", "plan_enterprise", active, 5)}, "s1", active},
		{"enterprise_live_beats_newer_cancelled_self_serve", []map[string]any{sub("s1", "plan_enterprise", active, 1), sub("s2", "plan_1", cancelled, 2)}, "s1", active},
		{"incomplete_is_returned", []map[string]any{sub("s1", "plan_1", types.SubscriptionStatusIncomplete, 1)}, "s1", types.SubscriptionStatusIncomplete},
		{"paused_is_returned", []map[string]any{sub("s1", "plan_1", types.SubscriptionStatusPaused, 1)}, "s1", types.SubscriptionStatusPaused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{"in:t1": "cust_1"})
			f.subs["in:t1"] = tt.subs
			c := newTestRemoteClient(t, f)

			got, err := c.LatestSubscription(context.Background(), "in:t1")
			require.NoError(t, err)
			wantSearches := 1
			if tt.wantID == "" || tt.wantStatus == cancelled {
				wantSearches = 2
			}

			require.Len(t, f.searches, wantSearches)
			assert.Equal(t, "in:t1", f.searches[0]["external_customer_id"])
			assert.ElementsMatch(t, []any{"active", "trialing", "incomplete", "paused"}, f.searches[0]["subscription_status"])
			assert.EqualValues(t, 1, f.searches[0]["limit"])
			if wantSearches == 2 {
				assert.Equal(t, []any{"cancelled"}, f.searches[1]["subscription_status"])
				assert.EqualValues(t, 1, f.searches[1]["limit"])
			}
			assert.NotContains(t, f.searches[0], "plan_id")

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

func TestUpdateBillingDetails_AddressSentWhole(t *testing.T) {
	tests := []struct {
		name        string
		details     billingDetails
		wantAddress bool
	}{
		{
			name:        "empty_parts_sent_to_clear_stale_values",
			details:     billingDetails{name: "Acme", line1: "1 Main St", city: "Pune", country: "IN"},
			wantAddress: true,
		},
		{
			name:    "no_address_leaves_it_untouched",
			details: billingDetails{name: "Acme"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{"in:t1": "cust_1"})
			c := newTestRemoteClient(t, f)

			require.NoError(t, c.UpdateBillingDetails(context.Background(), "in:t1", tt.details))

			require.Len(t, f.updates, 1)
			for _, field := range []string{"address_line1", "address_line2", "address_city", "address_state", "address_country", "address_postal_code"} {
				_, sent := f.updates[0][field]
				assert.Equal(t, tt.wantAddress, sent, field)
			}
			if tt.wantAddress {
				assert.Equal(t, "", f.updates[0]["address_line2"])
				assert.Equal(t, "", f.updates[0]["address_state"])
			}
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

func TestStartPlanCheckout(t *testing.T) {
	f := newFakeCloud(map[string]string{"in:t1": "cust_1"})
	c := newTestRemoteClient(t, f)

	co, err := c.StartPlanCheckout(context.Background(), checkoutInput{
		extID: "in:t1", subscriptionID: "sub_trial", planID: "plan_2",
		successURL: "https://app.test/ok", failureURL: "https://app.test/fail",
	})
	require.NoError(t, err)
	assert.Equal(t, "cs_new", co.id)
	assert.Equal(t, "https://checkout.test/new", co.url)

	// One subscription on the chosen plan, with no trial, behind a checkout keyed to the current subscription.
	require.Len(t, f.subscriptions, 1)
	body := f.subscriptions[0]
	assert.Equal(t, "plan_2", body["plan_id"])
	assert.Equal(t, "in:t1", body["external_customer_id"])
	assert.EqualValues(t, 0, body["trial_period_days"])
	assert.Nil(t, body["payment_behavior"])
	checkout := body["checkout"].(map[string]any)
	assert.Equal(t, "plan_checkout:sub_trial", checkout["idempotency_key"])
	assert.Equal(t, "https://app.test/ok", checkout["success_url"])
	assert.Empty(t, f.subCancels, "the trial stays until the purchase is paid")
}

func TestStartPlanCheckout_OpenCheckoutConflicts(t *testing.T) {
	f := newFakeCloud(map[string]string{"in:t1": "cust_1"})
	f.failPaths[pathSubscriptions] = cannedResponse{http.StatusConflict, `{"error":{"message":"exists"}}`}
	c := newTestRemoteClient(t, f)

	_, err := c.StartPlanCheckout(context.Background(), checkoutInput{extID: "in:t1", subscriptionID: "sub_trial", planID: "plan_1"})
	require.Error(t, err)
	assert.True(t, ierr.IsAlreadyExists(err), "got %v", err)
}

func TestEndTrials(t *testing.T) {
	f := newFakeCloud(map[string]string{"in:t1": "cust_1"})
	f.subs["in:t1"] = []map[string]any{
		{"id": "s_trial", "plan_id": "plan_1", "subscription_status": "trialing"},
		{"id": "s_enterprise", "plan_id": "plan_enterprise", "subscription_status": "trialing"},
		{"id": "s_paid", "plan_id": "plan_2", "subscription_status": "active"},
	}
	c := newTestRemoteClientWith(t, f, config.BillingConfig{PlanIDs: []string{"plan_1", "plan_2"}})

	require.NoError(t, c.EndTrials(context.Background(), "in:t1"))

	// Only the self-serve trial ends, at once and with no invoice.
	require.Len(t, f.subCancels, 1)
	assert.Equal(t, "s_trial", f.subCancels[0]["id"])
	assert.Equal(t, "immediate", f.subCancels[0]["cancellation_type"])
	assert.Equal(t, "skip", f.subCancels[0]["cancel_immediately_invoice_policy"])
	assert.Empty(t, f.voided)
}

// A trial that ended while the plan checkout was open is incomplete; its unpaid trial-end invoice goes with it.
func TestEndTrials_TrialThatEndedUnpaid(t *testing.T) {
	f := newFakeCloud(map[string]string{"in:t1": "cust_1"})
	f.subs["in:t1"] = []map[string]any{
		{"id": "s_trial", "plan_id": "plan_1", "subscription_status": "incomplete"},
		{"id": "s_paid", "plan_id": "plan_2", "subscription_status": "active"},
	}
	f.invoices = []map[string]any{
		{"id": "inv_trial_end", "subscription_id": "s_trial", "invoice_status": "FINALIZED", "payment_status": "PENDING", "total": "199"},
		{"id": "inv_paid", "subscription_id": "s_paid", "invoice_status": "FINALIZED", "payment_status": "SUCCEEDED", "total": "499"},
	}
	c := newTestRemoteClientWith(t, f, config.BillingConfig{PlanIDs: []string{"plan_1", "plan_2"}})

	require.NoError(t, c.EndTrials(context.Background(), "in:t1"))

	assert.Equal(t, []string{"inv_trial_end"}, f.voided)
	require.Len(t, f.subCancels, 1)
	assert.Equal(t, "s_trial", f.subCancels[0]["id"])
}

func TestRemoteClient_SummaryOffersPlansWhileOneCanBeBought(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		planID    string
		wantPlans []string
	}{
		{"trialing_sees_every_offered_plan", "trialing", "plan_1", []string{"plan_2", "plan_1"}},
		{"ended_trial_sees_every_offered_plan", "cancelled", "plan_1", []string{"plan_2", "plan_1"}},
		{"active_sees_none", "active", "plan_1", nil},
		{"enterprise_trial_sees_none", "trialing", "plan_enterprise", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloud(map[string]string{"in:t1": "cust_1"})
			f.plans["plan_2"] = map[string]any{"id": "plan_2", "name": "Growth", "prices": []any{}}
			f.subs["in:t1"] = []map[string]any{{"id": "s1", "plan_id": tt.planID, "subscription_status": tt.status}}
			f.subDetails["s1"] = map[string]any{
				"id": "s1", "subscription_status": tt.status,
				"plan": map[string]any{"id": tt.planID, "name": "Plan"},
			}
			c := newTestRemoteClientWith(t, f, config.BillingConfig{PlanIDs: []string{"plan_2", "plan_1"}})

			summary, err := c.Summary(context.Background(), "in:t1")
			require.NoError(t, err)
			got := lo.Map(summary.plans, func(p *dto.SelfBillingPlanResponse, _ int) string { return p.ID })
			if tt.wantPlans == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.wantPlans, got)
		})
	}
}

func TestRemoteClient_SummaryLoadsTheDefaultPlanOnce(t *testing.T) {
	f := newFakeCloud(map[string]string{"in:t1": "cust_1"})
	f.plans["plan_2"] = map[string]any{"id": "plan_2", "name": "Growth", "prices": []any{}}
	c := newTestRemoteClientWith(t, f, config.BillingConfig{PlanIDs: []string{"plan_1", "plan_2"}})

	_, err := c.Summary(context.Background(), "in:t1")
	require.NoError(t, err)
	assert.Equal(t, 1, lo.Count(f.requests, "GET /plans/plan_1"))
}

func TestRemoteClient_USDRates(t *testing.T) {
	f := newFakeCloud(map[string]string{})
	f.fxRates = []map[string]any{
		{"from_currency": "inr", "to_currency": "usd", "rate": "0.012", "source": "fixed"},
		{"from_currency": "EUR", "to_currency": "usd", "rate": "1.08", "source": "fixed"},
		{"from_currency": "gbp", "to_currency": "usd", "rate": "0", "source": "market"},
	}
	c := newTestRemoteClient(t, f)

	rates, err := c.USDRates(context.Background())
	require.NoError(t, err)

	assert.Len(t, rates, 2, "a market rate carries no usable value")
	assert.True(t, decimal.RequireFromString("0.012").Equal(rates["inr"]))
	assert.True(t, decimal.RequireFromString("1.08").Equal(rates["eur"]), "keys are lower-cased")

	require.Len(t, f.fxQueries, 1)
	assert.Equal(t, "tenant", f.fxQueries[0]["scope"])
	assert.Equal(t, "usd", f.fxQueries[0]["to_currency"])
	assert.Equal(t, "published", f.fxQueries[0]["status"])
}

func TestRemoteClient_USDRatesUnparseableRate(t *testing.T) {
	f := newFakeCloud(map[string]string{})
	f.fxRates = []map[string]any{
		{"from_currency": "inr", "to_currency": "usd", "rate": "not-a-number", "source": "fixed"},
	}
	c := newTestRemoteClient(t, f)

	_, err := c.USDRates(context.Background())
	require.Error(t, err)
	assert.True(t, ierr.IsInternal(err), "got %v", err)
}

func TestRemoteClient_USDRatesMapsCloudErrors(t *testing.T) {
	f := newFakeCloud(map[string]string{})
	f.failAll = &cannedResponse{http.StatusInternalServerError, `{"code":"internal_error","message":"boom","http_status_code":500}`}
	c := newTestRemoteClient(t, f)

	_, err := c.USDRates(context.Background())
	require.Error(t, err)
	assert.True(t, ierr.IsHTTPClient(err), "got %v", err)
	assert.True(t, isRemoteFailure(err))
}
