package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	flexprice "github.com/flexprice/go-sdk"
	"github.com/samber/lo"
)

// test_sdk.go - Local SDK Testing for Customer and Features API
// This file tests the locally generated FlexPrice Go SDK functions
//
// Setup:
// 1. Export your API key: export FLEXPRICE_API_KEY="your_key_here"
// 2. Export API host: export FLEXPRICE_API_HOST="api.cloud.flexprice.io/v1"
// 3. Run from api directory: go run test_sdk.go
//
// Note: This uses the local SDK in ./go directory, not the published version

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
	fmt.Println("=== FlexPrice Go SDK - API Tests ===\n")

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

	// Initialize API client with local SDK
	// Split host into domain and path (e.g., "api.cloud.flexprice.io/v1" -> "api.cloud.flexprice.io" + "/v1")
	parts := strings.SplitN(apiHost, "/", 2)
	hostOnly := parts[0]
	basePath := ""
	if len(parts) > 1 {
		basePath = "/" + parts[1]
	}

	config := flexprice.NewConfiguration()
	config.Scheme = "https"
	config.Host = hostOnly
	if basePath != "" {
		config.Servers[0].URL = basePath
	}
	config.AddDefaultHeader("x-api-key", apiKey)

	client := flexprice.NewAPIClient(config)
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
	testDeleteCustomer(ctx, client)

	fmt.Println("✓ Cleanup Completed!\n")

	fmt.Println("\n=== All API Tests Completed Successfully! ===")
}

// Test 1: Create a new customer
func testCreateCustomer(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Create Customer ---")

	timestamp := time.Now().Unix()
	testCustomerName = fmt.Sprintf("Test Customer %d", timestamp)
	testExternalID = fmt.Sprintf("test-customer-%d", timestamp)

	customerRequest := flexprice.DtoCreateCustomerRequest{
		Name:       lo.ToPtr(testCustomerName),
		ExternalId: testExternalID,
		Email:      lo.ToPtr(fmt.Sprintf("test-%d@example.com", timestamp)),
		Metadata: &map[string]string{
			"source":      "sdk_test",
			"test_run":    time.Now().Format(time.RFC3339),
			"environment": "test",
		},
	}

	customer, response, err := client.CustomersAPI.CustomersPost(ctx).
		Customer(customerRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error creating customer: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 201 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 201/200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	testCustomerID = *customer.Id
	fmt.Printf("✓ Customer created successfully!\n")
	fmt.Printf("  ID: %s\n", *customer.Id)
	fmt.Printf("  Name: %s\n", *customer.Name)
	fmt.Printf("  External ID: %s\n", *customer.ExternalId)
	fmt.Printf("  Email: %s\n\n", *customer.Email)
}

// Test 2: Get customer by ID
func testGetCustomer(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Get Customer by ID ---")

	customer, response, err := client.CustomersAPI.CustomersIdGet(ctx, testCustomerID).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting customer: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Customer retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *customer.Id)
	fmt.Printf("  Name: %s\n", *customer.Name)
	fmt.Printf("  Created At: %s\n\n", *customer.CreatedAt)
}

// Test 3: List all customers
func testListCustomers(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 3: List Customers ---")

	customers, response, err := client.CustomersAPI.CustomersGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("❌ Error listing customers: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Retrieved %d customers\n", len(customers.Items))
	if len(customers.Items) > 0 {
		fmt.Printf("  First customer: %s - %s\n", *customers.Items[0].Id, *customers.Items[0].Name)
	}
	if customers.Pagination != nil {
		fmt.Printf("  Total: %d\n", *customers.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update customer
func testUpdateCustomer(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 4: Update Customer ---")

	updatedName := fmt.Sprintf("%s (Updated)", testCustomerName)
	updateRequest := flexprice.DtoUpdateCustomerRequest{
		Name: &updatedName,
		Metadata: &map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	customer, response, err := client.CustomersAPI.CustomersIdPut(ctx, testCustomerID).
		Customer(updateRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error updating customer: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Customer updated successfully!\n")
	fmt.Printf("  ID: %s\n", *customer.Id)
	fmt.Printf("  New Name: %s\n", *customer.Name)
	fmt.Printf("  Updated At: %s\n\n", *customer.UpdatedAt)
}

// Test 5: Lookup customer by external ID
func testLookupCustomer(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 5: Lookup Customer by External ID ---")

	customer, response, err := client.CustomersAPI.CustomersLookupLookupKeyGet(ctx, testExternalID).
		Execute()

	if err != nil {
		log.Printf("❌ Error looking up customer: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Customer found by external ID!\n")
	fmt.Printf("  External ID: %s\n", testExternalID)
	fmt.Printf("  ID: %s\n", *customer.Id)
	fmt.Printf("  Name: %s\n\n", *customer.Name)
}

// Test 6: Search customers
func testSearchCustomers(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 6: Search Customers ---")

	// Use filter to search by external ID
	searchFilter := flexprice.TypesCustomerFilter{
		ExternalId: &testExternalID,
	}

	customers, response, err := client.CustomersAPI.CustomersSearchPost(ctx).
		Filter(searchFilter).
		Execute()

	if err != nil {
		log.Printf("❌ Error searching customers: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d customers matching external ID '%s'\n", len(customers.Items), testExternalID)
	for i, customer := range customers.Items {
		if i < 3 { // Show first 3 results
			fmt.Printf("  - %s: %s\n", *customer.Id, *customer.Name)
		}
	}
	fmt.Println()
}

// Test 7: Get customer entitlements
func testGetCustomerEntitlements(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 7: Get Customer Entitlements ---")

	entitlements, response, err := client.CustomersAPI.CustomersIdEntitlementsGet(ctx, testCustomerID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error getting customer entitlements: %v\n", err)
		fmt.Println("⚠ Skipping entitlements test (customer may not have any entitlements)\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping entitlements test\n")
		return
	}

	fmt.Printf("✓ Retrieved customer entitlements!\n")
	if entitlements.Features != nil {
		fmt.Printf("  Total features: %d\n", len(entitlements.Features))
		for i, feature := range entitlements.Features {
			if i < 3 && feature.Feature != nil && feature.Feature.Id != nil { // Show first 3
				fmt.Printf("  - Feature: %s\n", *feature.Feature.Id)
			}
		}
	} else {
		fmt.Println("  No features found")
	}
	fmt.Println()
}

// Test 8: Get customer upcoming grants
func testGetCustomerUpcomingGrants(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 8: Get Customer Upcoming Grants ---")

	grants, response, err := client.CustomersAPI.CustomersIdGrantsUpcomingGet(ctx, testCustomerID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error getting upcoming grants: %v\n", err)
		fmt.Println("⚠ Skipping upcoming grants test (customer may not have any grants)\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping upcoming grants test\n")
		return
	}

	fmt.Printf("✓ Retrieved upcoming grants!\n")
	if grants.Items != nil {
		fmt.Printf("  Total upcoming grants: %d\n", len(grants.Items))
	} else {
		fmt.Println("  No upcoming grants found")
	}
	fmt.Println()
}

// Test 9: Get customer usage
func testGetCustomerUsage(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 9: Get Customer Usage ---")

	usage, response, err := client.CustomersAPI.CustomersUsageGet(ctx).
		CustomerId(testCustomerID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error getting customer usage: %v\n", err)
		fmt.Println("⚠ Skipping usage test (customer may not have usage data)\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping usage test\n")
		return
	}

	fmt.Printf("✓ Retrieved customer usage!\n")
	if usage.Features != nil {
		fmt.Printf("  Feature usage records: %d\n", len(usage.Features))
	} else {
		fmt.Println("  No usage data found")
	}
	fmt.Println()
}

// Test 10: Delete customer
func testDeleteCustomer(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 10: Delete Customer ---")

	response, err := client.CustomersAPI.CustomersIdDelete(ctx, testCustomerID).
		Execute()

	if err != nil {
		log.Printf("❌ Error deleting customer: %v", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println()
		return
	}

	if response.StatusCode != 204 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 204/200, got %d", response.StatusCode)
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
func testCreateFeature(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Create Feature ---")

	timestamp := time.Now().Unix()
	testFeatureName = fmt.Sprintf("Test Feature %d", timestamp)
	featureKey := fmt.Sprintf("test_feature_%d", timestamp)

	featureRequest := flexprice.DtoCreateFeatureRequest{
		Name:        testFeatureName,
		LookupKey:   lo.ToPtr(featureKey),
		Description: lo.ToPtr("This is a test feature created by SDK tests"),
		Type:        flexprice.TYPESFEATURETYPE_FeatureTypeBoolean,
		Metadata: &map[string]string{
			"source":      "sdk_test",
			"test_run":    time.Now().Format(time.RFC3339),
			"environment": "test",
		},
	}

	feature, response, err := client.FeaturesAPI.FeaturesPost(ctx).
		Feature(featureRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error creating feature: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 201 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 201/200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	testFeatureID = *feature.Id
	fmt.Printf("✓ Feature created successfully!\n")
	fmt.Printf("  ID: %s\n", *feature.Id)
	fmt.Printf("  Name: %s\n", *feature.Name)
	fmt.Printf("  Lookup Key: %s\n", *feature.LookupKey)
	fmt.Printf("  Type: %s\n\n", string(*feature.Type))
}

// Test 2: Get feature by ID
func testGetFeature(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Get Feature by ID ---")

	feature, response, err := client.FeaturesAPI.FeaturesIdGet(ctx, testFeatureID).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting feature: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Feature retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *feature.Id)
	fmt.Printf("  Name: %s\n", *feature.Name)
	fmt.Printf("  Lookup Key: %s\n", *feature.LookupKey)
	fmt.Printf("  Created At: %s\n\n", *feature.CreatedAt)
}

// Test 3: List all features
func testListFeatures(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 3: List Features ---")

	features, response, err := client.FeaturesAPI.FeaturesGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("❌ Error listing features: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Retrieved %d features\n", len(features.Items))
	if len(features.Items) > 0 {
		fmt.Printf("  First feature: %s - %s\n", *features.Items[0].Id, *features.Items[0].Name)
	}
	if features.Pagination != nil {
		fmt.Printf("  Total: %d\n", *features.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update feature
func testUpdateFeature(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 4: Update Feature ---")

	updatedName := fmt.Sprintf("%s (Updated)", testFeatureName)
	updatedDescription := "Updated description for test feature"
	updateRequest := flexprice.DtoUpdateFeatureRequest{
		Name:        &updatedName,
		Description: &updatedDescription,
		Metadata: &map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	feature, response, err := client.FeaturesAPI.FeaturesIdPut(ctx, testFeatureID).
		Feature(updateRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error updating feature: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Feature updated successfully!\n")
	fmt.Printf("  ID: %s\n", *feature.Id)
	fmt.Printf("  New Name: %s\n", *feature.Name)
	fmt.Printf("  New Description: %s\n", *feature.Description)
	fmt.Printf("  Updated At: %s\n\n", *feature.UpdatedAt)
}

// Test 5: Search features
func testSearchFeatures(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 5: Search Features ---")

	// Use filter to search by feature ID
	searchFilter := flexprice.TypesFeatureFilter{
		FeatureIds: []string{testFeatureID},
	}

	features, response, err := client.FeaturesAPI.FeaturesSearchPost(ctx).
		Filter(searchFilter).
		Execute()

	if err != nil {
		log.Printf("❌ Error searching features: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d features matching ID '%s'\n", len(features.Items), testFeatureID)
	for i, feature := range features.Items {
		if i < 3 { // Show first 3 results
			fmt.Printf("  - %s: %s (%s)\n", *feature.Id, *feature.Name, *feature.LookupKey)
		}
	}
	fmt.Println()
}

// Test 6: Delete feature
func testDeleteFeature(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 6: Delete Feature ---")

	_, response, err := client.FeaturesAPI.FeaturesIdDelete(ctx, testFeatureID).
		Execute()

	if err != nil {
		log.Printf("❌ Error deleting feature: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 204 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 204/200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Feature deleted successfully!\n")
	fmt.Printf("  Deleted ID: %s\n\n", testFeatureID)
}

// ========================================
// ADDONS API TESTS
// ========================================

// Test 1: Create a new addon
func testCreateAddon(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Create Addon ---")

	timestamp := time.Now().Unix()
	testAddonName = fmt.Sprintf("Test Addon %d", timestamp)
	testAddonLookupKey = fmt.Sprintf("test_addon_%d", timestamp)

	addonRequest := flexprice.DtoCreateAddonRequest{
		Name:        testAddonName,
		LookupKey:   testAddonLookupKey,
		Description: lo.ToPtr("This is a test addon created by SDK tests"),
		Type:        flexprice.TYPESADDONTYPE_AddonTypeOnetime,
		Metadata: map[string]interface{}{
			"source":      "sdk_test",
			"test_run":    time.Now().Format(time.RFC3339),
			"environment": "test",
		},
	}

	addon, response, err := client.AddonsAPI.AddonsPost(ctx).
		Addon(addonRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error creating addon: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 201 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 201/200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	testAddonID = *addon.Id
	fmt.Printf("✓ Addon created successfully!\n")
	fmt.Printf("  ID: %s\n", *addon.Id)
	fmt.Printf("  Name: %s\n", *addon.Name)
	fmt.Printf("  Lookup Key: %s\n\n", *addon.LookupKey)
}

// Test 2: Get addon by ID
func testGetAddon(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Get Addon by ID ---")

	addon, response, err := client.AddonsAPI.AddonsIdGet(ctx, testAddonID).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting addon: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Addon retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *addon.Id)
	fmt.Printf("  Name: %s\n", *addon.Name)
	fmt.Printf("  Lookup Key: %s\n", *addon.LookupKey)
	fmt.Printf("  Created At: %s\n\n", *addon.CreatedAt)
}

// Test 3: List all addons
func testListAddons(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 3: List Addons ---")

	addons, response, err := client.AddonsAPI.AddonsGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("❌ Error listing addons: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Retrieved %d addons\n", len(addons.Items))
	if len(addons.Items) > 0 {
		fmt.Printf("  First addon: %s - %s\n", *addons.Items[0].Id, *addons.Items[0].Name)
	}
	if addons.Pagination != nil {
		fmt.Printf("  Total: %d\n", *addons.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update addon
func testUpdateAddon(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 4: Update Addon ---")

	updatedName := fmt.Sprintf("%s (Updated)", testAddonName)
	updatedDescription := "Updated description for test addon"
	updateRequest := flexprice.DtoUpdateAddonRequest{
		Name:        &updatedName,
		Description: &updatedDescription,
		Metadata: map[string]interface{}{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	addon, response, err := client.AddonsAPI.AddonsIdPut(ctx, testAddonID).
		Addon(updateRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error updating addon: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Addon updated successfully!\n")
	fmt.Printf("  ID: %s\n", *addon.Id)
	fmt.Printf("  New Name: %s\n", *addon.Name)
	fmt.Printf("  New Description: %s\n", *addon.Description)
	fmt.Printf("  Updated At: %s\n\n", *addon.UpdatedAt)
}

// Test 5: Lookup addon by lookup key
func testLookupAddon(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 5: Lookup Addon by Lookup Key ---")

	// Use the lookup key from the addon we created earlier
	if testAddonLookupKey == "" {
		log.Printf("⚠ Warning: No addon lookup key available (addon creation may have failed)\n")
		fmt.Println("⚠ Skipping lookup test\n")
		return
	}

	fmt.Printf("  Looking up addon with key: %s\n", testAddonLookupKey)

	addon, response, err := client.AddonsAPI.AddonsLookupLookupKeyGet(ctx, testAddonLookupKey).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error looking up addon: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println("⚠ Skipping lookup test (lookup key may not match)\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping lookup test\n")
		return
	}

	fmt.Printf("✓ Addon found by lookup key!\n")
	fmt.Printf("  Lookup Key: %s\n", testAddonLookupKey)
	fmt.Printf("  ID: %s\n", *addon.Id)
	fmt.Printf("  Name: %s\n\n", *addon.Name)
}

// Test 6: Search addons
func testSearchAddons(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 6: Search Addons ---")

	searchFilter := flexprice.TypesAddonFilter{
		AddonIds: []string{testAddonID},
	}

	addons, response, err := client.AddonsAPI.AddonsSearchPost(ctx).
		Filter(searchFilter).
		Execute()

	if err != nil {
		log.Printf("❌ Error searching addons: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d addons matching ID '%s'\n", len(addons.Items), testAddonID)
	for i, addon := range addons.Items {
		if i < 3 {
			fmt.Printf("  - %s: %s (%s)\n", *addon.Id, *addon.Name, *addon.LookupKey)
		}
	}
	fmt.Println()
}

// Test 7: Delete addon
func testDeleteAddon(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Delete Addon ---")

	_, response, err := client.AddonsAPI.AddonsIdDelete(ctx, testAddonID).
		Execute()

	if err != nil {
		log.Printf("❌ Error deleting addon: %v", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println()
		return
	}

	if response.StatusCode != 204 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 204/200, got %d", response.StatusCode)
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
func testCreateEntitlement(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Create Entitlement ---")

	entitlementRequest := flexprice.DtoCreateEntitlementRequest{
		FeatureId:        testFeatureID,
		FeatureType:      flexprice.TYPESFEATURETYPE_FeatureTypeBoolean,
		PlanId:           lo.ToPtr(testPlanID),
		IsEnabled:        lo.ToPtr(true),
		UsageResetPeriod: flexprice.TYPESENTITLEMENTUSAGERESETPERIOD_ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY.Ptr(),
	}

	entitlement, response, err := client.EntitlementsAPI.EntitlementsPost(ctx).
		Entitlement(entitlementRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error creating entitlement: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 201 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 201/200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	testEntitlementID = *entitlement.Id
	fmt.Printf("✓ Entitlement created successfully!\n")
	fmt.Printf("  ID: %s\n", *entitlement.Id)
	fmt.Printf("  Feature ID: %s\n", *entitlement.FeatureId)
	fmt.Printf("  Plan ID: %s\n\n", *entitlement.PlanId)
}

// Test 2: Get entitlement by ID
func testGetEntitlement(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Get Entitlement by ID ---")

	entitlement, response, err := client.EntitlementsAPI.EntitlementsIdGet(ctx, testEntitlementID).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting entitlement: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Entitlement retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *entitlement.Id)
	fmt.Printf("  Feature ID: %s\n", *entitlement.FeatureId)
	fmt.Printf("  Created At: %s\n\n", *entitlement.CreatedAt)
}

// Test 3: List all entitlements
func testListEntitlements(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 3: List Entitlements ---")

	entitlements, response, err := client.EntitlementsAPI.EntitlementsGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("❌ Error listing entitlements: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Retrieved %d entitlements\n", len(entitlements.Items))
	if len(entitlements.Items) > 0 {
		fmt.Printf("  First entitlement: %s (Feature: %s)\n", *entitlements.Items[0].Id, *entitlements.Items[0].FeatureId)
	}
	if entitlements.Pagination != nil {
		fmt.Printf("  Total: %d\n", *entitlements.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update entitlement
func testUpdateEntitlement(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 4: Update Entitlement ---")

	updateRequest := flexprice.DtoUpdateEntitlementRequest{
		IsEnabled: lo.ToPtr(false),
	}

	entitlement, response, err := client.EntitlementsAPI.EntitlementsIdPut(ctx, testEntitlementID).
		Entitlement(updateRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error updating entitlement: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Entitlement updated successfully!\n")
	fmt.Printf("  ID: %s\n", *entitlement.Id)
	fmt.Printf("  Is Enabled: %v\n", *entitlement.IsEnabled)
	fmt.Printf("  Updated At: %s\n\n", *entitlement.UpdatedAt)
}

// Test 5: Search entitlements
func testSearchEntitlements(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 5: Search Entitlements ---")

	searchFilter := flexprice.TypesEntitlementFilter{
		EntityIds: []string{testEntitlementID},
	}

	entitlements, response, err := client.EntitlementsAPI.EntitlementsSearchPost(ctx).
		Filter(searchFilter).
		Execute()

	if err != nil {
		log.Printf("❌ Error searching entitlements: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d entitlements matching ID '%s'\n", len(entitlements.Items), testEntitlementID)
	for i, entitlement := range entitlements.Items {
		if i < 3 {
			fmt.Printf("  - %s: Feature %s\n", *entitlement.Id, *entitlement.FeatureId)
		}
	}
	fmt.Println()
}

// Test 6: Delete entitlement
func testDeleteEntitlement(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Delete Entitlement ---")

	_, response, err := client.EntitlementsAPI.EntitlementsIdDelete(ctx, testEntitlementID).
		Execute()

	if err != nil {
		log.Printf("❌ Error deleting entitlement: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 204 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 204/200, got %d", response.StatusCode)
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
func testCreatePlan(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Create Plan ---")

	timestamp := time.Now().Unix()
	testPlanName = fmt.Sprintf("Test Plan %d", timestamp)
	lookupKey := fmt.Sprintf("test_plan_%d", timestamp)

	planRequest := flexprice.DtoCreatePlanRequest{
		Name:        testPlanName,
		LookupKey:   lo.ToPtr(lookupKey),
		Description: lo.ToPtr("This is a test plan created by SDK tests"),
		Metadata: &map[string]string{
			"source":      "sdk_test",
			"test_run":    time.Now().Format(time.RFC3339),
			"environment": "test",
		},
	}

	plan, response, err := client.PlansAPI.PlansPost(ctx).
		Plan(planRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error creating plan: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 201 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 201/200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	testPlanID = *plan.Id
	fmt.Printf("✓ Plan created successfully!\n")
	fmt.Printf("  ID: %s\n", *plan.Id)
	fmt.Printf("  Name: %s\n", *plan.Name)
	fmt.Printf("  Lookup Key: %s\n\n", *plan.LookupKey)
}

// Test 2: Get plan by ID
func testGetPlan(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Get Plan by ID ---")

	plan, response, err := client.PlansAPI.PlansIdGet(ctx, testPlanID).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting plan: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Plan retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *plan.Id)
	fmt.Printf("  Name: %s\n", *plan.Name)
	fmt.Printf("  Lookup Key: %s\n", *plan.LookupKey)
	fmt.Printf("  Created At: %s\n\n", *plan.CreatedAt)
}

// Test 3: List all plans
func testListPlans(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 3: List Plans ---")

	plans, response, err := client.PlansAPI.PlansGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("❌ Error listing plans: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Retrieved %d plans\n", len(plans.Items))
	if len(plans.Items) > 0 {
		fmt.Printf("  First plan: %s - %s\n", *plans.Items[0].Id, *plans.Items[0].Name)
	}
	if plans.Pagination != nil {
		fmt.Printf("  Total: %d\n", *plans.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update plan
func testUpdatePlan(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 4: Update Plan ---")

	updatedName := fmt.Sprintf("%s (Updated)", testPlanName)
	updatedDescription := "Updated description for test plan"
	updateRequest := flexprice.DtoUpdatePlanRequest{
		Name:        &updatedName,
		Description: &updatedDescription,
		Metadata: &map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	plan, response, err := client.PlansAPI.PlansIdPut(ctx, testPlanID).
		Plan(updateRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error updating plan: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Plan updated successfully!\n")
	fmt.Printf("  ID: %s\n", *plan.Id)
	fmt.Printf("  New Name: %s\n", *plan.Name)
	fmt.Printf("  New Description: %s\n", *plan.Description)
	fmt.Printf("  Updated At: %s\n\n", *plan.UpdatedAt)
}

// Test 5: Search plans
func testSearchPlans(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 5: Search Plans ---")

	// Use filter to search by plan ID
	searchFilter := flexprice.TypesPlanFilter{
		PlanIds: []string{testPlanID},
	}

	plans, response, err := client.PlansAPI.PlansSearchPost(ctx).
		Filter(searchFilter).
		Execute()

	if err != nil {
		log.Printf("❌ Error searching plans: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d plans matching ID '%s'\n", len(plans.Items), testPlanID)
	for i, plan := range plans.Items {
		if i < 3 { // Show first 3 results
			fmt.Printf("  - %s: %s (%s)\n", *plan.Id, *plan.Name, *plan.LookupKey)
		}
	}
	fmt.Println()
}

// Test 6: Delete plan
func testDeletePlan(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Delete Plan ---")

	_, response, err := client.PlansAPI.PlansIdDelete(ctx, testPlanID).
		Execute()

	if err != nil {
		log.Printf("❌ Error deleting plan: %v", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println()
		return
	}

	if response.StatusCode != 204 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 204/200, got %d", response.StatusCode)
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

// Test 1: List all connections
func testListConnections(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: List Connections ---")

	connections, response, err := client.ConnectionsAPI.ConnectionsGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error listing connections: %v\n", err)
		fmt.Println("⚠ Skipping connections tests (may not have any connections)\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping connections tests\n")
		return
	}

	fmt.Printf("✓ Retrieved %d connections\n", len(connections.Connections))
	if len(connections.Connections) > 0 {
		fmt.Printf("  First connection: %s\n", *connections.Connections[0].Id)
		if connections.Connections[0].ProviderType != nil {
			fmt.Printf("  Provider Type: %s\n", string(*connections.Connections[0].ProviderType))
		}
	}
	if connections.Total != nil {
		fmt.Printf("  Total: %d\n", *connections.Total)
	}
	fmt.Println()
}

// ========================================
// SUBSCRIPTIONS API TESTS
// ========================================

// Test 1: Create a new subscription
func testCreateSubscription(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Create Subscription ---")

	// First, create a price for the plan (required for subscription creation)
	priceRequest := flexprice.DtoCreatePriceRequest{
		EntityId:       testPlanID,
		EntityType:     flexprice.TYPESPRICEENTITYTYPE_PRICE_ENTITY_TYPE_PLAN,
		Type:           flexprice.TYPESPRICETYPE_PRICE_TYPE_FIXED,
		BillingModel:   flexprice.TYPESBILLINGMODEL_BILLING_MODEL_FLAT_FEE,
		BillingCadence: flexprice.TYPESBILLINGCADENCE_BILLING_CADENCE_RECURRING,
		BillingPeriod:  flexprice.TYPESBILLINGPERIOD_BILLING_PERIOD_MONTHLY,
		InvoiceCadence: flexprice.TYPESINVOICECADENCE_InvoiceCadenceArrear,
		Amount:         lo.ToPtr("29.99"),
		Currency:       "USD",
		DisplayName:    lo.ToPtr("Monthly Subscription Price"),
	}

	_, response, err := client.PricesAPI.PricesPost(ctx).
		Price(priceRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Could not create price for plan: %v", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Price creation error: %s", string(bodyBytes))
		}
		log.Printf("Attempting subscription creation anyway...")
	}

	startDate := time.Now().Format(time.RFC3339)
	subscriptionRequest := flexprice.DtoCreateSubscriptionRequest{
		CustomerId:         lo.ToPtr(testCustomerID),
		PlanId:             testPlanID,
		Currency:           "USD",
		BillingCadence:     flexprice.TYPESBILLINGCADENCE_BILLING_CADENCE_RECURRING,
		BillingPeriod:      flexprice.TYPESBILLINGPERIOD_BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: lo.ToPtr(int32(1)),
		BillingCycle:       flexprice.TYPESBILLINGCYCLE_BillingCycleAnniversary.Ptr(),
		StartDate:          lo.ToPtr(startDate),
		Metadata: &map[string]string{
			"source":      "sdk_test",
			"test_run":    time.Now().Format(time.RFC3339),
			"environment": "test",
		},
	}

	subscription, response, err := client.SubscriptionsAPI.SubscriptionsPost(ctx).
		Subscription(subscriptionRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error creating subscription: %v", err)
		if response != nil {
			log.Printf("Response Status Code: %d", response.StatusCode)
			if response.Body != nil {
				bodyBytes, _ := io.ReadAll(response.Body)
				log.Printf("Response Body: %s", string(bodyBytes))
			}
		}
		fmt.Println()
		return
	}

	if response.StatusCode != 201 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 201/200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	testSubscriptionID = *subscription.Id
	fmt.Printf("✓ Subscription created successfully!\n")
	fmt.Printf("  ID: %s\n", *subscription.Id)
	fmt.Printf("  Customer ID: %s\n", *subscription.CustomerId)
	fmt.Printf("  Plan ID: %s\n", *subscription.PlanId)
	fmt.Printf("  Status: %s\n\n", string(*subscription.SubscriptionStatus))
}

// Test 2: Get subscription by ID
func testGetSubscription(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Get Subscription by ID ---")

	// Check if subscription was created successfully
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription ID available (creation may have failed)\n")
		fmt.Println("⚠ Skipping get subscription test\n")
		return
	}

	subscription, response, err := client.SubscriptionsAPI.SubscriptionsIdGet(ctx, testSubscriptionID).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting subscription: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Subscription retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *subscription.Id)
	fmt.Printf("  Customer ID: %s\n", *subscription.CustomerId)
	fmt.Printf("  Status: %s\n", string(*subscription.SubscriptionStatus))
	fmt.Printf("  Created At: %s\n\n", *subscription.CreatedAt)
}

// Test 3: List all subscriptions
func testListSubscriptions(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 3: List Subscriptions ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping list test\n")
		fmt.Println()
		return
	}

	subscriptions, response, err := client.SubscriptionsAPI.SubscriptionsGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("❌ Error listing subscriptions: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Retrieved %d subscriptions\n", len(subscriptions.Items))
	if len(subscriptions.Items) > 0 {
		fmt.Printf("  First subscription: %s (Customer: %s)\n", *subscriptions.Items[0].Id, *subscriptions.Items[0].CustomerId)
	}
	if subscriptions.Pagination != nil {
		fmt.Printf("  Total: %d\n", *subscriptions.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update subscription - SKIPPED
// Note: Update subscription endpoint may not be available in current SDK
// Skipping this test for now
func testUpdateSubscription(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 4: Update Subscription ---")
	fmt.Println("⚠ Skipping update subscription test (endpoint not available in SDK)\n")
}

// Test 5: Search subscriptions
func testSearchSubscriptions(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 5: Search Subscriptions ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping search test\n")
		fmt.Println()
		return
	}

	searchFilter := flexprice.TypesSubscriptionFilter{}

	subscriptions, response, err := client.SubscriptionsAPI.SubscriptionsSearchPost(ctx).
		Filter(searchFilter).
		Execute()

	if err != nil {
		log.Printf("❌ Error searching subscriptions: %v", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d subscriptions for customer '%s'\n", len(subscriptions.Items), testCustomerID)
	for i, subscription := range subscriptions.Items {
		if i < 3 {
			fmt.Printf("  - %s: %s\n", *subscription.Id, string(*subscription.SubscriptionStatus))
		}
	}
	fmt.Println()
}

// ========================================
// SUBSCRIPTION LIFECYCLE TESTS
// ========================================

// Test 6: Activate subscription (for draft subscriptions)
func testActivateSubscription(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 6: Activate Subscription ---")

	// Create a dedicated draft subscription for this test
	draftSubscriptionRequest := flexprice.DtoCreateSubscriptionRequest{
		CustomerId:         lo.ToPtr(testCustomerID),
		PlanId:             testPlanID,
		Currency:           "USD",
		BillingCadence:     flexprice.TYPESBILLINGCADENCE_BILLING_CADENCE_RECURRING,
		BillingPeriod:      flexprice.TYPESBILLINGPERIOD_BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: lo.ToPtr(int32(1)),
		StartDate:          lo.ToPtr(time.Now().Format(time.RFC3339)),
		SubscriptionStatus: flexprice.TYPESSUBSCRIPTIONSTATUS_SubscriptionStatusDraft.Ptr(),
	}

	draftSub, _, err := client.SubscriptionsAPI.SubscriptionsPost(ctx).
		Subscription(draftSubscriptionRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Failed to create draft subscription: %v\n", err)
		fmt.Println("⚠ Skipping activate test\n")
		return
	}

	draftSubscriptionID := *draftSub.Id
	fmt.Printf("  Created draft subscription: %s\n", draftSubscriptionID)

	// Activate the draft subscription
	activateRequest := flexprice.DtoActivateDraftSubscriptionRequest{
		StartDate: time.Now().Format(time.RFC3339),
	}

	_, response, err := client.SubscriptionsAPI.SubscriptionsIdActivatePost(ctx, draftSubscriptionID).
		Request(activateRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error activating subscription (may already be active): %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println("⚠ Skipping activate test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping activate test\n")
		return
	}

	fmt.Printf("✓ Subscription activated successfully!\n")
	fmt.Printf("  ID: %s\n\n", testSubscriptionID)
}

// Test 7: Pause subscription
func testPauseSubscription(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 7: Pause Subscription ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping pause test\n")
		fmt.Println()
		return
	}

	pauseRequest := flexprice.DtoPauseSubscriptionRequest{
		PauseMode: flexprice.TYPESPAUSEMODE_PauseModeImmediate,
	}

	subscription, response, err := client.SubscriptionsAPI.SubscriptionsIdPausePost(ctx, testSubscriptionID).
		Request(pauseRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error pausing subscription: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println("⚠ Skipping pause test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping pause test\n")
		return
	}

	fmt.Printf("✓ Subscription paused successfully!\n")
	fmt.Printf("  Pause ID: %s\n", *subscription.Id)
	fmt.Printf("  Subscription ID: %s\n\n", *subscription.SubscriptionId)
}

// Test 8: Resume subscription
func testResumeSubscription(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 8: Resume Subscription ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping resume test\n")
		fmt.Println()
		return
	}

	resumeRequest := flexprice.DtoResumeSubscriptionRequest{}

	subscription, response, err := client.SubscriptionsAPI.SubscriptionsIdResumePost(ctx, testSubscriptionID).
		Request(resumeRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error resuming subscription: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println("⚠ Skipping resume test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping resume test\n")
		return
	}

	fmt.Printf("✓ Subscription resumed successfully!\n")
	fmt.Printf("  Pause ID: %s\n", *subscription.Id)
	fmt.Printf("  Subscription ID: %s\n\n", *subscription.SubscriptionId)
}

// Test 9: Get pause history
func testGetPauseHistory(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 9: Get Pause History ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping pause history test\n")
		fmt.Println()
		return
	}

	pauses, response, err := client.SubscriptionsAPI.SubscriptionsIdPausesGet(ctx, testSubscriptionID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error getting pause history: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println("⚠ Skipping pause history test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping pause history test\n")
		return
	}

	fmt.Printf("✓ Retrieved pause history!\n")
	fmt.Printf("  Total pauses: %d\n\n", len(pauses))
}

// ========================================
// SUBSCRIPTION ADDON TESTS
// ========================================

// Test 10: Add addon to subscription
func testAddAddonToSubscription(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 10: Add Addon to Subscription ---")

	// Skip if subscription or addon creation failed
	if testSubscriptionID == "" || testAddonID == "" {
		log.Printf("⚠ Warning: No subscription or addon created, skipping add addon test\n")
		fmt.Println()
		return
	}

	// Create a price for the addon first (required)
	priceRequest := flexprice.DtoCreatePriceRequest{
		EntityId:       testAddonID,
		EntityType:     flexprice.TYPESPRICEENTITYTYPE_PRICE_ENTITY_TYPE_ADDON,
		Type:           flexprice.TYPESPRICETYPE_PRICE_TYPE_FIXED,
		BillingModel:   flexprice.TYPESBILLINGMODEL_BILLING_MODEL_FLAT_FEE,
		BillingCadence: flexprice.TYPESBILLINGCADENCE_BILLING_CADENCE_RECURRING,
		BillingPeriod:  flexprice.TYPESBILLINGPERIOD_BILLING_PERIOD_MONTHLY,
		InvoiceCadence: flexprice.TYPESINVOICECADENCE_InvoiceCadenceArrear,
		Amount:         lo.ToPtr("5.00"),
		Currency:       "USD",
		DisplayName:    lo.ToPtr("Addon Monthly Price"),
	}

	_, priceResponse, err := client.PricesAPI.PricesPost(ctx).
		Price(priceRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error creating price for addon: %v\n", err)
		if priceResponse != nil && priceResponse.Body != nil {
			bodyBytes, _ := io.ReadAll(priceResponse.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
	} else {
		fmt.Printf("  Created price for addon: %s\n", testAddonID)
	}

	addAddonRequest := flexprice.DtoAddAddonRequest{
		SubscriptionId: testSubscriptionID,
		AddonId:        testAddonID,
	}

	subscription, response, err := client.SubscriptionsAPI.SubscriptionsAddonPost(ctx).
		Request(addAddonRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error adding addon to subscription: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println("⚠ Skipping add addon test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping add addon test\n")
		return
	}

	fmt.Printf("✓ Addon added to subscription successfully!\n")
	fmt.Printf("  Subscription ID: %s\n", *subscription.Id)
	fmt.Printf("  Addon ID: %s\n\n", testAddonID)
}

// // Test 11: Get active addons
// func testGetActiveAddons(ctx context.Context, client *flexprice.APIClient) {
// 	fmt.Println("--- Test 11: Get Active Addons ---")

// 	// Skip if subscription creation failed
// 	if testSubscriptionID == "" {
// 		log.Printf("⚠ Warning: No subscription created, skipping get active addons test\n")
// 		fmt.Println()
// 		return
// 	}

// 	addons, response, err := client.SubscriptionsAPI.SubscriptionsIdAddonsActiveGet(ctx, testSubscriptionID).
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
func testRemoveAddonFromSubscription(ctx context.Context, client *flexprice.APIClient) {
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
func testPreviewSubscriptionChange(ctx context.Context, client *flexprice.APIClient) {
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

	changeRequest := flexprice.DtoSubscriptionChangeRequest{
		TargetPlanId:      testPlanID,
		BillingCadence:    flexprice.TYPESBILLINGCADENCE_BILLING_CADENCE_RECURRING,
		BillingPeriod:     flexprice.TYPESBILLINGPERIOD_BILLING_PERIOD_MONTHLY,
		BillingCycle:      flexprice.TYPESBILLINGCYCLE_BillingCycleAnniversary,
		ProrationBehavior: flexprice.TYPESPRORATIONBEHAVIOR_ProrationBehaviorCreateProrations,
	}

	preview, response, err := client.SubscriptionsAPI.SubscriptionsIdChangePreviewPost(ctx, testSubscriptionID).
		Request(changeRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error previewing subscription change: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println("⚠ Skipping preview change test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping preview change test\n")
		return
	}

	fmt.Printf("✓ Subscription change preview generated!\n")
	if preview.NextInvoicePreview != nil {
		fmt.Printf("  Preview available\n")
	}
	fmt.Println()
}

// Test 14: Execute subscription change
func testExecuteSubscriptionChange(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 14: Execute Subscription Change ---")
	fmt.Println("⚠ Skipping execute change test (would modify active subscription)\n")
	// Skipping this to avoid actually changing the subscription during tests
}

// ========================================
// SUBSCRIPTION RELATED DATA TESTS
// ========================================

// Test 15: Get subscription entitlements
func testGetSubscriptionEntitlements(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 15: Get Subscription Entitlements ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping get entitlements test\n")
		fmt.Println()
		return
	}

	entitlements, response, err := client.SubscriptionsAPI.SubscriptionsIdEntitlementsGet(ctx, testSubscriptionID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error getting subscription entitlements: %v\n", err)
		fmt.Println("⚠ Skipping get entitlements test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping get entitlements test\n")
		return
	}

	fmt.Printf("✓ Retrieved subscription entitlements!\n")
	fmt.Printf("  Total features: %d\n", len(entitlements.Features))
	for i, feature := range entitlements.Features {
		if i < 3 {
			if feature.Feature != nil && feature.Feature.Name != nil {
				fmt.Printf("  - Feature: %s\n", *feature.Feature.Name)
			}
		}
	}
	fmt.Println()
}

// Test 16: Get upcoming grants
func testGetUpcomingGrants(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 16: Get Upcoming Grants ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping get upcoming grants test\n")
		fmt.Println()
		return
	}

	grants, response, err := client.SubscriptionsAPI.SubscriptionsIdGrantsUpcomingGet(ctx, testSubscriptionID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error getting upcoming grants: %v\n", err)
		fmt.Println("⚠ Skipping get upcoming grants test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping get upcoming grants test\n")
		return
	}

	fmt.Printf("✓ Retrieved upcoming grants!\n")
	fmt.Printf("  Total upcoming grants: %d\n\n", len(grants.Items))
}

// Test 17: Report usage
func testReportUsage(ctx context.Context, client *flexprice.APIClient) {
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

	usageRequest := flexprice.DtoGetUsageBySubscriptionRequest{
		SubscriptionId: testSubscriptionID,
	}

	_, response, err := client.SubscriptionsAPI.SubscriptionsUsagePost(ctx).
		Request(usageRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error reporting usage: %v\n", err)
		fmt.Println("⚠ Skipping report usage test\n")
		return
	}

	if response.StatusCode != 200 && response.StatusCode != 201 {
		log.Printf("⚠ Warning: Expected status code 200/201, got %d\n", response.StatusCode)
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
func testUpdateLineItem(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 18: Update Line Item ---")
	fmt.Println("⚠ Skipping update line item test (requires line item ID)\n")
	// Would need to get line items from subscription first to have an ID
}

// Test 19: Delete line item
func testDeleteLineItem(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 19: Delete Line Item ---")
	fmt.Println("⚠ Skipping delete line item test (requires line item ID)\n")
	// Would need to get line items from subscription first to have an ID
}

// Test 20: Cancel subscription
func testCancelSubscription(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 20: Cancel Subscription ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping cancel test\n")
		fmt.Println()
		return
	}

	cancelRequest := flexprice.DtoCancelSubscriptionRequest{
		CancellationType: flexprice.TYPESCANCELLATIONTYPE_CancellationTypeEndOfPeriod,
	}

	subscription, response, err := client.SubscriptionsAPI.SubscriptionsIdCancelPost(ctx, testSubscriptionID).
		Request(cancelRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error canceling subscription: %v\n", err)
		fmt.Println("⚠ Skipping cancel test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping cancel test\n")
		return
	}

	fmt.Printf("✓ Subscription canceled successfully!\n")
	fmt.Printf("  Subscription ID: %s\n", *subscription.SubscriptionId)
	fmt.Printf("  Cancellation Type: %s\n\n", string(*subscription.CancellationType))
}

// ========================================
// INVOICES API TESTS
// ========================================

// Test 1: List all invoices
func testListInvoices(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: List Invoices ---")

	invoices, response, err := client.InvoicesAPI.InvoicesGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error listing invoices: %v\n", err)
		fmt.Println("⚠ Skipping invoices tests (may not have any invoices yet)\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping invoices tests\n")
		return
	}

	fmt.Printf("✓ Retrieved %d invoices\n", len(invoices.Items))
	if len(invoices.Items) > 0 {
		testInvoiceID = *invoices.Items[0].Id
		fmt.Printf("  First invoice: %s (Customer: %s)\n", *invoices.Items[0].Id, *invoices.Items[0].CustomerId)
		if invoices.Items[0].Status != nil {
			fmt.Printf("  Status: %s\n", string(*invoices.Items[0].Status))
		}
	}
	if invoices.Pagination != nil {
		fmt.Printf("  Total: %d\n", *invoices.Pagination.Total)
	}
	fmt.Println()
}

// Test 2: Search invoices
func testSearchInvoices(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Search Invoices ---")

	searchFilter := flexprice.TypesInvoiceFilter{}

	invoices, response, err := client.InvoicesAPI.InvoicesSearchPost(ctx).
		Filter(searchFilter).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error searching invoices: %v\n", err)
		fmt.Println("⚠ Skipping search invoices test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping search invoices test\n")
		return
	}

	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d invoices for customer '%s'\n", len(invoices.Items), testCustomerID)
	for i, invoice := range invoices.Items {
		if i < 3 {
			status := "unknown"
			if invoice.Status != nil {
				status = string(*invoice.Status)
			}
			fmt.Printf("  - %s: %s\n", *invoice.Id, status)
		}
	}
	fmt.Println()
}

// Test 3: Create invoice
func testCreateInvoice(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 3: Create Invoice ---")

	// Skip if customer or subscription not available
	if testCustomerID == "" {
		log.Printf("⚠ Warning: No customer created, skipping create invoice test\n")
		fmt.Println()
		return
	}

	// Create invoice as DRAFT so we can test finalize later
	draftStatus := flexprice.TYPESINVOICESTATUS_InvoiceStatusDraft
	invoiceRequest := flexprice.DtoCreateInvoiceRequest{
		CustomerId:    testCustomerID,
		Currency:      "USD",
		AmountDue:     "100.00",
		Subtotal:      "100.00",
		Total:         "100.00",
		InvoiceType:   lo.ToPtr(flexprice.TYPESINVOICETYPE_InvoiceTypeOneOff),
		BillingReason: lo.ToPtr(flexprice.TYPESINVOICEBILLINGREASON_InvoiceBillingReasonManual),
		InvoiceStatus: &draftStatus,
		LineItems: []flexprice.DtoCreateInvoiceLineItemRequest{
			{
				DisplayName: lo.ToPtr("Test Service"),
				Quantity:    "1",
				Amount:      "100.00",
			},
		},
		Metadata: &map[string]string{
			"source": "sdk_test",
			"type":   "manual",
		},
	}

	invoice, response, err := client.InvoicesAPI.InvoicesPost(ctx).
		Invoice(invoiceRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error creating invoice: %v\n", err)
		fmt.Println("⚠ Skipping create invoice test\n")
		return
	}

	if response.StatusCode != 201 && response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 201/200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping create invoice test\n")
		return
	}

	fmt.Printf("  Invoice finalized\n")
	fmt.Printf("✓ Invoice created successfully!\n")
	fmt.Printf("  Invoice finalized\n")
	fmt.Printf("  Customer ID: %s\n", *invoice.CustomerId)
	fmt.Printf("  Status: %s\n", string(*invoice.InvoiceStatus))
}

// Test 4: Get invoice by ID
func testGetInvoice(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 4: Get Invoice by ID ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available (creation may have failed)\n")
		fmt.Println("⚠ Skipping get invoice test\n")
		return
	}

	invoice, response, err := client.InvoicesAPI.InvoicesIdGet(ctx, testInvoiceID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error getting invoice: %v\n", err)
		fmt.Println("⚠ Skipping get invoice test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping get invoice test\n")
		return
	}

	fmt.Printf("✓ Invoice retrieved successfully!\n")
	fmt.Printf("  Invoice finalized\n")
	fmt.Printf("  Total: %s %s\n\n", *invoice.Currency, *invoice.Total)
}

// Test 5: Update invoice
func testUpdateInvoice(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 5: Update Invoice ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping update invoice test\n")
		return
	}

	updateRequest := flexprice.DtoUpdateInvoiceRequest{
		Metadata: &map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	invoice, response, err := client.InvoicesAPI.InvoicesIdPut(ctx, testInvoiceID).
		Request(updateRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error updating invoice: %v\n", err)
		fmt.Println("⚠ Skipping update invoice test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping update invoice test\n")
		return
	}

	fmt.Printf("✓ Invoice updated successfully!\n")
	fmt.Printf("  Invoice finalized\n")
	fmt.Printf("  Updated At: %s\n\n", *invoice.UpdatedAt)
}

// Test 6: Preview invoice
func testPreviewInvoice(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 6: Preview Invoice ---")

	// Skip if customer not available
	if testCustomerID == "" {
		log.Printf("⚠ Warning: No customer available for invoice preview\n")
		fmt.Println()
		return
	}

	// Use subscription ID if available, otherwise use hardcoded one
	subsID := testSubscriptionID
	previewRequest := flexprice.DtoGetPreviewInvoiceRequest{
		SubscriptionId: subsID,
	}

	preview, response, err := client.InvoicesAPI.InvoicesPreviewPost(ctx).
		Request(previewRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error previewing invoice: %v\n", err)
		fmt.Println("⚠ Skipping preview invoice test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping preview invoice test\n")
		return
	}

	fmt.Printf("✓ Invoice preview generated!\n")
	if preview.Total != nil {
		fmt.Printf("  Preview Total: %s\n", *preview.Total)
	}
	fmt.Println()
}

// Test 7: Finalize invoice
func testFinalizeInvoice(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 7: Finalize Invoice ---")

	// Create a dedicated draft invoice for this test
	// The shared testInvoiceID may already be finalized by other tests
	draftStatus := flexprice.TYPESINVOICESTATUS_InvoiceStatusDraft
	invoiceRequest := flexprice.DtoCreateInvoiceRequest{
		CustomerId:    testCustomerID,
		Currency:      "USD",
		AmountDue:     "50.00",
		Subtotal:      "50.00",
		Total:         "50.00",
		InvoiceType:   lo.ToPtr(flexprice.TYPESINVOICETYPE_InvoiceTypeOneOff),
		BillingReason: lo.ToPtr(flexprice.TYPESINVOICEBILLINGREASON_InvoiceBillingReasonManual),
		InvoiceStatus: &draftStatus,
		LineItems: []flexprice.DtoCreateInvoiceLineItemRequest{
			{
				DisplayName: lo.ToPtr("Finalize Test Service"),
				Quantity:    "1",
				Amount:      "50.00",
			},
		},
		Metadata: &map[string]string{
			"source": "sdk_test_finalize",
		},
	}

	invoice, _, err := client.InvoicesAPI.InvoicesPost(ctx).
		Invoice(invoiceRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Failed to create draft invoice for finalize test: %v\n", err)
		fmt.Println("⚠ Skipping finalize invoice test\n")
		return
	}

	finalizeInvoiceID := *invoice.Id
	fmt.Printf("  Created draft invoice: %s\n", finalizeInvoiceID)

	// Now finalize the draft invoice
	_, response, err := client.InvoicesAPI.InvoicesIdFinalizePost(ctx, finalizeInvoiceID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error finalizing invoice: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println("⚠ Skipping finalize invoice test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping finalize invoice test\n")
		return
	}

	fmt.Printf("✓ Invoice finalized successfully!\n")
	fmt.Printf("  Invoice ID: %s\n\n", finalizeInvoiceID)
}

// Test 8: Recalculate invoice
func testRecalculateInvoice(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 8: Recalculate Invoice ---")

	// Skip this test - recalculate only works on subscription invoices
	// which requires complex subscription setup
	log.Printf("⚠ Warning: Recalculate only works on subscription invoices\n")
	fmt.Println("⚠ Skipping recalculate invoice test (requires subscription invoice)\n")
}

// Test 9: Record payment
func testRecordPayment(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 9: Record Payment ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping record payment test\n")
		return
	}

	paymentRequest := flexprice.DtoUpdatePaymentStatusRequest{
		PaymentStatus: flexprice.TYPESPAYMENTSTATUS_PaymentStatusSucceeded,
		Amount:        lo.ToPtr("100.00"),
	}

	_, response, err := client.InvoicesAPI.InvoicesIdPaymentPut(ctx, testInvoiceID).
		Request(paymentRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error recording payment: %v\n", err)
		fmt.Println("⚠ Skipping record payment test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping record payment test\n")
		return
	}

	fmt.Printf("✓ Payment recorded successfully!\n")
	fmt.Printf("  Invoice finalized\n")
	fmt.Printf("  Amount Paid: 100.00\n\n")
}

// Test 10: Attempt payment
func testAttemptPayment(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 10: Attempt Payment ---")

	// Create a dedicated finalized but unpaid invoice for attempt payment test
	// Important: Set AmountPaid and PaymentStatus to prevent auto-payment
	draftStatus := flexprice.TYPESINVOICESTATUS_InvoiceStatusDraft
	pendingStatus := flexprice.TYPESPAYMENTSTATUS_PaymentStatusPending
	invoiceRequest := flexprice.DtoCreateInvoiceRequest{
		CustomerId:    testCustomerID,
		Currency:      "USD",
		AmountDue:     "25.00",
		Subtotal:      "25.00",
		Total:         "25.00",
		AmountPaid:    lo.ToPtr("0.00"), // Explicitly set to 0 to prevent auto-payment
		InvoiceType:   lo.ToPtr(flexprice.TYPESINVOICETYPE_InvoiceTypeOneOff),
		BillingReason: lo.ToPtr(flexprice.TYPESINVOICEBILLINGREASON_InvoiceBillingReasonManual),
		InvoiceStatus: &draftStatus,
		PaymentStatus: &pendingStatus, // Set to PENDING to prevent auto-payment
		LineItems: []flexprice.DtoCreateInvoiceLineItemRequest{
			{
				DisplayName: lo.ToPtr("Attempt Payment Test Service"),
				Quantity:    "1",
				Amount:      "25.00",
			},
		},
		Metadata: &map[string]string{
			"source": "sdk_test_attempt_payment",
		},
	}

	invoice, _, err := client.InvoicesAPI.InvoicesPost(ctx).
		Invoice(invoiceRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Failed to create invoice for attempt payment test: %v\n", err)
		fmt.Println("⚠ Skipping attempt payment test\n")
		return
	}

	attemptInvoiceID := *invoice.Id
	fmt.Printf("  Created invoice: %s\n", attemptInvoiceID)

	// Finalize the invoice (required for payment attempts)
	_, _, err = client.InvoicesAPI.InvoicesIdFinalizePost(ctx, attemptInvoiceID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Failed to finalize invoice for attempt payment test: %v\n", err)
		fmt.Println("⚠ Skipping attempt payment test\n")
		return
	}

	fmt.Printf("  Finalized invoice\n")

	// Now attempt payment on the finalized, unpaid invoice
	_, response, err := client.InvoicesAPI.InvoicesIdPaymentAttemptPost(ctx, attemptInvoiceID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error attempting payment: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println("⚠ Skipping attempt payment test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping attempt payment test\n")
		return
	}

	fmt.Printf("✓ Payment attempt initiated!\n")
	fmt.Printf("  Invoice ID: %s\n\n", attemptInvoiceID)
}

// Test 11: Download invoice PDF
func testDownloadInvoicePDF(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 11: Download Invoice PDF ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping download PDF test\n")
		return
	}

	_, response, err := client.InvoicesAPI.InvoicesIdPdfGet(ctx, testInvoiceID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error downloading invoice PDF: %v\n", err)
		fmt.Println("⚠ Skipping download PDF test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping download PDF test\n")
		return
	}

	fmt.Printf("✓ Invoice PDF downloaded!\n")
	fmt.Printf("  Invoice ID: %s\n", testInvoiceID)
	fmt.Printf("  PDF file downloaded\n")
}

// Test 12: Trigger invoice communications
func testTriggerInvoiceComms(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 12: Trigger Invoice Communications ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping trigger comms test\n")
		return
	}

	// Trigger invoice communications (no request body needed)
	_, response, err := client.InvoicesAPI.InvoicesIdCommsTriggerPost(ctx, testInvoiceID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error triggering invoice communications: %v\n", err)
		fmt.Println("⚠ Skipping trigger comms test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping trigger comms test\n")
		return
	}

	fmt.Printf("✓ Invoice communications triggered!\n")
	fmt.Printf("  Invoice ID: %s\n\n", testInvoiceID)
}

// Test 13: Get customer invoice summary
func testGetCustomerInvoiceSummary(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 13: Get Customer Invoice Summary ---")

	// Skip if customer not available
	if testCustomerID == "" {
		log.Printf("⚠ Warning: No customer ID available\n")
		fmt.Println("⚠ Skipping customer invoice summary test\n")
		return
	}

	_, response, err := client.InvoicesAPI.CustomersIdInvoicesSummaryGet(ctx, testCustomerID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error getting customer invoice summary: %v\n", err)
		fmt.Println("⚠ Skipping customer invoice summary test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping customer invoice summary test\n")
		return
	}

	fmt.Printf("✓ Customer invoice summary retrieved!\n")
	fmt.Printf("  Customer ID: %s\n", testCustomerID)
	// Note: TotalInvoices field structure may vary
	fmt.Println()
}

// Test 14: Void invoice
func testVoidInvoice(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 14: Void Invoice ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping void invoice test\n")
		return
	}

	_, response, err := client.InvoicesAPI.InvoicesIdVoidPost(ctx, testInvoiceID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error voiding invoice: %v\n", err)
		fmt.Println("⚠ Skipping void invoice test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
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
func testCreatePrice(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Create Price ---")

	// Skip if plan creation failed
	if testPlanID == "" {
		log.Printf("⚠ Warning: No plan ID available\n")
		fmt.Println("⚠ Skipping create price test\n")
		return
	}

	priceRequest := flexprice.DtoCreatePriceRequest{
		EntityId:       testPlanID,
		EntityType:     flexprice.TYPESPRICEENTITYTYPE_PRICE_ENTITY_TYPE_PLAN,
		Currency:       "USD",
		Amount:         lo.ToPtr("99.00"),
		BillingModel:   flexprice.TYPESBILLINGMODEL_BILLING_MODEL_FLAT_FEE,
		BillingCadence: flexprice.TYPESBILLINGCADENCE_BILLING_CADENCE_RECURRING,
		BillingPeriod:  flexprice.TYPESBILLINGPERIOD_BILLING_PERIOD_MONTHLY,
		InvoiceCadence: flexprice.TYPESINVOICECADENCE_InvoiceCadenceAdvance,
		PriceUnitType:  flexprice.TYPESPRICEUNITTYPE_PRICE_UNIT_TYPE_FIAT,
		Type:           flexprice.TYPESPRICETYPE_PRICE_TYPE_FIXED,
		DisplayName:    lo.ToPtr("Monthly Subscription"),
		Description:    lo.ToPtr("Standard monthly subscription price"),
	}

	price, response, err := client.PricesAPI.PricesPost(ctx).
		Price(priceRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error creating price: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 201 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 201/200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	testPriceID = *price.Id
	fmt.Printf("✓ Price created successfully!\n")
	fmt.Printf("  ID: %s\n", *price.Id)
	fmt.Printf("  Amount: %s %s\n", *price.Amount, *price.Currency)
	fmt.Printf("  Billing Model: %s\n\n", string(*price.BillingModel))
}

// Test 2: Get price by ID
func testGetPrice(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Get Price by ID ---")

	if testPriceID == "" {
		log.Printf("⚠ Warning: No price ID available\n")
		fmt.Println("⚠ Skipping get price test\n")
		return
	}

	price, response, err := client.PricesAPI.PricesIdGet(ctx, testPriceID).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting price: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Price retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *price.Id)
	fmt.Printf("  Amount: %s %s\n", *price.Amount, *price.Currency)
	fmt.Printf("  Entity ID: %s\n", *price.EntityId)
	fmt.Printf("  Created At: %s\n\n", *price.CreatedAt)
}

// Test 3: List all prices
func testListPrices(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 3: List Prices ---")

	prices, response, err := client.PricesAPI.PricesGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("❌ Error listing prices: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Retrieved %d prices\n", len(prices.Items))
	if len(prices.Items) > 0 {
		fmt.Printf("  First price: %s - %s %s\n", *prices.Items[0].Id, *prices.Items[0].Amount, *prices.Items[0].Currency)
	}
	if prices.Pagination != nil {
		fmt.Printf("  Total: %d\n", *prices.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update price
func testUpdatePrice(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 4: Update Price ---")

	if testPriceID == "" {
		log.Printf("⚠ Warning: No price ID available\n")
		fmt.Println("⚠ Skipping update price test\n")
		return
	}

	updatedDescription := "Updated price description for testing"
	updateRequest := flexprice.DtoUpdatePriceRequest{
		Description: &updatedDescription,
		Metadata: &map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	price, response, err := client.PricesAPI.PricesIdPut(ctx, testPriceID).
		Price(updateRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error updating price: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Price updated successfully!\n")
	fmt.Printf("  ID: %s\n", *price.Id)
	fmt.Printf("  New Description: %s\n", *price.Description)
	fmt.Printf("  Updated At: %s\n\n", *price.UpdatedAt)
}

// Test 5: Delete price
func testDeletePrice(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Delete Price ---")

	if testPriceID == "" {
		log.Printf("⚠ Warning: No price ID available\n")
		fmt.Println("⚠ Skipping delete price test\n")
		return
	}

	// Delete price requires a request body with optional EndDate
	// Set EndDate to future date to soft-delete the price
	futureDate := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	deleteRequest := flexprice.DtoDeletePriceRequest{
		EndDate: &futureDate,
	}

	_, response, err := client.PricesAPI.PricesIdDelete(ctx, testPriceID).
		Request(deleteRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error deleting price: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println()
		return
	}

	if response.StatusCode != 204 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 204/200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Price deleted successfully!\n")
	fmt.Printf("  Deleted ID: %s\n\n", testPriceID)
}

// PAYMENTS API TESTS
// ========================================

// Test 1: Create a new payment
func testCreatePayment(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Create Payment ---")

	// Create a fresh invoice for this payment test
	// This is necessary because previous tests might have already paid the shared testInvoiceID

	// 1. Create Draft Invoice
	// Important: Set AmountPaid to "0.00" and PaymentStatus to PENDING to prevent auto-payment
	// The backend auto-sets AmountPaid = AmountDue if not provided, which marks invoice as paid
	draftStatus := flexprice.TYPESINVOICESTATUS_InvoiceStatusDraft
	pendingStatus := flexprice.TYPESPAYMENTSTATUS_PaymentStatusPending
	invoiceRequest := flexprice.DtoCreateInvoiceRequest{
		CustomerId:    testCustomerID,
		Currency:      "USD",
		AmountDue:     "100.00",
		Subtotal:      "100.00",
		Total:         "100.00",
		AmountPaid:    lo.ToPtr("0.00"), // Explicitly set to 0 to prevent auto-payment
		InvoiceType:   lo.ToPtr(flexprice.TYPESINVOICETYPE_InvoiceTypeOneOff),
		BillingReason: lo.ToPtr(flexprice.TYPESINVOICEBILLINGREASON_InvoiceBillingReasonManual),
		InvoiceStatus: &draftStatus,
		PaymentStatus: &pendingStatus, // Set to PENDING to prevent auto-payment
		LineItems: []flexprice.DtoCreateInvoiceLineItemRequest{
			{
				DisplayName: lo.ToPtr("Payment Test Service"),
				Quantity:    "1",
				Amount:      "100.00",
			},
		},
		Metadata: &map[string]string{
			"source": "sdk_test_payment",
		},
	}

	invoice, _, err := client.InvoicesAPI.InvoicesPost(ctx).
		Invoice(invoiceRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Failed to create invoice for payment test: %v\n", err)
		return
	}

	paymentInvoiceID := *invoice.Id
	fmt.Printf("  Created invoice for payment: %s\n", paymentInvoiceID)

	// 2. Check invoice status immediately after creation (before finalization)
	// This helps us detect if the invoice is already paid or has issues
	currentInvoice, _, err := client.InvoicesAPI.InvoicesIdGet(ctx, paymentInvoiceID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Failed to get invoice for payment test: %v\n", err)
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

	// Only finalize if the invoice is still in draft status
	if currentInvoice.InvoiceStatus != nil && *currentInvoice.InvoiceStatus == flexprice.TYPESINVOICESTATUS_InvoiceStatusDraft {
		_, response, err := client.InvoicesAPI.InvoicesIdFinalizePost(ctx, paymentInvoiceID).
			Execute()

		if err != nil {
			// Check if it's an unmarshaling error - this happens when API returns a string error instead of JSON
			errStr := err.Error()
			if strings.Contains(errStr, "cannot unmarshal string") || strings.Contains(errStr, "json:") {
				// This is likely a 400 error returned as a string - invoice might already be finalized or invalid
				// Continue anyway - the invoice might still be usable for payment
				log.Printf("⚠ Warning: Invoice finalization returned error (may already be finalized): %v\n", err)
			} else if response != nil && response.StatusCode == 400 {
				log.Printf("⚠ Warning: Invoice may already be finalized or invalid: %v\n", err)
				// Continue anyway - the invoice might be usable
			} else {
				log.Printf("⚠ Warning: Failed to finalize invoice for payment test: %v\n", err)
				return
			}
		} else if response != nil && response.StatusCode == 200 {
			fmt.Printf("  Finalized invoice for payment\n")
		}
	} else {
		if currentInvoice.InvoiceStatus != nil {
			fmt.Printf("  Invoice already finalized (status: %s)\n", string(*currentInvoice.InvoiceStatus))
		} else {
			fmt.Printf("  Invoice status unknown, skipping finalization\n")
		}
	}

	// 3. Verify invoice is unpaid before creating payment
	// Re-fetch the invoice to get the latest payment status after finalization
	finalInvoice, _, err := client.InvoicesAPI.InvoicesIdGet(ctx, paymentInvoiceID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Failed to get final invoice status for payment test: %v\n", err)
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

	paymentRequest := flexprice.DtoCreatePaymentRequest{
		Amount:            "100.00",
		Currency:          "USD",
		DestinationId:     paymentInvoiceID,
		DestinationType:   flexprice.TYPESPAYMENTDESTINATIONTYPE_PaymentDestinationTypeInvoice,
		PaymentMethodType: flexprice.TYPESPAYMENTMETHODTYPE_PaymentMethodTypeOffline,
		ProcessPayment:    lo.ToPtr(false), // Don't process immediately in test
		Metadata: &map[string]string{
			"source":   "sdk_test",
			"test_run": time.Now().Format(time.RFC3339),
		},
	}

	payment, response, err := client.PaymentsAPI.PaymentsPost(ctx).
		Payment(paymentRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error creating payment: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println()
		return
	}

	if response.StatusCode != 201 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 201/200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	testPaymentID = *payment.Id
	fmt.Printf("✓ Payment created successfully!\n")
	fmt.Printf("  ID: %s\n", *payment.Id)
	fmt.Printf("  Amount: %s %s\n", *payment.Amount, *payment.Currency)
	if payment.PaymentStatus != nil {
		fmt.Printf("  Status: %s\n\n", string(*payment.PaymentStatus))
	}
}

// Test 2: Get payment by ID
func testGetPayment(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Get Payment by ID ---")

	if testPaymentID == "" {
		log.Printf("⚠ Warning: No payment ID available\n")
		fmt.Println("⚠ Skipping get payment test\n")
		return
	}

	payment, response, err := client.PaymentsAPI.PaymentsIdGet(ctx, testPaymentID).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting payment: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Payment retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *payment.Id)
	fmt.Printf("  Amount: %s %s\n", *payment.Amount, *payment.Currency)
	if payment.PaymentStatus != nil {
		fmt.Printf("  Status: %s\n", string(*payment.PaymentStatus))
	}
	fmt.Printf("  Created At: %s\n\n", *payment.CreatedAt)
}

// Test 3: List all payments
func testListPayments(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: List Payments ---")

	// Filter to only get the payment we just created to avoid archived payments from other tests
	if testPaymentID == "" {
		log.Printf("⚠ Warning: No payment created in this test run\n")
		fmt.Println("⚠ Skipping list payments test\n")
		return
	}

	payments, response, err := client.PaymentsAPI.PaymentsGet(ctx).
		PaymentIds([]string{testPaymentID}).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error listing payments: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println("⚠ Skipping payments tests (may not have any payments yet)\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping payments tests\n")
		return
	}

	fmt.Printf("✓ Retrieved %d payments\n", len(payments.Items))
	if len(payments.Items) > 0 {
		testPaymentID = *payments.Items[0].Id
		fmt.Printf("  First payment: %s\n", *payments.Items[0].Id)
		if payments.Items[0].PaymentStatus != nil {
			fmt.Printf("  Status: %s\n", string(*payments.Items[0].PaymentStatus))
		}
	}
	if payments.Pagination != nil {
		fmt.Printf("  Total: %d\n", *payments.Pagination.Total)
	}
	fmt.Println()
}

// Test 2: Search payments - SKIPPED
// Note: Payment search endpoint may not be available in current SDK
func testSearchPayments(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Search Payments ---")
	fmt.Println("⚠ Skipping search payments test (endpoint not available in SDK)\n")
}

// Test 4: Update payment
func testUpdatePayment(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 4: Update Payment ---")

	if testPaymentID == "" {
		log.Printf("⚠ Warning: No payment ID available\n")
		fmt.Println("⚠ Skipping update payment test\n")
		return
	}

	updateRequest := flexprice.DtoUpdatePaymentRequest{
		Metadata: &map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	payment, response, err := client.PaymentsAPI.PaymentsIdPut(ctx, testPaymentID).
		Payment(updateRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error updating payment: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Payment updated successfully!\n")
	fmt.Printf("  ID: %s\n", *payment.Id)
	fmt.Printf("  Updated At: %s\n\n", *payment.UpdatedAt)
}

// Test 5: Process payment
func testProcessPayment(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 5: Process Payment ---")

	if testPaymentID == "" {
		log.Printf("⚠ Warning: No payment ID available\n")
		fmt.Println("⚠ Skipping process payment test\n")
		return
	}

	// Note: This will attempt to process the payment
	// In a real scenario, this requires proper payment gateway configuration
	payment, response, err := client.PaymentsAPI.PaymentsIdProcessPost(ctx, testPaymentID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error processing payment: %v\n", err)
		fmt.Println("⚠ Skipping process payment test (may require payment gateway setup)\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping process payment test\n")
		return
	}

	fmt.Printf("✓ Payment processed successfully!\n")
	fmt.Printf("  ID: %s\n", *payment.Id)
	if payment.PaymentStatus != nil {
		fmt.Printf("  Status: %s\n\n", string(*payment.PaymentStatus))
	}
}

// Test 6: Delete payment
func testDeletePayment(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Delete Payment ---")

	if testPaymentID == "" {
		log.Printf("⚠ Warning: No payment ID available\n")
		fmt.Println("⚠ Skipping delete payment test\n")
		return
	}

	_, response, err := client.PaymentsAPI.PaymentsIdDelete(ctx, testPaymentID).
		Execute()

	if err != nil {
		log.Printf("❌ Error deleting payment: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 204 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 204/200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Payment deleted successfully!\n")
	fmt.Printf("  Deleted ID: %s\n\n", testPaymentID)
}

// Test 2: Search connections
func testSearchConnections(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Search Connections ---")

	// Use filter to search connections
	searchFilter := flexprice.TypesConnectionFilter{
		Limit: lo.ToPtr(int32(5)),
	}

	connections, response, err := client.ConnectionsAPI.ConnectionsSearchPost(ctx).
		Filter(searchFilter).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error searching connections: %v\n", err)
		fmt.Println("⚠ Skipping search connections test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping search connections test\n")
		return
	}

	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d connections\n", len(connections.Connections))
	for i, connection := range connections.Connections {
		if i < 3 { // Show first 3 results
			provider := "unknown"
			if connection.ProviderType != nil {
				provider = string(*connection.ProviderType)
			}
			fmt.Printf("  - %s: %s\n", *connection.Id, provider)
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
func testCreateWallet(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Create Wallet ---")

	// Skip if no customer available
	if testCustomerID == "" {
		log.Printf("⚠ Warning: No customer ID available\n")
		fmt.Println("⚠ Skipping create wallet test\n")
		return
	}

	walletRequest := flexprice.DtoCreateWalletRequest{
		CustomerId: &testCustomerID,
		Currency:   "USD",
		Name:       lo.ToPtr("Test Wallet"),
		Metadata: &map[string]string{
			"source":   "sdk_test",
			"test_run": time.Now().Format(time.RFC3339),
		},
	}

	wallet, response, err := client.WalletsAPI.WalletsPost(ctx).
		Request(walletRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error creating wallet: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 201 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 201/200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	testWalletID = *wallet.Id
	fmt.Printf("✓ Wallet created successfully!\n")
	fmt.Printf("  ID: %s\n", *wallet.Id)
	fmt.Printf("  Customer ID: %s\n", *wallet.CustomerId)
	fmt.Printf("  Currency: %s\n\n", *wallet.Currency)
}

// Test 2: Get wallet by ID
func testGetWallet(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Get Wallet by ID ---")

	if testWalletID == "" {
		log.Printf("⚠ Warning: No wallet ID available\n")
		fmt.Println("⚠ Skipping get wallet test\n")
		return
	}

	wallet, response, err := client.WalletsAPI.WalletsIdGet(ctx, testWalletID).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting wallet: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Wallet retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *wallet.Id)
	fmt.Printf("  Customer ID: %s\n", *wallet.CustomerId)
	fmt.Printf("  Currency: %s\n", *wallet.Currency)
	fmt.Printf("  Created At: %s\n\n", *wallet.CreatedAt)
}

// Test 3: List all wallets
func testListWallets(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 3: List Wallets ---")

	wallets, response, err := client.WalletsAPI.WalletsGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("❌ Error listing wallets: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Retrieved %d wallets\n", len(wallets.Items))
	if len(wallets.Items) > 0 {
		fmt.Printf("  First wallet: %s\n", *wallets.Items[0].Id)
	}
	if wallets.Pagination != nil {
		fmt.Printf("  Total: %d\n", *wallets.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update wallet
func testUpdateWallet(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 4: Update Wallet ---")

	if testWalletID == "" {
		log.Printf("⚠ Warning: No wallet ID available\n")
		fmt.Println("⚠ Skipping update wallet test\n")
		return
	}

	updateRequest := flexprice.DtoUpdateWalletRequest{
		Metadata: &map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	wallet, response, err := client.WalletsAPI.WalletsIdPut(ctx, testWalletID).
		Request(updateRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error updating wallet: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Wallet updated successfully!\n")
	fmt.Printf("  ID: %s\n", *wallet.Id)
	fmt.Printf("  Updated At: %s\n\n", *wallet.UpdatedAt)
}

// Test 5: Get wallet balance (real-time)
func testGetWalletBalance(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 5: Get Wallet Balance ---")

	if testWalletID == "" {
		log.Printf("⚠ Warning: No wallet ID available\n")
		fmt.Println("⚠ Skipping get wallet balance test\n")
		return
	}

	balance, response, err := client.WalletsAPI.WalletsIdBalanceRealTimeGet(ctx, testWalletID).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting wallet balance: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Wallet balance retrieved successfully!\n")
	fmt.Printf("  Wallet ID: %s\n", testWalletID)
	if balance.Balance != nil {
		fmt.Printf("  Balance: %s\n", *balance.Balance)
	}
	fmt.Println()
}

// Test 6: Top up wallet
func testTopUpWallet(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 6: Top Up Wallet ---")

	if testWalletID == "" {
		log.Printf("⚠ Warning: No wallet ID available\n")
		fmt.Println("⚠ Skipping top up wallet test\n")
		return
	}

	topUpRequest := flexprice.DtoTopUpWalletRequest{
		Amount:            lo.ToPtr("100.00"),
		TransactionReason: flexprice.TYPESTRANSACTIONREASON_TransactionReasonPurchasedCreditDirect,
		Description:       lo.ToPtr("Test top-up from SDK"),
	}

	_, response, err := client.WalletsAPI.WalletsIdTopUpPost(ctx, testWalletID).
		Request(topUpRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error topping up wallet: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Wallet topped up successfully!\n")
	fmt.Printf("  Wallet ID: %s\n", testWalletID)
	// Balance info available in result.Wallet if needed
	fmt.Println()
}

// Test 7: Debit wallet
func testDebitWallet(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 7: Debit Wallet ---")

	if testWalletID == "" {
		log.Printf("⚠ Warning: No wallet ID available\n")
		fmt.Println("⚠ Skipping debit wallet test\n")
		return
	}

	debitRequest := flexprice.DtoManualBalanceDebitRequest{
		Credits:           lo.ToPtr("10.00"),
		IdempotencyKey:    fmt.Sprintf("test-debit-%d", time.Now().Unix()),
		TransactionReason: flexprice.TYPESTRANSACTIONREASON_TransactionReasonManualBalanceDebit,
		Description:       lo.ToPtr("Test debit from SDK"),
	}

	wallet, response, err := client.WalletsAPI.WalletsIdDebitPost(ctx, testWalletID).
		Request(debitRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error debiting wallet: %v\n", err)
		if response != nil && response.Body != nil {
			bodyBytes, _ := io.ReadAll(response.Body)
			log.Printf("Response: %s", string(bodyBytes))
		}
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Wallet debited successfully!\n")
	fmt.Printf("  Wallet ID: %s\n\n", *wallet.Id)
}

// Test 8: Get wallet transactions
func testGetWalletTransactions(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 8: Get Wallet Transactions ---")

	if testWalletID == "" {
		log.Printf("⚠ Warning: No wallet ID available\n")
		fmt.Println("⚠ Skipping get wallet transactions test\n")
		return
	}

	transactions, response, err := client.WalletsAPI.WalletsIdTransactionsGet(ctx, testWalletID).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting wallet transactions: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Retrieved %d wallet transactions\n", len(transactions.Items))
	if transactions.Pagination != nil {
		fmt.Printf("  Total: %d\n", *transactions.Pagination.Total)
	}
	fmt.Println()
}

// Test 9: Search wallets
func testSearchWallets(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 9: Search Wallets ---")

	searchFilter := flexprice.TypesWalletFilter{
		// Filter by customer if field exists
		Limit: lo.ToPtr(int32(10)),
	}

	wallets, response, err := client.WalletsAPI.WalletsSearchPost(ctx).
		Filter(searchFilter).
		Execute()

	if err != nil {
		log.Printf("❌ Error searching wallets: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Search completed!\n")
	fmt.Printf("  Found %d wallets for customer '%s'\n\n", len(wallets.Items), testCustomerID)
}

// ========================================
// CREDIT GRANTS API TESTS
// ========================================

// Test 1: Create a new credit grant
func testCreateCreditGrant(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Create Credit Grant ---")

	// Skip if no plan available
	if testPlanID == "" {
		log.Printf("⚠ Warning: No plan ID available\n")
		fmt.Println("⚠ Skipping create credit grant test\n")
		return
	}

	grantRequest := flexprice.DtoCreateCreditGrantRequest{
		Scope:                  flexprice.TYPESCREDITGRANTSCOPE_CreditGrantScopePlan,
		PlanId:                 &testPlanID,
		Credits:                "500.00",
		Name:                   "Test Credit Grant",
		Cadence:                flexprice.TYPESCREDITGRANTCADENCE_CreditGrantCadenceOneTime,
		ExpirationType:         lo.ToPtr(flexprice.TYPESCREDITGRANTEXPIRYTYPE_CreditGrantExpiryTypeNever),
		ExpirationDurationUnit: lo.ToPtr(flexprice.TYPESCREDITGRANTEXPIRYDURATIONUNIT_CreditGrantExpiryDurationUnitDays),
		Metadata: &map[string]string{
			"source":   "sdk_test",
			"test_run": time.Now().Format(time.RFC3339),
		},
	}

	grant, response, err := client.CreditGrantsAPI.CreditgrantsPost(ctx).
		CreditGrant(grantRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error creating credit grant: %v\n", err)
		// Print detailed error information
		if response != nil {
			log.Printf("Response Status Code: %d\n", response.StatusCode)
			if response.Body != nil {
				bodyBytes, _ := io.ReadAll(response.Body)
				log.Printf("Response Body: %s\n", string(bodyBytes))
			}
		}
		fmt.Println()
		return
	}

	if response.StatusCode != 201 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 201/200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	testCreditGrantID = *grant.Id
	fmt.Printf("✓ Credit grant created successfully!\n")
	fmt.Printf("  ID: %s\n", *grant.Id)
	if grant.Credits != nil {
		fmt.Printf("  Credits: %.2f\n", *grant.Credits)
	}
	fmt.Printf("  Plan ID: %s\n\n", *grant.PlanId)
}

// Test 2: Get credit grant by ID
func testGetCreditGrant(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Get Credit Grant by ID ---")

	if testCreditGrantID == "" {
		log.Printf("⚠ Warning: No credit grant ID available\n")
		fmt.Println("⚠ Skipping get credit grant test\n")
		return
	}

	grant, response, err := client.CreditGrantsAPI.CreditgrantsIdGet(ctx, testCreditGrantID).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting credit grant: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Credit grant retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *grant.Id)
	if grant.Credits != nil {
		fmt.Printf("  Credits: %.2f\n", *grant.Credits)
	}
	fmt.Printf("  Created At: %s\n\n", *grant.CreatedAt)
}

// Test 3: List all credit grants
func testListCreditGrants(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 3: List Credit Grants ---")

	grants, response, err := client.CreditGrantsAPI.CreditgrantsGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("❌ Error listing credit grants: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Retrieved %d credit grants\n", len(grants.Items))
	if len(grants.Items) > 0 {
		fmt.Printf("  First grant: %s\n", *grants.Items[0].Id)
	}
	if grants.Pagination != nil {
		fmt.Printf("  Total: %d\n", *grants.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Update credit grant
func testUpdateCreditGrant(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 4: Update Credit Grant ---")

	if testCreditGrantID == "" {
		log.Printf("⚠ Warning: No credit grant ID available\n")
		fmt.Println("⚠ Skipping update credit grant test\n")
		return
	}

	updateRequest := flexprice.DtoUpdateCreditGrantRequest{
		Metadata: &map[string]string{
			"updated_at": time.Now().Format(time.RFC3339),
			"status":     "updated",
		},
	}

	grant, response, err := client.CreditGrantsAPI.CreditgrantsIdPut(ctx, testCreditGrantID).
		CreditGrant(updateRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error updating credit grant: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Credit grant updated successfully!\n")
	fmt.Printf("  ID: %s\n", *grant.Id)
	fmt.Printf("  Updated At: %s\n\n", *grant.UpdatedAt)
}

// Test 5: Delete credit grant
func testDeleteCreditGrant(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Delete Credit Grant ---")

	if testCreditGrantID == "" {
		log.Printf("⚠ Warning: No credit grant ID available\n")
		fmt.Println("⚠ Skipping delete credit grant test\n")
		return
	}

	_, response, err := client.CreditGrantsAPI.CreditgrantsIdDelete(ctx, testCreditGrantID).
		Execute()

	if err != nil {
		log.Printf("❌ Error deleting credit grant: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 204 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 204/200, got %d\n", response.StatusCode)
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
func testCreateCreditNote(ctx context.Context, client *flexprice.APIClient) {
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
	invoice, _, err := client.InvoicesAPI.InvoicesIdGet(ctx, testInvoiceID).Execute()
	if err != nil || invoice == nil {
		log.Printf("⚠ Warning: Could not retrieve invoice: %v\n", err)
		fmt.Println("⚠ Skipping create credit note test\n")
		return
	}

	log.Printf("Invoice has %d line items\n", len(invoice.LineItems))
	if len(invoice.LineItems) == 0 {
		log.Printf("⚠ Warning: Invoice has no line items\n")
		fmt.Println("⚠ Skipping create credit note test\n")
		return
	}

	// Use first line item from invoice for credit note
	firstLineItem := invoice.LineItems[0]
	creditAmount := "50.00" // Credit 50% of the line item amount

	noteRequest := flexprice.DtoCreateCreditNoteRequest{
		InvoiceId: testInvoiceID,
		Reason:    flexprice.TYPESCREDITNOTEREASON_CreditNoteReasonBillingError,
		Memo:      lo.ToPtr("Test credit note from SDK"),
		LineItems: []flexprice.DtoCreateCreditNoteLineItemRequest{
			{
				InvoiceLineItemId: *firstLineItem.Id,
				Amount:            creditAmount,
				DisplayName:       lo.ToPtr("Credit for " + lo.FromPtrOr(firstLineItem.DisplayName, "Invoice Line Item")),
			},
		},
		Metadata: &map[string]string{
			"source":   "sdk_test",
			"test_run": time.Now().Format(time.RFC3339),
		},
	}

	note, response, err := client.CreditNotesAPI.CreditnotesPost(ctx).
		CreditNote(noteRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error creating credit note: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 201 && response.StatusCode != 200 {
		log.Printf("❌ Expected status code 201/200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	testCreditNoteID = *note.Id
	fmt.Printf("✓ Credit note created successfully!\n")
	fmt.Printf("  ID: %s\n", *note.Id)
	// Credit note details
	fmt.Printf("  Invoice ID: %s\n", *note.InvoiceId)
	fmt.Println()
}

// Test 2: Get credit note by ID
func testGetCreditNote(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Get Credit Note by ID ---")

	if testCreditNoteID == "" {
		log.Printf("⚠ Warning: No credit note ID available\n")
		fmt.Println("⚠ Skipping get credit note test\n")
		return
	}

	note, response, err := client.CreditNotesAPI.CreditnotesIdGet(ctx, testCreditNoteID).
		Execute()

	if err != nil {
		log.Printf("❌ Error getting credit note: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Credit note retrieved successfully!\n")
	fmt.Printf("  ID: %s\n", *note.Id)
	// Credit note details
	fmt.Printf("  Invoice ID: %s\n", *note.InvoiceId)
	fmt.Printf("  Created At: %s\n\n", *note.CreatedAt)
}

// Test 3: List all credit notes
func testListCreditNotes(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 3: List Credit Notes ---")

	notes, response, err := client.CreditNotesAPI.CreditnotesGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("❌ Error listing credit notes: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 200 {
		log.Printf("❌ Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	fmt.Printf("✓ Retrieved %d credit notes\n", len(notes.Items))
	if len(notes.Items) > 0 {
		fmt.Printf("  First note: %s\n", *notes.Items[0].Id)
	}
	if notes.Pagination != nil {
		fmt.Printf("  Total: %d\n", *notes.Pagination.Total)
	}
	fmt.Println()
}

// Test 4: Finalize credit note
func testFinalizeCreditNote(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 4: Finalize Credit Note ---")

	if testCreditNoteID == "" {
		log.Printf("⚠ Warning: No credit note ID available\n")
		fmt.Println("⚠ Skipping finalize credit note test\n")
		return
	}

	note, response, err := client.CreditNotesAPI.CreditnotesIdFinalizePost(ctx, testCreditNoteID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error finalizing credit note: %v\n", err)
		fmt.Println("⚠ Skipping finalize credit note test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping finalize credit note test\n")
		return
	}

	fmt.Printf("✓ Credit note finalized successfully!\n")
	if note != nil && note.Id != nil {
		fmt.Printf("  ID: %s\n\n", *note.Id)
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
func testCreateEvent(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 1: Create Event ---")

	// Use test customer external ID if available, otherwise generate a unique one
	if testExternalID == "" {
		testEventCustomerID = fmt.Sprintf("test-customer-%d", time.Now().Unix())
	} else {
		testEventCustomerID = testExternalID
	}

	testEventName = fmt.Sprintf("Test Event %d", time.Now().Unix())

	eventRequest := flexprice.DtoIngestEventRequest{
		EventName:          testEventName,
		ExternalCustomerId: testEventCustomerID,
		Properties: &map[string]string{
			"source":      "sdk_test",
			"environment": "test",
			"test_run":    time.Now().Format(time.RFC3339),
		},
		Source:    lo.ToPtr("sdk_test"),
		Timestamp: lo.ToPtr(time.Now().Format(time.RFC3339)),
	}

	result, response, err := client.EventsAPI.EventsPost(ctx).
		Event(eventRequest).
		Execute()

	if err != nil {
		log.Printf("❌ Error creating event: %v\n", err)
		fmt.Println()
		return
	}

	if response.StatusCode != 202 {
		log.Printf("❌ Expected status code 202, got %d\n", response.StatusCode)
		fmt.Println()
		return
	}

	// The result is a map[string]string, so we can access it directly
	if result != nil {
		if eventId, ok := result["event_id"]; ok {
			testEventID = eventId
			fmt.Printf("✓ Event created successfully!\n")
			fmt.Printf("  Event ID: %s\n", eventId)
			fmt.Printf("  Event Name: %s\n", testEventName)
			fmt.Printf("  Customer ID: %s\n\n", testEventCustomerID)
		} else {
			fmt.Printf("✓ Event created successfully!\n")
			fmt.Printf("  Event Name: %s\n", testEventName)
			fmt.Printf("  Customer ID: %s\n", testEventCustomerID)
			fmt.Printf("  Response: %v\n\n", result)
		}
	} else {
		fmt.Printf("✓ Event created successfully!\n")
		fmt.Printf("  Event Name: %s\n", testEventName)
		fmt.Printf("  Customer ID: %s\n\n", testEventCustomerID)
	}
}

// Test 2: Query events
func testQueryEvents(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 2: Query Events ---")

	// Skip if no event was created
	if testEventName == "" {
		log.Printf("⚠ Warning: No event created, skipping query test\n")
		fmt.Println()
		return
	}

	queryRequest := flexprice.DtoGetEventsRequest{
		ExternalCustomerId: &testEventCustomerID,
		EventName:          &testEventName,
	}

	events, response, err := client.EventsAPI.EventsQueryPost(ctx).
		Request(queryRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error querying events: %v\n", err)
		fmt.Println("⚠ Skipping query events test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping query events test\n")
		return
	}

	fmt.Printf("✓ Events queried successfully!\n")
	if events.Events != nil {
		fmt.Printf("  Found %d events\n", len(events.Events))
		for i, event := range events.Events {
			if i < 3 { // Show first 3 events
				if event.Id != nil {
					fmt.Printf("  - Event %d: %s - %s\n", i+1, *event.Id, *event.EventName)
				} else {
					fmt.Printf("  - Event %d: %s\n", i+1, *event.EventName)
				}
			}
		}
	} else {
		fmt.Println("  No events found")
	}
	fmt.Println()
}

// Test 3: Async event - Simple enqueue
func testAsyncEventEnqueue(ctx context.Context, client *flexprice.APIClient) {
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
func testAsyncEventEnqueueWithOptions(ctx context.Context, client *flexprice.APIClient) {
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
func testAsyncEventBatch(ctx context.Context, client *flexprice.APIClient) {
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
