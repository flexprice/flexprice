# Stripe Customer Synchronization Integration

This document explains how to set up Stripe customer synchronization for your FlexPrice tenant.

## Overview

The Stripe integration allows automatic bidirectional synchronization of customer data between your Stripe account and FlexPrice:

- **From FlexPrice to Stripe**: When you create a customer in FlexPrice, it can be automatically created in Stripe
- **From Stripe to FlexPrice**: When customers are created/updated in Stripe, they are automatically synced to FlexPrice via webhooks

## Prerequisites

1. A Stripe account with API access
2. FlexPrice tenant with appropriate environment configured
3. Access to your Stripe Dashboard webhook settings

## Setup Instructions

### Step 1: Configure Stripe Connection in FlexPrice

1. Navigate to **Settings > Integrations** in your FlexPrice dashboard
2. Click **Add Integration** and select **Stripe**
3. Provide the following information:
   - **Connection Name**: A descriptive name (e.g., "Production Stripe")
   - **Stripe Secret Key**: Your Stripe secret key (sk_...)
   - **Stripe Publishable Key**: Your Stripe publishable key (pk_...)
   - **Webhook Secret**: Will be generated in Step 2

### Step 2: Configure Webhook in Stripe Dashboard

1. Log into your [Stripe Dashboard](https://dashboard.stripe.com)
2. Navigate to **Developers > Webhooks**
3. Click **Add endpoint**
4. Configure the webhook:
   - **Endpoint URL**: `https://your-flexprice-domain.com/v1/webhooks/stripe/{tenant_id}/{environment_id}`
     - Replace `{tenant_id}` with your FlexPrice tenant ID
     - Replace `{environment_id}` with your FlexPrice environment ID
   - **Events to send**: Select the following events:
     - `customer.created`
     - `customer.updated`
     - `customer.deleted`
5. Click **Add endpoint**
6. Copy the **Signing secret** (whsec_...) and add it to your FlexPrice Stripe connection

### Step 3: Test the Integration

1. Create a test customer in Stripe Dashboard
2. Verify the customer appears in your FlexPrice customer list
3. Create a customer in FlexPrice and verify it appears in Stripe

## Webhook URL Format

The webhook URL follows this pattern:
```
POST /v1/webhooks/stripe/{tenant_id}/{environment_id}
```

### URL Parameters

- `tenant_id`: Your FlexPrice tenant identifier
- `environment_id`: Your FlexPrice environment identifier (e.g., production, staging)

### Example
```
https://api.flexprice.com/v1/webhooks/stripe/tenant_123/env_prod
```

## Supported Events

The integration handles the following Stripe webhook events:

### customer.created
- Creates a new customer in FlexPrice when a customer is created in Stripe
- Maps Stripe customer data to FlexPrice customer fields
- Stores Stripe customer ID in FlexPrice customer metadata

### customer.updated
- Updates existing FlexPrice customer when Stripe customer is modified
- Syncs name, email, and address information
- Maintains metadata associations

### customer.deleted
- Logs customer deletion event
- Does not automatically delete FlexPrice customer (configurable)

## Data Mapping

### Stripe to FlexPrice

| Stripe Field | FlexPrice Field | Notes |
|--------------|-----------------|-------|
| `id` | `metadata.stripe_customer_id` | Stored as metadata |
| `name` | `name` | Customer display name |
| `email` | `email` | Customer email address |
| `address.line1` | `address_line1` | Address line 1 |
| `address.line2` | `address_line2` | Address line 2 |
| `address.city` | `address_city` | City |
| `address.state` | `address_state` | State/Province |
| `address.postal_code` | `address_postal_code` | ZIP/Postal code |
| `address.country` | `address_country` | Country (ISO 3166-1 alpha-2) |
| `metadata.flexprice_customer_id` | `external_id` | If provided |

### FlexPrice to Stripe

| FlexPrice Field | Stripe Field | Notes |
|-----------------|--------------|-------|
| `id` | `metadata.flexprice_customer_id` | Stored as metadata |
| `name` | `name` | Customer display name |
| `email` | `email` | Customer email address |
| `address_line1` | `address.line1` | Address line 1 |
| `address_line2` | `address.line2` | Address line 2 |
| `address_city` | `address.city` | City |
| `address_state` | `address.state` | State/Province |
| `address_postal_code` | `address.postal_code` | ZIP/Postal code |
| `address_country` | `address.country` | Country (ISO 3166-1 alpha-2) |
| `environment_id` | `metadata.flexprice_environment` | Environment identifier |

## Duplicate Prevention

The integration includes built-in duplicate prevention:

1. **Stripe ID Check**: Before creating a customer, checks if a customer with the same Stripe ID already exists
2. **External ID Check**: Uses FlexPrice customer ID from Stripe metadata to prevent duplicates
3. **Email Matching**: Optional email-based matching for additional safety

## Error Handling

### Common Issues

1. **Invalid Webhook Signature**
   - Ensure webhook secret is correctly configured
   - Verify endpoint URL is accessible

2. **Customer Creation Failures**
   - Check required fields are present
   - Verify address format (country codes must be ISO 3166-1 alpha-2)

3. **Permission Errors**
   - Ensure Stripe API keys have customer read/write permissions
   - Verify FlexPrice API access for the environment

### Monitoring

Monitor webhook delivery in your Stripe Dashboard:
1. Go to **Developers > Webhooks**
2. Click on your FlexPrice webhook endpoint
3. Review delivery attempts and responses

## Security Considerations

1. **Webhook Signature Verification**: All webhooks are verified using Stripe's signature verification
2. **HTTPS Required**: Webhook endpoints must use HTTPS
3. **API Key Security**: Store Stripe API keys securely, never expose in client-side code
4. **Environment Isolation**: Use separate Stripe accounts/keys for different environments

## API Usage

### Manually Sync Customer to Stripe

```bash
curl -X POST "https://api.flexprice.com/v1/customers/{customer_id}/sync-to-stripe" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json"
```

### Get Customer Stripe Information

```bash
curl -X GET "https://api.flexprice.com/v1/customers/{customer_id}" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

Check the `metadata.stripe_customer_id` field for the associated Stripe customer ID.

## Troubleshooting

### Webhook Not Receiving Events

1. Verify webhook URL is correct and accessible
2. Check Stripe webhook event selection
3. Ensure webhook secret matches configuration
4. Review Stripe webhook delivery logs

### Customer Sync Issues

1. Check customer data format and required fields
2. Verify API key permissions
3. Review FlexPrice logs for error details
4. Ensure environment IDs match

### Data Inconsistencies

1. Check for duplicate customers in both systems
2. Verify metadata associations
3. Review sync timestamps
4. Consider manual reconciliation if needed

## Support

For additional support with Stripe integration:

1. Check FlexPrice documentation
2. Review Stripe webhook documentation
3. Contact FlexPrice support with integration details
4. Provide webhook delivery logs for troubleshooting
