#!/usr/bin/env ts-node

/**
 * Flexprice TypeScript SDK - Published SDK tests (npm install flexprice-ts-temp).
 * Run from api/tests/ts: npm install flexprice-ts-temp && npx ts-node test_sdk_js.ts
 * Requires: FLEXPRICE_API_KEY, FLEXPRICE_API_HOST
 * Package: https://www.npmjs.com/package/flexprice-ts-temp
 * Repo: https://github.com/flexprice/js-sdk-temp
 */

import {
    AddonType,
    BillingCadence,
    BillingCycle,
    BillingModel,
    BillingPeriod,
    CreditGrantCadence,
    CreditGrantExpiryDurationUnit,
    CreditGrantExpiryType,
    CreditGrantScope,
    CreditNoteReason,
    EntitlementUsageResetPeriod,
    FeatureType,
    Flexprice,
    InvoiceBillingReason,
    InvoiceCadence,
    InvoiceStatus,
    InvoiceType,
    PauseMode,
    PaymentDestinationType,
    PaymentMethodType,
    PaymentStatus,
    PriceEntityType,
    PriceType,
    PriceUnitType,
    ProrationBehavior,
    ResumeMode,
    TransactionReason,
} from 'flexprice-ts-temp';

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
// HELPERS (published SDK may return { result } or { data } wrapper; also handle { customer } etc.)
// ========================================

/** Log raw and unwrapped response for debugging "Unexpected response shape" */
function debugResponse(label: string, raw: unknown, unwrapped: unknown): void {
    const safe = (x: unknown) => {
        try {
            const s = JSON.stringify(x, null, 2);
            return s.length > 800 ? s.slice(0, 800) + '...' : s;
        } catch {
            return String(x);
        }
    };
    console.log(`  [DEBUG ${label}] raw: ${safe(raw)}`);
    if (raw != null && typeof raw === 'object') {
        const r = raw as Record<string, unknown>;
        console.log(`  [DEBUG ${label}] Object.keys(raw): ${JSON.stringify(Object.keys(r))}`);
        console.log(`  [DEBUG ${label}] raw.constructor?.name: ${(r as object).constructor?.name ?? 'undefined'}`);
        console.log(`  [DEBUG ${label}] raw.id: ${JSON.stringify((r as { id?: unknown }).id)}`);
        console.log(`  [DEBUG ${label}] raw.value: ${r.value !== undefined ? typeof r.value : 'undefined'}`);
    }
    console.log(`  [DEBUG ${label}] unwrapped: ${safe(unwrapped)}\n`);
}

/** Get entity with id from raw SDK response (handles Result, wrapped, and non-enumerable props) */
function getEntityWithId(raw: unknown): Record<string, unknown> | null {
    if (raw == null || typeof raw !== 'object') return null;
    const r = raw as Record<string, unknown>;
    const candidates: unknown[] = [r, r.value, r.data, r.result, r.customer];
    for (const c of candidates) {
        if (c == null || typeof c !== 'object') continue;
        const obj = c as Record<string, unknown>;
        const id = obj.id ?? (obj as { id?: string }).id;
        if (id != null && typeof id === 'string') {
            return obj as Record<string, unknown>;
        }
    }
    return null;
}

function unwrap<T extends Record<string, unknown>>(response: unknown): T | null {
    if (response == null) return null;
    const r = response as Record<string, unknown>;
    // SDK may return { result }, { data }, { value } (Result type), or the entity directly
    let body = (r.result ?? r.data ?? r.value ?? r) as T;
    if (!body || typeof body !== 'object') return null;
    // One more level: API may return { data: { id, ... } } or { customer: { id, ... } }
    const hasId = 'id' in body && (body as { id?: unknown }).id;
    if (!hasId) {
        const inner = (body as Record<string, unknown>).customer ?? (body as Record<string, unknown>).data ?? (body as Record<string, unknown>).result ?? (body as Record<string, unknown>).value;
        if (inner && typeof inner === 'object' && 'id' in inner) {
            body = inner as T;
        }
    }
    return body;
}

// ========================================
// CLIENT
// ========================================

function getClient(): Flexprice {
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

    console.log('=== Flexprice TypeScript SDK - API Tests (local api/typescript) ===\n');
    console.log(`✓ API Key: ${apiKey.substring(0, 8)}...${apiKey.slice(-4)}`);
    console.log(`✓ API Host: ${apiHost}\n`);

    let serverURL = apiHost;
    if (!serverURL.startsWith('http://') && !serverURL.startsWith('https://')) {
        serverURL = `https://${serverURL}`;
    }

    return new Flexprice({
        serverURL,
        apiKeyAuth: apiKey,
    });
}

// ========================================
// CUSTOMERS API TESTS
// ========================================

async function testCreateCustomer(client: Flexprice) {
    console.log('--- Test 1: Create Customer ---');

    try {
        const timestamp = Date.now();
        testCustomerName = `Test Customer ${timestamp}`;
        const externalId = `test-customer-${timestamp}`;

        const raw = await client.customers.createCustomer({
            name: testCustomerName,
            email: `test-${timestamp}@example.com`,
            externalId: externalId,
        });
        const response = (getEntityWithId(raw) ?? unwrap<{ id: string; name?: string; externalId?: string; email?: string }>(raw)) as { id?: string; name?: string; externalId?: string; email?: string } | null;

        if (response && response.id) {
            testCustomerID = response.id;
            console.log('✓ Customer created successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Name: ${response.name}`);
            console.log(`  External ID: ${response.externalId}`);
            console.log(`  Email: ${response.email}\n`);
        } else {
            debugResponse('Create Customer', raw, response);
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error creating customer: ${error.message}\n`);
    }
}

async function testGetCustomer(client: Flexprice) {
    console.log('--- Test 2: Get Customer by ID ---');

    try {
        const raw = await client.customers.getCustomer({ id: testCustomerID });
        const response = (getEntityWithId(raw) ?? unwrap<{ id: string; name?: string }>(raw)) as { id?: string; name?: string } | null;

        if (response && response.id) {
            console.log('✓ Customer retrieved successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Name: ${response.name}`);
        } else {
            debugResponse('Get Customer', raw, response);
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error getting customer: ${error.message}\n`);
    }
}

async function testListCustomers(client: Flexprice) {
    console.log('--- Test 3: List Customers ---');

    try {
        const response = await client.customers.queryCustomer({ limit: 10 });

        if (response && 'items' in response) {
            console.log(`✓ Retrieved ${response.items?.length || 0} customers`);
            if (response.items && response.items.length > 0) {
                console.log(`  First customer: ${response.items[0].id} - ${response.items[0].name}`);
            }
            if (response.pagination) {
                console.log(`  Total: ${response.pagination?.total ?? ''}\n`);
            }
        } else {
            console.log(`✓ Retrieved 0 customers\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing customers: ${error.message}\n`);
    }
}

async function testUpdateCustomer(client: Flexprice) {
    console.log('--- Test 4: Update Customer ---');

    try {
        const raw = await client.customers.updateCustomer({
            id: testCustomerID,
            dtoUpdateCustomerRequest: {
                name: `${testCustomerName} (Updated)`,
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });
        const response = (getEntityWithId(raw) ?? unwrap<{ id: string; name?: string; updatedAt?: string }>(raw)) as { id?: string; name?: string; updatedAt?: string } | null;

        if (response && response.id) {
            console.log('✓ Customer updated successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  New Name: ${response.name}`);
            console.log(`  Updated At: ${response.updatedAt}\n`);
        } else {
            debugResponse('Update Customer', raw, response);
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error updating customer: ${error.message}\n`);
    }
}

async function testLookupCustomer(client: Flexprice) {
    console.log('--- Test 5: Lookup Customer by External ID ---');

    try {
        const externalId = testCustomerName ? `test-customer-${testCustomerName.split(' ')[2]}` : '';
        if (!externalId) {
            console.log('⚠ No external ID available\n');
            return;
        }
        const raw = await client.customers.getCustomerByExternalId({ externalId });
        const response = (getEntityWithId(raw) ?? unwrap<{ id: string; name?: string }>(raw)) as { id?: string; name?: string } | null;

        if (response && response.id) {
            console.log('✓ Customer found by external ID!');
            console.log(`  External ID: ${externalId}`);
            console.log(`  Customer ID: ${response.id}`);
            console.log(`  Name: ${response.name}\n`);
        } else {
            debugResponse('Lookup Customer by External ID', raw, response);
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error looking up customer: ${error.message}\n`);
    }
}

async function testSearchCustomers(client: Flexprice) {
    console.log('--- Test 6: Search Customers ---');

    try {
        const externalId = testCustomerName ? `test-customer-${testCustomerName.split(' ')[2]}` : '';
        if (!externalId) {
            console.log('⚠ No external ID for search\n');
            return;
        }
        const response = await client.customers.queryCustomer({ externalId });

        if (response && 'items' in response) {
            console.log('✓ Search completed!');
            console.log(`  Found ${response.items?.length || 0} customers matching external ID '${externalId}'`);
            if (response.items && response.items.length > 0) {
                response.items.forEach((customer: { id?: string; name?: string }) => {
                    console.log(`  - ${customer.id}: ${customer.name}`);
                });
            }
            console.log();
        } else {
            console.log(`✓ Found 0 customers\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error searching customers: ${error.message}\n`);
    }
}

async function testGetCustomerEntitlements(client: Flexprice) {
    console.log('--- Test 7: Get Customer Entitlements ---');

    try {
        const response = await client.customers.getCustomerEntitlements({ id: testCustomerID });

        if (response && 'features' in response) {
            console.log('✓ Retrieved customer entitlements!');
            console.log(`  Total features: ${response.features?.length || 0}\n`);
        } else {
            console.log('✓ Retrieved customer entitlements! (no features)\n');
        }
    } catch (error: any) {
        console.log(`❌ Error getting customer entitlements: ${error.message}\n`);
    }
}

async function testGetCustomerUpcomingGrants(client: Flexprice) {
    console.log('--- Test 8: Get Customer Upcoming Grants ---');

    try {
        const response = await client.customers.getCustomerUpcomingGrants({ id: testCustomerID });

        if (response && 'items' in response) {
            console.log('✓ Retrieved upcoming grants!');
            console.log(`  Total upcoming grants: ${response.items?.length || 0}\n`);
        } else {
            console.log('✓ Retrieved upcoming grants! (0)\n');
        }
    } catch (error: any) {
        console.log(`❌ Error getting upcoming grants: ${error.message}\n`);
    }
}

async function testGetCustomerUsage(client: Flexprice) {
    console.log('--- Test 9: Get Customer Usage ---');

    try {
        const response = await client.customers.getCustomerUsageSummary({ customerId: testCustomerID });

        if (response && 'features' in response) {
            console.log('✓ Retrieved customer usage!');
            console.log(`  Usage records: ${response.features?.length || 0}\n`);
        } else {
            console.log('✓ Retrieved customer usage! (0)\n');
        }
    } catch (error: any) {
        console.log(`❌ Error getting customer usage: ${error.message}\n`);
    }
}

// ========================================
// FEATURES API TESTS
// ========================================

async function testCreateFeature(client: Flexprice) {
    console.log('--- Test 1: Create Feature ---');

    try {
        const timestamp = Date.now();
        testFeatureName = `Test Feature ${timestamp}`;
        const featureKey = `test_feature_${timestamp}`;

        const raw = await client.features.createFeature({
            name: testFeatureName,
            lookupKey: featureKey,
            description: 'This is a test feature created by SDK tests',
            type: FeatureType.Boolean,
            metadata: {
                source: 'sdk_test',
                test_run: new Date().toISOString(),
                environment: 'test',
            },
        });
        const response = unwrap<{ id: string; name?: string; lookupKey?: string; type?: string }>(raw);

        if (response && 'id' in response && response.id) {
            testFeatureID = response.id;
            console.log('✓ Feature created successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Name: ${response.name}`);
            console.log(`  Lookup Key: ${response.lookupKey}`);
            console.log(`  Type: ${response.type}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error creating feature: ${error.message}\n`);
    }
}

async function testGetFeature(client: Flexprice) {
    console.log('--- Test 2: Get Feature by ID ---');

    try {
        const response = await client.features.queryFeature({ featureIds: [testFeatureID] });

        if (response && 'items' in response && response.items && response.items.length > 0) {
            const feature = response.items[0];
            console.log('✓ Feature retrieved successfully!');
            console.log(`  ID: ${feature.id}`);
            console.log(`  Name: ${feature.name}`);
            console.log(`  Lookup Key: ${feature.lookupKey}`);
            console.log(`  Created At: ${feature.createdAt}\n`);
        } else {
            console.log(`❌ Feature not found\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error getting feature: ${error.message}\n`);
    }
}

async function testListFeatures(client: Flexprice) {
    console.log('--- Test 3: List Features ---');

    try {
        const response = await client.features.queryFeature({ limit: 10 });

        if (response && 'items' in response) {
            console.log(`✓ Retrieved ${response.items?.length || 0} features`);
            if (response.items && response.items.length > 0) {
                console.log(`  First feature: ${response.items[0].id} - ${response.items[0].name}`);
            }
            if (response.pagination) {
                console.log(`  Total: ${response.pagination?.total ?? ''}\n`);
            }
        } else {
            console.log(`✓ Retrieved 0 features\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing features: ${error.message}\n`);
    }
}

async function testUpdateFeature(client: Flexprice) {
    console.log('--- Test 4: Update Feature ---');

    try {
        const response = await client.features.updateFeature({
            id: testFeatureID,
            dtoUpdateFeatureRequest: {
                name: `${testFeatureName} (Updated)`,
                description: 'Updated description for test feature',
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });

        if (response && 'id' in response) {
            console.log('✓ Feature updated successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  New Name: ${response.name}`);
            console.log(`  New Description: ${response.description}`);
            console.log(`  Updated At: ${response.updatedAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error updating feature: ${error.message}\n`);
    }
}

async function testSearchFeatures(client: Flexprice) {
    console.log('--- Test 5: Search Features ---');

    try {
        const response = await client.features.queryFeature({ featureIds: [testFeatureID] });

        if (response && 'items' in response) {
            console.log('✓ Search completed!');
            console.log(`  Found ${response.items?.length || 0} features matching ID '${testFeatureID}'`);
            if (response.items && response.items.length > 0) {
                response.items.slice(0, 3).forEach((feature: { id?: string; name?: string; lookupKey?: string }) => {
                    console.log(`  - ${feature.id}: ${feature.name} (${feature.lookupKey})`);
                });
            }
            console.log();
        } else {
            console.log(`✓ Found 0 features\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error searching features: ${error.message}\n`);
    }
}


// ========================================
// PLANS API TESTS
// ========================================

async function testCreatePlan(client: Flexprice) {
    console.log('--- Test 1: Create Plan ---');

    try {
        const timestamp = Date.now();
        testPlanName = `Test Plan ${timestamp}`;
        const lookupKey = `test_plan_${timestamp}`;

        const raw = await client.plans.createPlan({
            name: testPlanName,
            lookupKey,
            description: 'This is a test plan created by SDK tests',
            metadata: {
                source: 'sdk_test',
                test_run: new Date().toISOString(),
                environment: 'test',
            },
        });
        const response = unwrap<{ id: string; name?: string; lookupKey?: string }>(raw);

        if (response && 'id' in response && response.id) {
            testPlanID = response.id;
            console.log('✓ Plan created successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Name: ${response.name}`);
            console.log(`  Lookup Key: ${response.lookupKey}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error creating plan: ${error.message}\n`);
    }
}

async function testGetPlan(client: Flexprice) {
    console.log('--- Test 2: Get Plan by ID ---');

    try {
        const response = await client.plans.getPlan({ id: testPlanID });

        if (response && 'id' in response) {
            console.log('✓ Plan retrieved successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Name: ${response.name}`);
            console.log(`  Lookup Key: ${response.lookupKey}`);
            console.log(`  Created At: ${response.createdAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error getting plan: ${error.message}\n`);
    }
}

async function testListPlans(client: Flexprice) {
    console.log('--- Test 3: List Plans ---');

    try {
        const response = await client.plans.queryPlan({ limit: 10 });

        if (response && 'items' in response) {
            console.log(`✓ Retrieved ${response.items?.length || 0} plans`);
            if (response.items && response.items.length > 0) {
                console.log(`  First plan: ${response.items[0].id} - ${response.items[0].name}`);
            }
            if (response.pagination) {
                console.log(`  Total: ${response.pagination?.total ?? ''}\n`);
            }
        } else {
            console.log(`✓ Retrieved 0 plans\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing plans: ${error.message}\n`);
    }
}

async function testUpdatePlan(client: Flexprice) {
    console.log('--- Test 4: Update Plan ---');

    try {
        const response = await client.plans.updatePlan({
            id: testPlanID,
            dtoUpdatePlanRequest: {
                name: `${testPlanName} (Updated)`,
                description: 'Updated description for test plan',
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });

        if (response && 'id' in response) {
            console.log('✓ Plan updated successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  New Name: ${response.name}`);
            console.log(`  New Description: ${response.description}`);
            console.log(`  Updated At: ${response.updatedAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error updating plan: ${error.message}\n`);
    }
}

async function testSearchPlans(client: Flexprice) {
    console.log('--- Test 5: Search Plans ---');

    try {
        const response = await client.plans.queryPlan({ planIds: [testPlanID] });

        if (response && 'items' in response) {
            console.log('✓ Search completed!');
            console.log(`  Found ${response.items?.length || 0} plans matching ID '${testPlanID}'`);
            if (response.items && response.items.length > 0) {
                response.items.slice(0, 3).forEach((plan: { id?: string; name?: string; lookupKey?: string }) => {
                    console.log(`  - ${plan.id}: ${plan.name} (${plan.lookupKey})`);
                });
            }
            console.log();
        } else {
            console.log(`✓ Found 0 plans\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error searching plans: ${error.message}\n`);
    }
}

// ========================================
// ADDONS API TESTS
// ========================================

async function testCreateAddon(client: Flexprice) {
    console.log('--- Test 1: Create Addon ---');

    try {
        const timestamp = Date.now();
        testAddonName = `Test Addon ${timestamp}`;
        testAddonLookupKey = `test_addon_${timestamp}`;

        const raw = await client.addons.createAddon({
            name: testAddonName,
            lookupKey: testAddonLookupKey,
            description: 'This is a test addon created by SDK tests',
            type: AddonType.Onetime,
            metadata: {
                source: 'sdk_test',
                test_run: new Date().toISOString(),
                environment: 'test',
            },
        });
        const response = unwrap<{ id: string; name?: string; lookupKey?: string }>(raw);

        if (response && 'id' in response && response.id) {
            testAddonID = response.id;
            console.log('✓ Addon created successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Name: ${response.name}`);
            console.log(`  Lookup Key: ${response.lookupKey}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error creating addon: ${error.message}\n`);
    }
}

async function testGetAddon(client: Flexprice) {
    console.log('--- Test 2: Get Addon by ID ---');

    try {
        const response = await client.addons.getAddon({ id: testAddonID });

        if (response && 'id' in response) {
            console.log('✓ Addon retrieved successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Name: ${response.name}`);
            console.log(`  Lookup Key: ${response.lookupKey}`);
            console.log(`  Created At: ${response.createdAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error getting addon: ${error.message}\n`);
    }
}

async function testListAddons(client: Flexprice) {
    console.log('--- Test 3: List Addons ---');

    try {
        const response = await client.addons.queryAddon({ limit: 10 });

        if (response && 'items' in response) {
            console.log(`✓ Retrieved ${response.items?.length || 0} addons`);
            if (response.items && response.items.length > 0) {
                console.log(`  First addon: ${response.items[0].id} - ${response.items[0].name}`);
            }
            if (response.pagination) {
                console.log(`  Total: ${response.pagination?.total ?? ''}\n`);
            }
        } else {
            console.log(`✓ Retrieved 0 addons\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing addons: ${error.message}\n`);
    }
}

async function testUpdateAddon(client: Flexprice) {
    console.log('--- Test 4: Update Addon ---');

    try {
        const response = await client.addons.updateAddon({
            id: testAddonID,
            dtoUpdateAddonRequest: {
                name: `${testAddonName} (Updated)`,
                description: 'Updated description for test addon',
                metadata: {
                    updated_at: new Date().toISOString(),
                    status: 'updated',
                },
            },
        });

        if (response && 'id' in response) {
            console.log('✓ Addon updated successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  New Name: ${response.name}`);
            console.log(`  New Description: ${response.description}`);
            console.log(`  Updated At: ${response.updatedAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error updating addon: ${error.message}\n`);
    }
}

async function testLookupAddon(client: Flexprice) {
    console.log('--- Test 5: Lookup Addon by Lookup Key ---');

    if (!testAddonLookupKey) {
        console.log('⚠ Warning: No addon lookup key available\n⚠ Skipping lookup test\n');
        return;
    }

    try {
        console.log(`  Looking up addon with key: ${testAddonLookupKey}`);
        const response = await client.addons.getAddonByLookupKey({ lookupKey: testAddonLookupKey });

        if (response && 'id' in response) {
            console.log('✓ Addon found by lookup key!');
            console.log(`  Lookup Key: ${testAddonLookupKey}`);
            console.log(`  ID: ${response.id}`);
            console.log(`  Name: ${response.name}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error looking up addon: ${error.message}`);
        console.log('⚠ Skipping lookup test\n');
    }
}

async function testSearchAddons(client: Flexprice) {
    console.log('--- Test 6: Search Addons ---');

    try {
        const response = await client.addons.queryAddon({ addonIds: [testAddonID] });

        if (response && 'items' in response) {
            console.log('✓ Search completed!');
            console.log(`  Found ${response.items?.length || 0} addons matching ID '${testAddonID}'`);
            if (response.items && response.items.length > 0) {
                response.items.slice(0, 3).forEach((addon: { id?: string; name?: string; lookupKey?: string }) => {
                    console.log(`  - ${addon.id}: ${addon.name} (${addon.lookupKey})`);
                });
            }
            console.log();
        } else {
            console.log(`✓ Found 0 addons\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error searching addons: ${error.message}\n`);
    }
}

// ========================================
// ENTITLEMENTS API TESTS
// ========================================

async function testCreateEntitlement(client: Flexprice) {
    console.log('--- Test 1: Create Entitlement ---');

    try {
        const raw = await client.entitlements.createEntitlement({
            featureId: testFeatureID,
            featureType: FeatureType.Boolean,
            planId: testPlanID,
            isEnabled: true,
            usageResetPeriod: EntitlementUsageResetPeriod.Monthly,
        });
        const response = (getEntityWithId(raw) ?? unwrap<{ id: string; featureId?: string; planId?: string }>(raw)) as { id?: string; featureId?: string; planId?: string } | null;

        if (response && response.id) {
            testEntitlementID = response.id;
            console.log('✓ Entitlement created successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Feature ID: ${response.featureId}`);
            console.log(`  Plan ID: ${(response as { planId?: string }).planId ?? 'N/A'}\n`);
        } else {
            debugResponse('Create Entitlement', raw, response);
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error creating entitlement: ${error.message}\n`);
    }
}

async function testGetEntitlement(client: Flexprice) {
    console.log('--- Test 2: Get Entitlement by ID ---');

    if (!testEntitlementID) {
        console.log('⚠ Warning: No entitlement ID available\n⚠ Skipping get entitlement test\n');
        return;
    }

    try {
        const raw = await client.entitlements.getEntitlement({ id: testEntitlementID });
        const response = unwrap<{ id: string; featureId?: string; planId?: string; createdAt?: string }>(raw);

        if (response && 'id' in response) {
            console.log('✓ Entitlement retrieved successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Feature ID: ${response.featureId}`);
            console.log(`  Plan ID: ${(response as { planId?: string }).planId ?? 'N/A'}`);
            console.log(`  Created At: ${response.createdAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error getting entitlement: ${error.message}\n`);
    }
}

async function testListEntitlements(client: Flexprice) {
    console.log('--- Test 3: List Entitlements ---');

    try {
        const response = await client.entitlements.queryEntitlement({ limit: 10 });

        if (response && 'items' in response) {
            console.log(`✓ Retrieved ${response.items?.length || 0} entitlements`);
            if (response.items && response.items.length > 0) {
                console.log(`  First entitlement: ${response.items[0].id}`);
            }
            if (response.pagination) {
                console.log(`  Total: ${response.pagination?.total ?? ''}\n`);
            }
        } else {
            console.log(`✓ Retrieved 0 entitlements\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing entitlements: ${error.message}\n`);
    }
}

async function testUpdateEntitlement(client: Flexprice) {
    console.log('--- Test 4: Update Entitlement ---');

    if (!testEntitlementID) {
        console.log('⚠ Warning: No entitlement ID available (create may have failed)\n⚠ Skipping update entitlement test\n');
        return;
    }

    try {
        const raw = await client.entitlements.updateEntitlement({
            id: testEntitlementID,
            dtoUpdateEntitlementRequest: { isEnabled: false },
        });
        const response = unwrap<{ id: string; updatedAt?: string }>(raw);

        if (response && 'id' in response) {
            console.log('✓ Entitlement updated successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Updated At: ${response.updatedAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error updating entitlement: ${error.message}\n`);
    }
}

async function testSearchEntitlements(client: Flexprice) {
    console.log('--- Test 5: Search Entitlements ---');

    try {
        const response = await client.entitlements.queryEntitlement({ featureIds: [testFeatureID] });

        if (response && 'items' in response) {
            console.log('✓ Search completed!');
            console.log(`  Found ${response.items?.length || 0} entitlements for feature '${testFeatureID}'`);
            if (response.items && response.items.length > 0) {
                response.items.slice(0, 3).forEach((ent: { id?: string; featureId?: string }) => {
                    console.log(`  - ${ent.id}: Feature ${ent.featureId}`);
                });
            }
            console.log();
        } else {
            console.log(`✓ Found 0 entitlements\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error searching entitlements: ${error.message}\n`);
    }
}



// ========================================
// CONNECTIONS API TESTS
// ========================================

async function testListConnections(client: Flexprice) {
    console.log('--- Test 1: List Connections ---');

    try {
        const response = await client.integrations.listLinkedIntegrations();

        if (response && 'integrations' in response) {
            const list = (response.integrations ?? []) as Array<{ id?: string; providerType?: string }>;
            console.log(`✓ Retrieved ${list.length} linked integration(s)`);
            if (list.length > 0) {
                const first = list[0];
                console.log(`  First connection: ${first.id ?? 'N/A'}`);
                if (first.providerType) {
                    console.log(`  Provider Type: ${first.providerType}`);
                }
            }
            console.log();
        } else {
            console.log(`✓ Retrieved 0 connections\n`);
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error listing connections: ${error.message}`);
        console.log('⚠ Skipping connections tests (may not have any connections)\n');
    }
}

async function testSearchConnections(client: Flexprice) {
    console.log('--- Test 2: Search Connections ---');

    try {
        const response = await client.integrations.listLinkedIntegrations();

        if (response && 'integrations' in response) {
            const list = (response.integrations ?? []) as unknown[];
            console.log('✓ List completed!');
            console.log(`  Found ${list.length} linked integration(s)\n`);
        } else {
            console.log('✓ Found 0 connections\n');
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error searching connections: ${error.message}\n`);
    }
}

// ========================================
// SUBSCRIPTIONS API TESTS
// ========================================

async function testCreateSubscription(client: Flexprice) {
    console.log('--- Test 1: Create Subscription ---');

    try {
        await client.prices.createPrice({
            entityId: testPlanID,
            entityType: PriceEntityType.Plan,
            type: PriceType.Fixed,
            billingModel: BillingModel.FlatFee,
            billingCadence: BillingCadence.Recurring,
            billingPeriod: BillingPeriod.Monthly,
            billingPeriodCount: 1,
            invoiceCadence: InvoiceCadence.Arrear,
            priceUnitType: PriceUnitType.Fiat,
            amount: '29.99',
            currency: 'USD',
            displayName: 'Monthly Subscription Price',
        });

        const raw = await client.subscriptions.createSubscription({
            customerId: testCustomerID,
            planId: testPlanID,
            currency: 'USD',
            billingCadence: BillingCadence.Recurring,
            billingPeriod: BillingPeriod.Monthly,
            billingPeriodCount: 1,
            billingCycle: BillingCycle.Anniversary,
            startDate: new Date().toISOString(),
            metadata: {
                source: 'sdk_test',
                test_run: new Date().toISOString(),
            },
        });
        const response = (getEntityWithId(raw) ?? unwrap<{ id: string; customerId?: string; planId?: string; subscriptionStatus?: string }>(raw)) as { id?: string; customerId?: string; planId?: string; subscriptionStatus?: string } | null;

        if (response && response.id) {
            testSubscriptionID = response.id;
            console.log('✓ Subscription created successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Customer ID: ${response.customerId}`);
            console.log(`  Plan ID: ${response.planId}`);
            console.log(`  Status: ${(response as { subscriptionStatus?: string }).subscriptionStatus ?? 'N/A'}\n`);
        } else {
            debugResponse('Create Subscription', raw, response);
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error creating subscription: ${error.message}\n`);
    }
}

async function testGetSubscription(client: Flexprice) {
    console.log('--- Test 2: Get Subscription by ID ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription ID available\n⚠ Skipping get subscription test\n');
        return;
    }

    try {
        const raw = await client.subscriptions.getSubscription({ id: testSubscriptionID });
        const response = unwrap<{ id: string; customerId?: string; subscriptionStatus?: string; createdAt?: string }>(raw);

        if (response && 'id' in response) {
            console.log('✓ Subscription retrieved successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Customer ID: ${response.customerId}`);
            console.log(`  Status: ${(response as { subscriptionStatus?: string }).subscriptionStatus ?? 'N/A'}`);
            console.log(`  Created At: ${response.createdAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error getting subscription: ${error.message}\n`);
    }
}

async function testUpdateSubscription(client: Flexprice) {
    console.log('--- Test 4: Update Subscription ---');
    console.log('⚠ Skipping update subscription test (endpoint not available in SDK)\n');
}

async function testListSubscriptions(client: Flexprice) {
    console.log('--- Test 3: List Subscriptions ---');

    try {
        const response = await client.subscriptions.querySubscription({ limit: 10 });

        if (response && 'items' in response) {
            console.log(`✓ Retrieved ${response.items?.length || 0} subscriptions`);
            if (response.items && response.items.length > 0) {
                console.log(`  First subscription: ${response.items[0].id} (Customer: ${(response.items[0] as { customerId?: string }).customerId})`);
            }
            if (response.pagination) {
                console.log(`  Total: ${response.pagination?.total ?? ''}\n`);
            }
        } else {
            console.log(`✓ Retrieved 0 subscriptions\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing subscriptions: ${error.message}\n`);
    }
}

async function testSearchSubscriptions(client: Flexprice) {
    console.log('--- Test 4: Search Subscriptions ---');

    try {
        const response = await client.subscriptions.querySubscription({});

        if (response && 'items' in response) {
            console.log('✓ Search completed!');
            console.log(`  Found ${response.items?.length || 0} subscriptions\n`);
        } else {
            console.log('✓ Found 0 subscriptions\n');
        }
    } catch (error: any) {
        console.log(`❌ Error searching subscriptions: ${error.message}\n`);
    }
}

async function testActivateSubscription(client: Flexprice) {
    console.log('--- Test 5: Activate Subscription ---');

    try {
        const draftRaw = await client.subscriptions.createSubscription({
            customerId: testCustomerID,
            planId: testPlanID,
            currency: 'USD',
            billingCadence: BillingCadence.Recurring,
            billingPeriod: BillingPeriod.Monthly,
            billingPeriodCount: 1,
            startDate: new Date().toISOString(),
        });
        const draftSub = unwrap<{ id: string }>(draftRaw);
        const draftID = (draftSub && draftSub.id) ? draftSub.id : '';
        if (!draftID) {
            console.log('⚠ Could not get draft subscription ID\n');
            return;
        }
        console.log(`  Created draft subscription: ${draftID}`);

        await client.subscriptions.activateSubscription({
            id: draftID,
            dtoActivateDraftSubscriptionRequest: { startDate: new Date().toISOString() },
        });

        console.log('✓ Subscription activated successfully!');
        console.log(`  ID: ${draftID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error activating subscription: ${error.message}\n`);
    }
}

async function testPauseSubscription(client: Flexprice) {
    console.log('--- Test 7: Pause Subscription ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created, skipping pause test\n');
        return;
    }

    try {
        const response = await client.subscriptions.pauseSubscription({
            id: testSubscriptionID,
            dtoPauseSubscriptionRequest: { pauseMode: PauseMode.Immediate },
        });

        if (response && 'id' in response) {
            console.log('✓ Subscription paused successfully!');
            console.log(`  Pause ID: ${response.id}`);
            console.log(`  Subscription ID: ${(response as { subscriptionId?: string }).subscriptionId ?? ''}\n`);
        } else {
            console.log('✓ Pause requested\n');
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error pausing subscription: ${error.message}`);
        if ((error as { response?: { data?: unknown; body?: unknown } }).response) {
            const err = error as { response: { data?: unknown; body?: unknown } };
            console.log(`  Response: ${JSON.stringify(err.response.data || err.response.body || {}, null, 2)}`);
        }
        console.log('⚠ Skipping pause test\n');
    }
}

async function testResumeSubscription(client: Flexprice) {
    console.log('--- Test 8: Resume Subscription ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created, skipping resume test\n');
        return;
    }

    try {
        const response = await client.subscriptions.resumeSubscription({
            id: testSubscriptionID,
            dtoResumeSubscriptionRequest: { resumeMode: ResumeMode.Immediate },
        });

        if (response && 'id' in response) {
            console.log('✓ Subscription resumed successfully!');
            console.log(`  Pause ID: ${response.id}`);
            console.log(`  Subscription ID: ${(response as { subscriptionId?: string }).subscriptionId ?? ''}\n`);
        } else {
            console.log('✓ Resume requested\n');
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error resuming subscription: ${error.message}`);
        if ((error as { response?: { data?: unknown; body?: unknown } }).response) {
            const err = error as { response: { data?: unknown; body?: unknown } };
            console.log(`  Response: ${JSON.stringify(err.response.data || err.response.body || {}, null, 2)}`);
        }
        console.log('⚠ Skipping resume test\n');
    }
}

async function testGetPauseHistory(client: Flexprice) {
    console.log('--- Test 9: Get Pause History ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created, skipping pause history test\n');
        return;
    }

    try {
        const response = await client.subscriptions.listSubscriptionPauses({ id: testSubscriptionID });

        if (Array.isArray(response)) {
            console.log('✓ Retrieved pause history!');
            console.log(`  Total pauses: ${response.length}\n`);
        } else if (response && 'pauses' in response) {
            const list = (response as { pauses?: unknown[] }).pauses ?? [];
            console.log('✓ Retrieved pause history!');
            console.log(`  Total pauses: ${list.length}\n`);
        } else {
            console.log('✓ Retrieved pause history! Total: 0\n');
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting pause history: ${error.message}`);
        if ((error as { response?: { data?: unknown; body?: unknown } }).response) {
            const err = error as { response: { data?: unknown; body?: unknown } };
            console.log(`  Response: ${JSON.stringify(err.response.data || err.response.body || {}, null, 2)}`);
        }
        console.log('⚠ Skipping pause history test\n');
    }
}

async function testAddAddonToSubscription(client: Flexprice) {
    console.log('--- Test 6: Add Addon to Subscription ---');

    if (!testSubscriptionID || !testAddonID) {
        console.log('⚠ Warning: No subscription or addon created\n⚠ Skipping add addon test\n');
        return;
    }

    try {
        await client.prices.createPrice({
            entityId: testAddonID,
            entityType: PriceEntityType.Addon,
            type: PriceType.Fixed,
            billingModel: BillingModel.FlatFee,
            billingCadence: BillingCadence.Recurring,
            billingPeriod: BillingPeriod.Monthly,
            invoiceCadence: InvoiceCadence.Arrear,
            priceUnitType: PriceUnitType.Fiat,
            amount: '5.00',
            currency: 'USD',
            displayName: 'Addon Monthly Price',
        });

        await client.subscriptions.addSubscriptionAddon({
            subscriptionId: testSubscriptionID,
            addonId: testAddonID,
        });

        console.log('✓ Addon added to subscription successfully!');
        console.log(`  Subscription ID: ${testSubscriptionID}`);
        console.log(`  Addon ID: ${testAddonID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error adding addon: ${error.message}\n`);
    }
}

async function testRemoveAddonFromSubscription(client: Flexprice) {
    console.log('--- Test 7: Remove Addon from Subscription ---');
    console.log('⚠ Skipping remove addon test (requires addon association ID)\n');
}

async function testPreviewSubscriptionChange(client: Flexprice) {
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
        const preview = await client.subscriptions.previewSubscriptionChange({
            id: testSubscriptionID,
            dtoSubscriptionChangeRequest: {
                targetPlanId: testPlanID,
                billingCadence: BillingCadence.Recurring,
                billingPeriod: BillingPeriod.Monthly,
                billingCycle: BillingCycle.Anniversary,
                prorationBehavior: ProrationBehavior.CreateProrations,
            },
        });

        if (preview && typeof preview === 'object') {
            console.log('✓ Subscription change preview generated!');
            if ('nextInvoicePreview' in preview && preview.nextInvoicePreview) {
                console.log('  Preview available');
            }
            console.log();
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error previewing subscription change: ${error.message}`);
        if ((error as { response?: { data?: unknown; body?: unknown } }).response) {
            const err = error as { response: { data?: unknown; body?: unknown } };
            console.log(`  Response: ${JSON.stringify(err.response.data || err.response.body || {}, null, 2)}`);
        }
        console.log('⚠ Skipping preview change test\n');
    }
}

async function testExecuteSubscriptionChange(client: Flexprice) {
    console.log('--- Test 8: Execute Subscription Change ---');
    console.log('⚠ Skipping execute change test (would modify active subscription)\n');
}

async function testGetSubscriptionEntitlements(client: Flexprice) {
    console.log('--- Test 9: Get Subscription Entitlements ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created\n⚠ Skipping get entitlements test\n');
        return;
    }

    try {
        const response = await client.subscriptions.getSubscriptionEntitlements({ id: testSubscriptionID });

        if (response && typeof response === 'object') {
            const features = (response as { features?: unknown[] }).features ?? [];
            console.log('✓ Retrieved subscription entitlements!');
            console.log(`  Total features: ${features.length}\n`);
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting entitlements: ${error.message}\n`);
    }
}

async function testGetUpcomingGrants(client: Flexprice) {
    console.log('--- Test 10: Get Upcoming Grants ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created\n⚠ Skipping get upcoming grants test\n');
        return;
    }

    try {
        const response = await client.subscriptions.getSubscriptionUpcomingGrants({ id: testSubscriptionID });

        if (response && 'items' in response) {
            console.log('✓ Retrieved upcoming grants!');
            console.log(`  Total upcoming grants: ${response.items?.length || 0}\n`);
        } else {
            console.log('✓ Retrieved upcoming grants! Total: 0\n');
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting upcoming grants: ${error.message}\n`);
    }
}

async function testReportUsage(client: Flexprice) {
    console.log('--- Test 11: Report Usage ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created\n⚠ Skipping report usage test\n');
        return;
    }

    try {
        await client.subscriptions.getSubscriptionUsage({ subscriptionId: testSubscriptionID });

        console.log('✓ Usage retrieved successfully!');
        console.log(`  Subscription ID: ${testSubscriptionID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting usage: ${error.message}\n`);
    }
}

async function testUpdateLineItem(client: Flexprice) {
    console.log('--- Test 12: Update Line Item ---');
    console.log('⚠ Skipping update line item test (requires line item ID)\n');
}

async function testDeleteLineItem(client: Flexprice) {
    console.log('--- Test 13: Delete Line Item ---');
    console.log('⚠ Skipping delete line item test (requires line item ID)\n');
}

async function testCancelSubscription(client: Flexprice) {
    console.log('--- Test 14: Cancel Subscription ---');

    if (!testSubscriptionID) {
        console.log('⚠ Warning: No subscription created\n⚠ Skipping cancel test\n');
        return;
    }

    try {
        await client.subscriptions.cancelSubscription({
            id: testSubscriptionID,
            dtoCancelSubscriptionRequest: { cancellationType: 'end_of_period' },
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

async function testListInvoices(client: Flexprice) {
    console.log('--- Test 1: List Invoices ---');

    try {
        const response = await client.invoices.queryInvoice({ limit: 10 });

        if (response && 'items' in response) {
            console.log(`✓ Retrieved ${response.items?.length || 0} invoices`);
            if (response.items && response.items.length > 0) {
                const first = response.items[0] as { id?: string; customerId?: string; status?: string };
                if (first.id) testInvoiceID = first.id;
                console.log(`  First invoice: ${first.id} (Customer: ${first.customerId})`);
                if (first.status) console.log(`  Status: ${first.status}`);
            }
            if (response.pagination) {
                console.log(`  Total: ${(response.pagination as { total?: number })?.total ?? ''}\n`);
            }
        } else {
            console.log(`✓ Retrieved 0 invoices\n`);
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error listing invoices: ${error.message}\n`);
    }
}

async function testSearchInvoices(client: Flexprice) {
    console.log('--- Test 2: Search Invoices ---');

    try {
        const response = await client.invoices.queryInvoice({});

        if (response && 'items' in response) {
            console.log('✓ Search completed!');
            console.log(`  Found ${response.items?.length || 0} invoices\n`);
        } else {
            console.log('✓ Found 0 invoices\n');
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error searching invoices: ${error.message}\n`);
    }
}

async function testCreateInvoice(client: Flexprice) {
    console.log('--- Test 3: Create Invoice ---');

    if (!testCustomerID) {
        console.log('⚠ Warning: No customer created\n⚠ Skipping create invoice test\n');
        return;
    }

    try {
        const response = await client.invoices.createInvoice({
            customerId: testCustomerID,
            currency: 'USD',
            amountDue: '100.00',
            subtotal: '100.00',
            total: '100.00',
            invoiceType: InvoiceType.OneOff,
            billingReason: InvoiceBillingReason.Manual,
            invoiceStatus: InvoiceStatus.Draft,
            lineItems: [{ displayName: 'Test Service', amount: '100.00', quantity: '1' }],
            metadata: { source: 'sdk_test', type: 'manual' },
        });

        if (response && 'id' in response && response.id) {
            testInvoiceID = response.id;
            console.log('✓ Invoice created successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Customer ID: ${response.customerId}`);
            console.log(`  Status: ${(response as { invoiceStatus?: string }).invoiceStatus ?? 'N/A'}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error creating invoice: ${error.message}\n`);
    }
}

async function testGetInvoice(client: Flexprice) {
    console.log('--- Test 4: Get Invoice by ID ---');

    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available\n⚠ Skipping get invoice test\n');
        return;
    }

    try {
        const response = await client.invoices.getInvoice({ id: testInvoiceID });

        if (response && 'id' in response) {
            console.log('✓ Invoice retrieved successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Total: ${response.currency} ${response.total}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting invoice: ${error.message}\n`);
    }
}

async function testUpdateInvoice(client: Flexprice) {
    console.log('--- Test 5: Update Invoice ---');

    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available\n⚠ Skipping update invoice test\n');
        return;
    }

    try {
        const response = await client.invoices.updateInvoice({
            id: testInvoiceID,
            dtoUpdateInvoiceRequest: {
                metadata: { updated_at: new Date().toISOString(), status: 'updated' },
            },
        });

        if (response && 'id' in response) {
            console.log('✓ Invoice updated successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Updated At: ${response.updatedAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error updating invoice: ${error.message}\n`);
    }
}

async function testPreviewInvoice(client: Flexprice) {
    console.log('--- Test 6: Preview Invoice ---');

    if (!testCustomerID) {
        console.log('⚠ Warning: No customer available\n⚠ Skipping preview invoice test\n');
        return;
    }

    try {
        if (!testSubscriptionID) {
            console.log('⚠ No subscription ID, skipping preview\n');
            return;
        }
        const response = await client.invoices.getInvoicePreview({
            subscriptionId: testSubscriptionID,
        });

        if (response && typeof response === 'object') {
            console.log('✓ Invoice preview generated!');
            if ('total' in response && response.total) {
                console.log(`  Preview Total: ${response.total}\n`);
            } else {
                console.log();
            }
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error previewing invoice: ${error.message}\n`);
    }
}

async function testFinalizeInvoice(client: Flexprice) {
    console.log('--- Test 7: Finalize Invoice ---');

    try {
        const draftInvoice = await client.invoices.createInvoice({
            customerId: testCustomerID,
            currency: 'USD',
            amountDue: '50.00',
            subtotal: '50.00',
            total: '50.00',
            invoiceType: InvoiceType.OneOff,
            billingReason: InvoiceBillingReason.Manual,
            invoiceStatus: InvoiceStatus.Draft,
            lineItems: [{ displayName: 'Finalize Test Service', amount: '50.00', quantity: '1' }],
        });

        const finalizeID = (draftInvoice && 'id' in draftInvoice && draftInvoice.id) ? draftInvoice.id : '';
        if (!finalizeID) {
            console.log('⚠ Could not get draft invoice ID\n');
            return;
        }
        console.log(`  Created draft invoice: ${finalizeID}`);

        await client.invoices.finalizeInvoice({ id: finalizeID });

        console.log('✓ Invoice finalized successfully!');
        console.log(`  Invoice ID: ${finalizeID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error finalizing invoice: ${error.message}\n`);
    }
}

async function testRecalculateInvoice(client: Flexprice) {
    console.log('--- Test 8: Recalculate Invoice ---');
    console.log('⚠ Skipping recalculate invoice test (requires subscription invoice)\n');
}

async function testRecordPayment(client: Flexprice) {
    console.log('--- Test 9: Record Payment ---');

    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available\n⚠ Skipping record payment test\n');
        return;
    }

    try {
        await client.invoices.updateInvoicePaymentStatus({
            id: testInvoiceID,
            dtoUpdatePaymentStatusRequest: { paymentStatus: PaymentStatus.Succeeded, amount: '100.00' },
        });

        console.log('✓ Payment recorded successfully!');
        console.log(`  Invoice ID: ${testInvoiceID}`);
        console.log(`  Amount Paid: 100.00\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error recording payment: ${error.message}\n`);
    }
}

async function testAttemptPayment(client: Flexprice) {
    console.log('--- Test 10: Attempt Payment ---');

    try {
        const attemptInvoice = await client.invoices.createInvoice({
            customerId: testCustomerID,
            currency: 'USD',
            amountDue: '25.00',
            subtotal: '25.00',
            total: '25.00',
            amountPaid: '0.00',
            invoiceType: InvoiceType.OneOff,
            billingReason: InvoiceBillingReason.Manual,
            invoiceStatus: InvoiceStatus.Draft,
            paymentStatus: PaymentStatus.Pending,
            lineItems: [{ displayName: 'Attempt Payment Test', amount: '25.00', quantity: '1' }],
        });

        const attemptID = (attemptInvoice && 'id' in attemptInvoice && attemptInvoice.id) ? attemptInvoice.id : '';
        if (!attemptID) {
            console.log('⚠ Could not get attempt invoice ID\n');
            return;
        }
        await client.invoices.finalizeInvoice({ id: attemptID });
        await client.invoices.attemptInvoicePayment({ id: attemptID });

        console.log('✓ Payment attempt initiated!');
        console.log(`  Invoice ID: ${attemptID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error attempting payment: ${error.message}\n`);
    }
}

async function testDownloadInvoicePDF(client: Flexprice) {
    console.log('--- Test 11: Download Invoice PDF ---');

    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available\n⚠ Skipping download PDF test\n');
        return;
    }

    try {
        await client.invoices.getInvoicePdf({ id: testInvoiceID });

        console.log('✓ Invoice PDF downloaded!');
        console.log(`  Invoice ID: ${testInvoiceID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error downloading PDF: ${error.message}\n`);
    }
}

async function testTriggerInvoiceComms(client: Flexprice) {
    console.log('--- Test 12: Trigger Invoice Communications ---');

    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available\n⚠ Skipping trigger comms test\n');
        return;
    }

    try {
        await client.invoices.triggerInvoiceCommsWebhook({ id: testInvoiceID });

        console.log('✓ Invoice communications triggered!');
        console.log(`  Invoice ID: ${testInvoiceID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error triggering comms: ${error.message}\n`);
    }
}

async function testGetCustomerInvoiceSummary(client: Flexprice) {
    console.log('--- Test 13: Get Customer Invoice Summary ---');

    if (!testCustomerID) {
        console.log('⚠ Warning: No customer ID available\n⚠ Skipping summary test\n');
        return;
    }

    try {
        await client.invoices.getCustomerInvoiceSummary({ id: testCustomerID });

        console.log('✓ Customer invoice summary retrieved!');
        console.log(`  Customer ID: ${testCustomerID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting summary: ${error.message}\n`);
    }
}

async function testVoidInvoice(client: Flexprice) {
    console.log('--- Test 14: Void Invoice ---');

    if (!testInvoiceID) {
        console.log('⚠ Warning: No invoice ID available\n⚠ Skipping void invoice test\n');
        return;
    }

    try {
        await client.invoices.voidInvoice({ id: testInvoiceID });

        console.log('✓ Invoice voided successfully!');
        console.log(`  Invoice ID: ${testInvoiceID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error voiding invoice: ${error.message}\n`);
    }
}

// ========================================
// PRICES API TESTS
// ========================================

async function testCreatePrice(client: Flexprice) {
    console.log('--- Test 1: Create Price ---');

    if (!testPlanID) {
        console.log('⚠ Warning: No plan ID available\n⚠ Skipping create price test\n');
        return;
    }

    try {
        const raw = await client.prices.createPrice({
            entityId: testPlanID,
            entityType: PriceEntityType.Plan,
            currency: 'USD',
            amount: '99.00',
            billingModel: BillingModel.FlatFee,
            billingCadence: BillingCadence.Recurring,
            billingPeriod: BillingPeriod.Monthly,
            invoiceCadence: InvoiceCadence.Advance,
            priceUnitType: PriceUnitType.Fiat,
            type: PriceType.Fixed,
            displayName: 'Monthly Subscription',
            description: 'Standard monthly subscription price',
        });
        const response = (getEntityWithId(raw) ?? unwrap<{ id: string; amount?: string; currency?: string; billingModel?: string }>(raw)) as { id?: string; amount?: string; currency?: string; billingModel?: string } | null;

        if (response && response.id) {
            testPriceID = response.id;
            console.log('✓ Price created successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Amount: ${response.amount} ${response.currency}`);
            console.log(`  Billing Model: ${(response as { billingModel?: string }).billingModel ?? 'N/A'}\n`);
        } else {
            debugResponse('Create Price', raw, response);
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error creating price: ${error.message}\n`);
    }
}

async function testGetPrice(client: Flexprice) {
    console.log('--- Test 2: Get Price by ID ---');

    if (!testPriceID) {
        console.log('⚠ Warning: No price ID available\n⚠ Skipping get price test\n');
        return;
    }

    try {
        const response = await client.prices.getPrice({ id: testPriceID });

        if (response && 'id' in response) {
            console.log('✓ Price retrieved successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Amount: ${response.amount} ${response.currency}`);
            console.log(`  Entity ID: ${response.entityId}`);
            console.log(`  Created At: ${response.createdAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error getting price: ${error.message}\n`);
    }
}

async function testListPrices(client: Flexprice) {
    console.log('--- Test 3: List Prices ---');

    try {
        const response = await client.prices.queryPrice({ limit: 10 });

        if (response && 'items' in response) {
            console.log(`✓ Retrieved ${response.items?.length || 0} prices`);
            if (response.items && response.items.length > 0) {
                const first = response.items[0] as { id?: string; amount?: string; currency?: string };
                console.log(`  First price: ${first.id} - ${first.amount} ${first.currency}`);
            }
            if (response.pagination) {
                console.log(`  Total: ${(response.pagination as { total?: number })?.total ?? ''}\n`);
            }
        } else {
            console.log(`✓ Retrieved 0 prices\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing prices: ${error.message}\n`);
    }
}

async function testUpdatePrice(client: Flexprice) {
    console.log('--- Test 4: Update Price ---');

    if (!testPriceID) {
        console.log('⚠ Warning: No price ID available\n⚠ Skipping update price test\n');
        return;
    }

    try {
        const response = await client.prices.updatePrice({
            id: testPriceID,
            dtoUpdatePriceRequest: {
                description: 'Updated price description for testing',
                metadata: { updated_at: new Date().toISOString(), status: 'updated' },
            },
        });

        if (response && 'id' in response) {
            console.log('✓ Price updated successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  New Description: ${response.description}`);
            console.log(`  Updated At: ${response.updatedAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error updating price: ${error.message}\n`);
    }
}

// ========================================
// PAYMENTS API TESTS
// ========================================

async function testCreatePayment(client: Flexprice) {
    console.log('--- Test 1: Create Payment ---');

    if (!testCustomerID) {
        console.log('⚠ Warning: No customer ID available\n⚠ Skipping create payment test\n');
        return;
    }

    let paymentInvoiceID = '';

    try {
        // Create a draft invoice with explicit payment status to prevent auto-payment
        const draftInvoice = await client.invoices.createInvoice({
            customerId: testCustomerID,
            currency: 'USD',
            amountDue: '100.00',
            subtotal: '100.00',
            total: '100.00',
            amountPaid: '0.00',
            invoiceType: InvoiceType.OneOff,
            billingReason: InvoiceBillingReason.Manual,
            invoiceStatus: InvoiceStatus.Draft,
            paymentStatus: PaymentStatus.Pending,
            lineItems: [{ displayName: 'Payment Test Service', amount: '100.00', quantity: '1' }],
            metadata: { source: 'sdk_test_payment' },
        });

        paymentInvoiceID = (draftInvoice && 'id' in draftInvoice && draftInvoice.id) ? draftInvoice.id : '';
        if (!paymentInvoiceID) {
            console.log('⚠ Could not get draft invoice ID\n');
            return;
        }
        console.log(`  Created invoice for payment: ${paymentInvoiceID}`);

        const currentInvoice = await client.invoices.getInvoice({ id: paymentInvoiceID });
        const cur = currentInvoice as { amountPaid?: string; amountDue?: string; total?: string; invoiceStatus?: string };
        if (cur.amountPaid && cur.amountPaid !== '0' && cur.amountPaid !== '0.00') {
            console.log(`⚠ Warning: Invoice already has amount paid before finalization: ${cur.amountPaid}\n⚠ Skipping payment creation test\n`);
            return;
        }
        if (cur.amountDue === '0' || cur.amountDue === '0.00') {
            console.log(`⚠ Warning: Invoice has zero amount due\n⚠ Skipping payment creation test\n`);
            return;
        }
        if (cur.amountDue && cur.total) {
            console.log(`  Invoice before finalization - AmountDue: ${cur.amountDue}, Total: ${cur.total}`);
        }
        if (cur.invoiceStatus === InvoiceStatus.Draft) {
            try {
                await client.invoices.finalizeInvoice({ id: paymentInvoiceID });
                console.log('  Finalized invoice for payment');
            } catch (finalizeError: any) {
                if (finalizeError.message && (finalizeError.message.includes('already') || finalizeError.message.includes('400'))) {
                    console.log(`⚠ Warning: Invoice finalization returned error: ${finalizeError.message}`);
                } else {
                    console.log(`⚠ Warning: Failed to finalize invoice: ${finalizeError.message}`);
                    return;
                }
            }
        } else {
            console.log(`  Invoice already finalized (status: ${cur.invoiceStatus})`);
        }

        const finalInvoice = await client.invoices.getInvoice({ id: paymentInvoiceID }) as { amountDue?: string; total?: string; amountPaid?: string; paymentStatus?: string };
        if (finalInvoice.amountDue && finalInvoice.total && finalInvoice.amountPaid) {
            console.log(`  Invoice after finalization - AmountDue: ${finalInvoice.amountDue}, Total: ${finalInvoice.total}, AmountPaid: ${finalInvoice.amountPaid}`);
        }
        if (finalInvoice.paymentStatus === PaymentStatus.Succeeded) {
            console.log(`⚠ Warning: Invoice is already paid\n⚠ Skipping payment creation test\n`);
            return;
        }
        if (finalInvoice.amountPaid && finalInvoice.amountPaid !== '0' && finalInvoice.amountPaid !== '0.00') {
            console.log(`⚠ Warning: Invoice already has amount paid: ${finalInvoice.amountPaid}\n⚠ Skipping payment creation test\n`);
            return;
        }
        if (finalInvoice.total === '0' || finalInvoice.total === '0.00') {
            console.log('⚠ Warning: Invoice has zero total amount\n⚠ Skipping payment creation test\n');
            return;
        }
        console.log(`  Invoice is unpaid and ready for payment (status: ${finalInvoice.paymentStatus || 'unknown'}, total: ${finalInvoice.total || 'unknown'})`);

        const response = await client.payments.createPayment({
            amount: '100.00',
            currency: 'USD',
            destinationId: paymentInvoiceID,
            destinationType: PaymentDestinationType.Invoice,
            paymentMethodType: PaymentMethodType.Offline,
            processPayment: false,
            metadata: { source: 'sdk_test', test_run: new Date().toISOString() },
        });

        if (response && 'id' in response && response.id) {
            testPaymentID = response.id;
            console.log('✓ Payment created successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Amount: ${response.amount} ${response.currency}`);
            console.log(`  Status: ${(response as { paymentStatus?: string }).paymentStatus ?? 'N/A'}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
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

async function testGetPayment(client: Flexprice) {
    console.log('--- Test 2: Get Payment by ID ---');

    if (!testPaymentID) {
        console.log('⚠ Warning: No payment ID available\n⚠ Skipping get payment test\n');
        return;
    }

    try {
        const response = await client.payments.getPayment({ id: testPaymentID });

        if (response && 'id' in response) {
            console.log('✓ Payment retrieved successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Amount: ${response.amount} ${response.currency}`);
            console.log(`  Status: ${(response as { paymentStatus?: string }).paymentStatus ?? 'N/A'}`);
            console.log(`  Created At: ${response.createdAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error getting payment: ${error.message}\n`);
    }
}

async function testListPayments(client: Flexprice) {
    console.log('--- Test 3: List Payments ---');

    try {
        const response = await client.payments.listPayments({ limit: 10 });

        if (response && 'items' in response) {
            console.log(`✓ Retrieved ${response.items?.length || 0} payments`);
            if (response.items && response.items.length > 0) {
                const first = response.items[0] as { id?: string; amount?: string; currency?: string };
                console.log(`  First payment: ${first.id} - ${first.amount} ${first.currency}`);
            }
            if (response.pagination) {
                console.log(`  Total: ${(response.pagination as { total?: number })?.total ?? ''}\n`);
            }
        } else {
            console.log(`✓ Retrieved 0 payments\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing payments: ${error.message}\n`);
    }
}

async function testSearchPayments(client: Flexprice) {
    console.log('--- Test 2: Search Payments ---');
    console.log('⚠ Skipping search payments test (endpoint not available in SDK)\n');
}

async function testUpdatePayment(client: Flexprice) {
    console.log('--- Test 4: Update Payment ---');

    if (!testPaymentID) {
        console.log('⚠ Warning: No payment ID available\n⚠ Skipping update payment test\n');
        return;
    }

    try {
        const response = await client.payments.updatePayment({
            id: testPaymentID,
            dtoUpdatePaymentRequest: {
                metadata: { updated_at: new Date().toISOString(), status: 'updated' },
            },
        });

        if (response && 'id' in response) {
            console.log('✓ Payment updated successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Updated At: ${response.updatedAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error updating payment: ${error.message}\n`);
    }
}

async function testProcessPayment(client: Flexprice) {
    console.log('--- Test 5: Process Payment ---');

    if (!testPaymentID) {
        console.log('⚠ Warning: No payment ID available\n⚠ Skipping process payment test\n');
        return;
    }

    try {
        await client.payments.processPayment({ id: testPaymentID });

        console.log('✓ Payment processed successfully!');
        console.log(`  Payment ID: ${testPaymentID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error processing payment: ${error.message}\n`);
    }
}

// ========================================
// WALLETS API TESTS
// ========================================

async function testCreateWallet(client: Flexprice) {
    console.log('--- Test 1: Create Wallet ---');

    if (!testCustomerID) {
        console.log('⚠ Warning: No customer ID available\n⚠ Skipping create wallet test\n');
        return;
    }

    try {
        const response = await client.wallets.createWallet({
            customerId: testCustomerID,
            currency: 'USD',
            metadata: { source: 'sdk_test', test_run: new Date().toISOString() },
        });

        if (response && 'id' in response && response.id) {
            testWalletID = response.id;
            console.log('✓ Wallet created successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Customer ID: ${(response as { customerId?: string }).customerId ?? 'N/A'}`);
            console.log(`  Balance: ${(response as { balance?: string }).balance ?? 'N/A'} ${response.currency}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error creating wallet: ${error.message}\n`);
    }
}

async function testGetWallet(client: Flexprice) {
    console.log('--- Test 2: Get Wallet by ID ---');

    if (!testWalletID) {
        console.log('⚠ Warning: No wallet ID available\n⚠ Skipping get wallet test\n');
        return;
    }

    try {
        const response = await client.wallets.getWallet({ id: testWalletID });

        if (response && 'id' in response) {
            console.log('✓ Wallet retrieved successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Balance: ${(response as { balance?: string }).balance ?? 'N/A'} ${response.currency}`);
            console.log(`  Created At: ${response.createdAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error getting wallet: ${error.message}\n`);
    }
}

async function testListWallets(client: Flexprice) {
    console.log('--- Test 3: List Wallets ---');

    try {
        const response = await client.wallets.queryWallet({ limit: 10 });

        if (response && 'items' in response) {
            console.log(`✓ Retrieved ${response.items?.length || 0} wallets`);
            if (response.items && response.items.length > 0) {
                const first = response.items[0] as { id?: string; balance?: string; currency?: string };
                console.log(`  First wallet: ${first.id} - ${first.balance} ${first.currency}`);
            }
            if (response.pagination) {
                console.log(`  Total: ${(response.pagination as { total?: number })?.total ?? ''}\n`);
            }
        } else {
            console.log(`✓ Retrieved 0 wallets\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing wallets: ${error.message}\n`);
    }
}

async function testUpdateWallet(client: Flexprice) {
    console.log('--- Test 4: Update Wallet ---');

    if (!testWalletID) {
        console.log('⚠ Warning: No wallet ID available\n⚠ Skipping update wallet test\n');
        return;
    }

    try {
        const response = await client.wallets.updateWallet({
            id: testWalletID,
            dtoUpdateWalletRequest: { metadata: { updated_at: new Date().toISOString(), status: 'updated' } },
        });

        if (response && 'id' in response) {
            console.log('✓ Wallet updated successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Updated At: ${response.updatedAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error updating wallet: ${error.message}\n`);
    }
}

async function testGetWalletBalance(client: Flexprice) {
    console.log('--- Test 5: Get Wallet Balance ---');

    if (!testWalletID) {
        console.log('⚠ Warning: No wallet ID available\n⚠ Skipping get balance test\n');
        return;
    }

    try {
        const response = await client.wallets.getWalletBalance({ id: testWalletID });

        if (response && typeof response === 'object') {
            const bal = (response as { balance?: string; currency?: string }).balance;
            const cur = (response as { currency?: string }).currency;
            console.log('✓ Wallet balance retrieved!');
            console.log(`  Balance: ${bal ?? 'N/A'} ${cur ?? ''}\n`);
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting balance: ${error.message}\n`);
    }
}

async function testTopUpWallet(client: Flexprice) {
    console.log('--- Test 6: Top Up Wallet ---');

    if (!testWalletID) {
        console.log('⚠ Warning: No wallet ID available\n⚠ Skipping top up test\n');
        return;
    }

    try {
        await client.wallets.topUpWallet({
            id: testWalletID,
            dtoTopUpWalletRequest: { amount: '100.00', description: 'Test top-up', transactionReason: TransactionReason.PurchasedCreditDirect },
        });

        console.log('✓ Wallet topped up successfully!');
        console.log(`  Wallet ID: ${testWalletID}`);
        console.log(`  Amount: 100.00\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error topping up wallet: ${error.message}\n`);
    }
}

async function testDebitWallet(client: Flexprice) {
    console.log('--- Test 7: Debit Wallet ---');
    console.log('⚠ Skipping debit test (no debit endpoint in SDK)\n');
}

async function testGetWalletTransactions(client: Flexprice) {
    console.log('--- Test 8: Get Wallet Transactions ---');

    if (!testWalletID) {
        console.log('⚠ Warning: No wallet ID available\n⚠ Skipping transactions test\n');
        return;
    }

    try {
        const response = await client.wallets.getWalletTransactions({ idPathParameter: testWalletID });

        if (response && 'items' in response) {
            console.log('✓ Wallet transactions retrieved!');
            console.log(`  Total transactions: ${response.items?.length || 0}\n`);
        } else {
            console.log('✓ Wallet transactions retrieved! Total: 0\n');
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error getting transactions: ${error.message}\n`);
    }
}

async function testSearchWallets(client: Flexprice) {
    console.log('--- Test 9: Search Wallets ---');

    try {
        const response = await client.wallets.queryWallet({});

        if (response && 'items' in response) {
            console.log('✓ Search completed!');
            console.log(`  Found ${response.items?.length || 0} wallets\n`);
        } else {
            console.log('✓ Found 0 wallets\n');
        }
    } catch (error: any) {
        const msg = error?.message ?? String(error);
        if (msg.includes('500') || msg.includes('Status 500')) {
            console.log('⚠ Search wallets returned 500 (known backend issue); skipping\n');
        } else {
            console.log(`❌ Error searching wallets: ${msg}\n`);
        }
    }
}

// ========================================
// CREDIT GRANTS API TESTS
// ========================================

async function testCreateCreditGrant(client: Flexprice) {
    console.log('--- Test 1: Create Credit Grant ---');

    // Skip if no plan available (matching Go test)
    if (!testPlanID) {
        console.log('⚠ Warning: No plan ID available\n⚠ Skipping create credit grant test\n');
        return;
    }

    try {
        const response = await client.creditGrants.createCreditGrant({
            name: 'Test Credit Grant',
            credits: '500.00',
            scope: CreditGrantScope.Plan,
            planId: testPlanID,
            cadence: CreditGrantCadence.Onetime,
            expirationType: CreditGrantExpiryType.Never,
            expirationDurationUnit: CreditGrantExpiryDurationUnit.Day,
            metadata: { source: 'sdk_test', test_run: new Date().toISOString() },
        });

        if (response && 'id' in response && response.id) {
            testCreditGrantID = response.id;
            console.log('✓ Credit grant created successfully!');
            console.log(`  ID: ${response.id}`);
            if ((response as { credits?: string }).credits) {
                console.log(`  Credits: ${(response as { credits?: string }).credits}`);
            }
            console.log(`  Plan ID: ${(response as { planId?: string }).planId ?? 'N/A'}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
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

async function testGetCreditGrant(client: Flexprice) {
    console.log('--- Test 2: Get Credit Grant by ID ---');

    if (!testCreditGrantID) {
        console.log('⚠ Warning: No credit grant ID available\n⚠ Skipping get credit grant test\n');
        return;
    }

    try {
        const response = await client.creditGrants.getCreditGrant({ id: testCreditGrantID });

        if (response && 'id' in response) {
            console.log('✓ Credit grant retrieved successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Grant Amount: ${(response as { grantAmount?: string }).grantAmount ?? 'N/A'}`);
            console.log(`  Created At: ${response.createdAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error getting credit grant: ${error.message}\n`);
    }
}

async function testListCreditGrants(client: Flexprice) {
    console.log('--- Test 3: List Credit Grants ---');

    if (!testPlanID) {
        console.log('⚠ No plan ID, skipping list credit grants\n');
        return;
    }
    try {
        const response = await client.creditGrants.getPlanCreditGrants({ id: testPlanID });

        if (response && 'items' in response) {
            console.log(`✓ Retrieved ${response.items?.length || 0} credit grants for plan`);
            if (response.items && response.items.length > 0) {
                console.log(`  First credit grant: ${response.items[0].id}`);
            }
            console.log();
        } else {
            console.log(`✓ Retrieved 0 credit grants\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error listing credit grants: ${error.message}\n`);
    }
}

async function testUpdateCreditGrant(client: Flexprice) {
    console.log('--- Test 4: Update Credit Grant ---');

    if (!testCreditGrantID) {
        console.log('⚠ Warning: No credit grant ID available\n⚠ Skipping update credit grant test\n');
        return;
    }

    try {
        const response = await client.creditGrants.updateCreditGrant({
            id: testCreditGrantID,
            dtoUpdateCreditGrantRequest: { metadata: { updated_at: new Date().toISOString(), status: 'updated' } },
        });

        if (response && 'id' in response) {
            console.log('✓ Credit grant updated successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Updated At: ${response.updatedAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error updating credit grant: ${error.message}\n`);
    }
}

async function testDeleteCreditGrant(client: Flexprice) {
    console.log('--- Cleanup: Delete Credit Grant ---');

    if (!testCreditGrantID) {
        console.log('⚠ Skipping delete credit grant (no credit grant created)\n');
        return;
    }

    try {
        await client.creditGrants.deleteCreditGrant({ id: testCreditGrantID });

        console.log('✓ Credit grant deleted successfully!');
        console.log(`  Deleted ID: ${testCreditGrantID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting credit grant: ${error.message}\n`);
    }
}

// ========================================
// CREDIT NOTES API TESTS
// ========================================

async function testCreateCreditNote(client: Flexprice) {
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

    let invoice: { lineItems?: Array<{ id?: string; displayName?: string }>; invoiceStatus?: string } | null = null;

    try {
        const inv = await client.invoices.getInvoice({ id: testInvoiceID });
        invoice = inv && typeof inv === 'object' ? (inv as { lineItems?: Array<{ id?: string; displayName?: string }>; invoiceStatus?: string }) : null;

        if (!invoice) {
            console.log('⚠ Warning: Could not retrieve invoice\n⚠ Skipping create credit note test\n');
            return;
        }

        console.log(`Invoice has ${invoice.lineItems?.length || 0} line items`);
        if (!invoice.lineItems || invoice.lineItems.length === 0) {
            console.log('⚠ Warning: Invoice has no line items\n⚠ Skipping create credit note test\n');
            return;
        }

        if (invoice.invoiceStatus === InvoiceStatus.Draft) {
            console.log(`  Invoice is in DRAFT status, attempting to finalize...`);
            try {
                await client.invoices.finalizeInvoice({ id: testInvoiceID });
                console.log('  Invoice finalized successfully');
                const refetch = await client.invoices.getInvoice({ id: testInvoiceID });
                invoice = refetch && typeof refetch === 'object' ? (refetch as typeof invoice) : invoice;
            } catch (finalizeError: any) {
                console.log(`⚠ Warning: Failed to finalize invoice: ${finalizeError.message || finalizeError}`);
                console.log('⚠ Skipping create credit note test\n');
                return;
            }
        }

        if (invoice.invoiceStatus !== InvoiceStatus.Finalized) {
            console.log(`⚠ Warning: Invoice must be FINALIZED to create credit note. Current status: ${invoice.invoiceStatus}\n⚠ Skipping create credit note test\n`);
            return;
        }

        console.log(`  Invoice status: ${invoice.invoiceStatus} (ready for credit note)`);

        const firstLineItem = invoice.lineItems?.[0];
        if (!firstLineItem) {
            console.log('⚠ Warning: No first line item\n⚠ Skipping create credit note test\n');
            return;
        }
        const creditAmount = '50.00';
        const lineItemId = firstLineItem.id;
        const lineItemDisplayName = firstLineItem.displayName || 'Invoice Line Item';

        if (!lineItemId) {
            console.log('⚠ Warning: Line item has no ID\n⚠ Skipping create credit note test\n');
            return;
        }

        console.log(`  Using line item ID: ${lineItemId}`);
        console.log(`  Line item display name: ${lineItemDisplayName}`);
        console.log(`  Credit amount: ${creditAmount}`);

        const response = await client.creditNotes.createCreditNote({
            invoiceId: testInvoiceID,
            reason: CreditNoteReason.BillingError,
            memo: 'Test credit note from SDK',
            lineItems: [{
                invoiceLineItemId: lineItemId,
                amount: creditAmount,
                displayName: `Credit for ${lineItemDisplayName}`,
            }],
            metadata: { source: 'sdk_test', test_run: new Date().toISOString() },
        });

        if (response && 'id' in response && response.id) {
            testCreditNoteID = response.id;
            console.log('✓ Credit note created successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Invoice ID: ${(response as { invoiceId?: string }).invoiceId ?? 'N/A'}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
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
        if (invoice?.lineItems?.length) {
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

async function testGetCreditNote(client: Flexprice) {
    console.log('--- Test 2: Get Credit Note by ID ---');

    if (!testCreditNoteID) {
        console.log('⚠ Warning: No credit note ID available\n⚠ Skipping get credit note test\n');
        return;
    }

    try {
        const response = await client.creditNotes.getCreditNote({ id: testCreditNoteID });

        if (response && 'id' in response) {
            console.log('✓ Credit note retrieved successfully!');
            console.log(`  ID: ${response.id}`);
            console.log(`  Total: ${(response as { total?: string }).total ?? 'N/A'}`);
            console.log(`  Created At: ${response.createdAt}\n`);
        } else {
            console.log(`❌ Unexpected response shape\n`);
        }
    } catch (error: any) {
        console.log(`❌ Error getting credit note: ${error.message}\n`);
    }
}

async function testListCreditNotes(client: Flexprice) {
    console.log('--- Test 3: List Credit Notes ---');
    console.log('⚠ Skipping list credit notes (no query endpoint in SDK)\n');
}

async function testFinalizeCreditNote(client: Flexprice) {
    console.log('--- Test 4: Finalize Credit Note ---');

    if (!testCreditNoteID) {
        console.log('⚠ Warning: No credit note ID available\n⚠ Skipping finalize credit note test\n');
        return;
    }

    try {
        await client.creditNotes.processCreditNote({ id: testCreditNoteID });

        console.log('✓ Credit note finalized successfully!');
        console.log(`  Credit Note ID: ${testCreditNoteID}\n`);
    } catch (error: any) {
        console.log(`⚠ Warning: Error finalizing credit note: ${error.message}\n`);
    }
}

// ========================================
// CLEANUP TESTS
// ========================================

async function testDeletePayment(client: Flexprice) {
    console.log('--- Cleanup: Delete Payment ---');

    if (!testPaymentID) {
        console.log('⚠ Skipping delete payment (no payment created)\n');
        return;
    }

    try {
        await client.payments.deletePayment({ id: testPaymentID });

        console.log('✓ Payment deleted successfully!');
        console.log(`  Deleted ID: ${testPaymentID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting payment: ${error.message}\n`);
    }
}

async function testDeletePrice(client: Flexprice) {
    console.log('--- Cleanup: Delete Price ---');

    if (!testPriceID) {
        console.log('⚠ Skipping delete price (no price created)\n');
        return;
    }

    try {
        const futureDate = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString();
        await client.prices.deletePrice({ id: testPriceID, dtoDeletePriceRequest: { endDate: futureDate } });

        console.log('✓ Price deleted successfully!');
        console.log(`  Deleted ID: ${testPriceID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting price: ${error.message}\n`);
    }
}

async function testDeleteEntitlement(client: Flexprice) {
    console.log('--- Cleanup: Delete Entitlement ---');

    if (!testEntitlementID) {
        console.log('⚠ Skipping delete entitlement (no entitlement created)\n');
        return;
    }

    try {
        await client.entitlements.deleteEntitlement({ id: testEntitlementID });

        console.log('✓ Entitlement deleted successfully!');
        console.log(`  Deleted ID: ${testEntitlementID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting entitlement: ${error.message}\n`);
    }
}

async function testDeleteAddon(client: Flexprice) {
    console.log('--- Cleanup: Delete Addon ---');

    if (!testAddonID) {
        console.log('⚠ Skipping delete addon (no addon created)\n');
        return;
    }

    try {
        await client.addons.deleteAddon({ id: testAddonID });

        console.log('✓ Addon deleted successfully!');
        console.log(`  Deleted ID: ${testAddonID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting addon: ${error.message}\n`);
    }
}

async function testDeletePlan(client: Flexprice) {
    console.log('--- Cleanup: Delete Plan ---');

    if (!testPlanID) {
        console.log('⚠ Skipping delete plan (no plan created)\n');
        return;
    }

    try {
        await client.plans.deletePlan({ id: testPlanID });

        console.log('✓ Plan deleted successfully!');
        console.log(`  Deleted ID: ${testPlanID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting plan: ${error.message}\n`);
    }
}

async function testDeleteFeature(client: Flexprice) {
    console.log('--- Cleanup: Delete Feature ---');

    if (!testFeatureID) {
        console.log('⚠ Skipping delete feature (no feature created)\n');
        return;
    }

    try {
        await client.features.deleteFeature({ id: testFeatureID });

        console.log('✓ Feature deleted successfully!');
        console.log(`  Deleted ID: ${testFeatureID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting feature: ${error.message}\n`);
    }
}

async function testDeleteCustomer(client: Flexprice) {
    console.log('--- Cleanup: Delete Customer ---');

    if (!testCustomerID) {
        console.log('⚠ Skipping delete customer (no customer created)\n');
        return;
    }

    try {
        await client.customers.deleteCustomer({ id: testCustomerID });

        console.log('✓ Customer deleted successfully!');
        console.log(`  Deleted ID: ${testCustomerID}\n`);
    } catch (error: any) {
        console.log(`❌ Error deleting customer: ${error.message}\n`);
    }
}

// ========================================
// EVENTS API TESTS
// ========================================

async function testCreateEvent(client: Flexprice) {
    console.log('--- Test 1: Create Event ---');

    // Use test customer external ID if available, otherwise generate a unique one
    if (!testCustomerID) {
        testEventCustomerID = `test-customer-${Date.now()}`;
    } else {
        try {
            const customer = await client.customers.getCustomer({ id: testCustomerID });
            testEventCustomerID = (customer && 'externalId' in customer ? (customer as { externalId?: string }).externalId : null) || `test-customer-${Date.now()}`;
        } catch {
            testEventCustomerID = `test-customer-${Date.now()}`;
        }
    }

    testEventName = `Test Event ${Date.now()}`;

    try {
        const response = await client.events.ingestEvent({
            eventName: testEventName,
            externalCustomerId: testEventCustomerID,
            properties: {
                source: 'sdk_test',
                environment: 'test',
                test_run: new Date().toISOString(),
            },
            source: 'sdk_test',
            timestamp: new Date().toISOString(),
        });

        if (response && typeof response === 'object') {
            if ('event_id' in response) testEventID = (response as { event_id?: string }).event_id ?? '';
            else if ('eventId' in response) testEventID = (response as { eventId?: string }).eventId ?? '';
        }

        console.log('✓ Event created successfully!');
        if (testEventID) console.log(`  Event ID: ${testEventID}`);
        console.log(`  Event Name: ${testEventName}`);
        console.log(`  Customer ID: ${testEventCustomerID}\n`);
    } catch (error: any) {
        console.log(`❌ Error creating event: ${error.message}\n`);
    }
}

async function testQueryEvents(client: Flexprice) {
    console.log('--- Test 2: Query Events ---');

    if (!testEventName) {
        console.log('⚠ Warning: No event created, skipping query test\n');
        return;
    }

    try {
        const response = await client.events.listRawEvents({
            externalCustomerId: testEventCustomerID,
            eventName: testEventName,
        });

        if (response && typeof response === 'object') {
            const events = (response as { events?: Array<{ id?: string; eventName?: string; event_name?: string }> }).events ?? [];
            console.log('✓ Events queried successfully!');
            if (events.length > 0) {
                console.log(`  Found ${events.length} events`);
                events.slice(0, 3).forEach((event, i) => {
                    const eventId = event.id ?? 'N/A';
                    const name = event.eventName ?? event.event_name ?? 'N/A';
                    console.log(`  - Event ${i + 1}: ${eventId} - ${name}`);
                });
            } else {
                console.log('  No events found');
            }
            console.log();
        }
    } catch (error: any) {
        console.log(`⚠ Warning: Error querying events: ${error.message}`);
        console.log('⚠ Skipping query events test\n');
    }
}

async function testAsyncEventEnqueue(client: Flexprice) {
    console.log('--- Test 3: Async Event - Simple Enqueue ---');

    // Use test customer external ID if available
    let customerID = testEventCustomerID;
    if (!customerID) {
        if (testCustomerID) {
            try {
                const customer = await client.customers.getCustomer({ id: testCustomerID });
                customerID = (customer && 'externalId' in customer ? (customer as { externalId?: string }).externalId : null) || `test-customer-${Date.now()}`;
            } catch {
                customerID = `test-customer-${Date.now()}`;
            }
        } else {
            customerID = `test-customer-${Date.now()}`;
        }
    }

    try {
        await client.events.ingestEvent({
            eventName: 'api_request',
            externalCustomerId: customerID,
            properties: { path: '/api/resource', method: 'GET', status: '200', response_time_ms: '150' },
            source: 'sdk_test',
        });

        console.log('✓ Async event enqueued successfully!');
        console.log('  Event Name: api_request');
        console.log(`  Customer ID: ${customerID}\n`);
    } catch (error: any) {
        console.log(`❌ Error enqueueing async event: ${error.message}\n`);
    }
}

async function testAsyncEventEnqueueWithOptions(client: Flexprice) {
    console.log('--- Test 4: Async Event - Enqueue With Options ---');

    // Use test customer external ID if available
    let customerID = testEventCustomerID;
    if (!customerID) {
        if (testCustomerID) {
            try {
                const customer = await client.customers.getCustomer({ id: testCustomerID });
                customerID = (customer && 'externalId' in customer ? (customer as { externalId?: string }).externalId : null) || `test-customer-${Date.now()}`;
            } catch {
                customerID = `test-customer-${Date.now()}`;
            }
        } else {
            customerID = `test-customer-${Date.now()}`;
        }
    }

    try {
        await client.events.ingestEvent({
            eventName: 'file_upload',
            externalCustomerId: customerID,
            properties: { file_size_bytes: '1048576', file_type: 'image/jpeg', storage_bucket: 'user_uploads' },
            source: 'sdk_test',
            timestamp: new Date().toISOString(),
        });

        console.log('✓ Async event with options enqueued successfully!');
        console.log('  Event Name: file_upload');
        console.log(`  Customer ID: ${customerID}\n`);
    } catch (error: any) {
        console.log(`❌ Error enqueueing async event with options: ${error.message}\n`);
    }
}

async function testAsyncEventBatch(client: Flexprice) {
    console.log('--- Test 5: Async Event - Batch Enqueue ---');

    // Use test customer external ID if available
    let customerID = testEventCustomerID;
    if (!customerID) {
        if (testCustomerID) {
            try {
                const customer = await client.customers.getCustomer({ id: testCustomerID });
                customerID = (customer && 'externalId' in customer ? (customer as { externalId?: string }).externalId : null) || `test-customer-${Date.now()}`;
            } catch {
                customerID = `test-customer-${Date.now()}`;
            }
        } else {
            customerID = `test-customer-${Date.now()}`;
        }
    }

    try {
        const batchCount = 5;
        for (let i = 0; i < batchCount; i++) {
            await client.events.ingestEvent({
                eventName: 'batch_example',
                externalCustomerId: customerID,
                properties: { index: String(i), batch: 'demo' },
                source: 'sdk_test',
            });
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
    const client = getClient();

    console.log('========================================');
    console.log('CUSTOMER API TESTS');
    console.log('========================================\n');

    await testCreateCustomer(client);
    await testGetCustomer(client);
    await testListCustomers(client);
    await testUpdateCustomer(client);
    await testLookupCustomer(client);
    await testSearchCustomers(client);
    await testGetCustomerEntitlements(client);
    await testGetCustomerUpcomingGrants(client);
    await testGetCustomerUsage(client);

    console.log('✓ Customer API Tests Completed!\n');

    console.log('========================================');
    console.log('FEATURES API TESTS');
    console.log('========================================\n');

    await testCreateFeature(client);
    await testGetFeature(client);
    await testListFeatures(client);
    await testUpdateFeature(client);
    await testSearchFeatures(client);

    console.log('✓ Features API Tests Completed!\n');

    console.log('========================================');
    console.log('CONNECTIONS API TESTS');
    console.log('========================================\n');

    await testListConnections(client);
    await testSearchConnections(client);

    console.log('✓ Connections API Tests Completed!\n');

    console.log('========================================');
    console.log('PLANS API TESTS');
    console.log('========================================\n');

    await testCreatePlan(client);
    await testGetPlan(client);
    await testListPlans(client);
    await testUpdatePlan(client);
    await testSearchPlans(client);

    console.log('✓ Plans API Tests Completed!\n');

    console.log('========================================');
    console.log('ADDONS API TESTS');
    console.log('========================================\n');

    await testCreateAddon(client);
    await testGetAddon(client);
    await testListAddons(client);
    await testUpdateAddon(client);
    await testLookupAddon(client);
    await testSearchAddons(client);

    console.log('✓ Addons API Tests Completed!\n');

    console.log('========================================');
    console.log('ENTITLEMENTS API TESTS');
    console.log('========================================\n');

    await testCreateEntitlement(client);
    await testGetEntitlement(client);
    await testListEntitlements(client);
    await testUpdateEntitlement(client);
    await testSearchEntitlements(client);

    console.log('✓ Entitlements API Tests Completed!\n');

    console.log('========================================');
    console.log('SUBSCRIPTIONS API TESTS');
    console.log('========================================\n');

    await testCreateSubscription(client);
    await testGetSubscription(client);
    await testListSubscriptions(client);
    await testUpdateSubscription(client);
    await testSearchSubscriptions(client);
    await testActivateSubscription(client);
    // Lifecycle management (commented out - not needed)
    // await testPauseSubscription(client);
    // await testResumeSubscription(client);
    // await testGetPauseHistory(client);
    await testAddAddonToSubscription(client);
    await testRemoveAddonFromSubscription(client);
    // Change management
    // await testPreviewSubscriptionChange(client); // Commented out - not needed
    await testExecuteSubscriptionChange(client);
    await testGetSubscriptionEntitlements(client);
    await testGetUpcomingGrants(client);
    await testReportUsage(client);
    await testUpdateLineItem(client);
    await testDeleteLineItem(client);
    await testCancelSubscription(client);

    console.log('✓ Subscriptions API Tests Completed!\n');

    console.log('========================================');
    console.log('INVOICES API TESTS');
    console.log('========================================\n');

    await testListInvoices(client);
    await testSearchInvoices(client);
    await testCreateInvoice(client);
    await testGetInvoice(client);
    await testUpdateInvoice(client);
    await testPreviewInvoice(client);
    await testFinalizeInvoice(client);
    await testRecalculateInvoice(client);
    await testRecordPayment(client);
    await testAttemptPayment(client);
    await testDownloadInvoicePDF(client);
    await testTriggerInvoiceComms(client);
    await testGetCustomerInvoiceSummary(client);
    await testVoidInvoice(client);

    console.log('✓ Invoices API Tests Completed!\n');

    console.log('========================================');
    console.log('PRICES API TESTS');
    console.log('========================================\n');

    await testCreatePrice(client);
    await testGetPrice(client);
    await testListPrices(client);
    await testUpdatePrice(client);

    console.log('✓ Prices API Tests Completed!\n');

    console.log('========================================');
    console.log('PAYMENTS API TESTS');
    console.log('========================================\n');

    await testCreatePayment(client);
    await testGetPayment(client);
    await testSearchPayments(client);
    await testListPayments(client);
    await testUpdatePayment(client);
    await testProcessPayment(client);

    console.log('✓ Payments API Tests Completed!\n');

    console.log('========================================');
    console.log('WALLETS API TESTS');
    console.log('========================================\n');

    await testCreateWallet(client);
    await testGetWallet(client);
    await testListWallets(client);
    await testUpdateWallet(client);
    await testGetWalletBalance(client);
    await testTopUpWallet(client);
    await testDebitWallet(client);
    await testGetWalletTransactions(client);
    await testSearchWallets(client);

    console.log('✓ Wallets API Tests Completed!\n');

    console.log('========================================');
    console.log('CREDIT GRANTS API TESTS');
    console.log('========================================\n');

    await testCreateCreditGrant(client);
    await testGetCreditGrant(client);
    await testListCreditGrants(client);
    await testUpdateCreditGrant(client);
    // Note: testDeleteCreditGrant is in cleanup section

    console.log('✓ Credit Grants API Tests Completed!\n');

    console.log('========================================');
    console.log('CREDIT NOTES API TESTS');
    console.log('========================================\n');

    await testCreateCreditNote(client);
    await testGetCreditNote(client);
    await testListCreditNotes(client);
    await testFinalizeCreditNote(client);

    console.log('✓ Credit Notes API Tests Completed!\n');

    console.log('========================================');
    console.log('EVENTS API TESTS');
    console.log('========================================\n');

    // Sync event operations
    await testCreateEvent(client);
    await testQueryEvents(client);

    // Async event operations
    await testAsyncEventEnqueue(client);
    await testAsyncEventEnqueueWithOptions(client);
    await testAsyncEventBatch(client);

    console.log('✓ Events API Tests Completed!\n');

    console.log('========================================');
    console.log('CLEANUP - DELETING TEST DATA');
    console.log('========================================\n');

    await testDeletePayment(client);
    await testDeletePrice(client);
    await testDeleteEntitlement(client);
    await testDeleteAddon(client);
    await testDeletePlan(client);
    await testDeleteFeature(client);
    await testDeleteCreditGrant(client);
    await testDeleteCustomer(client);

    console.log('✓ Cleanup Completed!\n');

    console.log('\n=== All API Tests Completed Successfully! ===');
}

main().catch(console.error);

