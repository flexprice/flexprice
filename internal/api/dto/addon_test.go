package dto

import (
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func TestAddAddonToSubscriptionRequest_Validate_RejectsDuplicateOverridePriceIDs(t *testing.T) {
	req := &AddAddonToSubscriptionRequest{
		AddonID: "addon_123",
		OverrideLineItems: []OverrideLineItemRequest{
			{PriceID: "price_1", Amount: lo.ToPtr(decimal.NewFromInt(10))},
			{PriceID: "price_1", Amount: lo.ToPtr(decimal.NewFromInt(20))},
		},
	}

	err := req.Validate()
	if err == nil {
		t.Fatal("expected validation error for duplicate override price_id, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate price_id in override line items") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddAddonToSubscriptionRequest_Validate_AcceptsDistinctOverridePriceIDs(t *testing.T) {
	req := &AddAddonToSubscriptionRequest{
		AddonID: "addon_123",
		OverrideLineItems: []OverrideLineItemRequest{
			{PriceID: "price_1", Amount: lo.ToPtr(decimal.NewFromInt(10))},
			{PriceID: "price_2", Amount: lo.ToPtr(decimal.NewFromInt(20))},
		},
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}
