# Customer Address and Metadata Enhancement

## Overview
This PRD outlines the changes required to add address information and metadata fields to the customer schema. This enhancement will allow storing structured address data and additional metadata for customers.

## Schema Changes

### New Fields
1. Address Fields (all nullable):
   - address_city (string): City, district, suburb, town, or village
   - address_country (string): Two-letter country code (ISO 3166-1 alpha-2)
   - address_line1 (string): Address line 1 (street, PO Box, company name)
   - address_line2 (string): Address line 2 (apartment, suite, unit, building)
   - address_postal_code (string): ZIP or postal code
   - address_state (string): State, county, province, or region

2. Metadata:
   - Type: map[string]string
   - Purpose: Store additional customer-related data

## Required Changes

### 1. Schema Changes
- Update `ent/schema/customer.go`:
  - Add individual address fields as separate columns
  - Add metadata field as a JSON field
  - Add appropriate indexes for address fields
  - Add schema type definitions for varchar fields

### 2. Domain Model Changes
- Update `internal/domain/customer/model.go`:
  - Add address fields directly to Customer struct
  - Add metadata field to Customer struct
  - Update FromEnt and ToEnt methods
  - Add address-related validation functions

### 3. DTO Changes
- Update `internal/api/dto/customer.go`:
  - Add address fields to CreateCustomerRequest
  - Add address fields to UpdateCustomerRequest
  - Add address fields to CustomerResponse
  - Update validation rules
  - Add address-specific validation functions

### 4. Repository Changes
- Update `internal/repository/ent/customer.go`:
  - Update Create method to handle individual address fields
  - Update Update method to handle address fields
  - Add query methods for address fields
  - Add address-related filtering capabilities

### 5. Service Changes
- Update `internal/service/customer.go`:
  - Update CreateCustomer to handle address fields
  - Update UpdateCustomer to handle address fields
  - Add address validation logic
  - Add address-related query methods

### 6. API Handler Changes
- Update `internal/api/v1/customer.go`:
  - Update request/response handling for address fields
  - Update API documentation
  - Add address-related query parameter handling

### 7. Test Changes
- Update `internal/service/customer_test.go`:
  - Add test cases for individual address fields
  - Add address validation test cases
  - Update existing test cases

- Update `internal/testutil/inmemory_customer_store.go`:
  - Update copyCustomer to handle address fields
  - Update test helper functions
  - Add address-related filtering support

## API Changes

### Request/Response Format Changes

#### Create Customer Request
```json
{
  "external_id": "string",
  "name": "string",
  "email": "string",
  "address_city": "string",
  "address_country": "string",
  "address_line1": "string",
  "address_line2": "string",
  "address_postal_code": "string",
  "address_state": "string",
  "metadata": {
    "key1": "value1",
    "key2": "value2"
  }
}
```

#### Update Customer Request
```json
{
  "external_id": "string",
  "name": "string",
  "email": "string",
  "address_city": "string",
  "address_country": "string",
  "address_line1": "string",
  "address_line2": "string",
  "address_postal_code": "string",
  "address_state": "string",
  "metadata": {
    "key1": "value1",
    "key2": "value2"
  }
}
```

## Implementation Plan

1. Schema and Model Changes
   - Implement Ent schema changes with separate address columns
   - Add appropriate indexes for address fields
   - Update domain model with individual fields

2. Data Layer Changes
   - Update repository implementation for separate fields
   - Add address-related query capabilities
   - Update test store implementation

3. Business Logic Changes
   - Update service layer with address validation
   - Update DTOs with individual fields
   - Implement address-specific validation

4. API Changes
   - Update handlers for separate address fields
   - Add address-related query parameters
   - Update API documentation

5. Testing
   - Add new test cases for address fields
   - Add validation test cases
   - Update existing tests
   - Manual testing

## Validation Rules

1. Address Fields:
   - address_country: Must be valid ISO 3166-1 alpha-2 code when provided
   - address_postal_code: Format validation based on country (optional)
   - All address fields are optional
   - Maximum length constraints:
     - address_line1, address_line2: 255 characters
     - address_city, address_state: 100 characters
     - address_country: 2 characters
     - address_postal_code: 20 characters

2. Metadata:
   - Keys must be non-empty strings
   - Values must be strings
   - Maximum map size (TBD based on requirements)

## Migration Considerations

1. Database Migration:
   - New address columns will be added as nullable
   - No data migration needed for existing records
   - Address fields will be null for existing records

2. API Compatibility:
   - Changes are backward compatible
   - Existing API clients will continue to work
   - New fields are optional in requests

## Testing Requirements

1. Unit Tests:
   - CRUD operations with address fields
   - Address field validation
   - Metadata operations
   - Edge cases (null fields, updates)
   - Country code validation

2. Integration Tests:
   - API endpoints with new fields
   - Database persistence
   - Query operations
   - Address field filtering

## Documentation Updates

1. API Documentation:
   - Update OpenAPI/Swagger specs
   - Update API examples
   - Update field descriptions
   - Document address field constraints

2. Internal Documentation:
   - Update code comments
   - Update README if needed
   - Document address validation rules

## Future Considerations

1. Address Validation:
   - Integration with address validation services
   - Country-specific format validation
   - Postal code format validation per country

2. Metadata:
   - Potential indexing of specific metadata fields
   - Search capabilities in metadata

3. Query Capabilities:
   - Filtering by individual address fields
   - Address-based geographic queries
   - Combined address field searches

4. Performance:
   - Indexing strategies for address fields
   - Query optimization for address searches
   - Caching strategies for frequently accessed addresses 