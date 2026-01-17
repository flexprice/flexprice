package subscription

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ProcessSubscriptionBillingWorkflowInput represents the input for processing a single subscription billing workflow
type ProcessSubscriptionBillingWorkflowInput struct {
	SubscriptionID string    `json:"subscription_id"`
	TenantID       string    `json:"tenant_id"`
	UserID         string    `json:"user_id"`
	EnvironmentID  string    `json:"environment_id"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
}

// Validate validates the process subscription billing workflow input
func (i *ProcessSubscriptionBillingWorkflowInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.PeriodStart.IsZero() {
		return ierr.NewError("period_start is required").
			WithHint("Period Start is required").
			Mark(ierr.ErrValidation)
	}
	if i.PeriodEnd.IsZero() {
		return ierr.NewError("period_end is required").
			WithHint("Period End is required").
			Mark(ierr.ErrValidation)
	}
	if i.PeriodEnd.Before(i.PeriodStart) {
		return ierr.NewError("period_end must be after period_start").
			WithHint("Period End must be after Period Start").
			WithReportableDetails(map[string]any{
				"period_start": i.PeriodStart,
				"period_end":   i.PeriodEnd,
			}).Mark(ierr.ErrValidation)
	}
	return nil
}

type ProcessSubscriptionBillingWorkflowResult struct {
	Success     bool      `json:"success"`
	Error       *string   `json:"error,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
}

type CheckDraftSubscriptionActivityInput struct {
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
	UserID         string `json:"user_id"`
}

// Validate validates the check draft subscription activity input
func (i *CheckDraftSubscriptionActivityInput) Validate() error {

	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

type CheckDraftSubscriptionActivityOutput struct {
	IsDraft bool `json:"is_draft"`
}

// CheckSubscriptionPauseStatusActivity input
type CheckSubscriptionPauseStatusActivityInput struct {
	SubscriptionID string    `json:"subscription_id"`
	TenantID       string    `json:"tenant_id"`
	EnvironmentID  string    `json:"environment_id"`
	CurrentTime    time.Time `json:"current_time"`
}

// Validate validates the check subscription pause status activity input
func (i *CheckSubscriptionPauseStatusActivityInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.CurrentTime.IsZero() {
		return ierr.NewError("current_time is required").
			WithHint("Current Time is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CheckSubscriptionPauseStatusActivity output
type CheckSubscriptionPauseStatusActivityOutput struct {
	ShouldSkipProcessing bool       `json:"should_skip_processing"`
	IsPaused             bool       `json:"is_paused"`
	WasResumed           bool       `json:"was_resumed"`
	UpdatedPeriodEnd     *time.Time `json:"updated_period_end,omitempty"`
	PauseID              *string    `json:"pause_id,omitempty"`
}

// CalculatePeriodsActivityInput represents the input for calculating billing periods
type CalculatePeriodsActivityInput struct {
	SubscriptionID string    `json:"subscription_id"`
	TenantID       string    `json:"tenant_id"`
	EnvironmentID  string    `json:"environment_id"`
	UserID         string    `json:"user_id"`
	CurrentTime    time.Time `json:"current_time"`
}

// Validate validates the calculate periods activity input
func (i *CalculatePeriodsActivityInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.CurrentTime.IsZero() {
		return ierr.NewError("current_time is required").
			WithHint("Current Time is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CalculatePeriodsActivityOutput represents the output for calculating billing periods
type CalculatePeriodsActivityOutput struct {
	Periods       []dto.Period `json:"periods"`
	ShouldProcess bool         `json:"should_process"`
}

// CreateInvoicesActivityInput represents the input for creating an invoice for a period
type CreateInvoicesActivityInput struct {
	SubscriptionID string       `json:"subscription_id"`
	TenantID       string       `json:"tenant_id"`
	EnvironmentID  string       `json:"environment_id"`
	UserID         string       `json:"user_id"`
	Periods        []dto.Period `json:"periods"`
}

// Validate validates the create invoices activity input
func (i *CreateInvoicesActivityInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CreateInvoicesActivityOutput represents the output for creating an invoice
type CreateInvoicesActivityOutput struct {
	InvoiceIDs []string `json:"invoice_ids"`
}

// UpdateSubscriptionPeriodActivityInput represents the input for updating subscription period
type UpdateSubscriptionPeriodActivityInput struct {
	SubscriptionID string    `json:"subscription_id"`
	TenantID       string    `json:"tenant_id"`
	EnvironmentID  string    `json:"environment_id"`
	UserID         string    `json:"user_id"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
}

// Validate validates the update subscription period activity input
func (i *UpdateSubscriptionPeriodActivityInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.PeriodStart.IsZero() {
		return ierr.NewError("period_start is required").
			WithHint("Period Start is required").
			Mark(ierr.ErrValidation)
	}
	if i.PeriodEnd.IsZero() {
		return ierr.NewError("period_end is required").
			WithHint("Period End is required").
			Mark(ierr.ErrValidation)
	}
	if i.PeriodEnd.Before(i.PeriodStart) {
		return ierr.NewError("period_end must be after period_start").
			WithHint("Period End must be after Period Start").
			WithReportableDetails(map[string]any{
				"period_start": i.PeriodStart,
				"period_end":   i.PeriodEnd,
			}).Mark(ierr.ErrValidation)
	}
	return nil
}

// UpdateSubscriptionPeriodActivityOutput represents the output for updating subscription period
type UpdateSubscriptionPeriodActivityOutput struct {
	Success bool `json:"success"`
}

// SyncInvoiceToExternalVendorActivityInput represents the input for syncing invoice to external vendor
type SyncInvoiceToExternalVendorActivityInput struct {
	InvoiceIDs    []string `json:"invoice_ids"`
	TenantID      string   `json:"tenant_id"`
	EnvironmentID string   `json:"environment_id"`
}

// Validate validates the sync invoice to external vendor activity input
func (i *SyncInvoiceToExternalVendorActivityInput) Validate() error {
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// SyncInvoiceToExternalVendorActivityOutput represents the output for syncing invoice
type SyncInvoiceToExternalVendorActivityOutput struct {
	Success bool `json:"success"`
}

// AttemptPaymentActivityInput represents the input for attempting payment
type AttemptPaymentActivityInput struct {
	SubscriptionID string   `json:"subscription_id"`
	TenantID       string   `json:"tenant_id"`
	EnvironmentID  string   `json:"environment_id"`
	InvoiceIDs     []string `json:"invoice_ids"`
}

// Validate validates the attempt payment activity input
func (i *AttemptPaymentActivityInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// AttemptPaymentActivityOutput represents the output for attempting payment
type AttemptPaymentActivityOutput struct {
	Success bool `json:"success"`
}

// CheckSubscriptionCancellationActivityInput represents the input for checking subscription cancellation
type CheckSubscriptionCancellationActivityInput struct {
	SubscriptionID string     `json:"subscription_id"`
	TenantID       string     `json:"tenant_id"`
	EnvironmentID  string     `json:"environment_id"`
	UserID         string     `json:"user_id"`
	Period         dto.Period `json:"period"`
}

// Validate validates the check subscription cancellation activity input
func (i *CheckSubscriptionCancellationActivityInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CheckSubscriptionCancellationActivityOutput represents the output for checking subscription cancellation
type CheckSubscriptionCancellationActivityOutput struct {
	IsCancelled bool `json:"is_cancelled"`
	Success     bool `json:"success"`
}

// ProcessPendingPlanChangesActivityInput represents the input for processing pending plan changes
type ProcessPendingPlanChangesActivityInput struct {
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
	UserID         string `json:"user_id"`
}

// Validate validates the process pending plan changes activity input
func (i *ProcessPendingPlanChangesActivityInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ProcessPendingPlanChangesActivityOutput represents the output for processing pending plan changes
type ProcessPendingPlanChangesActivityOutput struct {
	Success    bool `json:"success"`
	WasChanged bool `json:"was_changed"`
}
