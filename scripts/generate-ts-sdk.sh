#!/bin/bash

# TypeScript SDK Generation Script
# This script generates a modern TypeScript SDK with proper configuration

set -e -o pipefail

# Cleanup function to remove backup directories
cleanup_backups() {
    echo -e "${BLUE}🧹 Cleaning up backup directories...${NC}"
    if [ -d "$EXAMPLES_BACKUP" ]; then
        echo -e "${BLUE}🗑️  Removing examples backup: $EXAMPLES_BACKUP${NC}"
        rm -rf "$EXAMPLES_BACKUP"
    fi

    if [ -d "$CUSTOM_BACKUP" ]; then
        echo -e "${BLUE}🗑️  Removing custom APIs backup: $CUSTOM_BACKUP${NC}"
        rm -rf "$CUSTOM_BACKUP"
    fi

    if [ -d "$BACKUP_DIR" ]; then
        echo -e "${BLUE}🗑️  Removing main backup: $BACKUP_DIR${NC}"
        rm -rf "$BACKUP_DIR"
    fi
}

# Set trap to cleanup on script exit
trap cleanup_backups EXIT

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
API_DIR="api/javascript"
CUSTOM_DIR="custom-sdk-files/javascript"
SWAGGER_FILE="docs/swagger/swagger-3-0.json"
SDK_NAME="@flexprice/sdk"
SDK_VERSION="1.0.17"

echo -e "${BLUE}🚀 Starting TypeScript SDK generation...${NC}"

# Ensure we're in the project root directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_ROOT" || {
    echo -e "${RED}❌ Error: Could not change to project root directory: $PROJECT_ROOT${NC}"
    exit 1
}
echo -e "${BLUE}📁 Working from project root: $(pwd)${NC}"

# Verify we're in the right directory
if [ ! -f "go.mod" ] || [ ! -d "api" ]; then
    echo -e "${RED}❌ Error: Not in project root directory. Expected go.mod and api/ directory${NC}"
    echo -e "${YELLOW}💡 Current directory: $(pwd)${NC}"
    exit 1
fi

# Check if swagger file exists
if [ ! -f "$SWAGGER_FILE" ]; then
    echo -e "${RED}❌ Error: Swagger file not found at $SWAGGER_FILE${NC}"
    echo -e "${YELLOW}💡 Please run 'make swagger' first to generate the swagger files${NC}"
    exit 1
fi

# Check if openapi-generator-cli is installed
if ! command -v openapi-generator-cli &> /dev/null; then
    echo -e "${YELLOW}📦 Installing OpenAPI Generator CLI...${NC}"
    npm install -g @openapitools/openapi-generator-cli
fi

# Clean and create API directory while preserving examples and custom APIs
echo -e "${BLUE}🧹 Cleaning existing SDK directory while preserving examples and custom APIs...${NC}"
if [ -d "$API_DIR" ]; then
    # Backup examples directory if it exists
    if [ -d "$API_DIR/examples" ]; then
        echo -e "${BLUE}📁 Backing up examples directory...${NC}"
        EXAMPLES_BACKUP="${API_DIR}_examples_backup_$(date +%Y%m%d_%H%M%S)"
        cp -r "$API_DIR/examples" "$EXAMPLES_BACKUP"
    fi
    
    # Backup custom APIs if they exist
    if [ -d "$API_DIR/custom" ]; then
        echo -e "${BLUE}📁 Backing up custom APIs directory...${NC}"
        CUSTOM_BACKUP="${API_DIR}_custom_backup_$(date +%Y%m%d_%H%M%S)"
        cp -r "$API_DIR/custom" "$CUSTOM_BACKUP"
    fi
    
    # Try to remove normally first
    if ! rm -rf "$API_DIR" 2>/dev/null; then
        echo -e "${YELLOW}⚠️  Could not remove directory normally, creating backup and using new directory...${NC}"
        # Create a backup directory with timestamp
        BACKUP_DIR="${API_DIR}_backup_$(date +%Y%m%d_%H%M%S)"
        mv "$API_DIR" "$BACKUP_DIR" 2>/dev/null || {
            echo -e "${YELLOW}⚠️  Could not move directory, will work with existing directory...${NC}"
        }
    fi
fi
mkdir -p "$API_DIR"

# Restore examples directory if it was backed up
if [ -d "$EXAMPLES_BACKUP" ]; then
    echo -e "${BLUE}📁 Restoring examples directory...${NC}"
    mv "$EXAMPLES_BACKUP" "$API_DIR/examples"
fi

# Restore custom APIs directory if it was backed up
if [ -d "$CUSTOM_BACKUP" ]; then
    echo -e "${BLUE}📁 Restoring custom APIs directory...${NC}"
    mv "$CUSTOM_BACKUP" "$API_DIR/custom"
fi

# Generate TypeScript SDK
echo -e "${BLUE}⚙️  Generating TypeScript SDK...${NC}"
openapi-generator-cli generate \
    -i "$SWAGGER_FILE" \
    -g typescript-fetch \
    -o "$API_DIR" \
    --additional-properties=npmName="$SDK_NAME",npmVersion="$SDK_VERSION",npmRepository=https://github.com/flexprice/javascript-sdk.git,supportsES6=true,typescriptThreePlus=true,withNodeImports=true,withSeparateModelsAndApi=true,modelPackage=models,apiPackage=apis,enumPropertyNaming=UPPERCASE,stringEnums=true,modelPropertyNaming=camelCase,paramNaming=camelCase,withInterfaces=true,useSingleRequestParameter=true,platform=node,sortParamsByRequiredFlag=true,sortModelPropertiesByRequiredFlag=true,ensureUniqueParams=true,allowUnicodeIdentifiers=false,prependFormOrBodyParameters=false,apiNameSuffix=Api \
    --git-repo-id=javascript-sdk \
    --git-user-id=flexprice \
    --global-property apiTests=false,modelTests=false,apiDocs=true,modelDocs=true,withSeparateModelsAndApi=true,withInterfaces=true,useSingleRequestParameter=true,typescriptThreePlus=true,platform=node

# Configure package.json
echo -e "${BLUE}📝 Configuring package.json...${NC}"
cd "$API_DIR"

# Update package.json with modern configuration
npm pkg set type=module
npm pkg set main=./dist/index.js
npm pkg set module=./dist/index.js
npm pkg set types=./dist/index.d.ts
npm pkg set engines.node=">=16.0.0"
npm pkg set description="Official TypeScript/JavaScript SDK of Flexprice with modern ES7 module support"
npm pkg set keywords='["flexprice","sdk","typescript","javascript","api","billing","pricing","es7","esmodules","fetch"]'
npm pkg set scripts.build="tsc"
npm pkg set scripts.prepare="npm run build"
npm pkg set scripts.test="jest"
npm pkg set scripts.lint="eslint src/**/*.ts"
npm pkg set scripts."lint:fix"="eslint src/**/*.ts --fix"
npm pkg set files='["dist","src","README.md"]'
npm pkg set exports='{".": {"import": "./dist/index.js", "require": "./dist/index.cjs", "types": "./dist/index.d.ts"}, "./package.json": "./package.json"}'

# Remove invalid dependencies and add proper ones
echo -e "${BLUE}🔧 Fixing package.json dependencies...${NC}"

# Remove the invalid "expect": {} entry and other problematic entries
npm pkg delete devDependencies.expect
npm pkg delete devDependencies."@types/jest"

# Install TypeScript dependencies
echo -e "${BLUE}📦 Installing TypeScript dependencies...${NC}"
npm install --save-dev \
    typescript@^5.0.0 \
    @types/node@^20.0.0 \
    @typescript-eslint/eslint-plugin@^6.0.0 \
    @typescript-eslint/parser@^6.0.0 \
    eslint@^8.0.0 \
    jest@^29.5.0 \
    ts-jest@^29.1.0 \
    @types/jest@^29.5.0

# Create TypeScript configuration
echo -e "${BLUE}⚙️  Creating TypeScript configuration...${NC}"
cat > tsconfig.json << 'EOF'
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "node",
    "lib": ["ES2022", "DOM"],
    "declaration": true,
    "declarationMap": true,
    "sourceMap": true,
    "outDir": "./dist",
    "rootDir": "./src",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "forceConsistentCasingInFileNames": true,
    "allowSyntheticDefaultImports": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": false,
    "incremental": true,
    "tsBuildInfoFile": "./dist/.tsbuildinfo"
  },
  "include": ["src/**/*"],
  "exclude": ["node_modules", "dist", "**/*.test.ts", "**/*.spec.ts"]
}
EOF

# Create Jest configuration
echo -e "${BLUE}⚙️  Creating Jest configuration...${NC}"
cat > jest.config.js << 'EOF'
export default {
  preset: 'ts-jest/presets/default-esm',
  testEnvironment: 'node',
  extensionsToTreatAsEsm: ['.ts'],
  globals: {
    'ts-jest': {
      useESM: true,
    },
  },
  moduleNameMapping: {
    '^(\\.{1,2}/.*)\\.js$': '$1',
  },
  transform: {
    '^.+\\.ts$': ['ts-jest', {
      useESM: true,
    }],
  },
  testMatch: [
    '**/__tests__/**/*.test.ts',
    '**/?(*.)+(spec|test).ts',
  ],
  collectCoverageFrom: [
    'src/**/*.ts',
    '!src/**/*.d.ts',
    '!src/**/*.test.ts',
    '!src/**/*.spec.ts',
  ],
  coverageDirectory: 'coverage',
  coverageReporters: ['text', 'lcov', 'html'],
};
EOF

# Create ESLint configuration
echo -e "${BLUE}⚙️  Creating ESLint configuration...${NC}"
cat > .eslintrc.js << 'EOF'
module.exports = {
  parser: '@typescript-eslint/parser',
  extends: [
    'eslint:recommended',
    '@typescript-eslint/recommended',
  ],
  parserOptions: {
    ecmaVersion: 2022,
    sourceType: 'module',
    project: './tsconfig.json',
  },
  plugins: ['@typescript-eslint'],
  rules: {
    '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
    '@typescript-eslint/explicit-function-return-type': 'off',
    '@typescript-eslint/explicit-module-boundary-types': 'off',
    '@typescript-eslint/no-explicit-any': 'warn',
    '@typescript-eslint/no-non-null-assertion': 'warn',
    'prefer-const': 'error',
    'no-var': 'error',
  },
  env: {
    node: true,
    es2022: true,
  },
  ignorePatterns: ['dist/', 'node_modules/', '*.js'],
};
EOF

# Create .gitignore
echo -e "${BLUE}⚙️  Creating .gitignore...${NC}"
cat > .gitignore << 'EOF'
# Dependencies
node_modules/
npm-debug.log*
yarn-debug.log*
yarn-error.log*

# Build outputs
dist/
build/
*.tsbuildinfo

# Coverage
coverage/

# IDE
.vscode/
.idea/
*.swp
*.swo

# OS
.DS_Store
Thumbs.db

# Logs
logs/
*.log

# Runtime data
pids/
*.pid
*.seed
*.pid.lock

# Optional npm cache directory
.npm

# Optional eslint cache
.eslintcache

# Microbundle cache
.rpt2_cache/
.rts2_cache_cjs/
.rts2_cache_es/
.rts2_cache_umd/

# Optional REPL history
.node_repl_history

# Output of 'npm pack'
*.tgz

# Yarn Integrity file
.yarn-integrity

# dotenv environment variables file
.env
.env.test

# parcel-bundler cache (https://parceljs.org/)
.cache
.parcel-cache

# next.js build output
.next

# nuxt.js build output
.nuxt

# vuepress build output
.vuepress/dist

# Serverless directories
.serverless/

# FuseBox cache
.fusebox/

# DynamoDB Local files
.dynamodb/

# TernJS port file
.tern-port
EOF

# Create .openapi-generator-ignore
echo -e "${BLUE}⚙️  Creating .openapi-generator-ignore...${NC}"
cat > .openapi-generator-ignore << 'EOF'
# Ignore generated files that we'll replace
package.json
tsconfig.json
jest.config.js
.eslintrc.js
.gitignore
README.md

# Ignore test files for now
**/*.test.ts
**/*.spec.ts
**/__tests__/**

# Preserve custom APIs directory
custom/
EOF

# Copy custom APIs from custom-sdk-files directory
echo -e "${BLUE}📁 Copying custom APIs...${NC}"
echo -e "${BLUE}🔍 Checking for custom directory at: $CUSTOM_DIR${NC}"
echo -e "${BLUE}🔍 Current working directory: $(pwd)${NC}"
echo -e "${BLUE}🔍 Directory exists check: $(ls -la $CUSTOM_DIR 2>/dev/null || echo 'Directory not found')${NC}"

# Use absolute paths to ensure we find the custom directory
ABSOLUTE_CUSTOM_DIR="$PROJECT_ROOT/$CUSTOM_DIR"
ABSOLUTE_API_DIR="$PROJECT_ROOT/$API_DIR"

echo -e "${BLUE}🔍 Absolute custom directory: $ABSOLUTE_CUSTOM_DIR${NC}"
echo -e "${BLUE}🔍 Absolute API directory: $ABSOLUTE_API_DIR${NC}"

if [ -d "$ABSOLUTE_CUSTOM_DIR" ]; then
    echo -e "${BLUE}📁 Found custom directory: $ABSOLUTE_CUSTOM_DIR${NC}"
    # Copy custom APIs to the generated SDK
    if [ -d "$ABSOLUTE_CUSTOM_DIR/src/apis" ]; then
        echo -e "${BLUE}📁 Copying custom API files...${NC}"
        cp "$ABSOLUTE_CUSTOM_DIR/src/apis/"*.ts "$ABSOLUTE_API_DIR/src/apis/" 2>/dev/null || true
    fi
    if [ -d "$ABSOLUTE_CUSTOM_DIR/examples" ]; then
        echo -e "${BLUE}📁 Copying custom examples...${NC}"
        cp -r "$ABSOLUTE_CUSTOM_DIR/examples/"* "$ABSOLUTE_API_DIR/examples/" 2>/dev/null || true
    fi
    if [ -f "$ABSOLUTE_CUSTOM_DIR/"*.md ]; then
        echo -e "${BLUE}📁 Copying custom markdown files...${NC}"
        cp "$ABSOLUTE_CUSTOM_DIR/"*.md "$ABSOLUTE_API_DIR/" 2>/dev/null || true
    fi
    
    # Update the main index.ts to include custom APIs
    echo -e "${BLUE}🔧 Updating main index.ts to include custom APIs...${NC}"
    if [ -f "$ABSOLUTE_API_DIR/src/index.ts" ]; then
        # Add custom API exports to the main index
        echo "" >> "$ABSOLUTE_API_DIR/src/index.ts"
        echo "// Custom APIs" >> "$ABSOLUTE_API_DIR/src/index.ts"
        echo "export * from './apis/CustomerPortalApi';" >> "$ABSOLUTE_API_DIR/src/index.ts"
    fi
    
    # Update apis/index.ts to include custom APIs
    if [ -f "$ABSOLUTE_API_DIR/src/apis/index.ts" ]; then
        # Add custom API exports to the apis index
        echo "" >> "$ABSOLUTE_API_DIR/src/apis/index.ts"
        echo "// Custom APIs" >> "$ABSOLUTE_API_DIR/src/apis/index.ts"
        echo "export * from './CustomerPortalApi';" >> "$ABSOLUTE_API_DIR/src/apis/index.ts"
    fi
    
    echo -e "${GREEN}✅ Custom APIs copied successfully${NC}"
else
    echo -e "${YELLOW}⚠️  Custom APIs directory not found at $CUSTOM_DIR${NC}"
fi

# Build the project
echo -e "${BLUE}🔨 Building TypeScript project...${NC}"
npm run build

echo -e "${GREEN}✅ TypeScript SDK generated successfully!${NC}"
echo -e "${GREEN}📁 Location: $API_DIR${NC}"
echo -e "${GREEN}🚀 Ready for development and publishing${NC}"

# Show next steps
echo -e "${YELLOW}💡 Next steps:${NC}"
echo -e "  1. cd $API_DIR"
echo -e "  2. npm run test    # Run tests"
echo -e "  3. npm run lint    # Check code quality"
echo -e "  4. npm run build   # Build the project"
echo -e "  5. npm publish     # Publish to npm (when ready)"
