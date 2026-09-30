package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// validateBillingCurrency enforces the §8.2 guardrails for setting or clearing a customer's billing
// currency. A change (set or clear) is blocked while a checkout session is open. Setting one requires
// a valid fiat currency and a rate or custom factor for every active/trialing/paused subscription and
// every wallet held in a different currency. billingCurrency is the already-normalized target (nil or
// empty clears it).
func (s *customerService) validateBillingCurrency(ctx context.Context, customerID string, billingCurrency *string) error {
	open, err := s.hasOpenCheckoutSession(ctx, customerID)
	if err != nil {
		return err
	}
	if open {
		return ierr.NewError("open checkout session").
			WithHint("Complete or cancel the open checkout first.").
			Mark(ierr.ErrValidation)
	}

	// Clearing back to null is allowed once no checkout is open.
	if billingCurrency == nil || *billingCurrency == "" {
		return nil
	}
	target := strings.ToLower(*billingCurrency)

	settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)
	ccCfg, err := GetSetting[types.CustomCurrencyConfig](settingsSvc, ctx, types.SettingKeyCustomCurrencyConfig)
	if err != nil {
		return err
	}

	if _, ok := types.CURRENCY_CONFIG[target]; !ok || ccCfg.IsCustom(target) {
		return ierr.NewError("invalid billing currency").
			WithHint("Billing currency must be a supported fiat ISO currency.").
			WithReportableDetails(map[string]any{"billing_currency": target}).
			Mark(ierr.ErrValidation)
	}

	subs, err := s.activeConvertibleSubscriptions(ctx, customerID)
	if err != nil {
		return err
	}
	var missingSubs []string
	for _, sub := range subs {
		if types.IsMatchingCurrency(sub.Currency, target) {
			continue
		}
		if !s.conversionAvailable(ctx, ccCfg, sub.Currency, target) {
			missingSubs = append(missingSubs, fmt.Sprintf("%s->%s", sub.Currency, target))
		}
	}
	if len(missingSubs) > 0 {
		return ierr.NewError("missing exchange rates for subscriptions").
			WithHint("Configure a rate or custom factor for each pair before setting this billing currency.").
			WithReportableDetails(map[string]any{"missing_pairs": lo.Uniq(missingSubs)}).
			Mark(ierr.ErrValidation)
	}

	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return err
	}
	var missingWallets []string
	for _, w := range wallets {
		if types.IsMatchingCurrency(w.Currency, target) {
			continue
		}
		if !s.conversionAvailable(ctx, ccCfg, w.Currency, target) {
			missingWallets = append(missingWallets, fmt.Sprintf("%s->%s", w.Currency, target))
		}
	}
	if len(missingWallets) > 0 {
		return ierr.NewError("missing exchange rates for wallets").
			WithHint("Configure a rate or custom factor for each wallet pair before setting this billing currency.").
			WithReportableDetails(map[string]any{"missing_pairs": lo.Uniq(missingWallets)}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// conversionAvailable reports whether from can be converted to the billing currency: a custom
// currency needs a configured factor; a fiat currency needs a published tenant rate.
func (s *customerService) conversionAvailable(ctx context.Context, ccCfg types.CustomCurrencyConfig, from, to string) bool {
	if ccCfg.IsCustom(from) {
		return !ccCfg.RateFor(from, to).IsZero()
	}
	if _, err := s.FXRateRepo.GetTenantRate(ctx, from, to); err != nil {
		return false
	}
	return true
}

func (s *customerService) hasOpenCheckoutSession(ctx context.Context, customerID string) (bool, error) {
	filter := &types.CheckoutSessionFilter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		CustomerIDs:      []string{customerID},
		CheckoutStatuses: types.ActiveCheckoutStatuses(),
	}
	sessions, err := s.CheckoutSessionRepo.List(ctx, filter)
	if err != nil {
		return false, err
	}
	return len(sessions) > 0, nil
}

// activeConvertibleSubscriptions returns the customer's active, trialing or paused subscriptions,
// counting it as either the subscriber or the invoicing customer. The Go-side ownership check keeps
// this correct whether or not the underlying store filters by invoicing customer.
func (s *customerService) activeConvertibleSubscriptions(ctx context.Context, customerID string) ([]*subscription.Subscription, error) {
	statuses := []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusPaused,
	}

	seen := make(map[string]*subscription.Subscription)
	collect := func(subs []*subscription.Subscription) {
		for _, sub := range subs {
			if sub.CustomerID == customerID || sub.GetInvoicingCustomerID() == customerID {
				seen[sub.ID] = sub
			}
		}
	}

	bySubscriber := types.NewNoLimitSubscriptionFilter()
	bySubscriber.CustomerID = customerID
	bySubscriber.SubscriptionStatus = statuses
	subs, err := s.SubRepo.List(ctx, bySubscriber)
	if err != nil {
		return nil, err
	}
	collect(subs)

	byInvoicing := types.NewNoLimitSubscriptionFilter()
	byInvoicing.InvoicingCustomerIDs = []string{customerID}
	byInvoicing.SubscriptionStatus = statuses
	subs, err = s.SubRepo.List(ctx, byInvoicing)
	if err != nil {
		return nil, err
	}
	collect(subs)

	return lo.Values(seen), nil
}
