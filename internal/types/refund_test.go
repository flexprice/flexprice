package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRefundStatus_Validate(t *testing.T) {
	tests := []struct {
		name    string
		status  RefundStatus
		wantErr bool
	}{
		{name: "pending is valid", status: RefundStatusPending, wantErr: false},
		{name: "processing is valid", status: RefundStatusProcessing, wantErr: false},
		{name: "succeeded is valid", status: RefundStatusSucceeded, wantErr: false},
		{name: "failed is valid", status: RefundStatusFailed, wantErr: false},
		{name: "cancelled is valid", status: RefundStatusCancelled, wantErr: false},
		{name: "empty is invalid", status: RefundStatus(""), wantErr: true},
		{name: "unknown is invalid", status: RefundStatus("BOGUS"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.status.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRefundStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status RefundStatus
		want   bool
	}{
		{name: "pending is not terminal", status: RefundStatusPending, want: false},
		{name: "processing is not terminal", status: RefundStatusProcessing, want: false},
		{name: "succeeded is terminal", status: RefundStatusSucceeded, want: true},
		{name: "failed is terminal", status: RefundStatusFailed, want: true},
		{name: "cancelled is terminal", status: RefundStatusCancelled, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.IsTerminal())
		})
	}
}

func TestRefundReason_Validate(t *testing.T) {
	tests := []struct {
		name    string
		reason  RefundReason
		wantErr bool
	}{
		{name: "duplicate is valid", reason: RefundReasonDuplicate, wantErr: false},
		{name: "fraudulent is valid", reason: RefundReasonFraudulent, wantErr: false},
		{name: "requested by customer is valid", reason: RefundReasonRequestedByCustomer, wantErr: false},
		{name: "order change is valid", reason: RefundReasonOrderChange, wantErr: false},
		{name: "service issue is valid", reason: RefundReasonServiceIssue, wantErr: false},
		{name: "other is valid", reason: RefundReasonOther, wantErr: false},
		{name: "empty is invalid", reason: RefundReason(""), wantErr: true},
		{name: "unknown is invalid", reason: RefundReason("BOGUS"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.reason.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRefundFilter_Defaults(t *testing.T) {
	var f *RefundFilter

	assert.NotPanics(t, func() {
		_ = f.GetLimit()
		_ = f.GetOffset()
		_ = f.GetSort()
		_ = f.GetOrder()
		_ = f.GetStatus()
		_ = f.GetExpand()
		_ = f.IsUnlimited()
	})

	empty := &RefundFilter{}
	assert.Equal(t, NewDefaultQueryFilter().GetLimit(), empty.GetLimit())
	assert.NoError(t, empty.Validate())
}
