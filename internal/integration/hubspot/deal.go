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

// hubspotDateFormat is HubSpot's date-property format, matching QuoteProperties.ExpirationDate.
const hubspotDateFormat = "2006-01-02"

// DealSyncService handles synchronization of subscription data with HubSpot deals
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

// SyncSubscriptionLineItems makes a HubSpot deal's line items match the subscription's
// FIXED-price line items. Mapped line items are updated in place; unmapped ones are created
// and recorded. Safe to call repeatedly — this is the only sync entry point, and every
// trigger site calls it with no further arguments.
//
// Mappings whose line item is no longer returned by the repository (it ended before the
// current billing period) are deliberately left alone: their end date was already pushed to
// HubSpot when they ended, so deleting them would wipe correct history off the deal.
func (s *DealSyncService) SyncSubscriptionLineItems(ctx context.Context, subscriptionID string) error {
	sub, lineItems, err := s.subscriptionRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch subscription").
			Mark(ierr.ErrInternal)
	}

	cust, err := s.customerRepo.Get(ctx, sub.CustomerID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch customer").
			Mark(ierr.ErrInternal)
	}

	dealID, ok := cust.Metadata["hubspot_deal_id"]
	if !ok || dealID == "" {
		s.logger.Info(ctx, "customer has no HubSpot deal ID, skipping line item sync",
			"customer_id", cust.ID,
			"subscription_id", subscriptionID)
		return nil
	}

	fixedItems := lo.Filter(lineItems, func(li *subscription.SubscriptionLineItem, _ int) bool {
		return li.PriceType == types.PRICE_TYPE_FIXED
	})
	if len(fixedItems) == 0 {
		return nil
	}

	mappings, err := s.getLineItemMappings(ctx, lo.Map(fixedItems,
		func(li *subscription.SubscriptionLineItem, _ int) string { return li.ID }))
	if err != nil {
		return err
	}

	// Attempt every line item; a retry redoes only what is still missing.
	var firstErr error
	for _, lineItem := range fixedItems {
		if err := s.syncLineItem(ctx, sub, lineItem, dealID, mappings[lineItem.ID]); err != nil {
			s.logger.Error(ctx, "failed to sync line item to HubSpot deal",
				"error", err,
				"line_item_id", lineItem.ID,
				"subscription_id", subscriptionID,
				"deal_id", dealID)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

// getLineItemMappings returns published HubSpot mappings for the given line items, keyed by
// line item ID. Line items with no mapping are simply absent from the map.
func (s *DealSyncService) getLineItemMappings(
	ctx context.Context,
	lineItemIDs []string,
) (map[string]*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityIDs = lineItemIDs
	filter.EntityType = types.IntegrationEntityTypeSubscriptionLineItem
	filter.ProviderTypes = []string{string(types.SecretProviderHubSpot)}
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to look up HubSpot line item mappings").
			Mark(ierr.ErrDatabase)
	}

	return lo.SliceToMap(mappings,
		func(m *entityintegrationmapping.EntityIntegrationMapping) (string, *entityintegrationmapping.EntityIntegrationMapping) {
			return m.EntityID, m
		}), nil
}

// syncLineItem updates the mapped HubSpot line item, or creates one and records the mapping.
func (s *DealSyncService) syncLineItem(
	ctx context.Context,
	sub *subscription.Subscription,
	lineItem *subscription.SubscriptionLineItem,
	dealID string,
	mapping *entityintegrationmapping.EntityIntegrationMapping,
) error {
	props, err := s.buildLineItemProperties(ctx, sub, lineItem)
	if err != nil {
		return err
	}

	if mapping != nil {
		return s.client.UpdateDealLineItem(ctx, mapping.ProviderEntityID, props)
	}

	resp, err := s.client.CreateDealLineItem(ctx, &DealLineItemCreateRequest{
		Properties: *props,
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
	})
	if err != nil {
		return err
	}

	newMapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         lineItem.ID,
		EntityType:       types.IntegrationEntityTypeSubscriptionLineItem,
		ProviderType:     string(types.SecretProviderHubSpot),
		ProviderEntityID: resp.ID,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	if err := s.entityIntegrationMappingRepo.Create(ctx, newMapping); err != nil {
		// The HubSpot line item exists but we could not record it, so a retry would create a
		// second one. Remove it and let the retry start clean. A concurrent reconcile losing
		// the unique-index race lands here too.
		if delErr := s.client.DeleteDealLineItem(ctx, resp.ID); delErr != nil && !ierr.IsNotFound(delErr) {
			s.logger.Error(ctx, "failed to remove orphaned HubSpot line item after mapping persist failure",
				"error", delErr,
				"hubspot_line_item_id", resp.ID,
				"line_item_id", lineItem.ID)
		}
		return ierr.WithError(err).
			WithHint("Failed to persist HubSpot line item mapping").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// buildLineItemProperties maps a subscription line item to HubSpot line item properties.
// The end date is sent only when set — an unset date must not overwrite a real value, and
// `omitempty` on the field drops the key entirely.
func (s *DealSyncService) buildLineItemProperties(
	ctx context.Context,
	sub *subscription.Subscription,
	lineItem *subscription.SubscriptionLineItem,
) (*DealLineItemProperties, error) {
	priceObj, err := s.priceRepo.Get(ctx, lineItem.PriceID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Price not found; cannot build accurate HubSpot line item").
			Mark(ierr.ErrInternal)
	}

	unitPrice := priceObj.Amount

	description := string(lineItem.PriceType) + " pricing"
	if lineItem.DisplayName != "" {
		description = lineItem.DisplayName + " (" + string(lineItem.PriceType) + " pricing)"
	}

	props := &DealLineItemProperties{
		Name:                 lineItem.DisplayName,
		Price:                unitPrice.String(),
		Quantity:             lineItem.Quantity.String(),
		Amount:               unitPrice.Mul(lineItem.Quantity).String(),
		Discount:             "0",
		RecurringBillingFreq: s.mapBillingFrequency(sub.BillingPeriod),
		Description:          description,
	}

	if !lineItem.StartDate.IsZero() {
		props.RecurringBillingStartDate = lineItem.StartDate.UTC().Format(hubspotDateFormat)
	}
	if !lineItem.EndDate.IsZero() {
		props.RecurringBillingEndDate = lineItem.EndDate.UTC().Format(hubspotDateFormat)
	}

	return props, nil
}

// UpdateDealAmountFromACV updates the deal amount based on HubSpot's calculated ACV
// This should be called after line items are created and HubSpot has recalculated ACV
func (s *DealSyncService) UpdateDealAmountFromACV(ctx context.Context, customerID, dealID string) error {
	s.logger.Info(ctx, "updating deal amount from ACV",
		"customer_id", customerID,
		"deal_id", dealID)

	// Update deal amount based on ACV - just fetch and update, don't calculate
	if err := s.updateDealAmountFromHubSpot(ctx, dealID); err != nil {
		s.logger.Error(ctx, "failed to update deal amount",
			"error", err,
			"deal_id", dealID,
			"customer_id", customerID)
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

// updateDealAmountFromHubSpot fetches the deal's ACV from HubSpot and updates the deal amount
// This function only reads ACV calculated by HubSpot, never calculates manually
func (s *DealSyncService) updateDealAmountFromHubSpot(ctx context.Context, dealID string) error {
	s.logger.Info(ctx, "fetching deal to get ACV",
		"deal_id", dealID)

	// Get the deal to read its ACV
	deal, err := s.client.GetDeal(ctx, dealID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch deal from HubSpot").
			Mark(ierr.ErrHTTPClient)
	}

	s.logger.Info(ctx, "fetched deal properties",
		"deal_id", dealID,
		"acv", deal.Properties.ACV,
		"mrr", deal.Properties.MRR,
		"arr", deal.Properties.ARR)

	// Extract ACV from deal properties (already a string)
	acv := deal.Properties.ACV
	if acv == "" {
		s.logger.Info(ctx, "hs_acv property not found or empty",
			"deal_id", dealID,
			"deal_name", deal.Properties.DealName,
			"current_amount", deal.Properties.Amount)
		return ierr.NewError("ACV not found in HubSpot deal").
			WithHint("HubSpot has not calculated ACV yet or line items were not synced").
			Mark(ierr.ErrHTTPClient)
	}

	s.logger.Info(ctx, "updating deal amount with ACV",
		"deal_id", dealID,
		"acv", acv)

	// Update the deal amount
	updateProps := map[string]string{
		"amount": acv,
	}

	_, err = s.client.UpdateDeal(ctx, dealID, updateProps)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update deal amount").
			Mark(ierr.ErrHTTPClient)
	}

	s.logger.Info(ctx, "successfully updated deal amount",
		"deal_id", dealID,
		"amount", acv)

	return nil
}
