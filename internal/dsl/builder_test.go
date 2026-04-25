package dsl_test

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/dsl"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnd_BuildsGroupNode(t *testing.T) {
	node := dsl.And(dsl.Eq("status", "active"), dsl.Eq("plan", "free"))
	require.NotNil(t, node.Operator)
	assert.Equal(t, types.GroupOpAnd, *node.Operator)
	assert.Len(t, node.Conditions, 2)
}

func TestOr_BuildsGroupNode(t *testing.T) {
	node := dsl.Or(dsl.Eq("plan", "free"), dsl.Eq("plan", "pro"))
	require.NotNil(t, node.Operator)
	assert.Equal(t, types.GroupOpOr, *node.Operator)
	assert.Len(t, node.Conditions, 2)
}

func TestNot_BuildsGroupNode(t *testing.T) {
	node := dsl.Not(dsl.Eq("status", "deleted"))
	require.NotNil(t, node.Operator)
	assert.Equal(t, types.GroupOpNot, *node.Operator)
	assert.Len(t, node.Conditions, 1)
}

func TestEq_BuildsLeafNode(t *testing.T) {
	node := dsl.Eq("status", "active")
	require.NotNil(t, node.Condition)
	assert.Equal(t, "status", *node.Condition.Field)
	assert.Equal(t, types.EQUAL, *node.Condition.Operator)
	assert.Equal(t, "active", *node.Condition.Value.String)
}

func TestGt_BuildsLeafNode(t *testing.T) {
	node := dsl.Gt("credits", 0)
	require.NotNil(t, node.Condition)
	assert.Equal(t, types.GREATER_THAN, *node.Condition.Operator)
	assert.Equal(t, float64(0), *node.Condition.Value.Number)
}

func TestGte_BuildsLeafNode(t *testing.T) {
	node := dsl.Gte("credits", 5)
	require.NotNil(t, node.Condition)
	assert.Equal(t, "credits", *node.Condition.Field)
	assert.Equal(t, types.GREATER_THAN_OR_EQUAL, *node.Condition.Operator)
	assert.Equal(t, types.DataTypeNumber, *node.Condition.DataType)
	assert.Equal(t, float64(5), *node.Condition.Value.Number)
}

func TestLt_BuildsLeafNode(t *testing.T) {
	node := dsl.Lt("credits", 10)
	require.NotNil(t, node.Condition)
	assert.Equal(t, "credits", *node.Condition.Field)
	assert.Equal(t, types.LESS_THAN, *node.Condition.Operator)
	assert.Equal(t, types.DataTypeNumber, *node.Condition.DataType)
	assert.Equal(t, float64(10), *node.Condition.Value.Number)
}

func TestLte_BuildsLeafNode(t *testing.T) {
	node := dsl.Lte("credits", 10)
	require.NotNil(t, node.Condition)
	assert.Equal(t, "credits", *node.Condition.Field)
	assert.Equal(t, types.LESS_THAN_OR_EQUAL, *node.Condition.Operator)
	assert.Equal(t, types.DataTypeNumber, *node.Condition.DataType)
	assert.Equal(t, float64(10), *node.Condition.Value.Number)
}

func TestIn_BuildsLeafNode(t *testing.T) {
	node := dsl.In("status", []string{"active", "trialing"})
	require.NotNil(t, node.Condition)
	assert.Equal(t, types.IN, *node.Condition.Operator)
	assert.Equal(t, []string{"active", "trialing"}, node.Condition.Value.Array)
}

func TestIn_PanicsOnEmptyValues(t *testing.T) {
	assert.Panics(t, func() { dsl.In("status", nil) })
	assert.Panics(t, func() { dsl.In("status", []string{}) })
}

func TestNotIn_PanicsOnEmptyValues(t *testing.T) {
	assert.Panics(t, func() { dsl.NotIn("status", nil) })
	assert.Panics(t, func() { dsl.NotIn("status", []string{}) })
}

func TestNotIn_BuildsLeafNode(t *testing.T) {
	node := dsl.NotIn("status", []string{"deleted"})
	require.NotNil(t, node.Condition)
	assert.Equal(t, "status", *node.Condition.Field)
	assert.Equal(t, types.NOT_IN, *node.Condition.Operator)
	assert.Equal(t, types.DataTypeArray, *node.Condition.DataType)
	assert.Equal(t, []string{"deleted"}, node.Condition.Value.Array)
}

func TestContains_BuildsLeafNode(t *testing.T) {
	node := dsl.Contains("name", "acme")
	assert.Equal(t, types.CONTAINS, *node.Condition.Operator)
	assert.Equal(t, "acme", *node.Condition.Value.String)
}

func TestNotContains_BuildsLeafNode(t *testing.T) {
	node := dsl.NotContains("name", "test")
	require.NotNil(t, node.Condition)
	assert.Equal(t, "name", *node.Condition.Field)
	assert.Equal(t, types.NOT_CONTAINS, *node.Condition.Operator)
	assert.Equal(t, types.DataTypeString, *node.Condition.DataType)
	assert.Equal(t, "test", *node.Condition.Value.String)
}

func TestBefore_BuildsLeafNode(t *testing.T) {
	ts := time.Now()
	node := dsl.Before("created_at", ts)
	assert.Equal(t, types.BEFORE, *node.Condition.Operator)
	assert.Equal(t, ts, *node.Condition.Value.Date)
}

func TestAfter_BuildsLeafNode(t *testing.T) {
	ts := time.Now()
	node := dsl.After("created_at", ts)
	require.NotNil(t, node.Condition)
	assert.Equal(t, "created_at", *node.Condition.Field)
	assert.Equal(t, types.AFTER, *node.Condition.Operator)
	assert.Equal(t, types.DataTypeDate, *node.Condition.DataType)
	assert.Equal(t, ts, *node.Condition.Value.Date)
}

func TestBuiltTree_PassesValidation(t *testing.T) {
	node := dsl.Or(
		dsl.And(
			dsl.Eq("status", "active"),
			dsl.Gt("credits", 0),
		),
		dsl.Eq("plan", "free"),
	)
	require.NoError(t, node.Validate())
}
