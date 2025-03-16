#import "./invoice.typ"

#show: invoice.default-invoice.with(
  invoice-status: "{{ .InvoiceStatus }}",
  notes: "{{ .Notes }}",
  {{ if .BannerImage }}banner-image: image("{{ .BannerImage }}", width: 30%),{{ end }}
  invoice-id: "{{ .InvoiceID }}",
  invoice-number: "{{ .InvoiceNumber }}",
  customer-id: "{{ .CustomerID }}",
  {{ if .SubscriptionID }}subscription-id: "{{ .SubscriptionID }}",{{ end }}
  issuing-date: "{{ .IssuingDate }}",
  due-date: "{{ .DueDate }}",
  {{ if .PeriodStart }}period-start: "{{ .PeriodStart }}",{{ end }}
  {{ if .PeriodEnd }}period-end: "{{ .PeriodEnd }}",{{ end }}
  amount-due: {{ .AmountDue }},
  amount-paid: {{ .AmountPaid }},
  amount-remaining: {{ .AmountRemaining }},
  vat: {{ .VAT }},
  biller: (
    website: "{{ .Biller.website }}",
    name: "{{ .Biller.name }}",
    {{ if index .Biller "email" }}email: "{{ index .Biller "email" }}",{{ end }}
    {{ if index .Biller "help-email" }}help-email: "{{ index .Biller "help-email" }}",{{ end }}
    address: (
      street: "{{ index .Biller.address "street" }}",
      city: "{{ index .Biller.address "city" }}",
      postal-code: "{{ index .Biller.address "postal-code" }}",
      {{ if index .Biller.address "state" }}state: "{{ index .Biller.address "state" }}",{{ end }}
      {{ if index .Biller.address "country" }}country: "{{ index .Biller.address "country" }}",{{ end }}
    ),
    {{ if index .Biller "payment-instructions" }}payment-instructions: [{{ index .Biller "payment-instructions" }}],{{ end }}
  ),
  recipient: (
    name: "{{ .Recipient.name }}",
    {{ if index .Recipient "email" }}email: "{{ index .Recipient "email" }}",{{ end }}
    address: (
      street: "{{ index .Recipient.address "street" }}",
      city: "{{ index .Recipient.address "city" }}",
      postal-code: "{{ index .Recipient.address "postal-code" }}",
      {{ if index .Recipient.address "state" }}state: "{{ index .Recipient.address "state" }}",{{ end }}
      {{ if index .Recipient.address "country" }}country: "{{ index .Recipient.address "country" }}",{{ end }}
    )
  ),
  items: (
    {{ range $index, $item := .Items }}
    (
      plan-display-name: "{{ $item.plan-display-name }}",
      display-name: "{{ $item.display-name }}",
      {{ if index $item "period-start" }}period-start: "{{ index $item "period-start" }}",{{ end }}
      {{ if index $item "period-end" }}period-end: "{{ index $item "period-end" }}",{{ end }}
      quantity: {{ $item.quantity }},
      amount: {{ $item.amount }},
    ),
    {{ end }}
  ),
  styling: (
    font: "Inter",
    secondary-color: "#919191",
  )
)
