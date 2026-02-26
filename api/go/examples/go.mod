module flexprice-go-examples

go 1.22

require (
	github.com/flexprice/flexprice-go v0.0.0
	github.com/joho/godotenv v1.5.1
)

// When running from api/go/examples, use the parent SDK
replace github.com/flexprice/flexprice-go => ../
