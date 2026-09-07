package dto

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			// overage_factor is optional on the wire and defaults to 1.0.
			name: "accepts missing overage factor",
			req: CommitmentBucketRequest{
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 10, Minute: 0},
				Price:           &validPrice,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
			},
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
		quantity  decimal.Decimal
		linePrice *price.Price
		wantErr   bool
		errSub    string
	}{
		{name: "zero qty is valid (will default downstream)", quantity: decimal.Zero, linePrice: fixedPriceNoMin, wantErr: false},
		{name: "zero qty with floor is valid (floor only a default)", quantity: decimal.Zero, linePrice: fixedPriceMin5, wantErr: false},
		{name: "positive qty below floor is valid (no floor validation)", quantity: decimal.NewFromInt(2), linePrice: fixedPriceMin5, wantErr: false},
		{name: "positive qty at floor is valid", quantity: decimal.NewFromInt(5), linePrice: fixedPriceMin5, wantErr: false},
		{name: "positive qty above floor is valid", quantity: decimal.NewFromInt(10), linePrice: fixedPriceMin5, wantErr: false},
		{name: "negative qty rejected", quantity: decimal.NewFromInt(-1), linePrice: fixedPriceNoMin, wantErr: true, errSub: "non-negative"},
		{name: "negative qty with floor rejected", quantity: decimal.NewFromInt(-1), linePrice: fixedPriceMin5, wantErr: true, errSub: "non-negative"},
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
		quantity    decimal.Decimal
		minQuantity *decimal.Decimal
		wantQty     decimal.Decimal
	}{
		{
			name:        "zero qty + min_quantity → defaults to min_quantity",
			quantity:    decimal.Zero,
			minQuantity: lo.ToPtr(decimal.NewFromInt(5)),
			wantQty:     decimal.NewFromInt(5),
		},
		{
			name:        "zero qty + no min_quantity → defaults to 1",
			quantity:    decimal.Zero,
			minQuantity: nil,
			wantQty:     decimal.NewFromInt(1),
		},
		{
			name:        "explicit qty=3 with min_quantity=5 → stored as 3 (no floor)",
			quantity:    decimal.NewFromInt(3),
			minQuantity: lo.ToPtr(decimal.NewFromInt(5)),
			wantQty:     decimal.NewFromInt(3),
		},
		{
			name:        "explicit qty=7 with min_quantity=5 → stored as 7",
			quantity:    decimal.NewFromInt(7),
			minQuantity: lo.ToPtr(decimal.NewFromInt(5)),
			wantQty:     decimal.NewFromInt(7),
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

func TestValidateCommitmentFieldsCommon_OverageFactor(t *testing.T) {
	amount := decimal.NewFromInt(100)

	t.Run("accepts overage factor of exactly 1.0", func(t *testing.T) {
		err := validateCommitmentFieldsCommon(
			&amount, nil, types.COMMITMENT_TYPE_AMOUNT, lo.ToPtr(decimal.NewFromInt(1)), true,
		)
		assert.NoError(t, err)
	})

	t.Run("rejects overage factor below 1.0", func(t *testing.T) {
		err := validateCommitmentFieldsCommon(
			&amount, nil, types.COMMITMENT_TYPE_AMOUNT, lo.ToPtr(decimal.NewFromFloat(0.5)), true,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "commitment_overage_factor must be at least 1.0")
	})
}

func TestCreateSubscriptionLineItemRequest_Validate_CadenceMustDivideSub(t *testing.T) {
	sub := &subscription.Subscription{
		StartDate:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		BillingPeriod:      types.BILLING_PERIOD_QUARTER,
		BillingPeriodCount: 1,
	}

	cases := []struct {
		name       string
		subPer     types.BillingPeriod
		subCount   int
		pricePer   types.BillingPeriod
		priceCount int
		wantErr    bool
	}{
		{"monthly-on-quarterly-ok", types.BILLING_PERIOD_QUARTER, 1, types.BILLING_PERIOD_MONTHLY, 1, false},
		{"quarterly-on-quarterly-ok", types.BILLING_PERIOD_QUARTER, 1, types.BILLING_PERIOD_QUARTER, 1, false},
		{"onetime-always-ok", types.BILLING_PERIOD_QUARTER, 1, types.BILLING_PERIOD_ONETIME, 1, false},
		{"annual-on-quarterly-rejected", types.BILLING_PERIOD_QUARTER, 1, types.BILLING_PERIOD_ANNUAL, 1, true},
		{"halfyear-on-quarterly-rejected", types.BILLING_PERIOD_QUARTER, 1, types.BILLING_PERIOD_HALF_YEAR, 1, true},
		// Count-aware cases: 2-monthly (2mo) on quarterly (3mo) — periods look
		// compatible (both MONTHLY-family), but effective months 3 % 2 = 1, so
		// splitInvoicePeriodByLineItemCadence would fail at invoice time.
		// Validation must reject this so runtime never sees it.
		{"two-monthly-on-quarterly-rejected", types.BILLING_PERIOD_QUARTER, 1, types.BILLING_PERIOD_MONTHLY, 2, true},
		// 2-monthly on 6-month sub: 6 % 2 = 0 → allowed.
		{"two-monthly-on-halfyear-ok", types.BILLING_PERIOD_HALF_YEAR, 1, types.BILLING_PERIOD_MONTHLY, 2, false},
		// Sub count > 1: monthly on 2-monthly sub → 2 % 1 = 0 → allowed.
		{"monthly-on-two-monthly-sub-ok", types.BILLING_PERIOD_MONTHLY, 2, types.BILLING_PERIOD_MONTHLY, 1, false},
		// Zero counts default to 1: quarterly (period=QUARTER, count=0→1) with
		// monthly price (period=MONTHLY, count=0→1) → 3 % 1 = 0 → allowed.
		{"zero-counts-default-to-one", types.BILLING_PERIOD_QUARTER, 0, types.BILLING_PERIOD_MONTHLY, 0, false},
		// Same BillingPeriod, incompatible counts: MONTHLY×3 (3mo) sub vs
		// MONTHLY×2 (2mo) price. 3 % 2 = 1 → not a divisor. Must be rejected —
		// the same-BillingPeriod fast path only accepts identical counts.
		{"same-period-incompatible-counts-rejected", types.BILLING_PERIOD_MONTHLY, 3, types.BILLING_PERIOD_MONTHLY, 2, true},
		// Same BillingPeriod, price divides sub: MONTHLY×6 (6mo) sub vs
		// MONTHLY×2 (2mo) price. 6 % 2 = 0 → allowed (fan-out into 3 sub-windows).
		{"same-period-price-divides-sub-ok", types.BILLING_PERIOD_MONTHLY, 6, types.BILLING_PERIOD_MONTHLY, 2, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &subscription.Subscription{
				StartDate:          sub.StartDate,
				BillingPeriod:      tc.subPer,
				BillingPeriodCount: tc.subCount,
			}
			linePrice := &price.Price{
				BillingPeriod:      tc.pricePer,
				BillingPeriodCount: tc.priceCount,
			}
			req := &CreateSubscriptionLineItemRequest{
				PriceID:  "price_test",
				Quantity: decimal.NewFromInt(1),
			}
			err := req.Validate(linePrice, s)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCreateSubscriptionLineItemRequest_DefaultsCommitmentOverageFactor(t *testing.T) {
	t.Run("defaults to 1.0 when omitted", func(t *testing.T) {
		req := CreateSubscriptionLineItemRequest{
			PriceID:          "price_test",
			CommitmentAmount: lo.ToPtr(decimal.NewFromInt(100)),
		}
		if err := req.Validate(nil, nil); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if req.CommitmentOverageFactor == nil {
			t.Fatal("expected commitment_overage_factor to be defaulted")
		}
		if !req.CommitmentOverageFactor.Equal(decimal.NewFromInt(1)) {
			t.Fatalf("expected default of 1, got: %s", req.CommitmentOverageFactor)
		}
	})

	t.Run("keeps an explicit factor", func(t *testing.T) {
		req := CreateSubscriptionLineItemRequest{
			PriceID:                 "price_test",
			CommitmentAmount:        lo.ToPtr(decimal.NewFromInt(100)),
			CommitmentOverageFactor: lo.ToPtr(decimal.NewFromFloat(1.5)),
		}
		if err := req.Validate(nil, nil); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !req.CommitmentOverageFactor.Equal(decimal.NewFromFloat(1.5)) {
			t.Fatalf("expected 1.5, got: %s", req.CommitmentOverageFactor)
		}
	})

	t.Run("still rejects a factor below 1.0", func(t *testing.T) {
		req := CreateSubscriptionLineItemRequest{
			PriceID:                 "price_test",
			CommitmentAmount:        lo.ToPtr(decimal.NewFromInt(100)),
			CommitmentOverageFactor: lo.ToPtr(decimal.NewFromFloat(0.5)),
		}
		err := req.Validate(nil, nil)
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}
		if !strings.Contains(err.Error(), "commitment_overage_factor must be at least 1.0") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestCommitmentBucketRequest_ToTimeOfDayBucket_DefaultsOverageFactor(t *testing.T) {
	b := CommitmentBucketRequest{
		Start:           types.Bucket{Hour: 9, Minute: 0},
		End:             types.Bucket{Hour: 10, Minute: 0},
		CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
		CommitmentValue: decimal.NewFromInt(100),
	}.ToTimeOfDayBucket()

	if b.OverageFactor == nil || !b.OverageFactor.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("expected overage factor to default to 1, got: %v", b.OverageFactor)
	}
}
