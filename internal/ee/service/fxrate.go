package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	fxrate "github.com/flexprice/flexprice/internal/domain/fxrate"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// FXRateService configures and resolves tenant FX rates.
type FXRateService interface {
	// ResolveRate returns the rate for from→to using the most specific scope
	// (subscription → customer → tenant) whose validity window covers now. No HTTP
	// endpoint is exposed yet; this backs the invoice-conversion path.
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

// FXRateResolution is the rate a resolution produced.
type FXRateResolution struct {
	Rate   decimal.Decimal
	RateID string
	Scope  types.FXRateScope
	Source types.FXRateSource
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
	scopesChecked := make([]string, 0, 3)

	if req.SubscriptionID != "" {
		scopesChecked = append(scopesChecked, "subscription:"+req.SubscriptionID)
		rate, err := s.findOverride(ctx, types.FXRateScopeSubscription, req.SubscriptionID, req.From, req.To, now)
		if err != nil {
			return nil, err
		}
		if rate != nil {
			return resolveFixed(rate)
		}
	}

	if req.CustomerID != "" {
		scopesChecked = append(scopesChecked, "customer:"+req.CustomerID)
		rate, err := s.findOverride(ctx, types.FXRateScopeCustomer, req.CustomerID, req.From, req.To, now)
		if err != nil {
			return nil, err
		}
		if rate != nil {
			return resolveFixed(rate)
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
	return resolveFixed(tenantRate)
}

// findOverride returns the published override for (scope, scopeID, pair) whose window covers now.
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
		if windowCoversNow(r.StartDate, r.EndDate, now) {
			return r, nil
		}
	}
	return nil, nil
}

// windowCoversNow treats a nil start_date as −∞ and a nil end_date as +∞. end_date is exclusive.
func windowCoversNow(startDate, endDate *time.Time, now time.Time) bool {
	if startDate != nil && startDate.After(now) {
		return false
	}
	if endDate != nil && !endDate.After(now) {
		return false
	}
	return true
}

// resolveFixed turns a matched rate into a resolution. Only fixed rates resolve to a value
// today; a market rate is rejected until the market-rate integration is wired.
func resolveFixed(r *fxrate.FXRate) (*FXRateResolution, error) {
	if r.Source == types.FXRateSourceMarket {
		return nil, ierr.NewErrorf("market FX rate resolution is not supported yet for %s → %s", r.FromCurrency, r.ToCurrency).
			WithHint("This pair is configured to use market rates, which are not yet available.").
			Mark(ierr.ErrInvalidOperation)
	}
	return toResolution(r), nil
}

func toResolution(r *fxrate.FXRate) *FXRateResolution {
	return &FXRateResolution{
		Rate:   r.Rate,
		RateID: r.ID,
		Scope:  r.Scope,
		Source: r.Source,
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
	// Serialize the overlap check and the write under one advisory lock + transaction, so two
	// concurrent creates for the same scope/pair cannot both pass the check and persist
	// overlapping overrides. The overlap read runs on the writer (via the tx), avoiding replica lag.
	if err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.lockFXRateScope(txCtx, rate.Scope, rate.ScopeID, rate.FromCurrency, rate.ToCurrency); err != nil {
			return err
		}
		if err := s.validateForCreate(txCtx, rate); err != nil {
			return err
		}
		return s.FXRateRepo.Create(txCtx, rate)
	}); err != nil {
		return nil, err
	}
	return &dto.FXRateResponse{FXRate: rate}, nil
}

// lockFXRateScope takes a transaction-scoped advisory lock on a (tenant, env, scope, scope_id, pair)
// key, serializing concurrent writers so the no-overlapping-window check and the write are atomic.
func (s *fxRateService) lockFXRateScope(ctx context.Context, scope types.FXRateScope, scopeID, from, to string) error {
	key := fmt.Sprintf("fxrate:%s:%s:%s:%s:%s:%s",
		types.GetTenantID(ctx), types.GetEnvironmentID(ctx), scope, scopeID,
		strings.ToLower(from), strings.ToLower(to))
	return s.DB.LockWithWait(ctx, postgres.LockRequest{Key: key})
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

	// Only a published rate can be updated. Get does not filter by status, so without this an
	// archived override could be revived and have its window edited to overlap a live one.
	if existing.Status != types.StatusPublished {
		return nil, ierr.NewError("cannot update an archived fx rate").
			WithHint("This FX rate is archived; create a new override instead of updating it.").
			Mark(ierr.ErrValidation)
	}

	if existing.Scope == types.FXRateScopeTenant && (req.StartDate != nil || req.EndDate != nil) {
		return nil, ierr.NewError("tenant rates have no validity window").
			WithHint("A tenant FX rate applies at all times; validity windows are only for customer or subscription overrides.").
			Mark(ierr.ErrValidation)
	}

	builder := fxrate.NewFXRateBuilder(existing)

	if req.Rate != nil {
		if !req.Rate.IsPositive() {
			return nil, ierr.NewError("rate must be greater than zero").WithHint("FX rate must be a positive number").Mark(ierr.ErrValidation)
		}
		builder.WithRate(*req.Rate)
	}

	newStart := existing.StartDate
	newEnd := existing.EndDate
	if existing.Scope != types.FXRateScopeTenant {
		if req.StartDate != nil {
			newStart = req.StartDate
		}
		if req.EndDate != nil {
			newEnd = req.EndDate
		}
		if newStart != nil && newEnd != nil && !newStart.Before(*newEnd) {
			return nil, ierr.NewError("start_date must be before end_date").
				WithHint("The start of a validity window must be before its end.").
				Mark(ierr.ErrValidation)
		}
		builder.WithStartDate(newStart).WithEndDate(newEnd)
	}

	if req.Metadata != nil {
		builder.WithMetadata(req.Metadata)
	}

	updated := builder.Build()

	// Serialize the overlap check and the write under one advisory lock + transaction, reading on
	// the writer, so concurrent updates cannot create overlapping windows for the same scope/pair.
	if err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.lockFXRateScope(txCtx, existing.Scope, existing.ScopeID, existing.FromCurrency, existing.ToCurrency); err != nil {
			return err
		}
		if existing.Scope != types.FXRateScopeTenant {
			overlaps, oerr := s.FXRateRepo.FindOverlapping(txCtx, existing.Scope, existing.ScopeID, existing.FromCurrency, existing.ToCurrency, newStart, newEnd, existing.ID)
			if oerr != nil {
				return oerr
			}
			if len(overlaps) > 0 {
				return overlappingWindowError(existing.FromCurrency, existing.ToCurrency, overlaps)
			}
		}
		return s.FXRateRepo.Update(txCtx, updated)
	}); err != nil {
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
	if err := r.Source.Validate(); err != nil {
		return err
	}
	if err := s.validateCurrencies(ctx, r.FromCurrency, r.ToCurrency); err != nil {
		return err
	}
	// A fixed rate carries its own value; a market rate is resolved from the market-rate
	// integration at conversion time, so its stored rate is not required to be positive.
	if r.Source == types.FXRateSourceFixed && !r.Rate.IsPositive() {
		return ierr.NewError("rate must be greater than zero").
			WithHint("FX rate must be a positive number").
			Mark(ierr.ErrValidation)
	}

	switch r.Scope {
	case types.FXRateScopeTenant:
		if r.StartDate != nil || r.EndDate != nil {
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
		if _, err := s.CustomerRepo.Get(ctx, r.ScopeID); err != nil {
			return err
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

	if r.StartDate != nil && r.EndDate != nil && !r.StartDate.Before(*r.EndDate) {
		return ierr.NewError("start_date must be before end_date").
			WithHint("The start of a validity window must be before its end.").
			Mark(ierr.ErrValidation)
	}

	if r.Scope == types.FXRateScopeTenant {
		// one tenant rate per pair
		if existing, err := s.FXRateRepo.GetTenantRate(ctx, r.FromCurrency, r.ToCurrency); err == nil {
			return ierr.NewErrorf("a tenant FX rate for %s → %s already exists", r.FromCurrency, r.ToCurrency).
				WithHintf("A %s rate already exists. Edit it instead of adding another.", fxPairLabel(r.FromCurrency, r.ToCurrency)).
				WithReportableDetails(map[string]any{"fx_rate_id": existing.ID}).
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
				WithHintf("Add a global %s rate before adding an override.", fxPairLabel(r.FromCurrency, r.ToCurrency)).
				WithReportableDetails(map[string]any{"missing_pairs": []string{fxPairKey(r.FromCurrency, r.ToCurrency)}}).
				Mark(ierr.ErrValidation)
		}
		return err
	}

	// overrides must not overlap an existing window for the same scope/pair
	overlaps, err := s.FXRateRepo.FindOverlapping(ctx, r.Scope, r.ScopeID, r.FromCurrency, r.ToCurrency, r.StartDate, r.EndDate, "")
	if err != nil {
		return err
	}
	if len(overlaps) > 0 {
		return overlappingWindowError(r.FromCurrency, r.ToCurrency, overlaps)
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

// conversionAvailable reports whether from converts to the billing currency (custom factor or
// published tenant rate). Only a missing rate is "unavailable"; other lookup errors are returned.
func conversionAvailable(ctx context.Context, params ServiceParams, ccCfg types.CustomCurrencyConfig, from, to string) (bool, error) {
	if ccCfg.IsCustom(from) {
		return !ccCfg.RateFor(from, to).IsZero(), nil
	}
	if _, err := params.FXRateRepo.GetTenantRate(ctx, from, to); err != nil {
		if ierr.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// fxPairKey is the "from->to" form used in error details (missing_pairs).
func fxPairKey(from, to string) string {
	return fmt.Sprintf("%s->%s", strings.ToLower(from), strings.ToLower(to))
}

// fxPairLabel is the "USD → INR" form used in user-facing messages.
func fxPairLabel(from, to string) string {
	return fmt.Sprintf("%s → %s", strings.ToUpper(from), strings.ToUpper(to))
}

// fxPairKeysLabel turns sorted "from->to" keys into "USD → EUR, GBP → EUR".
func fxPairKeysLabel(keys []string) string {
	labels := make([]string, 0, len(keys))
	for _, key := range keys {
		from, to, _ := strings.Cut(key, "->")
		labels = append(labels, fxPairLabel(from, to))
	}
	return strings.Join(labels, ", ")
}

// overlappingWindowError reports an override whose window overlaps others for the same scope and pair.
func overlappingWindowError(from, to string, overlaps []*fxrate.FXRate) error {
	return ierr.NewErrorf("overlapping FX rate window for %s → %s", from, to).
		WithHintf("Another %s override overlaps this period.", fxPairLabel(from, to)).
		WithReportableDetails(map[string]any{"overlapping_fx_rate_ids": lo.Map(overlaps, func(r *fxrate.FXRate, _ int) string { return r.ID })}).
		Mark(ierr.ErrValidation)
}
