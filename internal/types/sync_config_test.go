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

func TestValidateCustomFields(t *testing.T) {
	servicePeriod := &ServicePeriodCustomFields{StartFieldID: "cf_start", EndFieldID: "cf_end"}

	t.Run("distinct fields", func(t *testing.T) {
		s := &InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: ZohoInvoiceSyncSettings{
				ServicePeriodCustomFields: servicePeriod,
				MetadataCustomFields: []MetadataCustomField{
					{MetadataCustomFieldSourceCustomer, "hubspot_company_id", "cf_hubspot_id"},
					{MetadataCustomFieldSourceCustomer, "brand_name", "cf_brand_name"},
				},
			},
		}
		assert.NoError(t, s.ValidateCustomFields())
	})

	t.Run("duplicate target field", func(t *testing.T) {
		s := &InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: ZohoInvoiceSyncSettings{
				MetadataCustomFields: []MetadataCustomField{
					{MetadataCustomFieldSourceCustomer, "brand_name", "cf_brand"},
					{MetadataCustomFieldSourceInvoice, "brand", "cf_brand"},
				},
			},
		}
		assert.Error(t, s.ValidateCustomFields())
	})

	t.Run("collides with service period field", func(t *testing.T) {
		s := &InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: ZohoInvoiceSyncSettings{
				ServicePeriodCustomFields: servicePeriod,
				MetadataCustomFields: []MetadataCustomField{
					{MetadataCustomFieldSourceCustomer, "brand_name", "cf_end"},
				},
			},
		}
		assert.Error(t, s.ValidateCustomFields())
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
		assert.Error(t, (&InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: ZohoInvoiceSyncSettings{
				MetadataCustomFields: many,
			},
		}).ValidateCustomFields())
		assert.NoError(t, (&InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: ZohoInvoiceSyncSettings{
				MetadataCustomFields: many[:MaxMetadataCustomFields],
			},
		}).ValidateCustomFields())
	})

	t.Run("nil and empty", func(t *testing.T) {
		var s *InvoiceSyncSettings
		assert.NoError(t, s.ValidateCustomFields())
		assert.NoError(t, (&InvoiceSyncSettings{}).ValidateCustomFields())
	})

	t.Run("global collides with metadata and service period", func(t *testing.T) {
		withGlobal := func(field string) *InvoiceSyncSettings {
			return &InvoiceSyncSettings{
				ZohoInvoiceSyncSettings: ZohoInvoiceSyncSettings{
					ServicePeriodCustomFields: servicePeriod,
					GlobalCustomFields:        []GlobalCustomField{{Field: field, Value: "flexprice"}},
					MetadataCustomFields: []MetadataCustomField{
						{MetadataCustomFieldSourceInvoice, "brand", "cf_brand"},
					},
				},
			}
		}
		assert.NoError(t, withGlobal("cf_source").ValidateCustomFields())
		assert.Error(t, withGlobal("cf_brand").ValidateCustomFields())
		assert.Error(t, withGlobal("cf_end").ValidateCustomFields())
	})

	t.Run("whitespace variant still collides with service period", func(t *testing.T) {
		s := &InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: ZohoInvoiceSyncSettings{
				ServicePeriodCustomFields: &ServicePeriodCustomFields{StartFieldID: " cf_start ", EndFieldID: "cf_end"},
				MetadataCustomFields: []MetadataCustomField{
					{MetadataCustomFieldSourceInvoice, "k", "cf_start"},
				},
			},
		}
		assert.Error(t, s.ValidateCustomFields())
	})

	t.Run("service period start and end must differ", func(t *testing.T) {
		s := &InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: ZohoInvoiceSyncSettings{
				ServicePeriodCustomFields: &ServicePeriodCustomFields{StartFieldID: "cf_same", EndFieldID: "cf_same"},
			},
		}
		assert.Error(t, s.ValidateCustomFields())
	})

	t.Run("global requires field and value", func(t *testing.T) {
		withGlobal := func(g GlobalCustomField) *InvoiceSyncSettings {
			return &InvoiceSyncSettings{
				ZohoInvoiceSyncSettings: ZohoInvoiceSyncSettings{GlobalCustomFields: []GlobalCustomField{g}},
			}
		}
		assert.Error(t, withGlobal(GlobalCustomField{Field: "  ", Value: "v"}).ValidateCustomFields())
		assert.Error(t, withGlobal(GlobalCustomField{Field: "cf_a", Value: "  "}).ValidateCustomFields())
		assert.NoError(t, withGlobal(GlobalCustomField{Field: "cf_a", Value: "v"}).ValidateCustomFields())
	})

	t.Run("cap spans both families", func(t *testing.T) {
		globals := make([]GlobalCustomField, 0, MaxMetadataCustomFields)
		for i := 0; i < MaxMetadataCustomFields; i++ {
			globals = append(globals, GlobalCustomField{Field: fmt.Sprintf("cf_g_%d", i), Value: "v"})
		}
		s := &InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: ZohoInvoiceSyncSettings{
				GlobalCustomFields: globals,
				MetadataCustomFields: []MetadataCustomField{
					{MetadataCustomFieldSourceInvoice, "brand", "cf_brand"},
				},
			},
		}
		assert.Error(t, s.ValidateCustomFields())
	})
}

func TestSyncConfigValidateRejectsBadMetadataCustomFields(t *testing.T) {
	cfg := &SyncConfig{
		InvoiceSyncSettings: &InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: ZohoInvoiceSyncSettings{
				MetadataCustomFields: []MetadataCustomField{
					{MetadataCustomFieldSourceCustomer, "brand_name", ""},
				},
			},
		},
	}
	assert.Error(t, cfg.Validate())
}

func TestZohoPaymentGatewaySettingsValidate(t *testing.T) {
	tests := []struct {
		name     string
		settings *ZohoPaymentGatewaySettings
		wantErr  bool
	}{
		{"nil settings", nil, false},
		{"disabled with nothing selected", &ZohoPaymentGatewaySettings{}, false},
		{"disabled keeps a saved selection", &ZohoPaymentGatewaySettings{Gateways: []ZohoPaymentGateway{ZohoPaymentGatewayStripe}}, false},
		{"enabled with one gateway", &ZohoPaymentGatewaySettings{Enabled: true, Gateways: []ZohoPaymentGateway{ZohoPaymentGatewayRazorpay}}, false},
		{"enabled with every supported gateway", &ZohoPaymentGatewaySettings{Enabled: true, Gateways: []ZohoPaymentGateway{
			ZohoPaymentGatewayPayPal, ZohoPaymentGatewayStripe, ZohoPaymentGatewayAuthorizeNet, ZohoPaymentGatewayPayflowPro,
			ZohoPaymentGateway2Checkout, ZohoPaymentGatewayBraintree, ZohoPaymentGatewayRazorpay,
		}, PayPalPaymentMethod: ZohoPayPalPaymentMethodStandard}, false},
		{"paypal with standard method", &ZohoPaymentGatewaySettings{Enabled: true, Gateways: []ZohoPaymentGateway{ZohoPaymentGatewayPayPal}, PayPalPaymentMethod: ZohoPayPalPaymentMethodStandard}, false},
		{"paypal with adaptive method", &ZohoPaymentGatewaySettings{Enabled: true, Gateways: []ZohoPaymentGateway{ZohoPaymentGatewayPayPal}, PayPalPaymentMethod: ZohoPayPalPaymentMethodAdaptive}, false},
		{"paypal without a method", &ZohoPaymentGatewaySettings{Enabled: true, Gateways: []ZohoPaymentGateway{ZohoPaymentGatewayPayPal}}, true},
		{"paypal without a method while disabled", &ZohoPaymentGatewaySettings{Gateways: []ZohoPaymentGateway{ZohoPaymentGatewayPayPal}}, true},
		{"unsupported paypal method", &ZohoPaymentGatewaySettings{Enabled: true, Gateways: []ZohoPaymentGateway{ZohoPaymentGatewayPayPal}, PayPalPaymentMethod: "express"}, true},
		{"paypal method ignored without paypal", &ZohoPaymentGatewaySettings{Enabled: true, Gateways: []ZohoPaymentGateway{ZohoPaymentGatewayStripe}}, false},
		{"enabled with nothing selected", &ZohoPaymentGatewaySettings{Enabled: true}, true},
		{"unsupported gateway", &ZohoPaymentGatewaySettings{Enabled: true, Gateways: []ZohoPaymentGateway{ZohoPaymentGatewayStripe, "paytm"}}, true},
		{"unsupported gateway while disabled", &ZohoPaymentGatewaySettings{Gateways: []ZohoPaymentGateway{"paytm"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.settings.Validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestSyncConfigValidateRejectsBadZohoPaymentGateways(t *testing.T) {
	cfg := &SyncConfig{
		InvoiceSyncSettings: &InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: ZohoInvoiceSyncSettings{
				PaymentGateways: &ZohoPaymentGatewaySettings{Enabled: true},
			},
		},
	}
	assert.Error(t, cfg.Validate())
}
