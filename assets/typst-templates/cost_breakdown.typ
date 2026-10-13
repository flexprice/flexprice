// Cost Breakdown template — a simplified pre-invoice document (no tax table, no addresses).
// Reuses date/number/currency helpers from default.typ.
#import "default.typ": parse-date, format-date, format-currency, format-number

#let invoice-data = json(sys.inputs.path)

#let json-array(data, key) = {
  let v = data.at(key, default: none)
  if v == none { () } else { v }
}

#let currency = invoice-data.at("currency", default: "$")
#let precision = invoice-data.at("precision", default: 2)
#let label-color = rgb("#919191")

// Renders "DD Mon YY" from a "YYYY-MM-DD" string, blank if empty.
#let short-date(s) = {
  if s == none or s == "" { "" } else { format-date(parse-date(s)) }
}

// force: true renders the row even when the value is empty (label with a blank value).
#let meta-row(label, value, force: false) = {
  if force or (value != none and value != "") {
    grid(
      columns: (auto, 1fr),
      column-gutter: 6pt,
      text(weight: "medium")[#label:],
      text(fill: label-color)[#value],
    )
  }
}

#set page(paper: "a4", margin: (x: 2.2cm, y: 2.2cm))
#set text(font: "Inter", size: 10pt)

#let biller = invoice-data.at("biller", default: (:))
#let recipient = invoice-data.at("recipient", default: (:))
#let period-start = short-date(invoice-data.at("period_start", default: ""))
#let period-end = short-date(invoice-data.at("period_end", default: ""))

// Title
#text(size: 22pt, weight: "bold")[Cost Breakdown]

#v(10pt)
#line(length: 100%, stroke: 0.5pt + label-color)
#v(10pt)

// Document metadata
#meta-row("Breakdown number", invoice-data.at("invoice_number", default: ""))
#meta-row("Date of issue", short-date(invoice-data.at("issuing_date", default: "")))
#if period-start != "" {
  meta-row("Service period", period-start + " - " + period-end)
}
#meta-row("PO Number", invoice-data.at("po_number", default: ""), force: true)

#v(10pt)
#line(length: 100%, stroke: 0.5pt + label-color)
#v(16pt)

// From / Bill to
#grid(
  columns: (1fr, 1fr),
  column-gutter: 20pt,
  [
    #text(weight: "bold")[From]
    #v(6pt)
    #text(weight: "bold", size: 12pt)[#biller.at("name", default: "")]
  ],
  [
    #text(weight: "bold")[Bill to]
    #v(6pt)
    #text(weight: "bold", size: 12pt)[#recipient.at("name", default: "")]
  ],
)

#v(24pt)

// Line items
#let items = json-array(invoice-data, "line_items")

#table(
  columns: (2.4fr, 1.6fr, 0.8fr, 1fr),
  align: (left, left, center, right),
  stroke: none,
  inset: (x: 4pt, y: 8pt),
  table.header(
    table.cell(fill: rgb("#f5f5f5"))[#text(fill: label-color, weight: "bold")[Item]],
    table.cell(fill: rgb("#f5f5f5"))[#text(fill: label-color, weight: "bold")[Interval]],
    table.cell(fill: rgb("#f5f5f5"))[#text(fill: label-color, weight: "bold")[Quantity]],
    table.cell(fill: rgb("#f5f5f5"))[#text(fill: label-color, weight: "bold")[Amount]],
  ),
  ..items.map(item => {
    let name = {
      let dn = item.at("display_name", default: "")
      if dn != "" { dn } else { item.at("plan_display_name", default: "") }
    }
    let start = short-date(item.at("period_start", default: ""))
    let end = short-date(item.at("period_end", default: ""))
    let interval = if start != "" { start + " - " + end } else { "" }
    let qty = item.at("quantity", default: 0)
    let qty-str = if calc.rem(qty, 1) == 0 { str(int(qty)) } else { format-number(qty) }
    (
      [#name],
      [#interval],
      [#qty-str],
      [#currency #format-currency(item.at("amount", default: 0), precision: precision)],
    )
  }).flatten(),
)

#v(6pt)
#line(length: 100%, stroke: 0.5pt + label-color)
#v(6pt)

// Subtotal
#grid(
  columns: (1fr, auto),
  align: (right, right),
  column-gutter: 24pt,
  text(weight: "bold")[Subtotal],
  text(weight: "bold")[#currency#format-currency(invoice-data.at("subtotal", default: 0), precision: precision)],
)

#v(24pt)
Tax details will be provided in the official invoice.

#let notes = invoice-data.at("notes", default: "")
#if notes != "" {
  v(28pt)
  text(weight: "bold", size: 12pt)[Notes]
  v(6pt)
  text(fill: label-color)[#notes]
}
