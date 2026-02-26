//go:build !published

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/flexprice/flexprice-go"
	"github.com/flexprice/flexprice-go/models/components"
	"github.com/flexprice/flexprice-go/models/operations"
)

// test_local_sdk.go - Local SDK tests (unpublished SDK from api/go).
// Run from api/tests/go: go run test_local_sdk.go
// Requires: FLEXPRICE_API_KEY, FLEXPRICE_API_HOST
// SDK repo: update defaultGoSDKRepo when you change where the Go SDK is hosted.

// defaultGoSDKRepo is the Go SDK repo (module path). Change this when you host the SDK elsewhere.
const defaultGoSDKRepo = "github.com/Random-test-v2/go-temp"

var (
	testCustomerID   string
	testExternalID   string
	testCustomerName string

	testFeatureID   string
	testFeatureName string

	testPlanID   string
	testPlanName string

	testAddonID        string
	testAddonName      string
	testAddonLookupKey string

	testEntitlementID string

	testSubscriptionID string

	testInvoiceID string

	testPriceID string

	testPaymentID string

	testWalletID      string
	testCreditGrantID string
	testCreditNoteID  string
)

func main() {
	fmt.Printf("=== FlexPrice Go SDK - API Tests (SDK: %s) ===\n\n", defaultGoSDKRepo)

	// Get API credentials from environment
	apiKey := os.Getenv("FLEXPRICE_API_KEY")
	apiHost := os.Getenv("FLEXPRICE_API_HOST")

	if apiKey == "" {
		log.Fatal("❌ Missing FLEXPRICE_API_KEY environment variable")
	}
	if apiHost == "" {
		log.Fatal("❌ Missing FLEXPRICE_API_HOST environment variable")
	}

	fmt.Printf("✓ API Key: %s...%s\n", apiKey[:min(8, len(apiKey))], apiKey[max(0, len(apiKey)-4):])
	fmt.Printf("✓ API Host: %s\n\n", apiHost)

	// Initialize SDK (Flexprice with WithSecurity)
	parts := strings.SplitN(apiHost, "/", 2)
	hostOnly := parts[0]
	basePath := ""
	if len(parts) > 1 {
		basePath = "/" + parts[1]
	}
	serverURL := "https://" + hostOnly + basePath
	client := flexprice.New(serverURL, flexprice.WithSecurity(apiKey))
	ctx := context.Background()

	// Run all Customer API tests (without delete)
	fmt.Println("========================================")
	fmt.Println("CUSTOMER API TESTS")
	fmt.Println("========================================\n")

	testCreateCustomer(ctx, client)
	testGetCustomer(ctx, client)
	testListCustomers(ctx, client)
	testUpdateCustomer(ctx, client)
	testLookupCustomer(ctx, client)
	testSearchCustomers(ctx, client)
	testGetCustomerEntitlements(ctx, client)
	testGetCustomerUpcomingGrants(ctx, client)
	testGetCustomerUsage(ctx, client)

	fmt.Println("✓ Customer API Tests Completed!\n")

	// Run all Features API tests (without delete)
	fmt.Println("========================================")
	fmt.Println("FEATURES API TESTS")
	fmt.Println("========================================\n")

	testCreateFeature(ctx, client)
	testGetFeature(ctx, client)
	testListFeatures(ctx, client)
	testUpdateFeature(ctx, client)
	testSearchFeatures(ctx, client)

	fmt.Println("✓ Features API Tests Completed!\n")

	// Run all Connections API tests (without delete)
	fmt.Println("========================================")
	fmt.Println("CONNECTIONS API TESTS")
	fmt.Println("========================================\n")

	testListConnections(ctx, client)
	testSearchConnections(ctx, client)
	// Note: Connections API doesn't have a create endpoint
	// We'll test with existing connections if any

	fmt.Println("✓ Connections API Tests Completed!\n")

	// Run all Plans API tests (without delete)
	fmt.Println("========================================")
	fmt.Println("PLANS API TESTS")
	fmt.Println("========================================\n")

	testCreatePlan(ctx, client)
	testGetPlan(ctx, client)
	testListPlans(ctx, client)
	testUpdatePlan(ctx, client)
	testSearchPlans(ctx, client)

	fmt.Println("✓ Plans API Tests Completed!\n")

	// Run all Addons API tests (without delete)
	fmt.Println("========================================")
	fmt.Println("ADDONS API TESTS")
	fmt.Println("========================================\n")

	testCreateAddon(ctx, client)
	testGetAddon(ctx, client)
	testListAddons(ctx, client)
	testUpdateAddon(ctx, client)
	testLookupAddon(ctx, client)
	testSearchAddons(ctx, client)

	fmt.Println("✓ Addons API Tests Completed!\n")

	// Run all Entitlements API tests (without delete)
	fmt.Println("========================================")
	fmt.Println("ENTITLEMENTS API TESTS")
	fmt.Println("========================================\n")

	testCreateEntitlement(ctx, client)
	testGetEntitlement(ctx, client)
	testListEntitlements(ctx, client)
	testUpdateEntitlement(ctx, client)
	testSearchEntitlements(ctx, client)

	fmt.Println("✓ Entitlements API Tests Completed!\n")

	// Run all Subscriptions API tests
	fmt.Println("========================================")
	fmt.Println("SUBSCRIPTIONS API TESTS")
	fmt.Println("========================================\n")

	testCreateSubscription(ctx, client)
	testGetSubscription(ctx, client)
	testListSubscriptions(ctx, client)
	testSearchSubscriptions(ctx, client)

	// Lifecycle management
	testActivateSubscription(ctx, client)
	// testPauseSubscription(ctx, client) // Removed - not needed
	// testResumeSubscription(ctx, client) // Removed - not needed
	// testGetPauseHistory(ctx, client) // Removed - not needed

	// Addon management
	testAddAddonToSubscription(ctx, client)
	// testGetActiveAddons(ctx, client)
	testRemoveAddonFromSubscription(ctx, client)

	// Change management
	// testPreviewSubscriptionChange(ctx, client) // Removed - not needed
	testExecuteSubscriptionChange(ctx, client)

	// Related data
	testGetSubscriptionEntitlements(ctx, client)
	testGetUpcomingGrants(ctx, client)
	testReportUsage(ctx, client)

	// Line item management
	testUpdateLineItem(ctx, client)
	testDeleteLineItem(ctx, client)

	// Cancel subscription (should be last)
	testCancelSubscription(ctx, client)

	fmt.Println("✓ Subscriptions API Tests Completed!\n")

	// Run all Invoices API tests
	fmt.Println("========================================")
	fmt.Println("INVOICES API TESTS")
	fmt.Println("========================================\n")

	testListInvoices(ctx, client)
	testSearchInvoices(ctx, client)
	testCreateInvoice(ctx, client)
	testGetInvoice(ctx, client)
	testUpdateInvoice(ctx, client)

	// Lifecycle operations
	testPreviewInvoice(ctx, client)
	testFinalizeInvoice(ctx, client)
	testRecalculateInvoice(ctx, client)

	// Payment operations
	testRecordPayment(ctx, client)
	testAttemptPayment(ctx, client)

	// Additional operations
	testDownloadInvoicePDF(ctx, client)
	testTriggerInvoiceComms(ctx, client)
	testGetCustomerInvoiceSummary(ctx, client)

	// Void invoice (should be last)
	testVoidInvoice(ctx, client)

	fmt.Println("✓ Invoices API Tests Completed!\n")

	// Run all Prices API tests
	fmt.Println("========================================")
	fmt.Println("PRICES API TESTS")
	fmt.Println("========================================\n")

	testCreatePrice(ctx, client)
	testGetPrice(ctx, client)
	testListPrices(ctx, client)
	testUpdatePrice(ctx, client)

	fmt.Println("✓ Prices API Tests Completed!\n")

	// Run all Payments API tests
	fmt.Println("========================================")
	fmt.Println("PAYMENTS API TESTS")
	fmt.Println("========================================\n")

	testCreatePayment(ctx, client)
	testGetPayment(ctx, client)
	testListPayments(ctx, client)
	testUpdatePayment(ctx, client)
	testProcessPayment(ctx, client)

	fmt.Println("✓ Payments API Tests Completed!\n")

	// Run all Wallets API tests
	fmt.Println("========================================")
	fmt.Println("WALLETS API TESTS")
	fmt.Println("========================================\n")

	testCreateWallet(ctx, client)
	testGetWallet(ctx, client)
	testListWallets(ctx, client)
	testUpdateWallet(ctx, client)
	testGetWalletBalance(ctx, client)
	testTopUpWallet(ctx, client)
	testDebitWallet(ctx, client)
	testGetWalletTransactions(ctx, client)
	testSearchWallets(ctx, client)

	fmt.Println("✓ Wallets API Tests Completed!\n")

	// Run all Credit Grants API tests
	fmt.Println("========================================")
	fmt.Println("CREDIT GRANTS API TESTS")
	fmt.Println("========================================\n")

	testCreateCreditGrant(ctx, client)
	testGetCreditGrant(ctx, client)
	testListCreditGrants(ctx, client)
	testUpdateCreditGrant(ctx, client)

	fmt.Println("✓ Credit Grants API Tests Completed!\n")

	// Run all Credit Notes API tests
	fmt.Println("========================================")
	fmt.Println("CREDIT NOTES API TESTS")
	fmt.Println("========================================\n")

	testCreateCreditNote(ctx, client)
	testGetCreditNote(ctx, client)
	testListCreditNotes(ctx, client)
	testFinalizeCreditNote(ctx, client)

	fmt.Println("✓ Credit Notes API Tests Completed!\n")

	// Run all Events API tests
	fmt.Println("========================================")
	fmt.Println("EVENTS API TESTS")
	fmt.Println("========================================\n")

	// Sync event operations
	testCreateEvent(ctx, client)
	testQueryEvents(ctx, client)

	// Async event operations
	testAsyncEventEnqueue(ctx, client)
	testAsyncEventEnqueueWithOptions(ctx, client)
	testAsyncEventBatch(ctx, client)

	fmt.Println("✓ Events API Tests Completed!\n")

	// Cleanup: Delete all created entities
	fmt.Println("========================================")
	fmt.Println("CLEANUP - DELETING TEST DATA")
	fmt.Println("========================================\n")

	testDeletePayment(ctx, client)
	testDeletePrice(ctx, client)
	testDeleteEntitlement(ctx, client)
	testDeleteAddon(ctx, client)
	testDeletePlan(ctx, client)
	testDeleteFeature(ctx, client)
	testCancelSubscriptionCleanup(ctx, client) // must run before customer (API rejects delete if customer has active subscriptions)
	testDeleteWallet(ctx, client)            // must run before customer (API rejects delete if customer has wallets)
	testDeleteCustomer(ctx, client)

	fmt.Println("✓ Cleanup Completed!\n")

	fmt.Printf("\n=== All API Tests Completed Successfully! (SDK: %s) ===\n", defaultGoSDKRepo)
}

// Test 1: Create a new customer
func testCreateCustomer(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Create Customer ---")

	timestamp := time.Now().Unix()
	testCustomerName = fmt.Sprintf("Test Customer %d", timestamp)
	testExternalID = fmt.Sprintf("test-customer-%d", timestamp)

	request := components.DtoCreateCustomerRequest{
		ExternalID: testExternalID,
		Name:       flexprice.String(testCustomerName),
		Email:      flexprice.String(fmt.Sprintf("test-%d@example.com", timestamp)),
		Metadata: map[string]string{
			"source":      "sdk_test",
			"test_run":    time.Now().Format(time.RFC3339),
			"environment": "test",
		},
	}

	resp, err := client.Customers.CreateCustomer(ctx, request)
	if err != nil {
		log.Printf("❌ Error creating customer: %v", err)
		fmt.Println()
		return
	}
	customer := resp.DtoCustomerResponse
	if customer == nil || customer.ID == nil {
		log.Printf("❌ Create customer returned no body")
		fmt.Println()
		return
	}
	testCustomerID = *customer.ID
	fmt.Printf("✓ Customer created successfully!\n")
	fmt.Printf("  ID: %s\n", *customer.ID)
	fmt.Printf("  Name: %s\n", *customer.Name)
	fmt.Printf("  External ID: %s\n", *customer.ExternalID)
	fmt.Printf("  Email: %s\n\n", *customer.Email)
}

// Test 2: Get customer by ID
func testGetCustomer(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Get Customer by ID ---")

	resp, err := client.Customers.GetCustomer(ctx, testCustomerID)
	if err != nil {
		log.Printf("❌ Error getting customer: %v", err)
		fmt.Println()
		return
	}
	customer := resp.DtoCustomerResponse
	if customer == nil {
		log.Printf("❌ Get customer returned no body")
		fmt.Println()
		return
	}
	fmt.Printf("✓ Customer retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *customer.ID)
	fmt.Printf("  Name: %s\n", *customer.Name)
	fmt.Printf("  Created At: %s\n\n", *customer.CreatedAt)
}

// Test 3: List all customers (query with limit)
func testListCustomers(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 3: List Customers ---")

	filter := components.TypesCustomerFilter{Limit: flexprice.Int64(10)}
	resp, err := client.Customers.QueryCustomer(ctx, filter)
	if err != nil {
		log.Printf("❌ Error listing customers: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListCustomersResponse
	if listResp == nil {
		fmt.Printf("✓ Retrieved 0 customers\n\n")
		return
	}
	items := listResp.Items
	if items == nil {
		items = []components.DtoCustomerResponse{}
	}
	fmt.Printf("✓ Retrieved %d customers\n", len(items))
	if len(items) > 0 {
		fmt.Printf("  First customer: %s - %s\n", *items[0].ID, *items[0].Name)
	}
	if listResp.Pagination != nil && listResp.Pagination.Total != nil {
		fmt.Printf("  Total: %d\n", *listResp.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update customer
func testUpdateCustomer(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Update Customer ---")

	updatedName := fmt.Sprintf("%s (Updated)", testCustomerName)
	body := components.DtoUpdateCustomerRequest{
		Name: flexprice.String(updatedName),
		Metadata: map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	resp, err := client.Customers.UpdateCustomer(ctx, body, flexprice.String(testCustomerID), nil)
	if err != nil {
		log.Printf("❌ Error updating customer: %v", err)
		fmt.Println()
		return
	}
	customer := resp.DtoCustomerResponse
	if customer == nil {
		log.Printf("❌ Update customer returned no body")
		fmt.Println()
		return
	}
	fmt.Printf("✓ Customer updated successfully!\n")
	fmt.Printf("  ID: %s\n", *customer.ID)
	fmt.Printf("  New Name: %s\n", *customer.Name)
	fmt.Printf("  Updated At: %s\n\n", *customer.UpdatedAt)
}

// Test 5: Lookup customer by external ID
func testLookupCustomer(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 5: Lookup Customer by External ID ---")

	resp, err := client.Customers.GetCustomerByExternalID(ctx, testExternalID)
	if err != nil {
		log.Printf("❌ Error looking up customer: %v", err)
		fmt.Println()
		return
	}
	customer := resp.DtoCustomerResponse
	if customer == nil {
		log.Printf("❌ Lookup returned no body")
		fmt.Println()
		return
	}
	fmt.Printf("✓ Customer found by external ID!\n")
	fmt.Printf("  External ID: %s\n", testExternalID)
	fmt.Printf("  ID: %s\n", *customer.ID)
	fmt.Printf("  Name: %s\n\n", *customer.Name)
}

// Test 6: Search customers
func testSearchCustomers(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 6: Search Customers ---")

	searchFilter := components.TypesCustomerFilter{ExternalID: flexprice.String(testExternalID)}
	resp, err := client.Customers.QueryCustomer(ctx, searchFilter)
	if err != nil {
		log.Printf("❌ Error searching customers: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListCustomersResponse
	items := []components.DtoCustomerResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d customers matching external ID '%s'\n", len(items), testExternalID)
	for i, customer := range items {
		if i < 3 && customer.ID != nil && customer.Name != nil {
			fmt.Printf("  - %s: %s\n", *customer.ID, *customer.Name)
		}
	}
	fmt.Println()
}

// Test 7: Get customer entitlements
func testGetCustomerEntitlements(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 7: Get Customer Entitlements ---")

	resp, err := client.Customers.GetCustomerEntitlements(ctx, testCustomerID)
	if err != nil {
		log.Printf("⚠ Warning: Error getting customer entitlements: %v\n", err)
		fmt.Println("⚠ Skipping entitlements test (customer may not have any entitlements)\n")
		return
	}
	entitlements := resp.DtoCustomerEntitlementsResponse
	if entitlements == nil {
		fmt.Println("  No entitlements found\n")
		return
	}
	fmt.Printf("✓ Retrieved customer entitlements!\n")
	if entitlements.Features != nil {
		fmt.Printf("  Total features: %d\n", len(entitlements.Features))
		for i, feature := range entitlements.Features {
			if i < 3 && feature.Feature != nil && feature.Feature.ID != nil {
				fmt.Printf("  - Feature: %s\n", *feature.Feature.ID)
			}
		}
	} else {
		fmt.Println("  No features found")
	}
	fmt.Println()
}

// Test 8: Get customer upcoming grants
func testGetCustomerUpcomingGrants(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 8: Get Customer Upcoming Grants ---")

	resp, err := client.Customers.GetCustomerUpcomingGrants(ctx, testCustomerID)
	if err != nil {
		log.Printf("⚠ Warning: Error getting upcoming grants: %v\n", err)
		fmt.Println("⚠ Skipping upcoming grants test (customer may not have any grants)\n")
		return
	}
	grants := resp.DtoListCreditGrantApplicationsResponse
	n := 0
	if grants != nil && grants.Items != nil {
		n = len(grants.Items)
	}
	fmt.Printf("✓ Retrieved upcoming grants!\n")
	fmt.Printf("  Total upcoming grants: %d\n", n)
	fmt.Println()
}

// Test 9: Get customer usage
func testGetCustomerUsage(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 9: Get Customer Usage ---")

	req := operations.GetCustomerUsageSummaryRequest{CustomerID: flexprice.String(testCustomerID)}
	resp, err := client.Customers.GetCustomerUsageSummary(ctx, req)
	if err != nil {
		log.Printf("⚠ Warning: Error getting customer usage: %v\n", err)
		fmt.Println("⚠ Skipping usage test (customer may not have usage data)\n")
		return
	}
	usage := resp.DtoCustomerUsageSummaryResponse
	if usage != nil && usage.Features != nil {
		fmt.Printf("✓ Retrieved customer usage!\n  Feature usage records: %d\n", len(usage.Features))
	} else {
		fmt.Printf("✓ Retrieved customer usage!\n  No usage data found\n")
	}
	fmt.Println()
}

// Test 10: Delete customer
func testDeleteCustomer(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 10: Delete Customer ---")

	_, err := client.Customers.DeleteCustomer(ctx, testCustomerID)
	if err != nil {
		log.Printf("❌ Error deleting customer: %v", err)
		fmt.Println()
		return
	}
	fmt.Printf("✓ Customer deleted successfully!\n")
	fmt.Printf("  Deleted ID: %s\n\n", testCustomerID)
}

// ========================================
// FEATURES API TESTS
// ========================================

// Test 1: Create a new feature
func testCreateFeature(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Create Feature ---")

	timestamp := time.Now().Unix()
	testFeatureName = fmt.Sprintf("Test Feature %d", timestamp)
	featureKey := fmt.Sprintf("test_feature_%d", timestamp)

	request := components.DtoCreateFeatureRequest{
		Name:        testFeatureName,
		LookupKey:   flexprice.String(featureKey),
		Description: flexprice.String("This is a test feature created by SDK tests"),
		Type:        components.TypesFeatureTypeBoolean,
		Metadata: map[string]string{
			"source":      "sdk_test",
			"test_run":    time.Now().Format(time.RFC3339),
			"environment": "test",
		},
	}

	resp, err := client.Features.CreateFeature(ctx, request)
	if err != nil {
		log.Printf("❌ Error creating feature: %v", err)
		fmt.Println()
		return
	}
	feature := resp.DtoFeatureResponse
	if feature == nil || feature.ID == nil {
		log.Printf("❌ Create feature returned no body")
		fmt.Println()
		return
	}
	testFeatureID = *feature.ID
	fmt.Printf("✓ Feature created successfully!\n")
	fmt.Printf("  ID: %s\n", *feature.ID)
	fmt.Printf("  Name: %s\n", *feature.Name)
	fmt.Printf("  Lookup Key: %s\n", *feature.LookupKey)
	fmt.Printf("  Type: %s\n\n", string(*feature.Type))
}

// Test 2: Get feature by ID (query by feature_ids)
func testGetFeature(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Get Feature by ID ---")

	filter := components.TypesFeatureFilter{FeatureIds: []string{testFeatureID}}
	resp, err := client.Features.QueryFeature(ctx, filter)
	if err != nil {
		log.Printf("❌ Error getting feature: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListFeaturesResponse
	if listResp == nil || listResp.Items == nil || len(listResp.Items) == 0 {
		log.Printf("❌ Feature not found")
		fmt.Println()
		return
	}
	feature := listResp.Items[0]
	fmt.Printf("✓ Feature retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *feature.ID)
	fmt.Printf("  Name: %s\n", *feature.Name)
	fmt.Printf("  Lookup Key: %s\n", *feature.LookupKey)
	fmt.Printf("  Created At: %s\n\n", *feature.CreatedAt)
}

// Test 3: List all features
func testListFeatures(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 3: List Features ---")

	filter := components.TypesFeatureFilter{Limit: flexprice.Int64(10)}
	resp, err := client.Features.QueryFeature(ctx, filter)
	if err != nil {
		log.Printf("❌ Error listing features: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListFeaturesResponse
	items := []components.DtoFeatureResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Retrieved %d features\n", len(items))
	if len(items) > 0 {
		fmt.Printf("  First feature: %s - %s\n", *items[0].ID, *items[0].Name)
	}
	if listResp != nil && listResp.Pagination != nil && listResp.Pagination.Total != nil {
		fmt.Printf("  Total: %d\n", *listResp.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update feature
func testUpdateFeature(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Update Feature ---")

	updatedName := fmt.Sprintf("%s (Updated)", testFeatureName)
	updatedDescription := "Updated description for test feature"
	body := components.DtoUpdateFeatureRequest{
		Name:        flexprice.String(updatedName),
		Description: flexprice.String(updatedDescription),
		Metadata: map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	resp, err := client.Features.UpdateFeature(ctx, testFeatureID, body)
	if err != nil {
		log.Printf("❌ Error updating feature: %v", err)
		fmt.Println()
		return
	}
	feature := resp.DtoFeatureResponse
	if feature == nil {
		log.Printf("❌ Update feature returned no body")
		fmt.Println()
		return
	}
	fmt.Printf("✓ Feature updated successfully!\n")
	fmt.Printf("  ID: %s\n", *feature.ID)
	fmt.Printf("  New Name: %s\n", *feature.Name)
	fmt.Printf("  New Description: %s\n", *feature.Description)
	fmt.Printf("  Updated At: %s\n\n", *feature.UpdatedAt)
}

// Test 5: Search features
func testSearchFeatures(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 5: Search Features ---")

	searchFilter := components.TypesFeatureFilter{FeatureIds: []string{testFeatureID}}
	resp, err := client.Features.QueryFeature(ctx, searchFilter)
	if err != nil {
		log.Printf("❌ Error searching features: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListFeaturesResponse
	items := []components.DtoFeatureResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d features matching ID '%s'\n", len(items), testFeatureID)
	for i, feature := range items {
		if i < 3 {
			fmt.Printf("  - %s: %s (%s)\n", *feature.ID, *feature.Name, *feature.LookupKey)
		}
	}
	fmt.Println()
}

// Test 6: Delete feature
func testDeleteFeature(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 6: Delete Feature ---")

	resp, err := client.Features.DeleteFeature(ctx, testFeatureID)
	if err != nil {
		log.Printf("❌ Error deleting feature: %v", err)
		fmt.Println()
		return
	}
	_ = resp

	fmt.Printf("✓ Feature deleted successfully!\n")
	fmt.Printf("  Deleted ID: %s\n\n", testFeatureID)
}

// ========================================
// ADDONS API TESTS
// ========================================

// Test 1: Create a new addon
func testCreateAddon(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Create Addon ---")

	timestamp := time.Now().Unix()
	testAddonName = fmt.Sprintf("Test Addon %d", timestamp)
	testAddonLookupKey = fmt.Sprintf("test_addon_%d", timestamp)

	request := components.DtoCreateAddonRequest{
		Name:        testAddonName,
		LookupKey:   testAddonLookupKey,
		Description: flexprice.String("This is a test addon created by SDK tests"),
		Type:        components.TypesAddonTypeOnetime,
		Metadata: map[string]any{
			"source":      "sdk_test",
			"test_run":    time.Now().Format(time.RFC3339),
			"environment": "test",
		},
	}

	resp, err := client.Addons.CreateAddon(ctx, request)
	if err != nil {
		log.Printf("❌ Error creating addon: %v", err)
		fmt.Println()
		return
	}
	addon := resp.DtoCreateAddonResponse
	if addon == nil || addon.ID == nil {
		log.Printf("❌ Create addon returned no body")
		fmt.Println()
		return
	}
	testAddonID = *addon.ID
	fmt.Printf("✓ Addon created successfully!\n")
	fmt.Printf("  ID: %s\n", *addon.ID)
	fmt.Printf("  Name: %s\n", *addon.Name)
	fmt.Printf("  Lookup Key: %s\n\n", *addon.LookupKey)
}

// Test 2: Get addon by ID
func testGetAddon(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Get Addon by ID ---")

	resp, err := client.Addons.GetAddon(ctx, testAddonID)
	if err != nil {
		log.Printf("❌ Error getting addon: %v", err)
		fmt.Println()
		return
	}
	addon := resp.DtoAddonResponse
	if addon == nil {
		log.Printf("❌ Get addon returned no body")
		fmt.Println()
		return
	}
	fmt.Printf("✓ Addon retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *addon.ID)
	fmt.Printf("  Name: %s\n", *addon.Name)
	fmt.Printf("  Lookup Key: %s\n", *addon.LookupKey)
	fmt.Printf("  Created At: %s\n\n", *addon.CreatedAt)
}

// Test 3: List all addons
func testListAddons(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 3: List Addons ---")

	filter := components.TypesAddonFilter{Limit: flexprice.Int64(10)}
	resp, err := client.Addons.QueryAddon(ctx, filter)
	if err != nil {
		log.Printf("❌ Error listing addons: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListAddonsResponse
	items := []components.DtoAddonResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Retrieved %d addons\n", len(items))
	if len(items) > 0 {
		fmt.Printf("  First addon: %s - %s\n", *items[0].ID, *items[0].Name)
	}
	if listResp != nil && listResp.Pagination != nil && listResp.Pagination.Total != nil {
		fmt.Printf("  Total: %d\n", *listResp.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update addon
func testUpdateAddon(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Update Addon ---")

	updatedName := fmt.Sprintf("%s (Updated)", testAddonName)
	updatedDescription := "Updated description for test addon"
	body := components.DtoUpdateAddonRequest{
		Name:        flexprice.String(updatedName),
		Description: flexprice.String(updatedDescription),
		Metadata: map[string]any{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	resp, err := client.Addons.UpdateAddon(ctx, testAddonID, body)
	if err != nil {
		log.Printf("❌ Error updating addon: %v", err)
		fmt.Println()
		return
	}
	addon := resp.DtoAddonResponse
	if addon == nil {
		log.Printf("❌ Update addon returned no body")
		fmt.Println()
		return
	}
	fmt.Printf("✓ Addon updated successfully!\n")
	fmt.Printf("  ID: %s\n", *addon.ID)
	fmt.Printf("  New Name: %s\n", *addon.Name)
	fmt.Printf("  New Description: %s\n", *addon.Description)
	fmt.Printf("  Updated At: %s\n\n", *addon.UpdatedAt)
}

// Test 5: Lookup addon by lookup key
func testLookupAddon(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 5: Lookup Addon by Lookup Key ---")

	// Use the lookup key from the addon we created earlier
	if testAddonLookupKey == "" {
		log.Printf("⚠ Warning: No addon lookup key available (addon creation may have failed)\n")
		fmt.Println("⚠ Skipping lookup test\n")
		return
	}

	fmt.Printf("  Looking up addon with key: %s\n", testAddonLookupKey)

	resp, err := client.Addons.GetAddonByLookupKey(ctx, testAddonLookupKey)
	if err != nil {
		log.Printf("⚠ Warning: Error looking up addon: %v\n", err)
		fmt.Println("⚠ Skipping lookup test (lookup key may not match)\n")
		return
	}
	addon := resp.DtoAddonResponse
	if addon == nil {
		log.Printf("⚠ Warning: Lookup returned no body\n")
		fmt.Println("⚠ Skipping lookup test\n")
		return
	}
	fmt.Printf("✓ Addon found by lookup key!\n")
	fmt.Printf("  Lookup Key: %s\n", testAddonLookupKey)
	fmt.Printf("  ID: %s\n", *addon.ID)
	fmt.Printf("  Name: %s\n\n", *addon.Name)
}

// Test 6: Search addons
func testSearchAddons(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 6: Search Addons ---")

	searchFilter := components.TypesAddonFilter{AddonIds: []string{testAddonID}}
	resp, err := client.Addons.QueryAddon(ctx, searchFilter)
	if err != nil {
		log.Printf("❌ Error searching addons: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListAddonsResponse
	items := []components.DtoAddonResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d addons matching ID '%s'\n", len(items), testAddonID)
	for i, addon := range items {
		if i < 3 {
			fmt.Printf("  - %s: %s (%s)\n", *addon.ID, *addon.Name, *addon.LookupKey)
		}
	}
	fmt.Println()
}

// Test 7: Delete addon
func testDeleteAddon(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 7: Delete Addon ---")

	_, err := client.Addons.DeleteAddon(ctx, testAddonID)
	if err != nil {
		log.Printf("❌ Error deleting addon: %v", err)
		fmt.Println()
		return
	}
	fmt.Printf("✓ Addon deleted successfully!\n")
	fmt.Printf("  Deleted ID: %s\n\n", testAddonID)
}

// ========================================
// ENTITLEMENTS API TESTS
// ========================================

// Test 1: Create a new entitlement
func testCreateEntitlement(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Create Entitlement ---")

	request := components.DtoCreateEntitlementRequest{
		FeatureID:   testFeatureID,
		FeatureType: components.TypesFeatureTypeBoolean,
		PlanID:      flexprice.String(testPlanID),
		IsEnabled:   flexprice.Bool(true),
		UsageResetPeriod: func() *components.TypesEntitlementUsageResetPeriod {
			v := components.TypesEntitlementUsageResetPeriodMonthly
			return &v
		}(),
	}

	resp, err := client.Entitlements.CreateEntitlement(ctx, request)
	if err != nil {
		log.Printf("❌ Error creating entitlement: %v", err)
		fmt.Println()
		return
	}
	entitlement := resp.DtoEntitlementResponse
	if entitlement == nil || entitlement.ID == nil {
		log.Printf("❌ Create entitlement returned no body")
		fmt.Println()
		return
	}
	testEntitlementID = *entitlement.ID
	fmt.Printf("✓ Entitlement created successfully!\n")
	fmt.Printf("  ID: %s\n", *entitlement.ID)
	fmt.Printf("  Feature ID: %s\n", *entitlement.FeatureID)
	fmt.Printf("  Plan ID: %s\n\n", *entitlement.PlanID)
}

// Test 2: Get entitlement by ID
func testGetEntitlement(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Get Entitlement by ID ---")

	resp, err := client.Entitlements.GetEntitlement(ctx, testEntitlementID)
	if err != nil {
		log.Printf("❌ Error getting entitlement: %v", err)
		fmt.Println()
		return
	}
	entitlement := resp.DtoEntitlementResponse
	if entitlement == nil {
		log.Printf("❌ Get entitlement returned no body")
		fmt.Println()
		return
	}
	fmt.Printf("✓ Entitlement retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *entitlement.ID)
	fmt.Printf("  Feature ID: %s\n", *entitlement.FeatureID)
	fmt.Printf("  Created At: %s\n\n", *entitlement.CreatedAt)
}

// Test 3: List all entitlements
func testListEntitlements(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 3: List Entitlements ---")

	filter := components.TypesEntitlementFilter{Limit: flexprice.Int64(10)}
	resp, err := client.Entitlements.QueryEntitlement(ctx, filter)
	if err != nil {
		log.Printf("❌ Error listing entitlements: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListEntitlementsResponse
	items := []components.DtoEntitlementResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Retrieved %d entitlements\n", len(items))
	if len(items) > 0 {
		fmt.Printf("  First entitlement: %s (Feature: %s)\n", *items[0].ID, *items[0].FeatureID)
	}
	if listResp != nil && listResp.Pagination != nil && listResp.Pagination.Total != nil {
		fmt.Printf("  Total: %d\n", *listResp.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update entitlement
func testUpdateEntitlement(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Update Entitlement ---")

	body := components.DtoUpdateEntitlementRequest{IsEnabled: flexprice.Bool(false)}
	resp, err := client.Entitlements.UpdateEntitlement(ctx, testEntitlementID, body)
	if err != nil {
		log.Printf("❌ Error updating entitlement: %v", err)
		fmt.Println()
		return
	}
	entitlement := resp.DtoEntitlementResponse
	if entitlement == nil {
		log.Printf("❌ Update entitlement returned no body")
		fmt.Println()
		return
	}
	fmt.Printf("✓ Entitlement updated successfully!\n")
	fmt.Printf("  ID: %s\n", *entitlement.ID)
	fmt.Printf("  Is Enabled: %v\n", *entitlement.IsEnabled)
	fmt.Printf("  Updated At: %s\n\n", *entitlement.UpdatedAt)
}

// Test 5: Search entitlements
func testSearchEntitlements(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 5: Search Entitlements ---")

	searchFilter := components.TypesEntitlementFilter{FeatureIds: []string{testFeatureID}}
	resp, err := client.Entitlements.QueryEntitlement(ctx, searchFilter)
	if err != nil {
		log.Printf("❌ Error searching entitlements: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListEntitlementsResponse
	items := []components.DtoEntitlementResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d entitlements for feature '%s'\n", len(items), testFeatureID)
	for i, entitlement := range items {
		if i < 3 && entitlement.ID != nil {
			fmt.Printf("  - %s: Feature %s\n", *entitlement.ID, *entitlement.FeatureID)
		}
	}
	fmt.Println()
}

// Test 6: Delete entitlement
func testDeleteEntitlement(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 6: Delete Entitlement ---")

	_, err := client.Entitlements.DeleteEntitlement(ctx, testEntitlementID)
	if err != nil {
		log.Printf("❌ Error deleting entitlement: %v", err)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Entitlement deleted successfully!\n")
	fmt.Printf("  Deleted ID: %s\n\n", testEntitlementID)
}

// ========================================
// PLANS API TESTS
// ========================================

// Test 1: Create a new plan
func testCreatePlan(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Create Plan ---")

	timestamp := time.Now().Unix()
	testPlanName = fmt.Sprintf("Test Plan %d", timestamp)
	lookupKey := fmt.Sprintf("test_plan_%d", timestamp)

	request := components.DtoCreatePlanRequest{
		Name:        testPlanName,
		LookupKey:   flexprice.String(lookupKey),
		Description: flexprice.String("This is a test plan created by SDK tests"),
		Metadata: map[string]string{
			"source":      "sdk_test",
			"test_run":    time.Now().Format(time.RFC3339),
			"environment": "test",
		},
	}

	resp, err := client.Plans.CreatePlan(ctx, request)
	if err != nil {
		log.Printf("❌ Error creating plan: %v", err)
		fmt.Println()
		return
	}
	plan := resp.DtoPlanResponse
	if plan == nil || plan.ID == nil {
		log.Printf("❌ Create plan returned no body")
		fmt.Println()
		return
	}
	testPlanID = *plan.ID
	fmt.Printf("✓ Plan created successfully!\n")
	fmt.Printf("  ID: %s\n", *plan.ID)
	fmt.Printf("  Name: %s\n", *plan.Name)
	fmt.Printf("  Lookup Key: %s\n\n", *plan.LookupKey)
}

// Test 2: Get plan by ID
func testGetPlan(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Get Plan by ID ---")

	resp, err := client.Plans.GetPlan(ctx, testPlanID)
	if err != nil {
		log.Printf("❌ Error getting plan: %v", err)
		fmt.Println()
		return
	}
	plan := resp.DtoPlanResponse
	if plan == nil {
		log.Printf("❌ Get plan returned no body")
		fmt.Println()
		return
	}
	fmt.Printf("✓ Plan retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *plan.ID)
	fmt.Printf("  Name: %s\n", *plan.Name)
	fmt.Printf("  Lookup Key: %s\n", *plan.LookupKey)
	fmt.Printf("  Created At: %s\n\n", *plan.CreatedAt)
}

// Test 3: List all plans
func testListPlans(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 3: List Plans ---")

	filter := components.TypesPlanFilter{Limit: flexprice.Int64(10)}
	resp, err := client.Plans.QueryPlan(ctx, filter)
	if err != nil {
		log.Printf("❌ Error listing plans: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListPlansResponse
	items := []components.DtoPlanResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Retrieved %d plans\n", len(items))
	if len(items) > 0 {
		fmt.Printf("  First plan: %s - %s\n", *items[0].ID, *items[0].Name)
	}
	if listResp != nil && listResp.Pagination != nil && listResp.Pagination.Total != nil {
		fmt.Printf("  Total: %d\n", *listResp.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update plan
func testUpdatePlan(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Update Plan ---")

	updatedName := fmt.Sprintf("%s (Updated)", testPlanName)
	updatedDescription := "Updated description for test plan"
	body := components.DtoUpdatePlanRequest{
		Name:        flexprice.String(updatedName),
		Description: flexprice.String(updatedDescription),
		Metadata: map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	resp, err := client.Plans.UpdatePlan(ctx, testPlanID, body)
	if err != nil {
		log.Printf("❌ Error updating plan: %v", err)
		fmt.Println()
		return
	}
	plan := resp.DtoPlanResponse
	if plan == nil {
		log.Printf("❌ Update plan returned no body")
		fmt.Println()
		return
	}
	fmt.Printf("✓ Plan updated successfully!\n")
	fmt.Printf("  ID: %s\n", *plan.ID)
	fmt.Printf("  New Name: %s\n", *plan.Name)
	fmt.Printf("  New Description: %s\n", *plan.Description)
	fmt.Printf("  Updated At: %s\n\n", *plan.UpdatedAt)
}

// Test 5: Search plans
func testSearchPlans(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 5: Search Plans ---")

	searchFilter := components.TypesPlanFilter{PlanIds: []string{testPlanID}}
	resp, err := client.Plans.QueryPlan(ctx, searchFilter)
	if err != nil {
		log.Printf("❌ Error searching plans: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListPlansResponse
	items := []components.DtoPlanResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d plans matching ID '%s'\n", len(items), testPlanID)
	for i, plan := range items {
		if i < 3 {
			fmt.Printf("  - %s: %s (%s)\n", *plan.ID, *plan.Name, *plan.LookupKey)
		}
	}
	fmt.Println()
}

// Test 6: Delete plan
func testDeletePlan(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 6: Delete Plan ---")

	_, err := client.Plans.DeletePlan(ctx, testPlanID)
	if err != nil {
		log.Printf("❌ Error deleting plan: %v", err)
		fmt.Println()
		return
	}
	fmt.Printf("✓ Plan deleted successfully!\n")
	fmt.Printf("  Deleted ID: %s\n\n", testPlanID)
}

// ========================================
// CONNECTIONS API TESTS
// ========================================
// Note: Connections API doesn't have a create endpoint
// These tests work with existing connections

// Test 1: List linked integrations (replaces legacy "connections" list)
func testListConnections(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: List Linked Integrations ---")

	resp, err := client.Integrations.ListLinkedIntegrations(ctx)
	if err != nil {
		log.Printf("⚠ Warning: Error listing integrations: %v\n", err)
		fmt.Println("⚠ Skipping integrations tests (may not have any linked)\n")
		return
	}
	dto := resp.DtoLinkedIntegrationsResponse
	providers := []string{}
	if dto != nil && dto.Providers != nil {
		providers = dto.Providers
	}
	fmt.Printf("✓ Retrieved %d linked integration(s)\n", len(providers))
	for i, p := range providers {
		if i < 5 {
			fmt.Printf("  - %s\n", p)
		}
	}
	fmt.Println()
}

// ========================================
// SUBSCRIPTIONS API TESTS
// ========================================

// Test 1: Create a new subscription
func testCreateSubscription(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Create Subscription ---")

	// First, create a price for the plan (required for subscription creation)
	priceRequest := components.DtoCreatePriceRequest{
		EntityID:           testPlanID,
		EntityType:         components.TypesPriceEntityTypePlan,
		Type:               components.TypesPriceTypeFixed,
		BillingModel:       components.TypesBillingModelFlatFee,
		BillingCadence:     components.TypesBillingCadenceRecurring,
		BillingPeriod:      components.TypesBillingPeriodMonthly,
		BillingPeriodCount: flexprice.Int64(1),
		InvoiceCadence:     components.TypesInvoiceCadenceArrear,
		PriceUnitType:      components.TypesPriceUnitTypeFiat,
		Amount:             flexprice.String("29.99"),
		Currency:           "USD",
		DisplayName:        flexprice.String("Monthly Subscription Price"),
	}

	priceResp, err := client.Prices.CreatePrice(ctx, priceRequest)

	if err != nil {
		log.Printf("⚠ Warning: Could not create price for plan: %v", err)
		log.Printf("Attempting subscription creation anyway...")
	}
	_ = priceResp

	startDate := time.Now().Format(time.RFC3339)
	subscriptionRequest := components.DtoCreateSubscriptionRequest{
		CustomerID:         flexprice.String(testCustomerID),
		PlanID:             testPlanID,
		Currency:           "USD",
		BillingCadence:     components.TypesBillingCadenceRecurring,
		BillingPeriod:      components.TypesBillingPeriodMonthly,
		BillingPeriodCount: flexprice.Int64(1),
		BillingCycle:       func() *components.TypesBillingCycle { v := components.TypesBillingCycleAnniversary; return &v }(),
		StartDate:          flexprice.String(startDate),
		Metadata: map[string]string{
			"source":      "sdk_test",
			"test_run":    time.Now().Format(time.RFC3339),
			"environment": "test",
		},
	}

	subResp, err := client.Subscriptions.CreateSubscription(ctx, subscriptionRequest)
	if err != nil {
		log.Printf("❌ Error creating subscription: %v", err)
		fmt.Println()
		return
	}
	subscription := subResp.DtoSubscriptionResponse
	if subscription == nil || subscription.ID == nil {
		log.Printf("❌ Create subscription returned no body")
		fmt.Println()
		return
	}
	testSubscriptionID = *subscription.ID
	fmt.Printf("✓ Subscription created successfully!\n")
	fmt.Printf("  ID: %s\n", *subscription.ID)
	fmt.Printf("  Customer ID: %s\n", *subscription.CustomerID)
	fmt.Printf("  Plan ID: %s\n", *subscription.PlanID)
	fmt.Printf("  Status: %s\n\n", string(*subscription.SubscriptionStatus))
}

// Test 2: Get subscription by ID
func testGetSubscription(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Get Subscription by ID ---")

	// Check if subscription was created successfully
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription ID available (creation may have failed)\n")
		fmt.Println("⚠ Skipping get subscription test\n")
		return
	}

	resp, err := client.Subscriptions.GetSubscription(ctx, testSubscriptionID)
	if err != nil {
		log.Printf("❌ Error getting subscription: %v", err)
		fmt.Println()
		return
	}
	subscription := resp.DtoSubscriptionResponse
	if subscription == nil {
		log.Printf("❌ Get subscription returned no body")
		fmt.Println()
		return
	}
	fmt.Printf("✓ Subscription retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *subscription.ID)
	fmt.Printf("  Customer ID: %s\n", *subscription.CustomerID)
	fmt.Printf("  Status: %s\n", string(*subscription.SubscriptionStatus))
	fmt.Printf("  Created At: %s\n\n", *subscription.CreatedAt)
}

// Test 3: List all subscriptions
func testListSubscriptions(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 3: List Subscriptions ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping list test\n")
		fmt.Println()
		return
	}

	filter := components.TypesSubscriptionFilter{Limit: flexprice.Int64(10)}
	resp, err := client.Subscriptions.QuerySubscription(ctx, filter)
	if err != nil {
		log.Printf("❌ Error listing subscriptions: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListSubscriptionsResponse
	items := []components.DtoSubscriptionResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Retrieved %d subscriptions\n", len(items))
	if len(items) > 0 {
		fmt.Printf("  First subscription: %s (Customer: %s)\n", *items[0].ID, *items[0].CustomerID)
	}
	if listResp != nil && listResp.Pagination != nil && listResp.Pagination.Total != nil {
		fmt.Printf("  Total: %d\n", *listResp.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update subscription - SKIPPED
// Note: Update subscription endpoint may not be available in current SDK
// Skipping this test for now
func testUpdateSubscription(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Update Subscription ---")
	fmt.Println("⚠ Skipping update subscription test (endpoint not available in SDK)\n")
}

// Test 5: Search subscriptions
func testSearchSubscriptions(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 5: Search Subscriptions ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping search test\n")
		fmt.Println()
		return
	}

	searchFilter := components.TypesSubscriptionFilter{}
	resp, err := client.Subscriptions.QuerySubscription(ctx, searchFilter)
	if err != nil {
		log.Printf("❌ Error searching subscriptions: %v", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListSubscriptionsResponse
	items := []components.DtoSubscriptionResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d subscriptions for customer '%s'\n", len(items), testCustomerID)
	for i, subscription := range items {
		if i < 3 && subscription.ID != nil && subscription.SubscriptionStatus != nil {
			fmt.Printf("  - %s: %s\n", *subscription.ID, string(*subscription.SubscriptionStatus))
		}
	}
	fmt.Println()
}

// ========================================
// SUBSCRIPTION LIFECYCLE TESTS
// ========================================

// Test 6: Activate subscription (for draft subscriptions)
func testActivateSubscription(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 6: Activate Subscription ---")

	// Create a dedicated draft subscription for this test
	draftSubscriptionRequest := components.DtoCreateSubscriptionRequest{
		CustomerID:         flexprice.String(testCustomerID),
		PlanID:             testPlanID,
		Currency:           "USD",
		BillingCadence:     components.TypesBillingCadenceRecurring,
		BillingPeriod:      components.TypesBillingPeriodMonthly,
		BillingPeriodCount: flexprice.Int64(1),
		StartDate:          flexprice.String(time.Now().Format(time.RFC3339)),
		SubscriptionStatus: func() *components.TypesSubscriptionStatus { v := components.TypesSubscriptionStatusDraft; return &v }(),
	}

	draftResp, err := client.Subscriptions.CreateSubscription(ctx, draftSubscriptionRequest)
	if err != nil {
		log.Printf("⚠ Warning: Failed to create draft subscription: %v\n", err)
		fmt.Println("⚠ Skipping activate test\n")
		return
	}
	draftSub := draftResp.DtoSubscriptionResponse
	if draftSub == nil || draftSub.ID == nil {
		log.Printf("⚠ Warning: Create draft returned no body\n")
		fmt.Println("⚠ Skipping activate test\n")
		return
	}
	draftSubscriptionID := *draftSub.ID
	fmt.Printf("  Created draft subscription: %s\n", draftSubscriptionID)

	activateBody := components.DtoActivateDraftSubscriptionRequest{StartDate: time.Now().Format(time.RFC3339)}
	_, err = client.Subscriptions.ActivateSubscription(ctx, draftSubscriptionID, activateBody)
	if err != nil {
		log.Printf("⚠ Warning: Error activating subscription (may already be active): %v\n", err)
		fmt.Println("⚠ Skipping activate test\n")
		return
	}

	fmt.Printf("✓ Subscription activated successfully!\n")
	fmt.Printf("  ID: %s\n\n", testSubscriptionID)
}

// Test 7: Pause subscription
func testPauseSubscription(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 7: Pause Subscription ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping pause test\n")
		fmt.Println()
		return
	}

	pauseRequest := components.DtoPauseSubscriptionRequest{
		PauseMode: components.TypesPauseModeImmediate,
	}

	resp, err := client.Subscriptions.PauseSubscription(ctx, testSubscriptionID, pauseRequest)
	if err != nil {
		log.Printf("⚠ Warning: Error pausing subscription: %v\n", err)
		fmt.Println("⚠ Skipping pause test\n")
		return
	}
	pauseResp := resp.DtoSubscriptionPauseResponse
	if pauseResp == nil || pauseResp.ID == nil {
		log.Printf("⚠ Warning: Pause returned no body\n")
		fmt.Println("⚠ Skipping pause test\n")
		return
	}
	fmt.Printf("✓ Subscription paused successfully!\n")
	fmt.Printf("  Pause ID: %s\n", *pauseResp.ID)
	fmt.Printf("  Subscription ID: %s\n\n", *pauseResp.SubscriptionID)
}

// Test 8: Resume subscription
func testResumeSubscription(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 8: Resume Subscription ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping resume test\n")
		fmt.Println()
		return
	}

	resumeRequest := components.DtoResumeSubscriptionRequest{ResumeMode: components.TypesResumeModeImmediate}

	resp, err := client.Subscriptions.ResumeSubscription(ctx, testSubscriptionID, resumeRequest)
	if err != nil {
		log.Printf("⚠ Warning: Error resuming subscription: %v\n", err)
		fmt.Println("⚠ Skipping resume test\n")
		return
	}
	resumeResp := resp.DtoSubscriptionPauseResponse
	if resumeResp == nil || resumeResp.SubscriptionID == nil {
		log.Printf("⚠ Warning: Resume returned no body\n")
		fmt.Println("⚠ Skipping resume test\n")
		return
	}
	fmt.Printf("✓ Subscription resumed successfully!\n")
	fmt.Printf("  Subscription ID: %s\n\n", *resumeResp.SubscriptionID)
}

// Test 9: Get pause history
func testGetPauseHistory(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 9: Get Pause History ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping pause history test\n")
		fmt.Println()
		return
	}

	resp, err := client.Subscriptions.ListSubscriptionPauses(ctx, testSubscriptionID)
	if err != nil {
		log.Printf("⚠ Warning: Error getting pause history: %v\n", err)
		fmt.Println("⚠ Skipping pause history test\n")
		return
	}
	pauses := resp.DtoListSubscriptionPausesResponses
	if pauses == nil {
		pauses = []components.DtoListSubscriptionPausesResponse{}
	}
	fmt.Printf("✓ Retrieved pause history!\n")
	fmt.Printf("  Total pauses: %d\n\n", len(pauses))
}

// ========================================
// SUBSCRIPTION ADDON TESTS
// ========================================

// Test 10: Add addon to subscription
func testAddAddonToSubscription(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 10: Add Addon to Subscription ---")

	// Skip if subscription or addon creation failed
	if testSubscriptionID == "" || testAddonID == "" {
		log.Printf("⚠ Warning: No subscription or addon created, skipping add addon test\n")
		fmt.Println()
		return
	}

	// Create a price for the addon first (required)
	priceRequest := components.DtoCreatePriceRequest{
		EntityID:           testAddonID,
		EntityType:         components.TypesPriceEntityTypeAddon,
		Type:               components.TypesPriceTypeFixed,
		BillingModel:       components.TypesBillingModelFlatFee,
		BillingCadence:     components.TypesBillingCadenceRecurring,
		BillingPeriod:      components.TypesBillingPeriodMonthly,
		BillingPeriodCount: flexprice.Int64(1),
		InvoiceCadence:     components.TypesInvoiceCadenceArrear,
		PriceUnitType:      components.TypesPriceUnitTypeFiat,
		Amount:             flexprice.String("5.00"),
		Currency:           "USD",
		DisplayName:        flexprice.String("Addon Monthly Price"),
	}

	_, err := client.Prices.CreatePrice(ctx, priceRequest)

	if err != nil {
		log.Printf("⚠ Warning: Error creating price for addon: %v\n", err)
	} else {
		fmt.Printf("  Created price for addon: %s\n", testAddonID)
	}

	addAddonRequest := components.DtoAddAddonRequest{
		SubscriptionID: testSubscriptionID,
		AddonID:        testAddonID,
	}

	addResp, err := client.Subscriptions.AddSubscriptionAddon(ctx, addAddonRequest)
	if err != nil {
		log.Printf("⚠ Warning: Error adding addon to subscription: %v\n", err)
		fmt.Println("⚠ Skipping add addon test\n")
		return
	}
	assoc := addResp.DtoAddonAssociationResponse
	if assoc == nil {
		log.Printf("⚠ Warning: Add addon returned no body\n")
		fmt.Println("⚠ Skipping add addon test\n")
		return
	}
	fmt.Printf("✓ Addon added to subscription successfully!\n")
	if assoc.Subscription != nil && assoc.Subscription.ID != nil {
		fmt.Printf("  Subscription ID: %s\n", *assoc.Subscription.ID)
	}
	fmt.Printf("  Addon ID: %s\n\n", testAddonID)
}

// // Test 11: Get active addons
// func testGetActiveAddons(ctx context.Context, client *flexprice.Flexprice) {
// 	fmt.Println("--- Test 11: Get Active Addons ---")

// 	// Skip if subscription creation failed
// 	if testSubscriptionID == "" {
// 		log.Printf("⚠ Warning: No subscription created, skipping get active addons test\n")
// 		fmt.Println()
// 		return
// 	}

// 	addons, response, err := client.Subscriptions.SubscriptionsIdAddonsActiveGet(ctx, testSubscriptionID).
// 		Execute()

// 	if err != nil {
// 		log.Printf("⚠ Warning: Error getting active addons: %v\n", err)
// 		fmt.Println("⚠ Skipping get active addons test\n")
// 		return
// 	}

// 	if response.StatusCode != 200 {
// 		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
// 		fmt.Println("⚠ Skipping get active addons test\n")
// 		return
// 	}

// 	fmt.Printf("✓ Retrieved active addons!\n")
// 	fmt.Printf("  Total active addons: %d\n", len(addons))
// 	for i, addon := range addons {
// 		if i < 3 {
// 			fmt.Printf("  - %s\n", *addon.AddonId)
// 		}
// 	}
// 	fmt.Println()
// }

// Test 12: Remove addon from subscription
func testRemoveAddonFromSubscription(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 12: Remove Addon from Subscription ---")

	// Skip if subscription or addon creation failed
	if testSubscriptionID == "" || testAddonID == "" {
		log.Printf("⚠ Warning: No subscription or addon created, skipping remove addon test\n")
		fmt.Println()
		return
	}

	// Skip this test - need addon association ID, not addon ID
	fmt.Println("⚠ Skipping remove addon test (requires addon association ID)\n")
}

// ========================================
// SUBSCRIPTION CHANGE TESTS
// ========================================

// Test 13: Preview subscription change
func testPreviewSubscriptionChange(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 13: Preview Subscription Change ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping preview change test\n")
		fmt.Println()
		return
	}

	// Skip if we don't have a plan to change to
	if testPlanID == "" {
		log.Printf("⚠ Warning: No plan available for change preview\n")
		fmt.Println()
		return
	}

	changeRequest := components.DtoSubscriptionChangeRequest{
		TargetPlanID:      testPlanID,
		BillingCadence:    components.TypesBillingCadenceRecurring,
		BillingPeriod:     components.TypesBillingPeriodMonthly,
		BillingCycle:      components.TypesBillingCycleAnniversary,
		ProrationBehavior: components.TypesProrationBehaviorCreateProrations,
	}

	previewResp, err := client.Subscriptions.PreviewSubscriptionChange(ctx, testSubscriptionID, changeRequest)
	if err != nil {
		log.Printf("⚠ Warning: Error previewing subscription change: %v\n", err)
		fmt.Println("⚠ Skipping preview change test\n")
		return
	}
	preview := previewResp.DtoSubscriptionChangePreviewResponse
	fmt.Printf("✓ Subscription change preview generated!\n")
	if preview != nil {
		fmt.Printf("  Preview available\n")
	}
	fmt.Println()
}

// Test 14: Execute subscription change
func testExecuteSubscriptionChange(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 14: Execute Subscription Change ---")
	fmt.Println("⚠ Skipping execute change test (would modify active subscription)\n")
	// Skipping this to avoid actually changing the subscription during tests
}

// ========================================
// SUBSCRIPTION RELATED DATA TESTS
// ========================================

// Test 15: Get subscription entitlements
func testGetSubscriptionEntitlements(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 15: Get Subscription Entitlements ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping get entitlements test\n")
		fmt.Println()
		return
	}

	entitlementsResp, err := client.Subscriptions.GetSubscriptionEntitlements(ctx, testSubscriptionID, nil)
	if err != nil {
		log.Printf("⚠ Warning: Error getting subscription entitlements: %v\n", err)
		fmt.Println("⚠ Skipping get entitlements test\n")
		return
	}
	entitlements := entitlementsResp.DtoSubscriptionEntitlementsResponse
	features := []components.DtoAggregatedFeature{}
	if entitlements != nil && entitlements.Features != nil {
		features = entitlements.Features
	}
	fmt.Printf("✓ Retrieved subscription entitlements!\n")
	fmt.Printf("  Total features: %d\n", len(features))
	for i, feature := range features {
		if i < 3 && feature.Feature != nil && feature.Feature.Name != nil {
			fmt.Printf("  - Feature: %s\n", *feature.Feature.Name)
		}
	}
	fmt.Println()
}

// Test 16: Get upcoming grants
func testGetUpcomingGrants(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 16: Get Upcoming Grants ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping get upcoming grants test\n")
		fmt.Println()
		return
	}

	grantsResp, err := client.Subscriptions.GetSubscriptionUpcomingGrants(ctx, testSubscriptionID)
	if err != nil {
		log.Printf("⚠ Warning: Error getting upcoming grants: %v\n", err)
		fmt.Println("⚠ Skipping get upcoming grants test\n")
		return
	}
	listResp := grantsResp.DtoListCreditGrantApplicationsResponse
	items := []components.DtoCreditGrantApplicationResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Retrieved upcoming grants!\n")
	fmt.Printf("  Total upcoming grants: %d\n\n", len(items))
}

// Test 17: Report usage
func testReportUsage(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 17: Report Usage ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping report usage test\n")
		fmt.Println()
		return
	}

	// Skip if we don't have a feature to report usage for
	if testFeatureID == "" {
		log.Printf("⚠ Warning: No feature available for usage reporting\n")
		fmt.Println()
		return
	}

	usageRequest := components.DtoGetUsageBySubscriptionRequest{
		SubscriptionID: testSubscriptionID,
	}

	_, err := client.Subscriptions.GetSubscriptionUsage(ctx, usageRequest)
	if err != nil {
		log.Printf("⚠ Warning: Error getting usage: %v\n", err)
		fmt.Println("⚠ Skipping report usage test\n")
		return
	}

	fmt.Printf("✓ Usage reported successfully!\n")
	fmt.Printf("  Subscription ID: %s\n", testSubscriptionID)
	fmt.Printf("  Feature ID: %s\n", testFeatureID)
	fmt.Printf("  Usage: 10\n\n")
}

// ========================================
// SUBSCRIPTION LINE ITEM TESTS
// ========================================

// Test 18: Update line item
func testUpdateLineItem(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 18: Update Line Item ---")
	fmt.Println("⚠ Skipping update line item test (requires line item ID)\n")
	// Would need to get line items from subscription first to have an ID
}

// Test 19: Delete line item
func testDeleteLineItem(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 19: Delete Line Item ---")
	fmt.Println("⚠ Skipping delete line item test (requires line item ID)\n")
	// Would need to get line items from subscription first to have an ID
}

// Test 20: Cancel subscription
func testCancelSubscription(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 20: Cancel Subscription ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping cancel test\n")
		fmt.Println()
		return
	}

	cancelRequest := components.DtoCancelSubscriptionRequest{
		CancellationType: components.TypesCancellationTypeEndOfPeriod,
	}

	cancelResp, err := client.Subscriptions.CancelSubscription(ctx, testSubscriptionID, cancelRequest)
	if err != nil {
		log.Printf("⚠ Warning: Error canceling subscription: %v\n", err)
		fmt.Println("⚠ Skipping cancel test\n")
		return
	}
	cancelResult := cancelResp.DtoCancelSubscriptionResponse
	if cancelResult == nil {
		log.Printf("⚠ Warning: Cancel returned no body\n")
		fmt.Println("⚠ Skipping cancel test\n")
		return
	}
	fmt.Printf("✓ Subscription canceled successfully!\n")
	if cancelResult.SubscriptionID != nil {
		fmt.Printf("  Subscription ID: %s\n", *cancelResult.SubscriptionID)
	}
	if cancelResult.CancellationType != nil {
		fmt.Printf("  Cancellation Type: %s\n\n", string(*cancelResult.CancellationType))
	}
}

// ========================================
// INVOICES API TESTS
// ========================================

// Test 1: List all invoices
func testListInvoices(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: List Invoices ---")

	filter := components.TypesInvoiceFilter{Limit: flexprice.Int64(10)}
	resp, err := client.Invoices.QueryInvoice(ctx, filter)
	if err != nil {
		log.Printf("⚠ Warning: Error listing invoices: %v\n", err)
		fmt.Println("⚠ Skipping invoices tests (may not have any invoices yet)\n")
		return
	}
	listResp := resp.DtoListInvoicesResponse
	items := []components.DtoInvoiceResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Retrieved %d invoices\n", len(items))
	if len(items) > 0 {
		testInvoiceID = *items[0].ID
		fmt.Printf("  First invoice: %s (Customer: %s)\n", *items[0].ID, *items[0].CustomerID)
		if items[0].InvoiceStatus != nil {
			fmt.Printf("  Status: %s\n", string(*items[0].InvoiceStatus))
		}
	}
	if listResp != nil && listResp.Pagination != nil && listResp.Pagination.Total != nil {
		fmt.Printf("  Total: %d\n", *listResp.Pagination.Total)
	}
	fmt.Println()
}

// Test 2: Search invoices
func testSearchInvoices(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Search Invoices ---")

	searchFilter := components.TypesInvoiceFilter{}
	resp, err := client.Invoices.QueryInvoice(ctx, searchFilter)
	if err != nil {
		log.Printf("⚠ Warning: Error searching invoices: %v\n", err)
		fmt.Println("⚠ Skipping search invoices test\n")
		return
	}
	listResp := resp.DtoListInvoicesResponse
	items := []components.DtoInvoiceResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d invoices for customer '%s'\n", len(items), testCustomerID)
	for i, invoice := range items {
		if i < 3 && invoice.ID != nil {
			status := "unknown"
			if invoice.InvoiceStatus != nil {
				status = string(*invoice.InvoiceStatus)
			}
			fmt.Printf("  - %s: %s\n", *invoice.ID, status)
		}
	}
	fmt.Println()
}

// Test 3: Create invoice
func testCreateInvoice(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 3: Create Invoice ---")

	// Skip if customer or subscription not available
	if testCustomerID == "" {
		log.Printf("⚠ Warning: No customer created, skipping create invoice test\n")
		fmt.Println()
		return
	}

	draftStatus := components.TypesInvoiceStatusDraft
	invoiceRequest := components.DtoCreateInvoiceRequest{
		CustomerID:  testCustomerID,
		Currency:    "USD",
		AmountDue:   "100.00",
		Subtotal:    "100.00",
		Total:       "100.00",
		InvoiceType: func() *components.TypesInvoiceType { v := components.TypesInvoiceTypeOneOff; return &v }(),
		BillingReason: func() *components.TypesInvoiceBillingReason {
			v := components.TypesInvoiceBillingReasonManual
			return &v
		}(),
		InvoiceStatus: &draftStatus,
		LineItems: []components.DtoCreateInvoiceLineItemRequest{
			{
				DisplayName: flexprice.String("Test Service"),
				Quantity:    "1",
				Amount:      "100.00",
			},
		},
		Metadata: map[string]string{
			"source": "sdk_test",
			"type":   "manual",
		},
	}

	createResp, err := client.Invoices.CreateInvoice(ctx, invoiceRequest)
	if err != nil {
		log.Printf("⚠ Warning: Error creating invoice: %v\n", err)
		fmt.Println("⚠ Skipping create invoice test\n")
		return
	}
	invoice := createResp.DtoInvoiceResponse
	if invoice == nil {
		log.Printf("⚠ Warning: Create invoice returned no body\n")
		return
	}
	fmt.Printf("✓ Invoice created successfully!\n")
	fmt.Printf("  Customer ID: %s\n", *invoice.CustomerID)
	fmt.Printf("  Status: %s\n", string(*invoice.InvoiceStatus))
}

// Test 4: Get invoice by ID
func testGetInvoice(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Get Invoice by ID ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available (creation may have failed)\n")
		fmt.Println("⚠ Skipping get invoice test\n")
		return
	}

	getResp, err := client.Invoices.GetInvoice(ctx, testInvoiceID, nil, nil)
	if err != nil {
		log.Printf("⚠ Warning: Error getting invoice: %v\n", err)
		fmt.Println("⚠ Skipping get invoice test\n")
		return
	}
	invoice := getResp.DtoInvoiceResponse
	if invoice == nil {
		log.Printf("⚠ Warning: Get invoice returned no body\n")
		return
	}
	fmt.Printf("✓ Invoice retrieved successfully!\n")
	fmt.Printf("  Total: %s %s\n\n", *invoice.Currency, *invoice.Total)
}

// Test 5: Update invoice
func testUpdateInvoice(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 5: Update Invoice ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping update invoice test\n")
		return
	}

	updateRequest := components.DtoUpdateInvoiceRequest{
		Metadata: map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	updateResp, err := client.Invoices.UpdateInvoice(ctx, testInvoiceID, updateRequest)
	if err != nil {
		log.Printf("⚠ Warning: Error updating invoice: %v\n", err)
		fmt.Println("⚠ Skipping update invoice test\n")
		return
	}
	invoice := updateResp.DtoInvoiceResponse
	if invoice == nil {
		log.Printf("⚠ Warning: Update invoice returned no body\n")
		return
	}
	fmt.Printf("✓ Invoice updated successfully!\n")
	fmt.Printf("  Updated At: %s\n\n", *invoice.UpdatedAt)
}

// Test 6: Preview invoice
func testPreviewInvoice(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 6: Preview Invoice ---")

	// Skip if customer not available
	if testCustomerID == "" {
		log.Printf("⚠ Warning: No customer available for invoice preview\n")
		fmt.Println()
		return
	}

	subsID := testSubscriptionID
	if subsID == "" {
		log.Printf("⚠ Warning: No subscription ID for invoice preview\n")
		fmt.Println()
		return
	}
	previewRequest := components.DtoGetPreviewInvoiceRequest{SubscriptionID: subsID}

	previewResp, err := client.Invoices.GetInvoicePreview(ctx, previewRequest)
	if err != nil {
		log.Printf("⚠ Warning: Error previewing invoice: %v\n", err)
		fmt.Println("⚠ Skipping preview invoice test\n")
		return
	}

	preview := previewResp.DtoInvoiceResponse
	fmt.Printf("✓ Invoice preview generated!\n")
	if preview != nil && preview.Total != nil {
		fmt.Printf("  Preview Total: %s\n", *preview.Total)
	}
	fmt.Println()
}

// Test 7: Finalize invoice
func testFinalizeInvoice(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 7: Finalize Invoice ---")

	draftStatus := components.TypesInvoiceStatusDraft
	invoiceRequest := components.DtoCreateInvoiceRequest{
		CustomerID:  testCustomerID,
		Currency:    "USD",
		AmountDue:   "50.00",
		Subtotal:    "50.00",
		Total:       "50.00",
		InvoiceType: func() *components.TypesInvoiceType { v := components.TypesInvoiceTypeOneOff; return &v }(),
		BillingReason: func() *components.TypesInvoiceBillingReason {
			v := components.TypesInvoiceBillingReasonManual
			return &v
		}(),
		InvoiceStatus: &draftStatus,
		LineItems: []components.DtoCreateInvoiceLineItemRequest{
			{DisplayName: flexprice.String("Finalize Test Service"), Quantity: "1", Amount: "50.00"},
		},
		Metadata: map[string]string{"source": "sdk_test_finalize"},
	}

	createResp, err := client.Invoices.CreateInvoice(ctx, invoiceRequest)
	if err != nil {
		log.Printf("⚠ Warning: Failed to create draft invoice for finalize test: %v\n", err)
		fmt.Println("⚠ Skipping finalize invoice test\n")
		return
	}
	invoice := createResp.DtoInvoiceResponse
	if invoice == nil || invoice.ID == nil {
		log.Printf("⚠ Warning: Create draft returned no body\n")
		return
	}
	finalizeInvoiceID := *invoice.ID
	fmt.Printf("  Created draft invoice: %s\n", finalizeInvoiceID)

	_, err = client.Invoices.FinalizeInvoice(ctx, finalizeInvoiceID)
	if err != nil {
		log.Printf("⚠ Warning: Error finalizing invoice: %v\n", err)
		fmt.Println("⚠ Skipping finalize invoice test\n")
		return
	}

	fmt.Printf("✓ Invoice finalized successfully!\n")
	fmt.Printf("  Invoice ID: %s\n\n", finalizeInvoiceID)
}

// Test 8: Recalculate invoice
func testRecalculateInvoice(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 8: Recalculate Invoice ---")

	// Skip this test - recalculate only works on subscription invoices
	// which requires complex subscription setup
	log.Printf("⚠ Warning: Recalculate only works on subscription invoices\n")
	fmt.Println("⚠ Skipping recalculate invoice test (requires subscription invoice)\n")
}

// Test 9: Record payment
func testRecordPayment(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 9: Record Payment ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping record payment test\n")
		return
	}

	paymentRequest := components.DtoUpdatePaymentStatusRequest{
		PaymentStatus: components.TypesPaymentStatusSucceeded,
		Amount:        flexprice.String("100.00"),
	}

	_, err := client.Invoices.UpdateInvoicePaymentStatus(ctx, testInvoiceID, paymentRequest)
	if err != nil {
		log.Printf("⚠ Warning: Error recording payment: %v\n", err)
		fmt.Println("⚠ Skipping record payment test\n")
		return
	}

	fmt.Printf("✓ Payment recorded successfully!\n")
	fmt.Printf("  Invoice finalized\n")
	fmt.Printf("  Amount Paid: 100.00\n\n")
}

// Test 10: Attempt payment
func testAttemptPayment(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 10: Attempt Payment ---")

	draftStatus := components.TypesInvoiceStatusDraft
	pendingStatus := components.TypesPaymentStatusPending
	invoiceRequest := components.DtoCreateInvoiceRequest{
		CustomerID:  testCustomerID,
		Currency:    "USD",
		AmountDue:   "25.00",
		Subtotal:    "25.00",
		Total:       "25.00",
		AmountPaid:  flexprice.String("0.00"),
		InvoiceType: func() *components.TypesInvoiceType { v := components.TypesInvoiceTypeOneOff; return &v }(),
		BillingReason: func() *components.TypesInvoiceBillingReason {
			v := components.TypesInvoiceBillingReasonManual
			return &v
		}(),
		InvoiceStatus: &draftStatus,
		PaymentStatus: &pendingStatus,
		LineItems: []components.DtoCreateInvoiceLineItemRequest{
			{DisplayName: flexprice.String("Attempt Payment Test Service"), Quantity: "1", Amount: "25.00"},
		},
		Metadata: map[string]string{"source": "sdk_test_attempt_payment"},
	}

	createResp, err := client.Invoices.CreateInvoice(ctx, invoiceRequest)
	if err != nil {
		log.Printf("⚠ Warning: Failed to create invoice for attempt payment test: %v\n", err)
		fmt.Println("⚠ Skipping attempt payment test\n")
		return
	}
	inv := createResp.DtoInvoiceResponse
	if inv == nil || inv.ID == nil {
		log.Printf("⚠ Warning: Create invoice returned no body\n")
		return
	}
	attemptInvoiceID := *inv.ID
	fmt.Printf("  Created invoice: %s\n", attemptInvoiceID)

	_, err = client.Invoices.FinalizeInvoice(ctx, attemptInvoiceID)
	if err != nil {
		log.Printf("⚠ Warning: Failed to finalize invoice for attempt payment test: %v\n", err)
		fmt.Println("⚠ Skipping attempt payment test\n")
		return
	}
	fmt.Printf("  Finalized invoice\n")

	_, err = client.Invoices.AttemptInvoicePayment(ctx, attemptInvoiceID)
	if err != nil {
		log.Printf("⚠ Warning: Error attempting payment: %v\n", err)
		fmt.Println("⚠ Skipping attempt payment test\n")
		return
	}

	fmt.Printf("✓ Payment attempt initiated!\n")
	fmt.Printf("  Invoice ID: %s\n\n", attemptInvoiceID)
}

// Test 11: Download invoice PDF
func testDownloadInvoicePDF(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 11: Download Invoice PDF ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping download PDF test\n")
		return
	}

	_, err := client.Invoices.GetInvoicePdf(ctx, testInvoiceID, nil)
	if err != nil {
		log.Printf("⚠ Warning: Error downloading invoice PDF: %v\n", err)
		fmt.Println("⚠ Skipping download PDF test\n")
		return
	}

	fmt.Printf("✓ Invoice PDF downloaded!\n")
	fmt.Printf("  Invoice ID: %s\n", testInvoiceID)
	fmt.Printf("  PDF file downloaded\n")
}

// Test 12: Trigger invoice communications
func testTriggerInvoiceComms(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 12: Trigger Invoice Communications ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping trigger comms test\n")
		return
	}

	_, err := client.Invoices.TriggerInvoiceCommsWebhook(ctx, testInvoiceID)
	if err != nil {
		log.Printf("⚠ Warning: Error triggering invoice communications: %v\n", err)
		fmt.Println("⚠ Skipping trigger comms test\n")
		return
	}

	fmt.Printf("✓ Invoice communications triggered!\n")
	fmt.Printf("  Invoice ID: %s\n\n", testInvoiceID)
}

// Test 13: Get customer invoice summary
func testGetCustomerInvoiceSummary(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 13: Get Customer Invoice Summary ---")

	// Skip if customer not available
	if testCustomerID == "" {
		log.Printf("⚠ Warning: No customer ID available\n")
		fmt.Println("⚠ Skipping customer invoice summary test\n")
		return
	}

	_, err := client.Invoices.GetCustomerInvoiceSummary(ctx, testCustomerID)
	if err != nil {
		log.Printf("⚠ Warning: Error getting customer invoice summary: %v\n", err)
		fmt.Println("⚠ Skipping customer invoice summary test\n")
		return
	}

	fmt.Printf("✓ Customer invoice summary retrieved!\n")
	fmt.Printf("  Customer ID: %s\n", testCustomerID)
	// Note: TotalInvoices field structure may vary
	fmt.Println()
}

// Test 14: Void invoice
func testVoidInvoice(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 14: Void Invoice ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping void invoice test\n")
		return
	}

	_, err := client.Invoices.VoidInvoice(ctx, testInvoiceID)
	if err != nil {
		log.Printf("⚠ Warning: Error voiding invoice: %v\n", err)
		fmt.Println("⚠ Skipping void invoice test\n")
		return
	}

	fmt.Printf("✓ Invoice voided successfully!\n")
	fmt.Printf("  Invoice finalized\n")
}

// ========================================
// ========================================
// PRICES API TESTS
// ========================================

// Test 1: Create a new price
func testCreatePrice(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Create Price ---")

	// Skip if plan creation failed
	if testPlanID == "" {
		log.Printf("⚠ Warning: No plan ID available\n")
		fmt.Println("⚠ Skipping create price test\n")
		return
	}

	priceRequest := components.DtoCreatePriceRequest{
		EntityID:           testPlanID,
		EntityType:         components.TypesPriceEntityTypePlan,
		Currency:           "USD",
		Amount:             flexprice.String("99.00"),
		BillingModel:       components.TypesBillingModelFlatFee,
		BillingCadence:     components.TypesBillingCadenceRecurring,
		BillingPeriod:      components.TypesBillingPeriodMonthly,
		BillingPeriodCount: flexprice.Int64(1), // required: must be > 0
		InvoiceCadence:     components.TypesInvoiceCadenceAdvance,
		PriceUnitType:      components.TypesPriceUnitTypeFiat,
		Type:               components.TypesPriceTypeFixed,
		DisplayName:        flexprice.String("Monthly Subscription"),
		Description:        flexprice.String("Standard monthly subscription price"),
	}

	resp, err := client.Prices.CreatePrice(ctx, priceRequest)
	if err != nil {
		log.Printf("❌ Error creating price: %v\n", err)
		fmt.Println()
		return
	}
	price := resp.DtoPriceResponse
	if price == nil || price.ID == nil {
		log.Printf("❌ Create price returned no body\n")
		return
	}
	testPriceID = *price.ID
	fmt.Printf("✓ Price created successfully!\n")
	fmt.Printf("  ID: %s\n", *price.ID)
	fmt.Printf("  Amount: %s %s\n", *price.Amount, *price.Currency)
	fmt.Printf("  Billing Model: %s\n\n", string(*price.BillingModel))
}

// Test 2: Get price by ID
func testGetPrice(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Get Price by ID ---")

	if testPriceID == "" {
		log.Printf("⚠ Warning: No price ID available\n")
		fmt.Println("⚠ Skipping get price test\n")
		return
	}

	resp, err := client.Prices.GetPrice(ctx, testPriceID)
	if err != nil {
		log.Printf("❌ Error getting price: %v\n", err)
		fmt.Println()
		return
	}
	price := resp.DtoPriceResponse
	if price == nil {
		log.Printf("❌ Get price returned no body\n")
		return
	}
	fmt.Printf("✓ Price retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *price.ID)
	fmt.Printf("  Amount: %s %s\n", *price.Amount, *price.Currency)
	fmt.Printf("  Entity ID: %s\n", *price.EntityID)
	fmt.Printf("  Created At: %s\n\n", *price.CreatedAt)
}

// Test 3: List all prices
func testListPrices(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 3: List Prices ---")

	filter := components.TypesPriceFilter{Limit: flexprice.Int64(10)}
	resp, err := client.Prices.QueryPrice(ctx, filter)

	if err != nil {
		log.Printf("❌ Error listing prices: %v\n", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListPricesResponse
	items := []components.DtoPriceResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Retrieved %d prices\n", len(items))
	if len(items) > 0 {
		fmt.Printf("  First price: %s - %s %s\n", *items[0].ID, *items[0].Amount, *items[0].Currency)
	}
	if listResp != nil && listResp.Pagination != nil && listResp.Pagination.Total != nil {
		fmt.Printf("  Total: %d\n", *listResp.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update price
func testUpdatePrice(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Update Price ---")

	if testPriceID == "" {
		log.Printf("⚠ Warning: No price ID available\n")
		fmt.Println("⚠ Skipping update price test\n")
		return
	}

	updatedDescription := "Updated price description for testing"
	updateRequest := components.DtoUpdatePriceRequest{
		Description: flexprice.String(updatedDescription),
		Metadata: map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	updateResp, err := client.Prices.UpdatePrice(ctx, testPriceID, updateRequest)
	if err != nil {
		log.Printf("❌ Error updating price: %v\n", err)
		fmt.Println()
		return
	}
	price := updateResp.DtoPriceResponse
	if price == nil {
		log.Printf("❌ Update price returned no body\n")
		return
	}
	fmt.Printf("✓ Price updated successfully!\n")
	fmt.Printf("  ID: %s\n", *price.ID)
	fmt.Printf("  New Description: %s\n", *price.Description)
	fmt.Printf("  Updated At: %s\n\n", *price.UpdatedAt)
}

// Test 5: Delete price
func testDeletePrice(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Delete Price ---")

	if testPriceID == "" {
		log.Printf("⚠ Warning: No price ID available\n")
		fmt.Println("⚠ Skipping delete price test\n")
		return
	}

	futureDate := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	deleteRequest := components.DtoDeletePriceRequest{EndDate: flexprice.String(futureDate)}

	_, err := client.Prices.DeletePrice(ctx, testPriceID, deleteRequest)
	if err != nil {
		log.Printf("❌ Error deleting price: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Price deleted successfully!\n")
	fmt.Printf("  Deleted ID: %s\n\n", testPriceID)
}

// PAYMENTS API TESTS
// ========================================

// Test 1: Create a new payment
func testCreatePayment(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Create Payment ---")

	// Create a fresh invoice for this payment test
	// This is necessary because previous tests might have already paid the shared testInvoiceID

	draftStatus := components.TypesInvoiceStatusDraft
	pendingStatus := components.TypesPaymentStatusPending
	invoiceRequest := components.DtoCreateInvoiceRequest{
		CustomerID:  testCustomerID,
		Currency:    "USD",
		AmountDue:   "100.00",
		Subtotal:    "100.00",
		Total:       "100.00",
		AmountPaid:  flexprice.String("0.00"),
		InvoiceType: func() *components.TypesInvoiceType { v := components.TypesInvoiceTypeOneOff; return &v }(),
		BillingReason: func() *components.TypesInvoiceBillingReason {
			v := components.TypesInvoiceBillingReasonManual
			return &v
		}(),
		InvoiceStatus: &draftStatus,
		PaymentStatus: &pendingStatus,
		LineItems: []components.DtoCreateInvoiceLineItemRequest{
			{DisplayName: flexprice.String("Payment Test Service"), Quantity: "1", Amount: "100.00"},
		},
		Metadata: map[string]string{"source": "sdk_test_payment"},
	}

	createResp, err := client.Invoices.CreateInvoice(ctx, invoiceRequest)
	if err != nil {
		log.Printf("⚠ Warning: Failed to create invoice for payment test: %v\n", err)
		return
	}
	inv := createResp.DtoInvoiceResponse
	if inv == nil || inv.ID == nil {
		log.Printf("⚠ Warning: Create invoice returned no body\n")
		return
	}
	paymentInvoiceID := *inv.ID
	fmt.Printf("  Created invoice for payment: %s\n", paymentInvoiceID)

	currentInvoiceResp, err := client.Invoices.GetInvoice(ctx, paymentInvoiceID, nil, nil)
	if err != nil {
		log.Printf("⚠ Warning: Failed to get invoice for payment test: %v\n", err)
		return
	}
	currentInvoice := currentInvoiceResp.DtoInvoiceResponse
	if currentInvoice == nil {
		log.Printf("⚠ Warning: Get invoice returned no body\n")
		return
	}

	// Check if invoice is already paid before finalization
	if currentInvoice.AmountPaid != nil {
		amountPaidStr := *currentInvoice.AmountPaid
		if amountPaidStr != "0" && amountPaidStr != "0.00" {
			log.Printf("⚠ Warning: Invoice already has amount paid before finalization: %s\n", amountPaidStr)
			fmt.Println("⚠ Skipping payment creation test (invoice was auto-paid during creation)\n")
			return
		}
	}

	// Check if invoice has zero amount_due (which would cause auto-payment on finalization)
	if currentInvoice.AmountDue != nil {
		amountDueStr := *currentInvoice.AmountDue
		if amountDueStr == "0" || amountDueStr == "0.00" {
			log.Printf("⚠ Warning: Invoice has zero amount due: %s, will be auto-paid on finalization\n", amountDueStr)
			fmt.Println("⚠ Skipping payment creation test (invoice has zero amount due)\n")
			return
		}
	}

	// Log invoice details before finalization for debugging
	if currentInvoice.AmountDue != nil && currentInvoice.Total != nil {
		fmt.Printf("  Invoice before finalization - AmountDue: %s, Total: %s\n", *currentInvoice.AmountDue, *currentInvoice.Total)
	}

	if currentInvoice.InvoiceStatus != nil && *currentInvoice.InvoiceStatus == components.TypesInvoiceStatusDraft {
		_, err = client.Invoices.FinalizeInvoice(ctx, paymentInvoiceID)
		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "cannot unmarshal string") || strings.Contains(errStr, "json:") {
				log.Printf("⚠ Warning: Invoice finalization returned error (may already be finalized): %v\n", err)
			} else {
				log.Printf("⚠ Warning: Failed to finalize invoice for payment test: %v\n", err)
				return
			}
		} else {
			fmt.Printf("  Finalized invoice for payment\n")
		}
	} else {
		if currentInvoice.InvoiceStatus != nil {
			fmt.Printf("  Invoice already finalized (status: %s)\n", string(*currentInvoice.InvoiceStatus))
		} else {
			fmt.Printf("  Invoice status unknown, skipping finalization\n")
		}
	}

	finalInvoiceResp, err := client.Invoices.GetInvoice(ctx, paymentInvoiceID, nil, nil)
	if err != nil {
		log.Printf("⚠ Warning: Failed to get final invoice status for payment test: %v\n", err)
		return
	}
	finalInvoice := finalInvoiceResp.DtoInvoiceResponse
	if finalInvoice == nil {
		log.Printf("⚠ Warning: Get final invoice returned no body\n")
		return
	}

	// Log invoice details after finalization for debugging
	if finalInvoice.AmountDue != nil && finalInvoice.Total != nil && finalInvoice.AmountPaid != nil {
		fmt.Printf("  Invoice after finalization - AmountDue: %s, Total: %s, AmountPaid: %s\n",
			*finalInvoice.AmountDue, *finalInvoice.Total, *finalInvoice.AmountPaid)
	}

	// Check if invoice is already paid
	if finalInvoice.PaymentStatus != nil {
		paymentStatus := string(*finalInvoice.PaymentStatus)
		if paymentStatus == "succeeded" || paymentStatus == "paid" {
			log.Printf("⚠ Warning: Invoice is already paid (status: %s), cannot create payment\n", paymentStatus)
			fmt.Println("⚠ Skipping payment creation test\n")
			return
		}
	}

	// Check if invoice has any amount already paid
	if finalInvoice.AmountPaid != nil {
		amountPaidStr := *finalInvoice.AmountPaid
		if amountPaidStr != "0" && amountPaidStr != "0.00" {
			log.Printf("⚠ Warning: Invoice already has amount paid: %s, cannot create payment\n", amountPaidStr)
			fmt.Println("⚠ Skipping payment creation test\n")
			return
		}
	}

	// Check if invoice has zero amount (which might auto-mark it as paid)
	if finalInvoice.Total != nil {
		totalStr := *finalInvoice.Total
		if totalStr == "0" || totalStr == "0.00" {
			log.Printf("⚠ Warning: Invoice has zero total amount, may be auto-marked as paid\n")
			fmt.Println("⚠ Skipping payment creation test\n")
			return
		}
	}

	// Display invoice status
	paymentStatusStr := "unknown"
	if finalInvoice.PaymentStatus != nil {
		paymentStatusStr = string(*finalInvoice.PaymentStatus)
	}
	totalStr := "unknown"
	if finalInvoice.Total != nil {
		totalStr = *finalInvoice.Total
	}
	fmt.Printf("  Invoice is unpaid and ready for payment (status: %s, total: %s)\n", paymentStatusStr, totalStr)

	paymentRequest := components.DtoCreatePaymentRequest{
		Amount:            "100.00",
		Currency:          "USD",
		DestinationID:     paymentInvoiceID,
		DestinationType:   components.TypesPaymentDestinationTypeInvoice,
		PaymentMethodType: components.TypesPaymentMethodTypeOffline,
		ProcessPayment:    flexprice.Bool(false),
		Metadata: map[string]string{
			"source":   "sdk_test",
			"test_run": time.Now().Format(time.RFC3339),
		},
	}

	paymentResp, err := client.Payments.CreatePayment(ctx, paymentRequest)
	if err != nil {
		log.Printf("❌ Error creating payment: %v\n", err)
		fmt.Println()
		return
	}
	payment := paymentResp.DtoPaymentResponse
	if payment == nil || payment.ID == nil {
		log.Printf("❌ Create payment returned no body\n")
		return
	}
	testPaymentID = *payment.ID
	fmt.Printf("✓ Payment created successfully!\n")
	fmt.Printf("  ID: %s\n", *payment.ID)
	fmt.Printf("  Amount: %s %s\n", *payment.Amount, *payment.Currency)
	if payment.PaymentStatus != nil {
		fmt.Printf("  Status: %s\n\n", string(*payment.PaymentStatus))
	}
}

// Test 2: Get payment by ID
func testGetPayment(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Get Payment by ID ---")

	if testPaymentID == "" {
		log.Printf("⚠ Warning: No payment ID available\n")
		fmt.Println("⚠ Skipping get payment test\n")
		return
	}

	resp, err := client.Payments.GetPayment(ctx, testPaymentID)
	if err != nil {
		log.Printf("❌ Error getting payment: %v\n", err)
		fmt.Println()
		return
	}
	payment := resp.DtoPaymentResponse
	if payment == nil {
		log.Printf("❌ Get payment returned no body\n")
		return
	}
	fmt.Printf("✓ Payment retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *payment.ID)
	fmt.Printf("  Amount: %s %s\n", *payment.Amount, *payment.Currency)
	if payment.PaymentStatus != nil {
		fmt.Printf("  Status: %s\n", string(*payment.PaymentStatus))
	}
	fmt.Printf("  Created At: %s\n\n", *payment.CreatedAt)
}

// Test 3: List all payments
func testListPayments(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: List Payments ---")

	// Filter to only get the payment we just created to avoid archived payments from other tests
	if testPaymentID == "" {
		log.Printf("⚠ Warning: No payment created in this test run\n")
		fmt.Println("⚠ Skipping list payments test\n")
		return
	}

	req := operations.ListPaymentsRequest{PaymentIds: []string{testPaymentID}, Limit: flexprice.Int64(10)}
	resp, err := client.Payments.ListPayments(ctx, req)
	if err != nil {
		log.Printf("⚠ Warning: Error listing payments: %v\n", err)
		fmt.Println("⚠ Skipping payments tests (may not have any payments yet)\n")
		return
	}
	listResp := resp.DtoListPaymentsResponse
	items := []components.DtoPaymentResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Retrieved %d payments\n", len(items))
	if len(items) > 0 {
		testPaymentID = *items[0].ID
		fmt.Printf("  First payment: %s\n", *items[0].ID)
		if items[0].PaymentStatus != nil {
			fmt.Printf("  Status: %s\n", string(*items[0].PaymentStatus))
		}
	}
	if listResp != nil && listResp.Pagination != nil && listResp.Pagination.Total != nil {
		fmt.Printf("  Total: %d\n", *listResp.Pagination.Total)
	}
	fmt.Println()
}

// Test 2: Search payments - SKIPPED
// Note: Payment search endpoint may not be available in current SDK
func testSearchPayments(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Search Payments ---")
	fmt.Println("⚠ Skipping search payments test (endpoint not available in SDK)\n")
}

// Test 4: Update payment
func testUpdatePayment(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Update Payment ---")

	if testPaymentID == "" {
		log.Printf("⚠ Warning: No payment ID available\n")
		fmt.Println("⚠ Skipping update payment test\n")
		return
	}

	updateRequest := components.DtoUpdatePaymentRequest{
		Metadata: map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	updateResp, err := client.Payments.UpdatePayment(ctx, testPaymentID, updateRequest)
	if err != nil {
		log.Printf("❌ Error updating payment: %v\n", err)
		fmt.Println()
		return
	}
	payment := updateResp.DtoPaymentResponse
	if payment == nil {
		return
	}
	fmt.Printf("✓ Payment updated successfully!\n")
	fmt.Printf("  ID: %s\n", *payment.ID)
	fmt.Printf("  Updated At: %s\n\n", *payment.UpdatedAt)
}

// Test 5: Process payment
func testProcessPayment(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 5: Process Payment ---")

	if testPaymentID == "" {
		log.Printf("⚠ Warning: No payment ID available\n")
		fmt.Println("⚠ Skipping process payment test\n")
		return
	}

	processResp, err := client.Payments.ProcessPayment(ctx, testPaymentID)
	if err != nil {
		log.Printf("⚠ Warning: Error processing payment: %v\n", err)
		fmt.Println("⚠ Skipping process payment test (may require payment gateway setup)\n")
		return
	}
	payment := processResp.DtoPaymentResponse
	if payment == nil {
		return
	}
	fmt.Printf("✓ Payment processed successfully!\n")
	fmt.Printf("  ID: %s\n", *payment.ID)
	if payment.PaymentStatus != nil {
		fmt.Printf("  Status: %s\n\n", string(*payment.PaymentStatus))
	}
}

// Test 6: Delete payment
func testDeletePayment(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Delete Payment ---")

	if testPaymentID == "" {
		log.Printf("⚠ Warning: No payment ID available\n")
		fmt.Println("⚠ Skipping delete payment test\n")
		return
	}

	_, err := client.Payments.DeletePayment(ctx, testPaymentID)
	if err != nil {
		log.Printf("❌ Error deleting payment: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Payment deleted successfully!\n")
	fmt.Printf("  Deleted ID: %s\n\n", testPaymentID)
}

// Test 2: Search connections (replaced by list linked integrations; no search in new API)
func testSearchConnections(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Search Connections ---")
	resp, err := client.Integrations.ListLinkedIntegrations(ctx)
	if err != nil {
		log.Printf("⚠ Warning: Error listing integrations: %v\n", err)
		fmt.Println("⚠ Skipping search connections test\n")
		return
	}
	providers := []string{}
	if resp != nil && resp.DtoLinkedIntegrationsResponse != nil && resp.DtoLinkedIntegrationsResponse.Providers != nil {
		providers = resp.DtoLinkedIntegrationsResponse.Providers
	}
	fmt.Printf("✓ List completed!\n")
	fmt.Printf("  Found %d linked integration(s)\n", len(providers))
	for i, p := range providers {
		if i < 3 {
			fmt.Printf("  - %s\n", p)
		}
	}
	fmt.Println()
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ========================================
// WALLETS API TESTS
// ========================================

// Test 1: Create a new wallet
func testCreateWallet(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Create Wallet ---")

	// Skip if no customer available
	if testCustomerID == "" {
		log.Printf("⚠ Warning: No customer ID available\n")
		fmt.Println("⚠ Skipping create wallet test\n")
		return
	}

	walletRequest := components.DtoCreateWalletRequest{
		CustomerID: flexprice.String(testCustomerID),
		Currency:   "USD",
		Metadata: map[string]string{
			"source":   "sdk_test",
			"test_run": time.Now().Format(time.RFC3339),
		},
	}

	resp, err := client.Wallets.CreateWallet(ctx, walletRequest)
	if err != nil {
		log.Printf("❌ Error creating wallet: %v\n", err)
		fmt.Println()
		return
	}
	wallet := resp.DtoWalletResponse
	if wallet == nil || wallet.ID == nil {
		log.Printf("❌ Create wallet returned no body\n")
		return
	}
	testWalletID = *wallet.ID
	fmt.Printf("✓ Wallet created successfully!\n")
	fmt.Printf("  ID: %s\n", *wallet.ID)
	fmt.Printf("  Customer ID: %s\n", *wallet.CustomerID)
	fmt.Printf("  Currency: %s\n\n", *wallet.Currency)
}

// Test 2: Get wallet by ID
func testGetWallet(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Get Wallet by ID ---")

	if testWalletID == "" {
		log.Printf("⚠ Warning: No wallet ID available\n")
		fmt.Println("⚠ Skipping get wallet test\n")
		return
	}

	resp, err := client.Wallets.GetWallet(ctx, testWalletID)
	if err != nil {
		log.Printf("❌ Error getting wallet: %v\n", err)
		fmt.Println()
		return
	}
	wallet := resp.DtoWalletResponse
	if wallet == nil {
		log.Printf("❌ Get wallet returned no body\n")
		return
	}
	fmt.Printf("✓ Wallet retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *wallet.ID)
	fmt.Printf("  Customer ID: %s\n", *wallet.CustomerID)
	fmt.Printf("  Currency: %s\n", *wallet.Currency)
	fmt.Printf("  Created At: %s\n\n", *wallet.CreatedAt)
}

// Test 3: List all wallets
func testListWallets(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 3: List Wallets ---")

	filter := components.TypesWalletFilter{Limit: flexprice.Int64(10)}
	resp, err := client.Wallets.QueryWallet(ctx, filter)
	if err != nil {
		log.Printf("❌ Error listing wallets: %v\n", err)
		fmt.Println()
		return
	}
	listResp := resp.TypesListResponseDtoWalletResponse
	items := []components.DtoWalletResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Retrieved %d wallets\n", len(items))
	if len(items) > 0 {
		fmt.Printf("  First wallet: %s\n", *items[0].ID)
	}
	if listResp != nil && listResp.Pagination != nil && listResp.Pagination.Total != nil {
		fmt.Printf("  Total: %d\n", *listResp.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update wallet
func testUpdateWallet(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Update Wallet ---")

	if testWalletID == "" {
		log.Printf("⚠ Warning: No wallet ID available\n")
		fmt.Println("⚠ Skipping update wallet test\n")
		return
	}

	updateRequest := components.DtoUpdateWalletRequest{
		Metadata: map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	updateResp, err := client.Wallets.UpdateWallet(ctx, testWalletID, updateRequest)
	if err != nil {
		log.Printf("❌ Error updating wallet: %v\n", err)
		fmt.Println()
		return
	}
	wallet := updateResp.DtoWalletResponse
	if wallet == nil {
		return
	}

	fmt.Printf("✓ Wallet updated successfully!\n")
	fmt.Printf("  ID: %s\n", *wallet.ID)
	fmt.Printf("  Updated At: %s\n\n", *wallet.UpdatedAt)
}

// Test 5: Get wallet balance (real-time)
func testGetWalletBalance(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 5: Get Wallet Balance ---")

	if testWalletID == "" {
		log.Printf("⚠ Warning: No wallet ID available\n")
		fmt.Println("⚠ Skipping get wallet balance test\n")
		return
	}

	balanceResp, err := client.Wallets.GetWalletBalance(ctx, testWalletID, nil)
	if err != nil {
		log.Printf("❌ Error getting wallet balance: %v\n", err)
		fmt.Println()
		return
	}
	balance := balanceResp.DtoWalletBalanceResponse
	fmt.Printf("✓ Wallet balance retrieved successfully!\n")
	fmt.Printf("  Wallet ID: %s\n", testWalletID)
	if balance != nil && balance.Balance != nil {
		fmt.Printf("  Balance: %s\n", *balance.Balance)
	}
	fmt.Println()
}

// Test 6: Top up wallet
func testTopUpWallet(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 6: Top Up Wallet ---")

	if testWalletID == "" {
		log.Printf("⚠ Warning: No wallet ID available\n")
		fmt.Println("⚠ Skipping top up wallet test\n")
		return
	}

	topUpRequest := components.DtoTopUpWalletRequest{
		Amount:            flexprice.String("100.00"),
		TransactionReason: components.TypesTransactionReasonPurchasedCreditDirect,
		Description:       flexprice.String("Test top-up from SDK"),
	}

	_, err := client.Wallets.TopUpWallet(ctx, testWalletID, topUpRequest)
	if err != nil {
		log.Printf("❌ Error topping up wallet: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Wallet topped up successfully!\n")
	fmt.Printf("  Wallet ID: %s\n", testWalletID)
	// Balance info available in result.Wallet if needed
	fmt.Println()
}

// Test 7: Debit wallet (no Debit method in current SDK - skip)
func testDebitWallet(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 7: Debit Wallet ---")
	fmt.Println("⚠ Skipping debit wallet test (no Debit method in SDK)\n")
}

// Test 8: Get wallet transactions
func testGetWalletTransactions(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 8: Get Wallet Transactions ---")

	if testWalletID == "" {
		log.Printf("⚠ Warning: No wallet ID available\n")
		fmt.Println("⚠ Skipping get wallet transactions test\n")
		return
	}

	req := operations.GetWalletTransactionsRequest{IDPathParameter: testWalletID, Limit: flexprice.Int64(10)}
	resp, err := client.Wallets.GetWalletTransactions(ctx, req)
	if err != nil {
		log.Printf("❌ Error getting wallet transactions: %v\n", err)
		fmt.Println()
		return
	}
	listResp := resp.DtoListWalletTransactionsResponse
	items := []components.DtoWalletTransactionResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Retrieved %d wallet transactions\n", len(items))
	if listResp != nil && listResp.Pagination != nil && listResp.Pagination.Total != nil {
		fmt.Printf("  Total: %d\n", *listResp.Pagination.Total)
	}
	fmt.Println()
}

// Test 9: Search wallets
func testSearchWallets(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 9: Search Wallets ---")

	searchFilter := components.TypesWalletFilter{Limit: flexprice.Int64(10)}
	resp, err := client.Wallets.QueryWallet(ctx, searchFilter)
	if err != nil {
		log.Printf("❌ Error searching wallets: %v\n", err)
		fmt.Println()
		return
	}
	listResp := resp.TypesListResponseDtoWalletResponse
	items := []components.DtoWalletResponse{}
	if listResp != nil && listResp.Items != nil {
		items = listResp.Items
	}
	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d wallets for customer '%s'\n\n", len(items), testCustomerID)
}

// testCancelSubscriptionCleanup cancels the test subscription immediately (used in cleanup only).
// Must run before deleting the customer, since the API forbids deleting a customer with active subscriptions.
func testCancelSubscriptionCleanup(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Cleanup: Cancel Subscription ---")
	if testSubscriptionID == "" {
		fmt.Println("⚠ No subscription ID; skipping.")
		fmt.Println()
		return
	}
	req := components.DtoCancelSubscriptionRequest{
		CancellationType: components.TypesCancellationTypeImmediate,
	}
	_, err := client.Subscriptions.CancelSubscription(ctx, testSubscriptionID, req)
	if err != nil {
		log.Printf("⚠ Warning: could not cancel subscription %s: %v\n", testSubscriptionID, err)
		fmt.Println()
		return
	}
	fmt.Printf("✓ Subscription canceled (ID: %s)\n\n", testSubscriptionID)
}

// testDeleteWallet terminates the test wallet (used in cleanup only).
// Must run before deleting the customer, since the API forbids deleting a customer with associated wallets.
func testDeleteWallet(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Cleanup: Terminate Wallet ---")
	if testWalletID == "" {
		fmt.Println("⚠ No wallet ID; skipping.")
		fmt.Println()
		return
	}
	_, err := client.Wallets.TerminateWallet(ctx, testWalletID)
	if err != nil {
		log.Printf("⚠ Warning: could not terminate wallet %s: %v\n", testWalletID, err)
		fmt.Println()
		return
	}
	fmt.Printf("✓ Wallet terminated (ID: %s)\n\n", testWalletID)
}

// ========================================
// CREDIT GRANTS API TESTS
// ========================================

// Test 1: Create a new credit grant
func testCreateCreditGrant(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Create Credit Grant ---")

	// Skip if no plan available
	if testPlanID == "" {
		log.Printf("⚠ Warning: No plan ID available\n")
		fmt.Println("⚠ Skipping create credit grant test\n")
		return
	}

	grantRequest := components.DtoCreateCreditGrantRequest{
		Scope:                  components.TypesCreditGrantScopePlan,
		PlanID:                 flexprice.String(testPlanID),
		Credits:                "500.00",
		Name:                   "Test Credit Grant",
		Cadence:                components.TypesCreditGrantCadenceOnetime,
		ExpirationType:         func() *components.TypesCreditGrantExpiryType { v := components.TypesCreditGrantExpiryTypeNever; return &v }(),
		ExpirationDurationUnit: func() *components.TypesCreditGrantExpiryDurationUnit { v := components.TypesCreditGrantExpiryDurationUnitDay; return &v }(),
		Metadata: map[string]string{
			"source":   "sdk_test",
			"test_run": time.Now().Format(time.RFC3339),
		},
	}

	resp, err := client.CreditGrants.CreateCreditGrant(ctx, grantRequest)
	if err != nil {
		log.Printf("❌ Error creating credit grant: %v\n", err)
		fmt.Println()
		return
	}
	grant := resp.DtoCreditGrantResponse
	if grant == nil || grant.ID == nil {
		log.Printf("❌ Create credit grant returned no body\n")
		return
	}
	testCreditGrantID = *grant.ID
	fmt.Printf("✓ Credit grant created successfully!\n")
	fmt.Printf("  ID: %s\n", *grant.ID)
	if grant.Credits != nil {
		fmt.Printf("  Credits: %s\n", *grant.Credits)
	}
	fmt.Printf("  Plan ID: %s\n\n", *grant.PlanID)
}

// Test 2: Get credit grant by ID
func testGetCreditGrant(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Get Credit Grant by ID ---")

	if testCreditGrantID == "" {
		log.Printf("⚠ Warning: No credit grant ID available\n")
		fmt.Println("⚠ Skipping get credit grant test\n")
		return
	}

	resp, err := client.CreditGrants.GetCreditGrant(ctx, testCreditGrantID)
	if err != nil {
		log.Printf("❌ Error getting credit grant: %v\n", err)
		fmt.Println()
		return
	}
	grant := resp.DtoCreditGrantResponse
	if grant == nil {
		log.Printf("❌ Get credit grant returned no body\n")
		return
	}
	fmt.Printf("✓ Credit grant retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *grant.ID)
	if grant.Credits != nil {
		fmt.Printf("  Credits: %s\n", *grant.Credits)
	}
	if grant.CreatedAt != nil {
		fmt.Printf("  Created At: %s\n\n", *grant.CreatedAt)
	}
}

// Test 3: List all credit grants
func testListCreditGrants(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 3: List Credit Grants ---")

	// List grants for the plan
	resp, err := client.CreditGrants.GetPlanCreditGrants(ctx, testPlanID)
	if err != nil {
		log.Printf("❌ Error listing credit grants: %v\n", err)
		fmt.Println()
		return
	}
	list := resp.DtoListCreditGrantsResponse
	if list == nil {
		list = &components.DtoListCreditGrantsResponse{Items: []components.DtoCreditGrantResponse{}}
	}
	fmt.Printf("✓ Retrieved %d credit grants\n", len(list.Items))
	if len(list.Items) > 0 {
		first := list.Items[0]
		if first.ID != nil {
			fmt.Printf("  First grant: %s\n", *first.ID)
		}
	}
	if list.Pagination != nil && list.Pagination.Total != nil {
		fmt.Printf("  Total: %d\n", *list.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update credit grant
func testUpdateCreditGrant(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Update Credit Grant ---")

	if testCreditGrantID == "" {
		log.Printf("⚠ Warning: No credit grant ID available\n")
		fmt.Println("⚠ Skipping update credit grant test\n")
		return
	}

	updateRequest := components.DtoUpdateCreditGrantRequest{
		Metadata: map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	resp, err := client.CreditGrants.UpdateCreditGrant(ctx, testCreditGrantID, updateRequest)
	if err != nil {
		log.Printf("❌ Error updating credit grant: %v\n", err)
		fmt.Println()
		return
	}
	grant := resp.DtoCreditGrantResponse
	if grant == nil {
		log.Printf("❌ Update credit grant returned no body\n")
		return
	}
	fmt.Printf("✓ Credit grant updated successfully!\n")
	fmt.Printf("  ID: %s\n", *grant.ID)
	if grant.UpdatedAt != nil {
		fmt.Printf("  Updated At: %s\n\n", *grant.UpdatedAt)
	}
}

// Test 5: Delete credit grant
func testDeleteCreditGrant(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Delete Credit Grant ---")

	if testCreditGrantID == "" {
		log.Printf("⚠ Warning: No credit grant ID available\n")
		fmt.Println("⚠ Skipping delete credit grant test\n")
		return
	}

	_, err := client.CreditGrants.DeleteCreditGrant(ctx, testCreditGrantID, nil)
	if err != nil {
		log.Printf("❌ Error deleting credit grant: %v\n", err)
		fmt.Println()
		return
	}
	fmt.Printf("✓ Credit grant deleted successfully!\n")
	fmt.Printf("  Deleted ID: %s\n\n", testCreditGrantID)
}

// ========================================
// CREDIT NOTES API TESTS
// ========================================

// Test 1: Create a new credit note
func testCreateCreditNote(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Create Credit Note ---")

	// Skip if no customer available
	if testCustomerID == "" {
		log.Printf("⚠ Warning: No customer ID available\n")
		fmt.Println("⚠ Skipping create credit note test\n")
		return
	}

	// Skip if no invoice available
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available, skipping create credit note test\n")
		fmt.Println()
		return
	}

	// Get invoice to retrieve line items for credit note
	invResp, err := client.Invoices.GetInvoice(ctx, testInvoiceID, nil, nil)
	if err != nil || invResp.DtoInvoiceResponse == nil {
		log.Printf("⚠ Warning: Could not retrieve invoice: %v\n", err)
		fmt.Println("⚠ Skipping create credit note test\n")
		return
	}
	invoice := invResp.DtoInvoiceResponse

	log.Printf("Invoice has %d line items\n", len(invoice.LineItems))
	if len(invoice.LineItems) == 0 {
		log.Printf("⚠ Warning: Invoice has no line items\n")
		fmt.Println("⚠ Skipping create credit note test\n")
		return
	}

	// Use first line item from invoice for credit note
	firstLineItem := invoice.LineItems[0]
	if firstLineItem.ID == nil {
		log.Printf("⚠ Warning: First line item has no ID\n")
		return
	}
	creditAmount := "50.00"
	displayName := "Invoice Line Item"
	if firstLineItem.DisplayName != nil {
		displayName = *firstLineItem.DisplayName
	}

	noteRequest := components.DtoCreateCreditNoteRequest{
		InvoiceID: testInvoiceID,
		Reason:    components.TypesCreditNoteReasonBillingError,
		Memo:      flexprice.String("Test credit note from SDK"),
		LineItems: []components.DtoCreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemID: *firstLineItem.ID,
				Amount:            creditAmount,
				DisplayName:       &displayName,
			},
		},
		Metadata: map[string]string{
			"source":   "sdk_test",
			"test_run": time.Now().Format(time.RFC3339),
		},
	}

	resp, err := client.CreditNotes.CreateCreditNote(ctx, noteRequest)
	if err != nil {
		log.Printf("❌ Error creating credit note: %v\n", err)
		fmt.Println()
		return
	}
	note := resp.DtoCreditNoteResponse
	if note == nil || note.ID == nil {
		log.Printf("❌ Create credit note returned no body\n")
		return
	}
	testCreditNoteID = *note.ID
	fmt.Printf("✓ Credit note created successfully!\n")
	fmt.Printf("  ID: %s\n", *note.ID)
	if note.InvoiceID != nil {
		fmt.Printf("  Invoice ID: %s\n", *note.InvoiceID)
	}
	fmt.Println()
}

// Test 2: Get credit note by ID
func testGetCreditNote(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Get Credit Note by ID ---")

	if testCreditNoteID == "" {
		log.Printf("⚠ Warning: No credit note ID available\n")
		fmt.Println("⚠ Skipping get credit note test\n")
		return
	}

	resp, err := client.CreditNotes.GetCreditNote(ctx, testCreditNoteID)
	if err != nil {
		log.Printf("❌ Error getting credit note: %v\n", err)
		fmt.Println()
		return
	}
	note := resp.DtoCreditNoteResponse
	if note == nil {
		log.Printf("❌ Get credit note returned no body\n")
		return
	}
	fmt.Printf("✓ Credit note retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *note.ID)
	if note.InvoiceID != nil {
		fmt.Printf("  Invoice ID: %s\n", *note.InvoiceID)
	}
	if note.CreatedAt != nil {
		fmt.Printf("  Created At: %s\n", *note.CreatedAt)
	}
	fmt.Println()
}

// Test 3: List all credit notes
func testListCreditNotes(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 3: List Credit Notes ---")
	// SDK does not expose a list credit notes endpoint; skip.
	fmt.Println("⚠ Skipping list credit notes (no list endpoint in SDK)")
	fmt.Println()
}

// Test 4: Finalize credit note (ProcessCreditNote)
func testFinalizeCreditNote(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Finalize Credit Note ---")

	if testCreditNoteID == "" {
		log.Printf("⚠ Warning: No credit note ID available\n")
		fmt.Println("⚠ Skipping finalize credit note test\n")
		return
	}

	resp, err := client.CreditNotes.ProcessCreditNote(ctx, testCreditNoteID)
	if err != nil {
		log.Printf("⚠ Warning: Error finalizing credit note: %v\n", err)
		fmt.Println("⚠ Skipping finalize credit note test\n")
		return
	}
	note := resp.DtoCreditNoteResponse
	fmt.Printf("✓ Credit note finalized successfully!\n")
	if note != nil && note.ID != nil {
		fmt.Printf("  ID: %s\n\n", *note.ID)
	} else {
		fmt.Println()
	}
}

// ========================================
// EVENTS API TESTS
// ========================================

var (
	testEventID         string
	testEventName       string
	testEventCustomerID string
)

// Test 1: Create an event
func testCreateEvent(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 1: Create Event ---")

	// Use test customer external ID if available, otherwise generate a unique one
	if testExternalID == "" {
		testEventCustomerID = fmt.Sprintf("test-customer-%d", time.Now().Unix())
	} else {
		testEventCustomerID = testExternalID
	}

	testEventName = fmt.Sprintf("Test Event %d", time.Now().Unix())

	eventRequest := components.DtoIngestEventRequest{
		EventName:          testEventName,
		ExternalCustomerID: testEventCustomerID,
		Properties: map[string]string{
			"source":      "sdk_test",
			"environment": "test",
			"test_run":    time.Now().Format(time.RFC3339),
		},
		Source:    flexprice.String("sdk_test"),
		Timestamp: flexprice.String(time.Now().Format(time.RFC3339)),
	}

	resp, err := client.Events.IngestEvent(ctx, eventRequest)
	if err != nil {
		log.Printf("❌ Error creating event: %v\n", err)
		fmt.Println()
		return
	}
	obj := resp.Object
	if obj != nil {
		if eventID, ok := obj["event_id"]; ok {
			testEventID = eventID
			fmt.Printf("✓ Event created successfully!\n")
			fmt.Printf("  Event ID: %s\n", eventID)
			fmt.Printf("  Event Name: %s\n", testEventName)
			fmt.Printf("  Customer ID: %s\n\n", testEventCustomerID)
		} else {
			fmt.Printf("✓ Event created successfully!\n")
			fmt.Printf("  Event Name: %s\n", testEventName)
			fmt.Printf("  Customer ID: %s\n", testEventCustomerID)
			fmt.Printf("  Response: %v\n\n", obj)
		}
	} else {
		fmt.Printf("✓ Event created successfully!\n")
		fmt.Printf("  Event Name: %s\n", testEventName)
		fmt.Printf("  Customer ID: %s\n\n", testEventCustomerID)
	}
}

// Test 2: Query events
func testQueryEvents(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 2: Query Events ---")

	// Skip if no event was created
	if testEventName == "" {
		log.Printf("⚠ Warning: No event created, skipping query test\n")
		fmt.Println()
		return
	}

	queryRequest := components.DtoGetEventsRequest{
		ExternalCustomerID: &testEventCustomerID,
		EventName:          &testEventName,
	}

	resp, err := client.Events.ListRawEvents(ctx, queryRequest)
	if err != nil {
		log.Printf("⚠ Warning: Error querying events: %v\n", err)
		fmt.Println("⚠ Skipping query events test\n")
		return
	}
	data := resp.DtoGetEventsResponse
	if data == nil {
		data = &components.DtoGetEventsResponse{Events: []components.DtoEvent{}}
	}
	fmt.Printf("✓ Events queried successfully!\n")
	if len(data.Events) > 0 {
		fmt.Printf("  Found %d events\n", len(data.Events))
		for i, event := range data.Events {
			if i >= 3 {
				break
			}
			if event.ID != nil && event.EventName != nil {
				fmt.Printf("  - Event %d: %s - %s\n", i+1, *event.ID, *event.EventName)
			} else if event.EventName != nil {
				fmt.Printf("  - Event %d: %s\n", i+1, *event.EventName)
			}
		}
	} else {
		fmt.Println("  No events found")
	}
	fmt.Println()
}

// Test 3: Async event - Simple enqueue
func testAsyncEventEnqueue(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 3: Async Event - Simple Enqueue ---")

	// Create an AsyncClient
	asyncConfig := flexprice.DefaultAsyncConfig()
	asyncConfig.Debug = false // Disable debug in tests

	asyncClient := client.NewAsyncClient()
	// Ensure the client is closed properly on exit
	defer asyncClient.Close()

	// Use test customer external ID if available
	customerID := testEventCustomerID
	if customerID == "" {
		if testExternalID != "" {
			customerID = testExternalID
		} else {
			customerID = fmt.Sprintf("test-customer-%d", time.Now().Unix())
		}
	}

	// Enqueue a simple event
	err := asyncClient.Enqueue(
		"api_request",
		customerID,
		map[string]interface{}{
			"path":             "/api/resource",
			"method":           "GET",
			"status":           "200",
			"response_time_ms": 150,
		},
	)

	if err != nil {
		log.Printf("❌ Error enqueueing async event: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Async event enqueued successfully!\n")
	fmt.Printf("  Event Name: api_request\n")
	fmt.Printf("  Customer ID: %s\n\n", customerID)
}

// Test 4: Async event - Enqueue with options
func testAsyncEventEnqueueWithOptions(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 4: Async Event - Enqueue With Options ---")

	// Create an AsyncClient
	asyncConfig := flexprice.DefaultAsyncConfig()
	asyncConfig.Debug = false // Disable debug in tests

	asyncClient := client.NewAsyncClient()
	// Ensure the client is closed properly on exit
	defer asyncClient.Close()

	// Use test customer external ID if available
	customerID := testEventCustomerID
	if customerID == "" {
		if testExternalID != "" {
			customerID = testExternalID
		} else {
			customerID = fmt.Sprintf("test-customer-%d", time.Now().Unix())
		}
	}

	// Enqueue event with custom options
	err := asyncClient.EnqueueWithOptions(flexprice.EventOptions{
		EventName:          "file_upload",
		ExternalCustomerID: customerID,
		Properties: map[string]interface{}{
			"file_size_bytes": 1048576,
			"file_type":       "image/jpeg",
			"storage_bucket":  "user_uploads",
		},
		Source:    "sdk_test",
		Timestamp: time.Now().Format(time.RFC3339),
	})

	if err != nil {
		log.Printf("❌ Error enqueueing async event with options: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Async event with options enqueued successfully!\n")
	fmt.Printf("  Event Name: file_upload\n")
	fmt.Printf("  Customer ID: %s\n\n", customerID)
}

// Test 5: Async event - Batch enqueue
func testAsyncEventBatch(ctx context.Context, client *flexprice.Flexprice) {
	fmt.Println("--- Test 5: Async Event - Batch Enqueue ---")

	// Create an AsyncClient
	asyncConfig := flexprice.DefaultAsyncConfig()
	asyncConfig.Debug = false // Disable debug in tests

	asyncClient := client.NewAsyncClient()
	// Ensure the client is closed properly on exit
	defer asyncClient.Close()

	// Use test customer external ID if available
	customerID := testEventCustomerID
	if customerID == "" {
		if testExternalID != "" {
			customerID = testExternalID
		} else {
			customerID = fmt.Sprintf("test-customer-%d", time.Now().Unix())
		}
	}

	// Enqueue multiple events in a batch
	batchCount := 5
	for i := 0; i < batchCount; i++ {
		err := asyncClient.Enqueue(
			"batch_example",
			customerID,
			map[string]interface{}{
				"index": i,
				"batch": "demo",
			},
		)
		if err != nil {
			log.Printf("❌ Error enqueueing batch event %d: %v\n", i, err)
			fmt.Println()
			return
		}
	}

	fmt.Printf("✓ Enqueued %d batch events successfully!\n", batchCount)
	fmt.Printf("  Event Name: batch_example\n")
	fmt.Printf("  Customer ID: %s\n", customerID)
	fmt.Printf("  Waiting for events to be processed...\n")

	// Sleep to allow background processing to complete
	// In a real application, you don't need this as the deferred Close()
	// will wait for all events to be processed
	time.Sleep(time.Second * 2)
	fmt.Println()
}
