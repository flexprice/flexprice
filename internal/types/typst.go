package types

// TemplateName represents the available invoice templates
type TemplateName string

const (
	// TemplateInvoiceDefault is the default invoice template
	TemplateInvoiceDefault TemplateName = "invoice.typ"
)

func (t TemplateName) String() string {
	return string(t)
}
