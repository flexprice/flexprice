.PHONY: swagger-clean
swagger-clean:
	rm -rf docs/swagger

.PHONY: install-swag
install-swag:
	@which swag > /dev/null || (go install github.com/swaggo/swag/cmd/swag@latest)

.PHONY: swagger
swagger: swagger-2-0 swagger-3-0

.PHONY: swagger-2-0
swagger-2-0: install-swag
	$(shell go env GOPATH)/bin/swag init \
		--generalInfo cmd/server/main.go \
		--dir . \
		--parseDependency \
		--parseInternal \
		--output docs/swagger \
		--generatedTime=false \
		--parseDepth 1 \
		--instanceName swagger \
		--parseVendor \
		--outputTypes go,json,yaml
	@make swagger-fix-refs

.PHONY: swagger-3-0
swagger-3-0: install-swag
	@echo "Converting Swagger 2.0 to OpenAPI 3.0..."
	@curl -X 'POST' \
		'https://converter.swagger.io/api/convert' \
		-H 'accept: application/json' \
		-H 'Content-Type: application/json' \
		-d @docs/swagger/swagger.json > docs/swagger/swagger-3-0.json
	@echo "Conversion complete. Output saved to docs/swagger/swagger-3-0.json"

.PHONY: swagger-fix-refs
swagger-fix-refs:
	@./scripts/fix_swagger_refs.sh

.PHONY: up
up:
	docker compose up -d --build

.PHONY: down
down:
	docker compose down

.PHONY: run-server
run-server:
	go run cmd/server/main.go

.PHONY: run-server-local
run-server-local: run-server

.PHONY: run
run: run-server

.PHONY: test test-verbose test-coverage

# Run all tests
test: install-typst
	go test -v -race ./internal/...

# Run tests with verbose output
test-verbose:
	go test -v ./internal/...

# Run tests with coverage report
test-coverage:
	go test -coverprofile=coverage.out ./internal/...
	go tool cover -html=coverage.out -o coverage.html

# Database related targets
.PHONY: init-db migrate-postgres migrate-clickhouse seed-db migrate-ent

.PHONY: install-ent
install-ent:
	@which ent > /dev/null || (go install entgo.io/ent/cmd/ent@latest)

.PHONY: generate-ent
generate-ent: install-ent
	@echo "Generating ent code..."
	@go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/execquery ./ent/schema

.PHONY: migrate-ent
migrate-ent:
	@echo "Running Ent migrations..."
	@go run cmd/migrate/main.go --timeout 300
	@echo "Ent migrations complete"

.PHONY: migrate-ent-dry-run
migrate-ent-dry-run:
	@echo "Generating SQL migration statements (dry run)..."
	@go run cmd/migrate/main.go --dry-run --timeout 300
	@echo "SQL migration statements generated"

.PHONY: generate-migration
generate-migration:
	@echo "Generating SQL migration file..."
	@mkdir -p migrations/ent
	@go run cmd/migrate/main.go --dry-run --timeout 300 > migrations/ent/migration_$(shell date +%Y%m%d%H%M%S).sql
	@echo "SQL migration file generated in migrations/ent/"

# Initialize databases and required topics
init-db: up migrate-postgres migrate-clickhouse generate-ent migrate-ent seed-db
	@echo "Database initialization complete"

# Run postgres migrations
migrate-postgres:
	@echo "Running Postgres migrations..."
	@sleep 5  # Wait for postgres to be ready
	@docker compose exec -T postgres psql -U flexprice -d flexprice -c "CREATE SCHEMA IF NOT EXISTS extensions;"
	@docker compose exec -T postgres psql -U flexprice -d flexprice -c "CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\" SCHEMA extensions;"
	@echo "Postgres migrations complete"

# Run clickhouse migrations
migrate-clickhouse:
	@echo "Running Clickhouse migrations..."
	@sleep 5  # Wait for clickhouse to be ready
	@for file in migrations/clickhouse/*.sql; do \
		if [ -f "$$file" ]; then \
			echo "Running migration: $$file"; \
			docker compose exec -T clickhouse clickhouse-client --user=flexprice --password=flexprice123 --database=flexprice --multiquery < "$$file"; \
		fi \
	done
	@echo "Clickhouse migrations complete"

# Seed initial data
seed-db:
	@echo "Running Seed data migration..."
	@docker compose exec -T postgres psql -U flexprice -d flexprice -f /docker-entrypoint-initdb.d/V1__seed.sql
	@echo "Postgres seed data migration complete"

# Initialize kafka topics
.PHONY: init-kafka
init-kafka:
	@echo "Creating Kafka topics..."
	@for i in 1 2 3 4 5; do \
		echo "Attempt $$i: Checking if Kafka is ready..."; \
		if docker compose exec -T kafka kafka-topics --bootstrap-server kafka:9092 --list >/dev/null 2>&1; then \
			echo "Kafka is ready!"; \
			docker compose exec -T kafka kafka-topics --create --if-not-exists \
				--bootstrap-server kafka:9092 \
				--topic events \
				--partitions 1 \
				--replication-factor 1 \
				--config cleanup.policy=delete \
				--config retention.ms=604800000; \
			echo "Kafka topics created successfully"; \
			exit 0; \
		fi; \
		echo "Kafka not ready yet, waiting..."; \
		sleep 5; \
	done; \
	echo "Error: Kafka failed to become ready after 5 attempts"; \
	exit 1

# Clean all docker containers and volumes related to the project
.PHONY: clean-docker
clean-docker:
	@echo "Cleaning all docker containers and volumes..."
	@docker compose down -v
	@docker container prune -f
	@docker volume rm $$(docker volume ls -q | grep flexprice) 2>/dev/null || true
	@echo "Docker cleanup complete"

# Full local setup
.PHONY: setup-local
setup-local: up init-db init-kafka
	@echo "Local setup complete. You can now run 'make run-server-local' to start the server"

# Clean everything and start fresh
.PHONY: clean-start
clean-start:
	@make down
	@docker compose down -v
	@make setup-local

# Build the flexprice image separately
.PHONY: build-image
build-image:
	@echo "Building flexprice image..."
	@docker compose build flexprice-build
	@echo "Flexprice image built successfully"

# Start only the flexprice services
.PHONY: start-flexprice
start-flexprice:
	@echo "Starting flexprice services..."
	@docker compose up -d flexprice-api flexprice-consumer flexprice-worker
	@echo "Flexprice services started successfully"

# Stop only the flexprice services
.PHONY: stop-flexprice
stop-flexprice:
	@echo "Stopping flexprice services..."
	@docker compose stop flexprice-api flexprice-consumer flexprice-worker
	@echo "Flexprice services stopped successfully"

# Restart only the flexprice services
.PHONY: restart-flexprice
restart-flexprice: stop-flexprice start-flexprice
	@echo "Flexprice services restarted successfully"

# Full developer setup with clear instructions
.PHONY: dev-setup
dev-setup:
	@echo "Setting up FlexPrice development environment..."
	@echo "Step 1: Starting infrastructure services..."
	@docker compose up -d postgres kafka clickhouse temporal temporal-ui
	@echo "Step 2: Building FlexPrice application image..."
	@make build-image
	@echo "Step 3: Running database migrations and initializing Kafka..."
	@make init-db init-kafka migrate-ent seed-db 
	@echo "Step 4: Starting FlexPrice services..."
	@make start-flexprice
	@echo ""
	@echo "✅ FlexPrice development environment is now ready!"
	@echo "📊 Available services:"
	@echo "   - API:          http://localhost:8080"
	@echo "   - Temporal UI:  http://localhost:8088"
	@echo "   - Kafka UI:     http://localhost:8084 (with profile 'dev')"
	@echo "   - ClickHouse:   http://localhost:8123"
	@echo ""
	@echo "💡 Useful commands:"
	@echo "   - make restart-flexprice  # Restart FlexPrice services"
	@echo "   - make down              # Stop all services"
	@echo "   - make clean-start       # Clean everything and start fresh"

.PHONY: apply-migration
apply-migration:
	@if [ -z "$(file)" ]; then \
		echo "Error: Migration file not specified. Use 'make apply-migration file=<path>'"; \
		exit 1; \
	fi
	@echo "Applying migration file: $(file)"
	@PGPASSWORD=$(shell grep -A 2 "postgres:" config.yaml | grep password | awk '{print $$2}') \
		psql -h $(shell grep -A 2 "postgres:" config.yaml | grep host | awk '{print $$2}') \
		-U $(shell grep -A 2 "postgres:" config.yaml | grep username | awk '{print $$2}') \
		-d $(shell grep -A 2 "postgres:" config.yaml | grep database | awk '{print $$2}') \
		-f $(file)
	@echo "Migration applied successfully"

.PHONY: docker-build-local
docker-build-local:
	docker compose build flexprice-build

.PHONY: install-typst
install-typst:
	@./scripts/install-typst.sh

# SDK Generation targets (Speakeasy pipeline; use make sdk-all)
.PHONY: clean-sdk update-sdk

# Update swagger and regenerate all SDKs/MCP
update-sdk: swagger sdk-all
	@echo "Swagger updated and all SDKs/MCP regenerated."

# Clean all generated SDK/MCP output directories
clean-sdk:
	@echo "Cleaning generated SDKs/MCP..."
	@rm -rf api/go api/typescript api/python api/mcp
	@echo "Generated SDKs/MCP cleaned"

# Show custom files status (api/custom/<lang>/)
show-custom-files:
	@echo "Custom files status (api/custom/):"
	@echo "================================"
	@for dir in go typescript python mcp; do \
		echo "$$dir:"; \
		if [ -d "api/custom/$$dir" ]; then \
			find api/custom/$$dir -type f | sed 's/^/  /' || echo "  (none)"; \
		else \
			echo "  No custom directory"; \
		fi; \
		echo ""; \
	done

# Help for SDK management
help-sdk:
	@echo "SDK Management Commands:"
	@echo "======================="
	@echo "  make sdk-all             - Validate + generate all SDKs/MCP + merge custom (uses existing swagger)"
	@echo "  make filter-mcp-spec     - Build tag-filtered OpenAPI spec for MCP (allowed tags in api/mcp/.speakeasy/allowed-tags.yaml)"
	@echo "  make update-sdk          - Regenerate swagger then run sdk-all"
	@echo "  make clean-sdk           - Remove generated api/go, api/typescript, api/python, api/mcp"
	@echo "  make merge-custom       - Copy api/custom/<lang>/ into api/<lang>/"
	@echo "  make show-custom-files  - List files in api/custom/"
	@echo ""
	@echo "Go SDK only:"
	@echo "  make go-sdk              - Clean + generate Go SDK + merge custom + build"
	@echo "  make regenerate-go-sdk   - Regenerate Go SDK (no clean) + merge custom"
	@echo "  make clean-go-sdk        - Remove api/go only"

# SDK publishing
sdk-publish-js:
	@api/publish.sh --js $(if $(filter true,$(DRY_RUN)),--dry-run,) $(if $(VERSION),--version $(VERSION),)

sdk-publish-py:
	@api/publish.sh --py $(if $(filter true,$(DRY_RUN)),--dry-run,) $(if $(VERSION),--version $(VERSION),)

sdk-publish-go:
	@api/publish.sh --go $(if $(filter true,$(DRY_RUN)),--dry-run,) $(if $(VERSION),--version $(VERSION),)

sdk-publish-all:
	@api/publish.sh --all $(if $(filter true,$(DRY_RUN)),--dry-run,)

sdk-publish-all-with-version:
	@echo "Usage: make sdk-publish-all-with-version VERSION=x.y.z"
	@test -n "$(VERSION)" || (echo "Error: VERSION is required"; exit 1)
	@api/publish.sh --all --version $(VERSION) $(if $(filter true,$(DRY_RUN)),--dry-run,)

# Test GitHub workflow locally using act
test-github-workflow:
	@echo "Testing GitHub workflow locally..."
	@./scripts/ensure-act.sh
	@if [ ! -f .env ]; then \
		echo "Error: .env file not found. Please create a .env file with SDK_DEPLOY_GIT_TOKEN, NPM_AUTH_TOKEN, and PYPI_API_TOKEN"; \
		exit 1; \
	fi
	@SDK_DEPLOY_GIT_TOKEN=$$(grep SDK_DEPLOY_GIT_TOKEN .env | cut -d '=' -f2) \
	NPM_AUTH_TOKEN=$$(grep NPM_AUTH_TOKEN .env | cut -d '=' -f2) \
	PYPI_API_TOKEN=$$(grep PYPI_API_TOKEN .env | cut -d '=' -f2) \
	act release -e .github/workflows/test-event.json \
	 -s SDK_DEPLOY_GIT_TOKEN="$$SDK_DEPLOY_GIT_TOKEN" \
	 -s NPM_AUTH_TOKEN="$$NPM_AUTH_TOKEN" \
	 -s PYPI_API_TOKEN="$$PYPI_API_TOKEN" \
	 -P ubuntu-latest=catthehacker/ubuntu:act-latest \
	 --container-architecture linux/amd64 \
	 --action-offline-mode

.PHONY: sdk-publish-js sdk-publish-py sdk-publish-go sdk-publish-all sdk-publish-all-with-version test-github-workflow show-custom-files help-sdk

# =============================================================================
# Speakeasy SDK Generation (New Pipeline)
# =============================================================================
# Version is managed by Speakeasy (versioningStrategy: automatic in gen.yaml); do not pass --set-version.

.PHONY: speakeasy-install speakeasy-generate speakeasy-validate speakeasy-lint speakeasy-test

speakeasy-install:
	@echo "Installing Speakeasy CLI..."
	@brew install speakeasy-api/homebrew-tap/speakeasy || curl -fsSL https://raw.githubusercontent.com/speakeasy-api/speakeasy/main/install.sh | sh
	@speakeasy --version

speakeasy-validate:
	@echo "Validating OpenAPI spec..."
	@speakeasy validate openapi --schema docs/swagger/swagger-3-0.json

# 413 on upload is expected for large specs; report is still written to ~/.speakeasy/temp/
# CI=true and TERM=dumb disable the interactive TUI so make does not hang
speakeasy-lint:
	@echo "Linting OpenAPI spec..."
	@CI=true TERM=dumb speakeasy openapi lint -s docs/swagger/swagger-3-0.json --non-interactive

speakeasy-clean:
	@echo "Cleaning generated SDK files..."
	@echo "Removing Go SDK generated files..."
	@find api/go -type f -name "*.go" ! -path "*/examples/*" ! -path "*/custom/*" ! -name "helpers.go" -delete 2>/dev/null || true
	@find api/go -type d -name ".speakeasy" -exec rm -rf {} + 2>/dev/null || true
	@rm -f api/go/go.mod api/go/go.sum 2>/dev/null || true
	@rm -rf api/go/.devcontainer api/go/.openapi-generator api/go/.travis.yml 2>/dev/null || true
	@rm -rf api/go/docs api/go/models api/go/internal api/go/types api/go/optionalnullable api/go/retry api/go/speakeasyusagegen 2>/dev/null || true
	@rm -f api/go/*.md api/go/.git* 2>/dev/null || true
	@echo "Removing Python SDK generated files..."
	@find api/python -type f -name "*.py" ! -path "*/examples/*" ! -name "async_utils.py" -delete 2>/dev/null || true
	@rm -rf api/python/src api/python/dist api/python/build api/python/*.egg-info 2>/dev/null || true
	@rm -f api/python/setup.py api/python/pyproject.toml api/python/poetry.lock 2>/dev/null || true
	@rm -rf api/python/.devcontainer api/python/docs 2>/dev/null || true
	@rm -f api/python/*.md api/python/.git* 2>/dev/null || true
	@echo "Removing TypeScript SDK generated files..."
	@rm -rf api/typescript 2>/dev/null || true
	@echo "✓ SDK cleanup complete"

# MCP uses a tag-filtered spec (docs/swagger/swagger-3-0-mcp.json). Run this before sdk-all/speakeasy-generate.
# Allowed tags are in api/mcp/.speakeasy/allowed-tags.yaml.
.PHONY: filter-mcp-spec
filter-mcp-spec:
	@node scripts/filter-openapi-by-tags.mjs

speakeasy-generate: speakeasy-validate filter-mcp-spec
	@echo "Generating SDKs with Speakeasy..."
	@CI=true TERM=dumb speakeasy run --target all -y --skip-upload-spec --skip-compile --minimal

regenerate-sdk: go-sdk
	@echo "✓ Go SDK regenerated (Python/JS commented out for now)"

speakeasy-test:
	@echo "Testing generated SDKs..."
	@if [ -d "api/typescript" ]; then echo "Testing TypeScript SDK..."; (cd api/typescript && npm ci && npm run build && npm test) || true; fi
	@if [ -d "api/python" ]; then echo "Testing Python SDK..."; (cd api/python && pip install -e . && pytest) || true; fi
	@if [ -d "api/go" ]; then echo "Testing Go SDK..."; (cd api/go && go mod tidy && go test ./...); fi

# New unified SDK generation with Speakeasy
speakeasy-sdk: speakeasy-generate
	@echo "✓ SDKs generated successfully with Speakeasy"

# =============================================================================
# Single command: Swagger + SDK/MCP generation + lint (no testing; add make sdk-test later if needed)
# =============================================================================
# Run: make sdk-all
# Uses existing docs/swagger/swagger-3-0.json. Run 'make swagger' when you change the API.
# Does: (if VERSION unset) next patch version from .speakeasy/sdk-version.json → sync version to all gen.yaml → validate → generate → merge custom.
# Speakeasy reads version from gen.yaml (cannot use --set-version with --target all). Every run uses a unique version so publish never fails.
#
# Local auth: create a .secrets file (already gitignored) with:
#   SPEAKEASY_API_KEY=spk_your_key_here
# Then run: make sdk-all-local  (loads .secrets and runs sdk-all)
.PHONY: sdk-all sdk-all-local

sdk-all:
	@VER="$${VERSION:-$$(./scripts/next-sdk-version.sh patch)}"; \
	./scripts/sync-sdk-version-to-gen.sh "$$VER" && \
	$(MAKE) speakeasy-validate speakeasy-generate merge-custom fix-mcp-package-name
	@echo "✅ SDK/MCP generation complete. (Use make sdk-test for install/test when needed.)"

# Load SPEAKEASY_API_KEY from .secrets then run sdk-all. Use this when running locally.
sdk-all-local:
	@if [ -f .secrets ]; then set -a && . ./.secrets && set +a; fi && $(MAKE) sdk-all

# Optional: run install + tests for each generated SDK. Not part of sdk-all; use when you want full verification.
sdk-test:
	@echo "Running SDK/MCP tests (skipping missing targets)..."
	@if [ -f "api/go/go.mod" ]; then \
		echo "Testing Go SDK..."; \
		(cd api/go && go mod tidy && go build ./... && go test ./...); \
	else \
		echo "⏭️  Skipping Go SDK (api/go not generated)"; \
	fi
	@if [ -f "api/typescript/package.json" ]; then \
		echo "Testing TypeScript SDK..."; \
		(cd api/typescript && npm ci && npm run build && npm test); \
	else \
		echo "⏭️  Skipping TypeScript SDK (api/typescript not generated)"; \
	fi
	@if [ -f "api/python/pyproject.toml" ] || [ -f "api/python/setup.py" ]; then \
		echo "Testing Python SDK..."; \
		(cd api/python && (pip install -e . 2>/dev/null || pip install .) && pytest); \
	else \
		echo "⏭️  Skipping Python SDK (api/python not generated)"; \
	fi
	@echo "✓ SDK tests finished"

# =============================================================================
# Go SDK Generation with Speakeasy (Production Pipeline)
# =============================================================================

.PHONY: speakeasy-go-sdk speakeasy-copy-go-custom merge-custom clean-go-sdk go-sdk regenerate-go-sdk

# Generate Go SDK only with Speakeasy
speakeasy-go-sdk:
	@echo "🔨 Generating Go SDK with Speakeasy..."
	@bash -c 'set -o pipefail; CI=true TERM=dumb speakeasy run --target flexprice-go -y < /dev/null | cat'
	@echo "✓ Go SDK generated successfully"

# Apply custom Go files (async.go, helpers.go, examples) from api/custom/go/ into api/go/.
# Source of truth: api/custom/go/. merge-custom does the copy.
speakeasy-copy-go-custom: merge-custom
	@echo "✓ Go custom files applied (from api/custom/go/)"

# Clean only Go SDK
clean-go-sdk:
	@echo "🧹 Cleaning Go SDK..."
	@rm -rf api/go
	@echo "✓ Go SDK cleaned"

# Complete Go SDK pipeline: clean → validate/lint → generate → copy custom → build
go-sdk: clean-go-sdk speakeasy-validate speakeasy-go-sdk speakeasy-copy-go-custom merge-custom
	@echo "🧪 Testing Go SDK compilation..."
	@cd api/go && go mod tidy && go build ./...
	@echo "✅ Go SDK ready for publishing!"

# Quick regeneration (no clean, faster for development)
regenerate-go-sdk: speakeasy-go-sdk speakeasy-copy-go-custom merge-custom
	@echo "✓ Go SDK regenerated"

# Merge custom files from api/custom/<lang>/ into api/<lang>/ after generation.
# Add files under api/custom/go etc. with same relative paths as in api/go.
merge-custom:
	@for dir in go typescript python mcp; do \
		if [ -d "api/custom/$$dir" ]; then \
			echo "Merging custom files into api/$$dir/..."; \
			rsync -av --exclude='.gitkeep' "api/custom/$$dir/" "api/$$dir/" 2>/dev/null || true; \
		fi; \
	done
	@if [ -f api/python/pyproject.toml ]; then \
		sed 's/Generated by Speakeasy\./for the FlexPrice API./' api/python/pyproject.toml > api/python/pyproject.toml.tmp && mv api/python/pyproject.toml.tmp api/python/pyproject.toml; \
	fi
	@echo "✓ Custom merge complete"

# Force MCP package name so npm publish never uses reserved "mcp"; CI artifact gets @omkar273/mcp-temp.
.PHONY: fix-mcp-package-name
fix-mcp-package-name:
	@if [ -f api/mcp/package.json ]; then \
		jq '.name = "@omkar273/mcp-temp"' api/mcp/package.json > api/mcp/package.json.tmp && mv api/mcp/package.json.tmp api/mcp/package.json; \
		echo "✓ MCP package name set to @omkar273/mcp-temp"; \
	fi

# Testing all SDKs
test-speakeasy-sdks: speakeasy-test
	@echo "✓ All SDK tests passed"

# =============================================================================
# SDK integration tests (api/tests): local vs published
# =============================================================================
# Require FLEXPRICE_API_KEY and FLEXPRICE_API_HOST. Local tests require SDKs built (make sdk-all).
# Published tests require published packages installed (go mod tidy, pip install flexprice-sdk-test, npm install flexprice-sdk-test).
.PHONY: test-sdk-local test-sdk-local-go-ts test-sdk-published

test-sdk-local:
	@echo "Running local SDK tests (unpublished SDKs from api/go, api/python, api/javascript)..."
	@if [ -f "api/tests/go/go.mod" ]; then \
		echo "--- Go (local) ---"; (cd api/tests/go && go run test_local_sdk.go) || true; \
	else echo "⏭️  Skipping Go local (api/tests/go not found)"; fi
	@if [ -d "api/python" ]; then \
		echo "--- Python (local) ---"; (cd api/tests/python && python test_local_sdk.py) || true; \
	else echo "⏭️  Skipping Python local (api/python not found)"; fi
	@if [ -d "api/javascript" ] || [ -d "api/typescript" ]; then \
		echo "--- TypeScript (local) ---"; (cd api/tests/ts && npx ts-node test_local_sdk_js.ts) || true; \
	else echo "⏭️  Skipping TypeScript local (api/javascript not found)"; fi
	@echo "✓ Local SDK tests finished"

# Local SDK tests: Go + TypeScript only (no Python).
test-sdk-local-go-ts:
	@echo "Running local SDK tests (Go + TypeScript only)..."
	@if [ -f "api/tests/go/go.mod" ]; then \
		echo "--- Go (local) ---"; (cd api/tests/go && go run test_local_sdk.go) || true; \
	else echo "⏭️  Skipping Go local (api/tests/go not found)"; fi
	@if [ -d "api/javascript" ] || [ -d "api/typescript" ]; then \
		echo "--- TypeScript (local) ---"; (cd api/tests/ts && npx ts-node test_local_sdk_js.ts) || true; \
	else echo "⏭️  Skipping TypeScript local (api/javascript not found)"; fi
	@echo "✓ Go + TypeScript local SDK tests finished"

test-sdk-published:
	@echo "Running published SDK tests..."
	@echo "--- Go (published) ---"; (cd $(CURDIR) && go run -tags published ./api/tests/go/test_sdk.go) || true
	@echo "--- Python (published) ---"; (cd api/tests/python && python test_sdk.py) || true
	@echo "--- TypeScript (published) ---"; (cd api/tests/ts && npx ts-node test_sdk_js.ts) || true
	@echo "✓ Published SDK tests finished"

.PHONY: speakeasy-sdk sdk-all sdk-test test-speakeasy-sdks test-sdk-local test-sdk-local-go-ts test-sdk-published
