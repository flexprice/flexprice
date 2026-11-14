package service

import (
	"context"
	"time"

	cerrors "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/email"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"golang.org/x/time/rate"
)

const (
	// emailAttachmentWait is how long a queued email waits for its attachments before it is sent without them.
	emailAttachmentWait = 10 * time.Minute
	// emailSendDeadline is how long a queued email may wait for the provider before it is marked failed.
	emailSendDeadline = 24 * time.Hour
	// emailDispatchWindow bounds one dispatch run's sends: send_rate_per_second × this window.
	emailDispatchWindow = 50 * time.Second
	// emailProviderFailureLimit is how many provider outage errors in a row end a dispatch run.
	emailProviderFailureLimit = 3
)

// errAttachmentNotReady marks an email whose attachment is still being produced.
var errAttachmentNotReady = cerrors.New("email attachment not ready")

// EmailDispatchResult counts what one dispatch run did with the emails it took.
type EmailDispatchResult struct {
	Sent     int
	Failed   int
	Deferred int
}

type deliveryOutcome int

const (
	deliverySent deliveryOutcome = iota
	deliveryFailed
	deliveryDeferred
	// deliveryProviderDown counts toward ending the run; deliveryRateLimited ends it at once.
	deliveryProviderDown
	deliveryRateLimited
)

// DispatchEmails sends queued emails from every environment, taking one email from each environment in
// turn, at most email.send_rate_per_second. An email that cannot be sent now stays queued for the next run.
func (s *emailNotificationService) DispatchEmails(ctx context.Context) (*EmailDispatchResult, error) {
	res := &EmailDispatchResult{}
	if !s.Config.Email.IsProviderConfigured() {
		return res, nil
	}

	sendRate := s.Config.Email.SendRatePerSecond
	budget := sendRate * int(emailDispatchWindow/time.Second)
	ctx = types.WithForceWriter(ctx)

	envs, err := s.EmailLogRepo.ListEnvironmentsWithQueuedEmails(ctx)
	if err != nil {
		return nil, err
	}
	queues := make([][]*emaillog.EmailLog, 0, len(envs))
	for _, env := range envs {
		queued, err := s.EmailLogRepo.ListQueued(envContext(ctx, env.TenantID, env.EnvironmentID), budget)
		if err != nil {
			return nil, err
		}
		queues = append(queues, queued)
	}

	paced := &emailNotificationService{ServiceParams: s.ServiceParams}
	paced.EmailProvider = pacedProvider{Provider: s.EmailProvider, limiter: rate.NewLimiter(rate.Limit(sendRate), 1)}

	providerFailures := 0
	for _, e := range takeTurns(queues, budget) {
		switch paced.dispatchOne(envContext(ctx, e.TenantID, e.EnvironmentID), e) {
		case deliverySent:
			res.Sent++
			providerFailures = 0
		case deliveryFailed:
			res.Failed++
		case deliveryDeferred:
			res.Deferred++
		case deliveryProviderDown:
			res.Deferred++
			providerFailures++
			if providerFailures >= emailProviderFailureLimit {
				s.Logger.Info(ctx, "email dispatch stopped: provider unavailable", "sent", res.Sent)
				return res, nil
			}
		case deliveryRateLimited:
			res.Deferred++
			s.Logger.Info(ctx, "email dispatch stopped: provider rate limit reached", "sent", res.Sent)
			return res, nil
		}
	}
	return res, nil
}

// dispatchOne sends one queued email, gives up on it past the send deadline, or leaves it queued.
func (s *emailNotificationService) dispatchOne(ctx context.Context, e *emaillog.EmailLog) deliveryOutcome {
	age := clock.Now().Sub(e.CreatedAt)
	if age >= emailSendDeadline {
		if err := s.MarkEmailFailed(ctx, e.ID, "The email could not be sent within 24 hours"); err != nil {
			s.Logger.Error(ctx, "failed to mark expired email failed", "error", err, "email_log_id", e.ID)
			return deliveryDeferred
		}
		return deliveryFailed
	}

	err := s.DeliverEmail(ctx, e.ID, age >= emailAttachmentWait)
	outcome := classifyDelivery(err)
	if outcome == deliveryDeferred && !cerrors.Is(err, errAttachmentNotReady) {
		s.Logger.Error(ctx, "email delivery deferred", "error", err, "email_log_id", e.ID)
	}
	return outcome
}

// classifyDelivery maps DeliverEmail's result to what the dispatch run does next.
func classifyDelivery(err error) deliveryOutcome {
	switch {
	case err == nil:
		return deliverySent
	case ierr.IsValidation(err):
		return deliveryFailed
	case cerrors.Is(err, errAttachmentNotReady):
		return deliveryDeferred
	case ierr.IsTooManyRequests(err):
		return deliveryRateLimited
	case ierr.IsServiceUnavailable(err):
		return deliveryProviderDown
	default:
		return deliveryDeferred
	}
}

// takeTurns takes one email from each queue in turn, up to limit emails.
func takeTurns(queues [][]*emaillog.EmailLog, limit int) []*emaillog.EmailLog {
	out := make([]*emaillog.EmailLog, 0, limit)
	for round := 0; len(out) < limit; round++ {
		took := false
		for _, q := range queues {
			if round < len(q) && len(out) < limit {
				out = append(out, q[round])
				took = true
			}
		}
		if !took {
			break
		}
	}
	return out
}

func envContext(ctx context.Context, tenantID, environmentID string) context.Context {
	return types.SetEnvironmentID(types.SetTenantID(ctx, tenantID), environmentID)
}

// pacedProvider holds every send to the limiter's rate.
type pacedProvider struct {
	email.Provider
	limiter *rate.Limiter
}

func (p pacedProvider) Send(ctx context.Context, msg email.OutboundEmail) (email.SendResult, error) {
	if err := p.limiter.Wait(ctx); err != nil {
		return email.SendResult{}, ierr.WithError(err).WithHint("Email dispatch stopped before sending").Mark(ierr.ErrServiceUnavailable)
	}
	return p.Provider.Send(ctx, msg)
}
