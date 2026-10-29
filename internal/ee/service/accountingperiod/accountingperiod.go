package accountingperiod

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainAccountingPeriod "github.com/flexprice/flexprice/internal/domain/accountingperiod"
	accountingPeriodRepo "github.com/flexprice/flexprice/internal/repository/accountingperiod"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// AccountingPeriodService resolves and materializes the periods journal entries post to.
type AccountingPeriodService interface {
	PeriodFor(ctx context.Context, businessDate time.Time) (*domainAccountingPeriod.AccountingPeriod, error)
	GetAccountingPeriods(ctx context.Context, filter *accountingPeriodRepo.Filter) (*dto.ListAccountingPeriodsResponse, error)
}

type accountingPeriodService struct {
	periodRepo accountingPeriodRepo.Repository
}

func NewAccountingPeriodService(periodRepo accountingPeriodRepo.Repository) AccountingPeriodService {
	return &accountingPeriodService{periodRepo: periodRepo}
}

// PeriodFor returns the open period containing businessDate, or the first open one after it.
// e.g. 2026-10-17 → October; if October is closed → November.
func (s *accountingPeriodService) PeriodFor(ctx context.Context, businessDate time.Time) (*domainAccountingPeriod.AccountingPeriod, error) {
	// businessDate is already a local date; keep its calendar fields rather than converting zones.
	start := time.Date(businessDate.Year(), businessDate.Month(), 1, 0, 0, 0, 0, time.UTC)
	for {
		period, err := s.periodRepo.GetOrCreate(ctx, start)
		if err != nil {
			return nil, err
		}
		if period.PeriodStatus() != types.AccountingPeriodStatusClosed {
			return period, nil
		}
		start = start.AddDate(0, 1, 0)
	}
}

func (s *accountingPeriodService) GetAccountingPeriods(ctx context.Context, filter *accountingPeriodRepo.Filter) (*dto.ListAccountingPeriodsResponse, error) {
	if filter == nil {
		filter = accountingPeriodRepo.NewFilter()
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	periods, err := s.periodRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count := len(periods)
	if !filter.IsUnlimited() {
		count, err = s.periodRepo.Count(ctx, filter)
		if err != nil {
			return nil, err
		}
	}

	return &dto.ListAccountingPeriodsResponse{
		Items: lo.Map(periods, func(p *domainAccountingPeriod.AccountingPeriod, _ int) *dto.AccountingPeriodResponse {
			return dto.NewAccountingPeriodResponse(p)
		}),
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}, nil
}
