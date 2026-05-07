package v1

import (
	apidto "github.com/flexprice/flexprice/internal/api/dto"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// Doc-only webhook event stubs for OpenAPI / SDK generation. These handlers are not registered on the router.
// Payload types live in internal/webhook/dto (package webhookDto).
//
// Anchor webhookDto types so swag can resolve @Success references in this file.
var _ = []any{
	(*apidto.RetryOutboundWebhookRequest)(nil),
	(*apidto.RetryOutboundWebhookResponse)(nil),
	(*webhookDto.InvoiceWebhookPayload)(nil),
	(*webhookDto.CommunicationWebhookPayload)(nil),
	(*webhookDto.SubscriptionWebhookPayload)(nil),
	(*webhookDto.SubscriptionPhaseWebhookPayload)(nil),
	(*webhookDto.CustomerWebhookPayload)(nil),
	(*webhookDto.PaymentWebhookPayload)(nil),
	(*webhookDto.FeatureWebhookPayload)(nil),
	(*webhookDto.AlertWebhookPayload)(nil),
	(*webhookDto.EntitlementWebhookPayload)(nil),
	(*webhookDto.WalletWebhookPayload)(nil),
	(*webhookDto.TransactionWebhookPayload)(nil),
	(*webhookDto.CreditNoteWebhookPayload)(nil),
}

// WebhookEventInvoiceUpdateFinalized godoc
// @Summary invoice.update.finalized
// @Description Fired when an invoice is finalized and locked for payment. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.InvoiceWebhookPayload "Webhook payload"
// @Router /webhook-events/invoice.update.finalized [post]
func WebhookEventInvoiceUpdateFinalized() {}

// WebhookEventInvoiceUpdateVoided godoc
// @Summary invoice.update.voided
// @Description Fired when an invoice is voided (e.g. order cancelled or duplicate). Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.InvoiceWebhookPayload "Webhook payload"
// @Router /webhook-events/invoice.update.voided [post]
func WebhookEventInvoiceUpdateVoided() {}

// WebhookEventInvoiceUpdatePayment godoc
// @Summary invoice.update.payment
// @Description Fired when an invoice payment status changes. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.InvoiceWebhookPayload "Webhook payload"
// @Router /webhook-events/invoice.update.payment [post]
func WebhookEventInvoiceUpdatePayment() {}

// WebhookEventInvoiceUpdate godoc
// @Summary invoice.update
// @Description Fired when an invoice is updated. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.InvoiceWebhookPayload "Webhook payload"
// @Router /webhook-events/invoice.update [post]
func WebhookEventInvoiceUpdate() {}

// WebhookEventInvoicePaymentOverdue godoc
// @Summary invoice.payment.overdue
// @Description Fired when an invoice payment is overdue past the due date. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.InvoiceWebhookPayload "Webhook payload"
// @Router /webhook-events/invoice.payment.overdue [post]
func WebhookEventInvoicePaymentOverdue() {}

// WebhookEventInvoiceCommunicationTriggered godoc
// @Summary invoice.communication.triggered
// @Description Fired when an invoice communication (e.g. email notification) is triggered. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.CommunicationWebhookPayload "Webhook payload"
// @Router /webhook-events/invoice.communication.triggered [post]
func WebhookEventInvoiceCommunicationTriggered() {}

// WebhookEventSubscriptionCreated godoc
// @Summary subscription.created
// @Description Fired when a new subscription is created. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.SubscriptionWebhookPayload "Webhook payload"
// @Router /webhook-events/subscription.created [post]
func WebhookEventSubscriptionCreated() {}

// WebhookEventSubscriptionDraftCreated godoc
// @Summary subscription.draft.created
// @Description Fired when a new draft subscription is created (not yet activated). Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.SubscriptionWebhookPayload "Webhook payload"
// @Router /webhook-events/subscription.draft.created [post]
func WebhookEventSubscriptionDraftCreated() {}

// WebhookEventSubscriptionActivated godoc
// @Summary subscription.activated
// @Description Fired when a draft subscription is activated. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.SubscriptionWebhookPayload "Webhook payload"
// @Router /webhook-events/subscription.activated [post]
func WebhookEventSubscriptionActivated() {}

// WebhookEventSubscriptionUpdated godoc
// @Summary subscription.updated
// @Description Fired when a subscription is updated (e.g. quantity, billing anchor, or metadata changes). Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.SubscriptionWebhookPayload "Webhook payload"
// @Router /webhook-events/subscription.updated [post]
func WebhookEventSubscriptionUpdated() {}

// WebhookEventSubscriptionPaused godoc
// @Summary subscription.paused
// @Description Fired when a subscription is paused. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.SubscriptionWebhookPayload "Webhook payload"
// @Router /webhook-events/subscription.paused [post]
func WebhookEventSubscriptionPaused() {}

// WebhookEventSubscriptionCancelled godoc
// @Summary subscription.cancelled
// @Description Fired when a subscription is cancelled (immediately or end-of-period). Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.SubscriptionWebhookPayload "Webhook payload"
// @Router /webhook-events/subscription.cancelled [post]
func WebhookEventSubscriptionCancelled() {}

// WebhookEventSubscriptionResumed godoc
// @Summary subscription.resumed
// @Description Fired when a paused subscription is resumed. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.SubscriptionWebhookPayload "Webhook payload"
// @Router /webhook-events/subscription.resumed [post]
func WebhookEventSubscriptionResumed() {}

// WebhookEventSubscriptionRenewalDue godoc
// @Summary subscription.renewal.due
// @Description Fired when a subscription renewal is upcoming (cron-driven). Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.SubscriptionWebhookPayload "Webhook payload"
// @Router /webhook-events/subscription.renewal.due [post]
func WebhookEventSubscriptionRenewalDue() {}

// WebhookEventSubscriptionPhaseCreated godoc
// @Summary subscription.phase.created
// @Description Fired when a new subscription phase is created. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.SubscriptionPhaseWebhookPayload "Webhook payload"
// @Router /webhook-events/subscription.phase.created [post]
func WebhookEventSubscriptionPhaseCreated() {}

// WebhookEventSubscriptionPhaseUpdated godoc
// @Summary subscription.phase.updated
// @Description Fired when a subscription phase is updated. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.SubscriptionPhaseWebhookPayload "Webhook payload"
// @Router /webhook-events/subscription.phase.updated [post]
func WebhookEventSubscriptionPhaseUpdated() {}

// WebhookEventSubscriptionPhaseDeleted godoc
// @Summary subscription.phase.deleted
// @Description Fired when a subscription phase is deleted. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.SubscriptionPhaseWebhookPayload "Webhook payload"
// @Router /webhook-events/subscription.phase.deleted [post]
func WebhookEventSubscriptionPhaseDeleted() {}

// WebhookEventCustomerCreated godoc
// @Summary customer.created
// @Description Fired when a new customer is created. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.CustomerWebhookPayload "Webhook payload"
// @Router /webhook-events/customer.created [post]
func WebhookEventCustomerCreated() {}

// WebhookEventCustomerUpdated godoc
// @Summary customer.updated
// @Description Fired when a customer is updated. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.CustomerWebhookPayload "Webhook payload"
// @Router /webhook-events/customer.updated [post]
func WebhookEventCustomerUpdated() {}

// WebhookEventCustomerDeleted godoc
// @Summary customer.deleted
// @Description Fired when a customer is deleted. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.CustomerWebhookPayload "Webhook payload"
// @Router /webhook-events/customer.deleted [post]
func WebhookEventCustomerDeleted() {}

// WebhookEventPaymentCreated godoc
// @Summary payment.created
// @Description Fired when a new payment is created. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.PaymentWebhookPayload "Webhook payload"
// @Router /webhook-events/payment.created [post]
func WebhookEventPaymentCreated() {}

// WebhookEventPaymentUpdated godoc
// @Summary payment.updated
// @Description Fired when a payment is updated. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.PaymentWebhookPayload "Webhook payload"
// @Router /webhook-events/payment.updated [post]
func WebhookEventPaymentUpdated() {}

// WebhookEventPaymentSuccess godoc
// @Summary payment.success
// @Description Fired when a payment succeeds. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.PaymentWebhookPayload "Webhook payload"
// @Router /webhook-events/payment.success [post]
func WebhookEventPaymentSuccess() {}

// WebhookEventPaymentFailed godoc
// @Summary payment.failed
// @Description Fired when a payment fails. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.PaymentWebhookPayload "Webhook payload"
// @Router /webhook-events/payment.failed [post]
func WebhookEventPaymentFailed() {}

// WebhookEventPaymentPending godoc
// @Summary payment.pending
// @Description Fired when a payment is pending processing. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.PaymentWebhookPayload "Webhook payload"
// @Router /webhook-events/payment.pending [post]
func WebhookEventPaymentPending() {}

// WebhookEventFeatureCreated godoc
// @Summary feature.created
// @Description Fired when a new feature is created. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.FeatureWebhookPayload "Webhook payload"
// @Router /webhook-events/feature.created [post]
func WebhookEventFeatureCreated() {}

// WebhookEventFeatureUpdated godoc
// @Summary feature.updated
// @Description Fired when a feature is updated. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.FeatureWebhookPayload "Webhook payload"
// @Router /webhook-events/feature.updated [post]
func WebhookEventFeatureUpdated() {}

// WebhookEventFeatureDeleted godoc
// @Summary feature.deleted
// @Description Fired when a feature is deleted. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.FeatureWebhookPayload "Webhook payload"
// @Router /webhook-events/feature.deleted [post]
func WebhookEventFeatureDeleted() {}

// WebhookEventFeatureWalletBalanceAlert godoc
// @Summary feature.wallet_balance.alert
// @Description Fired when a feature's associated wallet balance crosses an alert threshold. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.AlertWebhookPayload "Webhook payload"
// @Router /webhook-events/feature.wallet_balance.alert [post]
func WebhookEventFeatureWalletBalanceAlert() {}

// WebhookEventEntitlementCreated godoc
// @Summary entitlement.created
// @Description Fired when a new entitlement is created. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.EntitlementWebhookPayload "Webhook payload"
// @Router /webhook-events/entitlement.created [post]
func WebhookEventEntitlementCreated() {}

// WebhookEventEntitlementUpdated godoc
// @Summary entitlement.updated
// @Description Fired when an entitlement is updated. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.EntitlementWebhookPayload "Webhook payload"
// @Router /webhook-events/entitlement.updated [post]
func WebhookEventEntitlementUpdated() {}

// WebhookEventEntitlementDeleted godoc
// @Summary entitlement.deleted
// @Description Fired when an entitlement is deleted. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.EntitlementWebhookPayload "Webhook payload"
// @Router /webhook-events/entitlement.deleted [post]
func WebhookEventEntitlementDeleted() {}

// WebhookEventWalletCreated godoc
// @Summary wallet.created
// @Description Fired when a new wallet is created. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.WalletWebhookPayload "Webhook payload"
// @Router /webhook-events/wallet.created [post]
func WebhookEventWalletCreated() {}

// WebhookEventWalletUpdated godoc
// @Summary wallet.updated
// @Description Fired when a wallet is updated. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.WalletWebhookPayload "Webhook payload"
// @Router /webhook-events/wallet.updated [post]
func WebhookEventWalletUpdated() {}

// WebhookEventWalletTerminated godoc
// @Summary wallet.terminated
// @Description Fired when a wallet is terminated. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.WalletWebhookPayload "Webhook payload"
// @Router /webhook-events/wallet.terminated [post]
func WebhookEventWalletTerminated() {}

// WebhookEventWalletTransactionCreated godoc
// @Summary wallet.transaction.created
// @Description Fired when a new wallet transaction is created (top-up, deduction, etc.). Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.TransactionWebhookPayload "Webhook payload"
// @Router /webhook-events/wallet.transaction.created [post]
func WebhookEventWalletTransactionCreated() {}

// WebhookEventWalletCreditBalanceDropped godoc
// @Summary wallet.credit_balance.dropped
// @Description Fired when a wallet's credit balance drops below an alert threshold. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.WalletWebhookPayload "Webhook payload"
// @Router /webhook-events/wallet.credit_balance.dropped [post]
func WebhookEventWalletCreditBalanceDropped() {}

// WebhookEventWalletCreditBalanceRecovered godoc
// @Summary wallet.credit_balance.recovered
// @Description Fired when a wallet's credit balance recovers above an alert threshold. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.WalletWebhookPayload "Webhook payload"
// @Router /webhook-events/wallet.credit_balance.recovered [post]
func WebhookEventWalletCreditBalanceRecovered() {}

// WebhookEventWalletOngoingBalanceDropped godoc
// @Summary wallet.ongoing_balance.dropped
// @Description Fired when a wallet's ongoing balance drops below an alert threshold. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.WalletWebhookPayload "Webhook payload"
// @Router /webhook-events/wallet.ongoing_balance.dropped [post]
func WebhookEventWalletOngoingBalanceDropped() {}

// WebhookEventWalletOngoingBalanceRecovered godoc
// @Summary wallet.ongoing_balance.recovered
// @Description Fired when a wallet's ongoing balance recovers above an alert threshold. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.WalletWebhookPayload "Webhook payload"
// @Router /webhook-events/wallet.ongoing_balance.recovered [post]
func WebhookEventWalletOngoingBalanceRecovered() {}

// WebhookEventCreditNoteCreated godoc
// @Summary credit_note.created
// @Description Fired when a new credit note is created. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.CreditNoteWebhookPayload "Webhook payload"
// @Router /webhook-events/credit_note.created [post]
func WebhookEventCreditNoteCreated() {}

// WebhookEventCreditNoteUpdated godoc
// @Summary credit_note.updated
// @Description Fired when a credit note is updated. Doc-only for parsing.
// @Tags Webhook Events
// @Accept json
// @Produce json
// @Success 200 {object} webhookDto.CreditNoteWebhookPayload "Webhook payload"
// @Router /webhook-events/credit_note.updated [post]
func WebhookEventCreditNoteUpdated() {}
