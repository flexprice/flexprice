module github.com/flexprice/go-sdk/examples

go 1.23

require (
	github.com/flexprice/go-sdk v1.0.12
	github.com/joho/godotenv v1.5.1
)

// Use local SDK for testing
replace github.com/flexprice/go-sdk => ../
