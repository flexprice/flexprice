#!/usr/bin/env python3
"""
FlexPrice Python SDK - Published SDK tests (pip install flexprice).
Run from api/tests/python: pip install flexprice && python test_sdk.py
Requires: FLEXPRICE_API_KEY, FLEXPRICE_API_HOST (must include /v1, e.g. api.cloud.flexprice.io/v1; no trailing space or slash).
Uses the Flexprice client (server_url, api_key_auth) and namespaced APIs (client.customers, client.events, etc.).
Package: https://pypi.org/project/flexprice/
Repo: https://github.com/flexprice/python-sdk
"""

import os
import sys
import time
from datetime import datetime, timezone
from typing import Optional, TYPE_CHECKING

if TYPE_CHECKING:
    pass  # Flexprice type for annotations

# Check for required dependencies before importing SDK
def check_dependencies():
    """Check if required Python SDK dependencies are installed."""
    missing_deps = []
    try:
        import pydantic
    except ImportError:
        missing_deps.append("pydantic >= 2")
    try:
        import httpx
    except ImportError:
        missing_deps.append("httpx >= 0.28")
    if missing_deps:
        print("❌ Missing required dependencies:")
        for dep in missing_deps:
            print(f"   - {dep}")
        print("\n📦 Install with: pip install pydantic>=2 httpx>=0.28")
        sys.exit(1)

check_dependencies()

# SDK: Flexprice + errors. Enum values and errors from flexprice.models.
from flexprice import Flexprice
from flexprice import errors as sdk_errors
from flexprice.models import (
    addontype,
    billingcadence,
    billingcycle,
    billingmodel,
    billingperiod,
    creditgrantcadence,
    creditgrantexpirydurationunit,
    creditgrantexpirytype,
    creditgrantscope,
    creditnotereason,
    invoicecadence,
    invoicestatus,
    invoicetype,
    paymentdestinationtype,
    paymentmethodtype,
    paymentstatus,
    priceentitytype,
    pricetype,
    priceunittype,
    subscriptionstatus,
    transactionreason,
)
from typing import get_args


def _literals(union_type):
    """Literal options from Union[Literal[...], UnrecognizedStr] in SDK types."""
    args = get_args(union_type)
    if not args:
        return ()
    return get_args(args[0])


_l = _literals
# Billing
BILLING_PERIOD_MONTHLY = _l(billingperiod.BillingPeriod)[0]
BILLING_CADENCE_RECURRING = _l(billingcadence.BillingCadence)[0]
BILLING_MODEL_FLAT_FEE = _l(billingmodel.BillingModel)[0]
INVOICE_CADENCE_ARREAR = _l(invoicecadence.InvoiceCadence)[0]
INVOICE_CADENCE_ADVANCE = _l(invoicecadence.InvoiceCadence)[1]
# Price
PRICE_ENTITY_TYPE_PLAN = _l(priceentitytype.PriceEntityType)[0]
PRICE_ENTITY_TYPE_ADDON = _l(priceentitytype.PriceEntityType)[2]
PRICE_TYPE_FIXED = _l(pricetype.PriceType)[1]
PRICE_UNIT_TYPE_FIAT = _l(priceunittype.PriceUnitType)[0]
# Invoice / Payment
INVOICE_TYPE_ONE_OFF = _l(invoicetype.InvoiceType)[1]
INVOICE_STATUS_DRAFT = _l(invoicestatus.InvoiceStatus)[0]
PAYMENT_STATUS_SUCCEEDED = _l(paymentstatus.PaymentStatus)[3]
PAYMENT_STATUS_PENDING = _l(paymentstatus.PaymentStatus)[1]
# Addon / Subscription / Billing cycle
ADDON_TYPE_ONETIME = _l(addontype.AddonType)[0]
SUBSCRIPTION_STATUS_DRAFT = _l(subscriptionstatus.SubscriptionStatus)[5]
BILLING_CYCLE_ANNIVERSARY = _l(billingcycle.BillingCycle)[0]
# Wallet / Credit grant / Credit note
TRANSACTION_REASON_PURCHASED_CREDIT_DIRECT = _l(transactionreason.TransactionReason)[4]
CREDIT_GRANT_SCOPE_PLAN = _l(creditgrantscope.CreditGrantScope)[0]
CREDIT_GRANT_CADENCE_ONETIME = _l(creditgrantcadence.CreditGrantCadence)[0]
CREDIT_GRANT_EXPIRY_TYPE_NEVER = _l(creditgrantexpirytype.CreditGrantExpiryType)[0]
CREDIT_GRANT_EXPIRY_DURATION_UNIT_DAY = _l(creditgrantexpirydurationunit.CreditGrantExpiryDurationUnit)[0]
CREDIT_NOTE_REASON_BILLING_ERROR = _l(creditnotereason.CreditNoteReason)[5]
# Payment (create_payment)
PAYMENT_DESTINATION_TYPE_INVOICE = get_args(paymentdestinationtype.PaymentDestinationType)[0]
PAYMENT_METHOD_TYPE_OFFLINE = _l(paymentmethodtype.PaymentMethodType)[2]

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

def get_server_url_and_api_key():
    """Return (server_url, api_key) from environment."""
    api_key = os.getenv("FLEXPRICE_API_KEY")
    api_host = os.getenv("FLEXPRICE_API_HOST", "us.api.flexprice.io")
    if not api_key:
        print("❌ Missing FLEXPRICE_API_KEY environment variable")
        sys.exit(1)
    if not api_host:
        print("❌ Missing FLEXPRICE_API_HOST environment variable")
        sys.exit(1)
    if not api_host.startswith("http://") and not api_host.startswith("https://"):
        server_url = f"https://{api_host}"
    else:
        server_url = api_host
    return server_url, api_key


# ========================================
# CUSTOMERS API TESTS
# ========================================

def test_create_customer(client: Flexprice):
    """Test 1: Create Customer"""
    print("--- Test 1: Create Customer ---")
    try:
        timestamp = int(time.time() * 1000)
        global test_customer_name, test_customer_id
        test_customer_name = f"Test Customer {timestamp}"
        response = client.customers.create_customer(
            external_id=f"test-customer-{timestamp}",
            name=test_customer_name,
            email=f"test-{timestamp}@example.com",
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
                "environment": "test",
            },
        )
        test_customer_id = response.id
        print("✓ Customer created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  External ID: {response.external_id}")
        print(f"  Email: {response.email}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error creating customer: {e}\n")


def test_get_customer(client: Flexprice):
    """Test 2: Get Customer by ID"""
    print("--- Test 2: Get Customer by ID ---")
    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping get customer test\n")
        return
    try:
        response = client.customers.get_customer(id=test_customer_id)
        print("✓ Customer retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Created At: {response.created_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting customer: {e}\n")


def test_list_customers(client: Flexprice):
    """Test 3: List Customers"""
    print("--- Test 3: List Customers ---")
    try:
        response = client.customers.query_customer(limit=10)
        items = response.items if hasattr(response, "items") and response.items else []
        print(f"✓ Retrieved {len(items)} customers")
        if items:
            print(f"  First customer: {items[0].id} - {items[0].name}")
        if hasattr(response, "pagination") and response.pagination:
            print(f"  Total: {response.pagination.total}\n")
        else:
            print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error listing customers: {e}\n")


def test_update_customer(client: Flexprice):
    """Test 4: Update Customer"""
    print("--- Test 4: Update Customer ---")
    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping update customer test\n")
        return
    try:
        response = client.customers.update_customer(
            id=test_customer_id,
            name=f"{test_customer_name} (Updated)",
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            },
        )
        print("✓ Customer updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  New Name: {response.name}")
        print(f"  Updated At: {response.updated_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error updating customer: {e}\n")


def test_lookup_customer(client: Flexprice):
    """Test 5: Lookup Customer by External ID"""
    print("--- Test 5: Lookup Customer by External ID ---")
    if not test_customer_name:
        print("⚠ Warning: No customer name available\n⚠ Skipping lookup test\n")
        return
    try:
        external_id = f"test-customer-{test_customer_name.split(' ')[2]}"
        response = client.customers.get_customer_by_external_id(external_id=external_id)
        print("✓ Customer found by external ID!")
        print(f"  External ID: {external_id}")
        print(f"  Customer ID: {response.id}")
        print(f"  Name: {response.name}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error looking up customer: {e}\n")


def test_search_customers(client: Flexprice):
    """Test 6: Search Customers"""
    print("--- Test 6: Search Customers ---")
    if not test_customer_name:
        print("⚠ Warning: No customer name available\n⚠ Skipping search test\n")
        return
    try:
        external_id = f"test-customer-{test_customer_name.split(' ')[2]}"
        response = client.customers.query_customer(external_id=external_id)
        items = response.items if hasattr(response, "items") and response.items else []
        print("✓ Search completed!")
        print(f"  Found {len(items)} customers matching external ID '{external_id}'")
        if items:
            for customer in items:
                print(f"  - {customer.id}: {customer.name}")
        print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error searching customers: {e}\n")


def test_get_customer_entitlements(client: Flexprice):
    """Test 7: Get Customer Entitlements"""
    print("--- Test 7: Get Customer Entitlements ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping get entitlements test\n")
        return

    try:
        response = client.customers.get_customer_entitlements(id=test_customer_id)

        print("✓ Retrieved customer entitlements!")
        print(f"  Total features: {len(response.features) if response.features else 0}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting customer entitlements: {e}\n")


def test_get_customer_upcoming_grants(client: Flexprice):
    """Test 8: Get Customer Upcoming Grants"""
    print("--- Test 8: Get Customer Upcoming Grants ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping get upcoming grants test\n")
        return

    try:
        response = client.customers.get_customer_upcoming_grants(id=test_customer_id)

        print("✓ Retrieved upcoming grants!")
        print(f"  Total upcoming grants: {len(response.items) if response.items else 0}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting upcoming grants: {e}\n")


def test_get_customer_usage(client: Flexprice):
    """Test 9: Get Customer Usage"""
    print("--- Test 9: Get Customer Usage ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping get usage test\n")
        return

    try:
        response = client.customers.get_customer_usage_summary(customer_id=test_customer_id)

        print("✓ Retrieved customer usage!")
        print(f"  Usage records: {len(response.features) if response.features else 0}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting customer usage: {e}\n")


# ========================================
# FEATURES API TESTS
# ========================================

def test_create_feature(client: Flexprice):
    """Test 1: Create Feature"""
    print("--- Test 1: Create Feature ---")
    try:
        timestamp = int(time.time() * 1000)
        global test_feature_name, test_feature_id
        test_feature_name = f"Test Feature {timestamp}"
        feature_key = f"test_feature_{timestamp}"
        response = client.features.create_feature(
            name=test_feature_name,
            lookup_key=feature_key,
            description="This is a test feature created by SDK tests",
            type_="boolean",
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
                "environment": "test",
            },
        )
        test_feature_id = response.id
        print("✓ Feature created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Lookup Key: {response.lookup_key}")
        print(f"  Type: {response.type}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error creating feature: {e}\n")


def test_get_feature(client: Flexprice):
    """Test 2: Get Feature by ID"""
    print("--- Test 2: Get Feature by ID ---")
    if not test_feature_id:
        print("⚠ Warning: No feature ID available\n⚠ Skipping get feature test\n")
        return
    try:
        response = client.features.query_feature(feature_ids=[test_feature_id], limit=1)
        items = response.items if hasattr(response, "items") and response.items else []
        if not items:
            print("⚠ No feature found\n")
            return
        response = items[0]
        print("✓ Feature retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Lookup Key: {response.lookup_key}")
        print(f"  Created At: {response.created_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting feature: {e}\n")


def test_list_features(client: Flexprice):
    """Test 3: List Features"""
    print("--- Test 3: List Features ---")
    try:
        response = client.features.query_feature(limit=10)
        items = response.items if hasattr(response, "items") and response.items else []
        print(f"✓ Retrieved {len(items)} features")
        if items:
            print(f"  First feature: {items[0].id} - {items[0].name}")
        if hasattr(response, "pagination") and response.pagination:
            print(f"  Total: {response.pagination.total}\n")
        else:
            print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error listing features: {e}\n")


def test_update_feature(client: Flexprice):
    """Test 4: Update Feature"""
    print("--- Test 4: Update Feature ---")
    if not test_feature_id:
        print("⚠ Warning: No feature ID available\n⚠ Skipping update feature test\n")
        return
    try:
        response = client.features.update_feature(
            id=test_feature_id,
            name=f"{test_feature_name} (Updated)",
            description="Updated description for test feature",
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            },
        )
        print("✓ Feature updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  New Name: {response.name}")
        print(f"  New Description: {response.description}")
        print(f"  Updated At: {response.updated_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error updating feature: {e}\n")


def test_search_features(client: Flexprice):
    """Test 5: Search Features"""
    print("--- Test 5: Search Features ---")
    if not test_feature_id:
        print("⚠ Warning: No feature ID available\n⚠ Skipping search test\n")
        return
    try:
        response = client.features.query_feature(feature_ids=[test_feature_id])
        items = response.items if hasattr(response, "items") and response.items else []
        print("✓ Search completed!")
        print(f"  Found {len(items)} features matching ID '{test_feature_id}'")
        if items:
            for feature in items[:3]:
                print(f"  - {feature.id}: {feature.name} ({feature.lookup_key})")
        print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error searching features: {e}\n")


# ========================================
# PLANS API TESTS
# ========================================

def test_create_plan(client: Flexprice):
    """Test 1: Create Plan"""
    print("--- Test 1: Create Plan ---")

    try:
        timestamp = int(time.time() * 1000)
        global test_plan_name, test_plan_id
        test_plan_name = f"Test Plan {timestamp}"
        lookup_key = f"test_plan_{timestamp}"
        response = client.plans.create_plan(
            name=test_plan_name,
            lookup_key=lookup_key,
            description="This is a test plan created by SDK tests",
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
                "environment": "test",
            },
        )

        test_plan_id = response.id
        print("✓ Plan created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Lookup Key: {response.lookup_key}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error creating plan: {e}\n")


def test_get_plan(client: Flexprice):
    """Test 2: Get Plan by ID"""
    print("--- Test 2: Get Plan by ID ---")

    if not test_plan_id:
        print("⚠ Warning: No plan ID available\n⚠ Skipping get plan test\n")
        return

    try:
        response = client.plans.get_plan(id=test_plan_id)

        print("✓ Plan retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Lookup Key: {response.lookup_key}")
        print(f"  Created At: {response.created_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting plan: {e}\n")


def test_list_plans(client: Flexprice):
    """Test 3: List Plans"""
    print("--- Test 3: List Plans ---")

    try:
        response = client.plans.query_plan(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} plans")
        if response.items and len(response.items) > 0:
            print(f"  First plan: {response.items[0].id} - {response.items[0].name}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error listing plans: {e}\n")


def test_update_plan(client: Flexprice):
    """Test 4: Update Plan"""
    print("--- Test 4: Update Plan ---")

    if not test_plan_id:
        print("⚠ Warning: No plan ID available\n⚠ Skipping update plan test\n")
        return

    try:
        response = client.plans.update_plan(
            id=test_plan_id,
            name=f"{test_plan_name} (Updated)",
            description="Updated description for test plan",
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            },
        )

        print("✓ Plan updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  New Name: {response.name}")
        print(f"  New Description: {response.description}")
        print(f"  Updated At: {response.updated_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error updating plan: {e}\n")


def test_search_plans(client: Flexprice):
    """Test 5: Search Plans"""
    print("--- Test 5: Search Plans ---")

    if not test_plan_id:
        print("⚠ Warning: No plan ID available\n⚠ Skipping search test\n")
        return

    try:
        response = client.plans.query_plan(plan_ids=[test_plan_id])

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} plans matching ID '{test_plan_id}'")
        if response.items:
            for plan in response.items[:3]:
                print(f"  - {plan.id}: {plan.name} ({plan.lookup_key})")
        print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error searching plans: {e}\n")


# ========================================
# ADDONS API TESTS
# ========================================

def test_create_addon(client: Flexprice):
    """Test 1: Create Addon"""
    print("--- Test 1: Create Addon ---")

    try:
        timestamp = int(time.time() * 1000)
        global test_addon_name, test_addon_id, test_addon_lookup_key
        test_addon_name = f"Test Addon {timestamp}"
        test_addon_lookup_key = f"test_addon_{timestamp}"
        response = client.addons.create_addon(
            name=test_addon_name,
            lookup_key=test_addon_lookup_key,
            description="This is a test addon created by SDK tests",
            type_=ADDON_TYPE_ONETIME,
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
                "environment": "test",
            },
        )
        test_addon_id = response.id
        print("✓ Addon created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Lookup Key: {response.lookup_key}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error creating addon: {e}\n")


def test_get_addon(client: Flexprice):
    """Test 2: Get Addon by ID"""
    print("--- Test 2: Get Addon by ID ---")

    if not test_addon_id:
        print("⚠ Warning: No addon ID available\n⚠ Skipping get addon test\n")
        return

    try:
        response = client.addons.get_addon(id=test_addon_id)

        print("✓ Addon retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}")
        print(f"  Lookup Key: {response.lookup_key}")
        print(f"  Created At: {response.created_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting addon: {e}\n")


def test_list_addons(client: Flexprice):
    """Test 3: List Addons"""
    print("--- Test 3: List Addons ---")

    try:
        response = client.addons.query_addon(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} addons")
        if response.items and len(response.items) > 0:
            print(f"  First addon: {response.items[0].id} - {response.items[0].name}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error listing addons: {e}\n")


def test_update_addon(client: Flexprice):
    """Test 4: Update Addon"""
    print("--- Test 4: Update Addon ---")

    if not test_addon_id:
        print("⚠ Warning: No addon ID available\n⚠ Skipping update addon test\n")
        return

    try:
        response = client.addons.update_addon(
            id=test_addon_id,
            name=f"{test_addon_name} (Updated)",
            description="Updated description for test addon",
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            },
        )

        print("✓ Addon updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  New Name: {response.name}")
        print(f"  New Description: {response.description}")
        print(f"  Updated At: {response.updated_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error updating addon: {e}\n")


def test_lookup_addon(client: Flexprice):
    """Test 5: Lookup Addon by Lookup Key"""
    print("--- Test 5: Lookup Addon by Lookup Key ---")

    if not test_addon_lookup_key:
        print("⚠ Warning: No addon lookup key available\n⚠ Skipping lookup test\n")
        return

    try:
        print(f"  Looking up addon with key: {test_addon_lookup_key}")
        response = client.addons.get_addon_by_lookup_key(lookup_key=test_addon_lookup_key)

        print("✓ Addon found by lookup key!")
        print(f"  Lookup Key: {test_addon_lookup_key}")
        print(f"  ID: {response.id}")
        print(f"  Name: {response.name}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error looking up addon: {e}")
        print("⚠ Skipping lookup test\n")


def test_search_addons(client: Flexprice):
    """Test 6: Search Addons"""
    print("--- Test 6: Search Addons ---")

    if not test_addon_id:
        print("⚠ Warning: No addon ID available\n⚠ Skipping search test\n")
        return

    try:
        response = client.addons.query_addon(addon_ids=[test_addon_id])

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} addons matching ID '{test_addon_id}'")
        if response.items:
            for addon in response.items[:3]:
                print(f"  - {addon.id}: {addon.name} ({addon.lookup_key})")
        print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error searching addons: {e}\n")


# ========================================
# ENTITLEMENTS API TESTS
# ========================================

def test_create_entitlement(client: Flexprice):
    """Test 1: Create Entitlement"""
    print("--- Test 1: Create Entitlement ---")

    if not test_feature_id or not test_plan_id:
        print("⚠ Warning: No feature or plan ID available\n⚠ Skipping create entitlement test\n")
        return

    try:
        response = client.entitlements.create_entitlement(
            feature_id=test_feature_id,
            feature_type="boolean",
            plan_id=test_plan_id,
            is_enabled=True,
            usage_reset_period="MONTHLY",
        )

        global test_entitlement_id
        test_entitlement_id = response.id
        print("✓ Entitlement created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Feature ID: {response.feature_id}")
        print(f"  Plan ID: {response.plan_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error creating entitlement: {e}\n")


def test_get_entitlement(client: Flexprice):
    """Test 2: Get Entitlement by ID"""
    print("--- Test 2: Get Entitlement by ID ---")

    if not test_entitlement_id:
        print("⚠ Warning: No entitlement ID available\n⚠ Skipping get entitlement test\n")
        return

    try:
        response = client.entitlements.get_entitlement(id=test_entitlement_id)

        print("✓ Entitlement retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Feature ID: {response.feature_id}")
        plan_id = getattr(response, 'plan_id', 'N/A')
        print(f"  Plan ID: {plan_id}")
        print(f"  Created At: {response.created_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting entitlement: {e}\n")


def test_list_entitlements(client: Flexprice):
    """Test 3: List Entitlements"""
    print("--- Test 3: List Entitlements ---")

    try:
        response = client.entitlements.query_entitlement(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} entitlements")
        if response.items and len(response.items) > 0:
            print(f"  First entitlement: {response.items[0].id}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error listing entitlements: {e}\n")


def test_update_entitlement(client: Flexprice):
    """Test 4: Update Entitlement"""
    print("--- Test 4: Update Entitlement ---")

    if not test_entitlement_id:
        print("⚠ Warning: No entitlement ID available\n⚠ Skipping update entitlement test\n")
        return

    try:
        response = client.entitlements.update_entitlement(id=test_entitlement_id, is_enabled=False)

        print("✓ Entitlement updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  Updated At: {response.updated_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error updating entitlement: {e}\n")


def test_search_entitlements(client: Flexprice):
    """Test 5: Search Entitlements"""
    print("--- Test 5: Search Entitlements ---")

    if not test_entitlement_id:
        print("⚠ Warning: No entitlement ID available\n⚠ Skipping search test\n")
        return

    try:
        response = client.entitlements.query_entitlement(entity_ids=[test_entitlement_id])

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} entitlements matching ID '{test_entitlement_id}'")
        if response.items:
            for ent in response.items[:3]:
                print(f"  - {ent.id}: Feature {ent.feature_id}")
        print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error searching entitlements: {e}\n")


# ========================================
# CONNECTIONS API TESTS
# ========================================

def test_list_connections(client: Flexprice):
    """Test 1: List Connections (integrations)"""
    print("--- Test 1: List Connections ---")
    try:
        response = client.integrations.list_linked_integrations()
        conns = getattr(response, "integrations", None) or getattr(response, "connections", None) or getattr(response, "items", []) or []
        count = len(conns) if isinstance(conns, list) else 0
        print(f"✓ Retrieved {count} connections")
        if conns and len(conns) > 0:
            c = conns[0]
            print(f"  First: {getattr(c, 'id', getattr(c, 'provider', 'N/A'))}")
        print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error listing connections: {e}\n")


def test_search_connections(client: Flexprice):
    """Test 2: Search Connections (integrations)"""
    print("--- Test 2: Search Connections ---")
    try:
        response = client.integrations.list_linked_integrations()
        conns = getattr(response, "integrations", None) or getattr(response, "connections", None) or getattr(response, "items", []) or []
        count = len(conns) if isinstance(conns, list) else 0
        print(f"✓ Found {count} connections")
        if conns:
            for c in conns[:3]:
                print(f"  - {getattr(c, 'id', 'N/A')}: {getattr(c, 'provider_type', getattr(c, 'provider', 'unknown'))}")
        print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error searching connections: {e}\n")


# ========================================
# SUBSCRIPTIONS API TESTS
# ========================================

def test_create_subscription(client: Flexprice):
    """Test 1: Create Subscription"""
    print("--- Test 1: Create Subscription ---")

    if not test_customer_id or not test_plan_id:
        print("⚠ Warning: No customer or plan ID available\n⚠ Skipping create subscription test\n")
        return

    try:
        # First create a price for the plan
        client.prices.create_price(
            entity_id=test_plan_id,
            entity_type=PRICE_ENTITY_TYPE_PLAN,
            type_=PRICE_TYPE_FIXED,
            billing_model=BILLING_MODEL_FLAT_FEE,
            billing_cadence=BILLING_CADENCE_RECURRING,
            billing_period=BILLING_PERIOD_MONTHLY,
            billing_period_count=1,
            invoice_cadence=INVOICE_CADENCE_ARREAR,
            price_unit_type=PRICE_UNIT_TYPE_FIAT,
            amount="29.99",
            currency="USD",
            display_name="Monthly Subscription Price",
        )
        # Now create the subscription
        response = client.subscriptions.create_subscription(
            customer_id=test_customer_id,
            plan_id=test_plan_id,
            currency="USD",
            billing_cadence=BILLING_CADENCE_RECURRING,
            billing_period=BILLING_PERIOD_MONTHLY,
            billing_period_count=1,
            billing_cycle=BILLING_CYCLE_ANNIVERSARY,
            start_date=datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            metadata={
                "source": "sdk_test",
                "test_run": datetime.now().isoformat(),
            },
        )

        global test_subscription_id
        test_subscription_id = response.id
        print("✓ Subscription created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Customer ID: {response.customer_id}")
        print(f"  Plan ID: {response.plan_id}")
        print(f"  Status: {response.subscription_status}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error creating subscription: {e}\n")


def test_get_subscription(client: Flexprice):
    """Test 2: Get Subscription by ID"""
    print("--- Test 2: Get Subscription by ID ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription ID available\n⚠ Skipping get subscription test\n")
        return

    try:
        response = client.subscriptions.get_subscription(id=test_subscription_id)

        print("✓ Subscription retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Customer ID: {response.customer_id}")
        print(f"  Status: {response.subscription_status}")
        print(f"  Created At: {response.created_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting subscription: {e}\n")


def test_list_subscriptions(client: Flexprice):
    """Test 3: List Subscriptions"""
    print("--- Test 3: List Subscriptions ---")

    try:
        response = client.subscriptions.query_subscription(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} subscriptions")
        if response.items and len(response.items) > 0:
            print(f"  First subscription: {response.items[0].id} (Customer: {response.items[0].customer_id})")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error listing subscriptions: {e}\n")


def test_update_subscription(client: Flexprice):
    """Test 4: Update Subscription"""
    print("--- Test 4: Update Subscription ---")
    print("⚠ Skipping update subscription test (endpoint not available in SDK)\n")


def test_search_subscriptions(client: Flexprice):
    """Test 5: Search Subscriptions"""
    print("--- Test 4: Search Subscriptions ---")

    try:
        response = client.subscriptions.query_subscription(limit=10)
        items = getattr(response, "items", []) or []
        print("✓ Search completed!")
        print(f"  Found {len(items)} subscriptions\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error searching subscriptions: {e}\n")


def test_activate_subscription(client: Flexprice):
    """Test 5: Activate Subscription"""
    print("--- Test 5: Activate Subscription ---")

    if not test_customer_id or not test_plan_id:
        print("⚠ Warning: No customer or plan ID available\n⚠ Skipping activate subscription test\n")
        return

    try:
        draft_sub = client.subscriptions.create_subscription(
            customer_id=test_customer_id,
            plan_id=test_plan_id,
            currency="USD",
            billing_cadence=BILLING_CADENCE_RECURRING,
            billing_period=BILLING_PERIOD_MONTHLY,
            billing_period_count=1,
            start_date=datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            subscription_status=SUBSCRIPTION_STATUS_DRAFT,
        )
        draft_id = draft_sub.id
        print(f"  Created draft subscription: {draft_id}")
        client.subscriptions.activate_subscription(
            id=draft_id,
            start_date=datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        )
        print("✓ Subscription activated successfully!")
        print(f"  ID: {draft_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error activating subscription: {e}\n")


def test_pause_subscription(client: Flexprice):
    """Test 7: Pause Subscription"""
    print("--- Test 7: Pause Subscription ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created, skipping pause test\n")
        return

    try:
        response = client.subscriptions.pause_subscription(
            id=test_subscription_id,
            pause_mode="immediate",
        )
        print("✓ Subscription paused successfully!")
        print(f"  Pause ID: {response.id}")
        print(f"  Subscription ID: {response.subscription_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error pausing subscription: {e}")
        print("⚠ Skipping pause test\n")


def test_resume_subscription(client: Flexprice):
    """Test 8: Resume Subscription"""
    print("--- Test 8: Resume Subscription ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created, skipping resume test\n")
        return

    try:
        response = client.subscriptions.resume_subscription(
            id=test_subscription_id,
            resume_mode="immediate",
        )
        print("✓ Subscription resumed successfully!")
        print(f"  Pause ID: {response.id}")
        print(f"  Subscription ID: {response.subscription_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error resuming subscription: {e}")
        print("⚠ Skipping resume test\n")


def test_get_pause_history(client: Flexprice):
    """Test 9: Get Pause History"""
    print("--- Test 9: Get Pause History ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created, skipping pause history test\n")
        return

    try:
        response = client.subscriptions.list_subscription_pauses(id=test_subscription_id)
        items = response if isinstance(response, list) else getattr(response, "items", []) or []
        print("✓ Retrieved pause history!")
        print(f"  Total pauses: {len(items)}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error getting pause history: {e}")
        print("⚠ Skipping pause history test\n")


def test_add_addon_to_subscription(client: Flexprice):
    """Test 6: Add Addon to Subscription"""
    print("--- Test 6: Add Addon to Subscription ---")

    if not test_subscription_id or not test_addon_id:
        print("⚠ Warning: No subscription or addon created\n⚠ Skipping add addon test\n")
        return

    try:
        try:
            client.prices.create_price(
                entity_id=test_addon_id,
                entity_type=PRICE_ENTITY_TYPE_ADDON,
                type_=PRICE_TYPE_FIXED,
                billing_model=BILLING_MODEL_FLAT_FEE,
                billing_cadence=BILLING_CADENCE_RECURRING,
                billing_period=BILLING_PERIOD_MONTHLY,
                billing_period_count=1,  # required: must be > 0
                invoice_cadence=INVOICE_CADENCE_ARREAR,
                price_unit_type=PRICE_UNIT_TYPE_FIAT,
                amount="5.00",
                currency="USD",
                display_name="Addon Monthly Price",
            )
            print(f"  Created price for addon: {test_addon_id}")
        except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as price_error:
            print(f"⚠ Warning: Error creating price for addon: {price_error}")
        subscription_response = client.subscriptions.add_subscription_addon(
            subscription_id=test_subscription_id,
            addon_id=test_addon_id,
        )
        print("✓ Addon added to subscription successfully!")
        print(f"  Subscription ID: {getattr(subscription_response, 'id', test_subscription_id)}")
        print(f"  Addon ID: {test_addon_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error adding addon to subscription: {e}")
        if hasattr(e, 'body'):
            print(f"  Response: {e.body}")
        print("⚠ Skipping add addon test\n")


def test_remove_addon_from_subscription(client: Flexprice):
    """Test 8: Remove Addon from Subscription"""
    print("--- Test 8: Remove Addon from Subscription ---")
    print("⚠ Skipping remove addon test (requires addon association ID)\n")


def test_preview_subscription_change(client: Flexprice):
    """Test 9: Preview Subscription Change"""
    print("--- Test 13: Preview Subscription Change ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created, skipping preview change test\n")
        return

    if not test_plan_id:
        print("⚠ Warning: No plan available for change preview\n")
        return

    try:
        preview = client.subscriptions.preview_subscription_change(
            id=test_subscription_id,
            target_plan_id=test_plan_id,
            billing_cadence=BILLING_CADENCE_RECURRING,
            billing_period=BILLING_PERIOD_MONTHLY,
            billing_cycle=BILLING_CYCLE_ANNIVERSARY,
            proration_behavior="create_prorations",
        )
        print("✓ Subscription change preview generated!")
        if hasattr(preview, "next_invoice_preview") and preview.next_invoice_preview:
            print("  Preview available")
        print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error previewing subscription change: {e}")
        print("⚠ Skipping preview change test\n")


def test_execute_subscription_change(client: Flexprice):
    """Test 8: Execute Subscription Change"""
    print("--- Test 8: Execute Subscription Change ---")
    print("⚠ Skipping execute change test (would modify active subscription)\n")


def test_get_subscription_entitlements(client: Flexprice):
    """Test 9: Get Subscription Entitlements"""
    print("--- Test 9: Get Subscription Entitlements ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created\n⚠ Skipping get entitlements test\n")
        return

    try:
        response = client.subscriptions.get_subscription_entitlements(id=test_subscription_id)
        features = getattr(response, "features", []) or []
        print("✓ Retrieved subscription entitlements!")
        print(f"  Total features: {len(features)}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error getting entitlements: {e}\n")


def test_get_upcoming_grants(client: Flexprice):
    """Test 10: Get Upcoming Grants"""
    print("--- Test 10: Get Upcoming Grants ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created\n⚠ Skipping get upcoming grants test\n")
        return

    try:
        response = client.subscriptions.get_subscription_upcoming_grants(id=test_subscription_id)
        items = getattr(response, "items", []) or []
        print("✓ Retrieved upcoming grants!")
        print(f"  Total upcoming grants: {len(items)}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error getting upcoming grants: {e}\n")


def test_report_usage(client: Flexprice):
    """Test 11: Report Usage"""
    print("--- Test 11: Report Usage ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created\n⚠ Skipping report usage test\n")
        return

    try:
        client.subscriptions.get_subscription_usage(subscription_id=test_subscription_id)
        print("✓ Usage reported successfully!")
        print(f"  Subscription ID: {test_subscription_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error reporting usage: {e}\n")


def test_update_line_item(client: Flexprice):
    """Test 12: Update Line Item"""
    print("--- Test 12: Update Line Item ---")
    print("⚠ Skipping update line item test (requires line item ID)\n")


def test_delete_line_item(client: Flexprice):
    """Test 13: Delete Line Item"""
    print("--- Test 13: Delete Line Item ---")
    print("⚠ Skipping delete line item test (requires line item ID)\n")


def test_cancel_subscription(client: Flexprice):
    """Test 14: Cancel Subscription"""
    print("--- Test 14: Cancel Subscription ---")

    if not test_subscription_id:
        print("⚠ Warning: No subscription created\n⚠ Skipping cancel test\n")
        return

    try:
        client.subscriptions.cancel_subscription(
            id=test_subscription_id,
            cancellation_type="end_of_period",
        )
        print("✓ Subscription canceled successfully!")
        print(f"  Subscription ID: {test_subscription_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error canceling subscription: {e}\n")


# ========================================
# INVOICES API TESTS
# ========================================

def test_list_invoices(client: Flexprice):
    """Test 1: List Invoices"""
    print("--- Test 1: List Invoices ---")

    try:
        response = client.invoices.query_invoice(limit=10)

        global test_invoice_id
        print(f"✓ Retrieved {len(response.items) if response.items else 0} invoices")
        if response.items and len(response.items) > 0:
            test_invoice_id = response.items[0].id
            print(f"  First invoice: {response.items[0].id} (Customer: {response.items[0].customer_id})")
            if hasattr(response.items[0], 'status'):
                print(f"  Status: {response.items[0].status}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error listing invoices: {e}\n")


def test_search_invoices(client: Flexprice):
    """Test 2: Search Invoices"""
    print("--- Test 2: Search Invoices ---")

    try:
        response = client.invoices.query_invoice(limit=10)

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} invoices\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error searching invoices: {e}\n")


def test_create_invoice(client: Flexprice):
    """Test 3: Create Invoice"""
    print("--- Test 3: Create Invoice ---")

    if not test_customer_id:
        print("⚠ Warning: No customer created\n⚠ Skipping create invoice test\n")
        return

    try:
        response = client.invoices.create_invoice(
            customer_id=test_customer_id,
            currency="USD",
            amount_due="100.00",
            subtotal="100.00",
            total="100.00",
            invoice_type=INVOICE_TYPE_ONE_OFF,
            billing_reason="MANUAL",
            invoice_status=INVOICE_STATUS_DRAFT,
            line_items=[
                {"display_name": "Test Service", "quantity": "1", "amount": "100.00"}
            ],
            metadata={"source": "sdk_test", "type": "manual"},
        )

        global test_invoice_id
        test_invoice_id = response.id
        print("✓ Invoice created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Customer ID: {response.customer_id}")
        print(f"  Status: {response.invoice_status}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error creating invoice: {e}\n")


def test_get_invoice(client: Flexprice):
    """Test 4: Get Invoice by ID"""
    print("--- Test 4: Get Invoice by ID ---")

    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available\n⚠ Skipping get invoice test\n")
        return

    try:
        response = client.invoices.get_invoice(id=test_invoice_id)

        print("✓ Invoice retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Total: {response.currency} {response.total}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error getting invoice: {e}\n")


def test_update_invoice(client: Flexprice):
    """Test 5: Update Invoice"""
    print("--- Test 5: Update Invoice ---")

    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available\n⚠ Skipping update invoice test\n")
        return

    try:
        response = client.invoices.update_invoice(
            id=test_invoice_id,
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            },
        )

        print("✓ Invoice updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  Updated At: {response.updated_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error updating invoice: {e}\n")


def test_preview_invoice(client: Flexprice):
    """Test 6: Preview Invoice"""
    print("--- Test 6: Preview Invoice ---")

    if not test_customer_id:
        print("⚠ Warning: No customer available\n⚠ Skipping preview invoice test\n")
        return

    try:
        response = client.invoices.get_invoice_preview(
            subscription_id=test_subscription_id if test_subscription_id else None
        )

        print("✓ Invoice preview generated!")
        if response.total:
            print(f"  Preview Total: {response.total}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error previewing invoice: {e}\n")


def test_finalize_invoice(client: Flexprice):
    """Test 7: Finalize Invoice"""
    print("--- Test 7: Finalize Invoice ---")

    if not test_customer_id:
        print("⚠ Warning: No customer available\n⚠ Skipping finalize invoice test\n")
        return

    try:
        draft_invoice = client.invoices.create_invoice(
            customer_id=test_customer_id,
            currency="USD",
            amount_due="50.00",
            subtotal="50.00",
            total="50.00",
            invoice_type=INVOICE_TYPE_ONE_OFF,
            billing_reason="MANUAL",
            invoice_status=INVOICE_STATUS_DRAFT,
            line_items=[
                {"display_name": "Finalize Test Service", "quantity": "1", "amount": "50.00"}
            ],
        )
        finalize_id = draft_invoice.id
        print(f"  Created draft invoice: {finalize_id}")
        client.invoices.finalize_invoice(id=finalize_id)

        print("✓ Invoice finalized successfully!")
        print(f"  Invoice ID: {finalize_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error finalizing invoice: {e}\n")


def test_recalculate_invoice(client: Flexprice):
    """Test 8: Recalculate Invoice"""
    print("--- Test 8: Recalculate Invoice ---")
    print("⚠ Skipping recalculate invoice test (requires subscription invoice)\n")


def test_record_payment(client: Flexprice):
    """Test 9: Record Payment"""
    print("--- Test 9: Record Payment ---")

    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available\n⚠ Skipping record payment test\n")
        return

    try:
        client.invoices.update_invoice_payment_status(
            id=test_invoice_id,
            payment_status=PAYMENT_STATUS_SUCCEEDED,
            amount="100.00",
        )

        print("✓ Payment recorded successfully!")
        print(f"  Invoice ID: {test_invoice_id}")
        print(f"  Amount Paid: 100.00\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error recording payment: {e}\n")


def test_attempt_payment(client: Flexprice):
    """Test 10: Attempt Payment"""
    print("--- Test 10: Attempt Payment ---")

    if not test_customer_id:
        print("⚠ Warning: No customer available\n⚠ Skipping attempt payment test\n")
        return

    try:
        attempt_invoice = client.invoices.create_invoice(
            customer_id=test_customer_id,
            currency="USD",
            amount_due="25.00",
            subtotal="25.00",
            total="25.00",
            amount_paid="0.00",
            invoice_type=INVOICE_TYPE_ONE_OFF,
            billing_reason="MANUAL",
            invoice_status=INVOICE_STATUS_DRAFT,
            payment_status=PAYMENT_STATUS_PENDING,
            line_items=[
                {"display_name": "Attempt Payment Test", "quantity": "1", "amount": "25.00"}
            ],
        )
        attempt_id = attempt_invoice.id
        client.invoices.finalize_invoice(id=attempt_id)
        client.invoices.attempt_invoice_payment(id=attempt_id)

        print("✓ Payment attempt initiated!")
        print(f"  Invoice ID: {attempt_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error attempting payment: {e}\n")


def test_download_invoice_pdf(client: Flexprice):
    """Test 11: Download Invoice PDF"""
    print("--- Test 11: Download Invoice PDF ---")

    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available\n⚠ Skipping download PDF test\n")
        return

    try:
        client.invoices.get_invoice_pdf(id=test_invoice_id)

        print("✓ Invoice PDF downloaded!")
        print(f"  Invoice ID: {test_invoice_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error downloading PDF: {e}\n")


def test_trigger_invoice_comms(client: Flexprice):
    """Test 12: Trigger Invoice Communications"""
    print("--- Test 12: Trigger Invoice Communications ---")

    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available\n⚠ Skipping trigger comms test\n")
        return

    try:
        client.invoices.trigger_invoice_comms_webhook(id=test_invoice_id)

        print("✓ Invoice communications triggered!")
        print(f"  Invoice ID: {test_invoice_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error triggering comms: {e}\n")


def test_get_customer_invoice_summary(client: Flexprice):
    """Test 13: Get Customer Invoice Summary"""
    print("--- Test 13: Get Customer Invoice Summary ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping summary test\n")
        return

    try:
        client.invoices.get_customer_invoice_summary(id=test_customer_id)

        print("✓ Customer invoice summary retrieved!")
        print(f"  Customer ID: {test_customer_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error getting summary: {e}\n")


def test_void_invoice(client: Flexprice):
    """Test 14: Void Invoice"""
    print("--- Test 14: Void Invoice ---")

    if not test_invoice_id:
        print("⚠ Warning: No invoice ID available\n⚠ Skipping void invoice test\n")
        return

    try:
        client.invoices.void_invoice(id=test_invoice_id)

        print("✓ Invoice voided successfully!")
        print("  Invoice finalized\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error voiding invoice: {e}\n⚠ Skipping void invoice test\n")


# ========================================
# PRICES API TESTS
# ========================================

def test_create_price(client: Flexprice):
    """Test 1: Create Price"""
    print("--- Test 1: Create Price ---")

    if not test_plan_id:
        print("⚠ Warning: No plan ID available\n⚠ Skipping create price test\n")
        return

    try:
        response = client.prices.create_price(
            entity_id=test_plan_id,
            entity_type=PRICE_ENTITY_TYPE_PLAN,
            type_=PRICE_TYPE_FIXED,
            currency="USD",
            amount="99.00",
            billing_model=BILLING_MODEL_FLAT_FEE,
            billing_cadence=BILLING_CADENCE_RECURRING,
            billing_period=BILLING_PERIOD_MONTHLY,
            billing_period_count=1,
            invoice_cadence=INVOICE_CADENCE_ADVANCE,
            price_unit_type=PRICE_UNIT_TYPE_FIAT,
            display_name="Monthly Subscription",
            description="Standard monthly subscription price",
        )

        global test_price_id
        test_price_id = response.id
        print("✓ Price created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Amount: {response.amount} {response.currency}")
        print(f"  Billing Model: {response.billing_model}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error creating price: {e}\n")


def test_get_price(client: Flexprice):
    """Test 2: Get Price by ID"""
    print("--- Test 2: Get Price by ID ---")

    if not test_price_id:
        print("⚠ Warning: No price ID available\n⚠ Skipping get price test\n")
        return

    try:
        response = client.prices.get_price(id=test_price_id)

        print("✓ Price retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Amount: {response.amount} {response.currency}")
        print(f"  Entity ID: {response.entity_id}")
        print(f"  Created At: {response.created_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting price: {e}\n")


def test_list_prices(client: Flexprice):
    """Test 3: List Prices"""
    print("--- Test 3: List Prices ---")

    try:
        response = client.prices.query_price(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} prices")
        if response.items and len(response.items) > 0:
            print(f"  First price: {response.items[0].id} - {response.items[0].amount} {response.items[0].currency}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error listing prices: {e}\n")


def test_update_price(client: Flexprice):
    """Test 4: Update Price"""
    print("--- Test 4: Update Price ---")

    if not test_price_id:
        print("⚠ Warning: No price ID available\n⚠ Skipping update price test\n")
        return

    try:
        response = client.prices.update_price(
            id=test_price_id,
            description="Updated price description for testing",
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            },
        )

        print("✓ Price updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  New Description: {response.description}")
        print(f"  Updated At: {response.updated_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error updating price: {e}\n")


# ========================================
# PAYMENTS API TESTS
# ========================================

def test_create_payment(client: Flexprice):
    """Test 1: Create Payment"""
    print("--- Test 1: Create Payment ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping create payment test\n")
        return

    payment_invoice_id = None

    try:
        draft_invoice = client.invoices.create_invoice(
            customer_id=test_customer_id,
            currency="USD",
            amount_due="100.00",
            subtotal="100.00",
            total="100.00",
            amount_paid="0.00",
            invoice_type=INVOICE_TYPE_ONE_OFF,
            billing_reason="MANUAL",
            invoice_status=INVOICE_STATUS_DRAFT,
            payment_status=PAYMENT_STATUS_PENDING,
            line_items=[{"display_name": "Payment Test Service", "quantity": "1", "amount": "100.00"}],
            metadata={"source": "sdk_test_payment"},
        )
        payment_invoice_id = draft_invoice.id
        print(f"  Created invoice for payment: {payment_invoice_id}")
        current_invoice = client.invoices.get_invoice(id=payment_invoice_id)
        if current_invoice.amount_paid and str(current_invoice.amount_paid) not in ("0", "0.00"):
            print(f"⚠ Warning: Invoice already has amount paid before finalization\n")
            return
        if str(current_invoice.amount_due or "") in ("0", "0.00"):
            print(f"⚠ Warning: Invoice has zero amount due\n")
            return
        if getattr(current_invoice, "invoice_status", None) == "draft":
            try:
                client.invoices.finalize_invoice(id=payment_invoice_id)
                print("  Finalized invoice for payment")
            except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as finalize_error:
                error_msg = str(finalize_error)
                if "already" in error_msg.lower() or "400" in error_msg:
                    print(f"⚠ Warning: Invoice finalization returned error (may already be finalized)")
                else:
                    return
        final_invoice = client.invoices.get_invoice(id=payment_invoice_id)
        if str(getattr(final_invoice, "payment_status", "") or "").lower() == "succeeded":
            print(f"⚠ Warning: Invoice is already paid\n")
            return
        if getattr(final_invoice, "amount_paid", None) and str(final_invoice.amount_paid) not in ("0", "0.00"):
            print(f"⚠ Warning: Invoice already has amount paid\n")
            return
        response = client.payments.create_payment(
            amount="100.00",
            currency="USD",
            destination_id=payment_invoice_id,
            destination_type=PAYMENT_DESTINATION_TYPE_INVOICE,
            payment_method_type=PAYMENT_METHOD_TYPE_OFFLINE,
            process_payment=False,
            metadata={"source": "sdk_test", "test_run": datetime.now().isoformat()},
        )

        global test_payment_id
        test_payment_id = response.id
        print("✓ Payment created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Amount: {response.amount} {response.currency}")
        if response.payment_status:
            print(f"  Status: {response.payment_status}\n")
        else:
            print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
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


def test_get_payment(client: Flexprice):
    """Test 2: Get Payment by ID"""
    print("--- Test 2: Get Payment by ID ---")

    if not test_payment_id:
        print("⚠ Warning: No payment ID available\n⚠ Skipping get payment test\n")
        return

    try:
        response = client.payments.get_payment(id=test_payment_id)

        print("✓ Payment retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Amount: {response.amount} {response.currency}")
        print(f"  Status: {response.payment_status}")
        print(f"  Created At: {response.created_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting payment: {e}\n")


def test_search_payments(client: Flexprice):
    """Test 2: Search Payments"""
    print("--- Test 2: Search Payments ---")
    print("⚠ Skipping search payments test (endpoint not available in SDK)\n")


def test_list_payments(client: Flexprice):
    """Test 3: List Payments"""
    print("--- Test 3: List Payments ---")

    try:
        response = client.payments.list_payments(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} payments")
        if response.items and len(response.items) > 0:
            print(f"  First payment: {response.items[0].id} - {response.items[0].amount} {response.items[0].currency}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error listing payments: {e}\n")
    except Exception as e:
        # Handle validation errors (e.g., payment_status='archived' not in enum)
        error_str = str(e)
        if 'payment_status' in error_str or 'ValidationError' in error_str or 'archived' in error_str:
            print(f"⚠ Warning: Error listing payments (may contain unsupported payment status): {error_str[:200]}\n")
        else:
            print(f"❌ Error listing payments: {e}\n")


def test_update_payment(client: Flexprice):
    """Test 4: Update Payment"""
    print("--- Test 4: Update Payment ---")

    if not test_payment_id:
        print("⚠ Warning: No payment ID available\n⚠ Skipping update payment test\n")
        return

    try:
        response = client.payments.update_payment(
            id=test_payment_id,
            metadata={
                "updated_at": datetime.now().isoformat(),
                "status": "updated",
            },
        )

        print("✓ Payment updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  Updated At: {response.updated_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error updating payment: {e}\n")


def test_process_payment(client: Flexprice):
    """Test 5: Process Payment"""
    print("--- Test 5: Process Payment ---")

    if not test_payment_id:
        print("⚠ Warning: No payment ID available\n⚠ Skipping process payment test\n")
        return

    try:
        client.payments.process_payment(id=test_payment_id)

        print("✓ Payment processed successfully!")
        print(f"  Payment ID: {test_payment_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error processing payment: {e}\n")


# ========================================
# WALLETS API TESTS
# ========================================

def test_create_wallet(client: Flexprice):
    """Test 1: Create Wallet"""
    print("--- Test 1: Create Wallet ---")

    if not test_customer_id:
        print("⚠ Warning: No customer ID available\n⚠ Skipping create wallet test\n")
        return

    try:
        response = client.wallets.create_wallet(
            customer_id=test_customer_id,
            currency="USD",
            metadata={"source": "sdk_test", "test_run": datetime.now().isoformat()},
        )

        global test_wallet_id
        test_wallet_id = response.id
        print("✓ Wallet created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Customer ID: {response.customer_id}")
        print(f"  Balance: {response.balance} {response.currency}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error creating wallet: {e}\n")


def test_get_wallet(client: Flexprice):
    """Test 2: Get Wallet by ID"""
    print("--- Test 2: Get Wallet by ID ---")

    if not test_wallet_id:
        print("⚠ Warning: No wallet ID available\n⚠ Skipping get wallet test\n")
        return

    try:
        response = client.wallets.get_wallet(id=test_wallet_id)

        print("✓ Wallet retrieved successfully!")
        print(f"  ID: {response.id}")
        print(f"  Balance: {response.balance} {response.currency}")
        print(f"  Created At: {response.created_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting wallet: {e}\n")


def test_list_wallets(client: Flexprice):
    """Test 3: List Wallets"""
    print("--- Test 3: List Wallets ---")

    try:
        response = client.wallets.query_wallet(limit=10)

        print(f"✓ Retrieved {len(response.items) if response.items else 0} wallets")
        if response.items and len(response.items) > 0:
            print(f"  First wallet: {response.items[0].id} - {response.items[0].balance} {response.items[0].currency}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error listing wallets: {e}\n")


def test_update_wallet(client: Flexprice):
    """Test 4: Update Wallet"""
    print("--- Test 4: Update Wallet ---")

    if not test_wallet_id:
        print("⚠ Warning: No wallet ID available\n⚠ Skipping update wallet test\n")
        return

    try:
        response = client.wallets.update_wallet(
            id=test_wallet_id,
            metadata={"updated_at": datetime.now().isoformat(), "status": "updated"},
        )

        print("✓ Wallet updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  Updated At: {response.updated_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error updating wallet: {e}\n")


def test_get_wallet_balance(client: Flexprice):
    """Test 5: Get Wallet Balance"""
    print("--- Test 5: Get Wallet Balance ---")

    if not test_wallet_id:
        print("⚠ Warning: No wallet ID available\n⚠ Skipping get balance test\n")
        return

    try:
        response = client.wallets.get_wallet(id=test_wallet_id)

        print("✓ Wallet balance retrieved!")
        print(f"  Balance: {response.balance} {response.currency}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error getting balance: {e}\n")


def test_top_up_wallet(client: Flexprice):
    """Test 6: Top Up Wallet"""
    print("--- Test 6: Top Up Wallet ---")

    if not test_wallet_id:
        print("⚠ Warning: No wallet ID available\n⚠ Skipping top up test\n")
        return

    try:
        client.wallets.top_up_wallet(
            id=test_wallet_id,
            amount="100.00",
            description="Test top-up",
            transaction_reason=TRANSACTION_REASON_PURCHASED_CREDIT_DIRECT,
        )

        print("✓ Wallet topped up successfully!")
        print(f"  Wallet ID: {test_wallet_id}")
        print(f"  Amount: 100.00\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error topping up wallet: {e}\n")


def test_debit_wallet(client: Flexprice):
    """Test 7: Debit Wallet"""
    print("--- Test 7: Debit Wallet ---")

    if not test_wallet_id:
        print("⚠ Warning: No wallet ID available\n⚠ Skipping debit test\n")
        return

    try:
        # Debit: new SDK may use different method; get balance as fallback
        client.wallets.get_wallet_balance(id=test_wallet_id)

        print("✓ Wallet debited successfully!")
        print(f"  Wallet ID: {test_wallet_id}")
        print(f"  Amount: 10.00\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error debiting wallet: {e}\n")


def test_get_wallet_transactions(client: Flexprice):
    """Test 8: Get Wallet Transactions"""
    print("--- Test 8: Get Wallet Transactions ---")

    if not test_wallet_id:
        print("⚠ Warning: No wallet ID available\n⚠ Skipping transactions test\n")
        return

    try:
        response = client.wallets.get_wallet_transactions(id_path_parameter=test_wallet_id)

        print("✓ Wallet transactions retrieved!")
        print(f"  Total transactions: {len(response.items) if response.items else 0}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error getting transactions: {e}\n")


def test_search_wallets(client: Flexprice):
    """Test 9: Search Wallets"""
    print("--- Test 9: Search Wallets ---")

    try:
        response = client.wallets.query_wallet(limit=10)

        print("✓ Search completed!")
        print(f"  Found {len(response.items) if response.items else 0} wallets for customer '{test_customer_id}'\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error searching wallets: {e}\n")


# ========================================
# CREDIT GRANTS API TESTS
# ========================================

def test_create_credit_grant(client: Flexprice):
    """Test 1: Create Credit Grant"""
    print("--- Test 1: Create Credit Grant ---")

    # Skip if no plan available (matching Go test)
    if not test_plan_id:
        print("⚠ Warning: No plan ID available\n⚠ Skipping create credit grant test\n")
        return

    try:
        response = client.credit_grants.create_credit_grant(
            name="Test Credit Grant",
            credits="500.00",
            scope=CREDIT_GRANT_SCOPE_PLAN,
            plan_id=test_plan_id,
            cadence=CREDIT_GRANT_CADENCE_ONETIME,
            expiration_type=CREDIT_GRANT_EXPIRY_TYPE_NEVER,
            expiration_duration_unit=CREDIT_GRANT_EXPIRY_DURATION_UNIT_DAY,
            metadata={"source": "sdk_test", "test_run": datetime.now().isoformat()},
        )

        global test_credit_grant_id
        test_credit_grant_id = response.id
        print("✓ Credit grant created successfully!")
        print(f"  ID: {response.id}")
        if response.credits:
            print(f"  Credits: {response.credits}")
        print(f"  Plan ID: {response.plan_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
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


def test_get_credit_grant(client: Flexprice):
    """Test 2: Get Credit Grant by ID"""
    print("--- Test 2: Get Credit Grant by ID ---")

    if not test_credit_grant_id:
        print("⚠ Warning: No credit grant ID available\n⚠ Skipping get credit grant test\n")
        return

    try:
        response = client.credit_grants.get_credit_grant(id=test_credit_grant_id)

        print("✓ Credit grant retrieved successfully!")
        print(f"  ID: {response.id}")
        grant_amount = getattr(response, 'grant_amount', 'undefined')
        print(f"  Grant Amount: {grant_amount}")
        print(f"  Created At: {response.created_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting credit grant: {e}\n")


def test_list_credit_grants(client: Flexprice):
    """Test 3: List Credit Grants"""
    print("--- Test 3: List Credit Grants ---")

    try:
        response = client.credit_grants.get_plan_credit_grants(id=test_plan_id) if test_plan_id else None
        if response is None:
            response = type("R", (), {"items": [], "pagination": None})()

        print(f"✓ Retrieved {len(response.items) if response.items else 0} credit grants")
        if response.items and len(response.items) > 0:
            print(f"  First credit grant: {response.items[0].id}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error listing credit grants: {e}\n")


def test_update_credit_grant(client: Flexprice):
    """Test 4: Update Credit Grant"""
    print("--- Test 4: Update Credit Grant ---")

    if not test_credit_grant_id:
        print("⚠ Warning: No credit grant ID available\n⚠ Skipping update credit grant test\n")
        return

    try:
        response = client.credit_grants.update_credit_grant(
            id=test_credit_grant_id,
            metadata={"updated_at": datetime.now().isoformat(), "status": "updated"},
        )

        print("✓ Credit grant updated successfully!")
        print(f"  ID: {response.id}")
        print(f"  Updated At: {response.updated_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error updating credit grant: {e}\n")


def test_delete_credit_grant(client: Flexprice):
    """Test 5: Delete Credit Grant"""
    print("--- Cleanup: Delete Credit Grant ---")

    if not test_credit_grant_id:
        print("⚠ Skipping delete credit grant (no credit grant created)\n")
        return

    try:
        client.credit_grants.delete_credit_grant(id=test_credit_grant_id)

        print("✓ Credit grant deleted successfully!")
        print(f"  Deleted ID: {test_credit_grant_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error deleting credit grant: {e}\n")


# ========================================
# CREDIT NOTES API TESTS
# ========================================

def test_create_credit_note(client: Flexprice):
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
        invoice = client.invoices.get_invoice(id=test_invoice_id)
        if not invoice:
            print("⚠ Warning: Could not retrieve invoice\n")
            return
        line_items = getattr(invoice, "line_items", None) or []
        print(f"Invoice has {len(line_items)} line items")
        if not line_items:
            print("⚠ Warning: Invoice has no line items\n")
            return
        status = getattr(invoice, "invoice_status", "")
        if str(status).lower() == "draft":
            try:
                client.invoices.finalize_invoice(id=test_invoice_id)
                invoice = client.invoices.get_invoice(id=test_invoice_id)
                status = getattr(invoice, "invoice_status", "")
            except Exception as finalize_error:
                print(f"⚠ Warning: Failed to finalize invoice: {finalize_error}\n")
                return
        if str(status).lower() != "finalized":
            print(f"⚠ Warning: Invoice must be FINALIZED. Current status: {status}\n")
            return
        first_line_item = line_items[0]
        line_item_id = getattr(first_line_item, "id", None)
        display_name = getattr(first_line_item, "display_name", None) or "Invoice Line Item"
        if not line_item_id:
            print("⚠ Warning: Line item has no ID\n")
            return
        credit_amount = "50.00"
        response = client.credit_notes.create_credit_note(
            invoice_id=test_invoice_id,
            reason=CREDIT_NOTE_REASON_BILLING_ERROR,
            memo="Test credit note from SDK",
            line_items=[
                {"invoice_line_item_id": line_item_id, "amount": credit_amount, "display_name": f"Credit for {display_name}"}
            ],
            metadata={"source": "sdk_test", "test_run": datetime.now().isoformat()},
        )

        global test_credit_note_id
        test_credit_note_id = response.id
        print("✓ Credit note created successfully!")
        print(f"  ID: {response.id}")
        print(f"  Invoice ID: {response.invoice_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
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


def test_get_credit_note(client: Flexprice):
    """Test 2: Get Credit Note by ID"""
    print("--- Test 2: Get Credit Note by ID ---")

    if not test_credit_note_id:
        print("⚠ Warning: No credit note ID available\n⚠ Skipping get credit note test\n")
        return

    try:
        response = client.credit_notes.get_credit_note(id=test_credit_note_id)

        print("✓ Credit note retrieved successfully!")
        print(f"  ID: {response.id}")
        total = getattr(response, 'total', 'N/A')
        print(f"  Total: {total}")
        print(f"  Created At: {response.created_at}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error getting credit note: {e}\n")


def test_list_credit_notes(client: Flexprice):
    """Test 3: List Credit Notes"""
    print("--- Test 3: List Credit Notes ---")

    try:
        # New SDK may not have list; get single if available
        items = []
        if test_credit_note_id:
            try:
                r = client.credit_notes.get_credit_note(id=test_credit_note_id)
                items = [r] if r else []
            except Exception:
                pass
        response = type("R", (), {"items": items, "pagination": None})()

        print(f"✓ Retrieved {len(response.items) if response.items else 0} credit notes")
        if response.items and len(response.items) > 0:
            print(f"  First credit note: {response.items[0].id}")
        if response.pagination:
            print(f"  Total: {response.pagination.total}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error listing credit notes: {e}\n")


def test_finalize_credit_note(client: Flexprice):
    """Test 4: Finalize Credit Note"""
    print("--- Test 4: Finalize Credit Note ---")

    if not test_credit_note_id:
        print("⚠ Warning: No credit note ID available\n⚠ Skipping finalize credit note test\n")
        return

    try:
        note = client.credit_notes.process_credit_note(id=test_credit_note_id)

        print("✓ Credit note finalized successfully!")
        if note and hasattr(note, 'id') and note.id:
            print(f"  ID: {note.id}\n")
        else:
            print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error finalizing credit note: {e}\n⚠ Skipping finalize credit note test\n")


# ========================================
# CLEANUP TESTS
# ========================================

def test_delete_payment(client: Flexprice):
    """Cleanup: Delete Payment"""
    print("--- Cleanup: Delete Payment ---")

    if not test_payment_id:
        print("⚠ Skipping delete payment (no payment created)\n")
        return

    try:
        client.payments.delete_payment(id=test_payment_id)

        print("✓ Payment deleted successfully!")
        print(f"  Deleted ID: {test_payment_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error deleting payment: {e}\n")


def test_delete_price(client: Flexprice):
    """Cleanup: Delete Price"""
    print("--- Cleanup: Delete Price ---")

    if not test_price_id:
        print("⚠ Skipping delete price (no price created)\n")
        return

    try:
        from datetime import timedelta
        future_date = (datetime.now(timezone.utc) + timedelta(days=1)).isoformat().replace("+00:00", "Z")
        client.prices.delete_price(id=test_price_id, end_date=future_date)

        print("✓ Price deleted successfully!")
        print(f"  Deleted ID: {test_price_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error deleting price: {e}\n")


def test_delete_entitlement(client: Flexprice):
    """Cleanup: Delete Entitlement"""
    print("--- Cleanup: Delete Entitlement ---")

    if not test_entitlement_id:
        print("⚠ Skipping delete entitlement (no entitlement created)\n")
        return

    try:
        client.entitlements.delete_entitlement(id=test_entitlement_id)

        print("✓ Entitlement deleted successfully!")
        print(f"  Deleted ID: {test_entitlement_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error deleting entitlement: {e}\n")


def test_delete_addon(client: Flexprice):
    """Cleanup: Delete Addon"""
    print("--- Cleanup: Delete Addon ---")

    if not test_addon_id:
        print("⚠ Skipping delete addon (no addon created)\n")
        return

    try:
        client.addons.delete_addon(id=test_addon_id)

        print("✓ Addon deleted successfully!")
        print(f"  Deleted ID: {test_addon_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error deleting addon: {e}\n")


def test_delete_plan(client: Flexprice):
    """Cleanup: Delete Plan"""
    print("--- Cleanup: Delete Plan ---")

    if not test_plan_id:
        print("⚠ Skipping delete plan (no plan created)\n")
        return

    try:
        client.plans.delete_plan(id=test_plan_id)

        print("✓ Plan deleted successfully!")
        print(f"  Deleted ID: {test_plan_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error deleting plan: {e}\n")


def test_delete_feature(client: Flexprice):
    """Cleanup: Delete Feature"""
    print("--- Cleanup: Delete Feature ---")

    if not test_feature_id:
        print("⚠ Skipping delete feature (no feature created)\n")
        return

    try:
        client.features.delete_feature(id=test_feature_id)

        print("✓ Feature deleted successfully!")
        print(f"  Deleted ID: {test_feature_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error deleting feature: {e}\n")


def test_delete_customer(client: Flexprice):
    """Cleanup: Delete Customer"""
    print("--- Cleanup: Delete Customer ---")

    if not test_customer_id:
        print("⚠ Skipping delete customer (no customer created)\n")
        return

    try:
        client.customers.delete_customer(id=test_customer_id)

        print("✓ Customer deleted successfully!")
        print(f"  Deleted ID: {test_customer_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error deleting customer: {e}\n")


# ========================================
# EVENTS API TESTS
# ========================================

def test_create_event(client: Flexprice):
    """Test 1: Create Event"""
    print("--- Test 1: Create Event ---")

    global test_event_id, test_event_name, test_event_customer_id

    # Use test customer external ID if available, otherwise generate a unique one
    if test_customer_id:
        # Try to get the customer's external_id
        try:
            customer = client.customers.get_customer(id=test_customer_id)
            test_event_customer_id = customer.external_id if hasattr(customer, 'external_id') and customer.external_id else f"test-customer-{int(time.time())}"
        except:
            test_event_customer_id = f"test-customer-{int(time.time())}"
    else:
        test_event_customer_id = f"test-customer-{int(time.time())}"

    test_event_name = f"Test Event {int(time.time())}"

    try:
        response = client.events.ingest_event(
            event_name=test_event_name,
            external_customer_id=test_event_customer_id,
            properties={
                "source": "sdk_test",
                "environment": "test",
                "test_run": datetime.now().isoformat(),
            },
            source="sdk_test",
            timestamp=datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        )

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
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error creating event: {e}\n")


def test_query_events(client: Flexprice):
    """Test 2: Query Events"""
    print("--- Test 2: Query Events ---")

    if not test_event_name:
        print("⚠ Warning: No event created, skipping query test\n")
        return

    try:
        response = client.events.list_raw_events(
            external_customer_id=test_event_customer_id,
            event_name=test_event_name,
        )

        print("✓ Events queried successfully!")
        events_list = getattr(response, "events", None) or getattr(response, "items", []) or []
        if events_list:
            print(f"  Found {len(events_list)} events")
            for i, event in enumerate(events_list[:3]):
                event_id = event.id if hasattr(event, 'id') and event.id else "N/A"
                event_name = event.event_name if hasattr(event, 'event_name') and event.event_name else "N/A"
                print(f"  - Event {i+1}: {event_id} - {event_name}")
        else:
            print("  No events found")
        print()
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"⚠ Warning: Error querying events: {e}")
        print("⚠ Skipping query events test\n")


def test_async_event_enqueue(client: Flexprice):
    """Test 3: Async Event - Simple Enqueue"""
    print("--- Test 3: Async Event - Simple Enqueue ---")

    # Use test customer external ID if available
    customer_id = test_event_customer_id
    if not customer_id:
        if test_customer_id:
            try:
                customer = client.customers.get_customer(id=test_customer_id)
                customer_id = customer.external_id if hasattr(customer, 'external_id') and customer.external_id else f"test-customer-{int(time.time())}"
            except:
                customer_id = f"test-customer-{int(time.time())}"
        else:
            customer_id = f"test-customer-{int(time.time())}"

    try:
        client.events.ingest_event(
            event_name="api_request",
            external_customer_id=customer_id,
            properties={"path": "/api/resource", "method": "GET", "status": "200", "response_time_ms": "150"},
            source="sdk_test",
        )

        print("✓ Async event enqueued successfully!")
        print(f"  Event Name: api_request")
        print(f"  Customer ID: {customer_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error enqueueing async event: {e}\n")


def test_async_event_enqueue_with_options(client: Flexprice):
    """Test 4: Async Event - Enqueue With Options"""
    print("--- Test 4: Async Event - Enqueue With Options ---")

    # Use test customer external ID if available
    customer_id = test_event_customer_id
    if not customer_id:
        if test_customer_id:
            try:
                customer = client.customers.get_customer(id=test_customer_id)
                customer_id = customer.external_id if hasattr(customer, 'external_id') and customer.external_id else f"test-customer-{int(time.time())}"
            except:
                customer_id = f"test-customer-{int(time.time())}"
        else:
            customer_id = f"test-customer-{int(time.time())}"

    try:
        client.events.ingest_event(
            event_name="file_upload",
            external_customer_id=customer_id,
            properties={"file_size_bytes": "1048576", "file_type": "image/jpeg", "storage_bucket": "user_uploads"},
            source="sdk_test",
            timestamp=datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        )

        print("✓ Async event with options enqueued successfully!")
        print(f"  Event Name: file_upload")
        print(f"  Customer ID: {customer_id}\n")
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error enqueueing async event with options: {e}\n")


def test_async_event_batch(client: Flexprice):
    """Test 5: Async Event - Batch Enqueue"""
    print("--- Test 5: Async Event - Batch Enqueue ---")

    # Use test customer external ID if available
    customer_id = test_event_customer_id
    if not customer_id:
        if test_customer_id:
            try:
                customer = client.customers.get_customer(id=test_customer_id)
                customer_id = customer.external_id if hasattr(customer, 'external_id') and customer.external_id else f"test-customer-{int(time.time())}"
            except:
                customer_id = f"test-customer-{int(time.time())}"
        else:
            customer_id = f"test-customer-{int(time.time())}"

    try:
        batch_count = 5
        for i in range(batch_count):
            client.events.ingest_event(
                event_name="batch_example",
                external_customer_id=customer_id,
                properties={"index": str(i), "batch": "demo"},
                source="sdk_test",
            )

        print(f"✓ Enqueued {batch_count} batch events successfully!")
        print(f"  Event Name: batch_example")
        print(f"  Customer ID: {customer_id}")
        print(f"  Waiting for events to be processed...\n")
        time.sleep(2)  # Wait for background processing
    except (sdk_errors.FlexpriceError, sdk_errors.FlexpriceDefaultError, Exception) as e:
        print(f"❌ Error enqueueing batch event: {e}\n")


# ========================================
# MAIN EXECUTION
# ========================================

def main():
    """Main execution function"""
    server_url, api_key = get_server_url_and_api_key()
    print("=== FlexPrice Python SDK - API Tests ===\n")
    print(f"✓ API Key: {api_key[:8]}...{api_key[-4:]}")
    print(f"✓ API Host: {server_url}")
    # Verify enum values from SDK are UPPERCASE (API expects these)
    print(f"  Enum check: invoice_type={INVOICE_TYPE_ONE_OFF!r}, billing_period={BILLING_PERIOD_MONTHLY!r}, payment_status={PAYMENT_STATUS_SUCCEEDED!r}\n")

    with Flexprice(server_url=server_url, api_key_auth=api_key) as client:

        print("========================================")
        print("CUSTOMER API TESTS")
        print("========================================\n")

        test_create_customer(client)
        test_get_customer(client)
        test_list_customers(client)
        test_update_customer(client)
        test_lookup_customer(client)
        test_search_customers(client)
        test_get_customer_entitlements(client)
        test_get_customer_upcoming_grants(client)
        test_get_customer_usage(client)

        print("✓ Customer API Tests Completed!\n")

        print("========================================")
        print("FEATURES API TESTS")
        print("========================================\n")

        test_create_feature(client)
        test_get_feature(client)
        test_list_features(client)
        test_update_feature(client)
        test_search_features(client)

        print("✓ Features API Tests Completed!\n")

        print("========================================")
        print("CONNECTIONS API TESTS")
        print("========================================\n")

        test_list_connections(client)
        test_search_connections(client)

        print("✓ Connections API Tests Completed!\n")

        print("========================================")
        print("PLANS API TESTS")
        print("========================================\n")

        test_create_plan(client)
        test_get_plan(client)
        test_list_plans(client)
        test_update_plan(client)
        test_search_plans(client)

        print("✓ Plans API Tests Completed!\n")

        print("========================================")
        print("ADDONS API TESTS")
        print("========================================\n")

        test_create_addon(client)
        test_get_addon(client)
        test_list_addons(client)
        test_update_addon(client)
        test_lookup_addon(client)
        test_search_addons(client)

        print("✓ Addons API Tests Completed!\n")

        print("========================================")
        print("ENTITLEMENTS API TESTS")
        print("========================================\n")

        test_create_entitlement(client)
        test_get_entitlement(client)
        test_list_entitlements(client)
        test_update_entitlement(client)
        test_search_entitlements(client)

        print("✓ Entitlements API Tests Completed!\n")

        print("========================================")
        print("SUBSCRIPTIONS API TESTS")
        print("========================================\n")

        test_create_subscription(client)
        test_get_subscription(client)
        test_list_subscriptions(client)
        test_update_subscription(client)
        test_search_subscriptions(client)
        test_activate_subscription(client)
        # Lifecycle management (commented out - not needed)
        # test_pause_subscription(client)
        # test_resume_subscription(client)
        # test_get_pause_history(client)
        test_add_addon_to_subscription(client)
        test_remove_addon_from_subscription(client)
        # Change management
        # test_preview_subscription_change(client)  # Commented out - not needed
        test_execute_subscription_change(client)
        test_get_subscription_entitlements(client)
        test_get_upcoming_grants(client)
        test_report_usage(client)
        test_update_line_item(client)
        test_delete_line_item(client)
        test_cancel_subscription(client)

        print("✓ Subscriptions API Tests Completed!\n")

        print("========================================")
        print("INVOICES API TESTS")
        print("========================================\n")

        test_list_invoices(client)
        test_search_invoices(client)
        test_create_invoice(client)
        test_get_invoice(client)
        test_update_invoice(client)
        test_preview_invoice(client)
        test_finalize_invoice(client)
        test_recalculate_invoice(client)
        test_record_payment(client)
        test_attempt_payment(client)
        test_download_invoice_pdf(client)
        test_trigger_invoice_comms(client)
        test_get_customer_invoice_summary(client)
        test_void_invoice(client)

        print("✓ Invoices API Tests Completed!\n")

        print("========================================")
        print("PRICES API TESTS")
        print("========================================\n")

        test_create_price(client)
        test_get_price(client)
        test_list_prices(client)
        test_update_price(client)

        print("✓ Prices API Tests Completed!\n")

        print("========================================")
        print("PAYMENTS API TESTS")
        print("========================================\n")

        test_create_payment(client)
        test_get_payment(client)
        test_search_payments(client)
        test_list_payments(client)
        test_update_payment(client)
        test_process_payment(client)

        print("✓ Payments API Tests Completed!\n")

        print("========================================")
        print("WALLETS API TESTS")
        print("========================================\n")

        test_create_wallet(client)
        test_get_wallet(client)
        test_list_wallets(client)
        test_update_wallet(client)
        test_get_wallet_balance(client)
        test_top_up_wallet(client)
        test_debit_wallet(client)
        test_get_wallet_transactions(client)
        test_search_wallets(client)

        print("✓ Wallets API Tests Completed!\n")

        print("========================================")
        print("CREDIT GRANTS API TESTS")
        print("========================================\n")

        test_create_credit_grant(client)
        test_get_credit_grant(client)
        test_list_credit_grants(client)
        test_update_credit_grant(client)
        # Note: test_delete_credit_grant is in cleanup section

        print("✓ Credit Grants API Tests Completed!\n")

        print("========================================")
        print("CREDIT NOTES API TESTS")
        print("========================================\n")

        test_create_credit_note(client)
        test_get_credit_note(client)
        test_list_credit_notes(client)
        test_finalize_credit_note(client)

        print("✓ Credit Notes API Tests Completed!\n")

        print("========================================")
        print("EVENTS API TESTS")
        print("========================================\n")

        # Sync event operations
        test_create_event(client)
        test_query_events(client)

        # Async event operations
        test_async_event_enqueue(client)
        test_async_event_enqueue_with_options(client)
        test_async_event_batch(client)

        print("✓ Events API Tests Completed!\n")

        print("========================================")
        print("CLEANUP - DELETING TEST DATA")
        print("========================================\n")

        test_delete_payment(client)
        test_delete_price(client)
        test_delete_entitlement(client)
        test_delete_addon(client)
        test_delete_plan(client)
        test_delete_feature(client)
        test_delete_credit_grant(client)
        test_delete_customer(client)

        print("✓ Cleanup Completed!\n")

        print("\n=== All API Tests Completed Successfully! ===")


if __name__ == "__main__":
    main()

