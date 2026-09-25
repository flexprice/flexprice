package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// TaxEngine calculates tax and records it with whoever owns the filing. The native engine
// implements it too, so the callers run Calculate then Commit unconditionally and never branch
// on provider.
type TaxEngine interface {
	// GetProvider names the engine. Stamped onto every row it writes.
	GetProvider() types.TaxProvider

	// Calculate taxes the request's amount against the invoice it names. An invoice and a
	// credit note ask the same question, so they send the same request. Persists nothing on
	// the engine's side and is safe to repeat, so a preview calls it freely.
	Calculate(ctx context.Context, req dto.TaxCalculationRequest) (*dto.TaxCalculationResult, error)

	// Commit records this invoice's tax in the engine's own books, once, after finalization.
	Commit(ctx context.Context, inv *invoice.Invoice, appliedTaxes []*taxapplied.TaxApplied) (taxTransactionID string, err error)

	// Reverse un-files tax the engine already recorded, for a credit note against an invoice
	// or for a voided invoice, and returns the provider's transaction for the reversal.
	Reverse(ctx context.Context, req dto.TaxReversalRequest) (taxTransactionID string, err error)

	// TaxIdentifiers reads the registered tax numbers a compliant invoice has to carry. Read
	// when the invoice is rendered rather than stored, because the rendered PDF is kept and a
	// regeneration is rare. The native engine holds none and returns nothing.
	TaxIdentifiers(ctx context.Context, customerID string) (biller, customer []types.TaxIdentifier, err error)
}

// NewTaxEngine returns the engine the tenant's setting names for this environment. This is
// the only place in the system that names a provider.
//
// An absent setting, a disabled one and flexprice all resolve to native, so no existing tenant
// needs a migration. A named engine that cannot be built is an error and never a quiet fallback,
// because billing native rates under an external configuration produces a wrong invoice
// that looks right.
func NewTaxEngine(ctx context.Context, params ServiceParams) (TaxEngine, error) {
	cfg, err := taxConfig(ctx, params)
	if err != nil {
		return nil, err
	}

	if !cfg.UsesExternalTaxEngine() {
		return &nativeTaxEngine{ServiceParams: params}, nil
	}

	switch cfg.Provider {
	case types.TaxProviderStripe:
		if _, err := params.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe); err != nil {
			if ierr.IsNotFound(err) {
				return nil, ierr.WithError(err).
					WithHint("Connect Stripe before selecting it as the tax engine").
					Mark(ierr.ErrValidation)
			}
			return nil, err
		}
		return &stripeTaxEngine{ServiceParams: params}, nil

	default:
		// A typo in the setting must not silently change how a tenant is taxed.
		return nil, ierr.NewErrorf("unknown tax provider %q", cfg.Provider).
			WithHintf("An external tax engine must be one of: %s", types.JoinTaxProviders(types.ExternalTaxProviders())).
			WithReportableDetails(map[string]any{
				"tax_provider": cfg.Provider,
				"allowed":      types.ExternalTaxProviders(),
			}).
			Mark(ierr.ErrValidation)
	}
}

// taxConfig reads the tenant and environment's tax setting. Its zero value is the native
// engine, so a tenant that has never held the setting reads as native rather than failing.
func taxConfig(ctx context.Context, params ServiceParams) (types.TaxConfig, error) {
	settingsSvc := NewSettingsService(params).(*settingsService)

	cfg, err := GetSetting[types.TaxConfig](settingsSvc, ctx, types.SettingKeyTaxConfig)
	if err != nil {
		return types.TaxConfig{}, ierr.WithError(err).
			WithHint("Failed to load the tax configuration").
			Mark(ierr.ErrDatabase)
	}

	return cfg, nil
}
