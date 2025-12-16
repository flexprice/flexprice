#!/bin/bash
# Fix pylint configuration in Python SDK
# This adds pylint configuration to ignore redefined-builtin warnings
# Can be run before or after Speakeasy generation

PYTHON_SDK_DIR="api/python"
PYPROJECT_FILE="$PYTHON_SDK_DIR/pyproject.toml"

# Create directory if it doesn't exist
mkdir -p "$PYTHON_SDK_DIR"

# If pyproject.toml doesn't exist yet, create a minimal one
if [ ! -f "$PYPROJECT_FILE" ]; then
    echo "⚠️  Warning: pyproject.toml not found, will be created by Speakeasy"
    # Don't exit - we'll add config after generation
    exit 0
fi

# Check if pylint config already exists
if grep -q "\[tool.pylint.messages_control\]" "$PYPROJECT_FILE"; then
    # Check if redefined-builtin is already in the disable list
    if grep -q "redefined-builtin" "$PYPROJECT_FILE"; then
        echo "✓ Pylint configuration already correct"
        exit 0
    else
        # Update existing config to include redefined-builtin
        sed -i.bak 's/disable = \[\(.*\)\]/disable = [\1, "redefined-builtin"]/' "$PYPROJECT_FILE" 2>/dev/null || \
        sed -i '' 's/disable = \[\(.*\)\]/disable = [\1, "redefined-builtin"]/' "$PYPROJECT_FILE" 2>/dev/null
        rm -f "$PYPROJECT_FILE.bak" 2>/dev/null
        echo "✅ Updated pylint configuration"
        exit 0
    fi
fi

# Add pylint configuration at the end
echo "" >> "$PYPROJECT_FILE"
echo "[tool.pylint.messages_control]" >> "$PYPROJECT_FILE"
echo "disable = [\"redefined-builtin\"]" >> "$PYPROJECT_FILE"

echo "✅ Added pylint configuration to pyproject.toml"

