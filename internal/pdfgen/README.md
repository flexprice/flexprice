# Invoice PDF Generator

This package provides PDF generation functionality for FlexPrice invoices using Typst.

## Overview

The PDF generation module provides:

1. A service for generating PDF invoices from invoice data
2. A Temporal workflow for asynchronous PDF generation
3. Templates for rendering invoices in a consistent format
4. Repository integration with the Ent ORM

## Architecture

The module is structured as follows:

- `service.go` - Main service interface and implementation
- `models.go` - Data models for invoice templates
- `typst.go` - Typst renderer implementation
- `template.go` - Template management
- `repository.go` - Integration with Ent repositories
- `temporal/workflow.go` - Temporal workflow for async processing
- `assets/templates/` - Typst templates
- `assets/fonts/` - Font files

## Usage

### Generate Invoice PDF

```go
import "github.com/flexprice/flexprice/internal/pdfgen"

// Create repository
repo := pdfgen.NewEntRepository(client)

// Create renderer
renderer := pdfgen.NewTypstRenderer("/usr/bin/typst")

// Setup template manager
templateMgr := pdfgen.NewTemplateManager("/tmp/templates", "/tmp/fonts")
templateMgr.ExtractAssets()

// Create service
service := pdfgen.NewService(renderer, repo, pdfgen.ServiceConfig{
    TemplateDir:     "/tmp/templates",
    OutputDir:       "/tmp/invoices",
    FontDir:         "/tmp/fonts",
    DefaultTemplate: "invoice.typ",
})

// Generate PDF
pdfPath, err := service.GenerateInvoicePDF(ctx, "inv_123")
```

### Using Temporal Workflow

```go
// Register workflow and activities
worker := worker.New(client, pdfgen.TaskQueue, worker.Options{})
worker.RegisterWorkflow(pdfgen.InvoicePDFGenerationWorkflow)

activities := pdfgen.NewActivities(service)
worker.RegisterActivity(activities.GeneratePDF)
worker.RegisterActivity(activities.UpdateInvoicePDFLocation)

// Start worker
go worker.Run(worker.InterruptCh())

// Execute workflow
workflowOptions := client.StartWorkflowOptions{
    ID:        "pdf-generation-inv_123",
    TaskQueue: pdfgen.TaskQueue,
}

workflowRun, err := client.ExecuteWorkflow(
    context.Background(), 
    workflowOptions,
    pdfgen.InvoicePDFGenerationWorkflow,
    pdfgen.WorkflowInput{InvoiceID: "inv_123"},
)
```

## Customization

### Biller Customization

The biller information comes from the tenant/environment configuration. Each tenant can customize their:

- Company name, logo, and contact details
- Invoice styling
- Payment instructions
- VAT rates

The system first tries to fetch tenant-specific settings from the environment entity's metadata, falling back to defaults if not available.

### Template Customization

To customize the invoice template:

1. Create a new template file in `assets/templates/`
2. Update the `defaultTemplate` field in the service configuration

## Dependencies

- Typst - For PDF generation
- Temporal - For workflow orchestration
- Ent - For data access
