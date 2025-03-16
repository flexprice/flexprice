package pdfgen

import (
	"context"
	"strconv"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/customer"
	"github.com/flexprice/flexprice/ent/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// EntRepository implements the InvoiceRepository interface using Ent
type EntRepository struct {
	client *ent.Client
}

// NewEntRepository creates a new repository that uses Ent
func NewEntRepository(client *ent.Client) InvoiceRepository {
	return &EntRepository{
		client: client,
	}
}

// GetInvoiceWithLineItems retrieves invoice data with line items for PDF generation
func (r *EntRepository) GetInvoiceWithLineItems(ctx context.Context, invoiceID string) (*InvoiceData, error) {
	// Query the invoice with line items
	inv, err := r.client.Invoice.Query().
		Where(invoice.ID(invoiceID)).
		WithLineItems().
		Only(ctx)
	if err != nil {
		return nil, ierr.WithError(err).WithHintf("failed to retrieve invoice %s", invoiceID).Mark(ierr.ErrDatabase)
	}

	// Fetch customer information using the CustomerID from the invoice
	customer, err := r.client.Customer.Query().
		Where(customer.ID(inv.CustomerID)).
		Only(ctx)
	if err != nil {
		return nil, ierr.WithError(err).WithHintf("failed to retrieve customer %s", inv.CustomerID).Mark(ierr.ErrDatabase)
	}

	// Get tenant-specific biller info
	billerInfo, err := r.getTenantBillerInfo(ctx, inv.TenantID)
	if err != nil {
		// Fall back to default biller info if tenant-specific info is not available
		billerInfo = BillerInfo{}
	}

	// Convert to InvoiceData
	data := &InvoiceData{
		ID:              inv.ID,
		InvoiceNumber:   *inv.InvoiceNumber,
		CustomerID:      inv.CustomerID,
		SubscriptionID:  *inv.SubscriptionID,
		InvoiceType:     inv.InvoiceType,
		InvoiceStatus:   inv.InvoiceStatus,
		PaymentStatus:   inv.PaymentStatus,
		Currency:        inv.Currency,
		AmountDue:       inv.AmountDue,
		AmountPaid:      inv.AmountPaid,
		AmountRemaining: inv.AmountRemaining,
		Description:     inv.Description,
		BillingReason:   inv.BillingReason,
		Notes:           "",   // Will be populated from metadata if available
		VAT:             0.18, // Default 18% VAT, could be from tenant config
		Biller:          billerInfo,
		Recipient:       extractRecipientInfo(customer),
	}

	// Convert dates
	if inv.DueDate != nil {
		data.DueDate = *inv.DueDate
	}
	if inv.PaidAt != nil {
		data.PaidAt = inv.PaidAt
	}
	if inv.VoidedAt != nil {
		data.VoidedAt = inv.VoidedAt
	}
	if inv.FinalizedAt != nil {
		data.FinalizedAt = inv.FinalizedAt
	}
	if inv.PeriodStart != nil {
		data.PeriodStart = inv.PeriodStart
	}
	if inv.PeriodEnd != nil {
		data.PeriodEnd = inv.PeriodEnd
	}

	// Parse metadata if available
	if inv.Metadata != nil {
		// Try to extract notes from metadata
		if notes, ok := inv.Metadata["notes"]; ok {
			data.Notes = notes
		}

		// Try to extract VAT from metadata
		if vat, ok := inv.Metadata["vat"]; ok {
			data.VAT, err = strconv.ParseFloat(vat, 64)
			if err != nil {
				return nil, ierr.WithError(err).WithHintf("failed to parse VAT %s", vat).Mark(ierr.ErrDatabase)
			}
		}
	}

	// Set default recipient if not found in metadata
	if data.Recipient.Name == "" {
		data.Recipient = defaultRecipientInfo(inv.CustomerID)
	}

	// Convert line items
	if len(inv.Edges.LineItems) > 0 {
		data.LineItems = make([]LineItemData, len(inv.Edges.LineItems))

		for i, item := range inv.Edges.LineItems {
			lineItem := LineItemData{
				PlanDisplayName: *item.PlanDisplayName,
				DisplayName:     *item.DisplayName,
				Amount:          item.Amount,
				Quantity:        item.Quantity,
				Currency:        item.Currency,
			}

			if item.PeriodStart != nil {
				lineItem.PeriodStart = item.PeriodStart
			}
			if item.PeriodEnd != nil {
				lineItem.PeriodEnd = item.PeriodEnd
			}

			data.LineItems[i] = lineItem
		}
	} else {
		return nil, ierr.NewError("no line items found").Mark(ierr.ErrDatabase)
	}

	return data, nil
}

func (r *EntRepository) getTenantBillerInfo(ctx context.Context, tenantID string) (BillerInfo, error) {
	// Get tenant information
	tenant, err := r.client.Tenant.Get(ctx, tenantID)
	if err != nil {
		return BillerInfo{}, ierr.WithError(err).WithHintf("failed to retrieve tenant %s", tenantID).Mark(ierr.ErrDatabase)
	}

	// Extract billing info from the new field
	billerInfo := BillerInfo{
		Name: tenant.Name,
		Address: AddressInfo{
			Street:     "--",
			City:       "--",
			PostalCode: "--",
		},
	}

	// If billing_info is populated, use it to fill in the BillerInfo
	if tenant.BillingInfo != nil {
		if email, ok := tenant.BillingInfo["email"].(string); ok {
			billerInfo.Email = email
		}
		if website, ok := tenant.BillingInfo["website"].(string); ok {
			billerInfo.Website = website
		}
		if helpEmail, ok := tenant.BillingInfo["help_email"].(string); ok {
			billerInfo.HelpEmail = helpEmail
		}
		if paymentInstructions, ok := tenant.BillingInfo["payment_instructions"].(string); ok {
			billerInfo.PaymentInstructions = paymentInstructions
		}

		// Extract address information
		if address, ok := tenant.BillingInfo["address"].(map[string]interface{}); ok {
			if street, ok := address["address_line1"].(string); ok {
				billerInfo.Address.Street = street
			}
			if street2, ok := address["address_line2"].(string); ok {
				if billerInfo.Address.Street != "" {
					billerInfo.Address.Street += "\n" + street2
				} else {
					billerInfo.Address.Street = street2
				}
			}
			if city, ok := address["city"].(string); ok {
				billerInfo.Address.City = city
			}
			if state, ok := address["state"].(string); ok {
				billerInfo.Address.State = state
			}
			if postalCode, ok := address["postal_code"].(string); ok {
				billerInfo.Address.PostalCode = postalCode
			}
			if country, ok := address["country"].(string); ok {
				billerInfo.Address.Country = country
			}
		}
	}

	return billerInfo, nil
}

// defaultRecipientInfo returns default recipient information based on customer ID
func defaultRecipientInfo(customerID string) RecipientInfo {
	return RecipientInfo{
		Name: "Customer " + customerID,
		Address: AddressInfo{
			Street:     "--",
			City:       "--",
			PostalCode: "--",
		},
	}
}

// extractRecipientInfo extracts recipient information from metadata
func extractRecipientInfo(data *ent.Customer) RecipientInfo {
	if data == nil {
		return RecipientInfo{}
	}

	result := RecipientInfo{
		Name:  data.Name,
		Email: data.Email,
	}

	result.Address = AddressInfo{
		Street:     "--",
		City:       "--",
		PostalCode: "--",
	}

	if data.AddressLine1 != "" {
		result.Address.Street = data.AddressLine1
	}
	if data.AddressLine2 != "" {
		result.Address.Street += "\n" + data.AddressLine2
	}
	if data.AddressCity != "" {
		result.Address.City = data.AddressCity
	}
	if data.AddressState != "" {
		result.Address.State = data.AddressState
	}
	if data.AddressPostalCode != "" {
		result.Address.PostalCode = data.AddressPostalCode
	}
	if data.AddressCountry != "" {
		result.Address.Country = data.AddressCountry
	}

	return result
}

// stringFromMap safely extracts a string from a map with a default value
func stringFromMap(data map[string]interface{}, key, defaultValue string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	return defaultValue
}
