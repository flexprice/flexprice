package dto_test

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/stretchr/testify/assert"
)

func TestUpdateCustomerRequest_Validate_RejectsSystemMetaKeys(t *testing.T) {
	req := dto.UpdateCustomerRequest{
		Metadata: map[string]string{
			"_fp_has_parent_sub": "true",
		},
	}
	err := req.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reserved")
}

func TestUpdateCustomerRequest_Validate_AllowsUserMetaKeys(t *testing.T) {
	req := dto.UpdateCustomerRequest{
		Metadata: map[string]string{
			"hubspot_deal_id": "hs_123",
			"account_tier":    "enterprise",
		},
	}
	err := req.Validate()
	assert.NoError(t, err)
}
