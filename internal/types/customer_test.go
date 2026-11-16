package types

import (
	"encoding/json"
	"strings"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
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

// search_query is limited to 255 characters, not bytes, after trimming.
func TestCustomerFilter_ValidateSearchQueryLength(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"ascii_at_limit", strings.Repeat("a", 255), false},
		{"ascii_over_limit", strings.Repeat("a", 256), true},
		{"multibyte_at_limit", strings.Repeat("é", 255), false},
		{"multibyte_over_limit", strings.Repeat("客", 256), true},
		{"surrounding_spaces_not_counted", "  " + strings.Repeat("a", 255) + "  ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CustomerFilter{SearchQuery: tt.query}.Validate()
			if tt.wantErr {
				assert.True(t, ierr.IsValidation(err), "got %v", err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
