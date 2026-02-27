module github.com/flexprice/flexprice/api/tests/go

go 1.22

require github.com/flexprice/flexprice-go v0.0.0

require github.com/stretchr/testify v1.11.1 // indirect

// Fetch SDK from GitHub repo (module path inside repo is flexprice-go). Replace target must be a version (vX.Y.Z or pseudo-version), not a branch like "main".
replace github.com/flexprice/flexprice-go => github.com/flexprice/go-sdk-temp v1.0.62-0.20260226212919-b7874fb9828d
