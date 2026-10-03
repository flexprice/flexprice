package dto

import (
	"context"
	"strings"
	"time"

	fxrate "github.com/flexprice/flexprice/internal/domain/fxrate"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// CreateFXRateRequest is the request to configure an FX rate.
type CreateFXRateRequest struct {
	Scope        types.FXRateScope `json:"scope" validate:"required"`
	ScopeID      string            `json:"scope_id,omitempty"`
	FromCurrency string            `json:"from_currency" validate:"required"`
	ToCurrency   string            `json:"to_currency" validate:"required"`
	// Source defaults to "fixed". A fixed rate requires `rate`; a market rate ignores it
	// (the value comes from the market-rate integration at conversion time).
	Source    types.FXRateSource `json:"source,omitempty"`
	Rate      string             `json:"rate,omitempty"`
	StartDate *time.Time         `json:"start_date,omitempty"`
	EndDate   *time.Time         `json:"end_date,omitempty"`
	Metadata  map[string]string  `json:"metadata,omitempty"`
}

func (r *CreateFXRateRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if r.Source == "" {
		r.Source = types.FXRateSourceFixed
	}
	if err := r.Source.Validate(); err != nil {
		return err
	}
	if r.Source == types.FXRateSourceFixed && strings.TrimSpace(r.Rate) == "" {
		return ierr.NewError("rate is required for a fixed fx rate").
			WithHint("Provide a rate value, or set source to \"market\".").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ToFXRate parses the request into a domain FXRate. Currencies are stored lowercase.
func (r *CreateFXRateRequest) ToFXRate(ctx context.Context) (*fxrate.FXRate, error) {
	source := r.Source
	if source == "" {
		source = types.FXRateSourceFixed
	}

	// A market rate carries no fixed value (it is resolved from the integration), so an
	// empty rate is stored as zero rather than rejected.
	rate := decimal.Zero
	if strings.TrimSpace(r.Rate) != "" {
		parsed, err := decimal.NewFromString(r.Rate)
		if err != nil {
			return nil, ierr.NewError("invalid rate").
				WithHint("Rate must be a valid decimal number").
				WithReportableDetails(map[string]any{"rate": r.Rate}).
				Mark(ierr.ErrValidation)
		}
		rate = parsed
	}

	scopeID := r.ScopeID
	if r.Scope == types.FXRateScopeTenant {
		scopeID = types.FXRateScopeIDTenant
	}

	return &fxrate.FXRate{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FX_RATE),
		Scope:         r.Scope,
		ScopeID:       scopeID,
		FromCurrency:  strings.ToLower(r.FromCurrency),
		ToCurrency:    strings.ToLower(r.ToCurrency),
		Rate:          rate,
		Source:        source,
		StartDate:     r.StartDate,
		EndDate:       r.EndDate,
		Metadata:      r.Metadata,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}, nil
}

// UpdateFXRateRequest updates a rate. scope, scope_id and the currency pair are immutable.
type UpdateFXRateRequest struct {
	Rate      *string           `json:"rate,omitempty"`
	StartDate *time.Time        `json:"start_date,omitempty"`
	EndDate   *time.Time        `json:"end_date,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

func (r *UpdateFXRateRequest) Validate() error {
	return validator.ValidateRequest(r)
}

type FXRateResponse struct {
	*fxrate.FXRate
}

type ListFXRatesResponse = types.ListResponse[*FXRateResponse]
