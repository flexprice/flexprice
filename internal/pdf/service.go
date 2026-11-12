package pdf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/pdf"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/typst"
	"github.com/samber/lo"
)

// Generator defines the interface for PDF generation operations
type Generator interface {
	RenderInvoicePdf(ctx context.Context, data *pdf.InvoiceData, templateName *types.TemplateName) ([]byte, error)
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
func (s *service) RenderInvoicePdf(ctx context.Context, data *pdf.InvoiceData, templateName *types.TemplateName) ([]byte, error) {
	// todo: template management from caller
	template := types.TemplateInvoiceDefault
	if templateName != nil {
		template = lo.FromPtr(templateName)
	}

	rendered := *data
	files, err := writeBrandingImages(&rendered)
	defer removeFiles(files)
	if err != nil {
		return nil, err
	}

	jsonData, err := json.Marshal(&rendered)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to marshal invoice data").
			Mark(ierr.ErrSystem)
	}

	pdf, err := s.typst.CompileTemplate(
		template,
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

// writeBrandingImages writes the logo and icon to temp files and points the template at them.
func writeBrandingImages(data *pdf.InvoiceData) ([]string, error) {
	var files []string
	for _, img := range []struct {
		image *pdf.Image
		path  *string
	}{
		{data.Logo, &data.BannerImage},
		{data.Icon, &data.BillerIcon},
	} {
		if img.image == nil {
			continue
		}
		f, err := os.CreateTemp("", "branding-*."+img.image.Ext)
		if err != nil {
			return files, ierr.WithError(err).WithHint("failed to create branding image file").Mark(ierr.ErrSystem)
		}
		files = append(files, f.Name())
		_, err = f.Write(img.image.Data)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			return files, ierr.WithError(errors.Join(err, closeErr)).WithHint("failed to write branding image file").Mark(ierr.ErrSystem)
		}
		*img.path = f.Name()
	}
	return files, nil
}

func removeFiles(files []string) {
	for _, f := range files {
		os.Remove(f) // #nosec G104 -- best-effort cleanup
	}
}
