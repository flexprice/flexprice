package pdfgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
	"time"

	"github.com/pkg/errors"
)

// TypstRenderer handles rendering Typst templates
type TypstRenderer struct {
	typstBinaryPath string
}

// NewTypstRenderer creates a new Typst renderer
func NewTypstRenderer(typstBinaryPath string) *TypstRenderer {
	return &TypstRenderer{
		typstBinaryPath: typstBinaryPath,
	}
}

// PrepareTemplate converts invoice data to a typst format and prepares a temporary file
func (r *TypstRenderer) PrepareTemplate(templatePath string, data *InvoiceData) (string, error) {
	// Read the template file
	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", errors.Wrap(err, "failed to read template file")
	}

	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "invoice-*")
	if err != nil {
		return "", errors.Wrap(err, "failed to create temp directory")
	}

	// Create the main.typ file with the invoice data
	mainTypPath := filepath.Join(tempDir, "main.typ")

	// Create the typst template with Go's templating
	tmpl, err := template.New("invoice").Parse(string(templateContent))
	if err != nil {
		return "", errors.Wrap(err, "failed to parse template")
	}

	// Convert data to typst-compatible format
	typstData := convertToTypstFormat(data)

	// Render the template to the temp file
	f, err := os.Create(mainTypPath)
	if err != nil {
		return "", errors.Wrap(err, "failed to create temp file")
	}
	defer f.Close()

	err = tmpl.Execute(f, typstData)
	if err != nil {
		return "", errors.Wrap(err, "failed to render template")
	}

	return mainTypPath, nil
}

// CompileTemplate compiles a Typst template into a PDF
func (r *TypstRenderer) CompileTemplate(filePath string, fontDir string) ([]byte, error) {
	// Get the directory of the file
	dir := filepath.Dir(filePath)

	// Create a temporary file for the output
	outputPath := filepath.Join(dir, "output.pdf")

	// Create the typst compile command
	args := []string{
		"compile",
		filePath,
		outputPath,
	}

	// Add font path if provided
	if fontDir != "" {
		args = append(args, "--font-path", fontDir)
	}

	// Execute typst command
	cmd := exec.Command(r.typstBinaryPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to compile typst template: %s", string(output))
	}

	// Read the generated PDF
	pdfBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read compiled PDF")
	}

	// Clean up temp files
	os.RemoveAll(dir)

	return pdfBytes, nil
}

// TypstData represents data in a format suitable for Typst templates
type TypstData struct {
	InvoiceNumber   string    `json:"invoice-number"`
	Title           string    `json:"title"`
	InvoiceID       string    `json:"invoice-id"`
	CustomerID      string    `json:"customer-id"`
	SubscriptionID  string    `json:"subscription-id,omitempty"`
	InvoiceType     string    `json:"invoice-type"`
	InvoiceStatus   string    `json:"invoice-status"`
	PaymentStatus   string    `json:"payment-status"`
	IssuingDate     string    `json:"issuing-date"`
	DueDate         string    `json:"due-date"`
	PaidAt          string    `json:"paid-at,omitempty"`
	VoidedAt        string    `json:"voided-at,omitempty"`
	FinalizedAt     string    `json:"finalized-at,omitempty"`
	PeriodStart     string    `json:"period-start,omitempty"`
	PeriodEnd       string    `json:"period-end,omitempty"`
	Notes           string    `json:"notes"`
	AmountDue       float64   `json:"amount-due"`
	AmountPaid      float64   `json:"amount-paid"`
	AmountRemaining float64   `json:"amount-remaining"`
	VAT             float64   `json:"vat"`
	BillingReason   string    `json:"billing-reason,omitempty"`
	BannerImage     string    `json:"banner-image,omitempty"`
	Biller          BillerMap `json:"biller"`
	Recipient       BillerMap `json:"recipient"`
	Items           []ItemMap `json:"items"`
}

// BillerMap represents formatted biller information for Typst
type BillerMap map[string]interface{}

// ItemMap represents formatted line item information for Typst
type ItemMap map[string]interface{}

// convertToTypstFormat converts from the service data model to Typst-compatible format
func convertToTypstFormat(data *InvoiceData) TypstData {
	// Default title
	title := "Invoice " + data.InvoiceNumber

	// Format dates for Typst
	issuingDate := formatTypstDate(time.Now())
	dueDate := formatTypstDate(data.DueDate)

	// Format optional dates
	periodStart := ""
	if data.PeriodStart != nil {
		periodStart = formatTypstDate(*data.PeriodStart)
	}

	periodEnd := ""
	if data.PeriodEnd != nil {
		periodEnd = formatTypstDate(*data.PeriodEnd)
	}

	// Format biller and recipient as maps
	billerMap := mapFromBiller(data.Biller)
	recipientMap := mapFromRecipient(data.Recipient)

	// Format items
	items := make([]ItemMap, len(data.LineItems))
	for i, item := range data.LineItems {
		items[i] = mapFromLineItem(item)
	}

	// Convert decimal values to float for Typst
	amountDue, _ := data.AmountDue.Float64()
	amountPaid, _ := data.AmountPaid.Float64()
	amountRemaining, _ := data.AmountRemaining.Float64()

	return TypstData{
		InvoiceNumber:   data.InvoiceNumber,
		Title:           title,
		InvoiceID:       data.ID,
		CustomerID:      data.CustomerID,
		SubscriptionID:  data.SubscriptionID,
		InvoiceType:     data.InvoiceType,
		InvoiceStatus:   data.InvoiceStatus,
		PaymentStatus:   data.PaymentStatus,
		IssuingDate:     issuingDate,
		DueDate:         dueDate,
		PeriodStart:     periodStart,
		PeriodEnd:       periodEnd,
		Notes:           data.Notes,
		AmountDue:       amountDue,
		AmountPaid:      amountPaid,
		AmountRemaining: amountRemaining,
		VAT:             data.VAT,
		BillingReason:   data.BillingReason,
		Biller:          billerMap,
		Recipient:       recipientMap,
		Items:           items,
	}
}

// formatTypstDate formats a time.Time in YYYY-MM-DD format for Typst
func formatTypstDate(t time.Time) string {
	return t.Format("2006-01-02")
}

// mapFromBiller converts BillerInfo to a map for Typst template
func mapFromBiller(info BillerInfo) BillerMap {
	result := BillerMap{
		"name": info.Name,
	}

	// Add optional fields if present
	if info.Email != "" {
		result["email"] = info.Email
	}
	if info.Website != "" {
		result["website"] = info.Website
	}
	if info.HelpEmail != "" {
		result["help-email"] = info.HelpEmail
	}
	if info.PaymentInstructions != "" {
		result["payment-instructions"] = info.PaymentInstructions
	}

	// Add address
	result["address"] = BillerMap{
		"street":      info.Address.Street,
		"city":        info.Address.City,
		"postal-code": info.Address.PostalCode,
	}

	if info.Address.State != "" {
		result["address"].(BillerMap)["state"] = info.Address.State
	}
	if info.Address.Country != "" {
		result["address"].(BillerMap)["country"] = info.Address.Country
	}

	return result
}

// mapFromRecipient converts RecipientInfo to a map for Typst template
func mapFromRecipient(info RecipientInfo) BillerMap {
	result := BillerMap{
		"name": info.Name,
	}

	if info.Email != "" {
		result["email"] = info.Email
	}

	// Add address
	result["address"] = BillerMap{
		"street":      info.Address.Street,
		"city":        info.Address.City,
		"postal-code": info.Address.PostalCode,
	}

	if info.Address.State != "" {
		result["address"].(BillerMap)["state"] = info.Address.State
	}
	if info.Address.Country != "" {
		result["address"].(BillerMap)["country"] = info.Address.Country
	}

	return result
}

// mapFromLineItem converts LineItemData to a map for Typst template
func mapFromLineItem(item LineItemData) ItemMap {
	amount, _ := item.Amount.Float64()
	quantity, _ := item.Quantity.Float64()

	result := ItemMap{
		"plan-display-name": item.PlanDisplayName,
		"display-name":      item.DisplayName,
		"amount":            amount,
		"quantity":          quantity,
	}

	// Add period dates if present
	if item.PeriodStart != nil {
		result["period-start"] = formatTypstDate(*item.PeriodStart)
	}
	if item.PeriodEnd != nil {
		result["period-end"] = formatTypstDate(*item.PeriodEnd)
	}

	return result
}
