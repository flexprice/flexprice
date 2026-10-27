package selfbilling

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// fakeClient is an in-memory Flexprice Cloud keyed by customer external ID.
type fakeClient struct {
	mu sync.Mutex

	customers     map[string]bool
	legacyExtIDs  map[string]string // extID -> legacyExtID passed to EnsureCustomer
	subs          map[string]*remoteSubscription
	checkouts     map[string]*remoteCheckout
	summaries     map[string]*remoteSummary
	trialsStarted int
	err           error

	trialCheckouts   int
	trialCheckoutErr error
	portalExtIDs     []string

	billingUpdates map[string]billingDetails // extID -> last details passed to UpdateBillingDetails

	ingested  [][]usageEvent // one entry per IngestBulk call
	ingestErr error

	// freeFirstPeriod makes StartTrialEndCheckout end the trial with nothing to collect; the subscription turns active.
	freeFirstPeriod bool

	// staleAfterTrialEnd leaves the subscription unchanged after a free trial end, as a lagging read would.
	staleAfterTrialEnd bool
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		customers:    map[string]bool{},
		legacyExtIDs: map[string]string{},
		subs:         map[string]*remoteSubscription{},
		checkouts:    map[string]*remoteCheckout{},
		summaries:    map[string]*remoteSummary{},

		billingUpdates: map[string]billingDetails{},
	}
}

func (f *fakeClient) billingUpdate(extID string) (billingDetails, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	d, ok := f.billingUpdates[extID]
	return d, ok
}

func (f *fakeClient) EnsureCustomer(_ context.Context, in customerInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.customers[in.extID] = true
	f.legacyExtIDs[in.extID] = in.legacyExtID
	return f.err
}

func (f *fakeClient) StartTrial(_ context.Context, extID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.subs[extID] != nil {
		return false, f.err
	}
	f.trialsStarted++
	f.subs[extID] = &remoteSubscription{id: "sub_" + extID, status: types.SubscriptionStatusTrialing}
	return f.err == nil, f.err
}

func (f *fakeClient) LatestSubscription(_ context.Context, extID string) (*remoteSubscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.subs[extID], f.err
}

func (f *fakeClient) UpdateBillingDetails(_ context.Context, extID string, details billingDetails) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.billingUpdates[extID] = details
	return f.err
}

func (f *fakeClient) StartTrialEndCheckout(_ context.Context, in checkoutInput) (*remoteCheckout, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.trialCheckouts++
	if f.trialCheckoutErr != nil {
		return nil, f.trialCheckoutErr
	}

	if f.freeFirstPeriod {
		if sub, ok := f.subs[in.extID]; ok && !f.staleAfterTrialEnd {
			sub.status = types.SubscriptionStatusActive
		}
		return &remoteCheckout{status: types.CheckoutStatusCompleted, extID: in.extID, terminal: true}, f.err
	}

	co := &remoteCheckout{
		id:     "cs_" + in.subscriptionID,
		status: types.CheckoutStatusPending,
		url:    "https://checkout.test/" + in.subscriptionID,
		extID:  in.extID,
	}
	f.checkouts[co.id] = co
	return co, f.err
}

func (f *fakeClient) GetCheckout(_ context.Context, id string) (*remoteCheckout, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	co, ok := f.checkouts[id]
	if !ok {
		return nil, ierr.NewErrorf("checkout session %s not found", id).Mark(ierr.ErrNotFound)
	}
	return co, f.err
}

func (f *fakeClient) CancelCheckout(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if co, ok := f.checkouts[id]; ok {
		co.status = types.CheckoutStatusFailed
		co.terminal = true
	}
	return f.err
}

func (f *fakeClient) PortalSession(_ context.Context, extID string) (*portalSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.portalExtIDs = append(f.portalExtIDs, extID)
	return &portalSession{url: "https://portal.test"}, f.err
}

func (f *fakeClient) Summary(_ context.Context, extID string) (*remoteSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if sum, ok := f.summaries[extID]; ok {
		return sum, f.err
	}
	return &remoteSummary{subscription: f.subs[extID]}, f.err
}

func (f *fakeClient) IngestBulk(_ context.Context, events []usageEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.ingestErr != nil {
		return f.ingestErr
	}
	f.ingested = append(f.ingested, events)
	return nil
}

// recordingClient records the context provisioning ran with, signals each call, and can panic.
type recordingClient struct {
	*fakeClient
	called      chan struct{}
	ctxErr      error
	hasDeadline bool
	panic       bool
}

func newRecordingClient() *recordingClient {
	return &recordingClient{fakeClient: newFakeClient(), called: make(chan struct{}, 1)}
}

func (r *recordingClient) EnsureCustomer(ctx context.Context, in customerInput) error {
	r.fakeClient.mu.Lock()
	r.ctxErr = ctx.Err()
	_, r.hasDeadline = ctx.Deadline()
	r.fakeClient.mu.Unlock()

	r.called <- struct{}{}
	if r.panic {
		panic("flexprice cloud client blew up")
	}
	return r.fakeClient.EnsureCustomer(ctx, in)
}

// trials returns the number of trials started so far.
func (f *fakeClient) trials() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.trialsStarted
}

// hasCustomer reports whether the customer was ensured.
func (f *fakeClient) hasCustomer(extID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.customers[extID]
}

type testDeps struct {
	tenants  *testutil.InMemoryTenantStore
	envs     *testutil.InMemoryEnvironmentStore
	settings *testutil.InMemorySettingsStore
	usage    *testutil.InMemorySelfBillingStore
}

// newTestService returns a service with self-billing enabled against client.
func newTestService(client billingClient) (*selfBillingService, *testDeps) {
	d := &testDeps{
		tenants:  testutil.NewInMemoryTenantStore(),
		envs:     testutil.NewInMemoryEnvironmentStore(),
		settings: testutil.NewInMemorySettingsStore(),
		usage:    testutil.NewInMemorySelfBillingStore(),
	}

	params := service.ServiceParams{
		Logger:          logger.NewNoopLogger(),
		TenantRepo:      d.tenants,
		EnvironmentRepo: d.envs,
		SettingsRepo:    d.settings,

		SelfBillingClickHouseRepo: d.usage,
		SelfBillingPostgresRepo:   d.usage,

		Config: &config.Configuration{Billing: config.BillingConfig{
			Enabled:      true,
			Region:       "us",
			APIHost:      "https://cloud.test/v1",
			APIKey:       "sk",
			PlanID:       "plan_1",
			DashboardURL: "https://us.app.test",
		}},
	}

	settings := service.NewSettingsService(params)
	s := &selfBillingService{
		ServiceParams: params,
		client:        client,
		settings:      settings,
		envs:          service.NewEnvironmentService(d.envs, service.NewEnvAccessService(params.Config), settings, params),
	}
	return s, d
}

func seedTenant(t *testing.T, d *testDeps, id string, status types.TenantInternalStatus) {
	t.Helper()
	require.NoError(t, d.tenants.Create(context.Background(), &tenant.Tenant{
		ID:             id,
		Name:           id,
		Status:         types.StatusPublished,
		InternalStatus: status,
	}))
}

func tenantCtx(id string) context.Context {
	return types.SetTenantID(context.Background(), id)
}

func TestNew_DisabledIsNoop(t *testing.T) {
	svc := New(service.ServiceParams{Logger: logger.NewNoopLogger(), Config: &config.Configuration{}})

	st, err := svc.Status(context.Background(), "tenant_1")
	require.NoError(t, err)
	assert.False(t, st.Enabled)

	assert.NotPanics(t, func() {
		svc.OnTenantCreated(context.Background(), "tenant_1", "Acme", "a@acme.io")
	})

	ctx := context.Background()
	calls := map[string]func() error{
		"start_checkout": func() error {
			_, err := svc.StartCheckout(ctx, "tenant_1", dto.SelfBillingCheckoutRequest{})
			return err
		},
		"poll_checkout": func() error {
			_, err := svc.PollCheckout(ctx, "tenant_1", "cs_1")
			return err
		},
		"cancel_checkout": func() error {
			return svc.CancelCheckout(ctx, "tenant_1", "cs_1")
		},
		"summary": func() error {
			_, err := svc.Summary(ctx, "tenant_1")
			return err
		},
		"portal_session": func() error {
			_, err := svc.PortalSession(ctx, "tenant_1")
			return err
		},
	}
	for name, call := range calls {
		err := call()
		assert.True(t, ierr.IsNotFound(err), "%s: got %v", name, err)
	}

	assert.NoError(t, svc.RunHourly(ctx), "the hourly run is a no-op while disabled")
}

func TestStatus_Enabled(t *testing.T) {
	s, d := newTestService(newFakeClient())
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)

	st, err := s.Status(context.Background(), "t1")
	require.NoError(t, err)
	assert.True(t, st.Enabled)
	assert.Equal(t, types.TenantInternalStatusTrialing, st.Status)
}

func TestProvision(t *testing.T) {
	tests := []struct {
		name          string
		tenantID      string
		billingTenant string
		existingSub   bool
		wantCustomer  bool
		wantTrial     bool
		wantLegacyID  string
		wantAlert     bool
	}{
		{
			name:         "new_tenant_gets_customer_and_trial",
			tenantID:     "t1",
			wantCustomer: true,
			wantTrial:    true,
			wantAlert:    true,
		},
		{
			name:         "existing_subscription_no_second_trial",
			tenantID:     "t1",
			existingSub:  true,
			wantCustomer: true,
		},
		{
			name:          "billing_tenant_region_looks_up_legacy_bare_id",
			tenantID:      "t1",
			billingTenant: "t_billing",
			wantCustomer:  true,
			wantTrial:     true,
			wantLegacyID:  "t1",
			wantAlert:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			s, _ := newTestService(fc)
			sink := newAlertSink(t, s)
			s.Config.Billing.TenantID = tt.billingTenant
			if tt.existingSub {
				fc.subs["us:"+tt.tenantID] = &remoteSubscription{id: "old", status: types.SubscriptionStatusActive}
			}

			require.NoError(t, s.provision(context.Background(), tt.tenantID, "Acme", "a@acme.io"))

			assert.Equal(t, tt.wantCustomer, fc.customers["us:"+tt.tenantID])
			assert.Equal(t, tt.wantTrial, fc.trials() == 1)
			assert.Equal(t, tt.wantLegacyID, fc.legacyExtIDs["us:"+tt.tenantID])

			if !tt.wantAlert {
				assert.Empty(t, sink.waitFor(t, 0))
				return
			}

			got := sink.waitFor(t, 1)
			assert.Equal(t, alertTrialStarted, got[0].Event)
			assert.Equal(t, tt.tenantID, got[0].TenantID)
			assert.Equal(t, "Acme", got[0].TenantName)
			assert.Equal(t, "us", got[0].Region)
			assert.Empty(t, got[0].FromStatus)
			assert.False(t, got[0].OccurredAt.IsZero())
		})
	}
}

func TestOnTenantCreated_BillingTenantSkipped(t *testing.T) {
	rc := newRecordingClient()
	s, _ := newTestService(rc)
	s.Config.Billing.TenantID = "t_billing"

	s.OnTenantCreated(context.Background(), "t_billing", "Acme", "a@acme.io")

	assert.Never(t, func() bool { return len(rc.called) > 0 }, 50*time.Millisecond, 10*time.Millisecond)
}

func TestOnTenantCreated_RunsDetached(t *testing.T) {
	rc := newRecordingClient()
	s, _ := newTestService(rc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the request finishing must not abort provisioning
	s.OnTenantCreated(ctx, "t1", "Acme", "a@acme.io")

	require.Eventually(t, func() bool { return rc.trials() == 1 }, 2*time.Second, 5*time.Millisecond)
	assert.True(t, rc.hasCustomer("us:t1"))

	rc.fakeClient.mu.Lock()
	defer rc.fakeClient.mu.Unlock()
	require.NoError(t, rc.ctxErr)
	assert.True(t, rc.hasDeadline, "provisioning runs under its own timeout")
}

func TestOnTenantCreated_PanicIsRecovered(t *testing.T) {
	rc := newRecordingClient()
	rc.panic = true
	s, _ := newTestService(rc)

	assert.NotPanics(t, func() {
		s.OnTenantCreated(context.Background(), "t1", "Acme", "a@acme.io")
	})

	require.Eventually(t, func() bool { return len(rc.called) == 1 }, 2*time.Second, 5*time.Millisecond)
	assert.Equal(t, 0, rc.trials())
}

// failingClient fails the call under test with a Flexprice Cloud error.
type failingClient struct {
	*fakeClient
	err error
}

func (f *failingClient) PortalSession(context.Context, string) (*portalSession, error) {
	return nil, f.err
}

func TestLogRemoteFailure_LogsOnlyFlexpriceCloudErrors(t *testing.T) {
	remoteErr := cloudErr(sdkerrors.NewAPIError("boom", 500, "", nil))
	localErr := ierr.NewError("tenant not found").Mark(ierr.ErrNotFound)

	tests := []struct {
		name       string
		err        error
		wantLogged int
	}{
		{"remote_failure_logged_once", remoteErr, 1},
		{"local_not_found_not_logged", localErr, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zap.ErrorLevel)
			s, _ := newTestService(&failingClient{fakeClient: newFakeClient(), err: tt.err})
			s.Logger = logger.NewFromSugared(zap.New(core).Sugar())

			_, err := s.PortalSession(tenantCtx("t1"), "t1")
			require.Error(t, err)
			assert.Equal(t, tt.wantLogged, logs.Len())
		})
	}
}

// listFailingTenantStore fails List so the status sync errors.
type listFailingTenantStore struct {
	*testutil.InMemoryTenantStore
	err error
}

func (s *listFailingTenantStore) List(context.Context) ([]*tenant.Tenant, error) {
	return nil, s.err
}

func TestRunHourly(t *testing.T) {
	pushErr := errors.New("flexprice cloud down")
	syncErr := errors.New("tenants unavailable")

	tests := []struct {
		name       string
		pushErr    error
		syncErr    error
		wantErrs   []error
		wantActive bool
	}{
		{
			name:       "push_and_sync_succeed",
			wantActive: true,
		},
		{
			name:       "push_failure_still_syncs",
			pushErr:    pushErr,
			wantErrs:   []error{pushErr},
			wantActive: true,
		},
		{
			name:     "both_failures_joined",
			pushErr:  pushErr,
			syncErr:  syncErr,
			wantErrs: []error{pushErr, syncErr},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			fc.ingestErr = tt.pushErr
			s, d := newTestService(fc)
			seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
			fc.subs["us:t1"] = activeSub

			// One settled hour of usage, so the push reaches the client. Two hours back,
			// because the last hour is still settling for the first 15 minutes of each hour.
			d.usage.AddEvents(&domainsb.TenantHourEvents{
				TenantID:      "t1",
				EnvironmentID: "env_1",
				Hour:          time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour),
				Count:         1,
			})
			if tt.syncErr != nil {
				s.TenantRepo = &listFailingTenantStore{
					InMemoryTenantStore: d.tenants,
					err:                 tt.syncErr,
				}
			}

			err := s.RunHourly(context.Background())

			if len(tt.wantErrs) == 0 {
				require.NoError(t, err)
			}
			for _, want := range tt.wantErrs {
				assert.ErrorIs(t, err, want)
			}
			assert.Equal(t, tt.wantActive, tenantStatus(t, d, "t1") == types.TenantInternalStatusActive)
		})
	}
}

func TestOnTenantBillingDetailsUpdated(t *testing.T) {
	tests := []struct {
		name          string
		billingTenant string
		address       tenant.TenantAddress
		wantPushed    bool
		wantDetails   billingDetails
	}{
		{
			name:       "pushes_latest_name_and_address",
			address:    tenant.TenantAddress{Line1: "1 Main St", City: "Bengaluru", State: "KA", PostalCode: "560001", Country: "IN"},
			wantPushed: true,
			wantDetails: billingDetails{
				name:       "Acme Inc",
				line1:      "1 Main St",
				city:       "Bengaluru",
				state:      "KA",
				postalCode: "560001",
				country:    "IN",
			},
		},
		{
			name:        "no_address_pushes_name_only",
			wantPushed:  true,
			wantDetails: billingDetails{name: "Acme Inc"},
		},
		{
			name:          "billing_tenant_skipped",
			billingTenant: "t1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			s, d := newTestService(fc)
			s.Config.Billing.TenantID = tt.billingTenant
			require.NoError(t, d.tenants.Create(context.Background(), &tenant.Tenant{
				ID:             "t1",
				Name:           "Acme Inc",
				Status:         types.StatusPublished,
				InternalStatus: types.TenantInternalStatusTrialing,
				BillingDetails: tenant.TenantBillingDetails{Email: "billing@acme.io", Address: tt.address},
			}))

			s.OnTenantBillingDetailsUpdated(context.Background(), "t1")

			if !tt.wantPushed {
				assert.Never(t, func() bool { _, ok := fc.billingUpdate("us:t1"); return ok }, 50*time.Millisecond, 10*time.Millisecond)
				return
			}

			require.Eventually(t, func() bool { _, ok := fc.billingUpdate("us:t1"); return ok }, 2*time.Second, 5*time.Millisecond)
			got, _ := fc.billingUpdate("us:t1")
			assert.Equal(t, tt.wantDetails, got, "the email stays the signup email")
			assert.True(t, fc.hasCustomer("us:t1"), "the customer is provisioned first, in case the update raced signup")
		})
	}
}
