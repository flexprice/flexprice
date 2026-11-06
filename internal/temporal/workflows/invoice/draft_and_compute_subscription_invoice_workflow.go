package invoice

import (
	"time"

	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"github.com/flexprice/flexprice/internal/temporal/searchattr"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// ActivityCreateDraftForCurrentSubscriptionPeriod must match the registered method name.
	ActivityCreateDraftForCurrentSubscriptionPeriod = "CreateDraftForCurrentSubscriptionPeriodActivity"
	// ActivityListPastPeriodDrafts must match the registered method name.
	ActivityListPastPeriodDrafts = "ListPastPeriodDraftsActivity"
)

// DraftAndComputeSubscriptionInvoiceWorkflow creates an idempotent draft for the subscription's current period, then computes line items (same sequence as setup_draft_invoices script + ComputeInvoice).
func DraftAndComputeSubscriptionInvoiceWorkflow(
	ctx workflow.Context,
	input invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowInput,
) (*invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting draft-and-compute subscription invoice workflow",
		"subscription_id", input.SubscriptionID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return nil, err
	}

	searchattr.UpsertWorkflowSearchAttributes(ctx, map[string]interface{}{
		searchattr.SearchAttributeSubscriptionID: input.SubscriptionID,
		searchattr.SearchAttributeTenantID:       input.TenantID,
		searchattr.SearchAttributeEnvironmentID:  input.EnvironmentID,
	})

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	var invoiceIDs []string
	if input.RecomputePastPeriodDrafts {
		var pastOut invoiceModels.ListPastPeriodDraftsActivityOutput
		pastIn := invoiceModels.ListPastPeriodDraftsActivityInput{
			SubscriptionID: input.SubscriptionID,
			TenantID:       input.TenantID,
			EnvironmentID:  input.EnvironmentID,
			UserID:         input.UserID,
		}
		if err := workflow.ExecuteActivity(ctx, ActivityListPastPeriodDrafts, pastIn).Get(ctx, &pastOut); err != nil {
			logger.Error("Failed to list past-period drafts",
				"error", err,
				"subscription_id", input.SubscriptionID)
			return nil, err
		}
		invoiceIDs = append(invoiceIDs, pastOut.InvoiceIDs...)
	}

	if len(invoiceIDs) == 0 {
		logger.Info("No past-period drafts to recompute",
			"subscription_id", input.SubscriptionID)
	}

	var draftOut invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput
	draftIn := invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityInput{
		SubscriptionID:        input.SubscriptionID,
		TenantID:              input.TenantID,
		EnvironmentID:         input.EnvironmentID,
		UserID:                input.UserID,
		SkipIfAlreadyInvoiced: input.SkipIfAlreadyInvoiced,
	}
	if err := workflow.ExecuteActivity(ctx, ActivityCreateDraftForCurrentSubscriptionPeriod, draftIn).Get(ctx, &draftOut); err != nil {
		logger.Error("Failed to create draft for current subscription period",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return nil, err
	}
	if !draftOut.Skipped {
		invoiceIDs = append(invoiceIDs, draftOut.InvoiceID)
	}

	if len(invoiceIDs) == 0 {
		logger.Info("Draft-and-compute subscription invoice workflow skipped: current period already invoiced",
			"subscription_id", input.SubscriptionID)
		return &invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowResult{
			ComputeSkipped: true,
			Success:        true,
			CompletedAt:    workflow.Now(ctx),
		}, nil
	}

	// One draft failing must not stop the rest; the first error fails the workflow at the end.
	computeSkipped := draftOut.Skipped
	var computeErr error
	for _, invoiceID := range invoiceIDs {
		var computeOut invoiceModels.ComputeInvoiceActivityOutput
		computeIn := invoiceModels.ComputeInvoiceActivityInput{
			InvoiceID:     invoiceID,
			TenantID:      input.TenantID,
			EnvironmentID: input.EnvironmentID,
			UserID:        input.UserID,
		}
		if err := workflow.ExecuteActivity(ctx, ActivityComputeInvoice, computeIn).Get(ctx, &computeOut); err != nil {
			logger.Error("Failed to compute invoice",
				"error", err,
				"invoice_id", invoiceID)
			if computeErr == nil {
				computeErr = err
			}
			continue
		}
		if invoiceID == draftOut.InvoiceID {
			computeSkipped = computeOut.Skipped
		}
	}
	if computeErr != nil {
		return nil, computeErr
	}

	logger.Info("Draft-and-compute subscription invoice workflow completed",
		"subscription_id", input.SubscriptionID,
		"invoice_id", draftOut.InvoiceID,
		"computed_invoices", len(invoiceIDs),
		"compute_skipped", computeSkipped)

	return &invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowResult{
		InvoiceID:      draftOut.InvoiceID,
		ComputeSkipped: computeSkipped,
		Success:        true,
		CompletedAt:    workflow.Now(ctx),
	}, nil
}
