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

func TestCommitmentTrueUpProbe_UnderLeg(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	reg.LoadSeeds(e2eprobe.Seeds{PlanIDs: []string{"plan_1"}})

	// commitment ($5) + base fee ($19.99) = $24.99 minimum.
	total := "24.99"
	displayName := "Commitment True-Up"
	fc.invoices.previewResp = &sdkdtos.GetInvoicePreviewResponse{
		InvoiceResponse: &sdktypes.InvoiceResponse{
			Total: &total,
			LineItems: []sdktypes.InvoiceLineItemResponse{
				{DisplayName: &displayName},
			},
		},
	}

	// cursor starts at 0; first AddInt64 → 1; 1%2==1 → under leg.
	p := NewCommitmentTrueUpProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("under-leg Run() unexpected error: %v", err)
	}
	// Under leg ingests exactly 100 events.
	if len(fc.events.ingested) != 100 {
		t.Errorf("under-leg ingested = %d, want 100", len(fc.events.ingested))
	}
}

func TestCommitmentTrueUpProbe_OverLeg(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	reg.LoadSeeds(e2eprobe.Seeds{PlanIDs: []string{"plan_1"}})

	// commitment ($5) + overage ($2 × 1.5) + base fee ($19.99) = $27.99.
	total := "27.99"
	fc.invoices.previewResp = &sdkdtos.GetInvoicePreviewResponse{
		InvoiceResponse: &sdktypes.InvoiceResponse{Total: &total},
	}

	p := NewCommitmentTrueUpProbe(fc, reg, "test-run", lg)
	p.cursor = 1 // force even leg on first Run (AddInt64 → 2)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("over-leg Run() unexpected error: %v", err)
	}
	if len(fc.events.ingested) != 700 {
		t.Errorf("over-leg ingested = %d, want 700", len(fc.events.ingested))
	}
}

func TestCommitmentTrueUpProbe_UnderLegMissingTrueUpLineFails(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	reg.LoadSeeds(e2eprobe.Seeds{PlanIDs: []string{"plan_1"}})

	// Preview has correct total ($24.99 = commitment + base) but no true-up
	// marker on any line item.
	total := "24.99"
	unrelated := "Base Fee"
	fc.invoices.previewResp = &sdkdtos.GetInvoicePreviewResponse{
		InvoiceResponse: &sdktypes.InvoiceResponse{
			Total:     &total,
			LineItems: []sdktypes.InvoiceLineItemResponse{{DisplayName: &unrelated}},
		},
	}

	p := NewCommitmentTrueUpProbe(fc, reg, "test-run", lg)
	err := p.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error when no true-up line item present, got nil")
	}
}

func TestCommitmentTrueUpProbe_OverLegWrongTotalFails(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	reg.LoadSeeds(e2eprobe.Seeds{PlanIDs: []string{"plan_1"}})

	// Total should be $8 with overage; return $7 (naive usage without 1.5× multiplier).
	total := "7.00"
	fc.invoices.previewResp = &sdkdtos.GetInvoicePreviewResponse{
		InvoiceResponse: &sdktypes.InvoiceResponse{Total: &total},
	}

	p := NewCommitmentTrueUpProbe(fc, reg, "test-run", lg)
	p.cursor = 1 // force over leg
	err := p.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error when over-leg total deviates from $8, got nil")
	}
}

func TestCommitmentTrueUpProbe_MissingSeedsSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	p := NewCommitmentTrueUpProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("empty seeds must soft-skip; got %v", err)
	}
	if len(fc.customers.created) != 0 {
		t.Errorf("no customer should be created on empty seeds")
	}
}
