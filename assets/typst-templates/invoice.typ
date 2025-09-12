#import "default.typ" as template

#let invoice-data = json(sys.inputs.path)

#show: template.default-invoice.with(
  currency: invoice-data.at("currency", default: "$"),
  banner-image: if "banner_image" in invoice-data {
    image(invoice-data.banner_image, width: 30%)
  },
  invoice-status: invoice-data.at("invoice_status", default: "DRAFT"),
  invoice-number: invoice-data.at("invoice_number", default: ""),
  issuing-date: invoice-data.at("issuing_date", default: ""),
  due-date: invoice-data.at("due_date", default: ""),
  amount-due: invoice-data.at("amount_due", default: 0),
  notes: invoice-data.at("notes", default: ""),
  subtotal: invoice-data.at("subtotal", default: 0),
  discount: invoice-data.at("total_discount", default: 0),
  tax: invoice-data.at("total_tax", default: 0),
  biller: (
    name: invoice-data.at("biller", default: (:)).at("name", default: ""),
    email: invoice-data.at("biller", default: (:)).at("email", default: ""),
    help-email: invoice-data.at("biller", default: (:)).at("help_email", default: ""),
    address: (
      street: invoice-data.at("biller", default: (:)).at("address", default: (:)).at("street", default: ""),
      city: invoice-data.at("biller", default: (:)).at("address", default: (:)).at("city", default: ""),
      postal-code: invoice-data.at("biller", default: (:)).at("address", default: (:)).at("postal_code", default: ""),
      state: invoice-data.at("biller", default: (:)).at("address", default: (:)).at("state", default: ""),
      country: invoice-data.at("biller", default: (:)).at("address", default: (:)).at("country", default: ""),
    ),
    payment-instructions: invoice-data.at("biller", default: (:)).at("payment_instructions", default: ""),
  ),
  recipient: (
    name: invoice-data.at("recipient", default: (:)).at("name", default: ""),
    email: invoice-data.at("recipient", default: (:)).at("email", default: ""),
    address: (
      street: invoice-data.at("recipient", default: (:)).at("address", default: (:)).at("street", default: ""),
      city: invoice-data.at("recipient", default: (:)).at("address", default: (:)).at("city", default: ""),
      postal-code: invoice-data.at("recipient", default: (:)).at("address", default: (:)).at("postal_code", default: ""),
      state: invoice-data.at("recipient", default: (:)).at("address", default: (:)).at("state", default: ""),
      country: invoice-data.at("recipient", default: (:)).at("address", default: (:)).at("country", default: ""),
    )
  ),
  items: invoice-data.at("line_items", default: ()),
  applied-taxes: invoice-data.at("applied_taxes", default: ()),
  applied-discounts: invoice-data.at("applied_discounts", default: ()),
  styling: (
    font: if "styling" in invoice-data and "font" in invoice-data.styling {
      invoice-data.styling.font
    } else {
      "Inter"
    },
    secondary-color: if "styling" in invoice-data and "secondary_color" in invoice-data.styling {
      invoice-data.styling.secondary_color
    } else {
      "#919191"
    },
  )
)