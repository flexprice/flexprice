package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
)

// captureWebhookPublisher records every webhook event it receives.
type captureWebhookPublisher struct {
	events []*types.WebhookEvent
}

func (c *captureWebhookPublisher) PublishWebhook(_ context.Context, event *types.WebhookEvent) error {
	c.events = append(c.events, event)
	return nil
}

func (c *captureWebhookPublisher) Close() error { return nil }

// buildRenewalService creates a SubscriptionService wired to the suite's stores but with a
// caller-supplied config and webhook publisher so each sub-test can vary those independently.
func (s *SubscriptionServiceSuite) buildRenewalService(cfg *config.Configuration, whPublisher *captureWebhookPublisher) SubscriptionService {
	return NewSubscriptionService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     cfg,
		DB:                         s.GetDB(),
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:            s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		PriceUnitRepo:              s.GetStores().PriceUnitRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		AddonRepo:                  testutil.NewInMemoryAddonStore(),
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           whPublisher,
		ProrationCalculator:        s.GetCalculator(),
		FeatureUsageRepo:           s.GetStores().FeatureUsageRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
	})
}

func (s *SubscriptionServiceSuite) TestProcessSubscriptionRenewalDueAlert() {
	ctx := s.GetContext()
	now := time.Now().UTC()

	// activeSub returns a minimal active, published subscription ending at periodEnd.
	activeSub := func(id string, periodEnd time.Time, cancelAtPeriodEnd bool) *subscription.Subscription {
		return &subscription.Subscription{
			ID:                 id,
			CustomerID:         s.testData.customer.ID,
			PlanID:             s.testData.plan.ID,
			SubscriptionStatus: types.SubscriptionStatusActive,
			CurrentPeriodEnd:   periodEnd,
			CancelAtPeriodEnd:  cancelAtPeriodEnd,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
	}

	// With lookAheadHours=24: target = now+24h, window = [now+23h, now+25h].
	inWindow := now.Add(24 * time.Hour)
	outsideWindow := now.Add(48 * time.Hour)

	tests := []struct {
		name             string
		lookAheadHours   int // 0 means use default (24)
		subscriptions    []*subscription.Subscription
		wantErr          bool
		wantWebhookCount int
		wantEventName    types.WebhookEventName
	}{
		{
			name:           "negative_look_ahead_hours_returns_error",
			lookAheadHours: -1,
			wantErr:        true,
		},
		{
			name:             "no_subscriptions_returns_no_error_no_webhooks",
			wantWebhookCount: 0,
		},
		{
			name:             "subscription_in_window_publishes_renewal_due_webhook",
			subscriptions:    []*subscription.Subscription{activeSub("sub_in_window", inWindow, false)},
			wantWebhookCount: 1,
			wantEventName:    types.WebhookEventSubscriptionRenewalDue,
		},
		{
			name:             "subscription_outside_window_is_excluded",
			subscriptions:    []*subscription.Subscription{activeSub("sub_out_of_window", outsideWindow, false)},
			wantWebhookCount: 0,
		},
		{
			name:             "cancel_at_period_end_subscription_is_excluded",
			subscriptions:    []*subscription.Subscription{activeSub("sub_cancel_at_end", inWindow, true)},
			wantWebhookCount: 0,
		},
		{
			name: "multiple_subscriptions_only_in_window_ones_get_webhook",
			subscriptions: []*subscription.Subscription{
				activeSub("sub_in_1", inWindow, false),
				activeSub("sub_in_2", inWindow, false),
				activeSub("sub_out", outsideWindow, false),
				activeSub("sub_cancel", inWindow, true),
			},
			wantWebhookCount: 2,
		},
		{
			name:           "custom_look_ahead_uses_correct_window",
			lookAheadHours: 48,
			// target = now+48h, window = [now+47h, now+49h]
			// inWindow (now+24h) is outside this window; outsideWindow (now+48h) is inside.
			subscriptions: []*subscription.Subscription{
				activeSub("sub_48h", outsideWindow, false),
				activeSub("sub_24h", inWindow, false), // outside the 48h±1h window
			},
			wantWebhookCount: 1,
			wantEventName:    types.WebhookEventSubscriptionRenewalDue,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.ClearStores()

			cfg := *s.GetConfig()
			cfg.Subscription.RenewalAlertLookAheadHours = tc.lookAheadHours

			capture := &captureWebhookPublisher{}
			svc := s.buildRenewalService(&cfg, capture)

			for _, sub := range tc.subscriptions {
				s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))
			}

			err := svc.ProcessSubscriptionRenewalDueAlert(ctx)

			if tc.wantErr {
				s.Error(err)
				return
			}
			s.NoError(err)
			s.Len(capture.events, tc.wantWebhookCount)
			if tc.wantEventName != "" && len(capture.events) > 0 {
				s.Equal(tc.wantEventName, capture.events[0].EventName)
			}
		})
	}
}
