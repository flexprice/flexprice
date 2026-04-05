#!/usr/bin/env python3
"""
FlexPrice Python SDK — comprehensive example.

Demonstrates sync and async usage patterns:
  - Client initialization
  - Customer create / get / paginate
  - Idempotency headers
  - Error handling (409 conflict)
  - Safe optional-field access
  - Plan and subscription creation
  - Per-request retry config
  - Single-event and bulk-event ingestion
  - Subscription cancellation
  - Invoice listing with filters
  - Full async mirror of the sync flow

Set FLEXPRICE_API_KEY (and optionally FLEXPRICE_API_HOST) in .env or environment.
"""

import asyncio
import os
import sys
import time
import uuid

from dotenv import load_dotenv

load_dotenv()

from flexprice import Flexprice
from flexprice import utils as flexprice_utils
from flexprice.models import (
    BulkIngestEventRequest,
    CancelSubscriptionRequest,
    CreateCustomerRequest,
    CreatePlanRequest,
    CreateSubscriptionRequest,
    CustomerFilter,
    IngestEventRequest,
    InvoiceFilter,
    PlanFilter,
)
from flexprice.models.errors.flexpriceerror import FlexpriceError

# ── Helpers ──────────────────────────────────────────────────────────────────

def _server_url() -> str:
    api_host = os.getenv("FLEXPRICE_API_HOST", "us.api.flexprice.io")
    return api_host if api_host.startswith(("http://", "https://")) else f"https://{api_host}"


def _api_key() -> str:
    key = os.getenv("FLEXPRICE_API_KEY", "")
    if not key:
        print("ERROR: FLEXPRICE_API_KEY is not set.", file=sys.stderr)
        sys.exit(1)
    return key


def _uid(prefix: str) -> str:
    """Generate a short unique external ID suitable for idempotent re-runs."""
    return f"{prefix}-{uuid.uuid4().hex[:8]}"


# ── Section: Create or get customer ──────────────────────────────────────────

def create_or_get_customer(client: Flexprice, external_id: str):
    """
    Create a customer.  If the server returns 409 (already exists), fetch
    and return the existing record instead.

    Also demonstrates:
      - Pydantic model construction with optional fields
      - TypedDict variant (plain dict) as an equivalent alternative
      - Safe access to optional response fields
    """
    print(f"\n[customers] Creating customer external_id={external_id!r}")

    request = CreateCustomerRequest(
        external_id=external_id,
        name="Acme Corp",
        email="billing@acme.example.com",
        address_line1="123 Market St",
        address_city="San Francisco",
        address_state="CA",
        address_postal_code="94105",
        address_country="US",
        metadata={"tier": "enterprise", "source": "sdk-example"},
    )

    # Equivalent using TypedDict (plain dict) variant — no import needed for the type itself:
    #
    #   request = {
    #       "external_id": external_id,
    #       "name": "Acme Corp",
    #       "email": "billing@acme.example.com",
    #       "address_country": "US",
    #       "metadata": {"tier": "enterprise"},
    #   }

    try:
        customer = client.customers.create_customer(request=request)
        print(f"  Created: id={customer.id!r}  name={customer.name or 'N/A'}")
        return customer
    except FlexpriceError as exc:
        if exc.status_code == 409:
            # Customer already exists — look it up by external_id
            print(f"  409 conflict — fetching existing customer …")
            resp = client.customers.query_customer(
                request=CustomerFilter(external_id=external_id, limit=1)
            )
            customers = resp.data if resp.data else []
            if not customers:
                print("ERROR: conflict but no customer found", file=sys.stderr)
                sys.exit(1)
            customer = customers[0]
            print(f"  Found existing: id={customer.id!r}")
            return customer
        print(f"ERROR creating customer: {exc.status_code} — {exc.message}", file=sys.stderr)
        raise


# ── Section: Paginate customers ───────────────────────────────────────────────

def list_all_customers(client: Flexprice):
    """Paginate through all customers using limit/offset."""
    print("\n[customers] Paginating all customers …")
    page_size = 20
    offset = 0
    total_fetched = 0

    while True:
        resp = client.customers.query_customer(
            request=CustomerFilter(limit=page_size, offset=offset, order="asc")
        )
        page = resp.data or []
        total_fetched += len(page)

        for c in page:
            print(f"  customer id={c.id!r}  email={c.email or 'N/A'}")

        # Determine whether there is a next page.
        # `resp.total` holds the server-side total count (when available).
        page_total = getattr(resp, "total", None)
        if page_total is not None and total_fetched >= page_total:
            break
        if len(page) < page_size:
            break
        offset += page_size

    print(f"  Total fetched: {total_fetched}")


# ── Section: Create plan ──────────────────────────────────────────────────────

def create_plan(client: Flexprice) -> str:
    """Create a minimal plan and return its ID."""
    print("\n[plans] Creating plan …")
    plan = client.plans.create_plan(
        request=CreatePlanRequest(
            name="Growth Plan",
            description="Monthly usage-based plan for growing teams",
            lookup_key=_uid("growth"),
            metadata={"category": "saas"},
        )
    )
    print(f"  Created plan id={plan.id!r}  name={plan.name or 'N/A'}")
    return plan.id


# ── Section: Create subscription ─────────────────────────────────────────────

def create_subscription(client: Flexprice, customer_id: str, plan_id: str) -> str:
    """
    Subscribe a customer to a plan.

    BillingCadence values: "RECURRING" | "ONETIME"
    BillingPeriod values:  "MONTHLY" | "ANNUAL" | "WEEKLY" | "DAILY" |
                           "QUARTERLY" | "HALF_YEARLY"
    CollectionMethod:      "charge_automatically" | "send_invoice"
    """
    print("\n[subscriptions] Creating subscription …")
    sub = client.subscriptions.create_subscription(
        request=CreateSubscriptionRequest(
            customer_id=customer_id,
            plan_id=plan_id,
            billing_cadence="RECURRING",
            billing_period="MONTHLY",
            billing_period_count=1,
            currency="USD",
            collection_method="charge_automatically",
            metadata={"env": "production"},
        )
    )
    print(f"  Created subscription id={sub.id!r}  status={getattr(sub, 'subscription_status', 'N/A')}")
    return sub.id


# ── Section: Ingest events ────────────────────────────────────────────────────

def ingest_events(client: Flexprice, external_customer_id: str):
    """
    Ingest a single event, then a bulk batch.

    Per-request retry config is applied to the single-event call to show
    how to override the global retry settings on an individual call.
    """
    print("\n[events] Ingesting single event with per-request retry config …")

    # Build a custom RetryConfig for this one call
    retry_cfg = flexprice_utils.RetryConfig(
        strategy="backoff",
        backoff=flexprice_utils.BackoffStrategy(
            initial_interval=500,    # ms
            max_interval=10_000,     # ms
            exponent=1.5,
            max_elapsed_time=30_000, # ms
        ),
        retry_connection_errors=True,
    )

    client.events.ingest_event(
        request=IngestEventRequest(
            event_name="api_call",
            external_customer_id=external_customer_id,
            event_id=_uid("evt"),
            timestamp=time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            properties={
                "model": "gpt-4o",
                "tokens_in": "512",
                "tokens_out": "256",
                "latency_ms": "320",
            },
            source="sdk-example",
        ),
        retries=retry_cfg,
    )
    print("  Single event ingested.")

    # Bulk ingest
    print("[events] Bulk-ingesting events …")
    client.events.ingest_events_bulk(
        request=BulkIngestEventRequest(
            events=[
                IngestEventRequest(
                    event_name="api_call",
                    external_customer_id=external_customer_id,
                    event_id=_uid("evt"),
                    properties={"model": "gpt-4o-mini", "tokens_in": "100", "tokens_out": "80"},
                ),
                IngestEventRequest(
                    event_name="storage_write",
                    external_customer_id=external_customer_id,
                    event_id=_uid("evt"),
                    properties={"bytes": "4096", "bucket": "user-data"},
                ),
            ]
        )
    )
    print("  Bulk events ingested.")


# ── Section: List invoices ────────────────────────────────────────────────────

def list_invoices(client: Flexprice, customer_id: str):
    """List invoices for a customer, filtered by status."""
    print("\n[invoices] Listing invoices …")
    resp = client.invoices.query_invoice(
        request=InvoiceFilter(
            customer_id=customer_id,
            limit=10,
            offset=0,
            order="desc",
        )
    )
    invoices = resp.data or []
    if not invoices:
        print("  No invoices found (expected for a brand-new subscription).")
        return
    for inv in invoices:
        amount = getattr(inv, "amount_due", None)
        status = getattr(inv, "invoice_status", "N/A")
        print(f"  invoice id={inv.id!r}  status={status}  amount_due={amount!r}")


# ── Section: Cancel subscription ─────────────────────────────────────────────

def cancel_subscription(client: Flexprice, subscription_id: str):
    """
    Cancel a subscription at end-of-period.

    CancellationType values: "immediate" | "end_of_period" | "scheduled_date"
    """
    print(f"\n[subscriptions] Cancelling subscription id={subscription_id!r} at end of period …")
    sub = client.subscriptions.cancel_subscription(
        id=subscription_id,
        body=CancelSubscriptionRequest(
            cancellation_type="end_of_period",
            reason="Downgrade requested by customer",
        ),
    )
    print(f"  Subscription status after cancel: {getattr(sub, 'subscription_status', 'N/A')}")


# ── Section: Async demo ───────────────────────────────────────────────────────

async def async_demo():
    """
    Mirror of the sync flow using the async client (async with / await _async).
    All SDK methods have a corresponding `<method>_async` variant.
    """
    print("\n" + "=" * 60)
    print("ASYNC DEMO")
    print("=" * 60)

    server_url = _server_url()
    api_key = _api_key()
    external_id = _uid("async-customer")

    async with Flexprice(server_url=server_url, api_key_auth=api_key) as client:

        # ── Create customer (async) ───────────────────────────────────────
        print(f"\n[async][customers] Creating customer external_id={external_id!r}")
        try:
            customer = await client.customers.create_customer_async(
                request=CreateCustomerRequest(
                    external_id=external_id,
                    name="Async Corp",
                    email="async@example.com",
                    address_country="US",
                    metadata={"flow": "async-demo"},
                )
            )
            customer_id = customer.id
            print(f"  Created: id={customer_id!r}  name={customer.name or 'N/A'}")
        except FlexpriceError as exc:
            if exc.status_code == 409:
                print("  409 — fetching existing customer …")
                resp = await client.customers.query_customer_async(
                    request=CustomerFilter(external_id=external_id, limit=1)
                )
                customers = resp.data or []
                if not customers:
                    print("ERROR: conflict but no customer found", file=sys.stderr)
                    return
                customer_id = customers[0].id
                print(f"  Existing id={customer_id!r}")
            else:
                print(f"ERROR: {exc.status_code} — {exc.message}", file=sys.stderr)
                return

        # ── Create plan (async) ───────────────────────────────────────────
        print("\n[async][plans] Creating plan …")
        plan = await client.plans.create_plan_async(
            request=CreatePlanRequest(
                name="Async Growth Plan",
                lookup_key=_uid("async-plan"),
            )
        )
        plan_id = plan.id
        print(f"  Created plan id={plan_id!r}")

        # ── Create subscription (async) ───────────────────────────────────
        print("\n[async][subscriptions] Creating subscription …")
        sub = await client.subscriptions.create_subscription_async(
            request=CreateSubscriptionRequest(
                customer_id=customer_id,
                plan_id=plan_id,
                billing_cadence="RECURRING",
                billing_period="MONTHLY",
                currency="USD",
                collection_method="send_invoice",
            )
        )
        subscription_id = sub.id
        print(f"  Created subscription id={subscription_id!r}")

        # ── Ingest event (async) ──────────────────────────────────────────
        print("\n[async][events] Ingesting event …")
        await client.events.ingest_event_async(
            request=IngestEventRequest(
                event_name="api_call",
                external_customer_id=external_id,
                event_id=_uid("async-evt"),
                properties={"model": "claude-3-5-haiku", "tokens_in": "200", "tokens_out": "150"},
            )
        )
        print("  Event ingested.")

        # ── List invoices (async) ─────────────────────────────────────────
        print("\n[async][invoices] Listing invoices …")
        inv_resp = await client.invoices.query_invoice_async(
            request=InvoiceFilter(customer_id=customer_id, limit=5, offset=0)
        )
        invoices = inv_resp.data or []
        print(f"  Invoices found: {len(invoices)}")

        # ── Cancel subscription (async) ───────────────────────────────────
        print(f"\n[async][subscriptions] Cancelling subscription id={subscription_id!r} …")
        await client.subscriptions.cancel_subscription_async(
            id=subscription_id,
            body=CancelSubscriptionRequest(
                cancellation_type="end_of_period",
                reason="Async demo teardown",
            ),
        )
        print("  Subscription cancelled.")

    print("\n[async] Done.")


# ── Main (sync) ───────────────────────────────────────────────────────────────

def main():
    server_url = _server_url()
    api_key = _api_key()

    print(f"FlexPrice Python SDK — complete example")
    print(f"Server: {server_url}")
    print(f"API key: {api_key[:4]}…{api_key[-4:]}")

    # External IDs are time-stamped so each run is idempotent on re-run:
    # the 409-conflict handler will reuse the existing object.
    external_id = _uid("customer")

    with Flexprice(server_url=server_url, api_key_auth=api_key) as client:

        # ── Customers ────────────────────────────────────────────────────
        customer = create_or_get_customer(client, external_id)
        customer_id = customer.id

        # Retrieve the same customer directly by FlexPrice ID
        fetched = client.customers.get_customer(id=customer_id)
        print(f"\n[customers] get_customer: id={fetched.id!r}  email={fetched.email or 'N/A'}")

        list_all_customers(client)

        # ── Plans ────────────────────────────────────────────────────────
        plan_id = create_plan(client)

        # Verify plan is queryable
        plan_resp = client.plans.query_plan(
            request=PlanFilter(plan_ids=[plan_id], limit=1)
        )
        plans = plan_resp.data or []
        print(f"\n[plans] query returned {len(plans)} plan(s)")

        # ── Subscriptions ─────────────────────────────────────────────────
        subscription_id = create_subscription(client, customer_id, plan_id)

        # Fetch subscription directly
        sub = client.subscriptions.get_subscription(id=subscription_id)
        print(f"\n[subscriptions] get_subscription: id={sub.id!r}")

        # ── Events ───────────────────────────────────────────────────────
        ingest_events(client, external_id)

        # ── Invoices ─────────────────────────────────────────────────────
        list_invoices(client, customer_id)

        # ── Cancel ───────────────────────────────────────────────────────
        cancel_subscription(client, subscription_id)

    print("\n[sync] Done.")

    # ── Async section ─────────────────────────────────────────────────────
    asyncio.run(async_demo())


if __name__ == "__main__":
    main()
