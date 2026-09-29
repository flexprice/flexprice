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
	Rate         string            `json:"rate" validate:"required"`
	ValidFrom    *time.Time        `json:"valid_from,omitempty"`
	ValidTo      *time.Time        `json:"valid_to,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

func (r CreateFXRateRequest) Validate() error {
	if err := validator.ValidateRequest(&r); err != nil {
		return err
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	return nil
}

// ToFXRate parses the request into a domain FXRate. Currencies are stored lowercase.
func (r CreateFXRateRequest) ToFXRate(ctx context.Context) (*fxrate.FXRate, error) {
	rate, err := decimal.NewFromString(r.Rate)
	if err != nil {
		return nil, ierr.NewError("invalid rate").
			WithHint("Rate must be a valid decimal number").
			WithReportableDetails(map[string]any{"rate": r.Rate}).
			Mark(ierr.ErrValidation)
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
		ValidFrom:     r.ValidFrom,
		ValidTo:       r.ValidTo,
		Metadata:      r.Metadata,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}, nil
}

// UpdateFXRateRequest updates a rate. scope, scope_id and the currency pair are immutable.
type UpdateFXRateRequest struct {
	Rate      *string           `json:"rate,omitempty"`
	ValidFrom *time.Time        `json:"valid_from,omitempty"`
	ValidTo   *time.Time        `json:"valid_to,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

func (r UpdateFXRateRequest) Validate() error {
	return validator.ValidateRequest(&r)
}

type FXRateResponse struct {
	*fxrate.FXRate
}

type ListFXRatesResponse = types.ListResponse[*FXRateResponse]

// ResolveFXRateResponse is the rate a resolution produced.
type ResolveFXRateResponse struct {
	Rate         string `json:"rate"`
	RateID       string `json:"rate_id,omitempty"`
	Scope        string `json:"scope"`
	FromCurrency string `json:"from_currency"`
	ToCurrency   string `json:"to_currency"`
}
