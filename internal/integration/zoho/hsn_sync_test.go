package zoho

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHSNPriceRepo serves prices by ID and records how many queries were issued.
type fakeHSNPriceRepo struct {
	price.Repository
	prices    map[string]*price.Price
	listCalls int
	listErr   error
}

func (f *fakeHSNPriceRepo) ListAll(_ context.Context, filter *types.PriceFilter) ([]*price.Price, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*price.Price
	for _, id := range filter.PriceIDs {
		if p, ok := f.prices[id]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// capturingItemSyncSvc records the inputs handed to item creation.
type capturingItemSyncSvc struct {
	inputs []ItemSyncInput
}

func (c *capturingItemSyncSvc) EnsureItemsMapped(_ context.Context, inputs []ItemSyncInput, _ *ItemTaxResolution) (map[string]string, error) {
	c.inputs = inputs
	out := map[string]string{}
	for _, in := range inputs {
		out[in.PriceID] = "zoho_item_" + in.PriceID
	}
	return out, nil
}

func hsnPrice(id, code, parentID string) *price.Price {
	p := &price.Price{ID: id, ParentPriceID: parentID}
	if code != "" {
		p.Metadata = price.JSONBMetadata{types.MetadataKeyHSNSAC: code}
	}
	return p
}

func hsnLineItem(id, priceID string) *invoice.InvoiceLineItem {
	return &invoice.InvoiceLineItem{
		ID:          id,
		PriceID:     lo.ToPtr(priceID),
		DisplayName: lo.ToPtr("BBNow"),
		Amount:      decimal.NewFromInt(15000),
		Quantity:    decimal.NewFromInt(1),
	}
}

func newHSNTestService(priceRepo price.Repository) (*InvoiceService, *capturingItemSyncSvc) {
	itemSvc := &capturingItemSyncSvc{}
	svc := &InvoiceService{
		client:      &fakeSyncZohoClient{},
		itemSyncSvc: itemSvc,
		taxSvc:      fakeSyncTaxSvc{},
		priceRepo:   priceRepo,
		logger:      logger.NewNoopLogger(),
	}
	return svc, itemSvc
}

func TestResolveHSNSACAcrossPrices(t *testing.T) {
	repo := &fakeHSNPriceRepo{prices: map[string]*price.Price{
		"price_own":      hsnPrice("price_own", "998415", ""),
		"price_override": hsnPrice("price_override", "", "price_plan"),
		"price_plan":     hsnPrice("price_plan", "998314", ""),
		"price_bare":     hsnPrice("price_bare", "", ""),
	}}
	svc, _ := newHSNTestService(repo)

	got := svc.resolveHSNSAC(context.Background(), []ItemSyncInput{
		{PriceID: "price_own"},
		{PriceID: "price_override"},
		{PriceID: "price_bare"},
	})

	assert.Equal(t, "998415", got["price_own"])
	assert.Equal(t, "998314", got["price_override"], "override must inherit the root plan price code")
	assert.Empty(t, got["price_bare"], "no code anywhere means send nothing, not a blanket default")
	assert.Equal(t, 2, repo.listCalls, "one query for the prices, one for the missing root price")
}

func TestResolveHSNSACSkipsRootQueryWhenUnneeded(t *testing.T) {
	repo := &fakeHSNPriceRepo{prices: map[string]*price.Price{
		"price_own": hsnPrice("price_own", "998415", ""),
	}}
	svc, _ := newHSNTestService(repo)

	svc.resolveHSNSAC(context.Background(), []ItemSyncInput{{PriceID: "price_own"}})
	assert.Equal(t, 1, repo.listCalls, "no root lookup when every price carries its own code")
}

func TestResolveHSNSACDegradesOnRepoError(t *testing.T) {
	repo := &fakeHSNPriceRepo{listErr: assert.AnError}
	svc, _ := newHSNTestService(repo)

	got := svc.resolveHSNSAC(context.Background(), []ItemSyncInput{{PriceID: "price_own"}})
	assert.Empty(t, got["price_own"], "a lookup failure must omit the code, not fail the sync")
}

func TestResolveHSNSACEmptyInputs(t *testing.T) {
	repo := &fakeHSNPriceRepo{prices: map[string]*price.Price{}}
	svc, _ := newHSNTestService(repo)

	got := svc.resolveHSNSAC(context.Background(), nil)
	assert.Empty(t, got)
	assert.Equal(t, 0, repo.listCalls, "no query for an invoice with no syncable lines")
}

func TestHSNReachesLineItemsAndItems(t *testing.T) {
	repo := &fakeHSNPriceRepo{prices: map[string]*price.Price{
		"price_a": hsnPrice("price_a", "998415", ""),
		"price_b": hsnPrice("price_b", "", ""),
	}}
	svc, itemSvc := newHSNTestService(repo)

	inv := &invoice.Invoice{
		ID: "inv_1",
		LineItems: []*invoice.InvoiceLineItem{
			hsnLineItem("li_a", "price_a"),
			hsnLineItem("li_b", "price_b"),
		},
	}

	lines, err := svc.buildLineItems(context.Background(), inv)
	require.NoError(t, err)
	require.Len(t, lines, 2)

	assert.Equal(t, "998415", lines[0].HSNOrSAC)
	assert.Empty(t, lines[1].HSNOrSAC, "an uncoded price sends no hsn_or_sac so Zoho uses the item's own")

	byPrice := map[string]string{}
	for _, in := range itemSvc.inputs {
		byPrice[in.PriceID] = in.HSNOrSAC
	}
	assert.Equal(t, "998415", byPrice["price_a"],
		"the Zoho item must be created with the code, not just the line")
	assert.Empty(t, byPrice["price_b"])
}
