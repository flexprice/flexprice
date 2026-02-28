module github.com/flexprice/flexprice/api/tests/go

go 1.22

require github.com/flexprice/flexprice-go/v2 v2.0.0

// Use local SDK generated in api/go
replace github.com/flexprice/flexprice-go/v2 => ../../go
