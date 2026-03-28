package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestComputeCommitmentBreakdown(t *testing.T) {
	t.Run("no line items", func(t *testing.T) {
		result := computeCommitmentBreakdown(nil)
		assert.False(t, result.HasCommitments)
		assert.Equal(t, 0, result.LineItemCount)
	})

	t.Run("no commitments on line items", func(t *testing.T) {
		items := []*invoice.InvoiceLineItem{
			{Amount: decimal.NewFromInt(100)},
			{Amount: decimal.NewFromInt(200)},
		}
		result := computeCommitmentBreakdown(items)
		assert.False(t, result.HasCommitments)
		assert.Equal(t, 0, result.LineItemCount)
	})

	t.Run("single line item with overage", func(t *testing.T) {
		items := []*invoice.InvoiceLineItem{
			{
				Amount: decimal.NewFromInt(150),
				CommitmentInfo: &types.CommitmentInfo{
					Type:                             types.COMMITMENT_TYPE_AMOUNT,
					Amount:                           decimal.NewFromInt(100),
					TrueUpEnabled:                    false,
					ComputedCommitmentUtilizedAmount: decimal.NewFromInt(100),
					ComputedOverageAmount:            decimal.NewFromInt(50),
					ComputedTrueUpAmount:             decimal.Zero,
				},
			},
		}
		result := computeCommitmentBreakdown(items)
		assert.True(t, result.HasCommitments)
		assert.Equal(t, 1, result.LineItemCount)
		assert.True(t, result.TotalCommitmentAmount.Equal(decimal.NewFromInt(100)))
		assert.True(t, result.TotalCommitmentUtilized.Equal(decimal.NewFromInt(100)))
		assert.True(t, result.TotalOverageAmount.Equal(decimal.NewFromInt(50)))
		assert.True(t, result.TotalTrueUpAmount.Equal(decimal.Zero))
		assert.True(t, result.TotalCommitmentCharge.Equal(decimal.NewFromInt(150)))
	})

	t.Run("single line item with true-up", func(t *testing.T) {
		items := []*invoice.InvoiceLineItem{
			{
				Amount: decimal.NewFromInt(100),
				CommitmentInfo: &types.CommitmentInfo{
					Type:                             types.COMMITMENT_TYPE_AMOUNT,
					Amount:                           decimal.NewFromInt(100),
					TrueUpEnabled:                    true,
					ComputedCommitmentUtilizedAmount: decimal.NewFromInt(60),
					ComputedOverageAmount:            decimal.Zero,
					ComputedTrueUpAmount:             decimal.NewFromInt(40),
				},
			},
		}
		result := computeCommitmentBreakdown(items)
		assert.True(t, result.HasCommitments)
		assert.True(t, result.TotalCommitmentUtilized.Equal(decimal.NewFromInt(60)))
		assert.True(t, result.TotalTrueUpAmount.Equal(decimal.NewFromInt(40)))
		assert.True(t, result.TotalCommitmentCharge.Equal(decimal.NewFromInt(100)))
	})

	t.Run("multiple line items mixed commitment and non-commitment", func(t *testing.T) {
		items := []*invoice.InvoiceLineItem{
			{
				Amount: decimal.NewFromInt(120),
				CommitmentInfo: &types.CommitmentInfo{
					Type:                             types.COMMITMENT_TYPE_AMOUNT,
					Amount:                           decimal.NewFromInt(100),
					ComputedCommitmentUtilizedAmount: decimal.NewFromInt(100),
					ComputedOverageAmount:            decimal.NewFromInt(20),
					ComputedTrueUpAmount:             decimal.Zero,
				},
			},
			{
				Amount: decimal.NewFromInt(50), // no commitment
			},
			{
				Amount: decimal.NewFromInt(200),
				CommitmentInfo: &types.CommitmentInfo{
					Type:                             types.COMMITMENT_TYPE_QUANTITY,
					Amount:                           decimal.NewFromInt(200),
					Quantity:                         decimal.NewFromInt(1000),
					TrueUpEnabled:                    true,
					ComputedCommitmentUtilizedAmount: decimal.NewFromInt(150),
					ComputedOverageAmount:            decimal.Zero,
					ComputedTrueUpAmount:             decimal.NewFromInt(50),
				},
			},
		}
		result := computeCommitmentBreakdown(items)
		assert.True(t, result.HasCommitments)
		assert.Equal(t, 2, result.LineItemCount)
		assert.True(t, result.TotalCommitmentAmount.Equal(decimal.NewFromInt(300)))
		assert.True(t, result.TotalCommitmentUtilized.Equal(decimal.NewFromInt(250)))
		assert.True(t, result.TotalOverageAmount.Equal(decimal.NewFromInt(20)))
		assert.True(t, result.TotalTrueUpAmount.Equal(decimal.NewFromInt(50)))
		assert.True(t, result.TotalCommitmentCharge.Equal(decimal.NewFromInt(320)))
	})
}

func TestNewInvoiceResponse_CommitmentBreakdown(t *testing.T) {
	t.Run("included when commitments exist", func(t *testing.T) {
		inv := &invoice.Invoice{
			ID:         "inv_1",
			CustomerID: "cust_1",
			Currency:   "USD",
			LineItems: []*invoice.InvoiceLineItem{
				{
					ID:       "li_1",
					Amount:   decimal.NewFromInt(120),
					Currency: "USD",
					CommitmentInfo: &types.CommitmentInfo{
						Type:                             types.COMMITMENT_TYPE_AMOUNT,
						Amount:                           decimal.NewFromInt(100),
						ComputedCommitmentUtilizedAmount: decimal.NewFromInt(100),
						ComputedOverageAmount:            decimal.NewFromInt(20),
						ComputedTrueUpAmount:             decimal.Zero,
					},
				},
			},
		}
		resp := NewInvoiceResponse(inv)
		assert.NotNil(t, resp.CommitmentBreakdown)
		assert.True(t, resp.CommitmentBreakdown.HasCommitments)
		assert.True(t, resp.CommitmentBreakdown.TotalCommitmentCharge.Equal(decimal.NewFromInt(120)))
	})

	t.Run("omitted when no commitments", func(t *testing.T) {
		inv := &invoice.Invoice{
			ID:         "inv_2",
			CustomerID: "cust_1",
			Currency:   "USD",
			LineItems: []*invoice.InvoiceLineItem{
				{
					ID:       "li_1",
					Amount:   decimal.NewFromInt(50),
					Currency: "USD",
				},
			},
		}
		resp := NewInvoiceResponse(inv)
		assert.Nil(t, resp.CommitmentBreakdown)
	})
}
