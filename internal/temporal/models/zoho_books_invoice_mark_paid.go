package models

import (
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ZohoBooksInvoiceMarkPaidWorkflowInput contains the input for the Zoho Books mark-paid workflow
type ZohoBooksInvoiceMarkPaidWorkflowInput struct {
	invoiceID     string
	tenantID      string
	environmentID string
}

// NewZohoBooksInvoiceMarkPaidWorkflowInput builds the workflow input for marking a Zoho Books invoice paid.
func NewZohoBooksInvoiceMarkPaidWorkflowInput(invoiceID, tenantID, environmentID string) ZohoBooksInvoiceMarkPaidWorkflowInput {
	return ZohoBooksInvoiceMarkPaidWorkflowInput{
		invoiceID:     invoiceID,
		tenantID:      tenantID,
		environmentID: environmentID,
	}
}

func (input *ZohoBooksInvoiceMarkPaidWorkflowInput) InvoiceID() string {
	if input == nil {
		return ""
	}
	return input.invoiceID
}

func (input *ZohoBooksInvoiceMarkPaidWorkflowInput) TenantID() string {
	if input == nil {
		return ""
	}
	return input.tenantID
}

func (input *ZohoBooksInvoiceMarkPaidWorkflowInput) EnvironmentID() string {
	if input == nil {
		return ""
	}
	return input.environmentID
}

func (input *ZohoBooksInvoiceMarkPaidWorkflowInput) SetTenantID(tenantID string) {
	input.tenantID = tenantID
}

func (input *ZohoBooksInvoiceMarkPaidWorkflowInput) SetEnvironmentID(environmentID string) {
	input.environmentID = environmentID
}

func (input *ZohoBooksInvoiceMarkPaidWorkflowInput) Validate() error {
	if input.InvoiceID() == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("InvoiceID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.TenantID() == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("TenantID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.EnvironmentID() == "" {
		return ierr.NewError("environment_id is required").
			WithHint("EnvironmentID must not be empty").
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (input ZohoBooksInvoiceMarkPaidWorkflowInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(zohoBooksInvoiceMarkPaidWorkflowInputWire{
		InvoiceID:     input.invoiceID,
		TenantID:      input.tenantID,
		EnvironmentID: input.environmentID,
	})
}

func (input *ZohoBooksInvoiceMarkPaidWorkflowInput) UnmarshalJSON(data []byte) error {
	var wire zohoBooksInvoiceMarkPaidWorkflowInputWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	input.invoiceID = wire.InvoiceID
	input.tenantID = wire.TenantID
	input.environmentID = wire.EnvironmentID
	return nil
}

type zohoBooksInvoiceMarkPaidWorkflowInputWire struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}
