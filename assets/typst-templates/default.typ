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


#let format-number = (num) => {
  let str-num = str(num)
  let parts = str-num.split(".")
  let integer-part = str(parts.at(0))
  let decimal-part = if parts.len() > 1 { "." + parts.at(1) } else { "" }

  // Add commas every 3 digits from the right
  let chars = integer-part.rev().clusters()
  let result = ""
  for (i, c) in chars.enumerate() {
    if calc.rem-euclid(i, 3) == 0 and i != 0 {
      result += ","
    }
    result += c
  }

  result.rev() + decimal-part
}

// Define the default-invoice function
#let default-invoice(
  language: "en",
  currency: "$",
  title: none,
  banner-image: none,
  invoice-status: "DRAFT",       // DRAFT, FINALIZED, VOIDED
  invoice-number: none,
  issuing-date: none,
  due-date: none,
  amount-due: 0,  
  notes: "",
  biller: (:),                  // Company info
  recipient: (:),               // Customer info
  keywords: (),
  styling: (:),                 // font, font-size, margin (sets defaults below)
  items: (),                    // Line items
  vat: 0,                       // VAT percentage as decimal
  doc,
) = {
  // Set styling defaults
  styling.font = styling.at("font", default: "Inter")
  styling.font-size = styling.at("font-size", default: 10pt)
  styling.primary-color = styling.at("primary-color", default: rgb("#4361ee"))
  styling.margin = styling.at("margin", default: (
    top: 15mm,
    right: 15mm,
    bottom: 15mm,
    left: 15mm,
  ))
  styling.line-color = styling.at("line-color", default: rgb("#eee"))
  styling.secondary-color = rgb(styling.at("secondary-color", default: rgb("#666666")))

  // Set document properties
  let issuing-date-value = if issuing-date != none { issuing-date }
        else { datetime.today().display("[year]-[month]-[day]") }

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
        #text(weight: "medium", size: 2em)[Invoice]
      ]
    )
    v(1em)
  } else {
    text(weight: "bold", size: 2em)[Invoice]
  }

  grid(
    columns: (1fr, 1fr, 1fr),
    gutter: 0.5em,
    align: auto,
    [
      #text(weight: "regular", fill: styling.secondary-color)[Invoice Number]\
      #text(weight: "regular")[#invoice-number]
    ],
    [
      #text(weight: "regular", fill: styling.secondary-color)[Date of Issue]\
      #text(weight: "regular")[#issuing-date-value]
    ],
    [
      #text(weight: "regular", fill: styling.secondary-color)[Date Due]\
      #text(weight: "regular")[#due-date]
    ],
  )

  line(length: 100%, stroke: styling.line-color)

  v(2em)

  // Biller and Recipient Information
  grid(
    columns: (1fr, 1fr),
    gutter: 1em,
    [
      #text(weight: "semibold", size: 12pt)[From]
      #v(0.25em)
      #text(weight: "medium")[#biller.name] \
      #text(fill: gray)[#biller.at("email", default: "--")] \
      #text(fill: styling.secondary-color)[#biller.at("address", default: (:)).at("street", default: "--")] \
      #text(fill: styling.secondary-color)[#biller.at("address", default: (:)).at("city", default: "--")] \
      #text(fill: styling.secondary-color)[#biller.at("address", default: (:)).at("postal-code", default: "--")]

    ],
    [
      #text(weight: "semibold", size: 12pt)[Bill to]
      #v(0.25em)
      #text(weight: "medium")[#recipient.name] \
      #text(fill: gray)[#recipient.at("email", default: "--")] \
      #text(fill: styling.secondary-color)[#recipient.at("address", default: (:)).at("street", default: "--")] \
      #text(fill: styling.secondary-color)[#recipient.at("address", default: (:)).at("city", default: "--")] \
      #text(fill: styling.secondary-color)[#recipient.at("address", default: (:)).at("postal-code", default: "--")]
    ]
  )

  v(2em)
  line(length: 100%, stroke: styling.line-color)
  v(1em)

  // Order Details
  [== Order Details]
  v(1em)

  table(
    columns: (1fr, 2fr, 1fr, 1fr, 1fr),
    inset: 8pt,
    align: (left, left, left, center, right),
    fill: white,
    stroke: (x, y) => (
      bottom: if y == 0 { 1pt + styling.line-color } else { 1pt + styling.line-color },
    ),
    table.header(
      [*Subscription*],
      [*Description*],
      [*Interval*],
      [*Quantity*],
      [*Amount*],
    ),
    ..items.map((item) => {
      (
        item.at("plan_display_name", default: "Plan"),
        item.at("description", default: "Recurring"),
        if item.at("period_start", default: none) != none and item.at("period_end", default: none) != none {
          [#format-date(parse-date(item.at("period_start"))) - #format-date(parse-date(item.at("period_end")))]
        } else {
          "-"
        },
        format-number(item.quantity),
        [#currency #format-number(item.quantity * item.amount)],
      )
    }).flatten(),
  )

  v(1em)

  // Totals
  align(right,
    table(
      columns: 2,
      align: (left, right),
      inset: 6pt,
      stroke: none,
      [Subtotal], [#currency#format-number(amount-due)],
      [Tax], if vat == 0 { [-] } else { [#currency#format-number(calc.round(amount-due * vat, digits: 2))] },
      table.hline(stroke: 1pt + styling.line-color),
      [*Total Amount*], [*#currency#format-number(amount-due + calc.round(amount-due * vat, digits: 2))*],
    )
  )

  v(2em)

  // Payment information
  if invoice-status == "FINALIZED" {
    [== Payment Information]
    v(1em)

    [We kindly request that you complete the payment by the due date of #due-date. Your prompt attention to this matter is greatly appreciated.]

    if "payment-instructions" in biller {
      v(0.5em)
      biller.payment-instructions
    }
  }

  // Notes
  if notes != "" {
    v(1em)
    [== Notes]
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