package zoho

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cnFakeMappingRepo struct {
	entityintegrationmapping.Repository
	mappings   []*entityintegrationmapping.EntityIntegrationMapping
	listCalls  []*types.EntityIntegrationMappingFilter
	createErr  error
	createdIDs []string
}

func (f *cnFakeMappingRepo) List(_ context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	f.listCalls = append(f.listCalls, filter)
	var out []*entityintegrationmapping.EntityIntegrationMapping
	for _, m := range f.mappings {
		if m.EntityType == filter.EntityType && (m.EntityID == filter.EntityID || lo.Contains(filter.EntityIDs, m.EntityID)) {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *cnFakeMappingRepo) Create(_ context.Context, m *entityintegrationmapping.EntityIntegrationMapping) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.mappings = append(f.mappings, m)
	f.createdIDs = append(f.createdIDs, m.EntityID)
	return nil
}

func (f *cnFakeMappingRepo) creditNoteMappings() []*entityintegrationmapping.EntityIntegrationMapping {
	return lo.Filter(f.mappings, func(m *entityintegrationmapping.EntityIntegrationMapping, _ int) bool {
		return m.EntityType == types.IntegrationEntityTypeCreditNote
	})
}

type cnFakeInvoiceRepo struct {
	invoice.Repository
	inv *invoice.Invoice
}

func (f *cnFakeInvoiceRepo) Get(_ context.Context, _ string) (*invoice.Invoice, error) {
	return f.inv, nil
}

type cnFakeZohoClient struct {
	ZohoClient
	zohoInvoice     *InvoiceResponse
	syncConfig      *types.SyncConfig
	creditNote      CreditNoteResponse
	createErr       error
	refundErr       error
	applyErr        error
	createReq       *CreditNoteCreateRequest
	createInvoiceID string
	createCalls     int
	refundCNIDs     []string
	refundReqs      []*CreditNoteRefundRequest
	applyCNIDs      []string
	applyReqs       []*CreditNoteApplyRequest
}

func (f *cnFakeZohoClient) GetConnection(_ context.Context) (*connection.Connection, error) {
	return &connection.Connection{ID: "conn_test", ProviderType: types.SecretProviderZohoBooks}, nil
}

func (f *cnFakeZohoClient) GetZohoBooksSyncConfig(_ context.Context) (*types.SyncConfig, error) {
	return f.syncConfig, nil
}

func (f *cnFakeZohoClient) GetInvoice(_ context.Context, _ string) (*InvoiceResponse, error) {
	return f.zohoInvoice, nil
}

func (f *cnFakeZohoClient) CreateCreditNote(_ context.Context, zohoInvoiceID string, req *CreditNoteCreateRequest) (*CreditNoteResponse, error) {
	f.createCalls++
	f.createReq = req
	f.createInvoiceID = zohoInvoiceID
	if f.createErr != nil {
		return nil, f.createErr
	}
	resp := f.creditNote
	return &resp, nil
}

func (f *cnFakeZohoClient) RefundCreditNote(_ context.Context, zohoCreditNoteID string, req *CreditNoteRefundRequest) (*CreditNoteRefundResponse, error) {
	f.refundCNIDs = append(f.refundCNIDs, zohoCreditNoteID)
	f.refundReqs = append(f.refundReqs, req)
	if f.refundErr != nil {
		return nil, f.refundErr
	}
	return &CreditNoteRefundResponse{CreditNoteRefundID: "zoho_refund_1"}, nil
}

func (f *cnFakeZohoClient) ApplyCreditNoteToInvoices(_ context.Context, zohoCreditNoteID string, req *CreditNoteApplyRequest) error {
	f.applyCNIDs = append(f.applyCNIDs, zohoCreditNoteID)
	f.applyReqs = append(f.applyReqs, req)
	return f.applyErr
}

// cnFixture is a ₹200 invoice synced to Zoho with two ₹100 priced lines, and a ₹100 credit
// note on its second line. Zoho returns a credit note with tax, so its total and balance are ₹118.
type cnFixture struct {
	client      *cnFakeZohoClient
	mappingRepo *cnFakeMappingRepo
	invoice     *invoice.Invoice
	svc         ZohoCreditNoteService
	creditNote  *creditnote.CreditNote
}

func newCreditNoteFixture(cnType types.CreditNoteType) *cnFixture {
	finalizedAt := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	flexInvoice := &invoice.Invoice{
		ID: "inv_1",
		LineItems: []*invoice.InvoiceLineItem{
			{ID: "li_1", PriceID: lo.ToPtr("price_1"), Amount: decimal.NewFromInt(100)},
			{ID: "li_2", PriceID: lo.ToPtr("price_2"), Amount: decimal.NewFromInt(100)},
		},
	}
	client := &cnFakeZohoClient{
		zohoInvoice: &InvoiceResponse{InvoiceID: "zinv_1", CustomerID: "zcust_1"},
		syncConfig: &types.SyncConfig{InvoiceSyncSettings: &types.InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{DepositToAccountID: "460000000000358"},
		}},
		creditNote: CreditNoteResponse{
			CreditNoteID:     "zcn_1",
			CreditNoteNumber: "CN-00001",
			Status:           "open",
			Total:            decimal.NewFromInt(118),
			Balance:          decimal.NewFromInt(118),
		},
	}
	mappingRepo := &cnFakeMappingRepo{mappings: []*entityintegrationmapping.EntityIntegrationMapping{
		{EntityType: types.IntegrationEntityTypeInvoice, EntityID: "inv_1", ProviderEntityID: "zinv_1"},
		{EntityType: types.IntegrationEntityTypePrice, EntityID: "price_1", ProviderEntityID: "zitem_1"},
		{EntityType: types.IntegrationEntityTypePrice, EntityID: "price_2", ProviderEntityID: "zitem_2"},
	}}
	return &cnFixture{
		client:      client,
		mappingRepo: mappingRepo,
		invoice:     flexInvoice,
		svc:         NewCreditNoteService(client, &cnFakeInvoiceRepo{inv: flexInvoice}, mappingRepo, logger.NewNoopLogger()),
		creditNote: &creditnote.CreditNote{
			ID:               "cn_1",
			InvoiceID:        "inv_1",
			CreditNoteType:   cnType,
			CreditNoteStatus: types.CreditNoteStatusFinalized,
			FinalizedAt:      &finalizedAt,
			TotalAmount:      decimal.NewFromInt(100),
			LineItems: []*creditnote.CreditNoteLineItem{
				{ID: "cnli_1", InvoiceLineItemID: "li_2", DisplayName: "Seats", Amount: decimal.NewFromInt(100)},
			},
		},
	}
}

func (f *cnFixture) sync(refunds ...*refund.Refund) error {
	return f.svc.SyncCreditNoteToZoho(context.Background(), ZohoCreditNoteSyncRequest{CreditNote: f.creditNote, Refunds: refunds})
}

func succeededRefund(id string, destination types.RefundDestination, gateway *string) *refund.Refund {
	succeededAt := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	return &refund.Refund{
		ID:                id,
		CreditNoteID:      lo.ToPtr("cn_1"),
		RefundStatus:      types.RefundStatusSucceeded,
		RefundDestination: destination,
		PaymentGateway:    gateway,
		Amount:            decimal.NewFromInt(100),
		SettledAmount:     decimal.NewFromInt(100),
		SucceededAt:       &succeededAt,
	}
}

func TestSyncCreditNoteToZoho_SkippedBeforeAnyZohoCall(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(f *cnFixture)
	}{
		{"nil credit note", func(f *cnFixture) { f.creditNote = nil }},
		{"draft credit note", func(f *cnFixture) { f.creditNote.CreditNoteStatus = types.CreditNoteStatusDraft }},
		{"voided credit note", func(f *cnFixture) { f.creditNote.CreditNoteStatus = types.CreditNoteStatusVoided }},
		{"invoice not synced to Zoho", func(f *cnFixture) {
			f.mappingRepo.mappings = lo.Reject(f.mappingRepo.mappings, func(m *entityintegrationmapping.EntityIntegrationMapping, _ int) bool {
				return m.EntityType == types.IntegrationEntityTypeInvoice
			})
		}},
		{"credit note already synced to Zoho", func(f *cnFixture) {
			f.mappingRepo.mappings = append(f.mappingRepo.mappings, &entityintegrationmapping.EntityIntegrationMapping{
				EntityType: types.IntegrationEntityTypeCreditNote, EntityID: "cn_1", ProviderEntityID: "zcn_1",
			})
		}},
	}

	for _, cnType := range []types.CreditNoteType{types.CreditNoteTypeRefund, types.CreditNoteTypeAdjustment} {
		for _, tt := range tests {
			t.Run(string(cnType)+"/"+tt.name, func(t *testing.T) {
				f := newCreditNoteFixture(cnType)
				tt.mutate(f)

				require.NoError(t, f.sync(succeededRefund("ref_1", types.RefundDestinationWallet, nil)))

				assert.Equal(t, 0, f.client.createCalls)
				assert.Empty(t, f.client.refundReqs)
				assert.Empty(t, f.client.applyReqs)
				assert.Empty(t, f.mappingRepo.createdIDs)
			})
		}
	}
}

func TestSyncCreditNoteToZoho_CreatesOpenCreditNoteAndSavesMapping(t *testing.T) {
	f := newCreditNoteFixture(types.CreditNoteTypeAdjustment)

	require.NoError(t, f.sync())

	require.Equal(t, 1, f.client.createCalls)
	assert.Equal(t, "zinv_1", f.client.createInvoiceID)
	req := f.client.createReq
	assert.Equal(t, "zcust_1", req.CustomerID)
	assert.Equal(t, "2026-11-17", req.Date)
	assert.False(t, req.IsDraft)
	assert.Equal(t, "cn_1", req.ReferenceNumber)
	require.Len(t, req.LineItems, 1)
	line := req.LineItems[0]
	assert.Equal(t, "zitem_2", line.ItemID)
	assert.Equal(t, "zinv_1", line.InvoiceID)
	assert.Equal(t, "Seats", line.Name)
	assert.True(t, line.Rate.Equal(decimal.NewFromInt(100)))
	assert.True(t, line.Quantity.Equal(decimal.NewFromInt(1)))

	mappings := f.mappingRepo.creditNoteMappings()
	require.Len(t, mappings, 1)
	m := mappings[0]
	assert.Equal(t, "cn_1", m.EntityID)
	assert.Equal(t, "zcn_1", m.ProviderEntityID)
	assert.Equal(t, string(types.SecretProviderZohoBooks), m.ProviderType)
	assert.Equal(t, "conn_test", lo.FromPtr(m.ConnectionID))
	assert.Equal(t, "open", m.Metadata["zoho_status"])
	assert.Equal(t, "CN-00001", m.Metadata["zoho_creditnote_number"])
	assert.Equal(t, "zinv_1", m.Metadata["zoho_invoice_id"])
	assert.NotEmpty(t, m.Metadata["synced_at"])
}

func TestSyncCreditNoteToZoho_DateFallsBackToNowWithoutFinalizedAt(t *testing.T) {
	f := newCreditNoteFixture(types.CreditNoteTypeAdjustment)
	f.creditNote.FinalizedAt = nil

	before := clock.Now().Format(zohoDateLayout)
	require.NoError(t, f.sync())
	after := clock.Now().Format(zohoDateLayout)

	assert.Contains(t, []string{before, after}, f.client.createReq.Date)
}

func TestSyncCreditNoteToZoho_LineItems(t *testing.T) {
	t.Run("each line credits the Zoho item of its invoice line's price", func(t *testing.T) {
		f := newCreditNoteFixture(types.CreditNoteTypeAdjustment)
		f.creditNote.LineItems = []*creditnote.CreditNoteLineItem{
			{ID: "cnli_1", InvoiceLineItemID: "li_1", DisplayName: "Platform", Amount: decimal.NewFromInt(40)},
			{ID: "cnli_2", InvoiceLineItemID: "li_2", DisplayName: "Seats", Amount: decimal.NewFromInt(60)},
		}

		require.NoError(t, f.sync())

		require.Len(t, f.client.createReq.LineItems, 2)
		assert.Equal(t, "zitem_1", f.client.createReq.LineItems[0].ItemID)
		assert.Equal(t, "Platform", f.client.createReq.LineItems[0].Name)
		assert.True(t, f.client.createReq.LineItems[0].Rate.Equal(decimal.NewFromInt(40)))
		assert.Equal(t, "zitem_2", f.client.createReq.LineItems[1].ItemID)
		assert.True(t, f.client.createReq.LineItems[1].Rate.Equal(decimal.NewFromInt(60)))
	})

	t.Run("lines sharing a price are looked up once and both sent", func(t *testing.T) {
		f := newCreditNoteFixture(types.CreditNoteTypeAdjustment)
		f.invoice.LineItems = append(f.invoice.LineItems, &invoice.InvoiceLineItem{ID: "li_3", PriceID: lo.ToPtr("price_2"), Amount: decimal.NewFromInt(50)})
		f.creditNote.LineItems = []*creditnote.CreditNoteLineItem{
			{ID: "cnli_1", InvoiceLineItemID: "li_2", DisplayName: "Seats", Amount: decimal.NewFromInt(100)},
			{ID: "cnli_2", InvoiceLineItemID: "li_3", DisplayName: "Seats (prorated)", Amount: decimal.NewFromInt(50)},
		}

		require.NoError(t, f.sync())

		priceLookups := lo.Filter(f.mappingRepo.listCalls, func(filter *types.EntityIntegrationMappingFilter, _ int) bool {
			return filter.EntityType == types.IntegrationEntityTypePrice
		})
		require.Len(t, priceLookups, 1)
		assert.Equal(t, []string{"price_2"}, priceLookups[0].EntityIDs)
		assert.Equal(t, "conn_test", priceLookups[0].ConnectionID)

		require.Len(t, f.client.createReq.LineItems, 2)
		assert.Equal(t, "zitem_2", f.client.createReq.LineItems[0].ItemID)
		assert.Equal(t, "zitem_2", f.client.createReq.LineItems[1].ItemID)
	})

	t.Run("Zoho item id is trimmed", func(t *testing.T) {
		f := newCreditNoteFixture(types.CreditNoteTypeAdjustment)
		f.mappingRepo.mappings[2].ProviderEntityID = " zitem_2 "

		require.NoError(t, f.sync())

		assert.Equal(t, "zitem_2", f.client.createReq.LineItems[0].ItemID)
	})
}

func TestSyncCreditNoteToZoho_InvalidLineItemsFailBeforeCreate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(f *cnFixture)
	}{
		{"credit note line points to a missing invoice line", func(f *cnFixture) {
			f.creditNote.LineItems[0].InvoiceLineItemID = "li_missing"
		}},
		{"invoice line has no price", func(f *cnFixture) { f.invoice.LineItems[1].PriceID = nil }},
		{"invoice line has an empty price", func(f *cnFixture) { f.invoice.LineItems[1].PriceID = lo.ToPtr("") }},
		{"price is not synced to Zoho", func(f *cnFixture) {
			f.mappingRepo.mappings = lo.Reject(f.mappingRepo.mappings, func(m *entityintegrationmapping.EntityIntegrationMapping, _ int) bool {
				return m.EntityID == "price_2"
			})
		}},
		{"price mapping has a blank Zoho item", func(f *cnFixture) { f.mappingRepo.mappings[2].ProviderEntityID = "  " }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCreditNoteFixture(types.CreditNoteTypeAdjustment)
			tt.mutate(f)

			err := f.sync()

			require.Error(t, err)
			assert.True(t, ierr.IsValidation(err), "want validation error, got %v", err)
			assert.Equal(t, 0, f.client.createCalls)
			assert.Empty(t, f.mappingRepo.createdIDs)
		})
	}
}

func TestSyncCreditNoteToZoho_ZohoInvoiceNotFound(t *testing.T) {
	f := newCreditNoteFixture(types.CreditNoteTypeAdjustment)
	f.client.zohoInvoice = nil

	err := f.sync()

	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
	assert.Equal(t, 0, f.client.createCalls)
}

func TestSyncCreditNoteToZoho_CreateFailsSavesNoMapping(t *testing.T) {
	f := newCreditNoteFixture(types.CreditNoteTypeRefund)
	f.client.createErr = errors.New("zoho down")

	err := f.sync(succeededRefund("ref_1", types.RefundDestinationWallet, nil))

	require.ErrorIs(t, err, f.client.createErr)
	assert.Empty(t, f.mappingRepo.creditNoteMappings())
	assert.Empty(t, f.client.refundReqs)
}

func TestSyncCreditNoteToZoho_MappingSaveFails(t *testing.T) {
	f := newCreditNoteFixture(types.CreditNoteTypeAdjustment)
	f.mappingRepo.createErr = errors.New("db down")

	err := f.sync()

	require.ErrorIs(t, err, f.mappingRepo.createErr)
	assert.Empty(t, f.client.applyReqs)
}

func TestSyncCreditNoteToZoho_AdjustmentAppliesBalanceToInvoice(t *testing.T) {
	f := newCreditNoteFixture(types.CreditNoteTypeAdjustment)
	// Total and balance differ so the test shows the balance is what gets applied.
	f.client.creditNote.Total = decimal.NewFromInt(118)
	f.client.creditNote.Balance = decimal.NewFromInt(90)

	require.NoError(t, f.sync(succeededRefund("ref_1", types.RefundDestinationWallet, nil)))

	require.Len(t, f.client.applyReqs, 1)
	assert.Equal(t, []string{"zcn_1"}, f.client.applyCNIDs)
	require.Len(t, f.client.applyReqs[0].Invoices, 1)
	assert.Equal(t, "zinv_1", f.client.applyReqs[0].Invoices[0].InvoiceID)
	assert.True(t, f.client.applyReqs[0].Invoices[0].AmountApplied.Equal(decimal.NewFromInt(90)))
	assert.Empty(t, f.client.refundReqs, "an adjustment credit note never records a refund")
}

func TestSyncCreditNoteToZoho_AdjustmentWithoutBalanceSkipsApply(t *testing.T) {
	for _, balance := range []decimal.Decimal{decimal.Zero, decimal.NewFromInt(-1)} {
		t.Run(balance.String(), func(t *testing.T) {
			f := newCreditNoteFixture(types.CreditNoteTypeAdjustment)
			f.client.creditNote.Balance = balance

			require.NoError(t, f.sync())

			assert.Equal(t, 1, f.client.createCalls)
			assert.Empty(t, f.client.applyReqs)
		})
	}
}

func TestSyncCreditNoteToZoho_ApplyFailsKeepsMapping(t *testing.T) {
	f := newCreditNoteFixture(types.CreditNoteTypeAdjustment)
	f.client.applyErr = errors.New("zoho rejected")

	require.ErrorIs(t, f.sync(), f.client.applyErr)
	require.Len(t, f.mappingRepo.creditNoteMappings(), 1)

	// A retry finds the mapping and skips; the apply is then recorded by hand.
	f.client.applyErr = nil
	require.NoError(t, f.sync())
	assert.Equal(t, 1, f.client.createCalls)
	assert.Len(t, f.client.applyReqs, 1)
}

func TestSyncCreditNoteToZoho_RefundRecordsEachSucceededRefund(t *testing.T) {
	tests := []struct {
		name     string
		refund   *refund.Refund
		wantMode string
	}{
		{"wallet refund", succeededRefund("ref_wallet", types.RefundDestinationWallet, nil), "prepaid_wallet"},
		{"razorpay refund", succeededRefund("ref_razorpay", types.RefundDestinationGateway, lo.ToPtr("razorpay")), "razorpay"},
		{"stripe refund", succeededRefund("ref_stripe", types.RefundDestinationGateway, lo.ToPtr("stripe")), "stripe"},
		{"gateway refund without a gateway name", succeededRefund("ref_gateway", types.RefundDestinationGateway, nil), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCreditNoteFixture(types.CreditNoteTypeRefund)

			require.NoError(t, f.sync(tt.refund))

			require.Equal(t, 1, f.client.createCalls)
			require.Len(t, f.client.refundReqs, 1)
			assert.Equal(t, []string{"zcn_1"}, f.client.refundCNIDs)
			req := f.client.refundReqs[0]
			assert.Equal(t, tt.wantMode, req.RefundMode)
			assert.Equal(t, tt.refund.ID, req.ReferenceNumber)
			assert.Equal(t, "460000000000358", req.FromAccountID)
			assert.Equal(t, "2026-11-18", req.Date)
			assert.True(t, req.Amount.Equal(decimal.NewFromInt(100)))
			assert.Empty(t, f.client.applyReqs, "a refund credit note is never applied to the invoice")
		})
	}
}

func TestSyncCreditNoteToZoho_RefundSendsSettledAmount(t *testing.T) {
	f := newCreditNoteFixture(types.CreditNoteTypeRefund)
	r := succeededRefund("ref_1", types.RefundDestinationGateway, lo.ToPtr("razorpay"))
	r.Amount = decimal.NewFromInt(100)
	r.SettledAmount = decimal.NewFromFloat(99.5)

	require.NoError(t, f.sync(r))

	require.Len(t, f.client.refundReqs, 1)
	assert.True(t, f.client.refundReqs[0].Amount.Equal(decimal.NewFromFloat(99.5)))
}

func TestSyncCreditNoteToZoho_RefundDateFallsBackToNowWithoutSucceededAt(t *testing.T) {
	f := newCreditNoteFixture(types.CreditNoteTypeRefund)
	r := succeededRefund("ref_1", types.RefundDestinationWallet, nil)
	r.SucceededAt = nil

	before := clock.Now().Format(zohoDateLayout)
	require.NoError(t, f.sync(r))
	after := clock.Now().Format(zohoDateLayout)

	require.Len(t, f.client.refundReqs, 1)
	assert.Contains(t, []string{before, after}, f.client.refundReqs[0].Date)
}

func TestSyncCreditNoteToZoho_RefundRecordsOnlySucceededRowsOfThisCreditNote(t *testing.T) {
	f := newCreditNoteFixture(types.CreditNoteTypeRefund)

	failedGateway := succeededRefund("ref_failed", types.RefundDestinationGateway, lo.ToPtr("razorpay"))
	failedGateway.RefundStatus = types.RefundStatusFailed
	pending := succeededRefund("ref_pending", types.RefundDestinationGateway, lo.ToPtr("razorpay"))
	pending.RefundStatus = types.RefundStatusPending
	otherCreditNote := succeededRefund("ref_other", types.RefundDestinationWallet, nil)
	otherCreditNote.CreditNoteID = lo.ToPtr("cn_other")
	noCreditNote := succeededRefund("ref_none", types.RefundDestinationWallet, nil)
	noCreditNote.CreditNoteID = nil

	// A failed gateway refund falls back to a wallet refund, so both rows exist and only the
	// wallet one is recorded.
	require.NoError(t, f.sync(
		failedGateway,
		succeededRefund("ref_wallet_fallback", types.RefundDestinationWallet, nil),
		pending,
		nil,
		otherCreditNote,
		noCreditNote,
		succeededRefund("ref_gateway", types.RefundDestinationGateway, lo.ToPtr("razorpay")),
	))

	refs := lo.Map(f.client.refundReqs, func(r *CreditNoteRefundRequest, _ int) string { return r.ReferenceNumber })
	assert.Equal(t, []string{"ref_wallet_fallback", "ref_gateway"}, refs)
	assert.Equal(t, []string{"prepaid_wallet", "razorpay"}, lo.Map(f.client.refundReqs, func(r *CreditNoteRefundRequest, _ int) string { return r.RefundMode }))
}

func TestSyncCreditNoteToZoho_RefundWithNoSucceededRowsOnlyCreatesCreditNote(t *testing.T) {
	f := newCreditNoteFixture(types.CreditNoteTypeRefund)
	failed := succeededRefund("ref_failed", types.RefundDestinationWallet, nil)
	failed.RefundStatus = types.RefundStatusFailed

	require.NoError(t, f.sync(failed))

	assert.Equal(t, 1, f.client.createCalls)
	assert.Empty(t, f.client.refundReqs)
	assert.Empty(t, f.client.applyReqs)
}

func TestSyncCreditNoteToZoho_RefundFromAccountFollowsDepositTo(t *testing.T) {
	tests := []struct {
		name        string
		syncConfig  *types.SyncConfig
		wantAccount string
	}{
		{"deposit to account set", &types.SyncConfig{InvoiceSyncSettings: &types.InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{DepositToAccountID: " 460000000000358 "},
		}}, "460000000000358"},
		{"deposit to account empty", &types.SyncConfig{InvoiceSyncSettings: &types.InvoiceSyncSettings{}}, ""},
		{"no invoice sync settings", &types.SyncConfig{}, ""},
		{"no sync config", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCreditNoteFixture(types.CreditNoteTypeRefund)
			f.client.syncConfig = tt.syncConfig

			require.NoError(t, f.sync(succeededRefund("ref_1", types.RefundDestinationWallet, nil)))

			require.Len(t, f.client.refundReqs, 1)
			assert.Equal(t, tt.wantAccount, f.client.refundReqs[0].FromAccountID)
		})
	}
}

func TestSyncCreditNoteToZoho_RefundFailureStopsAndRetrySkips(t *testing.T) {
	f := newCreditNoteFixture(types.CreditNoteTypeRefund)
	f.client.refundErr = errors.New("zoho rejected")
	refunds := []*refund.Refund{
		succeededRefund("ref_1", types.RefundDestinationWallet, nil),
		succeededRefund("ref_2", types.RefundDestinationGateway, lo.ToPtr("razorpay")),
	}

	require.ErrorIs(t, f.sync(refunds...), f.client.refundErr)
	assert.Len(t, f.client.refundReqs, 1, "the first failure stops the remaining refunds")
	require.Len(t, f.mappingRepo.creditNoteMappings(), 1, "the created credit note stays mapped")

	// A retry finds the mapping and skips; the missing refunds are recorded by hand.
	f.client.refundErr = nil
	require.NoError(t, f.sync(refunds...))
	assert.Equal(t, 1, f.client.createCalls)
	assert.Len(t, f.client.refundReqs, 1)
}

type creditNoteRT struct {
	method   string
	url      string
	body     map[string]any
	response string
}

func (c *creditNoteRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.method = r.Method
	c.url = r.URL.String()
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &c.body)
	}
	return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(c.response)), Header: http.Header{}}, nil
}

func newCreditNoteClient(response string) (*Client, *creditNoteRT) {
	rt := &creditNoteRT{response: response}
	c, _ := newIDClient("https://www.zohoapis.in", "60001")
	c.httpClient = &http.Client{Transport: rt}
	return c, rt
}

func TestClientCreateCreditNote(t *testing.T) {
	c, rt := newCreditNoteClient(`{"code":0,"creditnote":{"creditnote_id":"zcn_1","creditnote_number":"CN-00001","status":"open","total":118,"balance":90}}`)

	resp, err := c.CreateCreditNote(context.Background(), "zinv_1", &CreditNoteCreateRequest{
		CustomerID:      "zcust_1",
		Date:            "2026-11-17",
		ReferenceNumber: "cn_1",
		LineItems: []CreditNoteLineItem{
			{ItemID: "zitem_1", InvoiceID: "zinv_1", Name: "Seats", Rate: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, rt.method)
	assert.True(t, strings.HasPrefix(rt.url, "https://www.zohoapis.in/books/v3/creditnotes?"), rt.url)
	assert.Contains(t, rt.url, "invoice_id=zinv_1")
	assert.Contains(t, rt.url, "organization_id=60001")

	assert.Equal(t, "zcust_1", rt.body["customer_id"])
	assert.Equal(t, "2026-11-17", rt.body["date"])
	assert.Equal(t, false, rt.body["is_draft"])
	assert.Equal(t, "cn_1", rt.body["reference_number"])
	lines, ok := rt.body["line_items"].([]any)
	require.True(t, ok)
	require.Len(t, lines, 1)
	line, ok := lines[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "zitem_1", line["item_id"])
	assert.Equal(t, "zinv_1", line["invoice_id"])
	assert.NotContains(t, line, "invoice_item_id")

	assert.Equal(t, "zcn_1", resp.CreditNoteID)
	assert.Equal(t, "CN-00001", resp.CreditNoteNumber)
	assert.Equal(t, "open", resp.Status)
	assert.True(t, resp.Total.Equal(decimal.NewFromInt(118)))
	assert.True(t, resp.Balance.Equal(decimal.NewFromInt(90)))
}

func TestClientRefundCreditNote(t *testing.T) {
	tests := []struct {
		name        string
		fromAccount string
		wantAccount bool
	}{
		{"from account sent when set", "460000000000358", true},
		{"from account omitted when empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rt := newCreditNoteClient(`{"code":0,"creditnote_refund":{"creditnote_refund_id":"zref_1"}}`)

			resp, err := c.RefundCreditNote(context.Background(), "zcn_1", &CreditNoteRefundRequest{
				Date:            "2026-11-18",
				RefundMode:      "prepaid_wallet",
				Amount:          decimal.NewFromInt(100),
				FromAccountID:   tt.fromAccount,
				ReferenceNumber: "ref_1",
			})
			require.NoError(t, err)

			assert.Equal(t, http.MethodPost, rt.method)
			assert.True(t, strings.HasPrefix(rt.url, "https://www.zohoapis.in/books/v3/creditnotes/zcn_1/refunds?"), rt.url)
			assert.Equal(t, "2026-11-18", rt.body["date"])
			assert.Equal(t, "prepaid_wallet", rt.body["refund_mode"])
			assert.Equal(t, "ref_1", rt.body["reference_number"])
			assert.NotContains(t, rt.body, "payment_form")
			if tt.wantAccount {
				assert.Equal(t, tt.fromAccount, rt.body["from_account_id"])
			} else {
				assert.NotContains(t, rt.body, "from_account_id")
			}
			assert.Equal(t, "zref_1", resp.CreditNoteRefundID)
		})
	}
}

func TestClientApplyCreditNoteToInvoices(t *testing.T) {
	c, rt := newCreditNoteClient(`{"code":0,"message":"Credits have been applied to the invoice(s)."}`)

	err := c.ApplyCreditNoteToInvoices(context.Background(), "zcn_1", &CreditNoteApplyRequest{
		Invoices: []CreditNoteInvoiceApply{{InvoiceID: "zinv_1", AmountApplied: decimal.NewFromInt(90)}},
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, rt.method)
	assert.True(t, strings.HasPrefix(rt.url, "https://www.zohoapis.in/books/v3/creditnotes/zcn_1/invoices?"), rt.url)
	assert.NotContains(t, rt.body, "from_account_id")
	invoices, ok := rt.body["invoices"].([]any)
	require.True(t, ok)
	require.Len(t, invoices, 1)
	applied, ok := invoices[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "zinv_1", applied["invoice_id"])
	assert.Equal(t, "90", applied["amount_applied"], "decimals go out as strings, like invoice and payment amounts")
}
