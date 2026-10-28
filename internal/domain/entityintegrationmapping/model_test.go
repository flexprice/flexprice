package entityintegrationmapping

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

func TestEntityIntegrationMappingValidate_ConnectionID(t *testing.T) {
	tests := []struct {
		name         string
		validate     func(*EntityIntegrationMapping) error
		providerType types.IntegrationProviderType
		connectionID *string
		wantErr      bool
	}{
		{name: "stripe_without_connection", validate: ValidateConnection, providerType: types.IntegrationProviderTypeStripe, wantErr: true},
		{name: "stripe_with_connection", validate: ValidateConnection, providerType: types.IntegrationProviderTypeStripe, connectionID: lo.ToPtr("conn_1")},
		{name: "non_connection_provider_without", validate: ValidateConnection, providerType: types.IntegrationProviderTypePaypal},
		{name: "validate_leaves_connection_rule_to_caller", validate: Validate, providerType: types.IntegrationProviderTypeStripe},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &EntityIntegrationMapping{
				EntityID:         "cust_1",
				EntityType:       types.IntegrationEntityTypeCustomer,
				ProviderType:     string(tt.providerType),
				ProviderEntityID: "ext_1",
				ConnectionID:     tt.connectionID,
			}
			err := tt.validate(m)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
