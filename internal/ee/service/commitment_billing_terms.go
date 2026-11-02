package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// planInstalments expands terms into the commitment's instalments, valuing each commitment period with amountOf.
// terms must be new: expanding fills the amount of a percentage term.
func (s *commitmentService) planInstalments(
	ctx context.Context,
	c *commitment.Commitment,
	cc *commitmentContext,
	amountOf func(capacity decimal.Decimal) decimal.Decimal,
	terms []*billingterm.Term,
) ([]*billingterm.Instalment, error) {
	periods, err := commitmentPeriods(c, cc.Sub, amountOf, bucketFor(c, cc), c.StartDate, *c.EndDate)
	if err != nil {
		return nil, err
	}

	values := lo.Map(periods, func(p commitmentPeriod, _ int) billingterm.PeriodValue {
		return billingterm.PeriodValue{
			Period: types.Period{Start: p.Start, End: p.End},
			Amount: p.Amount,
		}
	})

	return billingterm.Expand(ctx, c, values, terms)
}
