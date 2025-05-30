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
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/secret"
)

// SecretUpdate is the builder for updating Secret entities.
type SecretUpdate struct {
	config
	hooks    []Hook
	mutation *SecretMutation
}

// Where appends a list predicates to the SecretUpdate builder.
func (su *SecretUpdate) Where(ps ...predicate.Secret) *SecretUpdate {
	su.mutation.Where(ps...)
	return su
}

// SetStatus sets the "status" field.
func (su *SecretUpdate) SetStatus(s string) *SecretUpdate {
	su.mutation.SetStatus(s)
	return su
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (su *SecretUpdate) SetNillableStatus(s *string) *SecretUpdate {
	if s != nil {
		su.SetStatus(*s)
	}
	return su
}

// SetUpdatedAt sets the "updated_at" field.
func (su *SecretUpdate) SetUpdatedAt(t time.Time) *SecretUpdate {
	su.mutation.SetUpdatedAt(t)
	return su
}

// SetUpdatedBy sets the "updated_by" field.
func (su *SecretUpdate) SetUpdatedBy(s string) *SecretUpdate {
	su.mutation.SetUpdatedBy(s)
	return su
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (su *SecretUpdate) SetNillableUpdatedBy(s *string) *SecretUpdate {
	if s != nil {
		su.SetUpdatedBy(*s)
	}
	return su
}

// ClearUpdatedBy clears the value of the "updated_by" field.
func (su *SecretUpdate) ClearUpdatedBy() *SecretUpdate {
	su.mutation.ClearUpdatedBy()
	return su
}

// SetName sets the "name" field.
func (su *SecretUpdate) SetName(s string) *SecretUpdate {
	su.mutation.SetName(s)
	return su
}

// SetNillableName sets the "name" field if the given value is not nil.
func (su *SecretUpdate) SetNillableName(s *string) *SecretUpdate {
	if s != nil {
		su.SetName(*s)
	}
	return su
}

// SetType sets the "type" field.
func (su *SecretUpdate) SetType(s string) *SecretUpdate {
	su.mutation.SetType(s)
	return su
}

// SetNillableType sets the "type" field if the given value is not nil.
func (su *SecretUpdate) SetNillableType(s *string) *SecretUpdate {
	if s != nil {
		su.SetType(*s)
	}
	return su
}

// SetProvider sets the "provider" field.
func (su *SecretUpdate) SetProvider(s string) *SecretUpdate {
	su.mutation.SetProvider(s)
	return su
}

// SetNillableProvider sets the "provider" field if the given value is not nil.
func (su *SecretUpdate) SetNillableProvider(s *string) *SecretUpdate {
	if s != nil {
		su.SetProvider(*s)
	}
	return su
}

// SetValue sets the "value" field.
func (su *SecretUpdate) SetValue(s string) *SecretUpdate {
	su.mutation.SetValue(s)
	return su
}

// SetNillableValue sets the "value" field if the given value is not nil.
func (su *SecretUpdate) SetNillableValue(s *string) *SecretUpdate {
	if s != nil {
		su.SetValue(*s)
	}
	return su
}

// ClearValue clears the value of the "value" field.
func (su *SecretUpdate) ClearValue() *SecretUpdate {
	su.mutation.ClearValue()
	return su
}

// SetDisplayID sets the "display_id" field.
func (su *SecretUpdate) SetDisplayID(s string) *SecretUpdate {
	su.mutation.SetDisplayID(s)
	return su
}

// SetNillableDisplayID sets the "display_id" field if the given value is not nil.
func (su *SecretUpdate) SetNillableDisplayID(s *string) *SecretUpdate {
	if s != nil {
		su.SetDisplayID(*s)
	}
	return su
}

// ClearDisplayID clears the value of the "display_id" field.
func (su *SecretUpdate) ClearDisplayID() *SecretUpdate {
	su.mutation.ClearDisplayID()
	return su
}

// SetPermissions sets the "permissions" field.
func (su *SecretUpdate) SetPermissions(s []string) *SecretUpdate {
	su.mutation.SetPermissions(s)
	return su
}

// AppendPermissions appends s to the "permissions" field.
func (su *SecretUpdate) AppendPermissions(s []string) *SecretUpdate {
	su.mutation.AppendPermissions(s)
	return su
}

// ClearPermissions clears the value of the "permissions" field.
func (su *SecretUpdate) ClearPermissions() *SecretUpdate {
	su.mutation.ClearPermissions()
	return su
}

// SetExpiresAt sets the "expires_at" field.
func (su *SecretUpdate) SetExpiresAt(t time.Time) *SecretUpdate {
	su.mutation.SetExpiresAt(t)
	return su
}

// SetNillableExpiresAt sets the "expires_at" field if the given value is not nil.
func (su *SecretUpdate) SetNillableExpiresAt(t *time.Time) *SecretUpdate {
	if t != nil {
		su.SetExpiresAt(*t)
	}
	return su
}

// ClearExpiresAt clears the value of the "expires_at" field.
func (su *SecretUpdate) ClearExpiresAt() *SecretUpdate {
	su.mutation.ClearExpiresAt()
	return su
}

// SetLastUsedAt sets the "last_used_at" field.
func (su *SecretUpdate) SetLastUsedAt(t time.Time) *SecretUpdate {
	su.mutation.SetLastUsedAt(t)
	return su
}

// SetNillableLastUsedAt sets the "last_used_at" field if the given value is not nil.
func (su *SecretUpdate) SetNillableLastUsedAt(t *time.Time) *SecretUpdate {
	if t != nil {
		su.SetLastUsedAt(*t)
	}
	return su
}

// ClearLastUsedAt clears the value of the "last_used_at" field.
func (su *SecretUpdate) ClearLastUsedAt() *SecretUpdate {
	su.mutation.ClearLastUsedAt()
	return su
}

// SetProviderData sets the "provider_data" field.
func (su *SecretUpdate) SetProviderData(m map[string]string) *SecretUpdate {
	su.mutation.SetProviderData(m)
	return su
}

// ClearProviderData clears the value of the "provider_data" field.
func (su *SecretUpdate) ClearProviderData() *SecretUpdate {
	su.mutation.ClearProviderData()
	return su
}

// Mutation returns the SecretMutation object of the builder.
func (su *SecretUpdate) Mutation() *SecretMutation {
	return su.mutation
}

// Save executes the query and returns the number of nodes affected by the update operation.
func (su *SecretUpdate) Save(ctx context.Context) (int, error) {
	su.defaults()
	return withHooks(ctx, su.sqlSave, su.mutation, su.hooks)
}

// SaveX is like Save, but panics if an error occurs.
func (su *SecretUpdate) SaveX(ctx context.Context) int {
	affected, err := su.Save(ctx)
	if err != nil {
		panic(err)
	}
	return affected
}

// Exec executes the query.
func (su *SecretUpdate) Exec(ctx context.Context) error {
	_, err := su.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (su *SecretUpdate) ExecX(ctx context.Context) {
	if err := su.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (su *SecretUpdate) defaults() {
	if _, ok := su.mutation.UpdatedAt(); !ok {
		v := secret.UpdateDefaultUpdatedAt()
		su.mutation.SetUpdatedAt(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (su *SecretUpdate) check() error {
	if v, ok := su.mutation.Name(); ok {
		if err := secret.NameValidator(v); err != nil {
			return &ValidationError{Name: "name", err: fmt.Errorf(`ent: validator failed for field "Secret.name": %w`, err)}
		}
	}
	if v, ok := su.mutation.GetType(); ok {
		if err := secret.TypeValidator(v); err != nil {
			return &ValidationError{Name: "type", err: fmt.Errorf(`ent: validator failed for field "Secret.type": %w`, err)}
		}
	}
	if v, ok := su.mutation.Provider(); ok {
		if err := secret.ProviderValidator(v); err != nil {
			return &ValidationError{Name: "provider", err: fmt.Errorf(`ent: validator failed for field "Secret.provider": %w`, err)}
		}
	}
	return nil
}

func (su *SecretUpdate) sqlSave(ctx context.Context) (n int, err error) {
	if err := su.check(); err != nil {
		return n, err
	}
	_spec := sqlgraph.NewUpdateSpec(secret.Table, secret.Columns, sqlgraph.NewFieldSpec(secret.FieldID, field.TypeString))
	if ps := su.mutation.predicates; len(ps) > 0 {
		_spec.Predicate = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	if value, ok := su.mutation.Status(); ok {
		_spec.SetField(secret.FieldStatus, field.TypeString, value)
	}
	if value, ok := su.mutation.UpdatedAt(); ok {
		_spec.SetField(secret.FieldUpdatedAt, field.TypeTime, value)
	}
	if su.mutation.CreatedByCleared() {
		_spec.ClearField(secret.FieldCreatedBy, field.TypeString)
	}
	if value, ok := su.mutation.UpdatedBy(); ok {
		_spec.SetField(secret.FieldUpdatedBy, field.TypeString, value)
	}
	if su.mutation.UpdatedByCleared() {
		_spec.ClearField(secret.FieldUpdatedBy, field.TypeString)
	}
	if su.mutation.EnvironmentIDCleared() {
		_spec.ClearField(secret.FieldEnvironmentID, field.TypeString)
	}
	if value, ok := su.mutation.Name(); ok {
		_spec.SetField(secret.FieldName, field.TypeString, value)
	}
	if value, ok := su.mutation.GetType(); ok {
		_spec.SetField(secret.FieldType, field.TypeString, value)
	}
	if value, ok := su.mutation.Provider(); ok {
		_spec.SetField(secret.FieldProvider, field.TypeString, value)
	}
	if value, ok := su.mutation.Value(); ok {
		_spec.SetField(secret.FieldValue, field.TypeString, value)
	}
	if su.mutation.ValueCleared() {
		_spec.ClearField(secret.FieldValue, field.TypeString)
	}
	if value, ok := su.mutation.DisplayID(); ok {
		_spec.SetField(secret.FieldDisplayID, field.TypeString, value)
	}
	if su.mutation.DisplayIDCleared() {
		_spec.ClearField(secret.FieldDisplayID, field.TypeString)
	}
	if value, ok := su.mutation.Permissions(); ok {
		_spec.SetField(secret.FieldPermissions, field.TypeJSON, value)
	}
	if value, ok := su.mutation.AppendedPermissions(); ok {
		_spec.AddModifier(func(u *sql.UpdateBuilder) {
			sqljson.Append(u, secret.FieldPermissions, value)
		})
	}
	if su.mutation.PermissionsCleared() {
		_spec.ClearField(secret.FieldPermissions, field.TypeJSON)
	}
	if value, ok := su.mutation.ExpiresAt(); ok {
		_spec.SetField(secret.FieldExpiresAt, field.TypeTime, value)
	}
	if su.mutation.ExpiresAtCleared() {
		_spec.ClearField(secret.FieldExpiresAt, field.TypeTime)
	}
	if value, ok := su.mutation.LastUsedAt(); ok {
		_spec.SetField(secret.FieldLastUsedAt, field.TypeTime, value)
	}
	if su.mutation.LastUsedAtCleared() {
		_spec.ClearField(secret.FieldLastUsedAt, field.TypeTime)
	}
	if value, ok := su.mutation.ProviderData(); ok {
		_spec.SetField(secret.FieldProviderData, field.TypeJSON, value)
	}
	if su.mutation.ProviderDataCleared() {
		_spec.ClearField(secret.FieldProviderData, field.TypeJSON)
	}
	if n, err = sqlgraph.UpdateNodes(ctx, su.driver, _spec); err != nil {
		if _, ok := err.(*sqlgraph.NotFoundError); ok {
			err = &NotFoundError{secret.Label}
		} else if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return 0, err
	}
	su.mutation.done = true
	return n, nil
}

// SecretUpdateOne is the builder for updating a single Secret entity.
type SecretUpdateOne struct {
	config
	fields   []string
	hooks    []Hook
	mutation *SecretMutation
}

// SetStatus sets the "status" field.
func (suo *SecretUpdateOne) SetStatus(s string) *SecretUpdateOne {
	suo.mutation.SetStatus(s)
	return suo
}

// SetNillableStatus sets the "status" field if the given value is not nil.
func (suo *SecretUpdateOne) SetNillableStatus(s *string) *SecretUpdateOne {
	if s != nil {
		suo.SetStatus(*s)
	}
	return suo
}

// SetUpdatedAt sets the "updated_at" field.
func (suo *SecretUpdateOne) SetUpdatedAt(t time.Time) *SecretUpdateOne {
	suo.mutation.SetUpdatedAt(t)
	return suo
}

// SetUpdatedBy sets the "updated_by" field.
func (suo *SecretUpdateOne) SetUpdatedBy(s string) *SecretUpdateOne {
	suo.mutation.SetUpdatedBy(s)
	return suo
}

// SetNillableUpdatedBy sets the "updated_by" field if the given value is not nil.
func (suo *SecretUpdateOne) SetNillableUpdatedBy(s *string) *SecretUpdateOne {
	if s != nil {
		suo.SetUpdatedBy(*s)
	}
	return suo
}

// ClearUpdatedBy clears the value of the "updated_by" field.
func (suo *SecretUpdateOne) ClearUpdatedBy() *SecretUpdateOne {
	suo.mutation.ClearUpdatedBy()
	return suo
}

// SetName sets the "name" field.
func (suo *SecretUpdateOne) SetName(s string) *SecretUpdateOne {
	suo.mutation.SetName(s)
	return suo
}

// SetNillableName sets the "name" field if the given value is not nil.
func (suo *SecretUpdateOne) SetNillableName(s *string) *SecretUpdateOne {
	if s != nil {
		suo.SetName(*s)
	}
	return suo
}

// SetType sets the "type" field.
func (suo *SecretUpdateOne) SetType(s string) *SecretUpdateOne {
	suo.mutation.SetType(s)
	return suo
}

// SetNillableType sets the "type" field if the given value is not nil.
func (suo *SecretUpdateOne) SetNillableType(s *string) *SecretUpdateOne {
	if s != nil {
		suo.SetType(*s)
	}
	return suo
}

// SetProvider sets the "provider" field.
func (suo *SecretUpdateOne) SetProvider(s string) *SecretUpdateOne {
	suo.mutation.SetProvider(s)
	return suo
}

// SetNillableProvider sets the "provider" field if the given value is not nil.
func (suo *SecretUpdateOne) SetNillableProvider(s *string) *SecretUpdateOne {
	if s != nil {
		suo.SetProvider(*s)
	}
	return suo
}

// SetValue sets the "value" field.
func (suo *SecretUpdateOne) SetValue(s string) *SecretUpdateOne {
	suo.mutation.SetValue(s)
	return suo
}

// SetNillableValue sets the "value" field if the given value is not nil.
func (suo *SecretUpdateOne) SetNillableValue(s *string) *SecretUpdateOne {
	if s != nil {
		suo.SetValue(*s)
	}
	return suo
}

// ClearValue clears the value of the "value" field.
func (suo *SecretUpdateOne) ClearValue() *SecretUpdateOne {
	suo.mutation.ClearValue()
	return suo
}

// SetDisplayID sets the "display_id" field.
func (suo *SecretUpdateOne) SetDisplayID(s string) *SecretUpdateOne {
	suo.mutation.SetDisplayID(s)
	return suo
}

// SetNillableDisplayID sets the "display_id" field if the given value is not nil.
func (suo *SecretUpdateOne) SetNillableDisplayID(s *string) *SecretUpdateOne {
	if s != nil {
		suo.SetDisplayID(*s)
	}
	return suo
}

// ClearDisplayID clears the value of the "display_id" field.
func (suo *SecretUpdateOne) ClearDisplayID() *SecretUpdateOne {
	suo.mutation.ClearDisplayID()
	return suo
}

// SetPermissions sets the "permissions" field.
func (suo *SecretUpdateOne) SetPermissions(s []string) *SecretUpdateOne {
	suo.mutation.SetPermissions(s)
	return suo
}

// AppendPermissions appends s to the "permissions" field.
func (suo *SecretUpdateOne) AppendPermissions(s []string) *SecretUpdateOne {
	suo.mutation.AppendPermissions(s)
	return suo
}

// ClearPermissions clears the value of the "permissions" field.
func (suo *SecretUpdateOne) ClearPermissions() *SecretUpdateOne {
	suo.mutation.ClearPermissions()
	return suo
}

// SetExpiresAt sets the "expires_at" field.
func (suo *SecretUpdateOne) SetExpiresAt(t time.Time) *SecretUpdateOne {
	suo.mutation.SetExpiresAt(t)
	return suo
}

// SetNillableExpiresAt sets the "expires_at" field if the given value is not nil.
func (suo *SecretUpdateOne) SetNillableExpiresAt(t *time.Time) *SecretUpdateOne {
	if t != nil {
		suo.SetExpiresAt(*t)
	}
	return suo
}

// ClearExpiresAt clears the value of the "expires_at" field.
func (suo *SecretUpdateOne) ClearExpiresAt() *SecretUpdateOne {
	suo.mutation.ClearExpiresAt()
	return suo
}

// SetLastUsedAt sets the "last_used_at" field.
func (suo *SecretUpdateOne) SetLastUsedAt(t time.Time) *SecretUpdateOne {
	suo.mutation.SetLastUsedAt(t)
	return suo
}

// SetNillableLastUsedAt sets the "last_used_at" field if the given value is not nil.
func (suo *SecretUpdateOne) SetNillableLastUsedAt(t *time.Time) *SecretUpdateOne {
	if t != nil {
		suo.SetLastUsedAt(*t)
	}
	return suo
}

// ClearLastUsedAt clears the value of the "last_used_at" field.
func (suo *SecretUpdateOne) ClearLastUsedAt() *SecretUpdateOne {
	suo.mutation.ClearLastUsedAt()
	return suo
}

// SetProviderData sets the "provider_data" field.
func (suo *SecretUpdateOne) SetProviderData(m map[string]string) *SecretUpdateOne {
	suo.mutation.SetProviderData(m)
	return suo
}

// ClearProviderData clears the value of the "provider_data" field.
func (suo *SecretUpdateOne) ClearProviderData() *SecretUpdateOne {
	suo.mutation.ClearProviderData()
	return suo
}

// Mutation returns the SecretMutation object of the builder.
func (suo *SecretUpdateOne) Mutation() *SecretMutation {
	return suo.mutation
}

// Where appends a list predicates to the SecretUpdate builder.
func (suo *SecretUpdateOne) Where(ps ...predicate.Secret) *SecretUpdateOne {
	suo.mutation.Where(ps...)
	return suo
}

// Select allows selecting one or more fields (columns) of the returned entity.
// The default is selecting all fields defined in the entity schema.
func (suo *SecretUpdateOne) Select(field string, fields ...string) *SecretUpdateOne {
	suo.fields = append([]string{field}, fields...)
	return suo
}

// Save executes the query and returns the updated Secret entity.
func (suo *SecretUpdateOne) Save(ctx context.Context) (*Secret, error) {
	suo.defaults()
	return withHooks(ctx, suo.sqlSave, suo.mutation, suo.hooks)
}

// SaveX is like Save, but panics if an error occurs.
func (suo *SecretUpdateOne) SaveX(ctx context.Context) *Secret {
	node, err := suo.Save(ctx)
	if err != nil {
		panic(err)
	}
	return node
}

// Exec executes the query on the entity.
func (suo *SecretUpdateOne) Exec(ctx context.Context) error {
	_, err := suo.Save(ctx)
	return err
}

// ExecX is like Exec, but panics if an error occurs.
func (suo *SecretUpdateOne) ExecX(ctx context.Context) {
	if err := suo.Exec(ctx); err != nil {
		panic(err)
	}
}

// defaults sets the default values of the builder before save.
func (suo *SecretUpdateOne) defaults() {
	if _, ok := suo.mutation.UpdatedAt(); !ok {
		v := secret.UpdateDefaultUpdatedAt()
		suo.mutation.SetUpdatedAt(v)
	}
}

// check runs all checks and user-defined validators on the builder.
func (suo *SecretUpdateOne) check() error {
	if v, ok := suo.mutation.Name(); ok {
		if err := secret.NameValidator(v); err != nil {
			return &ValidationError{Name: "name", err: fmt.Errorf(`ent: validator failed for field "Secret.name": %w`, err)}
		}
	}
	if v, ok := suo.mutation.GetType(); ok {
		if err := secret.TypeValidator(v); err != nil {
			return &ValidationError{Name: "type", err: fmt.Errorf(`ent: validator failed for field "Secret.type": %w`, err)}
		}
	}
	if v, ok := suo.mutation.Provider(); ok {
		if err := secret.ProviderValidator(v); err != nil {
			return &ValidationError{Name: "provider", err: fmt.Errorf(`ent: validator failed for field "Secret.provider": %w`, err)}
		}
	}
	return nil
}

func (suo *SecretUpdateOne) sqlSave(ctx context.Context) (_node *Secret, err error) {
	if err := suo.check(); err != nil {
		return _node, err
	}
	_spec := sqlgraph.NewUpdateSpec(secret.Table, secret.Columns, sqlgraph.NewFieldSpec(secret.FieldID, field.TypeString))
	id, ok := suo.mutation.ID()
	if !ok {
		return nil, &ValidationError{Name: "id", err: errors.New(`ent: missing "Secret.id" for update`)}
	}
	_spec.Node.ID.Value = id
	if fields := suo.fields; len(fields) > 0 {
		_spec.Node.Columns = make([]string, 0, len(fields))
		_spec.Node.Columns = append(_spec.Node.Columns, secret.FieldID)
		for _, f := range fields {
			if !secret.ValidColumn(f) {
				return nil, &ValidationError{Name: f, err: fmt.Errorf("ent: invalid field %q for query", f)}
			}
			if f != secret.FieldID {
				_spec.Node.Columns = append(_spec.Node.Columns, f)
			}
		}
	}
	if ps := suo.mutation.predicates; len(ps) > 0 {
		_spec.Predicate = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	if value, ok := suo.mutation.Status(); ok {
		_spec.SetField(secret.FieldStatus, field.TypeString, value)
	}
	if value, ok := suo.mutation.UpdatedAt(); ok {
		_spec.SetField(secret.FieldUpdatedAt, field.TypeTime, value)
	}
	if suo.mutation.CreatedByCleared() {
		_spec.ClearField(secret.FieldCreatedBy, field.TypeString)
	}
	if value, ok := suo.mutation.UpdatedBy(); ok {
		_spec.SetField(secret.FieldUpdatedBy, field.TypeString, value)
	}
	if suo.mutation.UpdatedByCleared() {
		_spec.ClearField(secret.FieldUpdatedBy, field.TypeString)
	}
	if suo.mutation.EnvironmentIDCleared() {
		_spec.ClearField(secret.FieldEnvironmentID, field.TypeString)
	}
	if value, ok := suo.mutation.Name(); ok {
		_spec.SetField(secret.FieldName, field.TypeString, value)
	}
	if value, ok := suo.mutation.GetType(); ok {
		_spec.SetField(secret.FieldType, field.TypeString, value)
	}
	if value, ok := suo.mutation.Provider(); ok {
		_spec.SetField(secret.FieldProvider, field.TypeString, value)
	}
	if value, ok := suo.mutation.Value(); ok {
		_spec.SetField(secret.FieldValue, field.TypeString, value)
	}
	if suo.mutation.ValueCleared() {
		_spec.ClearField(secret.FieldValue, field.TypeString)
	}
	if value, ok := suo.mutation.DisplayID(); ok {
		_spec.SetField(secret.FieldDisplayID, field.TypeString, value)
	}
	if suo.mutation.DisplayIDCleared() {
		_spec.ClearField(secret.FieldDisplayID, field.TypeString)
	}
	if value, ok := suo.mutation.Permissions(); ok {
		_spec.SetField(secret.FieldPermissions, field.TypeJSON, value)
	}
	if value, ok := suo.mutation.AppendedPermissions(); ok {
		_spec.AddModifier(func(u *sql.UpdateBuilder) {
			sqljson.Append(u, secret.FieldPermissions, value)
		})
	}
	if suo.mutation.PermissionsCleared() {
		_spec.ClearField(secret.FieldPermissions, field.TypeJSON)
	}
	if value, ok := suo.mutation.ExpiresAt(); ok {
		_spec.SetField(secret.FieldExpiresAt, field.TypeTime, value)
	}
	if suo.mutation.ExpiresAtCleared() {
		_spec.ClearField(secret.FieldExpiresAt, field.TypeTime)
	}
	if value, ok := suo.mutation.LastUsedAt(); ok {
		_spec.SetField(secret.FieldLastUsedAt, field.TypeTime, value)
	}
	if suo.mutation.LastUsedAtCleared() {
		_spec.ClearField(secret.FieldLastUsedAt, field.TypeTime)
	}
	if value, ok := suo.mutation.ProviderData(); ok {
		_spec.SetField(secret.FieldProviderData, field.TypeJSON, value)
	}
	if suo.mutation.ProviderDataCleared() {
		_spec.ClearField(secret.FieldProviderData, field.TypeJSON)
	}
	_node = &Secret{config: suo.config}
	_spec.Assign = _node.assignValues
	_spec.ScanValues = _node.scanValues
	if err = sqlgraph.UpdateNode(ctx, suo.driver, _spec); err != nil {
		if _, ok := err.(*sqlgraph.NotFoundError); ok {
			err = &NotFoundError{secret.Label}
		} else if sqlgraph.IsConstraintError(err) {
			err = &ConstraintError{msg: err.Error(), wrap: err}
		}
		return nil, err
	}
	suo.mutation.done = true
	return _node, nil
}
