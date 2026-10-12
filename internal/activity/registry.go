package activity

import (
	"fmt"
	"slices"

	"github.com/flexprice/flexprice/internal/types"
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
	Actions  []Action
	Customer CustomerRef
}

type refKind int

const (
	refNone refKind = iota
	refSelf
	refField
	refParent
	refFirstOf
)

// CustomerRef says where an entry's customer is found; only the consumer resolves it.
type CustomerRef struct {
	kind    refKind
	column  string
	parent  types.SystemEntityType
	whenCol string
	whenVal string
	refs    []CustomerRef
}

// Self marks the row itself as the customer.
func Self() CustomerRef { return CustomerRef{kind: refSelf} }

// Field reads the customer id from a snapshot column.
func Field(col string) CustomerRef { return CustomerRef{kind: refField, column: col} }

// Parent reads a parent entity id from a snapshot column; the consumer looks up the parent's customer.
func Parent(t types.SystemEntityType, col string) CustomerRef {
	return CustomerRef{kind: refParent, parent: t, column: col}
}

func None() CustomerRef { return CustomerRef{} }

// FirstOf takes the first ref whose condition matches.
func FirstOf(refs ...CustomerRef) CustomerRef { return CustomerRef{kind: refFirstOf, refs: refs} }

// When makes the ref apply only when the snapshot column equals val.
func (r CustomerRef) When(col, val string) CustomerRef {
	r.whenCol, r.whenVal = col, val
	return r
}

// columns lists every snapshot column the ref reads.
func (r CustomerRef) columns() []string {
	var cols []string
	if r.column != "" {
		cols = append(cols, r.column)
	}
	if r.whenCol != "" {
		cols = append(cols, r.whenCol)
	}
	for _, sub := range r.refs {
		cols = append(cols, sub.columns()...)
	}
	return cols
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

// AllowedActions is created, updated, deleted plus the declared actions of t.
func (r *Registry) AllowedActions(t types.SystemEntityType) []Action {
	reg, ok := r.byType[t]
	if !ok {
		return nil
	}
	prefix := string(t) + "."
	allowed := []Action{Action(prefix + "created"), Action(prefix + "updated"), Action(prefix + "deleted")}
	for _, action := range reg.Actions {
		if !slices.Contains(allowed, action) {
			allowed = append(allowed, action)
		}
	}
	return allowed
}

// IsAction reports whether action is valid for any registered entity.
func (r *Registry) IsAction(action Action) bool {
	for t := range r.byType {
		for _, a := range r.AllowedActions(t) {
			if a == action {
				return true
			}
		}
	}
	return false
}
