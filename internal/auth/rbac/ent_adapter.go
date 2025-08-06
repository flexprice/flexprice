package rbac

import (
	"context"
	"fmt"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/rbacpolicy"
	"github.com/flexprice/flexprice/internal/logger"
)

// EntAdapter implements Casbin's persist.Adapter interface using Ent
type EntAdapter struct {
	client *ent.Client
	logger *logger.Logger
}

// NewEntAdapter creates a new Ent adapter for Casbin
func NewEntAdapter(client *ent.Client, logger *logger.Logger) *EntAdapter {
	return &EntAdapter{
		client: client,
		logger: logger,
	}
}

// LoadPolicy loads all policy rules from the database
func (a *EntAdapter) LoadPolicy(model model.Model) error {
	a.logger.Debugw("loading policies from database")

	ctx := context.Background()
	policies, err := a.client.RBACPolicy.Query().
		Where(rbacpolicy.StatusEQ("published")).
		All(ctx)

	if err != nil {
		return fmt.Errorf("failed to load policies: %w", err)
	}

	for _, policy := range policies {
		// Format: p, role, resource, action, tenant, effect
		line := fmt.Sprintf("p, %s, %s, %s, %s, %s",
			policy.Role, policy.Resource, policy.Action, policy.TenantID, policy.Effect)

		persist.LoadPolicyLine(line, model)
	}

	a.logger.Infow("loaded policies from database", "count", len(policies))
	return nil
}

// SavePolicy saves all policy rules to the database
func (a *EntAdapter) SavePolicy(model model.Model) error {
	a.logger.Debugw("saving policies to database")

	ctx := context.Background()

	// Clear existing policies
	_, err := a.client.RBACPolicy.Delete().Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to clear existing policies: %w", err)
	}

	// Get all policy rules from the model
	rules, _ := model.GetPolicy("p", "p")

	for _, rule := range rules {
		if len(rule) >= 5 {
			_, err := a.client.RBACPolicy.Create().
				SetRole(rule[0]).
				SetResource(rule[1]).
				SetAction(rule[2]).
				SetTenantID(rule[3]).
				SetEffect(rule[4]).
				Save(ctx)

			if err != nil {
				a.logger.Errorw("failed to save policy", "error", err, "rule", rule)
			}
		}
	}

	a.logger.Infow("saved policies to database", "count", len(rules))
	return nil
}

// AddPolicy adds a policy rule to the storage
func (a *EntAdapter) AddPolicy(sec string, ptype string, rule []string) error {
	a.logger.Debugw("adding policy", "ptype", ptype, "rule", rule)

	if len(rule) < 4 {
		return fmt.Errorf("invalid policy rule: insufficient parameters")
	}

	ctx := context.Background()
	_, err := a.client.RBACPolicy.Create().
		SetRole(rule[0]).
		SetResource(rule[1]).
		SetAction(rule[2]).
		SetTenantID(rule[3]).
		SetEffect(rule[4]).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to add policy: %w", err)
	}

	return nil
}

// RemovePolicy removes a policy rule from the storage
func (a *EntAdapter) RemovePolicy(sec string, ptype string, rule []string) error {
	a.logger.Debugw("removing policy", "ptype", ptype, "rule", rule)

	if len(rule) < 4 {
		return fmt.Errorf("invalid policy rule: insufficient parameters")
	}

	ctx := context.Background()
	_, err := a.client.RBACPolicy.Delete().
		Where(
			rbacpolicy.Role(rule[0]),
			rbacpolicy.Resource(rule[1]),
			rbacpolicy.Action(rule[2]),
			rbacpolicy.TenantID(rule[3]),
		).
		Exec(ctx)

	if err != nil {
		return fmt.Errorf("failed to remove policy: %w", err)
	}

	return nil
}

// RemoveFilteredPolicy removes policy rules that match the filter from the storage
func (a *EntAdapter) RemoveFilteredPolicy(sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	a.logger.Debugw("removing filtered policy", "ptype", ptype, "fieldIndex", fieldIndex, "fieldValues", fieldValues)

	ctx := context.Background()
	query := a.client.RBACPolicy.Delete()

	// Apply filters based on fieldIndex
	switch fieldIndex {
	case 0: // Role
		if len(fieldValues) > 0 {
			query = query.Where(rbacpolicy.Role(fieldValues[0]))
		}
	case 1: // Resource
		if len(fieldValues) > 0 {
			query = query.Where(rbacpolicy.Resource(fieldValues[0]))
		}
	case 2: // Action
		if len(fieldValues) > 0 {
			query = query.Where(rbacpolicy.Action(fieldValues[0]))
		}
	case 3: // Tenant
		if len(fieldValues) > 0 {
			query = query.Where(rbacpolicy.TenantID(fieldValues[0]))
		}
	case 4: // Effect
		if len(fieldValues) > 0 {
			query = query.Where(rbacpolicy.Effect(fieldValues[0]))
		}
	}

	_, err := query.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to remove filtered policy: %w", err)
	}

	return nil
}

// AddPolicies adds multiple policy rules to the storage
func (a *EntAdapter) AddPolicies(sec string, ptype string, rules [][]string) error {
	a.logger.Debugw("adding multiple policies", "ptype", ptype, "count", len(rules))

	ctx := context.Background()

	for _, rule := range rules {
		if len(rule) >= 4 {
			_, err := a.client.RBACPolicy.Create().
				SetRole(rule[0]).
				SetResource(rule[1]).
				SetAction(rule[2]).
				SetTenantID(rule[3]).
				SetEffect(rule[4]).
				Save(ctx)

			if err != nil {
				a.logger.Errorw("failed to add policy", "error", err, "rule", rule)
			}
		}
	}

	return nil
}

// RemovePolicies removes multiple policy rules from the storage
func (a *EntAdapter) RemovePolicies(sec string, ptype string, rules [][]string) error {
	a.logger.Debugw("removing multiple policies", "ptype", ptype, "count", len(rules))

	ctx := context.Background()

	for _, rule := range rules {
		if len(rule) >= 4 {
			_, err := a.client.RBACPolicy.Delete().
				Where(
					rbacpolicy.Role(rule[0]),
					rbacpolicy.Resource(rule[1]),
					rbacpolicy.Action(rule[2]),
					rbacpolicy.TenantID(rule[3]),
				).
				Exec(ctx)

			if err != nil {
				a.logger.Errorw("failed to remove policy", "error", err, "rule", rule)
			}
		}
	}

	return nil
}

// UpdatePolicy updates a policy rule from the storage
func (a *EntAdapter) UpdatePolicy(sec string, ptype string, oldRule, newRule []string) error {
	a.logger.Debugw("updating policy", "ptype", ptype, "oldRule", oldRule, "newRule", newRule)

	// Remove old rule
	err := a.RemovePolicy(sec, ptype, oldRule)
	if err != nil {
		return fmt.Errorf("failed to remove old policy: %w", err)
	}

	// Add new rule
	err = a.AddPolicy(sec, ptype, newRule)
	if err != nil {
		return fmt.Errorf("failed to add new policy: %w", err)
	}

	return nil
}

// UpdatePolicies updates multiple policy rules from the storage
func (a *EntAdapter) UpdatePolicies(sec string, ptype string, oldRules, newRules [][]string) error {
	a.logger.Debugw("updating multiple policies", "ptype", ptype, "oldCount", len(oldRules), "newCount", len(newRules))

	// Remove old rules
	err := a.RemovePolicies(sec, ptype, oldRules)
	if err != nil {
		return fmt.Errorf("failed to remove old policies: %w", err)
	}

	// Add new rules
	err = a.AddPolicies(sec, ptype, newRules)
	if err != nil {
		return fmt.Errorf("failed to add new policies: %w", err)
	}

	return nil
}

// UpdateFilteredPolicies updates policy rules that match the filter from the storage
func (a *EntAdapter) UpdateFilteredPolicies(sec string, ptype string, newRules [][]string, fieldIndex int, fieldValues ...string) (bool, error) {
	a.logger.Debugw("updating filtered policies", "ptype", ptype, "newCount", len(newRules), "fieldIndex", fieldIndex, "fieldValues", fieldValues)

	// Remove filtered policies
	err := a.RemoveFilteredPolicy(sec, ptype, fieldIndex, fieldValues...)
	if err != nil {
		return false, fmt.Errorf("failed to remove filtered policies: %w", err)
	}

	// Add new rules
	err = a.AddPolicies(sec, ptype, newRules)
	if err != nil {
		return false, fmt.Errorf("failed to add new policies: %w", err)
	}

	return true, nil
}

// IsFiltered returns true if the loaded policy has been filtered
func (a *EntAdapter) IsFiltered() bool {
	return false // We load all policies, no filtering
}

// LoadFilteredPolicy loads only policy rules that match the filter
func (a *EntAdapter) LoadFilteredPolicy(model model.Model, filter interface{}) error {
	a.logger.Debugw("loading filtered policies", "filter", filter)

	ctx := context.Background()
	query := a.client.RBACPolicy.Query().Where(rbacpolicy.StatusEQ("published"))

	// Apply filters if provided
	if filterMap, ok := filter.(map[string]interface{}); ok {
		if tenantID, exists := filterMap["tenant_id"]; exists {
			if tenantStr, ok := tenantID.(string); ok {
				query = query.Where(rbacpolicy.TenantID(tenantStr))
			}
		}
		if role, exists := filterMap["role"]; exists {
			if roleStr, ok := role.(string); ok {
				query = query.Where(rbacpolicy.Role(roleStr))
			}
		}
	}

	policies, err := query.All(ctx)
	if err != nil {
		return fmt.Errorf("failed to load filtered policies: %w", err)
	}

	for _, policy := range policies {
		line := fmt.Sprintf("p, %s, %s, %s, %s, %s",
			policy.Role, policy.Resource, policy.Action, policy.TenantID, policy.Effect)

		persist.LoadPolicyLine(line, model)
	}

	a.logger.Infow("loaded filtered policies from database", "count", len(policies))
	return nil
}
