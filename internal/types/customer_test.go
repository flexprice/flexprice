package types

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomerFilter_GetStatus(t *testing.T) {
	statusInPublishedAndArchived := []*FilterCondition{
		{
			Field:    lo.ToPtr("status"),
			Operator: lo.ToPtr(IN),
			DataType: lo.ToPtr(DataTypeArray),
			Value:    &Value{Array: []string{string(StatusPublished), string(StatusArchived)}},
		},
	}

	t.Run("defaults to published when no status is specified", func(t *testing.T) {
		f := NewCustomerFilter()
		assert.Equal(t, string(StatusPublished), f.GetStatus())
	})

	t.Run("skips default when DSL filters are present so archived can be included", func(t *testing.T) {
		f := NewCustomerFilter()
		f.Filters = statusInPublishedAndArchived
		assert.Equal(t, "", f.GetStatus())
	})

	t.Run("honors explicit query status over DSL", func(t *testing.T) {
		f := NewCustomerFilter()
		f.Status = lo.ToPtr(StatusArchived)
		f.Filters = statusInPublishedAndArchived
		assert.Equal(t, string(StatusArchived), f.GetStatus())
	})

	t.Run("empty query status is treated as unset", func(t *testing.T) {
		f := NewCustomerFilter()
		f.Status = lo.ToPtr(Status(""))
		assert.Equal(t, string(StatusPublished), f.GetStatus())
	})

	t.Run("empty filters slice skips published-only default", func(t *testing.T) {
		f := NewCustomerFilter()
		f.Filters = []*FilterCondition{}
		assert.Equal(t, "", f.GetStatus())
	})
}

func TestCustomerFilter_UnmarshalFrontendSearchJSON(t *testing.T) {
	t.Run("empty filters", func(t *testing.T) {
		var f CustomerFilter
		err := json.Unmarshal([]byte(`{"limit":10,"offset":0,"sort":[{"field":"updated_at","direction":"desc"}],"filters":[]}`), &f)
		require.NoError(t, err)
		assert.NotNil(t, f.Filters)
		assert.Equal(t, "", f.GetStatus())
	})

	t.Run("status in published and archived", func(t *testing.T) {
		var f CustomerFilter
		err := json.Unmarshal([]byte(`{"limit":10,"offset":0,"sort":[{"field":"updated_at","direction":"desc"}],"filters":[{"field":"status","operator":"in","data_type":"array","value":{"array":["published","archived"]}}]}`), &f)
		require.NoError(t, err)
		assert.True(t, HasFieldFilter(f.Filters, "status"))
		assert.Equal(t, "", f.GetStatus())
	})
}
