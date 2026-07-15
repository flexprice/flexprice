package dto

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
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

func TestCreateSubscriptionLineItemRequest_Validate_Quantity(t *testing.T) {
	fixedPriceNoMin := &price.Price{
		Type:        types.PRICE_TYPE_FIXED,
		MinQuantity: nil,
	}
	fixedPriceMin5 := &price.Price{
		Type:        types.PRICE_TYPE_FIXED,
		MinQuantity: lo.ToPtr(decimal.NewFromInt(5)),
	}

	tests := []struct {
		name      string
		quantity  *decimal.Decimal
		linePrice *price.Price
		wantErr   bool
		errSub    string
	}{
		{
			name:      "nil quantity, no min quantity set: no error",
			quantity:  nil,
			linePrice: fixedPriceNoMin,
		},
		{
			name:      "nil quantity, min quantity set: no error (nil is never checked against floor)",
			quantity:  nil,
			linePrice: fixedPriceMin5,
		},
		{
			name:      "explicit zero quantity, no min quantity: no error",
			quantity:  lo.ToPtr(decimal.Zero),
			linePrice: fixedPriceNoMin,
		},
		{
			name:      "explicit zero quantity, min quantity set: error below floor",
			quantity:  lo.ToPtr(decimal.Zero),
			linePrice: fixedPriceMin5,
			wantErr:   true,
			errSub:    "quantity must be greater than or equal to min_quantity",
		},
		{
			name:      "explicit quantity below min quantity: error",
			quantity:  lo.ToPtr(decimal.NewFromInt(2)),
			linePrice: fixedPriceMin5,
			wantErr:   true,
			errSub:    "quantity must be greater than or equal to min_quantity",
		},
		{
			name:      "explicit quantity at min quantity: no error",
			quantity:  lo.ToPtr(decimal.NewFromInt(5)),
			linePrice: fixedPriceMin5,
		},
		{
			name:      "explicit quantity above min quantity: no error",
			quantity:  lo.ToPtr(decimal.NewFromInt(10)),
			linePrice: fixedPriceMin5,
		},
		{
			name:      "negative quantity: error regardless of min quantity",
			quantity:  lo.ToPtr(decimal.NewFromInt(-1)),
			linePrice: fixedPriceMin5,
			wantErr:   true,
			errSub:    "quantity must be non-negative",
		},
		{
			name:      "nil linePrice, negative quantity: error not dependent on linePrice",
			quantity:  lo.ToPtr(decimal.NewFromInt(-1)),
			linePrice: nil,
			wantErr:   true,
			errSub:    "quantity must be non-negative",
		},
		{
			name:      "nil linePrice, explicit quantity (including 0): no floor check applies",
			quantity:  lo.ToPtr(decimal.Zero),
			linePrice: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &CreateSubscriptionLineItemRequest{
				PriceID:  "price_test",
				Quantity: tt.quantity,
			}
			err := req.Validate(tt.linePrice, nil)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSub)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCreateSubscriptionLineItemRequest_ToSubscriptionLineItem_QuantityDefaulting(t *testing.T) {
	ctx := context.Background()

	sub := &SubscriptionResponse{
		Subscription: &subscription.Subscription{
			ID:         "sub_test",
			CustomerID: "cust_test",
			Currency:   "usd",
			StartDate:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	basePrice := func(minQuantity *decimal.Decimal) *PriceResponse {
		return &PriceResponse{
			Price: &price.Price{
				Type:           types.PRICE_TYPE_FIXED,
				BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
				InvoiceCadence: types.InvoiceCadenceAdvance,
				MinQuantity:    minQuantity,
			},
		}
	}

	tests := []struct {
		name        string
		quantity    *decimal.Decimal
		minQuantity *decimal.Decimal
		wantQty     decimal.Decimal
	}{
		{
			name:        "omitted quantity, min quantity set: defaults to min quantity",
			quantity:    nil,
			minQuantity: lo.ToPtr(decimal.NewFromInt(5)),
			wantQty:     decimal.NewFromInt(5),
		},
		{
			name:        "omitted quantity, no min quantity: defaults to price default quantity",
			quantity:    nil,
			minQuantity: nil,
			wantQty:     decimal.NewFromInt(1), // fixed price default quantity
		},
		{
			name:        "explicit zero quantity, min quantity set: honored as-is, not substituted",
			quantity:    lo.ToPtr(decimal.Zero),
			minQuantity: lo.ToPtr(decimal.NewFromInt(5)),
			wantQty:     decimal.Zero,
		},
		{
			name:        "explicit quantity, min quantity set: passes through as-is",
			quantity:    lo.ToPtr(decimal.NewFromInt(3)),
			minQuantity: lo.ToPtr(decimal.NewFromInt(5)),
			wantQty:     decimal.NewFromInt(3),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &CreateSubscriptionLineItemRequest{
				PriceID:  "price_test",
				Quantity: tt.quantity,
			}
			params := LineItemParams{
				Subscription: sub,
				Price:        basePrice(tt.minQuantity),
				EntityType:   types.SubscriptionLineItemEntityTypeSubscription,
			}

			lineItem := req.ToSubscriptionLineItem(ctx, params)

			assert.True(t, tt.wantQty.Equal(lineItem.Quantity), "expected quantity %s, got %s", tt.wantQty.String(), lineItem.Quantity.String())
		})
	}
}
