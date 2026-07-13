package tabs

import (
	"context"
	"fmt"
	"strings"
	"time"

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
		logger:       logger,
	}
}

func (s *InvoiceService) SyncInvoiceToTabs(ctx context.Context, req TabsInvoiceSyncRequest) (*TabsInvoiceSyncResponse, error) {
	inv, err := s.invoiceRepo.Get(ctx, req.InvoiceID)
	if err != nil {
		return nil, err
	}

	// Idempotency: if the invoice is already mapped to Tabs, return the existing mapping.
	if existing, ok, err := s.existingInvoiceMapping(ctx, req.InvoiceID); err != nil {
		return nil, err
	} else if ok {
		return &TabsInvoiceSyncResponse{
			ContractID:     existing.ProviderEntityID,
			TabsCustomerID: metadataString(existing.Metadata, "tabs_customer_id"),
			TabsInvoiceID:  metadataString(existing.Metadata, "tabs_invoice_id"),
			Currency:       inv.Currency,
		}, nil
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

	err = s.syncObligations(ctx, inv, tabsContractID)
	issueDate := inv.IssueDate
	if err != nil {
		return nil, err
	}

	// Look up the Tabs invoice(s) generated for the obligations (by contract + issue date). The
	// invoice is mapped to its Tabs contract; the Tabs invoice id is recorded in the metadata.
	tabsInvoiceID, err := s.fetchTabsInvoiceID(ctx, tabsContractID, issueDate.Format(tabsDateLayout))
	if err != nil {
		return nil, err
	}

	if tabsInvoiceID == "" {
		return nil, fmt.Errorf("unable to create invoice in tabs")
	}

	if err := s.mapInvoice(ctx, inv, tabsContractID, tabsCustomerID, tabsInvoiceID); err != nil {
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

// mapInvoice persists the flexprice-invoice -> tabs-contract mapping, recording the Tabs customer
// and invoice ids in the metadata. It makes the sync idempotent at the invoice level.
func (s *InvoiceService) mapInvoice(ctx context.Context, inv *invoice.Invoice, contractID, tabsCustomerID, tabsInvoiceID string) error {
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         inv.ID,
		EntityType:       types.IntegrationEntityTypeInvoice,
		ProviderType:     string(types.SecretProviderTabs),
		ProviderEntityID: tabsInvoiceID,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
		Metadata: map[string]interface{}{
			"contract_id":      contractID,
			"tabs_customer_id": tabsCustomerID,
			"tabs_invoice_id":  tabsInvoiceID,
			"synced_at":        time.Now().UTC().Format(time.RFC3339),
		},
	}
	return s.mappingRepo.Create(ctx, mapping)
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
	ids := lineItemIDs(lineItems)
	synced := make(map[string]struct{}, len(ids))
	if len(ids) == 0 {
		return synced, nil
	}

	filter := types.NewNoLimitEntityIntegrationMappingFilter()
	filter.EntityIDs = ids
	filter.EntityType = types.IntegrationEntityTypeInvoiceLineItem
	filter.ProviderTypes = []string{string(types.SecretProviderTabs)}
	filter.Status = lo.ToPtr(types.StatusPublished)

	existing, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, m := range existing {
		synced[m.EntityID] = struct{}{}
	}
	return synced, nil
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

// metadataString safely reads a string value from a mapping's metadata.
func metadataString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}
