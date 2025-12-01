#!/bin/bash
# Test script for Benthos Flexprice integration

set -e

echo "🚀 Benthos Flexprice Test"
echo ""

# Check if benthos is installed
if ! command -v benthos &> /dev/null; then
    echo "Installing Benthos..."
    go install github.com/benthosdev/benthos/v4/cmd/benthos@latest
    echo "✅ Benthos installed"
else
    echo "✅ Benthos found"
fi

echo ""
echo "Available tests:"
echo "  1. Single events (100 events, 1 per request)"
echo "  2. Bulk events (1000 events, 100 per batch)"
echo ""

# Check environment variables
if [ -z "$FLEXPRICE_API_KEY" ]; then
    echo "⚠️  Warning: FLEXPRICE_API_KEY not set, using default 'test_key'"
    export FLEXPRICE_API_KEY="test_key"
fi

if [ -z "$FLEXPRICE_BASE_URL" ]; then
    echo "⚠️  Warning: FLEXPRICE_BASE_URL not set, using default 'http://localhost:8080'"
    export FLEXPRICE_BASE_URL="http://localhost:8080"
fi

echo ""
echo "Configuration:"
echo "  API URL: $FLEXPRICE_BASE_URL"
echo "  API Key: ${FLEXPRICE_API_KEY:0:10}..."
echo ""

# Run based on argument
case "${1:-single}" in
  single)
    echo "Running single event test..."
    benthos -c config.yaml
    ;;
  bulk)
    echo "Running bulk event test..."
    benthos -c config-bulk.yaml
    ;;
  *)
    echo "Usage: $0 [single|bulk]"
    exit 1
    ;;
esac

echo ""
echo "✅ Test completed!"
