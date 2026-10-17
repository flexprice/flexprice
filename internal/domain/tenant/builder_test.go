package tenant

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestBuilder_CopiesAndSets(t *testing.T) {
	orig := &Tenant{ID: "t1", InternalStatus: types.TenantInternalStatusTrialing, Metadata: types.Metadata{"a": "1"}}
	got := NewBuilder(orig).WithInternalStatus(types.TenantInternalStatusActive).Build()

	assert.Equal(t, types.TenantInternalStatusActive, got.InternalStatus)
	assert.Equal(t, types.TenantInternalStatusTrialing, orig.InternalStatus, "original untouched")
	got.Metadata["b"] = "2"
	assert.NotContains(t, orig.Metadata, "b", "metadata map is copied")
}
