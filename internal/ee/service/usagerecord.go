package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// UsageRecordService reads the usage_records table written by the marketplace snapshot cron and
// reported by the marketplace reporting cron.
type UsageRecordService interface {
	ListUsageRecords(ctx context.Context, filter *types.UsageRecordFilter) (*dto.ListUsageRecordsResponse, error)
}

type usageRecordService struct {
	ServiceParams
}

// NewUsageRecordService creates a new usage record service.
func NewUsageRecordService(params ServiceParams) UsageRecordService {
	return &usageRecordService{ServiceParams: params}
}

func (s *usageRecordService) ListUsageRecords(ctx context.Context, filter *types.UsageRecordFilter) (*dto.ListUsageRecordsResponse, error) {
	if filter == nil {
		filter = types.NewUsageRecordFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	// GetLimit() is 0 both for an unset limit and for an explicitly unlimited filter, so the
	// unlimited case has to be read before normalizing and reused for the count below.
	isUnlimited := filter.IsUnlimited()
	if !isUnlimited && filter.GetLimit() <= 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	records, err := s.UsageRecordRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.UsageRecordResponse, len(records))
	for i, record := range records {
		responses[i] = &dto.UsageRecordResponse{UsageRecord: record}
	}

	// len(records) is only the current page's count; an unlimited filter already returns every
	// matching row, but a paginated one needs the true total from a separate count query.
	total := len(responses)
	if !isUnlimited {
		total, err = s.UsageRecordRepo.Count(ctx, filter)
		if err != nil {
			return nil, err
		}
	}

	return &dto.ListUsageRecordsResponse{
		Items: responses,
		Pagination: types.NewPaginationResponse(
			total,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}, nil
}
