module github.com/flexprice/go-sdk/v2/examples

go 1.22

require (
	github.com/flexprice/go-sdk/v2 v2.0.0
	github.com/joho/godotenv v1.5.1
)

// After merge-custom, run from api/go/examples; parent is the SDK root.
replace github.com/flexprice/go-sdk/v2 => ../
