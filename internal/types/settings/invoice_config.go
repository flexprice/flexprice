package settings

import "errors"

type InvoiceConfig struct {
	InvoiceNumberPrefix        string `json:"invoice_number_prefix"`
	InvoiceNumberFormat        string `json:"invoice_number_format"`
	InvoiceNumberStartSequence int    `json:"invoice_number_start_sequence"`
	InvoiceNumberTimezone      string `json:"invoice_number_timezone"`
	InvoiceNumberSeparator     string `json:"invoice_number_separator"`
	InvoiceNumberSuffixLength  int    `json:"invoice_number_suffix_length"`
	InvoiceNumberDueDateDays   int    `json:"invoice_number_due_date_days"`
}

func (i *InvoiceConfig) Validate() error {
	if i.InvoiceNumberPrefix == "" {
		return errors.New("invoice_number_prefix is required")
	}
	if i.InvoiceNumberFormat == "" {
		return errors.New("invoice_number_format is required")
	}
	return nil
}

func (i *InvoiceConfig) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"prefix":                  i.InvoiceNumberPrefix,
		"format":                  i.InvoiceNumberFormat,
		"start_sequence":          i.InvoiceNumberStartSequence,
		"invoice_number_timezone": i.InvoiceNumberTimezone,
		"separator":               i.InvoiceNumberSeparator,
		"suffix_length":           i.InvoiceNumberSuffixLength,
		"due_date_days":           i.InvoiceNumberDueDateDays,
	}
}

func InvoiceConfigFromMap(m map[string]interface{}) *InvoiceConfig {
	return &InvoiceConfig{
		InvoiceNumberPrefix:        m["prefix"].(string),
		InvoiceNumberFormat:        m["format"].(string),
		InvoiceNumberStartSequence: m["start_sequence"].(int),
		InvoiceNumberTimezone:      m["timezone"].(string),
		InvoiceNumberSeparator:     m["separator"].(string),
		InvoiceNumberSuffixLength:  m["suffix_length"].(int),
		InvoiceNumberDueDateDays:   m["due_date_days"].(int),
	}
}
