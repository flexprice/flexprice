package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/types"
)

// nativeTaxEngine is Flexprice's own calculation behind the same interface as an external
// one. It runs the rate cascade and arithmetic the tax service already owns.
type nativeTaxEngine struct {
	ServiceParams
}

func (e *nativeTaxEngine) GetProvider() types.TaxProvider {
	return types.TaxProviderFlexprice
}

// Calculate returns the amount untaxed for anything that is not an invoice. Native tax is
// resolved from an invoice's rate associations; a credit note has none of its own, so a natively
// credited amount carries no tax today. An invoice never reaches here: the invoice path resolves
// its rates first and calls TaxService.CalculateTaxes with them.
func (e *nativeTaxEngine) Calculate(ctx context.Context, req dto.TaxCalculationRequest) (*dto.TaxCalculationResult, error) {
	return &dto.TaxCalculationResult{
		Provider:    types.TaxProviderFlexprice,
		AmountTotal: req.Amount,
	}, nil
}

// Commit does nothing. Native tax is filed by the tenant, not by a provider, so there is
// no transaction to record.
func (e *nativeTaxEngine) Commit(ctx context.Context, inv *invoice.Invoice, appliedTaxes []*taxapplied.TaxApplied) (string, error) {
	return "", nil
}

// Reverse does nothing. Native tax was never filed with a provider, so there is nothing to
// un-file and no transaction to record.
func (e *nativeTaxEngine) Reverse(ctx context.Context, req dto.TaxReversalRequest) (string, error) {
	return "", nil
}

// TaxIdentifiers returns nothing. Flexprice holds no registered tax numbers of its own, and a
// natively taxed invoice has none to print.
func (e *nativeTaxEngine) TaxIdentifiers(ctx context.Context, customerID string) ([]types.TaxIdentifier, []types.TaxIdentifier, error) {
	return nil, nil, nil
}
