package dto

import (
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// SendInvoiceEmailRequest overrides who receives an invoice email; empty To sends to the customer's email.
type SendInvoiceEmailRequest struct {
	To []string `json:"to,omitempty" validate:"omitempty,max=50,dive,email"`
	CC []string `json:"cc,omitempty" validate:"omitempty,max=50,dive,email"`
}

func (r *SendInvoiceEmailRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// EmailLogResponse is one email sent to a customer and where its delivery stands.
type EmailLogResponse struct {
	*emaillog.EmailLog
}

type ListEmailLogsResponse = types.ListResponse[*EmailLogResponse]

func NewEmailLogResponse(e *emaillog.EmailLog) *EmailLogResponse {
	if e == nil {
		return nil
	}
	return &EmailLogResponse{EmailLog: e}
}
