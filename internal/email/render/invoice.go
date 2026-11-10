package render

// InvoiceState is the invoice's badge, as the hosted invoice page shows it.
type InvoiceState string

const (
	InvoiceStateDue     InvoiceState = "due"
	InvoiceStateOverdue InvoiceState = "overdue"
	InvoiceStatePaid    InvoiceState = "paid"
)

// Invoice is the invoice email's content; amounts and dates arrive formatted.
type Invoice struct {
	Merchant     string
	ContactEmail string
	Number       string
	State        InvoiceState
	Amount       string
	When         string
	BilledTo     string
	InvoiceDate  string

	// ViewURL opens the hosted invoice; empty means no button.
	ViewURL     string
	PDFAttached bool
	// PDFURL is the invoice's own PDF link, shown when nothing is attached.
	PDFURL string

	ChargeCount int
	Groups      []ChargeGroup
	// MoreCharges counts lines left out to keep the email short.
	MoreCharges int

	Subtotal string
	// Adjustments are discounts, prepaid credits and taxes, signed as shown.
	Adjustments []Row
	Total       string
	// AmountPaid is set on a partly paid invoice.
	AmountPaid string
	// AmountDue is empty once the invoice is paid.
	AmountDue string
}

type ChargeGroup struct {
	Name  string
	Total string
	Lines []ChargeLine
}

type ChargeLine struct {
	Name   string
	Detail string
	Amount string
}

type Row struct {
	Label  string
	Amount string
}

// AmountLabel names the headline amount.
func (v Invoice) AmountLabel() string {
	if v.State == InvoiceStatePaid {
		return "Amount paid"
	}
	return "Amount due"
}

// Badge is the state as the customer reads it.
func (v Invoice) Badge() string {
	switch v.State {
	case InvoiceStatePaid:
		return "Paid"
	case InvoiceStateOverdue:
		return "Overdue"
	default:
		return "Due"
	}
}

var invoicePage = newPage("invoice_finalized")

// InvoiceFinalized renders the email sent for a finalized invoice.
func InvoiceFinalized(v Invoice) (Content, error) {
	return invoicePage.render("Invoice "+v.Number+" from "+v.Merchant, v)
}
