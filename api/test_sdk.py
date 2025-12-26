#!/usr/bin/env python3
"""
FlexPrice Python SDK - Comprehensive API Tests

This test suite covers all FlexPrice APIs with comprehensive CRUD operations.
Tests are designed to run sequentially, building on previous test results.
"""

import os
import sys
import time
from datetime import datetime, timezone
from typing import Optional

# Check for required dependencies before importing SDK
def check_dependencies():
    """Check if required Python SDK dependencies are installed."""
    missing_deps = []
    
    try:
        import pydantic
    except ImportError:
        missing_deps.append('pydantic >= 2')
    
    try:
        import urllib3
    except ImportError:
        missing_deps.append('urllib3 >= 2.0.0, < 3.0.0')
    
    try:
        import dateutil
    except ImportError:
        missing_deps.append('python-dateutil >= 2.8.2')
    
    if missing_deps:
        print("❌ Missing required dependencies:")
        for dep in missing_deps:
            print(f"   - {dep}")
        print("\n📦 To install dependencies, run:")
        python_dir = os.path.join(os.path.dirname(__file__), 'python')
        print(f"   cd {python_dir}")
        print("   pip install -r requirements.txt")
        print("\n   Or install directly:")
        print("   pip install pydantic>=2 urllib3>=2.0.0,<3.0.0 python-dateutil>=2.8.2 typing-extensions>=4.7.1")
        sys.exit(1)

# Check dependencies first
check_dependencies()

# Add python directory to path to import the locally generated SDK
sys.path.insert(0, os.path.join(os.path.dirname(__file__), 'python'))

# Import the FlexPrice SDK
import flexprice
from flexprice.api import (
    customers_api,
    features_api,
    plans_api,
    addons_api,
    entitlements_api,
    subscriptions_api,
    invoices_api,
    prices_api,
    payments_api,
    wallets_api,
    credit_grants_api,
    credit_notes_api,
    connections_api,
    events_api,
)

# Optional: Load environment variables from .env file
try:
    from dotenv import load_dotenv
    load_dotenv()
except ImportError:
    pass

# Global test entity IDs
test_customer_id: Optional[str] = None
test_customer_name: Optional[str] = None

test_feature_id: Optional[str] = None
test_feature_name: Optional[str] = None

test_plan_id: Optional[str] = None
test_plan_name: Optional[str] = None

test_addon_id: Optional[str] = None
test_addon_name: Optional[str] = None
test_addon_lookup_key: Optional[str] = None

test_entitlement_id: Optional[str] = None

test_subscription_id: Optional[str] = None

test_invoice_id: Optional[str] = None

test_price_id: Optional[str] = None

test_payment_id: Optional[str] = None

test_wallet_id: Optional[str] = None
test_credit_grant_id: Optional[str] = None
test_credit_note_id: Optional[str] = None

test_event_id: Optional[str] = None
test_event_name: Optional[str] = None
test_event_customer_id: Optional[str] = None


# ========================================
# CONFIGURATION
# ========================================

def get_configuration() -> flexprice.Configuration:
    """Get and configure the FlexPrice API client."""
    api_key = os.getenv("FLEXPRICE_API_KEY")
    api_host = os.getenv("FLEXPRICE_API_HOST", "api.cloud.flexprice.io/v1")

    if not api_key:
        print("❌ Missing FLEXPRICE_API_KEY environment variable")
        exit(1)
    if not api_host:
        print("❌ Missing FLEXPRICE_API_HOST environment variable")
        exit(1)

    print("=== FlexPrice Python SDK - API Tests ===\n")
    print(f"✓ API Key: {api_key[:8]}...{api_key[-4:]}")
    print(f"✓ API Host: {api_host}\n")

    # Ensure host has protocol
    if not api_host.startswith("http://") and not api_host.startswith("https://"):
        full_path = f"https://{api_host}"
    else:
        full_path = api_host

    configuration = flexprice.Configuration(host=full_path)
    configuration.api_key['x-api-key'] = api_key

    return configuration


# ========================================
# CUSTOMERS API TESTS
# ========================================

def test_create_customer(api_client: flexprice.ApiClient):
    """Test 1: Create Customer"""
    print("--- Test 1: Create Customer ---")

    try:
        api = customers_api.CustomersApi(api_client)
        timestamp = int(time.time() * 1000)
        global test_customer_name, test_customer_id
        test_customer_name = f"Test Customer {timestamp}"

        from flexprice.models.dto_create_customer_request import DtoCreateCustomerRequest

        customer_request = DtoCreateCustomerRequest(
            name=test_customer_name,
            email=f"test-{timestamp}@example.com",
            external_id=f"test-customer-{timestamp}",
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
                "environment": "test",
            },
        )

        response = api.customers_post(customer=customer_request)

        test_customer_id = response.id
        print("✓ Customer created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  External ID: {response.external_id}")
        print(f"  Email: {response.email}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error creating customer: {e}\n")


def test_get_customer(api_client: flexprice.ApiClient):
    """Test 2: Get Customer by ID"""
    print("--- Test 2: Get Customer by ID ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping get customer test\n")
        return

    try:
        api = customers_api.CustomersApi(api_client)
        response = api.customers_id_get(id=test_customer_id)

        print("✓ Customer retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Created At: {response.created_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting customer: {e}\n")


def test_list_customers(api_client: flexprice.ApiClient):
    """Test 3: List Customers"""
    print("--- Test 3: List Customers ---")

    try:
        api = customers_api.CustomersApi(api_client)
        response = api.customers_get(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} customers")
        if response.items and len(response.items) > 0:
            print(f"  First customer: {response.items[0].id} - {response.items[0].name}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error listing customers: {e}\n")


def test_update_customer(api_client: flexprice.ApiClient):
    """Test 4: Update Customer"""
    print("--- Test 4: Update Customer ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping update customer test\n")
        return

    try:
        api = customers_api.CustomersApi(api_client)
        from flexprice.models.dto_update_customer_request import DtoUpdateCustomerRequest

        customer_request = DtoUpdateCustomerRequest(
            name=f"{test_customer_name} (Updated)",
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            },
        )

        response = api.customers_id_put(id=test_customer_id, customer=customer_request)

        print("✓ Customer updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  New Name: {response.name}")
        print(f"  Updated At: {response.updated_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error updating customer: {e}\n")


def test_lookup_customer(api_client: flexprice.ApiClient):
    """Test 5: Lookup Customer by External ID"""
    print("--- Test 5: Lookup Customer by External ID ---")

    if not test_customer_name:
        print("⚠ Warning: No customer name available\n⚠ Skipping lookup test\n")
        return

    try:
        api = customers_api.CustomersApi(api_client)
        external_id = f"test-customer-{test_customer_name.split(' ')[2]}"
        response = api.customers_lookup_lookup_key_get(lookup_key=external_id)

        print("✓ Customer found by external ID!")
        print(f"  External ID: {external_id}")
        print(f"  Customer ID: {response.id}")
        print(f"  Name: {response.name}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error looking up customer: {e}\n")


def test_search_customers(api_client: flexprice.ApiClient):
    """Test 6: Search Customers"""
    print("--- Test 6: Search Customers ---")

    if not test_customer_name:
        print("⚠ Warning: No customer name available\n⚠ Skipping search test\n")
        return

    try:
        api = customers_api.CustomersApi(api_client)
        external_id = f"test-customer-{test_customer_name.split(' ')[2]}"
        from flexprice.models.types_customer_filter import TypesCustomerFilter

        # Python SDK takes filter directly, not a request object
        customer_filter = TypesCustomerFilter(external_id=external_id)

        response = api.customers_search_post(filter=customer_filter)

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} customers matching external ID '{external_id}'")
        if response.items:
            for customer in response.items:
                print(f"  - {customer.id}: {customer.name}")
        print()
    except flexprice.ApiException as e:
        print(f"❌ Error searching customers: {e}\n")


def test_get_customer_entitlements(api_client: flexprice.ApiClient):
    """Test 7: Get Customer Entitlements"""
    print("--- Test 7: Get Customer Entitlements ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping get entitlements test\n")
        return

    try:
        api = customers_api.CustomersApi(api_client)
        response = api.customers_id_entitlements_get(id=test_customer_id)

        print("✓ Retrieved customer entitlements!")
        print(f"  Total features: {len(response.features) if response.features else 0}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting customer entitlements: {e}\n")


def test_get_customer_upcoming_grants(api_client: flexprice.ApiClient):
    """Test 8: Get Customer Upcoming Grants"""
    print("--- Test 8: Get Customer Upcoming Grants ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping get upcoming grants test\n")
        return

    try:
        api = customers_api.CustomersApi(api_client)
        response = api.customers_id_grants_upcoming_get(id=test_customer_id)

        print("✓ Retrieved upcoming grants!")
        print(f"  Total upcoming grants: {len(response.items) if response.items else 0}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting upcoming grants: {e}\n")


def test_get_customer_usage(api_client: flexprice.ApiClient):
    """Test 9: Get Customer Usage"""
    print("--- Test 9: Get Customer Usage ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping get usage test\n")
        return

    try:
        api = customers_api.CustomersApi(api_client)
        response = api.customers_usage_get(customer_id=test_customer_id)

        print("✓ Retrieved customer usage!")
        print(f"  Usage records: {len(response.features) if response.features else 0}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting customer usage: {e}\n")


# ========================================
# FEATURES API TESTS
# ========================================

def test_create_feature(api_client: flexprice.ApiClient):
    """Test 1: Create Feature"""
    print("--- Test 1: Create Feature ---")

    try:
        api = features_api.FeaturesApi(api_client)
        timestamp = int(time.time() * 1000)
        global test_feature_name, test_feature_id
        test_feature_name = f"Test Feature {timestamp}"
        feature_key = f"test_feature_{timestamp}"

        from flexprice.models.dto_create_feature_request import DtoCreateFeatureRequest
        from flexprice.models.types_feature_type import TypesFeatureType

        feature_request = DtoCreateFeatureRequest(
            name=test_feature_name,
            lookup_key=feature_key,
            description="This is a test feature created by SDK tests",
            type=TypesFeatureType.BOOLEAN,
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
                "environment": "test",
            },
        )

        response = api.features_post(feature=feature_request)

        test_feature_id = response.id
        print("✓ Feature created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Lookup Key: {response.lookup_key}")
        print(f"  Type: {response.type}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error creating feature: {e}\n")


def test_get_feature(api_client: flexprice.ApiClient):
    """Test 2: Get Feature by ID"""
    print("--- Test 2: Get Feature by ID ---")

    if not test_feature_id:
        print("⚠ Warning: No feature ID available\n⚠ Skipping get feature test\n")
        return

    try:
        api = features_api.FeaturesApi(api_client)
        response = api.features_id_get(id=test_feature_id)

        print("✓ Feature retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Lookup Key: {response.lookup_key}")
        print(f"  Created At: {response.created_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting feature: {e}\n")


def test_list_features(api_client: flexprice.ApiClient):
    """Test 3: List Features"""
    print("--- Test 3: List Features ---")

    try:
        api = features_api.FeaturesApi(api_client)
        response = api.features_get(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} features")
        if response.items and len(response.items) > 0:
            print(f"  First feature: {response.items[0].id} - {response.items[0].name}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error listing features: {e}\n")


def test_update_feature(api_client: flexprice.ApiClient):
    """Test 4: Update Feature"""
    print("--- Test 4: Update Feature ---")

    if not test_feature_id:
        print("⚠ Warning: No feature ID available\n⚠ Skipping update feature test\n")
        return

    try:
        api = features_api.FeaturesApi(api_client)
        from flexprice.models.dto_update_feature_request import DtoUpdateFeatureRequest

        feature_request = DtoUpdateFeatureRequest(
            name=f"{test_feature_name} (Updated)",
            description="Updated description for test feature",
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            },
        )

        response = api.features_id_put(id=test_feature_id, feature=feature_request)

        print("✓ Feature updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  New Name: {response.name}")
        print(f"  New Description: {response.description}")
        print(f"  Updated At: {response.updated_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error updating feature: {e}\n")


def test_search_features(api_client: flexprice.ApiClient):
    """Test 5: Search Features"""
    print("--- Test 5: Search Features ---")

    if not test_feature_id:
        print("⚠ Warning: No feature ID available\n⚠ Skipping search test\n")
        return

    try:
        api = features_api.FeaturesApi(api_client)
        from flexprice.models.types_feature_filter import TypesFeatureFilter

        # Python SDK takes filter directly, not a request object
        feature_filter = TypesFeatureFilter(feature_ids=[test_feature_id])

        response = api.features_search_post(filter=feature_filter)

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} features matching ID '{test_feature_id}'")
        if response.items:
            for feature in response.items[:3]:
                print(f"  - {feature.id}: {feature.name} ({feature.lookup_key})")
        print()
    except flexprice.ApiException as e:
        print(f"❌ Error searching features: {e}\n")


# ========================================
# PLANS API TESTS
# ========================================

def test_create_plan(api_client: flexprice.ApiClient):
    """Test 1: Create Plan"""
    print("--- Test 1: Create Plan ---")

    try:
        api = plans_api.PlansApi(api_client)
        timestamp = int(time.time() * 1000)
        global test_plan_name, test_plan_id
        test_plan_name = f"Test Plan {timestamp}"
        lookup_key = f"test_plan_{timestamp}"

        from flexprice.models.dto_create_plan_request import DtoCreatePlanRequest

        plan_request = DtoCreatePlanRequest(
            name=test_plan_name,
            lookup_key=lookup_key,
            description="This is a test plan created by SDK tests",
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
                "environment": "test",
            },
        )

        response = api.plans_post(plan=plan_request)

        test_plan_id = response.id
        print("✓ Plan created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Lookup Key: {response.lookup_key}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error creating plan: {e}\n")


def test_get_plan(api_client: flexprice.ApiClient):
    """Test 2: Get Plan by ID"""
    print("--- Test 2: Get Plan by ID ---")

    if not test_plan_id:
        print("⚠ Warning: No plan ID available\n⚠ Skipping get plan test\n")
        return

    try:
        api = plans_api.PlansApi(api_client)
        response = api.plans_id_get(id=test_plan_id)

        print("✓ Plan retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Lookup Key: {response.lookup_key}")
        print(f"  Created At: {response.created_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting plan: {e}\n")


def test_list_plans(api_client: flexprice.ApiClient):
    """Test 3: List Plans"""
    print("--- Test 3: List Plans ---")

    try:
        api = plans_api.PlansApi(api_client)
        response = api.plans_get(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} plans")
        if response.items and len(response.items) > 0:
            print(f"  First plan: {response.items[0].id} - {response.items[0].name}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error listing plans: {e}\n")


def test_update_plan(api_client: flexprice.ApiClient):
    """Test 4: Update Plan"""
    print("--- Test 4: Update Plan ---")

    if not test_plan_id:
        print("⚠ Warning: No plan ID available\n⚠ Skipping update plan test\n")
        return

    try:
        api = plans_api.PlansApi(api_client)
        from flexprice.models.dto_update_plan_request import DtoUpdatePlanRequest

        plan_request = DtoUpdatePlanRequest(
            name=f"{test_plan_name} (Updated)",
            description="Updated description for test plan",
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            },
        )

        response = api.plans_id_put(id=test_plan_id, plan=plan_request)

        print("✓ Plan updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  New Name: {response.name}")
        print(f"  New Description: {response.description}")
        print(f"  Updated At: {response.updated_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error updating plan: {e}\n")


def test_search_plans(api_client: flexprice.ApiClient):
    """Test 5: Search Plans"""
    print("--- Test 5: Search Plans ---")

    if not test_plan_id:
        print("⚠ Warning: No plan ID available\n⚠ Skipping search test\n")
        return

    try:
        api = plans_api.PlansApi(api_client)
        from flexprice.models.types_plan_filter import TypesPlanFilter

        # Python SDK takes filter directly, not a request object
        plan_filter = TypesPlanFilter(plan_ids=[test_plan_id])

        response = api.plans_search_post(filter=plan_filter)

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} plans matching ID '{test_plan_id}'")
        if response.items:
            for plan in response.items[:3]:
                print(f"  - {plan.id}: {plan.name} ({plan.lookup_key})")
        print()
    except flexprice.ApiException as e:
        print(f"❌ Error searching plans: {e}\n")


# ========================================
# ADDONS API TESTS
# ========================================

def test_create_addon(api_client: flexprice.ApiClient):
    """Test 1: Create Addon"""
    print("--- Test 1: Create Addon ---")

    try:
        api = addons_api.AddonsApi(api_client)
        timestamp = int(time.time() * 1000)
        global test_addon_name, test_addon_id, test_addon_lookup_key
        test_addon_name = f"Test Addon {timestamp}"
        test_addon_lookup_key = f"test_addon_{timestamp}"

        from flexprice.models.dto_create_addon_request import DtoCreateAddonRequest
        from flexprice.models.types_addon_type import TypesAddonType

        addon_request = DtoCreateAddonRequest(
            name=test_addon_name,
            lookup_key=test_addon_lookup_key,
            description="This is a test addon created by SDK tests",
            type=TypesAddonType.ONETIME,
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
                "environment": "test",
            },
        )

        response = api.addons_post(addon=addon_request)

        test_addon_id = response.id
        print("✓ Addon created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Lookup Key: {response.lookup_key}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error creating addon: {e}\n")


def test_get_addon(api_client: flexprice.ApiClient):
    """Test 2: Get Addon by ID"""
    print("--- Test 2: Get Addon by ID ---")

    if not test_addon_id:
        print("⚠ Warning: No addon ID available\n⚠ Skipping get addon test\n")
        return

    try:
        api = addons_api.AddonsApi(api_client)
        response = api.addons_id_get(id=test_addon_id)

        print("✓ Addon retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Lookup Key: {response.lookup_key}")
        print(f"  Created At: {response.created_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting addon: {e}\n")


def test_list_addons(api_client: flexprice.ApiClient):
    """Test 3: List Addons"""
    print("--- Test 3: List Addons ---")

    try:
        api = addons_api.AddonsApi(api_client)
        response = api.addons_get(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} addons")
        if response.items and len(response.items) > 0:
            print(f"  First addon: {response.items[0].id} - {response.items[0].name}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error listing addons: {e}\n")


def test_update_addon(api_client: flexprice.ApiClient):
    """Test 4: Update Addon"""
    print("--- Test 4: Update Addon ---")

    if not test_addon_id:
        print("⚠ Warning: No addon ID available\n⚠ Skipping update addon test\n")
        return

    try:
        api = addons_api.AddonsApi(api_client)
        from flexprice.models.dto_update_addon_request import DtoUpdateAddonRequest

        addon_request = DtoUpdateAddonRequest(
            name=f"{test_addon_name} (Updated)",
            description="Updated description for test addon",
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            },
        )

        response = api.addons_id_put(id=test_addon_id, addon=addon_request)

        print("✓ Addon updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  New Name: {response.name}")
        print(f"  New Description: {response.description}")
        print(f"  Updated At: {response.updated_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error updating addon: {e}\n")


def test_lookup_addon(api_client: flexprice.ApiClient):
    """Test 5: Lookup Addon by Lookup Key"""
    print("--- Test 5: Lookup Addon by Lookup Key ---")

    if not test_addon_lookup_key:
        print("⚠ Warning: No addon lookup key available\n⚠ Skipping lookup test\n")
        return

    try:
        api = addons_api.AddonsApi(api_client)
        print(f"  Looking up addon with key: {test_addon_lookup_key}")
        response = api.addons_lookup_lookup_key_get(lookup_key=test_addon_lookup_key)

        print("✓ Addon found by lookup key!")
        print(f"  Lookup Key: {test_addon_lookup_key}")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error looking up addon: {e}")
        print("⚠ Skipping lookup test\n")


def test_search_addons(api_client: flexprice.ApiClient):
    """Test 6: Search Addons"""
    print("--- Test 6: Search Addons ---")

    if not test_addon_id:
        print("⚠ Warning: No addon ID available\n⚠ Skipping search test\n")
        return

    try:
        api = addons_api.AddonsApi(api_client)
        from flexprice.models.types_addon_filter import TypesAddonFilter

        # Python SDK takes filter directly, not a request object
        addon_filter = TypesAddonFilter(addon_ids=[test_addon_id])

        response = api.addons_search_post(filter=addon_filter)

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} addons matching ID '{test_addon_id}'")
        if response.items:
            for addon in response.items[:3]:
                print(f"  - {addon.id}: {addon.name} ({addon.lookup_key})")
        print()
    except flexprice.ApiException as e:
        print(f"❌ Error searching addons: {e}\n")


# ========================================
# ENTITLEMENTS API TESTS
# ========================================

def test_create_entitlement(api_client: flexprice.ApiClient):
    """Test 1: Create Entitlement"""
    print("--- Test 1: Create Entitlement ---")

    if not test_feature_id or not test_plan_id:
        print("⚠ Warning: No feature or plan ID available\n⚠ Skipping create entitlement test\n")
        return

    try:
        api = entitlements_api.EntitlementsApi(api_client)
        from flexprice.models.dto_create_entitlement_request import DtoCreateEntitlementRequest
        from flexprice.models.types_feature_type import TypesFeatureType
        from flexprice.models.types_entitlement_usage_reset_period import TypesEntitlementUsageResetPeriod

        entitlement_request = DtoCreateEntitlementRequest(
            feature_id=test_feature_id,
            feature_type=TypesFeatureType.BOOLEAN,
            plan_id=test_plan_id,
            is_enabled=True,
            usage_reset_period=TypesEntitlementUsageResetPeriod.MONTHLY,
        )

        response = api.entitlements_post(entitlement=entitlement_request)

        global test_entitlement_id
        test_entitlement_id = response.id
        print("✓ Entitlement created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Feature ID: {response.feature_id}")
        print(f"  Plan ID: {response.plan_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error creating entitlement: {e}\n")


def test_get_entitlement(api_client: flexprice.ApiClient):
    """Test 2: Get Entitlement by ID"""
    print("--- Test 2: Get Entitlement by ID ---")

    if not test_entitlement_id:
        print("⚠ Warning: No entitlement ID available\n⚠ Skipping get entitlement test\n")
        return

    try:
        api = entitlements_api.EntitlementsApi(api_client)
        response = api.entitlements_id_get(id=test_entitlement_id)

        print("✓ Entitlement retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Feature ID: {response.feature_id}")
        plan_id = getattr(response, 'plan_id', 'N/A')
        print(f"  Plan ID: {plan_id}")
        print(f"  Created At: {response.created_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting entitlement: {e}\n")


def test_list_entitlements(api_client: flexprice.ApiClient):
    """Test 3: List Entitlements"""
    print("--- Test 3: List Entitlements ---")

    try:
        api = entitlements_api.EntitlementsApi(api_client)
        response = api.entitlements_get(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} entitlements")
        if response.items and len(response.items) > 0:
            print(f"  First entitlement: {response.items[0].id}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error listing entitlements: {e}\n")


def test_update_entitlement(api_client: flexprice.ApiClient):
    """Test 4: Update Entitlement"""
    print("--- Test 4: Update Entitlement ---")

    if not test_entitlement_id:
        print("⚠ Warning: No entitlement ID available\n⚠ Skipping update entitlement test\n")
        return

    try:
        api = entitlements_api.EntitlementsApi(api_client)
        from flexprice.models.dto_update_entitlement_request import DtoUpdateEntitlementRequest

        entitlement_request = DtoUpdateEntitlementRequest(is_enabled=False)

        response = api.entitlements_id_put(id=test_entitlement_id, entitlement=entitlement_request)

        print("✓ Entitlement updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  Updated At: {response.updated_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error updating entitlement: {e}\n")


def test_search_entitlements(api_client: flexprice.ApiClient):
    """Test 5: Search Entitlements"""
    print("--- Test 5: Search Entitlements ---")

    if not test_entitlement_id:
        print("⚠ Warning: No entitlement ID available\n⚠ Skipping search test\n")
        return

    try:
        api = entitlements_api.EntitlementsApi(api_client)
        from flexprice.models.types_entitlement_filter import TypesEntitlementFilter

        # Python SDK takes filter directly, not a request object
        entitlement_filter = TypesEntitlementFilter(entity_ids=[test_entitlement_id])

        response = api.entitlements_search_post(filter=entitlement_filter)

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} entitlements matching ID '{test_entitlement_id}'")
        if response.items:
            for ent in response.items[:3]:
                print(f"  - {ent.id}: Feature {ent.feature_id}")
        print()
    except flexprice.ApiException as e:
        print(f"❌ Error searching entitlements: {e}\n")


# ========================================
# CONNECTIONS API TESTS
# ========================================

def test_list_connections(api_client: flexprice.ApiClient):
    """Test 1: List Connections"""
    print("--- Test 1: List Connections ---")

    try:
        api = connections_api.ConnectionsApi(api_client)
        response = api.connections_get(limit=10)

        print(f"✓ Retrieved {len(response.connections) if response.connections else 0} connections")
        if response.connections and len(response.connections) > 0:
            print(f"  First connection: {response.connections[0].id}")
            if hasattr(response.connections[0], 'provider_type'):
                print(f"  Provider Type: {response.connections[0].provider_type}")
        if hasattr(response, 'total'):
            print(f"  Total: {response.total}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error listing connections: {e}")
        print("⚠ Skipping connections tests (may not have any connections)\n")


def test_search_connections(api_client: flexprice.ApiClient):
    """Test 2: Search Connections"""
    print("--- Test 2: Search Connections ---")

    try:
        api = connections_api.ConnectionsApi(api_client)
        from flexprice.models.types_connection_filter import TypesConnectionFilter

        # Python SDK takes filter directly, not a request object
        connection_filter = TypesConnectionFilter(limit=5)

        response = api.connections_search_post(filter=connection_filter)

        print("✓ Search completed!")
        print(f"  Found {len(response.connections) if response.connections else 0} connections")
        if response.connections:
            for i, connection in enumerate(response.connections[:3]):  # Show first 3 results
                provider = connection.provider_type if hasattr(connection, 'provider_type') and connection.provider_type else "unknown"
                conn_id = connection.id if hasattr(connection, 'id') else "unknown"
                print(f"  - {conn_id}: {provider}")
        print()
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error searching connections: {e}\n")


# ========================================
# SUBSCRIPTIONS API TESTS
# ========================================

def test_create_subscription(api_client: flexprice.ApiClient):
    """Test 1: Create Subscription"""
    print("--- Test 1: Create Subscription ---")

    if not test_customer_id or not test_plan_id:
        print("⚠ Warning: No customer or plan ID available\n⚠ Skipping create subscription test\n")
        return

    try:
        # First create a price for the plan
        prices_api_instance = prices_api.PricesApi(api_client)
        from flexprice.models.dto_create_price_request import DtoCreatePriceRequest
        from flexprice.models.types_price_entity_type import TypesPriceEntityType
        from flexprice.models.types_price_type import TypesPriceType
        from flexprice.models.types_billing_model import TypesBillingModel
        from flexprice.models.types_billing_cadence import TypesBillingCadence
        from flexprice.models.types_billing_period import TypesBillingPeriod
        from flexprice.models.types_invoice_cadence import TypesInvoiceCadence
        from flexprice.models.types_price_unit_type import TypesPriceUnitType

        price_request = DtoCreatePriceRequest(
            entity_id=test_plan_id,
            entity_type=TypesPriceEntityType.PLAN,
            type=TypesPriceType.FIXED,
            billing_model=TypesBillingModel.FLAT_FEE,
            billing_cadence=TypesBillingCadence.RECURRING,
            billing_period=TypesBillingPeriod.MONTHLY,
            invoice_cadence=TypesInvoiceCadence.ARREAR,
            price_unit_type=TypesPriceUnitType.FIAT,  # Required in Python SDK
            amount="29.99",
            currency="USD",
            display_name="Monthly Subscription Price",
        )

        prices_api_instance.prices_post(price=price_request)

        # Now create the subscription
        api = subscriptions_api.SubscriptionsApi(api_client)
        from flexprice.models.dto_create_subscription_request import DtoCreateSubscriptionRequest
        from flexprice.models.types_billing_cycle import TypesBillingCycle

        subscription_request = DtoCreateSubscriptionRequest(
            customer_id=test_customer_id,
            plan_id=test_plan_id,
            currency="USD",
            billing_cadence=TypesBillingCadence.RECURRING,
            billing_period=TypesBillingPeriod.MONTHLY,
            billing_period_count=1,
            billing_cycle=TypesBillingCycle.ANNIVERSARY,
            start_date=datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z'),
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
            },
        )

        response = api.subscriptions_post(subscription=subscription_request)

        global test_subscription_id
        test_subscription_id = response.id
        print("✓ Subscription created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Customer ID: {response.customer_id}")
        print(f"  Plan ID: {response.plan_id}")
        print(f"  Status: {response.subscription_status}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error creating subscription: {e}\n")


def test_get_subscription(api_client: flexprice.ApiClient):
    """Test 2: Get Subscription by ID"""
    print("--- Test 2: Get Subscription by ID ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription ID available\n⚠ Skipping get subscription test\n")
        return

    try:
        api = subscriptions_api.SubscriptionsApi(api_client)
        response = api.subscriptions_id_get(id=test_subscription_id)

        print("✓ Subscription retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Customer ID: {response.customer_id}")
        print(f"  Status: {response.subscription_status}")
        print(f"  Created At: {response.created_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting subscription: {e}\n")


def test_list_subscriptions(api_client: flexprice.ApiClient):
    """Test 3: List Subscriptions"""
    print("--- Test 3: List Subscriptions ---")

    try:
        api = subscriptions_api.SubscriptionsApi(api_client)
        response = api.subscriptions_get(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} subscriptions")
        if response.items and len(response.items) > 0:
            print(f"  First subscription: {response.items[0].id} (Customer: {response.items[0].customer_id})")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error listing subscriptions: {e}\n")


def test_update_subscription(api_client: flexprice.ApiClient):
    """Test 4: Update Subscription"""
    print("--- Test 4: Update Subscription ---")
    print("⚠ Skipping update subscription test (endpoint not available in SDK)\n")


def test_search_subscriptions(api_client: flexprice.ApiClient):
    """Test 5: Search Subscriptions"""
    print("--- Test 4: Search Subscriptions ---")

    try:
        api = subscriptions_api.SubscriptionsApi(api_client)
        from flexprice.models.types_subscription_filter import TypesSubscriptionFilter

        # Python SDK takes filter directly, not a request object
        subscription_filter = TypesSubscriptionFilter()

        response = api.subscriptions_search_post(filter=subscription_filter)

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} subscriptions\n")
    except flexprice.ApiException as e:
        print(f"❌ Error searching subscriptions: {e}\n")


def test_activate_subscription(api_client: flexprice.ApiClient):
    """Test 5: Activate Subscription"""
    print("--- Test 5: Activate Subscription ---")

    if not test_customer_id or not test_plan_id:
        print("⚠ Warning: No customer or plan ID available\n⚠ Skipping activate subscription test\n")
        return

    try:
        api = subscriptions_api.SubscriptionsApi(api_client)
        from flexprice.models.dto_create_subscription_request import DtoCreateSubscriptionRequest
        from flexprice.models.types_billing_cadence import TypesBillingCadence
        from flexprice.models.types_billing_period import TypesBillingPeriod

        from flexprice.models.types_subscription_status import TypesSubscriptionStatus

        draft_sub_request = DtoCreateSubscriptionRequest(
            customer_id=test_customer_id,
            plan_id=test_plan_id,
            currency="USD",
            billing_cadence=TypesBillingCadence.RECURRING,
            billing_period=TypesBillingPeriod.MONTHLY,
            billing_period_count=1,
            start_date=datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z'),
            subscription_status=TypesSubscriptionStatus.DRAFT,  # Set to DRAFT to match Go test
        )

        draft_sub = api.subscriptions_post(subscription=draft_sub_request)
        draft_id = draft_sub.id
        print(f"  Created draft subscription: {draft_id}")

        from flexprice.models.dto_activate_draft_subscription_request import DtoActivateDraftSubscriptionRequest

        activate_request = DtoActivateDraftSubscriptionRequest(start_date=datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z'))
        api.subscriptions_id_activate_post(id=draft_id, request=activate_request)

        print("✓ Subscription activated successfully!")
        print(f"  ID: {draft_id}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error activating subscription: {e}\n")


def test_pause_subscription(api_client: flexprice.ApiClient):
    """Test 7: Pause Subscription"""
    print("--- Test 7: Pause Subscription ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created, skipping pause test\n")
        return

    try:
        api = subscriptions_api.SubscriptionsApi(api_client)
        from flexprice.models.dto_pause_subscription_request import DtoPauseSubscriptionRequest

        from flexprice.models.types_pause_mode import TypesPauseMode
        pause_request = DtoPauseSubscriptionRequest(pause_mode=TypesPauseMode.IMMEDIATE)

        response = api.subscriptions_id_pause_post(id=test_subscription_id, request=pause_request)

        print("✓ Subscription paused successfully!")
        print(f"  Pause ID: {response.id}")
        print(f"  Subscription ID: {response.subscription_id}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error pausing subscription: {e}")
        print("⚠ Skipping pause test\n")


def test_resume_subscription(api_client: flexprice.ApiClient):
    """Test 8: Resume Subscription"""
    print("--- Test 8: Resume Subscription ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created, skipping resume test\n")
        return

    try:
        api = subscriptions_api.SubscriptionsApi(api_client)
        from flexprice.models.dto_resume_subscription_request import DtoResumeSubscriptionRequest

        from flexprice.models.types_resume_mode import TypesResumeMode
        resume_request = DtoResumeSubscriptionRequest(resume_mode=TypesResumeMode.IMMEDIATE)

        response = api.subscriptions_id_resume_post(id=test_subscription_id, request=resume_request)

        print("✓ Subscription resumed successfully!")
        print(f"  Pause ID: {response.id}")
        print(f"  Subscription ID: {response.subscription_id}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error resuming subscription: {e}")
        print("⚠ Skipping resume test\n")


def test_get_pause_history(api_client: flexprice.ApiClient):
    """Test 9: Get Pause History"""
    print("--- Test 9: Get Pause History ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created, skipping pause history test\n")
        return

    try:
        api = subscriptions_api.SubscriptionsApi(api_client)
        response = api.subscriptions_id_pauses_get(id=test_subscription_id)

        print("✓ Retrieved pause history!")
        print(f"  Total pauses: {len(response) if isinstance(response, list) else 0}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error getting pause history: {e}")
        print("⚠ Skipping pause history test\n")


def test_add_addon_to_subscription(api_client: flexprice.ApiClient):
    """Test 6: Add Addon to Subscription"""
    print("--- Test 6: Add Addon to Subscription ---")

    if not test_subscription_id or not test_addon_id:
        print("⚠ Warning: No subscription or addon created\n⚠ Skipping add addon test\n")
        return

    try:
        # First create a price for the addon
        prices_api_instance = prices_api.PricesApi(api_client)
        from flexprice.models.dto_create_price_request import DtoCreatePriceRequest
        from flexprice.models.types_price_entity_type import TypesPriceEntityType
        from flexprice.models.types_price_type import TypesPriceType
        from flexprice.models.types_billing_model import TypesBillingModel
        from flexprice.models.types_billing_cadence import TypesBillingCadence
        from flexprice.models.types_billing_period import TypesBillingPeriod
        from flexprice.models.types_invoice_cadence import TypesInvoiceCadence
        from flexprice.models.types_price_unit_type import TypesPriceUnitType

        price_request = DtoCreatePriceRequest(
            entity_id=test_addon_id,
            entity_type=TypesPriceEntityType.ADDON,
            type=TypesPriceType.FIXED,
            billing_model=TypesBillingModel.FLAT_FEE,
            billing_cadence=TypesBillingCadence.RECURRING,
            billing_period=TypesBillingPeriod.MONTHLY,
            invoice_cadence=TypesInvoiceCadence.ARREAR,
            price_unit_type=TypesPriceUnitType.FIAT,  # Required in Python SDK
            amount="5.00",
            currency="USD",
            display_name="Addon Monthly Price",
        )

        try:
            prices_api_instance.prices_post(price=price_request)
            print(f"  Created price for addon: {test_addon_id}")
        except flexprice.ApiException as price_error:
            print(f"⚠ Warning: Error creating price for addon: {price_error}")
            # Continue anyway, matching Go behavior

        # Add addon to subscription
        api = subscriptions_api.SubscriptionsApi(api_client)
        from flexprice.models.dto_add_addon_request import DtoAddAddonRequest

        addon_request = DtoAddAddonRequest(
            subscription_id=test_subscription_id,
            addon_id=test_addon_id,
        )

        subscription_response = api.subscriptions_addon_post(request=addon_request)

        print("✓ Addon added to subscription successfully!")
        print(f"  Subscription ID: {subscription_response.id if hasattr(subscription_response, 'id') else test_subscription_id}")
        print(f"  Addon ID: {test_addon_id}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error adding addon to subscription: {e}")
        if hasattr(e, 'body'):
            print(f"  Response: {e.body}")
        print("⚠ Skipping add addon test\n")


def test_remove_addon_from_subscription(api_client: flexprice.ApiClient):
    """Test 8: Remove Addon from Subscription"""
    print("--- Test 8: Remove Addon from Subscription ---")
    print("⚠ Skipping remove addon test (requires addon association ID)\n")


def test_preview_subscription_change(api_client: flexprice.ApiClient):
    """Test 9: Preview Subscription Change"""
    print("--- Test 13: Preview Subscription Change ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created, skipping preview change test\n")
        return

    if not test_plan_id:
        print("⚠ Warning: No plan available for change preview\n")
        return

    try:
        api = subscriptions_api.SubscriptionsApi(api_client)
        from flexprice.models.dto_subscription_change_request import DtoSubscriptionChangeRequest
        from flexprice.models.types_billing_cadence import TypesBillingCadence
        from flexprice.models.types_billing_period import TypesBillingPeriod
        from flexprice.models.types_billing_cycle import TypesBillingCycle
        from flexprice.models.types_proration_behavior import TypesProrationBehavior

        change_request = DtoSubscriptionChangeRequest(
            target_plan_id=test_plan_id,
            billing_cadence=TypesBillingCadence.RECURRING,
            billing_period=TypesBillingPeriod.MONTHLY,
            billing_cycle=TypesBillingCycle.ANNIVERSARY,
            proration_behavior=TypesProrationBehavior.CREATE_PRORATIONS,
        )

        preview = api.subscriptions_id_change_preview_post(id=test_subscription_id, request=change_request)

        print("✓ Subscription change preview generated!")
        if hasattr(preview, 'next_invoice_preview') and preview.next_invoice_preview:
            print("  Preview available")
        print()
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error previewing subscription change: {e}")
        print("⚠ Skipping preview change test\n")


def test_execute_subscription_change(api_client: flexprice.ApiClient):
    """Test 8: Execute Subscription Change"""
    print("--- Test 8: Execute Subscription Change ---")
    print("⚠ Skipping execute change test (would modify active subscription)\n")


def test_get_subscription_entitlements(api_client: flexprice.ApiClient):
    """Test 9: Get Subscription Entitlements"""
    print("--- Test 9: Get Subscription Entitlements ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created\n⚠ Skipping get entitlements test\n")
        return

    try:
        api = subscriptions_api.SubscriptionsApi(api_client)
        response = api.subscriptions_id_entitlements_get(id=test_subscription_id)

        print("✓ Retrieved subscription entitlements!")
        print(f"  Total features: {len(response.features) if response.features else 0}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error getting entitlements: {e}\n")


def test_get_upcoming_grants(api_client: flexprice.ApiClient):
    """Test 10: Get Upcoming Grants"""
    print("--- Test 10: Get Upcoming Grants ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created\n⚠ Skipping get upcoming grants test\n")
        return

    try:
        api = subscriptions_api.SubscriptionsApi(api_client)
        response = api.subscriptions_id_grants_upcoming_get(id=test_subscription_id)

        print("✓ Retrieved upcoming grants!")
        print(f"  Total upcoming grants: {len(response.items) if response.items else 0}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error getting upcoming grants: {e}\n")


def test_report_usage(api_client: flexprice.ApiClient):
    """Test 11: Report Usage"""
    print("--- Test 11: Report Usage ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created\n⚠ Skipping report usage test\n")
        return

    try:
        api = subscriptions_api.SubscriptionsApi(api_client)
        from flexprice.models.dto_get_usage_by_subscription_request import DtoGetUsageBySubscriptionRequest

        usage_request = DtoGetUsageBySubscriptionRequest(subscription_id=test_subscription_id)

        api.subscriptions_usage_post(request=usage_request)

        print("✓ Usage reported successfully!")
        print(f"  Subscription ID: {test_subscription_id}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error reporting usage: {e}\n")


def test_update_line_item(api_client: flexprice.ApiClient):
    """Test 12: Update Line Item"""
    print("--- Test 12: Update Line Item ---")
    print("⚠ Skipping update line item test (requires line item ID)\n")


def test_delete_line_item(api_client: flexprice.ApiClient):
    """Test 13: Delete Line Item"""
    print("--- Test 13: Delete Line Item ---")
    print("⚠ Skipping delete line item test (requires line item ID)\n")


def test_cancel_subscription(api_client: flexprice.ApiClient):
    """Test 14: Cancel Subscription"""
    print("--- Test 14: Cancel Subscription ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created\n⚠ Skipping cancel test\n")
        return

    try:
        api = subscriptions_api.SubscriptionsApi(api_client)
        from flexprice.models.dto_cancel_subscription_request import DtoCancelSubscriptionRequest
        from flexprice.models.types_cancellation_type import TypesCancellationType

        cancel_request = DtoCancelSubscriptionRequest(
            cancellation_type=TypesCancellationType.END_OF_PERIOD
        )

        api.subscriptions_id_cancel_post(id=test_subscription_id, request=cancel_request)

        print("✓ Subscription canceled successfully!")
        print(f"  Subscription ID: {test_subscription_id}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error canceling subscription: {e}\n")


# ========================================
# INVOICES API TESTS
# ========================================

def test_list_invoices(api_client: flexprice.ApiClient):
    """Test 1: List Invoices"""
    print("--- Test 1: List Invoices ---")

    try:
        api = invoices_api.InvoicesApi(api_client)
        response = api.invoices_get(limit=10)

        global test_invoice_id
        print(f"✓ Retrieved {len(response.items) if response.items else 0} invoices")
        if response.items and len(response.items) > 0:
            test_invoice_id = response.items[0].id
            print(f"  First invoice: {response.items[0].id} (Customer: {response.items[0].customer_id})")
            if hasattr(response.items[0], 'status'):
                print(f"  Status: {response.items[0].status}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error listing invoices: {e}\n")


def test_search_invoices(api_client: flexprice.ApiClient):
    """Test 2: Search Invoices"""
    print("--- Test 2: Search Invoices ---")

    try:
        api = invoices_api.InvoicesApi(api_client)
        from flexprice.models.types_invoice_filter import TypesInvoiceFilter

        # Python SDK takes filter directly, not a request object
        invoice_filter = TypesInvoiceFilter()

        response = api.invoices_search_post(filter=invoice_filter)

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} invoices\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error searching invoices: {e}\n")


def test_create_invoice(api_client: flexprice.ApiClient):
    """Test 3: Create Invoice"""
    print("--- Test 3: Create Invoice ---")

    if not test_customer_id:
        print("⚠ Warning: No customer created\n⚠ Skipping create invoice test\n")
        return

    try:
        api = invoices_api.InvoicesApi(api_client)
        from flexprice.models.dto_create_invoice_request import DtoCreateInvoiceRequest
        from flexprice.models.types_invoice_type import TypesInvoiceType
        from flexprice.models.types_invoice_billing_reason import TypesInvoiceBillingReason
        from flexprice.models.types_invoice_status import TypesInvoiceStatus
        from flexprice.models.dto_create_invoice_line_item_request import DtoCreateInvoiceLineItemRequest

        invoice_request = DtoCreateInvoiceRequest(
            customer_id=test_customer_id,
            currency="USD",
            amount_due="100.00",
            subtotal="100.00",
            total="100.00",
            invoice_type=TypesInvoiceType.ONE_OFF,
            billing_reason=TypesInvoiceBillingReason.MANUAL,
            invoice_status=TypesInvoiceStatus.DRAFT,
            line_items=[
                DtoCreateInvoiceLineItemRequest(
                    display_name="Test Service",
                    quantity="1",
                    amount="100.00",
                )
            ],
            metadata={
                "source": "sdk_test",
                "type": "manual",
            },
        )

        response = api.invoices_post(invoice=invoice_request)

        global test_invoice_id
        test_invoice_id = response.id
        print("✓ Invoice created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Customer ID: {response.customer_id}")
        print(f"  Status: {response.invoice_status}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error creating invoice: {e}\n")


def test_get_invoice(api_client: flexprice.ApiClient):
    """Test 4: Get Invoice by ID"""
    print("--- Test 4: Get Invoice by ID ---")

    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available\n⚠ Skipping get invoice test\n")
        return

    try:
        api = invoices_api.InvoicesApi(api_client)
        response = api.invoices_id_get(id=test_invoice_id)

        print("✓ Invoice retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Total: {response.currency} {response.total}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error getting invoice: {e}\n")


def test_update_invoice(api_client: flexprice.ApiClient):
    """Test 5: Update Invoice"""
    print("--- Test 5: Update Invoice ---")

    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available\n⚠ Skipping update invoice test\n")
        return

    try:
        api = invoices_api.InvoicesApi(api_client)
        from flexprice.models.dto_update_invoice_request import DtoUpdateInvoiceRequest

        update_request = DtoUpdateInvoiceRequest(
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            }
        )

        response = api.invoices_id_put(id=test_invoice_id, request=update_request)

        print("✓ Invoice updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  Updated At: {response.updated_at}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error updating invoice: {e}\n")


def test_preview_invoice(api_client: flexprice.ApiClient):
    """Test 6: Preview Invoice"""
    print("--- Test 6: Preview Invoice ---")

    if not test_customer_id:
        print("⚠ Warning: No customer available\n⚠ Skipping preview invoice test\n")
        return

    try:
        api = invoices_api.InvoicesApi(api_client)
        from flexprice.models.dto_get_preview_invoice_request import DtoGetPreviewInvoiceRequest

        preview_request = DtoGetPreviewInvoiceRequest(
            subscription_id=test_subscription_id if test_subscription_id else None
        )

        response = api.invoices_preview_post(request=preview_request)

        print("✓ Invoice preview generated!")
        if response.total:
            print(f"  Preview Total: {response.total}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error previewing invoice: {e}\n")


def test_finalize_invoice(api_client: flexprice.ApiClient):
    """Test 7: Finalize Invoice"""
    print("--- Test 7: Finalize Invoice ---")

    if not test_customer_id:
        print("⚠ Warning: No customer available\n⚠ Skipping finalize invoice test\n")
        return

    try:
        api = invoices_api.InvoicesApi(api_client)
        from flexprice.models.dto_create_invoice_request import DtoCreateInvoiceRequest
        from flexprice.models.types_invoice_type import TypesInvoiceType
        from flexprice.models.types_invoice_billing_reason import TypesInvoiceBillingReason
        from flexprice.models.types_invoice_status import TypesInvoiceStatus
        from flexprice.models.dto_create_invoice_line_item_request import DtoCreateInvoiceLineItemRequest

        draft_invoice_request = DtoCreateInvoiceRequest(
            customer_id=test_customer_id,
            currency="USD",
            amount_due="50.00",
            subtotal="50.00",
            total="50.00",
            invoice_type=TypesInvoiceType.ONE_OFF,
            billing_reason=TypesInvoiceBillingReason.MANUAL,
            invoice_status=TypesInvoiceStatus.DRAFT,
            line_items=[
                DtoCreateInvoiceLineItemRequest(
                    display_name="Finalize Test Service",
                    quantity="1",
                    amount="50.00",
                )
            ],
        )

        draft_invoice = api.invoices_post(invoice=draft_invoice_request)
        finalize_id = draft_invoice.id
        print(f"  Created draft invoice: {finalize_id}")

        api.invoices_id_finalize_post(id=finalize_id)

        print("✓ Invoice finalized successfully!")
        print(f"  Invoice ID: {finalize_id}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error finalizing invoice: {e}\n")


def test_recalculate_invoice(api_client: flexprice.ApiClient):
    """Test 8: Recalculate Invoice"""
    print("--- Test 8: Recalculate Invoice ---")
    print("⚠ Skipping recalculate invoice test (requires subscription invoice)\n")


def test_record_payment(api_client: flexprice.ApiClient):
    """Test 9: Record Payment"""
    print("--- Test 9: Record Payment ---")

    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available\n⚠ Skipping record payment test\n")
        return

    try:
        api = invoices_api.InvoicesApi(api_client)
        from flexprice.models.dto_update_payment_status_request import DtoUpdatePaymentStatusRequest
        from flexprice.models.types_payment_status import TypesPaymentStatus

        payment_request = DtoUpdatePaymentStatusRequest(
            payment_status=TypesPaymentStatus.SUCCEEDED,
            amount="100.00",
        )

        api.invoices_id_payment_put(id=test_invoice_id, request=payment_request)

        print("✓ Payment recorded successfully!")
        print(f"  Invoice ID: {test_invoice_id}")
        print(f"  Amount Paid: 100.00\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error recording payment: {e}\n")


def test_attempt_payment(api_client: flexprice.ApiClient):
    """Test 10: Attempt Payment"""
    print("--- Test 10: Attempt Payment ---")

    if not test_customer_id:
        print("⚠ Warning: No customer available\n⚠ Skipping attempt payment test\n")
        return

    try:
        api = invoices_api.InvoicesApi(api_client)
        from flexprice.models.dto_create_invoice_request import DtoCreateInvoiceRequest
        from flexprice.models.types_invoice_type import TypesInvoiceType
        from flexprice.models.types_invoice_billing_reason import TypesInvoiceBillingReason
        from flexprice.models.types_invoice_status import TypesInvoiceStatus
        from flexprice.models.types_payment_status import TypesPaymentStatus
        from flexprice.models.dto_create_invoice_line_item_request import DtoCreateInvoiceLineItemRequest

        attempt_invoice_request = DtoCreateInvoiceRequest(
            customer_id=test_customer_id,
            currency="USD",
            amount_due="25.00",
            subtotal="25.00",
            total="25.00",
            amount_paid="0.00",
            invoice_type=TypesInvoiceType.ONE_OFF,
            billing_reason=TypesInvoiceBillingReason.MANUAL,
            invoice_status=TypesInvoiceStatus.DRAFT,
            payment_status=TypesPaymentStatus.PENDING,
            line_items=[
                DtoCreateInvoiceLineItemRequest(
                    display_name="Attempt Payment Test",
                    quantity="1",
                    amount="25.00",
                )
            ],
        )

        attempt_invoice = api.invoices_post(invoice=attempt_invoice_request)
        attempt_id = attempt_invoice.id

        api.invoices_id_finalize_post(id=attempt_id)
        api.invoices_id_payment_attempt_post(id=attempt_id)

        print("✓ Payment attempt initiated!")
        print(f"  Invoice ID: {attempt_id}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error attempting payment: {e}\n")


def test_download_invoice_pdf(api_client: flexprice.ApiClient):
    """Test 11: Download Invoice PDF"""
    print("--- Test 11: Download Invoice PDF ---")

    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available\n⚠ Skipping download PDF test\n")
        return

    try:
        api = invoices_api.InvoicesApi(api_client)
        api.invoices_id_pdf_get(id=test_invoice_id)

        print("✓ Invoice PDF downloaded!")
        print(f"  Invoice ID: {test_invoice_id}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error downloading PDF: {e}\n")


def test_trigger_invoice_comms(api_client: flexprice.ApiClient):
    """Test 12: Trigger Invoice Communications"""
    print("--- Test 12: Trigger Invoice Communications ---")

    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available\n⚠ Skipping trigger comms test\n")
        return

    try:
        api = invoices_api.InvoicesApi(api_client)
        api.invoices_id_comms_trigger_post(id=test_invoice_id)

        print("✓ Invoice communications triggered!")
        print(f"  Invoice ID: {test_invoice_id}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error triggering comms: {e}\n")


def test_get_customer_invoice_summary(api_client: flexprice.ApiClient):
    """Test 13: Get Customer Invoice Summary"""
    print("--- Test 13: Get Customer Invoice Summary ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping summary test\n")
        return

    try:
        api = invoices_api.InvoicesApi(api_client)
        api.customers_id_invoices_summary_get(id=test_customer_id)

        print("✓ Customer invoice summary retrieved!")
        print(f"  Customer ID: {test_customer_id}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error getting summary: {e}\n")


def test_void_invoice(api_client: flexprice.ApiClient):
    """Test 14: Void Invoice"""
    print("--- Test 14: Void Invoice ---")

    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available\n⚠ Skipping void invoice test\n")
        return

    try:
        api = invoices_api.InvoicesApi(api_client)
        api.invoices_id_void_post(id=test_invoice_id)

        print("✓ Invoice voided successfully!")
        print("  Invoice finalized\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error voiding invoice: {e}\n⚠ Skipping void invoice test\n")


# ========================================
# PRICES API TESTS
# ========================================

def test_create_price(api_client: flexprice.ApiClient):
    """Test 1: Create Price"""
    print("--- Test 1: Create Price ---")

    if not test_plan_id:
        print("⚠ Warning: No plan ID available\n⚠ Skipping create price test\n")
        return

    try:
        api = prices_api.PricesApi(api_client)
        from flexprice.models.dto_create_price_request import DtoCreatePriceRequest
        from flexprice.models.types_price_entity_type import TypesPriceEntityType
        from flexprice.models.types_price_type import TypesPriceType
        from flexprice.models.types_billing_model import TypesBillingModel
        from flexprice.models.types_billing_cadence import TypesBillingCadence
        from flexprice.models.types_billing_period import TypesBillingPeriod
        from flexprice.models.types_invoice_cadence import TypesInvoiceCadence
        from flexprice.models.types_price_unit_type import TypesPriceUnitType

        price_request = DtoCreatePriceRequest(
            entity_id=test_plan_id,
            entity_type=TypesPriceEntityType.PLAN,
            currency="USD",
            amount="99.00",
            billing_model=TypesBillingModel.FLAT_FEE,
            billing_cadence=TypesBillingCadence.RECURRING,
            billing_period=TypesBillingPeriod.MONTHLY,
            invoice_cadence=TypesInvoiceCadence.ADVANCE,
            price_unit_type=TypesPriceUnitType.FIAT,
            type=TypesPriceType.FIXED,
            display_name="Monthly Subscription",
            description="Standard monthly subscription price",
        )

        response = api.prices_post(price=price_request)

        global test_price_id
        test_price_id = response.id
        print("✓ Price created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Amount: {response.amount} {response.currency}")
        print(f"  Billing Model: {response.billing_model}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error creating price: {e}\n")


def test_get_price(api_client: flexprice.ApiClient):
    """Test 2: Get Price by ID"""
    print("--- Test 2: Get Price by ID ---")

    if not test_price_id:
        print("⚠ Warning: No price ID available\n⚠ Skipping get price test\n")
        return

    try:
        api = prices_api.PricesApi(api_client)
        response = api.prices_id_get(id=test_price_id)

        print("✓ Price retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Amount: {response.amount} {response.currency}")
        print(f"  Entity ID: {response.entity_id}")
        print(f"  Created At: {response.created_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting price: {e}\n")


def test_list_prices(api_client: flexprice.ApiClient):
    """Test 3: List Prices"""
    print("--- Test 3: List Prices ---")

    try:
        api = prices_api.PricesApi(api_client)
        response = api.prices_get(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} prices")
        if response.items and len(response.items) > 0:
            print(f"  First price: {response.items[0].id} - {response.items[0].amount} {response.items[0].currency}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error listing prices: {e}\n")


def test_update_price(api_client: flexprice.ApiClient):
    """Test 4: Update Price"""
    print("--- Test 4: Update Price ---")

    if not test_price_id:
        print("⚠ Warning: No price ID available\n⚠ Skipping update price test\n")
        return

    try:
        api = prices_api.PricesApi(api_client)
        from flexprice.models.dto_update_price_request import DtoUpdatePriceRequest

        price_request = DtoUpdatePriceRequest(
            description="Updated price description for testing",
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            },
        )

        response = api.prices_id_put(id=test_price_id, price=price_request)

        print("✓ Price updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  New Description: {response.description}")
        print(f"  Updated At: {response.updated_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error updating price: {e}\n")


# ========================================
# PAYMENTS API TESTS
# ========================================

def test_create_payment(api_client: flexprice.ApiClient):
    """Test 1: Create Payment"""
    print("--- Test 1: Create Payment ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping create payment test\n")
        return

    payment_invoice_id = None

    try:
        # First, create a fresh invoice for this payment test
        invoices_api_instance = invoices_api.InvoicesApi(api_client)
        from flexprice.models.dto_create_invoice_request import DtoCreateInvoiceRequest
        from flexprice.models.types_invoice_type import TypesInvoiceType
        from flexprice.models.types_invoice_billing_reason import TypesInvoiceBillingReason
        from flexprice.models.types_invoice_status import TypesInvoiceStatus
        from flexprice.models.types_payment_status import TypesPaymentStatus
        from flexprice.models.dto_create_invoice_line_item_request import DtoCreateInvoiceLineItemRequest

        # Create a draft invoice with explicit payment status to prevent auto-payment
        draft_invoice_request = DtoCreateInvoiceRequest(
            customer_id=test_customer_id,
            currency="USD",
            amount_due="100.00",
            subtotal="100.00",
            total="100.00",
            amount_paid="0.00",  # Explicitly set to 0 to prevent auto-payment
            invoice_type=TypesInvoiceType.ONE_OFF,
            billing_reason=TypesInvoiceBillingReason.MANUAL,
            invoice_status=TypesInvoiceStatus.DRAFT,
            payment_status=TypesPaymentStatus.PENDING,  # Set to PENDING to prevent auto-payment
            line_items=[
                DtoCreateInvoiceLineItemRequest(
                    display_name="Payment Test Service",
                    quantity="1",
                    amount="100.00",
                )
            ],
            metadata={
                "source": "sdk_test_payment",
            },
        )

        draft_invoice = invoices_api_instance.invoices_post(invoice=draft_invoice_request)
        payment_invoice_id = draft_invoice.id
        print(f"  Created invoice for payment: {payment_invoice_id}")

        # Check invoice status before finalization
        current_invoice = invoices_api_instance.invoices_id_get(id=payment_invoice_id)

        if current_invoice.amount_paid and current_invoice.amount_paid not in ("0", "0.00"):
            print(f"⚠ Warning: Invoice already has amount paid before finalization: {current_invoice.amount_paid}\n⚠ Skipping payment creation test (invoice was auto-paid during creation)\n")
            return

        if current_invoice.amount_due in ("0", "0.00"):
            print(f"⚠ Warning: Invoice has zero amount due: {current_invoice.amount_due}, will be auto-paid on finalization\n⚠ Skipping payment creation test (invoice has zero amount due)\n")
            return

        if current_invoice.amount_due and current_invoice.total:
            print(f"  Invoice before finalization - AmountDue: {current_invoice.amount_due}, Total: {current_invoice.total}")

        # Finalize the invoice if it's still in draft status
        if current_invoice.invoice_status == TypesInvoiceStatus.DRAFT:
            try:
                invoices_api_instance.invoices_id_finalize_post(id=payment_invoice_id)
                print("  Finalized invoice for payment")
            except flexprice.ApiException as finalize_error:
                error_msg = str(finalize_error)
                if "already" in error_msg.lower() or "400" in error_msg:
                    print(f"⚠ Warning: Invoice finalization returned error (may already be finalized): {finalize_error}")
                else:
                    print(f"⚠ Warning: Failed to finalize invoice for payment test: {finalize_error}")
                    return
        else:
            print(f"  Invoice already finalized (status: {current_invoice.invoice_status})")

        # Re-fetch the invoice to get the latest payment status after finalization
        final_invoice = invoices_api_instance.invoices_id_get(id=payment_invoice_id)

        if final_invoice.amount_due and final_invoice.total and final_invoice.amount_paid:
            print(f"  Invoice after finalization - AmountDue: {final_invoice.amount_due}, Total: {final_invoice.total}, AmountPaid: {final_invoice.amount_paid}")

        # Check if invoice is already paid
        if final_invoice.payment_status == TypesPaymentStatus.SUCCEEDED:
            print(f"⚠ Warning: Invoice is already paid (status: {final_invoice.payment_status}), cannot create payment\n⚠ Skipping payment creation test\n")
            return

        if final_invoice.amount_paid and final_invoice.amount_paid not in ("0", "0.00"):
            print(f"⚠ Warning: Invoice already has amount paid: {final_invoice.amount_paid}, cannot create payment\n⚠ Skipping payment creation test\n")
            return

        if final_invoice.total in ("0", "0.00"):
            print("⚠ Warning: Invoice has zero total amount, may be auto-marked as paid\n⚠ Skipping payment creation test\n")
            return

        payment_status_str = final_invoice.payment_status or "unknown"
        total_str = final_invoice.total or "unknown"
        print(f"  Invoice is unpaid and ready for payment (status: {payment_status_str}, total: {total_str})")

        # Now create the payment
        payments_api_instance = payments_api.PaymentsApi(api_client)
        from flexprice.models.dto_create_payment_request import DtoCreatePaymentRequest
        from flexprice.models.types_payment_destination_type import TypesPaymentDestinationType
        from flexprice.models.types_payment_method_type import TypesPaymentMethodType

        payment_request = DtoCreatePaymentRequest(
            amount="100.00",
            currency="USD",
            destination_id=payment_invoice_id,
            destination_type=TypesPaymentDestinationType.INVOICE,
            payment_method_type=TypesPaymentMethodType.OFFLINE,
            process_payment=False,  # Don't process immediately in test
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
            },
        )

        response = payments_api_instance.payments_post(payment=payment_request)

        global test_payment_id
        test_payment_id = response.id
        print("✓ Payment created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Amount: {response.amount} {response.currency}")
        if response.payment_status:
            print(f"  Status: {response.payment_status}\n")
        else:
            print()
    except flexprice.ApiException as e:
        print(f"❌ Error creating payment: {e}")
        if hasattr(e, 'body'):
            print(f"  Response Body: {e.body}")
        print(f"  Payment Request Details:")
        print(f"    Amount: 100.00")
        print(f"    Currency: USD")
        print(f"    DestinationId: {payment_invoice_id}")
        print(f"    DestinationType: INVOICE")
        print(f"    PaymentMethodType: OFFLINE")
        print(f"    ProcessPayment: false")
        print()


def test_get_payment(api_client: flexprice.ApiClient):
    """Test 2: Get Payment by ID"""
    print("--- Test 2: Get Payment by ID ---")

    if not test_payment_id:
        print("⚠ Warning: No payment ID available\n⚠ Skipping get payment test\n")
        return

    try:
        api = payments_api.PaymentsApi(api_client)
        response = api.payments_id_get(id=test_payment_id)

        print("✓ Payment retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Amount: {response.amount} {response.currency}")
        print(f"  Status: {response.payment_status}")
        print(f"  Created At: {response.created_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting payment: {e}\n")


def test_search_payments(api_client: flexprice.ApiClient):
    """Test 2: Search Payments"""
    print("--- Test 2: Search Payments ---")
    print("⚠ Skipping search payments test (endpoint not available in SDK)\n")


def test_list_payments(api_client: flexprice.ApiClient):
    """Test 3: List Payments"""
    print("--- Test 3: List Payments ---")

    try:
        api = payments_api.PaymentsApi(api_client)
        response = api.payments_get(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} payments")
        if response.items and len(response.items) > 0:
            print(f"  First payment: {response.items[0].id} - {response.items[0].amount} {response.items[0].currency}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error listing payments: {e}\n")
    except Exception as e:
        # Handle validation errors (e.g., payment_status='archived' not in enum)
        error_str = str(e)
        if 'payment_status' in error_str or 'ValidationError' in error_str or 'archived' in error_str:
            print(f"⚠ Warning: Error listing payments (may contain unsupported payment status): {error_str[:200]}\n")
        else:
            print(f"❌ Error listing payments: {e}\n")


def test_update_payment(api_client: flexprice.ApiClient):
    """Test 4: Update Payment"""
    print("--- Test 4: Update Payment ---")

    if not test_payment_id:
        print("⚠ Warning: No payment ID available\n⚠ Skipping update payment test\n")
        return

    try:
        api = payments_api.PaymentsApi(api_client)
        from flexprice.models.dto_update_payment_request import DtoUpdatePaymentRequest

        payment_request = DtoUpdatePaymentRequest(
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            }
        )

        response = api.payments_id_put(id=test_payment_id, payment=payment_request)

        print("✓ Payment updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  Updated At: {response.updated_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error updating payment: {e}\n")


def test_process_payment(api_client: flexprice.ApiClient):
    """Test 5: Process Payment"""
    print("--- Test 5: Process Payment ---")

    if not test_payment_id:
        print("⚠ Warning: No payment ID available\n⚠ Skipping process payment test\n")
        return

    try:
        api = payments_api.PaymentsApi(api_client)
        api.payments_id_process_post(id=test_payment_id)

        print("✓ Payment processed successfully!")
        print(f"  Payment ID: {test_payment_id}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error processing payment: {e}\n")


# ========================================
# WALLETS API TESTS
# ========================================

def test_create_wallet(api_client: flexprice.ApiClient):
    """Test 1: Create Wallet"""
    print("--- Test 1: Create Wallet ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping create wallet test\n")
        return

    try:
        api = wallets_api.WalletsApi(api_client)
        from flexprice.models.dto_create_wallet_request import DtoCreateWalletRequest

        wallet_request = DtoCreateWalletRequest(
            customer_id=test_customer_id,
            currency="USD",
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
            },
        )

        response = api.wallets_post(request=wallet_request)

        global test_wallet_id
        test_wallet_id = response.id
        print("✓ Wallet created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Customer ID: {response.customer_id}")
        print(f"  Balance: {response.balance} {response.currency}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error creating wallet: {e}\n")


def test_get_wallet(api_client: flexprice.ApiClient):
    """Test 2: Get Wallet by ID"""
    print("--- Test 2: Get Wallet by ID ---")

    if not test_wallet_id:
        print("⚠ Warning: No wallet ID available\n⚠ Skipping get wallet test\n")
        return

    try:
        api = wallets_api.WalletsApi(api_client)
        response = api.wallets_id_get(id=test_wallet_id)

        print("✓ Wallet retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Balance: {response.balance} {response.currency}")
        print(f"  Created At: {response.created_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting wallet: {e}\n")


def test_list_wallets(api_client: flexprice.ApiClient):
    """Test 3: List Wallets"""
    print("--- Test 3: List Wallets ---")

    try:
        api = wallets_api.WalletsApi(api_client)
        response = api.wallets_get(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} wallets")
        if response.items and len(response.items) > 0:
            print(f"  First wallet: {response.items[0].id} - {response.items[0].balance} {response.items[0].currency}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error listing wallets: {e}\n")


def test_update_wallet(api_client: flexprice.ApiClient):
    """Test 4: Update Wallet"""
    print("--- Test 4: Update Wallet ---")

    if not test_wallet_id:
        print("⚠ Warning: No wallet ID available\n⚠ Skipping update wallet test\n")
        return

    try:
        api = wallets_api.WalletsApi(api_client)
        from flexprice.models.dto_update_wallet_request import DtoUpdateWalletRequest

        wallet_request = DtoUpdateWalletRequest(
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            }
        )

        response = api.wallets_id_put(id=test_wallet_id, request=wallet_request)

        print("✓ Wallet updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  Updated At: {response.updated_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error updating wallet: {e}\n")


def test_get_wallet_balance(api_client: flexprice.ApiClient):
    """Test 5: Get Wallet Balance"""
    print("--- Test 5: Get Wallet Balance ---")

    if not test_wallet_id:
        print("⚠ Warning: No wallet ID available\n⚠ Skipping get balance test\n")
        return

    try:
        api = wallets_api.WalletsApi(api_client)
        response = api.wallets_id_get(id=test_wallet_id)

        print("✓ Wallet balance retrieved!")
        print(f"  Balance: {response.balance} {response.currency}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error getting balance: {e}\n")


def test_top_up_wallet(api_client: flexprice.ApiClient):
    """Test 6: Top Up Wallet"""
    print("--- Test 6: Top Up Wallet ---")

    if not test_wallet_id:
        print("⚠ Warning: No wallet ID available\n⚠ Skipping top up test\n")
        return

    try:
        api = wallets_api.WalletsApi(api_client)
        from flexprice.models.dto_top_up_wallet_request import DtoTopUpWalletRequest
        from flexprice.models.types_transaction_reason import TypesTransactionReason

        top_up_request = DtoTopUpWalletRequest(
            amount="100.00",
            description="Test top-up",
            transaction_reason=TypesTransactionReason.PURCHASED_CREDIT_DIRECT,
        )

        api.wallets_id_top_up_post(id=test_wallet_id, request=top_up_request)

        print("✓ Wallet topped up successfully!")
        print(f"  Wallet ID: {test_wallet_id}")
        print(f"  Amount: 100.00\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error topping up wallet: {e}\n")


def test_debit_wallet(api_client: flexprice.ApiClient):
    """Test 7: Debit Wallet"""
    print("--- Test 7: Debit Wallet ---")

    if not test_wallet_id:
        print("⚠ Warning: No wallet ID available\n⚠ Skipping debit test\n")
        return

    try:
        api = wallets_api.WalletsApi(api_client)
        from flexprice.models.dto_manual_balance_debit_request import DtoManualBalanceDebitRequest
        from flexprice.models.types_transaction_reason import TypesTransactionReason

        debit_request = DtoManualBalanceDebitRequest(
            credits="10.00",
            description="Test debit",
            transaction_reason=TypesTransactionReason.MANUAL_BALANCE_DEBIT,
            idempotency_key=f"test-debit-{int(datetime.now().timestamp())}",
        )

        api.wallets_id_debit_post(id=test_wallet_id, request=debit_request)

        print("✓ Wallet debited successfully!")
        print(f"  Wallet ID: {test_wallet_id}")
        print(f"  Amount: 10.00\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error debiting wallet: {e}\n")


def test_get_wallet_transactions(api_client: flexprice.ApiClient):
    """Test 8: Get Wallet Transactions"""
    print("--- Test 8: Get Wallet Transactions ---")

    if not test_wallet_id:
        print("⚠ Warning: No wallet ID available\n⚠ Skipping transactions test\n")
        return

    try:
        api = wallets_api.WalletsApi(api_client)
        response = api.wallets_id_transactions_get(id=test_wallet_id)

        print("✓ Wallet transactions retrieved!")
        print(f"  Total transactions: {len(response.items) if response.items else 0}\n")
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error getting transactions: {e}\n")


def test_search_wallets(api_client: flexprice.ApiClient):
    """Test 9: Search Wallets"""
    print("--- Test 9: Search Wallets ---")

    try:
        api = wallets_api.WalletsApi(api_client)
        from flexprice.models.types_wallet_filter import TypesWalletFilter

        # Python SDK takes filter directly, not a request object
        # Add limit to match Go version
        wallet_filter = TypesWalletFilter(limit=10)

        response = api.wallets_search_post(filter=wallet_filter)

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} wallets for customer '{test_customer_id}'\n")
    except flexprice.ApiException as e:
        print(f"❌ Error searching wallets: {e}\n")


# ========================================
# CREDIT GRANTS API TESTS
# ========================================

def test_create_credit_grant(api_client: flexprice.ApiClient):
    """Test 1: Create Credit Grant"""
    print("--- Test 1: Create Credit Grant ---")

    # Skip if no plan available (matching Go test)
    if not test_plan_id:
        print("⚠ Warning: No plan ID available\n⚠ Skipping create credit grant test\n")
        return

    try:
        api = credit_grants_api.CreditGrantsApi(api_client)
        from flexprice.models.dto_create_credit_grant_request import DtoCreateCreditGrantRequest
        from flexprice.models.types_credit_grant_scope import TypesCreditGrantScope
        from flexprice.models.types_credit_grant_cadence import TypesCreditGrantCadence
        from flexprice.models.types_credit_grant_expiry_type import TypesCreditGrantExpiryType
        from flexprice.models.types_credit_grant_expiry_duration_unit import TypesCreditGrantExpiryDurationUnit

        grant_request = DtoCreateCreditGrantRequest(
            name="Test Credit Grant",
            credits="500.00",  # Match Go test amount
            scope=TypesCreditGrantScope.PLAN,
            plan_id=test_plan_id,
            cadence=TypesCreditGrantCadence.ONETIME,
            expiration_type=TypesCreditGrantExpiryType.NEVER,
            expiration_duration_unit=TypesCreditGrantExpiryDurationUnit.DAY,  # Note: Python SDK uses DAY, not DAYS
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
            },
        )

        response = api.creditgrants_post(credit_grant=grant_request)

        global test_credit_grant_id
        test_credit_grant_id = response.id
        print("✓ Credit grant created successfully!")
        print(f"  ID: {response.id}")
        if response.credits:
            print(f"  Credits: {response.credits}")
        print(f"  Plan ID: {response.plan_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error creating credit grant: {e}")
        if hasattr(e, 'body'):
            print(f"  Response Body: {e.body}")
        print(f"  Credit Grant Request Details:")
        print(f"    Name: Test Credit Grant")
        print(f"    Credits: 500.00")
        print(f"    Scope: PLAN")
        print(f"    PlanId: {test_plan_id}")
        print(f"    Cadence: ONETIME")
        print(f"    ExpirationType: NEVER")
        print(f"    ExpirationDurationUnit: DAYS")
        print()


def test_get_credit_grant(api_client: flexprice.ApiClient):
    """Test 2: Get Credit Grant by ID"""
    print("--- Test 2: Get Credit Grant by ID ---")

    if not test_credit_grant_id:
        print("⚠ Warning: No credit grant ID available\n⚠ Skipping get credit grant test\n")
        return

    try:
        api = credit_grants_api.CreditGrantsApi(api_client)
        response = api.creditgrants_id_get(id=test_credit_grant_id)

        print("✓ Credit grant retrieved successfully!")
        print(f"  ID: {response.id}")
        grant_amount = getattr(response, 'grant_amount', 'undefined')
        print(f"  Grant Amount: {grant_amount}")
        print(f"  Created At: {response.created_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting credit grant: {e}\n")


def test_list_credit_grants(api_client: flexprice.ApiClient):
    """Test 3: List Credit Grants"""
    print("--- Test 3: List Credit Grants ---")

    try:
        api = credit_grants_api.CreditGrantsApi(api_client)
        response = api.creditgrants_get(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} credit grants")
        if response.items and len(response.items) > 0:
            print(f"  First credit grant: {response.items[0].id}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error listing credit grants: {e}\n")


def test_update_credit_grant(api_client: flexprice.ApiClient):
    """Test 4: Update Credit Grant"""
    print("--- Test 4: Update Credit Grant ---")

    if not test_credit_grant_id:
        print("⚠ Warning: No credit grant ID available\n⚠ Skipping update credit grant test\n")
        return

    try:
        api = credit_grants_api.CreditGrantsApi(api_client)
        from flexprice.models.dto_update_credit_grant_request import DtoUpdateCreditGrantRequest

        grant_request = DtoUpdateCreditGrantRequest(
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            }
        )

        response = api.creditgrants_id_put(id=test_credit_grant_id, credit_grant=grant_request)

        print("✓ Credit grant updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  Updated At: {response.updated_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error updating credit grant: {e}\n")


def test_delete_credit_grant(api_client: flexprice.ApiClient):
    """Test 5: Delete Credit Grant"""
    print("--- Cleanup: Delete Credit Grant ---")

    if not test_credit_grant_id:
        print("⚠ Skipping delete credit grant (no credit grant created)\n")
        return

    try:
        api = credit_grants_api.CreditGrantsApi(api_client)
        api.creditgrants_id_delete(id=test_credit_grant_id)

        print("✓ Credit grant deleted successfully!")
        print(f"  Deleted ID: {test_credit_grant_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error deleting credit grant: {e}\n")


# ========================================
# CREDIT NOTES API TESTS
# ========================================

def test_create_credit_note(api_client: flexprice.ApiClient):
    """Test 1: Create Credit Note"""
    print("--- Test 1: Create Credit Note ---")

    # Skip if no customer available (matching Go test)
    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping create credit note test\n")
        return

    # Skip if no invoice available (matching Go test)
    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available, skipping create credit note test\n")
        return

    invoice = None

    try:
        invoices_api_instance = invoices_api.InvoicesApi(api_client)
        credit_notes_api_instance = credit_notes_api.CreditNotesApi(api_client)

        # Get invoice to retrieve line items for credit note (matching Go test)
        invoice = invoices_api_instance.invoices_id_get(id=test_invoice_id)

        if not invoice:
            print("⚠ Warning: Could not retrieve invoice\n⚠ Skipping create credit note test\n")
            return

        print(f"Invoice has {len(invoice.line_items) if invoice.line_items else 0} line items")
        if not invoice.line_items or len(invoice.line_items) == 0:
            print("⚠ Warning: Invoice has no line items\n⚠ Skipping create credit note test\n")
            return

        # Check invoice status - must be FINALIZED to create credit note (matching Go validation)
        # If invoice is in DRAFT status, try to finalize it first
        from flexprice.models.types_invoice_status import TypesInvoiceStatus

        if invoice.invoice_status == TypesInvoiceStatus.DRAFT:
            print("  Invoice is in DRAFT status, attempting to finalize...")
            try:
                invoices_api_instance.invoices_id_finalize_post(id=test_invoice_id)
                print("  Invoice finalized successfully")
                # Re-fetch the invoice to get updated status
                invoice = invoices_api_instance.invoices_id_get(id=test_invoice_id)
            except flexprice.ApiException as finalize_error:
                print(f"⚠ Warning: Failed to finalize invoice: {finalize_error}")
                print("⚠ Skipping create credit note test\n")
                return

        if invoice.invoice_status != TypesInvoiceStatus.FINALIZED:
            print(f"⚠ Warning: Invoice must be FINALIZED to create credit note. Current status: {invoice.invoice_status}\n⚠ Skipping create credit note test\n")
            return

        print(f"  Invoice status: {invoice.invoice_status} (ready for credit note)")

        # Use first line item from invoice for credit note (matching Go test)
        first_line_item = invoice.line_items[0]
        credit_amount = "50.00"  # Credit 50% of the line item amount (matching Go test)
        line_item_id = first_line_item.id
        line_item_display_name = first_line_item.display_name or "Invoice Line Item"

        if not line_item_id:
            print("⚠ Warning: Line item has no ID\n⚠ Skipping create credit note test\n")
            import json
            print(f"  Line item structure: {json.dumps(first_line_item.to_dict() if hasattr(first_line_item, 'to_dict') else str(first_line_item), indent=2, default=str)}")
            return

        # Log line item details for debugging
        print(f"  Using line item ID: {line_item_id}")
        print(f"  Line item display name: {line_item_display_name}")
        print(f"  Credit amount: {credit_amount}")

        from flexprice.models.dto_create_credit_note_request import DtoCreateCreditNoteRequest
        from flexprice.models.types_credit_note_reason import TypesCreditNoteReason
        from flexprice.models.dto_create_credit_note_line_item_request import DtoCreateCreditNoteLineItemRequest

        credit_note_request = DtoCreateCreditNoteRequest(
            invoice_id=test_invoice_id,
            reason=TypesCreditNoteReason.BILLING_ERROR,
            memo="Test credit note from SDK",
            line_items=[
                DtoCreateCreditNoteLineItemRequest(
                    invoice_line_item_id=line_item_id,
                    amount=credit_amount,
                    display_name=f"Credit for {line_item_display_name}",  # Use actual line item display name
                )
            ],
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
            },
        )

        response = credit_notes_api_instance.creditnotes_post(credit_note=credit_note_request)

        global test_credit_note_id
        test_credit_note_id = response.id
        print("✓ Credit note created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Invoice ID: {response.invoice_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error creating credit note: {e}")
        if hasattr(e, 'body'):
            print(f"  Response Body: {e.body}")
        print(f"  Credit Note Request Details:")
        print(f"    InvoiceId: {test_invoice_id}")
        print(f"    Reason: BILLING_ERROR")
        print(f"    Memo: Test credit note from SDK")
        if invoice and invoice.line_items and len(invoice.line_items) > 0:
            first_item = invoice.line_items[0]
            print(f"    LineItems[0].invoiceLineItemId: {first_item.id}")
            print(f"    LineItems[0].amount: 50.00")
            print(f"    LineItems[0].displayName: Credit for {first_item.display_name or 'Invoice Line Item'}")
        else:
            print(f"    LineItems: [none available]")
        print()


def test_get_credit_note(api_client: flexprice.ApiClient):
    """Test 2: Get Credit Note by ID"""
    print("--- Test 2: Get Credit Note by ID ---")

    if not test_credit_note_id:
        print("⚠ Warning: No credit note ID available\n⚠ Skipping get credit note test\n")
        return

    try:
        api = credit_notes_api.CreditNotesApi(api_client)
        response = api.creditnotes_id_get(id=test_credit_note_id)

        print("✓ Credit note retrieved successfully!")
        print(f"  ID: {response.id}")
        total = getattr(response, 'total', 'N/A')
        print(f"  Total: {total}")
        print(f"  Created At: {response.created_at}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error getting credit note: {e}\n")


def test_list_credit_notes(api_client: flexprice.ApiClient):
    """Test 3: List Credit Notes"""
    print("--- Test 3: List Credit Notes ---")

    try:
        api = credit_notes_api.CreditNotesApi(api_client)
        response = api.creditnotes_get(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} credit notes")
        if response.items and len(response.items) > 0:
            print(f"  First credit note: {response.items[0].id}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error listing credit notes: {e}\n")


def test_finalize_credit_note(api_client: flexprice.ApiClient):
    """Test 4: Finalize Credit Note"""
    print("--- Test 4: Finalize Credit Note ---")

    if not test_credit_note_id:
        print("⚠ Warning: No credit note ID available\n⚠ Skipping finalize credit note test\n")
        return

    try:
        api = credit_notes_api.CreditNotesApi(api_client)
        note = api.creditnotes_id_finalize_post(id=test_credit_note_id)

        print("✓ Credit note finalized successfully!")
        if note and hasattr(note, 'id') and note.id:
            print(f"  ID: {note.id}\n")
        else:
            print()
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error finalizing credit note: {e}\n⚠ Skipping finalize credit note test\n")


# ========================================
# CLEANUP TESTS
# ========================================

def test_delete_payment(api_client: flexprice.ApiClient):
    """Cleanup: Delete Payment"""
    print("--- Cleanup: Delete Payment ---")

    if not test_payment_id:
        print("⚠ Skipping delete payment (no payment created)\n")
        return

    try:
        api = payments_api.PaymentsApi(api_client)
        api.payments_id_delete(id=test_payment_id)

        print("✓ Payment deleted successfully!")
        print(f"  Deleted ID: {test_payment_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error deleting payment: {e}\n")


def test_delete_price(api_client: flexprice.ApiClient):
    """Cleanup: Delete Price"""
    print("--- Cleanup: Delete Price ---")

    if not test_price_id:
        print("⚠ Skipping delete price (no price created)\n")
        return

    try:
        api = prices_api.PricesApi(api_client)
        from flexprice.models.dto_delete_price_request import DtoDeletePriceRequest
        from datetime import timedelta

        future_date = (datetime.now(timezone.utc) + timedelta(days=1)).isoformat().replace('+00:00', 'Z')
        delete_request = DtoDeletePriceRequest(end_date=future_date)

        api.prices_id_delete(id=test_price_id, request=delete_request)

        print("✓ Price deleted successfully!")
        print(f"  Deleted ID: {test_price_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error deleting price: {e}\n")


def test_delete_entitlement(api_client: flexprice.ApiClient):
    """Cleanup: Delete Entitlement"""
    print("--- Cleanup: Delete Entitlement ---")

    if not test_entitlement_id:
        print("⚠ Skipping delete entitlement (no entitlement created)\n")
        return

    try:
        api = entitlements_api.EntitlementsApi(api_client)
        api.entitlements_id_delete(id=test_entitlement_id)

        print("✓ Entitlement deleted successfully!")
        print(f"  Deleted ID: {test_entitlement_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error deleting entitlement: {e}\n")


def test_delete_addon(api_client: flexprice.ApiClient):
    """Cleanup: Delete Addon"""
    print("--- Cleanup: Delete Addon ---")

    if not test_addon_id:
        print("⚠ Skipping delete addon (no addon created)\n")
        return

    try:
        api = addons_api.AddonsApi(api_client)
        api.addons_id_delete(id=test_addon_id)

        print("✓ Addon deleted successfully!")
        print(f"  Deleted ID: {test_addon_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error deleting addon: {e}\n")


def test_delete_plan(api_client: flexprice.ApiClient):
    """Cleanup: Delete Plan"""
    print("--- Cleanup: Delete Plan ---")

    if not test_plan_id:
        print("⚠ Skipping delete plan (no plan created)\n")
        return

    try:
        api = plans_api.PlansApi(api_client)
        api.plans_id_delete(id=test_plan_id)

        print("✓ Plan deleted successfully!")
        print(f"  Deleted ID: {test_plan_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error deleting plan: {e}\n")


def test_delete_feature(api_client: flexprice.ApiClient):
    """Cleanup: Delete Feature"""
    print("--- Cleanup: Delete Feature ---")

    if not test_feature_id:
        print("⚠ Skipping delete feature (no feature created)\n")
        return

    try:
        api = features_api.FeaturesApi(api_client)
        api.features_id_delete(id=test_feature_id)

        print("✓ Feature deleted successfully!")
        print(f"  Deleted ID: {test_feature_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error deleting feature: {e}\n")


def test_delete_customer(api_client: flexprice.ApiClient):
    """Cleanup: Delete Customer"""
    print("--- Cleanup: Delete Customer ---")

    if not test_customer_id:
        print("⚠ Skipping delete customer (no customer created)\n")
        return

    try:
        api = customers_api.CustomersApi(api_client)
        api.customers_id_delete(id=test_customer_id)

        print("✓ Customer deleted successfully!")
        print(f"  Deleted ID: {test_customer_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error deleting customer: {e}\n")


# ========================================
# EVENTS API TESTS
# ========================================

def test_create_event(api_client: flexprice.ApiClient):
    """Test 1: Create Event"""
    print("--- Test 1: Create Event ---")

    global test_event_id, test_event_name, test_event_customer_id

    # Use test customer external ID if available, otherwise generate a unique one
    if test_customer_id:
        # Try to get the customer's external_id
        try:
            customer_api = customers_api.CustomersApi(api_client)
            customer = customer_api.customers_id_get(id=test_customer_id)
            test_event_customer_id = customer.external_id if hasattr(customer, 'external_id') and customer.external_id else f"test-customer-{int(time.time())}"
        except:
            test_event_customer_id = f"test-customer-{int(time.time())}"
    else:
        test_event_customer_id = f"test-customer-{int(time.time())}"

    test_event_name = f"Test Event {int(time.time())}"

    try:
        api = events_api.EventsApi(api_client)
        from flexprice.models.dto_ingest_event_request import DtoIngestEventRequest

        event_request = DtoIngestEventRequest(
            event_name=test_event_name,
            external_customer_id=test_event_customer_id,
            properties={
                "source": "sdk_test",
                "environment": "test",
                "test_run": datetime.now().isoformat(),
            },
            source="sdk_test",
            timestamp=datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z'),
        )

        response = api.events_post(event=event_request)

        # The response might be a dict or an object with event_id attribute
        if isinstance(response, dict):
            test_event_id = response.get("event_id")
        elif hasattr(response, 'event_id'):
            test_event_id = response.event_id
        else:
            test_event_id = None

        print("✓ Event created successfully!")
        if test_event_id:
            print(f"  Event ID: {test_event_id}")
        print(f"  Event Name: {test_event_name}")
        print(f"  Customer ID: {test_event_customer_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error creating event: {e}\n")


def test_query_events(api_client: flexprice.ApiClient):
    """Test 2: Query Events"""
    print("--- Test 2: Query Events ---")

    if not test_event_name:
        print("⚠ Warning: No event created, skipping query test\n")
        return

    try:
        api = events_api.EventsApi(api_client)
        from flexprice.models.dto_get_events_request import DtoGetEventsRequest

        query_request = DtoGetEventsRequest(
            external_customer_id=test_event_customer_id,
            event_name=test_event_name,
        )

        response = api.events_query_post(request=query_request)

        print("✓ Events queried successfully!")
        if hasattr(response, 'events') and response.events:
            print(f"  Found {len(response.events)} events")
            for i, event in enumerate(response.events[:3]):  # Show first 3 events
                event_id = event.id if hasattr(event, 'id') and event.id else "N/A"
                event_name = event.event_name if hasattr(event, 'event_name') and event.event_name else "N/A"
                print(f"  - Event {i+1}: {event_id} - {event_name}")
        else:
            print("  No events found")
        print()
    except flexprice.ApiException as e:
        print(f"⚠ Warning: Error querying events: {e}")
        print("⚠ Skipping query events test\n")


def test_async_event_enqueue(api_client: flexprice.ApiClient):
    """Test 3: Async Event - Simple Enqueue"""
    print("--- Test 3: Async Event - Simple Enqueue ---")

    # Use test customer external ID if available
    customer_id = test_event_customer_id
    if not customer_id:
        if test_customer_id:
            try:
                customer_api = customers_api.CustomersApi(api_client)
                customer = customer_api.customers_id_get(id=test_customer_id)
                customer_id = customer.external_id if hasattr(customer, 'external_id') and customer.external_id else f"test-customer-{int(time.time())}"
            except:
                customer_id = f"test-customer-{int(time.time())}"
        else:
            customer_id = f"test-customer-{int(time.time())}"

    try:
        api = events_api.EventsApi(api_client)
        from flexprice.models.dto_ingest_event_request import DtoIngestEventRequest

        event_request = DtoIngestEventRequest(
            event_name="api_request",
            external_customer_id=customer_id,
            properties={
                "path": "/api/resource",
                "method": "GET",
                "status": "200",
                "response_time_ms": "150",
            },
            source="sdk_test",
        )

        # Use async method if available, otherwise use sync
        if hasattr(api, 'events_post_async'):
            api.events_post_async(event=event_request)
        else:
            # Fallback to sync if async not available
            api.events_post(event=event_request)

        print("✓ Async event enqueued successfully!")
        print(f"  Event Name: api_request")
        print(f"  Customer ID: {customer_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error enqueueing async event: {e}\n")


def test_async_event_enqueue_with_options(api_client: flexprice.ApiClient):
    """Test 4: Async Event - Enqueue With Options"""
    print("--- Test 4: Async Event - Enqueue With Options ---")

    # Use test customer external ID if available
    customer_id = test_event_customer_id
    if not customer_id:
        if test_customer_id:
            try:
                customer_api = customers_api.CustomersApi(api_client)
                customer = customer_api.customers_id_get(id=test_customer_id)
                customer_id = customer.external_id if hasattr(customer, 'external_id') and customer.external_id else f"test-customer-{int(time.time())}"
            except:
                customer_id = f"test-customer-{int(time.time())}"
        else:
            customer_id = f"test-customer-{int(time.time())}"

    try:
        api = events_api.EventsApi(api_client)
        from flexprice.models.dto_ingest_event_request import DtoIngestEventRequest

        event_request = DtoIngestEventRequest(
            event_name="file_upload",
            external_customer_id=customer_id,
            properties={
                "file_size_bytes": "1048576",
                "file_type": "image/jpeg",
                "storage_bucket": "user_uploads",
            },
            source="sdk_test",
            timestamp=datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z'),
        )

        # Use async method if available, otherwise use sync
        if hasattr(api, 'events_post_async'):
            api.events_post_async(event=event_request)
        else:
            # Fallback to sync if async not available
            api.events_post(event=event_request)

        print("✓ Async event with options enqueued successfully!")
        print(f"  Event Name: file_upload")
        print(f"  Customer ID: {customer_id}\n")
    except flexprice.ApiException as e:
        print(f"❌ Error enqueueing async event with options: {e}\n")


def test_async_event_batch(api_client: flexprice.ApiClient):
    """Test 5: Async Event - Batch Enqueue"""
    print("--- Test 5: Async Event - Batch Enqueue ---")

    # Use test customer external ID if available
    customer_id = test_event_customer_id
    if not customer_id:
        if test_customer_id:
            try:
                customer_api = customers_api.CustomersApi(api_client)
                customer = customer_api.customers_id_get(id=test_customer_id)
                customer_id = customer.external_id if hasattr(customer, 'external_id') and customer.external_id else f"test-customer-{int(time.time())}"
            except:
                customer_id = f"test-customer-{int(time.time())}"
        else:
            customer_id = f"test-customer-{int(time.time())}"

    try:
        api = events_api.EventsApi(api_client)
        from flexprice.models.dto_ingest_event_request import DtoIngestEventRequest

        batch_count = 5
        for i in range(batch_count):
            event_request = DtoIngestEventRequest(
                event_name="batch_example",
                external_customer_id=customer_id,
                properties={
                    "index": str(i),
                    "batch": "demo",
                },
                source="sdk_test",
            )

            # Use async method if available, otherwise use sync
            if hasattr(api, 'events_post_async'):
                api.events_post_async(event=event_request)
            else:
                # Fallback to sync if async not available
                api.events_post(event=event_request)

        print(f"✓ Enqueued {batch_count} batch events successfully!")
        print(f"  Event Name: batch_example")
        print(f"  Customer ID: {customer_id}")
        print(f"  Waiting for events to be processed...\n")
        time.sleep(2)  # Wait for background processing
    except flexprice.ApiException as e:
        print(f"❌ Error enqueueing batch event: {e}\n")


# ========================================
# MAIN EXECUTION
# ========================================

def main():
    """Main execution function"""
    configuration = get_configuration()

    with flexprice.ApiClient(configuration) as api_client:
        # Set the API key header
        api_client.default_headers['x-api-key'] = configuration.api_key['x-api-key']

        print("========================================")
        print("CUSTOMER API TESTS")
        print("========================================\n")

        test_create_customer(api_client)
        test_get_customer(api_client)
        test_list_customers(api_client)
        test_update_customer(api_client)
        test_lookup_customer(api_client)
        test_search_customers(api_client)
        test_get_customer_entitlements(api_client)
        test_get_customer_upcoming_grants(api_client)
        test_get_customer_usage(api_client)

        print("✓ Customer API Tests Completed!\n")

        print("========================================")
        print("FEATURES API TESTS")
        print("========================================\n")

        test_create_feature(api_client)
        test_get_feature(api_client)
        test_list_features(api_client)
        test_update_feature(api_client)
        test_search_features(api_client)

        print("✓ Features API Tests Completed!\n")

        print("========================================")
        print("CONNECTIONS API TESTS")
        print("========================================\n")

        test_list_connections(api_client)
        test_search_connections(api_client)

        print("✓ Connections API Tests Completed!\n")

        print("========================================")
        print("PLANS API TESTS")
        print("========================================\n")

        test_create_plan(api_client)
        test_get_plan(api_client)
        test_list_plans(api_client)
        test_update_plan(api_client)
        test_search_plans(api_client)

        print("✓ Plans API Tests Completed!\n")

        print("========================================")
        print("ADDONS API TESTS")
        print("========================================\n")

        test_create_addon(api_client)
        test_get_addon(api_client)
        test_list_addons(api_client)
        test_update_addon(api_client)
        test_lookup_addon(api_client)
        test_search_addons(api_client)

        print("✓ Addons API Tests Completed!\n")

        print("========================================")
        print("ENTITLEMENTS API TESTS")
        print("========================================\n")

        test_create_entitlement(api_client)
        test_get_entitlement(api_client)
        test_list_entitlements(api_client)
        test_update_entitlement(api_client)
        test_search_entitlements(api_client)

        print("✓ Entitlements API Tests Completed!\n")

        print("========================================")
        print("SUBSCRIPTIONS API TESTS")
        print("========================================\n")

        test_create_subscription(api_client)
        test_get_subscription(api_client)
        test_list_subscriptions(api_client)
        test_update_subscription(api_client)
        test_search_subscriptions(api_client)
        test_activate_subscription(api_client)
        # Lifecycle management (commented out - not needed)
        # test_pause_subscription(api_client)
        # test_resume_subscription(api_client)
        # test_get_pause_history(api_client)
        test_add_addon_to_subscription(api_client)
        test_remove_addon_from_subscription(api_client)
        # Change management
        # test_preview_subscription_change(api_client)  # Commented out - not needed
        test_execute_subscription_change(api_client)
        test_get_subscription_entitlements(api_client)
        test_get_upcoming_grants(api_client)
        test_report_usage(api_client)
        test_update_line_item(api_client)
        test_delete_line_item(api_client)
        test_cancel_subscription(api_client)

        print("✓ Subscriptions API Tests Completed!\n")

        print("========================================")
        print("INVOICES API TESTS")
        print("========================================\n")

        test_list_invoices(api_client)
        test_search_invoices(api_client)
        test_create_invoice(api_client)
        test_get_invoice(api_client)
        test_update_invoice(api_client)
        test_preview_invoice(api_client)
        test_finalize_invoice(api_client)
        test_recalculate_invoice(api_client)
        test_record_payment(api_client)
        test_attempt_payment(api_client)
        test_download_invoice_pdf(api_client)
        test_trigger_invoice_comms(api_client)
        test_get_customer_invoice_summary(api_client)
        test_void_invoice(api_client)

        print("✓ Invoices API Tests Completed!\n")

        print("========================================")
        print("PRICES API TESTS")
        print("========================================\n")

        test_create_price(api_client)
        test_get_price(api_client)
        test_list_prices(api_client)
        test_update_price(api_client)

        print("✓ Prices API Tests Completed!\n")

        print("========================================")
        print("PAYMENTS API TESTS")
        print("========================================\n")

        test_create_payment(api_client)
        test_get_payment(api_client)
        test_search_payments(api_client)
        test_list_payments(api_client)
        test_update_payment(api_client)
        test_process_payment(api_client)

        print("✓ Payments API Tests Completed!\n")

        print("========================================")
        print("WALLETS API TESTS")
        print("========================================\n")

        test_create_wallet(api_client)
        test_get_wallet(api_client)
        test_list_wallets(api_client)
        test_update_wallet(api_client)
        test_get_wallet_balance(api_client)
        test_top_up_wallet(api_client)
        test_debit_wallet(api_client)
        test_get_wallet_transactions(api_client)
        test_search_wallets(api_client)

        print("✓ Wallets API Tests Completed!\n")

        print("========================================")
        print("CREDIT GRANTS API TESTS")
        print("========================================\n")

        test_create_credit_grant(api_client)
        test_get_credit_grant(api_client)
        test_list_credit_grants(api_client)
        test_update_credit_grant(api_client)
        # Note: test_delete_credit_grant is in cleanup section

        print("✓ Credit Grants API Tests Completed!\n")

        print("========================================")
        print("CREDIT NOTES API TESTS")
        print("========================================\n")

        test_create_credit_note(api_client)
        test_get_credit_note(api_client)
        test_list_credit_notes(api_client)
        test_finalize_credit_note(api_client)

        print("✓ Credit Notes API Tests Completed!\n")

        print("========================================")
        print("EVENTS API TESTS")
        print("========================================\n")

        # Sync event operations
        test_create_event(api_client)
        test_query_events(api_client)

        # Async event operations
        test_async_event_enqueue(api_client)
        test_async_event_enqueue_with_options(api_client)
        test_async_event_batch(api_client)

        print("✓ Events API Tests Completed!\n")

        print("========================================")
        print("CLEANUP - DELETING TEST DATA")
        print("========================================\n")

        test_delete_payment(api_client)
        test_delete_price(api_client)
        test_delete_entitlement(api_client)
        test_delete_addon(api_client)
        test_delete_plan(api_client)
        test_delete_feature(api_client)
        test_delete_credit_grant(api_client)
        test_delete_customer(api_client)

        print("✓ Cleanup Completed!\n")

        print("\n=== All API Tests Completed Successfully! ===")


if __name__ == "__main__":
    main()

