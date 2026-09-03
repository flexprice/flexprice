package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// Poll backoff. Told to the client as next_poll_after_ms and reused as the debounce
// TTL, so the advertised cadence and the gateway-call window cannot drift apart. A
// payment settles within seconds of the customer acting, so a fresh session is worth
// asking about often and an old one is not.
var checkoutPollBackoff = []struct {
	upTo     time.Duration // session age below which this interval applies
	interval time.Duration
}{
	{upTo: 30 * time.Second, interval: 2 * time.Second},
	{upTo: 2 * time.Minute, interval: 5 * time.Second},
}

// checkoutPollBackoffFloor is used once a session is older than every band above.
const checkoutPollBackoffFloor = 10 * time.Second

// checkoutPollInterval returns how long a client should wait before reading the
// session again, and how long a gateway call is debounced for. Zero means stop.
func checkoutPollInterval(session *domainCheckout.CheckoutSession) time.Duration {
	if session == nil || session.CheckoutStatus.IsTerminal() {
		return 0
	}
	age := time.Since(session.CreatedAt)
	for _, band := range checkoutPollBackoff {
		if age < band.upTo {
			return band.interval
		}
	}
	return checkoutPollBackoffFloor
}

// refreshSessionFromGateway reconciles a non-terminal session against the provider and
// completes it if the customer has paid — the recovery path for a webhook that was
// late, dropped, or errored. The client asserts nothing; every transition runs through
// CompleteCheckoutSession, and MarkCompleted's conditional UPDATE is what makes a third
// caller safe.
//
// stale=true means this response is stored state that was not checked against the
// provider — debounced, or the gateway did not answer. Errors are never returned: a
// customer who paid and sees an error is worse off than one who sees "processing".
func (s *checkoutSessionService) refreshSessionFromGateway(
	ctx context.Context,
	session *domainCheckout.CheckoutSession,
) (stale bool) {
	if session == nil || session.CheckoutStatus.IsTerminal() {
		return false
	}
	if session.CheckoutPaymentID == nil {
		// Fulfilment never got as far as creating a payment; there is nothing to ask
		// the provider about.
		return false
	}

	// Debounce. Acquired and never released — TTL expiry is the window, which is why
	// this uses single-shot AcquireLock: a caller inside the window is turned away, not
	// queued. Fails open, since losing it costs one extra gateway call whereas failing
	// closed would silently stop all reconciliation. Safe only while the outbound rate
	// limiter provides a second ceiling.
	if s.Locker != nil {
		lockKey := cache.GenerateKey(ctx, cache.PrefixCheckoutPollLock, *session.CheckoutPaymentID)
		lock, err := s.Locker.AcquireLock(ctx, lockKey, checkoutPollInterval(session))
		if err != nil {
			s.Logger.Warn(ctx, "checkout poll debounce unavailable, calling gateway anyway",
				"session_id", session.ID, "error", err)
		} else if lock != nil && !lock.AcquiredSuccessfully() {
			return true
		}
	}

	state, err := s.fetchProviderPaymentState(ctx, session)
	if err != nil {
		s.Logger.Error(ctx, "checkout poll gateway fetch failed",
			"session_id", session.ID,
			"payment_id", *session.CheckoutPaymentID,
			"error", err)
		return true
	}
	if state == nil {
		return false
	}

	// Success transitions only. A declined payment is deliberately left PENDING — the
	// link is still live and the customer may retry, and writing FAILED would close
	// that window and then block the retry from reaching SUCCEEDED. Abandonment is the
	// sweeper's job.
	if state.Status != types.PaymentStatusSucceeded && state.Status != types.PaymentStatusOverpaid {
		return false
	}

	// Carry the discovered pay_ into completion, which persists it onto the payment.
	// Merging onto the stored result happens inside CompleteCheckoutSession.
	var providerResult *types.CheckoutProviderResult
	if state.GatewayPaymentID != "" {
		providerResult = &types.CheckoutProviderResult{ProviderPaymentIntentID: state.GatewayPaymentID}
	}

	if err := s.CompleteCheckoutSession(ctx, session.ID, providerResult); err != nil {
		// ErrAlreadyExists means the webhook, the sweeper, or a concurrent poll got
		// there first. The work is done; reporting failure would be wrong.
		if ierr.IsAlreadyExists(err) {
			return false
		}
		s.Logger.Error(ctx, "checkout poll completion failed",
			"session_id", session.ID, "error", err)
		return true
	}

	return false
}

// fetchProviderPaymentState asks the session's provider what happened to its payment.
// Provider-agnostic: the handles come off the payment row (written at creation by
// recordGatewayHandles) and go back to the adapter that produced them, which is the
// only thing that knows what kind of object each id is.
//
// Returns (nil, nil) when there is nothing to ask — no handles, or no read API.
func (s *checkoutSessionService) fetchProviderPaymentState(
	ctx context.Context,
	session *domainCheckout.CheckoutSession,
) (*interfaces.PaymentState, error) {
	p, err := s.PaymentRepo.Get(ctx, *session.CheckoutPaymentID)
	if err != nil {
		return nil, err
	}

	req := interfaces.PaymentStateRequest{
		GatewayPaymentID:  lo.FromPtr(p.GatewayPaymentID),
		GatewayTrackingID: lo.FromPtr(p.GatewayTrackingID),
		InvoiceID:         p.DestinationID,
	}
	if req.GatewayPaymentID == "" && req.GatewayTrackingID == "" {
		// The provider was never reached, or the session predates handle recording.
		return nil, nil
	}

	provider, err := s.resolveCheckoutProvider(ctx, session.PaymentProvider)
	if err != nil {
		return nil, err
	}

	state, err := provider.FetchPaymentState(ctx, req)
	if err != nil {
		if ierr.IsNotImplemented(err) {
			// This provider reconciles by webhook only. Not a failure.
			s.Logger.Debug(ctx, "checkout provider has no payment state read, skipping poll",
				"session_id", session.ID, "provider", session.PaymentProvider)
			return nil, nil
		}
		return nil, err
	}
	return state, nil
}

func (s *checkoutSessionService) resolveCheckoutProvider(
	ctx context.Context,
	provider types.CheckoutPaymentProvider,
) (interfaces.CheckoutProvider, error) {
	if s.checkoutProviderFor != nil {
		return s.checkoutProviderFor(ctx, provider)
	}
	return s.IntegrationFactory.GetCheckoutProvider(
		ctx,
		provider,
		NewCustomerService(s.ServiceParams),
		NewInvoiceService(s.ServiceParams),
	)
}
