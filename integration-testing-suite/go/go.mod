module github.com/flexprice/flexprice/integration-testing-suite/go

go 1.23

require github.com/flexprice/go-sdk/v2 v2.0.16

require (
	github.com/spyzhov/ajson v0.8.0 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
)

replace github.com/flexprice/go-sdk/v2 => ../../api/go/
