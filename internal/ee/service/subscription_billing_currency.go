package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// validateSubscriptionBillingCurrency enforces §8.3 at subscription create. It returns the billing
// currency to create a subscription-scope fx_rate for (only when a fiat subscription is convertible
// and the request carries an fx_rate); it returns "" when no inline rate should be created.
//
//   - Nothing to convert (no billing currency, or it matches the subscription currency): an fx_rate in
//     the request is rejected; otherwise a no-op.
//   - Custom-currency subscription: needs a custom factor for the billing currency; an fx_rate is rejected.
//   - Fiat subscription: needs a tenant rate for the pair; an fx_rate then creates a subscription override.
func (s *subscriptionService) validateSubscriptionBillingCurrency(
	ctx context.Context,
	sub *subscription.Subscription,
	subscriber *customer.Customer,
	ccCfg types.CustomCurrencyConfig,
	req dto.CreateSubscriptionRequest,
) (string, error) {
	invoicingCust := subscriber
	if sub.InvoicingCustomerID != nil && *sub.InvoicingCustomerID != "" && *sub.InvoicingCustomerID != subscriber.ID {
		c, err := s.CustomerRepo.Get(ctx, *sub.InvoicingCustomerID)
		if err != nil {
			return "", err
		}
		invoicingCust = c
	}

	chargeCurrency := sub.Currency
	billing := ""
	if invoicingCust.BillingCurrency != nil {
		billing = *invoicingCust.BillingCurrency
	}

	// Nothing to convert.
	if billing == "" || types.IsMatchingCurrency(billing, chargeCurrency) {
		if req.FxRate != nil {
			return "", ierr.NewError("fx_rate is not applicable").
				WithHint("This subscription's currency needs no conversion, so an fx_rate cannot be set.").
				Mark(ierr.ErrValidation)
		}
		return "", nil
	}

	// Custom-currency subscription: convert through the custom factor, never fx_rates.
	if ccCfg.IsCustom(chargeCurrency) {
		if req.FxRate != nil {
			return "", ierr.NewError("fx_rate is not applicable to a custom-currency subscription").
				WithHint("A custom-currency subscription converts through its configured factor, not an fx_rate.").
				Mark(ierr.ErrValidation)
		}
		if ccCfg.RateFor(chargeCurrency, billing).IsZero() {
			return "", ierr.NewErrorf("no conversion factor from %s to %s", chargeCurrency, billing).
				WithHintf("Add a %s to %s factor to the custom currency configuration before creating this subscription.", chargeCurrency, billing).
				Mark(ierr.ErrValidation)
		}
		return "", nil
	}

	// Fiat subscription: every override needs a tenant rate for the pair (§8.1).
	if _, err := s.FXRateRepo.GetTenantRate(ctx, chargeCurrency, billing); err != nil {
		return "", ierr.NewErrorf("no exchange rate configured for %s to %s", chargeCurrency, billing).
			WithHintf("Set a tenant rate for %s to %s before creating this subscription.", chargeCurrency, billing).
			WithReportableDetails(map[string]any{"from": chargeCurrency, "to": billing}).
			Mark(ierr.ErrValidation)
	}

	if req.FxRate != nil {
		return billing, nil
	}
	return "", nil
}
