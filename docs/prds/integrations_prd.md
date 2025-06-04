# PRD: Integrations Management System

## 1. Introduction

This document outlines the requirements for an Integrations Management System. The goal is to provide a standardized way to define, manage, and utilize integrations with external services or internal modules within the Flexprice platform. This system will allow linking integrations to core entities such as Customers, Products, and Subscriptions, and will provide a clear way to understand and leverage the capabilities of each integration.

## 2. Goals

- Define a clear concept for `Connection` (or `Provider`) and `Integration` entities.
- Enable linking `Integration` instances to `Customer`, `Product`, `Subscription`, and potentially other entities.
- Provide mechanisms to manage integrations (Create, Read, Update, Delete).
- Clearly define and utilize `IntegrationCapability` and `IntegrationGateway` concepts.
- Standardize how the platform interacts with various integrated services.

## 3. Proposed Solution

We will introduce the following key entities:

### 3.1. Connection / Provider

This entity represents the type of external service or system we are integrating with. It defines the "what" and "how" of the connection at a high level.

- **Attributes:**
  - `connection_code` (string, unique): A machine-readable identifier for the connection type (e.g., "stripe", "xero", "internal_inventory_system").
  - `display_name` (string): A human-readable name for the connection (e.g., "Stripe Payments", "Xero Accounting", "Internal Inventory").
  - `description` (text, optional): A brief description of the connection provider and its purpose.
  - `logo_url` (string, optional): URL to a logo for the provider.
  - `status` (enum: "active", "beta", "deprecated"): Current status of the provider.

### 3.2. Integration

This entity represents a specific configured instance of a `Connection/Provider`. For example, a specific Stripe account connected to the platform.

- **Attributes:**
  - `integration_id` (UUID, primary key): Unique identifier for the integration instance.
  - `connection_code` (string, foreign key to `Connection.connection_code`): Specifies the type of connection this integration uses.
  - `name` (string): A user-defined name for this specific integration instance (e.g., "Primary Stripe Account", "EU Region Xero").
  - `credentials` (encrypted JSON/text): Securely stored credentials or configuration required to connect to the external service (e.g., API keys, OAuth tokens). Structure will vary by `connection_code`.
  - `status` (enum: "active", "inactive", "error", "pending_configuration"): Current status of this integration instance.
  - `created_at` (timestamp): Timestamp of creation.
  - `updated_at` (timestamp): Timestamp of last update.
  - `metadata` (JSON, optional): Additional non-sensitive configuration or data.

### 3.3. Linking Integrations to Other Entities

Integrations need to be associated with various entities within the system. This allows context-specific use of integrations. For example, a specific customer might have their data synced via a particular integration instance.

We can achieve this through a polymorphic association or dedicated join tables. A polymorphic approach might be more flexible.

**Option 1: Polymorphic Association on `Integration` (or a linking table)**

Create a table like `entity_integrations`:

- `integration_id` (foreign key to `Integration.integration_id`)
- `entity_id` (string or UUID): ID of the linked entity (e.g., customer_id, product_id).
- `entity_type` (string): Type of the linked entity (e.g., "customer", "product", "subscription").
- `purpose` (string, optional): Describes why this integration is linked to this entity (e.g., "payment_processing", "data_sync", "notification_service").
- `is_default` (boolean, optional): Indicates if this is the default integration of this type for the entity.

**Option 2: Dedicated Join Tables (less flexible for new entities)**

- `customer_integrations` (customer_id, integration_id, purpose, ...)
- `product_integrations` (product_id, integration_id, purpose, ...)
- `subscription_integrations` (subscription_id, integration_id, purpose, ...)

**Recommendation:** Opt for Polymorphic Association for greater flexibility.

### 3.4. Integration Capability (`IntegrationCapability`)

This entity defines what a specific _type_ of integration (i.e., a `Connection/Provider`) can do. Capabilities are predefined and associated with `Connection` types.

- **Attributes:**

  - `capability_code` (string, unique): A machine-readable identifier for the capability (e.g., "PROCESS_PAYMENT", "SYNC_CUSTOMER_DATA", "SEND_INVOICE").
  - `connection_code` (string, foreign key to `Connection.connection_code`): The connection type this capability belongs to.
  - `description` (text): What this capability entails.
  - `parameters_schema` (JSON, optional): A JSON schema defining any parameters required to execute this capability.

- **Example Capabilities for a "Stripe" `Connection`:**
  - `PROCESS_PAYMENT`
  - `REFUND_PAYMENT`
  - `CREATE_CUSTOMER_PROFILE`
  - `SYNC_TRANSACTIONS`

### 3.5. Integration Gateway (`IntegrationGateway`)

The `IntegrationGateway` is the actual implementation layer that interacts with the external service based on an `Integration`'s configuration and a specific `IntegrationCapability`. It acts as an abstraction layer.

- **Concept:** For each `Connection/Provider` (e.g., Stripe), there will be a corresponding gateway implementation (e.g., `StripeGateway`).
- **Functionality:**

  - Takes an `integration_id` (to fetch credentials and configuration) and a `capability_code` (and its parameters).
  - Executes the specific action defined by the capability using the integration's credentials.
  - Handles API calls, data transformation, error handling, and logging specific to that external service.

- **Example Usage:**
  ```
  // Hypothetical internal service code
  paymentService.charge(customerId, amount, { integrationId: "stripe_integration_abc", capability: "PROCESS_PAYMENT" })
  ```
  Internally, this would resolve to:
  1. Fetch `Integration` with `integration_id="stripe_integration_abc"`.
  2. Get `connection_code="stripe"` from the integration.
  3. Identify the `StripeGateway`.
  4. Call `stripeGateway.executeCapability("PROCESS_PAYMENT", integration.credentials, { amount: ..., currency: ..., customer: ... })`.

## 4. Data Model (Conceptual ERD)

```
+---------------------+      +-----------------------+      +--------------------------+
| Connection/Provider |----->| IntegrationCapability |      | Integration              |
|---------------------| 1   *|-----------------------|      |--------------------------|
| connection_code (PK)|      | capability_code (PK)  |<-----| integration_id (PK)      |
| display_name        |      | connection_code (FK)  |      | connection_code (FK)     |
| description         |      | description           |      | name                     |
| logo_url            |      | parameters_schema     |      | credentials (encrypted)  |
| status              |      +-----------------------+      | status                   |
+---------------------+                                     | created_at               |
        ^                                                   | updated_at               |
        |                                                   | metadata                 |
        |                                                   +--------------------------+
        |                                                            |
        |                                                            | 1
        |                                                            |
        +------------------------------------------------------------+
                                                                     | *
                                                      +--------------------------+
                                                      | EntityIntegrationLink    |
                                                      |--------------------------|
                                                      | integration_id (FK)      |
                                                      | entity_id                |
                                                      | entity_type              |
                                                      | purpose                  |
                                                      | is_default               |
                                                      +--------------------------+
                                                            /|\
                                                             | (Polymorphic: Customer, Product, Subscription, etc.)
```

_(Note: `IntegrationGateway` is a code concept, not typically a direct DB entity, but it uses data from `Integration` and `IntegrationCapability`)_

## 5. API Design / Endpoints

The following API endpoints will be required to manage and utilize integrations.

### 5.1. Connection/Provider Management (Admin/Internal)

- **`POST /internal/connections`**: Create a new connection provider.
  - Request Body: `{ connection_code, display_name, description, logo_url, status }`
- **`GET /internal/connections`**: List all available connection providers.
- **`GET /internal/connections/{connection_code}`**: Get details of a specific connection provider.
- **`PUT /internal/connections/{connection_code}`**: Update a connection provider.
- **`DELETE /internal/connections/{connection_code}`**: Delete a connection provider (soft delete recommended).

- **`POST /internal/connections/{connection_code}/capabilities`**: Add a capability to a connection provider.
  - Request Body: `{ capability_code, description, parameters_schema }`
- **`GET /internal/connections/{connection_code}/capabilities`**: List capabilities of a connection provider.
- **`GET /internal/connections/{connection_code}/capabilities/{capability_code}`**: Get details of a specific capability.
- **`PUT /internal/connections/{connection_code}/capabilities/{capability_code}`**: Update a capability.
- **`DELETE /internal/connections/{connection_code}/capabilities/{capability_code}`**: Remove a capability.

### 5.2. Integration Instance Management (Tenant/User Facing)

- **`POST /integrations`**: Create/configure a new integration instance for the current tenant/user.
  - Request Body: `{ connection_code, name, credentials, metadata }`
  - Response: Full `Integration` object.
- **`GET /integrations`**: List all integration instances for the current tenant/user.
  - Query Params: `connection_code` (optional filter), `status` (optional filter).
- **`GET /integrations/{integration_id}`**: Get details of a specific integration instance.
- **`PUT /integrations/{integration_id}`**: Update an integration instance (e.g., name, credentials, status, metadata).
- **`DELETE /integrations/{integration_id}`**: Delete an integration instance.

### 5.3. Linking Integrations to Entities

Endpoints for managing links between integrations and other entities (e.g., Customer, Product).

- **`POST /entities/{entity_type}/{entity_id}/integrations`**: Link an integration to an entity.
  - Request Body: `{ integration_id, purpose, is_default }`
- **`GET /entities/{entity_type}/{entity_id}/integrations`**: List integrations linked to an entity.
- **`DELETE /entities/{entity_type}/{entity_id}/integrations/{integration_id}`**: Unlink an integration from an entity.

### 5.4. Utilizing Integrations (Internal Service APIs)

These are not direct REST APIs but represent how internal services would leverage the system.

- **`GET /integrations/{integration_id}/capabilities`**: (Potentially useful) List available capabilities for a _specific configured integration instance_. This could be derived from its `connection_code`'s capabilities.
- **`POST /integrations/{integration_id}/execute`**: (Conceptual) An endpoint or service call to execute a capability.
  - Request Body: `{ capability_code, parameters }`
  - The `IntegrationGateway` for the integration's `connection_code` would handle this.

## 6. Use Cases / User Stories

- **Admin:** As an Admin, I want to define a new "Salesforce" connection provider so that tenants can integrate their Salesforce accounts.
- **Admin:** As an Admin, I want to add "SYNC_CONTACTS" and "SYNC_OPPORTUNITIES" capabilities to the "Salesforce" provider.
- **Tenant:** As a Tenant, I want to connect my Stripe account to Flexprice by providing my API keys, so I can process payments.
- **Tenant:** As a Tenant, I want to link my "Primary Stripe Account" integration to all new subscriptions by default for payment processing.
- **Tenant:** As a Tenant, I want to see a list of all my active integrations and their statuses.
- **System:** When a new subscription is created, the system should use the default payment integration linked to the customer (or a platform default) to set up recurring billing.
- **System:** When a user initiates a "Sync Data with Xero" action, the system uses the user's configured Xero integration and the "SYNC_INVOICES" capability via the Xero gateway.

## 7. How to use `IntegrationCapability` and `IntegrationGateway`

- **`IntegrationCapability`** acts as a contract. It defines _what_ can be done through a certain type of connection (e.g., `Connection.connection_code = "stripe"` can `PROCESS_PAYMENT`). These are usually predefined when a `Connection` type is developed.
- When a user configures an `Integration` (e.g., their specific Stripe account), that instance inherits the capabilities of its parent `Connection` type.
- **`IntegrationGateway`** is the service/module in code (e.g., `StripeGateway`, `XeroGateway`) that knows _how_ to perform those capabilities.
  - It receives an `integration_id` (to load the specific `Integration` entity with its credentials) and a `capability_code` (with any necessary parameters).
  - The gateway then makes the necessary API calls to the external service, handles data mapping, error handling, etc.

**Example Flow: Processing a Payment**

1.  A service (e.g., Billing Service) needs to process a payment for a customer.
2.  It determines which `Integration` instance to use (e.g., customer's preferred payment method, or a default). Let's say `integration_id = "integ_123"` which is a Stripe integration.
3.  The Billing Service calls a central `IntegrationExecutionService` (or directly the relevant gateway if known):
    `IntegrationExecutionService.execute(integration_id="integ_123", capability_code="PROCESS_PAYMENT", parameters={amount: 100.00, currency: "USD", customer_id: "cust_abc"})`
4.  The `IntegrationExecutionService` performs the following:
    a. Loads `Integration` with `id="integ_123"`. Gets `connection_code="stripe"` and `credentials`.
    b. Loads `IntegrationCapability` for `connection_code="stripe"` and `capability_code="PROCESS_PAYMENT"` (mainly for validation or schema if needed, but the core logic is in the gateway).
    c. Resolves the appropriate gateway: `StripeGateway`.
    d. Calls `stripeGateway.processPayment(credentials, parameters)`.
5.  `StripeGateway` uses the provided credentials and parameters to make an API call to Stripe.
6.  The result (success/failure, transaction ID) is returned up the chain.

## 8. Storing the Integration Entity in DB

The `Integration` entity, as defined in section 3.2, will be stored in a dedicated database table (e.g., `integrations`).

- `integration_id`: Primary Key.
- `connection_code`: Foreign key to the `connections` table (or `connection_providers` table).
- `name`: User-friendly name for the instance.
- `credentials`: This field needs to be encrypted at rest. The actual structure of the decrypted data will depend on the `connection_code`. For example, Stripe might need `{ api_key: "sk_...", publishable_key: "pk_..." }`, while an OAuth-based integration might store `{ access_token: "...", refresh_token: "...", expires_at: "..." }`. A robust encryption mechanism is crucial.
- `status`: Tracks the health and usability of the integration.
- `metadata`: Can store non-sensitive settings, like "Default region for S3 bucket" if the integration is for S3.

## 9. Open Questions / Future Considerations

- **UI/UX for managing integrations:** Detailed mockups will be needed.
- **Error handling and retry mechanisms:** How are transient vs. permanent errors from external services handled?
- **Monitoring and alerting:** How to monitor the health of integrations.
- **Security review:** For credential storage and handling.
- **Versioning of capabilities or gateways:** How to handle changes in external APIs.
- **Dynamic loading of gateway implementations:** (Advanced) Could gateways be plugins?
- **Granular permissions:** Who can configure/manage which integrations or use specific capabilities?

This PRD provides a foundational plan for the Integrations Management System.
