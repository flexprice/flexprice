package activity

import (
	"fmt"
	"slices"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// Registration describes how one entity type is logged.
type Registration struct {
	EntityType types.SystemEntityType
	EntName    string // ent mutation Type(), e.g. "Invoice"
	// Snapshot converts a written row to its public-API shape; build it with snapshotOf.
	Snapshot Snapshot
	// Label lists snapshot keys whose first non-empty value names the entity.
	Label []string
	// Actions are the declared actions, highest priority first.
	Actions []Action

	// actions is every action of the entity, highest priority first: deleted, created, Actions, updated.
	actions []Action
}

// action names a built-in action of the entity, e.g. invoice.created.
func (r *Registration) action(verb string) Action {
	return Action(string(r.EntityType) + "." + verb)
}

// rank is the action's precedence when one transaction writes the entity more than once; 0 for an unknown action.
func (r *Registration) rank(a Action) int {
	if i := slices.Index(r.actions, a); i >= 0 {
		return len(r.actions) - i
	}
	return 0
}

// label is the first non-empty Label field of the entity's state.
func (r *Registration) label(state map[string]any) string {
	for _, key := range r.Label {
		if v := stringField(state, key); v != "" {
			return v
		}
	}
	return ""
}

// Registry holds the registrations, indexed by ent name and entity type.
type Registry struct {
	byEnt  map[string]*Registration
	byType map[types.SystemEntityType]*Registration
}

func NewRegistry(regs ...Registration) (*Registry, error) {
	r := &Registry{byEnt: map[string]*Registration{}, byType: map[types.SystemEntityType]*Registration{}}
	for i := range regs {
		reg := &regs[i]
		if reg.EntName == "" || reg.EntityType == "" || reg.Snapshot.convert == nil {
			return nil, fmt.Errorf("activity: incomplete registration for %q", reg.EntityType)
		}
		if _, dup := r.byEnt[reg.EntName]; dup {
			return nil, fmt.Errorf("activity: duplicate ent name %q", reg.EntName)
		}
		if _, dup := r.byType[reg.EntityType]; dup {
			return nil, fmt.Errorf("activity: duplicate entity type %q", reg.EntityType)
		}
		reg.actions = append([]Action{reg.action("deleted"), reg.action("created")}, reg.Actions...)
		reg.actions = lo.Uniq(append(reg.actions, reg.action("updated")))
		r.byEnt[reg.EntName] = reg
		r.byType[reg.EntityType] = reg
	}
	return r, nil
}

func (r *Registry) ByEntName(name string) (*Registration, bool) {
	reg, ok := r.byEnt[name]
	return reg, ok
}

func (r *Registry) ByType(t types.SystemEntityType) (*Registration, bool) {
	reg, ok := r.byType[t]
	return reg, ok
}

// AllowedActions lists every action of t, highest priority first.
func (r *Registry) AllowedActions(t types.SystemEntityType) []Action {
	reg, ok := r.byType[t]
	if !ok {
		return nil
	}
	return slices.Clone(reg.actions)
}

// IsAction reports whether action is valid for any registered entity.
func (r *Registry) IsAction(action Action) bool {
	for _, reg := range r.byType {
		if slices.Contains(reg.actions, action) {
			return true
		}
	}
	return false
}
