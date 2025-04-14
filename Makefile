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

.PHONY: swagger-3-0
swagger-3-0: install-swag
	@echo "Converting Swagger 2.0 to OpenAPI 3.0..."
	@curl -X 'POST' \
		'https://converter.swagger.io/api/convert' \
		-H 'accept: application/json' \
		-H 'Content-Type: application/json' \
		-d @docs/swagger/swagger.json > docs/swagger/swagger-3-0.json
	@echo "Conversion complete. Output saved to docs/swagger/swagger-3-0.json"

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
	@go run cmd/migrate/main.go
	@echo "Ent migrations complete"

.PHONY: migrate-ent-dry-run
migrate-ent-dry-run:
	@echo "Generating SQL migration statements (dry run)..."
	@go run cmd/migrate/main.go --dry-run
	@echo "SQL migration statements generated"

.PHONY: generate-migration
generate-migration:
	@echo "Generating SQL migration file..."
	@mkdir -p migrations/ent
	@go run cmd/migrate/main.go --dry-run > migrations/ent/migration_$(shell date +%Y%m%d%H%M%S).sql
	@echo "SQL migration file generated in migrations/ent/"

# Initialize databases and required topics
init-db: up migrate-postgres migrate-clickhouse generate-ent migrate-ent seed-db
	@echo "Database initialization complete"

# Run postgres migrations
migrate-postgres:
	@echo "Running Postgres migrations..."
	@sleep 5  # Wait for postgres to be ready
	@PGPASSWORD=flexprice123 psql -h localhost -U flexprice -d flexprice -c "CREATE SCHEMA IF NOT EXISTS extensions;"
	@PGPASSWORD=flexprice123 psql -h localhost -U flexprice -d flexprice -c "CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\" SCHEMA extensions;"
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
	@PGPASSWORD=flexprice123 psql -h localhost -U flexprice -d flexprice -f migrations/postgres/V1__seed.sql
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

.PHONY: generate-sdk generate-go-sdk generate-python-sdk generate-javascript-sdk

# Generate all SDKs
generate-sdk: generate-go-sdk generate-python-sdk generate-javascript-sdk
	@echo "All SDKs generated successfully"

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
	@echo "Python SDK generated successfully"

# Generate JavaScript SDK
generate-javascript-sdk: install-openapi-generator
	@echo "Generating JavaScript SDK..."
	@openapi-generator-cli generate \
		-i docs/swagger/swagger-3-0.json \
		-g javascript \
		-o api/javascript \
		--additional-properties=projectName=@flexprice/sdk \
		--git-repo-id=javascript-sdk \
		--git-user-id=flexprice \
		--global-property apiTests=false,modelTests=false
	@echo "JavaScript SDK generated successfully"

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
	@test -n "$(VERSION)" || (echo "Error: VERSION is required"; exit 1)
	@./scripts/ensure-act.sh
	@act workflow_dispatch -e .github/workflows/test-event.json -s GITHUB_TOKEN="$(shell cat .secrets.git)" -P ubuntu-latest=catthehacker/ubuntu:act-latest --container-architecture linux/amd64 --action-offline-mode

.PHONY: sdk-publish-js sdk-publish-py sdk-publish-go sdk-publish-all sdk-publish-all-with-version test-github-workflow
