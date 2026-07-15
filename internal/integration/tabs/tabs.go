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
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const tabsDateLayout = "2006-01-02"

// tabsInvoiceSyncLockTTL bounds how long a per-invoice sync lock is held if the holder crashes without
// releasing it. Aligned to the sync activity's StartToCloseTimeout (5m) so a run can't outlive its
// lock; on normal completion or error the lock is released immediately via defer.
const tabsInvoiceSyncLockTTL = 2 * time.Minute

type TabsInvoiceService interface {
	SyncInvoiceToTabs(ctx context.Context, req TabsInvoiceSyncRequest) (*TabsInvoiceSyncResponse, error)
}

type InvoiceService struct {
	client       TabsClient
	customerRepo customer.Repository
	subRepo      subscription.Repository
	planRepo     plan.Repository
	priceRepo    price.Repository
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
	priceRepo price.Repository,
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
		priceRepo:    priceRepo,
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

	// If the invoice was already synced, re-sync it rather than short-circuiting: the previously
	// synced obligations are deleted from Tabs (and their mappings dropped) below so syncObligations
	// recreates them from the invoice's current line items, and a fresh Tabs invoice is fetched. The
	// existing invoice mapping is updated in place with the new Tabs invoice id.
	existingMapping, alreadySynced, err := s.existingInvoiceMapping(ctx, req.InvoiceID)
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

	// On a re-sync, drop the previously-synced obligations (from Tabs and locally) so they are
	// recreated from the invoice's current line items below. Deletion is driven by the obligation
	// ids recorded on the invoice mapping, so obligations whose line items were removed (or recreated
	// with new ids) since the last sync are still cleaned up.
	if alreadySynced {
		if err = s.deletePreviousObligations(ctx, existingMapping, inv, tabsContractID); err != nil {
			return nil, err
		}
	}

	if err = s.syncObligations(ctx, inv, tabsContractID); err != nil {
		return nil, err
	}

	// Look up the Tabs invoice(s) generated for the obligations (by contract + issue date). The
	// invoice is mapped to its Tabs contract; the Tabs invoice id is recorded in the metadata.
	issueDate := lo.FromPtr(inv.IssueDate)
	tabsInvoiceID, err := s.fetchTabsInvoiceID(ctx, tabsContractID, issueDate.Format(tabsDateLayout))
	if err != nil {
		return nil, err
	}

	if tabsInvoiceID == "" {
		return nil, fmt.Errorf("unable to create invoice in tabs")
	}

	// Record the obligations synced for this invoice so a later re-sync can delete them even if the
	// invoice's line items change (or are recreated with new ids) in the meantime.
	obligationIDs, err := s.syncedObligationIDs(ctx, inv.LineItems)
	if err != nil {
		return nil, err
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

// existingInvoiceMapping returns the published Tabs mapping for the invoice, if any.
func (s *InvoiceService) existingInvoiceMapping(ctx context.Context, invoiceID string) (*entityintegrationmapping.EntityIntegrationMapping, bool, error) {
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

// persistInvoiceMapping records the flexprice-invoice -> tabs mapping, storing the Tabs customer,
// contract and invoice ids in the metadata. On a first sync it creates the mapping; on a re-sync
// it updates the existing mapping in place with the freshly-fetched Tabs invoice id. This keeps the
// invoice-level sync idempotent.
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

// tabsInvoiceMetadata builds the metadata recorded on an invoice mapping. obligation_ids records the
// Tabs obligations synced for the invoice so a re-sync can delete them independently of the current
// line items.
func tabsInvoiceMetadata(contractID, tabsCustomerID, tabsInvoiceID string, obligationIDs []string) map[string]interface{} {
	return map[string]interface{}{
		"contract_id":      contractID,
		"tabs_customer_id": tabsCustomerID,
		"tabs_invoice_id":  tabsInvoiceID,
		"obligation_ids":   obligationIDs,
		"synced_at":        time.Now().UTC().Format(time.RFC3339),
	}
}

// deletePreviousObligations removes the obligations recorded on the invoice's existing Tabs mapping
// from Tabs and drops their line-item -> obligation mappings, so a re-sync recreates obligations from
// the invoice's current line items. Because the obligation ids are read from the invoice mapping (not
// the current line items), obligations whose line items were removed — or recreated with new ids —
// since the last sync are still cleaned up. Obligation deletion is idempotent (see DeleteObligation),
// so the enclosing Temporal activity is safe to retry.
func (s *InvoiceService) deletePreviousObligations(ctx context.Context, mapping *entityintegrationmapping.EntityIntegrationMapping, inv *invoice.Invoice, contractID string) error {
	obligationIDs := metadataStrings(mapping.Metadata, "obligation_ids")

	// Match the line-item mappings to drop by obligation id, so mappings for removed/recreated line
	// items are found too. Fall back to the current line items for invoices mapped before obligation
	// ids were recorded.
	var lineItemMappings []*entityintegrationmapping.EntityIntegrationMapping
	var err error
	if len(obligationIDs) > 0 {
		lineItemMappings, err = s.mappingsByObligationIDs(ctx, obligationIDs)
	} else {
		lineItemMappings, err = s.lineItemObligationMappings(ctx, inv.LineItems)
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

// syncedObligationIDs returns the distinct Tabs obligation ids currently mapped to the invoice's line
// items, i.e. the obligations just synced.
func (s *InvoiceService) syncedObligationIDs(ctx context.Context, lineItems []*invoice.InvoiceLineItem) ([]string, error) {
	mappings, err := s.lineItemObligationMappings(ctx, lineItems)
	if err != nil {
		return nil, err
	}
	return distinctObligationIDs(mappings), nil
}

// mappingsByObligationIDs returns the published line-item -> Tabs-obligation mappings whose obligation
// (provider entity) id is in the given set. Obligation ids are unique per Tabs obligation, so this
// scopes to a single invoice's obligations.
func (s *InvoiceService) mappingsByObligationIDs(ctx context.Context, obligationIDs []string) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
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

// distinctObligationIDs returns the distinct non-empty obligation (provider entity) ids from the
// given line-item mappings.
func distinctObligationIDs(mappings []*entityintegrationmapping.EntityIntegrationMapping) []string {
	ids := make([]string, 0, len(mappings))
	for _, m := range mappings {
		if m.ProviderEntityID != "" {
			ids = append(ids, m.ProviderEntityID)
		}
	}
	return lo.Uniq(ids)
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

// ensureCustomer guarantees a flexprice-customer -> tabs-customer mapping exists, creating the
// customer in Tabs (async job) if needed, and returns the Tabs customer id. It is idempotent:
// if a mapping already exists it is returned without calling Tabs.
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

// ensureContract guarantees a flexprice-subscription -> tabs-contract mapping exists, creating
// the contract in Tabs (synchronous) if needed, and returns the Tabs contract id. It is
// idempotent: if a mapping already exists it is returned without calling Tabs.
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
		Name:       s.contractName(ctx, subscriptionID),
		CustomerID: tabsCustomerID,
	})
	if err != nil {
		return "", err
	}

	// The contract is created in NEW status; mark it PROCESSED before persisting the mapping so
	// a mapping only ever records a fully-processed contract.
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

// contractName derives the Tabs contract name from the subscription's plan name, falling back
// to a subscription-id based name if the subscription or plan cannot be resolved.
func (s *InvoiceService) contractName(ctx context.Context, subscriptionID string) string {
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

// syncObligations creates Tabs obligations for an invoice's line items on the given contract.
// Line items are grouped by the Tabs product their price maps to (a product is auto-created and
// mapped when the price isn't mapped yet); each product group is aggregated into a single
// obligation whose amount is the sum of the group's line items.
//
// It is idempotent and safe to retry after a partial failure: after each obligation is created,
// a line-item -> obligation mapping is persisted for every line item in the group, and any group
// whose line items are already mapped is skipped on a subsequent run. This prevents a retry (e.g.
// when a later group's CreateObligation fails) from duplicating obligations already pushed to Tabs.
func (s *InvoiceService) syncObligations(ctx context.Context, inv *invoice.Invoice, contractID string) (err error) {
	if len(inv.LineItems) == 0 {
		return nil
	}

	// The Tabs product name/description and the obligation cadence all come from the price
	// (bulk-loaded by PriceID), not the invoice/subscription line item.
	pricesByID, err := s.pricesByID(ctx, inv.LineItems)
	if err != nil {
		return err
	}

	// Resolve every referenced price to a Tabs product id, creating (and mapping) a product in Tabs
	// for any price that isn't mapped yet.
	productByPrice, err := s.ensureProducts(ctx, inv.LineItems, pricesByID)
	if err != nil {
		return err
	}

	// Line items already pushed to a Tabs obligation on a previous (possibly partial) attempt.
	syncedLineItems, err := s.syncedLineItemIDs(ctx, inv.LineItems)
	if err != nil {
		return err
	}

	// Group line items by Tabs product id and emit one obligation per group. The billingSchedule
	// startDate is the invoice issue date.
	billingStartDate := lo.FromPtr(inv.IssueDate)

	itemsByProduct := groupByProduct(inv.LineItems, productByPrice)
	for productID, items := range itemsByProduct {
		// A product group's obligation is created by a single atomic call, so if any of its line
		// items is already mapped the whole group was synced on a prior attempt — skip it.
		if groupAlreadySynced(items, syncedLineItems) {
			continue
		}

		// Resolve the obligation fields from the group before building the request. Cadence comes
		// from the price; total and the service period come from the items.
		cadence := pricesByID[lo.FromPtr(items[0].PriceID)].InvoiceCadence
		total, serviceStart, serviceEnd := aggregateLineItems(items)

		req := buildObligation(billingStartDate, productID, cadence, total, serviceStart, serviceEnd)
		resp, cErr := s.client.CreateObligation(ctx, contractID, req)
		if cErr != nil {
			return cErr
		}

		// Record the line-item -> obligation mapping so a retry skips this group.
		if err = s.mapLineItemsToObligation(ctx, items, resp.Payload.ID); err != nil {
			return err
		}
	}

	return nil
}

// syncedLineItemIDs returns the set of the invoice's line item ids that already carry a Tabs
// obligation mapping, i.e. were synced on a previous attempt.
func (s *InvoiceService) syncedLineItemIDs(ctx context.Context, lineItems []*invoice.InvoiceLineItem) (map[string]struct{}, error) {
	mappings, err := s.lineItemObligationMappings(ctx, lineItems)
	if err != nil {
		return nil, err
	}
	synced := make(map[string]struct{}, len(mappings))
	for _, m := range mappings {
		synced[m.EntityID] = struct{}{}
	}
	return synced, nil
}

// lineItemObligationMappings returns the published line-item -> Tabs-obligation mappings for the
// invoice's line items (empty when the invoice has no line items).
func (s *InvoiceService) lineItemObligationMappings(ctx context.Context, lineItems []*invoice.InvoiceLineItem) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
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

// groupAlreadySynced reports whether a product group was already synced to Tabs. A group's
// obligation is created atomically, so a single already-mapped line item implies the whole group.
func groupAlreadySynced(items []*invoice.InvoiceLineItem, synced map[string]struct{}) bool {
	for _, li := range items {
		if _, ok := synced[li.ID]; ok {
			return true
		}
	}
	return false
}

// mapLineItemsToObligation persists a line-item -> Tabs-obligation mapping for every line item in
// a synced group, making the obligation retrievable (and the group skippable) on a retry.
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

// groupByProduct groups line items by the Tabs product their price maps to. Line items whose
// price has no resolvable product are skipped.
func groupByProduct(lineItems []*invoice.InvoiceLineItem, productByPrice map[string]string) map[string][]*invoice.InvoiceLineItem {
	itemsByProduct := make(map[string][]*invoice.InvoiceLineItem)
	for _, li := range lineItems {
		productID := productByPrice[lo.FromPtr(li.PriceID)]
		if productID == "" {
			continue
		}
		itemsByProduct[productID] = append(itemsByProduct[productID], li)
	}
	return itemsByProduct
}

// aggregateLineItems sums the line items' amounts and widens the service period to cover every
// item, returning the group total and the earliest start / latest end.
func aggregateLineItems(items []*invoice.InvoiceLineItem) (total decimal.Decimal, serviceStart, serviceEnd time.Time) {
	total = decimal.Zero
	serviceStart = lo.FromPtr(items[0].PeriodStart)
	serviceEnd = lo.FromPtr(items[0].PeriodEnd)
	for _, li := range items {
		total = total.Add(li.Amount)
	}
	return total, serviceStart, serviceEnd
}

// ensureProducts resolves every line item's price to a Tabs product id. Existing price -> product
// mappings are loaded in one query; any price without a mapping gets a product created in Tabs and
// a mapping persisted. Returns a priceID -> tabsProductID map.
func (s *InvoiceService) ensureProducts(ctx context.Context, lineItems []*invoice.InvoiceLineItem, pricesByID map[string]*price.Price) (map[string]string, error) {
	priceIDs := lineItemPriceIDs(lineItems)
	out := make(map[string]string, len(priceIDs))
	if len(priceIDs) == 0 {
		return out, nil
	}

	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityIDs = priceIDs
	filter.EntityType = types.IntegrationEntityTypePrice
	filter.ProviderTypes = []string{string(types.SecretProviderTabs)}
	filter.Status = lo.ToPtr(types.StatusPublished)

	existing, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, m := range existing {
		out[m.EntityID] = m.ProviderEntityID
	}

	for _, priceID := range priceIDs {
		if _, ok := out[priceID]; ok {
			continue
		}
		productID, cErr := s.createProduct(ctx, priceID, pricesByID[priceID])
		if cErr != nil {
			return nil, cErr
		}
		out[priceID] = productID
	}
	return out, nil
}

// createProduct creates a Tabs product for a flexprice price and persists the price -> product
// mapping, returning the new Tabs product id.
func (s *InvoiceService) createProduct(ctx context.Context, priceID string, p *price.Price) (string, error) {
	name, description := productNameDescription(priceID, p)
	resp, err := s.client.CreateProduct(ctx, CreateProductRequest{
		Status:      "ACTIVE",
		Name:        name,
		Description: description,
	})
	if err != nil {
		return "", err
	}
	productID := resp.Payload.ID

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         priceID,
		EntityType:       types.IntegrationEntityTypePrice,
		ProviderType:     string(types.SecretProviderTabs),
		ProviderEntityID: productID,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	if err = s.mappingRepo.Create(ctx, mapping); err != nil {
		return "", err
	}

	s.logger.Info(ctx, "tabs: product created and mapped",
		"flexprice_price_id", priceID, "tabs_product_id", productID)
	return productID, nil
}

// fetchTabsInvoiceID looks up the Tabs invoices created for the given contract and obligation
// issue date, returning the first Tabs invoice id (empty when none exist yet).
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

// pricesByID bulk-loads the invoice line items' prices and maps priceID -> price.
func (s *InvoiceService) pricesByID(ctx context.Context, lineItems []*invoice.InvoiceLineItem) (map[string]*price.Price, error) {
	priceIDs := lineItemPriceIDs(lineItems)
	out := make(map[string]*price.Price, len(priceIDs))
	if len(priceIDs) == 0 {
		return out, nil
	}
	prices, err := s.priceRepo.List(ctx, types.NewNoLimitPriceFilter().WithPriceIDs(priceIDs))
	if err != nil {
		return nil, err
	}
	for _, p := range prices {
		out[p.ID] = p
	}
	return out, nil
}

// buildObligation maps an aggregated product group's resolved fields to a Tabs obligation request.
func buildObligation(billingStartDate time.Time, productID string, cadence types.InvoiceCadence, total decimal.Decimal, serviceStart, serviceEnd time.Time) CreateObligationRequest {
	return CreateObligationRequest{
		ServiceStartDate: serviceStart.Format(tabsDateLayout),
		ServiceEndDate:   serviceEnd.Format(tabsDateLayout),
		BillingSchedule: BillingSchedule{
			StartDate:           billingStartDate.Format(tabsDateLayout),
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

const (
	invoiceDateStrategyArrears       = "ARREARS"
	invoiceDateStrategyFirstOfPeriod = "FIRST_OF_PERIOD"
)

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

// lineItemPriceIDs returns the distinct non-empty price ids referenced by the line items.
func lineItemPriceIDs(lineItems []*invoice.InvoiceLineItem) []string {
	ids := make([]string, 0, len(lineItems))
	for _, li := range lineItems {
		if li.PriceID != nil && *li.PriceID != "" {
			ids = append(ids, *li.PriceID)
		}
	}
	return lo.Uniq(ids)
}

// productNameDescription derives the Tabs product name and description from the price, falling back
// to the price id when the price is missing or unnamed.
func productNameDescription(priceID string, p *price.Price) (name, description string) {
	name = priceID
	if p != nil && p.DisplayName != "" {
		name = p.DisplayName
	}
	if p != nil {
		description = p.Description
	}
	if description == "" {
		description = name
	}
	return name, description
}
