package ent

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/flexprice/flexprice/ent"
	domain "github.com/flexprice/flexprice/internal/domain/commitment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

var errQueryCaptured = errors.New("query captured")

// sqlCaptureDriver records the SQL Ent builds and fails every call, so no database is needed.
type sqlCaptureDriver struct {
	queries []string
}

func (d *sqlCaptureDriver) Exec(_ context.Context, query string, _, _ any) error {
	d.queries = append(d.queries, query)
	return errQueryCaptured
}

func (d *sqlCaptureDriver) Query(_ context.Context, query string, _, _ any) error {
	d.queries = append(d.queries, query)
	return errQueryCaptured
}

func (d *sqlCaptureDriver) Tx(context.Context) (dialect.Tx, error) {
	return nil, errQueryCaptured
}

func (d *sqlCaptureDriver) Close() error {
	return nil
}

func (d *sqlCaptureDriver) Dialect() string {
	return dialect.Postgres
}

func newCapturingCommitmentRepository() (domain.Repository, *sqlCaptureDriver) {
	driver := &sqlCaptureDriver{}
	client := ent.NewClient(ent.Driver(driver))

	clients := &postgres.EntClients{
		Writer: client,
		Reader: client,
	}

	return NewCommitmentRepository(postgres.NewClient(clients, nil, nil), nil), driver
}

func TestCommitmentRepository_ListAppliesFiltersAndSort(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1")

	nameEquals := &types.FilterCondition{
		Field:    lo.ToPtr("name"),
		Operator: lo.ToPtr(types.EQUAL),
		DataType: lo.ToPtr(types.DataTypeString),
		Value: &types.Value{
			String: lo.ToPtr("Annual minimum"),
		},
	}

	valueDesc := &types.SortCondition{
		Field:     "value",
		Direction: types.SortDirectionDesc,
	}

	tests := []struct {
		name        string
		filter      *domain.Filter
		wantSQL     []string
		dontWantSQL []string
		wantInvalid bool
	}{
		{
			name:   "no sort keeps the default order",
			filter: domain.NewNoLimitFilter(),
			wantSQL: []string{
				`ORDER BY "commitments"."priority" ASC, "commitments"."start_date" ASC, "commitments"."id" ASC`,
			},
		},
		{
			name: "filter conditions and sort are applied",
			filter: &domain.Filter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				Filters: []*types.FilterCondition{
					nameEquals,
				},
				Sort: []*types.SortCondition{
					valueDesc,
				},
			},
			wantSQL: []string{
				`AND "name" = $4`,
				`ORDER BY "value" DESC`,
			},
			dontWantSQL: []string{
				`ORDER BY "commitments"."priority"`,
			},
		},
		{
			name: "active window keeps open-ended commitments",
			filter: &domain.Filter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				ActiveFrom:  lo.ToPtr(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)),
				ActiveTo:    lo.ToPtr(time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)),
			},
			wantSQL: []string{
				`("commitments"."end_date" IS NULL OR "commitments"."end_date" > $`,
				`"commitments"."start_date" < $`,
			},
		},
		{
			name: "unknown filter field is a validation error",
			filter: &domain.Filter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				Filters: []*types.FilterCondition{
					{
						Field:    lo.ToPtr("no_such_column"),
						Operator: lo.ToPtr(types.EQUAL),
						DataType: lo.ToPtr(types.DataTypeString),
						Value: &types.Value{
							String: lo.ToPtr("x"),
						},
					},
				},
			},
			wantInvalid: true,
		},
		{
			name: "unknown sort field is a validation error",
			filter: &domain.Filter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				Sort: []*types.SortCondition{
					{
						Field:     "no_such_column",
						Direction: types.SortDirectionAsc,
					},
				},
			},
			wantInvalid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, driver := newCapturingCommitmentRepository()

			_, err := repo.List(ctx, tt.filter)
			require.Error(t, err)

			if tt.wantInvalid {
				require.True(t, ierr.IsValidation(err), "got %v", err)
				require.Empty(t, driver.queries)
				return
			}

			require.Len(t, driver.queries, 1)
			for _, want := range tt.wantSQL {
				require.Contains(t, driver.queries[0], want)
			}

			for _, dontWant := range tt.dontWantSQL {
				require.NotContains(t, driver.queries[0], dontWant)
			}
		})
	}
}

func TestCommitmentRepository_CountAppliesFilters(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1")
	repo, driver := newCapturingCommitmentRepository()

	filter := &domain.Filter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		Filters: []*types.FilterCondition{
			{
				Field:    lo.ToPtr("currency"),
				Operator: lo.ToPtr(types.EQUAL),
				DataType: lo.ToPtr(types.DataTypeString),
				Value: &types.Value{
					String: lo.ToPtr("usd"),
				},
			},
		},
	}

	_, err := repo.Count(ctx, filter)
	require.Error(t, err)

	require.Len(t, driver.queries, 1)
	require.Contains(t, driver.queries[0], `AND "currency" = $4`)
}

func TestCommitmentRepository_RejectsConditionWithoutValue(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1")
	repo, driver := newCapturingCommitmentRepository()

	filter := &domain.Filter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		Filters: []*types.FilterCondition{
			{
				Field:    lo.ToPtr("name"),
				Operator: lo.ToPtr(types.EQUAL),
				DataType: lo.ToPtr(types.DataTypeString),
			},
		},
	}

	_, err := repo.List(ctx, filter)
	require.Error(t, err)
	require.True(t, ierr.IsValidation(err))

	_, err = repo.Count(ctx, filter)
	require.Error(t, err)
	require.True(t, ierr.IsValidation(err))

	require.Empty(t, driver.queries)
}
