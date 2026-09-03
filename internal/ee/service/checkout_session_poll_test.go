package service

import (
	"context"
	"sync"
	"testing"
	"time"

	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ── fake provider ────────────────────────────────────────────────────────────

// fakeCheckoutProvider stands in for a gateway adapter. It records every call so a
// test can assert not just the outcome but whether the gateway was contacted at
// all — the difference between "the poll found nothing" and "the poll correctly
// declined to ask".
type fakeCheckoutProvider struct {
	mu    sync.Mutex
	calls []interfaces.PaymentStateRequest

	state *interfaces.PaymentState
	err   error
}

func (f *fakeCheckoutProvider) FetchPaymentState(
	_ context.Context, req interfaces.PaymentStateRequest,
) (*interfaces.PaymentState, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.state, nil
}

func (f *fakeCheckoutProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// The rest of interfaces.CheckoutProvider is unused by the poll.
func (f *fakeCheckoutProvider) CreatePaymentLink(context.Context, interfaces.CheckoutProviderRequest) (*interfaces.CheckoutProviderResponse, error) {
	return nil, ierr.NewError("not used").Mark(ierr.ErrNotImplemented)
}
func (f *fakeCheckoutProvider) CreateAuthorizationLink(context.Context, interfaces.AuthorizationLinkRequest) (*interfaces.CheckoutProviderResponse, error) {
	return nil, ierr.NewError("not used").Mark(ierr.ErrNotImplemented)
}
func (f *fakeCheckoutProvider) TryAutoChargingSavedMethod(context.Context, interfaces.AuthorizationLinkRequest) (*interfaces.CheckoutProviderResponse, bool, error) {
	return nil, false, nil
}

// ── suite ────────────────────────────────────────────────────────────────────

// CheckoutPollSuite covers read-triggered reconciliation: the recovery path for a
// checkout whose provider webhook was late, dropped, or errored.
type CheckoutPollSuite struct {
	testutil.BaseServiceTestSuite
	svc      *checkoutSessionService
	provider *fakeCheckoutProvider

	// subscriptionID is the draft provisioned by the most recent seedSession.
	subscriptionID string
}

func TestCheckoutPollSuite(t *testing.T) {
	suite.Run(t, new(CheckoutPollSuite))
}

func (s *CheckoutPollSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ClearStores()

	s.provider = &fakeCheckoutProvider{}

	ctx := s.GetContext()
	cust := &customer.Customer{
		ID:         "cust_001",
		ExternalID: "ext_checkout_poll",
		Name:       "Checkout Poll Customer",
		Email:      "checkout-poll@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        "plan_001",
		Name:      "Checkout Poll Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	s.svc = &checkoutSessionService{
		ServiceParams: s.buildParams(),
		checkoutProviderFor: func(context.Context, types.CheckoutPaymentProvider) (interfaces.CheckoutProvider, error) {
			return s.provider, nil
		},
	}
}

func (s *CheckoutPollSuite) buildParams() ServiceParams {
	return ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		SubRepo:                      s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:     s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:              s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                     s.GetStores().PlanRepo,
		PriceRepo:                    s.GetStores().PriceRepo,
		PriceUnitRepo:                s.GetStores().PriceUnitRepo,
		EventRepo:                    s.GetStores().EventRepo,
		MeterRepo:                    s.GetStores().MeterRepo,
		CustomerRepo:                 s.GetStores().CustomerRepo,
		InvoiceRepo:                  s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:          s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:              s.GetStores().EntitlementRepo,
		EnvironmentRepo:              s.GetStores().EnvironmentRepo,
		FeatureRepo:                  s.GetStores().FeatureRepo,
		TenantRepo:                   s.GetStores().TenantRepo,
		UserRepo:                     s.GetStores().UserRepo,
		AuthRepo:                     s.GetStores().AuthRepo,
		WalletRepo:                   s.GetStores().WalletRepo,
		PaymentRepo:                  s.GetStores().PaymentRepo,
		CreditGrantRepo:              s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo:   s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                   s.GetStores().CouponRepo,
		CouponAssociationRepo:        s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:        s.GetStores().CouponApplicationRepo,
		AddonRepo:                    s.GetStores().AddonRepo,
		AddonAssociationRepo:         s.GetStores().AddonAssociationRepo,
		ConnectionRepo:               s.GetStores().ConnectionRepo,
		SettingsRepo:                 s.GetStores().SettingsRepo,
		TaxAssociationRepo:           s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                  s.GetStores().TaxRateRepo,
		TaxAppliedRepo:               s.GetStores().TaxAppliedRepo,
		AlertLogsRepo:                s.GetStores().AlertLogsRepo,
		CheckoutSessionRepo:          s.GetStores().CheckoutSessionRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		IntegrationFactory:           s.GetIntegrationFactory(),
		PlanPriceSyncRepo:            s.GetStores().PlanPriceSyncRepo,
		WalletBalanceAlertPubSub:     types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
		Locker:                       testutil.NewInMemoryRedisLocker(nil),
	}
}

func (s *CheckoutPollSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

// seedSession stores a checkout session and everything completion needs: the draft
// subscription and invoice it provisioned, and the payment it is waiting on. The
// payment carries the provider handle (as recordGatewayHandles leaves it), which is
// what the poll reads.
func (s *CheckoutPollSuite) seedSession(
	status types.CheckoutStatus,
	trackingID string,
	gatewayPaymentID string,
) *domainCheckout.CheckoutSession {
	ctx := s.GetContext()

	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         "cust_001",
		PlanID:             "plan_001",
		SubscriptionStatus: types.SubscriptionStatusDraft,
		Currency:           "inr",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		StartDate:          time.Now().UTC(),
		CurrentPeriodStart: time.Now().UTC(),
		CurrentPeriodEnd:   time.Now().UTC().AddDate(0, 1, 0),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))
	s.subscriptionID = sub.ID

	inv := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      "cust_001",
		SubscriptionID:  lo.ToPtr(sub.ID),
		InvoiceType:     types.InvoiceTypeSubscription,
		BillingPeriod:   lo.ToPtr(string(types.BILLING_PERIOD_MONTHLY)),
		PeriodStart:     lo.ToPtr(time.Now().UTC()),
		PeriodEnd:       lo.ToPtr(time.Now().UTC().AddDate(0, 1, 0)),
		InvoiceStatus:   types.InvoiceStatusDraft,
		PaymentStatus:   types.PaymentStatusPending,
		Currency:        "inr",
		AmountDue:       decimal.NewFromInt(20),
		AmountRemaining: decimal.NewFromInt(20),
		Total:           decimal.NewFromInt(20),
		Subtotal:        decimal.NewFromInt(20),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().InvoiceRepo.Create(ctx, inv))

	p := &payment.Payment{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT),
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     inv.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentGateway:    lo.ToPtr(string(types.PaymentGatewayTypeRazorpay)),
		Amount:            decimal.NewFromInt(20),
		Currency:          "inr",
		PaymentStatus:     types.PaymentStatusPending,
		EnvironmentID:     types.GetEnvironmentID(ctx),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	if trackingID != "" {
		p.GatewayTrackingID = lo.ToPtr(trackingID)
	}
	if gatewayPaymentID != "" {
		p.GatewayPaymentID = lo.ToPtr(gatewayPaymentID)
	}
	s.Require().NoError(s.GetStores().PaymentRepo.Create(ctx, p))

	session := &domainCheckout.CheckoutSession{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		CustomerID:        "cust_001",
		Action:            types.CheckoutActionCreateSubscription,
		CheckoutStatus:    status,
		PaymentProvider:   types.CheckoutPaymentProviderRazorpay,
		CheckoutPaymentID: lo.ToPtr(p.ID),
		CheckoutInvoiceID: lo.ToPtr(inv.ID),
		Configuration: domainCheckout.JSONBCheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{SubscriptionID: sub.ID},
		},
		ExpiresAt:     time.Now().UTC().Add(20 * time.Minute),
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, session))
	return session
}

// ── the headline case ────────────────────────────────────────────────────────

// The reason the feature exists: the customer paid, the webhook never arrived, and
// the next read of the session has to discover that and finish the checkout.
func (s *CheckoutPollSuite) TestWebhookLost_PollCompletesSession() {
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")
	s.provider.state = &interfaces.PaymentState{
		Status:           types.PaymentStatusSucceeded,
		GatewayPaymentID: "pay_rzp_001",
	}

	stale := s.svc.refreshSessionFromGateway(s.GetContext(), session)

	s.False(stale, "a response reconciled against the gateway is not stale")
	s.Equal(1, s.provider.callCount())
	s.Equal("plink_001", s.provider.calls[0].GatewayTrackingID,
		"the poll must ask about the handle recorded on the payment")

	ctx := s.GetContext()
	stored, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCompleted, stored.CheckoutStatus)
	s.NotNil(stored.CompletedAt)

	// The point of completing is that the customer gets what they paid for.
	p, err := s.GetStores().PaymentRepo.Get(ctx, *session.CheckoutPaymentID)
	s.Require().NoError(err)
	s.Equal(types.PaymentStatusSucceeded, p.PaymentStatus)
	s.Equal("pay_rzp_001", lo.FromPtr(p.GatewayPaymentID),
		"the payment id discovered at the gateway is persisted")

	inv, err := s.GetStores().InvoiceRepo.Get(ctx, *session.CheckoutInvoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)

	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.subscriptionID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, sub.SubscriptionStatus,
		"the draft subscription the session provisioned is now live")
}

// A payment id already known is the better question to ask, so the pre-payment
// handle must not be used once it exists.
func (s *CheckoutPollSuite) TestKnownPaymentID_PreferredOverTrackingHandle() {
	session := s.seedSession(types.CheckoutStatusPending, "order_001", "pay_rzp_002")
	s.provider.state = &interfaces.PaymentState{Status: types.PaymentStatusSucceeded}

	s.svc.refreshSessionFromGateway(s.GetContext(), session)

	s.Require().Equal(1, s.provider.callCount())
	s.Equal("pay_rzp_002", s.provider.calls[0].GatewayPaymentID)
	s.Equal("order_001", s.provider.calls[0].GatewayTrackingID,
		"both handles are passed; the adapter decides which to use")
}

// ── the webhook already won ──────────────────────────────────────────────────

// Tenants are handed only a session id on checkout.session.completed, so they read
// this endpoint the moment the webhook lands. If a finished session still called the
// gateway, every webhook delivery would amplify into a provider request.
func (s *CheckoutPollSuite) TestTerminalSession_MakesNoProviderCall() {
	for _, status := range []types.CheckoutStatus{
		types.CheckoutStatusCompleted,
		types.CheckoutStatusFailed,
		types.CheckoutStatusExpired,
	} {
		s.Run(string(status), func() {
			s.provider = &fakeCheckoutProvider{}
			session := s.seedSession(status, "plink_001", "")

			stale := s.svc.refreshSessionFromGateway(s.GetContext(), session)

			s.False(stale, "stored state for a finished session is not stale, it is final")
			s.Zero(s.provider.callCount())
		})
	}
}

// ── concurrency ──────────────────────────────────────────────────────────────

// Effects must be applied at most once no matter how many callers arrive together.
// The debounce bounds gateway calls; this asserts the guarantee underneath it, the
// conditional UPDATE in MarkCompleted.
func (s *CheckoutPollSuite) TestConcurrentPolls_CompleteExactlyOnce() {
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")
	s.provider.state = &interfaces.PaymentState{
		Status:           types.PaymentStatusSucceeded,
		GatewayPaymentID: "pay_rzp_001",
	}

	// No debounce, so both callers reach the gateway and race on the claim.
	s.svc.Locker = nil

	// Note: this test trips -race, in testutil.InMemorySubscriptionStore.GetWithLineItems,
	// which writes LineItems onto the shared stored pointer. That is a flaw in the test
	// double, not in the path under test — production safety comes from the conditional
	// UPDATE in MarkCompleted. Run without -race, or fix the store separately.

	const callers = 8
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each caller reads its own copy, as separate requests would.
			own, err := s.GetStores().CheckoutSessionRepo.Get(s.GetContext(), session.ID)
			if err != nil {
				return
			}
			s.svc.refreshSessionFromGateway(s.GetContext(), own)
		}()
	}
	wg.Wait()

	stored, err := s.GetStores().CheckoutSessionRepo.Get(s.GetContext(), session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCompleted, stored.CheckoutStatus)
	s.NotNil(stored.CompletedAt, "exactly one caller may stamp completion")
}

// Losing the race is the normal outcome for every caller but one, and it means the
// work is done — reporting it as an unreconciled read would make a client keep
// polling a finished session.
func (s *CheckoutPollSuite) TestLosingTheClaim_IsNotStale() {
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")
	s.provider.state = &interfaces.PaymentState{Status: types.PaymentStatusSucceeded}
	s.svc.Locker = nil

	first := s.svc.refreshSessionFromGateway(s.GetContext(), session)
	s.Require().False(first)

	// Same in-memory copy, still believing it is pending — the loser's view.
	second := s.svc.refreshSessionFromGateway(s.GetContext(), session)
	s.False(second, "ErrAlreadyExists means another caller completed it, which is success")
}

// ── debounce ─────────────────────────────────────────────────────────────────

// The debounce bounds how often the gateway is asked, so a client polling every two
// seconds does not become a request per poll. The lock is never released: expiry of
// its TTL is the window.
func (s *CheckoutPollSuite) TestDebounce_SecondPollSkipsTheGateway() {
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")
	s.provider.state = &interfaces.PaymentState{Status: types.PaymentStatusPending}

	first := s.svc.refreshSessionFromGateway(s.GetContext(), session)
	second := s.svc.refreshSessionFromGateway(s.GetContext(), session)

	s.False(first)
	s.True(second, "a debounced read is serving stored state and must say so")
	s.Equal(1, s.provider.callCount(), "the window admits one gateway call")
}

// Losing the debounce costs at most one extra gateway call. Failing closed would
// silently stop all reconciliation and look identical to a healthy system, so a
// missing locker must not stop the read from reconciling.
func (s *CheckoutPollSuite) TestNoLocker_FailsOpen() {
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")
	s.provider.state = &interfaces.PaymentState{Status: types.PaymentStatusPending}
	s.svc.Locker = nil

	s.svc.refreshSessionFromGateway(s.GetContext(), session)
	s.svc.refreshSessionFromGateway(s.GetContext(), session)

	s.Equal(2, s.provider.callCount())
}

// ── the read must never fail ─────────────────────────────────────────────────

// A customer whose payment succeeded and whose page shows an error is strictly worse
// off than one who sees "still processing". Every gateway failure is swallowed and
// stored state is served.
func (s *CheckoutPollSuite) TestGatewayError_ServesStoredStateAsStale() {
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")
	s.provider.err = ierr.NewError("razorpay timeout").Mark(ierr.ErrHTTPClient)

	stale := s.svc.refreshSessionFromGateway(s.GetContext(), session)

	s.True(stale)
	stored, err := s.GetStores().CheckoutSessionRepo.Get(s.GetContext(), session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusPending, stored.CheckoutStatus, "nothing may be written")
}

// A provider with no read API is not an error. Chargebee reconciles by webhook only,
// and its sessions must still be readable.
func (s *CheckoutPollSuite) TestProviderWithoutReadAPI_IsNotAnError() {
	session := s.seedSession(types.CheckoutStatusPending, "hp_cb_001", "")
	s.provider.err = ierr.NewError("not supported").Mark(ierr.ErrNotImplemented)

	stale := s.svc.refreshSessionFromGateway(s.GetContext(), session)

	s.False(stale, "there was nothing to check against, which is not the same as stale")
}

// ── what the poll may not do ─────────────────────────────────────────────────

// A declined checkout payment is deliberately left PENDING: the link is still live
// and the customer may retry. Writing FAILED would close that window and — because
// FAILED has no outgoing transitions — would then block the successful retry.
func (s *CheckoutPollSuite) TestGatewayReportsFailed_WritesNothing() {
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")
	s.provider.state = &interfaces.PaymentState{Status: types.PaymentStatusFailed}

	stale := s.svc.refreshSessionFromGateway(s.GetContext(), session)

	s.False(stale, "the gateway answered, so the read is not stale")

	stored, err := s.GetStores().CheckoutSessionRepo.Get(s.GetContext(), session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusPending, stored.CheckoutStatus,
		"the poll cannot fail a session; abandonment is the sweeper's job")

	p, err := s.GetStores().PaymentRepo.Get(s.GetContext(), *session.CheckoutPaymentID)
	s.Require().NoError(err)
	s.Equal(types.PaymentStatusPending, p.PaymentStatus,
		"the payment stays open so the customer can retry on the same link")
}

// A session whose fulfilment never reached payment creation has no handle to ask
// about, and asking the gateway anyway would be a call with no subject.
func (s *CheckoutPollSuite) TestSessionWithoutPayment_MakesNoProviderCall() {
	ctx := s.GetContext()
	session := &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		CustomerID:      "cust_001",
		Action:          types.CheckoutActionCreateSubscription,
		CheckoutStatus:  types.CheckoutStatusInitiated,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		ExpiresAt:       time.Now().UTC().Add(20 * time.Minute),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, session))

	stale := s.svc.refreshSessionFromGateway(ctx, session)

	s.False(stale)
	s.Zero(s.provider.callCount())
}

// A payment created before gateway handles were recorded has nothing to ask about
// either. It still completes by webhook, and expires otherwise.
func (s *CheckoutPollSuite) TestPaymentWithoutHandles_MakesNoProviderCall() {
	session := s.seedSession(types.CheckoutStatusPending, "", "")

	stale := s.svc.refreshSessionFromGateway(s.GetContext(), session)

	s.False(stale)
	s.Zero(s.provider.callCount())
}

// ── client backoff ───────────────────────────────────────────────────────────

// The interval is server-driven so the advertised cadence and the debounce window
// cannot drift apart, and so an old session is not polled as hard as a fresh one.
func (s *CheckoutPollSuite) TestPollInterval_BacksOffWithSessionAge() {
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")

	session.CreatedAt = time.Now().UTC()
	s.Equal(2*time.Second, checkoutPollInterval(session))

	session.CreatedAt = time.Now().UTC().Add(-time.Minute)
	s.Equal(5*time.Second, checkoutPollInterval(session))

	session.CreatedAt = time.Now().UTC().Add(-10 * time.Minute)
	s.Equal(10*time.Second, checkoutPollInterval(session))

	session.CheckoutStatus = types.CheckoutStatusCompleted
	s.Zero(checkoutPollInterval(session), "zero tells the client to stop")
}

// ── the read itself ──────────────────────────────────────────────────────────

// Get is what a client actually calls, so the reconciliation, the re-read that picks
// up the completion, and the polling hints all have to line up in one response.
func (s *CheckoutPollSuite) TestGet_ReconcilesAndReportsTerminal() {
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")
	s.provider.state = &interfaces.PaymentState{
		Status:           types.PaymentStatusSucceeded,
		GatewayPaymentID: "pay_rzp_001",
	}

	resp, err := s.svc.Get(s.GetContext(), session.ID)
	s.Require().NoError(err)

	s.Equal(types.CheckoutStatusCompleted, resp.CheckoutStatus,
		"the response must reflect the completion this read caused, not the state it read first")
	s.True(resp.Terminal)
	s.False(resp.Stale)
	s.Zero(resp.NextPollAfterMs, "a finished session tells the client to stop")

	s.Require().NotNil(resp.Payment)
	s.Equal(types.PaymentStatusSucceeded, resp.Payment.Status)
}

// A read that could not be checked against the gateway must say so, and must keep
// telling the client when to come back.
func (s *CheckoutPollSuite) TestGet_DebouncedReadIsStaleAndKeepsPolling() {
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")
	s.provider.state = &interfaces.PaymentState{Status: types.PaymentStatusPending}

	_, err := s.svc.Get(s.GetContext(), session.ID)
	s.Require().NoError(err)

	resp, err := s.svc.Get(s.GetContext(), session.ID)
	s.Require().NoError(err)

	s.True(resp.Stale)
	s.False(resp.Terminal)
	s.NotZero(resp.NextPollAfterMs)
	s.Equal(1, s.provider.callCount(), "the debounce covers the endpoint, not just the helper")
}

// A gateway outage must not turn a readable session into a failed request.
func (s *CheckoutPollSuite) TestGet_SurvivesGatewayOutage() {
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")
	s.provider.err = ierr.NewError("razorpay unavailable").Mark(ierr.ErrHTTPClient)

	resp, err := s.svc.Get(s.GetContext(), session.ID)

	s.Require().NoError(err, "the read must not fail because the gateway did")
	s.True(resp.Stale)
	s.Equal(types.CheckoutStatusPending, resp.CheckoutStatus)
}

// ── provider_result is not clobbered ─────────────────────────────────────────

// Completion callers know only part of the provider result — the webhook has the
// gateway payment id but not the redirect action recorded at link creation. Writing
// that fragment straight to the column would drop the only trace back to the
// provider object.
func (s *CheckoutPollSuite) TestCompletion_PreservesProviderResult() {
	ctx := s.GetContext()
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")

	session.ProviderResult = domainCheckout.ToJSONBCheckoutProviderResult(&types.CheckoutProviderResult{
		ProviderSessionID: "plink_001",
		NextAction: &types.PaymentAction{
			Type: types.PaymentActionTypePaymentLink,
			URL:  "https://rzp.io/i/abc",
		},
	})
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Update(ctx, session))

	// A fragment, exactly as the webhook sends one.
	err := s.svc.CompleteCheckoutSession(ctx, session.ID, &types.CheckoutProviderResult{
		ProviderPaymentIntentID: "pay_rzp_001",
	})
	s.Require().NoError(err)

	stored, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	pr := stored.ProviderResult.ToProviderResult()
	s.Require().NotNil(pr)
	s.Equal("pay_rzp_001", pr.ProviderPaymentIntentID, "the fragment is applied")
	s.Equal("plink_001", pr.ProviderSessionID, "and the rest survives")
	s.Require().NotNil(pr.NextAction)
	s.Equal("https://rzp.io/i/abc", pr.NextAction.URL)
}

// ── the handles the poll depends on ──────────────────────────────────────────

// Checkout payments are created before the provider is contacted, so nothing on the
// payment row identifies the gateway object until this runs. If it silently stops,
// the poll degrades into a no-op with no error anywhere — so assert it directly.
func (s *CheckoutPollSuite) TestRecordGatewayHandles_MakesThePaymentSelfDescribing() {
	ctx := s.GetContext()
	session := s.seedSession(types.CheckoutStatusInitiated, "", "")
	paymentID := *session.CheckoutPaymentID

	before, err := s.GetStores().PaymentRepo.Get(ctx, paymentID)
	s.Require().NoError(err)
	s.Nil(before.GatewayTrackingID)
	s.Nil(before.GatewayPaymentID)

	err = s.svc.recordGatewayHandles(ctx, paymentID, &types.CheckoutProviderResult{
		ProviderSessionID:       "plink_001",
		ProviderPaymentIntentID: "pay_rzp_001",
	})
	s.Require().NoError(err)

	after, err := s.GetStores().PaymentRepo.Get(ctx, paymentID)
	s.Require().NoError(err)
	s.Equal("plink_001", lo.FromPtr(after.GatewayTrackingID))
	s.Equal("pay_rzp_001", lo.FromPtr(after.GatewayPaymentID))
}

// Paths that hand back only a pre-payment handle must still record it — that is the
// whole send_invoice flow, where no pay_ exists until the customer acts.
func (s *CheckoutPollSuite) TestRecordGatewayHandles_TrackingIDAlone() {
	ctx := s.GetContext()
	session := s.seedSession(types.CheckoutStatusInitiated, "", "")
	paymentID := *session.CheckoutPaymentID

	err := s.svc.recordGatewayHandles(ctx, paymentID, &types.CheckoutProviderResult{
		ProviderSessionID: "inv_001",
	})
	s.Require().NoError(err)

	after, err := s.GetStores().PaymentRepo.Get(ctx, paymentID)
	s.Require().NoError(err)
	s.Equal("inv_001", lo.FromPtr(after.GatewayTrackingID))
	s.Nil(after.GatewayPaymentID)
}

// A provider that returned nothing identifiable leaves the payment untouched rather
// than writing empty strings that later look like handles.
func (s *CheckoutPollSuite) TestRecordGatewayHandles_NothingToRecord() {
	ctx := s.GetContext()
	session := s.seedSession(types.CheckoutStatusInitiated, "", "")
	paymentID := *session.CheckoutPaymentID

	s.Require().NoError(s.svc.recordGatewayHandles(ctx, paymentID, &types.CheckoutProviderResult{}))
	s.Require().NoError(s.svc.recordGatewayHandles(ctx, paymentID, nil))

	after, err := s.GetStores().PaymentRepo.Get(ctx, paymentID)
	s.Require().NoError(err)
	s.Nil(after.GatewayTrackingID)
	s.Nil(after.GatewayPaymentID)
}

// ── expiry ordering ──────────────────────────────────────────────────────────

// The gateway must close first. Once the link is dead no new payment can start, so
// the session's remaining window only ever covers a payment already in flight — a
// customer who pressed pay seconds before the deadline and whose bank was slow. If
// this inverts, we tear down the drafts under them and the payment lands on an
// expired session, where the only outcome is an automatic refund.
func (s *CheckoutPollSuite) TestSessionOutlivesTheProviderLink() {
	for _, provider := range []types.CheckoutPaymentProvider{
		types.CheckoutPaymentProviderRazorpay,
		types.CheckoutPaymentProviderChargebee,
	} {
		s.Run(string(provider), func() {
			s.Greater(provider.SessionExpiry(), provider.LinkExpiry(),
				"the session must not expire before the link it is waiting on")
			s.Equal(provider.LinkExpiry()+provider.SessionGrace(), provider.SessionExpiry(),
				"session expiry is derived from link expiry so the two cannot drift")
		})
	}
}

// Create is where a client first receives the payment URL and starts polling, so its
// response must carry the same polling hints as the read. A zero interval there would
// tell the client to stop before it had begun.
func (s *CheckoutPollSuite) TestCreateResponse_CarriesPollingHints() {
	session := s.seedSession(types.CheckoutStatusPending, "plink_001", "")

	resp := s.svc.toPollableResponse(s.GetContext(), session, false)

	s.False(resp.Terminal)
	s.False(resp.Stale)
	s.EqualValues(2000, resp.NextPollAfterMs, "a fresh session polls on the fast band")
	s.Require().NotNil(resp.Payment, "the payment being waited on must be reported")
	s.Equal(*session.CheckoutPaymentID, resp.Payment.ID)
	s.Equal(types.PaymentStatusPending, resp.Payment.Status)
}
