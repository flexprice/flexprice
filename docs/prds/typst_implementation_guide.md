# FlexPrice Typst Template Enhancement Guide

This document outlines the approach for enhancing FlexPrice's PDF generation capabilities through the Typst template system:

1. Adding new template designs for existing data models (invoices)
2. Supporting new data models beyond invoices (e.g., quotations, reports)

## Current Implementation Overview

FlexPrice currently uses a two-file approach for invoice PDF generation:

- `invoice.typ` - Adapter that maps JSON data to Typst variables
- `default.typ` - Template implementation with styling and layout

```
/assets/typst-templates/
├── invoice.typ      # Adapter: Converts JSON to Typst variables
└── default.typ      # Template: Defines styling and layout
```

The Go code generates JSON data and passes it to the Typst compiler, which uses `invoice.typ` as the entry point, which then applies the `default.typ` template.

## 1. Adding New Template Designs for Invoices

To add a new "classic" template while maintaining compatibility with the existing invoice data model:

### Step 1: Create the classic template file

Create a new file at `/assets/typst-templates/classic.typ` with styling inspired by traditional invoice designs:

```typst
#let classic-invoice(
  language: "en",
  currency: "$",
  title: none,
  banner-image: none,
  invoice-status: "DRAFT",
  invoice-number: none,
  issuing-date: none,
  due-date: none,
  amount-due: 0,
  notes: "",
  biller: (:),
  recipient: (:),
  keywords: (),
  styling: (:),
  items: (),
  vat: 0,
  doc,
) = {
  // Classic template styling and layout implementation
  // Keep the same parameter structure as default-invoice

  // Set styling defaults - use different colors, fonts, etc.
  styling.font = styling.at("font", default: "Georgia")
  styling.font-size = styling.at("font-size", default: 10pt)
  styling.primary-color = styling.at("primary-color", default: rgb("#333333"))
  // ...other styling defaults

  // Document layout specific to classic style
  // ...
}
```

### Step 2: Modify the invoice adapter to support template selection

Update `/assets/typst-templates/invoice.typ` to dynamically select the template based on styling parameter:

```typst
#import "default.typ" as default_template
#import "classic.typ" as classic_template

#let invoice-data = json(sys.inputs.path)

// Select template based on styling.template parameter
#let template-function = if "styling" in invoice-data and
                         "template" in invoice-data.styling and
                         invoice-data.styling.template == "classic" {
  classic_template.classic-invoice
} else {
  default_template.default-invoice
}

// Apply the selected template with the data
#show: template-function.with(
  currency: if "currency" in invoice-data {
    invoice-data.currency
  },
  // ... other parameter mappings (unchanged)
)
```

### Step 3: Pass template selection in the JSON data

When generating invoice PDFs, include a template selection parameter:

```json
{
  "invoice_number": "INV-2025-0042",
  "styling": {
    "template": "classic", // Specify template name here
    "font": "Georgia",
    "secondary_color": "#555555"
  }
  // ... other invoice data
}
```

### Step 4: Testing the new template

Test the implementation by generating invoices with different template selections:

```go
// Generate invoice with default template
invoiceData := &InvoiceData{
    // ... invoice fields
}

// Generate invoice with classic template
invoiceData := &InvoiceData{
    // ... invoice fields
    Styling: &Styling{
        Template: "classic",
        Font:     "Georgia",
    },
}
```

## 2. Supporting New Data Models (Quotations)

To extend the system for generating quotation PDFs:

### Step 1: Define the quotation data model in Go

```go
// Quotation represents a price quote document
type Quotation struct {
    QuotationID     string    `json:"quotation_id"`
    QuotationNumber string    `json:"quotation_number"`
    IssueDate       string    `json:"issue_date"`
    ExpiryDate      string    `json:"expiry_date"`
    Customer        Customer  `json:"customer"`
    Vendor          Vendor    `json:"vendor"`
    Items           []Item    `json:"items"`
    SubTotal        float64   `json:"subtotal"`
    TaxAmount       float64   `json:"tax_amount"`
    TaxRate         float64   `json:"tax_rate"`
    Total           float64   `json:"total"`
    Terms           string    `json:"terms"`
    Notes           string    `json:"notes"`
    Currency        string    `json:"currency"`
    Styling         *Styling  `json:"styling,omitempty"`
}
```

### Step 2: Create the quotation adapter template

Create a new file at `/assets/typst-templates/quotation.typ`:

```typst
#import "quotation-default.typ" as default_template
// Import other quotation templates as needed

#let quotation-data = json(sys.inputs.path)

// Select template based on styling parameter
#let template-function = if "styling" in quotation-data and
                         "template" in quotation-data.styling and
                         quotation-data.styling.template == "formal" {
  // Import formal template when available
  // formal_template.formal-quotation
  default_template.default-quotation  // Fallback to default for now
} else {
  default_template.default-quotation
}

// Apply the selected template with the quotation data
#show: template-function.with(
  quotation-number: quotation-data.quotation_number,
  issue-date: quotation-data.issue_date,
  expiry-date: quotation-data.expiry_date,
  // Map other quotation fields
  // ...
)
```

### Step 3: Create the default quotation template

Create `/assets/typst-templates/quotation-default.typ`:

```typst
#let default-quotation(
  language: "en",
  currency: "$",
  title: none,
  company-logo: none,
  quotation-number: none,
  issue-date: none,
  expiry-date: none,
  customer: (:),
  vendor: (:),
  items: (),
  subtotal: 0,
  tax-rate: 0,
  tax-amount: 0,
  total: 0,
  terms: "",
  notes: "",
  styling: (:),
  doc,
) = {
  // Quotation template implementation
  // ...
}
```

### Step 4: Update the Go service to handle quotation generation

```go
// QuotationService handles quotation PDF generation
type QuotationService struct {
    typstBinary      string
    templateDir      string
    fontDir          string
    outputDir        string
    quotationTemplate string
}

// NewQuotationService creates a new quotation service
func NewQuotationService(config *Config) *QuotationService {
    return &QuotationService{
        typstBinary:       config.TypstBinary,
        templateDir:       config.TemplateDir,
        fontDir:           config.FontDir,
        outputDir:         config.OutputDir,
        quotationTemplate: "quotation.typ", // Use the quotation adapter
    }
}

// GenerateQuotationPDF generates a PDF quotation
func (s *QuotationService) GenerateQuotationPDF(quotation *Quotation) (string, error) {
    // Similar implementation to invoice generation but using the quotation template
    // ...
}
```

## Future Template Management System

For a future template management system, consider these features:

1. **Template Registry**: Maintain a registry of available templates for each document type.

2. **Database Storage**: Store templates in the database for dynamic updates without code changes.

3. **Template Editor**: Provide a UI for customers to customize templates.

4. **Preview System**: Allow previewing template changes before saving.

5. **Versioning**: Maintain version history of templates for rollback capability.

6. **Template Inheritance**: Support base templates that can be extended.

7. **Dynamic Components**: Create reusable components that can be shared across templates.

8. **Validation**: Validate templates against their data models to ensure compatibility.

## Implementation Path

1. **Current**: Single invoice template with hardcoded styling
2. **Next**: Multiple invoice templates selectable via styling.template parameter
3. **Future**: Support for new document types (quotations, reports, etc.)
4. **Advanced**: Template management system with database storage and UI

## Code examples

Example 1: Invoice adapter with template selection

```typst
// Example 1: Invoice adapter with template selection
// File: /assets/typst-templates/invoice.typ

#import "default.typ" as default_template
#import "classic.typ" as classic_template
#import "modern.typ" as modern_template

#let invoice-data = json(sys.inputs.path)

// Helper function to safely get nested values
#let get-value(obj, key, default: none) = {
  if key in obj { obj.at(key) } else { default }
}

// Select template based on styling.template parameter
#let template-function = {
  if "styling" in invoice-data {
    let template-name = get-value(invoice-data.styling, "template")
    if template-name == "classic" {
      classic_template.classic-invoice
    } else if template-name == "modern" {
      modern_template.modern-invoice
    } else {
      default_template.default-invoice
    }
  } else {
    default_template.default-invoice
  }
}

// Apply the selected template with the data
#show: template-function.with(
  currency: get-value(invoice-data, "currency"),
  banner-image: if "banner_image" in invoice-data {
    image(invoice-data.banner_image, width: 30%)
  },
  invoice-status: invoice-data.invoice_status,
  invoice-number: invoice-data.invoice_number,
  issuing-date: invoice-data.issuing_date,
  due-date: invoice-data.due_date,
  amount-due: invoice-data.amount_due,
  notes: get-value(invoice-data, "notes", default: ""),
  vat: get-value(invoice-data, "vat", default: 0),
  biller: (
    name: invoice-data.biller.name,
    email: get-value(invoice-data.biller, "email"),
    help-email: get-value(invoice-data.biller, "help_email"),
    address: (
      street: get-value(invoice-data.biller.address, "street"),
      city: get-value(invoice-data.biller.address, "city"),
      postal-code: get-value(invoice-data.biller.address, "postal_code"),
      state: get-value(invoice-data.biller.address, "state"),
      country: get-value(invoice-data.biller.address, "country"),
    ),
    payment-instructions: get-value(invoice-data.biller, "payment_instructions"),
  ),
  recipient: (
    name: invoice-data.recipient.name,
    email: get-value(invoice-data.recipient, "email"),
    address: (
      street: get-value(invoice-data.recipient.address, "street"),
      city: get-value(invoice-data.recipient.address, "city"),
      postal-code: get-value(invoice-data.recipient.address, "postal_code"),
      state: get-value(invoice-data.recipient.address, "state"),
      country: get-value(invoice-data.recipient.address, "country"),
    )
  ),
  items: invoice-data.line_items,
  styling: (
    font: get-value(
      invoice-data.styling, "font",
      default: "Inter"
    ),
    secondary-color: get-value(
      invoice-data.styling, "secondary_color",
      default: "#919191"
    ),
  )
)

```

Example 2: Classic invoice template implementation

```typst
// Example 2: Classic invoice template implementation
// File: /assets/typst-templates/classic.typ

#let format-date = (date-str) => {
  let parts = date-str.split("-")
  if parts.len() != 3 {
    panic("Invalid date format: " + date-str)
  }

  let month-names = (
    "January", "February", "March", "April", "May", "June",
    "July", "August", "September", "October", "November", "December"
  )

  let month-idx = int(parts.at(1)) - 1

  str(int(parts.at(2))) + " " + month-names.at(month-idx) + ", " + parts.at(0)
}

#let classic-invoice(
  language: "en",
  currency: "$",
  title: none,
  banner-image: none,
  invoice-status: "DRAFT",
  invoice-number: none,
  issuing-date: none,
  due-date: none,
  amount-due: 0,
  notes: "",
  biller: (:),
  recipient: (:),
  keywords: (),
  styling: (:),
  items: (),
  vat: 0,
  doc,
) = {
  // Set styling defaults for classic template
  styling.font = styling.at("font", default: "Times New Roman")
  styling.font-size = styling.at("font-size", default: 10pt)
  styling.primary-color = styling.at("primary-color", default: rgb("#333333"))
  styling.secondary-color = rgb(styling.at("secondary-color", default: rgb("#666666")))
  styling.accent-color = rgb(styling.at("accent-color", default: rgb("#8b0000")))
  styling.line-color = styling.at("line-color", default: rgb("#dddddd"))

  // Document properties
  set document(
    title: if title != none { title } else { "Invoice " + invoice-number },
    keywords: keywords,
  )

  // Page settings
  set page(
    margin: (top: 20mm, right: 15mm, bottom: 20mm, left: 15mm),
    numbering: "1",
    number-align: center,
    header: locate(loc => {
      if loc.page() != 1 {
        text(fill: styling.primary-color)[
          *Invoice #invoice-number* #h(1fr) *Page #loc.page()*
        ]
        line(length: 100%, stroke: styling.line-color)
      }
    }),
  )

  // Text settings
  set text(
    font: styling.font,
    size: styling.font-size,
    fill: styling.primary-color,
  )

  // Classic header
  grid(
    columns: (1fr, 1fr),
    gutter: 2em,
    [
      #text(size: 2em, weight: "bold")[INVOICE]

      #v(0.5em)

      #if invoice-status == "DRAFT" {
        rect(
          inset: (x: 0.5em, y: 0.3em),
          radius: 3pt,
          stroke: styling.accent-color,
          fill: rgb("#fff0f0"),
          [#text(size: 0.9em, fill: styling.accent-color)[DRAFT]]
        )
      } else if invoice-status == "VOID" {
        rect(
          inset: (x: 0.5em, y: 0.3em),
          radius: 3pt,
          stroke: rgb("#777"),
          fill: rgb("#f0f0f0"),
          [#text(size: 0.9em, fill: rgb("#777"))[VOID]]
        )
      }

      #v(1em)

      #grid(
        columns: (auto, 1fr),
        gutter: 1em,
        align: (right, left),
        [*Invoice Number:*], [#invoice-number],
        [*Issue Date:*], [#format-date(issuing-date)],
        [*Due Date:*], [#format-date(due-date)],
      )
    ],
    [
      #align(right)[
        #if banner-image != none {
          banner-image
        } else {
          text(size: 1.5em, weight: "bold")[#biller.name]
        }
      ]
    ]
  )

  // Line separator
  v(1em)
  line(length: 100%, stroke: styling.accent-color + 2pt)
  v(2em)

  // Biller and recipient information
  grid(
    columns: (1fr, 1fr),
    gutter: 2em,
    [
      #text(size: 1.2em, weight: "medium")[From:]
      #v(0.5em)
      #text(weight: "bold")[#biller.name] \
      #if "email" in biller { [#biller.email] \ }
      #if "address" in biller {
        if "street" in biller.address { [#biller.address.street] \ }
        [#biller.address.city #if "state" in biller.address {
          [, #biller.address.state]
        } #biller.address.postal-code] \
        #if "country" in biller.address { [#biller.address.country] \ }
      }
    ],
    [
      #text(size: 1.2em, weight: "medium")[Bill To:]
      #v(0.5em)
      #text(weight: "bold")[#recipient.name] \
      #if "email" in recipient { [#recipient.email] \ }
      #if "address" in recipient {
        if "street" in recipient.address { [#recipient.address.street] \ }
        [#recipient.address.city #if "state" in recipient.address {
          [, #recipient.address.state]
        } #recipient.address.postal-code] \
        #if "country" in recipient.address { [#recipient.address.country] \ }
      }
    ],
  )

  v(2em)

  // Items table
  rect(
    width: 100%,
    inset: 0pt,
    stroke: styling.line-color,
    [
      #table(
        columns: (1fr, 2fr, 1fr, 1fr, 1fr),
        inset: 10pt,
        align: (left, left, center, center, right),
        stroke: styling.line-color,
        fill: (c, r) => if r == 0 { rgb("#f5f5f5") } else { none },
        table.header(
          fill: styling.primary-color,
          text(fill: white, weight: "bold")[Item],
          text(fill: white, weight: "bold")[Description],
          text(fill: white, weight: "bold")[Period],
          text(fill: white, weight: "bold")[Quantity],
          text(fill: white, weight: "bold")[Amount],
        ),
        ..items.map((item) => {
          (
            text(weight: "medium")[#item.at("plan_display_name", default: "Plan")],
            item.at("description", default: "Service"),
            if "period_start" in item and "period_end" in item {
              let start = format-date(item.period_start)
              let end = format-date(item.period_end)
              [#start to #end]
            } else {
              [--]
            },
            str(item.quantity),
            [#currency #calc.round(item.quantity * item.amount, digits: 2)],
          )
        }).flatten(),
      )
    ]
  )

  v(1em)

  // Totals
  align(right)[
    #table(
      columns: (auto, auto),
      inset: (x: 12pt, y: 8pt),
      align: (left, right),
      stroke: none,
      [Subtotal:], [#currency #amount-due],
      [Tax (#calc.round(vat * 100, digits: 2)%):],
      if vat == 0 { [--] } else {
        [#currency #calc.round(amount-due * vat, digits: 2)]
      },
      [*Total:*], [
        #box(
          fill: styling.accent-color,
          inset: (x: 12pt, y: 6pt),
          radius: 4pt,
          text(fill: white, weight: "bold")[
            #currency #calc.round(amount-due + (amount-due * vat), digits: 2)
          ]
        )
      ],
    )
  ]

  v(2em)

  // Payment information
  if invoice-status == "FINALIZED" {
    [== Payment Information]
    v(0.5em)

    [Please complete payment by *#format-date(due-date)*.]

    if "payment-instructions" in biller {
      v(0.5em)
      biller.payment-instructions
    }
  }

  // Notes
  if notes != "" {
    v(1.5em)
    rect(
      width: 100%,
      fill: rgb("#f9f9f9"),
      inset: 12pt,
      radius: 4pt,
      [
        #text(weight: "medium", fill: styling.accent-color)[Notes:] \
        #notes
      ]
    )
  }

  // Footer
  v(2em)
  align(center)[
    #line(length: 30%, stroke: styling.line-color)
    #v(0.5em)
    #text(size: 8pt)[
      #biller.name #if "help-email" in biller {
        [• #biller.help-email]
      }
    ]
  ]

  doc
}
```

Example 3: Quotation adapter template for new data model

```typst
// Example 3: Quotation adapter template for new data model
// File: /assets/typst-templates/quotation.typ

#import "quotation-default.typ" as default_template

#let quotation-data = json(sys.inputs.path)

// Helper function to safely get nested values
#let get-value(obj, key, default: none) = {
  if key in obj { obj.at(key) } else { default }
}

// Select template based on styling
#let template-function = {
  if "styling" in quotation-data {
    let template-name = get-value(quotation-data.styling, "template")
    if template-name == "formal" {
      // Import when available
      // formal_template.formal-quotation
      default_template.default-quotation
    } else {
      default_template.default-quotation
    }
  } else {
    default_template.default-quotation
  }
}

// Apply the selected template with the quotation data
#show: template-function.with(
  quotation-number: quotation-data.quotation_number,
  issue-date: quotation-data.issue_date,
  expiry-date: quotation-data.expiry_date,
  currency: get-value(quotation-data, "currency", default: "$"),
  company-logo: if "company_logo" in quotation-data {
    image(quotation-data.company_logo, width: 30%)
  },
  customer: (
    name: quotation-data.customer.name,
    email: get-value(quotation-data.customer, "email"),
    address: (
      street: get-value(quotation-data.customer.address, "street"),
      city: get-value(quotation-data.customer.address, "city"),
      postal-code: get-value(quotation-data.customer.address, "postal_code"),
      state: get-value(quotation-data.customer.address, "state"),
      country: get-value(quotation-data.customer.address, "country"),
    )
  ),
  vendor: (
    name: quotation-data.vendor.name,
    email: get-value(quotation-data.vendor, "email"),
    address: (
      street: get-value(quotation-data.vendor.address, "street"),
      city: get-value(quotation-data.vendor.address, "city"),
      postal-code: get-value(quotation-data.vendor.address, "postal_code"),
      state: get-value(quotation-data.vendor.address, "state"),
      country: get-value(quotation-data.vendor.address, "country"),
    )
  ),
  items: quotation-data.items,
  subtotal: quotation-data.subtotal,
  tax-rate: get-value(quotation-data, "tax_rate", default: 0),
  tax-amount: get-value(quotation-data, "tax_amount", default: 0),
  total: quotation-data.total,
  terms: get-value(quotation-data, "terms", default: ""),
  notes: get-value(quotation-data, "notes", default: ""),
  styling: (
    font: get-value(
      quotation-data.styling, "font",
      default: "Inter"
    ),
    primary-color: get-value(
      quotation-data.styling, "primary_color",
      default: "#2d5bff"
    ),
  )
)
```
