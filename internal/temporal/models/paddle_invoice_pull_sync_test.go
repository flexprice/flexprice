package models

import (
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaddleInvoicePullSyncWorkflowInput_Validate(t *testing.T) {
	tests := []struct {
		name    string
		input   PaddleInvoicePullSyncWorkflowInput
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid input",
			input: PaddleInvoicePullSyncWorkflowInput{
				InvoiceID:     "inv_001",
				TenantID:      "tenant_001",
				EnvironmentID: "env_001",
			},
			wantErr: false,
		},
		{
			name: "missing invoice_id",
			input: PaddleInvoicePullSyncWorkflowInput{
				InvoiceID:     "",
				TenantID:      "tenant_001",
				EnvironmentID: "env_001",
			},
			wantErr: true,
			errMsg:  "invoice_id is required",
		},
		{
			name: "missing tenant_id",
			input: PaddleInvoicePullSyncWorkflowInput{
				InvoiceID:     "inv_001",
				TenantID:      "",
				EnvironmentID: "env_001",
			},
			wantErr: true,
			errMsg:  "tenant_id is required",
		},
		{
			name: "missing environment_id",
			input: PaddleInvoicePullSyncWorkflowInput{
				InvoiceID:     "inv_001",
				TenantID:      "tenant_001",
				EnvironmentID: "",
			},
			wantErr: true,
			errMsg:  "environment_id is required",
		},
		{
			name: "all fields empty",
			input: PaddleInvoicePullSyncWorkflowInput{
				InvoiceID:     "",
				TenantID:      "",
				EnvironmentID: "",
			},
			wantErr: true,
			errMsg:  "invoice_id is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				assert.True(t, ierr.IsValidation(err), "expected a validation error")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
