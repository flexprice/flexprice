package zoho

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

const FLEXPRICE_STANDARD_EXEMPTION = "FLEXPRICE_STANDARD_EXEMPTION"

// ItemTaxResolution holds the resolved tax fields to attach to a Zoho item.
// When IsTaxable is true, TaxID is set. When false, TaxExemptionID is set.
type ItemTaxResolution struct {
	TaxID          string
	TaxExemptionID string
	IsTaxable      bool
}

// ZohoTaxService handles tax and tax-exemption operations against Zoho Books.
type ZohoTaxService interface {
	// ResolveItemTax determines which tax or exemption to attach when creating a Zoho item.
	// It prefers the default tax; if none exists it falls back to (or creates) FLEXPRICE_STANDARD_EXEMPTION.
	ResolveItemTax(ctx context.Context) (*ItemTaxResolution, error)
	ListTaxes(ctx context.Context, page, perPage int) (*ListTaxesResponse, error)
	CreateTaxExemption(ctx context.Context, req *CreateTaxExemptionRequest) (*TaxExemption, error)
	ListTaxExemptions(ctx context.Context) ([]TaxExemption, error)
}

type TaxService struct {
	client ZohoClient
	logger *logger.Logger
}

func NewTaxService(client ZohoClient, logger *logger.Logger) ZohoTaxService {
	return &TaxService{client: client, logger: logger}
}

// ResolveItemTax implements the fallback chain:
//  1. List taxes → return the first default tax.
//  2. No default tax → list exemptions → return FLEXPRICE_STANDARD_EXEMPTION if present.
//  3. Exemption missing → create it, then return it.
func (s *TaxService) ResolveItemTax(ctx context.Context) (*ItemTaxResolution, error) {
	for page := 1; ; page++ {
		taxesResp, err := s.client.ListTaxes(ctx, page, 200)
		if err != nil {
			return nil, ierr.WithError(err).WithHint("failed to list Zoho taxes").Mark(ierr.ErrInternal)
		}
		for _, t := range taxesResp.Taxes {
			if t.IsDefaultTax {
				s.logger.Debugw("resolved default tax for item", "tax_id", t.TaxID, "tax_name", t.TaxName)
				return &ItemTaxResolution{TaxID: t.TaxID, IsTaxable: true}, nil
			}
		}
		if !taxesResp.PageContext.HasMorePage {
			break
		}
	}

	exemptions, err := s.client.ListTaxExemptions(ctx)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("failed to list Zoho tax exemptions").Mark(ierr.ErrInternal)
	}
	for _, e := range exemptions {
		if e.TaxExemptionCode == FLEXPRICE_STANDARD_EXEMPTION {
			s.logger.Debugw("using existing tax exemption for item", "tax_exemption_id", e.TaxExemptionID)
			return &ItemTaxResolution{TaxExemptionID: e.TaxExemptionID, IsTaxable: false}, nil
		}
	}

	s.logger.Infow("creating FLEXPRICE_STANDARD_EXEMPTION in Zoho Books")
	created, err := s.client.CreateTaxExemption(ctx, &CreateTaxExemptionRequest{
		TaxExemptionCode: FLEXPRICE_STANDARD_EXEMPTION,
		Description:      "Standard tax exemption for FlexPrice items",
		Type:             "item",
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to create FLEXPRICE_STANDARD_EXEMPTION in Zoho Books").
			Mark(ierr.ErrInternal)
	}
	s.logger.Infow("created FLEXPRICE_STANDARD_EXEMPTION", "tax_exemption_id", created.TaxExemptionID)
	return &ItemTaxResolution{TaxExemptionID: created.TaxExemptionID, IsTaxable: false}, nil
}

func (s *TaxService) ListTaxes(ctx context.Context, page, perPage int) (*ListTaxesResponse, error) {
	return s.client.ListTaxes(ctx, page, perPage)
}

func (s *TaxService) CreateTaxExemption(ctx context.Context, req *CreateTaxExemptionRequest) (*TaxExemption, error) {
	return s.client.CreateTaxExemption(ctx, req)
}

func (s *TaxService) ListTaxExemptions(ctx context.Context) ([]TaxExemption, error) {
	return s.client.ListTaxExemptions(ctx)
}
