package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type AlertSettingsServiceSuite struct {
	testutil.BaseServiceTestSuite
	alertService AlertService
	sub          *subscription.Subscription
	lineItem     *subscription.SubscriptionLineItem
	grp          *group.Group
}

func TestAlertSettingsService(t *testing.T) {
	suite.Run(t, new(AlertSettingsServiceSuite))
}

func (s *AlertSettingsServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *AlertSettingsServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *AlertSettingsServiceSuite) setupService() {
	s.alertService = NewAlertService(ServiceParams{
		Logger:                   s.GetLogger(),
		DB:                       s.GetDB(),
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		GroupRepo:                s.GetStores().GroupRepo,
		AlertRepo:                s.GetStores().AlertRepo,
	})
}

func (s *AlertSettingsServiceSuite) setupTestData() {
	ctx := s.GetContext()
	now := time.Now().UTC()

	s.sub = &subscription.Subscription{
		ID:                 "sub_alert_test",
		CustomerID:         "cust_alert_test",
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "usd",
		BillingAnchor:      now,
		StartDate:          now,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.lineItem = &subscription.SubscriptionLineItem{
		ID:             "subs_line_alert_test",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.sub.CustomerID,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        "meter_alert_test",
		DisplayName:    "API Calls",
		Quantity:       decimal.Zero,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.sub.StartDate,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.sub, []*subscription.SubscriptionLineItem{s.lineItem}))

	s.grp = &group.Group{
		ID:            "group_alert_test",
		Name:          "API Calls",
		EntityType:    types.GroupEntityTypeFeature,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().GroupRepo.Create(ctx, s.grp))
}

// fullConfig mirrors the ERD example: info < warning < critical, all "above".
func fullConfig() *types.AlertSettings {
	return &types.AlertSettings{
		AlertEnabled: lo.ToPtr(true),
		Critical:     &types.AlertThreshold{Threshold: decimal.NewFromInt(1000), Condition: types.AlertConditionAbove},
		Warning:      &types.AlertThreshold{Threshold: decimal.NewFromInt(500), Condition: types.AlertConditionAbove},
		Info:         &types.AlertThreshold{Threshold: decimal.NewFromInt(250), Condition: types.AlertConditionAbove},
	}
}

func (s *AlertSettingsServiceSuite) TestCreateAlertSettings_SubscriptionScope_Success() {
	resp, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType: types.AlertEntityTypeSubscription,
		EntityID:   s.sub.ID,
		Config:     fullConfig(),
	})

	s.NoError(err)
	s.Require().NotNil(resp)
	s.Equal(types.AlertEntityTypeSubscription, resp.EntityType)
	s.Equal(s.sub.ID, resp.EntityID)
	s.Nil(resp.ParentEntityType)
	s.True(resp.Enabled)
	s.Equal(types.StatusPublished, resp.Status)
}

func (s *AlertSettingsServiceSuite) TestCreateAlertSettings_LineItemScope_Success() {
	resp, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType:       types.AlertEntityTypeSubscriptionLineItem,
		EntityID:         s.lineItem.ID,
		ParentEntityType: types.AlertEntityTypeSubscription,
		ParentEntityID:   s.sub.ID,
		Config:           fullConfig(),
	})

	s.NoError(err)
	s.Require().NotNil(resp)
	s.Equal(types.AlertEntityTypeSubscriptionLineItem, resp.EntityType)
	s.Require().NotNil(resp.ParentEntityID)
	s.Equal(s.sub.ID, *resp.ParentEntityID)
}

func (s *AlertSettingsServiceSuite) TestCreateAlertSettings_GroupScope_Success() {
	resp, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType:       types.AlertEntityTypeGroup,
		EntityID:         s.grp.ID,
		ParentEntityType: types.AlertEntityTypeSubscription,
		ParentEntityID:   s.sub.ID,
		Config:           fullConfig(),
	})

	s.NoError(err)
	s.Require().NotNil(resp)
	s.Equal(types.AlertEntityTypeGroup, resp.EntityType)
	s.Equal(s.grp.ID, resp.EntityID)
}

func (s *AlertSettingsServiceSuite) TestCreateAlertSettings_InvalidEntityType() {
	_, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType: types.AlertEntityTypeWallet,
		EntityID:   "wallet_1",
		Config:     fullConfig(),
	})

	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *AlertSettingsServiceSuite) TestCreateAlertSettings_LineItem_MissingParent() {
	_, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType: types.AlertEntityTypeSubscriptionLineItem,
		EntityID:   s.lineItem.ID,
		Config:     fullConfig(),
	})

	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *AlertSettingsServiceSuite) TestCreateAlertSettings_Group_MissingParent() {
	_, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType: types.AlertEntityTypeGroup,
		EntityID:   s.grp.ID,
		Config:     fullConfig(),
	})

	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *AlertSettingsServiceSuite) TestCreateAlertSettings_NonExistentSubscription() {
	_, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType: types.AlertEntityTypeSubscription,
		EntityID:   "sub_does_not_exist",
		Config:     fullConfig(),
	})

	s.Error(err)
	s.True(ierr.IsNotFound(err))
}

func (s *AlertSettingsServiceSuite) TestCreateAlertSettings_LineItemNotOnSubscription() {
	otherSub := &subscription.Subscription{
		ID:                 "sub_alert_other",
		CustomerID:         "cust_alert_other",
		SubscriptionStatus: types.SubscriptionStatusActive,
		Currency:           "usd",
		BillingAnchor:      time.Now().UTC(),
		StartDate:          time.Now().UTC(),
		CurrentPeriodStart: time.Now().UTC(),
		CurrentPeriodEnd:   time.Now().UTC().AddDate(0, 1, 0),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), otherSub))

	_, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType:       types.AlertEntityTypeSubscriptionLineItem,
		EntityID:         s.lineItem.ID,
		ParentEntityType: types.AlertEntityTypeSubscription,
		ParentEntityID:   otherSub.ID,
		Config:           fullConfig(),
	})

	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *AlertSettingsServiceSuite) TestCreateAlertSettings_BelowConditionRejected() {
	config := &types.AlertSettings{
		AlertEnabled: lo.ToPtr(true),
		Info:         &types.AlertThreshold{Threshold: decimal.NewFromInt(100), Condition: types.AlertConditionBelow},
	}

	_, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType: types.AlertEntityTypeSubscription,
		EntityID:   s.sub.ID,
		Config:     config,
	})

	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *AlertSettingsServiceSuite) TestCreateAlertSettings_DuplicateRejected() {
	req := dto.CreateAlertSettingsRequest{
		EntityType: types.AlertEntityTypeSubscription,
		EntityID:   s.sub.ID,
		Config:     fullConfig(),
	}

	_, err := s.alertService.CreateAlertSettings(s.GetContext(), req)
	s.NoError(err)

	_, err = s.alertService.CreateAlertSettings(s.GetContext(), req)
	s.Error(err)
	s.True(ierr.IsAlreadyExists(err))
}

func (s *AlertSettingsServiceSuite) TestUpdateAlertSettings_PatchesConfigAndSyncsEnabled() {
	created, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType: types.AlertEntityTypeSubscription,
		EntityID:   s.sub.ID,
		Config:     fullConfig(),
	})
	s.Require().NoError(err)
	s.True(created.Enabled)

	// Info stays at 300 so it still ranks below the warning (500) / critical (1000) that survive the patch.
	disabledConfig := &types.AlertSettings{
		AlertEnabled: lo.ToPtr(false),
		Info:         &types.AlertThreshold{Threshold: decimal.NewFromInt(300), Condition: types.AlertConditionAbove},
	}

	updated, err := s.alertService.UpdateAlertSettings(s.GetContext(), created.ID, dto.UpdateAlertSettingsRequest{
		Config: disabledConfig,
	})
	s.Require().NoError(err)
	s.False(updated.Enabled)
	s.Require().NotNil(updated.Config.Info)
	s.True(updated.Config.Info.Threshold.Equal(decimal.NewFromInt(300)))

	// Fields the patch didn't mention (critical, warning) survive from the original fullConfig().
	s.Require().NotNil(updated.Config.Critical)
	s.True(updated.Config.Critical.Threshold.Equal(decimal.NewFromInt(1000)))
	s.Require().NotNil(updated.Config.Warning)
	s.True(updated.Config.Warning.Threshold.Equal(decimal.NewFromInt(500)))

	// Identity fields are untouched by update.
	s.Equal(types.AlertEntityTypeSubscription, updated.EntityType)
	s.Equal(s.sub.ID, updated.EntityID)
}

// A warning-only patch would fail validation on its own (warning needs critical), but passes once
// merged with a stored config that already has critical.
func (s *AlertSettingsServiceSuite) TestUpdateAlertSettings_PartialPatchDoesNotRequireOtherFields() {
	created, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType: types.AlertEntityTypeSubscription,
		EntityID:   s.sub.ID,
		Config:     fullConfig(),
	})
	s.Require().NoError(err)

	warningOnlyPatch := &types.AlertSettings{
		Warning: &types.AlertThreshold{Threshold: decimal.NewFromInt(600), Condition: types.AlertConditionAbove},
	}

	updated, err := s.alertService.UpdateAlertSettings(s.GetContext(), created.ID, dto.UpdateAlertSettingsRequest{
		Config: warningOnlyPatch,
	})
	s.Require().NoError(err)
	s.Require().NotNil(updated.Config.Warning)
	s.True(updated.Config.Warning.Threshold.Equal(decimal.NewFromInt(600)))
	s.Require().NotNil(updated.Config.Critical)
	s.True(updated.Config.Critical.Threshold.Equal(decimal.NewFromInt(1000)))
	s.True(updated.Enabled, "alert_enabled untouched by the patch must keep its stored value")
}

func (s *AlertSettingsServiceSuite) TestDeleteAlertSettings_SoftDeleteExcludedFromList() {
	created, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType: types.AlertEntityTypeSubscription,
		EntityID:   s.sub.ID,
		Config:     fullConfig(),
	})
	s.Require().NoError(err)

	s.NoError(s.alertService.DeleteAlertSettings(s.GetContext(), created.ID))

	_, err = s.alertService.GetAlertSettings(s.GetContext(), created.ID)
	s.Error(err)
	s.True(ierr.IsNotFound(err))

	list, err := s.alertService.ListAlertSettings(s.GetContext(), &types.AlertSettingsFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  types.AlertEntityTypeSubscription,
		EntityID:    s.sub.ID,
	})
	s.NoError(err)
	s.Empty(list.Items)
}

func (s *AlertSettingsServiceSuite) TestListAlertSettings_Filters() {
	_, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType: types.AlertEntityTypeSubscription,
		EntityID:   s.sub.ID,
		Config:     fullConfig(),
	})
	s.Require().NoError(err)

	lineItemConfig := fullConfig()
	lineItemConfig.AlertEnabled = lo.ToPtr(false)
	lineItemResp, err := s.alertService.CreateAlertSettings(s.GetContext(), dto.CreateAlertSettingsRequest{
		EntityType:       types.AlertEntityTypeSubscriptionLineItem,
		EntityID:         s.lineItem.ID,
		ParentEntityType: types.AlertEntityTypeSubscription,
		ParentEntityID:   s.sub.ID,
		Config:           lineItemConfig,
	})
	s.Require().NoError(err)

	// Filter by entity_type + parent_entity_id
	list, err := s.alertService.ListAlertSettings(s.GetContext(), &types.AlertSettingsFilter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		EntityType:       types.AlertEntityTypeSubscriptionLineItem,
		ParentEntityID:   s.sub.ID,
		ParentEntityType: types.AlertEntityTypeSubscription,
	})
	s.NoError(err)
	s.Require().Len(list.Items, 1)
	s.Equal(lineItemResp.ID, list.Items[0].ID)

	// Filter by entity_ids (batched lookup across subscriptions)
	list, err = s.alertService.ListAlertSettings(s.GetContext(), &types.AlertSettingsFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityIDs:   []string{s.sub.ID},
	})
	s.NoError(err)
	s.Require().Len(list.Items, 1)

	// Filter by enabled=false
	list, err = s.alertService.ListAlertSettings(s.GetContext(), &types.AlertSettingsFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		Enabled:     lo.ToPtr(false),
	})
	s.NoError(err)
	s.Require().Len(list.Items, 1)
	s.Equal(lineItemResp.ID, list.Items[0].ID)
}
