package contract

import (
	"encoding/json"
	"sort"
)

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
			"type":       s.enumString(documentTypes...),
			"title":      s.str(),
			"page_start": s.integer(),
			"page_end":   s.integer(),
		})),
		"contract_type": s.term(contractTypes...),
		"customer": s.object(map[string]any{
			"legal_name":      s.term(),
			"billing_address": s.term(),
			"primary_contact": s.term(),
			"billing_email":   s.term(),
		}),
		"term": s.object(map[string]any{
			"signed_date":          s.term(),
			"effective_date":       s.term(),
			"term_months":          s.term(),
			"auto_renew":           s.term("true", "false"),
			"renewal_term_months":  s.term(),
			"notice_days":          s.term(),
			"renewal_price_change": s.term(),
			"cancellation":         s.term(),
		}),
		"billing": s.object(map[string]any{
			"structure":     s.term(),
			"currency":      s.term(),
			"invoice_day":   s.term(),
			"payment_terms": s.term(),
			"late_fee":      s.term(),
			"tax":           s.term(),
		}),
		"charges": s.array(s.object(map[string]any{
			"kind":              s.enumString(chargeKinds...),
			"name":              s.term(),
			"amount":            s.term(),
			"amount_period":     s.term(billingPeriods...),
			"billing_period":    s.term(billingPeriods...),
			"billing_timing":    s.term(billingTimings...),
			"included_quantity": s.term(),
			"unit":              s.term(),
			"tier_mode":         s.term(tierModes...),
			"tiers": s.array(s.object(map[string]any{
				"up_to":       s.str(),
				"unit_amount": s.str(),
				"flat_amount": s.str(),
			})),
			"trigger":             s.term(),
			"revenue_recognition": s.term(),
		})),
		"commitments": s.array(s.object(map[string]any{
			"amount":    s.term(),
			"period":    s.term(billingPeriods...),
			"covers":    s.term(),
			"shortfall": s.term(),
		})),
		"credits": s.array(s.object(map[string]any{
			"amount": s.term(),
			"kind":   s.term(),
			"expiry": s.term(),
		})),
		"service_levels": s.array(s.object(map[string]any{
			"description": s.term(),
		})),
		"doubts": s.array(s.str()),
	})
}

func (s *ContractExtractionService) term(allowed ...string) map[string]any {
	value := s.str()
	if len(allowed) > 0 {
		value = s.enumString(append([]string{""}, allowed...)...)
	}
	return s.object(map[string]any{
		"value":      value,
		"confidence": s.integer(),
		"page":       s.integer(),
		"quote":      s.str(),
		"options":    s.array(s.str()),
		"note":       s.str(),
	})
}

func (s *ContractExtractionService) object(props map[string]any) map[string]any {
	required := make([]string, 0, len(props))
	for k := range props {
		required = append(required, k)
	}
	sort.Strings(required)
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func (s *ContractExtractionService) array(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func (s *ContractExtractionService) str() map[string]any { return map[string]any{"type": "string"} }

func (s *ContractExtractionService) integer() map[string]any {
	return map[string]any{"type": "integer"}
}

func (s *ContractExtractionService) enumString(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}
