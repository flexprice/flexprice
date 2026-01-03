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

# SDK Generation targets
.PHONY: install-openapi-generator
install-openapi-generator:
	@which openapi-generator-cli > /dev/null || (npm install -g @openapitools/openapi-generator-cli)

.PHONY: generate-sdk generate-go-sdk generate-python-sdk generate-javascript-sdk regenerate-sdk clean-sdk update-sdk

# Generate all SDKs
generate-sdk: generate-go-sdk generate-python-sdk generate-javascript-sdk
	@echo "All SDKs generated successfully with custom files"

# Regenerate all SDKs (clean + generate) - DEPRECATED: Use speakeasy-regenerate-sdk
# regenerate-sdk: clean-sdk generate-sdk
# 	@echo "All SDKs regenerated successfully with custom files"

# Update swagger and regenerate all SDKs
update-sdk: swagger regenerate-sdk
	@echo "Swagger updated and all SDKs regenerated with custom files"

# Clean all generated SDKs
clean-sdk:
	@echo "Cleaning generated SDKs..."
	@rm -rf api/javascript api/python api/go
	@echo "Generated SDKs cleaned"

# Generate Go SDK
generate-go-sdk: install-openapi-generator
	@echo "Generating Go SDK..."
	@openapi-generator-cli generate \
		-i docs/swagger/swagger-3-0.json \
		-g go \
		-o api/go \
		--additional-properties=packageName=flexprice,isGoSubmodule=true,enumClassPrefix=true,structPrefix=true \
		--git-repo-id=go-sdk \
		--git-user-id=flexprice \
		--global-property apiTests=false,modelTests=false
	@chmod +x api/scripts/go/add_go_async.sh
	@./api/scripts/go/add_go_async.sh
	@echo "Copying custom files..."
	@./scripts/copy-custom-files.sh go
	@echo "Go SDK generated successfully"

# Generate Python SDK
generate-python-sdk: install-openapi-generator
	@echo "Generating Python SDK..."
	@openapi-generator-cli generate \
		-i docs/swagger/swagger-3-0.json \
		-g python \
		-o api/python \
		--additional-properties=packageName=flexprice \
		--git-repo-id=python-sdk \
		--git-user-id=flexprice \
		--global-property apiTests=false,modelTests=false
	@python api/scripts/python/add_python_async.py || echo "Failed to add async functionality, but continuing..."
	@echo "Copying custom files..."
	@./scripts/copy-custom-files.sh python
	@echo "Python SDK generated successfully"

# Generate JavaScript/TypeScript SDK
generate-javascript-sdk: install-openapi-generator
	@echo "Generating TypeScript SDK with modern ES7 module support..."
	@./scripts/generate-ts-sdk.sh

# Copy custom files to specific SDKs (manual operation)
copy-custom:
	@echo "Copying custom files to all SDKs..."
	@./scripts/copy-custom-files.sh javascript
	@./scripts/copy-custom-files.sh python
	@./scripts/copy-custom-files.sh go

copy-javascript-custom:
	@echo "Copying custom files to JavaScript SDK..."
	@./scripts/copy-custom-files.sh javascript

copy-python-custom:
	@echo "Copying custom files to Python SDK..."
	@./scripts/copy-custom-files.sh python

copy-go-custom:
	@echo "Copying custom files to Go SDK..."
	@./scripts/copy-custom-files.sh go

# Show custom files status
show-custom-files:
	@echo "Custom files status:"
	@echo "==================="
	@echo "JavaScript custom files:"
	@if [ -d "api/custom/javascript" ]; then \
		find api/custom/javascript -type f -not -name "README.md" | sed 's/^/  /' || echo "  No custom files found"; \
	else \
		echo "  No custom directory found"; \
	fi
	@echo ""
	@echo "Python custom files:"
	@if [ -d "api/custom/python" ]; then \
		find api/custom/python -type f -not -name "README.md" | sed 's/^/  /' || echo "  No custom files found"; \
	else \
		echo "  No custom directory found"; \
	fi
	@echo ""
	@echo "Go custom files:"
	@if [ -d "api/custom/go" ]; then \
		find api/custom/go -type f -not -name "README.md" | sed 's/^/  /' || echo "  No custom files found"; \
	else \
		echo "  No custom directory found"; \
	fi

# Help for SDK management
help-sdk:
	@echo "SDK Management Commands:"
	@echo "======================="
	@echo "  make generate-sdk        - Generate all SDKs with custom files"
	@echo "  make regenerate-sdk      - Clean and regenerate all SDKs"
	@echo "  make update-sdk          - Update swagger and regenerate all SDKs"
	@echo "  make clean-sdk           - Clean all generated SDKs"
	@echo "  make copy-custom         - Copy custom files to all SDKs"
	@echo "  make show-custom-files   - Show status of custom files"
	@echo ""
	@echo "Individual SDK Commands:"
	@echo "  make generate-javascript-sdk  - Generate JavaScript SDK"
	@echo "  make generate-python-sdk      - Generate Python SDK"
	@echo "  make generate-go-sdk          - Generate Go SDK"
	@echo "  make copy-javascript-custom   - Copy custom files to JavaScript SDK"
	@echo "  make copy-python-custom       - Copy custom files to Python SDK"
	@echo "  make copy-go-custom           - Copy custom files to Go SDK"

# Note: Alternative modern approach removed during cleanup

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

.PHONY: speakeasy-install speakeasy-generate speakeasy-validate speakeasy-test

speakeasy-install:
	@echo "Installing Speakeasy CLI..."
	@brew install speakeasy-api/homebrew-tap/speakeasy || curl -fsSL https://raw.githubusercontent.com/speakeasy-api/speakeasy/main/install.sh | sh
	@speakeasy --version

speakeasy-validate:
	@echo "Validating OpenAPI spec..."
	@speakeasy validate openapi --schema docs/swagger/swagger-3-0.json

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
	@echo "Removing JavaScript SDK generated files..."
	@find api/javascript/src -type f -name "*.ts" ! -path "*/apis/CustomerPortal.ts" -delete 2>/dev/null || true
	@rm -rf api/javascript/dist api/javascript/node_modules 2>/dev/null || true
	@rm -f api/javascript/package*.json api/javascript/tsconfig*.json 2>/dev/null || true
	@rm -rf api/javascript/.devcontainer api/javascript/docs 2>/dev/null || true
	@rm -f api/javascript/*.md api/javascript/.git* 2>/dev/null || true
	@echo "✓ SDK cleanup complete"

speakeasy-generate:
	@echo "Generating SDKs with Speakeasy..."
	@speakeasy run

regenerate-sdk: go-sdk
	@echo "✓ Go SDK regenerated (Python/JS commented out for now)"

speakeasy-test:
	@echo "Testing generated SDKs..."
	@echo "Testing JavaScript SDK..."
	@cd api/javascript && npm ci && npm run build && npm test
	@echo "Testing Python SDK..."
	@cd api/python && pip install -e . && pytest
	@echo "Testing Go SDK..."
	@cd api/go && go mod tidy && go test ./...

# New unified SDK generation with Speakeasy
speakeasy-sdk: swagger speakeasy-generate
	@echo "✓ SDKs generated successfully with Speakeasy"

# =============================================================================
# Go SDK Generation with Speakeasy (Production Pipeline)
# =============================================================================

.PHONY: speakeasy-go-sdk speakeasy-copy-go-custom clean-go-sdk go-sdk regenerate-go-sdk

# Generate Go SDK only with Speakeasy
speakeasy-go-sdk:
	@echo "🔨 Generating Go SDK with Speakeasy..."
	@speakeasy run --target flexprice-go
	@echo "✓ Go SDK generated successfully"

# Copy custom files to Go SDK (post-generation)
speakeasy-copy-go-custom:
	@echo "📋 Copying custom files to Go SDK..."
	@if [ ! -d "api/go" ]; then \
		echo "❌ Error: api/go directory not found. Run 'make speakeasy-go-sdk' first"; \
		exit 1; \
	fi
	@# Copy helpers.go
	@if [ -f "api/custom/go/helpers.go" ]; then \
		cp api/custom/go/helpers.go api/go/; \
		echo "✓ Copied helpers.go"; \
	else \
		echo "⚠️  Warning: api/custom/go/helpers.go not found"; \
	fi
	@# Copy async.go
	@if [ -f "api/custom/go/async.go" ]; then \
		cp api/custom/go/async.go api/go/; \
		echo "✓ Copied async.go"; \
	else \
		echo "⚠️  Warning: api/custom/go/async.go not found"; \
	fi
	@# Copy examples directory
	@if [ -d "api/custom/go/examples" ]; then \
		rm -rf api/go/examples 2>/dev/null || true; \
		cp -r api/custom/go/examples api/go/; \
		echo "✓ Copied examples/ directory"; \
	else \
		echo "⚠️  Warning: api/custom/go/examples not found"; \
	fi
	@echo "✅ Custom files copied successfully"

# Clean only Go SDK
clean-go-sdk:
	@echo "🧹 Cleaning Go SDK..."
	@rm -rf api/go
	@echo "✓ Go SDK cleaned"

# Complete Go SDK pipeline: clean → generate → copy custom files → build
go-sdk: clean-go-sdk swagger speakeasy-go-sdk speakeasy-copy-go-custom
	@echo "🧪 Testing Go SDK compilation..."
	@cd api/go && go mod tidy && go build ./...
	@echo "✅ Go SDK ready for publishing!"

# Quick regeneration (no clean, faster for development)
regenerate-go-sdk: swagger speakeasy-go-sdk speakeasy-copy-go-custom
	@echo "✓ Go SDK regenerated"


# Testing all SDKs
test-speakeasy-sdks: speakeasy-test
	@echo "✓ All SDK tests passed"

# Migration helpers
speakeasy-migrate:
	@echo "Running Speakeasy migration..."
	@./scripts/migrate-to-speakeasy.sh

speakeasy-compare:
	@echo "Comparing old and new SDK structures..."
	@./scripts/compare-sdks.sh

speakeasy-archive-old:
	@echo "Archiving old OpenAPI Generator pipeline..."
	@./scripts/archive-old-pipeline.sh

.PHONY: speakeasy-sdk test-speakeasy-sdks speakeasy-migrate speakeasy-compare speakeasy-archive-old
