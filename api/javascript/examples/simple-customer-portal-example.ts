/**
 * Simple Customer Portal Example
 * 
 * This example demonstrates how to use the CustomerPortalApi
 * to fetch customer data with minimal code.
 */

import { CustomerPortalApi, getCustomerData, getCustomerSummary } from '../src/apis/CustomerPortalApi';
import * as runtime from '../src/runtime';

// Configure the API client
const defaultClient = runtime.ApiClient.instance;
defaultClient.basePath = "https://api.cloud.flexprice.io/v1";

const apiKeyAuth = defaultClient.authentications["ApiKeyAuth"];
apiKeyAuth.apiKey = process.env.FLEXPRICE_API_KEY || "your-api-key-here";
apiKeyAuth.in = "header";
apiKeyAuth.name = "x-api-key";

async function main() {
    console.log("🚀 Customer Portal API Example");
    console.log("================================");

    // Create API instance
    const api = new CustomerPortalApi();

    try {
        // Example 1: Get all customer data
        console.log("\n📊 Fetching customer data...");
        const customerData = await api.getCustomerData('customer-123', {
            includeCustomer: true,
            includeSubscriptions: true,
            includeInvoices: true,
            includePayments: true,
            subscriptionLimit: 5,
            days: 30
        });

        console.log("✅ Customer data fetched successfully!");
        console.log(`   Customer: ${customerData.customer?.name || 'N/A'}`);
        console.log(`   Subscriptions: ${customerData.subscriptions?.data?.length || 0}`);
        console.log(`   Invoices: ${customerData.invoices?.data?.length || 0}`);
        console.log(`   Payments: ${customerData.payments?.data?.length || 0}`);

        if (customerData.errors?.length) {
            console.log(`   ⚠️  Errors: ${customerData.errors.length}`);
        }

        // Example 2: Get customer summary
        console.log("\n📈 Fetching customer summary...");
        const summary = await api.getCustomerSummary('customer-123');

        if (summary) {
            console.log("✅ Customer summary:");
            console.log(`   Status: ${summary.status}`);
            console.log(`   Active Subscriptions: ${summary.activeSubscriptions}`);
            console.log(`   Total Spent: ${summary.currency} ${summary.totalSpent}`);
            console.log(`   Last Payment: ${summary.lastPaymentDate || 'N/A'}`);
        }

        // Example 3: One-liner function
        console.log("\n⚡ Using one-liner function...");
        const quickData = await getCustomerData('customer-123', {
            includeCustomer: true,
            includeSubscriptions: true,
            subscriptionLimit: 3
        });

        console.log("✅ Quick data fetch completed!");
        console.log(`   Subscriptions: ${quickData.subscriptions?.data?.length || 0}`);

    } catch (error) {
        console.error("❌ Error:", error);
    }
}

// Run the example
if (require.main === module) {
    main().catch(console.error);
}

export { main };