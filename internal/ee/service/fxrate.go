package service

import (
	"context"
	"time"

	fxrate "github.com/flexprice/flexprice/internal/domain/fxrate"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// FXRateService configures and resolves tenant FX rates.
type FXRateService interface {
	// ResolveRate returns the rate for from→to using the most specific scope
	// (subscription → customer → tenant) whose validity window covers now.
	ResolveRate(ctx context.Context, req ResolveFXRateRequest) (*FXRateResolution, error)
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
