# Flexprice Go Example: Customer Portal Usage

This example demonstrates using the Flexprice Go SDK to record usage and fetch a simple cost summary for a customer portal.

Setup
1. Ensure Go >= 1.22 is installed
2. Copy .env.example to .env and fill values
3. Install deps and run

```
go mod tidy
# Portal example
go run ./cmd/portal
# Recurring credits example
go run ./cmd/credits
```

Files
- cmd/portal/main.go: Example CLI simulating two usage events and a summary fetch
- cmd/credits/main.go: Simulates recurring credits (grant + consumption) and computes remaining balance

Notes
- Replace placeholder values with your actual Flexprice credentials
