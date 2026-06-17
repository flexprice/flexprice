package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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
				Price:           &validPrice,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
			},
		},
		{
			name: "rejects non-subscription entity type on price",
			req: CommitmentBucketRequest{
				Start: types.Bucket{Hour: 9, Minute: 0},
				End:   types.Bucket{Hour: 10, Minute: 0},
				Price: &CreatePriceRequest{
					EntityType:   types.PRICE_ENTITY_TYPE_PLAN,
					BillingModel: types.BILLING_MODEL_FLAT_FEE,
				},
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
			},
			wantErr: true, errSub: "SUBSCRIPTION",
		},
		{
			name: "rejects bad bucket point",
			req: CommitmentBucketRequest{
				Start:           types.Bucket{Hour: 25, Minute: 0},
				End:             types.Bucket{Hour: 10, Minute: 0},
				Price:           &validPrice,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
			},
			wantErr: true, errSub: "hour",
		},
		{
			name: "rejects missing overage factor",
			req: CommitmentBucketRequest{
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 10, Minute: 0},
				Price:           &validPrice,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
			},
			wantErr: true, errSub: "overage_factor is required",
		},
		{
			// Exactly 1.0 is valid for buckets: overage bills at base rate.
			name: "accepts overage factor of exactly 1.0",
			req: CommitmentBucketRequest{
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 10, Minute: 0},
				Price:           &validPrice,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
				OverageFactor:   lo.ToPtr(decimal.NewFromInt(1)),
			},
		},
		{
			name: "rejects overage factor below 1.0",
			req: CommitmentBucketRequest{
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 10, Minute: 0},
				Price:           &validPrice,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(0.5)),
			},
			wantErr: true, errSub: "overage_factor must be at least 1.0",
		},
		{
			// Reuse contract: id refers to an existing bucket whose price is kept.
			name: "accepts id without price",
			req: CommitmentBucketRequest{
				ID:              "bucket_existing",
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 10, Minute: 0},
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
			},
		},
		{
			name: "rejects missing both id and price",
			req: CommitmentBucketRequest{
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 10, Minute: 0},
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
			},
			wantErr: true, errSub: "bucket price is required",
		},
		{
			name: "rejects both id and price",
			req: CommitmentBucketRequest{
				ID:              "bucket_existing",
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 10, Minute: 0},
				Price:           &validPrice,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
			},
			wantErr: true, errSub: "cannot provide both id and price",
		},
		{
			name: "rejects missing commitment (filter-only bucket)",
			req: CommitmentBucketRequest{
				Start:         types.Bucket{Hour: 9, Minute: 0},
				End:           types.Bucket{Hour: 10, Minute: 0},
				Price:         &validPrice,
				OverageFactor: lo.ToPtr(decimal.NewFromFloat(1.5)),
			},
			wantErr: true, errSub: "commitment_type is required",
		},
		{
			name: "rejects zero commitment value",
			req: CommitmentBucketRequest{
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 10, Minute: 0},
				Price:           &validPrice,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.Zero,
				OverageFactor:   lo.ToPtr(decimal.NewFromFloat(1.5)),
			},
			wantErr: true, errSub: "commitment_value must be > 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate(0)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSub)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
