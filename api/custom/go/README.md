# Go SDK Custom Files

Place your custom files for the Go SDK here.

## Directory Structure

```
go/
├── helpers.go         # Custom utilities and helpers
├── extensions/        # Extended API methods
├── types/            # Additional type definitions
├── examples/         # Custom examples
├── docs/             # Custom documentation
└── config/           # Custom configuration files
```

## Example Usage

To add a custom utility file:

1. Create `api/custom/go/helpers.go`
2. Run `make generate-go-sdk`
3. The file will be copied to `api/go/helpers.go`
