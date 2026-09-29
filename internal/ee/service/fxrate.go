package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	fxrate "github.com/flexprice/flexprice/internal/domain/fxrate"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// FXRateService configures and resolves tenant FX rates.
type FXRateService interface {
	// ResolveRate returns the rate for from→to using the most specific scope
	// (subscription → customer → tenant) whose validity window covers now.
	ResolveRate(ctx context.Context, req ResolveFXRateRequest) (*FXRateResolution, error)

	CreateFXRate(ctx context.Context, req dto.CreateFXRateRequest) (*dto.FXRateResponse, error)
	GetFXRate(ctx context.Context, id string) (*dto.FXRateResponse, error)
	ListFXRates(ctx context.Context, filter *types.FXRateFilter) (*dto.ListFXRatesResponse, error)
	UpdateFXRate(ctx context.Context, id string, req dto.UpdateFXRateRequest) (*dto.FXRateResponse, error)
	DeleteFXRate(ctx context.Context, id string) error
}

// ResolveFXRateRequest asks for the rate from→to for an optional customer/subscription.
type ResolveFXRateRequest struct {
	From           string
	To             string
	CustomerID     string
	SubscriptionID string
}

// FXRateResolution is the rate a resolution produced. Scope is "identity" when from == to.
type FXRateResolution struct {
	Rate   decimal.Decimal
	RateID string
	Scope  string
	From   string
	To     string
}

type fxRateService struct {
	ServiceParams
}

func NewFXRateService(params ServiceParams) FXRateService {
	return &fxRateService{ServiceParams: params}
}

func (s *fxRateService) ResolveRate(ctx context.Context, req ResolveFXRateRequest) (*FXRateResolution, error) {
	return s.resolveRateAt(ctx, req, time.Now().UTC())
}

// resolveRateAt is ResolveRate with an injected clock, so validity windows are testable.
func (s *fxRateService) resolveRateAt(ctx context.Context, req ResolveFXRateRequest, now time.Time) (*FXRateResolution, error) {
	// Same currency needs no rate and no query.
	if types.IsMatchingCurrency(req.From, req.To) {
		return &FXRateResolution{Rate: decimal.NewFromInt(1), Scope: "identity", From: req.From, To: req.To}, nil
	}

	scopesChecked := make([]string, 0, 3)

	if req.SubscriptionID != "" {
		scopesChecked = append(scopesChecked, "subscription:"+req.SubscriptionID)
		rate, err := s.findOverride(ctx, types.FXRateScopeSubscription, req.SubscriptionID, req.From, req.To, now)
		if err != nil {
			return nil, err
		}
		if rate != nil {
			return toResolution(rate), nil
		}
	}

	if req.CustomerID != "" {
		scopesChecked = append(scopesChecked, "customer:"+req.CustomerID)
		rate, err := s.findOverride(ctx, types.FXRateScopeCustomer, req.CustomerID, req.From, req.To, now)
		if err != nil {
			return nil, err
		}
		if rate != nil {
			return toResolution(rate), nil
		}
	}

	scopesChecked = append(scopesChecked, "tenant")
	tenantRate, err := s.FXRateRepo.GetTenantRate(ctx, req.From, req.To)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, fxRateNotFound(req.From, req.To, scopesChecked)
		}
		return nil, err
	}
	return toResolution(tenantRate), nil
}

// findOverride returns the published override for (scope, scopeID, pair) whose
// window covers now, or nil when none applies.
func (s *fxRateService) findOverride(ctx context.Context, scope types.FXRateScope, scopeID, from, to string, now time.Time) (*fxrate.FXRate, error) {
	scopeCopy := scope
	filter := &types.FXRateFilter{
		QueryFilter:  types.NewNoLimitQueryFilter(),
		Scope:        &scopeCopy,
		ScopeID:      &scopeID,
		FromCurrency: &from,
		ToCurrency:   &to,
	}
	rates, err := s.FXRateRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, r := range rates {
		if windowCoversNow(r.ValidFrom, r.ValidTo, now) {
			return r, nil
		}
	}
	return nil, nil
}

// windowCoversNow treats a nil valid_from as −∞ and a nil valid_to as +∞. valid_to is exclusive.
func windowCoversNow(validFrom, validTo *time.Time, now time.Time) bool {
	if validFrom != nil && validFrom.After(now) {
		return false
	}
	if validTo != nil && !validTo.After(now) {
		return false
	}
	return true
}

func toResolution(r *fxrate.FXRate) *FXRateResolution {
	return &FXRateResolution{
		Rate:   r.Rate,
		RateID: r.ID,
		Scope:  string(r.Scope),
		From:   r.FromCurrency,
		To:     r.ToCurrency,
	}
}

func fxRateNotFound(from, to string, scopesChecked []string) error {
	return ierr.NewErrorf("no FX rate configured for %s → %s", from, to).
		WithHintf("No exchange rate configured for %s → %s. Set a tenant rate first.", from, to).
		WithReportableDetails(map[string]any{
			"from_currency":  from,
			"to_currency":    to,
			"scopes_checked": scopesChecked,
		}).
		Mark(ierr.ErrNotFound)
}

func (s *fxRateService) CreateFXRate(ctx context.Context, req dto.CreateFXRateRequest) (*dto.FXRateResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	rate, err := req.ToFXRate(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.validateForCreate(ctx, rate); err != nil {
		return nil, err
	}
	if err := s.FXRateRepo.Create(ctx, rate); err != nil {
		return nil, err
	}
	return &dto.FXRateResponse{FXRate: rate}, nil
}

func (s *fxRateService) GetFXRate(ctx context.Context, id string) (*dto.FXRateResponse, error) {
	if id == "" {
		return nil, ierr.NewError("fx_rate_id is required").WithHint("FX rate ID is required").Mark(ierr.ErrValidation)
	}
	rate, err := s.FXRateRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &dto.FXRateResponse{FXRate: rate}, nil
}

func (s *fxRateService) ListFXRates(ctx context.Context, filter *types.FXRateFilter) (*dto.ListFXRatesResponse, error) {
	if filter == nil {
		filter = types.NewFXRateFilter()
	}
	rates, err := s.FXRateRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	count, err := s.FXRateRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}
	items := lo.Map(rates, func(r *fxrate.FXRate, _ int) *dto.FXRateResponse {
		return &dto.FXRateResponse{FXRate: r}
	})
	resp := types.NewListResponse(items, count, filter.GetLimit(), filter.GetOffset())
	return &resp, nil
}

func (s *fxRateService) UpdateFXRate(ctx context.Context, id string, req dto.UpdateFXRateRequest) (*dto.FXRateResponse, error) {
	if id == "" {
		return nil, ierr.NewError("fx_rate_id is required").WithHint("FX rate ID is required").Mark(ierr.ErrValidation)
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	existing, err := s.FXRateRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if existing.Scope == types.FXRateScopeTenant && (req.ValidFrom != nil || req.ValidTo != nil) {
		return nil, ierr.NewError("tenant rates have no validity window").
			WithHint("A tenant FX rate applies at all times; validity windows are only for customer or subscription overrides.").
			Mark(ierr.ErrValidation)
	}

	builder := fxrate.NewFXRateBuilder(existing)

	if req.Rate != nil {
		rate, perr := decimal.NewFromString(*req.Rate)
		if perr != nil {
			return nil, ierr.NewError("invalid rate").WithHint("Rate must be a valid decimal number").Mark(ierr.ErrValidation)
		}
		if !rate.IsPositive() {
			return nil, ierr.NewError("rate must be greater than zero").WithHint("FX rate must be a positive number").Mark(ierr.ErrValidation)
		}
		builder.WithRate(rate)
	}

	if existing.Scope != types.FXRateScopeTenant {
		newFrom := existing.ValidFrom
		newTo := existing.ValidTo
		if req.ValidFrom != nil {
			newFrom = req.ValidFrom
		}
		if req.ValidTo != nil {
			newTo = req.ValidTo
		}
		if newFrom != nil && newTo != nil && !newFrom.Before(*newTo) {
			return nil, ierr.NewError("valid_from must be before valid_to").
				WithHint("The start of a validity window must be before its end.").
				Mark(ierr.ErrValidation)
		}
		overlaps, oerr := s.FXRateRepo.FindOverlapping(ctx, existing.Scope, existing.ScopeID, existing.FromCurrency, existing.ToCurrency, newFrom, newTo, existing.ID)
		if oerr != nil {
			return nil, oerr
		}
		if len(overlaps) > 0 {
			return nil, ierr.NewErrorf("overlapping FX rate window for %s → %s", existing.FromCurrency, existing.ToCurrency).
				WithHint("Another override for this scope and currency pair covers an overlapping period.").
				Mark(ierr.ErrValidation)
		}
		builder.WithValidFrom(newFrom).WithValidTo(newTo)
	}

	if req.Metadata != nil {
		builder.WithMetadata(req.Metadata)
	}

	updated := builder.Build()
	if err := s.FXRateRepo.Update(ctx, updated); err != nil {
		return nil, err
	}
	return &dto.FXRateResponse{FXRate: updated}, nil
}

func (s *fxRateService) DeleteFXRate(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("fx_rate_id is required").WithHint("FX rate ID is required").Mark(ierr.ErrValidation)
	}
	existing, err := s.FXRateRepo.Get(ctx, id)
	if err != nil {
		return err
	}
	if existing.Scope == types.FXRateScopeTenant {
		return ierr.NewError("tenant rates cannot be deleted").
			WithHint("A tenant FX rate cannot be deleted because every override falls back to it. Update its value instead.").
			WithReportableDetails(map[string]any{"fx_rate_id": id}).
			Mark(ierr.ErrValidation)
	}
	return s.FXRateRepo.Delete(ctx, existing)
}

// validateForCreate enforces the §8.1 guardrails on a new rate.
func (s *fxRateService) validateForCreate(ctx context.Context, r *fxrate.FXRate) error {
	if err := s.validateCurrencies(ctx, r.FromCurrency, r.ToCurrency); err != nil {
		return err
	}
	if !r.Rate.IsPositive() {
		return ierr.NewError("rate must be greater than zero").
			WithHint("FX rate must be a positive number").
			Mark(ierr.ErrValidation)
	}

	switch r.Scope {
	case types.FXRateScopeTenant:
		if r.ValidFrom != nil || r.ValidTo != nil {
			return ierr.NewError("tenant rates have no validity window").
				WithHint("Validity windows are only for customer or subscription overrides.").
				Mark(ierr.ErrValidation)
		}
	case types.FXRateScopeCustomer:
		if r.ScopeID == "" {
			return ierr.NewError("scope_id is required for a customer rate").
				WithHint("Provide the customer_id as scope_id.").
				Mark(ierr.ErrValidation)
		}
	case types.FXRateScopeSubscription:
		if r.ScopeID == "" {
			return ierr.NewError("scope_id is required for a subscription rate").
				WithHint("Provide the subscription_id as scope_id.").
				Mark(ierr.ErrValidation)
		}
		sub, err := s.SubRepo.Get(ctx, r.ScopeID)
		if err != nil {
			return err
		}
		if !types.IsMatchingCurrency(sub.Currency, r.FromCurrency) {
			return ierr.NewErrorf("subscription currency %s does not match from_currency %s", sub.Currency, r.FromCurrency).
				WithHint("A subscription-scoped rate must convert from the subscription's own currency.").
				Mark(ierr.ErrValidation)
		}
	}

	if r.ValidFrom != nil && r.ValidTo != nil && !r.ValidFrom.Before(*r.ValidTo) {
		return ierr.NewError("valid_from must be before valid_to").
			WithHint("The start of a validity window must be before its end.").
			Mark(ierr.ErrValidation)
	}

	if r.Scope == types.FXRateScopeTenant {
		// one tenant rate per pair
		if _, err := s.FXRateRepo.GetTenantRate(ctx, r.FromCurrency, r.ToCurrency); err == nil {
			return ierr.NewErrorf("a tenant FX rate for %s → %s already exists", r.FromCurrency, r.ToCurrency).
				WithHint("Update the existing tenant rate instead of creating another.").
				Mark(ierr.ErrAlreadyExists)
		} else if !ierr.IsNotFound(err) {
			return err
		}
		return nil
	}

	// overrides need a tenant rate for the same pair
	if _, err := s.FXRateRepo.GetTenantRate(ctx, r.FromCurrency, r.ToCurrency); err != nil {
		if ierr.IsNotFound(err) {
			return ierr.NewErrorf("no tenant FX rate configured for %s → %s", r.FromCurrency, r.ToCurrency).
				WithHint("Configure a tenant rate for this pair before adding an override.").
				Mark(ierr.ErrValidation)
		}
		return err
	}

	// overrides must not overlap an existing window for the same scope/pair
	overlaps, err := s.FXRateRepo.FindOverlapping(ctx, r.Scope, r.ScopeID, r.FromCurrency, r.ToCurrency, r.ValidFrom, r.ValidTo, "")
	if err != nil {
		return err
	}
	if len(overlaps) > 0 {
		return ierr.NewErrorf("overlapping FX rate window for %s → %s", r.FromCurrency, r.ToCurrency).
			WithHint("Another override for this scope and currency pair covers an overlapping period.").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// validateCurrencies rejects equal codes, non-fiat codes, and tenant custom currencies.
func (s *fxRateService) validateCurrencies(ctx context.Context, from, to string) error {
	if types.IsMatchingCurrency(from, to) {
		return ierr.NewError("from_currency and to_currency must differ").
			WithHint("An FX rate converts between two different currencies.").
			Mark(ierr.ErrValidation)
	}
	for _, code := range []string{from, to} {
		if _, ok := types.CURRENCY_CONFIG[strings.ToLower(code)]; !ok {
			return ierr.NewErrorf("unsupported currency %s", code).
				WithHint("FX rates support fiat ISO currency codes only.").
				Mark(ierr.ErrValidation)
		}
	}
	ccCfg, err := s.customCurrencyConfig(ctx)
	if err != nil {
		return err
	}
	if ccCfg.IsCustom(from) || ccCfg.IsCustom(to) {
		return ierr.NewError("custom currencies are not allowed on an FX rate").
			WithHint("A custom currency converts through the custom currency config, not FX rates.").
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (s *fxRateService) customCurrencyConfig(ctx context.Context) (types.CustomCurrencyConfig, error) {
	settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)
	return GetSetting[types.CustomCurrencyConfig](settingsSvc, ctx, types.SettingKeyCustomCurrencyConfig)
}
