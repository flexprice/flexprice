package hubspot

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type DealSyncService struct {
	client                       HubSpotClient
	customerRepo                 customer.Repository
	subscriptionRepo             subscription.Repository
	priceRepo                    price.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewDealSyncService creates a new HubSpot deal sync service
func NewDealSyncService(
	client HubSpotClient,
	customerRepo customer.Repository,
	subscriptionRepo subscription.Repository,
	priceRepo price.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) *DealSyncService {
	return &DealSyncService{
		client:                       client,
		customerRepo:                 customerRepo,
		subscriptionRepo:             subscriptionRepo,
		priceRepo:                    priceRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

func (s *DealSyncService) getLineItemMapping(ctx context.Context, lineItemID string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = lineItemID
	filter.EntityType = types.IntegrationEntityTypeSubscriptionLineItem
	filter.ProviderTypes = []string{string(types.SecretProviderHubSpot)}
	filter.Status = lo.ToPtr(types.StatusPublished)

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to look up HubSpot line item mapping").
			Mark(ierr.ErrDatabase)
	}
	if len(mappings) == 0 {
		return nil, nil
	}
	return mappings[0], nil
}

func (s *DealSyncService) SyncLineItemCreated(ctx context.Context, subscriptionID, lineItemID, dealID string) error {
	existing, err := s.getLineItemMapping(ctx, lineItemID)
	if err != nil {
		return err
	}
	if existing != nil {
		s.logger.Info(ctx, "HubSpot line item mapping already exists, skipping create",
			"line_item_id", lineItemID, "hubspot_line_item_id", existing.ProviderEntityID)
		return nil
	}

	sub, lineItems, err := s.subscriptionRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch subscription for HubSpot line item sync").
			Mark(ierr.ErrInternal)
	}

	lineItem, found := lo.Find(lineItems, func(li *subscription.SubscriptionLineItem) bool {
		return li.ID == lineItemID
	})
	if !found {
		s.logger.Info(ctx, "line item not found on subscription, skipping HubSpot sync",
			"subscription_id", subscriptionID, "line_item_id", lineItemID)
		return nil
	}

	if lineItem.PriceType != types.PRICE_TYPE_FIXED {
		s.logger.Info(ctx, "line item is not FIXED, skipping HubSpot sync",
			"line_item_id", lineItemID, "price_type", lineItem.PriceType)
		return nil
	}

	priceObj, err := s.priceRepo.Get(ctx, lineItem.PriceID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Price not found; cannot create accurate HubSpot line item").
			Mark(ierr.ErrInternal)
	}

	unitPrice := priceObj.Amount
	totalAmount := unitPrice.Mul(lineItem.Quantity)
	billingFreq := s.mapBillingFrequency(sub.BillingPeriod)

	description := string(lineItem.PriceType) + " pricing"
	if lineItem.DisplayName != "" {
		description = lineItem.DisplayName + " (" + string(lineItem.PriceType) + " pricing)"
	}

	req := &DealLineItemCreateRequest{
		Properties: DealLineItemProperties{
			Name:                 lineItem.DisplayName,
			Price:                unitPrice.String(),
			Quantity:             lineItem.Quantity.String(),
			Amount:               totalAmount.String(),
			Discount:             "0",
			RecurringBillingFreq: billingFreq,
			Description:          description,
		},
		Associations: []LineItemAssociation{
			{
				To: AssociationTarget{ID: dealID},
				Types: []AssociationType{
					{
						AssociationCategory: string(AssociationCategoryHubSpotDefined),
						AssociationTypeID:   AssociationTypeLineItemToDeal,
					},
				},
			},
		},
	}

	s.logger.Info(ctx, "creating HubSpot line item",
		"deal_id", dealID, "line_item_id", lineItemID,
		"quantity", lineItem.Quantity.String(), "unit_price", unitPrice.String())

	resp, err := s.client.CreateDealLineItem(ctx, req)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create HubSpot line item").
			Mark(ierr.ErrHTTPClient)
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         lineItemID,
		EntityType:       types.IntegrationEntityTypeSubscriptionLineItem,
		ProviderType:     string(types.SecretProviderHubSpot),
		ProviderEntityID: resp.ID,
		Metadata:         map[string]interface{}{"deal_id": dealID},
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	if err := s.entityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		// The HubSpot line item now exists but we couldn't record it locally, so a
		// Temporal retry would otherwise find no mapping and create a duplicate.
		// Compensate by deleting the just-created line item so the retry starts clean.
		if delErr := s.client.DeleteDealLineItem(ctx, resp.ID); delErr != nil && !ierr.IsNotFound(delErr) {
			s.logger.Error(ctx, "failed to compensate orphaned HubSpot line item after mapping persist failure",
				"error", delErr, "hubspot_line_item_id", resp.ID, "line_item_id", lineItemID)
		}
		return ierr.WithError(err).
			WithHint("Failed to persist HubSpot line item mapping").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Info(ctx, "successfully created HubSpot line item",
		"line_item_id", lineItemID, "hubspot_line_item_id", resp.ID, "deal_id", dealID)
	return nil
}

func (s *DealSyncService) SyncLineItemDeleted(ctx context.Context, lineItemID string) error {
	mapping, err := s.getLineItemMapping(ctx, lineItemID)
	if err != nil {
		return err
	}
	if mapping == nil {
		s.logger.Info(ctx, "no HubSpot line item mapping found, skipping delete",
			"line_item_id", lineItemID)
		return nil
	}

	err = s.client.DeleteDealLineItem(ctx, mapping.ProviderEntityID)
	if err != nil && !ierr.IsNotFound(err) {
		return ierr.WithError(err).
			WithHint("Failed to delete HubSpot line item").
			Mark(ierr.ErrHTTPClient)
	}
	if err != nil {
		s.logger.Info(ctx, "HubSpot line item already gone, self-healing stale mapping",
			"line_item_id", lineItemID, "hubspot_line_item_id", mapping.ProviderEntityID)
	}

	if err := s.entityIntegrationMappingRepo.Delete(ctx, mapping); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete HubSpot line item mapping").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Info(ctx, "successfully deleted HubSpot line item",
		"line_item_id", lineItemID, "hubspot_line_item_id", mapping.ProviderEntityID)
	return nil
}

func (s *DealSyncService) UpdateDealAmountFromACV(ctx context.Context, customerID, dealID string) error {
	s.logger.Info(ctx, "updating deal amount from ACV", "customer_id", customerID, "deal_id", dealID)
	if err := s.updateDealAmountFromHubSpot(ctx, dealID); err != nil {
		s.logger.Error(ctx, "failed to update deal amount",
			"error", err, "deal_id", dealID, "customer_id", customerID)
		return err
	}
	return nil
}

// mapBillingFrequency converts FlexPrice billing period to HubSpot billing frequency
func (s *DealSyncService) mapBillingFrequency(period types.BillingPeriod) string {
	switch period {
	case types.BILLING_PERIOD_MONTHLY:
		return "monthly"
	case types.BILLING_PERIOD_ANNUAL:
		return "annually"
	case types.BILLING_PERIOD_WEEKLY:
		return "weekly"
	case types.BILLING_PERIOD_QUARTER:
		return "quarterly"
	default:
		return string(period)
	}
}

func (s *DealSyncService) updateDealAmountFromHubSpot(ctx context.Context, dealID string) error {
	deal, err := s.client.GetDeal(ctx, dealID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch deal from HubSpot").
			Mark(ierr.ErrHTTPClient)
	}

	acv := deal.Properties.ACV
	if acv == "" {
		return ierr.NewError("ACV not found in HubSpot deal").
			WithHint("HubSpot has not calculated ACV yet or line items were not synced").
			Mark(ierr.ErrHTTPClient)
	}

	_, err = s.client.UpdateDeal(ctx, dealID, map[string]string{"amount": acv})
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update deal amount").
			Mark(ierr.ErrHTTPClient)
	}

	s.logger.Info(ctx, "successfully updated deal amount", "deal_id", dealID, "amount", acv)
	return nil
}
