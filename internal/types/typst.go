package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// TemplateName represents the available invoice templates
type TemplateName string

const (
	// TemplateInvoiceDefault is the default invoice template
	TemplateInvoiceDefault TemplateName = "invoice.typ"

	// TemplateInvoiceProfessional is a modern professional invoice template with improved typography and layout
	TemplateInvoiceProfessional TemplateName = "professional-invoice.typ"

	// TemplateInvoiceInvoice is a modern professional invoice template with improved typography and layout
	TemplateOlaKrutrimInvoice TemplateName = "ola-krutrim-invoice.typ"
)

func (t TemplateName) String() string {
	return string(t)
}

func (t TemplateName) Validate() error {
	allowed := []TemplateName{
		TemplateInvoiceDefault,
		TemplateInvoiceProfessional,
		TemplateOlaKrutrimInvoice,
	}
	if !lo.Contains(allowed, t) {
		return ierr.NewError("invalid template name").
			WithHint("Please provide a valid template name").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
