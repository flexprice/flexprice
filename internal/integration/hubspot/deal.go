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

// DealSyncService handles synchronization of subscription line items with HubSpot deal line items.
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

// getLineItemMapping returns the published subscription_line_item -> HubSpot mapping for
// lineItemID, if one exists.
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

// SyncLineItemCreated creates a HubSpot deal line item for the given subscription line item
// and associates it with dealID. A no-op (not an error) if:
//   - the line item is not FIXED (usage-based line items are never synced to HubSpot)
//   - a mapping for this line item already exists (idempotent retry-safety: the workflow may
//     be retried after a partial failure, and must never double-create)
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
		return ierr.WithError(err).
			WithHint("Failed to persist HubSpot line item mapping").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Info(ctx, "successfully created HubSpot line item",
		"line_item_id", lineItemID, "hubspot_line_item_id", resp.ID, "deal_id", dealID)
	return nil
}

// SyncLineItemDeleted deletes the HubSpot line item mapped to lineItemID, if any. A no-op if
// there is no mapping (never synced, e.g. the line item wasn't FIXED at creation time) or if
// HubSpot reports the line item is already gone (self-heals the stale local mapping instead
// of failing).
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

// UpdateDealAmountFromACV updates the deal amount based on HubSpot's calculated ACV.
// This should be called after a line item create/delete, once HubSpot has recalculated ACV.
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

// updateDealAmountFromHubSpot fetches the deal's ACV from HubSpot and updates the deal amount.
// This function only reads ACV calculated by HubSpot, never calculates manually.
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
