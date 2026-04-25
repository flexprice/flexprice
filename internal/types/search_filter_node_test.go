package types

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func leafCond(field, op string, val *Value, dt DataType) *FilterCondition {
	return &FilterCondition{
		Field:    lo.ToPtr(field),
		Operator: lo.ToPtr(FilterOperatorType(op)),
		DataType: lo.ToPtr(dt),
		Value:    val,
	}
}

func stringLeaf(field, val string) *FilterNode {
	return &FilterNode{Condition: leafCond(field, string(EQUAL), &Value{String: &val}, DataTypeString)}
}

func TestFilterNode_Validate_LeafValid(t *testing.T) {
	err := stringLeaf("status", "active").Validate()
	require.NoError(t, err)
}

func TestFilterNode_Validate_BothRolesSet(t *testing.T) {
	node := &FilterNode{
		Operator:  lo.ToPtr(GroupOpAnd),
		Condition: leafCond("status", string(EQUAL), &Value{String: lo.ToPtr("active")}, DataTypeString),
	}
	assert.Error(t, node.Validate())
}

func TestFilterNode_Validate_NeitherRoleSet(t *testing.T) {
	assert.Error(t, (&FilterNode{}).Validate())
}

func TestFilterNode_Validate_GroupWithNoChildren(t *testing.T) {
	node := &FilterNode{Operator: lo.ToPtr(GroupOpAnd), Conditions: []*FilterNode{}}
	assert.Error(t, node.Validate())
}

func TestFilterNode_Validate_NotGroupTwoChildren(t *testing.T) {
	leaf := stringLeaf("status", "active")
	node := &FilterNode{
		Operator:   lo.ToPtr(GroupOpNot),
		Conditions: []*FilterNode{leaf, leaf},
	}
	assert.Error(t, node.Validate())
}

func TestFilterNode_Validate_NotGroupOneChild(t *testing.T) {
	node := &FilterNode{
		Operator:   lo.ToPtr(GroupOpNot),
		Conditions: []*FilterNode{stringLeaf("status", "active")},
	}
	require.NoError(t, node.Validate())
}

func TestFilterNode_Validate_AndGroupValid(t *testing.T) {
	node := &FilterNode{
		Operator:   lo.ToPtr(GroupOpAnd),
		Conditions: []*FilterNode{stringLeaf("status", "active"), stringLeaf("plan", "free")},
	}
	require.NoError(t, node.Validate())
}

func TestFilterNode_Validate_MaxDepthExceeded(t *testing.T) {
	node := stringLeaf("status", "active")
	for i := 0; i <= MaxFilterDepth; i++ {
		node = &FilterNode{
			Operator:   lo.ToPtr(GroupOpAnd),
			Conditions: []*FilterNode{node},
		}
	}
	assert.Error(t, node.Validate())
}

func TestFilterNode_Validate_MaxDepthAllowed(t *testing.T) {
	node := stringLeaf("status", "active")
	for i := 0; i < MaxFilterDepth; i++ {
		node = &FilterNode{
			Operator:   lo.ToPtr(GroupOpAnd),
			Conditions: []*FilterNode{node},
		}
	}
	require.NoError(t, node.Validate())
}

func TestFilterNode_Validate_MaxNodesExceeded(t *testing.T) {
	leaves := make([]*FilterNode, MaxFilterNodes+1)
	for i := range leaves {
		leaves[i] = stringLeaf("status", "active")
	}
	node := &FilterNode{Operator: lo.ToPtr(GroupOpAnd), Conditions: leaves}
	assert.Error(t, node.Validate())
}

func TestDSLFilter_Validate_NilIsNoop(t *testing.T) {
	var d *DSLFilter
	require.NoError(t, d.Validate())
}

func TestDSLFilter_Validate_PropagatesFilterNodeError(t *testing.T) {
	d := &DSLFilter{FilterNode: &FilterNode{}} // invalid: neither leaf nor group
	assert.Error(t, d.Validate())
}

func TestFilterNode_Validate_OrGroupValid(t *testing.T) {
	node := &FilterNode{
		Operator:   lo.ToPtr(GroupOpOr),
		Conditions: []*FilterNode{stringLeaf("plan", "free"), stringLeaf("plan", "pro")},
	}
	require.NoError(t, node.Validate())
}

func TestFilterNode_Validate_NilChildInConditions(t *testing.T) {
	node := &FilterNode{
		Operator:   lo.ToPtr(GroupOpAnd),
		Conditions: []*FilterNode{stringLeaf("status", "active"), nil},
	}
	assert.Error(t, node.Validate())
}

func TestDSLFilter_Validate_PropagatesFiltersError(t *testing.T) {
	d := &DSLFilter{
		Filters: []*FilterCondition{
			{}, // invalid: no field/operator/data_type/value
		},
	}
	assert.Error(t, d.Validate())
}

func TestDSLFilter_Validate_PropagatesSortError(t *testing.T) {
	d := &DSLFilter{
		Sort: []*SortCondition{
			{Field: "", Direction: ""}, // invalid: empty field
		},
	}
	assert.Error(t, d.Validate())
}
