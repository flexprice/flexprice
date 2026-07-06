package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
)

// getConnectionIfExists returns the connection for a provider, or nil if none is configured.
// A "not found" result is not an error — it simply means the tenant hasn't set up that provider.
// Real DB errors are still propagated.
func getConnectionIfExists(ctx context.Context, connRepo connection.Repository, provider types.SecretProvider) (*connection.Connection, error) {
	conn, err := connRepo.GetByProvider(ctx, provider)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, nil // provider not configured for this tenant — skip silently
		}
		return nil, fmt.Errorf("provider %s lookup failed: %w", provider, err)
	}
	return conn, nil
}

// invoiceAlreadySynced returns true when the entity mapping table already has a record for
// (invoiceID, invoice, provider). This is the primary idempotency guard that prevents
// duplicate external invoices when the same Kafka message is consumed more than once
// (e.g. two consumers on the same topic, at-least-once redelivery, manual replay).
func invoiceAlreadySynced(ctx context.Context, eimRepo entityintegrationmapping.Repository, invoiceID string, provider types.SecretProvider) bool {
	if eimRepo == nil {
		return false
	}
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = invoiceID
	filter.EntityType = types.IntegrationEntityTypeInvoice
	filter.ProviderTypes = []string{string(provider)}
	count, err := eimRepo.Count(ctx, filter)
	return err == nil && count > 0
}

// customerAlreadySynced returns true when the entity mapping table already has a record for
// (customerID, customer, provider). Same idempotency guarantee as invoiceAlreadySynced.
func customerAlreadySynced(ctx context.Context, eimRepo entityintegrationmapping.Repository, customerID string, provider types.SecretProvider) bool {
	if eimRepo == nil {
		return false
	}
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = customerID
	filter.EntityType = types.IntegrationEntityTypeCustomer
	filter.ProviderTypes = []string{string(provider)}
	count, err := eimRepo.Count(ctx, filter)
	return err == nil && count > 0
}

// subscriptionAlreadySynced returns true when the entity mapping table already has a record for
// (subscriptionID, subscription, provider). Same idempotency guarantee as invoiceAlreadySynced.
func subscriptionAlreadySynced(ctx context.Context, eimRepo entityintegrationmapping.Repository, subscriptionID string, provider types.SecretProvider) bool {
	if eimRepo == nil {
		return false
	}
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = subscriptionID
	filter.EntityType = types.IntegrationEntityTypeSubscription
	filter.ProviderTypes = []string{string(provider)}
	count, err := eimRepo.Count(ctx, filter)
	return err == nil && count > 0
}

// invoiceVendorSyncInput holds the minimal data needed to dispatch a provider trigger.
// Invoice and subscription details are fetched inside the Temporal activity after a
// short sleep, avoiding races where the event arrives before the DB transaction commits.
type invoiceVendorSyncInput struct {
	TenantID      string
	EnvironmentID string
	UserID        string
	InvoiceID     string
}

type customerVendorSyncInput struct {
	TenantID      string
	EnvironmentID string
	UserID        string
	CustomerID    string
}

// invoiceSyncProviderOrder is the fixed code order used both to build the enabled-provider
// list and as the tie-break when a customer has no allow-list. This is the canonical
// dispatch order for invoice outbound sync.
var invoiceSyncProviderOrder = []types.SecretProvider{
	types.SecretProviderStripe,
	types.SecretProviderRazorpay,
	types.SecretProviderChargebee,
	types.SecretProviderQuickBooks,
	types.SecretProviderHubSpot,
	types.SecretProviderMoyasar,
	types.SecretProviderNomod,
	types.SecretProviderPaddle,
	types.SecretProviderZohoBooks,
	types.SecretProviderWhop,
}

// invoiceOutboundEnabledInOrder returns the providers that have an enabled outbound
// invoice connection, in fixed code order. It fetches all of the tenant/environment's
// connections in a single query and indexes them by provider, rather than issuing one
// lookup per provider. A real DB error aborts; a missing connection simply excludes that
// provider.
func invoiceOutboundEnabledInOrder(
	ctx context.Context,
	connRepo connection.Repository,
) ([]types.SecretProvider, error) {
	filter := types.NewNoLimitConnectionFilter()
	filter.Status = lo.ToPtr(types.StatusPublished)
	conns, err := connRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	byProvider := make(map[types.SecretProvider]*connection.Connection, len(conns))
	for _, conn := range conns {
		if _, seen := byProvider[conn.ProviderType]; !seen {
			byProvider[conn.ProviderType] = conn
		}
	}

	var enabled []types.SecretProvider
	for _, p := range invoiceSyncProviderOrder {
		if conn, ok := byProvider[p]; ok && conn.IsInvoiceOutboundEnabled() {
			enabled = append(enabled, p)
		}
	}
	return enabled, nil
}

// resolveAllowedProviders returns the customer's ordered allow-list for the invoice. It
// reads the invoice to recover the customer id, then loads the customer. The invoice is
// already committed by the time its system event is dispatched, so there is no commit race.
// Missing entities are not fatal — they yield a nil allow-list (⇒ first enabled provider by
// fixed order).
func resolveAllowedProviders(
	ctx context.Context,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	invoiceID string,
	log *logger.Logger,
) ([]types.SecretProvider, error) {
	if invoiceRepo == nil || customerRepo == nil {
		return nil, nil
	}

	inv, err := invoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		if ierr.IsNotFound(err) || ent.IsNotFound(err) {
			log.Info(ctx, "integration_events: invoice not found while resolving sync target, defaulting to fixed order",
				"invoice_id", invoiceID)
			return nil, nil
		}
		return nil, err
	}

	if inv.CustomerID == "" {
		return nil, nil
	}

	cust, err := customerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		if ierr.IsNotFound(err) || ent.IsNotFound(err) {
			log.Info(ctx, "integration_events: customer not found while resolving sync target, defaulting to fixed order",
				"customer_id", inv.CustomerID, "invoice_id", invoiceID)
			return nil, nil
		}
		return nil, err
	}
	return cust.AllowedIntegrationProviders, nil
}

// ResolveInvoiceSyncTarget returns the single provider an invoice should sync to.
//
//	allowed        — customer.AllowedIntegrationProviders, in priority order (may be empty)
//	enabledInOrder — providers with an enabled outbound connection, in fixed code order
//
// Returns (target, true) or ("", false) when nothing is resolvable. It is pure and
// side-effect-free: it performs no I/O and is the single source of truth for invoice-sync
// routing.
func ResolveInvoiceSyncTarget(allowed []types.SecretProvider, enabledInOrder []types.SecretProvider) (types.SecretProvider, bool) {
	// Build a set of enabled providers for O(1) membership checks.
	enabledSet := make(map[types.SecretProvider]struct{}, len(enabledInOrder))
	for _, p := range enabledInOrder {
		enabledSet[p] = struct{}{}
	}

	// Non-empty allow-list: return the first allowed entry that is enabled.
	// Unknown/misspelled entries are simply never in the set, so they are ignored.
	if len(allowed) > 0 {
		for _, p := range allowed {
			if _, ok := enabledSet[p]; ok {
				return p, true
			}
		}
		return "", false
	}

	// Empty allow-list: fall back to the first enabled provider by fixed code order.
	if len(enabledInOrder) > 0 {
		return enabledInOrder[0], true
	}

	return "", false
}

// DispatchInvoiceVendorSync resolves an invoice to exactly one integration provider and
// starts that provider's Temporal sync workflow. The target is the first entry of the
// customer's allowed_integration_providers with an enabled outbound connection, or — when
// the allow-list is empty — the first enabled provider by fixed code order. Invoice details
// are still fetched inside the Temporal activity after a short sleep (the dispatcher only
// reads the customer, which pre-exists the invoice), preserving the commit-race avoidance.
// eimRepo is used for idempotency: if a mapping already exists the provider trigger is skipped.
func DispatchInvoiceVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	log *logger.Logger,
	event *types.WebhookEvent,
	msgUUID string,
) error {
	if cfg != nil && !cfg.IntegrationEvents.Enabled {
		return nil
	}

	// Parse the invoice id from the event payload. The customer is resolved by re-reading
	// the invoice, so no customer_id is expected here.
	var pl struct {
		InvoiceID string `json:"invoice_id"`
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil || pl.InvoiceID == "" {
		log.Error(ctx, "integration_events: invalid invoice payload, dropping",
			"message_uuid", msgUUID,
			"error", err,
		)
		return nil
	}

	in := invoiceVendorSyncInput{
		TenantID:      event.TenantID,
		EnvironmentID: event.EnvironmentID,
		UserID:        event.UserID,
		InvoiceID:     pl.InvoiceID,
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}

	// Determine enabled providers (fixed order) and the customer's allow-list, then resolve
	// to a single target.
	enabledInOrder, err := invoiceOutboundEnabledInOrder(ctx, connRepo)
	if err != nil {
		return fmt.Errorf("integration_events: failed to determine enabled providers for invoice %s: %w", in.InvoiceID, err)
	}
	allowed, err := resolveAllowedProviders(ctx, customerRepo, invoiceRepo, pl.InvoiceID, log)
	if err != nil {
		return fmt.Errorf("integration_events: failed to resolve allow-list for invoice %s: %w", in.InvoiceID, err)
	}

	target, ok := ResolveInvoiceSyncTarget(allowed, enabledInOrder)
	if !ok {
		log.Info(ctx, "integration_events: no invoice sync target resolved, skipping",
			"invoice_id", in.InvoiceID,
			"tenant_id", in.TenantID,
			"environment_id", in.EnvironmentID,
			"allowed_integration_providers", allowed,
			"enabled_providers", enabledInOrder,
		)
		return nil
	}

	log.Info(ctx, "integration_events: dispatching invoice vendor sync",
		"invoice_id", in.InvoiceID,
		"tenant_id", in.TenantID,
		"environment_id", in.EnvironmentID,
		"resolved_provider", target,
	)

	trigger, ok := invoiceSyncTrigger(ctx, connRepo, eimRepo, temporalSvc, log, in, target)
	if !ok {
		// Should not happen: target came from invoiceSyncProviderOrder.
		log.Error(ctx, "integration_events: resolved provider has no invoice trigger",
			"invoice_id", in.InvoiceID, "resolved_provider", target,
			"error", fmt.Errorf("provider %s resolved but has no invoice trigger", target))
		return nil
	}
	if err = trigger(); err != nil {
		return fmt.Errorf("integration_events: provider dispatch failed for invoice %s: %w", in.InvoiceID, err)
	}
	return nil
}

// invoiceSyncTrigger maps a resolved provider to its invoice-sync trigger closure. The
// returned closure retains the per-provider guards (connection existence, outbound
// enablement, idempotency) so routing is an additional filter, never a replacement.
func invoiceSyncTrigger(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
	provider types.SecretProvider,
) (func() error, bool) {
	triggers := map[types.SecretProvider]func() error{
		types.SecretProviderStripe:     func() error { return triggerStripeIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		types.SecretProviderRazorpay:   func() error { return triggerRazorpayIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		types.SecretProviderChargebee:  func() error { return triggerChargebeeIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		types.SecretProviderQuickBooks: func() error { return triggerQuickBooksIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		types.SecretProviderHubSpot:    func() error { return triggerHubSpotIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		types.SecretProviderMoyasar:    func() error { return triggerMoyasarIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		types.SecretProviderNomod:      func() error { return triggerNomodIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		types.SecretProviderPaddle:     func() error { return triggerPaddleIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		types.SecretProviderZohoBooks:  func() error { return triggerZohoBooksIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		types.SecretProviderWhop:       func() error { return triggerWhopIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
	}
	trigger, ok := triggers[provider]
	return trigger, ok
}

// DispatchCustomerVendorSync starts Temporal customer-sync workflows for each enabled provider.
// Used by the integration consumer on customer.created.
// eimRepo is used for idempotency: if a mapping already exists the provider trigger is skipped.
func DispatchCustomerVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	log *logger.Logger,
	event *types.WebhookEvent,
	msgUUID string,
) error {
	if cfg != nil && !cfg.IntegrationEvents.Enabled {
		return nil
	}

	var payload webhookDto.InternalCustomerEvent
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		log.Error(ctx, "integration_events: invalid customer payload, dropping",
			"message_uuid", msgUUID,
			"error", err,
		)
		return nil
	}

	if payload.CustomerID == "" {
		log.Info(context.Background(), "integration_events: customer payload missing customer_id, dropping",
			"message_uuid", msgUUID,
		)
		return nil
	}

	in := customerVendorSyncInput{
		TenantID:      event.TenantID,
		EnvironmentID: event.EnvironmentID,
		UserID:        event.UserID,
		CustomerID:    payload.CustomerID,
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}

	if in.CustomerID == "" {
		return nil
	}

	log.Info(ctx, "integration_events: dispatching customer vendor sync",
		"customer_id", in.CustomerID,
		"tenant_id", in.TenantID,
		"environment_id", in.EnvironmentID,
	)

	var dispatchErrs []error
	for _, trigger := range []func() error{
		func() error { return triggerStripeCustomerSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		func() error {
			return triggerRazorpayCustomerSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in)
		},
		func() error {
			return triggerChargebeeCustomerSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in)
		},
		func() error {
			return triggerQuickBooksCustomerSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in)
		},
		func() error { return triggerNomodCustomerSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		func() error { return triggerPaddleCustomerSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
	} {
		if err := trigger(); err != nil {
			dispatchErrs = append(dispatchErrs, err)
		}
	}

	if len(dispatchErrs) > 0 {
		return fmt.Errorf("integration_events: one or more provider dispatches failed for customer %s: %w", in.CustomerID, errors.Join(dispatchErrs...))
	}

	return nil
}

// DispatchSubscriptionVendorSync starts PaddleSubscriptionSyncWorkflow for every new subscription.
func DispatchSubscriptionVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	log *logger.Logger,
	event *types.WebhookEvent,
	msgUUID string,
) error {
	if cfg != nil && !cfg.IntegrationEvents.Enabled {
		return nil
	}

	var pl struct {
		SubscriptionID string `json:"subscription_id"`
		CustomerID     string `json:"customer_id"`
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil || pl.SubscriptionID == "" {
		log.Info(context.Background(), "integration_events: invalid subscription.created payload, skipping",
			"message_uuid", msgUUID, "error", err)
		return nil
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}

	return triggerPaddleSubscriptionSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, subscriptionVendorSyncInput{
		TenantID:       event.TenantID,
		EnvironmentID:  event.EnvironmentID,
		UserID:         event.UserID,
		SubscriptionID: pl.SubscriptionID,
		CustomerID:     pl.CustomerID,
	})
}

type subscriptionVendorSyncInput struct {
	TenantID       string
	EnvironmentID  string
	UserID         string
	SubscriptionID string
	CustomerID     string
}

func triggerPaddleSubscriptionSyncIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in subscriptionVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderPaddle)
	if err != nil {
		return err
	}

	if conn == nil {
		return nil
	}
	if subscriptionAlreadySynced(ctx, eimRepo, in.SubscriptionID, types.SecretProviderPaddle) {
		log.Info(ctx, "integration_events: subscription already synced to Paddle, skipping",
			"subscription_id", in.SubscriptionID)
		return nil
	}

	input := temporalmodels.PaddleSubscriptionSyncWorkflowInput{
		SubscriptionID: in.SubscriptionID,
		CustomerID:     in.CustomerID,
		TenantID:       in.TenantID,
		EnvironmentID:  in.EnvironmentID,
	}
	workflowRun, wfErr := temporalSvc.ExecuteWorkflow(ctx, types.TemporalPaddleSubscriptionSyncWorkflow, input)
	if wfErr != nil {
		log.Error(ctx, "integration_events: failed to start PaddleSubscriptionSyncWorkflow",
			"subscription_id", in.SubscriptionID, "error", wfErr)
		return fmt.Errorf("paddle subscription sync workflow start failed: %w", wfErr)
	}
	log.Info(ctx, "integration_events: PaddleSubscriptionSyncWorkflow started",
		"subscription_id", in.SubscriptionID,
		"workflow_id", workflowRun.GetID())
	return nil
}

func executeWorkflow(
	ctx context.Context,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	workflowType types.TemporalWorkflowType,
	input interface{},
	provider types.SecretProvider,
	invoiceID string,
) error {
	workflowRun, err := temporalSvc.ExecuteWorkflow(ctx, workflowType, input)
	if err != nil {
		log.Error(ctx, "integration_events: failed to start workflow",
			"provider", provider,
			"workflow_type", workflowType,
			"invoice_id", invoiceID,
			"error", err,
		)
		return fmt.Errorf("provider %s workflow start failed: %w", provider, err)
	}

	log.Info(ctx, "integration_events: workflow started",
		"provider", provider,
		"workflow_type", workflowType,
		"invoice_id", invoiceID,
		"workflow_id", workflowRun.GetID(),
		"run_id", workflowRun.GetRunID(),
	)
	return nil
}

func executeCustomerWorkflow(
	ctx context.Context,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	workflowType types.TemporalWorkflowType,
	input interface{},
	provider types.SecretProvider,
	customerID string,
) error {
	workflowRun, err := temporalSvc.ExecuteWorkflow(ctx, workflowType, input)
	if err != nil {
		log.Error(ctx, "integration_events: failed to start workflow",
			"provider", provider,
			"workflow_type", workflowType,
			"customer_id", customerID,
			"error", err,
		)
		return fmt.Errorf("provider %s workflow start failed: %w", provider, err)
	}

	log.Info(ctx, "integration_events: workflow started",
		"provider", provider,
		"workflow_type", workflowType,
		"customer_id", customerID,
		"workflow_id", workflowRun.GetID(),
		"run_id", workflowRun.GetRunID(),
	)
	return nil
}

func triggerStripeIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderStripe)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, types.SecretProviderStripe) {
		log.Info(ctx, "integration_events: invoice already synced to Stripe, skipping", "invoice_id", in.InvoiceID)
		return nil
	}

	input := &temporalmodels.StripeInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalStripeInvoiceSyncWorkflow, input, types.SecretProviderStripe, in.InvoiceID)
}

func triggerRazorpayIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderRazorpay)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, types.SecretProviderRazorpay) {
		log.Info(ctx, "integration_events: invoice already synced to Razorpay, skipping", "invoice_id", in.InvoiceID)
		return nil
	}

	input := &temporalmodels.RazorpayInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalRazorpayInvoiceSyncWorkflow, input, types.SecretProviderRazorpay, in.InvoiceID)
}

func triggerChargebeeIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderChargebee)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, types.SecretProviderChargebee) {
		log.Info(ctx, "integration_events: invoice already synced to Chargebee, skipping", "invoice_id", in.InvoiceID)
		return nil
	}

	input := &temporalmodels.ChargebeeInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalChargebeeInvoiceSyncWorkflow, input, types.SecretProviderChargebee, in.InvoiceID)
}

func triggerQuickBooksIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderQuickBooks)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, types.SecretProviderQuickBooks) {
		log.Info(ctx, "integration_events: invoice already synced to QuickBooks, skipping", "invoice_id", in.InvoiceID)
		return nil
	}

	input := &temporalmodels.QuickBooksInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalQuickBooksInvoiceSyncWorkflow, input, types.SecretProviderQuickBooks, in.InvoiceID)
}

func triggerHubSpotIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderHubSpot)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, types.SecretProviderHubSpot) {
		log.Info(ctx, "integration_events: invoice already synced to HubSpot, skipping", "invoice_id", in.InvoiceID)
		return nil
	}

	input := &temporalmodels.HubSpotInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalHubSpotInvoiceSyncWorkflow, input, types.SecretProviderHubSpot, in.InvoiceID)
}

func triggerMoyasarIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderMoyasar)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, types.SecretProviderMoyasar) {
		log.Info(ctx, "integration_events: invoice already synced to Moyasar, skipping", "invoice_id", in.InvoiceID)
		return nil
	}

	input := &temporalmodels.MoyasarInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalMoyasarInvoiceSyncWorkflow, input, types.SecretProviderMoyasar, in.InvoiceID)
}

func triggerNomodIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderNomod)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, types.SecretProviderNomod) {
		log.Info(ctx, "integration_events: invoice already synced to Nomod, skipping", "invoice_id", in.InvoiceID)
		return nil
	}

	input := &temporalmodels.NomodInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalNomodInvoiceSyncWorkflow, input, types.SecretProviderNomod, in.InvoiceID)
}

func triggerPaddleIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderPaddle)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, types.SecretProviderPaddle) {
		log.Info(ctx, "integration_events: invoice already synced to Paddle, skipping", "invoice_id", in.InvoiceID)
		return nil
	}

	input := &temporalmodels.PaddleInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalPaddleInvoiceSyncWorkflow, input, types.SecretProviderPaddle, in.InvoiceID)
}

func triggerZohoBooksIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderZohoBooks)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, types.SecretProviderZohoBooks) {
		log.Info(ctx, "integration_events: invoice already synced to Zoho Books, skipping", "invoice_id", in.InvoiceID)
		return nil
	}
	input := &temporalmodels.ZohoBooksInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalZohoBooksInvoiceSyncWorkflow, input, types.SecretProviderZohoBooks, in.InvoiceID)
}

func triggerStripeCustomerSyncIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in customerVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderStripe)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsCustomerOutboundEnabled() {
		return nil
	}
	if customerAlreadySynced(ctx, eimRepo, in.CustomerID, types.SecretProviderStripe) {
		log.Info(ctx, "integration_events: customer already synced to Stripe, skipping", "customer_id", in.CustomerID)
		return nil
	}
	input := &temporalmodels.StripeCustomerSyncWorkflowInput{
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeCustomerWorkflow(ctx, temporalSvc, log, types.TemporalStripeCustomerSyncWorkflow, input, types.SecretProviderStripe, in.CustomerID)
}

func triggerRazorpayCustomerSyncIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in customerVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderRazorpay)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsCustomerOutboundEnabled() {
		return nil
	}
	if customerAlreadySynced(ctx, eimRepo, in.CustomerID, types.SecretProviderRazorpay) {
		log.Info(ctx, "integration_events: customer already synced to Razorpay, skipping", "customer_id", in.CustomerID)
		return nil
	}
	input := &temporalmodels.RazorpayCustomerSyncWorkflowInput{
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeCustomerWorkflow(ctx, temporalSvc, log, types.TemporalRazorpayCustomerSyncWorkflow, input, types.SecretProviderRazorpay, in.CustomerID)
}

func triggerChargebeeCustomerSyncIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in customerVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderChargebee)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsCustomerOutboundEnabled() {
		return nil
	}
	if customerAlreadySynced(ctx, eimRepo, in.CustomerID, types.SecretProviderChargebee) {
		log.Info(ctx, "integration_events: customer already synced to Chargebee, skipping", "customer_id", in.CustomerID)
		return nil
	}
	input := &temporalmodels.ChargebeeCustomerSyncWorkflowInput{
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeCustomerWorkflow(ctx, temporalSvc, log, types.TemporalChargebeeCustomerSyncWorkflow, input, types.SecretProviderChargebee, in.CustomerID)
}

func triggerQuickBooksCustomerSyncIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in customerVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderQuickBooks)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsCustomerOutboundEnabled() {
		return nil
	}
	if customerAlreadySynced(ctx, eimRepo, in.CustomerID, types.SecretProviderQuickBooks) {
		log.Info(ctx, "integration_events: customer already synced to QuickBooks, skipping", "customer_id", in.CustomerID)
		return nil
	}
	input := &temporalmodels.QuickBooksCustomerSyncWorkflowInput{
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeCustomerWorkflow(ctx, temporalSvc, log, types.TemporalQuickBooksCustomerSyncWorkflow, input, types.SecretProviderQuickBooks, in.CustomerID)
}

func triggerNomodCustomerSyncIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in customerVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderNomod)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsCustomerOutboundEnabled() {
		return nil
	}
	if customerAlreadySynced(ctx, eimRepo, in.CustomerID, types.SecretProviderNomod) {
		log.Info(ctx, "integration_events: customer already synced to Nomod, skipping", "customer_id", in.CustomerID)
		return nil
	}
	input := &temporalmodels.NomodCustomerSyncWorkflowInput{
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeCustomerWorkflow(ctx, temporalSvc, log, types.TemporalNomodCustomerSyncWorkflow, input, types.SecretProviderNomod, in.CustomerID)
}

func triggerPaddleCustomerSyncIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in customerVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderPaddle)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsCustomerOutboundEnabled() {
		return nil
	}
	if customerAlreadySynced(ctx, eimRepo, in.CustomerID, types.SecretProviderPaddle) {
		log.Info(ctx, "integration_events: customer already synced to Paddle, skipping", "customer_id", in.CustomerID)
		return nil
	}
	input := &temporalmodels.PaddleCustomerSyncWorkflowInput{
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeCustomerWorkflow(ctx, temporalSvc, log, types.TemporalPaddleCustomerSyncWorkflow, input, types.SecretProviderPaddle, in.CustomerID)
}

// DispatchInvoicePaidVendorSync pushes the "invoice paid" status back to the single
// integration the invoice is routed to. It resolves the target with the same rules as
// DispatchInvoiceVendorSync; only when the resolved target is Whop (the only provider that
// implements a mark-paid workflow) is a workflow started. Any other resolved target has no
// paid-status push and is a no-op.
func DispatchInvoicePaidVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	log *logger.Logger,
	event *types.WebhookEvent,
	msgUUID string,
) error {
	if cfg != nil && !cfg.IntegrationEvents.Enabled {
		return nil
	}

	var pl struct {
		InvoiceID string `json:"invoice_id"`
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil || pl.InvoiceID == "" {
		log.Error(ctx, "integration_events: invalid invoice payment payload, dropping",
			"message_uuid", msgUUID,
			"error", err,
		)
		return nil
	}

	in := invoiceVendorSyncInput{
		TenantID:      event.TenantID,
		EnvironmentID: event.EnvironmentID,
		UserID:        event.UserID,
		InvoiceID:     pl.InvoiceID,
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}

	enabledInOrder, err := invoiceOutboundEnabledInOrder(ctx, connRepo)
	if err != nil {
		return fmt.Errorf("integration_events: failed to determine enabled providers for invoice %s: %w", in.InvoiceID, err)
	}
	allowed, err := resolveAllowedProviders(ctx, customerRepo, invoiceRepo, pl.InvoiceID, log)
	if err != nil {
		return fmt.Errorf("integration_events: failed to resolve allow-list for invoice %s: %w", in.InvoiceID, err)
	}

	target, ok := ResolveInvoiceSyncTarget(allowed, enabledInOrder)
	if !ok {
		log.Info(ctx, "integration_events: no invoice paid sync target resolved, skipping",
			"invoice_id", in.InvoiceID,
			"allowed_integration_providers", allowed,
			"enabled_providers", enabledInOrder,
		)
		return nil
	}

	log.Info(ctx, "integration_events: dispatching invoice paid vendor sync",
		"invoice_id", in.InvoiceID,
		"tenant_id", in.TenantID,
		"environment_id", in.EnvironmentID,
		"resolved_provider", target,
	)

	// Only Whop has a mark-paid workflow; other resolved targets have nothing to push.
	if target != types.SecretProviderWhop {
		log.Info(ctx, "integration_events: resolved provider has no mark-paid step, skipping",
			"invoice_id", in.InvoiceID, "resolved_provider", target)
		return nil
	}

	if err := triggerWhopMarkPaidIfEnabled(ctx, connRepo, temporalSvc, log, in); err != nil {
		return fmt.Errorf("integration_events: whop mark-paid dispatch failed for invoice %s: %w", in.InvoiceID, err)
	}
	return nil
}

func triggerWhopMarkPaidIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderWhop)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.WhopInvoiceMarkPaidWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalWhopInvoiceMarkPaidWorkflow, input, types.SecretProviderWhop, in.InvoiceID)
}

func triggerWhopIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderWhop)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, types.SecretProviderWhop) {
		log.Info(ctx, "integration_events: invoice already synced to Whop, skipping", "invoice_id", in.InvoiceID)
		return nil
	}

	input := &temporalmodels.WhopInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalWhopInvoiceSyncWorkflow, input, types.SecretProviderWhop, in.InvoiceID)
}

var errTemporalUnavailable = fmt.Errorf("integration_events: temporal service not available")
