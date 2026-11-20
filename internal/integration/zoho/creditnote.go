package zoho

import (
	"context"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	zohoDateLayout              = "2006-01-02"
	zohoRefundModePrepaidWallet = "prepaid_wallet"
	zohoCreditNoteStatusVoid    = "void"
)

type ZohoCreditNoteService interface {
	// SyncCreditNoteToZoho creates the credit note on its synced Zoho invoice, then records
	// its succeeded refunds (refund credit note) or applies it to the invoice (adjustment
	// credit note). A credit note already mapped to Zoho is skipped. A voided credit note that
	// was synced has its applied credit removed and is voided in Zoho.
	SyncCreditNoteToZoho(ctx context.Context, req ZohoCreditNoteSyncRequest) error
}

// ZohoCreditNoteSyncRequest carries the finalized credit note and its refund rows.
type ZohoCreditNoteSyncRequest struct {
	CreditNote *creditnote.CreditNote
	Refunds    []*refund.Refund
}

type creditNoteService struct {
	client      ZohoClient
	invoiceRepo invoice.Repository
	mappingRepo entityintegrationmapping.Repository
	logger      *logger.Logger
}

func NewCreditNoteService(
	client ZohoClient,
	invoiceRepo invoice.Repository,
	mappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) ZohoCreditNoteService {
	return &creditNoteService{
		client:      client,
		invoiceRepo: invoiceRepo,
		mappingRepo: mappingRepo,
		logger:      logger,
	}
}

func (s *creditNoteService) SyncCreditNoteToZoho(ctx context.Context, req ZohoCreditNoteSyncRequest) error {
	cn := req.CreditNote
	if cn == nil {
		return nil
	}

	switch cn.CreditNoteStatus {
	case types.CreditNoteStatusFinalized:
		return s.syncFinalizedCreditNote(ctx, cn, req.Refunds)
	case types.CreditNoteStatusVoided:
		return s.voidCreditNote(ctx, cn)
	default:
		return nil
	}
}

func (s *creditNoteService) syncFinalizedCreditNote(ctx context.Context, cn *creditnote.CreditNote, refunds []*refund.Refund) error {
	conn, err := s.client.GetConnection(ctx)
	if err != nil {
		return err
	}

	invoiceMapping, err := s.findMapping(ctx, types.IntegrationEntityTypeInvoice, cn.InvoiceID, conn.ID)
	if err != nil {
		return err
	}
	if invoiceMapping == nil {
		s.logger.Info(ctx, "invoice is not synced to Zoho, skipping credit note sync",
			"credit_note_id", cn.ID,
			"invoice_id", cn.InvoiceID)
		return nil
	}
	zohoInvoiceID := strings.TrimSpace(invoiceMapping.ProviderEntityID)

	settings, err := s.getInvoiceSyncSettings(ctx)
	if err != nil {
		return err
	}

	creditNoteMapping, err := s.findMapping(ctx, types.IntegrationEntityTypeCreditNote, cn.ID, conn.ID)
	if err != nil {
		return err
	}
	if creditNoteMapping != nil {
		s.logger.Info(ctx, "credit note already synced to Zoho, skipping",
			"credit_note_id", cn.ID,
			"zoho_credit_note_id", creditNoteMapping.ProviderEntityID)
		return nil
	}

	// Zoho requires the exchange rate when the credit note's currency differs from the
	// organization's base currency. The credit note and its refunds use the same rate.
	_, exchangeRate, err := s.client.ResolveInvoiceCurrency(ctx, cn.Currency)
	if err != nil {
		return err
	}

	zohoCreditNote, err := s.createCreditNote(ctx, cn, zohoInvoiceID, conn.ID, exchangeRate)
	if err != nil {
		return err
	}

	switch cn.CreditNoteType {
	case types.CreditNoteTypeRefund:
		return s.recordRefunds(ctx, cn, zohoCreditNote.CreditNoteID, refunds, settings, exchangeRate)
	case types.CreditNoteTypeAdjustment:
		return s.applyToInvoice(ctx, cn, zohoCreditNote.CreditNoteID, zohoInvoiceID, zohoCreditNote.Balance)
	default:
		return nil
	}
}

// createCreditNote creates the Open Zoho credit note on the invoice and saves its mapping.
func (s *creditNoteService) createCreditNote(ctx context.Context, cn *creditnote.CreditNote, zohoInvoiceID, connectionID string, exchangeRate float64) (*CreditNoteResponse, error) {
	flexInvoice, err := s.invoiceRepo.Get(ctx, cn.InvoiceID)
	if err != nil {
		return nil, err
	}

	zohoInvoice, err := s.client.GetInvoice(ctx, zohoInvoiceID)
	if err != nil {
		return nil, err
	}
	if zohoInvoice == nil {
		return nil, ierr.NewError("zoho invoice not found").
			WithHintf("Zoho invoice %s for invoice %s was not found", zohoInvoiceID, cn.InvoiceID).
			Mark(ierr.ErrNotFound)
	}

	lineItems, err := s.buildCreditNoteLineItems(ctx, cn, flexInvoice, zohoInvoiceID, connectionID)
	if err != nil {
		return nil, err
	}

	date := clock.Now()
	if cn.FinalizedAt != nil {
		date = *cn.FinalizedAt
	}

	zohoCreditNote, err := s.client.CreateCreditNote(ctx, zohoInvoiceID, &CreditNoteCreateRequest{
		CustomerID:      zohoInvoice.CustomerID,
		Date:            date.Format(zohoDateLayout),
		IsDraft:         false,
		ExchangeRate:    exchangeRate,
		ReferenceNumber: cn.ID,
		LineItems:       lineItems,
	})
	if err != nil {
		return nil, err
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityType:       types.IntegrationEntityTypeCreditNote,
		EntityID:         cn.ID,
		ProviderType:     string(types.SecretProviderZohoBooks),
		ProviderEntityID: zohoCreditNote.CreditNoteID,
		EnvironmentID:    cn.EnvironmentID,
		BaseModel:        types.GetDefaultBaseModel(ctx),
		Metadata: map[string]interface{}{
			"synced_at":              clock.Now().Format(time.RFC3339),
			"zoho_status":            zohoCreditNote.Status,
			"zoho_creditnote_number": zohoCreditNote.CreditNoteNumber,
			"zoho_invoice_id":        zohoInvoiceID,
		},
		ConnectionID: lo.ToPtr(connectionID),
	}
	mapping.TenantID = cn.TenantID
	if err := s.mappingRepo.Create(ctx, mapping); err != nil {
		return nil, err
	}

	s.logger.Info(ctx, "created Zoho credit note",
		"credit_note_id", cn.ID,
		"invoice_id", cn.InvoiceID,
		"zoho_credit_note_id", zohoCreditNote.CreditNoteID,
		"zoho_invoice_id", zohoInvoiceID,
		"zoho_status", zohoCreditNote.Status)
	return zohoCreditNote, nil
}

// buildCreditNoteLineItems credits, per credit note line, the Zoho item mapped to the price of
// the invoice line it credits.
func (s *creditNoteService) buildCreditNoteLineItems(ctx context.Context, cn *creditnote.CreditNote, flexInvoice *invoice.Invoice, zohoInvoiceID, connectionID string) ([]CreditNoteLineItem, error) {
	invoiceLineByID := lo.SliceToMap(flexInvoice.LineItems, func(li *invoice.InvoiceLineItem) (string, *invoice.InvoiceLineItem) {
		return li.ID, li
	})

	priceIDs := make([]string, len(cn.LineItems))
	for i, cnLine := range cn.LineItems {
		invoiceLine, ok := invoiceLineByID[cnLine.InvoiceLineItemID]
		if !ok || lo.FromPtr(invoiceLine.PriceID) == "" {
			return nil, ierr.NewError("credit note line has no priced invoice line").
				WithHintf("Invoice line %s of credit note %s has no price", cnLine.InvoiceLineItemID, cn.ID).
				Mark(ierr.ErrValidation)
		}
		priceIDs[i] = *invoiceLine.PriceID
	}

	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityType = types.IntegrationEntityTypePrice
	filter.EntityIDs = lo.Uniq(priceIDs)
	filter.ProviderTypes = []string{string(types.SecretProviderZohoBooks)}
	filter.ConnectionID = connectionID
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	mappings, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	zohoItemIDByPriceID := lo.SliceToMap(mappings, func(m *entityintegrationmapping.EntityIntegrationMapping) (string, string) {
		return m.EntityID, strings.TrimSpace(m.ProviderEntityID)
	})

	lineItems := make([]CreditNoteLineItem, 0, len(cn.LineItems))
	for i, cnLine := range cn.LineItems {
		priceID := priceIDs[i]
		zohoItemID := zohoItemIDByPriceID[priceID]
		if zohoItemID == "" {
			return nil, ierr.NewError("price is not synced to zoho").
				WithHintf("Price %s of credit note %s has no Zoho item", priceID, cn.ID).
				Mark(ierr.ErrValidation)
		}
		lineItems = append(lineItems, CreditNoteLineItem{
			ItemID:    zohoItemID,
			InvoiceID: zohoInvoiceID,
			Name:      cnLine.DisplayName,
			Rate:      cnLine.Amount,
			Quantity:  decimal.NewFromInt(1),
		})
	}
	return lineItems, nil
}

// recordRefunds records one Zoho refund per succeeded refund row.
func (s *creditNoteService) recordRefunds(ctx context.Context, cn *creditnote.CreditNote, zohoCreditNoteID string, refunds []*refund.Refund, settings *types.InvoiceSyncSettings, exchangeRate float64) error {
	for _, r := range refunds {
		if r == nil || r.RefundStatus != types.RefundStatusSucceeded || lo.FromPtr(r.CreditNoteID) != cn.ID {
			continue
		}

		date := clock.Now()
		if r.SucceededAt != nil {
			date = *r.SucceededAt
		}

		refundMode := zohoRefundModePrepaidWallet
		if r.RefundDestination == types.RefundDestinationGateway {
			refundMode = lo.FromPtr(r.PaymentGateway)
		}

		zohoRefund, err := s.client.RefundCreditNote(ctx, zohoCreditNoteID, &CreditNoteRefundRequest{
			Date:            date.Format(zohoDateLayout),
			RefundMode:      refundMode,
			Amount:          r.SettledAmount,
			ExchangeRate:    exchangeRate,
			FromAccountID:   settings.ZohoDepositToAccountID(),
			ReferenceNumber: r.ID,
		})
		if err != nil {
			return err
		}

		s.logger.Info(ctx, "recorded Zoho credit note refund",
			"credit_note_id", cn.ID,
			"refund_id", r.ID,
			"zoho_credit_note_id", zohoCreditNoteID,
			"zoho_refund_id", zohoRefund.CreditNoteRefundID,
			"amount", r.SettledAmount.String(),
			"refund_mode", refundMode)
	}
	return nil
}

// applyToInvoice applies the credit note's balance to the Zoho invoice it was raised on.
func (s *creditNoteService) applyToInvoice(ctx context.Context, cn *creditnote.CreditNote, zohoCreditNoteID, zohoInvoiceID string, balance decimal.Decimal) error {
	if !balance.IsPositive() {
		return nil
	}

	err := s.client.ApplyCreditNoteToInvoices(ctx, zohoCreditNoteID, &CreditNoteApplyRequest{
		Invoices: []CreditNoteInvoiceApply{{InvoiceID: zohoInvoiceID, AmountApplied: balance}},
	})
	if err != nil {
		return err
	}

	s.logger.Info(ctx, "applied Zoho credit note to invoice",
		"credit_note_id", cn.ID,
		"zoho_credit_note_id", zohoCreditNoteID,
		"zoho_invoice_id", zohoInvoiceID,
		"amount", balance.String())
	return nil
}

// voidCreditNote removes the credit note's applied credit from the Zoho invoice and voids it in
// Zoho, then records the void on its mapping. A credit note never synced to Zoho is skipped.
func (s *creditNoteService) voidCreditNote(ctx context.Context, cn *creditnote.CreditNote) error {
	conn, err := s.client.GetConnection(ctx)
	if err != nil {
		return err
	}

	creditNoteMapping, err := s.findMapping(ctx, types.IntegrationEntityTypeCreditNote, cn.ID, conn.ID)
	if err != nil {
		return err
	}
	if creditNoteMapping == nil {
		s.logger.Info(ctx, "credit note was never synced to Zoho, skipping void",
			"credit_note_id", cn.ID,
			"invoice_id", cn.InvoiceID)
		return nil
	}
	zohoCreditNoteID := strings.TrimSpace(creditNoteMapping.ProviderEntityID)

	zohoCreditNote, err := s.client.GetCreditNote(ctx, zohoCreditNoteID)
	if err != nil {
		return err
	}

	if zohoCreditNote.Status == zohoCreditNoteStatusVoid {
		s.logger.Info(ctx, "Zoho credit note is already void",
			"credit_note_id", cn.ID,
			"zoho_credit_note_id", zohoCreditNoteID)
	} else {
		// The delete takes the application's id, which only the invoice's applied credits list returns.
		invoiceMapping, err := s.findMapping(ctx, types.IntegrationEntityTypeInvoice, cn.InvoiceID, conn.ID)
		if err != nil {
			return err
		}
		if invoiceMapping != nil {
			zohoInvoiceID := strings.TrimSpace(invoiceMapping.ProviderEntityID)
			credits, err := s.client.ListInvoiceCreditsApplied(ctx, zohoInvoiceID)
			if err != nil {
				return err
			}
			for _, credit := range credits {
				if credit.CreditNoteID != zohoCreditNoteID {
					continue
				}
				if err := s.client.DeleteInvoiceAppliedCredit(ctx, zohoInvoiceID, credit.CreditNotesInvoiceID); err != nil {
					return err
				}
				s.logger.Info(ctx, "removed Zoho credit note's applied credit from invoice",
					"credit_note_id", cn.ID,
					"zoho_credit_note_id", zohoCreditNoteID,
					"zoho_invoice_id", zohoInvoiceID,
					"amount", credit.AmountApplied.String())
			}
		}
		if err := s.client.VoidCreditNote(ctx, zohoCreditNoteID); err != nil {
			return err
		}
		s.logger.Info(ctx, "voided Zoho credit note",
			"credit_note_id", cn.ID,
			"zoho_credit_note_id", zohoCreditNoteID)
	}

	if creditNoteMapping.Metadata == nil {
		creditNoteMapping.Metadata = map[string]interface{}{}
	}
	creditNoteMapping.Metadata["zoho_status"] = zohoCreditNoteStatusVoid
	return s.mappingRepo.Update(ctx, creditNoteMapping)
}

// findMapping returns the published Zoho mapping of a Flexprice entity on this connection, or nil.
func (s *creditNoteService) findMapping(ctx context.Context, entityType types.IntegrationEntityType, entityID, connectionID string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := types.NewEntityIntegrationMappingFilter()
	filter.EntityType = entityType
	filter.EntityID = entityID
	filter.ProviderTypes = []string{string(types.SecretProviderZohoBooks)}
	filter.ConnectionID = connectionID
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	mappings, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(mappings) == 0 {
		return nil, nil
	}
	return mappings[0], nil
}

func (s *creditNoteService) getInvoiceSyncSettings(ctx context.Context) (*types.InvoiceSyncSettings, error) {
	syncConfig, err := s.client.GetZohoBooksSyncConfig(ctx)
	if err != nil {
		return nil, err
	}
	if syncConfig == nil {
		return nil, nil
	}
	return syncConfig.InvoiceSyncSettings, nil
}
