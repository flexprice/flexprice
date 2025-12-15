# Flexprice QuickBooks Integration

## Table of Contents
1. [Overview](#overview)
2. [Objectives](#objectives)
3. [Architecture](#architecture)
4. [Connection Setup](#connection-setup)
5. [Customer Sync](#customer-sync)
6. [Invoice Sync](#invoice-sync)
7. [Line Item Mapping](#line-item-mapping)
8. [Entity Integration Mapping](#entity-integration-mapping)
9. [Error Handling and Resilience](#error-handling-and-resilience)
10. [Data Flow Diagrams](#data-flow-diagrams)
11. [Technical Specifications](#technical-specifications)
12. [API Design](#api-design)
13. [Testing Requirements](#testing-requirements)
14. [Security Considerations](#security-considerations)
15. [Performance and Scalability](#performance-and-scalability)
16. [Future Enhancements](#future-enhancements)
17. [Base Framework Establishment Approach](#)

---

## Overview

This PRD outlines the requirements for integrating Flexprice with QuickBooks Online (QBO) to enable **one-way synchronization** of invoices from Flexprice to QuickBooks. The integration follows the same architectural patterns established by the existing integrations (Stripe, Razorpay), ensuring consistency and maintainability across the codebase.

### Why Account API is Required (Read-Only)

**Important Clarification**: While the primary requirements are Customer and Invoice sync, we also need **read-only access** to the Account API because:

1. **QuickBooks Requirement**: According to [QuickBooks Item API documentation](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/most-commonly-used/item), Service items **MUST** have an `IncomeAccountRef` field
2. **Line Items Need Items**: Invoice line items reference QuickBooks Items (products/services)
3. **Items Need Accounts**: When creating Items for line items, we must reference an existing Income Account
4. **We Don't Create Accounts**: Accounts must already exist in QuickBooks (created manually by the user). We only query them to get the reference ID.

**Account API Usage**:
- ✅ **Read**: Query for Income accounts to get reference for Item creation
- ✅ **Read**: Get default income account from Preferences API
- ❌ **Create**: We never create accounts (must exist in QuickBooks)
- ❌ **Update**: We never update accounts

### Key Characteristics
- **Sync Direction**: One-way only (Flexprice → QuickBooks)
- **Primary Entity**: Invoices
- **Customer Handling**: On-demand creation (not required for every invoice)
- **Sync Trigger**: Invoice sync setting in connection configuration
- **Architecture**: Follows existing integration pattern for consistency

### Integration Scope
- ✅ Invoice synchronization (outbound only)
- ✅ Customer creation (on-demand, when needed for invoice sync)
- ✅ Line item mapping and synchronization
- ✅ Item (Product/Service) creation (on-demand, when needed for line items)
- ✅ Account reading (read-only, to get Income Account references for Items)
- ❌ Account creation (accounts must exist in QuickBooks - created manually)
- ❌ Payment reconciliation (out of scope for initial version)
- ❌ Bidirectional sync (out of scope)
- ❌ Plan/Product sync (out of scope)

---

## Objectives

### Primary Goals
1. **Automated Invoice Sync**: Automatically sync invoices from Flexprice to QuickBooks when invoice sync is enabled in the connection settings
2. **Customer Management**: Ensure customers exist in QuickBooks before syncing invoices, creating them on-demand if necessary
3. **Accurate Line Item Mapping**: Preserve all invoice line item details when syncing to QuickBooks
4. **Extensible Architecture**: Design a pipeline that is manageable and can be extended for future enhancements

### Success Criteria
- Invoices sync successfully to QuickBooks when sync is enabled
- Customers are automatically created in QuickBooks when needed
- All line items are accurately represented in QuickBooks invoices
- Integration failures don't break core Flexprice functionality
- System can handle high-volume invoice syncs efficiently

---

## Architecture

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Flexprice Core System                        │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │   Invoice       │  │    Customer    │  │   Connection    │  │
│  │   Service       │  │    Service     │  │    Service      │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                Integration Factory & Services                   │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │   QuickBooks    │  │   QuickBooks    │  │   QuickBooks    │  │
│  │    Client       │  │    Customer     │  │    Invoice      │  │
│  │                 │  │    Service      │  │    Sync         │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Entity Integration Mapping                   │
├─────────────────────────────────────────────────────────────────┤
│  Maps Flexprice entities to QuickBooks entities for sync       │
│  - Customer ID ↔ QuickBooks Customer ID                        │
│  - Invoice ID ↔ QuickBooks Invoice ID                          │
│  - Line Item ID ↔ QuickBooks Line Item ID                      │
└─────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                    QuickBooks Online API                        │
└─────────────────────────────────────────────────────────────────┘
```

### Integration Factory Pattern

Following the existing integration pattern, the QuickBooks integration will use a factory pattern:

```go
type QuickBooksIntegration struct {
    Client         *quickbooks.Client
    CustomerSvc    *quickbooks.CustomerService
    InvoiceSyncSvc  *quickbooks.InvoiceSyncService
}
```

### Directory Structure

```
internal/integration/quickbooks/
├── client.go              # QuickBooks API client
├── customer.go            # Customer sync service
├── invoice_sync.go        # Invoice sync service
├── line_item.go           # Line item mapping utilities
├── types.go               # QuickBooks-specific types and DTOs
└── README.md              # QuickBooks integration documentation
```

---

## Connection Setup

### Connection Metadata Structure

```go
type QuickBooksConnectionMetadata struct {
    // OAuth Credentials (encrypted)
    ClientID         string `json:"client_id"`          // OAuth Client ID (encrypted)
    ClientSecret     string `json:"client_secret"`      // OAuth Client Secret (encrypted)
    AccessToken      string `json:"access_token"`       // OAuth Access Token (encrypted)
    RefreshToken     string `json:"refresh_token"`      // OAuth Refresh Token (encrypted)
    
    // Company Configuration (not encrypted)
    RealmID          string `json:"realm_id"`           // QuickBooks Company ID (not encrypted)
    Environment      string `json:"environment"`        // "sandbox" or "production"
    
    // Token Management
    TokenExpiresAt   int64  `json:"token_expires_at"`   // Token expiration timestamp
    
    // Webhook Security (encrypted)
    WebhookVerifierToken string `json:"webhook_verifier_token,omitempty"` // Webhook verifier token from QuickBooks (encrypted)
}
```

**Field Details**:
- `ClientID`, `ClientSecret`: OAuth 2.0 credentials from QuickBooks app
- `AccessToken`, `RefreshToken`: OAuth tokens obtained during authorization
- `RealmID`: QuickBooks Company ID (visible in QuickBooks URL)
- `Environment`: Either "sandbox" or "production"
- `TokenExpiresAt`: Unix timestamp when access token expires (6 months)
- `WebhookVerifierToken`: Token from Intuit Developer Portal for webhook signature verification (optional but recommended)

### Sync Configuration

The sync configuration:

```go
type SyncConfig struct {
    Invoice *EntitySyncConfig `json:"invoice,omitempty"`
}

type EntitySyncConfig struct {
    Inbound  bool `json:"inbound"`  // Always false for QuickBooks
    Outbound bool `json:"outbound"` // true when invoice sync is enabled
}
```

### Connection Validation

- Validate OAuth credentials are present
- Validate RealmID is provided
- Validate environment is either "sandbox" or "production"
- Test API connectivity during connection setup
- Webhook verifier token is optional (recommended for production)

### Frontend Connection Setup Flow

#### Step 1: Initial Connection Creation (OAuth Flow)
Frontend sends during OAuth **init** (all fields in **PLAIN TEXT**):
```json
POST /v1/oauth/init
{
  "provider": "quickbooks",
  "name": "QuickBooks Production",
  "credentials": {
    "client_id": "...",                      // Plain text - backend encrypts
    "client_secret": "...",                  // Plain text - backend encrypts
    "webhook_verifier_token": "..."         // Plain text - backend encrypts (OPTIONAL)
  },
  "metadata": {
    "realm_id": "...",                       // Plain text - NOT encrypted (public company ID)
    "environment": "production",             // Plain text - NOT encrypted
    "income_account_id": "79"                // Plain text - NOT encrypted
  },
  "sync_config": {
    "invoice": {
      "inbound": false,
      "outbound": true
    },
    "payment": {
      "inbound": true,   // Enable webhook payment sync
      "outbound": false
    }
  }
}
```

Backend automatically (during OAuth init + complete):
1. ✅ Receives plain text from frontend
2. ✅ Encrypts **ALL credentials** (`client_id`, `client_secret`, `webhook_verifier_token`) and stores in OAuth session
3. ✅ Returns OAuth URL + session_id
4. ✅ User authorizes in QuickBooks
5. ✅ Frontend sends OAuth complete with `session_id`, `code`, `state`, `realm_id`
6. ✅ Backend retrieves session, validates CSRF, exchanges code for tokens
7. ✅ Backend encrypts each credential separately for final storage
8. ✅ Backend stores all encrypted values in `connections.encrypted_secret_data.quickbooks`
9. ✅ Backend stores `sync_config` in `connections.sync_config`
10. ✅ Does NOT encrypt `realm_id`, `environment`, `income_account_id` (public identifiers)

**Important Notes**:
- ✅ Frontend sends **ALL fields in PLAIN TEXT** - backend handles encryption
- ✅ **`webhook_verifier_token`** goes in `credentials` (NOT metadata) - same as Chargebee pattern
- ✅ Webhook verifier token is **optional but recommended** for security
- ✅ If not configured, webhooks will still process with warning log (for development)
- ✅ Token is stored in `EncryptedSecretData.QuickBooks.WebhookVerifierToken` (encrypted at rest)
- ⚠️ **Step 3 below is NO LONGER USED** - webhook token is now configured during OAuth init

#### Step 2: Configure Webhook in QuickBooks (User Action)
User must manually configure webhook in Intuit Developer Portal:
1. Go to QuickBooks app dashboard
2. Navigate to "Webhooks" section
3. Add webhook endpoint: `https://app.flexprice.io/v1/webhooks/quickbooks/{tenant_id}/{environment_id}`
4. Subscribe to `Payment` entity events (Create/Update)
5. Copy the **webhook verifier token** from Intuit Developer Portal
6. Add the webhook token to the OAuth init request (see Step 1 above)

### Provider Type

```go
const SecretProviderQuickBooks SecretProvider = "quickbooks"
```

---

## Customer Sync

### Overview

Customers are **not automatically created** in QuickBooks. Instead, customer creation happens **on-demand** when syncing an invoice, only if the customer doesn't already exist in QuickBooks.

### Customer Existence Check

Before syncing an invoice, the system must:
1. Check if customer exists in QuickBooks using Flexprice customer email
2. If customer exists, retrieve QuickBooks Customer ID
3. If customer doesn't exist, create customer in QuickBooks
4. Store mapping in Entity Integration Mapping table

### Customer Lookup Strategy

**Primary Identifier**: Email address
- Search QuickBooks customers by email
- If multiple customers found, use the first match
- If no customer found, create new customer

**Fallback Strategy**:
- If email is not available, use customer name
- If still not found, create new customer

### Customer Creation Flow

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Flexprice     │    │     Check       │    │    Create       │
│   Invoice       │───▶│   Customer      │───▶│   Customer      │
│   Created       │    │   Exists in     │    │   in QuickBooks │
│                 │    │   QuickBooks    │    │                 │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                │                       │
                                ▼                       ▼
                       ┌─────────────────┐    ┌─────────────────┐
                       │   Customer      │    │    Create       │
                       │   Found         │    │   Integration   │
                       │                 │    │    Mapping      │
                       └─────────────────┘    └─────────────────┘
```

### Customer Data Mapping

**Flexprice → QuickBooks Customer Fields**:

| Flexprice Field | QuickBooks Field | Notes |
|----------------|------------------|-------|
| `name` | `DisplayName` | Full name or company name (sanitize special characters) |
| `email` | `PrimaryEmailAddr.Address` | Primary email address |
| `phone` | `PrimaryPhone.FreeFormNumber` | Primary phone number |
| `address_line1` | `BillAddr.Line1` | Billing address line 1 |
| `address_line2` | `BillAddr.Line2` | Billing address line 2 |
| `city` | `BillAddr.City` | Billing city |
| `state` | `BillAddr.CountrySubDivisionCode` | Billing state/province |
| `postal_code` | `BillAddr.PostalCode` | Billing postal code |
| `country` | `BillAddr.Country` | Billing country |
| `currency` | `CurrencyRef.value` | Customer currency (if multi-currency enabled) |
| `id` | `Metadata` | Stored in custom field for reference |

**Important Validation Rules**:
- **Customer Name Sanitization**: Customer names cannot contain special characters like single quotes (`'`) or double quotes (`"`) ([avontus.com](https://www.avontus.com/media/10710/QuickBooksGuide.pdf))
  - Remove or replace special characters before creating customer
  - Example: `O'Brien` → `OBrien` or `O Brien`
- **Email Validation**: Email is used as primary lookup identifier
- **Required Fields**: `DisplayName` is required for customer creation

### Customer Service Interface

```go
// EnsureCustomerSyncedToQuickBooks ensures a customer exists in QuickBooks
// Returns QuickBooks Customer ID
func (s *CustomerService) EnsureCustomerSyncedToQuickBooks(
    ctx context.Context,
    customerID string,
    customerService interfaces.CustomerService,
) (string, error)

// FindCustomerInQuickBooks searches for customer by email
func (s *CustomerService) FindCustomerInQuickBooks(
    ctx context.Context,
    email string,
) (*QuickBooksCustomer, error)

// CreateCustomerInQuickBooks creates a new customer in QuickBooks
func (s *CustomerService) CreateCustomerInQuickBooks(
    ctx context.Context,
    FlexpriceCustomer *customer.Customer,
) (*QuickBooksCustomer, error)
```

### Multi-Currency Considerations

**Important**: QuickBooks customers are assigned a single currency. If a Flexprice customer uses multiple currencies:
- **Option 1**: Create separate QuickBooks customers for each currency (recommended)
- **Option 2**: Use the customer's primary/default currency
- **Option 3**: Use the invoice currency (may require customer update)

**Recommended Approach**: Use invoice currency when creating/updating customer for that specific invoice sync.

---

## Invoice Sync

### Overview

Invoices are synced from Flexprice to QuickBooks when:
1. Invoice sync setting is enabled in the QuickBooks connection (`sync_config.invoice.outbound = true`)
2. Invoice is created or finalized in Flexprice
3. Invoice has a valid customer

### Sync Trigger Points

- After invoice creation in `invoiceService.CreateInvoice`
- After subscription invoice generation in `invoiceService.CreateSubscriptionInvoice`
- Manual sync via API endpoint (future enhancement)

### Invoice Sync Flow

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Flexprice     │    │     Check       │    │    Ensure       │
│   Invoice       │───▶│   QuickBooks    │───▶│   Customer      │
│   Created       │    │   Connection    │    │   Exists        │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                │                       │
                                ▼                       ▼
                       ┌─────────────────┐    ┌─────────────────┐
                       │   Check Sync    │    │   Create        │
                       │   Setting       │    │   Customer if   │
                       │   Enabled       │    │   Needed        │
                       └─────────────────┘    └─────────────────┘
                                │                       │
                                ▼                       ▼
                       ┌─────────────────┐    ┌─────────────────┐
                       │   Check        │    │   Create        │
                       │   Existing     │    │   Invoice in    │
                       │   Mapping      │    │   QuickBooks    │
                       └─────────────────┘    └─────────────────┘
                                │                       │
                                ▼                       ▼
                       ┌─────────────────┐    ┌─────────────────┐
                       │   Sync Line     │    │   Create        │
                       │   Items         │    │   Integration   │
                       │                 │    │   Mapping       │
                       └─────────────────┘    └─────────────────┘
```

### Invoice Sync Service

```go
type InvoiceSyncService struct {
    client                       *Client
    customerSvc                   *CustomerService
    invoiceRepo                   invoice.Repository
    entityIntegrationMappingRepo  entityintegrationmapping.Repository
    logger                        *logger.Logger
}

// SyncInvoiceToQuickBooks syncs a Flexprice invoice to QuickBooks
func (s *InvoiceSyncService) SyncInvoiceToQuickBooks(
    ctx context.Context,
    req QuickBooksInvoiceSyncRequest,
    customerService interfaces.CustomerService,
) (*QuickBooksInvoiceSyncResponse, error)
```

### Invoice Sync Process (Step-by-Step)

#### Step 1: Validate Connection
- Check if QuickBooks connection exists
- Verify connection is active and authenticated
- Return error if connection not available

#### Step 2: Check Sync Configuration
- Verify `sync_config.invoice.outbound = true`
- If disabled, skip sync (not an error)

#### Step 3: Get Flexprice Invoice
- Retrieve invoice from repository
- Validate invoice has required fields
- Check invoice status (only sync finalized/paid invoices)
- **Validate Invoice Number**: 
  - Check if invoice number exceeds 11 characters
  - Truncate or map to valid format if needed
- **Validate Transaction Date**: 
  - Check if transaction date is after book close date
  - Query Preferences API to get `AccountingInfoPrefs.BookCloseDate`
  - Prevent sync if date is invalid (Error #6200)

#### Step 4: Check Existing Mapping
- Query Entity Integration Mapping for existing QuickBooks invoice
- If mapping exists, use existing QuickBooks Invoice ID
- If not, proceed with creation

#### Step 5: Ensure Customer Exists
- Call `EnsureCustomerSyncedToQuickBooks`
- This will check if customer exists, create if needed
- Get QuickBooks Customer ID

#### Step 6: Create Invoice in QuickBooks
- Create draft invoice in QuickBooks
- Set invoice metadata with Flexprice references
- Store QuickBooks Invoice ID

#### Step 7: Create Entity Mapping
- Create mapping record: Flexprice Invoice ID → QuickBooks Invoice ID
- Store sync metadata (timestamp, source, etc.)

#### Step 8: Sync Line Items
- For each line item in Flexprice invoice:
  - Map line item to QuickBooks line item format
  - **Ensure Item exists in QuickBooks**:
    - Query for item by name: `SELECT * FROM Item WHERE Name = '{name}' AND Type = 'Service' AND Active = true`
    - If not found, create item with:
      - Type: `Service`
      - Name: Sanitized (remove special characters like quotes)
      - Income Account: Get from Preferences or query for first active Income account
      - Sales Price: From line item data
  - **Validate Item has Income Account**: Required field for items
  - Add line item to QuickBooks invoice with ItemRef

#### Step 9: Finalize Invoice
- Update invoice totals
- Set invoice status in QuickBooks
- Mark invoice as ready for payment (if applicable)

#### Step 10: Update Flexprice Invoice
- Store QuickBooks Invoice ID in Flexprice invoice metadata
- Store QuickBooks invoice URL (if available)
- Update invoice sync status

### Invoice Data Mapping

**Flexprice → QuickBooks Invoice Fields**:

| Flexprice Field | QuickBooks Field | Notes |
|----------------|------------------|-------|
| `invoice_number` | `DocNumber` | Invoice number/identifier (max 11 characters) ([pdx1.corrigo.com](https://pdx1.corrigo.com/QBESLearningCenter/Content/User%20Guides/IFSM_QB_IntegrationGuide.pdf)) |
| `created_at` | `TxnDate` | Transaction date (YYYY-MM-DD format) |
| `due_date` | `DueDate` | Payment due date (YYYY-MM-DD format) |
| `currency` | `CurrencyRef.value` | Invoice currency (if multi-currency enabled) |
| `subtotal` | `SubTotalAmt` | Subtotal before tax (decimal) |
| `total_tax` | `TxnTaxDetail.TotalTax` | Total tax amount (decimal) |
| `total` | `TotalAmt` | Final total amount (decimal) |
| `amount_due` | `Balance` | Outstanding balance (decimal) |
| `description` | `PrivateNote` | Internal notes |
| `customer_id` | `CustomerRef.value` | QuickBooks Customer ID (required) |
| `id` | `CustomField` | Flexprice Invoice ID in metadata |

**Important Constraints**:
- **Invoice Number Limit**: QuickBooks supports invoice numbers up to **11 characters** ([pdx1.corrigo.com](https://pdx1.corrigo.com/QBESLearningCenter/Content/User%20Guides/IFSM_QB_IntegrationGuide.pdf))
  - If Flexprice invoice number exceeds 11 characters, implement truncation or mapping strategy
  - Consider using hash or UUID suffix for uniqueness
- **Required Fields**: 
  - `CustomerRef` (must exist in QuickBooks)
  - `TxnDate` (transaction date)
  - `Line` (at least one line item)
- **Date Format**: All dates must be in `YYYY-MM-DD` format
- **Amount Precision**: All amounts are decimal values (not integers like Stripe)

### Invoice Status Mapping

| Flexprice Status | QuickBooks Status | Notes |
|-----------------|-------------------|-------|
| `draft` | Not synced | Don't sync draft invoices |
| `open` | `Pending` | Invoice created, awaiting payment |
| `paid` | `Paid` | Invoice fully paid |
| `void` | `Voided` | Invoice voided |
| `uncollectible` | `Uncollectible` | Marked as uncollectible |

### Invoice Sync Request/Response

```go
type QuickBooksInvoiceSyncRequest struct {
    InvoiceID string `json:"invoice_id" validate:"required"`
}

type QuickBooksInvoiceSyncResponse struct {
    InvoiceID            string    `json:"invoice_id"`
    QuickBooksInvoiceID  string    `json:"quickbooks_invoice_id"`
    Status              string    `json:"status"`
    Amount              decimal.Decimal `json:"amount"`
    Currency            string    `json:"currency"`
    InvoiceURL          string    `json:"invoice_url,omitempty"`
    CreatedAt           time.Time `json:"created_at"`
    UpdatedAt           time.Time `json:"updated_at"`
}
```

---

## Line Item Mapping

### Overview

Line items are the core of invoice synchronization. Each Flexprice invoice line item must be accurately mapped to QuickBooks line items, preserving all relevant details.

### Line Item Structure in QuickBooks

QuickBooks line items require:
- **Item Reference**: Product/Service item in QuickBooks (required)
  - Item must exist in QuickBooks before adding to invoice
  - Item must have an **Income Account** reference (critical requirement)
- **Description**: Line item description
- **Quantity**: Quantity of items
- **Rate**: Unit price
- **Amount**: Total line amount (Quantity × Rate)
- **Tax Code**: Tax classification (if applicable)

**Critical Requirement - Income Account**:
- Every Service Item in QuickBooks **must** have an Income Account reference
- Account must be of type `Income` and `Active = true`
- Get default account from Preferences API: `SalesFormsPrefs.DefaultItemSalesRef`
- Or query for first active Income account: `SELECT * FROM Account WHERE AccountType = 'Income' AND Active = true`
- Reference: [QuickBooks Account API](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/all-entities/account)

### Line Item Mapping Strategy

#### Option 1: Product/Service Item (Recommended)
- Each line item maps to a QuickBooks Item
- Item must exist in QuickBooks before adding to invoice
- If item doesn't exist, create it automatically

#### Option 2: Description-Only Line Item
- Use description field without item reference
- Simpler but less structured
- Not recommended for detailed accounting

**Recommended Approach**: Use Option 1 with automatic item creation.

### Line Item Data Mapping

**Flexprice → QuickBooks Line Item Fields**:

| Flexprice Field | QuickBooks Field | Notes |
|----------------|------------------|-------|
| `display_name` | `Description` | Line item description |
| `amount` | `Amount` | Total line amount |
| `quantity` | `Qty` | Quantity |
| `price_unit_amount` | `Rate` | Unit price (if available) |
| `plan_display_name` | `ItemRef.name` | Product/service name |
| `meter_display_name` | `ItemRef.name` | Alternative name source |
| `currency` | Inherited from invoice | Currency from parent invoice |
| `period_start` | `CustomField` | Stored in metadata |
| `period_end` | `CustomField` | Stored in metadata |
| `id` | `CustomField` | Flexprice Line Item ID in metadata |

### Product/Service Item Creation

When a line item references a product/service that doesn't exist in QuickBooks:

#### Item Creation Flow
1. Check if item exists by name (query with `Type = 'Service'` and `Active = true`)
2. If not found, create new item:
   - **Type**: `Service` (recommended for SaaS/subscription businesses)
   - **Name**: From `plan_display_name` or `meter_display_name` or `display_name`
     - **Name Validation**: Customer names cannot contain special characters like single or double quotes ([avontus.com](https://www.avontus.com/media/10710/QuickBooksGuide.pdf))
     - Sanitize names before creating items
   - **Description**: From `display_name`
   - **Income Account**: 
     - Query for default income account from Preferences: `SalesFormsPrefs.DefaultItemSalesRef`
     - Or query for first active Income account: `SELECT * FROM Account WHERE AccountType = 'Income' AND Active = true`
     - **Required**: Item must have an income account reference
   - **Sales Price**: From `price_unit_amount` or calculated from `amount / quantity`
   - **Taxable**: Based on invoice tax settings
   - **Active**: Set to `true`

#### Item Lookup Strategy
- **Primary**: Search by exact name match with `Type = 'Service'` and `Active = true`
- **Fallback**: Search by partial name match (if exact match not found)
- **Create New**: If no match found

#### Account Entity Requirements

**Critical**: Items require an Income Account reference. According to [QuickBooks Item API](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/most-commonly-used/item), Service items MUST have an `IncomeAccountRef`.

**Important**: We only READ accounts from QuickBooks, we never create them. Accounts must already exist in the QuickBooks company.

**Account Query**:
```sql
SELECT * FROM Account WHERE AccountType = 'Income' AND Active = true
```

**Account Reference in Item**:
```json
{
  "IncomeAccountRef": {
    "value": "1",
    "name": "Services"
  }
}
```

**Default Account Strategy**:
1. First, try to get default from Preferences API: `GET /v3/company/{realmId}/preferences`
2. Check `SalesFormsPrefs.DefaultItemSalesRef` for default income account
3. If not available, query for first active Income account: `SELECT * FROM Account WHERE AccountType = 'Income' AND Active = true`
4. Cache account reference to avoid repeated queries
5. **Error Handling**: If no Income account exists, return clear error - accounts must be created manually in QuickBooks

### Line Item Sync Process

```go
// syncLineItemsToQuickBooks syncs all line items to QuickBooks invoice
func (s *InvoiceSyncService) syncLineItemsToQuickBooks(
    ctx context.Context,
    flexInvoice *invoice.Invoice,
    qbInvoiceID string,
    customerService interfaces.CustomerService,
) error

// addLineItemToQuickBooksInvoice adds a single line item
func (s *InvoiceSyncService) addLineItemToQuickBooksInvoice(
    ctx context.Context,
    qbClient *quickbooks.Client,
    qbInvoiceID string,
    lineItem *invoice.InvoiceLineItem,
    flexInvoice *invoice.Invoice,
) error
```

### Line Item Details Handling

#### Quantity and Amount
- If `quantity` is provided and > 0:
  - Use `quantity` as Qty
  - Calculate `Rate = amount / quantity`
- If `quantity` is 0 or not provided:
  - Set Qty = 1
  - Use `amount` as Rate

#### Tax Handling
- Extract tax information from Flexprice line item (if available)
- Map to QuickBooks tax code
- If tax code doesn't exist, use default tax code or create new one

#### Period Information
- Store `period_start` and `period_end` in line item custom fields
- Useful for subscription-based billing tracking

### Line Item Metadata

Store Flexprice-specific data in QuickBooks line item custom fields:
```json
{
  "Flexprice_line_item_id": "line_item_123",
  "Flexprice_price_id": "price_456",
  "Flexprice_meter_id": "meter_789",
  "period_start": "2024-01-01T00:00:00Z",
  "period_end": "2024-01-31T23:59:59Z",
  "sync_source": "Flexprice"
}
```

---

## Entity Integration Mapping

### Purpose

Entity Integration Mapping provides a lookup table between Flexprice entities and QuickBooks entities, enabling:
- Sync status tracking
- Avoiding duplicate syncing
- Efficient lookups during sync operations
- Metadata storage for sync context

### Mapping Structure

```go
type EntityIntegrationMapping struct {
    ID               string                     // Unique mapping ID
    EntityID         string                     // Flexprice entity ID
    EntityType       types.IntegrationEntityType // "customer" or "invoice"
    ProviderType     string                     // "quickbooks"
    ProviderEntityID string                     // QuickBooks entity ID
    Metadata         map[string]interface{}     // Sync metadata
    EnvironmentID    string                     // Environment context
}
```

### Supported Entity Types

- `customer` - Flexprice Customer ↔ QuickBooks Customer
- `invoice` - Flexprice Invoice ↔ QuickBooks Invoice

### Mapping Creation

**Customer Mapping**:
```go
mapping := &entityintegrationmapping.EntityIntegrationMapping{
    ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
    EntityID:         FlexpriceCustomerID,
    EntityType:       types.IntegrationEntityTypeCustomer,
    ProviderType:     "quickbooks",
    ProviderEntityID: quickBooksCustomerID,
    Metadata: map[string]interface{}{
        "created_via":           "Flexprice_to_provider",
        "quickbooks_customer_email": customer.Email,
        "synced_at":             time.Now().UTC().Format(time.RFC3339),
    },
    EnvironmentID: types.GetEnvironmentID(ctx),
    BaseModel:     types.GetDefaultBaseModel(ctx),
}
```

**Invoice Mapping**:
```go
mapping := &entityintegrationmapping.EntityIntegrationMapping{
    ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
    EntityID:         FlexpriceInvoiceID,
    EntityType:       types.IntegrationEntityTypeInvoice,
    ProviderType:     "quickbooks",
    ProviderEntityID: quickBooksInvoiceID,
    Metadata: map[string]interface{}{
        "sync_timestamp": time.Now().Unix(),
        "sync_source":    "Flexprice",
        "invoice_number": invoice.InvoiceNumber,
    },
    EnvironmentID: types.GetEnvironmentID(ctx),
    BaseModel:     types.GetDefaultBaseModel(ctx),
}
```

### Mapping Lookup

**Finding Customer Mapping**:
```go
filter := &types.EntityIntegrationMappingFilter{
    EntityID:      customerID,
    EntityType:    types.IntegrationEntityTypeCustomer,
    ProviderTypes: []string{"quickbooks"},
    QueryFilter:   types.NewDefaultQueryFilter(),
}
mappings, err := repo.List(ctx, filter)
```

**Finding Invoice Mapping**:
```go
filter := &types.EntityIntegrationMappingFilter{
    EntityID:      invoiceID,
    EntityType:    types.IntegrationEntityTypeInvoice,
    ProviderTypes: []string{"quickbooks"},
    QueryFilter:   types.NewDefaultQueryFilter(),
}
mappings, err := repo.List(ctx, filter)
```

---

## Error Handling and Resilience

### Error Categories

#### 1. Connection Errors
- **OAuth Token Expired**: 
  - Access tokens expire after 6 months ([coefficient.io](https://coefficient.io/quickbooks-api/setup-quickbooks-api-integration))
  - Use refresh token to obtain new access token
  - Retry request after token refresh
- **Invalid Credentials**: Return error, require re-authentication
- **API Rate Limits (HTTP 429)**: 
  - Implement exponential backoff retry ([coefficient.io](https://coefficient.io/quickbooks-api/setup-quickbooks-api-integration))
  - Respect `Retry-After` header if provided
  - Queue requests when rate limit approached
- **Network Failures**: Retry with exponential backoff
- **Invalid Realm ID**: Verify Realm ID is correct and connection is active

#### 2. Validation Errors
- **Missing Required Fields**: Return validation error
- **Invalid Data Format**: Return validation error with details
- **Currency Mismatch**: Handle gracefully (warn or convert)

#### 3. Business Logic Errors
- **Customer Not Found**: Create customer automatically
- **Item Not Found**: Create item automatically
- **Invoice Already Synced**: Return existing mapping (not an error)
- **Duplicate Invoice Number**: 
  - QuickBooks invoice numbers must be unique
  - Append suffix or use UUID if duplicate detected
  - Consider using Flexprice invoice ID as fallback
- **Account Period Closed (Error #6200)**: 
  - Check `AccountingInfoPrefs.BookCloseDate` before creating transactions ([blogs.intuit.com](https://blogs.intuit.com/2025/04/24/smoother-transactions-leveraging-intuit-api-entities/))
  - Return user-friendly error message
  - Prevent sync if transaction date is before book close date
- **Invalid Customer Name**: 
  - Customer names cannot contain special characters like single or double quotes ([avontus.com](https://www.avontus.com/media/10710/QuickBooksGuide.pdf))
  - Sanitize customer names before creation
- **Missing Income Account**: 
  - Query for default income account from Preferences
  - If no Income account exists, return error (accounts must be created manually in QuickBooks)
  - Create item with valid Income Account reference once account is found

#### 4. System Errors
- **Database Errors**: Log and return system error
- **API Timeout**: Retry with backoff
- **Unexpected API Response**: Log and return system error

### Error Handling Strategy

#### Graceful Degradation
- Sync failures should **not** break core Flexprice functionality
- Invoice creation in Flexprice should succeed even if QuickBooks sync fails
- Log all sync failures for debugging
- Provide retry mechanism for failed syncs

#### Retry Logic
```go
type RetryConfig struct {
    MaxRetries      int           // Maximum number of retries
    InitialDelay    time.Duration // Initial retry delay
    MaxDelay        time.Duration // Maximum retry delay
    BackoffFactor   float64       // Exponential backoff factor
}
```

**Retry Scenarios**:
- Transient network errors: Retry with exponential backoff
- Rate limit errors: Retry after rate limit window
- Token expiration: Refresh token and retry once
- Permanent errors: Don't retry, log and notify

#### Error Logging

All errors should be logged with:
- Correlation ID for tracing
- Entity IDs (invoice, customer)
- Error type and message
- Stack trace (for system errors)
- Context information (user, environment)

### Error Response Format

```go
type QuickBooksSyncError struct {
    Code            string                 `json:"code"`
    Message         string                 `json:"message"`
    Hint            string                 `json:"hint,omitempty"`
    Details         map[string]interface{} `json:"details,omitempty"`
    Retryable       bool                   `json:"retryable"`
    EntityID        string                 `json:"entity_id,omitempty"`
    EntityType      string                 `json:"entity_type,omitempty"`
}
```

### Monitoring and Alerting

- **Success Rate**: Track percentage of successful syncs
- **Failure Rate**: Track percentage of failed syncs
- **Average Sync Time**: Monitor performance
- **Error Rate by Type**: Identify common failure patterns
- **Alerts**: Notify on high failure rates or critical errors

---

## Data Flow Diagrams

### Complete Invoice Sync Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                        Flexprice System                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   Invoice   │  │  Customer    │  │ Connection   │              │
│  │   Created   │  │   Service    │  │   Service    │              │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘              │
│         │                │                │                     │
│         ▼                ▼                ▼                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   Check     │  │  Ensure     │  │   Check      │              │
│  │   Sync      │  │  Customer   │  │   Connection │              │
│  │   Enabled   │  │  Exists     │  │   Active     │              │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘              │
│         │                │                │                     │
│         ▼                ▼                ▼                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   Check     │  │  Create     │  │   Get        │              │
│  │   Existing  │  │  Customer   │  │   Customer   │              │
│  │   Mapping   │  │  if Needed  │  │   ID         │              │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘              │
│         │                │                │                     │
│         ▼                ▼                ▼                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   Create    │  │   Create    │  │   Sync      │              │
│  │   Invoice   │  │   Mapping   │  │   Line      │              │
│  │   in QBO    │  │   Record    │  │   Items     │              │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘              │
│         │                │                │                     │
│         ▼                ▼                ▼                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   Finalize  │  │   Update    │  │   Create     │              │
│  │   Invoice   │  │   Flexprice │  │   Invoice    │              │
│  │             │  │   Metadata  │  │   Mapping    │              │
│  └─────────────┘  └─────────────┘  └─────────────┘              │
└─────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                      QuickBooks Online                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │  Customer   │  │   Invoice   │  │    Item     │              │
│  │   Created   │  │   Created   │  │   Created   │              │
│  └─────────────┘  └─────────────┘  └─────────────┘              │
└─────────────────────────────────────────────────────────────────┘
```

### Customer Creation Flow

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Flexprice     │    │     Search      │    │    Customer     │
│   Customer      │───▶│   QuickBooks    │───▶│   Found?        │
│   (from Invoice)│    │   by Email      │    │                 │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                │                       │
                                │ Yes                   │ No
                                ▼                       ▼
                       ┌─────────────────┐    ┌─────────────────┐
                       │   Return        │    │    Create       │
                       │   Customer ID   │    │   Customer in   │
                       │                 │    │   QuickBooks    │
                       └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │    Create       │
                                              │   Integration   │
                                              │    Mapping      │
                                              └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │   Return        │
                                              │   Customer ID   │
                                              └─────────────────┘
```

### Line Item Sync Flow

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Flexprice     │    │   For Each      │    │     Check       │
│   Line Items    │───▶│   Line Item     │───▶│   Item Exists   │
│                 │    │                 │    │   in QuickBooks │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                │                       │
                                │ Yes                   │ No
                                ▼                       ▼
                       ┌─────────────────┐    ┌─────────────────┐
                       │   Get Item      │    │    Create       │
                       │   Reference     │    │   Item in      │
                       │                 │    │   QuickBooks   │
                       └─────────────────┘    └─────────────────┘
                                │                       │
                                ▼                       ▼
                       ┌─────────────────┐    ┌─────────────────┐
                       │   Map Line      │    │   Add Line      │
                       │   Item Data     │    │   Item to       │
                       │                 │    │   Invoice      │
                       └─────────────────┘    └─────────────────┘
```

---

## Technical Specifications

### QuickBooks Online API

#### Authentication
- **OAuth 2.0**: Use OAuth 2.0 for authentication ([coefficient.io](https://coefficient.io/quickbooks-api/setup-quickbooks-api-integration))
- **Token Management**: Implement token refresh mechanism
  - **Access Token Expiration**: Access tokens expire after 6 months ([coefficient.io](https://coefficient.io/quickbooks-api/setup-quickbooks-api-integration))
  - **Refresh Token**: Use refresh token to obtain new access tokens before expiration
  - **Token Storage**: Store tokens encrypted at rest
- **Realm ID**: Required for all API calls (Company ID)
- **OAuth Scopes**: Request minimum required scopes:
  - `com.intuit.quickbooks.accounting` - For invoice, customer, and item operations

#### API Versioning

**Critical**: Always specify the minor version in API requests to access latest features and avoid defaulting to outdated versions ([coefficient.io](https://coefficient.io/quickbooks-api/setup-quickbooks-api-integration))

- **Minor Version Parameter**: Include `?minorversion=70` (or latest) in all API requests
- **Example**: `https://quickbooks.api.intuit.com/v3/company/{realmId}/invoice?minorversion=70`
- **Query Endpoint**: `https://quickbooks.api.intuit.com/v3/company/{realmId}/query?minorversion=70&query=...`
- **Best Practice**: Use latest minor version for new features and bug fixes

#### API Endpoints Used

**Base URLs**:
- **Sandbox**: `https://sandbox-quickbooks.api.intuit.com`
- **Production**: `https://quickbooks.api.intuit.com`

**Customers** - Reference: [QuickBooks Customer API](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/most-commonly-used/customer):
- **Query Customer by Email**: 
  ```
  GET /v3/company/{realmId}/query?minorversion=70&query=SELECT * FROM Customer WHERE PrimaryEmailAddr = '{email}'
  ```
- **Create Customer**: 
  ```
  POST /v3/company/{realmId}/customer?minorversion=70
  ```
  - **Required Fields**: `DisplayName`
- **Get Customer**: 
  ```
  GET /v3/company/{realmId}/customer/{id}?minorversion=70
  ```
- **Update Customer**: 
  ```
  POST /v3/company/{realmId}/customer?minorversion=70
  ```
  - **Required**: Include `SyncToken` for optimistic locking

**Invoices** - Reference: [QuickBooks Invoice API](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/most-commonly-used/invoice):
- **Create Invoice**: 
  ```
  POST /v3/company/{realmId}/invoice?minorversion=70
  ```
  - **Required Fields**: `CustomerRef`, `TxnDate`, `Line` (at least one line item)
  - **Line Items**: Each line item must have `DetailType = "SalesItemLineDetail"` with `ItemRef` pointing to existing Item
- **Get Invoice**: 
  ```
  GET /v3/company/{realmId}/invoice/{id}?minorversion=70
  ```
- **Update Invoice**: 
  ```
  POST /v3/company/{realmId}/invoice?minorversion=70
  ```
  - **Required**: Include `SyncToken` for optimistic locking
- **Query Invoices**: 
  ```
  GET /v3/company/{realmId}/query?minorversion=70&query=SELECT * FROM Invoice WHERE DocNumber = '{invoiceNumber}'
  ```

**Items (Products/Services)** - Reference: [QuickBooks Item API](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/most-commonly-used/item):
- **Query Item by Name**: 
  ```
  GET /v3/company/{realmId}/query?minorversion=70&query=SELECT * FROM Item WHERE Name = '{name}' AND Type = 'Service' AND Active = true
  ```
- **Create Item**: 
  ```
  POST /v3/company/{realmId}/item?minorversion=70
  ```
  - **Required Fields**: `Name`, `Type` (must be "Service"), `IncomeAccountRef` (required for Service items)
- **Get Item**: 
  ```
  GET /v3/company/{realmId}/item/{id}?minorversion=70
  ```

**Accounts** (Read-Only - Required for Item Creation):
- **Query Income Accounts**: 
  ```
  GET /v3/company/{realmId}/query?minorversion=70&query=SELECT * FROM Account WHERE AccountType = 'Income' AND Active = true
  ```
- **Get Default Income Account**: 
  ```
  GET /v3/company/{realmId}/preferences?minorversion=70
  ```
  - Check `SalesFormsPrefs.DefaultItemSalesRef` for default income account
- **Note**: We only READ accounts, never create them. Accounts must already exist in QuickBooks.

**Preferences** (For validation):
- **Get Preferences**: 
  ```
  GET /v3/company/{realmId}/preferences?minorversion=70
  ```
  - Use `AccountingInfoPrefs.BookCloseDate` to validate accounting period is not closed
  - Prevents "Account Period Closed (#6200)" error ([blogs.intuit.com](https://blogs.intuit.com/2025/04/24/smoother-transactions-leveraging-intuit-api-entities/))

#### API Query Syntax

QuickBooks uses SQL-like query syntax for retrieving entities:

**Basic Query Format**:
```
SELECT {fields} FROM {EntityType} WHERE {conditions}
```

**Query Examples**:
- Find customer by email:
  ```sql
  SELECT * FROM Customer WHERE PrimaryEmailAddr = 'customer@example.com'
  ```
- Find item by name:
  ```sql
  SELECT * FROM Item WHERE Name = 'Service Name' AND Type = 'Service' AND Active = true
  ```
- Find invoice by document number:
  ```sql
  SELECT * FROM Invoice WHERE DocNumber = 'INV-001'
  ```

**Query Limitations**:
- Maximum 13 entities per query response
- Use pagination with `MAXRESULTS` and `STARTPOSITION` for large datasets
- Example: `SELECT * FROM Customer MAXRESULTS 13 STARTPOSITION 1`

#### API Rate Limits
- **Sandbox**: 100 requests per minute ([coefficient.io](https://coefficient.io/quickbooks-api/setup-quickbooks-api-integration))
- **Production**: 500 requests per minute ([coefficient.io](https://coefficient.io/quickbooks-api/setup-quickbooks-api-integration))
- **Rate Limit Response**: HTTP 429 (Too Many Requests)
- **Implementation**: 
  - Implement exponential backoff retry strategy
  - Track request count per minute
  - Queue requests when approaching limits
  - Use `Retry-After` header if provided

#### API Response Format
- **Content-Type**: `application/json`
- **Success Response**: JSON with `QueryResponse` or entity object
- **Error Response**: JSON with `Fault` object containing:
  - `type`: Error type (e.g., "ValidationFault", "AuthenticationFault")
  - `Error`: Array of error details with:
    - `code`: Error code (e.g., "6200" for closed period)
    - `Detail`: Error message
    - `element`: Field that caused error

#### Common Error Codes

| Error Code | Description | Handling Strategy |
|------------|-------------|-------------------|
| `6200` | Account Period Closed | Check `AccountingInfoPrefs.BookCloseDate` before creating transactions |
| `100` | Authentication Failure | Refresh OAuth token |
| `101` | Invalid Realm ID | Verify Realm ID is correct |
| `6000` | Validation Error | Check required fields and data format |
| `6010` | Duplicate Name | Handle duplicate customer/item names |
| `429` | Rate Limit Exceeded | Implement exponential backoff retry |

#### Change Data Capture (CDC) API

For future enhancements, use CDC API to track changes efficiently ([blogs.intuit.com](https://blogs.intuit.com/2018/09/10/quickbooks-online-api-best-practices/)):

- **Endpoint**: `GET /v3/company/{realmId}/cdc?entities=Invoice,Customer&changedSince={timestamp}`
- **Purpose**: Fetch list of entities that changed since specified timestamp
- **Benefits**: Reduces need for frequent polling and full data syncs
- **Use Case**: Future bidirectional sync implementation

### QuickBooks Client Implementation

```go
type Client struct {
    connectionRepo  connection.Repository
    encryptionSvc   security.EncryptionService
    logger          *logger.Logger
    httpClient      *http.Client
    minorVersion    string // e.g., "70" - always include in requests
}

// GetQuickBooksClient returns authenticated QuickBooks API client
func (c *Client) GetQuickBooksClient(ctx context.Context) (*quickbooks.APIClient, error)

// HasQuickBooksConnection checks if QuickBooks connection exists
func (c *Client) HasQuickBooksConnection(ctx context.Context) bool

// RefreshToken refreshes OAuth token if expired
// Access tokens expire after 6 months
func (c *Client) RefreshToken(ctx context.Context) error

// GetDefaultIncomeAccount gets default income account from Preferences
func (c *Client) GetDefaultIncomeAccount(ctx context.Context) (*QuickBooksAccountRef, error)

// ValidateAccountingPeriod checks if transaction date is after book close date
func (c *Client) ValidateAccountingPeriod(ctx context.Context, txnDate time.Time) error

// QueryEntities executes QuickBooks query with minor version
func (c *Client) QueryEntities(ctx context.Context, query string) (*QueryResponse, error)

// CreateEntity creates entity with minor version parameter
func (c *Client) CreateEntity(ctx context.Context, entityType string, entity interface{}) (interface{}, error)
```

**Client Configuration**:
- **Minor Version**: Always include `minorversion=70` (or latest) in all API requests
- **Base URL**: Determine from environment (sandbox vs production)
- **Rate Limiting**: Track requests per minute and implement throttling
- **Token Refresh**: Automatically refresh tokens before expiration (6 months)

### Data Types

#### QuickBooks Customer
```go
type QuickBooksCustomer struct {
    ID          string `json:"Id"`
    SyncToken   string `json:"SyncToken"`
    DisplayName string `json:"DisplayName"`
    PrimaryEmailAddr struct {
        Address string `json:"Address"`
    } `json:"PrimaryEmailAddr"`
    BillAddr *QuickBooksAddress `json:"BillAddr,omitempty"`
    CurrencyRef *QuickBooksRef  `json:"CurrencyRef,omitempty"`
    Metadata map[string]string  `json:"-"`
}
```

#### QuickBooks Invoice
```go
type QuickBooksInvoice struct {
    ID          string `json:"Id"`
    SyncToken   string `json:"SyncToken"`
    DocNumber   string `json:"DocNumber"`
    TxnDate     string `json:"TxnDate"`
    DueDate     string `json:"DueDate,omitempty"`
    CustomerRef QuickBooksRef `json:"CustomerRef"`
    Line        []QuickBooksLineItem `json:"Line"`
    SubTotalAmt float64 `json:"SubTotalAmt"`
    TotalAmt    float64 `json:"TotalAmt"`
    Balance     float64 `json:"Balance"`
    CurrencyRef *QuickBooksRef `json:"CurrencyRef,omitempty"`
}
```

#### QuickBooks Line Item
```go
type QuickBooksLineItem struct {
    ID          string `json:"Id,omitempty"`
    LineNum     int    `json:"LineNum,omitempty"`
    Description string `json:"Description"`
    Amount      float64 `json:"Amount"`
    DetailType  string `json:"DetailType"` // "SalesItemLineDetail"
    SalesItemLineDetail struct {
        ItemRef    QuickBooksRef `json:"ItemRef"` // Required: Reference to Item entity
        Qty        float64       `json:"Qty,omitempty"`
        UnitPrice  float64       `json:"UnitPrice,omitempty"`
        TaxCodeRef *QuickBooksRef `json:"TaxCodeRef,omitempty"` // Optional: Tax code reference
    } `json:"SalesItemLineDetail"`
}
```

#### QuickBooks Account (Required for Items)
```go
type QuickBooksAccount struct {
    ID          string `json:"Id"`
    Name        string `json:"Name"`
    AccountType string `json:"AccountType"` // "Income", "Expense", etc.
    Active      bool   `json:"Active"`
    AccountSubType string `json:"AccountSubType,omitempty"`
}

type QuickBooksAccountRef struct {
    Value string `json:"value"` // Account ID
    Name  string `json:"name"` // Account Name
}
```

#### QuickBooks Preferences (For Validation)
```go
type QuickBooksPreferences struct {
    AccountingInfoPrefs struct {
        BookCloseDate string `json:"BookCloseDate,omitempty"` // YYYY-MM-DD format
    } `json:"AccountingInfoPrefs"`
    SalesFormsPrefs struct {
        DefaultItemSalesRef *QuickBooksAccountRef `json:"DefaultItemSalesRef,omitempty"`
    } `json:"SalesFormsPrefs"`
}
```

---

## API Design

### Service Layer Integration

Add QuickBooks sync to invoice service:

```go
// syncInvoiceToQuickBooksIfEnabled syncs invoice to QuickBooks if enabled
func (s *invoiceService) syncInvoiceToQuickBooksIfEnabled(
    ctx context.Context,
    inv *invoice.Invoice,
) error {
    // Check if QuickBooks connection exists
    conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderQuickBooks)
    if err != nil || conn == nil {
        s.Logger.Debugw("QuickBooks connection not available, skipping invoice sync",
            "invoice_id", inv.ID,
            "error", err)
        return nil // Not an error, just skip sync
    }

    // Check if invoice sync is enabled
    if !conn.IsInvoiceOutboundEnabled() {
        s.Logger.Debugw("invoice sync disabled for QuickBooks connection, skipping",
            "invoice_id", inv.ID,
            "connection_id", conn.ID)
        return nil
    }

    // Get QuickBooks integration
    qbIntegration, err := s.IntegrationFactory.GetQuickBooksIntegration(ctx)
    if err != nil {
        s.Logger.Errorw("failed to get QuickBooks integration, skipping invoice sync",
            "invoice_id", inv.ID,
            "error", err)
        return nil // Don't fail the entire process
    }

    // Create customer service instance
    customerService := NewCustomerService(s.ServiceParams)

    // Create sync request
    syncRequest := quickbooks.QuickBooksInvoiceSyncRequest{
        InvoiceID: inv.ID,
    }

    // Perform the sync
    syncResponse, err := qbIntegration.InvoiceSyncSvc.SyncInvoiceToQuickBooks(
        ctx,
        syncRequest,
        customerService,
    )
    if err != nil {
        return err
    }

    s.Logger.Infow("successfully synced invoice to QuickBooks",
        "invoice_id", inv.ID,
        "quickbooks_invoice_id", syncResponse.QuickBooksInvoiceID,
        "status", syncResponse.Status)

    return nil
}
```

### Integration Factory Update

```go
// GetQuickBooksIntegration returns a complete QuickBooks integration setup
func (f *Factory) GetQuickBooksIntegration(ctx context.Context) (*QuickBooksIntegration, error) {
    // Create QuickBooks client
    qbClient := quickbooks.NewClient(
        f.connectionRepo,
        f.encryptionService,
        f.logger,
    )

    // Create customer service
    customerSvc := quickbooks.NewCustomerService(
        qbClient,
        f.customerRepo,
        f.entityIntegrationMappingRepo,
        f.logger,
    )

    // Create invoice sync service
    invoiceSyncSvc := quickbooks.NewInvoiceSyncService(
        qbClient,
        customerSvc,
        f.invoiceRepo,
        f.entityIntegrationMappingRepo,
        f.logger,
    )

    return &QuickBooksIntegration{
        Client:        qbClient,
        CustomerSvc:   customerSvc,
        InvoiceSyncSvc: invoiceSyncSvc,
    }, nil
}
```

---

## Testing Requirements

### Unit Tests

#### Customer Service Tests
- Test customer lookup by email
- Test customer creation
- Test customer mapping creation
- Test error handling for missing customers

#### Invoice Sync Service Tests
- Test invoice sync flow
- Test line item mapping
- Test existing mapping detection
- Test error handling and retries

#### Line Item Service Tests
- Test line item to QuickBooks format conversion
- Test item creation
- Test item lookup
- Test quantity and amount calculations

### Integration Tests

#### End-to-End Sync Test
1. Create customer in Flexprice
2. Create invoice with line items
3. Enable QuickBooks sync
4. Verify invoice syncs to QuickBooks
5. Verify customer created in QuickBooks
6. Verify line items synced correctly
7. Verify mapping records created

#### Error Scenario Tests
- Test sync with invalid credentials
- Test sync with expired token
- Test sync with missing customer
- Test sync with duplicate invoice number
- Test sync with API rate limits

### Test Data Requirements

- Valid QuickBooks sandbox credentials
- Test customers with various data combinations
- Test invoices with different line item types
- Test edge cases (zero amounts, missing fields, etc.)

---

## Security Considerations

### Authentication and Authorization

#### OAuth Token Management
- **Encryption**: Store OAuth tokens encrypted at rest
- **Token Refresh**: Automatically refresh expired tokens
- **Token Rotation**: Support token rotation for security
- **Scope Limitation**: Request minimum required OAuth scopes

#### API Security
- **HTTPS Only**: All API calls must use HTTPS
- **Request Signing**: Sign requests if required by QuickBooks
- **Rate Limiting**: Respect API rate limits
- **Error Handling**: Don't expose sensitive data in error messages

### Data Protection

#### Sensitive Data
- Encrypt QuickBooks credentials in database
- Don't log sensitive customer data
- Mask sensitive fields in logs
- Secure API key storage

#### Data Privacy
- Comply with data privacy regulations
- Only sync necessary data fields
- Allow customers to opt-out of sync
- Provide data deletion capabilities

---

## Performance and Scalability

### Performance Requirements

#### Sync Performance
- **Target**: Sync invoice within 5 seconds (excluding API latency)
- **Throughput**: Support 100+ invoices per minute
- **Concurrency**: Support parallel syncs for different invoices

#### Optimization Strategies
- **Batch Operations**: Batch item lookups where possible
- **Caching**: Cache customer and item lookups
- **Connection Pooling**: Reuse HTTP connections
- **Async Processing**: Consider async processing for high volume

### Scalability Considerations

#### Horizontal Scaling
- Design for stateless operation
- Support multiple worker instances
- Use distributed locking for critical sections

#### Database Optimization
- Index Entity Integration Mapping table properly
- Optimize customer lookup queries
- Cache frequently accessed data

#### Rate Limit Management
- Implement rate limit tracking
- Queue syncs when rate limits hit
- Distribute syncs across time windows

---

## Payment Reconciliation (Inbound Only)

### Overview

The QuickBooks integration supports **inbound-only** payment reconciliation from QuickBooks to Flexprice. When a payment is recorded and closed in QuickBooks, Flexprice automatically updates the corresponding invoice status.

**Important API Clarification**: This integration uses the **QuickBooks Online Accounting API** (`/v3/company/{realmId}/payment`) for reading payment records. This is NOT the QuickBooks Payments API which is for processing card transactions. The Payment entity in the Accounting API represents a record that a payment was received against an invoice.

Reference: [QuickBooks Payment API](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/all-entities/payment)

### Implementation Status

✅ **COMPLETED** - Inbound payment sync is fully implemented and tested.

### Sync Direction

| Direction | Trigger | Action | Status |
|-----------|---------|--------|--------|
| QuickBooks → Flexprice | Webhook (`Payment.Create`) | Mark invoice as SUCCEEDED with offline payment | ✅ Implemented |

**Note**: Outbound sync (Flexprice → QuickBooks) is intentionally NOT supported. Payments are only tracked within Flexprice; QuickBooks users record payments directly in their accounting system.

### QuickBooks → Flexprice (Inbound Sync)

**Implementation**: `internal/integration/quickbooks/payment.go:HandleExternalPaymentFromWebhook()`

When a payment is "recorded and closed" against an invoice in QuickBooks, Flexprice receives a webhook notification and marks the invoice as paid.

**Important Design Decision**: We DO NOT create a separate payment record in Flexprice. Instead, we directly update the invoice status to `SUCCEEDED` with `payment_method: offline`. This approach:
- ✅ Avoids decimal precision issues between systems
- ✅ Simplifies the sync logic (1:1 invoice relationship)
- ✅ Stores all relevant payment data in invoice metadata
- ✅ Maintains invoice-centric payment tracking
- ✅ Follows the same pattern as other inbound integrations (like Stripe webhooks)

#### Webhook Setup

1. Register webhook endpoint in Intuit Developer Portal: `https://app.flexprice.io/v1/webhooks/quickbooks/{tenant_id}/{environment_id}`
2. Subscribe to `Payment` entity events (Create/Update)
3. Configure webhook verifier token for signature validation
4. Enable payment inbound sync in connection: `sync_config.payment.inbound = true`

#### Webhook Payload Structure

```json
{
  "eventNotifications": [
    {
      "realmId": "123456789",
      "dataChangeEvent": {
        "entities": [
          {
            "name": "Payment",
            "id": "222",
            "operation": "Create",
            "lastUpdated": "2025-12-04T18:14:00.000Z"
          }
        ]
      }
    }
  ]
}
```

**Note**: Webhook payload is minimal - only contains Payment ID. Must call API to get full details.

#### Processing Flow

```
1. Receive webhook event (Payment.Create/Update)
2. Verify signature using intuit-signature header (optional if verifier token configured)
3. Check payment inbound sync is enabled (sync_config.payment.inbound = true)
4. Call GET /v3/company/{realmId}/payment/{id} to fetch full payment details
5. Extract LinkedTxn[].TxnId to get QuickBooks Invoice ID
6. Query entity_integration_mapping to find Flexprice invoice ID
7. Update Flexprice invoice directly (NO payment record created):
   - invoice.payment_status = SUCCEEDED
   - invoice.paid_at = current timestamp
   - invoice.amount_paid = invoice.amount_due
   - invoice.amount_remaining = 0
   - invoice.metadata["payment_recorded_by"] = "quickbooks"
   - invoice.metadata["payment_method"] = "offline"
   - invoice.metadata["quickbooks_payment_id"] = QB Payment ID (e.g., "222")
   - invoice.metadata["quickbooks_invoice_id"] = QB Invoice ID (e.g., "219")
   - invoice.metadata["entity_mapping_id"] = Entity mapping ID
   - invoice.metadata["payment_synced_at"] = ISO 8601 timestamp
8. Save updated invoice to database
```

**Example Invoice Metadata After Sync**:
```json
{
  "payment_method": "offline",
  "entity_mapping_id": "eim_01KBN8RW13DZ67V94RKY1KHAZW",
  "payment_synced_at": "2025-12-04T18:14:00Z",
  "payment_recorded_by": "quickbooks",
  "quickbooks_invoice_id": "219",
  "quickbooks_payment_id": "222"
}
```

### Payment Data Types

#### Payment Response (Inbound Only)

```go
// PaymentResponse represents a payment response from QuickBooks
// Used for INBOUND sync (QuickBooks → Flexprice) when processing webhooks
type PaymentResponse struct {
    ID          string        `json:"Id"`                       // QuickBooks Payment ID
    TxnDate     string        `json:"TxnDate"`                  // Payment date (YYYY-MM-DD)
    TotalAmt    float64       `json:"TotalAmt"`                 // Total payment amount
    CustomerRef AccountRef    `json:"CustomerRef"`              // Customer who made payment
    Line        []PaymentLine `json:"Line,omitempty"`           // Payment lines with linked invoices
    PrivateNote string        `json:"PrivateNote,omitempty"`    // Internal memo from QuickBooks
}

// PaymentLine represents a single line in a payment linking to an invoice
// Used to extract which invoice(s) a payment was applied to
type PaymentLine struct {
    Amount    float64     `json:"Amount"`    // Line amount
    LinkedTxn []LinkedTxn `json:"LinkedTxn"` // Links to invoice(s)
}

// LinkedTxn links a payment to a specific transaction (invoice)
type LinkedTxn struct {
    TxnId   string `json:"TxnId"`   // QuickBooks Invoice ID
    TxnType string `json:"TxnType"` // Always "Invoice" for invoice payments
}
```

**Important Notes**:
- We only READ payment data from QuickBooks (via `GET /payment/{id}`)
- We DO NOT create payments in QuickBooks (no outbound sync)
- The `Line` array is used to extract which invoice was paid
- QuickBooks supports multi-invoice payments, but we reconcile each individually

### Entity Integration Mapping

**Note**: Payment entity mappings are NOT created. We only track invoice mappings for payment reconciliation.

When a payment is recorded in QuickBooks and synced to Flexprice:
1. The QuickBooks Payment entity contains `LinkedTxn` with the QB Invoice ID
2. We query `entity_integration_mapping` to find the Flexprice invoice ID using the QB Invoice ID
3. We update the Flexprice invoice directly (no payment record or mapping created)

**Why No Payment Mapping?**:
- Payments are recorded directly on invoices in Flexprice
- No separate payment entity exists in Flexprice for offline payments
- Invoice metadata stores all necessary QuickBooks payment details
- Simpler architecture with less overhead

### Sync Configuration

Payment reconciliation is controlled via `SyncConfig` in the connection settings:

```go
type SyncConfig struct {
    Invoice *EntitySyncConfig `json:"invoice,omitempty"`
    Payment *EntitySyncConfig `json:"payment,omitempty"`  // Payment reconciliation configuration
}

type EntitySyncConfig struct {
    Inbound  bool `json:"inbound"`   // Enable inbound sync (Provider → Flexprice)
    Outbound bool `json:"outbound"`  // Not used for payments (always false)
}
```

**Payment Sync Settings**:
- `payment.inbound = true`: Enable QuickBooks → Flexprice (webhook-based)
- `payment.outbound`: **Not supported** - always ignored for QuickBooks integration

**Example Configuration**:
```json
{
  "sync_config": {
    "invoice": {
      "inbound": false,
      "outbound": true
    },
    "payment": {
      "inbound": true,
      "outbound": false
    }
  }
}
```

### Webhook Security

#### 1. Webhook Verifier Token Configuration

**Where to Get It**:
- Log into Intuit Developer Portal
- Navigate to your app → Webhooks section
- The webhook verifier token is displayed when you configure webhooks
- This token is used by QuickBooks to sign webhook payloads

**Storage Pattern** (Following Chargebee Pattern):
- ✅ **Stored in**: `EncryptedSecretData.QuickBooks.WebhookVerifierToken`
- ✅ **Encrypted**: Yes, encrypted at rest using AES-256
- ✅ **Optional**: Yes, but recommended for production
- ❌ **NOT stored in**: Plain `metadata` field

**Frontend Workflow**:
1. User creates QuickBooks connection via OAuth (Step 1)
2. User manually configures webhook in Intuit Developer Portal:
   - Webhook URL: `https://app.flexprice.io/v1/webhooks/quickbooks/{tenant_id}/{environment_id}`
   - Subscribe to: `Payment` entity (Create/Update events)
3. User copies webhook verifier token from Intuit portal
4. Frontend sends PATCH request to update connection:
   ```json
   {
     "encrypted_secret_data": {
       "quickbooks": {
         "webhook_verifier_token": "TOKEN_FROM_QUICKBOOKS"
       }
     }
   }
   ```
5. Backend encrypts and stores token, invalidates cache

#### 2. Signature Verification (Optional but Recommended)
   - QuickBooks sends `intuit-signature` header with webhook requests
   - Backend verifies signature using HMAC-SHA256 with webhook verifier token as key
   - **If verifier token NOT configured**:
     - ⚠️ Logs warning: "webhook verifier token not configured - SECURITY RISK, skipping signature verification"
     - ✅ Webhook processing CONTINUES (does NOT block)
     - ✅ Payment sync works normally
     - 💡 Suitable for development/testing
   - **If verifier token configured**:
     - ✅ Signature verification is enforced
     - ❌ Invalid signatures are rejected (HTTP 401)
     - ✅ Production-ready security
   - Reference: [QuickBooks Webhook Verification](https://developer.intuit.com/app/developer/qbo/docs/develop/webhooks/verifying-the-event-notification)

#### 3. HTTPS Only
   - Webhook endpoint MUST use HTTPS in production
   - HTTP allowed only for localhost development

#### 4. Idempotency
   - Check for duplicate webhook events using payment ID
   - Skip processing if invoice already marked as SUCCEEDED
   - Prevents duplicate payment recording

#### 5. Return 200 OK Immediately
   - Return HTTP 200 immediately to acknowledge receipt
   - Process webhook asynchronously to prevent QuickBooks retries
   - QuickBooks retries if non-200 response or timeout

#### 6. Realm ID Validation
   - Verify webhook `realmId` matches connection's configured Realm ID
   - Prevents processing webhooks for wrong QuickBooks company

### Error Handling

| Scenario | Action | HTTP Response |
|----------|--------|---------------|
| Invoice not found in mapping | Log warning, skip sync | 200 OK |
| Invoice already SUCCEEDED | Log info, skip sync | 200 OK |
| Webhook verification fails | Log error (if verifier configured) | 401 Unauthorized |
| QuickBooks API error (GET payment) | Log error, return 200 OK | 200 OK |
| Sync disabled in config | Log debug, skip processing | 200 OK |
| Realm ID mismatch | Log warning, skip notification | 200 OK |

**Graceful Degradation**:
- Webhook failures should NOT break the invoice flow
- All errors are logged with context (payment ID, invoice ID, error details)
- Failed syncs can be retried by re-recording payment in QuickBooks

### Security & Data Protection

**No Sensitive Data in Logs** ✅:
- Access tokens, refresh tokens, client secrets are NEVER logged
- Only non-sensitive identifiers are logged (payment IDs, invoice IDs, realm IDs)
- Error messages sanitized to exclude tokens and secrets
- All OAuth credentials encrypted at rest in database

**Audit Trail**:
- All payment syncs logged with:
  - Timestamp
  - Sync direction (inbound/outbound)
  - Payment ID (both systems)
  - Invoice ID (both systems)
  - Sync result (success/failure)

### Testing Checklist

#### Inbound Sync (QuickBooks → Flexprice)
- [ ] Record payment in QuickBooks for synced invoice
- [ ] Verify webhook received and processed
- [ ] Verify Flexprice invoice status updated to SUCCEEDED
- [ ] Verify invoice metadata contains QuickBooks payment details
- [ ] Verify duplicate webhook handling (idempotency)
- [ ] Test with payment inbound sync disabled
- [ ] Test with invoice not synced to Flexprice

#### Error Scenarios
- [ ] Test with invalid QuickBooks credentials
- [ ] Test with expired OAuth token (verify auto-refresh)
- [ ] Test webhook signature verification
- [ ] Test realm ID mismatch
- [ ] Test API rate limit handling

### Implementation Files

| File | Purpose |
|------|---------|
| `internal/integration/quickbooks/payment.go` | Payment reconciliation service (inbound only) |
| `internal/integration/quickbooks/webhook/handler.go` | Webhook handler for inbound payment events |
| `internal/integration/quickbooks/webhook/types.go` | Webhook payload DTOs |
| `internal/integration/quickbooks/dto.go` | Payment API response DTOs |
| `internal/integration/quickbooks/client.go` | Payment API client methods (GetPayment only) |
| `internal/integration/factory.go` | Payment service injection |
| `internal/api/v1/webhook.go` | Webhook API endpoint |

---

## Future Enhancements

### Payment Enhancements

#### Advanced Payment Reconciliation
- **Payment Reversals/Refunds**: Track refunds and payment reversals from QuickBooks
- **Payment Reconciliation Reports**: Dashboard showing payment sync status and discrepancies
- **Manual Sync Trigger**: Admin interface to manually trigger payment reconciliation
- **Payment Gateway Details**: Store additional payment method details from QuickBooks
- **Outbound Payment Sync**: Consider adding Flexprice → QuickBooks payment sync in future if business needs arise

**Note**: Partial payment support is already implemented using ReconcilePaymentStatus()

#### Enhanced Line Item Support
- Support for discounts
- Support for taxes at line item level
- Support for custom fields

#### Advanced Features
- **Bulk Sync Operations**: Sync multiple invoices in batch
- **Sync Scheduling and Retry Queues**: Queue failed syncs for retry
- **Sync Status Dashboard**: Monitor sync health and statistics
- **Sync History and Audit Logs**: Track all sync operations
- **Move to Temporal**: Use Temporal for reliable workflow orchestration

### Extensibility Points

The architecture should support:
- Adding new entity types (e.g., credits, bills)
- Adding new sync directions (full bidirectional for all entities)
- Adding new QuickBooks entities (e.g., estimates, credit memos)
- Custom field mapping configuration
- Plugin system for custom transformations

---

## Base Framework Establishment Approach

```
If Quickbook connection is active
    - Plan created in Flexprice
      - Get Price ids & associated meter names (if charge not recurring)
      - Create Item in Quickbook for each price
          - Get Flexprice's Income account
          - Create Item
              - Item Name: {plan name}-{meter name}
              - Item Description: {price_id}
              - Item type: Service
              - Flexprice Income account: {Flexprice's Income account: name, value}
          - Map in entity_integration_mapping table for provider: Quickbooks
            - EntityID: {price_id}
            - EntityType: "price"
            - ProviderType: "quickbooks"
            - ProviderEntityID: {Quickbooks' item id}
            - Metadata:
              - {
                  "synced_at": "2025-11-25T09:50:21Z",
                  "quickbooks_item_name": {item_name}
                }
    
    - Sync Invoice
        - Invoice Created
        - if Invoice sync is enabled 
            - check if customer exists in entity_integration_mapping table for provider: Quickbooks with entity type: "customer"
                - if no, create customer in Quickbook
                    - Customer.DisplayName
                    - Customer.PrimaryEmailAddr.Address
                    - Customer.BillAddr.Line1
                    - Customer.BillAddr.Line2
                    - Customer.BillAddr.City
                    - Customer.BillAddr.Country
                    - Customer.BillAddr.PostalCode
                - Map in entity_integration_mapping table for provider: Quickbooks
                      - EntityID: {customer_id}
                      - EntityType: "customer"
                      - ProviderType: "quickbooks"
                      - ProviderEntityID: {Quickbooks' customer id}
                      - Metadata:
                        - {
                            "synced_at": "2025-11-25T09:50:21Z",
                            "quickbooks_customer_display_name": {customer_display_name}
                            "quickbooks_customer_primary_email_addr_address": {customer_primary_email_addr_address}
                            "quickbooks_customer_bill_addr_line1": {customer_bill_addr_line1}
                            "quickbooks_customer_bill_addr_line2": {customer_bill_addr_line2}
                            "quickbooks_customer_bill_addr_city": {customer_bill_addr_city}
                            "quickbooks_customer_bill_addr_country": {customer_bill_addr_country}
                            "quickbooks_customer_bill_addr_postal_code": {customer_bill_addr_postal_code}
                        }

            - Get customer id

            - create inv in Quickbook
                - CustomerRef: value: {Quickbooks' customer id}
                  - Line:
                    - for each flexprice's invoice line item 
                        - Get price id
                        - fetch the item id from the entity_integration_mapping table for provider: Quickbooks with entity type: "price"
                        - Get item name: item_name and value: item_id
                        - DetailType: SalesItemLineDetail
                        - Amount: {flexprice's invoice line item amount}
                        - UnitPrice: {flexprice's invoice line item price unit amount}
                        - Description: {flexprice's invoice line item display name}
                        - SalesItemLineDetail:
                            - ItemRef:
                                - name: {item_name} //optional
                                - value: {item_id}
                  - Metadata:
                    - Flexprice_invoice_id: {flexprice's invoice id}
                    
```



## Appendix

### QuickBooks API Resources

#### Official Documentation
- [QuickBooks Online API Documentation](https://developer.intuit.com/app/developer/qbo/docs)
- [OAuth 2.0 Authentication](https://developer.intuit.com/app/developer/qbo/docs/develop/authentication-and-authorization)
- [Invoice API Reference](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/all-entities/invoice)
- [Customer API Reference](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/all-entities/customer)
- [Item API Reference](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/all-entities/item)
- [Account API Reference](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/all-entities/account)
- [Payment API Reference](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/all-entities/payment)
- [Preferences API Reference](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/all-entities/preferences)
- [Webhooks Documentation](https://developer.intuit.com/app/developer/qbo/docs/develop/webhooks)

#### Best Practices and Guides
- [QuickBooks API Best Practices](https://blogs.intuit.com/2018/09/10/quickbooks-online-api-best-practices/) - CDC API, Webhooks, Optimization
- [Intuit API Optimization Best Practices](https://blogs.intuit.com/2025/08/11/best-practices-for-intuit-api-optimization-part-1/) - Performance optimization
- [Smoother Transactions with Intuit API](https://blogs.intuit.com/2025/04/24/smoother-transactions-leveraging-intuit-api-entities/) - Error handling, validation
- [QuickBooks API Setup Guide](https://coefficient.io/quickbooks-api/setup-quickbooks-api-integration) - OAuth, versioning, rate limits
- [Platform Requirements](https://developer.intuit.com/app/developer/qbo/docs/go-live/publish-app/platform-requirements) - Compliance, data privacy

#### Integration Guides
- [QuickBooks Integration Guide](https://www.avontus.com/media/10710/QuickBooksGuide.pdf) - Customer matching, line items, data validation

### Related Documents

- [Stripe Integration Documentation](./STRIPE_INTEGRATION_DOCUMENTATION.md)
- [Integration Architecture](./integration_architecture.md)
- [Entity Integration Mapping Design](./entity_integration_mapping.md)

### Glossary

- **QBO**: QuickBooks Online
- **Realm ID**: QuickBooks Company ID (required for all API calls)
- **Sync Token**: QuickBooks entity version token for optimistic locking (required for updates)
- **Item**: QuickBooks product or service (must have Income Account reference) - [Item API](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/most-commonly-used/item)
- **Line Item**: Individual entry on an invoice - must reference an Item via `ItemRef`
- **Minor Version**: API version parameter (e.g., `minorversion=70`) - always include in requests
- **CDC API**: Change Data Capture API for tracking entity changes
- **Book Close Date**: Accounting period close date - transactions cannot be created before this date
- **Income Account**: Account type required for Service items in QuickBooks - [Account API](https://developer.intuit.com/app/developer/qbo/docs/api/accounting/all-entities/account) (read-only, must exist in QuickBooks)
- **SyncToken**: QuickBooks entity version token for optimistic locking (required for updates)

---

## Document History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2025-11-24 | Tsage | Initial PRD creation |
| 1.1 | 2025-11-25 | Tsage | Define Workflow |
| 1.2 | 2025-12-04 | Tsage | Add 2-way payment sync implementation details, security audit, testing checklist |
| 1.3 | 2025-12-05 | Tsage | Simplify to inbound-only payment reconciliation, remove outbound sync |


---

**End of Document**

