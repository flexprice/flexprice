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
	"github.com/flexprice/flexprice/internal/logger"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
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
) error {
	if cfg != nil && !cfg.IntegrationEvents.Enabled {
		return nil
	}

	// Parse invoice ID from the event payload — no DB calls at this stage.
	var pl struct {
		InvoiceID string `json:"invoice_id"`
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil || pl.InvoiceID == "" {
		log.Errorw("integration_events: invalid invoice payload, dropping",
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

	log.Infow("integration_events: dispatching invoice vendor sync",
		"invoice_id", in.InvoiceID,
		"tenant_id", in.TenantID,
		"environment_id", in.EnvironmentID,
	)

	var dispatchErrs []error
	for _, trigger := range []func() error{
		func() error { return triggerStripeIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		func() error { return triggerRazorpayIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		func() error { return triggerChargebeeIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		func() error { return triggerQuickBooksIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		func() error { return triggerHubSpotIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		func() error { return triggerMoyasarIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		func() error { return triggerNomodIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
		func() error { return triggerPaddleIfEnabled(ctx, connRepo, eimRepo, temporalSvc, log, in) },
	} {
		if err := trigger(); err != nil {
			dispatchErrs = append(dispatchErrs, err)
		}
	}

	if len(dispatchErrs) > 0 {
		return fmt.Errorf("integration_events: one or more provider dispatches failed for invoice %s: %w", in.InvoiceID, errors.Join(dispatchErrs...))
	}

	return nil
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
		log.Errorw("integration_events: invalid customer payload, dropping",
			"message_uuid", msgUUID,
			"error", err,
		)
		return nil
	}

	if payload.CustomerID == "" {
		log.Warnw("integration_events: customer payload missing customer_id, dropping",
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

	log.Infow("integration_events: dispatching customer vendor sync",
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
		log.Errorw("integration_events: failed to start workflow",
			"provider", provider,
			"workflow_type", workflowType,
			"invoice_id", invoiceID,
			"error", err,
		)
		return fmt.Errorf("provider %s workflow start failed: %w", provider, err)
	}

	log.Infow("integration_events: workflow started",
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
		log.Errorw("integration_events: failed to start workflow",
			"provider", provider,
			"workflow_type", workflowType,
			"customer_id", customerID,
			"error", err,
		)
		return fmt.Errorf("provider %s workflow start failed: %w", provider, err)
	}

	log.Infow("integration_events: workflow started",
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
		log.Infow("integration_events: invoice already synced to Stripe, skipping", "invoice_id", in.InvoiceID)
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
		log.Infow("integration_events: invoice already synced to Razorpay, skipping", "invoice_id", in.InvoiceID)
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
		log.Infow("integration_events: invoice already synced to Chargebee, skipping", "invoice_id", in.InvoiceID)
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
		log.Infow("integration_events: invoice already synced to QuickBooks, skipping", "invoice_id", in.InvoiceID)
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
		log.Infow("integration_events: invoice already synced to HubSpot, skipping", "invoice_id", in.InvoiceID)
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
		log.Infow("integration_events: invoice already synced to Moyasar, skipping", "invoice_id", in.InvoiceID)
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
		log.Infow("integration_events: invoice already synced to Nomod, skipping", "invoice_id", in.InvoiceID)
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
		log.Infow("integration_events: invoice already synced to Paddle, skipping", "invoice_id", in.InvoiceID)
		return nil
	}

	input := &temporalmodels.PaddleInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalPaddleInvoiceSyncWorkflow, input, types.SecretProviderPaddle, in.InvoiceID)
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
		log.Infow("integration_events: customer already synced to Stripe, skipping", "customer_id", in.CustomerID)
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
		log.Infow("integration_events: customer already synced to Razorpay, skipping", "customer_id", in.CustomerID)
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
		log.Infow("integration_events: customer already synced to Chargebee, skipping", "customer_id", in.CustomerID)
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
		log.Infow("integration_events: customer already synced to QuickBooks, skipping", "customer_id", in.CustomerID)
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
		log.Infow("integration_events: customer already synced to Nomod, skipping", "customer_id", in.CustomerID)
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
		log.Infow("integration_events: customer already synced to Paddle, skipping", "customer_id", in.CustomerID)
		return nil
	}
	input := &temporalmodels.PaddleCustomerSyncWorkflowInput{
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeCustomerWorkflow(ctx, temporalSvc, log, types.TemporalPaddleCustomerSyncWorkflow, input, types.SecretProviderPaddle, in.CustomerID)
}

var errTemporalUnavailable = fmt.Errorf("integration_events: temporal service not available")
