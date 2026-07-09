package invoice

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestInvoice_CollectionMethod_DefaultsNilNotSendInvoice(t *testing.T) {
	t.Parallel()
	inv := &Invoice{}
	require.Nil(t, inv.CollectionMethod)
}

func TestInvoice_CollectionMethod_CanBeSetToChargeAutomatically(t *testing.T) {
	t.Parallel()
	cm := types.CollectionMethodChargeAutomatically
	inv := &Invoice{CollectionMethod: &cm}
	require.Equal(t, types.CollectionMethodChargeAutomatically, *inv.CollectionMethod)
}
