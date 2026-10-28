package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// fillPreferredConnectionIDs sets each customer's preferred connection with one association list per page.
func fillPreferredConnectionIDs(ctx context.Context, params ServiceParams, items ...*dto.CustomerResponse) error {
	if len(items) == 0 {
		return nil
	}
	filter := types.NewNoLimitConnectionAssociationFilter()
	filter.EntityType = types.ConnectionAssociationEntityTypeCustomer
	filter.EntityIDs = lo.Uniq(lo.Map(items, func(c *dto.CustomerResponse, _ int) string { return c.Customer.ID }))
	associations, err := params.ConnectionAssociationRepo.List(ctx, filter)
	if err != nil {
		return err
	}
	byCustomer := make(map[string]string, len(associations))
	for _, a := range associations {
		byCustomer[a.EntityID] = a.ConnectionID
	}
	for _, item := range items {
		if id, ok := byCustomer[item.Customer.ID]; ok {
			item.PreferredConnectionID = lo.ToPtr(id)
		}
	}
	return nil
}
