package types

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadataCustomFieldValidate(t *testing.T) {
	tests := []struct {
		name    string
		field   MetadataCustomField
		wantErr bool
	}{
		{"customer source", MetadataCustomField{MetadataCustomFieldSourceCustomer, "brand_name", "cf_brand"}, false},
		{"invoice source", MetadataCustomField{MetadataCustomFieldSourceInvoice, "po_number", "4069923000000000001"}, false},
		{"empty source", MetadataCustomField{"", "brand_name", "cf_brand"}, true},
		{"unknown source", MetadataCustomField{"subscription", "brand_name", "cf_brand"}, true},
		{"blank metadata key", MetadataCustomField{MetadataCustomFieldSourceCustomer, "  ", "cf_brand"}, true},
		{"blank field", MetadataCustomField{MetadataCustomFieldSourceCustomer, "brand_name", ""}, true},
		{"key at cap", MetadataCustomField{MetadataCustomFieldSourceCustomer, strings.Repeat("k", maxCustomFieldRefLen), "cf_brand"}, false},
		{"key over cap", MetadataCustomField{MetadataCustomFieldSourceCustomer, strings.Repeat("k", maxCustomFieldRefLen+1), "cf_brand"}, true},
		{"field over cap", MetadataCustomField{MetadataCustomFieldSourceCustomer, "brand_name", strings.Repeat("f", maxCustomFieldRefLen+1)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.field.Validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateMetadataCustomFields(t *testing.T) {
	servicePeriod := &ServicePeriodCustomFields{StartFieldID: "cf_start", EndFieldID: "cf_end"}

	t.Run("distinct fields", func(t *testing.T) {
		s := &InvoiceSyncSettings{
			ServicePeriodCustomFields: servicePeriod,
			MetadataCustomFields: []MetadataCustomField{
				{MetadataCustomFieldSourceCustomer, "hubspot_company_id", "cf_hubspot_id"},
				{MetadataCustomFieldSourceCustomer, "brand_name", "cf_brand_name"},
			},
		}
		assert.NoError(t, s.ValidateMetadataCustomFields())
	})

	t.Run("duplicate target field", func(t *testing.T) {
		s := &InvoiceSyncSettings{
			MetadataCustomFields: []MetadataCustomField{
				{MetadataCustomFieldSourceCustomer, "brand_name", "cf_brand"},
				{MetadataCustomFieldSourceInvoice, "brand", "cf_brand"},
			},
		}
		assert.Error(t, s.ValidateMetadataCustomFields())
	})

	t.Run("collides with service period field", func(t *testing.T) {
		s := &InvoiceSyncSettings{
			ServicePeriodCustomFields: servicePeriod,
			MetadataCustomFields: []MetadataCustomField{
				{MetadataCustomFieldSourceCustomer, "brand_name", "cf_end"},
			},
		}
		assert.Error(t, s.ValidateMetadataCustomFields())
	})

	t.Run("too many mappings", func(t *testing.T) {
		many := make([]MetadataCustomField, 0, MaxMetadataCustomFields+1)
		for i := 0; i <= MaxMetadataCustomFields; i++ {
			many = append(many, MetadataCustomField{
				MetadataCustomFieldSourceCustomer,
				fmt.Sprintf("key_%d", i),
				fmt.Sprintf("cf_%d", i),
			})
		}
		assert.Error(t, (&InvoiceSyncSettings{MetadataCustomFields: many}).ValidateMetadataCustomFields())
		assert.NoError(t, (&InvoiceSyncSettings{MetadataCustomFields: many[:MaxMetadataCustomFields]}).ValidateMetadataCustomFields())
	})

	t.Run("nil and empty", func(t *testing.T) {
		var s *InvoiceSyncSettings
		assert.NoError(t, s.ValidateMetadataCustomFields())
		assert.NoError(t, (&InvoiceSyncSettings{}).ValidateMetadataCustomFields())
	})
}

func TestSyncConfigValidateRejectsBadMetadataCustomFields(t *testing.T) {
	cfg := &SyncConfig{
		InvoiceSyncSettings: &InvoiceSyncSettings{
			MetadataCustomFields: []MetadataCustomField{
				{MetadataCustomFieldSourceCustomer, "brand_name", ""},
			},
		},
	}
	assert.Error(t, cfg.Validate())
}
