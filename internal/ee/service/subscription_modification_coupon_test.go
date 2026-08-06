package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	coupon_domain "github.com/flexprice/flexprice/internal/domain/coupon"
	coupon_association "github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// ─────────────────────────────────────────────
// Coupon test helpers
// ─────────────────────────────────────────────

// createCoupon creates and saves a published percentage-off coupon.
func (s *SubscriptionModificationServiceSuite) createCoupon() *coupon_domain.Coupon {
	ctx := s.GetContext()
	pct := decimal.NewFromInt(10)
	id := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON)
	c := &coupon_domain.Coupon{
		ID:            id,
		Name:          "Test Coupon",
		Type:          types.CouponTypePercentage,
		Cadence:       types.CouponCadenceForever,
		PercentageOff: &pct,
		CouponCode:    lo.ToPtr(id),
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	c.Status = types.StatusPublished
	s.Require().NoError(s.GetStores().CouponRepo.Create(ctx, c))
	return c
}

// createCouponAssociation creates and saves a coupon association for the given subscription.
func (s *SubscriptionModificationServiceSuite) createCouponAssociation(
	couponID, subID string,
	startDate time.Time,
	endDate *time.Time,
) *coupon_association.CouponAssociation {
	ctx := s.GetContext()
	assoc := &coupon_association.CouponAssociation{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
		CouponID:       couponID,
		SubscriptionID: subID,
		StartDate:      startDate,
		EndDate:        endDate,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CouponAssociationRepo.Create(ctx, assoc))
	return assoc
}

// ─────────────────────────────────────────────
// Coupon modification tests
// ─────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) TestCouponModification() {
	type tc struct {
		name string
		run  func()
	}

	cases := []tc{
		{
			name: "add coupon with start_date in past",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-add-past")
				sub := s.createActiveSub(cust.ID)
				c := s.createCoupon()
				past := s.GetNow().Add(-24 * time.Hour)

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:     dto.SubModifyCouponActionAdd,
						CouponCode: c.CouponCode,
						StartDate:  &past,
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)
				s.NotNil(resp.Subscription)

				// Verify association was created with the past start date
				filter := &types.CouponAssociationFilter{
					QueryFilter:     types.NewNoLimitQueryFilter(),
					SubscriptionIDs: []string{sub.ID},
					CouponIDs:       []string{c.ID},
				}
				assocs, err := s.GetStores().CouponAssociationRepo.List(ctx, filter)
				s.Require().NoError(err)
				s.Require().Len(assocs, 1)
				s.True(assocs[0].StartDate.Equal(past.UTC()))
			},
		},
		{
			name: "add coupon with start_date in future",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-add-future")
				sub := s.createActiveSub(cust.ID)
				c := s.createCoupon()
				future := s.GetNow().Add(72 * time.Hour)

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:     dto.SubModifyCouponActionAdd,
						CouponCode: c.CouponCode,
						StartDate:  &future,
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)
				s.NotNil(resp.Subscription)

				// Verify association was created with the future start date
				filter := &types.CouponAssociationFilter{
					QueryFilter:     types.NewNoLimitQueryFilter(),
					SubscriptionIDs: []string{sub.ID},
					CouponIDs:       []string{c.ID},
				}
				assocs, err := s.GetStores().CouponAssociationRepo.List(ctx, filter)
				s.Require().NoError(err)
				s.Require().Len(assocs, 1)
				s.True(assocs[0].StartDate.Equal(future.UTC()))
			},
		},
		{
			name: "add coupon with no start_date defaults to now",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-add-nil-date")
				sub := s.createActiveSub(cust.ID)
				c := s.createCoupon()

				before := time.Now().UTC().Add(-time.Second)
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:     dto.SubModifyCouponActionAdd,
						CouponCode: c.CouponCode,
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)

				// Verify association was created with StartDate >= before
				filter := &types.CouponAssociationFilter{
					QueryFilter:     types.NewNoLimitQueryFilter(),
					SubscriptionIDs: []string{sub.ID},
					CouponIDs:       []string{c.ID},
				}
				assocs, err := s.GetStores().CouponAssociationRepo.List(ctx, filter)
				s.Require().NoError(err)
				s.Require().Len(assocs, 1)
				s.True(!assocs[0].StartDate.Before(before), "StartDate should be >= now when no start_date is provided")
			},
		},
		{
			name: "add coupon — duplicate active association returns error",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-add-dup")
				sub := s.createActiveSub(cust.ID)
				c := s.createCoupon()

				now := s.GetNow()
				// Create an existing active association starting at now
				s.createCouponAssociation(c.ID, sub.ID, now, nil)

				// Try to add the same coupon at the same time
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:     dto.SubModifyCouponActionAdd,
						CouponCode: c.CouponCode,
					},
				}
				_, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().Error(err, "duplicate active association should be rejected")
			},
		},
		{
			name: "add coupon — coupon not found returns error",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-add-notfound")
				sub := s.createActiveSub(cust.ID)

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:     dto.SubModifyCouponActionAdd,
						CouponCode: lo.ToPtr("BOGUS-NONEXISTENT-CODE-XYZ"),
					},
				}
				_, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().Error(err, "unknown coupon code should return error")
			},
		},
		{
			name: "remove coupon — sets end_date to now",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-rm-now")
				sub := s.createActiveSub(cust.ID)
				c := s.createCoupon()

				now := s.GetNow()
				assoc := s.createCouponAssociation(c.ID, sub.ID, now, nil)

				before := time.Now().UTC().Add(-time.Second)
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:              dto.SubModifyCouponActionRemove,
						CouponAssociationID: &assoc.ID,
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)

				updated, err := s.GetStores().CouponAssociationRepo.Get(ctx, assoc.ID)
				s.Require().NoError(err)
				s.Require().NotNil(updated.EndDate)
				s.True(!updated.EndDate.Before(before), "EndDate should be set to approximately now")
			},
		},
		{
			name: "remove coupon with explicit end_date in future — sets association end_date to specified future date",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-rm-future-end")
				sub := s.createActiveSub(cust.ID)
				c := s.createCoupon()

				now := s.GetNow()
				assoc := s.createCouponAssociation(c.ID, sub.ID, now, nil)

				futureEnd := now.Add(30 * 24 * time.Hour)
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:              dto.SubModifyCouponActionRemove,
						CouponAssociationID: &assoc.ID,
						EndDate:             &futureEnd,
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)

				updated, err := s.GetStores().CouponAssociationRepo.Get(ctx, assoc.ID)
				s.Require().NoError(err)
				s.Require().NotNil(updated.EndDate)
				s.True(updated.EndDate.Equal(futureEnd.UTC()), "EndDate should be set to the explicit future end_date")
			},
		},
		{
			name: "remove coupon with explicit end_date in past — backdates the association end_date",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-rm-past-end")
				sub := s.createActiveSub(cust.ID)
				c := s.createCoupon()

				// Association started 72 hours ago, still open.
				pastStart := s.GetNow().Add(-72 * time.Hour)
				assoc := s.createCouponAssociation(c.ID, sub.ID, pastStart, nil)

				// Request removal effective 24 hours ago (backdate).
				pastEnd := s.GetNow().Add(-24 * time.Hour)
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:              dto.SubModifyCouponActionRemove,
						CouponAssociationID: &assoc.ID,
						EndDate:             &pastEnd,
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)

				updated, err := s.GetStores().CouponAssociationRepo.Get(ctx, assoc.ID)
				s.Require().NoError(err)
				s.Require().NotNil(updated.EndDate)
				s.True(updated.EndDate.Equal(pastEnd.UTC()), "EndDate should be backdated to the explicit past end_date")
			},
		},
		{
			name: "remove coupon — association not found returns error",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-rm-notfound")
				sub := s.createActiveSub(cust.ID)
				bogusID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION)

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:              dto.SubModifyCouponActionRemove,
						CouponAssociationID: &bogusID,
					},
				}
				_, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().Error(err, "bogus association ID should return error")
			},
		},
		{
			name: "remove coupon — association belongs to different subscription returns error",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-rm-wrong-sub")
				sub1 := s.createActiveSub(cust.ID)
				sub2 := s.createActiveSub(cust.ID)
				c := s.createCoupon()

				now := s.GetNow()
				// Association belongs to sub2
				assoc := s.createCouponAssociation(c.ID, sub2.ID, now, nil)

				// Try to remove from sub1
				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:              dto.SubModifyCouponActionRemove,
						CouponAssociationID: &assoc.ID,
					},
				}
				_, err := s.service.Execute(ctx, sub1.ID, req)
				s.Require().Error(err, "removing association from wrong subscription should be rejected")
			},
		},
		{
			name: "remove coupon — already inactive returns error",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-rm-inactive")
				sub := s.createActiveSub(cust.ID)
				c := s.createCoupon()

				// Create association that already ended in the past
				now := s.GetNow()
				pastStart := now.Add(-72 * time.Hour)
				pastEnd := now.Add(-24 * time.Hour)
				assoc := s.createCouponAssociation(c.ID, sub.ID, pastStart, &pastEnd)

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:              dto.SubModifyCouponActionRemove,
						CouponAssociationID: &assoc.ID,
					},
				}
				_, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().Error(err, "removing an already-inactive association should be rejected")
			},
		},
		{
			name: "preview add coupon — no DB write, returns subscription state",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-preview-add")
				sub := s.createActiveSub(cust.ID)
				c := s.createCoupon()

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:     dto.SubModifyCouponActionAdd,
						CouponCode: c.CouponCode,
					},
				}
				resp, err := s.service.Preview(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)
				s.NotNil(resp.Subscription)

				// Verify no association was persisted
				filter := &types.CouponAssociationFilter{
					QueryFilter:     types.NewNoLimitQueryFilter(),
					SubscriptionIDs: []string{sub.ID},
					CouponIDs:       []string{c.ID},
				}
				assocs, err := s.GetStores().CouponAssociationRepo.List(ctx, filter)
				s.Require().NoError(err)
				s.Empty(assocs, "Preview must not persist any coupon association")
			},
		},
		// ── New test cases for subscription_id / subscription_line_item_id targeting ──
		{
			name: "add coupon at subscription level via subscription_id",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-add-sub-id")
				sub := s.createActiveSub(cust.ID)
				c := s.createCoupon()

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:         dto.SubModifyCouponActionAdd,
						CouponCode:     c.CouponCode,
						SubscriptionID: &sub.ID,
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)

				filter := &types.CouponAssociationFilter{
					QueryFilter:     types.NewNoLimitQueryFilter(),
					SubscriptionIDs: []string{sub.ID},
					CouponIDs:       []string{c.ID},
				}
				assocs, err := s.GetStores().CouponAssociationRepo.List(ctx, filter)
				s.Require().NoError(err)
				s.Require().Len(assocs, 1)
				s.Nil(assocs[0].SubscriptionLineItemID, "should be subscription-level (no line item)")
			},
		},
		{
			name: "add coupon at line-item level via subscription_line_item_id",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-add-li-id")
				sub := s.createActiveSub(cust.ID)
				li := s.createFixedLineItem(sub.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceArrear)
				c := s.createCoupon()

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:                 dto.SubModifyCouponActionAdd,
						CouponCode:             c.CouponCode,
						SubscriptionLineItemID: &li.ID,
					},
				}
				resp, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().NoError(err)
				s.Require().NotNil(resp)

				filter := &types.CouponAssociationFilter{
					QueryFilter:     types.NewNoLimitQueryFilter(),
					SubscriptionIDs: []string{sub.ID},
					CouponIDs:       []string{c.ID},
				}
				assocs, err := s.GetStores().CouponAssociationRepo.List(ctx, filter)
				s.Require().NoError(err)
				s.Require().Len(assocs, 1)
				s.Require().NotNil(assocs[0].SubscriptionLineItemID)
				s.Equal(li.ID, *assocs[0].SubscriptionLineItemID)
			},
		},
		{
			name: "add coupon — both subscription_id and subscription_line_item_id provided",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-add-both-fields")
				sub := s.createActiveSub(cust.ID)
				c := s.createCoupon()
				fakeLineItemID := types.GenerateUUIDWithPrefix("sli")

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:                 dto.SubModifyCouponActionAdd,
						CouponCode:             c.CouponCode,
						SubscriptionID:         &sub.ID,
						SubscriptionLineItemID: &fakeLineItemID,
					},
				}
				_, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().Error(err, "providing both subscription_id and subscription_line_item_id should fail validation")
			},
		},
		{
			name: "add coupon — subscription_line_item_id not on this subscription",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-add-wrong-li")
				sub1 := s.createActiveSub(cust.ID)
				sub2 := s.createActiveSub(cust.ID)
				// Create a line item belonging to sub2
				li := s.createFixedLineItem(sub2.ID, cust.ID, decimal.NewFromInt(1), types.InvoiceCadenceArrear)
				c := s.createCoupon()

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:                 dto.SubModifyCouponActionAdd,
						CouponCode:             c.CouponCode,
						SubscriptionLineItemID: &li.ID,
					},
				}
				// Apply to sub1, but li belongs to sub2
				_, err := s.service.Execute(ctx, sub1.ID, req)
				s.Require().Error(err, "line item from different subscription should be rejected")
			},
		},
		{
			name: "add coupon — subscription_id mismatch",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-add-sub-mismatch")
				sub1 := s.createActiveSub(cust.ID)
				sub2 := s.createActiveSub(cust.ID)
				c := s.createCoupon()

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:         dto.SubModifyCouponActionAdd,
						CouponCode:     c.CouponCode,
						SubscriptionID: &sub2.ID, // mismatch: sub2 != sub1
					},
				}
				_, err := s.service.Execute(ctx, sub1.ID, req)
				s.Require().Error(err, "subscription_id mismatch should return error")
			},
		},
		{
			name: "add coupon — coupon_code required",
			run: func() {
				ctx := s.GetContext()
				cust := s.createCustomer("coup-add-no-code")
				sub := s.createActiveSub(cust.ID)

				req := dto.ExecuteSubscriptionModifyRequest{
					Type: dto.SubscriptionModifyTypeCoupon,
					CouponParams: &dto.SubModifyCouponParams{
						Action:     dto.SubModifyCouponActionAdd,
						CouponCode: nil, // no code provided
					},
				}
				_, err := s.service.Execute(ctx, sub.ID, req)
				s.Require().Error(err, "missing coupon_code should fail validation")
			},
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			tc.run()
		})
	}
}

// createCouponWithLimits creates a published percentage-off coupon with the
// given redemption controls, for the VAPT redemption-enforcement tests.
func (s *SubscriptionModificationServiceSuite) createCouponWithLimits(
	maxRedemptions *int,
	totalRedemptions int,
	redeemAfter, redeemBefore *time.Time,
) *coupon_domain.Coupon {
	ctx := s.GetContext()
	pct := decimal.NewFromInt(10)
	id := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON)
	c := &coupon_domain.Coupon{
		ID:               id,
		Name:             "Limited Coupon",
		Type:             types.CouponTypePercentage,
		Cadence:          types.CouponCadenceForever,
		PercentageOff:    &pct,
		CouponCode:       lo.ToPtr(id),
		MaxRedemptions:   maxRedemptions,
		TotalRedemptions: totalRedemptions,
		RedeemAfter:      redeemAfter,
		RedeemBefore:     redeemBefore,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	c.Status = types.StatusPublished
	s.Require().NoError(s.GetStores().CouponRepo.Create(ctx, c))
	return c
}

// TestCouponRedemptionEnforcement covers the VAPT finding that the
// subscription modify/execute add-coupon path did not enforce max_redemptions,
// redeem_after, or redeem_before, and never incremented total_redemptions.
func (s *SubscriptionModificationServiceSuite) TestCouponRedemptionEnforcement() {
	s.Run("max_redemptions already reached — add is rejected", func() {
		ctx := s.GetContext()
		cust := s.createCustomer("coup-max-reached")
		sub := s.createActiveSub(cust.ID)
		// max=2, already at 2 → limit reached.
		c := s.createCouponWithLimits(lo.ToPtr(2), 2, nil, nil)

		req := dto.ExecuteSubscriptionModifyRequest{
			Type: dto.SubscriptionModifyTypeCoupon,
			CouponParams: &dto.SubModifyCouponParams{
				Action:     dto.SubModifyCouponActionAdd,
				CouponCode: c.CouponCode,
			},
		}
		_, err := s.service.Execute(ctx, sub.ID, req)
		s.Require().Error(err, "coupon at max_redemptions must not be redeemable")

		// No association should have been created.
		assocs, listErr := s.GetStores().CouponAssociationRepo.List(ctx, &types.CouponAssociationFilter{
			QueryFilter:     types.NewNoLimitQueryFilter(),
			SubscriptionIDs: []string{sub.ID},
			CouponIDs:       []string{c.ID},
		})
		s.Require().NoError(listErr)
		s.Len(assocs, 0)
	})

	s.Run("redeem_after in the future — add is rejected", func() {
		ctx := s.GetContext()
		cust := s.createCustomer("coup-not-yet-valid")
		sub := s.createActiveSub(cust.ID)
		future := s.GetNow().Add(72 * time.Hour)
		c := s.createCouponWithLimits(nil, 0, &future, nil)

		req := dto.ExecuteSubscriptionModifyRequest{
			Type: dto.SubscriptionModifyTypeCoupon,
			CouponParams: &dto.SubModifyCouponParams{
				Action:     dto.SubModifyCouponActionAdd,
				CouponCode: c.CouponCode,
			},
		}
		_, err := s.service.Execute(ctx, sub.ID, req)
		s.Require().Error(err, "coupon before its redeem_after must not be redeemable")
	})

	s.Run("redeem_before in the past — add is rejected", func() {
		ctx := s.GetContext()
		cust := s.createCustomer("coup-expired")
		sub := s.createActiveSub(cust.ID)
		past := s.GetNow().Add(-72 * time.Hour)
		c := s.createCouponWithLimits(nil, 0, nil, &past)

		req := dto.ExecuteSubscriptionModifyRequest{
			Type: dto.SubscriptionModifyTypeCoupon,
			CouponParams: &dto.SubModifyCouponParams{
				Action:     dto.SubModifyCouponActionAdd,
				CouponCode: c.CouponCode,
			},
		}
		_, err := s.service.Execute(ctx, sub.ID, req)
		s.Require().Error(err, "coupon past its redeem_before must not be redeemable")
	})

	s.Run("successful add increments total_redemptions", func() {
		ctx := s.GetContext()
		cust := s.createCustomer("coup-increments")
		sub := s.createActiveSub(cust.ID)
		c := s.createCouponWithLimits(lo.ToPtr(5), 0, nil, nil)

		req := dto.ExecuteSubscriptionModifyRequest{
			Type: dto.SubscriptionModifyTypeCoupon,
			CouponParams: &dto.SubModifyCouponParams{
				Action:     dto.SubModifyCouponActionAdd,
				CouponCode: c.CouponCode,
			},
		}
		_, err := s.service.Execute(ctx, sub.ID, req)
		s.Require().NoError(err)

		updated, getErr := s.GetStores().CouponRepo.Get(ctx, c.ID)
		s.Require().NoError(getErr)
		s.Equal(1, updated.TotalRedemptions, "total_redemptions must be incremented on redemption")
	})
}
