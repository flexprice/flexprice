package pdf

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/pdf"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/typst"
)

// Generator defines the interface for PDF generation operations
type Generator interface {
	RenderInvoicePdf(ctx context.Context, data *pdf.InvoiceData) ([]byte, error)
}

type Config struct {
}

type service struct {
	config Config
	typst  typst.Compiler
}

// NewGenerator creates a new PDF service
func NewGenerator(config *config.Configuration, typst typst.Compiler) Generator {
	return &service{
		config: Config{},
		typst:  typst,
	}
}

// RenderPdf implements Service.RenderPdf
func (s *service) RenderInvoicePdf(ctx context.Context, data *pdf.InvoiceData) ([]byte, error) {
	// todo: template management from caller
	templateName := types.TemplateInvoiceDefault

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to marshal invoice data").
			Mark(ierr.ErrSystem)
	}

	pdf, err := s.typst.CompileTemplate(
		templateName,
		jsonData,
		typst.WithOutputFile(fmt.Sprintf("invoice-%s.pdf", data.ID)),
	)

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to compile invoice template").
			Mark(ierr.ErrSystem)
	}

	return pdf, nil
}
