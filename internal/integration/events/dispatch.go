package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// DispatchOption narrows a dispatch.
type DispatchOption func(*dispatchOptions)

type dispatchOptions struct {
	onlyProvider types.SecretProvider
}

// OnlyProvider limits a dispatch to one provider: a push the user chose for one
// connection must not also reach every other connected provider.
func OnlyProvider(provider types.SecretProvider) DispatchOption {
	return func(o *dispatchOptions) { o.onlyProvider = provider }
}

func resolveDispatchOptions(opts []DispatchOption) dispatchOptions {
	var o dispatchOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// providerTrigger is one provider's sync start, named so a dispatch can keep to one provider.
type providerTrigger struct {
	provider types.SecretProvider
	run      func() error
}

// runTriggers starts each provider's sync (only the chosen provider's, when one is set)
// and gathers the failures.
func runTriggers(triggers []providerTrigger, o dispatchOptions) []error {
	var errs []error
	for _, trigger := range triggers {
		if o.onlyProvider != "" && trigger.provider != o.onlyProvider {
			continue
		}
		if err := trigger.run(); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

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
// (invoiceID, invoice, connection). This is the primary idempotency guard that prevents
// duplicate external invoices when the same Kafka message is consumed more than once
// (e.g. two consumers on the same topic, at-least-once redelivery, manual replay).
func invoiceAlreadySynced(ctx context.Context, eimRepo entityintegrationmapping.Repository, invoiceID string, conn *connection.Connection) bool {
	if eimRepo == nil {
		return false
	}
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = invoiceID
	filter.EntityType = types.IntegrationEntityTypeInvoice
	filter.ProviderTypes = []string{string(conn.ProviderType)}
	filter.ConnectionID = conn.ID
	count, err := eimRepo.Count(ctx, filter)
	return err == nil && count > 0
}

// customerAlreadySynced returns true when the entity mapping table already has a record for
// (customerID, customer, connection). Same idempotency guarantee as invoiceAlreadySynced.
func customerAlreadySynced(ctx context.Context, eimRepo entityintegrationmapping.Repository, customerID string, conn *connection.Connection) bool {
	if eimRepo == nil {
		return false
	}
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = customerID
	filter.EntityType = types.IntegrationEntityTypeCustomer
	filter.ProviderTypes = []string{string(conn.ProviderType)}
	filter.ConnectionID = conn.ID
	count, err := eimRepo.Count(ctx, filter)
	return err == nil && count > 0
}

// subscriptionAlreadySynced returns true when the entity mapping table already has a record for
// (subscriptionID, subscription, connection). Same idempotency guarantee as invoiceAlreadySynced.
func subscriptionAlreadySynced(ctx context.Context, eimRepo entityintegrationmapping.Repository, subscriptionID string, conn *connection.Connection) bool {
	if eimRepo == nil {
		return false
	}
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = subscriptionID
	filter.EntityType = types.IntegrationEntityTypeSubscription
	filter.ProviderTypes = []string{string(conn.ProviderType)}
	filter.ConnectionID = conn.ID
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
	Resync        bool
}

type customerVendorSyncInput struct {
	TenantID      string
	EnvironmentID string
	UserID        string
	CustomerID    string
}

// DispatchTabsInvoiceVendorSync starts the Tabs invoice sync workflow when a Tabs
// connection exists. Used for invoice.update; other providers sync on invoice.update.finalized.
func DispatchTabsInvoiceVendorSync(
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

	var payload webhookDto.InternalInvoiceEvent
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.InvoiceID == "" {
		log.Error(ctx, "integration_events: invalid invoice payload, dropping",
			"message_uuid", msgUUID,
			"error", err,
		)
		return nil
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}

	in := invoiceVendorSyncInput{
		TenantID:      event.TenantID,
		EnvironmentID: event.EnvironmentID,
		UserID:        event.UserID,
		InvoiceID:     payload.InvoiceID,
	}

	return triggerTabsIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in)
}

// DispatchInvoiceVendorSync parses the invoice ID from the event payload and starts
// Temporal sync workflows for each enabled provider. No DB reads are performed here —
// the invoice is fetched inside the Temporal activity after a short sleep, avoiding
// the race condition where the event arrives before the DB transaction commits.
// eimRepo is used for idempotency: if a mapping already exists the provider trigger is skipped.
func DispatchInvoiceVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	log *logger.Logger,
	event *types.WebhookEvent,
	msgUUID string,
	resync bool,
	opts ...DispatchOption,
) error {
	if cfg != nil && !cfg.IntegrationEvents.Enabled {
		return nil
	}

	// Parse invoice ID from the event payload — no DB calls at this stage.
	var payload webhookDto.InternalInvoiceEvent
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.InvoiceID == "" {
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
		InvoiceID:     payload.InvoiceID,
		Resync:        resync,
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}

	log.Info(ctx, "integration_events: dispatching invoice vendor sync",
		"invoice_id", in.InvoiceID,
		"tenant_id", in.TenantID,
		"environment_id", in.EnvironmentID,
	)

	dispatchErrs := runTriggers([]providerTrigger{
		{types.SecretProviderStripe, func() error { return triggerStripeIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
		{types.SecretProviderRazorpay, func() error { return triggerRazorpayIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
		{types.SecretProviderChargebee, func() error { return triggerChargebeeIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
		{types.SecretProviderQuickBooks, func() error { return triggerQuickBooksIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
		{types.SecretProviderHubSpot, func() error { return triggerHubSpotIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
		{types.SecretProviderMoyasar, func() error { return triggerMoyasarIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
		{types.SecretProviderNomod, func() error { return triggerNomodIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
		{types.SecretProviderPaddle, func() error { return triggerPaddleIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
		{types.SecretProviderZohoBooks, func() error { return triggerZohoBooksIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
		{types.SecretProviderWhop, func() error { return triggerWhopIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
		{types.SecretProviderTabs, func() error { return triggerTabsIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
	}, resolveDispatchOptions(opts))

	if len(dispatchErrs) > 0 {
		return fmt.Errorf("integration_events: one or more provider dispatches failed for invoice %s: %w", in.InvoiceID, errors.Join(dispatchErrs...))
	}

	return nil
}

// DispatchInvoiceAutoEmail emails a finalized invoice to the customer when the environment's
// auto-send setting covers invoices. It is gated by a configured email provider, not by integration events.
func DispatchInvoiceAutoEmail(
	ctx context.Context,
	cfg *config.Configuration,
	sender interfaces.InvoiceEmailSender,
	log *logger.Logger,
	event *types.WebhookEvent,
	msgUUID string,
) error {
	if cfg == nil || !cfg.Email.IsProviderConfigured() {
		return nil
	}

	var payload webhookDto.InternalInvoiceEvent
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.InvoiceID == "" {
		log.Error(ctx, "integration_events: invalid invoice payload, dropping auto email",
			"message_uuid", msgUUID,
			"error", err,
		)
		return nil
	}
	return sender.SendAutoInvoiceEmail(ctx, payload.InvoiceID)
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
	opts ...DispatchOption,
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

	dispatchErrs := runTriggers([]providerTrigger{
		{types.SecretProviderStripe, func() error { return triggerStripeCustomerSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
		{types.SecretProviderRazorpay, func() error {
			return triggerRazorpayCustomerSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in)
		}},
		{types.SecretProviderChargebee, func() error {
			return triggerChargebeeCustomerSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in)
		}},
		{types.SecretProviderQuickBooks, func() error {
			return triggerQuickBooksCustomerSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in)
		}},
		{types.SecretProviderNomod, func() error { return triggerNomodCustomerSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
		{types.SecretProviderPaddle, func() error { return triggerPaddleCustomerSyncIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) }},
	}, resolveDispatchOptions(opts))

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
	if subscriptionAlreadySynced(ctx, eimRepo, in.SubscriptionID, conn) {
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
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, conn) {
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
	if conn == nil {
		return nil
	}
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, conn) {
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
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, conn) {
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
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, conn) {
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
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, conn) {
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
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, conn) {
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
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, conn) {
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
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, conn) {
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
	if !in.Resync && invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, conn) {
		log.Info(ctx, "integration_events: invoice already synced to Zoho Books, skipping", "invoice_id", in.InvoiceID)
		return nil
	}
	input := &temporalmodels.ZohoBooksInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
		Resync:        in.Resync,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalZohoBooksInvoiceSyncWorkflow, input, types.SecretProviderZohoBooks, in.InvoiceID)
}

func triggerTabsIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	eimRepo entityintegrationmapping.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in invoiceVendorSyncInput,
) error {
	conn, err := getConnectionIfExists(ctx, connRepo, types.SecretProviderTabs)
	if err != nil {
		return err
	}
	if conn == nil || !conn.IsInvoiceOutboundEnabled() {
		return nil
	}
	input := &temporalmodels.TabsInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalTabsInvoiceSyncWorkflow, input, types.SecretProviderTabs, in.InvoiceID)
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
	if customerAlreadySynced(ctx, eimRepo, in.CustomerID, conn) {
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
	if customerAlreadySynced(ctx, eimRepo, in.CustomerID, conn) {
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
	if customerAlreadySynced(ctx, eimRepo, in.CustomerID, conn) {
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
	if customerAlreadySynced(ctx, eimRepo, in.CustomerID, conn) {
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
	if customerAlreadySynced(ctx, eimRepo, in.CustomerID, conn) {
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
	if customerAlreadySynced(ctx, eimRepo, in.CustomerID, conn) {
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

// DispatchInvoicePaidVendorSync starts provider-specific workflows to push the
// "invoice paid" status back to the external vendor. Currently only Whop needs this.
func DispatchInvoicePaidVendorSync(
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

	var payload webhookDto.InternalInvoiceEvent
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.InvoiceID == "" {
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
		InvoiceID:     payload.InvoiceID,
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}

	log.Info(ctx, "integration_events: dispatching invoice paid vendor sync",
		"invoice_id", in.InvoiceID,
		"tenant_id", in.TenantID,
		"environment_id", in.EnvironmentID,
	)

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
	if invoiceAlreadySynced(ctx, eimRepo, in.InvoiceID, conn) {
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
