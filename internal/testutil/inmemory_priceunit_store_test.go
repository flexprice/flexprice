package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/priceunit"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryPriceUnitStore_Create(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPriceUnitStore()

	t.Run("successful creation", func(t *testing.T) {
		pu := &priceunit.PriceUnit{
			ID:             "test-id",
			Name:           "Test Token",
			Code:           "TEST",
			Symbol:         "T",
			BaseCurrency:   "USD",
			ConversionRate: decimal.NewFromFloat(0.01),
			Precision:      2,
			EnvironmentID:  "test-env",
			Metadata:       map[string]string{"key": "value"},
			BaseModel: types.BaseModel{
				TenantID:  "test-tenant",
				Status:    types.StatusPublished,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		}

		created, err := store.Create(ctx, pu)
		require.NoError(t, err)
		assert.Equal(t, pu.ID, created.ID)
		assert.Equal(t, pu.Code, created.Code)
		assert.Equal(t, pu.Name, created.Name)
		assert.Equal(t, pu.Symbol, created.Symbol)
		assert.Equal(t, pu.BaseCurrency, created.BaseCurrency)
		assert.Equal(t, pu.ConversionRate, created.ConversionRate)
		assert.Equal(t, pu.Precision, created.Precision)
		assert.Equal(t, pu.EnvironmentID, created.EnvironmentID)
		assert.Equal(t, pu.Metadata, created.Metadata)
	})

	t.Run("nil price unit", func(t *testing.T) {
		_, err := store.Create(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "price unit cannot be nil")
	})

	t.Run("duplicate ID", func(t *testing.T) {
		pu := &priceunit.PriceUnit{
			ID:   "duplicate-id",
			Code: "DUP",
			BaseModel: types.BaseModel{
				TenantID: "test-tenant",
				Status:   types.StatusPublished,
			},
		}

		_, err := store.Create(ctx, pu)
		require.NoError(t, err)

		_, err = store.Create(ctx, pu)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("sets environment ID from context", func(t *testing.T) {
		ctxWithEnv := context.WithValue(ctx, types.CtxEnvironmentID, "context-env")
		pu := &priceunit.PriceUnit{
			ID:   "env-test-id",
			Code: "ENV_TEST",
			BaseModel: types.BaseModel{
				TenantID: "test-tenant",
				Status:   types.StatusPublished,
			},
		}

		created, err := store.Create(ctxWithEnv, pu)
		require.NoError(t, err)
		assert.Equal(t, "context-env", created.EnvironmentID)
	})
}

func TestInMemoryPriceUnitStore_Get(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPriceUnitStore()

	t.Run("successful get", func(t *testing.T) {
		pu := &priceunit.PriceUnit{
			ID:   "get-test-id",
			Code: "GET_TEST",
			BaseModel: types.BaseModel{
				TenantID: "test-tenant",
				Status:   types.StatusPublished,
			},
		}

		_, err := store.Create(ctx, pu)
		require.NoError(t, err)

		retrieved, err := store.Get(ctx, pu.ID)
		require.NoError(t, err)
		assert.Equal(t, pu.ID, retrieved.ID)
		assert.Equal(t, pu.Code, retrieved.Code)
	})

	t.Run("not found", func(t *testing.T) {
		_, err := store.Get(ctx, "non-existent-id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestInMemoryPriceUnitStore_GetByCode(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPriceUnitStore()

	t.Run("successful get by code", func(t *testing.T) {
		pu := &priceunit.PriceUnit{
			ID:   "code-test-id",
			Code: "CODE_TEST",
			BaseModel: types.BaseModel{
				TenantID: "test-tenant",
				Status:   types.StatusPublished,
			},
		}

		_, err := store.Create(ctx, pu)
		require.NoError(t, err)

		retrieved, err := store.GetByCode(ctx, pu.Code)
		require.NoError(t, err)
		assert.Equal(t, pu.ID, retrieved.ID)
		assert.Equal(t, pu.Code, retrieved.Code)
	})

	t.Run("not found by code", func(t *testing.T) {
		_, err := store.GetByCode(ctx, "NON_EXISTENT")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("empty code", func(t *testing.T) {
		_, err := store.GetByCode(ctx, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "code cannot be empty")
	})
}

func TestInMemoryPriceUnitStore_List(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPriceUnitStore()

	// Create test data
	pu1 := &priceunit.PriceUnit{
		ID:             "list-test-1",
		Name:           "Token A",
		Code:           "TOKEN_A",
		Symbol:         "A",
		BaseCurrency:   "USD",
		ConversionRate: decimal.NewFromFloat(0.01),
		Precision:      2,
		EnvironmentID:  "test-env",
		BaseModel: types.BaseModel{
			TenantID: "test-tenant",
			Status:   types.StatusPublished,
		},
	}

	pu2 := &priceunit.PriceUnit{
		ID:             "list-test-2",
		Name:           "Token B",
		Code:           "TOKEN_B",
		Symbol:         "B",
		BaseCurrency:   "EUR",
		ConversionRate: decimal.NewFromFloat(0.02),
		Precision:      3,
		EnvironmentID:  "test-env",
		BaseModel: types.BaseModel{
			TenantID: "test-tenant",
			Status:   types.StatusPublished,
		},
	}

	pu3 := &priceunit.PriceUnit{
		ID:             "list-test-3",
		Name:           "Token C",
		Code:           "TOKEN_C",
		Symbol:         "C",
		BaseCurrency:   "USD",
		ConversionRate: decimal.NewFromFloat(0.03),
		Precision:      4,
		EnvironmentID:  "test-env",
		BaseModel: types.BaseModel{
			TenantID: "test-tenant",
			Status:   types.StatusArchived,
		},
	}

	_, err := store.Create(ctx, pu1)
	require.NoError(t, err)
	_, err = store.Create(ctx, pu2)
	require.NoError(t, err)
	_, err = store.Create(ctx, pu3)
	require.NoError(t, err)

	t.Run("list all", func(t *testing.T) {
		filter := types.NewNoLimitPriceUnitFilter()
		priceUnits, err := store.List(ctx, filter)
		require.NoError(t, err)
		assert.Len(t, priceUnits, 3)
	})

	t.Run("list with nil filter", func(t *testing.T) {
		priceUnits, err := store.List(ctx, nil)
		require.NoError(t, err)
		assert.Len(t, priceUnits, 3)
	})

	t.Run("filter by price unit IDs", func(t *testing.T) {
		filter := &types.PriceUnitFilter{
			QueryFilter:  types.NewNoLimitQueryFilter(),
			PriceUnitIDs: []string{"list-test-1", "list-test-3"},
		}
		priceUnits, err := store.List(ctx, filter)
		require.NoError(t, err)
		assert.Len(t, priceUnits, 2)

		ids := make([]string, len(priceUnits))
		for i, pu := range priceUnits {
			ids[i] = pu.ID
		}
		assert.Contains(t, ids, "list-test-1")
		assert.Contains(t, ids, "list-test-3")
	})

	t.Run("filter by code", func(t *testing.T) {
		filter := &types.PriceUnitFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
			Filters: []*types.FilterCondition{
				{
					Field:    lo.ToPtr("code"),
					Operator: lo.ToPtr(types.EQUAL),
					DataType: lo.ToPtr(types.DataTypeString),
					Value: &types.Value{
						String: lo.ToPtr("TOKEN_A"),
					},
				},
			},
		}
		priceUnits, err := store.List(ctx, filter)
		require.NoError(t, err)
		assert.Len(t, priceUnits, 1)
		assert.Equal(t, "TOKEN_A", priceUnits[0].Code)
	})

	t.Run("filter by base currency", func(t *testing.T) {
		filter := &types.PriceUnitFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
			Filters: []*types.FilterCondition{
				{
					Field:    lo.ToPtr("base_currency"),
					Operator: lo.ToPtr(types.EQUAL),
					DataType: lo.ToPtr(types.DataTypeString),
					Value: &types.Value{
						String: lo.ToPtr("USD"),
					},
				},
			},
		}
		priceUnits, err := store.List(ctx, filter)
		require.NoError(t, err)
		assert.Len(t, priceUnits, 2)

		for _, pu := range priceUnits {
			assert.Equal(t, "USD", pu.BaseCurrency)
		}
	})

	t.Run("filter by name contains", func(t *testing.T) {
		filter := &types.PriceUnitFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
			Filters: []*types.FilterCondition{
				{
					Field:    lo.ToPtr("name"),
					Operator: lo.ToPtr(types.CONTAINS),
					DataType: lo.ToPtr(types.DataTypeString),
					Value: &types.Value{
						String: lo.ToPtr("Token A"),
					},
				},
			},
		}
		priceUnits, err := store.List(ctx, filter)
		require.NoError(t, err)
		assert.Len(t, priceUnits, 1)
		assert.Equal(t, "Token A", priceUnits[0].Name)
	})

	t.Run("filter by precision", func(t *testing.T) {
		filter := &types.PriceUnitFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
			Filters: []*types.FilterCondition{
				{
					Field:    lo.ToPtr("precision"),
					Operator: lo.ToPtr(types.EQUAL),
					DataType: lo.ToPtr(types.DataTypeNumber),
					Value: &types.Value{
						Number: lo.ToPtr(3.0),
					},
				},
			},
		}
		priceUnits, err := store.List(ctx, filter)
		require.NoError(t, err)
		assert.Len(t, priceUnits, 1)
		assert.Equal(t, 3, priceUnits[0].Precision)
	})

	t.Run("filter by status", func(t *testing.T) {
		filter := &types.PriceUnitFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
			Filters: []*types.FilterCondition{
				{
					Field:    lo.ToPtr("status"),
					Operator: lo.ToPtr(types.EQUAL),
					DataType: lo.ToPtr(types.DataTypeString),
					Value: &types.Value{
						String: lo.ToPtr("archived"),
					},
				},
			},
		}
		priceUnits, err := store.List(ctx, filter)
		require.NoError(t, err)
		assert.Len(t, priceUnits, 1)
		assert.Equal(t, types.StatusArchived, priceUnits[0].Status)
	})

	t.Run("pagination", func(t *testing.T) {
		filter := &types.PriceUnitFilter{
			QueryFilter: &types.QueryFilter{
				Limit:  lo.ToPtr(2),
				Offset: lo.ToPtr(0),
			},
		}
		priceUnits, err := store.List(ctx, filter)
		require.NoError(t, err)
		assert.Len(t, priceUnits, 2)
	})
}

func TestInMemoryPriceUnitStore_Update(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPriceUnitStore()

	t.Run("successful update", func(t *testing.T) {
		pu := &priceunit.PriceUnit{
			ID:   "update-test-id",
			Code: "UPDATE_TEST",
			BaseModel: types.BaseModel{
				TenantID: "test-tenant",
				Status:   types.StatusPublished,
			},
		}

		_, err := store.Create(ctx, pu)
		require.NoError(t, err)

		pu.Name = "Updated Name"
		pu.Symbol = "U"
		updated, err := store.Update(ctx, pu)
		require.NoError(t, err)
		assert.Equal(t, "Updated Name", updated.Name)
		assert.Equal(t, "U", updated.Symbol)
	})

	t.Run("nil price unit", func(t *testing.T) {
		_, err := store.Update(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "price unit cannot be nil")
	})

	t.Run("update non-existent", func(t *testing.T) {
		pu := &priceunit.PriceUnit{
			ID:   "non-existent",
			Code: "NON_EXISTENT",
			BaseModel: types.BaseModel{
				TenantID: "test-tenant",
				Status:   types.StatusPublished,
			},
		}

		_, err := store.Update(ctx, pu)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestInMemoryPriceUnitStore_Delete(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPriceUnitStore()

	t.Run("successful delete", func(t *testing.T) {
		pu := &priceunit.PriceUnit{
			ID:   "delete-test-id",
			Code: "DELETE_TEST",
			BaseModel: types.BaseModel{
				TenantID: "test-tenant",
				Status:   types.StatusPublished,
			},
		}

		_, err := store.Create(ctx, pu)
		require.NoError(t, err)

		err = store.Delete(ctx, pu)
		require.NoError(t, err)

		_, err = store.Get(ctx, pu.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("nil price unit", func(t *testing.T) {
		err := store.Delete(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "price unit cannot be nil")
	})

	t.Run("delete non-existent", func(t *testing.T) {
		pu := &priceunit.PriceUnit{
			ID:   "non-existent",
			Code: "NON_EXISTENT",
			BaseModel: types.BaseModel{
				TenantID: "test-tenant",
				Status:   types.StatusPublished,
			},
		}

		err := store.Delete(ctx, pu)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestInMemoryPriceUnitStore_Clear(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPriceUnitStore()

	// Create some data
	pu1 := &priceunit.PriceUnit{
		ID:   "clear-test-1",
		Code: "CLEAR_1",
		BaseModel: types.BaseModel{
			TenantID: "test-tenant",
			Status:   types.StatusPublished,
		},
	}

	pu2 := &priceunit.PriceUnit{
		ID:   "clear-test-2",
		Code: "CLEAR_2",
		BaseModel: types.BaseModel{
			TenantID: "test-tenant",
			Status:   types.StatusPublished,
		},
	}

	_, err := store.Create(ctx, pu1)
	require.NoError(t, err)
	_, err = store.Create(ctx, pu2)
	require.NoError(t, err)

	// Verify data exists
	filter := types.NewNoLimitPriceUnitFilter()
	priceUnits, err := store.List(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, priceUnits, 2)

	// Clear the store
	store.Clear()

	// Verify data is gone
	priceUnits, err = store.List(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, priceUnits, 0)
}

func TestInMemoryPriceUnitStore_DeepCopy(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPriceUnitStore()

	pu := &priceunit.PriceUnit{
		ID:             "copy-test-id",
		Name:           "Copy Test",
		Code:           "COPY_TEST",
		Symbol:         "C",
		BaseCurrency:   "USD",
		ConversionRate: decimal.NewFromFloat(0.01),
		Precision:      2,
		EnvironmentID:  "test-env",
		Metadata:       map[string]string{"key1": "value1", "key2": "value2"},
		BaseModel: types.BaseModel{
			TenantID:  "test-tenant",
			Status:    types.StatusPublished,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	_, err := store.Create(ctx, pu)
	require.NoError(t, err)

	// Modify original
	pu.Name = "Modified Name"
	pu.Metadata["key1"] = "modified_value"
	pu.ConversionRate = decimal.NewFromFloat(0.02)

	// Verify stored version is unchanged (deep copy)
	retrieved, err := store.Get(ctx, pu.ID)
	require.NoError(t, err)
	assert.Equal(t, "Copy Test", retrieved.Name)                          // Original name
	assert.Equal(t, "value1", retrieved.Metadata["key1"])                 // Original metadata
	assert.Equal(t, decimal.NewFromFloat(0.01), retrieved.ConversionRate) // Original conversion rate
}

func TestInMemoryPriceUnitStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryPriceUnitStore()

	// Test concurrent creates
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			pu := &priceunit.PriceUnit{
				ID:   fmt.Sprintf("concurrent-test-%d", i),
				Code: fmt.Sprintf("CONCURRENT_%d", i),
				BaseModel: types.BaseModel{
					TenantID: "test-tenant",
					Status:   types.StatusPublished,
				},
			}
			_, err := store.Create(ctx, pu)
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all items were created
	filter := types.NewNoLimitPriceUnitFilter()
	priceUnits, err := store.List(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, priceUnits, 10)
}
