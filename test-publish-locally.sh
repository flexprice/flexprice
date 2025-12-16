#!/bin/bash

# Local test script to simulate the sdk_publish.yaml workflow
# Usage: ./test-publish-locally.sh [version] [dry-run]
# Example: ./test-publish-locally.sh 1.0.0 true

set -e

VERSION=${1:-"1.0.0-test"}
DRY_RUN=${2:-"true"}

echo "=========================================="
echo "Testing SDK Publishing Workflow Locally"
echo "=========================================="
echo "Version: $VERSION"
echo "Dry Run: $DRY_RUN"
echo ""

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Step 1: Check prerequisites
echo -e "${YELLOW}Step 1: Checking prerequisites...${NC}"
if ! command -v speakeasy &> /dev/null; then
    echo -e "${RED}✗ Speakeasy not found. Installing...${NC}"
    brew install speakeasy-api/tap/speakeasy || {
        echo -e "${RED}Failed to install Speakeasy. Please install manually.${NC}"
        exit 1
    }
fi
echo -e "${GREEN}✓ Speakeasy found${NC}"

if ! command -v go &> /dev/null; then
    echo -e "${RED}✗ Go not found. Please install Go.${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Go found: $(go version)${NC}"

# Step 2: Check if Speakeasy is authenticated
echo ""
echo -e "${YELLOW}Step 2: Checking Speakeasy authentication...${NC}"
if [ -z "$SPEAKEASY_API_KEY" ]; then
    echo -e "${YELLOW}⚠ SPEAKEASY_API_KEY not set in environment${NC}"
    echo "You can set it with: export SPEAKEASY_API_KEY=your_key"
    echo "Or it will be prompted during speakeasy auth login"
    read -p "Do you want to authenticate now? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        speakeasy auth login
    fi
else
    echo -e "${GREEN}✓ SPEAKEASY_API_KEY is set${NC}"
    speakeasy auth login --api-key "$SPEAKEASY_API_KEY" || echo "Authentication check..."
fi

# Step 3: Ensure swagger is up to date
echo ""
echo -e "${YELLOW}Step 3: Checking OpenAPI specification...${NC}"
if [ ! -f "docs/swagger/swagger-3-0.json" ]; then
    echo -e "${YELLOW}⚠ swagger-3-0.json not found. Generating...${NC}"
    make swagger
fi
echo -e "${GREEN}✓ OpenAPI spec found${NC}"

# Step 4: Generate SDK with Speakeasy
echo ""
echo -e "${YELLOW}Step 4: Generating SDK with Speakeasy...${NC}"
if [ -n "$SPEAKEASY_API_KEY" ]; then
    export SPEAKEASY_API_KEY
fi

if speakeasy run --target flexprice-go-sdk; then
    echo -e "${GREEN}✓ SDK generated successfully${NC}"
else
    echo -e "${YELLOW}⚠ SDK generation had warnings (this is expected)${NC}"
    if [ ! -d "api/go" ]; then
        echo -e "${RED}✗ SDK directory not found. Generation failed.${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ SDK directory exists despite warnings${NC}"
fi

# Step 5: Verify SDK was generated
echo ""
echo -e "${YELLOW}Step 5: Verifying SDK files...${NC}"
if [ ! -d "api/go" ]; then
    echo -e "${RED}✗ api/go directory not found${NC}"
    exit 1
fi

echo "Generated files:"
ls -la api/go | head -20
echo -e "${GREEN}✓ SDK files exist${NC}"

# Step 6: Verify SDK compiles
echo ""
echo -e "${YELLOW}Step 6: Verifying SDK compiles...${NC}"
cd api/go
go mod tidy
if go build ./...; then
    echo -e "${GREEN}✓ SDK compiles successfully${NC}"
else
    echo -e "${RED}✗ SDK compilation failed${NC}"
    exit 1
fi
cd ../..

# Step 7: Dry run or actual publish
echo ""
if [ "$DRY_RUN" = "true" ]; then
    echo -e "${YELLOW}==========================================${NC}"
    echo -e "${YELLOW}=== DRY RUN MODE ===${NC}"
    echo -e "${YELLOW}==========================================${NC}"
    echo ""
    echo "SDK was generated successfully but NOT published."
    echo "Would publish version: $VERSION"
    echo "Repository: github.com/flexprice/go-sdk"
    echo ""
    echo "Generated files summary:"
    echo "  - Main SDK files: $(find api/go -name '*.go' -type f | wc -l | tr -d ' ') Go files"
    echo "  - README.md: $(test -f api/go/README.md && echo '✓' || echo '✗')"
    echo "  - go.mod: $(test -f api/go/go.mod && echo '✓' || echo '✗')"
    echo ""
    echo -e "${GREEN}✓ Dry run completed successfully!${NC}"
    echo ""
    echo "To actually publish, run:"
    echo "  ./test-publish-locally.sh $VERSION false"
    echo ""
    echo "Or push to GitHub and use the Actions workflow."
else
    echo -e "${YELLOW}Step 7: Publishing to GitHub (REAL MODE)...${NC}"
    echo ""
    echo -e "${RED}⚠ WARNING: This will actually publish to GitHub!${NC}"
    read -p "Are you sure you want to publish version $VERSION? (yes/no) " -r
    echo
    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        echo "Publishing cancelled."
        exit 0
    fi
    
    # Check for GitHub token
    if [ -z "$SDK_DEPLOY_GIT_TOKEN" ]; then
        echo -e "${RED}✗ SDK_DEPLOY_GIT_TOKEN not set${NC}"
        echo "Set it with: export SDK_DEPLOY_GIT_TOKEN=your_token"
        exit 1
    fi
    
    # Create temporary directory for testing
    TEST_REPO_DIR="/tmp/go-sdk-test-$$"
    echo "Creating test repository clone in: $TEST_REPO_DIR"
    
    # Clone the repository
    git clone "https://x-access-token:$SDK_DEPLOY_GIT_TOKEN@github.com/flexprice/go-sdk.git" "$TEST_REPO_DIR" || {
        echo -e "${RED}✗ Failed to clone repository${NC}"
        echo "Check your SDK_DEPLOY_GIT_TOKEN and repository access"
        exit 1
    }
    
    # Copy files
    echo "Copying SDK files..."
    rm -rf "$TEST_REPO_DIR"/* || true
    rm -rf "$TEST_REPO_DIR"/.* 2>/dev/null || true
    cp -r api/go/* "$TEST_REPO_DIR/" || echo "No files to copy"
    
    # Copy LICENSE if exists
    if [ -f "LICENSE" ]; then
        cp LICENSE "$TEST_REPO_DIR/" || echo "LICENSE copy failed"
    fi
    
    # Commit and tag
    cd "$TEST_REPO_DIR"
    git config user.name "Local Test"
    git config user.email "test@local"
    git add .
    
    if git diff --staged --quiet; then
        echo -e "${YELLOW}⚠ No changes to commit${NC}"
    else
        git commit -m "Update SDK to version $VERSION" || echo "Commit failed"
    fi
    
    # Check if tag exists
    if git rev-parse "v$VERSION" >/dev/null 2>&1; then
        echo -e "${YELLOW}⚠ Tag v$VERSION already exists${NC}"
        read -p "Delete and recreate tag? (y/n) " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            git tag -d "v$VERSION" || true
            git push origin ":refs/tags/v$VERSION" || true
        fi
    fi
    
    git tag -a "v$VERSION" -m "Version $VERSION" || echo "Tag creation failed"
    
    echo ""
    echo -e "${GREEN}Ready to push!${NC}"
    echo "Repository: $TEST_REPO_DIR"
    echo "Would execute:"
    echo "  git push origin main"
    echo "  git push --tags"
    echo ""
    read -p "Push to GitHub now? (yes/no) " -r
    echo
    if [[ $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        git push origin main
        git push --tags
        echo -e "${GREEN}✓ Published successfully!${NC}"
    else
        echo "Push cancelled. Repository left at: $TEST_REPO_DIR"
        echo "You can review and push manually if needed."
    fi
    
    cd - > /dev/null
fi

echo ""
echo -e "${GREEN}==========================================${NC}"
echo -e "${GREEN}Test completed successfully!${NC}"
echo -e "${GREEN}==========================================${NC}"

