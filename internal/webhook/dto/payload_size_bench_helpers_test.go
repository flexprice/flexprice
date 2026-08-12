package webhookDto

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// benchTime is a fixed reference timestamp used across every payload-size fixture so
// benchmark output (and marshaled byte counts) are deterministic between runs.
var benchTime = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

// buildCustomerResponse builds a realistic full customer API response, scaling the
// number of third-party integration mappings -- the one collection on CustomerResponse
// that grows with data volume.
func buildCustomerResponse(nIntegrations int) *dto.CustomerResponse {
	integrations := make([]*dto.EntityIntegrationMappingResponse, nIntegrations)
	for i := 0; i < nIntegrations; i++ {
		integrations[i] = &dto.EntityIntegrationMappingResponse{
			ID:               fmt.Sprintf("integration_mapping_%d", i),
			EntityID:         "cust_bench_customer",
			EntityType:       types.IntegrationEntityTypeCustomer,
			ProviderType:     "stripe",
			ProviderEntityID: fmt.Sprintf("cus_stripe_%d", i),
			EnvironmentID:    "env_bench",
			TenantID:         "tenant_bench",
			Status:           types.StatusPublished,
			Metadata:         map[string]interface{}{"source": "benchmark"},
			CreatedAt:        "2026-01-15T12:00:00Z",
			UpdatedAt:        "2026-01-15T12:00:00Z",
			CreatedBy:        "bench",
			UpdatedBy:        "bench",
		}
	}

	return &dto.CustomerResponse{
		Customer: &customer.Customer{
			ID:                "cust_bench_customer",
			ExternalID:        "ext_cust_bench_customer",
			Name:              "Benchmark Customer Inc.",
			Email:             "billing@benchmark-customer.example",
			AddressLine1:      "123 Benchmark Ave",
			AddressLine2:      "Suite 100",
			AddressCity:       "San Francisco",
			AddressState:      "CA",
			AddressPostalCode: "94107",
			AddressCountry:    "US",
			Timezone:          "America/Los_Angeles",
			Metadata:          types.Metadata{"plan_tier": "enterprise"},
		},
		Integrations: integrations,
	}
}

// buildFeatureResponse builds a realistic full feature API response, including the
// nested meter and group objects that the minimal webhook payload drops. No natural
// scaling collection exists on a feature, so this is a single fixed fixture.
func buildFeatureResponse() *dto.FeatureResponse {
	return &dto.FeatureResponse{
		Feature: &feature.Feature{
			ID:           "feat_bench_feature",
			Name:         "API Requests",
			LookupKey:    "api_requests",
			Description:  "Number of API requests made by the customer",
			Type:         types.FeatureTypeMetered,
			UnitSingular: "request",
			UnitPlural:   "requests",
			MeterID:      "meter_bench_meter",
			GroupID:      "group_bench_group",
			Metadata:     types.Metadata{"category": "usage"},
		},
		Meter: &dto.MeterResponse{
			ID:        "meter_bench_meter",
			Name:      "API Request Meter",
			TenantID:  "tenant_bench",
			EventName: "api_request",
			CreatedAt: benchTime,
			UpdatedAt: benchTime,
			Status:    "published",
		},
		Group: &dto.GroupResponse{
			ID:         "group_bench_group",
			Name:       "Usage Features",
			LookupKey:  "usage_features",
			EntityType: "feature",
			EntityIDs:  []string{"feat_bench_feature"},
			Status:     "published",
			Metadata:   map[string]string{"source": "benchmark"},
			CreatedAt:  benchTime,
			UpdatedAt:  benchTime,
		},
	}
}

// buildWalletResponse builds a realistic full wallet API response. No natural scaling
// collection exists on a wallet, so this is a single fixed fixture.
func buildWalletResponse() *dto.WalletResponse {
	return &dto.WalletResponse{
		Wallet: &wallet.Wallet{
			ID:             "wallet_bench_wallet",
			CustomerID:     "cust_bench_customer",
			Currency:       "usd",
			Balance:        decimal.NewFromInt(5000),
			CreditBalance:  decimal.NewFromInt(5000),
			WalletStatus:   types.WalletStatusActive,
			Name:           "Benchmark Wallet",
			WalletType:     types.WalletTypePrePaid,
			ConversionRate: decimal.NewFromInt(1),
		},
	}
}
