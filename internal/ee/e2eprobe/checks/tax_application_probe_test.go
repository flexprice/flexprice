package checks

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	itypes "github.com/flexprice/flexprice/internal/types"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
)

func TestTaxApplicationProbe_HappyPath(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	reg.LoadSeeds(e2eprobe.Seeds{
		PlanIDs:           []string{"plan_1"},
		SharedTaxRateCode: "E2EPROBE_TAX_10PCT",
		SharedTaxRateID:   "taxrate_1",
	})

	// Preview response includes a matching TaxRateID + correct 10% math.
	trID := "taxrate_1"
	taxable := "1.00"
	taxAmt := "0.10"
	fc.invoices.previewResp = &sdkdtos.GetInvoicePreviewResponse{
		InvoiceResponse: &sdktypes.InvoiceResponse{
			Taxes: []sdktypes.TaxAppliedResponse{{
				TaxRateID:     &trID,
				TaxableAmount: &taxable,
				TaxAmount:     &taxAmt,
			}},
		},
	}

	p := NewTaxApplicationProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	// Tax association was created + deleted.
	if len(fc.taxAssociations.created) != 1 {
		t.Errorf("tax associations created = %d, want 1", len(fc.taxAssociations.created))
	}
	if len(fc.taxAssociations.deleted) != 1 {
		t.Errorf("tax associations deleted = %d, want 1 (best-effort cleanup)", len(fc.taxAssociations.deleted))
	}
	if len(fc.events.ingested) != 100 {
		t.Errorf("ingested = %d, want 100", len(fc.events.ingested))
	}
}

func TestTaxApplicationProbe_MissingTaxRateSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	// Only PlanIDs set — SharedTaxRateCode empty → soft skip.
	reg.LoadSeeds(e2eprobe.Seeds{PlanIDs: []string{"plan_1"}})

	p := NewTaxApplicationProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("missing SharedTaxRateCode must soft-skip; got %v", err)
	}
	if len(fc.customers.created) != 0 {
		t.Errorf("no customer should be created when tax rate seed absent")
	}
}

func TestTaxApplicationProbe_EmptyTaxesFails(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	reg.LoadSeeds(e2eprobe.Seeds{
		PlanIDs:           []string{"plan_1"},
		SharedTaxRateCode: "E2EPROBE_TAX_10PCT",
		SharedTaxRateID:   "taxrate_1",
	})

	// Preview has empty Taxes — probe must fail.
	fc.invoices.previewResp = &sdkdtos.GetInvoicePreviewResponse{
		InvoiceResponse: &sdktypes.InvoiceResponse{},
	}

	p := NewTaxApplicationProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err == nil {
		t.Fatalf("expected error when preview.Taxes empty, got nil")
	}
}

func TestTaxApplicationProbe_WrongTaxMathFails(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	reg.LoadSeeds(e2eprobe.Seeds{
		PlanIDs:           []string{"plan_1"},
		SharedTaxRateCode: "E2EPROBE_TAX_10PCT",
		SharedTaxRateID:   "taxrate_1",
	})

	// Preview has 20% tax instead of 10% — off by $0.10.
	trID := "taxrate_1"
	taxable := "1.00"
	taxAmt := "0.20"
	fc.invoices.previewResp = &sdkdtos.GetInvoicePreviewResponse{
		InvoiceResponse: &sdktypes.InvoiceResponse{
			Taxes: []sdktypes.TaxAppliedResponse{{
				TaxRateID:     &trID,
				TaxableAmount: &taxable,
				TaxAmount:     &taxAmt,
			}},
		},
	}

	p := NewTaxApplicationProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err == nil {
		t.Fatalf("expected error when tax_amount deviates from 10%%, got nil")
	}
}
