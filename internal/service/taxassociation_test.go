package service

import (
	"fmt"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type TaxAssociationServiceSuite struct {
	testutil.BaseServiceTestSuite
	taxService    TaxService
	configService TaxAssociationService
}

func TestTaxAssociationService(t *testing.T) {
	suite.Run(t, new(TaxAssociationServiceSuite))
}

func (s *TaxAssociationServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	serviceParams := ServiceParams{
		DB:                 s.GetDB(),
		TaxRateRepo:        s.GetStores().TaxRateRepo,
		TaxAssociationRepo: s.GetStores().TaxAssociationRepo,
		Logger:             s.GetLogger(),
	}

	s.taxService = NewTaxService(serviceParams)
	s.configService = NewTaxAssociationService(serviceParams)
}

// Helper function to create a tax rate in the store
func (s *TaxAssociationServiceSuite) createTaxRateInStore(taxRate *taxrate.TaxRate) {
	err := s.GetStores().TaxRateRepo.Create(s.GetContext(), taxRate)
	s.NoError(err)
}

// Helper function to create a sample tax rate
func (s *TaxAssociationServiceSuite) createSampleTaxRate(code string) *taxrate.TaxRate {
	taxRate := &taxrate.TaxRate{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:            fmt.Sprintf("Test Tax Rate %s", code),
		Code:            code,
		TaxRateType:     types.TaxRateTypePercentage,
		PercentageValue: lo.ToPtr(decimal.NewFromFloat(10.0)),
		Currency:        "USD",
		TaxRateStatus:   types.TaxRateStatusActive,
		Scope:           types.TaxRateScopeExternal,
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.createTaxRateInStore(taxRate)
	return taxRate
}

// Helper function to create a valid tax association request
func (s *TaxAssociationServiceSuite) createTaxAssociationRequest(taxRateID, entityID string, entityType types.TaxrateEntityType) *dto.CreateTaxAssociationRequest {
	return &dto.CreateTaxAssociationRequest{
		TaxRateID:  taxRateID,
		EntityType: entityType,
		EntityID:   entityID,
		Priority:   100,
		AutoApply:  true,
	}
}

func (s *TaxAssociationServiceSuite) TestCreateTaxAssociation() {
	// Create a tax rate first
	taxRate := s.createSampleTaxRate("TEST_TAX")

	s.Run("Valid Tax Association - Customer", func() {
		req := s.createTaxAssociationRequest(taxRate.ID, "customer-123", types.TaxrateEntityTypeCustomer)

		resp, err := s.configService.Create(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(req.TaxRateID, resp.TaxRateID)
		s.Equal(req.EntityType, resp.EntityType)
		s.Equal(req.EntityID, resp.EntityID)
		s.Equal(req.Priority, resp.Priority)
		s.Equal(req.AutoApply, resp.AutoApply)
	})

	s.Run("Valid Tax Association - Subscription", func() {
		req := s.createTaxAssociationRequest(taxRate.ID, "subscription-456", types.TaxrateEntityTypeSubscription)
		req.Priority = 50
		req.AutoApply = false

		resp, err := s.configService.Create(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(req.TaxRateID, resp.TaxRateID)
		s.Equal(req.EntityType, resp.EntityType)
		s.Equal(req.EntityID, resp.EntityID)
		s.Equal(req.Priority, resp.Priority)
		s.Equal(req.AutoApply, resp.AutoApply)
	})

	s.Run("Valid Tax Association - Invoice", func() {
		req := s.createTaxAssociationRequest(taxRate.ID, "invoice-789", types.TaxrateEntityTypeInvoice)
		req.Priority = 1

		resp, err := s.configService.Create(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(req.EntityType, resp.EntityType)
		s.Equal(req.EntityID, resp.EntityID)
		s.Equal(req.Priority, resp.Priority)
	})

	s.Run("Valid Tax Association - Tenant", func() {
		req := s.createTaxAssociationRequest(taxRate.ID, "tenant-999", types.TaxrateEntityTypeTenant)

		resp, err := s.configService.Create(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(req.EntityType, resp.EntityType)
		s.Equal(req.EntityID, resp.EntityID)
	})

	s.Run("Invalid - Empty Tax Rate ID", func() {
		req := s.createTaxAssociationRequest("", "entity-123", types.TaxrateEntityTypeCustomer)

		resp, err := s.configService.Create(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("Invalid - Non-existent Tax Rate ID", func() {
		req := s.createTaxAssociationRequest("non-existent-tax-rate", "entity-123", types.TaxrateEntityTypeCustomer)

		resp, err := s.configService.Create(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("Invalid - Empty Entity ID", func() {
		req := s.createTaxAssociationRequest(taxRate.ID, "", types.TaxrateEntityTypeCustomer)

		resp, err := s.configService.Create(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("Invalid - Invalid Entity Type", func() {
		req := &dto.CreateTaxAssociationRequest{
			TaxRateID:  taxRate.ID,
			EntityType: "invalid_entity_type",
			EntityID:   "entity-123",
			Priority:   100,
			AutoApply:  true,
		}

		resp, err := s.configService.Create(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("Invalid - Negative Priority", func() {
		req := s.createTaxAssociationRequest(taxRate.ID, "entity-123", types.TaxrateEntityTypeCustomer)
		req.Priority = -1

		resp, err := s.configService.Create(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
	})
}

func (s *TaxAssociationServiceSuite) TestGetTaxAssociation() {
	// Create a tax rate and tax association first
	taxRate := s.createSampleTaxRate("GET_TAX")

	createReq := s.createTaxAssociationRequest(taxRate.ID, "customer-get", types.TaxrateEntityTypeCustomer)
	createResp, err := s.configService.Create(s.GetContext(), createReq)
	s.NoError(err)
	s.NotNil(createResp)

	s.Run("Valid Tax Association ID", func() {
		resp, err := s.configService.Get(s.GetContext(), createResp.ID)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(createResp.ID, resp.ID)
		s.Equal(createResp.TaxRateID, resp.TaxRateID)
		s.Equal(createResp.EntityType, resp.EntityType)
		s.Equal(createResp.EntityID, resp.EntityID)
	})

	s.Run("Invalid - Empty Tax Association ID", func() {
		resp, err := s.configService.Get(s.GetContext(), "")
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("Invalid - Non-existent Tax Association ID", func() {
		resp, err := s.configService.Get(s.GetContext(), "non-existent-id")
		s.Error(err)
		s.Nil(resp)
	})
}

func (s *TaxAssociationServiceSuite) TestListTaxAssociations() {
	// Create multiple tax rates and associations
	taxRate1 := s.createSampleTaxRate("LIST_TAX_1")
	taxRate2 := s.createSampleTaxRate("LIST_TAX_2")

	// Create associations for different entities
	associations := []*dto.CreateTaxAssociationRequest{
		s.createTaxAssociationRequest(taxRate1.ID, "customer-1", types.TaxrateEntityTypeCustomer),
		s.createTaxAssociationRequest(taxRate1.ID, "customer-2", types.TaxrateEntityTypeCustomer),
		s.createTaxAssociationRequest(taxRate2.ID, "subscription-1", types.TaxrateEntityTypeSubscription),
		s.createTaxAssociationRequest(taxRate2.ID, "invoice-1", types.TaxrateEntityTypeInvoice),
	}

	for _, req := range associations {
		_, err := s.configService.Create(s.GetContext(), req)
		s.NoError(err)
	}

	s.Run("List All Tax Associations", func() {
		filter := types.NewTaxAssociationFilter()
		resp, err := s.configService.List(s.GetContext(), filter)
		s.NoError(err)
		s.NotNil(resp)
		s.GreaterOrEqual(len(resp.Items), 4)
		s.NotNil(resp.Pagination)
	})

	s.Run("List Tax Associations by Entity Type", func() {
		filter := types.NewTaxAssociationFilter()
		filter.EntityType = types.TaxrateEntityTypeCustomer
		resp, err := s.configService.List(s.GetContext(), filter)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(2, len(resp.Items))
		for _, item := range resp.Items {
			s.Equal(types.TaxrateEntityTypeCustomer, item.EntityType)
		}
	})

	s.Run("List Tax Associations by Entity ID", func() {
		filter := types.NewTaxAssociationFilter()
		filter.EntityID = "customer-1"
		resp, err := s.configService.List(s.GetContext(), filter)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(1, len(resp.Items))
		s.Equal("customer-1", resp.Items[0].EntityID)
	})

	s.Run("List Tax Associations by Tax Rate ID", func() {
		filter := types.NewTaxAssociationFilter()
		filter.TaxRateIDs = []string{taxRate1.ID}
		resp, err := s.configService.List(s.GetContext(), filter)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(2, len(resp.Items))
		for _, item := range resp.Items {
			s.Equal(taxRate1.ID, item.TaxRateID)
		}
	})

	s.Run("List Tax Associations with Auto Apply Filter", func() {
		filter := types.NewTaxAssociationFilter()
		filter.AutoApply = lo.ToPtr(true)
		resp, err := s.configService.List(s.GetContext(), filter)
		s.NoError(err)
		s.NotNil(resp)
		for _, item := range resp.Items {
			s.True(item.AutoApply)
		}
	})

	s.Run("List Tax Associations with Pagination", func() {
		filter := types.NewTaxAssociationFilter()
		filter.Limit = lo.ToPtr(2)
		filter.Offset = lo.ToPtr(0)
		resp, err := s.configService.List(s.GetContext(), filter)
		s.NoError(err)
		s.NotNil(resp)
		s.LessOrEqual(len(resp.Items), 2)
	})

	s.Run("List Tax Associations with Nil Filter", func() {
		resp, err := s.configService.List(s.GetContext(), nil)
		s.NoError(err)
		s.NotNil(resp)
		s.GreaterOrEqual(len(resp.Items), 4)
	})
}

func (s *TaxAssociationServiceSuite) TestUpdateTaxAssociation() {
	// Create a tax rate and tax association first
	taxRate := s.createSampleTaxRate("UPDATE_TAX")

	createReq := s.createTaxAssociationRequest(taxRate.ID, "customer-update", types.TaxrateEntityTypeCustomer)
	createResp, err := s.configService.Create(s.GetContext(), createReq)
	s.NoError(err)
	s.NotNil(createResp)

	s.Run("Valid Update - Priority and Auto Apply", func() {
		req := &dto.TaxAssociationUpdateRequest{
			Priority:  10,
			AutoApply: false,
		}

		resp, err := s.configService.Update(s.GetContext(), createResp.ID, req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(req.Priority, resp.Priority)
		s.Equal(req.AutoApply, resp.AutoApply)
	})

	s.Run("Valid Update - Metadata", func() {
		req := &dto.TaxAssociationUpdateRequest{
			Priority:  20,
			AutoApply: true,
			Metadata: map[string]string{
				"updated":    "true",
				"reason":     "tax_law_change",
				"updated_by": "admin",
			},
		}

		resp, err := s.configService.Update(s.GetContext(), createResp.ID, req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(req.Priority, resp.Priority)
		s.Equal(req.AutoApply, resp.AutoApply)
		s.Equal(req.Metadata, resp.Metadata)
	})

	s.Run("Invalid - Empty Tax Association ID", func() {
		req := &dto.TaxAssociationUpdateRequest{
			Priority: 30,
		}

		resp, err := s.configService.Update(s.GetContext(), "", req)
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("Invalid - Non-existent Tax Association ID", func() {
		req := &dto.TaxAssociationUpdateRequest{
			Priority: 30,
		}

		resp, err := s.configService.Update(s.GetContext(), "non-existent-id", req)
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("Invalid - Negative Priority", func() {
		req := &dto.TaxAssociationUpdateRequest{
			Priority: -5,
		}

		resp, err := s.configService.Update(s.GetContext(), createResp.ID, req)
		s.Error(err)
		s.Nil(resp)
	})
}

func (s *TaxAssociationServiceSuite) TestDeleteTaxAssociation() {
	// Create a tax rate and tax association first
	taxRate := s.createSampleTaxRate("DELETE_TAX")

	createReq := s.createTaxAssociationRequest(taxRate.ID, "customer-delete", types.TaxrateEntityTypeCustomer)
	createResp, err := s.configService.Create(s.GetContext(), createReq)
	s.NoError(err)
	s.NotNil(createResp)

	s.Run("Valid Delete", func() {
		err := s.configService.Delete(s.GetContext(), createResp.ID)
		s.NoError(err)

		// Verify the tax association is deleted
		resp, err := s.configService.Get(s.GetContext(), createResp.ID)
		s.Error(err) // Should not be found since it's deleted
		s.Nil(resp)
	})

	s.Run("Invalid - Empty Tax Association ID", func() {
		err := s.configService.Delete(s.GetContext(), "")
		s.Error(err)
	})

	s.Run("Invalid - Non-existent Tax Association ID", func() {
		err := s.configService.Delete(s.GetContext(), "non-existent-id")
		s.Error(err)
	})
}

func (s *TaxAssociationServiceSuite) TestLinkTaxRatesToEntity() {
	s.Run("Link with Existing Tax Rates", func() {
		// Create existing tax rates
		taxRate1 := s.createSampleTaxRate("LINK_TAX_1")
		taxRate2 := s.createSampleTaxRate("LINK_TAX_2")

		entityID := "customer-link-existing"
		entityType := types.TaxrateEntityTypeCustomer

		taxRateLinks := []*dto.CreateEntityTaxAssociation{
			{
				TaxRateID:  lo.ToPtr(taxRate1.ID),
				EntityType: entityType,
				EntityID:   entityID,
				Priority:   10,
				AutoApply:  true,
			},
			{
				TaxRateID:  lo.ToPtr(taxRate2.ID),
				EntityType: entityType,
				EntityID:   entityID,
				Priority:   20,
				AutoApply:  false,
			},
		}

		resp, err := s.configService.LinkTaxRatesToEntity(s.GetContext(), entityType, entityID, taxRateLinks)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(entityID, resp.EntityID)
		s.Equal(entityType, resp.EntityType)
		s.Equal(2, len(resp.LinkedTaxRates))

		// Check that tax rates were linked correctly
		for i, linked := range resp.LinkedTaxRates {
			s.NotEmpty(linked.TaxRateID)
			s.NotEmpty(linked.TaxAssociationID)
			s.Equal(taxRateLinks[i].Priority, linked.Priority)
			s.Equal(taxRateLinks[i].AutoApply, linked.AutoApply)
			s.False(linked.WasCreated) // Should be false since we used existing tax rates
		}
	})

	s.Run("Link with New Tax Rates", func() {
		entityID := "customer-link-new"
		entityType := types.TaxrateEntityTypeCustomer

		taxRateLinks := []*dto.CreateEntityTaxAssociation{
			{
				CreateTaxRateRequest: dto.CreateTaxRateRequest{
					Name:            "New Tax Rate 1",
					Code:            "NEW_TAX_1",
					TaxRateType:     types.TaxRateTypePercentage,
					PercentageValue: lo.ToPtr(decimal.NewFromFloat(15.0)),
					Currency:        "USD",
					Scope:           types.TaxRateScopeExternal,
				},
				EntityType: entityType,
				EntityID:   entityID,
				Priority:   5,
				AutoApply:  true,
			},
			{
				CreateTaxRateRequest: dto.CreateTaxRateRequest{
					Name:        "New Tax Rate 2",
					Code:        "NEW_TAX_2",
					TaxRateType: types.TaxRateTypeFixed,
					FixedValue:  lo.ToPtr(decimal.NewFromFloat(25.0)),
					Currency:    "USD",
					Scope:       types.TaxRateScopeInternal,
				},
				EntityType: entityType,
				EntityID:   entityID,
				Priority:   15,
				AutoApply:  false,
			},
		}

		resp, err := s.configService.LinkTaxRatesToEntity(s.GetContext(), entityType, entityID, taxRateLinks)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(entityID, resp.EntityID)
		s.Equal(entityType, resp.EntityType)
		s.Equal(2, len(resp.LinkedTaxRates))

		// Check that tax rates were created and linked correctly
		for i, linked := range resp.LinkedTaxRates {
			s.NotEmpty(linked.TaxRateID)
			s.NotEmpty(linked.TaxAssociationID)
			s.Equal(taxRateLinks[i].Priority, linked.Priority)
			s.Equal(taxRateLinks[i].AutoApply, linked.AutoApply)
			s.True(linked.WasCreated) // Should be true since we created new tax rates
		}

		// Verify the tax rates were actually created
		for _, linked := range resp.LinkedTaxRates {
			taxResp, err := s.taxService.GetTaxRate(s.GetContext(), linked.TaxRateID)
			s.NoError(err)
			s.NotNil(taxResp)
		}
	})

	s.Run("Link with Mixed Existing and New Tax Rates", func() {
		// Create one existing tax rate
		existingTaxRate := s.createSampleTaxRate("MIXED_EXISTING")

		entityID := "customer-link-mixed"
		entityType := types.TaxrateEntityTypeSubscription

		taxRateLinks := []*dto.CreateEntityTaxAssociation{
			{
				TaxRateID:  lo.ToPtr(existingTaxRate.ID),
				EntityType: entityType,
				EntityID:   entityID,
				Priority:   1,
				AutoApply:  true,
			},
			{
				CreateTaxRateRequest: dto.CreateTaxRateRequest{
					Name:            "Mixed New Tax",
					Code:            "MIXED_NEW",
					TaxRateType:     types.TaxRateTypePercentage,
					PercentageValue: lo.ToPtr(decimal.NewFromFloat(12.5)),
					Currency:        "USD",
					Scope:           types.TaxRateScopeOneTime,
				},
				EntityType: entityType,
				EntityID:   entityID,
				Priority:   2,
				AutoApply:  false,
			},
		}

		resp, err := s.configService.LinkTaxRatesToEntity(s.GetContext(), entityType, entityID, taxRateLinks)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(2, len(resp.LinkedTaxRates))

		// Check the first link (existing tax rate)
		s.Equal(existingTaxRate.ID, resp.LinkedTaxRates[0].TaxRateID)
		s.False(resp.LinkedTaxRates[0].WasCreated)

		// Check the second link (new tax rate)
		s.True(resp.LinkedTaxRates[1].WasCreated)
	})

	s.Run("Link with Empty Tax Rate Links", func() {
		entityID := "customer-empty"
		entityType := types.TaxrateEntityTypeCustomer

		resp, err := s.configService.LinkTaxRatesToEntity(s.GetContext(), entityType, entityID, []*dto.CreateEntityTaxAssociation{})
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(entityID, resp.EntityID)
		s.Equal(entityType, resp.EntityType)
		s.Equal(0, len(resp.LinkedTaxRates))
	})

	s.Run("Invalid - Non-existent Tax Rate ID", func() {
		entityID := "customer-invalid"
		entityType := types.TaxrateEntityTypeCustomer

		taxRateLinks := []*dto.CreateEntityTaxAssociation{
			{
				TaxRateID:  lo.ToPtr("non-existent-tax-rate"),
				EntityType: entityType,
				EntityID:   entityID,
				Priority:   1,
				AutoApply:  true,
			},
		}

		resp, err := s.configService.LinkTaxRatesToEntity(s.GetContext(), entityType, entityID, taxRateLinks)
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("Invalid - Invalid New Tax Rate", func() {
		entityID := "customer-invalid-new"
		entityType := types.TaxrateEntityTypeCustomer

		taxRateLinks := []*dto.CreateEntityTaxAssociation{
			{
				CreateTaxRateRequest: dto.CreateTaxRateRequest{
					Name:        "", // Invalid - empty name
					Code:        "INVALID_TAX",
					TaxRateType: types.TaxRateTypePercentage,
					Currency:    "USD",
					Scope:       types.TaxRateScopeExternal,
				},
				EntityType: entityType,
				EntityID:   entityID,
				Priority:   1,
				AutoApply:  true,
			},
		}

		resp, err := s.configService.LinkTaxRatesToEntity(s.GetContext(), entityType, entityID, taxRateLinks)
		s.Error(err)
		s.Nil(resp)
	})
}

func (s *TaxAssociationServiceSuite) TestPriorityResolution() {
	// Create multiple tax rates
	taxRate1 := s.createSampleTaxRate("PRIORITY_1")
	taxRate2 := s.createSampleTaxRate("PRIORITY_2")
	taxRate3 := s.createSampleTaxRate("PRIORITY_3")

	entityID := "customer-priority"
	entityType := types.TaxrateEntityTypeCustomer

	// Create associations with different priorities
	associations := []*dto.CreateTaxAssociationRequest{
		s.createTaxAssociationRequest(taxRate1.ID, entityID, entityType),
		s.createTaxAssociationRequest(taxRate2.ID, entityID, entityType),
		s.createTaxAssociationRequest(taxRate3.ID, entityID, entityType),
	}

	// Set different priorities (lower number = higher priority)
	associations[0].Priority = 10 // Highest priority
	associations[1].Priority = 20 // Medium priority
	associations[2].Priority = 30 // Lowest priority

	// Create all associations
	for _, req := range associations {
		_, err := s.configService.Create(s.GetContext(), req)
		s.NoError(err)
	}

	s.Run("List Associations Sorted by Priority", func() {
		filter := types.NewTaxAssociationFilter()
		filter.EntityType = entityType
		filter.EntityID = entityID

		resp, err := s.configService.List(s.GetContext(), filter)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(3, len(resp.Items))

		// Verify sorting by priority (ascending - lower number = higher priority)
		s.LessOrEqual(resp.Items[0].Priority, resp.Items[1].Priority)
		s.LessOrEqual(resp.Items[1].Priority, resp.Items[2].Priority)
	})
}

func (s *TaxAssociationServiceSuite) TestAutoApplyFunctionality() {
	// Create tax rates
	autoApplyTaxRate := s.createSampleTaxRate("AUTO_APPLY")
	manualTaxRate := s.createSampleTaxRate("MANUAL")

	entityID := "subscription-auto"
	entityType := types.TaxrateEntityTypeSubscription

	// Create associations with different auto apply settings
	associations := []*dto.CreateTaxAssociationRequest{
		s.createTaxAssociationRequest(autoApplyTaxRate.ID, entityID, entityType),
		s.createTaxAssociationRequest(manualTaxRate.ID, entityID, entityType),
	}

	associations[0].AutoApply = true
	associations[1].AutoApply = false

	// Create all associations
	for _, req := range associations {
		_, err := s.configService.Create(s.GetContext(), req)
		s.NoError(err)
	}

	s.Run("Filter by Auto Apply - True", func() {
		filter := types.NewTaxAssociationFilter()
		filter.EntityType = entityType
		filter.EntityID = entityID
		filter.AutoApply = lo.ToPtr(true)

		resp, err := s.configService.List(s.GetContext(), filter)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(1, len(resp.Items))
		s.True(resp.Items[0].AutoApply)
		s.Equal(autoApplyTaxRate.ID, resp.Items[0].TaxRateID)
	})

	s.Run("Filter by Auto Apply - False", func() {
		filter := types.NewTaxAssociationFilter()
		filter.EntityType = entityType
		filter.EntityID = entityID
		filter.AutoApply = lo.ToPtr(false)

		resp, err := s.configService.List(s.GetContext(), filter)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(1, len(resp.Items))
		s.False(resp.Items[0].AutoApply)
		s.Equal(manualTaxRate.ID, resp.Items[0].TaxRateID)
	})
}

func (s *TaxAssociationServiceSuite) TestDifferentEntityTypes() {
	taxRate := s.createSampleTaxRate("ENTITY_TEST")

	entityTypes := []types.TaxrateEntityType{
		types.TaxrateEntityTypeCustomer,
		types.TaxrateEntityTypeSubscription,
		types.TaxrateEntityTypeInvoice,
		types.TaxrateEntityTypeTenant,
	}

	for i, entityType := range entityTypes {
		s.Run(fmt.Sprintf("Entity Type %s", entityType), func() {
			entityID := fmt.Sprintf("entity-%d", i)
			req := s.createTaxAssociationRequest(taxRate.ID, entityID, entityType)

			resp, err := s.configService.Create(s.GetContext(), req)
			s.NoError(err)
			s.NotNil(resp)
			s.Equal(entityType, resp.EntityType)
			s.Equal(entityID, resp.EntityID)
		})
	}
}

func (s *TaxAssociationServiceSuite) TestComplexScenarios() {
	s.Run("Multiple Tax Rates for Same Entity with Different Priorities", func() {
		// This tests a complex scenario where a customer has multiple tax rates
		// with different priorities and auto-apply settings
		customerID := "customer-complex"

		// Create multiple tax rates
		gstTaxRate := s.createSampleTaxRate("GST_COMPLEX")
		vatTaxRate := s.createSampleTaxRate("VAT_COMPLEX")
		serviceTaxRate := s.createSampleTaxRate("SERVICE_COMPLEX")

		// Create associations with different settings
		associations := []*dto.CreateTaxAssociationRequest{
			{
				TaxRateID:  gstTaxRate.ID,
				EntityType: types.TaxrateEntityTypeCustomer,
				EntityID:   customerID,
				Priority:   1, // Highest priority
				AutoApply:  true,
			},
			{
				TaxRateID:  vatTaxRate.ID,
				EntityType: types.TaxrateEntityTypeCustomer,
				EntityID:   customerID,
				Priority:   10, // Medium priority
				AutoApply:  true,
			},
			{
				TaxRateID:  serviceTaxRate.ID,
				EntityType: types.TaxrateEntityTypeCustomer,
				EntityID:   customerID,
				Priority:   20, // Lowest priority
				AutoApply:  false,
			},
		}

		// Create all associations
		createdAssociations := make([]*dto.TaxAssociationResponse, len(associations))
		for i, req := range associations {
			resp, err := s.configService.Create(s.GetContext(), req)
			s.NoError(err)
			createdAssociations[i] = resp
		}

		// List all associations for the customer
		filter := types.NewTaxAssociationFilter()
		filter.EntityType = types.TaxrateEntityTypeCustomer
		filter.EntityID = customerID

		resp, err := s.configService.List(s.GetContext(), filter)
		s.NoError(err)
		s.Equal(3, len(resp.Items))

		// Verify they are sorted by priority
		s.Equal(1, resp.Items[0].Priority)
		s.Equal(10, resp.Items[1].Priority)
		s.Equal(20, resp.Items[2].Priority)

		// List only auto-apply associations
		filter.AutoApply = lo.ToPtr(true)
		respAutoApply, err := s.configService.List(s.GetContext(), filter)
		s.NoError(err)
		s.Equal(2, len(respAutoApply.Items))

		for _, item := range respAutoApply.Items {
			s.True(item.AutoApply)
		}
	})
}

func (s *TaxAssociationServiceSuite) TestEdgeCases() {
	s.Run("Zero Priority", func() {
		taxRate := s.createSampleTaxRate("ZERO_PRIORITY")
		req := s.createTaxAssociationRequest(taxRate.ID, "entity-zero", types.TaxrateEntityTypeCustomer)
		req.Priority = 0

		resp, err := s.configService.Create(s.GetContext(), req)
		s.NoError(err)
		s.Equal(0, resp.Priority)
	})

	s.Run("High Priority Value", func() {
		taxRate := s.createSampleTaxRate("HIGH_PRIORITY")
		req := s.createTaxAssociationRequest(taxRate.ID, "entity-high", types.TaxrateEntityTypeCustomer)
		req.Priority = 9999

		resp, err := s.configService.Create(s.GetContext(), req)
		s.NoError(err)
		s.Equal(9999, resp.Priority)
	})

	s.Run("Same Tax Rate Multiple Entities", func() {
		taxRate := s.createSampleTaxRate("MULTI_ENTITY")

		entities := []string{"customer-1", "customer-2", "customer-3"}
		for _, entityID := range entities {
			req := s.createTaxAssociationRequest(taxRate.ID, entityID, types.TaxrateEntityTypeCustomer)
			resp, err := s.configService.Create(s.GetContext(), req)
			s.NoError(err)
			s.Equal(taxRate.ID, resp.TaxRateID)
			s.Equal(entityID, resp.EntityID)
		}

		// Verify all associations exist
		filter := types.NewTaxAssociationFilter()
		filter.TaxRateIDs = []string{taxRate.ID}
		resp, err := s.configService.List(s.GetContext(), filter)
		s.NoError(err)
		s.Equal(3, len(resp.Items))
	})
}
