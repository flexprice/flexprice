#let parse-date = (date-str) => {
  let parts = date-str.split("-")
  if parts.len() != 3 {
    panic(
      "Invalid date string: " + date-str + "\n" +
      "Expected format: YYYY-MM-DD"
    )
  }
  datetime(
    year: int(parts.at(0)),
    month: int(parts.at(1)),
    day: int(parts.at(2)),
  )
}

#let format-date = (date) => {
  let month-names = (
    "Jan", "Feb", "Mar", "Apr", "May", "Jun",
    "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"
  )

  let day = if date.day() < 10 {
    "0" + str(date.day())
  } else {
    str(date.day())
  }
  let month = month-names.at(date.month() - 1)
  let year = str(date.year()).slice(2) // Get last 2 digits

  day + " " + month + " " + year
}


#let format-number = (num, precision: 2) => {
  let str-num = str(num)
  let parts = str-num.split(".")
  let integer-part = str(parts.at(0))
  let decimal-part = if parts.len() > 1 { 
    let raw-decimal = parts.at(1)
    // Ensure exactly the specified precision decimal places
    if raw-decimal.len() < precision {
      let zeros = ""
      for i in range(precision - raw-decimal.len()) {
        zeros += "0"
      }
      raw-decimal + zeros
    } else if raw-decimal.len() > precision {
      raw-decimal.slice(0, precision)
    } else {
      raw-decimal
    }
  } else { 
    let zeros = ""
    for i in range(precision) {
      zeros += "0"
    }
    zeros
  }

  // Add commas every 3 digits from the right
  let chars = integer-part.rev().clusters()
  let result = ""
  for (i, c) in chars.enumerate() {
    if calc.rem-euclid(i, 3) == 0 and i != 0 {
      result += ","
    }
    result += c
  }

  if precision > 0 {
    result.rev() + "." + decimal-part
  } else {
    result.rev()
  }
}

#let format-currency = (num, precision: 2) => {
  let multiplier = calc.pow(10.0, precision)
  let rounded = calc.round(num * multiplier) / multiplier
  format-number(rounded, precision: precision)
}

// Define the default-invoice function
#let default-invoice(
  language: "en",
  currency: "$",
  precision: 2,
  title: none,
  banner-image: none,
  invoice-status: "DRAFT",       // DRAFT, FINALIZED, VOIDED
  invoice-number: none,
  issuing-date: "",
  due-date: none,
  service-period: none,              // Service period for billing
  amount-due: 0,  
  notes: "",
  biller: (:),                  // Company info
  recipient: (:),               // Customer info
  keywords: (),
  styling: (:),                 // font, font-size, margin (sets defaults below)
  items: (),                    // Line items
  applied-taxes: (),            // Applied taxes breakdown
  applied-discounts: (),        // Applied discounts breakdown
  subtotal: 0,                  // Subtotal before discounts and tax
  discount: 0,                  // Total discounts
  tax: 0,                       // Total tax
  doc,
) = {
  // Set styling defaults
  styling.font = styling.at("font", default: "Inter")
  styling.font-size = styling.at("font-size", default: 9pt)
  styling.primary-color = styling.at("primary-color", default: rgb("#000000"))
  styling.margin = styling.at("margin", default: (
    top: 12mm,
    right: 10mm,
    bottom: 10mm,
    left: 10mm,
  ))
  styling.line-color = styling.at("line-color", default: rgb("#e0e0e0"))
  styling.secondary-color = rgb(styling.at("secondary-color", default: rgb("#707070")))
  styling.table-header-bg = rgb("#f8f9fa")
  styling.table-header-color = rgb("#2c3e50")
  styling.table-header-border = rgb("#dee2e6")

  // Set document properties
  let issuing-date-value = if issuing-date != "" { issuing-date }
        else { datetime.today().display("[year]-[month]-[day]") }

  // Initialize service period from items if not provided
  let service-period-value = if service-period != none { 
    service-period 
  } else if items.len() > 0 {
    // Extract period from first item that has period information
    let first-item-with-period = items.find(item => 
      item.at("period_start", default: "") != "" and item.at("period_end", default: "") != ""
    )
    if first-item-with-period != none {
      let period-start = first-item-with-period.at("period_start")
      let period-end = first-item-with-period.at("period_end")
      format-date(parse-date(period-start)) + " - " + format-date(parse-date(period-end))
    } else {
      "--"
    }
  } else {
    "--"
  }

  set document(
    title: if title != none { title } else { "Invoice " + invoice-number },
    keywords: keywords,
    date: parse-date(issuing-date-value),
  )

  set page(
    margin: styling.margin,
    numbering: none,
  )

  set text(
    font: styling.font,
    size: styling.font-size,
  )

  set table(stroke: none)

  // Document header with banner image if provided
  if banner-image != none {
    grid(
      columns: (1fr, auto),
      align: (left + horizon, right + horizon),
      gutter: 0em,
      [
        #banner-image
      ],
      [
        #text(weight: "medium", size: 1.6em)[Invoice]
      ]
    )
    v(0.8em)
  } else {
    text(weight: "bold", size: 2.2em, fill: styling.primary-color)[Invoice]
  }

  v(0.8em)

  // Invoice details in vertical format
  [
    #text(weight: "medium", size: 10pt)[Invoice number:] #text(weight: "regular", size: 10pt, fill: rgb("#666666"))[#invoice-number] \
    #text(weight: "medium", size: 10pt)[Date of issue:] #text(weight: "regular", size: 10pt, fill: rgb("#666666"))[#issuing-date-value] \
    #text(weight: "medium", size: 10pt)[Date due:] #text(weight: "regular", size: 10pt, fill: rgb("#666666"))[#due-date] \
    #text(weight: "medium", size: 10pt)[Service period:] #text(weight: "regular", size: 10pt, fill: rgb("#666666"))[#service-period-value]
  ]

  line(length: 100%, stroke: 0.5pt + styling.line-color)

  v(1.2em)

  // Biller and Recipient Information
  grid(
    columns: (1fr, 1fr),
    gutter: 0.8em,
    [
      #text(weight: "semibold", size: 11pt)[From]
      #v(0.3em)
      #text(weight: "semibold", size: 10pt)[#biller.name] \
      #text(weight: "regular", size: 9pt, fill: rgb("#666666"))[#biller.at("email", default: "--")] \
      #text(weight: "regular", size: 9pt, fill: rgb("#666666"))[#biller.at("address", default: (:)).at("street", default: "--")] \
      #text(weight: "regular", size: 9pt, fill: rgb("#666666"))[#biller.at("address", default: (:)).at("city", default: "--")] \
      #text(weight: "regular", size: 9pt, fill: rgb("#666666"))[#biller.at("address", default: (:)).at("postal-code", default: "--")]
    ],
    [
      #text(weight: "semibold", size: 11pt)[Bill to]
      #v(0.3em)
      #text(weight: "semibold", size: 10pt)[#recipient.name] \
      #text(weight: "regular", size: 9pt, fill: rgb("#666666"))[#recipient.at("email", default: "--")] \
      #text(weight: "regular", size: 9pt, fill: rgb("#666666"))[#recipient.at("address", default: (:)).at("street", default: "--")] \
      #text(weight: "regular", size: 9pt, fill: rgb("#666666"))[#recipient.at("address", default: (:)).at("city", default: "--")] \
      #text(weight: "regular", size: 9pt, fill: rgb("#666666"))[#recipient.at("address", default: (:)).at("postal-code", default: "--")]
    ]
  )

  v(1.5em)
  let last-group = ""
  
  // Display items
  for (i, item) in items.enumerate() {
    let group-name = item.at("group", default: "")
    let normalized-group = if group-name == "" or group-name == "--" { "" } else { group-name }
    
    // Show group header when group changes from empty to a group, or when group name changes
    if normalized-group != "" and normalized-group != last-group {
      if i > 0 {
        v(0.5em)
      }
      table(
        columns: (3fr, 1.5fr, 0.8fr, 1.2fr),
        inset: (top: 8pt, bottom: 8pt, left: 8pt, right: 8pt),
        align: left,
        fill: rgb("#e9ecef"),
        stroke: (x, y) => (
          bottom: 1pt + rgb("#d0d5db"),
        ),
        [#text(weight: "semibold", size: 10pt, fill: rgb("#2c3e50"))[#normalized-group]],
        [],
        [],
        []
      )
    }
    
    let line-total = item.amount
    let amount-display = if line-total < 0 {
      [−#currency #format-currency(calc.abs(line-total), precision: precision)]
    } else {
      [#currency #format-currency(line-total, precision: precision)]
    }
    
    let has_period = item.at("period_start", default: "") != "" and item.at("period_end", default: "") != ""
    let interval = if has_period {
      [#format-date(parse-date(item.at("period_start"))) - #format-date(parse-date(item.at("period_end")))]
    } else {
      "-"
    }
    
    if i == 0 {
      // Add header only for the first item
      table(
        columns: (3fr, 1.5fr, 0.8fr, 1.2fr),
        inset: (top: 12pt, bottom: 12pt, left: 8pt, right: 8pt),
        align: (left, left, center, right),
        fill: (x, y) => if y == 0 { rgb("#f8f9fa") } else { white },
        stroke: (x, y) => (
          bottom: if y == 0 { 1pt + rgb("#e9ecef") } else { 0.5pt + rgb("#e9ecef") },
        ),
      table.header(
        [#text(weight: "semibold", size: 10pt, fill: rgb("#2c3e50"))[Item]],
        [#text(weight: "semibold", size: 10pt, fill: rgb("#2c3e50"))[Interval]],
        [#text(weight: "semibold", size: 10pt, fill: rgb("#2c3e50"))[Quantity]],
        [#text(weight: "semibold", size: 10pt, fill: rgb("#2c3e50"))[Amount]],
      ),
        [#item.at("display_name", default: item.at("plan_display_name", default: "Plan"))], 
        [#interval],
        [#format-number(item.quantity)],
        [#amount-display]
      )
    } else {
      table(
        columns: (3fr, 1.5fr, 0.8fr, 1.2fr),
        inset: (top: 10pt, bottom: 10pt, left: 8pt, right: 8pt),
        align: (left, left, center, right),
        fill: white,
        stroke: (x, y) => (
          bottom: 0.5pt + rgb("#e9ecef"),
        ),
        [#item.at("display_name", default: item.at("plan_display_name", default: "Plan"))], 
        [#interval],
        [#format-number(item.quantity)],
        [#amount-display]
      )
    }
    
    // Check if this item has usage breakdown and add it directly below
    let has_usage_breakdown = "usage_breakdown" in item and item.usage_breakdown != none and item.usage_breakdown.len() > 0
    
    if has_usage_breakdown {
      for usage_item in item.usage_breakdown {
        if "grouped_by" in usage_item {
          let grouped_by = usage_item.at("grouped_by", default: none)
          let cost = usage_item.at("cost", default: none)
          let usage = usage_item.at("usage", default: none)
          
          if grouped_by != none {
            let resource_name = grouped_by.at("resource_name", default: 
              grouped_by.at("type", default: 
                grouped_by.at("feature_id", default: 
                  grouped_by.at("source", default: "—"))))
            
            let cost_value = 0.0
            if cost != none {
              cost_value = float(str(cost))
            }
            
            let usage_value = 0.0
            if usage != none {
              usage_value = float(str(usage))
            }
            
            table(
              columns: (3fr, 1.5fr, 0.8fr, 1.2fr),
              inset: (top: 6pt, bottom: 6pt, left: 2em, right: 8pt),
              align: (left, left, center, right),
              fill: rgb("#f8f9fa"),
              stroke: none,
              [#text(size: 0.85em, fill: rgb("#666666"), weight: "regular")[└─ #resource_name]],
              [],
              [#text(size: 0.9em, weight: "medium")[#format-number(usage_value)]],
            [#text(size: 0.9em, weight: "medium")[#currency #format-currency(cost_value, precision: precision)]]
            )
          }
        }
      }
    }
    
    // Update last group and add spacing
    last-group = normalized-group
    if i < items.len() - 1 {
      v(0.5em)
    }
  }
  
  // End of line items with usage breakdowns

  v(1em)

  // Totals
  align(right,
    table(
      columns: 2,
      align: (left, right),
      inset: 6pt,
      stroke: none,
      // Always show subtotal
      [Subtotal], [#currency#format-currency(subtotal, precision: precision)],
      
      // Show discount row only if there's a discount
      ..if discount > 0 { ([Discount], [−#currency#format-currency(discount, precision: precision)]) } else { () },
      
      // Show tax row only if there's tax
      ..if tax > 0 { ([Tax], [#currency#format-currency(tax, precision: precision)]) } else { () },
      
      table.hline(stroke: 0.5pt + black),
      [*Net Payable*], [*#currency#format-currency(subtotal - discount + tax, precision: precision)*],
    )
  )

  v(2em)

  // Applied Discounts section (if any discounts were applied)
  if applied-discounts.len() > 0 {
    text(weight: "medium", size: 1.1em)[Applied Discounts]
    v(0.5em)

    table(
      columns: (1fr, 1fr, 1fr, 1fr, 1fr),
      inset: 8pt,
      align: (left, left, right, right, left),
      fill: white,
      stroke: (x, y) => (
        bottom: if y == 0 { 1pt + styling.line-color } else { 1pt + styling.line-color },
      ),
      table.header(
        [*Discount Name*],
        [*Type*],
        [*Value*],
        [*Discount Amount*],
        [*Line Item Ref.*],
      ),
      ..applied-discounts.map((discount) => {
        let value-display = if discount.type == "percentage" {
          [#format-currency(discount.value, precision: precision)%]
        } else {
          [#currency#format-currency(discount.value, precision: precision)]
        }
        
        (
          discount.discount_name,
          discount.type,
          value-display,
          [#currency#format-currency(discount.discount_amount, precision: precision)],
          discount.line_item_ref,
        )
      }).flatten(),
    )

    v(1em)
  }

  // Applied Taxes section (if any taxes were applied)
  if applied-taxes.len() > 0 {
    text(weight: "medium", size: 1.1em)[Applied Taxes]
    v(0.5em)

    table(
      columns: (1fr, 1fr, 1fr, 1fr, 1fr, 1fr),
      inset: 8pt,
      align: (left, left, left, right, right, right),
      fill: white,
      stroke: (x, y) => (
        bottom: if y == 0 { 1pt + styling.line-color } else { 1pt + styling.line-color },
      ),
      table.header(
        [*Tax Name*],
        [*Code*],
        [*Type*],
        [*Rate*],
        [*Taxable Amount*],
        [*Tax Amount*],
        // [*Applied At*],
      ),
      ..applied-taxes.map((tax) => {
        let rate-display = if tax.tax_type == "percentage" {
          [#format-currency(tax.tax_rate, precision: precision)%]
        } else {
          [#currency#format-currency(tax.tax_rate, precision: precision)]
        }
        
        (
          tax.tax_name,
          tax.tax_code,
          tax.tax_type,
          rate-display,
          [#currency#format-currency(tax.taxable_amount, precision: precision)],
          [#currency#format-currency(tax.tax_amount, precision: precision)],
          // tax.applied_at,
        )
      }).flatten(),
    )

    v(1em)
  }

  // Payment information
  if invoice-status == "FINALIZED" {
    text(weight: "medium", size: 1.1em)[Payment Information]
    v(0.8em)

    [We kindly request that you complete the payment by the due date of #due-date. Your prompt attention to this matter is greatly appreciated.]

    if "payment-instructions" in biller {
      v(0.5em)
      biller.payment-instructions
    }
  }

  // Notes
  if notes != "" {
    v(1em)
    text(weight: "medium", size: 1.1em)[Notes]
    v(0.5em)
    notes
  }

  // Footer
  v(3em)
  align(bottom,   align(center, text(size: 8pt)[
    #biller.name ⋅ 
    #{if "website" in biller {[#link("https://" + biller.website)[#biller.website] ⋅ ]}}
    #{if "help-email" in biller {[#link(biller.help-email)[#biller.help-email]]}}
  ]))

  doc
}