#!/bin/bash

# TypeScript SDK Generation Script
# This script generates a modern TypeScript SDK with proper configuration

set -e -o pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
API_DIR="api/javascript"
SWAGGER_FILE="docs/swagger/swagger-3-0.json"
SDK_NAME="@flexprice/sdk"
SDK_VERSION="1.0.0"

echo -e "${BLUE}🚀 Starting TypeScript SDK generation...${NC}"

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

# Clean and create API directory while preserving examples
echo -e "${BLUE}🧹 Cleaning existing SDK directory while preserving examples...${NC}"
if [ -d "$API_DIR" ]; then
    # Backup examples directory if it exists
    if [ -d "$API_DIR/examples" ]; then
        echo -e "${BLUE}📁 Backing up examples directory...${NC}"
        EXAMPLES_BACKUP="${API_DIR}_examples_backup_$(date +%Y%m%d_%H%M%S)"
        cp -r "$API_DIR/examples" "$EXAMPLES_BACKUP"
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

# Generate TypeScript SDK
echo -e "${BLUE}⚙️  Generating TypeScript SDK...${NC}"
openapi-generator-cli generate \
    -i "$SWAGGER_FILE" \
    -g typescript-fetch \
    -o "$API_DIR" \
    --additional-properties=npmName="$SDK_NAME",supportsES6=true,typescriptThreePlus=true,withNodeImports=true,withSeparateModelsAndApi=true,modelPackage=models,apiPackage=apis,enumPropertyNaming=UPPERCASE,stringEnums=true,modelPropertyNaming=camelCase,paramNaming=camelCase,withInterfaces=true,useSingleRequestParameter=true,platform=node,sortParamsByRequiredFlag=true,sortModelPropertiesByRequiredFlag=true,ensureUniqueParams=true,allowUnicodeIdentifiers=false,prependFormOrBodyParameters=false,apiNameSuffix=Api \
    --git-repo-id=javascript-sdk \
    --git-user-id=flexprice \
    --global-property apiTests=false,modelTests=false,apiDocs=true,modelDocs=true,withSeparateModelsAndApi=true,withInterfaces=true,useSingleRequestParameter=true,typescriptThreePlus=true,platform=node

# Configure package.json
echo -e "${BLUE}📝 Configuring package.json...${NC}"
cd "$API_DIR"

# Create package.json with proper JSON structure
echo -e "${BLUE}🔧 Creating package.json with proper JSON structure...${NC}"
cat > package.json << EOF
{
  "name": "@flexprice/sdk",
  "version": "$SDK_VERSION",
  "description": "Official TypeScript/JavaScript SDK of Flexprice",
  "author": "Flexprice",
  "repository": {
    "type": "git",
    "url": "https://github.com/flexprice/javascript-sdk.git"
  },
  "main": "./dist/index.js",
  "typings": "./dist/index.d.ts",
  "module": "./dist/index.js",
  "sideEffects": false,
  "scripts": {
    "build": "tsc",
    "prepare": "npm run build",
    "test": "jest",
    "lint": "eslint src/**/*.ts",
    "lint:fix": "eslint src/**/*.ts --fix"
  },
  "type": "module",
  "types": "./dist/index.d.ts",
  "engines": {
    "node": ">=16.0.0"
  },
  "keywords": ["flexprice", "sdk", "typescript", "javascript", "api", "billing", "pricing", "es7", "esmodules", "fetch"],
  "files": ["dist", "README.md"],
  "exports": {
    ".": {
      "import": "./dist/index.js",
      "require": "./dist/index.cjs",
      "types": "./dist/index.d.ts"
    },
    "./package.json": "./package.json"
  }
}
EOF

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
    "module": "ES2022",
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
  "exclude": ["node_modules", "**/*.test.ts", "**/*.spec.ts"]
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
EOF

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
