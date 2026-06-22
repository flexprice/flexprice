package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	taxassociation "github.com/flexprice/flexprice/internal/domain/taxassociation"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// ─────────────────────────────────────────────
// Tax test helpers
// ─────────────────────────────────────────────

// createTaxRate creates and saves an active percentage tax rate.
func (s *SubscriptionModificationServiceSuite) createTaxRate() *taxrate.TaxRate {
	ctx := s.GetContext()
	pct := decimal.NewFromInt(10)
	tr := &taxrate.TaxRate{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:            "Test Tax Rate",
		TaxRateStatus:   types.TaxRateStatusActive,
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: &pct,
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().TaxRateRepo.Create(ctx, tr))
	return tr
}

// createTaxAssociation creates and saves a tax association for the given subscription.
func (s *SubscriptionModificationServiceSuite) createTaxAssociation(
	taxRateID, subID string,
	startDate time.Time,
	endDate *time.Time,
) *taxassociation.TaxAssociation {
	ctx := s.GetContext()
	assoc := &taxassociation.TaxAssociation{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION),
		TaxRateID:     taxRateID,
		EntityType:    types.TaxRateEntityTypeSubscription,
		EntityID:      subID,
		StartDate:     startDate,
		EndDate:       endDate,
		Priority:      100,
		AutoApply:     true,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().TaxAssociationRepo.Create(ctx, assoc))
	return assoc
}

// ─────────────────────────────────────────────
// Tax modification tests
// ─────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) TestTaxModification() {
	type tc struct {
		name string
		run  func()
	}

	cases := []tc{
		{
			name: "add tax with effective_date in past",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("tax-add-past")
				sub := s.createActiveSub(cust.ID)
				tr := s.createTaxRate()
				past := s.GetNow().Add(-24 * time.Hour)

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeTax,
					TaxParams: &dto.SubModifyTaxParams{
						Action:        dto.SubModifyTaxActionAdd,
						TaxRateID:     &tr.ID,
						EffectiveDate: &past,
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)
				s.NotNil(resp.Subscription)

				// Verify association was created with the past start date
				filter := &types.TaxAssociationFilter{
					QueryFilter: types.NewNoLimitQueryFilter(),
					EntityType:  types.TaxRateEntityTypeSubscription,
					EntityID:    sub.ID,
					TaxRateIDs:  []string{tr.ID},
				}
				assocs, err := s.GetStores().TaxAssociationRepo.List(ctx, filter)
				s.Require().NoError(err)
				s.Require().Len(assocs, 1)
				s.True(assocs[0].StartDate.Equal(past.UTC()))
			},
		},
		{
			name: "add tax with effective_date in future",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("tax-add-future")
				sub := s.createActiveSub(cust.ID)
				tr := s.createTaxRate()
				future := s.GetNow().Add(72 * time.Hour)

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeTax,
					TaxParams: &dto.SubModifyTaxParams{
						Action:        dto.SubModifyTaxActionAdd,
						TaxRateID:     &tr.ID,
						EffectiveDate: &future,
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)
				s.NotNil(resp.Subscription)

				// Verify association was created with the future start date
				filter := &types.TaxAssociationFilter{
					QueryFilter: types.NewNoLimitQueryFilter(),
					EntityType:  types.TaxRateEntityTypeSubscription,
					EntityID:    sub.ID,
					TaxRateIDs:  []string{tr.ID},
				}
				assocs, err := s.GetStores().TaxAssociationRepo.List(ctx, filter)
				s.Require().NoError(err)
				s.Require().Len(assocs, 1)
				s.True(assocs[0].StartDate.Equal(future.UTC()))
			},
		},
		{
			name: "add tax with nil effective_date",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("tax-add-nil-date")
				sub := s.createActiveSub(cust.ID)
				tr := s.createTaxRate()

				before := time.Now().UTC().Add(-time.Second)
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeTax,
					TaxParams: &dto.SubModifyTaxParams{
						Action:    dto.SubModifyTaxActionAdd,
						TaxRateID: &tr.ID,
						// EffectiveDate is nil → should default to now
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)

				// Verify association was created with StartDate >= before
				filter := &types.TaxAssociationFilter{
					QueryFilter: types.NewNoLimitQueryFilter(),
					EntityType:  types.TaxRateEntityTypeSubscription,
					EntityID:    sub.ID,
					TaxRateIDs:  []string{tr.ID},
				}
				assocs, err := s.GetStores().TaxAssociationRepo.List(ctx, filter)
				s.Require().NoError(err)
				s.Require().Len(assocs, 1)
				s.True(!assocs[0].StartDate.Before(before), "StartDate should be >= now when EffectiveDate is nil")
			},
		},
		{
			name: "add tax — duplicate active association returns error",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("tax-add-dup")
				sub := s.createActiveSub(cust.ID)
				tr := s.createTaxRate()

				now := s.GetNow()
				// Create an existing active association starting at now
				s.createTaxAssociation(tr.ID, sub.ID, now, nil)

				// Try to add the same tax rate at the same time
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeTax,
					TaxParams: &dto.SubModifyTaxParams{
						Action:        dto.SubModifyTaxActionAdd,
						TaxRateID:     &tr.ID,
						EffectiveDate: &now,
					},
				}
				_, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().Error(err, "duplicate active association should be rejected")
			},
		},
		{
			name: "add tax — tax rate not found returns error",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("tax-add-notfound")
				sub := s.createActiveSub(cust.ID)
				bogusID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE)

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeTax,
					TaxParams: &dto.SubModifyTaxParams{
						Action:    dto.SubModifyTaxActionAdd,
						TaxRateID: &bogusID,
					},
				}
				_, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().Error(err, "unknown tax rate ID should return error")
			},
		},
		{
			name: "remove tax with effective_date in past",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("tax-rm-past")
				sub := s.createActiveSub(cust.ID)
				tr := s.createTaxRate()

				now := s.GetNow()
				past := now.Add(-48 * time.Hour)
				// Association starting 72h ago, currently open
				assoc := s.createTaxAssociation(tr.ID, sub.ID, now.Add(-72*time.Hour), nil)

				// Remove with past effective date
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeTax,
					TaxParams: &dto.SubModifyTaxParams{
						Action:           dto.SubModifyTaxActionRemove,
						TaxAssociationID: &assoc.ID,
						EffectiveDate:    &past,
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)

				// Verify EndDate was set to past
				updated, err := s.GetStores().TaxAssociationRepo.Get(ctx, assoc.ID)
				s.Require().NoError(err)
				s.Require().NotNil(updated.EndDate)
				s.True(updated.EndDate.Equal(past.UTC()))
			},
		},
		{
			name: "remove tax with effective_date in future",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("tax-rm-future")
				sub := s.createActiveSub(cust.ID)
				tr := s.createTaxRate()

				now := s.GetNow()
				future := now.Add(48 * time.Hour)
				assoc := s.createTaxAssociation(tr.ID, sub.ID, now, nil)

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeTax,
					TaxParams: &dto.SubModifyTaxParams{
						Action:           dto.SubModifyTaxActionRemove,
						TaxAssociationID: &assoc.ID,
						EffectiveDate:    &future,
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)

				// Verify EndDate was set to future
				updated, err := s.GetStores().TaxAssociationRepo.Get(ctx, assoc.ID)
				s.Require().NoError(err)
				s.Require().NotNil(updated.EndDate)
				s.True(updated.EndDate.Equal(future.UTC()))
			},
		},
		{
			name: "remove tax with nil effective_date",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("tax-rm-nil-date")
				sub := s.createActiveSub(cust.ID)
				tr := s.createTaxRate()

				now := s.GetNow()
				assoc := s.createTaxAssociation(tr.ID, sub.ID, now, nil)

				before := time.Now().UTC().Add(-time.Second)
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeTax,
					TaxParams: &dto.SubModifyTaxParams{
						Action:           dto.SubModifyTaxActionRemove,
						TaxAssociationID: &assoc.ID,
						// EffectiveDate nil → defaults to now
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)

				updated, err := s.GetStores().TaxAssociationRepo.Get(ctx, assoc.ID)
				s.Require().NoError(err)
				s.Require().NotNil(updated.EndDate)
				s.True(!updated.EndDate.Before(before), "EndDate should be >= now when EffectiveDate is nil")
			},
		},
		{
			name: "remove tax — association not found returns error",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("tax-rm-notfound")
				sub := s.createActiveSub(cust.ID)
				bogusID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION)

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeTax,
					TaxParams: &dto.SubModifyTaxParams{
						Action:           dto.SubModifyTaxActionRemove,
						TaxAssociationID: &bogusID,
					},
				}
				_, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().Error(err, "bogus association ID should return error")
			},
		},
		{
			name: "remove tax — association belongs to different subscription returns error",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("tax-rm-wrong-sub")
				sub1 := s.createActiveSub(cust.ID)
				sub2 := s.createActiveSub(cust.ID)
				tr := s.createTaxRate()

				now := s.GetNow()
				// Association belongs to sub2
				assoc := s.createTaxAssociation(tr.ID, sub2.ID, now, nil)

				// Try to remove from sub1
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeTax,
					TaxParams: &dto.SubModifyTaxParams{
						Action:           dto.SubModifyTaxActionRemove,
						TaxAssociationID: &assoc.ID,
					},
				}
				_, err := s.service.Execute(ctx, sub1.ID, req)
				s.Require().Error(err, "removing association from wrong subscription should be rejected")
			},
		},
		{
			name: "remove tax — already inactive returns error",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("tax-rm-inactive")
				sub := s.createActiveSub(cust.ID)
				tr := s.createTaxRate()

				// Create association that already ended in the past
				now := s.GetNow()
				pastStart := now.Add(-72 * time.Hour)
				pastEnd := now.Add(-24 * time.Hour)
				assoc := s.createTaxAssociation(tr.ID, sub.ID, pastStart, &pastEnd)

				effectiveDate := now
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeTax,
					TaxParams: &dto.SubModifyTaxParams{
						Action:           dto.SubModifyTaxActionRemove,
						TaxAssociationID: &assoc.ID,
						EffectiveDate:    &effectiveDate,
					},
				}
				_, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().Error(err, "removing an already-inactive association should be rejected")
			},
		},
		{
			name: "preview add tax — no DB write, returns subscription state",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("tax-preview-add")
				sub := s.createActiveSub(cust.ID)
				tr := s.createTaxRate()

				future := s.GetNow().Add(24 * time.Hour)
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeTax,
					TaxParams: &dto.SubModifyTaxParams{
						Action:        dto.SubModifyTaxActionAdd,
						TaxRateID:     &tr.ID,
						EffectiveDate: &future,
					},
				}
				resp, err := s.service.Preview(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)
				s.NotNil(resp.Subscription)

				// Verify no association was persisted
				filter := &types.TaxAssociationFilter{
					QueryFilter: types.NewNoLimitQueryFilter(),
					EntityType:  types.TaxRateEntityTypeSubscription,
					EntityID:    sub.ID,
					TaxRateIDs:  []string{tr.ID},
				}
				assocs, err := s.GetStores().TaxAssociationRepo.List(ctx, filter)
				s.Require().NoError(err)
				s.Empty(assocs, "Preview must not persist any tax association")
			},
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			tc.run()
		})
	}
}
