package tabs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type TabsInvoiceService interface {
	SyncInvoiceToTabs(ctx context.Context, req TabsInvoiceSyncRequest) (*TabsInvoiceSyncResponse, error)
}

type InvoiceService struct {
	client       TabsClient
	customerRepo customer.Repository
	subRepo      subscription.Repository
	planRepo     plan.Repository
	invoiceRepo  invoice.Repository
	mappingRepo  entityintegrationmapping.Repository
	locker       cache.Locker
	logger       *logger.Logger
}

func NewInvoiceService(
	client TabsClient,
	customerRepo customer.Repository,
	subRepo subscription.Repository,
	planRepo plan.Repository,
	invoiceRepo invoice.Repository,
	mappingRepo entityintegrationmapping.Repository,
	locker cache.Locker,
	logger *logger.Logger,
) TabsInvoiceService {
	return &InvoiceService{
		client:       client,
		customerRepo: customerRepo,
		subRepo:      subRepo,
		planRepo:     planRepo,
		invoiceRepo:  invoiceRepo,
		mappingRepo:  mappingRepo,
		locker:       locker,
		logger:       logger,
	}
}

func (s *InvoiceService) SyncInvoiceToTabs(ctx context.Context, req TabsInvoiceSyncRequest) (*TabsInvoiceSyncResponse, error) {
	if s.locker != nil {
		lockKey := cache.GenerateKey(ctx, cache.PrefixTabsInvoiceSyncLock, req.InvoiceID)
		lock, err := s.locker.AcquireLock(ctx, lockKey, tabsInvoiceSyncLockTTL)
		if err != nil {
			return nil, err
		}
		if !lock.AcquiredSuccessfully() {
			s.logger.Info(ctx, "tabs: invoice sync already in progress, skipping", "invoice_id", req.InvoiceID)
			return nil, nil
		}
		defer func() {
			if releaseErr := lock.Release(ctx); releaseErr != nil {
				s.logger.Error(ctx, "tabs: failed to release invoice sync lock", "invoice_id", req.InvoiceID, "error", releaseErr)
			}
		}()
	}
	inv, err := s.invoiceRepo.Get(ctx, req.InvoiceID)
	if err != nil {
		return nil, err
	}

	existingMapping, alreadySynced, err := s.getExistingInvoiceMapping(ctx, req.InvoiceID)
	if err != nil {
		return nil, err
	}

	cust, err := s.customerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		return nil, err
	}

	tabsCustomerID, err := s.ensureCustomer(ctx, cust, inv.Currency)
	if err != nil || tabsCustomerID == "" {
		return nil, fmt.Errorf("failed to ensure customer sync on tabs: %w", err)
	}

	if inv.SubscriptionID == nil || *inv.SubscriptionID == "" {
		return nil, ierr.NewError("tabs sync requires a subscription invoice").
			WithHint("Tabs obligations are created on a contract, which maps to a subscription").
			WithReportableDetails(map[string]interface{}{"invoice_id": inv.ID}).
			Mark(ierr.ErrValidation)
	}

	tabsContractID, err := s.ensureContract(ctx, *inv.SubscriptionID, tabsCustomerID)
	if err != nil {
		return nil, err
	}

	// Re-sync: delete the previously-synced obligations (by the obligation ids on the invoice mapping,
	// so obligations for removed/recreated line items are caught too) before recreating them below.
	if alreadySynced {
		if err = s.deletePreviousObligations(ctx, existingMapping, inv, tabsContractID); err != nil {
			return nil, err
		}
	}

	if !inv.AmountRemaining.IsPositive() {
		if existingMapping != nil {
			if err = s.mappingRepo.Delete(ctx, existingMapping); err != nil {
				return nil, err
			}
		}
		s.logger.Info(ctx, "tabs: previously-synced invoice is now zero, removed tabs obligations",
			"invoice_id", inv.ID, "tabs_contract_id", tabsContractID)
		return &TabsInvoiceSyncResponse{
			ContractID:     tabsContractID,
			TabsCustomerID: tabsCustomerID,
			Currency:       inv.Currency,
		}, nil
	}

	// Returned ids are recorded on the mapping so a later re-sync can delete these obligations.
	obligationIDs, err := s.syncObligations(ctx, inv, tabsContractID)
	if err != nil {
		return nil, err
	}

	// Find the Tabs invoice generated from the obligations (by contract + issue date).
	issueDate := lo.FromPtr(inv.IssueDate)
	tabsInvoiceID, err := s.fetchTabsInvoiceID(ctx, tabsContractID, issueDate.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}

	if tabsInvoiceID == "" {
		return nil, fmt.Errorf("unable to create invoice in tabs")
	}

	if err := s.persistInvoiceMapping(ctx, existingMapping, inv, tabsContractID, tabsCustomerID, tabsInvoiceID, obligationIDs); err != nil {
		return nil, err
	}

	s.logger.Info(ctx, "tabs: invoice synced",
		"invoice_id", inv.ID,
		"tabs_customer_id", tabsCustomerID,
		"tabs_contract_id", tabsContractID)

	return &TabsInvoiceSyncResponse{
		ContractID:     tabsContractID,
		TabsCustomerID: tabsCustomerID,
		TabsInvoiceID:  tabsInvoiceID,
		Currency:       inv.Currency,
	}, nil
}

// getExistingInvoiceMapping returns the published Tabs mapping for the invoice, if any.
func (s *InvoiceService) getExistingInvoiceMapping(ctx context.Context, invoiceID string) (*entityintegrationmapping.EntityIntegrationMapping, bool, error) {
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = invoiceID
	filter.EntityType = types.IntegrationEntityTypeInvoice
	filter.ProviderTypes = []string{string(types.SecretProviderTabs)}
	filter.Status = lo.ToPtr(types.StatusPublished)

	existing, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return nil, false, err
	}
	if len(existing) == 0 {
		return nil, false, nil
	}
	return existing[0], true, nil
}

func (s *InvoiceService) persistInvoiceMapping(ctx context.Context, existing *entityintegrationmapping.EntityIntegrationMapping, inv *invoice.Invoice, contractID, tabsCustomerID, tabsInvoiceID string, obligationIDs []string) error {
	if existing != nil {
		existing.ProviderEntityID = tabsInvoiceID
		existing.Metadata = tabsInvoiceMetadata(contractID, tabsCustomerID, tabsInvoiceID, obligationIDs)
		return s.mappingRepo.Update(ctx, existing)
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         inv.ID,
		EntityType:       types.IntegrationEntityTypeInvoice,
		ProviderType:     string(types.SecretProviderTabs),
		ProviderEntityID: tabsInvoiceID,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
		Metadata:         tabsInvoiceMetadata(contractID, tabsCustomerID, tabsInvoiceID, obligationIDs),
	}
	return s.mappingRepo.Create(ctx, mapping)
}

// tabsInvoiceMetadata builds the metadata for an invoice mapping. obligation_ids lets a re-sync delete
// the invoice's obligations independently of its current line items.
func tabsInvoiceMetadata(contractID, tabsCustomerID, tabsInvoiceID string, obligationIDs []string) map[string]interface{} {
	return map[string]interface{}{
		"contract_id":      contractID,
		"tabs_customer_id": tabsCustomerID,
		"tabs_invoice_id":  tabsInvoiceID,
		"obligation_ids":   obligationIDs,
		"synced_at":        time.Now().UTC().Format(time.RFC3339),
	}
}

func (s *InvoiceService) deletePreviousObligations(ctx context.Context, mapping *entityintegrationmapping.EntityIntegrationMapping, inv *invoice.Invoice, contractID string) error {
	obligationIDs := metadataStrings(mapping.Metadata, "obligation_ids")

	// Prefer matching by obligation id (catches removed/recreated line items); fall back to the current
	// line items for invoices mapped before obligation ids were recorded.
	var lineItemMappings []*entityintegrationmapping.EntityIntegrationMapping
	var err error
	if len(obligationIDs) > 0 {
		lineItemMappings, err = s.getMappingsByObligationIDs(ctx, obligationIDs)
	} else {
		lineItemMappings, err = s.getLineItemObligationMappings(ctx, inv.LineItems)
		obligationIDs = distinctObligationIDs(lineItemMappings)
	}
	if err != nil {
		return err
	}

	if len(obligationIDs) == 0 {
		return nil
	}

	for _, obligationID := range obligationIDs {
		if err = s.client.DeleteObligation(ctx, contractID, obligationID); err != nil {
			return err
		}
	}
	for _, m := range lineItemMappings {
		if err = s.mappingRepo.Delete(ctx, m); err != nil {
			return err
		}
	}

	s.logger.Info(ctx, "tabs: previous obligations deleted for re-sync",
		"invoice_id", inv.ID, "tabs_contract_id", contractID, "obligation_count", len(obligationIDs))
	return nil
}

// getMappingsByObligationIDs returns the line-item mappings whose obligation id is in the given set.
func (s *InvoiceService) getMappingsByObligationIDs(ctx context.Context, obligationIDs []string) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	if len(obligationIDs) == 0 {
		return nil, nil
	}

	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityType = types.IntegrationEntityTypeInvoiceLineItem
	filter.ProviderTypes = []string{string(types.SecretProviderTabs)}
	filter.ProviderEntityIDs = obligationIDs
	filter.Status = lo.ToPtr(types.StatusPublished)

	return s.mappingRepo.List(ctx, filter)
}

// distinctObligationIDs returns the distinct non-empty obligation ids from the line-item mappings.
func distinctObligationIDs(mappings []*entityintegrationmapping.EntityIntegrationMapping) []string {
	ids := make([]string, 0, len(mappings))
	for _, m := range mappings {
		if m.ProviderEntityID != "" {
			ids = append(ids, m.ProviderEntityID)
		}
	}
	return lo.Uniq(ids)
}

// metadataString safely reads a string value from a mapping's metadata.
func metadataString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// metadataStrings reads a string slice from a mapping's metadata, tolerating the []interface{} form
// produced by a JSON round-trip through the datastore.
func metadataStrings(m map[string]interface{}, key string) []string {
	if m == nil {
		return nil
	}
	switch v := m[key].(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

// ensureCustomer returns the Tabs customer id for a flexprice customer, creating it (async job) and
// mapping it on first use. Idempotent: an existing mapping is returned without calling Tabs.
func (s *InvoiceService) ensureCustomer(ctx context.Context, cust *customer.Customer, currency string) (string, error) {
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = cust.ID
	filter.EntityType = types.IntegrationEntityTypeCustomer
	filter.ProviderTypes = []string{string(types.SecretProviderTabs)}
	filter.Status = lo.ToPtr(types.StatusPublished)

	existing, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return "", err
	}
	if len(existing) > 0 {
		return existing[0].ProviderEntityID, nil
	}

	customerTabs, err := s.client.CreateCustomer(ctx, CreateCustomerRequest{
		Name:                       cust.Name,
		PrimaryBillingContactEmail: cust.Email,
		// Tabs requires ISO 4217 currency codes in UPPERCASE (e.g. "USD"); FlexPrice stores them lowercase.
		DefaultCurrency: strings.ToUpper(currency),
	})
	if err != nil {
		return "", err
	}

	payload, err := s.client.WaitForJob(ctx, customerTabs.Payload.JobID)
	if err != nil {
		return "", err
	}

	tabsCustomerID, _ := payload.Results.Data["customerId"].(string)
	if tabsCustomerID == "" {
		return "", ierr.NewError("tabs customer id missing from job result").
			WithHint("Tabs create-customer job succeeded but returned no customerId").
			WithReportableDetails(map[string]interface{}{"job_id": customerTabs.Payload.JobID}).
			Mark(ierr.ErrSystem)
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         cust.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderTabs),
		ProviderEntityID: tabsCustomerID,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	if err = s.mappingRepo.Create(ctx, mapping); err != nil {
		return "", err
	}

	s.logger.Info(ctx, "tabs: customer created and mapped",
		"flexprice_customer_id", cust.ID, "tabs_customer_id", tabsCustomerID)
	return tabsCustomerID, nil
}

// ensureContract returns the Tabs contract id for a flexprice subscription, creating and mapping it on
// first use. Idempotent: an existing mapping is returned without calling Tabs.
func (s *InvoiceService) ensureContract(ctx context.Context, subscriptionID, tabsCustomerID string) (string, error) {
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityID = subscriptionID
	filter.EntityType = types.IntegrationEntityTypeSubscription
	filter.ProviderTypes = []string{string(types.SecretProviderTabs)}
	filter.Status = lo.ToPtr(types.StatusPublished)

	existing, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return "", err
	}
	if len(existing) > 0 {
		return existing[0].ProviderEntityID, nil
	}

	contract, err := s.client.CreateContract(ctx, CreateContractRequest{
		Name:       s.getContractName(ctx, subscriptionID),
		CustomerID: tabsCustomerID,
	})
	if err != nil {
		return "", err
	}

	// Contracts are created in NEW status; move to PROCESSED before mapping it.
	if err = s.client.MarkContractProcessed(ctx, contract.Payload.ID); err != nil {
		return "", err
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         subscriptionID,
		EntityType:       types.IntegrationEntityTypeSubscription,
		ProviderType:     string(types.SecretProviderTabs),
		ProviderEntityID: contract.Payload.ID,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	if err = s.mappingRepo.Create(ctx, mapping); err != nil {
		return "", err
	}

	s.logger.Info(ctx, "tabs: contract created and mapped",
		"flexprice_subscription_id", subscriptionID, "tabs_contract_id", contract.Payload.ID)
	return contract.Payload.ID, nil
}

// getContractName is the subscription's plan name, or a subscription-id based fallback if unresolved.
func (s *InvoiceService) getContractName(ctx context.Context, subscriptionID string) string {
	fallback := fmt.Sprintf("Flexprice Subscription %s", subscriptionID)

	sub, err := s.subRepo.Get(ctx, subscriptionID)
	if err != nil || sub == nil || sub.PlanID == "" {
		return fallback
	}
	pl, err := s.planRepo.Get(ctx, sub.PlanID)
	if err != nil || pl == nil || pl.Name == "" {
		return fallback
	}
	return pl.Name
}

// syncObligations creates at most two obligations for an invoice — one fixed, one usage — on the
// environment's shared category products, and returns the distinct obligation ids now backing the
// invoice (this run's plus any from an earlier partial attempt) for recording on the invoice mapping.
//
// Idempotent and retry-safe: each obligation's line-item mappings are persisted, so a category already
// mapped by a prior attempt is skipped rather than duplicated. Categories that net to zero (fully
// covered by prepaid credits/payments) create no obligation.
func (s *InvoiceService) syncObligations(ctx context.Context, inv *invoice.Invoice, contractID string) ([]string, error) {
	if len(inv.LineItems) == 0 {
		return nil, nil
	}

	productByCategory, err := s.resolveCategoryProducts(ctx, inv.LineItems)
	if err != nil {
		return nil, err
	}

	// Line items (and obligations) already mapped by a previous, possibly partial, attempt. The line-item
	// set skips re-creating an already-synced category; the obligation ids seed the returned set.
	existingMappings, err := s.getLineItemObligationMappings(ctx, inv.LineItems)
	if err != nil {
		return nil, err
	}
	syncedLineItems := make(map[string]struct{}, len(existingMappings))
	for _, m := range existingMappings {
		syncedLineItems[m.EntityID] = struct{}{}
	}
	obligationIDs := distinctObligationIDs(existingMappings)

	billingStartDate := lo.FromPtr(inv.IssueDate)
	itemsByCategory := groupLineItemsByCategory(inv.LineItems)

	// Aggregate only the categories present on the invoice (it may carry just one type of charge).
	grossByCategory := make(map[chargeCategory]decimal.Decimal, len(itemsByCategory))
	periodByCategory := make(map[chargeCategory][2]time.Time, len(itemsByCategory))
	for category, items := range itemsByCategory {
		gross, start, end := aggregateLineItems(items)
		grossByCategory[category] = gross
		periodByCategory[category] = [2]time.Time{start, end}
	}

	// Apply prepaid credits + payments to usage first, then the remainder to fixed (no taxes/discounts
	// here). Clamped at 0, so netUsage + netFixed == invoice amount remaining.
	grossUsage, grossFixed := grossByCategory[categoryUsage], grossByCategory[categoryFixed]
	paidAmount := inv.TotalPrepaidCreditsApplied.Add(inv.AmountPaid)
	netUsage := decimal.Max(grossUsage.Sub(paidAmount), decimal.Zero)
	creditLeft := decimal.Max(paidAmount.Sub(grossUsage), decimal.Zero)
	netFixed := decimal.Max(grossFixed.Sub(creditLeft), decimal.Zero)

	amountByCategory := map[chargeCategory]decimal.Decimal{
		categoryFixed: netFixed,
		categoryUsage: netUsage,
	}

	for category, items := range itemsByCategory {
		// Already synced by a prior attempt, or nothing to bill.
		if groupAlreadySynced(items, syncedLineItems) {
			continue
		}
		amount := amountByCategory[category]
		if !amount.IsPositive() {
			continue
		}

		period := periodByCategory[category]
		req := buildObligation(billingStartDate, productByCategory[category], category.cadence(), amount, period[0], period[1])
		resp, cErr := s.client.CreateObligation(ctx, contractID, req)
		if cErr != nil {
			return nil, cErr
		}
		obligationIDs = append(obligationIDs, resp.Payload.ID)

		// Map the line items to the obligation so a retry skips this category.
		if err = s.mapLineItemsToObligation(ctx, items, resp.Payload.ID); err != nil {
			return nil, err
		}
	}

	return lo.Uniq(obligationIDs), nil
}

// categorizeLineItem classifies a line item by PriceType; anything not explicitly USAGE falls back to
// fixed, so a nil/unknown type is never dropped or billed as usage.
func categorizeLineItem(li *invoice.InvoiceLineItem) chargeCategory {
	if lo.FromPtr(li.PriceType) == string(types.PRICE_TYPE_USAGE) {
		return categoryUsage
	}
	return categoryFixed
}

// productName is the name of the Tabs product backing this category in an environment.
func (c chargeCategory) productName() string {
	if c == categoryUsage {
		return "Usage Charges"
	}
	return "Fixed Charges"
}

// cadence maps a category to the invoice cadence its obligation bills on: fixed charges in advance,
// usage charges in arrears. invoiceDateStrategy turns this into the Tabs invoiceDateStrategy.
func (c chargeCategory) cadence() types.InvoiceCadence {
	if c == categoryUsage {
		return types.InvoiceCadenceArrear
	}
	return types.InvoiceCadenceAdvance
}

// groupLineItemsByCategory groups the invoice's line items into the fixed and usage categories (at most two groups).
func groupLineItemsByCategory(lineItems []*invoice.InvoiceLineItem) map[chargeCategory][]*invoice.InvoiceLineItem {
	itemsByCategory := make(map[chargeCategory][]*invoice.InvoiceLineItem, 2)
	for _, li := range lineItems {
		category := categorizeLineItem(li)
		itemsByCategory[category] = append(itemsByCategory[category], li)
	}
	return itemsByCategory
}

// resolveCategoryProducts returns the Tabs product id per category the invoice touches. Each environment
// has at most two products (fixed, usage), reused across invoices: a product is created only when the
// environment has none for that category, and every referenced price is mapped to its category product.
func (s *InvoiceService) resolveCategoryProducts(ctx context.Context, lineItems []*invoice.InvoiceLineItem) (map[chargeCategory]string, error) {
	categoryByPrice := make(map[string]chargeCategory)
	neededCategories := make(map[chargeCategory]struct{})
	for _, li := range lineItems {
		category := categorizeLineItem(li)
		neededCategories[category] = struct{}{}
		if li.PriceID != nil && *li.PriceID != "" {
			categoryByPrice[*li.PriceID] = category
		}
	}

	// The environment's price -> product mappings yield each category's existing product (from the
	// metadata) and the prices already mapped.
	existing, err := s.mappingRepo.List(ctx, newTabsPriceMappingFilter())
	if err != nil {
		return nil, err
	}
	productByCategory := make(map[chargeCategory]string, 2)
	mappedPrices := make(map[string]struct{}, len(existing))
	for _, m := range existing {
		mappedPrices[m.EntityID] = struct{}{}
		if category := metadataString(m.Metadata, "tabs_category"); category != "" {
			productByCategory[chargeCategory(category)] = m.ProviderEntityID
		}
	}

	// Create a product for any needed category the environment doesn't have yet.
	for category := range neededCategories {
		if productByCategory[category] == "" {
			productID, cErr := s.createCategoryProduct(ctx, category)
			if cErr != nil {
				return nil, cErr
			}
			productByCategory[category] = productID
		}
	}

	// Map any not-yet-mapped price to its category product.
	for priceID, category := range categoryByPrice {
		if _, ok := mappedPrices[priceID]; ok {
			continue
		}
		if err = s.mapPriceToProduct(ctx, priceID, productByCategory[category], category); err != nil {
			return nil, err
		}
	}

	return productByCategory, nil
}

// newTabsPriceMappingFilter returns the filter for the environment's published price -> Tabs-product mappings.
func newTabsPriceMappingFilter() *types.EntityIntegrationMappingFilter {
	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityType = types.IntegrationEntityTypePrice
	filter.ProviderTypes = []string{string(types.SecretProviderTabs)}
	filter.Status = lo.ToPtr(types.StatusPublished)
	return filter
}

// createCategoryProduct creates the shared Tabs product backing a category and returns its id.
func (s *InvoiceService) createCategoryProduct(ctx context.Context, category chargeCategory) (string, error) {
	resp, err := s.client.CreateProduct(ctx, CreateProductRequest{
		Status:      "ACTIVE",
		Name:        category.productName(),
		Description: category.productName(),
	})
	if err != nil {
		return "", err
	}
	s.logger.Info(ctx, "tabs: category product created",
		"category", string(category), "tabs_product_id", resp.Payload.ID)
	return resp.Payload.ID, nil
}

// mapPriceToProduct persists a price -> Tabs-product mapping, recording the category in the metadata.
func (s *InvoiceService) mapPriceToProduct(ctx context.Context, priceID, productID string, category chargeCategory) error {
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         priceID,
		EntityType:       types.IntegrationEntityTypePrice,
		ProviderType:     string(types.SecretProviderTabs),
		ProviderEntityID: productID,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
		Metadata:         map[string]interface{}{"tabs_category": string(category)},
	}
	if err := s.mappingRepo.Create(ctx, mapping); err != nil {
		return err
	}
	s.logger.Info(ctx, "tabs: price mapped to category product",
		"flexprice_price_id", priceID, "tabs_product_id", productID, "category", string(category))
	return nil
}

// getLineItemObligationMappings returns the line-item -> Tabs-obligation mappings for the line items.
func (s *InvoiceService) getLineItemObligationMappings(ctx context.Context, lineItems []*invoice.InvoiceLineItem) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	ids := lineItemIDs(lineItems)
	if len(ids) == 0 {
		return nil, nil
	}

	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityIDs = ids
	filter.EntityType = types.IntegrationEntityTypeInvoiceLineItem
	filter.ProviderTypes = []string{string(types.SecretProviderTabs)}
	filter.Status = lo.ToPtr(types.StatusPublished)

	return s.mappingRepo.List(ctx, filter)
}

// groupAlreadySynced reports whether a category was already synced — one mapped line item implies all,
// since its obligation is created atomically.
func groupAlreadySynced(items []*invoice.InvoiceLineItem, synced map[string]struct{}) bool {
	for _, li := range items {
		if _, ok := synced[li.ID]; ok {
			return true
		}
	}
	return false
}

// mapLineItemsToObligation persists a line-item -> Tabs-obligation mapping for every line item.
func (s *InvoiceService) mapLineItemsToObligation(ctx context.Context, items []*invoice.InvoiceLineItem, obligationID string) error {
	for _, li := range items {
		if li.ID == "" {
			continue
		}
		mapping := &entityintegrationmapping.EntityIntegrationMapping{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
			EntityID:         li.ID,
			EntityType:       types.IntegrationEntityTypeInvoiceLineItem,
			ProviderType:     string(types.SecretProviderTabs),
			ProviderEntityID: obligationID,
			EnvironmentID:    types.GetEnvironmentID(ctx),
			BaseModel:        types.GetDefaultBaseModel(ctx),
		}
		if err := s.mappingRepo.Create(ctx, mapping); err != nil {
			return err
		}
	}
	return nil
}

// aggregateLineItems sums the line items' amounts and returns the group total and its service period.
// Must be called with a non-empty group.
func aggregateLineItems(items []*invoice.InvoiceLineItem) (total decimal.Decimal, serviceStart, serviceEnd time.Time) {
	total = decimal.Zero
	serviceStart = lo.FromPtr(items[0].PeriodStart)
	serviceEnd = lo.FromPtr(items[0].PeriodEnd)
	for _, li := range items {
		total = total.Add(li.Amount)
	}
	return total, serviceStart, serviceEnd
}

// fetchTabsInvoiceID returns the first Tabs invoice for the contract + issue date (empty if none yet).
func (s *InvoiceService) fetchTabsInvoiceID(ctx context.Context, contractID string, issueDate string) (string, error) {
	resp, err := s.client.ListInvoicesByContract(ctx, contractID, issueDate)
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Payload.Data) == 0 {
		return "", nil
	}
	return resp.Payload.Data[0].ID, nil
}

// buildObligation maps an aggregated category's resolved fields to a Tabs obligation request.
func buildObligation(billingStartDate time.Time, productID string, cadence types.InvoiceCadence, total decimal.Decimal, serviceStart, serviceEnd time.Time) CreateObligationRequest {
	return CreateObligationRequest{
		ServiceStartDate: serviceStart.Format(time.DateOnly),
		ServiceEndDate:   serviceEnd.Format(time.DateOnly),
		BillingSchedule: BillingSchedule{
			StartDate:           billingStartDate.Format(time.DateOnly),
			InvoiceDateStrategy: invoiceDateStrategy(cadence),
			IsRecurring:         false,
			Interval:            "NONE",
			IntervalFrequency:   1,
			NetPaymentTerms:     1,
			Quantity:            1,
			BillingType:         "FLAT",
			PricingType:         "SIMPLE",
			InvoiceType:         "INVOICE",
			ProductId:           productID,
			Pricing: []ObligationPricing{{
				Tier:        1,
				Amount:      total.InexactFloat64(),
				AmountType:  "TOTAL_INVOICE",
				TierMinimum: 0,
			}},
		},
	}
}

// invoiceDateStrategy maps a flexprice invoice cadence to a Tabs invoiceDateStrategy.
func invoiceDateStrategy(cadence types.InvoiceCadence) string {
	if cadence == types.InvoiceCadenceAdvance {
		return invoiceDateStrategyFirstOfPeriod
	}
	return invoiceDateStrategyArrears
}

// lineItemIDs returns the distinct non-empty ids of the line items.
func lineItemIDs(lineItems []*invoice.InvoiceLineItem) []string {
	ids := make([]string, 0, len(lineItems))
	for _, li := range lineItems {
		if li.ID != "" {
			ids = append(ids, li.ID)
		}
	}
	return lo.Uniq(ids)
}
