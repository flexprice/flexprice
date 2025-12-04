package subscription

import (
	"time"

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
	if i.UserID == "" {
		return ierr.NewError("user_id is required").
			WithHint("User ID is required").
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

// BillingPeriod represents a single billing period
type BillingPeriod struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// CalculatePeriodsActivityOutput represents the output for calculating billing periods
type CalculatePeriodsActivityOutput struct {
	Periods        []BillingPeriod `json:"periods"`
	HasMorePeriods bool            `json:"has_more_periods"`
	ReachedEndDate bool            `json:"reached_end_date"`
	FinalPeriodEnd *time.Time      `json:"final_period_end,omitempty"`
}

// ProcessPeriodsActivityInput represents the input for processing billing periods
type ProcessPeriodsActivityInput struct {
	SubscriptionID string          `json:"subscription_id"`
	TenantID       string          `json:"tenant_id"`
	EnvironmentID  string          `json:"environment_id"`
	Periods        []BillingPeriod `json:"periods"`
}

// Validate validates the process periods activity input
func (i *ProcessPeriodsActivityInput) Validate() error {
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
	if len(i.Periods) == 0 {
		return ierr.NewError("periods are required").
			WithHint("At least one period is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ProcessPeriodsActivityOutput represents the output for processing billing periods
type ProcessPeriodsActivityOutput struct {
	InvoiceIDs       []string `json:"invoice_ids"`
	PeriodsProcessed int      `json:"periods_processed"`
}

// CreateInvoicesActivityInput represents the input for creating an invoice for a period
type CreateInvoicesActivityInput struct {
	SubscriptionID string    `json:"subscription_id"`
	TenantID       string    `json:"tenant_id"`
	EnvironmentID  string    `json:"environment_id"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
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

// CreateInvoicesActivityOutput represents the output for creating an invoice
type CreateInvoicesActivityOutput struct {
	InvoiceID      *string `json:"invoice_id,omitempty"`
	InvoiceCreated bool    `json:"invoice_created"`
}

// UpdateSubscriptionPeriodActivityInput represents the input for updating subscription period
type UpdateSubscriptionPeriodActivityInput struct {
	SubscriptionID string    `json:"subscription_id"`
	TenantID       string    `json:"tenant_id"`
	EnvironmentID  string    `json:"environment_id"`
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
	InvoiceID      string `json:"invoice_id"`
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

// Validate validates the sync invoice to external vendor activity input
func (i *SyncInvoiceToExternalVendorActivityInput) Validate() error {
	if i.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("Invoice ID is required").
			Mark(ierr.ErrValidation)
	}
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

// SyncInvoiceToExternalVendorActivityOutput represents the output for syncing invoice
type SyncInvoiceToExternalVendorActivityOutput struct {
	Success bool `json:"success"`
}

// AttemptPaymentActivityInput represents the input for attempting payment
type AttemptPaymentActivityInput struct {
	InvoiceID      string `json:"invoice_id"`
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

// Validate validates the attempt payment activity input
func (i *AttemptPaymentActivityInput) Validate() error {
	if i.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("Invoice ID is required").
			Mark(ierr.ErrValidation)
	}
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
	SubscriptionID string    `json:"subscription_id"`
	TenantID       string    `json:"tenant_id"`
	EnvironmentID  string    `json:"environment_id"`
	PeriodEnd      time.Time `json:"period_end"`
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
	if i.PeriodEnd.IsZero() {
		return ierr.NewError("period_end is required").
			WithHint("Period End is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CheckSubscriptionCancellationActivityOutput represents the output for checking subscription cancellation
type CheckSubscriptionCancellationActivityOutput struct {
	ShouldCancel bool       `json:"should_cancel"`
	CancelledAt  *time.Time `json:"cancelled_at,omitempty"`
}
