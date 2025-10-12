# Python SDK Custom Files

Place your custom files for the Python SDK here.

## Directory Structure

```
python/
├── flexprice/
│   ├── utils/           # Custom utilities and helpers
│   ├── extensions/      # Extended API methods
│   └── types/          # Additional type definitions
├── examples/           # Custom examples
├── docs/              # Custom documentation
└── config/            # Custom configuration files
```

## Example Usage

To add a custom utility file:

1. Create `api/custom/python/flexprice/utils/helpers.py`
2. Run `make generate-python-sdk`
3. The file will be copied to `api/python/flexprice/utils/helpers.py`
