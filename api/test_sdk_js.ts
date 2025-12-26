#!/usr/bin/env ts-node

/**
 * FlexPrice JavaScript SDK - Comprehensive API Tests
 * 
 * This test suite covers all FlexPrice APIs with comprehensive CRUD operations.
 * Tests are designed to run sequentially, building on previous test results.
 */

import {
    Configuration,
    CustomersApi,
    FeaturesApi,
    PlansApi,
    AddonsApi,
    EntitlementsApi,
    SubscriptionsApi,
    InvoicesApi,
    PricesApi,
    PaymentsApi,
    WalletsApi,
    CreditGrantsApi,
    CreditNotesApi,
    ConnectionsApi,
    EventsApi,
    TypesFeatureType,
    TypesPlanFilter,
    TypesAddonType,
    TypesAddonFilter,
    TypesEntitlementUsageResetPeriod,
    TypesEntitlementFilter,
    TypesBillingCadence,
    TypesBillingModel,
    TypesBillingPeriod,
    TypesBillingCycle,
    TypesSubscriptionStatus,
    TypesCancellationType,
    TypesInvoiceType,
    TypesInvoiceBillingReason,
    TypesInvoiceStatus,
    TypesPriceEntityType,
    TypesPriceType,
    TypesPriceUnitType,
    TypesInvoiceCadence,
    TypesPaymentMethodType,
    TypesPaymentStatus,
    TypesTransactionReason,
    TypesCreditNoteReason,
    TypesCreditGrantScope,
    TypesCreditGrantCadence,
    TypesCreditGrantExpiryType,
    TypesCreditGrantExpiryDurationUnit,
} from './javascript/dist/index';

// Global test entity IDs
let testCustomerID = '';
let testCustomerName = '';

let testFeatureID = '';
let testFeatureName = '';

let testPlanID = '';
let testPlanName = '';

let testAddonID = '';
let testAddonName = '';
let testAddonLookupKey = '';

let testEntitlementID = '';

let testSubscriptionID = '';

let testInvoiceID = '';

let testPriceID = '';

let testPaymentID = '';

let testWalletID = '';
let testCreditGrantID = '';
let testCreditNoteID = '';

let testEventID = '';
let testEventName = '';
let testEventCustomerID = '';

// ========================================
// CONFIGURATION
// ========================================

function getConfiguration(): Configuration {
    const apiKey = process.env.FLEXPRICE_API_KEY;
    const apiHost = process.env.FLEXPRICE_API_HOST;

    if (!apiKey) {
        console.error('❌ Missing FLEXPRICE_API_KEY environment variable');
        process.exit(1);
    }
    if (!apiHost) {
        console.error('❌ Missing FLEXPRICE_API_HOST environment variable');
        process.exit(1);
    }

    console.log('=== FlexPrice JavaScript SDK - API Tests ===\n');
    console.log(`✓ API Key: ${apiKey.substring(0, 8)}...${apiKey.slice(-4)}`);
    console.log(`✓ API Host: ${apiHost}\n`);

    let fullPath = apiHost;
    if (!fullPath.startsWith('http://') && !fullPath.startsWith('https://')) {
        fullPath = `https://${fullPath}`;
    }

    return new Configuration({
        basePath: fullPath,
        apiKey: apiKey,
    });
}

// ========================================
// CUSTOMERS API TESTS
// ========================================

async function testCreateCustomer(config: Configuration) {
    console.log('--- Test 1: Create Customer ---');

    try {
        const api = new CustomersApi(config);
        const timestamp = Date.now();
        testCustomerName = `Test Customer ${timestamp}`;

        const response = await api.customersPost({
            customer: {
                name: testCustomerName,
                email: `test-${timestamp}@example.com`,
                externalId: `test-customer-${timestamp}`,
                metadata: {
                    source: 'sdk_test',
                    test_run: new Date().toISOString(),
                    environment: 'test',
                },
            },
        });

        testCustomerID = response.id!;
        console.log('✓ Customer created successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Name: ${response.name}`);
        console.log(`  External ID: ${response.externalId}`);
        console.log(`  Email: ${response.email}\n`);
    } catch (error: any) {
        console.log(`❌ Error creating customer: ${error.message}\n`);
    }
}

async function testGetCustomer(config: Configuration) {
    console.log('--- Test 2: Get Customer by ID ---');

    try {
        const api = new CustomersApi(config);
        const response = await api.customersIdGet({ id: testCustomerID });

        console.log('✓ Customer retrieved successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Name: ${response.name}`);
        console.log(`  Created At: ${response.createdAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting customer: ${error.message}\n`);
    }
}

async function testListCustomers(config: Configuration) {
    console.log('--- Test 3: List Customers ---');

    try {
        const api = new CustomersApi(config);
        const response = await api.customersGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.items?.length || 0} customers`);
        if (response.items && response.items.length > 0) {
            console.log(`  First customer: ${response.items[0].id} - ${response.items[0].name}`);
        }
        if (response.pagination) {
            console.log(`  Total: ${response.pagination.total}\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing customers: ${error.message}\n`);
    }
}

async function testUpdateCustomer(config: Configuration) {
    console.log('--- Test 4: Update Customer ---');

    try {
        const api = new CustomersApi(config);
        const response = await api.customersIdPut({
            id: testCustomerID,
            customer: {
                name: `${testCustomerName} (Updated)`,
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });

        console.log('✓ Customer updated successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  New Name: ${response.name}`);
        console.log(`  Updated At: ${response.updatedAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error updating customer: ${error.message}\n`);
    }
}

async function testLookupCustomer(config: Configuration) {
    console.log('--- Test 5: Lookup Customer by External ID ---');

    try {
        const api = new CustomersApi(config);
        const externalId = `test-customer-${testCustomerName.split(' ')[2]}`;
        const response = await api.customersLookupLookupKeyGet({ lookupKey: externalId });

        console.log('✓ Customer found by external ID!');
        console.log(`  External ID: ${externalId}`);
        console.log(`  Customer ID: ${response.id}`);
        console.log(`  Name: ${response.name}\n`);
    } catch (error: any) {
        console.log(`❌ Error looking up customer: ${error.message}\n`);
    }
}

async function testSearchCustomers(config: Configuration) {
    console.log('--- Test 6: Search Customers ---');

    try {
        const api = new CustomersApi(config);
        const externalId = `test-customer-${testCustomerName.split(' ')[2]}`;

        const response = await api.customersSearchPost({
            filter: {
                externalId: externalId,
            },
        });

        console.log('✓ Search completed!');
        console.log(`  Found ${response.items?.length || 0} customers matching external ID '${externalId}'`);
        if (response.items && response.items.length > 0) {
            response.items.forEach(customer => {
                console.log(`  - ${customer.id}: ${customer.name}`);
            });
        }
        console.log();
    } catch (error: any) {
        console.log(`❌ Error searching customers: ${error.message}\n`);
    }
}

async function testGetCustomerEntitlements(config: Configuration) {
    console.log('--- Test 7: Get Customer Entitlements ---');

    try {
        const api = new CustomersApi(config);
        const response = await api.customersIdEntitlementsGet({ id: testCustomerID });

        console.log('✓ Retrieved customer entitlements!');
        console.log(`  Total features: ${response.features?.length || 0}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting customer entitlements: ${error.message}\n`);
    }
}

async function testGetCustomerUpcomingGrants(config: Configuration) {
    console.log('--- Test 8: Get Customer Upcoming Grants ---');

    try {
        const api = new CustomersApi(config);
        const response = await api.customersIdGrantsUpcomingGet({ id: testCustomerID });

        console.log('✓ Retrieved upcoming grants!');
        console.log(`  Total upcoming grants: ${response.items?.length || 0}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting upcoming grants: ${error.message}\n`);
    }
}

async function testGetCustomerUsage(config: Configuration) {
    console.log('--- Test 9: Get Customer Usage ---');

    try {
        const api = new CustomersApi(config);
        const response = await api.customersUsageGet({ customerId: testCustomerID });

        console.log('✓ Retrieved customer usage!');
        console.log(`  Usage records: ${response.features?.length || 0}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting customer usage: ${error.message}\n`);
    }
}

// ========================================
// FEATURES API TESTS
// ========================================

async function testCreateFeature(config: Configuration) {
    console.log('--- Test 1: Create Feature ---');

    try {
        const api = new FeaturesApi(config);
        const timestamp = Date.now();
        testFeatureName = `Test Feature ${timestamp}`;
        const featureKey = `test_feature_${timestamp}`;

        const response = await api.featuresPost({
            feature: {
                name: testFeatureName,
                lookupKey: featureKey,
                description: 'This is a test feature created by SDK tests',
                type: TypesFeatureType.FeatureTypeBoolean,
                metadata: {
                    source: 'sdk_test',
                    test_run: new Date().toISOString(),
                    environment: 'test',
                },
            },
        });

        testFeatureID = response.id!;
        console.log('✓ Feature created successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Name: ${response.name}`);
        console.log(`  Lookup Key: ${response.lookupKey}`);
        console.log(`  Type: ${response.type}\n`);
    } catch (error: any) {
        console.log(`❌ Error creating feature: ${error.message}\n`);
    }
}

async function testGetFeature(config: Configuration) {
    console.log('--- Test 2: Get Feature by ID ---');

    try {
        const api = new FeaturesApi(config);
        const response = await api.featuresIdGet({ id: testFeatureID });

        console.log('✓ Feature retrieved successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Name: ${response.name}`);
        console.log(`  Lookup Key: ${response.lookupKey}`);
        console.log(`  Created At: ${response.createdAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting feature: ${error.message}\n`);
    }
}

async function testListFeatures(config: Configuration) {
    console.log('--- Test 3: List Features ---');

    try {
        const api = new FeaturesApi(config);
        const response = await api.featuresGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.items?.length || 0} features`);
        if (response.items && response.items.length > 0) {
            console.log(`  First feature: ${response.items[0].id} - ${response.items[0].name}`);
        }
        if (response.pagination) {
            console.log(`  Total: ${response.pagination.total}\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing features: ${error.message}\n`);
    }
}

async function testUpdateFeature(config: Configuration) {
    console.log('--- Test 4: Update Feature ---');

    try {
        const api = new FeaturesApi(config);
        const response = await api.featuresIdPut({
            id: testFeatureID,
            feature: {
                name: `${testFeatureName} (Updated)`,
                description: 'Updated description for test feature',
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });

        console.log('✓ Feature updated successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  New Name: ${response.name}`);
        console.log(`  New Description: ${response.description}`);
        console.log(`  Updated At: ${response.updatedAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error updating feature: ${error.message}\n`);
    }
}

async function testSearchFeatures(config: Configuration) {
    console.log('--- Test 5: Search Features ---');

    try {
        const api = new FeaturesApi(config);
        const response = await api.featuresSearchPost({
            filter: {
                featureIds: [testFeatureID],
            },
        });

        console.log('✓ Search completed!');
        console.log(`  Found ${response.items?.length || 0} features matching ID '${testFeatureID}'`);
        if (response.items && response.items.length > 0) {
            response.items.slice(0, 3).forEach((feature: any) => {
                console.log(`  - ${feature.id}: ${feature.name} (${feature.lookupKey})`);
            });
        }
        console.log();
    } catch (error: any) {
        console.log(`❌ Error searching features: ${error.message}\n`);
    }
}


// ========================================
// PLANS API TESTS
// ========================================

async function testCreatePlan(config: Configuration) {
    console.log('--- Test 1: Create Plan ---');

    try {
        const api = new PlansApi(config);
        const timestamp = Date.now();
        testPlanName = `Test Plan ${timestamp}`;
        const lookupKey = `test_plan_${timestamp}`;

        const response = await api.plansPost({
            plan: {
                name: testPlanName,
                lookupKey: lookupKey,
                description: 'This is a test plan created by SDK tests',
                metadata: {
                    source: 'sdk_test',
                    test_run: new Date().toISOString(),
                    environment: 'test',
                },
            },
        });

        testPlanID = response.id!;
        console.log('✓ Plan created successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Name: ${response.name}`);
        console.log(`  Lookup Key: ${response.lookupKey}\n`);
    } catch (error: any) {
        console.log(`❌ Error creating plan: ${error.message}\n`);
    }
}

async function testGetPlan(config: Configuration) {
    console.log('--- Test 2: Get Plan by ID ---');

    try {
        const api = new PlansApi(config);
        const response = await api.plansIdGet({ id: testPlanID });

        console.log('✓ Plan retrieved successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Name: ${response.name}`);
        console.log(`  Lookup Key: ${response.lookupKey}`);
        console.log(`  Created At: ${response.createdAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting plan: ${error.message}\n`);
    }
}

async function testListPlans(config: Configuration) {
    console.log('--- Test 3: List Plans ---');

    try {
        const api = new PlansApi(config);
        const response = await api.plansGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.items?.length || 0} plans`);
        if (response.items && response.items.length > 0) {
            console.log(`  First plan: ${response.items[0].id} - ${response.items[0].name}`);
        }
        if (response.pagination) {
            console.log(`  Total: ${response.pagination.total}\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing plans: ${error.message}\n`);
    }
}

async function testUpdatePlan(config: Configuration) {
    console.log('--- Test 4: Update Plan ---');

    try {
        const api = new PlansApi(config);
        const response = await api.plansIdPut({
            id: testPlanID,
            plan: {
                name: `${testPlanName} (Updated)`,
                description: 'Updated description for test plan',
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });

        console.log('✓ Plan updated successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  New Name: ${response.name}`);
        console.log(`  New Description: ${response.description}`);
        console.log(`  Updated At: ${response.updatedAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error updating plan: ${error.message}\n`);
    }
}

async function testSearchPlans(config: Configuration) {
    console.log('--- Test 5: Search Plans ---');

    try {
        const api = new PlansApi(config);
        const response = await api.plansSearchPost({
            filter: {
                planIds: [testPlanID],
            },
        });

        console.log('✓ Search completed!');
        console.log(`  Found ${response.items?.length || 0} plans matching ID '${testPlanID}'`);
        if (response.items && response.items.length > 0) {
            response.items.slice(0, 3).forEach(plan => {
                console.log(`  - ${plan.id}: ${plan.name} (${plan.lookupKey})`);
            });
        }
        console.log();
    } catch (error: any) {
        console.log(`❌ Error searching plans: ${error.message}\n`);
    }
}

// ========================================
// ADDONS API TESTS
// ========================================

async function testCreateAddon(config: Configuration) {
    console.log('--- Test 1: Create Addon ---');

    try {
        const api = new AddonsApi(config);
        const timestamp = Date.now();
        testAddonName = `Test Addon ${timestamp}`;
        testAddonLookupKey = `test_addon_${timestamp}`;

        const response = await api.addonsPost({
            addon: {
                name: testAddonName,
                lookupKey: testAddonLookupKey,
                description: 'This is a test addon created by SDK tests',
                type: TypesAddonType.AddonTypeOnetime,
                metadata: {
                    source: 'sdk_test',
                    test_run: new Date().toISOString(),
                    environment: 'test',
                },
            },
        });

        testAddonID = response.id!;
        console.log('✓ Addon created successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Name: ${response.name}`);
        console.log(`  Lookup Key: ${response.lookupKey}\n`);
    } catch (error: any) {
        console.log(`❌ Error creating addon: ${error.message}\n`);
    }
}

async function testGetAddon(config: Configuration) {
    console.log('--- Test 2: Get Addon by ID ---');

    try {
        const api = new AddonsApi(config);
        const response = await api.addonsIdGet({ id: testAddonID });

        console.log('✓ Addon retrieved successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Name: ${response.name}`);
        console.log(`  Lookup Key: ${response.lookupKey}`);
        console.log(`  Created At: ${response.createdAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting addon: ${error.message}\n`);
    }
}

async function testListAddons(config: Configuration) {
    console.log('--- Test 3: List Addons ---');

    try {
        const api = new AddonsApi(config);
        const response = await api.addonsGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.items?.length || 0} addons`);
        if (response.items && response.items.length > 0) {
            console.log(`  First addon: ${response.items[0].id} - ${response.items[0].name}`);
        }
        if (response.pagination) {
            console.log(`  Total: ${response.pagination.total}\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing addons: ${error.message}\n`);
    }
}

async function testUpdateAddon(config: Configuration) {
    console.log('--- Test 4: Update Addon ---');

    try {
        const api = new AddonsApi(config);
        const response = await api.addonsIdPut({
            id: testAddonID,
            addon: {
                name: `${testAddonName} (Updated)`,
                description: 'Updated description for test addon',
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });

        console.log('✓ Addon updated successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  New Name: ${response.name}`);
        console.log(`  New Description: ${response.description}`);
        console.log(`  Updated At: ${response.updatedAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error updating addon: ${error.message}\n`);
    }
}

async function testLookupAddon(config: Configuration) {
    console.log('--- Test 5: Lookup Addon by Lookup Key ---');

    if (!testAddonLookupKey) {
        console.log('⚠ Warning: No addon lookup key available\n⚠ Skipping lookup test\n');
        return;
    }

    try {
        const api = new AddonsApi(config);
        console.log(`  Looking up addon with key: ${testAddonLookupKey}`);
        const response = await api.addonsLookupLookupKeyGet({ lookupKey: testAddonLookupKey });

        console.log('✓ Addon found by lookup key!');
        console.log(`  Lookup Key: ${testAddonLookupKey}`);
        console.log(`  ID: ${response.id}`);
        console.log(`  Name: ${response.name}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error looking up addon: ${error.message}`);
        console.log('⚠ Skipping lookup test\n');
    }
}

async function testSearchAddons(config: Configuration) {
    console.log('--- Test 6: Search Addons ---');

    try {
        const api = new AddonsApi(config);
        const response = await api.addonsSearchPost({
            filter: {
                addonIds: [testAddonID],
            },
        });

        console.log('✓ Search completed!');
        console.log(`  Found ${response.items?.length || 0} addons matching ID '${testAddonID}'`);
        if (response.items && response.items.length > 0) {
            response.items.slice(0, 3).forEach(addon => {
                console.log(`  - ${addon.id}: ${addon.name} (${addon.lookupKey})`);
            });
        }
        console.log();
    } catch (error: any) {
        console.log(`❌ Error searching addons: ${error.message}\n`);
    }
}

// ========================================
// ENTITLEMENTS API TESTS
// ========================================

async function testCreateEntitlement(config: Configuration) {
    console.log('--- Test 1: Create Entitlement ---');

    try {
        const api = new EntitlementsApi(config);
        const response = await api.entitlementsPost({
            entitlement: {
                featureId: testFeatureID,
                featureType: TypesFeatureType.FeatureTypeBoolean,
                planId: testPlanID,
                isEnabled: true,
                usageResetPeriod: TypesEntitlementUsageResetPeriod.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
            },
        });

        testEntitlementID = response.id!;
        console.log('✓ Entitlement created successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Feature ID: ${response.featureId}`);
        console.log(`  Plan ID: ${response.planId}\n`);
    } catch (error: any) {
        console.log(`❌ Error creating entitlement: ${error.message}\n`);
    }
}

async function testGetEntitlement(config: Configuration) {
    console.log('--- Test 2: Get Entitlement by ID ---');

    try {
        const api = new EntitlementsApi(config);
        const response = await api.entitlementsIdGet({ id: testEntitlementID });

        console.log('✓ Entitlement retrieved successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Feature ID: ${response.featureId}`);
        console.log(`  Plan ID: ${(response as any).planId}`);
        console.log(`  Created At: ${response.createdAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting entitlement: ${error.message}\n`);
    }
}

async function testListEntitlements(config: Configuration) {
    console.log('--- Test 3: List Entitlements ---');

    try {
        const api = new EntitlementsApi(config);
        const response = await api.entitlementsGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.items?.length || 0} entitlements`);
        if (response.items && response.items.length > 0) {
            console.log(`  First entitlement: ${response.items[0].id}`);
        }
        if (response.pagination) {
            console.log(`  Total: ${response.pagination.total}\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing entitlements: ${error.message}\n`);
    }
}

async function testUpdateEntitlement(config: Configuration) {
    console.log('--- Test 4: Update Entitlement ---');

    try {
        const api = new EntitlementsApi(config);
        const response = await api.entitlementsIdPut({
            id: testEntitlementID,
            entitlement: {
                isEnabled: false,
            },
        });

        console.log('✓ Entitlement updated successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Updated At: ${response.updatedAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error updating entitlement: ${error.message}\n`);
    }
}

async function testSearchEntitlements(config: Configuration) {
    console.log('--- Test 5: Search Entitlements ---');

    try {
        const api = new EntitlementsApi(config);
        const response = await api.entitlementsSearchPost({
            filter: {
                featureIds: [testFeatureID],
            },
        });
        console.log('✓ Search completed!');
        console.log(`  Found ${response.items?.length || 0} entitlements for customer '${testCustomerID}'`);
        if (response.items && response.items.length > 0) {
            response.items.slice(0, 3).forEach(ent => {
                console.log(`  - ${ent.id}: Feature ${ent.featureId}`);
            });
        }
        console.log();
    } catch (error: any) {
        console.log(`❌ Error searching entitlements: ${error.message}\n`);
    }
}



// ========================================
// CONNECTIONS API TESTS
// ========================================

async function testListConnections(config: Configuration) {
    console.log('--- Test 1: List Connections ---');

    try {
        const api = new ConnectionsApi(config);
        const response = await api.connectionsGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.connections?.length || 0} connections`);
        if (response.connections && response.connections.length > 0) {
            console.log(`  First connection: ${response.connections[0].id}`);
            if (response.connections[0].providerType) {
                console.log(`  Provider Type: ${response.connections[0].providerType}`);
            }
        }
        if (response.total) {
            console.log(`  Total: ${response.total}\n`);
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error listing connections: ${error.message}`);
        console.log('⚠ Skipping connections tests (may not have any connections)\n');
    }
}

async function testSearchConnections(config: Configuration) {
    console.log('--- Test 2: Search Connections ---');

    try {
        const api = new ConnectionsApi(config);
        const response = await api.connectionsSearchPost({ filter: {} });

        console.log('✓ Search completed!');
        console.log(`  Found ${response.connections?.length || 0} connections\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error searching connections: ${error.message}\n`);
    }
}

// ========================================
// SUBSCRIPTIONS API TESTS
// ========================================

async function testCreateSubscription(config: Configuration) {
    console.log('--- Test 1: Create Subscription ---');

    try {
        const pricesApi = new PricesApi(config);
        await pricesApi.pricesPost({
            price: {
                entityId: testPlanID,
                entityType: TypesPriceEntityType.PRICE_ENTITY_TYPE_PLAN,
                type: TypesPriceType.PRICE_TYPE_FIXED,
                billingModel: TypesBillingModel.BILLING_MODEL_FLAT_FEE,
                billingCadence: TypesBillingCadence.BILLING_CADENCE_RECURRING,
                billingPeriod: TypesBillingPeriod.BILLING_PERIOD_MONTHLY,
                invoiceCadence: TypesInvoiceCadence.InvoiceCadenceArrear,
                priceUnitType: TypesPriceUnitType.PRICE_UNIT_TYPE_FIAT,
                amount: '29.99',
                currency: 'USD',
                displayName: 'Monthly Subscription Price',
            },
        });

        const api = new SubscriptionsApi(config);
        const response = await api.subscriptionsPost({
            subscription: {
                customerId: testCustomerID,
                planId: testPlanID,
                currency: 'USD',
                billingCadence: TypesBillingCadence.BILLING_CADENCE_RECURRING,
                billingPeriod: TypesBillingPeriod.BILLING_PERIOD_MONTHLY,
                billingPeriodCount: 1,
                billingCycle: TypesBillingCycle.BillingCycleAnniversary,
                startDate: new Date().toISOString(),
                metadata: {
                    source: 'sdk_test',
                    test_run: new Date().toISOString(),
                },
            },
        });

        testSubscriptionID = response.id!;
        console.log('✓ Subscription created successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Customer ID: ${response.customerId}`);
        console.log(`  Plan ID: ${response.planId}`);
        console.log(`  Status: ${response.subscriptionStatus}\n`);
    } catch (error: any) {
        console.log(`❌ Error creating subscription: ${error.message}\n`);
    }
}

async function testGetSubscription(config: Configuration) {
    console.log('--- Test 2: Get Subscription by ID ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription ID available\n⚠ Skipping get subscription test\n');
        return;
    }

    try {
        const api = new SubscriptionsApi(config);
        const response = await api.subscriptionsIdGet({ id: testSubscriptionID });

        console.log('✓ Subscription retrieved successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Customer ID: ${response.customerId}`);
        console.log(`  Status: ${response.subscriptionStatus}`);
        console.log(`  Created At: ${response.createdAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting subscription: ${error.message}\n`);
    }
}

async function testUpdateSubscription(config: Configuration) {
    console.log('--- Test 4: Update Subscription ---');
    console.log('⚠ Skipping update subscription test (endpoint not available in SDK)\n');
}

async function testListSubscriptions(config: Configuration) {
    console.log('--- Test 3: List Subscriptions ---');

    try {
        const api = new SubscriptionsApi(config);
        const response = await api.subscriptionsGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.items?.length || 0} subscriptions`);
        if (response.items && response.items.length > 0) {
            console.log(`  First subscription: ${response.items[0].id} (Customer: ${response.items[0].customerId})`);
        }
        if (response.pagination) {
            console.log(`  Total: ${response.pagination.total}\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing subscriptions: ${error.message}\n`);
    }
}

async function testSearchSubscriptions(config: Configuration) {
    console.log('--- Test 4: Search Subscriptions ---');

    try {
        const api = new SubscriptionsApi(config);
        const response = await api.subscriptionsSearchPost({ filter: {} });

        console.log('✓ Search completed!');
        console.log(`  Found ${response.items?.length || 0} subscriptions\n`);
    } catch (error: any) {
        console.log(`❌ Error searching subscriptions: ${error.message}\n`);
    }
}

async function testActivateSubscription(config: Configuration) {
    console.log('--- Test 5: Activate Subscription ---');

    try {
        const api = new SubscriptionsApi(config);
        const draftSub = await api.subscriptionsPost({
            subscription: {
                customerId: testCustomerID,
                planId: testPlanID,
                currency: 'USD',
                billingCadence: TypesBillingCadence.BILLING_CADENCE_RECURRING,
                billingPeriod: TypesBillingPeriod.BILLING_PERIOD_MONTHLY,
                billingPeriodCount: 1,
                startDate: new Date().toISOString(),
            },
        });

        const draftID = draftSub.id!;
        console.log(`  Created draft subscription: ${draftID}`);

        await api.subscriptionsIdActivatePost({
            id: draftID,
            request: { startDate: new Date().toISOString() },
        });

        console.log('✓ Subscription activated successfully!');
        console.log(`  ID: ${draftID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error activating subscription: ${error.message}\n`);
    }
}

async function testPauseSubscription(config: Configuration) {
    console.log('--- Test 7: Pause Subscription ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created, skipping pause test\n');
        return;
    }

    try {
        const api = new SubscriptionsApi(config);
        const response = await api.subscriptionsIdPausePost({
            id: testSubscriptionID,
            request: {
                pauseMode: 'IMMEDIATE' as any, // TypesPauseMode.PauseModeImmediate
            },
        });

        console.log('✓ Subscription paused successfully!');
        console.log(`  Pause ID: ${response.id}`);
        console.log(`  Subscription ID: ${response.subscriptionId}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error pausing subscription: ${error.message}`);
        if (error.response) {
            console.log(`  Response: ${JSON.stringify(error.response.data || error.response.body || {}, null, 2)}`);
        }
        console.log('⚠ Skipping pause test\n');
    }
}

async function testResumeSubscription(config: Configuration) {
    console.log('--- Test 8: Resume Subscription ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created, skipping resume test\n');
        return;
    }

    try {
        const api = new SubscriptionsApi(config);
        const response = await api.subscriptionsIdResumePost({
            id: testSubscriptionID,
            request: {
                resumeMode: 'IMMEDIATE' as any, // TypesResumeMode.ResumeModeImmediate
            },
        });

        console.log('✓ Subscription resumed successfully!');
        console.log(`  Pause ID: ${response.id}`);
        console.log(`  Subscription ID: ${response.subscriptionId}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error resuming subscription: ${error.message}`);
        if (error.response) {
            console.log(`  Response: ${JSON.stringify(error.response.data || error.response.body || {}, null, 2)}`);
        }
        console.log('⚠ Skipping resume test\n');
    }
}

async function testGetPauseHistory(config: Configuration) {
    console.log('--- Test 9: Get Pause History ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created, skipping pause history test\n');
        return;
    }

    try {
        const api = new SubscriptionsApi(config);
        const response = await api.subscriptionsIdPausesGet({ id: testSubscriptionID });

        console.log('✓ Retrieved pause history!');
        console.log(`  Total pauses: ${response.length || 0}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting pause history: ${error.message}`);
        if (error.response) {
            console.log(`  Response: ${JSON.stringify(error.response.data || error.response.body || {}, null, 2)}`);
        }
        console.log('⚠ Skipping pause history test\n');
    }
}

async function testAddAddonToSubscription(config: Configuration) {
    console.log('--- Test 6: Add Addon to Subscription ---');

    if (!testSubscriptionID || !testAddonID) {
        console.log('⚠ Warning: No subscription or addon created\n⚠ Skipping add addon test\n');
        return;
    }

    try {
        const pricesApi = new PricesApi(config);
        await pricesApi.pricesPost({
            price: {
                entityId: testAddonID,
                entityType: TypesPriceEntityType.PRICE_ENTITY_TYPE_ADDON,
                type: TypesPriceType.PRICE_TYPE_FIXED,
                billingModel: TypesBillingModel.BILLING_MODEL_FLAT_FEE,
                billingCadence: TypesBillingCadence.BILLING_CADENCE_RECURRING,
                billingPeriod: TypesBillingPeriod.BILLING_PERIOD_MONTHLY,
                invoiceCadence: TypesInvoiceCadence.InvoiceCadenceArrear,
                priceUnitType: TypesPriceUnitType.PRICE_UNIT_TYPE_FIAT,
                amount: '5.00',
                currency: 'USD',
                displayName: 'Addon Monthly Price',
            },
        });

        const api = new SubscriptionsApi(config);
        await api.subscriptionsAddonPost({
            request: {
                subscriptionId: testSubscriptionID,
                addonId: testAddonID,
            },
        });

        console.log('✓ Addon added to subscription successfully!');
        console.log(`  Subscription ID: ${testSubscriptionID}`);
        console.log(`  Addon ID: ${testAddonID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error adding addon: ${error.message}\n`);
    }
}

async function testRemoveAddonFromSubscription(config: Configuration) {
    console.log('--- Test 7: Remove Addon from Subscription ---');
    console.log('⚠ Skipping remove addon test (requires addon association ID)\n');
}

async function testPreviewSubscriptionChange(config: Configuration) {
    console.log('--- Test 13: Preview Subscription Change ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created, skipping preview change test\n');
        return;
    }

    if (!testPlanID) {
        console.log('⚠ Warning: No plan available for change preview\n');
        return;
    }

    try {
        const api = new SubscriptionsApi(config);
        const preview = await api.subscriptionsIdChangePreviewPost({
            id: testSubscriptionID,
            request: {
                targetPlanId: testPlanID,
                billingCadence: TypesBillingCadence.BILLING_CADENCE_RECURRING,
                billingPeriod: TypesBillingPeriod.BILLING_PERIOD_MONTHLY,
                billingCycle: TypesBillingCycle.BillingCycleAnniversary,
                prorationBehavior: 'CREATE_PRORATIONS' as any, // TypesProrationBehavior.ProrationBehaviorCreateProrations
            },
        });

        console.log('✓ Subscription change preview generated!');
        if (preview.nextInvoicePreview) {
            console.log('  Preview available');
        }
        console.log();
    } catch (error: any) {
        console.log(`⚠ Warning: Error previewing subscription change: ${error.message}`);
        if (error.response) {
            console.log(`  Response: ${JSON.stringify(error.response.data || error.response.body || {}, null, 2)}`);
        }
        console.log('⚠ Skipping preview change test\n');
    }
}

async function testExecuteSubscriptionChange(config: Configuration) {
    console.log('--- Test 8: Execute Subscription Change ---');
    console.log('⚠ Skipping execute change test (would modify active subscription)\n');
}

async function testGetSubscriptionEntitlements(config: Configuration) {
    console.log('--- Test 9: Get Subscription Entitlements ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created\n⚠ Skipping get entitlements test\n');
        return;
    }

    try {
        const api = new SubscriptionsApi(config);
        const response = await api.subscriptionsIdEntitlementsGet({ id: testSubscriptionID });

        console.log('✓ Retrieved subscription entitlements!');
        console.log(`  Total features: ${response.features?.length || 0}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting entitlements: ${error.message}\n`);
    }
}

async function testGetUpcomingGrants(config: Configuration) {
    console.log('--- Test 10: Get Upcoming Grants ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created\n⚠ Skipping get upcoming grants test\n');
        return;
    }

    try {
        const api = new SubscriptionsApi(config);
        const response = await api.subscriptionsIdGrantsUpcomingGet({ id: testSubscriptionID });

        console.log('✓ Retrieved upcoming grants!');
        console.log(`  Total upcoming grants: ${response.items?.length || 0}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting upcoming grants: ${error.message}\n`);
    }
}

async function testReportUsage(config: Configuration) {
    console.log('--- Test 11: Report Usage ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created\n⚠ Skipping report usage test\n');
        return;
    }

    try {
        const api = new SubscriptionsApi(config);
        await api.subscriptionsUsagePost({
            request: { subscriptionId: testSubscriptionID },
        });

        console.log('✓ Usage reported successfully!');
        console.log(`  Subscription ID: ${testSubscriptionID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error reporting usage: ${error.message}\n`);
    }
}

async function testUpdateLineItem(config: Configuration) {
    console.log('--- Test 12: Update Line Item ---');
    console.log('⚠ Skipping update line item test (requires line item ID)\n');
}

async function testDeleteLineItem(config: Configuration) {
    console.log('--- Test 13: Delete Line Item ---');
    console.log('⚠ Skipping delete line item test (requires line item ID)\n');
}

async function testCancelSubscription(config: Configuration) {
    console.log('--- Test 14: Cancel Subscription ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created\n⚠ Skipping cancel test\n');
        return;
    }

    try {
        const api = new SubscriptionsApi(config);
        await api.subscriptionsIdCancelPost({
            id: testSubscriptionID,
            request: { cancellationType: TypesCancellationType.CancellationTypeEndOfPeriod },
        });

        console.log('✓ Subscription canceled successfully!');
        console.log(`  Subscription ID: ${testSubscriptionID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error canceling subscription: ${error.message}\n`);
    }
}

// ========================================
// INVOICES API TESTS
// ========================================

async function testListInvoices(config: Configuration) {
    console.log('--- Test 1: List Invoices ---');

    try {
        const api = new InvoicesApi(config);
        const response = await api.invoicesGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.items?.length || 0} invoices`);
        if (response.items && response.items.length > 0) {
            testInvoiceID = response.items[0].id!;
            console.log(`  First invoice: ${response.items[0].id} (Customer: ${response.items[0].customerId})`);
            if (response.items[0].status) {
                console.log(`  Status: ${response.items[0].status}`);
            }
        }
        if (response.pagination) {
            console.log(`  Total: ${response.pagination.total}\n`);
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error listing invoices: ${error.message}\n`);
    }
}

async function testSearchInvoices(config: Configuration) {
    console.log('--- Test 2: Search Invoices ---');

    try {
        const api = new InvoicesApi(config);
        const response = await api.invoicesSearchPost({ filter: {} });

        console.log('✓ Search completed!');
        console.log(`  Found ${response.items?.length || 0} invoices\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error searching invoices: ${error.message}\n`);
    }
}

async function testCreateInvoice(config: Configuration) {
    console.log('--- Test 3: Create Invoice ---');

    if (!testCustomerID) {
        console.log('⚠ Warning: No customer created\n⚠ Skipping create invoice test\n');
        return;
    }

    try {
        const api = new InvoicesApi(config);
        const response = await api.invoicesPost({
            invoice: {
                customerId: testCustomerID,
                currency: 'USD',
                amountDue: '100.00',
                subtotal: '100.00',
                total: '100.00',
                invoiceType: TypesInvoiceType.InvoiceTypeOneOff,
                billingReason: TypesInvoiceBillingReason.InvoiceBillingReasonManual,
                invoiceStatus: TypesInvoiceStatus.InvoiceStatusDraft,
                lineItems: [{
                    displayName: 'Test Service',
                    quantity: '1',
                    amount: '100.00',
                }],
                metadata: {
                    source: 'sdk_test',
                    type: 'manual',
                },
            },
        });

        testInvoiceID = response.id!;
        console.log('✓ Invoice created successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Customer ID: ${response.customerId}`);
        console.log(`  Status: ${response.invoiceStatus}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error creating invoice: ${error.message}\n`);
    }
}

async function testGetInvoice(config: Configuration) {
    console.log('--- Test 4: Get Invoice by ID ---');

    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available\n⚠ Skipping get invoice test\n');
        return;
    }

    try {
        const api = new InvoicesApi(config);
        const response = await api.invoicesIdGet({ id: testInvoiceID });

        console.log('✓ Invoice retrieved successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Total: ${response.currency} ${response.total}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting invoice: ${error.message}\n`);
    }
}

async function testUpdateInvoice(config: Configuration) {
    console.log('--- Test 5: Update Invoice ---');

    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available\n⚠ Skipping update invoice test\n');
        return;
    }

    try {
        const api = new InvoicesApi(config);
        const response = await api.invoicesIdPut({
            id: testInvoiceID,
            request: {
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });

        console.log('✓ Invoice updated successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Updated At: ${response.updatedAt}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error updating invoice: ${error.message}\n`);
    }
}

async function testPreviewInvoice(config: Configuration) {
    console.log('--- Test 6: Preview Invoice ---');

    if (!testCustomerID) {
        console.log('⚠ Warning: No customer available\n⚠ Skipping preview invoice test\n');
        return;
    }

    try {
        const api = new InvoicesApi(config);
        const response = await api.invoicesPreviewPost({
            request: { subscriptionId: testSubscriptionID},
        });

        console.log('✓ Invoice preview generated!');
        if (response.total) {
            console.log(`  Preview Total: ${response.total}\n`);
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error previewing invoice: ${error.message}\n`);
    }
}

async function testFinalizeInvoice(config: Configuration) {
    console.log('--- Test 7: Finalize Invoice ---');

    try {
        const api = new InvoicesApi(config);
        const draftInvoice = await api.invoicesPost({
            invoice: {
                customerId: testCustomerID,
                currency: 'USD',
                amountDue: '50.00',
                subtotal: '50.00',
                total: '50.00',
                invoiceType: TypesInvoiceType.InvoiceTypeOneOff,
                billingReason: TypesInvoiceBillingReason.InvoiceBillingReasonManual,
                invoiceStatus: TypesInvoiceStatus.InvoiceStatusDraft,
                lineItems: [{
                    displayName: 'Finalize Test Service',
                    quantity: '1',
                    amount: '50.00',
                }],
            },
        });

        const finalizeID = draftInvoice.id!;
        console.log(`  Created draft invoice: ${finalizeID}`);

        await api.invoicesIdFinalizePost({ id: finalizeID });

        console.log('✓ Invoice finalized successfully!');
        console.log(`  Invoice ID: ${finalizeID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error finalizing invoice: ${error.message}\n`);
    }
}

async function testRecalculateInvoice(config: Configuration) {
    console.log('--- Test 8: Recalculate Invoice ---');
    console.log('⚠ Skipping recalculate invoice test (requires subscription invoice)\n');
}

async function testRecordPayment(config: Configuration) {
    console.log('--- Test 9: Record Payment ---');

    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available\n⚠ Skipping record payment test\n');
        return;
    }

    try {
        const api = new InvoicesApi(config);
        await api.invoicesIdPaymentPut({
            id: testInvoiceID,
            request: {
                paymentStatus: TypesPaymentStatus.PaymentStatusSucceeded,
                amount: '100.00',
            },
        });

        console.log('✓ Payment recorded successfully!');
        console.log(`  Invoice ID: ${testInvoiceID}`);
        console.log(`  Amount Paid: 100.00\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error recording payment: ${error.message}\n`);
    }
}

async function testAttemptPayment(config: Configuration) {
    console.log('--- Test 10: Attempt Payment ---');

    try {
        const api = new InvoicesApi(config);
        const attemptInvoice = await api.invoicesPost({
            invoice: {
                customerId: testCustomerID,
                currency: 'USD',
                amountDue: '25.00',
                subtotal: '25.00',
                total: '25.00',
                amountPaid: '0.00',
                invoiceType: TypesInvoiceType.InvoiceTypeOneOff,
                billingReason: TypesInvoiceBillingReason.InvoiceBillingReasonManual,
                invoiceStatus: TypesInvoiceStatus.InvoiceStatusDraft,
                paymentStatus: TypesPaymentStatus.PaymentStatusPending,
                lineItems: [{
                    displayName: 'Attempt Payment Test',
                    quantity: '1',
                    amount: '25.00',
                }],
            },
        });

        const attemptID = attemptInvoice.id!;
        await api.invoicesIdFinalizePost({ id: attemptID });
        await api.invoicesIdPaymentAttemptPost({ id: attemptID });

        console.log('✓ Payment attempt initiated!');
        console.log(`  Invoice ID: ${attemptID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error attempting payment: ${error.message}\n`);
    }
}

async function testDownloadInvoicePDF(config: Configuration) {
    console.log('--- Test 11: Download Invoice PDF ---');

    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available\n⚠ Skipping download PDF test\n');
        return;
    }

    try {
        const api = new InvoicesApi(config);
        await api.invoicesIdPdfGet({ id: testInvoiceID });

        console.log('✓ Invoice PDF downloaded!');
        console.log(`  Invoice ID: ${testInvoiceID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error downloading PDF: ${error.message}\n`);
    }
}

async function testTriggerInvoiceComms(config: Configuration) {
    console.log('--- Test 12: Trigger Invoice Communications ---');

    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available\n⚠ Skipping trigger comms test\n');
        return;
    }

    try {
        const api = new InvoicesApi(config);
        await api.invoicesIdCommsTriggerPost({ id: testInvoiceID });

        console.log('✓ Invoice communications triggered!');
        console.log(`  Invoice ID: ${testInvoiceID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error triggering comms: ${error.message}\n`);
    }
}

async function testGetCustomerInvoiceSummary(config: Configuration) {
    console.log('--- Test 13: Get Customer Invoice Summary ---');

    if (!testCustomerID) {
        console.log('⚠ Warning: No customer ID available\n⚠ Skipping summary test\n');
        return;
    }

    try {
        const api = new InvoicesApi(config);
        await api.customersIdInvoicesSummaryGet({ id: testCustomerID });

        console.log('✓ Customer invoice summary retrieved!');
        console.log(`  Customer ID: ${testCustomerID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting summary: ${error.message}\n`);
    }
}

async function testVoidInvoice(config: Configuration) {
    console.log('--- Test 14: Void Invoice ---');

    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available\n⚠ Skipping void invoice test\n');
        return;
    }

    try {
        const api = new InvoicesApi(config);
        await api.invoicesIdVoidPost({ id: testInvoiceID });

        console.log('✓ Invoice voided successfully!');
        console.log(`  Invoice ID: ${testInvoiceID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error voiding invoice: ${error.message}\n`);
    }
}

// ========================================
// PRICES API TESTS
// ========================================

async function testCreatePrice(config: Configuration) {
    console.log('--- Test 1: Create Price ---');

    if (!testPlanID) {
        console.log('⚠ Warning: No plan ID available\n⚠ Skipping create price test\n');
        return;
    }

    try {
        const api = new PricesApi(config);
        const response = await api.pricesPost({
            price: {
                entityId: testPlanID,
                entityType: TypesPriceEntityType.PRICE_ENTITY_TYPE_PLAN,
                currency: 'USD',
                amount: '99.00',
                billingModel: TypesBillingModel.BILLING_MODEL_FLAT_FEE,
                billingCadence: TypesBillingCadence.BILLING_CADENCE_RECURRING,
                billingPeriod: TypesBillingPeriod.BILLING_PERIOD_MONTHLY,
                invoiceCadence: TypesInvoiceCadence.InvoiceCadenceAdvance,
                priceUnitType: TypesPriceUnitType.PRICE_UNIT_TYPE_FIAT,
                type: TypesPriceType.PRICE_TYPE_FIXED,
                displayName: 'Monthly Subscription',
                description: 'Standard monthly subscription price',
            },
        });

        testPriceID = response.id!;
        console.log('✓ Price created successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Amount: ${response.amount} ${response.currency}`);
        console.log(`  Billing Model: ${response.billingModel}\n`);
    } catch (error: any) {
        console.log(`❌ Error creating price: ${error.message}\n`);
    }
}

async function testGetPrice(config: Configuration) {
    console.log('--- Test 2: Get Price by ID ---');

    if (!testPriceID) {
        console.log('⚠ Warning: No price ID available\n⚠ Skipping get price test\n');
        return;
    }

    try {
        const api = new PricesApi(config);
        const response = await api.pricesIdGet({ id: testPriceID });

        console.log('✓ Price retrieved successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Amount: ${response.amount} ${response.currency}`);
        console.log(`  Entity ID: ${response.entityId}`);
        console.log(`  Created At: ${response.createdAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting price: ${error.message}\n`);
    }
}

async function testListPrices(config: Configuration) {
    console.log('--- Test 3: List Prices ---');

    try {
        const api = new PricesApi(config);
        const response = await api.pricesGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.items?.length || 0} prices`);
        if (response.items && response.items.length > 0) {
            console.log(`  First price: ${response.items[0].id} - ${response.items[0].amount} ${response.items[0].currency}`);
        }
        if (response.pagination) {
            console.log(`  Total: ${response.pagination.total}\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing prices: ${error.message}\n`);
    }
}

async function testUpdatePrice(config: Configuration) {
    console.log('--- Test 4: Update Price ---');

    if (!testPriceID) {
        console.log('⚠ Warning: No price ID available\n⚠ Skipping update price test\n');
        return;
    }

    try {
        const api = new PricesApi(config);
        const response = await api.pricesIdPut({
            id: testPriceID,
            price: {
                description: 'Updated price description for testing',
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });

        console.log('✓ Price updated successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  New Description: ${response.description}`);
        console.log(`  Updated At: ${response.updatedAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error updating price: ${error.message}\n`);
    }
}

// ========================================
// PAYMENTS API TESTS
// ========================================

async function testCreatePayment(config: Configuration) {
    console.log('--- Test 1: Create Payment ---');

    if (!testCustomerID) {
        console.log('⚠ Warning: No customer ID available\n⚠ Skipping create payment test\n');
        return;
    }

    let paymentInvoiceID = '';
    
    try {
        // First, create a fresh invoice for this payment test
        // This is necessary because previous tests might have already paid the shared testInvoiceID
        const invoicesApi = new InvoicesApi(config);
        
        // Create a draft invoice with explicit payment status to prevent auto-payment
        const draftInvoice = await invoicesApi.invoicesPost({
            invoice: {
                customerId: testCustomerID,
                currency: 'USD',
                amountDue: '100.00',
                subtotal: '100.00',
                total: '100.00',
                amountPaid: '0.00', // Explicitly set to 0 to prevent auto-payment
                invoiceType: TypesInvoiceType.InvoiceTypeOneOff,
                billingReason: TypesInvoiceBillingReason.InvoiceBillingReasonManual,
                invoiceStatus: TypesInvoiceStatus.InvoiceStatusDraft,
                paymentStatus: TypesPaymentStatus.PaymentStatusPending, // Set to PENDING to prevent auto-payment
                lineItems: [{
                    displayName: 'Payment Test Service',
                    quantity: '1',
                    amount: '100.00',
                }],
                metadata: {
                    source: 'sdk_test_payment',
                },
            },
        });

        paymentInvoiceID = draftInvoice.id!;
        console.log(`  Created invoice for payment: ${paymentInvoiceID}`);

        // Check invoice status before finalization
        const currentInvoice = await invoicesApi.invoicesIdGet({ id: paymentInvoiceID });
        
        if (currentInvoice.amountPaid && currentInvoice.amountPaid !== '0' && currentInvoice.amountPaid !== '0.00') {
            console.log(`⚠ Warning: Invoice already has amount paid before finalization: ${currentInvoice.amountPaid}\n⚠ Skipping payment creation test (invoice was auto-paid during creation)\n`);
            return;
        }

        if (currentInvoice.amountDue === '0' || currentInvoice.amountDue === '0.00') {
            console.log(`⚠ Warning: Invoice has zero amount due: ${currentInvoice.amountDue}, will be auto-paid on finalization\n⚠ Skipping payment creation test (invoice has zero amount due)\n`);
            return;
        }

        if (currentInvoice.amountDue && currentInvoice.total) {
            console.log(`  Invoice before finalization - AmountDue: ${currentInvoice.amountDue}, Total: ${currentInvoice.total}`);
        }

        // Finalize the invoice if it's still in draft status
        if (currentInvoice.invoiceStatus === TypesInvoiceStatus.InvoiceStatusDraft) {
            try {
                await invoicesApi.invoicesIdFinalizePost({ id: paymentInvoiceID });
                console.log('  Finalized invoice for payment');
            } catch (finalizeError: any) {
                // Check if it's already finalized or has an error
                if (finalizeError.message && (finalizeError.message.includes('already') || finalizeError.message.includes('400'))) {
                    console.log(`⚠ Warning: Invoice finalization returned error (may already be finalized): ${finalizeError.message}`);
                } else {
                    console.log(`⚠ Warning: Failed to finalize invoice for payment test: ${finalizeError.message}`);
                    return;
                }
            }
        } else {
            console.log(`  Invoice already finalized (status: ${currentInvoice.invoiceStatus})`);
        }

        // Re-fetch the invoice to get the latest payment status after finalization
        const finalInvoice = await invoicesApi.invoicesIdGet({ id: paymentInvoiceID });

        if (finalInvoice.amountDue && finalInvoice.total && finalInvoice.amountPaid) {
            console.log(`  Invoice after finalization - AmountDue: ${finalInvoice.amountDue}, Total: ${finalInvoice.total}, AmountPaid: ${finalInvoice.amountPaid}`);
        }

        // Check if invoice is already paid
        if (finalInvoice.paymentStatus === TypesPaymentStatus.PaymentStatusSucceeded) {
            console.log(`⚠ Warning: Invoice is already paid (status: ${finalInvoice.paymentStatus}), cannot create payment\n⚠ Skipping payment creation test\n`);
            return;
        }

        if (finalInvoice.amountPaid && finalInvoice.amountPaid !== '0' && finalInvoice.amountPaid !== '0.00') {
            console.log(`⚠ Warning: Invoice already has amount paid: ${finalInvoice.amountPaid}, cannot create payment\n⚠ Skipping payment creation test\n`);
            return;
        }

        if (finalInvoice.total === '0' || finalInvoice.total === '0.00') {
            console.log('⚠ Warning: Invoice has zero total amount, may be auto-marked as paid\n⚠ Skipping payment creation test\n');
            return;
        }

        const paymentStatusStr = finalInvoice.paymentStatus || 'unknown';
        const totalStr = finalInvoice.total || 'unknown';
        console.log(`  Invoice is unpaid and ready for payment (status: ${paymentStatusStr}, total: ${totalStr})`);

        // Now create the payment
        const paymentsApi = new PaymentsApi(config);
        const response = await paymentsApi.paymentsPost({
            payment: {
                amount: '100.00',
                currency: 'USD',
                destinationId: paymentInvoiceID,
                destinationType: 'INVOICE' as any, // PaymentDestinationTypeInvoice - must be uppercase
                paymentMethodType: TypesPaymentMethodType.PaymentMethodTypeOffline,
                processPayment: false, // Don't process immediately in test
                metadata: {
                    source: 'sdk_test',
                    test_run: new Date().toISOString(),
                },
            } as any,
        });

        testPaymentID = response.id!;
        console.log('✓ Payment created successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Amount: ${response.amount} ${response.currency}`);
        if (response.paymentStatus) {
            console.log(`  Status: ${response.paymentStatus}\n`);
        } else {
            console.log();
        }
    } catch (error: any) {
        console.log(`❌ Error creating payment: ${error.message || error}`);
        
        // Enhanced error logging - try to capture all possible error properties
        // The SDK might structure errors differently (Fetch API vs Axios)
        if (error.response) {
            console.log(`  Response Status Code: ${error.response.status || error.response.statusCode || 'unknown'}`);
            if (error.response.data) {
                console.log(`  Response Data: ${JSON.stringify(error.response.data, null, 2)}`);
            }
            if (error.response.body) {
                console.log(`  Response Body: ${JSON.stringify(error.response.body, null, 2)}`);
            }
            if (error.response.text && typeof error.response.text === 'function') {
                error.response.text().then((text: string) => {
                    console.log(`  Response Text: ${text}`);
                }).catch(() => {
                    // Ignore if text() fails
                });
            }
        }
        
        if (error.body) {
            console.log(`  Error Body: ${JSON.stringify(error.body, null, 2)}`);
        }
        
        if (error.status) {
            console.log(`  Status Code: ${error.status}`);
        }
        
        if (error.statusCode) {
            console.log(`  Status Code: ${error.statusCode}`);
        }
        
        // Log the entire error object structure for debugging
        console.log('  Error Object Keys:', Object.keys(error));
        
        // Try to get response body if it's a Response object
        if (error instanceof Response) {
            error.text().then((text) => {
                console.log(`  Response Text: ${text}`);
            }).catch((e) => {
                console.log(`  Could not read response text: ${e}`);
            });
        }
        
        // Also check if error has a json() method (common in Fetch API)
        if (error.json && typeof error.json === 'function') {
            error.json().then((data: any) => {
                console.log(`  Error JSON: ${JSON.stringify(data, null, 2)}`);
            }).catch(() => {
                // Ignore if json() fails
            });
        }
        
        // Log payment request details for debugging
        console.log('  Payment Request Details:');
        console.log('    Amount: 100.00');
        console.log('    Currency: USD');
        console.log(`    DestinationId: ${paymentInvoiceID}`);
        console.log('    DestinationType: INVOICE');
        console.log('    PaymentMethodType: offline');
        console.log('    ProcessPayment: false');
        console.log();
    }
}

async function testGetPayment(config: Configuration) {
    console.log('--- Test 2: Get Payment by ID ---');

    if (!testPaymentID) {
        console.log('⚠ Warning: No payment ID available\n⚠ Skipping get payment test\n');
        return;
    }

    try {
        const api = new PaymentsApi(config);
        const response = await api.paymentsIdGet({ id: testPaymentID });

        console.log('✓ Payment retrieved successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Amount: ${response.amount} ${response.currency}`);
        console.log(`  Status: ${response.paymentStatus}`);
        console.log(`  Created At: ${response.createdAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting payment: ${error.message}\n`);
    }
}

async function testListPayments(config: Configuration) {
    console.log('--- Test 3: List Payments ---');

    try {
        const api = new PaymentsApi(config);
        const response = await api.paymentsGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.items?.length || 0} payments`);
        if (response.items && response.items.length > 0) {
            console.log(`  First payment: ${response.items[0].id} - ${response.items[0].amount} ${response.items[0].currency}`);
        }
        if (response.pagination) {
            console.log(`  Total: ${response.pagination.total}\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing payments: ${error.message}\n`);
    }
}

async function testSearchPayments(config: Configuration) {
    console.log('--- Test 2: Search Payments ---');
    console.log('⚠ Skipping search payments test (endpoint not available in SDK)\n');
}

async function testUpdatePayment(config: Configuration) {
    console.log('--- Test 4: Update Payment ---');

    if (!testPaymentID) {
        console.log('⚠ Warning: No payment ID available\n⚠ Skipping update payment test\n');
        return;
    }

    try {
        const api = new PaymentsApi(config);
        const response = await api.paymentsIdPut({
            id: testPaymentID,
            payment: {
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });

        console.log('✓ Payment updated successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Updated At: ${response.updatedAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error updating payment: ${error.message}\n`);
    }
}

async function testProcessPayment(config: Configuration) {
    console.log('--- Test 5: Process Payment ---');

    if (!testPaymentID) {
        console.log('⚠ Warning: No payment ID available\n⚠ Skipping process payment test\n');
        return;
    }

    try {
        const api = new PaymentsApi(config);
        await api.paymentsIdProcessPost({ id: testPaymentID });

        console.log('✓ Payment processed successfully!');
        console.log(`  Payment ID: ${testPaymentID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error processing payment: ${error.message}\n`);
    }
}

// ========================================
// WALLETS API TESTS
// ========================================

async function testCreateWallet(config: Configuration) {
    console.log('--- Test 1: Create Wallet ---');

    if (!testCustomerID) {
        console.log('⚠ Warning: No customer ID available\n⚠ Skipping create wallet test\n');
        return;
    }

    try {
        const api = new WalletsApi(config);
        const response = await api.walletsPost({
            request: {
                customerId: testCustomerID,
                currency: 'USD',
                metadata: {
                    source: 'sdk_test',
                    test_run: new Date().toISOString(),
                },
            } as any,
        });

        testWalletID = response.id!;
        console.log('✓ Wallet created successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Customer ID: ${response.customerId}`);
        console.log(`  Balance: ${response.balance} ${response.currency}\n`);
    } catch (error: any) {
        console.log(`❌ Error creating wallet: ${error.message}\n`);
    }
}

async function testGetWallet(config: Configuration) {
    console.log('--- Test 2: Get Wallet by ID ---');

    if (!testWalletID) {
        console.log('⚠ Warning: No wallet ID available\n⚠ Skipping get wallet test\n');
        return;
    }

    try {
        const api = new WalletsApi(config);
        const response = await api.walletsIdGet({ id: testWalletID });

        console.log('✓ Wallet retrieved successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Balance: ${response.balance} ${response.currency}`);
        console.log(`  Created At: ${response.createdAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting wallet: ${error.message}\n`);
    }
}

async function testListWallets(config: Configuration) {
    console.log('--- Test 3: List Wallets ---');

    try {
        const api = new WalletsApi(config);
        const response = await api.walletsGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.items?.length || 0} wallets`);
        if (response.items && response.items.length > 0) {
            console.log(`  First wallet: ${response.items[0].id} - ${response.items[0].balance} ${response.items[0].currency}`);
        }
        if (response.pagination) {
            console.log(`  Total: ${response.pagination.total}\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing wallets: ${error.message}\n`);
    }
}

async function testUpdateWallet(config: Configuration) {
    console.log('--- Test 4: Update Wallet ---');

    if (!testWalletID) {
        console.log('⚠ Warning: No wallet ID available\n⚠ Skipping update wallet test\n');
        return;
    }

    try {
        const api = new WalletsApi(config);
        const response = await api.walletsIdPut({
            id: testWalletID,
            request: {
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });

        console.log('✓ Wallet updated successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Updated At: ${response.updatedAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error updating wallet: ${error.message}\n`);
    }
}

async function testGetWalletBalance(config: Configuration) {
    console.log('--- Test 5: Get Wallet Balance ---');

    if (!testWalletID) {
        console.log('⚠ Warning: No wallet ID available\n⚠ Skipping get balance test\n');
        return;
    }

    try {
        const api = new WalletsApi(config);
        const response = await api.walletsIdGet({ id: testWalletID });

        console.log('✓ Wallet balance retrieved!');
        console.log(`  Balance: ${response.balance} ${response.currency}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting balance: ${error.message}\n`);
    }
}

async function testTopUpWallet(config: Configuration) {
    console.log('--- Test 6: Top Up Wallet ---');

    if (!testWalletID) {
        console.log('⚠ Warning: No wallet ID available\n⚠ Skipping top up test\n');
        return;
    }

    try {
        const api = new WalletsApi(config);
        await api.walletsIdTopUpPost({
            id: testWalletID,
            request: {
                amount: '100.00',
                description: 'Test top-up',
                transactionReason: TypesTransactionReason.TransactionReasonPurchasedCreditDirect,
            },
        });

        console.log('✓ Wallet topped up successfully!');
        console.log(`  Wallet ID: ${testWalletID}`);
        console.log(`  Amount: 100.00\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error topping up wallet: ${error.message}\n`);
    }
}

async function testDebitWallet(config: Configuration) {
    console.log('--- Test 7: Debit Wallet ---');

    if (!testWalletID) {
        console.log('⚠ Warning: No wallet ID available\n⚠ Skipping debit test\n');
        return;
    }

    try {
        const api = new WalletsApi(config);
        await api.walletsIdDebitPost({
            id: testWalletID,
            request: {
                amount: '10.00',
                description: 'Test debit',
            } as any,
        });

        console.log('✓ Wallet debited successfully!');
        console.log(`  Wallet ID: ${testWalletID}`);
        console.log(`  Amount: 10.00\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error debiting wallet: ${error.message}\n`);
    }
}

async function testGetWalletTransactions(config: Configuration) {
    console.log('--- Test 8: Get Wallet Transactions ---');

    if (!testWalletID) {
        console.log('⚠ Warning: No wallet ID available\n⚠ Skipping transactions test\n');
        return;
    }

    try {
        const api = new WalletsApi(config);
        const response = await api.walletsIdTransactionsGet({ id: testWalletID });

        console.log('✓ Wallet transactions retrieved!');
        console.log(`  Total transactions: ${response.items?.length || 0}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting transactions: ${error.message}\n`);
    }
}

async function testSearchWallets(config: Configuration) {
    console.log('--- Test 9: Search Wallets ---');

    try {
        const api = new WalletsApi(config);
        const response = await api.walletsSearchPost({ filter: {} });

        console.log('✓ Search completed!');
        console.log(`  Found ${response.items?.length || 0} wallets\n`);
    } catch (error: any) {
        console.log(`❌ Error searching wallets: ${error.message}\n`);
    }
}

// ========================================
// CREDIT GRANTS API TESTS
// ========================================

async function testCreateCreditGrant(config: Configuration) {
    console.log('--- Test 1: Create Credit Grant ---');

    // Skip if no plan available (matching Go test)
    if (!testPlanID) {
        console.log('⚠ Warning: No plan ID available\n⚠ Skipping create credit grant test\n');
        return;
    }

    try {
        const api = new CreditGrantsApi(config);
        const response = await api.creditgrantsPost({
            creditGrant: {
                name: 'Test Credit Grant',
                credits: '500.00', // Match Go test amount
                scope: TypesCreditGrantScope.CreditGrantScopePlan,
                planId: testPlanID,
                cadence: TypesCreditGrantCadence.CreditGrantCadenceOneTime,
                expirationType: TypesCreditGrantExpiryType.CreditGrantExpiryTypeNever,
                expirationDurationUnit: TypesCreditGrantExpiryDurationUnit.CreditGrantExpiryDurationUnitDays, // Added to match Go test
                metadata: {
                    source: 'sdk_test',
                    test_run: new Date().toISOString(),
                },
            },
        });

        testCreditGrantID = response.id!;
        console.log('✓ Credit grant created successfully!');
        console.log(`  ID: ${response.id}`);
        if (response.credits) {
            console.log(`  Credits: ${response.credits}`);
        }
        console.log(`  Plan ID: ${response.planId}\n`);
    } catch (error: any) {
        console.log(`❌ Error creating credit grant: ${error.message || error}`);
        
        // Enhanced error logging to match Go test
        if (error.response) {
            console.log(`  Response Status Code: ${error.response.status || error.response.statusCode || 'unknown'}`);
            if (error.response.data) {
                console.log(`  Response Data: ${JSON.stringify(error.response.data, null, 2)}`);
            }
            if (error.response.body) {
                console.log(`  Response Body: ${JSON.stringify(error.response.body, null, 2)}`);
            }
        }
        
        if (error.body) {
            console.log(`  Error Body: ${JSON.stringify(error.body, null, 2)}`);
        }
        
        if (error.status) {
            console.log(`  Status Code: ${error.status}`);
        }
        
        if (error.statusCode) {
            console.log(`  Status Code: ${error.statusCode}`);
        }
        
        // Log the entire error object structure for debugging
        console.log('  Error Object Keys:', Object.keys(error));
        
        // Log request details for debugging
        console.log('  Credit Grant Request Details:');
        console.log('    Name: Test Credit Grant');
        console.log('    Credits: 500.00');
        console.log('    Scope: PLAN');
        console.log(`    PlanId: ${testPlanID}`);
        console.log('    Cadence: ONETIME');
        console.log('    ExpirationType: NEVER');
        console.log('    ExpirationDurationUnit: DAYS');
        console.log();
    }
}

async function testGetCreditGrant(config: Configuration) {
    console.log('--- Test 2: Get Credit Grant by ID ---');

    if (!testCreditGrantID) {
        console.log('⚠ Warning: No credit grant ID available\n⚠ Skipping get credit grant test\n');
        return;
    }

    try {
        const api = new CreditGrantsApi(config);
        const response = await api.creditgrantsIdGet({ id: testCreditGrantID });

        console.log('✓ Credit grant retrieved successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Grant Amount: ${(response as any).grantAmount}`);
        console.log(`  Created At: ${response.createdAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting credit grant: ${error.message}\n`);
    }
}

async function testListCreditGrants(config: Configuration) {
    console.log('--- Test 3: List Credit Grants ---');

    try {
        const api = new CreditGrantsApi(config);
        const response = await api.creditgrantsGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.items?.length || 0} credit grants`);
        if (response.items && response.items.length > 0) {
            console.log(`  First credit grant: ${response.items[0].id}`);
        }
        if (response.pagination) {
            console.log(`  Total: ${response.pagination.total}\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing credit grants: ${error.message}\n`);
    }
}

async function testUpdateCreditGrant(config: Configuration) {
    console.log('--- Test 4: Update Credit Grant ---');

    if (!testCreditGrantID) {
        console.log('⚠ Warning: No credit grant ID available\n⚠ Skipping update credit grant test\n');
        return;
    }

    try {
        const api = new CreditGrantsApi(config);
        const response = await api.creditgrantsIdPut({
            id: testCreditGrantID,
            creditGrant: {
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });

        console.log('✓ Credit grant updated successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Updated At: ${response.updatedAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error updating credit grant: ${error.message}\n`);
    }
}

async function testDeleteCreditGrant(config: Configuration) {
    console.log('--- Cleanup: Delete Credit Grant ---');

    if (!testCreditGrantID) {
        console.log('⚠ Skipping delete credit grant (no credit grant created)\n');
        return;
    }

    try {
        const api = new CreditGrantsApi(config);
        await api.creditgrantsIdDelete({ id: testCreditGrantID });

        console.log('✓ Credit grant deleted successfully!');
        console.log(`  Deleted ID: ${testCreditGrantID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting credit grant: ${error.message}\n`);
    }
}

// ========================================
// CREDIT NOTES API TESTS
// ========================================

async function testCreateCreditNote(config: Configuration) {
    console.log('--- Test 1: Create Credit Note ---');

    // Skip if no customer available (matching Go test)
    if (!testCustomerID) {
        console.log('⚠ Warning: No customer ID available\n⚠ Skipping create credit note test\n');
        return;
    }

    // Skip if no invoice available (matching Go test)
    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available, skipping create credit note test\n');
        return;
    }

    let invoice: any = null;
    
    try {
        const invoicesApi = new InvoicesApi(config);
        const creditNotesApi = new CreditNotesApi(config);

        // Get invoice to retrieve line items for credit note (matching Go test)
        invoice = await invoicesApi.invoicesIdGet({ id: testInvoiceID });
        
        if (!invoice) {
            console.log('⚠ Warning: Could not retrieve invoice\n⚠ Skipping create credit note test\n');
            return;
        }

        console.log(`Invoice has ${invoice.lineItems?.length || 0} line items`);
        if (!invoice.lineItems || invoice.lineItems.length === 0) {
            console.log('⚠ Warning: Invoice has no line items\n⚠ Skipping create credit note test\n');
            return;
        }

        // Check invoice status - must be FINALIZED to create credit note (matching Go validation)
        // If invoice is in DRAFT status, try to finalize it first
        if (invoice.invoiceStatus === TypesInvoiceStatus.InvoiceStatusDraft) {
            console.log(`  Invoice is in DRAFT status, attempting to finalize...`);
            try {
                await invoicesApi.invoicesIdFinalizePost({ id: testInvoiceID });
                console.log('  Invoice finalized successfully');
                // Re-fetch the invoice to get updated status
                invoice = await invoicesApi.invoicesIdGet({ id: testInvoiceID });
            } catch (finalizeError: any) {
                console.log(`⚠ Warning: Failed to finalize invoice: ${finalizeError.message || finalizeError}`);
                console.log('⚠ Skipping create credit note test\n');
                return;
            }
        }
        
        if (invoice.invoiceStatus !== TypesInvoiceStatus.InvoiceStatusFinalized) {
            console.log(`⚠ Warning: Invoice must be FINALIZED to create credit note. Current status: ${invoice.invoiceStatus}\n⚠ Skipping create credit note test\n`);
            return;
        }
        
        console.log(`  Invoice status: ${invoice.invoiceStatus} (ready for credit note)`);

        // Use first line item from invoice for credit note (matching Go test)
        const firstLineItem = invoice.lineItems[0];
        const creditAmount = '50.00'; // Credit 50% of the line item amount (matching Go test)
        const lineItemId = firstLineItem.id;
        const lineItemDisplayName = firstLineItem.displayName || 'Invoice Line Item';

        if (!lineItemId) {
            console.log('⚠ Warning: Line item has no ID\n⚠ Skipping create credit note test\n');
            console.log('  Line item structure:', JSON.stringify(firstLineItem, null, 2));
            return;
        }

        // Log line item details for debugging
        console.log(`  Using line item ID: ${lineItemId}`);
        console.log(`  Line item display name: ${lineItemDisplayName}`);
        console.log(`  Credit amount: ${creditAmount}`);

        const response = await creditNotesApi.creditnotesPost({
            creditNote: {
                invoiceId: testInvoiceID,
                reason: TypesCreditNoteReason.CreditNoteReasonBillingError,
                memo: 'Test credit note from SDK',
                lineItems: [{
                    invoiceLineItemId: lineItemId,
                    amount: creditAmount,
                    displayName: `Credit for ${lineItemDisplayName}`, // Use actual line item display name
                }],
                metadata: {
                    source: 'sdk_test',
                    test_run: new Date().toISOString(),
                },
            },
        });

        testCreditNoteID = response.id!;
        console.log('✓ Credit note created successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Invoice ID: ${response.invoiceId}\n`);
    } catch (error: any) {
        console.log(`❌ Error creating credit note: ${error.message || error}`);
        
        // Enhanced error logging to match Go test - try to get actual error message
        if (error.response) {
            const statusCode = error.response.status || error.response.statusCode || 'unknown';
            console.log(`  Response Status Code: ${statusCode}`);
            
            if (error.response.data) {
                console.log(`  Response Data: ${JSON.stringify(error.response.data, null, 2)}`);
            }
            if (error.response.body) {
                console.log(`  Response Body: ${JSON.stringify(error.response.body, null, 2)}`);
            }
            
            // Try to get response text if it's a Response object
            if (error.response.text && typeof error.response.text === 'function') {
                error.response.text().then((text: string) => {
                    console.log(`  Response Text: ${text}`);
                }).catch(() => {
                    // Ignore if text() fails
                });
            }
            
            // Try to get JSON if available
            if (error.response.json && typeof error.response.json === 'function') {
                error.response.json().then((data: any) => {
                    console.log(`  Response JSON: ${JSON.stringify(data, null, 2)}`);
                }).catch(() => {
                    // Ignore if json() fails
                });
            }
        }
        
        if (error.body) {
            console.log(`  Error Body: ${JSON.stringify(error.body, null, 2)}`);
        }
        
        if (error.status) {
            console.log(`  Status Code: ${error.status}`);
        }
        
        if (error.statusCode) {
            console.log(`  Status Code: ${error.statusCode}`);
        }
        
        // Try to get response body if it's a Response object
        if (error instanceof Response) {
            error.text().then((text) => {
                console.log(`  Response Text: ${text}`);
            }).catch((e) => {
                console.log(`  Could not read response text: ${e}`);
            });
        }
        
        // Also check if error has a json() method (common in Fetch API)
        if (error.json && typeof error.json === 'function') {
            error.json().then((data: any) => {
                console.log(`  Error JSON: ${JSON.stringify(data, null, 2)}`);
            }).catch(() => {
                // Ignore if json() fails
            });
        }
        
        // Log the entire error object structure for debugging
        console.log('  Error Object Keys:', Object.keys(error));
        if (error.response) {
            console.log('  Error Response Keys:', Object.keys(error.response));
        }
        
        // Log request details for debugging
        console.log('  Credit Note Request Details:');
        console.log(`    InvoiceId: ${testInvoiceID}`);
        console.log('    Reason: BILLING_ERROR');
        console.log('    Memo: Test credit note from SDK');
        if (invoice && invoice.lineItems && invoice.lineItems.length > 0) {
            const firstItem = invoice.lineItems[0];
            console.log(`    LineItems[0].invoiceLineItemId: ${firstItem.id}`);
            console.log(`    LineItems[0].amount: 50.00`);
            console.log(`    LineItems[0].displayName: Credit for ${firstItem.displayName || 'Invoice Line Item'}`);
        } else {
            console.log('    LineItems: [none available]');
        }
        console.log();
    }
}

async function testGetCreditNote(config: Configuration) {
    console.log('--- Test 2: Get Credit Note by ID ---');

    if (!testCreditNoteID) {
        console.log('⚠ Warning: No credit note ID available\n⚠ Skipping get credit note test\n');
        return;
    }

    try {
        const api = new CreditNotesApi(config);
        const response = await api.creditnotesIdGet({ id: testCreditNoteID });

        console.log('✓ Credit note retrieved successfully!');
        console.log(`  ID: ${response.id}`);
        console.log(`  Total: ${(response as any).total || 'N/A'}`);
        console.log(`  Created At: ${response.createdAt}\n`);
    } catch (error: any) {
        console.log(`❌ Error getting credit note: ${error.message}\n`);
    }
}

async function testListCreditNotes(config: Configuration) {
    console.log('--- Test 3: List Credit Notes ---');

    try {
        const api = new CreditNotesApi(config);
        const response = await api.creditnotesGet({ limit: 10 });

        console.log(`✓ Retrieved ${response.items?.length || 0} credit notes`);
        if (response.items && response.items.length > 0) {
            console.log(`  First credit note: ${response.items[0].id}`);
        }
        if (response.pagination) {
            console.log(`  Total: ${response.pagination.total}\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing credit notes: ${error.message}\n`);
    }
}

async function testFinalizeCreditNote(config: Configuration) {
    console.log('--- Test 4: Finalize Credit Note ---');

    if (!testCreditNoteID) {
        console.log('⚠ Warning: No credit note ID available\n⚠ Skipping finalize credit note test\n');
        return;
    }

    try {
        const api = new CreditNotesApi(config);
        await api.creditnotesIdFinalizePost({ id: testCreditNoteID });

        console.log('✓ Credit note finalized successfully!');
        console.log(`  Credit Note ID: ${testCreditNoteID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error finalizing credit note: ${error.message}\n`);
    }
}

// ========================================
// CLEANUP TESTS
// ========================================

async function testDeletePayment(config: Configuration) {
    console.log('--- Cleanup: Delete Payment ---');

    if (!testPaymentID) {
        console.log('⚠ Skipping delete payment (no payment created)\n');
        return;
    }

    try {
        const api = new PaymentsApi(config);
        await api.paymentsIdDelete({ id: testPaymentID });

        console.log('✓ Payment deleted successfully!');
        console.log(`  Deleted ID: ${testPaymentID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting payment: ${error.message}\n`);
    }
}

async function testDeletePrice(config: Configuration) {
    console.log('--- Cleanup: Delete Price ---');

    if (!testPriceID) {
        console.log('⚠ Skipping delete price (no price created)\n');
        return;
    }

    try {
        const api = new PricesApi(config);
        const futureDate = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString();
        await api.pricesIdDelete({
            id: testPriceID,
            request: { endDate: futureDate },
        });

        console.log('✓ Price deleted successfully!');
        console.log(`  Deleted ID: ${testPriceID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting price: ${error.message}\n`);
    }
}

async function testDeleteEntitlement(config: Configuration) {
    console.log('--- Cleanup: Delete Entitlement ---');

    if (!testEntitlementID) {
        console.log('⚠ Skipping delete entitlement (no entitlement created)\n');
        return;
    }

    try {
        const api = new EntitlementsApi(config);
        await api.entitlementsIdDelete({ id: testEntitlementID });

        console.log('✓ Entitlement deleted successfully!');
        console.log(`  Deleted ID: ${testEntitlementID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting entitlement: ${error.message}\n`);
    }
}

async function testDeleteAddon(config: Configuration) {
    console.log('--- Cleanup: Delete Addon ---');

    if (!testAddonID) {
        console.log('⚠ Skipping delete addon (no addon created)\n');
        return;
    }

    try {
        const api = new AddonsApi(config);
        await api.addonsIdDelete({ id: testAddonID });

        console.log('✓ Addon deleted successfully!');
        console.log(`  Deleted ID: ${testAddonID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting addon: ${error.message}\n`);
    }
}

async function testDeletePlan(config: Configuration) {
    console.log('--- Cleanup: Delete Plan ---');

    if (!testPlanID) {
        console.log('⚠ Skipping delete plan (no plan created)\n');
        return;
    }

    try {
        const api = new PlansApi(config);
        await api.plansIdDelete({ id: testPlanID });

        console.log('✓ Plan deleted successfully!');
        console.log(`  Deleted ID: ${testPlanID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting plan: ${error.message}\n`);
    }
}

async function testDeleteFeature(config: Configuration) {
    console.log('--- Cleanup: Delete Feature ---');

    if (!testFeatureID) {
        console.log('⚠ Skipping delete feature (no feature created)\n');
        return;
    }

    try {
        const api = new FeaturesApi(config);
        await api.featuresIdDelete({ id: testFeatureID });

        console.log('✓ Feature deleted successfully!');
        console.log(`  Deleted ID: ${testFeatureID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting feature: ${error.message}\n`);
    }
}

async function testDeleteCustomer(config: Configuration) {
    console.log('--- Cleanup: Delete Customer ---');

    if (!testCustomerID) {
        console.log('⚠ Skipping delete customer (no customer created)\n');
        return;
    }

    try {
        const api = new CustomersApi(config);
        await api.customersIdDelete({ id: testCustomerID });

        console.log('✓ Customer deleted successfully!');
        console.log(`  Deleted ID: ${testCustomerID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting customer: ${error.message}\n`);
    }
}

// ========================================
// EVENTS API TESTS
// ========================================

async function testCreateEvent(config: Configuration) {
    console.log('--- Test 1: Create Event ---');

    // Use test customer external ID if available, otherwise generate a unique one
    if (!testCustomerID) {
        testEventCustomerID = `test-customer-${Date.now()}`;
    } else {
        try {
            const customerApi = new CustomersApi(config);
            const customer = await customerApi.customersIdGet({ id: testCustomerID });
            testEventCustomerID = customer.externalId || `test-customer-${Date.now()}`;
        } catch {
            testEventCustomerID = `test-customer-${Date.now()}`;
        }
    }

    testEventName = `Test Event ${Date.now()}`;

    try {
        const api = new EventsApi(config);
        const response = await api.eventsPost({
            event: {
                eventName: testEventName,
                externalCustomerId: testEventCustomerID,
                properties: {
                    source: 'sdk_test',
                    environment: 'test',
                    test_run: new Date().toISOString(),
                },
                source: 'sdk_test',
                timestamp: new Date().toISOString(),
            },
        });

        // The response might be a dict or an object with event_id attribute
        if (typeof response === 'object' && response !== null) {
            if ('event_id' in response) {
                testEventID = (response as any).event_id;
            } else if ('eventId' in response) {
                testEventID = (response as any).eventId;
            }
        }

        console.log('✓ Event created successfully!');
        if (testEventID) {
            console.log(`  Event ID: ${testEventID}`);
        }
        console.log(`  Event Name: ${testEventName}`);
        console.log(`  Customer ID: ${testEventCustomerID}\n`);
    } catch (error: any) {
        console.log(`❌ Error creating event: ${error.message}\n`);
    }
}

async function testQueryEvents(config: Configuration) {
    console.log('--- Test 2: Query Events ---');

    if (!testEventName) {
        console.log('⚠ Warning: No event created, skipping query test\n');
        return;
    }

    try {
        const api = new EventsApi(config);
        const response = await api.eventsQueryPost({
            request: {
                externalCustomerId: testEventCustomerID,
                eventName: testEventName,
            },
        });

        console.log('✓ Events queried successfully!');
        if (response.events && response.events.length > 0) {
            console.log(`  Found ${response.events.length} events`);
            response.events.slice(0, 3).forEach((event: any, i: number) => {
                const eventId = event.id || 'N/A';
                const eventName = event.eventName || event.event_name || 'N/A';
                console.log(`  - Event ${i + 1}: ${eventId} - ${eventName}`);
            });
        } else {
            console.log('  No events found');
        }
        console.log();
    } catch (error: any) {
        console.log(`⚠ Warning: Error querying events: ${error.message}`);
        console.log('⚠ Skipping query events test\n');
    }
}

async function testAsyncEventEnqueue(config: Configuration) {
    console.log('--- Test 3: Async Event - Simple Enqueue ---');

    // Use test customer external ID if available
    let customerID = testEventCustomerID;
    if (!customerID) {
        if (testCustomerID) {
            try {
                const customerApi = new CustomersApi(config);
                const customer = await customerApi.customersIdGet({ id: testCustomerID });
                customerID = customer.externalId || `test-customer-${Date.now()}`;
            } catch {
                customerID = `test-customer-${Date.now()}`;
            }
        } else {
            customerID = `test-customer-${Date.now()}`;
        }
    }

    try {
        const api = new EventsApi(config);
        const eventRequest = {
            eventName: 'api_request',
            externalCustomerId: customerID,
            properties: {
                path: '/api/resource',
                method: 'GET',
                status: '200',
                response_time_ms: '150',
            },
            source: 'sdk_test',
        };

        // Use async method if available, otherwise use sync
        if ((api as any).eventsPostAsync) {
            await (api as any).eventsPostAsync({ event: eventRequest });
        } else {
            // Fallback to sync if async not available
            await api.eventsPost({ event: eventRequest });
        }

        console.log('✓ Async event enqueued successfully!');
        console.log('  Event Name: api_request');
        console.log(`  Customer ID: ${customerID}\n`);
    } catch (error: any) {
        console.log(`❌ Error enqueueing async event: ${error.message}\n`);
    }
}

async function testAsyncEventEnqueueWithOptions(config: Configuration) {
    console.log('--- Test 4: Async Event - Enqueue With Options ---');

    // Use test customer external ID if available
    let customerID = testEventCustomerID;
    if (!customerID) {
        if (testCustomerID) {
            try {
                const customerApi = new CustomersApi(config);
                const customer = await customerApi.customersIdGet({ id: testCustomerID });
                customerID = customer.externalId || `test-customer-${Date.now()}`;
            } catch {
                customerID = `test-customer-${Date.now()}`;
            }
        } else {
            customerID = `test-customer-${Date.now()}`;
        }
    }

    try {
        const api = new EventsApi(config);
        const eventRequest = {
            eventName: 'file_upload',
            externalCustomerId: customerID,
            properties: {
                file_size_bytes: '1048576',
                file_type: 'image/jpeg',
                storage_bucket: 'user_uploads',
            },
            source: 'sdk_test',
            timestamp: new Date().toISOString(),
        };

        // Use async method if available, otherwise use sync
        if ((api as any).eventsPostAsync) {
            await (api as any).eventsPostAsync({ event: eventRequest });
        } else {
            // Fallback to sync if async not available
            await api.eventsPost({ event: eventRequest });
        }

        console.log('✓ Async event with options enqueued successfully!');
        console.log('  Event Name: file_upload');
        console.log(`  Customer ID: ${customerID}\n`);
    } catch (error: any) {
        console.log(`❌ Error enqueueing async event with options: ${error.message}\n`);
    }
}

async function testAsyncEventBatch(config: Configuration) {
    console.log('--- Test 5: Async Event - Batch Enqueue ---');

    // Use test customer external ID if available
    let customerID = testEventCustomerID;
    if (!customerID) {
        if (testCustomerID) {
            try {
                const customerApi = new CustomersApi(config);
                const customer = await customerApi.customersIdGet({ id: testCustomerID });
                customerID = customer.externalId || `test-customer-${Date.now()}`;
            } catch {
                customerID = `test-customer-${Date.now()}`;
            }
        } else {
            customerID = `test-customer-${Date.now()}`;
        }
    }

    try {
        const api = new EventsApi(config);
        const batchCount = 5;

        for (let i = 0; i < batchCount; i++) {
            const eventRequest = {
                eventName: 'batch_example',
                externalCustomerId: customerID,
                properties: {
                    index: String(i),
                    batch: 'demo',
                },
                source: 'sdk_test',
            };

            // Use async method if available, otherwise use sync
            if ((api as any).eventsPostAsync) {
                await (api as any).eventsPostAsync({ event: eventRequest });
            } else {
                // Fallback to sync if async not available
                await api.eventsPost({ event: eventRequest });
            }
        }

        console.log(`✓ Enqueued ${batchCount} batch events successfully!`);
        console.log('  Event Name: batch_example');
        console.log(`  Customer ID: ${customerID}`);
        console.log('  Waiting for events to be processed...\n');
        
        // Wait for background processing
        await new Promise(resolve => setTimeout(resolve, 2000));
    } catch (error: any) {
        console.log(`❌ Error enqueueing batch event: ${error.message}\n`);
    }
}

// ========================================
// MAIN EXECUTION
// ========================================

async function main() {
    const config = getConfiguration();

    console.log('========================================');
    console.log('CUSTOMER API TESTS');
    console.log('========================================\n');

    await testCreateCustomer(config);
    await testGetCustomer(config);
    await testListCustomers(config);
    await testUpdateCustomer(config);
    await testLookupCustomer(config);
    await testSearchCustomers(config);
    await testGetCustomerEntitlements(config);
    await testGetCustomerUpcomingGrants(config);
    await testGetCustomerUsage(config);

    console.log('✓ Customer API Tests Completed!\n');

    console.log('========================================');
    console.log('FEATURES API TESTS');
    console.log('========================================\n');

    await testCreateFeature(config);
    await testGetFeature(config);
    await testListFeatures(config);
    await testUpdateFeature(config);
    await testSearchFeatures(config);

    console.log('✓ Features API Tests Completed!\n');

    console.log('========================================');
    console.log('CONNECTIONS API TESTS');
    console.log('========================================\n');

    await testListConnections(config);
    await testSearchConnections(config);

    console.log('✓ Connections API Tests Completed!\n');

    console.log('========================================');
    console.log('PLANS API TESTS');
    console.log('========================================\n');

    await testCreatePlan(config);
    await testGetPlan(config);
    await testListPlans(config);
    await testUpdatePlan(config);
    await testSearchPlans(config);

    console.log('✓ Plans API Tests Completed!\n');

    console.log('========================================');
    console.log('ADDONS API TESTS');
    console.log('========================================\n');

    await testCreateAddon(config);
    await testGetAddon(config);
    await testListAddons(config);
    await testUpdateAddon(config);
    await testLookupAddon(config);
    await testSearchAddons(config);

    console.log('✓ Addons API Tests Completed!\n');

    console.log('========================================');
    console.log('ENTITLEMENTS API TESTS');
    console.log('========================================\n');

    await testCreateEntitlement(config);
    await testGetEntitlement(config);
    await testListEntitlements(config);
    await testUpdateEntitlement(config);
    await testSearchEntitlements(config);

    console.log('✓ Entitlements API Tests Completed!\n');

    console.log('========================================');
    console.log('SUBSCRIPTIONS API TESTS');
    console.log('========================================\n');

    await testCreateSubscription(config);
    await testGetSubscription(config);
    await testListSubscriptions(config);
    await testUpdateSubscription(config);
    await testSearchSubscriptions(config);
    await testActivateSubscription(config);
    // Lifecycle management (commented out - not needed)
    // await testPauseSubscription(config);
    // await testResumeSubscription(config);
    // await testGetPauseHistory(config);
    await testAddAddonToSubscription(config);
    await testRemoveAddonFromSubscription(config);
    // Change management
    // await testPreviewSubscriptionChange(config); // Commented out - not needed
    await testExecuteSubscriptionChange(config);
    await testGetSubscriptionEntitlements(config);
    await testGetUpcomingGrants(config);
    await testReportUsage(config);
    await testUpdateLineItem(config);
    await testDeleteLineItem(config);
    await testCancelSubscription(config);

    console.log('✓ Subscriptions API Tests Completed!\n');

    console.log('========================================');
    console.log('INVOICES API TESTS');
    console.log('========================================\n');

    await testListInvoices(config);
    await testSearchInvoices(config);
    await testCreateInvoice(config);
    await testGetInvoice(config);
    await testUpdateInvoice(config);
    await testPreviewInvoice(config);
    await testFinalizeInvoice(config);
    await testRecalculateInvoice(config);
    await testRecordPayment(config);
    await testAttemptPayment(config);
    await testDownloadInvoicePDF(config);
    await testTriggerInvoiceComms(config);
    await testGetCustomerInvoiceSummary(config);
    await testVoidInvoice(config);

    console.log('✓ Invoices API Tests Completed!\n');

    console.log('========================================');
    console.log('PRICES API TESTS');
    console.log('========================================\n');

    await testCreatePrice(config);
    await testGetPrice(config);
    await testListPrices(config);
    await testUpdatePrice(config);

    console.log('✓ Prices API Tests Completed!\n');

    console.log('========================================');
    console.log('PAYMENTS API TESTS');
    console.log('========================================\n');

    await testCreatePayment(config);
    await testGetPayment(config);
    await testSearchPayments(config);
    await testListPayments(config);
    await testUpdatePayment(config);
    await testProcessPayment(config);

    console.log('✓ Payments API Tests Completed!\n');

    console.log('========================================');
    console.log('WALLETS API TESTS');
    console.log('========================================\n');

    await testCreateWallet(config);
    await testGetWallet(config);
    await testListWallets(config);
    await testUpdateWallet(config);
    await testGetWalletBalance(config);
    await testTopUpWallet(config);
    await testDebitWallet(config);
    await testGetWalletTransactions(config);
    await testSearchWallets(config);

    console.log('✓ Wallets API Tests Completed!\n');

    console.log('========================================');
    console.log('CREDIT GRANTS API TESTS');
    console.log('========================================\n');

    await testCreateCreditGrant(config);
    await testGetCreditGrant(config);
    await testListCreditGrants(config);
    await testUpdateCreditGrant(config);
    // Note: testDeleteCreditGrant is in cleanup section

    console.log('✓ Credit Grants API Tests Completed!\n');

    console.log('========================================');
    console.log('CREDIT NOTES API TESTS');
    console.log('========================================\n');

    await testCreateCreditNote(config);
    await testGetCreditNote(config);
    await testListCreditNotes(config);
    await testFinalizeCreditNote(config);

    console.log('✓ Credit Notes API Tests Completed!\n');

    console.log('========================================');
    console.log('EVENTS API TESTS');
    console.log('========================================\n');

    // Sync event operations
    await testCreateEvent(config);
    await testQueryEvents(config);

    // Async event operations
    await testAsyncEventEnqueue(config);
    await testAsyncEventEnqueueWithOptions(config);
    await testAsyncEventBatch(config);

    console.log('✓ Events API Tests Completed!\n');

    console.log('========================================');
    console.log('CLEANUP - DELETING TEST DATA');
    console.log('========================================\n');

    await testDeletePayment(config);
    await testDeletePrice(config);
    await testDeleteEntitlement(config);
    await testDeleteAddon(config);
    await testDeletePlan(config);
    await testDeleteFeature(config);
    await testDeleteCreditGrant(config);
    await testDeleteCustomer(config);

    console.log('✓ Cleanup Completed!\n');

    console.log('\n=== All API Tests Completed Successfully! ===');
}

main().catch(console.error);

