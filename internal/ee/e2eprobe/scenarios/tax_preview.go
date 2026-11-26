package scenarios

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/world"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// taxPreviewEvents makes a $1.00 usage line at the plan's $0.01 a unit on the
// entitlement-free meter, so the usage itself is taxable, not just the base fee.
const taxPreviewEvents = 100

// taxPreview puts usage on a clone carrying the shared tax association and
// checks the preview taxes it at the shared rate.
type taxPreview struct {
	batch usageBatch
	poll  pollWindow
}

func newTaxPreview(scenario.Deps) *taxPreview {
	return &taxPreview{
		batch: usageBatch{meter: "sum_commit", events: taxPreviewEvents, value: "1"},
		poll:  pollWindow{every: 5 * time.Second, within: 90 * time.Second},
	}
}

func (*taxPreview) Name() string               { return "tax-preview" }
func (*taxPreview) Kind() e2eprobe.Kind        { return e2eprobe.KindFlow }
func (*taxPreview) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitTax} }

func (s *taxPreview) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	eph, err := sc.Clone(ctx)
	if err != nil {
		return sc.Fail("clone", err)
	}
	noteClone(sc, eph)
	batch, err := s.batch.build(sc, eph.ExternalID())
	if err != nil {
		return sc.Fail("batch", err)
	}
	if err := sc.Client().Events().IngestBulk(ctx, types.BulkIngestEventRequest{Events: batch}); err != nil {
		return sc.Fail("ingest_bulk", err)
	}
	if _, err := awaitCloneUsage(ctx, sc, s.poll, eph, s.batch.meter, s.batch.units()); err != nil {
		return sc.Fail("poll_usage", err)
	}
	inv, err := previewOf(ctx, sc, eph.SubscriptionID())
	if err != nil {
		return sc.Fail("preview", err)
	}
	fx := sc.World().Fixtures()
	found := false
	for _, tx := range inv.Taxes {
		if !isSharedRate(tx, fx) {
			continue
		}
		found = true
		sc.Money("tax amount", decimalOf(tx.TaxableAmount).Mul(sharedTaxRate), decimalOf(tx.TaxAmount))
	}
	sc.Truth("preview carries the shared tax rate", found)
	return sc.Result("assert_tax_preview")
}

// isSharedRate matches by id when the fixture knows it, else by code.
func isSharedRate(tx types.TaxAppliedResponse, fx *world.Fixtures) bool {
	if fx.TaxRateID() != "" && tx.TaxRateID != nil && *tx.TaxRateID == fx.TaxRateID() {
		return true
	}
	return tx.TaxRate != nil && tx.TaxRate.Code != nil && *tx.TaxRate.Code == fx.TaxRateCode()
}

// previewOf reads the subscription's upcoming invoice.
func previewOf(ctx context.Context, sc *scenario.Context, subID string) (*types.InvoiceResponse, error) {
	resp, err := sc.Client().Invoices().GetPreview(ctx, types.GetPreviewInvoiceRequest{SubscriptionID: subID})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.InvoiceResponse == nil {
		return nil, errors.New("preview returned no invoice")
	}
	return resp.InvoiceResponse, nil
}
