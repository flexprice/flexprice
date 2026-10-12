package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// validateBillingCurrency blocks a change while a checkout is open, and a new value without a rate
// for every live subscription and wallet in another currency. nil or "" clears it.
func (s *customerService) validateBillingCurrency(ctx context.Context, customerID string, billingCurrency *string) error {
	openSessions, err := openCheckoutSessionIDs(ctx, s.ServiceParams, customerID)
	if err != nil {
		return err
	}
	if len(openSessions) > 0 {
		return ierr.NewError("open checkout session").
			WithHint("Complete or cancel the open checkout first.").
			WithReportableDetails(map[string]any{
				"customer_id":          customerID,
				"checkout_session_ids": openSessions,
			}).
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
		ok, err := conversionAvailable(ctx, s.ServiceParams, ccCfg, sub.Currency, target)
		if err != nil {
			return err
		}
		if !ok {
			missingSubs = append(missingSubs, fmt.Sprintf("%s->%s", sub.Currency, target))
		}
	}
	if len(missingSubs) > 0 {
		return missingExchangeRatesError(ccCfg, "missing conversions for subscriptions", missingSubs)
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
		ok, err := conversionAvailable(ctx, s.ServiceParams, ccCfg, w.Currency, target)
		if err != nil {
			return err
		}
		if !ok {
			missingWallets = append(missingWallets, fmt.Sprintf("%s->%s", w.Currency, target))
		}
	}
	if len(missingWallets) > 0 {
		return missingExchangeRatesError(ccCfg, "missing conversions for wallets", missingWallets)
	}

	return nil
}

// activeConvertibleSubscriptions returns active, trialing or paused subscriptions invoiced to the
// customer: its own subscriptions without another payer, and ones it pays for on another's behalf.
func (s *customerService) activeConvertibleSubscriptions(ctx context.Context, customerID string) ([]*subscription.Subscription, error) {
	statuses := []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusPaused,
	}

	seen := make(map[string]*subscription.Subscription)
	collect := func(subs []*subscription.Subscription) {
		for _, sub := range subs {
			if sub.GetInvoicingCustomerID() == customerID {
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

// missingExchangeRatesError lists, sorted, every pair that needs a rate (or, for a custom currency,
// a factor) before the billing currency can change.
func missingExchangeRatesError(ccCfg types.CustomCurrencyConfig, reason string, pairs []string) error {
	pairs = lo.Uniq(pairs)
	slices.Sort(pairs)
	factors, rates := lo.FilterReject(pairs, func(pair string, _ int) bool {
		from, _, _ := strings.Cut(pair, "->")
		return ccCfg.IsCustom(from)
	})

	var missing []string
	if len(rates) > 0 {
		missing = append(missing, "No exchange rate for "+fxPairKeysLabel(rates))
	}
	if len(factors) > 0 {
		missing = append(missing, "No conversion factor for "+fxPairKeysLabel(factors))
	}
	fix := "Add these rates"
	switch {
	case len(rates) > 0 && len(factors) > 0:
		fix = "Add these rates and custom currency factors"
	case len(factors) > 0:
		fix = "Add these factors in the custom currency settings"
	}

	return ierr.NewError(reason).
		WithHintf("%s. %s before setting this billing currency.", strings.Join(missing, ". "), fix).
		WithReportableDetails(map[string]any{"missing_pairs": pairs}).
		Mark(ierr.ErrValidation)
}
