# flexprice

Developer-friendly & type-safe Python SDK specifically catered to leverage *flexprice* API.

[![Built by Speakeasy](https://img.shields.io/badge/Built_by-SPEAKEASY-374151?style=for-the-badge&labelColor=f3f4f6)](https://www.speakeasy.com/?utm_source=flexprice&utm_campaign=python)
[![License: MIT](https://img.shields.io/badge/LICENSE_//_MIT-3b5bdb?style=for-the-badge&labelColor=eff6ff)](https://opensource.org/licenses/MIT)


<br /><br />
> [!IMPORTANT]
> This SDK is not yet ready for production use. To complete setup please follow the steps outlined in your [workspace](https://app.speakeasy.com/org/flexprice/prod). Delete this section before > publishing to a package manager.

<!-- Start Summary [summary] -->
## Summary

FlexPrice API: FlexPrice API Service
<!-- End Summary [summary] -->

<!-- Start Table of Contents [toc] -->
## Table of Contents
<!-- $toc-max-depth=2 -->
* [flexprice](#flexprice)
  * [SDK Installation](#sdk-installation)
  * [IDE Support](#ide-support)
  * [SDK Example Usage](#sdk-example-usage)
  * [Authentication](#authentication)
  * [Available Resources and Operations](#available-resources-and-operations)
  * [File uploads](#file-uploads)
  * [Retries](#retries)
  * [Error Handling](#error-handling)
  * [Custom HTTP Client](#custom-http-client)
  * [Resource Management](#resource-management)
  * [Debugging](#debugging)
* [Development](#development)
  * [Maturity](#maturity)
  * [Contributions](#contributions)

<!-- End Table of Contents [toc] -->

<!-- Start SDK Installation [installation] -->
## SDK Installation

> [!TIP]
> To finish publishing your SDK to PyPI you must [run your first generation action](https://www.speakeasy.com/docs/github-setup#step-by-step-guide).


> [!NOTE]
> **Python version upgrade policy**
>
> Once a Python version reaches its [official end of life date](https://devguide.python.org/versions/), a 3-month grace period is provided for users to upgrade. Following this grace period, the minimum python version supported in the SDK will be updated.

The SDK can be installed with *uv*, *pip*, or *poetry* package managers.

### uv

*uv* is a fast Python package installer and resolver, designed as a drop-in replacement for pip and pip-tools. It's recommended for its speed and modern Python tooling capabilities.

```bash
uv add git+<UNSET>.git
```

### PIP

*PIP* is the default package installer for Python, enabling easy installation and management of packages from PyPI via the command line.

```bash
pip install git+<UNSET>.git
```

### Poetry

*Poetry* is a modern tool that simplifies dependency management and package publishing by using a single `pyproject.toml` file to handle project metadata and dependencies.

```bash
poetry add git+<UNSET>.git
```

### Shell and script usage with `uv`

You can use this SDK in a Python shell with [uv](https://docs.astral.sh/uv/) and the `uvx` command that comes with it like so:

```shell
uvx --from flexprice python
```

It's also possible to write a standalone Python script without needing to set up a whole project like so:

```python
#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.9"
# dependencies = [
#     "flexprice",
# ]
# ///

from flexprice import Flexprice

sdk = Flexprice(
  # SDK arguments
)

# Rest of script here...
```

Once that is saved to a file, you can run it with `uv run script.py` where
`script.py` can be replaced with the actual file name.
<!-- End SDK Installation [installation] -->

<!-- Start IDE Support [idesupport] -->
## IDE Support

### PyCharm

Generally, the SDK will work well with most IDEs out of the box. However, when using PyCharm, you can enjoy much better integration with Pydantic by installing an additional plugin.

- [PyCharm Pydantic Plugin](https://docs.pydantic.dev/latest/integrations/pycharm/)
<!-- End IDE Support [idesupport] -->

<!-- Start SDK Example Usage [usage] -->
## SDK Example Usage

### Example

```python
# Synchronous Example
from flexprice import Flexprice


with Flexprice(
    server_url="https://api.example.com",
    api_key_auth="<YOUR_API_KEY_HERE>",
) as f_client:

    res = f_client.addons.list()

    # Handle response
    print(res)
```

</br>

The same SDK client can also be used to make asynchronous requests by importing asyncio.

```python
# Asynchronous Example
import asyncio
from flexprice import Flexprice

async def main():

    async with Flexprice(
        server_url="https://api.example.com",
        api_key_auth="<YOUR_API_KEY_HERE>",
    ) as f_client:

        res = await f_client.addons.list_async()

        # Handle response
        print(res)

asyncio.run(main())
```
<!-- End SDK Example Usage [usage] -->

<!-- Start Authentication [security] -->
## Authentication

### Per-Client Security Schemes

This SDK supports the following security scheme globally:

| Name           | Type   | Scheme  |
| -------------- | ------ | ------- |
| `api_key_auth` | apiKey | API key |

To authenticate with the API the `api_key_auth` parameter must be set when initializing the SDK client instance. For example:
```python
from flexprice import Flexprice


with Flexprice(
    server_url="https://api.example.com",
    api_key_auth="<YOUR_API_KEY_HERE>",
) as f_client:

    res = f_client.addons.list()

    # Handle response
    print(res)

```
<!-- End Authentication [security] -->

<!-- Start Available Resources and Operations [operations] -->
## Available Resources and Operations

<details open>
<summary>Available methods</summary>

### [Addons](docs/sdks/addons/README.md)

* [list](docs/sdks/addons/README.md#list) - List addons
* [create](docs/sdks/addons/README.md#create) - Create addon
* [get_by_lookup_key](docs/sdks/addons/README.md#get_by_lookup_key) - Get addon by lookup key
* [search](docs/sdks/addons/README.md#search) - List addons by filter
* [get](docs/sdks/addons/README.md#get) - Get addon
* [update](docs/sdks/addons/README.md#update) - Update addon
* [delete](docs/sdks/addons/README.md#delete) - Delete addon

### [AlertLogs](docs/sdks/alertlogs/README.md)

* [search](docs/sdks/alertlogs/README.md#search) - List alert logs by filter

### [Auth](docs/sdks/auth/README.md)

* [login](docs/sdks/auth/README.md#login) - Login
* [signup](docs/sdks/auth/README.md#signup) - Sign up

### [Connections](docs/sdks/connections/README.md)

* [list](docs/sdks/connections/README.md#list) - Get connections
* [search](docs/sdks/connections/README.md#search) - List connections by filter
* [get_by_id](docs/sdks/connections/README.md#get_by_id) - Get a connection
* [update](docs/sdks/connections/README.md#update) - Update a connection
* [delete](docs/sdks/connections/README.md#delete) - Delete a connection

### [Costs](docs/sdks/costs/README.md)

* [create](docs/sdks/costs/README.md#create) - Create a new costsheet
* [get_active](docs/sdks/costs/README.md#get_active) - Get active costsheet for tenant
* [get_analytics](docs/sdks/costs/README.md#get_analytics) - Get combined revenue and cost analytics
* [search](docs/sdks/costs/README.md#search) - List costsheets by filter
* [get_by_id](docs/sdks/costs/README.md#get_by_id) - Get a costsheet by ID
* [update](docs/sdks/costs/README.md#update) - Update a costsheet
* [delete](docs/sdks/costs/README.md#delete) - Delete a costsheet

### [Coupons](docs/sdks/coupons/README.md)

* [list](docs/sdks/coupons/README.md#list) - List coupons with filtering
* [create](docs/sdks/coupons/README.md#create) - Create a new coupon
* [get_by_id](docs/sdks/coupons/README.md#get_by_id) - Get a coupon by ID
* [update](docs/sdks/coupons/README.md#update) - Update a coupon
* [delete](docs/sdks/coupons/README.md#delete) - Delete a coupon

### [Creditgrants](docs/sdks/creditgrants/README.md)

* [get](docs/sdks/creditgrants/README.md#get) - Get credit grants

### [CreditGrants](docs/sdks/flexpricecreditgrants/README.md)

* [create](docs/sdks/flexpricecreditgrants/README.md#create) - Create a new credit grant
* [get_by_id](docs/sdks/flexpricecreditgrants/README.md#get_by_id) - Get a credit grant by ID
* [update](docs/sdks/flexpricecreditgrants/README.md#update) - Update a credit grant
* [delete](docs/sdks/flexpricecreditgrants/README.md#delete) - Delete a credit grant
* [get_for_plan](docs/sdks/flexpricecreditgrants/README.md#get_for_plan) - Get plan credit grants

### [Creditnotes](docs/sdks/flexpricecreditnotes/README.md)

* [get](docs/sdks/flexpricecreditnotes/README.md#get) - Get a credit note by ID

### [CreditNotes](docs/sdks/creditnotes/README.md)

* [list](docs/sdks/creditnotes/README.md#list) - List credit notes with filtering
* [create](docs/sdks/creditnotes/README.md#create) - Create a new credit note
* [finalize](docs/sdks/creditnotes/README.md#finalize) - Process a draft credit note
* [void](docs/sdks/creditnotes/README.md#void) - Void a credit note

### [Customers](docs/sdks/customers/README.md)

* [list](docs/sdks/customers/README.md#list) - Get customers
* [create](docs/sdks/customers/README.md#create) - Create a customer
* [get_by_lookup_key](docs/sdks/customers/README.md#get_by_lookup_key) - Get a customer by lookup key
* [search](docs/sdks/customers/README.md#search) - List customers by filter
* [get_usage_summary](docs/sdks/customers/README.md#get_usage_summary) - Get customer usage summary
* [get_by_id](docs/sdks/customers/README.md#get_by_id) - Get a customer
* [update](docs/sdks/customers/README.md#update) - Update a customer
* [delete](docs/sdks/customers/README.md#delete) - Delete a customer
* [get_entitlements](docs/sdks/customers/README.md#get_entitlements) - Get customer entitlements
* [get_upcoming_grants](docs/sdks/customers/README.md#get_upcoming_grants) - Get upcoming credit grant applications

### [Entitlements](docs/sdks/entitlements/README.md)

* [get_for_addon](docs/sdks/entitlements/README.md#get_for_addon) - Get addon entitlements
* [list](docs/sdks/entitlements/README.md#list) - Get entitlements
* [create](docs/sdks/entitlements/README.md#create) - Create a new entitlement
* [bulk_create](docs/sdks/entitlements/README.md#bulk_create) - Create multiple entitlements in bulk
* [filter](docs/sdks/entitlements/README.md#filter) - List entitlements by filter
* [get](docs/sdks/entitlements/README.md#get) - Get an entitlement by ID
* [update](docs/sdks/entitlements/README.md#update) - Update an entitlement
* [delete](docs/sdks/entitlements/README.md#delete) - Delete an entitlement

### [EntityIntegrationMappings](docs/sdks/entityintegrationmappings/README.md)

* [list](docs/sdks/entityintegrationmappings/README.md#list) - List entity integration mappings
* [create](docs/sdks/entityintegrationmappings/README.md#create) - Create entity integration mapping
* [get_by_id](docs/sdks/entityintegrationmappings/README.md#get_by_id) - Get entity integration mapping
* [delete](docs/sdks/entityintegrationmappings/README.md#delete) - Delete entity integration mapping

### [Environments](docs/sdks/environments/README.md)

* [list](docs/sdks/environments/README.md#list) - Get environments
* [create](docs/sdks/environments/README.md#create) - Create an environment
* [get](docs/sdks/environments/README.md#get) - Get an environment
* [update](docs/sdks/environments/README.md#update) - Update an environment

### [Events](docs/sdks/events/README.md)

* [ingest](docs/sdks/events/README.md#ingest) - Ingest event
* [get_analytics](docs/sdks/events/README.md#get_analytics) - Get usage analytics
* [bulk_ingest](docs/sdks/events/README.md#bulk_ingest) - Bulk Ingest events
* [huggingface_inference](docs/sdks/events/README.md#huggingface_inference) - Get hugging face inference data
* [get_monitoring_data](docs/sdks/events/README.md#get_monitoring_data) - Get monitoring data
* [query](docs/sdks/events/README.md#query) - List raw events
* [get_usage_stats](docs/sdks/events/README.md#get_usage_stats) - Get usage statistics
* [get_usage_by_meter](docs/sdks/events/README.md#get_usage_by_meter) - Get usage by meter

### [Features](docs/sdks/features/README.md)

* [list](docs/sdks/features/README.md#list) - List features
* [create](docs/sdks/features/README.md#create) - Create a new feature
* [search](docs/sdks/features/README.md#search) - List features by filter
* [get_by_id](docs/sdks/features/README.md#get_by_id) - Get a feature by ID
* [update](docs/sdks/features/README.md#update) - Update a feature
* [delete](docs/sdks/features/README.md#delete) - Delete a feature

### [Groups](docs/sdks/groups/README.md)

* [create](docs/sdks/groups/README.md#create) - Create a group
* [search](docs/sdks/groups/README.md#search) - Get groups
* [get_by_id](docs/sdks/groups/README.md#get_by_id) - Get a group
* [delete](docs/sdks/groups/README.md#delete) - Delete a group

### [Integrations](docs/sdks/integrations/README.md)

* [delete_by_id](docs/sdks/integrations/README.md#delete_by_id) - Delete an integration
* [list_linked](docs/sdks/integrations/README.md#list_linked) - List linked integrations
* [get](docs/sdks/integrations/README.md#get) - Get integration details
* [create_or_update](docs/sdks/integrations/README.md#create_or_update) - Create or update an integration

### [Invoices](docs/sdks/invoices/README.md)

* [get_summary_by_customer_id](docs/sdks/invoices/README.md#get_summary_by_customer_id) - Get a customer invoice summary
* [list](docs/sdks/invoices/README.md#list) - List invoices
* [create](docs/sdks/invoices/README.md#create) - Create a new one off invoice
* [preview](docs/sdks/invoices/README.md#preview) - Get a preview invoice
* [search](docs/sdks/invoices/README.md#search) - List invoices by filter
* [get_by_id](docs/sdks/invoices/README.md#get_by_id) - Get an invoice by ID
* [update](docs/sdks/invoices/README.md#update) - Update an invoice
* [trigger_comms](docs/sdks/invoices/README.md#trigger_comms) - Trigger communication webhook for an invoice
* [finalize](docs/sdks/invoices/README.md#finalize) - Finalize an invoice
* [update_payment_status](docs/sdks/invoices/README.md#update_payment_status) - Update invoice payment status
* [initiate_payment](docs/sdks/invoices/README.md#initiate_payment) - Attempt payment for an invoice
* [get_pdf](docs/sdks/invoices/README.md#get_pdf) - Get PDF for an invoice
* [recalculate](docs/sdks/invoices/README.md#recalculate) - Recalculate invoice totals and line items
* [void](docs/sdks/invoices/README.md#void) - Void an invoice

### [Payments](docs/sdks/payments/README.md)

* [list](docs/sdks/payments/README.md#list) - List payments
* [create](docs/sdks/payments/README.md#create) - Create a new payment
* [get_by_id](docs/sdks/payments/README.md#get_by_id) - Get a payment by ID
* [update](docs/sdks/payments/README.md#update) - Update a payment
* [delete](docs/sdks/payments/README.md#delete) - Delete a payment
* [process](docs/sdks/payments/README.md#process) - Process a payment

### [Plans](docs/sdks/plans/README.md)

* [list](docs/sdks/plans/README.md#list) - Get plans
* [create](docs/sdks/plans/README.md#create) - Create a new plan
* [search](docs/sdks/plans/README.md#search) - List plans by filter
* [get_by_id](docs/sdks/plans/README.md#get_by_id) - Get a plan
* [update](docs/sdks/plans/README.md#update) - Update a plan
* [delete](docs/sdks/plans/README.md#delete) - Delete a plan
* [sync_subscriptions](docs/sdks/plans/README.md#sync_subscriptions) - Synchronize plan prices
* [get_entitlements](docs/sdks/plans/README.md#get_entitlements) - Get plan entitlements

### [Prices](docs/sdks/prices/README.md)

* [list](docs/sdks/prices/README.md#list) - Get prices
* [create](docs/sdks/prices/README.md#create) - Create a new price
* [bulk_create](docs/sdks/prices/README.md#bulk_create) - Create multiple prices in bulk
* [get_by_id](docs/sdks/prices/README.md#get_by_id) - Get a price by ID
* [update](docs/sdks/prices/README.md#update) - Update a price
* [delete](docs/sdks/prices/README.md#delete) - Delete a price

### [PriceUnits](docs/sdks/priceunits/README.md)

* [list](docs/sdks/priceunits/README.md#list) - List price units
* [create](docs/sdks/priceunits/README.md#create) - Create a new price unit
* [get_by_code](docs/sdks/priceunits/README.md#get_by_code) - Get a price unit by code
* [search](docs/sdks/priceunits/README.md#search) - List price units by filter
* [get_by_id](docs/sdks/priceunits/README.md#get_by_id) - Get a price unit by ID
* [update](docs/sdks/priceunits/README.md#update) - Update a price unit
* [archive](docs/sdks/priceunits/README.md#archive) - Archive a price unit

### [Rbac](docs/sdks/rbac/README.md)

* [list_roles](docs/sdks/rbac/README.md#list_roles) - List all RBAC roles
* [get_role](docs/sdks/rbac/README.md#get_role) - Get a specific RBAC role

### [ScheduledTasks](docs/sdks/scheduledtasks/README.md)

* [list](docs/sdks/scheduledtasks/README.md#list) - List scheduled tasks
* [create](docs/sdks/scheduledtasks/README.md#create) - Create a scheduled task
* [get](docs/sdks/scheduledtasks/README.md#get) - Get a scheduled task
* [update](docs/sdks/scheduledtasks/README.md#update) - Update a scheduled task
* [delete](docs/sdks/scheduledtasks/README.md#delete) - Delete a scheduled task
* [trigger_run](docs/sdks/scheduledtasks/README.md#trigger_run) - Trigger force run

### [Secrets](docs/sdks/secrets/README.md)

* [list_api_keys](docs/sdks/secrets/README.md#list_api_keys) - List API keys
* [create_api_key](docs/sdks/secrets/README.md#create_api_key) - Create a new API key
* [delete_api_key](docs/sdks/secrets/README.md#delete_api_key) - Delete an API key

### [Subscriptions](docs/sdks/subscriptions/README.md)

* [list](docs/sdks/subscriptions/README.md#list) - List subscriptions
* [create](docs/sdks/subscriptions/README.md#create) - Create subscription
* [add_addon](docs/sdks/subscriptions/README.md#add_addon) - Add addon to subscription
* [remove_addon](docs/sdks/subscriptions/README.md#remove_addon) - Remove addon from subscription
* [update_line_item](docs/sdks/subscriptions/README.md#update_line_item) - Update subscription line item
* [delete_line_item](docs/sdks/subscriptions/README.md#delete_line_item) - Delete subscription line item
* [search](docs/sdks/subscriptions/README.md#search) - List subscriptions by filter
* [get_usage](docs/sdks/subscriptions/README.md#get_usage) - Get usage by subscription
* [get](docs/sdks/subscriptions/README.md#get) - Get subscription
* [activate](docs/sdks/subscriptions/README.md#activate) - Activate draft subscription
* [cancel](docs/sdks/subscriptions/README.md#cancel) - Cancel subscription
* [execute_change](docs/sdks/subscriptions/README.md#execute_change) - Execute subscription plan change
* [preview_plan_change](docs/sdks/subscriptions/README.md#preview_plan_change) - Preview subscription plan change
* [get_entitlements](docs/sdks/subscriptions/README.md#get_entitlements) - Get subscription entitlements
* [get_upcoming_grants](docs/sdks/subscriptions/README.md#get_upcoming_grants) - Get upcoming credit grant applications
* [pause](docs/sdks/subscriptions/README.md#pause) - Pause a subscription
* [list_pauses](docs/sdks/subscriptions/README.md#list_pauses) - List all pauses for a subscription
* [resume](docs/sdks/subscriptions/README.md#resume) - Resume a paused subscription

### [Tasks](docs/sdks/tasks/README.md)

* [list](docs/sdks/tasks/README.md#list) - List tasks
* [create](docs/sdks/tasks/README.md#create) - Create a new task
* [get_result](docs/sdks/tasks/README.md#get_result) - Get task processing result
* [get](docs/sdks/tasks/README.md#get) - Get a task
* [update_status](docs/sdks/tasks/README.md#update_status) - Update task status

### [TaxAssociations](docs/sdks/taxassociations/README.md)

* [list](docs/sdks/taxassociations/README.md#list) - List tax associations
* [create](docs/sdks/taxassociations/README.md#create) - Create Tax Association
* [get_by_id](docs/sdks/taxassociations/README.md#get_by_id) - Get Tax Association
* [update](docs/sdks/taxassociations/README.md#update) - Update tax association
* [delete](docs/sdks/taxassociations/README.md#delete) - Delete tax association

### [TaxRates](docs/sdks/taxrates/README.md)

* [get_all](docs/sdks/taxrates/README.md#get_all) - Get tax rates
* [create](docs/sdks/taxrates/README.md#create) - Create a tax rate
* [get](docs/sdks/taxrates/README.md#get) - Get a tax rate
* [update](docs/sdks/taxrates/README.md#update) - Update a tax rate
* [delete](docs/sdks/taxrates/README.md#delete) - Delete a tax rate

### [Tenants](docs/sdks/tenants/README.md)

* [get_billing](docs/sdks/tenants/README.md#get_billing) - Get billing usage for the current tenant
* [create](docs/sdks/tenants/README.md#create) - Create a new tenant
* [update](docs/sdks/tenants/README.md#update) - Update a tenant
* [get_by_id](docs/sdks/tenants/README.md#get_by_id) - Get tenant by ID

### [Users](docs/sdks/users/README.md)

* [create_service_account](docs/sdks/users/README.md#create_service_account) - Create service account
* [get_with_me](docs/sdks/users/README.md#get_with_me) - Get user info
* [search](docs/sdks/users/README.md#search) - List service accounts with filters

### [Wallets](docs/sdks/wallets/README.md)

* [list_customer_wallets](docs/sdks/wallets/README.md#list_customer_wallets) - Get Customer Wallets
* [get_by_customer_id](docs/sdks/wallets/README.md#get_by_customer_id) - Get wallets by customer ID
* [create](docs/sdks/wallets/README.md#create) - Create a new wallet
* [get](docs/sdks/wallets/README.md#get) - Get wallet by ID
* [update](docs/sdks/wallets/README.md#update) - Update a wallet
* [get_balance_real_time](docs/sdks/wallets/README.md#get_balance_real_time) - Get wallet balance
* [debit](docs/sdks/wallets/README.md#debit) - Debit a wallet
* [terminate](docs/sdks/wallets/README.md#terminate) - Terminate a wallet
* [top_up](docs/sdks/wallets/README.md#top_up) - Top up wallet
* [get_transactions](docs/sdks/wallets/README.md#get_transactions) - Get wallet transactions

### [Webhooks](docs/sdks/webhooks/README.md)

* [process_chargebee](docs/sdks/webhooks/README.md#process_chargebee) - Handle Chargebee webhook events
* [handle_hubspot](docs/sdks/webhooks/README.md#handle_hubspot) - Handle HubSpot webhook events
* [process_nomod](docs/sdks/webhooks/README.md#process_nomod) - Handle Nomod webhook events
* [handle_quickbooks](docs/sdks/webhooks/README.md#handle_quickbooks) - Handle QuickBooks webhook events
* [handle_razorpay](docs/sdks/webhooks/README.md#handle_razorpay) - Handle Razorpay webhook events
* [process_stripe](docs/sdks/webhooks/README.md#process_stripe) - Handle Stripe webhook events

</details>
<!-- End Available Resources and Operations [operations] -->

<!-- Start File uploads [file-upload] -->
## File uploads

Certain SDK methods accept file objects as part of a request body or multi-part request. It is possible and typically recommended to upload files as a stream rather than reading the entire contents into memory. This avoids excessive memory consumption and potentially crashing with out-of-memory errors when working with very large files. The following example demonstrates how to attach a file stream to a request.

> [!TIP]
>
> For endpoints that handle file uploads bytes arrays can also be used. However, using streams is recommended for large files.
>

```python
from flexprice import Flexprice


with Flexprice(
    server_url="https://api.example.com",
    api_key_auth="<YOUR_API_KEY_HERE>",
) as f_client:

    res = f_client.events.get_analytics(request=open("example.file", "rb"))

    # Handle response
    print(res)

```
<!-- End File uploads [file-upload] -->

<!-- Start Retries [retries] -->
## Retries

Some of the endpoints in this SDK support retries. If you use the SDK without any configuration, it will fall back to the default retry strategy provided by the API. However, the default retry strategy can be overridden on a per-operation basis, or across the entire SDK.

To change the default retry strategy for a single API call, simply provide a `RetryConfig` object to the call:
```python
from flexprice import Flexprice
from flexprice.utils import BackoffStrategy, RetryConfig


with Flexprice(
    server_url="https://api.example.com",
    api_key_auth="<YOUR_API_KEY_HERE>",
) as f_client:

    res = f_client.addons.list(,
        RetryConfig("backoff", BackoffStrategy(1, 50, 1.1, 100), False))

    # Handle response
    print(res)

```

If you'd like to override the default retry strategy for all operations that support retries, you can use the `retry_config` optional parameter when initializing the SDK:
```python
from flexprice import Flexprice
from flexprice.utils import BackoffStrategy, RetryConfig


with Flexprice(
    server_url="https://api.example.com",
    retry_config=RetryConfig("backoff", BackoffStrategy(1, 50, 1.1, 100), False),
    api_key_auth="<YOUR_API_KEY_HERE>",
) as f_client:

    res = f_client.addons.list()

    # Handle response
    print(res)

```
<!-- End Retries [retries] -->

<!-- Start Error Handling [errors] -->
## Error Handling

[`FlexpriceError`](./src/flexprice/errors/flexpriceerror.py) is the base class for all HTTP error responses. It has the following properties:

| Property           | Type             | Description                                                                             |
| ------------------ | ---------------- | --------------------------------------------------------------------------------------- |
| `err.message`      | `str`            | Error message                                                                           |
| `err.status_code`  | `int`            | HTTP response status code eg `404`                                                      |
| `err.headers`      | `httpx.Headers`  | HTTP response headers                                                                   |
| `err.body`         | `str`            | HTTP body. Can be empty string if no body is returned.                                  |
| `err.raw_response` | `httpx.Response` | Raw HTTP response                                                                       |
| `err.data`         |                  | Optional. Some errors may contain structured data. [See Error Classes](#error-classes). |

### Example
```python
from flexprice import Flexprice, errors


with Flexprice(
    server_url="https://api.example.com",
    api_key_auth="<YOUR_API_KEY_HERE>",
) as f_client:
    res = None
    try:

        res = f_client.addons.list()

        # Handle response
        print(res)


    except errors.FlexpriceError as e:
        # The base class for HTTP error responses
        print(e.message)
        print(e.status_code)
        print(e.body)
        print(e.headers)
        print(e.raw_response)

        # Depending on the method different errors may be thrown
        if isinstance(e, errors.ErrorsErrorResponse):
            print(e.data.error)  # Optional[models.ErrorsErrorDetail]
            print(e.data.success)  # Optional[bool]
```

### Error Classes
**Primary errors:**
* [`FlexpriceError`](./src/flexprice/errors/flexpriceerror.py): The base class for HTTP error responses.
  * [`ErrorsErrorResponse`](./src/flexprice/errors/errorserrorresponse.py): *

<details><summary>Less common errors (5)</summary>

<br />

**Network errors:**
* [`httpx.RequestError`](https://www.python-httpx.org/exceptions/#httpx.RequestError): Base class for request errors.
    * [`httpx.ConnectError`](https://www.python-httpx.org/exceptions/#httpx.ConnectError): HTTP client was unable to make a request to a server.
    * [`httpx.TimeoutException`](https://www.python-httpx.org/exceptions/#httpx.TimeoutException): HTTP request timed out.


**Inherit from [`FlexpriceError`](./src/flexprice/errors/flexpriceerror.py)**:
* [`ResponseValidationError`](./src/flexprice/errors/responsevalidationerror.py): Type mismatch between the response data and the expected Pydantic model. Provides access to the Pydantic validation error via the `cause` attribute.

</details>

\* Check [the method documentation](#available-resources-and-operations) to see if the error is applicable.
<!-- End Error Handling [errors] -->

<!-- Start Custom HTTP Client [http-client] -->
## Custom HTTP Client

The Python SDK makes API calls using the [httpx](https://www.python-httpx.org/) HTTP library.  In order to provide a convenient way to configure timeouts, cookies, proxies, custom headers, and other low-level configuration, you can initialize the SDK client with your own HTTP client instance.
Depending on whether you are using the sync or async version of the SDK, you can pass an instance of `HttpClient` or `AsyncHttpClient` respectively, which are Protocol's ensuring that the client has the necessary methods to make API calls.
This allows you to wrap the client with your own custom logic, such as adding custom headers, logging, or error handling, or you can just pass an instance of `httpx.Client` or `httpx.AsyncClient` directly.

For example, you could specify a header for every request that this sdk makes as follows:
```python
from flexprice import Flexprice
import httpx

http_client = httpx.Client(headers={"x-custom-header": "someValue"})
s = Flexprice(client=http_client)
```

or you could wrap the client with your own custom logic:
```python
from flexprice import Flexprice
from flexprice.httpclient import AsyncHttpClient
import httpx

class CustomClient(AsyncHttpClient):
    client: AsyncHttpClient

    def __init__(self, client: AsyncHttpClient):
        self.client = client

    async def send(
        self,
        request: httpx.Request,
        *,
        stream: bool = False,
        auth: Union[
            httpx._types.AuthTypes, httpx._client.UseClientDefault, None
        ] = httpx.USE_CLIENT_DEFAULT,
        follow_redirects: Union[
            bool, httpx._client.UseClientDefault
        ] = httpx.USE_CLIENT_DEFAULT,
    ) -> httpx.Response:
        request.headers["Client-Level-Header"] = "added by client"

        return await self.client.send(
            request, stream=stream, auth=auth, follow_redirects=follow_redirects
        )

    def build_request(
        self,
        method: str,
        url: httpx._types.URLTypes,
        *,
        content: Optional[httpx._types.RequestContent] = None,
        data: Optional[httpx._types.RequestData] = None,
        files: Optional[httpx._types.RequestFiles] = None,
        json: Optional[Any] = None,
        params: Optional[httpx._types.QueryParamTypes] = None,
        headers: Optional[httpx._types.HeaderTypes] = None,
        cookies: Optional[httpx._types.CookieTypes] = None,
        timeout: Union[
            httpx._types.TimeoutTypes, httpx._client.UseClientDefault
        ] = httpx.USE_CLIENT_DEFAULT,
        extensions: Optional[httpx._types.RequestExtensions] = None,
    ) -> httpx.Request:
        return self.client.build_request(
            method,
            url,
            content=content,
            data=data,
            files=files,
            json=json,
            params=params,
            headers=headers,
            cookies=cookies,
            timeout=timeout,
            extensions=extensions,
        )

s = Flexprice(async_client=CustomClient(httpx.AsyncClient()))
```
<!-- End Custom HTTP Client [http-client] -->

<!-- Start Resource Management [resource-management] -->
## Resource Management

The `Flexprice` class implements the context manager protocol and registers a finalizer function to close the underlying sync and async HTTPX clients it uses under the hood. This will close HTTP connections, release memory and free up other resources held by the SDK. In short-lived Python programs and notebooks that make a few SDK method calls, resource management may not be a concern. However, in longer-lived programs, it is beneficial to create a single SDK instance via a [context manager][context-manager] and reuse it across the application.

[context-manager]: https://docs.python.org/3/reference/datamodel.html#context-managers

```python
from flexprice import Flexprice
def main():

    with Flexprice(
        server_url="https://api.example.com",
        api_key_auth="<YOUR_API_KEY_HERE>",
    ) as f_client:
        # Rest of application here...


# Or when using async:
async def amain():

    async with Flexprice(
        server_url="https://api.example.com",
        api_key_auth="<YOUR_API_KEY_HERE>",
    ) as f_client:
        # Rest of application here...
```
<!-- End Resource Management [resource-management] -->

<!-- Start Debugging [debug] -->
## Debugging

You can setup your SDK to emit debug logs for SDK requests and responses.

You can pass your own logger class directly into your SDK.
```python
from flexprice import Flexprice
import logging

logging.basicConfig(level=logging.DEBUG)
s = Flexprice(server_url="https://example.com", debug_logger=logging.getLogger("flexprice"))
```
<!-- End Debugging [debug] -->

<!-- Placeholder for Future Speakeasy SDK Sections -->

# Development

## Maturity

This SDK is in beta, and there may be breaking changes between versions without a major version update. Therefore, we recommend pinning usage
to a specific package version. This way, you can install the same version each time without breaking changes unless you are intentionally
looking for the latest version.

## Contributions

While we value open-source contributions to this SDK, this library is generated programmatically. Any manual changes added to internal files will be overwritten on the next generation. 
We look forward to hearing your feedback. Feel free to open a PR or an issue with a proof of concept and we'll do our best to include it in a future release. 

### SDK Created by [Speakeasy](https://www.speakeasy.com/?utm_source=flexprice&utm_campaign=python)
