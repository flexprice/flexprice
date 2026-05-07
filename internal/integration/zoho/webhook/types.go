package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ZohoBooksInvoiceWebhookStatus* are Zoho Books invoice status strings we match on inbound webhooks.
const (
	ZohoBooksInvoiceWebhookStatusPaid   = "paid"
	ZohoBooksInvoiceWebhookStatusVoid   = "void"
	ZohoBooksInvoiceWebhookStatusVoided = "voided"
)

// zohoJSONScalarString unmarshals JSON string, number, or null (Zoho often sends amounts as numbers).
type zohoJSONScalarString string

func (z *zohoJSONScalarString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*z = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*z = zohoJSONScalarString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		*z = zohoJSONScalarString(string(n))
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*z = zohoJSONScalarString(strconv.FormatFloat(f, 'f', -1, 64))
		return nil
	}
	return fmt.Errorf("zohoJSONScalarString: unsupported JSON value %s", string(b))
}

func (z zohoJSONScalarString) String() string {
	return strings.TrimSpace(string(z))
}

// Payload is the minimal JSON shape Zoho Books sends for Invoice / Contacts webhooks.
type Payload struct {
	OrganizationID zohoJSONScalarString `json:"organization_id,omitempty"`
	Invoice        *InvoicePayload      `json:"invoice,omitempty"`
	Contact        *ContactPayload      `json:"contact,omitempty"`
}

// InvoicePayload captures fields needed for paid reconciliation.
type InvoicePayload struct {
	InvoiceID   zohoJSONScalarString `json:"invoice_id"`
	Status      string               `json:"status,omitempty"`
	Total       zohoJSONScalarString `json:"total,omitempty"`
	Balance     zohoJSONScalarString `json:"balance,omitempty"`
	PaymentMade zohoJSONScalarString `json:"payment_made,omitempty"`
	Currency    string               `json:"currency_code,omitempty"`
}

// ContactPayload captures fields for inbound customer creation.
type ContactPayload struct {
	ContactID   string `json:"contact_id"`
	ContactName string `json:"contact_name,omitempty"`
	ContactType string `json:"contact_type,omitempty"`
	Email       string `json:"email,omitempty"`
	// ContactPersons mirrors Zoho nested emails when top-level email is empty.
	ContactPersons []struct {
		Email string `json:"email,omitempty"`
	} `json:"contact_persons,omitempty"`
}
