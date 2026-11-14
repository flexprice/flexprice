package contract

import "encoding/json"

func (s *ContractExtractionService) responseSchema() json.RawMessage {
	b, err := json.Marshal(s.buildSchema())
	if err != nil {
		panic(err)
	}
	return b
}

func (s *ContractExtractionService) buildSchema() map[string]any {
	return s.object(map[string]any{
		"documents": s.array(s.object(map[string]any{
			"type":       s.enumString("Kind of document", documentTypes...),
			"title":      s.str("Title as printed"),
			"page_start": s.integer("First page of this document, 1-based"),
			"page_end":   s.integer("Last page of this document, 1-based"),
		})),
		"contract_type": s.enumString("What this contract does", contractTypes...),
		"customer": s.object(map[string]any{
			"legal_name":      s.str("Legal name of the party being billed, never the provider"),
			"billing_address": s.str("Billing or registered address of the customer"),
			"primary_contact": s.str("Customer's primary or signing contact"),
			"billing_email":   s.str("Email address invoices go to"),
		}),
		"term": s.object(map[string]any{
			"signed_date":          s.str("Date signed, YYYY-MM-DD"),
			"effective_date":       s.str("Date the term starts, YYYY-MM-DD"),
			"term_months":          s.str("Initial term in whole months"),
			"auto_renew":           s.enumString("Whether the contract renews automatically", "true", "false"),
			"renewal_term_months":  s.str("Length of each renewal in whole months"),
			"notice_days":          s.str("Days of notice needed to stop renewal"),
			"renewal_price_change": s.str("How prices may change at renewal"),
			"cancellation":         s.str("When and how either party may terminate"),
		}),
		"billing": s.object(map[string]any{
			"structure":     s.str("Overall billing structure, e.g. platform fee plus usage"),
			"currency":      s.str("ISO 4217 currency code"),
			"invoice_day":   s.str("When in each period invoices are issued"),
			"payment_terms": s.str("Payment terms, e.g. Net 30"),
			"late_fee":      s.str("Interest or fee on late payment"),
			"tax":           s.str("Who pays which taxes"),
		}),
		"charges": s.array(s.object(map[string]any{
			"kind":              s.enumString("Kind of charge", chargeKinds...),
			"name":              s.str("Name of the charge as written"),
			"amount":            s.str("Amount as a plain decimal, 0 if waived"),
			"amount_period":     s.enumString("Period the amount is quoted for", billingPeriods...),
			"billing_period":    s.enumString("How often it is invoiced", billingPeriods...),
			"billing_timing":    s.enumString("Invoiced in advance or in arrears", billingTimings...),
			"included_quantity": s.str("Usage included before overage, as a number"),
			"unit":              s.str("Unit the usage is measured in"),
			"tier_mode":         s.enumString("How tiers price usage", tierModes...),
			"tiers": s.array(s.object(map[string]any{
				"up_to":       s.str("Upper bound of the tier; empty for the last tier"),
				"unit_amount": s.str("Price per unit in this tier"),
				"flat_amount": s.str("Flat fee for this tier, if any"),
			})),
			"trigger":             s.str("Event that bills a one-time charge, e.g. go-live"),
			"revenue_recognition": s.str("Short hint: evenly over the term, as used, or at a milestone"),
		})),
		"commitments": s.array(s.object(map[string]any{
			"amount":    s.str("Minimum commitment as a plain decimal"),
			"period":    s.enumString("Period the commitment applies to", billingPeriods...),
			"covers":    s.str("What counts toward the commitment"),
			"shortfall": s.str("How a shortfall is billed"),
		})),
		"credits": s.array(s.object(map[string]any{
			"amount": s.str("Credit amount as a plain decimal"),
			"kind":   s.str("onboarding, promotional or SLA"),
			"expiry": s.str("Expiry date, YYYY-MM-DD"),
		})),
		"service_levels": s.array(s.object(map[string]any{
			"description": s.str("SLA and the credit it triggers"),
		})),
		"doubts": s.array(s.str("Something a reviewer should know")),
	})
}

func (s *ContractExtractionService) object(props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props}
}

func (s *ContractExtractionService) array(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func (s *ContractExtractionService) str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func (s *ContractExtractionService) integer(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func (s *ContractExtractionService) enumString(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}
