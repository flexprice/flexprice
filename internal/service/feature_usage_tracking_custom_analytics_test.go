package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCustomAnalytics_ApplyCustomRule(t *testing.T) {
	// Create a minimal test service - we only need the method, not the full service
	service := &featureUsageTrackingService{}

	tests := []struct {
		name           string
		rule           types.CustomAnalyticsRule
		sourceItem     dto.UsageAnalyticItem
		expectedResult *dto.CustomAnalyticItem
		expectNil      bool
	}{
		{
			name: "VAPI hosting fee - revenue per minute calculation",
			rule: types.CustomAnalyticsRule{
				ID:         string(types.CustomAnalyticsRuleRevenuePerMinute),
				TargetType: "feature",
				TargetID:   "feat_vapi_hosting_fee",
			},
			sourceItem: dto.UsageAnalyticItem{
				FeatureID:   "feat_vapi_hosting_fee",
				FeatureName: "feat_vapi_hosting_fee",
				TotalUsage:  decimal.NewFromInt(30000000), // 30 million milliseconds = 500 minutes
				TotalCost:   decimal.NewFromInt(1500),     // $1500
			},
			expectedResult: &dto.CustomAnalyticItem{
				ID:          string(types.CustomAnalyticsRuleRevenuePerMinute),
				Name:        "Revenue per Minute",
				FeatureName: "feat_vapi_hosting_fee",
				Value:       decimal.NewFromInt(3), // $1500 / 500 minutes = $3/min
				Type:        "feature",
			},
			expectNil: false,
		},
		{
			name: "Zero usage - should return nil to avoid division by zero",
			rule: types.CustomAnalyticsRule{
				ID:         string(types.CustomAnalyticsRuleRevenuePerMinute),
				TargetType: "feature",
				TargetID:   "feat_vapi_hosting_fee",
			},
			sourceItem: dto.UsageAnalyticItem{
				FeatureID:   "feat_vapi_hosting_fee",
				FeatureName: "feat_vapi_hosting_fee",
				TotalUsage:  decimal.Zero,
				TotalCost:   decimal.NewFromInt(1500),
			},
			expectedResult: nil,
			expectNil:      true,
		},
		{
			name: "Unknown rule ID - should return nil",
			rule: types.CustomAnalyticsRule{
				ID:         "unknown-rule",
				TargetType: "feature",
				TargetID:   "feat_test",
			},
			sourceItem: dto.UsageAnalyticItem{
				FeatureID:  "feat_test",
				TotalUsage: decimal.NewFromInt(1000),
				TotalCost:  decimal.NewFromInt(100),
			},
			expectedResult: nil,
			expectNil:      true,
		},
		{
			name: "Small usage - precise calculation",
			rule: types.CustomAnalyticsRule{
				ID:         string(types.CustomAnalyticsRuleRevenuePerMinute),
				TargetType: "feature",
				TargetID:   "feat_vapi_hosting_fee",
			},
			sourceItem: dto.UsageAnalyticItem{
				FeatureID:   "feat_vapi_hosting_fee",
				FeatureName: "feat_vapi_hosting_fee",
				TotalUsage:  decimal.NewFromInt(60000), // 1 minute in milliseconds
				TotalCost:   decimal.NewFromFloat(5.50),
			},
			expectedResult: &dto.CustomAnalyticItem{
				ID:          string(types.CustomAnalyticsRuleRevenuePerMinute),
				Name:        "Revenue per Minute",
				FeatureName: "feat_vapi_hosting_fee",
				Value:       decimal.NewFromFloat(5.50), // $5.50 / 1 minute = $5.50/min
				Type:        "feature",
			},
			expectNil: false,
		},
		{
			name: "Large usage - verify calculation accuracy",
			rule: types.CustomAnalyticsRule{
				ID:         string(types.CustomAnalyticsRuleRevenuePerMinute),
				TargetType: "feature",
				TargetID:   "feat_vapi_hosting_fee",
			},
			sourceItem: dto.UsageAnalyticItem{
				FeatureID:   "feat_vapi_hosting_fee",
				FeatureName: "feat_vapi_hosting_fee",
				TotalUsage:  decimal.NewFromInt(120000000), // 2000 minutes
				TotalCost:   decimal.NewFromInt(10000),     // $10,000
			},
			expectedResult: &dto.CustomAnalyticItem{
				ID:          string(types.CustomAnalyticsRuleRevenuePerMinute),
				Name:        "Revenue per Minute",
				FeatureName: "feat_vapi_hosting_fee",
				Value:       decimal.NewFromInt(5), // $10,000 / 2000 minutes = $5/min
				Type:        "feature",
			},
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.applyCustomRule(tt.rule, tt.sourceItem)

			if tt.expectNil {
				assert.Nil(t, result, "Expected nil result")
			} else {
				assert.NotNil(t, result, "Expected non-nil result")
				assert.Equal(t, tt.expectedResult.ID, result.ID)
				assert.Equal(t, tt.expectedResult.Name, result.Name)
				assert.Equal(t, tt.expectedResult.Type, result.Type)
				assert.True(t, tt.expectedResult.Value.Equal(result.Value),
					"Expected value %s, got %s", tt.expectedResult.Value.String(), result.Value.String())
			}
		})
	}
}

func TestCustomAnalytics_ConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      types.CustomAnalyticsConfig
		expectError bool
	}{
		{
			name: "Valid configuration",
			config: types.CustomAnalyticsConfig{
				Rules: []types.CustomAnalyticsRule{
					{
						ID:         "test-rule",
						TargetType: "feature",
						TargetID:   "feat_test",
					},
				},
			},
			expectError: false,
		},
		{
			name: "Empty rules array is valid",
			config: types.CustomAnalyticsConfig{
				Rules: []types.CustomAnalyticsRule{},
			},
			expectError: false,
		},
		{
			name: "Multiple rules",
			config: types.CustomAnalyticsConfig{
				Rules: []types.CustomAnalyticsRule{
					{
						ID:         "rule1",
						TargetType: "feature",
						TargetID:   "feat_1",
					},
					{
						ID:         "rule2",
						TargetType: "meter",
						TargetID:   "meter_1",
					},
					{
						ID:         "rule3",
						TargetType: "event_name",
						TargetID:   "event_1",
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
