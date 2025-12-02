package hubspot

// Provider constants:
// Use types.SecretProviderHubSpot for provider type (defined in internal/types/secret.go)
// Use types.ConnectionMetadataTypeHubSpot for connection metadata type (defined in internal/types/connection.go)

// HubSpot webhook subscription types
type SubscriptionType string

const (
	SubscriptionTypeDealPropertyChange SubscriptionType = "deal.propertyChange"
	SubscriptionTypeDealCreation       SubscriptionType = "deal.creation"
)

// HubSpot deal properties
const (
	PropertyNameDealStage = "dealstage"
)

// HubSpot deal stage values
type DealStage string

const (
	DealStageClosedWon DealStage = "closedwon"
)

// HubSpot invoice status values
type InvoiceStatus string

const (
	InvoiceStatusDraft InvoiceStatus = "draft"
	InvoiceStatusOpen  InvoiceStatus = "open"
)

// HubSpot Association Categories
// These define the category/type of relationship between objects in HubSpot
type AssociationCategory string

const (
	// AssociationCategoryHubSpotDefined represents associations defined by HubSpot
	// These are standard, built-in associations that HubSpot provides out of the box
	// Examples: Contact to Company, Deal to Contact, Line Item to Deal
	AssociationCategoryHubSpotDefined AssociationCategory = "HUBSPOT_DEFINED"

	// AssociationCategoryUserDefined represents custom associations created by users
	// These are associations that users create for their specific business needs
	AssociationCategoryUserDefined AssociationCategory = "USER_DEFINED"

	// AssociationCategoryIntegratorDefined represents associations created by integrations
	// These are associations that third-party integrations create
	AssociationCategoryIntegratorDefined AssociationCategory = "INTEGRATOR_DEFINED"
)

// HubSpot Association Type IDs
// These numeric IDs represent specific types of associations between objects
// Reference: https://developers.hubspot.com/docs/api/crm/associations
const (
	// Line Item associations
	// AssociationTypeLineItemToDeal - Associates a line item with a deal
	// This is the primary association for adding products/services to deals
	AssociationTypeLineItemToDeal = 20

	// AssociationTypeDealToLineItem - Reverse association from deal to line item
	// Used when querying line items associated with a deal
	AssociationTypeDealToLineItem = 19

	// Invoice associations (for reference, not currently used in deal sync)
	// AssociationTypeLineItemToInvoice - Associates a line item with an invoice
	AssociationTypeLineItemToInvoice = 20 // Same ID as line item to deal

	// AssociationTypeInvoiceToContact - Associates an invoice with a contact
	AssociationTypeInvoiceToContact = 705

	// Quote associations
	// AssociationTypeLineItemToQuote - Associates a line item with a quote
	// This is the primary association for adding products/services to quotes
	// Per HubSpot docs: associationTypeId 67 is for quote to line item
	AssociationTypeLineItemToQuote = 67

	// AssociationTypeQuoteToDeal - Associates a quote with a deal
	// Per HubSpot docs: associationTypeId 64 is for quote to deal
	AssociationTypeQuoteToDeal = 64

	// AssociationTypeQuoteToQuoteTemplate - Associates a quote with a quote template
	// Per HubSpot docs: associationTypeId 286 is for quote to quote template
	AssociationTypeQuoteToQuoteTemplate = 286
)
