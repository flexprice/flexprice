package dto_test

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testGSTIN  = "27AAMCM4148E1ZD"
	testHSNSAC = "998415"
)

// A price update that trips ShouldCreateNewPrice() builds a brand new price via
// ToCreatePriceRequest. If metadata were not carried over, hsn_sac would be
// silently dropped and the product would quietly lose its tax classification.
func TestToCreatePriceRequestPreservesHSNSAC(t *testing.T) {
	existing := &price.Price{
		ID:                 "price_existing",
		Currency:           "INR",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           "plan_test_123",
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		Amount:             decimal.NewFromInt(15000),
		Metadata:           price.JSONBMetadata{types.MetadataKeyHSNSAC: testHSNSAC},
	}

	t.Run("carried over when the update omits metadata", func(t *testing.T) {
		newAmount := decimal.NewFromInt(20000)
		req := dto.UpdatePriceRequest{Amount: &newAmount}
		require.True(t, req.ShouldCreateNewPrice())

		createReq := req.ToCreatePriceRequest(existing)
		assert.Equal(t, testHSNSAC, createReq.Metadata[types.MetadataKeyHSNSAC])
	})

	t.Run("explicit metadata replaces the map wholesale", func(t *testing.T) {
		newAmount := decimal.NewFromInt(20000)
		req := dto.UpdatePriceRequest{
			Amount:   &newAmount,
			Metadata: map[string]string{"note": "repriced"},
		}

		createReq := req.ToCreatePriceRequest(existing)
		// Documents current behaviour: supplying metadata replaces rather than
		// merges, so a caller that sets metadata must resend hsn_sac.
		assert.Equal(t, "repriced", createReq.Metadata["note"])
		assert.Empty(t, createReq.Metadata[types.MetadataKeyHSNSAC])
	})

	t.Run("resending hsn_sac alongside other keys keeps it", func(t *testing.T) {
		newAmount := decimal.NewFromInt(20000)
		req := dto.UpdatePriceRequest{
			Amount: &newAmount,
			Metadata: map[string]string{
				"note":                  "repriced",
				types.MetadataKeyHSNSAC: testHSNSAC,
			},
		}

		createReq := req.ToCreatePriceRequest(existing)
		assert.Equal(t, testHSNSAC, createReq.Metadata[types.MetadataKeyHSNSAC])
	})
}

func TestCustomerRequestValidatesTaxMetadata(t *testing.T) {
	t.Run("create rejects a malformed GSTIN", func(t *testing.T) {
		req := dto.CreateCustomerRequest{
			ExternalID: "cust_ext_1",
			Name:       "MCGILL FOODS PRIVATE LIMITED",
			Metadata:   map[string]string{types.MetadataKeyGSTIN: "not-a-gstin"},
		}
		err := req.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GSTIN")
	})

	t.Run("create accepts a valid GSTIN", func(t *testing.T) {
		req := dto.CreateCustomerRequest{
			ExternalID: "cust_ext_1",
			Name:       "MCGILL FOODS PRIVATE LIMITED",
			Metadata:   map[string]string{types.MetadataKeyGSTIN: testGSTIN},
		}
		require.NoError(t, req.Validate())
	})

	t.Run("create rejects a PAN contradicting the GSTIN", func(t *testing.T) {
		req := dto.CreateCustomerRequest{
			ExternalID: "cust_ext_1",
			Name:       "MCGILL FOODS PRIVATE LIMITED",
			Metadata: map[string]string{
				types.MetadataKeyGSTIN: testGSTIN,
				types.MetadataKeyPAN:   "ZZZZZ1111Z",
			},
		}
		require.Error(t, req.Validate())
	})

	t.Run("update rejects a malformed GSTIN", func(t *testing.T) {
		req := dto.UpdateCustomerRequest{
			Metadata: map[string]string{types.MetadataKeyGSTIN: "27AAMCM4148E1Z"},
		}
		require.Error(t, req.Validate())
	})

	t.Run("customer without tax metadata is unaffected", func(t *testing.T) {
		req := dto.CreateCustomerRequest{
			ExternalID: "cust_ext_2",
			Name:       "Acme Inc",
			Metadata:   map[string]string{"tier": "enterprise"},
		}
		require.NoError(t, req.Validate())
	})
}

func TestPriceRequestValidatesHSNSAC(t *testing.T) {
	t.Run("create rejects a malformed HSN/SAC", func(t *testing.T) {
		req := validFlatFeeRequest()
		req.Metadata = map[string]string{types.MetadataKeyHSNSAC: "99AB15"}
		require.Error(t, req.Validate())
	})

	t.Run("create accepts a valid HSN/SAC", func(t *testing.T) {
		req := validFlatFeeRequest()
		req.Metadata = map[string]string{types.MetadataKeyHSNSAC: testHSNSAC}
		require.NoError(t, req.Validate())
	})

	t.Run("update rejects a malformed HSN/SAC", func(t *testing.T) {
		req := dto.UpdatePriceRequest{
			Metadata: map[string]string{types.MetadataKeyHSNSAC: "99841"},
		}
		require.Error(t, req.Validate())
	})
}
