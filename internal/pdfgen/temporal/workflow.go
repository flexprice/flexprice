package temporal

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/pdfgen"
	"github.com/pkg/errors"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// TaskQueue is the name of the task queue for invoice PDF workflows
	TaskQueue = "invoice-pdf-generation"

	// WorkflowName is the name of the invoice PDF generation workflow
	WorkflowName = "InvoicePDFGenerationWorkflow"

	// ActivityPrefix is the prefix for all activity names
	ActivityPrefix = "InvoicePDFGeneration_"
)

// Activities defines the interface for invoice PDF generation activities
type Activities struct {
	service pdfgen.Service
}

// NewActivities creates a new instance of the invoice PDF generation activities
func NewActivities(service pdfgen.Service) *Activities {
	return &Activities{
		service: service,
	}
}

// WorkflowInput represents the input to the invoice PDF generation workflow
type WorkflowInput struct {
	InvoiceID string `json:"invoice_id"`
}

// WorkflowOutput represents the output from the invoice PDF generation workflow
type WorkflowOutput struct {
	InvoiceID   string `json:"invoice_id"`
	PDFLocation string `json:"pdf_location"`
	Error       string `json:"error,omitempty"`
}

// InvoicePDFGenerationWorkflow is the workflow for generating invoice PDFs
func InvoicePDFGenerationWorkflow(ctx workflow.Context, input WorkflowInput) (*WorkflowOutput, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting invoice PDF generation workflow", "invoiceID", input.InvoiceID)

	output := &WorkflowOutput{
		InvoiceID: input.InvoiceID,
	}

	// Activity options with retry policy
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}

	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Execute the PDF generation activity
	var pdfLocation string
	err := workflow.ExecuteActivity(ctx, ActivityPrefix+"GeneratePDF", input.InvoiceID).Get(ctx, &pdfLocation)
	if err != nil {
		output.Error = err.Error()
		logger.Error("Failed to generate PDF", "error", err)
		return output, err
	}

	output.PDFLocation = pdfLocation

	// Execute the invoice update activity to store the PDF location
	err = workflow.ExecuteActivity(ctx, ActivityPrefix+"UpdateInvoicePDFLocation", input.InvoiceID, pdfLocation).Get(ctx, nil)
	if err != nil {
		output.Error = err.Error()
		logger.Error("Failed to update invoice PDF location", "error", err)
		return output, err
	}

	logger.Info("Successfully completed invoice PDF generation workflow", "invoiceID", input.InvoiceID)
	return output, nil
}

// GeneratePDF generates the PDF for an invoice
func (a *Activities) GeneratePDF(ctx context.Context, invoiceID string) (string, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("Generating PDF for invoice", "invoiceID", invoiceID)

	pdfPath, err := a.service.GenerateInvoicePDF(ctx, invoiceID)
	if err != nil {
		return "", errors.Wrap(err, "failed to generate PDF")
	}

	return pdfPath, nil
}

// UpdateInvoicePDFLocation updates the invoice with the generated PDF location
func (a *Activities) UpdateInvoicePDFLocation(ctx context.Context, invoiceID, pdfLocation string) error {
	logger := activity.GetLogger(ctx)
	logger.Info("Updating invoice PDF location", "invoiceID", invoiceID, "pdfLocation", pdfLocation)

	// Implement updating the invoice in the database
	// This would typically call an invoice repository or service method

	return nil
}
