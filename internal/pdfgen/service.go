package pdfgen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

// Service defines the interface for PDF generation operations
type Service interface {
	// GenerateInvoicePDF generates a PDF for the given invoice ID
	GenerateInvoicePDF(ctx context.Context, invoiceID string) (string, error)
	
	// RenderInvoice renders an invoice template with the provided data
	RenderInvoice(ctx context.Context, data *InvoiceData) ([]byte, error)
	
	// GetInvoiceData retrieves invoice data for PDF generation
	GetInvoiceData(ctx context.Context, invoiceID string) (*InvoiceData, error)
}

// ServiceImpl implements the Service interface
type ServiceImpl struct {
	renderer        *TypstRenderer
	invoiceRepo     InvoiceRepository
	templateDir     string
	outputDir       string
	fontDir         string
	defaultTemplate string
}

// InvoiceRepository defines the interface for retrieving invoice data
type InvoiceRepository interface {
	GetInvoiceWithLineItems(ctx context.Context, invoiceID string) (*InvoiceData, error)
}

// NewService creates a new PDF generation service
func NewService(renderer *TypstRenderer, invoiceRepo InvoiceRepository, config ServiceConfig) Service {
	return &ServiceImpl{
		renderer:        renderer,
		invoiceRepo:     invoiceRepo,
		templateDir:     config.TemplateDir,
		outputDir:       config.OutputDir,
		fontDir:         config.FontDir,
		defaultTemplate: config.DefaultTemplate,
	}
}

// ServiceConfig contains configuration for the PDF service
type ServiceConfig struct {
	TemplateDir     string
	OutputDir       string
	FontDir         string
	DefaultTemplate string
}

// GenerateInvoicePDF generates a PDF for the given invoice ID
func (s *ServiceImpl) GenerateInvoicePDF(ctx context.Context, invoiceID string) (string, error) {
	// Get invoice data
	data, err := s.GetInvoiceData(ctx, invoiceID)
	if err != nil {
		return "", errors.Wrap(err, "failed to get invoice data")
	}

	// Render the PDF
	pdfBytes, err := s.RenderInvoice(ctx, data)
	if err != nil {
		return "", errors.Wrap(err, "failed to render invoice")
	}

	// Ensure output directory exists
	if err := os.MkdirAll(s.outputDir, 0755); err != nil {
		return "", errors.Wrap(err, "failed to create output directory")
	}

	// Write to file
	pdfPath := filepath.Join(s.outputDir, fmt.Sprintf("%s.pdf", data.InvoiceNumber))
	if err := os.WriteFile(pdfPath, pdfBytes, 0644); err != nil {
		return "", errors.Wrap(err, "failed to write PDF to file")
	}

	return pdfPath, nil
}

// RenderInvoice renders an invoice template with the provided data
func (s *ServiceImpl) RenderInvoice(ctx context.Context, data *InvoiceData) ([]byte, error) {
	templatePath := filepath.Join(s.templateDir, s.defaultTemplate)
	
	// Generate a temporary file with template data filled in
	tmpFile, err := s.renderer.PrepareTemplate(templatePath, data)
	if err != nil {
		return nil, errors.Wrap(err, "failed to prepare template")
	}
	defer os.Remove(tmpFile)
	
	// Compile the template to PDF
	pdfBytes, err := s.renderer.CompileTemplate(tmpFile, s.fontDir)
	if err != nil {
		return nil, errors.Wrap(err, "failed to compile template")
	}
	
	return pdfBytes, nil
}

// GetInvoiceData retrieves invoice data for PDF generation
func (s *ServiceImpl) GetInvoiceData(ctx context.Context, invoiceID string) (*InvoiceData, error) {
	// Get invoice data with line items
	data, err := s.invoiceRepo.GetInvoiceWithLineItems(ctx, invoiceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to retrieve invoice data")
	}
	
	// Set default VAT if not specified
	if data.VAT == 0 {
		data.VAT = 0.18 // Default 18% VAT
	}
	
	// Note: We don't set default biller info here as the user of the API is the biller
	// The biller info should come from the environment/tenant configuration or be passed explicitly
	
	return data, nil
}
