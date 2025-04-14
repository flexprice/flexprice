#!/bin/bash
set -e

# Script to publish the Go SDK to the SDK repository
# This can be run manually to push SDK changes

# Configuration variables - modify as needed
SDK_REPO_URL="git@github.com:flexprice/go-sdk.git"
MAIN_REPO_PATH="$(git rev-parse --show-toplevel)"
SDK_CLONE_PATH="/tmp/flexprice-go"
VERSION="${1:-$(date +%Y%m%d%H%M%S)}"

# Read GitHub token from .secrets.git file
if [ -f "$MAIN_REPO_PATH/.secrets.git" ]; then
    GITHUB_TOKEN=$(cat "$MAIN_REPO_PATH/.secrets.git")
    # Convert SSH URL to HTTPS URL with token
    SDK_REPO_URL_WITH_TOKEN="https://x-access-token:${GITHUB_TOKEN}@github.com/flexprice/go-sdk.git"
    echo "GitHub token found, using authenticated HTTPS URL"
else
    echo "Warning: .secrets.git file not found, proceeding with SSH URL"
    SDK_REPO_URL_WITH_TOKEN="$SDK_REPO_URL"
fi

echo "Publishing SDK version: $VERSION"

# Function to handle errors
handle_error() {
    echo "Error: $1"
    exit 1
}

# Step 1: Clone the Go SDK repository
echo "Cloning SDK repository..."
if [ -d "$SDK_CLONE_PATH" ]; then
    echo "Cleaning existing SDK clone directory..."
    rm -rf "$SDK_CLONE_PATH"
fi

git clone "$SDK_REPO_URL_WITH_TOKEN" "$SDK_CLONE_PATH" || handle_error "Failed to clone repository"

# Step 2: Copy SDK files to the cloned repo
echo "Copying SDK files..."
rm -rf "$SDK_CLONE_PATH"/*
cp -r "$MAIN_REPO_PATH/api/go"/* "$SDK_CLONE_PATH" || handle_error "Failed to copy SDK files"

# Step 3: Copy the license file
echo "Copying license file..."
cp "$MAIN_REPO_PATH/LICENSE" "$SDK_CLONE_PATH" || echo "Warning: Failed to copy LICENSE file, but continuing"

# Step 4: Use custom README instead of the auto-generated one
echo "Setting up README..."
cat > "$SDK_CLONE_PATH/README.md" << 'EOF'
# Flexprice Go SDK

The official Go client library for Flexprice API

## Installation

```bash
go get github.com/flexprice/go-sdk
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/flexprice/go-sdk"
)

func main() {
	// Initialize client with API key
	client := flexprice.NewClient(os.Getenv("FLEXPRICE_API_KEY"))

	// For example, to get customer details
	customer, err := client.Customers.Get(context.Background(), "customer_id")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Customer: %+v\n", customer)
}
```
EOF

# Step 5: Optionally append auto-generated API documentation to the custom README
if [ -f "$MAIN_REPO_PATH/api/go/README.md" ]; then
    echo "Appending auto-generated API documentation..."
    echo "" >> "$SDK_CLONE_PATH/README.md"
    echo "## API Documentation" >> "$SDK_CLONE_PATH/README.md"
    echo "" >> "$SDK_CLONE_PATH/README.md"
    cat "$MAIN_REPO_PATH/api/go/README.md" >> "$SDK_CLONE_PATH/README.md"
else
    echo "Warning: Auto-generated README not found, skipping API documentation append"
fi

# Step 6: Commit and push changes
echo "Committing and pushing changes..."
cd "$SDK_CLONE_PATH" || handle_error "Failed to change to SDK repository directory"

git config user.name "Flexprice SDK Bot"
git config user.email "sdk@flexprice.dev"

# Configure remote URL with token for push operations before any git operations
if [ -n "$GITHUB_TOKEN" ]; then
    echo "Setting remote URL with authentication token for push..."
    git remote set-url origin "$SDK_REPO_URL_WITH_TOKEN"
fi

# Add all files to git
git add -A

# Check if there are changes to commit
if git diff-index --quiet HEAD --; then
    echo "No changes to commit"
else
    # Commit changes
    git commit -m "Update SDK to version $VERSION"
    echo "Changes committed successfully"
fi

# Create tag (force if it already exists)
if git tag -a "v$VERSION" -m "Version $VERSION" 2>/dev/null; then
    echo "Tag v$VERSION created"
else
    echo "Tag already exists, recreating..."
    git tag -d "v$VERSION" 2>/dev/null || true
    git tag -a "v$VERSION" -m "Version $VERSION"
fi

# Prompt for push confirmation
read -r -p "Ready to push SDK changes to the repository. Continue? (y/n): " CONFIRM
if [[ $CONFIRM =~ ^[Yy]$ ]]; then
    echo "Pushing changes to SDK repository..."
    
    # Push with verbose output for debugging
    git push -v origin main || handle_error "Failed to push to main branch"
    git push -v origin "v$VERSION" || handle_error "Failed to push tag"
    
    echo "✅ SDK published successfully!"
else
    echo "Push cancelled. Changes are committed locally at $SDK_CLONE_PATH"
fi

echo "Done!" 