package dsl

import (
	"testing"

	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testResolve(field string) (string, error) { return field, nil }

func strLeaf(field, val string) *types.FilterNode {
	return &types.FilterNode{
		Condition: &types.FilterCondition{
			Field:    lo.ToPtr(field),
			Operator: lo.ToPtr(types.EQUAL),
			DataType: lo.ToPtr(types.DataTypeString),
			Value:    &types.Value{String: &val},
		},
	}
}

func numLeaf(field string, val float64) *types.FilterNode {
	return &types.FilterNode{
		Condition: &types.FilterCondition{
			Field:    lo.ToPtr(field),
			Operator: lo.ToPtr(types.GREATER_THAN),
			DataType: lo.ToPtr(types.DataTypeNumber),
			Value:    &types.Value{Number: &val},
		},
	}
}

func predicateSQL(t *testing.T, p *sql.Predicate) (string, []any) {
	t.Helper()
	sel := sql.Select("*").From(sql.Table("t"))
	sel.Where(p)
	return sel.Query()
}

func TestBuildNodePredicate_LeafEq(t *testing.T) {
	node := strLeaf("status", "active")
	require.NoError(t, node.Validate())

	pred, err := buildNodePredicate(node, testResolve, map[string]bool{})
	require.NoError(t, err)
	require.NotNil(t, pred)

	query, args := predicateSQL(t, pred)
	assert.Contains(t, query, "status")
	assert.Contains(t, args, "active")
}

func TestBuildNodePredicate_LeafGt(t *testing.T) {
	node := numLeaf("credits", 0)
	require.NoError(t, node.Validate())

	pred, err := buildNodePredicate(node, testResolve, map[string]bool{})
	require.NoError(t, err)

	query, args := predicateSQL(t, pred)
	assert.Contains(t, query, "credits")
	assert.Contains(t, args, float64(0))
}

func TestBuildNodePredicate_AndGroup(t *testing.T) {
	node := &types.FilterNode{
		Operator:   lo.ToPtr(types.GroupOpAnd),
		Conditions: []*types.FilterNode{strLeaf("status", "active"), numLeaf("credits", 0)},
	}
	require.NoError(t, node.Validate())

	pred, err := buildNodePredicate(node, testResolve, map[string]bool{})
	require.NoError(t, err)

	query, _ := predicateSQL(t, pred)
	assert.Contains(t, query, "AND")
}

func TestBuildNodePredicate_OrGroup(t *testing.T) {
	node := &types.FilterNode{
		Operator:   lo.ToPtr(types.GroupOpOr),
		Conditions: []*types.FilterNode{strLeaf("status", "active"), strLeaf("plan", "free")},
	}
	require.NoError(t, node.Validate())

	pred, err := buildNodePredicate(node, testResolve, map[string]bool{})
	require.NoError(t, err)

	query, _ := predicateSQL(t, pred)
	assert.Contains(t, query, "OR")
}

func TestBuildNodePredicate_NotGroup(t *testing.T) {
	node := &types.FilterNode{
		Operator:   lo.ToPtr(types.GroupOpNot),
		Conditions: []*types.FilterNode{strLeaf("status", "deleted")},
	}
	require.NoError(t, node.Validate())

	pred, err := buildNodePredicate(node, testResolve, map[string]bool{})
	require.NoError(t, err)

	query, _ := predicateSQL(t, pred)
	assert.Contains(t, query, "NOT")
}

func TestBuildNodePredicate_BlockedField(t *testing.T) {
	node := strLeaf("tenant_id", "abc")
	require.NoError(t, node.Validate())

	_, err := buildNodePredicate(node, testResolve, map[string]bool{"tenant_id": true})
	assert.Error(t, err)
}

func TestBuildNodePredicate_NestedOrInsideAnd(t *testing.T) {
	inner := &types.FilterNode{
		Operator:   lo.ToPtr(types.GroupOpOr),
		Conditions: []*types.FilterNode{strLeaf("plan", "free"), strLeaf("plan", "pro")},
	}
	node := &types.FilterNode{
		Operator:   lo.ToPtr(types.GroupOpAnd),
		Conditions: []*types.FilterNode{strLeaf("status", "active"), inner},
	}
	require.NoError(t, node.Validate())

	pred, err := buildNodePredicate(node, testResolve, map[string]bool{})
	require.NoError(t, err)

	query, _ := predicateSQL(t, pred)
	assert.Contains(t, query, "AND")
	assert.Contains(t, query, "OR")
}

func TestApplyFilterNode_NilNode(t *testing.T) {
	got, err := ApplyFilterNode[int, int](42, nil, testResolve, func(p Predicate) int { return 0 }, nil)
	require.NoError(t, err)
	assert.Equal(t, 42, got)
}

func TestApplyFilterNode_BlockedFieldReturnsError(t *testing.T) {
	node := &types.FilterNode{
		Condition: &types.FilterCondition{
			Field:    lo.ToPtr("tenant_id"),
			Operator: lo.ToPtr(types.EQUAL),
			DataType: lo.ToPtr(types.DataTypeString),
			Value:    &types.Value{String: lo.ToPtr("abc")},
		},
	}
	_, err := ApplyFilterNode[int, int](
		0, node,
		func(f string) (string, error) { return f, nil },
		func(p Predicate) int { return 0 },
		nil,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tenant_id")
}
