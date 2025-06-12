package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

type CreditGrantApplicationService interface {
	ProcessScheduledApplications(ctx context.Context) error
}

type creditGrantApplicationService struct {
	ServiceParams
}

func NewCreditGrantApplicationService(serviceParams ServiceParams) CreditGrantApplicationService {
	return &creditGrantApplicationService{
		ServiceParams: serviceParams,
	}
}

func (s *creditGrantApplicationService) ProcessScheduledApplications(ctx context.Context) error {
	applications, err := s.CreditGrantApplicationRepo.FindAllScheduledApplications(ctx)
	if err != nil {
		return err
	}

	subscriptionService := NewSubscriptionService(s.ServiceParams)
	creditGrantService := NewCreditGrantService(s.ServiceParams)
	// add tenant_id and env_id in context for each application
	for _, cga := range applications {

		// we check if the application is alre	ady applied
		if cga.ApplicationStatus == types.ApplicationStatusApplied {
			// we skip the application if it is already applied
			continue
		}

		ctxWithTenant := context.WithValue(ctx, types.CtxTenantID, cga.TenantID)
		ctxWithEnv := context.WithValue(ctxWithTenant, types.CtxEnvironmentID, cga.EnvironmentID)

		// we validate subscription state
		subscription, err := subscriptionService.GetSubscription(ctxWithEnv, cga.SubscriptionID)
		if err != nil {
			return err
		}

		// we check if the credit grant is active or not
		creditGrant, err := creditGrantService.GetCreditGrant(ctxWithEnv, cga.CreditGrantID)
		if err != nil {
			return err
		}

		err = s.processApplication(ctxWithEnv, cga, subscription.Subscription, creditGrant.CreditGrant)
		if err != nil {
			return err
		}

	}

	return nil
}

func (s *creditGrantApplicationService) processApplication(ctx context.Context, cga *creditgrantapplication.CreditGrantApplication, subscription *subscription.Subscription, creditGrant *creditgrant.CreditGrant) error {

	// validate subscription state
	stateHandler := NewSubscriptionStateHandler(subscription, creditGrant)

	action, reason := stateHandler.DetermineAction()

	switch action {
	case StateActionSkip:
		s.Logger.Infow("skipping application", "application_id", cga.ID, "reason", reason)
		// we skip the application
		return nil
	case StateActionApply:
		s.Logger.Infow("applying application", "application_id", cga.ID, "reason", reason)
		// we apply the application
		return nil
	case StateActionCancel:
		s.Logger.Infow("cancelling application", "application_id", cga.ID, "reason", reason)
		// we cancel the application
		return nil
	case StateActionDefer:
		s.Logger.Infow("deferring application", "application_id", cga.ID, "reason", reason)
		// we defer the application
		return nil
	default:
		s.Logger.Infow("invalid action", "application_id", cga.ID, "reason", reason)
		return nil
	}

	// you create the same idempotency key and check if CGA exists with that key
	// if exists and applied successfully then skip
	// if exists and applied failed then retry
	// if not exists then apply

	// every time you apply:
	// for current period you create a CGA with idempotency key and other details
	// and create a new entry with scheduled status in the DB for next period
	// use GetNextBillingDate to get the period start ending

}
