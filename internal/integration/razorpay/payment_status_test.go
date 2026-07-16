package razorpay

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPaymentStatus_EmptyID(t *testing.T) {
	svc := &PaymentService{}
	status, err := svc.GetPaymentStatus(context.Background(), "")
	assert.Error(t, err)
	assert.Empty(t, status)
}
