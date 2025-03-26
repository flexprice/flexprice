// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"fmt"
	"math"

	"entgo.io/ent"
	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqlgraph"
	"entgo.io/ent/schema/field"
	"github.com/flexprice/flexprice/ent/integrationentity"
	"github.com/flexprice/flexprice/ent/predicate"
)

// IntegrationEntityQuery is the builder for querying IntegrationEntity entities.
type IntegrationEntityQuery struct {
	config
	ctx        *QueryContext
	order      []integrationentity.OrderOption
	inters     []Interceptor
	predicates []predicate.IntegrationEntity
	// intermediate query (i.e. traversal path).
	sql  *sql.Selector
	path func(context.Context) (*sql.Selector, error)
}

// Where adds a new predicate for the IntegrationEntityQuery builder.
func (ieq *IntegrationEntityQuery) Where(ps ...predicate.IntegrationEntity) *IntegrationEntityQuery {
	ieq.predicates = append(ieq.predicates, ps...)
	return ieq
}

// Limit the number of records to be returned by this query.
func (ieq *IntegrationEntityQuery) Limit(limit int) *IntegrationEntityQuery {
	ieq.ctx.Limit = &limit
	return ieq
}

// Offset to start from.
func (ieq *IntegrationEntityQuery) Offset(offset int) *IntegrationEntityQuery {
	ieq.ctx.Offset = &offset
	return ieq
}

// Unique configures the query builder to filter duplicate records on query.
// By default, unique is set to true, and can be disabled using this method.
func (ieq *IntegrationEntityQuery) Unique(unique bool) *IntegrationEntityQuery {
	ieq.ctx.Unique = &unique
	return ieq
}

// Order specifies how the records should be ordered.
func (ieq *IntegrationEntityQuery) Order(o ...integrationentity.OrderOption) *IntegrationEntityQuery {
	ieq.order = append(ieq.order, o...)
	return ieq
}

// First returns the first IntegrationEntity entity from the query.
// Returns a *NotFoundError when no IntegrationEntity was found.
func (ieq *IntegrationEntityQuery) First(ctx context.Context) (*IntegrationEntity, error) {
	nodes, err := ieq.Limit(1).All(setContextOp(ctx, ieq.ctx, ent.OpQueryFirst))
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, &NotFoundError{integrationentity.Label}
	}
	return nodes[0], nil
}

// FirstX is like First, but panics if an error occurs.
func (ieq *IntegrationEntityQuery) FirstX(ctx context.Context) *IntegrationEntity {
	node, err := ieq.First(ctx)
	if err != nil && !IsNotFound(err) {
		panic(err)
	}
	return node
}

// FirstID returns the first IntegrationEntity ID from the query.
// Returns a *NotFoundError when no IntegrationEntity ID was found.
func (ieq *IntegrationEntityQuery) FirstID(ctx context.Context) (id string, err error) {
	var ids []string
	if ids, err = ieq.Limit(1).IDs(setContextOp(ctx, ieq.ctx, ent.OpQueryFirstID)); err != nil {
		return
	}
	if len(ids) == 0 {
		err = &NotFoundError{integrationentity.Label}
		return
	}
	return ids[0], nil
}

// FirstIDX is like FirstID, but panics if an error occurs.
func (ieq *IntegrationEntityQuery) FirstIDX(ctx context.Context) string {
	id, err := ieq.FirstID(ctx)
	if err != nil && !IsNotFound(err) {
		panic(err)
	}
	return id
}

// Only returns a single IntegrationEntity entity found by the query, ensuring it only returns one.
// Returns a *NotSingularError when more than one IntegrationEntity entity is found.
// Returns a *NotFoundError when no IntegrationEntity entities are found.
func (ieq *IntegrationEntityQuery) Only(ctx context.Context) (*IntegrationEntity, error) {
	nodes, err := ieq.Limit(2).All(setContextOp(ctx, ieq.ctx, ent.OpQueryOnly))
	if err != nil {
		return nil, err
	}
	switch len(nodes) {
	case 1:
		return nodes[0], nil
	case 0:
		return nil, &NotFoundError{integrationentity.Label}
	default:
		return nil, &NotSingularError{integrationentity.Label}
	}
}

// OnlyX is like Only, but panics if an error occurs.
func (ieq *IntegrationEntityQuery) OnlyX(ctx context.Context) *IntegrationEntity {
	node, err := ieq.Only(ctx)
	if err != nil {
		panic(err)
	}
	return node
}

// OnlyID is like Only, but returns the only IntegrationEntity ID in the query.
// Returns a *NotSingularError when more than one IntegrationEntity ID is found.
// Returns a *NotFoundError when no entities are found.
func (ieq *IntegrationEntityQuery) OnlyID(ctx context.Context) (id string, err error) {
	var ids []string
	if ids, err = ieq.Limit(2).IDs(setContextOp(ctx, ieq.ctx, ent.OpQueryOnlyID)); err != nil {
		return
	}
	switch len(ids) {
	case 1:
		id = ids[0]
	case 0:
		err = &NotFoundError{integrationentity.Label}
	default:
		err = &NotSingularError{integrationentity.Label}
	}
	return
}

// OnlyIDX is like OnlyID, but panics if an error occurs.
func (ieq *IntegrationEntityQuery) OnlyIDX(ctx context.Context) string {
	id, err := ieq.OnlyID(ctx)
	if err != nil {
		panic(err)
	}
	return id
}

// All executes the query and returns a list of IntegrationEntities.
func (ieq *IntegrationEntityQuery) All(ctx context.Context) ([]*IntegrationEntity, error) {
	ctx = setContextOp(ctx, ieq.ctx, ent.OpQueryAll)
	if err := ieq.prepareQuery(ctx); err != nil {
		return nil, err
	}
	qr := querierAll[[]*IntegrationEntity, *IntegrationEntityQuery]()
	return withInterceptors[[]*IntegrationEntity](ctx, ieq, qr, ieq.inters)
}

// AllX is like All, but panics if an error occurs.
func (ieq *IntegrationEntityQuery) AllX(ctx context.Context) []*IntegrationEntity {
	nodes, err := ieq.All(ctx)
	if err != nil {
		panic(err)
	}
	return nodes
}

// IDs executes the query and returns a list of IntegrationEntity IDs.
func (ieq *IntegrationEntityQuery) IDs(ctx context.Context) (ids []string, err error) {
	if ieq.ctx.Unique == nil && ieq.path != nil {
		ieq.Unique(true)
	}
	ctx = setContextOp(ctx, ieq.ctx, ent.OpQueryIDs)
	if err = ieq.Select(integrationentity.FieldID).Scan(ctx, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// IDsX is like IDs, but panics if an error occurs.
func (ieq *IntegrationEntityQuery) IDsX(ctx context.Context) []string {
	ids, err := ieq.IDs(ctx)
	if err != nil {
		panic(err)
	}
	return ids
}

// Count returns the count of the given query.
func (ieq *IntegrationEntityQuery) Count(ctx context.Context) (int, error) {
	ctx = setContextOp(ctx, ieq.ctx, ent.OpQueryCount)
	if err := ieq.prepareQuery(ctx); err != nil {
		return 0, err
	}
	return withInterceptors[int](ctx, ieq, querierCount[*IntegrationEntityQuery](), ieq.inters)
}

// CountX is like Count, but panics if an error occurs.
func (ieq *IntegrationEntityQuery) CountX(ctx context.Context) int {
	count, err := ieq.Count(ctx)
	if err != nil {
		panic(err)
	}
	return count
}

// Exist returns true if the query has elements in the graph.
func (ieq *IntegrationEntityQuery) Exist(ctx context.Context) (bool, error) {
	ctx = setContextOp(ctx, ieq.ctx, ent.OpQueryExist)
	switch _, err := ieq.FirstID(ctx); {
	case IsNotFound(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("ent: check existence: %w", err)
	default:
		return true, nil
	}
}

// ExistX is like Exist, but panics if an error occurs.
func (ieq *IntegrationEntityQuery) ExistX(ctx context.Context) bool {
	exist, err := ieq.Exist(ctx)
	if err != nil {
		panic(err)
	}
	return exist
}

// Clone returns a duplicate of the IntegrationEntityQuery builder, including all associated steps. It can be
// used to prepare common query builders and use them differently after the clone is made.
func (ieq *IntegrationEntityQuery) Clone() *IntegrationEntityQuery {
	if ieq == nil {
		return nil
	}
	return &IntegrationEntityQuery{
		config:     ieq.config,
		ctx:        ieq.ctx.Clone(),
		order:      append([]integrationentity.OrderOption{}, ieq.order...),
		inters:     append([]Interceptor{}, ieq.inters...),
		predicates: append([]predicate.IntegrationEntity{}, ieq.predicates...),
		// clone intermediate query.
		sql:  ieq.sql.Clone(),
		path: ieq.path,
	}
}

// GroupBy is used to group vertices by one or more fields/columns.
// It is often used with aggregate functions, like: count, max, mean, min, sum.
//
// Example:
//
//	var v []struct {
//		TenantID string `json:"tenant_id,omitempty"`
//		Count int `json:"count,omitempty"`
//	}
//
//	client.IntegrationEntity.Query().
//		GroupBy(integrationentity.FieldTenantID).
//		Aggregate(ent.Count()).
//		Scan(ctx, &v)
func (ieq *IntegrationEntityQuery) GroupBy(field string, fields ...string) *IntegrationEntityGroupBy {
	ieq.ctx.Fields = append([]string{field}, fields...)
	grbuild := &IntegrationEntityGroupBy{build: ieq}
	grbuild.flds = &ieq.ctx.Fields
	grbuild.label = integrationentity.Label
	grbuild.scan = grbuild.Scan
	return grbuild
}

// Select allows the selection one or more fields/columns for the given query,
// instead of selecting all fields in the entity.
//
// Example:
//
//	var v []struct {
//		TenantID string `json:"tenant_id,omitempty"`
//	}
//
//	client.IntegrationEntity.Query().
//		Select(integrationentity.FieldTenantID).
//		Scan(ctx, &v)
func (ieq *IntegrationEntityQuery) Select(fields ...string) *IntegrationEntitySelect {
	ieq.ctx.Fields = append(ieq.ctx.Fields, fields...)
	sbuild := &IntegrationEntitySelect{IntegrationEntityQuery: ieq}
	sbuild.label = integrationentity.Label
	sbuild.flds, sbuild.scan = &ieq.ctx.Fields, sbuild.Scan
	return sbuild
}

// Aggregate returns a IntegrationEntitySelect configured with the given aggregations.
func (ieq *IntegrationEntityQuery) Aggregate(fns ...AggregateFunc) *IntegrationEntitySelect {
	return ieq.Select().Aggregate(fns...)
}

func (ieq *IntegrationEntityQuery) prepareQuery(ctx context.Context) error {
	for _, inter := range ieq.inters {
		if inter == nil {
			return fmt.Errorf("ent: uninitialized interceptor (forgotten import ent/runtime?)")
		}
		if trv, ok := inter.(Traverser); ok {
			if err := trv.Traverse(ctx, ieq); err != nil {
				return err
			}
		}
	}
	for _, f := range ieq.ctx.Fields {
		if !integrationentity.ValidColumn(f) {
			return &ValidationError{Name: f, err: fmt.Errorf("ent: invalid field %q for query", f)}
		}
	}
	if ieq.path != nil {
		prev, err := ieq.path(ctx)
		if err != nil {
			return err
		}
		ieq.sql = prev
	}
	return nil
}

func (ieq *IntegrationEntityQuery) sqlAll(ctx context.Context, hooks ...queryHook) ([]*IntegrationEntity, error) {
	var (
		nodes = []*IntegrationEntity{}
		_spec = ieq.querySpec()
	)
	_spec.ScanValues = func(columns []string) ([]any, error) {
		return (*IntegrationEntity).scanValues(nil, columns)
	}
	_spec.Assign = func(columns []string, values []any) error {
		node := &IntegrationEntity{config: ieq.config}
		nodes = append(nodes, node)
		return node.assignValues(columns, values)
	}
	for i := range hooks {
		hooks[i](ctx, _spec)
	}
	if err := sqlgraph.QueryNodes(ctx, ieq.driver, _spec); err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nodes, nil
	}
	return nodes, nil
}

func (ieq *IntegrationEntityQuery) sqlCount(ctx context.Context) (int, error) {
	_spec := ieq.querySpec()
	_spec.Node.Columns = ieq.ctx.Fields
	if len(ieq.ctx.Fields) > 0 {
		_spec.Unique = ieq.ctx.Unique != nil && *ieq.ctx.Unique
	}
	return sqlgraph.CountNodes(ctx, ieq.driver, _spec)
}

func (ieq *IntegrationEntityQuery) querySpec() *sqlgraph.QuerySpec {
	_spec := sqlgraph.NewQuerySpec(integrationentity.Table, integrationentity.Columns, sqlgraph.NewFieldSpec(integrationentity.FieldID, field.TypeString))
	_spec.From = ieq.sql
	if unique := ieq.ctx.Unique; unique != nil {
		_spec.Unique = *unique
	} else if ieq.path != nil {
		_spec.Unique = true
	}
	if fields := ieq.ctx.Fields; len(fields) > 0 {
		_spec.Node.Columns = make([]string, 0, len(fields))
		_spec.Node.Columns = append(_spec.Node.Columns, integrationentity.FieldID)
		for i := range fields {
			if fields[i] != integrationentity.FieldID {
				_spec.Node.Columns = append(_spec.Node.Columns, fields[i])
			}
		}
	}
	if ps := ieq.predicates; len(ps) > 0 {
		_spec.Predicate = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	if limit := ieq.ctx.Limit; limit != nil {
		_spec.Limit = *limit
	}
	if offset := ieq.ctx.Offset; offset != nil {
		_spec.Offset = *offset
	}
	if ps := ieq.order; len(ps) > 0 {
		_spec.Order = func(selector *sql.Selector) {
			for i := range ps {
				ps[i](selector)
			}
		}
	}
	return _spec
}

func (ieq *IntegrationEntityQuery) sqlQuery(ctx context.Context) *sql.Selector {
	builder := sql.Dialect(ieq.driver.Dialect())
	t1 := builder.Table(integrationentity.Table)
	columns := ieq.ctx.Fields
	if len(columns) == 0 {
		columns = integrationentity.Columns
	}
	selector := builder.Select(t1.Columns(columns...)...).From(t1)
	if ieq.sql != nil {
		selector = ieq.sql
		selector.Select(selector.Columns(columns...)...)
	}
	if ieq.ctx.Unique != nil && *ieq.ctx.Unique {
		selector.Distinct()
	}
	for _, p := range ieq.predicates {
		p(selector)
	}
	for _, p := range ieq.order {
		p(selector)
	}
	if offset := ieq.ctx.Offset; offset != nil {
		// limit is mandatory for offset clause. We start
		// with default value, and override it below if needed.
		selector.Offset(*offset).Limit(math.MaxInt32)
	}
	if limit := ieq.ctx.Limit; limit != nil {
		selector.Limit(*limit)
	}
	return selector
}

// IntegrationEntityGroupBy is the group-by builder for IntegrationEntity entities.
type IntegrationEntityGroupBy struct {
	selector
	build *IntegrationEntityQuery
}

// Aggregate adds the given aggregation functions to the group-by query.
func (iegb *IntegrationEntityGroupBy) Aggregate(fns ...AggregateFunc) *IntegrationEntityGroupBy {
	iegb.fns = append(iegb.fns, fns...)
	return iegb
}

// Scan applies the selector query and scans the result into the given value.
func (iegb *IntegrationEntityGroupBy) Scan(ctx context.Context, v any) error {
	ctx = setContextOp(ctx, iegb.build.ctx, ent.OpQueryGroupBy)
	if err := iegb.build.prepareQuery(ctx); err != nil {
		return err
	}
	return scanWithInterceptors[*IntegrationEntityQuery, *IntegrationEntityGroupBy](ctx, iegb.build, iegb, iegb.build.inters, v)
}

func (iegb *IntegrationEntityGroupBy) sqlScan(ctx context.Context, root *IntegrationEntityQuery, v any) error {
	selector := root.sqlQuery(ctx).Select()
	aggregation := make([]string, 0, len(iegb.fns))
	for _, fn := range iegb.fns {
		aggregation = append(aggregation, fn(selector))
	}
	if len(selector.SelectedColumns()) == 0 {
		columns := make([]string, 0, len(*iegb.flds)+len(iegb.fns))
		for _, f := range *iegb.flds {
			columns = append(columns, selector.C(f))
		}
		columns = append(columns, aggregation...)
		selector.Select(columns...)
	}
	selector.GroupBy(selector.Columns(*iegb.flds...)...)
	if err := selector.Err(); err != nil {
		return err
	}
	rows := &sql.Rows{}
	query, args := selector.Query()
	if err := iegb.build.driver.Query(ctx, query, args, rows); err != nil {
		return err
	}
	defer rows.Close()
	return sql.ScanSlice(rows, v)
}

// IntegrationEntitySelect is the builder for selecting fields of IntegrationEntity entities.
type IntegrationEntitySelect struct {
	*IntegrationEntityQuery
	selector
}

// Aggregate adds the given aggregation functions to the selector query.
func (ies *IntegrationEntitySelect) Aggregate(fns ...AggregateFunc) *IntegrationEntitySelect {
	ies.fns = append(ies.fns, fns...)
	return ies
}

// Scan applies the selector query and scans the result into the given value.
func (ies *IntegrationEntitySelect) Scan(ctx context.Context, v any) error {
	ctx = setContextOp(ctx, ies.ctx, ent.OpQuerySelect)
	if err := ies.prepareQuery(ctx); err != nil {
		return err
	}
	return scanWithInterceptors[*IntegrationEntityQuery, *IntegrationEntitySelect](ctx, ies.IntegrationEntityQuery, ies, ies.inters, v)
}

func (ies *IntegrationEntitySelect) sqlScan(ctx context.Context, root *IntegrationEntityQuery, v any) error {
	selector := root.sqlQuery(ctx)
	aggregation := make([]string, 0, len(ies.fns))
	for _, fn := range ies.fns {
		aggregation = append(aggregation, fn(selector))
	}
	switch n := len(*ies.selector.flds); {
	case n == 0 && len(aggregation) > 0:
		selector.Select(aggregation...)
	case n != 0 && len(aggregation) > 0:
		selector.AppendSelect(aggregation...)
	}
	rows := &sql.Rows{}
	query, args := selector.Query()
	if err := ies.driver.Query(ctx, query, args, rows); err != nil {
		return err
	}
	defer rows.Close()
	return sql.ScanSlice(rows, v)
}
