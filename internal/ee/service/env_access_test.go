package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestEnvAccessService(t *testing.T) {
	// Test configuration with user environment mapping
	cfg := &config.Configuration{
		EnvAccess: config.EnvAccessConfig{
			UserEnvMapping: map[string]map[string][]string{
				"tenant1": {
					"user1": {"env1", "env2"},
					"user2": {"env1"},
				},
				"tenant2": {
					"user3": {"env3"},
				},
			},
		},
	}

	envAccessService := NewEnvAccessService(cfg)
	ctx := context.Background()

	t.Run("TestHasEnvironmentAccess", func(t *testing.T) {
		testCases := []struct {
			name          string
			userID        string
			tenantID      string
			environmentID string
			expected      bool
		}{
			{
				name:          "User with access to specific environment",
				userID:        "user1",
				tenantID:      "tenant1",
				environmentID: "env1",
				expected:      true,
			},
			{
				name:          "User without access to specific environment",
				userID:        "user2",
				tenantID:      "tenant1",
				environmentID: "env2",
				expected:      false,
			},
			{
				name:          "User not in mapping (super admin)",
				userID:        "user_not_in_mapping",
				tenantID:      "tenant1",
				environmentID: "env1",
				expected:      true,
			},
			{
				name:          "Tenant not in mapping (super admin)",
				userID:        "user1",
				tenantID:      "tenant_not_in_mapping",
				environmentID: "env1",
				expected:      true,
			},
			{
				name:          "Empty environment ID (backward compatibility)",
				userID:        "user1",
				tenantID:      "tenant1",
				environmentID: "",
				expected:      true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result := envAccessService.HasEnvironmentAccess(ctx, tc.userID, tc.tenantID, tc.environmentID)
				assert.Equal(t, tc.expected, result)
			})
		}
	})

	t.Run("TestGetAllowedEnvironments", func(t *testing.T) {
		testCases := []struct {
			name     string
			userID   string
			tenantID string
			expected []string
		}{
			{
				name:     "User with multiple environments",
				userID:   "user1",
				tenantID: "tenant1",
				expected: []string{"env1", "env2"},
			},
			{
				name:     "User with single environment",
				userID:   "user2",
				tenantID: "tenant1",
				expected: []string{"env1"},
			},
			{
				name:     "User not in mapping (super admin)",
				userID:   "user_not_in_mapping",
				tenantID: "tenant1",
				expected: []string{}, // Empty means all access
			},
			{
				name:     "Tenant not in mapping (super admin)",
				userID:   "user1",
				tenantID: "tenant_not_in_mapping",
				expected: []string{}, // Empty means all access
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result := envAccessService.GetAllowedEnvironments(ctx, tc.userID, tc.tenantID)
				assert.ElementsMatch(t, tc.expected, result)
			})
		}
	})

	t.Run("TestNoConfigurationSuperAdmin", func(t *testing.T) {
		// Test with no configuration - all users should have super admin access
		noCfg := &config.Configuration{
			EnvAccess: config.EnvAccessConfig{
				UserEnvMapping: nil,
			},
		}

		noConfigService := NewEnvAccessService(noCfg)

		// Should allow access to any environment
		result := noConfigService.HasEnvironmentAccess(ctx, "any_user", "any_tenant", "any_env")
		assert.True(t, result)

		// Should return empty list (meaning all access)
		allowedEnvs := noConfigService.GetAllowedEnvironments(ctx, "any_user", "any_tenant")
		assert.Empty(t, allowedEnvs)
	})
}
