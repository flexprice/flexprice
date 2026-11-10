package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/email"
	"github.com/flexprice/flexprice/internal/email/render"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	// maxEmailAttachmentBytes is Resend's cap on one email, attachments included.
	maxEmailAttachmentBytes = 40 << 20
	// maxInvoiceEmailLines keeps long invoices short; the hosted invoice has every line.
	maxInvoiceEmailLines = 10
)

// Metadata keys on an invoice email's log.
const (
	emailMetaPDF = "pdf"

	emailPDFAttached     = "attached"
	emailPDFInvoiceLink  = "invoice_link"
	emailPDFProviderLink = "billing_provider_link"
	emailPDFTooLarge     = "too_large"
	emailPDFDisabled     = "disabled"
	emailPDFUnavailable  = "unavailable"
)

func (s *emailNotificationService) composeInvoiceEmail(ctx context.Context, e *emaillog.EmailLog, allowMissingAttachments bool) (*composedEmail, error) {
	invoiceSvc := NewInvoiceService(s.ServiceParams)
	inv, err := invoiceSvc.GetInvoice(ctx, e.EntityID)
	if err != nil {
		return nil, err
	}
	if inv.InvoiceStatus != types.InvoiceStatusFinalized {
		return nil, ierr.NewError("invoice is no longer finalized").
			WithHint("The invoice was voided before the email was sent").
			Mark(ierr.ErrInvalidOperation)
	}
	if err := invoiceSvc.WithHostedInvoiceURLs(ctx, inv); err != nil {
		return nil, err
	}

	merchant, err := invoiceMerchant(ctx, s.ServiceParams, &inv.Invoice)
	if err != nil {
		return nil, err
	}

	paidWith, err := s.invoicePaidWith(ctx, inv.ID)
	if err != nil {
		return nil, err
	}

	view := invoiceEmailView(inv, merchant, paidWith, clock.Now())
	composed := &composedEmail{metadata: map[string]string{}}

	cfg, err := GetSetting[types.EmailNotificationConfig](NewSettingsService(s.ServiceParams).(*settingsService), ctx, types.SettingKeyEmailNotificationConfig)
	if err != nil {
		return nil, err
	}
	switch {
	case !cfg.AttachInvoicePDF:
		composed.metadata[emailMetaPDF] = emailPDFDisabled
	default:
		if err := attachInvoicePDF(ctx, invoiceSvc.(*invoiceService), &inv.Invoice, &view, composed); err != nil {
			if !allowMissingAttachments {
				return nil, ierr.WithError(err).WithHint("The invoice PDF is not ready yet").Mark(ierr.ErrServiceUnavailable)
			}
			s.Logger.Info(ctx, "sending invoice email without its pdf", "email_log_id", e.ID, "invoice_id", inv.ID, "reason", err.Error())
			composed.metadata[emailMetaPDF] = emailPDFUnavailable
		}
	}

	if composed.content, err = render.InvoiceFinalized(view); err != nil {
		return nil, err
	}
	return composed, nil
}

// attachInvoicePDF attaches Flexprice's stored PDF, or links the invoice's own PDF; a billing
// provider's expiring link and PDFs over the size cap are left out.
func attachInvoicePDF(ctx context.Context, invoiceSvc *invoiceService, inv *invoice.Invoice, view *render.Invoice, composed *composedEmail) error {
	src, err := invoiceSvc.pdfSource(ctx, inv, false)
	if err != nil {
		return err
	}
	switch {
	case inv.InvoicePDFURL != nil:
		view.PDFURL = src.url
		composed.metadata[emailMetaPDF] = emailPDFInvoiceLink
		return nil
	case src.url != "":
		composed.metadata[emailMetaPDF] = emailPDFProviderLink
		return nil
	}

	data := src.data
	if data == nil {
		if data, err = src.store.Download(ctx, src.key); err != nil {
			return err
		}
	}
	if len(data) > maxEmailAttachmentBytes {
		composed.metadata[emailMetaPDF] = emailPDFTooLarge
		return nil
	}
	composed.attachments = []email.Attachment{{Filename: view.Number + ".pdf", Content: data}}
	composed.metadata[emailMetaPDF] = emailPDFAttached
	view.PDFAttached = true
	return nil
}

// invoicePaidWith names how the invoice was last paid, as the hosted invoice page does.
func (s *emailNotificationService) invoicePaidWith(ctx context.Context, invoiceID string) (string, error) {
	filter := types.NewNoLimitPaymentFilter()
	filter.DestinationType = lo.ToPtr(string(types.PaymentDestinationTypeInvoice))
	filter.DestinationID = lo.ToPtr(invoiceID)
	payments, err := NewPaymentService(s.ServiceParams).ListPayments(ctx, filter)
	if err != nil {
		return "", err
	}

	var last *dto.PaymentResponse
	for _, p := range payments.Items {
		if p.PaymentStatus == types.PaymentStatusSucceeded && p.SucceededAt != nil &&
			(last == nil || p.SucceededAt.After(*last.SucceededAt)) {
			last = p
		}
	}
	if last == nil {
		return "", nil
	}
	if last.PaymentMethodType == types.PaymentMethodTypePaymentLink && lo.FromPtr(last.PaymentGateway) != "" {
		return words(lo.FromPtr(last.PaymentGateway)), nil
	}
	return words(string(last.PaymentMethodType)), nil
}

// invoiceMerchant is who issued the invoice: its billing entity, else the tenant.
func invoiceMerchant(ctx context.Context, params ServiceParams, inv *invoice.Invoice) (*tenant.Tenant, error) {
	t, err := params.TenantRepo.GetByID(ctx, types.GetTenantID(ctx))
	if err != nil {
		return nil, err
	}
	billingEntity, err := NewInvoiceService(params).GetInvoiceBillingEntity(ctx, inv)
	if err != nil {
		return nil, err
	}
	if billingEntity != nil {
		return billingEntityAsTenant(t, billingEntity), nil
	}
	return t, nil
}

func invoiceEmailSubject(inv *invoice.Invoice, merchantName string) string {
	return "Invoice " + invoiceNumber(inv) + " from " + merchantName
}

func invoiceNumber(inv *invoice.Invoice) string {
	if n := lo.FromPtr(inv.InvoiceNumber); n != "" {
		return n
	}
	return inv.ID
}

// invoiceEmailView lays the invoice out as the hosted invoice page does, in one column.
func invoiceEmailView(inv *dto.InvoiceResponse, merchant *tenant.Tenant, paidWith string, now time.Time) render.Invoice {
	money := func(d decimal.Decimal) string { return formatMoney(d, inv.Currency) }
	state := invoiceEmailState(inv, now)

	v := render.Invoice{
		Merchant:     merchant.Name,
		ContactEmail: merchant.BillingDetails.ReplyToEmail(),
		Number:       invoiceNumber(&inv.Invoice),
		State:        state,
		ViewURL:      inv.HostedInvoiceURL,
		InvoiceDate:  formatDay(lo.CoalesceOrEmpty(inv.IssueDate, inv.FinalizedAt, &inv.CreatedAt)),
		Subtotal:     money(inv.Subtotal),
		Total:        money(inv.Total),
	}
	if inv.Customer != nil && inv.Customer.Customer != nil {
		v.BilledTo = customerDisplayName(inv.Customer.Customer)
	}

	switch state {
	case render.InvoiceStatePaid:
		v.Amount = money(inv.Total)
		if inv.PaidAt != nil {
			v.When = "Paid " + formatDay(inv.PaidAt)
			if paidWith != "" {
				v.When += " with " + paidWith
			}
		}
	case render.InvoiceStateOverdue:
		v.Amount = money(inv.AmountRemaining)
		v.When = "Overdue since " + formatDay(inv.DueDate)
		v.AmountDue = v.Amount
	default:
		v.Amount = money(inv.AmountRemaining)
		if inv.DueDate != nil {
			v.When = "Due " + formatDay(inv.DueDate)
		}
		v.AmountDue = v.Amount
	}
	if state != render.InvoiceStatePaid && !inv.AmountPaid.IsZero() {
		v.AmountPaid = money(inv.AmountPaid)
	}

	if !inv.TotalDiscount.IsZero() {
		v.Adjustments = append(v.Adjustments, render.Row{Label: "Discounts", Amount: "−" + money(inv.TotalDiscount)})
	}
	if !inv.TotalPrepaidCreditsApplied.IsZero() {
		v.Adjustments = append(v.Adjustments, render.Row{Label: "Prepaid credits", Amount: "−" + money(inv.TotalPrepaidCreditsApplied)})
	}
	v.Adjustments = append(v.Adjustments, invoiceTaxRows(inv, money)...)

	v.Groups, v.ChargeCount, v.MoreCharges = invoiceChargeGroups(inv.LineItems, money)
	return v
}

// invoiceEmailState is the hosted invoice page's badge: paid once nothing is owed, overdue
// the calendar day (UTC) after the due date.
func invoiceEmailState(inv *dto.InvoiceResponse, now time.Time) render.InvoiceState {
	switch inv.PaymentStatus {
	case types.PaymentStatusSucceeded, types.PaymentStatusOverpaid, types.PaymentStatusRefunded, types.PaymentStatusPartiallyRefunded:
		return render.InvoiceStatePaid
	}
	if inv.PaymentStatus != types.PaymentStatusProcessing && !inv.AmountRemaining.IsPositive() {
		return render.InvoiceStatePaid
	}
	if inv.DueDate != nil && dayUTC(now).After(dayUTC(*inv.DueDate)) {
		return render.InvoiceStateOverdue
	}
	return render.InvoiceStateDue
}

// invoiceChargeGroups groups charged lines by plan or add-on in first-seen order (the portal's
// toGroups), keeping the first maxInvoiceEmailLines lines; group totals count every line.
func invoiceChargeGroups(items []*dto.InvoiceLineItemResponse, money func(decimal.Decimal) string) ([]render.ChargeGroup, int, int) {
	type group struct {
		name  string
		total decimal.Decimal
		lines []render.ChargeLine
	}
	var groups []*group
	shown, count := 0, 0

	for _, li := range items {
		if li == nil || li.Amount.IsZero() {
			continue
		}
		count++
		name := lo.CoalesceOrEmpty(lo.FromPtr(li.DisplayName), lo.FromPtr(li.MeterDisplayName), "Charge")
		groupName := lo.FromPtr(li.PlanDisplayName)
		if groupName == "" {
			groupName = "Other charges"
			if lo.FromPtr(li.EntityType) == string(types.InvoiceLineItemEntityTypeAddon) {
				groupName = name
			}
		}
		net := li.Amount.Sub(li.LineItemDiscount).Sub(li.InvoiceLevelDiscount).Sub(li.PrepaidCreditsApplied)

		g, ok := lo.Find(groups, func(g *group) bool { return g.name == groupName })
		if !ok {
			g = &group{name: groupName}
			groups = append(groups, g)
		}
		g.total = g.total.Add(net)
		if shown < maxInvoiceEmailLines {
			g.lines = append(g.lines, render.ChargeLine{Name: name, Detail: chargeDetail(li, money), Amount: money(net)})
			shown++
		}
	}

	out := make([]render.ChargeGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, render.ChargeGroup{Name: g.name, Total: money(g.total), Lines: g.lines})
	}
	return out, count, count - shown
}

// chargeDetail is "$100.00 each" or "$100.00 × 3" for a fixed charge and "1,800 used" for usage.
func chargeDetail(li *dto.InvoiceLineItemResponse, money func(decimal.Decimal) string) string {
	if lo.FromPtr(li.PriceType) == string(types.PRICE_TYPE_USAGE) || li.Quantity.IsZero() {
		return groupDigits(li.Quantity.String()) + " used"
	}
	unit := money(li.Amount.Div(li.Quantity).Round(2))
	if li.Quantity.Equal(decimal.NewFromInt(1)) {
		return unit + " each"
	}
	return unit + " × " + groupDigits(li.Quantity.String())
}

func invoiceTaxRows(inv *dto.InvoiceResponse, money func(decimal.Decimal) string) []render.Row {
	var rows []render.Row
	for _, t := range inv.Taxes {
		if t == nil || t.TaxAmount.IsZero() {
			continue
		}
		label := "Tax"
		if t.TaxRate != nil && t.TaxRate.TaxRate != nil {
			label = lo.CoalesceOrEmpty(t.TaxRate.Name, label)
			if p := t.TaxRate.PercentageValue; p != nil {
				label += " (" + p.String() + "%)"
			}
		}
		rows = append(rows, render.Row{Label: label, Amount: money(t.TaxAmount)})
	}
	if len(rows) == 0 && !inv.TotalTax.IsZero() {
		rows = append(rows, render.Row{Label: "Tax", Amount: money(inv.TotalTax)})
	}
	return rows
}

// formatMoney is "$1,297.92": the currency's symbol, or its code and a space when it has none.
func formatMoney(amount decimal.Decimal, currency string) string {
	sign := ""
	if amount.IsNegative() {
		sign = "-"
	}
	symbol := types.GetCurrencySymbol(currency)
	if strings.EqualFold(symbol, currency) {
		symbol += " "
	}
	return sign + symbol + groupDigits(amount.Abs().StringFixed(types.GetCurrencyPrecision(currency)))
}

// groupDigits puts thousands separators in a plain decimal string: 1234567.5 → 1,234,567.5.
func groupDigits(n string) string {
	whole, frac, hasFrac := strings.Cut(n, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if hasFrac {
		b.WriteString("." + frac)
	}
	return b.String()
}

func formatDay(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format("2 Jan 2006")
}

func dayUTC(t time.Time) time.Time {
	return t.UTC().Truncate(24 * time.Hour)
}

func customerDisplayName(c *customer.Customer) string {
	return lo.CoalesceOrEmpty(c.Name, c.ExternalID)
}

// words turns an enum such as PAYMENT_LINK or stripe into "Payment link" or "Stripe".
func words(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "_", " "))
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
