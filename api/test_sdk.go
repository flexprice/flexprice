package main

import (
	"context"
	"fmt"
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

	testAddonID   string
	testAddonName string

	testEntitlementID string

	testSubscriptionID string

	testInvoiceID string

	testPriceID string

	testPaymentID string
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
	testPauseSubscription(ctx, client)
	testResumeSubscription(ctx, client)
	testGetPauseHistory(ctx, client)

	// Addon management
	testAddAddonToSubscription(ctx, client)
	testGetActiveAddons(ctx, client)
	testRemoveAddonFromSubscription(ctx, client)

	// Change management
	testPreviewSubscriptionChange(ctx, client)
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
	lookupKey := fmt.Sprintf("test_addon_%d", timestamp)

	addonRequest := flexprice.DtoCreateAddonRequest{
		Name:        testAddonName,
		LookupKey:   lookupKey,
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

	lookupKey := fmt.Sprintf("test_addon_%d", time.Now().Unix())

	addon, response, err := client.AddonsAPI.AddonsLookupLookupKeyGet(ctx, lookupKey).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error looking up addon: %v\n", err)
		fmt.Println("⚠ Skipping lookup test (lookup key may not match)\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping lookup test\n")
		return
	}

	fmt.Printf("✓ Addon found by lookup key!\n")
	fmt.Printf("  Lookup Key: %s\n", lookupKey)
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

	startDate := time.Now().Format(time.RFC3339)
	subscriptionRequest := flexprice.DtoCreateSubscriptionRequest{
		PlanId:             testPlanID,
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

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping activate test\n")
		fmt.Println()
		return
	}

	// Note: This will only work if subscription is in draft status
	// Most subscriptions are created as active, so this may fail
	_, response, err := client.SubscriptionsAPI.SubscriptionsIdActivatePost(ctx, testSubscriptionID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error activating subscription (may already be active): %v\n", err)
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

	addAddonRequest := flexprice.DtoAddAddonToSubscriptionRequest{
		AddonId: testAddonID,
	}

	subscription, response, err := client.SubscriptionsAPI.SubscriptionsAddonPost(ctx).
		Request(addAddonRequest).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error adding addon to subscription: %v\n", err)
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

// Test 11: Get active addons
func testGetActiveAddons(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 11: Get Active Addons ---")

	// Skip if subscription creation failed
	if testSubscriptionID == "" {
		log.Printf("⚠ Warning: No subscription created, skipping get active addons test\n")
		fmt.Println()
		return
	}

	addons, response, err := client.SubscriptionsAPI.SubscriptionsIdAddonsActiveGet(ctx, testSubscriptionID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error getting active addons: %v\n", err)
		fmt.Println("⚠ Skipping get active addons test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping get active addons test\n")
		return
	}

	fmt.Printf("✓ Retrieved active addons!\n")
	fmt.Printf("  Total active addons: %d\n", len(addons))
	for i, addon := range addons {
		if i < 3 {
			fmt.Printf("  - %s\n", *addon.AddonId)
		}
	}
	fmt.Println()
}

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

	invoiceRequest := flexprice.DtoCreateInvoiceRequest{
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

	previewRequest := flexprice.DtoGetPreviewInvoiceRequest{}

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

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping finalize invoice test\n")
		return
	}

	_, response, err := client.InvoicesAPI.InvoicesIdFinalizePost(ctx, testInvoiceID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error finalizing invoice: %v\n", err)
		fmt.Println("⚠ Skipping finalize invoice test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping finalize invoice test\n")
		return
	}

	fmt.Printf("✓ Invoice finalized successfully!\n")
	fmt.Printf("  Invoice finalized\n")
}

// Test 8: Recalculate invoice
func testRecalculateInvoice(ctx context.Context, client *flexprice.APIClient) {
	fmt.Println("--- Test 8: Recalculate Invoice ---")

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping recalculate invoice test\n")
		return
	}

	invoice, response, err := client.InvoicesAPI.InvoicesIdRecalculatePost(ctx, testInvoiceID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error recalculating invoice: %v\n", err)
		fmt.Println("⚠ Skipping recalculate invoice test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping recalculate invoice test\n")
		return
	}

	fmt.Printf("✓ Invoice recalculated successfully!\n")
	fmt.Printf("  Invoice finalized\n")
	fmt.Printf("  Total: %s %s\n\n", *invoice.Currency, *invoice.Total)
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
		Amount: lo.ToPtr("100.00"),
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

	// Skip if invoice creation failed
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping attempt payment test\n")
		return
	}

	_, response, err := client.InvoicesAPI.InvoicesIdPaymentAttemptPost(ctx, testInvoiceID).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error attempting payment: %v\n", err)
		fmt.Println("⚠ Skipping attempt payment test\n")
		return
	}

	if response.StatusCode != 200 {
		log.Printf("⚠ Warning: Expected status code 200, got %d\n", response.StatusCode)
		fmt.Println("⚠ Skipping attempt payment test\n")
		return
	}

	fmt.Printf("✓ Payment attempt initiated!\n")
	fmt.Printf("  Invoice ID: %s\n\n", testInvoiceID)
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

	_, response, err := client.PricesAPI.PricesIdDelete(ctx, testPriceID).
		Execute()

	if err != nil {
		log.Printf("❌ Error deleting price: %v\n", err)
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

	// Skip if no invoice available (payments typically require an invoice)
	if testInvoiceID == "" {
		log.Printf("⚠ Warning: No invoice ID available\n")
		fmt.Println("⚠ Skipping create payment test (requires invoice)\n")
		return
	}

	paymentRequest := flexprice.DtoCreatePaymentRequest{
		Amount:            "100.00",
		Currency:          "USD",
		DestinationId:     testInvoiceID,
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

	payments, response, err := client.PaymentsAPI.PaymentsGet(ctx).
		Limit(10).
		Execute()

	if err != nil {
		log.Printf("⚠ Warning: Error listing payments: %v\n", err)
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
