package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/logger"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
)

// InvoiceVendorSyncInput is the minimal data needed to fan out invoice sync workflows.
type InvoiceVendorSyncInput struct {
	TenantID         string
	EnvironmentID    string
	UserID           string
	InvoiceID        string
	CustomerID       string
	CollectionMethod string
}

// CustomerVendorSyncInput is the minimal data needed to fan out customer sync workflows.
type CustomerVendorSyncInput struct {
	TenantID      string
	EnvironmentID string
	UserID        string
	CustomerID    string
}

// DispatchInvoiceVendorSync starts Temporal invoice-sync workflows for each enabled provider.
// Used by the integration consumer (on invoice.update.finalized) and by manual SyncInvoiceToExternalVendors.
func DispatchInvoiceVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) error {
	if cfg != nil && !cfg.IntegrationEvents.Enabled {
		return nil
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return errTemporalUnavailable
	}

	if in.InvoiceID == "" || in.CustomerID == "" {
		return nil
	}

	log.Infow("integration_events: dispatching invoice vendor sync",
		"invoice_id", in.InvoiceID,
		"customer_id", in.CustomerID,
		"tenant_id", in.TenantID,
		"environment_id", in.EnvironmentID,
	)

	var dispatchErrs []error
	for _, trigger := range []func() error{
		func() error { return triggerStripeIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerRazorpayIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerChargebeeIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerQuickBooksIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerHubSpotIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerMoyasarIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerNomodIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerPaddleIfEnabled(ctx, connRepo, temporalSvc, log, in) },
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
func DispatchCustomerVendorSync(
	ctx context.Context,
	cfg *config.Configuration,
	connRepo connection.Repository,
	log *logger.Logger,
	in CustomerVendorSyncInput,
) error {
	if cfg != nil && !cfg.IntegrationEvents.Enabled {
		return nil
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
		func() error { return triggerStripeCustomerSyncIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerRazorpayCustomerSyncIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerChargebeeCustomerSyncIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerQuickBooksCustomerSyncIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerNomodCustomerSyncIfEnabled(ctx, connRepo, temporalSvc, log, in) },
		func() error { return triggerPaddleCustomerSyncIfEnabled(ctx, connRepo, temporalSvc, log, in) },
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
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderStripe, err)
	}
	if conn == nil {
		return nil
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.StripeInvoiceSyncWorkflowInput{
		InvoiceID:        in.InvoiceID,
		CustomerID:       in.CustomerID,
		CollectionMethod: in.CollectionMethod,
		TenantID:         in.TenantID,
		EnvironmentID:    in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalStripeInvoiceSyncWorkflow, input, types.SecretProviderStripe, in.InvoiceID)
}

func triggerRazorpayIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderRazorpay)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderRazorpay, err)
	}
	if conn == nil {
		return nil
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.RazorpayInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalRazorpayInvoiceSyncWorkflow, input, types.SecretProviderRazorpay, in.InvoiceID)
}

func triggerChargebeeIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderChargebee)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderChargebee, err)
	}
	if conn == nil {
		return nil
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.ChargebeeInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalChargebeeInvoiceSyncWorkflow, input, types.SecretProviderChargebee, in.InvoiceID)
}

func triggerQuickBooksIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderQuickBooks)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderQuickBooks, err)
	}
	if conn == nil {
		return nil
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.QuickBooksInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalQuickBooksInvoiceSyncWorkflow, input, types.SecretProviderQuickBooks, in.InvoiceID)
}

func triggerHubSpotIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderHubSpot)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderHubSpot, err)
	}
	if conn == nil {
		return nil
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.HubSpotInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalHubSpotInvoiceSyncWorkflow, input, types.SecretProviderHubSpot, in.InvoiceID)
}

func triggerMoyasarIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderMoyasar)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderMoyasar, err)
	}
	if conn == nil {
		return nil
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.MoyasarInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalMoyasarInvoiceSyncWorkflow, input, types.SecretProviderMoyasar, in.InvoiceID)
}

func triggerNomodIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderNomod)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderNomod, err)
	}
	if conn == nil {
		return nil
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.NomodInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalNomodInvoiceSyncWorkflow, input, types.SecretProviderNomod, in.InvoiceID)
}

func triggerPaddleIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in InvoiceVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderPaddle)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderPaddle, err)
	}
	if conn == nil {
		return nil
	}
	if !conn.IsInvoiceOutboundEnabled() {
		return nil
	}

	input := &temporalmodels.PaddleInvoiceSyncWorkflowInput{
		InvoiceID:     in.InvoiceID,
		CustomerID:    in.CustomerID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
	}
	return executeWorkflow(ctx, temporalSvc, log, types.TemporalPaddleInvoiceSyncWorkflow, input, types.SecretProviderPaddle, in.InvoiceID)
}

func triggerStripeCustomerSyncIfEnabled(
	ctx context.Context,
	connRepo connection.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in CustomerVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderStripe, err)
	}
	if conn == nil || !conn.IsCustomerOutboundEnabled() {
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
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in CustomerVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderRazorpay)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderRazorpay, err)
	}
	if conn == nil || !conn.IsCustomerOutboundEnabled() {
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
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in CustomerVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderChargebee)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderChargebee, err)
	}
	if conn == nil || !conn.IsCustomerOutboundEnabled() {
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
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in CustomerVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderQuickBooks)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderQuickBooks, err)
	}
	if conn == nil || !conn.IsCustomerOutboundEnabled() {
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
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in CustomerVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderNomod)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderNomod, err)
	}
	if conn == nil || !conn.IsCustomerOutboundEnabled() {
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
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
	in CustomerVendorSyncInput,
) error {
	conn, err := connRepo.GetByProvider(ctx, types.SecretProviderPaddle)
	if err != nil {
		return fmt.Errorf("provider %s lookup failed: %w", types.SecretProviderPaddle, err)
	}
	if conn == nil || !conn.IsCustomerOutboundEnabled() {
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
