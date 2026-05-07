package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainconn "github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const zohoWebhookSignatureHeader = "X-Zoho-Webhook-Signature"

// ErrInvalidWebhookSignature is returned when X-Zoho-Webhook-Signature does not match the payload.
var ErrInvalidWebhookSignature = errors.New("invalid Zoho webhook signature")

// ErrWebhookSecretNotConfigured is returned when the connection has no webhook secret to verify with.
var ErrWebhookSecretNotConfigured = errors.New("Zoho Books webhook secret is not configured")

// Handler processes verified Zoho Books webhooks (invoice paid / voided, contact created).
type Handler struct {
	logger *logger.Logger
}

// NewHandler constructs a Zoho Books webhook handler.
func NewHandler(log *logger.Logger) *Handler {
	return &Handler{logger: log}
}

// SignatureHeaderName returns the HTTP header Zoho uses for the HMAC digest.
func SignatureHeaderName() string { return zohoWebhookSignatureHeader }

// ServiceDeps are required application services for side effects.
type ServiceDeps struct {
	PaymentService                  interfaces.PaymentService
	InvoiceService                  interfaces.InvoiceService
	CustomerService                 interfaces.CustomerService
	EntityIntegrationMappingService interfaces.EntityIntegrationMappingService
}

// Handle verifies the signature and dispatches on payload shape.
// webhookSecretPlain must be the decrypted signing secret from the connection (see zoho.Client.GetZohoBooksWebhookConfig).
func (h *Handler) Handle(ctx context.Context, conn *domainconn.Connection, parsedURL *url.URL, rawBody []byte, signatureHeader string, webhookSecretPlain string, deps *ServiceDeps) error {
	if deps == nil {
		return ierr.NewError("zoho webhook: nil dependencies").Mark(ierr.ErrInternal)
	}
	zb := conn.EncryptedSecretData.ZohoBooks
	if zb == nil {
		return ierr.NewError("Zoho Books metadata missing").Mark(ierr.ErrNotFound)
	}
	if strings.TrimSpace(webhookSecretPlain) == "" {
		return ErrWebhookSecretNotConfigured
	}

	signing := BuildSigningString(parsedURL, rawBody)
	if !VerifySignature(signatureHeader, signing, webhookSecretPlain) {
		return fmt.Errorf("%w", ErrInvalidWebhookSignature)
	}

	var p Payload
	if err := json.Unmarshal(rawBody, &p); err != nil {
		return ierr.WithError(err).Mark(ierr.ErrValidation)
	}
	if org := p.OrganizationID.String(); org != "" && zb.OrganizationID != "" && org != zb.OrganizationID {
		h.logger.Warnw("zoho webhook organization_id mismatch",
			"payload_org", org,
			"connection_org", zb.OrganizationID)
		return ierr.NewError("organization_id does not match connection").Mark(ierr.ErrValidation)
	}

	switch {
	case p.Invoice != nil:
		return h.handleInvoice(ctx, p.Invoice, deps)
	case p.Contact != nil:
		return h.handleContact(ctx, conn, p.Contact, deps)
	default:
		h.logger.Debugw("zoho webhook: no invoice or contact in payload, ignoring")
		return nil
	}
}

func (h *Handler) handleInvoice(ctx context.Context, inv *InvoicePayload, deps *ServiceDeps) error {
	if inv == nil || inv.InvoiceID.String() == "" {
		return nil
	}
	zohoInvID := inv.InvoiceID.String()
	st := strings.TrimSpace(inv.Status)
	switch {
	case zohoWebhookInvoiceStatusVoid(st):
		return h.handleInvoiceVoided(ctx, zohoInvID, deps)
	case strings.EqualFold(st, ZohoBooksInvoiceWebhookStatusPaid):
		return h.handleInvoicePaid(ctx, zohoInvID, inv, deps)
	default:
		return nil
	}
}

func (h *Handler) handleInvoiceVoided(ctx context.Context, zohoInvID string, deps *ServiceDeps) error {
	mapping, err := h.findInvoiceMapping(ctx, zohoInvID, deps)
	if err != nil || mapping == nil {
		h.logger.Debugw("zoho webhook: no FlexPrice invoice mapping for void",
			"zoho_invoice_id", zohoInvID,
			"error", err)
		return nil
	}

	invResp, err := deps.InvoiceService.GetInvoice(ctx, mapping.EntityID)
	if err != nil {
		return err
	}
	if invResp == nil {
		return nil
	}
	if invResp.InvoiceStatus == types.InvoiceStatusVoided {
		h.logger.Debugw("zoho webhook: FlexPrice invoice already voided",
			"invoice_id", mapping.EntityID,
			"zoho_invoice_id", zohoInvID)
		return nil
	}

	voidReq := dto.InvoiceVoidRequest{
		Metadata: types.Metadata{
			"void_source":     "zoho_books_webhook",
			"zoho_invoice_id": zohoInvID,
		},
	}
	if err := deps.InvoiceService.VoidInvoice(ctx, mapping.EntityID, voidReq); err != nil {
		h.logger.Errorw("zoho webhook: VoidInvoice failed",
			"error", err,
			"invoice_id", mapping.EntityID,
			"zoho_invoice_id", zohoInvID)
		return err
	}

	h.logger.Infow("zoho webhook: voided FlexPrice invoice from Zoho",
		"invoice_id", mapping.EntityID,
		"zoho_invoice_id", zohoInvID)
	return nil
}

func (h *Handler) handleInvoicePaid(ctx context.Context, zohoInvID string, inv *InvoicePayload, deps *ServiceDeps) error {
	mapping, err := h.findInvoiceMapping(ctx, zohoInvID, deps)
	if err != nil || mapping == nil {
		h.logger.Debugw("zoho webhook: no FlexPrice invoice mapping",
			"zoho_invoice_id", zohoInvID,
			"error", err)
		return nil
	}

	invResp, err := deps.InvoiceService.GetInvoice(ctx, mapping.EntityID)
	if err != nil {
		return err
	}
	if invResp == nil {
		return nil
	}
	if invResp.PaymentStatus == types.PaymentStatusSucceeded || invResp.PaymentStatus == types.PaymentStatusOverpaid {
		h.logger.Debugw("zoho webhook: invoice already reconciled as paid",
			"invoice_id", mapping.EntityID,
			"payment_status", invResp.PaymentStatus)
		return nil
	}

	amount, ok := resolvePaidAmount(inv, invResp)
	if !ok || amount.IsZero() {
		h.logger.Warnw("zoho webhook: could not resolve paid amount",
			"zoho_invoice_id", zohoInvID,
			"flex_invoice_id", mapping.EntityID)
		return nil
	}

	gatewayID := "zoho_books:invoice:" + zohoInvID + ":paid"
	filter := types.NewNoLimitPaymentFilter()
	filter.GatewayPaymentID = &gatewayID
	listResp, err := deps.PaymentService.ListPayments(ctx, filter)
	if err == nil && listResp != nil && len(listResp.Items) > 0 {
		h.logger.Debugw("zoho webhook: payment already exists",
			"payment_id", listResp.Items[0].ID,
			"gateway_payment_id", gatewayID)
		return nil
	}

	createReq := &dto.CreatePaymentRequest{
		IdempotencyKey:    gatewayID,
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     mapping.EntityID,
		Amount:            amount,
		Currency:          invResp.Currency,
		PaymentMethodType: types.PaymentMethodTypeCard,
		ProcessPayment:    false,
		Metadata: types.Metadata{
			"payment_source":     "zoho_books_external",
			"zoho_invoice_id":    zohoInvID,
			"entity_mapping_id":  mapping.ID,
			"webhook_event_hint": "invoice.paid",
		},
	}

	payResp, err := deps.PaymentService.CreatePayment(ctx, createReq)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	_, err = deps.PaymentService.UpdatePayment(ctx, payResp.ID, dto.UpdatePaymentRequest{
		PaymentStatus:    lo.ToPtr(string(types.PaymentStatusSucceeded)),
		GatewayPaymentID: &gatewayID,
		SucceededAt:      &now,
	})
	if err != nil {
		return err
	}

	if err := deps.InvoiceService.ReconcilePaymentStatus(ctx, mapping.EntityID, types.PaymentStatusSucceeded, &amount); err != nil {
		h.logger.Errorw("zoho webhook: ReconcilePaymentStatus failed",
			"error", err,
			"invoice_id", mapping.EntityID,
			"amount", amount.String())
		return err
	}

	h.logger.Infow("zoho webhook: reconciled invoice paid from Zoho",
		"invoice_id", mapping.EntityID,
		"zoho_invoice_id", zohoInvID,
		"amount", amount.String())
	return nil
}

func (h *Handler) handleContact(ctx context.Context, conn *domainconn.Connection, c *ContactPayload, deps *ServiceDeps) error {
	if c == nil || c.ContactID == "" {
		return nil
	}
	if !conn.IsCustomerInboundEnabled() {
		h.logger.Debugw("zoho webhook: customer inbound disabled, skipping contact",
			"zoho_contact_id", c.ContactID)
		return nil
	}
	ct := strings.ToLower(strings.TrimSpace(c.ContactType))
	if ct == "vendor" {
		return nil
	}

	existing, err := h.findCustomerMapping(ctx, c.ContactID, deps)
	if err != nil {
		return err
	}
	if existing != nil {
		h.logger.Debugw("zoho webhook: customer mapping already exists",
			"zoho_contact_id", c.ContactID,
			"entity_id", existing.EntityID)
		return nil
	}

	email := primaryContactEmail(c)
	if email == "" {
		h.logger.Debugw("zoho webhook: contact has no email, skipping customer create",
			"zoho_contact_id", c.ContactID)
		return nil
	}

	custFilter := types.NewNoLimitCustomerFilter()
	custFilter.Email = email
	custList, err := deps.CustomerService.GetCustomers(ctx, custFilter)
	if err != nil {
		return err
	}
	if custList != nil && len(custList.Items) > 0 {
		h.logger.Debugw("zoho webhook: FlexPrice customer with same email exists, skipping create",
			"email", email,
			"zoho_contact_id", c.ContactID)
		return nil
	}

	name := strings.TrimSpace(c.ContactName)
	if name == "" {
		name = email
	}

	extID := fmt.Sprintf("zoho_books_%s", c.ContactID)
	createReq := dto.CreateCustomerRequest{
		ExternalID:             extID,
		Name:                   name,
		Email:                  email,
		SkipOnboardingWorkflow: true,
		Metadata: map[string]string{
			"source":            "zoho_books_webhook",
			"zoho_contact_id":   c.ContactID,
			"zoho_contact_type": c.ContactType,
		},
	}

	custResp, err := deps.CustomerService.CreateCustomer(ctx, createReq)
	if err != nil {
		return err
	}

	_, err = deps.EntityIntegrationMappingService.CreateEntityIntegrationMapping(ctx, dto.CreateEntityIntegrationMappingRequest{
		EntityID:         custResp.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderZohoBooks),
		ProviderEntityID: c.ContactID,
		Metadata: map[string]interface{}{
			"source": "zoho_books_inbound_webhook",
		},
	})
	if err != nil {
		h.logger.Errorw("zoho webhook: customer created but mapping failed",
			"error", err,
			"customer_id", custResp.ID,
			"zoho_contact_id", c.ContactID)
		return err
	}

	h.logger.Infow("zoho webhook: created FlexPrice customer from Zoho contact",
		"customer_id", custResp.ID,
		"zoho_contact_id", c.ContactID)
	return nil
}

func (h *Handler) findInvoiceMapping(ctx context.Context, zohoInvoiceID string, deps *ServiceDeps) (*dto.EntityIntegrationMappingResponse, error) {
	filter := types.NewEntityIntegrationMappingFilter()
	filter.ProviderEntityIDs = []string{zohoInvoiceID}
	filter.ProviderTypes = []string{string(types.SecretProviderZohoBooks)}
	filter.EntityType = types.IntegrationEntityTypeInvoice
	list, err := deps.EntityIntegrationMappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return nil, err
	}
	if list == nil || len(list.Items) == 0 {
		return nil, nil
	}
	return list.Items[0], nil
}

func (h *Handler) findCustomerMapping(ctx context.Context, zohoContactID string, deps *ServiceDeps) (*dto.EntityIntegrationMappingResponse, error) {
	filter := types.NewEntityIntegrationMappingFilter()
	filter.ProviderEntityIDs = []string{zohoContactID}
	filter.ProviderTypes = []string{string(types.SecretProviderZohoBooks)}
	filter.EntityType = types.IntegrationEntityTypeCustomer
	list, err := deps.EntityIntegrationMappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return nil, err
	}
	if list == nil || len(list.Items) == 0 {
		return nil, nil
	}
	return list.Items[0], nil
}

// zohoWebhookInvoiceStatusVoid matches Zoho Books invoice statuses for a voided invoice.
func zohoWebhookInvoiceStatusVoid(st string) bool {
	switch strings.ToLower(strings.TrimSpace(st)) {
	case ZohoBooksInvoiceWebhookStatusVoid, ZohoBooksInvoiceWebhookStatusVoided:
		return true
	default:
		return false
	}
}

func primaryContactEmail(c *ContactPayload) string {
	if c == nil {
		return ""
	}
	if e := strings.TrimSpace(c.Email); e != "" {
		return e
	}
	for _, p := range c.ContactPersons {
		if e := strings.TrimSpace(p.Email); e != "" {
			return e
		}
	}
	return ""
}

func resolvePaidAmount(inv *InvoicePayload, flex *dto.InvoiceResponse) (decimal.Decimal, bool) {
	if inv == nil || flex == nil {
		return decimal.Zero, false
	}
	if s := inv.PaymentMade.String(); s != "" {
		if d, err := decimal.NewFromString(s); err == nil && d.IsPositive() {
			return d, true
		}
	}
	total, errT := decimal.NewFromString(inv.Total.String())
	bal, errB := decimal.NewFromString(inv.Balance.String())
	if errT == nil && errB == nil {
		d := total.Sub(bal)
		if d.IsPositive() {
			return d, true
		}
	}
	if errT == nil && total.IsPositive() {
		balStr := inv.Balance.String()
		if balStr == "" || balStr == "0" || balStr == "0.00" || balStr == "0.0" {
			return total, true
		}
	}
	if flex.AmountRemaining.IsPositive() {
		return flex.AmountRemaining, true
	}
	return decimal.Zero, false
}
