package razorpay

import (
	"errors"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	rzperrors "github.com/razorpay/razorpay-go/errors"
	"github.com/stretchr/testify/assert"
)

func TestRequestError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want func(error) bool
	}{
		{
			name: "a described 4xx is a validation error",
			err:  &rzperrors.BadRequestError{Message: "The amount must be atleast INR 1.00"},
			want: ierr.IsValidation,
		},
		{
			name: "a rejected key is a validation error",
			err:  &rzperrors.BadRequestError{Message: "Authentication failed"},
			want: ierr.IsValidation,
		},
		{
			name: "a rate limit is retryable",
			err:  &rzperrors.BadRequestError{Message: "Too many requests"},
			want: ierr.IsTooManyRequests,
		},
		{
			name: "an undescribed 4xx is internal",
			err:  &rzperrors.BadRequestError{},
			want: ierr.IsInternal,
		},
		{
			name: "a server error is internal",
			err:  &rzperrors.ServerError{Message: "boom"},
			want: ierr.IsInternal,
		},
		{
			name: "a transport error is internal",
			err:  errors.New("dial tcp: i/o timeout"),
			want: ierr.IsInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestError(tt.err, "failed to create invoice in Razorpay", "Unable to create invoice in Razorpay")
			assert.True(t, tt.want(got), got.Error())
		})
	}
}
