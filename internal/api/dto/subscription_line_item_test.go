package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCommitmentBucketRequest_Validate(t *testing.T) {
	validPrice := CreatePriceRequest{
		EntityType:   types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
	tests := []struct {
		name    string
		req     CommitmentBucketRequest
		wantErr bool
		errSub  string
	}{
		{
			name: "valid amount commitment",
			req: CommitmentBucketRequest{
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 10, Minute: 0},
				Price:           validPrice,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
			},
		},
		{
			name: "rejects non-subscription entity type on price",
			req: CommitmentBucketRequest{
				Start: types.Bucket{Hour: 9, Minute: 0},
				End:   types.Bucket{Hour: 10, Minute: 0},
				Price: CreatePriceRequest{
					EntityType:   types.PRICE_ENTITY_TYPE_PLAN,
					BillingModel: types.BILLING_MODEL_FLAT_FEE,
				},
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
			},
			wantErr: true, errSub: "SUBSCRIPTION",
		},
		{
			name: "rejects bad bucket point",
			req: CommitmentBucketRequest{
				Start:           types.Bucket{Hour: 25, Minute: 0},
				End:             types.Bucket{Hour: 10, Minute: 0},
				Price:           validPrice,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
			},
			wantErr: true, errSub: "hour",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSub)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
