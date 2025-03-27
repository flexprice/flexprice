// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqlgraph"
	"entgo.io/ent/dialect/sql/sqljson"
	"entgo.io/ent/schema/field"
	"github.com/flexprice/flexprice/ent/integrationentity"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/types"
)

// IntegrationEntityUpdate is the builder for updating IntegrationEntity entities.
type IntegrationEntityUpdate struct {
	config
	hooks    []Hook
	mutation *IntegrationEntityMutation
}

// Where appends a list predicates to the IntegrationEntityUpdate builder.
func (ieu *IntegrationEntityUpdate) Where(ps ...predicate.IntegrationEntity) *IntegrationEntityUpdate {
	ieu.mutation.Where(ps...)
	return ieu
}

// SetStatus sets the "status" field.
func (ieu *IntegrationEntityUpdate) SetStatus(s string) *IntegrationEntityUpdate {
	ieu.mutation.SetStatus(s)
	return ieu
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (ieu *IntegrationEntityUpdate) SetNillableStatus(s *string) *IntegrationEntityUpdate {
	if s != nil {
		ieu.SetStatus(*s)
	}
	return ieu
}

// SetUpdatedAt sets the "updated_at" field.
func (ieu *IntegrationEntityUpdate) SetUpdatedAt(t time.Time) *IntegrationEntityUpdate {
	ieu.mutation.SetUpdatedAt(t)
	return ieu
}

// SetUpdatedBy sets the "updated_by" field.
func (ieu *IntegrationEntityUpdate) SetUpdatedBy(s string) *IntegrationEntityUpdate {
	ieu.mutation.SetUpdatedBy(s)
	return ieu
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (ieu *IntegrationEntityUpdate) SetNillableUpdatedBy(s *string) *IntegrationEntityUpdate {
	if s != nil {
		ieu.SetUpdatedBy(*s)
	}
	return ieu
}

// ClearUpdatedBy clears the value of the "updated_by" field.
func (ieu *IntegrationEntityUpdate) ClearUpdatedBy() *IntegrationEntityUpdate {
	ieu.mutation.ClearUpdatedBy()
	return ieu
}

// SetEntityType sets the "entity_type" field.
func (ieu *IntegrationEntityUpdate) SetEntityType(tt types.EntityType) *IntegrationEntityUpdate {
	ieu.mutation.SetEntityType(tt)
	return ieu
}

// SetNillableEntityType sets the "entity_type" field if the given value is not nil.
func (ieu *IntegrationEntityUpdate) SetNillableEntityType(tt *types.EntityType) *IntegrationEntityUpdate {
	if tt != nil {
		ieu.SetEntityType(*tt)
	}
	return ieu
}

// SetEntityID sets the "entity_id" field.
func (ieu *IntegrationEntityUpdate) SetEntityID(s string) *IntegrationEntityUpdate {
	ieu.mutation.SetEntityID(s)
	return ieu
}

// SetNillableEntityID sets the "entity_id" field if the given value is not nil.
func (ieu *IntegrationEntityUpdate) SetNillableEntityID(s *string) *IntegrationEntityUpdate {
	if s != nil {
		ieu.SetEntityID(*s)
	}
	return ieu
}

// SetProviderType sets the "provider_type" field.
func (ieu *IntegrationEntityUpdate) SetProviderType(tp types.SecretProvider) *IntegrationEntityUpdate {
	ieu.mutation.SetProviderType(tp)
	return ieu
}

// SetNillableProviderType sets the "provider_type" field if the given value is not nil.
func (ieu *IntegrationEntityUpdate) SetNillableProviderType(tp *types.SecretProvider) *IntegrationEntityUpdate {
	if tp != nil {
		ieu.SetProviderType(*tp)
	}
	return ieu
}

// SetProviderID sets the "provider_id" field.
func (ieu *IntegrationEntityUpdate) SetProviderID(s string) *IntegrationEntityUpdate {
	ieu.mutation.SetProviderID(s)
	return ieu
}

// SetNillableProviderID sets the "provider_id" field if the given value is not nil.
func (ieu *IntegrationEntityUpdate) SetNillableProviderID(s *string) *IntegrationEntityUpdate {
	if s != nil {
		ieu.SetProviderID(*s)
	}
	return ieu
}

// ClearProviderID clears the value of the "provider_id" field.
func (ieu *IntegrationEntityUpdate) ClearProviderID() *IntegrationEntityUpdate {
	ieu.mutation.ClearProviderID()
	return ieu
}

// SetSyncStatus sets the "sync_status" field.
func (ieu *IntegrationEntityUpdate) SetSyncStatus(ts types.SyncStatus) *IntegrationEntityUpdate {
	ieu.mutation.SetSyncStatus(ts)
	return ieu
}

// SetNillableSyncStatus sets the "sync_status" field if the given value is not nil.
func (ieu *IntegrationEntityUpdate) SetNillableSyncStatus(ts *types.SyncStatus) *IntegrationEntityUpdate {
	if ts != nil {
		ieu.SetSyncStatus(*ts)
	}
	return ieu
}

// SetLastSyncedAt sets the "last_synced_at" field.
func (ieu *IntegrationEntityUpdate) SetLastSyncedAt(t time.Time) *IntegrationEntityUpdate {
	ieu.mutation.SetLastSyncedAt(t)
	return ieu
}

// SetNillableLastSyncedAt sets the "last_synced_at" field if the given value is not nil.
func (ieu *IntegrationEntityUpdate) SetNillableLastSyncedAt(t *time.Time) *IntegrationEntityUpdate {
	if t != nil {
		ieu.SetLastSyncedAt(*t)
	}
	return ieu
}

// ClearLastSyncedAt clears the value of the "last_synced_at" field.
func (ieu *IntegrationEntityUpdate) ClearLastSyncedAt() *IntegrationEntityUpdate {
	ieu.mutation.ClearLastSyncedAt()
	return ieu
}

// SetLastErrorMsg sets the "last_error_msg" field.
func (ieu *IntegrationEntityUpdate) SetLastErrorMsg(s string) *IntegrationEntityUpdate {
	ieu.mutation.SetLastErrorMsg(s)
	return ieu
}

// SetNillableLastErrorMsg sets the "last_error_msg" field if the given value is not nil.
func (ieu *IntegrationEntityUpdate) SetNillableLastErrorMsg(s *string) *IntegrationEntityUpdate {
	if s != nil {
		ieu.SetLastErrorMsg(*s)
	}
	return ieu
}

// ClearLastErrorMsg clears the value of the "last_error_msg" field.
func (ieu *IntegrationEntityUpdate) ClearLastErrorMsg() *IntegrationEntityUpdate {
	ieu.mutation.ClearLastErrorMsg()
	return ieu
}

// SetSyncHistory sets the "sync_history" field.
func (ieu *IntegrationEntityUpdate) SetSyncHistory(se []schema.SyncEvent) *IntegrationEntityUpdate {
	ieu.mutation.SetSyncHistory(se)
	return ieu
}

// AppendSyncHistory appends se to the "sync_history" field.
func (ieu *IntegrationEntityUpdate) AppendSyncHistory(se []schema.SyncEvent) *IntegrationEntityUpdate {
	ieu.mutation.AppendSyncHistory(se)
	return ieu
}

// SetMetadata sets the "metadata" field.
func (ieu *IntegrationEntityUpdate) SetMetadata(m map[string]string) *IntegrationEntityUpdate {
	ieu.mutation.SetMetadata(m)
	return ieu
}

// Mutation returns the IntegrationEntityMutation object of the builder.
func (ieu *IntegrationEntityUpdate) Mutation() *IntegrationEntityMutation {
	return ieu.mutation
}

// Save executes the query and returns the number of nodes affected by the update operation.
func (ieu *IntegrationEntityUpdate) Save(ctx context.Context) (int, error) {
	ieu.defaults()
	return withHooks(ctx, ieu.sqlSave, ieu.mutation, ieu.hooks)
}

// SaveX is like Save, but panics if an error occurs.
func (ieu *IntegrationEntityUpdate) SaveX(ctx context.Context) int {
	affected, err := ieu.Save(ctx)
	if err != nil {
		panic(err)
	}
	return affected
}

// Exec executes the query.
func (ieu *IntegrationEntityUpdate) Exec(ctx context.Context) error {
	_, err := ieu.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (ieu *IntegrationEntityUpdate) ExecX(ctx context.Context) {
	if err := ieu.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (ieu *IntegrationEntityUpdate) defaults() {
	if _, ok := ieu.mutation.UpdatedAt(); !ok {
		v := integrationentity.UpdateDefaultUpdatedAt()
		ieu.mutation.SetUpdatedAt(v)
	}
}

func (ieu *IntegrationEntityUpdate) sqlSave(ctx context.Context) (n int, err error) {
	_spec := sqlgraph.NewUpdateSpec(integrationentity.Table, integrationentity.Columns, sqlgraph.NewFieldSpec(integrationentity.FieldID, field.TypeString))
	if ps := ieu.mutation.predicates; len(ps) > 0 {
		_spec.Predicate = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	if value, ok := ieu.mutation.Status(); ok {
		_spec.SetField(integrationentity.FieldStatus, field.TypeString, value)
	}
	if value, ok := ieu.mutation.UpdatedAt(); ok {
		_spec.SetField(integrationentity.FieldUpdatedAt, field.TypeTime, value)
	}
	if ieu.mutation.CreatedByCleared() {
		_spec.ClearField(integrationentity.FieldCreatedBy, field.TypeString)
	}
	if value, ok := ieu.mutation.UpdatedBy(); ok {
		_spec.SetField(integrationentity.FieldUpdatedBy, field.TypeString, value)
	}
	if ieu.mutation.UpdatedByCleared() {
		_spec.ClearField(integrationentity.FieldUpdatedBy, field.TypeString)
	}
	if ieu.mutation.EnvironmentIDCleared() {
		_spec.ClearField(integrationentity.FieldEnvironmentID, field.TypeString)
	}
	if value, ok := ieu.mutation.EntityType(); ok {
		_spec.SetField(integrationentity.FieldEntityType, field.TypeString, value)
	}
	if value, ok := ieu.mutation.EntityID(); ok {
		_spec.SetField(integrationentity.FieldEntityID, field.TypeString, value)
	}
	if value, ok := ieu.mutation.ProviderType(); ok {
		_spec.SetField(integrationentity.FieldProviderType, field.TypeString, value)
	}
	if value, ok := ieu.mutation.ProviderID(); ok {
		_spec.SetField(integrationentity.FieldProviderID, field.TypeString, value)
	}
	if ieu.mutation.ProviderIDCleared() {
		_spec.ClearField(integrationentity.FieldProviderID, field.TypeString)
	}
	if value, ok := ieu.mutation.SyncStatus(); ok {
		_spec.SetField(integrationentity.FieldSyncStatus, field.TypeString, value)
	}
	if value, ok := ieu.mutation.LastSyncedAt(); ok {
		_spec.SetField(integrationentity.FieldLastSyncedAt, field.TypeTime, value)
	}
	if ieu.mutation.LastSyncedAtCleared() {
		_spec.ClearField(integrationentity.FieldLastSyncedAt, field.TypeTime)
	}
	if value, ok := ieu.mutation.LastErrorMsg(); ok {
		_spec.SetField(integrationentity.FieldLastErrorMsg, field.TypeString, value)
	}
	if ieu.mutation.LastErrorMsgCleared() {
		_spec.ClearField(integrationentity.FieldLastErrorMsg, field.TypeString)
	}
	if value, ok := ieu.mutation.SyncHistory(); ok {
		_spec.SetField(integrationentity.FieldSyncHistory, field.TypeJSON, value)
	}
	if value, ok := ieu.mutation.AppendedSyncHistory(); ok {
		_spec.AddModifier(func(u *sql.UpdateBuilder) {
			sqljson.Append(u, integrationentity.FieldSyncHistory, value)
		})
	}
	if value, ok := ieu.mutation.Metadata(); ok {
		_spec.SetField(integrationentity.FieldMetadata, field.TypeJSON, value)
	}
	if n, err = sqlgraph.UpdateNodes(ctx, ieu.driver, _spec); err != nil {
		if _, ok := err.(*sqlgraph.NotFoundError); ok {
			err = &NotFoundError{integrationentity.Label}
		} else if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return 0, err
	}
	ieu.mutation.done = true
	return n, nil
}

// IntegrationEntityUpdateOne is the builder for updating a single IntegrationEntity entity.
type IntegrationEntityUpdateOne struct {
	config
	fields   []string
	hooks    []Hook
	mutation *IntegrationEntityMutation
}

// SetStatus sets the "status" field.
func (ieuo *IntegrationEntityUpdateOne) SetStatus(s string) *IntegrationEntityUpdateOne {
	ieuo.mutation.SetStatus(s)
	return ieuo
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (ieuo *IntegrationEntityUpdateOne) SetNillableStatus(s *string) *IntegrationEntityUpdateOne {
	if s != nil {
		ieuo.SetStatus(*s)
	}
	return ieuo
}

// SetUpdatedAt sets the "updated_at" field.
func (ieuo *IntegrationEntityUpdateOne) SetUpdatedAt(t time.Time) *IntegrationEntityUpdateOne {
	ieuo.mutation.SetUpdatedAt(t)
	return ieuo
}

// SetUpdatedBy sets the "updated_by" field.
func (ieuo *IntegrationEntityUpdateOne) SetUpdatedBy(s string) *IntegrationEntityUpdateOne {
	ieuo.mutation.SetUpdatedBy(s)
	return ieuo
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (ieuo *IntegrationEntityUpdateOne) SetNillableUpdatedBy(s *string) *IntegrationEntityUpdateOne {
	if s != nil {
		ieuo.SetUpdatedBy(*s)
	}
	return ieuo
}

// ClearUpdatedBy clears the value of the "updated_by" field.
func (ieuo *IntegrationEntityUpdateOne) ClearUpdatedBy() *IntegrationEntityUpdateOne {
	ieuo.mutation.ClearUpdatedBy()
	return ieuo
}

// SetEntityType sets the "entity_type" field.
func (ieuo *IntegrationEntityUpdateOne) SetEntityType(tt types.EntityType) *IntegrationEntityUpdateOne {
	ieuo.mutation.SetEntityType(tt)
	return ieuo
}

// SetNillableEntityType sets the "entity_type" field if the given value is not nil.
func (ieuo *IntegrationEntityUpdateOne) SetNillableEntityType(tt *types.EntityType) *IntegrationEntityUpdateOne {
	if tt != nil {
		ieuo.SetEntityType(*tt)
	}
	return ieuo
}

// SetEntityID sets the "entity_id" field.
func (ieuo *IntegrationEntityUpdateOne) SetEntityID(s string) *IntegrationEntityUpdateOne {
	ieuo.mutation.SetEntityID(s)
	return ieuo
}

// SetNillableEntityID sets the "entity_id" field if the given value is not nil.
func (ieuo *IntegrationEntityUpdateOne) SetNillableEntityID(s *string) *IntegrationEntityUpdateOne {
	if s != nil {
		ieuo.SetEntityID(*s)
	}
	return ieuo
}

// SetProviderType sets the "provider_type" field.
func (ieuo *IntegrationEntityUpdateOne) SetProviderType(tp types.SecretProvider) *IntegrationEntityUpdateOne {
	ieuo.mutation.SetProviderType(tp)
	return ieuo
}

// SetNillableProviderType sets the "provider_type" field if the given value is not nil.
func (ieuo *IntegrationEntityUpdateOne) SetNillableProviderType(tp *types.SecretProvider) *IntegrationEntityUpdateOne {
	if tp != nil {
		ieuo.SetProviderType(*tp)
	}
	return ieuo
}

// SetProviderID sets the "provider_id" field.
func (ieuo *IntegrationEntityUpdateOne) SetProviderID(s string) *IntegrationEntityUpdateOne {
	ieuo.mutation.SetProviderID(s)
	return ieuo
}

// SetNillableProviderID sets the "provider_id" field if the given value is not nil.
func (ieuo *IntegrationEntityUpdateOne) SetNillableProviderID(s *string) *IntegrationEntityUpdateOne {
	if s != nil {
		ieuo.SetProviderID(*s)
	}
	return ieuo
}

// ClearProviderID clears the value of the "provider_id" field.
func (ieuo *IntegrationEntityUpdateOne) ClearProviderID() *IntegrationEntityUpdateOne {
	ieuo.mutation.ClearProviderID()
	return ieuo
}

// SetSyncStatus sets the "sync_status" field.
func (ieuo *IntegrationEntityUpdateOne) SetSyncStatus(ts types.SyncStatus) *IntegrationEntityUpdateOne {
	ieuo.mutation.SetSyncStatus(ts)
	return ieuo
}

// SetNillableSyncStatus sets the "sync_status" field if the given value is not nil.
func (ieuo *IntegrationEntityUpdateOne) SetNillableSyncStatus(ts *types.SyncStatus) *IntegrationEntityUpdateOne {
	if ts != nil {
		ieuo.SetSyncStatus(*ts)
	}
	return ieuo
}

// SetLastSyncedAt sets the "last_synced_at" field.
func (ieuo *IntegrationEntityUpdateOne) SetLastSyncedAt(t time.Time) *IntegrationEntityUpdateOne {
	ieuo.mutation.SetLastSyncedAt(t)
	return ieuo
}

// SetNillableLastSyncedAt sets the "last_synced_at" field if the given value is not nil.
func (ieuo *IntegrationEntityUpdateOne) SetNillableLastSyncedAt(t *time.Time) *IntegrationEntityUpdateOne {
	if t != nil {
		ieuo.SetLastSyncedAt(*t)
	}
	return ieuo
}

// ClearLastSyncedAt clears the value of the "last_synced_at" field.
func (ieuo *IntegrationEntityUpdateOne) ClearLastSyncedAt() *IntegrationEntityUpdateOne {
	ieuo.mutation.ClearLastSyncedAt()
	return ieuo
}

// SetLastErrorMsg sets the "last_error_msg" field.
func (ieuo *IntegrationEntityUpdateOne) SetLastErrorMsg(s string) *IntegrationEntityUpdateOne {
	ieuo.mutation.SetLastErrorMsg(s)
	return ieuo
}

// SetNillableLastErrorMsg sets the "last_error_msg" field if the given value is not nil.
func (ieuo *IntegrationEntityUpdateOne) SetNillableLastErrorMsg(s *string) *IntegrationEntityUpdateOne {
	if s != nil {
		ieuo.SetLastErrorMsg(*s)
	}
	return ieuo
}

// ClearLastErrorMsg clears the value of the "last_error_msg" field.
func (ieuo *IntegrationEntityUpdateOne) ClearLastErrorMsg() *IntegrationEntityUpdateOne {
	ieuo.mutation.ClearLastErrorMsg()
	return ieuo
}

// SetSyncHistory sets the "sync_history" field.
func (ieuo *IntegrationEntityUpdateOne) SetSyncHistory(se []schema.SyncEvent) *IntegrationEntityUpdateOne {
	ieuo.mutation.SetSyncHistory(se)
	return ieuo
}

// AppendSyncHistory appends se to the "sync_history" field.
func (ieuo *IntegrationEntityUpdateOne) AppendSyncHistory(se []schema.SyncEvent) *IntegrationEntityUpdateOne {
	ieuo.mutation.AppendSyncHistory(se)
	return ieuo
}

// SetMetadata sets the "metadata" field.
func (ieuo *IntegrationEntityUpdateOne) SetMetadata(m map[string]string) *IntegrationEntityUpdateOne {
	ieuo.mutation.SetMetadata(m)
	return ieuo
}

// Mutation returns the IntegrationEntityMutation object of the builder.
func (ieuo *IntegrationEntityUpdateOne) Mutation() *IntegrationEntityMutation {
	return ieuo.mutation
}

// Where appends a list predicates to the IntegrationEntityUpdate builder.
func (ieuo *IntegrationEntityUpdateOne) Where(ps ...predicate.IntegrationEntity) *IntegrationEntityUpdateOne {
	ieuo.mutation.Where(ps...)
	return ieuo
}

// Select allows selecting one or more fields (columns) of the returned entity.
// The default is selecting all fields defined in the entity schema.
func (ieuo *IntegrationEntityUpdateOne) Select(field string, fields ...string) *IntegrationEntityUpdateOne {
	ieuo.fields = append([]string{field}, fields...)
	return ieuo
}

// Save executes the query and returns the updated IntegrationEntity entity.
func (ieuo *IntegrationEntityUpdateOne) Save(ctx context.Context) (*IntegrationEntity, error) {
	ieuo.defaults()
	return withHooks(ctx, ieuo.sqlSave, ieuo.mutation, ieuo.hooks)
}

// SaveX is like Save, but panics if an error occurs.
func (ieuo *IntegrationEntityUpdateOne) SaveX(ctx context.Context) *IntegrationEntity {
	node, err := ieuo.Save(ctx)
	if err != nil {
		panic(err)
	}
	return node
}

// Exec executes the query on the entity.
func (ieuo *IntegrationEntityUpdateOne) Exec(ctx context.Context) error {
	_, err := ieuo.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (ieuo *IntegrationEntityUpdateOne) ExecX(ctx context.Context) {
	if err := ieuo.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (ieuo *IntegrationEntityUpdateOne) defaults() {
	if _, ok := ieuo.mutation.UpdatedAt(); !ok {
		v := integrationentity.UpdateDefaultUpdatedAt()
		ieuo.mutation.SetUpdatedAt(v)
	}
}

func (ieuo *IntegrationEntityUpdateOne) sqlSave(ctx context.Context) (_node *IntegrationEntity, err error) {
	_spec := sqlgraph.NewUpdateSpec(integrationentity.Table, integrationentity.Columns, sqlgraph.NewFieldSpec(integrationentity.FieldID, field.TypeString))
	id, ok := ieuo.mutation.ID()
	if !ok {
		return nil, &ValidationError{Name: "id", err: errors.New(`ent: missing "IntegrationEntity.id" for update`)}
	}
	_spec.Node.ID.Value = id
	if fields := ieuo.fields; len(fields) > 0 {
		_spec.Node.Columns = make([]string, 0, len(fields))
		_spec.Node.Columns = append(_spec.Node.Columns, integrationentity.FieldID)
		for _, f := range fields {
			if !integrationentity.ValidColumn(f) {
				return nil, &ValidationError{Name: f, err: fmt.Errorf("ent: invalid field %q for query", f)}
			}
			if f != integrationentity.FieldID {
				_spec.Node.Columns = append(_spec.Node.Columns, f)
			}
		}
	}
	if ps := ieuo.mutation.predicates; len(ps) > 0 {
		_spec.Predicate = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	if value, ok := ieuo.mutation.Status(); ok {
		_spec.SetField(integrationentity.FieldStatus, field.TypeString, value)
	}
	if value, ok := ieuo.mutation.UpdatedAt(); ok {
		_spec.SetField(integrationentity.FieldUpdatedAt, field.TypeTime, value)
	}
	if ieuo.mutation.CreatedByCleared() {
		_spec.ClearField(integrationentity.FieldCreatedBy, field.TypeString)
	}
	if value, ok := ieuo.mutation.UpdatedBy(); ok {
		_spec.SetField(integrationentity.FieldUpdatedBy, field.TypeString, value)
	}
	if ieuo.mutation.UpdatedByCleared() {
		_spec.ClearField(integrationentity.FieldUpdatedBy, field.TypeString)
	}
	if ieuo.mutation.EnvironmentIDCleared() {
		_spec.ClearField(integrationentity.FieldEnvironmentID, field.TypeString)
	}
	if value, ok := ieuo.mutation.EntityType(); ok {
		_spec.SetField(integrationentity.FieldEntityType, field.TypeString, value)
	}
	if value, ok := ieuo.mutation.EntityID(); ok {
		_spec.SetField(integrationentity.FieldEntityID, field.TypeString, value)
	}
	if value, ok := ieuo.mutation.ProviderType(); ok {
		_spec.SetField(integrationentity.FieldProviderType, field.TypeString, value)
	}
	if value, ok := ieuo.mutation.ProviderID(); ok {
		_spec.SetField(integrationentity.FieldProviderID, field.TypeString, value)
	}
	if ieuo.mutation.ProviderIDCleared() {
		_spec.ClearField(integrationentity.FieldProviderID, field.TypeString)
	}
	if value, ok := ieuo.mutation.SyncStatus(); ok {
		_spec.SetField(integrationentity.FieldSyncStatus, field.TypeString, value)
	}
	if value, ok := ieuo.mutation.LastSyncedAt(); ok {
		_spec.SetField(integrationentity.FieldLastSyncedAt, field.TypeTime, value)
	}
	if ieuo.mutation.LastSyncedAtCleared() {
		_spec.ClearField(integrationentity.FieldLastSyncedAt, field.TypeTime)
	}
	if value, ok := ieuo.mutation.LastErrorMsg(); ok {
		_spec.SetField(integrationentity.FieldLastErrorMsg, field.TypeString, value)
	}
	if ieuo.mutation.LastErrorMsgCleared() {
		_spec.ClearField(integrationentity.FieldLastErrorMsg, field.TypeString)
	}
	if value, ok := ieuo.mutation.SyncHistory(); ok {
		_spec.SetField(integrationentity.FieldSyncHistory, field.TypeJSON, value)
	}
	if value, ok := ieuo.mutation.AppendedSyncHistory(); ok {
		_spec.AddModifier(func(u *sql.UpdateBuilder) {
			sqljson.Append(u, integrationentity.FieldSyncHistory, value)
		})
	}
	if value, ok := ieuo.mutation.Metadata(); ok {
		_spec.SetField(integrationentity.FieldMetadata, field.TypeJSON, value)
	}
	_node = &IntegrationEntity{config: ieuo.config}
	_spec.Assign = _node.assignValues
	_spec.ScanValues = _node.scanValues
	if err = sqlgraph.UpdateNode(ctx, ieuo.driver, _spec); err != nil {
		if _, ok := err.(*sqlgraph.NotFoundError); ok {
			err = &NotFoundError{integrationentity.Label}
		} else if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	ieuo.mutation.done = true
	return _node, nil
}
