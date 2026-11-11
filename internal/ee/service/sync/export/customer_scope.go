package export

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const (
	maxScopedCustomers       = 10000
	maxScopedReportCustomers = 1000
)

// customerScope restricts exports to filtered customers.
type customerScope struct {
	repo customer.Repository
	max  int
}

func newCustomerScope(repo customer.Repository, max int) customerScope {
	return customerScope{repo: repo, max: max}
}

// scoped is false without a filter.
func (s customerScope) resolve(ctx context.Context, request *dto.ExportRequest) (refs []customer.Ref, scoped bool, err error) {
	filter := request.SourceConfig.GetCustomerFilter()
	if filter == nil {
		return nil, false, nil
	}
	if s.repo == nil {
		return nil, true, ierr.NewError("customer repository not configured").
			WithHint("Customer filters are unavailable").
			Mark(ierr.ErrSystem)
	}
	ctx = types.SetTenantID(ctx, request.TenantID)
	ctx = types.SetEnvironmentID(ctx, request.EnvID)
	refs, err = s.repo.ListRefsByFilter(ctx, filter, s.max)
	return refs, true, err
}

// idListFilter keeps the stored filter's status.
func idListFilter(stored *types.CustomerFilter, ids []string) *types.CustomerFilter {
	f := &types.CustomerFilter{QueryFilter: types.NewNoLimitQueryFilter(), CustomerIDs: ids}
	status := stored.GetStatus()
	if status == "" {
		f.Filters = []*types.FilterCondition{}
		return f
	}
	f.QueryFilter.Status = lo.ToPtr(types.Status(status))
	return f
}

func refIDs(refs []customer.Ref) []string {
	return lo.Map(refs, func(r customer.Ref, _ int) string { return r.ID })
}

func refExternalIDs(refs []customer.Ref) []string {
	return lo.Map(refs, func(r customer.Ref, _ int) string { return r.ExternalID })
}
