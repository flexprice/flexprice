# FlexPrice API SDKs

This directory contains the generated SDKs for the Flexprice API. The SDKs are generated from the OpenAPI specification in `/docs/swagger/swagger-3-0.json`.

## SDK Generation

The SDKs are generated using:
- **Go SDK**: Speakeasy SDK Generator
- **Python SDK**: OpenAPI Generator  
- **JavaScript SDK**: OpenAPI Generator

To generate the SDKs, run:

```bash
make generate-sdk
```

This will generate the following SDKs:

- Go SDK in `api/go/` (using Speakeasy)
- Python SDK in `api/python/` (using OpenAPI Generator)
- JavaScript SDK in `api/javascript/` (using OpenAPI Generator)

## Test Files

Each SDK includes test files in a `test` directory, which serve as:

1. Functional tests for the SDK
2. Examples of how to use the SDK

The test files are preserved when regenerating the SDKs through the GitHub Actions workflow, which backs up test files before generation and restores them afterward.

## SDK Publishing

The SDKs are published to their respective repositories:

- Go SDK: [github.com/flexprice/go-sdk](https://github.com/flexprice/go-sdk)
- Python SDK: [github.com/flexprice/python-sdk](https://github.com/flexprice/python-sdk)
- JavaScript SDK: [github.com/flexprice/javascript-sdk](https://github.com/flexprice/javascript-sdk)

### Publishing Process

#### Go SDK (Speakeasy)
1. The Go SDK is generated from the OpenAPI specification using Speakeasy
2. The SDK is automatically built, tested, and validated
3. Files are pushed to the dedicated [go-sdk repository](https://github.com/flexprice/go-sdk)
4. Version tags are created for Go module versioning

#### Python & JavaScript SDKs (OpenAPI Generator)
1. SDKs are generated from the OpenAPI specification
2. Test files are preserved during generation
3. Dependencies are updated and tests are run
4. SDKs are published to PyPI (Python) and npm (JavaScript)

The publishing process is automated via GitHub Actions and can be triggered by:

- Manually running the `sdk-publish.yml` workflow with a version number
- Using the dedicated `sdk_publish.yaml` workflow for Go SDK publishing
- Using the `sdk_generation.yaml` workflow for automated SDK regeneration

## Local Development

When developing locally, it's recommended to:

1. Generate the SDKs using `make generate-sdk`
2. Run tests to ensure functionality
3. Test your changes locally before pushing

To avoid committing generated code to the main repository, the SDK directories are included in `.gitignore` but exclude the test directories.

## Available SDKs

- **Go**: `api/go`
- **Python**: `api/python`
- **JavaScript**: `api/javascript`

## Generating SDKs

The SDKs are generated from the OpenAPI 3.0 specification located at `docs/swagger/swagger-3-0.json`.

- **Go SDK**: Generated using Speakeasy SDK Generator for modern, type-safe Go code
- **Python & JavaScript SDKs**: Generated using OpenAPI Generator CLI

To generate all SDKs, run:

```bash
make generate-sdk
```

To generate a specific SDK, run one of the following commands:

```bash
make generate-go-sdk          # Generates Go SDK with Speakeasy
make generate-go-sdk-legacy   # Legacy OpenAPI Generator version (backup)
make generate-python-sdk      # Generates Python SDK
make generate-javascript-sdk  # Generates JavaScript SDK
```

## SDK Usage

### Go SDK

```go
import (
    "context"
    flexprice "github.com/your-org/flexprice/api/go"
)

func main() {
    cfg := flexprice.NewConfiguration()
    cfg.Host = "your-api-host"
    client := flexprice.NewAPIClient(cfg)
    
    // Use the client to make API calls
    // ...
}
```

### Python SDK

```python
import flexprice
from flexprice.api_client import ApiClient
from flexprice.configuration import Configuration

# Configure API client
configuration = Configuration(host="your-api-host")
api_client = ApiClient(configuration)

# Use the client to make API calls
# ...
```

### JavaScript SDK

```javascript
import * as flexprice from 'flexprice';

// Configure API client
const apiClient = new flexprice.ApiClient("your-api-host");

// Use the client to make API calls
// ...
```

## Customization

### Go SDK (Speakeasy)
The Go SDK generation can be customized by modifying:
- `.speakeasy/gen.yaml` - SDK generation configuration
- `.speakeasy/workflow.yaml` - Workflow and target configuration

### Python & JavaScript SDKs (OpenAPI Generator)
The SDK generation process can be customized by modifying the OpenAPI Generator configuration in the Makefile.

## CI/CD Integration

The SDKs are automatically generated and published as part of the CI/CD pipeline:

### Workflows
- **`.github/workflows/sdk-publish.yml`**: Main workflow for generating and publishing all SDKs
- **`.github/workflows/sdk_generation.yaml`**: Automated SDK regeneration workflow (scheduled/on-demand)
- **`.github/workflows/sdk_publish.yaml`**: Dedicated Go SDK publishing workflow using Speakeasy

### Go SDK CI/CD
The Go SDK uses Speakeasy's automated workflow which:
1. Validates the OpenAPI specification
2. Generates the SDK with type-safe Go code
3. Compiles and tests the generated code
4. Publishes to the go-sdk repository with version tags

### Required Secrets
- `SPEAKEASY_API_KEY`: API key for Speakeasy SDK generation
- `SDK_DEPLOY_GIT_TOKEN`: GitHub token for pushing to SDK repositories
- `NPM_AUTH_TOKEN`: Token for publishing to npm
- `PYPI_API_TOKEN`: Token for publishing to PyPI

## Publishing SDKs

The SDKs can be published using the automated GitHub Actions workflows or manually.

### Automated Publishing (Recommended)

Use the GitHub Actions workflow:
```bash
# Navigate to Actions tab in GitHub
# Run "Generate and Publish SDK Packages" workflow
# Provide version number (e.g., 1.0.0)
# Optionally enable dry_run for testing
```

### JavaScript SDK

The JavaScript SDK is published to npm as the `flexprice` package. To install it:

```bash
npm install flexprice
```

### Python SDK

The Python SDK is published to PyPI as the `flexprice` package. To install it:

```bash
pip install flexprice
```

### Go SDK

The Go SDK is published as a Go module on GitHub using Speakeasy. To use it in your Go project:

```go
import "github.com/flexprice/go-sdk"
```

And add it to your dependencies:

```bash
go get github.com/flexprice/go-sdk
```

### Manual Publishing

If you need to publish the SDKs manually, follow these steps:

#### JavaScript SDK (npm)

```bash
cd api/javascript
# Update version in package.json if needed
npm publish --access public
```

#### Python SDK (PyPI)

```bash
cd api/python
pip install build twine
python -m build
python -m twine upload dist/*
```

#### Go SDK (GitHub with Speakeasy)

The Go SDK publishing is automated through Speakeasy and GitHub Actions. Manual publishing is not recommended but can be done using:

```bash
# Generate SDK first
make generate-go-sdk

# The workflow will automatically:
# 1. Clone the go-sdk repository
# 2. Copy generated files
# 3. Commit and tag the version
# 4. Push to GitHub

# For manual override, see .github/workflows/sdk-publish.yml
```

## Required Secrets for CI/CD

To enable automatic publishing in the CI/CD workflow, you need to set up the following secrets in your GitHub repository:

- `SPEAKEASY_API_KEY`: Speakeasy API key for Go SDK generation
- `SDK_DEPLOY_GIT_TOKEN`: GitHub token with write access to SDK repositories
- `NPM_AUTH_TOKEN`: Access token for publishing to npm
- `PYPI_API_TOKEN`: PyPI API token for publishing Python packages

For more information on setting up these secrets, refer to:
- [Speakeasy: Authentication](https://www.speakeasy.com/docs/authentication)
- [npm: Creating and viewing access tokens](https://docs.npmjs.com/creating-and-viewing-access-tokens)
- [PyPI: Creating API tokens](https://pypi.org/help/#apitoken) 