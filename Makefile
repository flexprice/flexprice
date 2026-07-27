.PHONY: swagger-clean
swagger-clean:
	rm -rf docs/swagger

# Swag v2 pin. Use `go run` for generation so CI never runs a random host `swag` from PATH while
# expecting $(GOPATH)/bin/swag (install-swag skipped when `which swag` succeeds).
SWAG_V2_PKG := github.com/swaggo/swag/v2/cmd/swag@v2.0.0-rc5

.PHONY: install-swag
install-swag:
	go install $(SWAG_V2_PKG)

.PHONY: swagger
swagger: swagger-2-0 swagger-3-0

.PHONY: swagger-2-0-generate
swagger-2-0-generate:
	@echo "go mod download (warm cache; swag v2 treats go list stderr as fatal)..."
	go mod download
	@echo "Running swag via go run $(SWAG_V2_PKG) ..."
	go run $(SWAG_V2_PKG) init \
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

.PHONY: swagger-2-0-node
swagger-2-0-node:
	node scripts/fix_swagger_internal_types.mjs

.PHONY: swagger-2-0
swagger-2-0: swagger-2-0-generate swagger-fix-refs swagger-2-0-node

.PHONY: swagger-3-0
swagger-3-0:
	@echo "Converting Swagger 2.0 to OpenAPI 3.0..."
	@curl -X 'POST' \
		'https://converter.swagger.io/api/convert' \
		-H 'accept: application/json' \
		-H 'Content-Type: application/json' \
		-d @docs/swagger/swagger.json > docs/swagger/swagger-3-0.json
	@echo "Conversion complete. Output saved to docs/swagger/swagger-3-0.json"
	@node scripts/fix_swagger_internal_types.mjs
	@./scripts/update_swagger_servers.sh

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

.PHONY: run-e2eprobe
run-e2eprobe:
	go run cmd/e2eprobe/main.go

E2EPROBE_IMAGE ?= flexprice/e2eprobe

# Single-platform build loaded into the local Docker engine for testing.
# (buildx cannot --load a multi-arch manifest into the default image store.)
.PHONY: e2eprobe-docker-build
e2eprobe-docker-build:
	docker buildx build --load \
	  -f Dockerfile.e2eprobe -t $(E2EPROBE_IMAGE) .

# Multi-arch build pushed to a registry. Override the tag with
# E2EPROBE_IMAGE=<registry>/e2eprobe:<tag>. buildx discards a multi-platform
# result unless it is pushed, so --push is required here.
.PHONY: e2eprobe-docker-push
e2eprobe-docker-push:
	docker buildx build --push --platform linux/amd64,linux/arm64 \
	  -f Dockerfile.e2eprobe -t $(E2EPROBE_IMAGE) .

.PHONY: run-server-local
run-server-local: run-server

.PHONY: run
run: run-server

.PHONY: dev-token
dev-token:
	@tenant_id=''; \
	while [ -z "$$tenant_id" ]; do \
		printf 'Tenant ID: '; \
		if ! IFS= read -r tenant_id; then \
			printf '\nUnable to read tenant ID.\n'; \
			exit 1; \
		fi; \
		if [ -z "$$tenant_id" ]; then \
			echo 'Tenant ID is required.'; \
		fi; \
	done; \
	printf 'Environment ID (optional): '; \
	if ! IFS= read -r environment_id; then \
		printf '\nUnable to read environment ID.\n'; \
		exit 1; \
	fi; \
	go run ./scripts -cmd generate-dev-token -tenant-id "$$tenant_id" -environment-id "$$environment_id"

# ---------------------------------------------------------------------------
# Local development targets — load .env.local on top of .env so local Docker
# infra overrides take effect without touching production config.
# ---------------------------------------------------------------------------

# Run API server locally (loads .env then .env.local)
.PHONY: run-local-api
run-local-api:
	@set -a && [ -f .env ] && . ./.env; [ -f .env.local ] && . ./.env.local; set +a; \
	FLEXPRICE_DEPLOYMENT_MODE=api go run cmd/server/main.go

# Run Kafka consumer locally (loads .env then .env.local)
.PHONY: run-local-consumer
run-local-consumer:
	@set -a && [ -f .env ] && . ./.env; [ -f .env.local ] && . ./.env.local; set +a; \
	FLEXPRICE_DEPLOYMENT_MODE=consumer go run cmd/server/main.go

# Run all services in a single process locally (loads .env then .env.local)
.PHONY: run-local
run-local:
	@set -a && [ -f .env ] && . ./.env; [ -f .env.local ] && . ./.env.local; set +a; \
	FLEXPRICE_DEPLOYMENT_MODE=local go run cmd/server/main.go

# ---------------------------------------------------------------------------
# Worktree-aware server — deterministic port from branch name, shared .env
# ---------------------------------------------------------------------------

# Run server on a port derived from the current branch name (8080 for main,
# 8100-8899 for other branches). Loads .env from the main worktree so there
# is only ever one .env file regardless of how many worktrees are active.
.PHONY: run-wt
run-wt:
	@export PATH="/usr/local/go/bin:$$HOME/.local/bin:$$PATH"; \
	BRANCH=$$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo detached); \
	if [ "$$BRANCH" = "main" ] || [ "$$BRANCH" = "master" ]; then PORT=8080; \
	else PORT=$$((8100 + $$(printf '%s' "$$BRANCH" | cksum | awk '{print $$1}') % 800)); fi; \
	MAIN_WT=$$(git worktree list --porcelain | awk '/^worktree /{print $$2; exit}'); \
	ENV_FILE="$$MAIN_WT/.env"; \
	REPO_ROOT=$$(git rev-parse --show-toplevel); \
	printf '\n+---------------------------------------------+\n'; \
	printf '|  branch : %-34s|\n' "$$BRANCH"; \
	printf '|  port   : %-34s|\n' "$$PORT"; \
	printf '|  url    : %-34s|\n' "http://localhost:$$PORT"; \
	printf '|  .env   : %-34s|\n' "$$ENV_FILE"; \
	printf '+---------------------------------------------+\n\n'; \
	[ -f "$$ENV_FILE" ] || { printf 'ERROR: .env not found at %s\n' "$$ENV_FILE" >&2; exit 1; }; \
	set -a && . "$$ENV_FILE" && set +a; \
	export FLEXPRICE_SERVER_ADDRESS=":$$PORT"; \
	exec go run "$$REPO_ROOT/cmd/server/main.go"

# Show all worktrees, their computed ports, and whether a server is running.
# Calls scripts/wt-ports.sh from the main worktree (works before and after commit).
.PHONY: wt-ports
wt-ports:
	@MAIN_WT=$$(git worktree list --porcelain | awk '/^worktree /{print $$2; exit}'); \
	bash "$$MAIN_WT/scripts/wt-ports.sh"

# Run Ent schema migrations against local Docker postgres
.PHONY: migrate-local
migrate-local:
	@set -a && [ -f .env.local ] && . ./.env.local; set +a; \
	go run ./cmd/migrate postgres

.PHONY: test test-verbose test-coverage

# Run go test, stream output, then print a short failure summary at the end.
# Usage: $(call run-go-test,<go test args...>)
define run-go-test
	@bash -c 'set -o pipefail; \
	tmp=$$(mktemp -t flexprice-test.XXXXXX); \
	go test $(1) 2>&1 | tee "$$tmp"; \
	status=$$?; \
	echo ""; \
	echo "======== FAILED TESTS ========"; \
	grep -E "^--- FAIL:" "$$tmp" || echo "(none)"; \
	echo "======== FAILED PACKAGES ====="; \
	grep -E "^FAIL[[:space:]]" "$$tmp" || echo "(none)"; \
	rm -f "$$tmp"; \
	exit $$status'
endef

# Run all tests
test: install-typst
	$(call run-go-test,-v -race ./internal/...)

# Run tests with verbose output
test-verbose:
	$(call run-go-test,-v ./internal/...)

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
	@go run ./cmd/migrate postgres --timeout 300
	@echo "Ent migrations complete"

.PHONY: migrate-ent-dry-run
migrate-ent-dry-run:
	@echo "Generating SQL migration statements (dry run)..."
	@go run ./cmd/migrate postgres --dry-run --timeout 300
	@echo "SQL migration statements generated"

.PHONY: generate-migration
generate-migration:
	@echo "Generating SQL migration file..."
	@mkdir -p migrations/ent
	@go run ./cmd/migrate postgres --dry-run --timeout 300 > migrations/ent/migration_$(shell date +%Y%m%d%H%M%S).sql
	@echo "SQL migration file generated in migrations/ent/"

# Initialize databases and required topics
init-db: up migrate-postgres migrate-clickhouse generate-ent migrate-ent seed-db
	@echo "Database initialization complete"

# One-off: seed Svix event-types on a new/self-hosted env.
# Reads webhook.svix_config.base_url / auth_token (FLEXPRICE_WEBHOOK_SVIX_CONFIG_BASE_URL / FLEXPRICE_WEBHOOK_SVIX_CONFIG_AUTH_TOKEN).
# Usage: make seed-svix
.PHONY: seed-svix
seed-svix:
	@go run ./cmd/migrate svix

# Same, run inside the flexprice image (no local Go toolchain needed).
# Usage: make seed-svix-docker [IMAGE=flexprice-app:local]
IMAGE ?= flexprice-app:local
.PHONY: seed-svix-docker
seed-svix-docker:
	@docker run --rm --env-file .env --entrypoint ./migrate $(IMAGE) svix

# Run postgres migrations
migrate-postgres:
	@echo "Running Postgres migrations..."
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		if docker compose exec -T postgres pg_isready -U flexprice -d flexprice >/dev/null 2>&1; then \
			echo "Postgres is ready"; \
			docker compose exec -T postgres psql -U flexprice -d flexprice -c "CREATE SCHEMA IF NOT EXISTS extensions;"; \
			docker compose exec -T postgres psql -U flexprice -d flexprice -c "CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\" SCHEMA extensions;"; \
			echo "Postgres migrations complete"; \
			exit 0; \
		fi; \
		echo "Postgres not ready yet (attempt $$i/10), waiting 3s..."; \
		sleep 3; \
	done; \
	echo "Error: Postgres failed to become ready"; exit 1

# Run clickhouse migrations
migrate-clickhouse:
	@echo "Running Clickhouse migrations..."
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		if docker compose exec -T clickhouse clickhouse-client --user=flexprice --password=flexprice123 --database=flexprice --query "SELECT 1" >/dev/null 2>&1; then \
			echo "Clickhouse is ready"; \
			for file in migrations/clickhouse/*.sql; do \
				if [ -f "$$file" ]; then \
					echo "Running migration: $$file"; \
					docker compose exec -T clickhouse clickhouse-client --user=flexprice --password=flexprice123 --database=flexprice --multiquery < "$$file" || true; \
				fi; \
			done; \
			echo "Clickhouse migrations complete"; \
			exit 0; \
		fi; \
		echo "Clickhouse not ready yet (attempt $$i/10), waiting 3s..."; \
		sleep 3; \
	done; \
	echo "Error: Clickhouse failed to become ready"; exit 1

# Seed initial data
seed-db:
	@echo "Running Seed data migration..."
	@docker compose exec -T postgres psql -U flexprice -d flexprice -f /docker-entrypoint-initdb.d/V1__seed.sql
	@echo "Postgres seed data migration complete"

# Initialize kafka topics
.PHONY: init-kafka
init-kafka:
	@echo "Creating Kafka topics..."
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		echo "Attempt $$i: Checking if Kafka is ready..."; \
		if docker compose exec -T kafka kafka-topics --bootstrap-server kafka:9092 --list >/dev/null 2>&1; then \
			echo "Kafka is ready!"; \
			for topic in \
				events \
				events_lazy \
				events_dlq \
				events_backfill \
				events_post_processing \
				events_post_processing_backfill \
				system_events \
				wallet_alert \
				onboarding_events \
				staging_benchmarking \
				prod_events_v4 \
				staging_events_backfill \
				staging_events; do \
				echo "Creating topic: $$topic"; \
				docker compose exec -T kafka kafka-topics --create --if-not-exists \
					--bootstrap-server kafka:9092 \
					--topic $$topic \
					--partitions 1 \
					--replication-factor 1 \
					--config cleanup.policy=delete \
					--config retention.ms=604800000; \
			done; \
			echo "Kafka topics created successfully"; \
			exit 0; \
		fi; \
		echo "Kafka not ready yet, waiting 5s..."; \
		sleep 5; \
	done; \
	echo "Error: Kafka failed to become ready after 10 attempts"; \
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
	@make migrate-postgres migrate-clickhouse migrate-ent seed-db init-kafka
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
	@echo "🔑 Default API Key (for local testing):"
	@echo "   sk_local_flexprice_test_key"
	@echo "   (pass as: -H 'x-api-key: sk_local_flexprice_test_key')"
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
	@docker compose exec -T postgres psql -U flexprice -d flexprice < $(file)
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
	@echo "  make filter-mcp-spec     - Build tag-filtered OpenAPI spec for MCP (allowed tags in .speakeasy/mcp/allowed-tags.yaml)"
	@echo "  make update-sdk          - Regenerate swagger then run sdk-all"
	@echo "  make clean-sdk           - Remove generated api/go, api/typescript, api/python, api/mcp"
	@echo "  make merge-custom       - Copy api/custom/<lang>/ into api/<lang>/"
	@echo "  make sync-gen-to-output - Copy .speakeasy/gen/*.yaml to api/<lang>/.speakeasy/gen.yaml (run before generate)"
	@echo "  make show-custom-files  - List files in api/custom/"
	@echo ""
	@echo "Go SDK only:"
	@echo "  make go-sdk              - Clean + generate Go SDK + merge custom + build"
	@echo "  make regenerate-go-sdk   - Regenerate Go SDK (no clean) + merge custom"
	@echo "  make clean-go-sdk        - Remove api/go only"
	@echo ""
	@echo "SDK integration tests (published SDKs, api/tests):"
	@echo "  make test-sdk / test-sdks - Run all SDK tests (Go, Python, TypeScript) in one command"
	@echo ""
	@echo "White-label SDK:"
	@echo "  make check-wl-templates  - Verify WL templates match .speakeasy/gen/*.yaml"

# SDK publishing: done via GitHub Actions (.github/workflows/generate-sdks.yml). No api/publish.sh in repo.
sdk-publish-js sdk-publish-py sdk-publish-go sdk-publish-all sdk-publish-all-with-version:
	@echo "Publishing is done via the Generate SDKs workflow. Push to main or run workflow_dispatch on .github/workflows/generate-sdks.yml"; exit 1

# Test Generate SDKs workflow locally using act
test-github-workflow:
	@echo "Testing Generate SDKs workflow locally..."
	@./scripts/ensure-act.sh
	@if [ ! -f .secrets ] && [ ! -f .env ]; then \
		echo "Error: Create .secrets or .env with SPEAKEASY_API_KEY, SDK_DEPLOY_GIT_TOKEN, NPM_TOKEN, PYPI_TOKEN"; \
		exit 1; \
	fi
	@( [ -f .secrets ] && set -a && . ./.secrets && set +a ) || ( set -a && . ./.env && set +a ); \
	act workflow_dispatch -W .github/workflows/generate-sdks.yml \
	 -s SPEAKEASY_API_KEY="$${SPEAKEASY_API_KEY}" \
	 -s SDK_DEPLOY_GIT_TOKEN="$${SDK_DEPLOY_GIT_TOKEN}" \
	 -s NPM_TOKEN="$${NPM_TOKEN:-$$NPM_AUTH_TOKEN}" \
	 -s PYPI_TOKEN="$${PYPI_TOKEN:-$$PYPI_API_TOKEN}" \
	 -P ubuntu-latest=catthehacker/ubuntu:act-latest \
	 --container-architecture linux/amd64

.PHONY: test-github-workflow show-custom-files help-sdk check-wl-templates

# Check that white-label templates are in sync with .speakeasy/gen/*.yaml
# Run this after Speakeasy CLI updates gen configs (it auto-bumps fields)
# Note: workflow.yaml.tmpl is intentionally excluded — it has no .speakeasy/gen/ counterpart.
check-wl-templates:
	@bash -c '\
	  DRIFT=0; \
	  for lang in go typescript python mcp; do \
	    diff \
	      <(sed '"'"'s/$${WL_[^}]*}/PLACEHOLDER/g; s/"PLACEHOLDER"/PLACEHOLDER/g'"'"' configs/white-label/templates/$$lang.yaml.tmpl) \
	      <(sed \
	          -e '"'"'s/github\.com\/flexprice[^ ]*/PLACEHOLDER/g'"'"' \
	          -e '"'"'s/Flexprice/PLACEHOLDER/g'"'"' \
	          -e '"'"'s/FlexPrice/PLACEHOLDER/g'"'"' \
	          -e '"'"'s/flexprice/PLACEHOLDER/g'"'"' \
	          -e '"'"'s/"@PLACEHOLDER\/[^"]*"/PLACEHOLDER/g'"'"' \
	          .speakeasy/gen/$$lang.yaml) \
	      || { echo "DRIFT DETECTED: configs/white-label/templates/$$lang.yaml.tmpl is out of sync with .speakeasy/gen/$$lang.yaml — update the template to match"; DRIFT=1; }; \
	  done; \
	  [ "$$DRIFT" -eq 0 ] && echo "All white-label templates are in sync." || exit 1 \
	'

# =============================================================================
# Speakeasy SDK Generation (New Pipeline)
# =============================================================================
# Version is managed by Speakeasy (versioningStrategy: automatic in gen.yaml); do not pass --set-version.

.PHONY: speakeasy-install speakeasy-generate speakeasy-validate speakeasy-lint

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
# Allowed tags: .speakeasy/mcp/allowed-tags.yaml
.PHONY: filter-mcp-spec
filter-mcp-spec:
	@echo "Applying scope overlay to base spec..."
	@speakeasy overlay apply \
		-s docs/swagger/swagger-3-0.json \
		-o .speakeasy/overlays/scopes.yaml \
		> docs/swagger/swagger-3-0-with-scopes.yaml
	@echo "Converting YAML to JSON..."
	@python3 -c "import yaml, json; print(json.dumps(yaml.safe_load(open('docs/swagger/swagger-3-0-with-scopes.yaml')), indent=2))" \
		> docs/swagger/swagger-3-0-with-scopes.json 2>/dev/null || \
	(pip3 install --break-system-packages pyyaml > /dev/null 2>&1 && \
	 python3 -c "import yaml, json; print(json.dumps(yaml.safe_load(open('docs/swagger/swagger-3-0-with-scopes.yaml')), indent=2))" \
		> docs/swagger/swagger-3-0-with-scopes.json)
	@echo "Filtering spec by allowed tags..."
	@node scripts/filter-openapi-by-tags.mjs \
		--spec docs/swagger/swagger-3-0-with-scopes.json \
		--out docs/swagger/swagger-3-0-mcp.json
	@rm -f docs/swagger/swagger-3-0-with-scopes.yaml docs/swagger/swagger-3-0-with-scopes.json
	@echo "MCP spec created with scopes at docs/swagger/swagger-3-0-mcp.json"

# Copy central gen (.speakeasy/gen/*.yaml) into api/<lang>/.speakeasy/gen.yaml so Speakeasy CLI finds config.
.PHONY: sync-gen-to-output
sync-gen-to-output:
	@./scripts/sync-gen-to-output.sh

speakeasy-generate: speakeasy-validate filter-mcp-spec sync-gen-to-output
	@echo "Generating SDKs with Speakeasy..."
	@CI=true TERM=dumb speakeasy run --target all -y --skip-upload-spec --skip-compile --minimal

# =============================================================================
# Single command: Swagger + SDK/MCP generation + merge custom (no testing; use make test-sdk for integration tests)
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
	@echo "✅ SDK/MCP generation complete. (Use make test-sdk to run SDK integration tests.)"

# Load SPEAKEASY_API_KEY from .secrets then run sdk-all. Use this when running locally.
sdk-all-local:
	@if [ -f .secrets ]; then set -a && . ./.secrets && set +a; fi && $(MAKE) sdk-all

# =============================================================================
# Go SDK Generation with Speakeasy (Production Pipeline)
# =============================================================================

.PHONY: speakeasy-go-sdk merge-custom clean-go-sdk go-sdk regenerate-go-sdk sync-gen-to-output

# Generate Go SDK only with Speakeasy
speakeasy-go-sdk:
	@echo "🔨 Generating Go SDK with Speakeasy..."
	@bash -c 'set -o pipefail; CI=true TERM=dumb speakeasy run --target flexprice-go -y --skip-compile < /dev/null | cat'
	@echo "✓ Go SDK generated successfully"

# Clean only Go SDK
clean-go-sdk:
	@echo "🧹 Cleaning Go SDK..."
	@rm -rf api/go
	@echo "✓ Go SDK cleaned"

# Complete Go SDK pipeline: clean → validate → sync gen → generate → merge custom → build
go-sdk: clean-go-sdk speakeasy-validate sync-gen-to-output speakeasy-go-sdk merge-custom
	@echo "🧪 Testing Go SDK compilation..."
	@cd api/go && go mod tidy && go build ./...
	@echo "✅ Go SDK ready for publishing!"

# Quick regeneration (no clean, faster for development)
regenerate-go-sdk: sync-gen-to-output speakeasy-go-sdk merge-custom
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
	@if [ -f api/typescript/src/index.ts ] && [ -f api/typescript/src/index.extras.ts ]; then \
		node scripts/patch-ts-sdk-index.mjs; \
	fi
	@echo "✓ Custom merge complete"

# Force MCP package name so npm publish uses @flexprice/mcp-server.
.PHONY: fix-mcp-package-name
fix-mcp-package-name:
	@if [ -f api/mcp/package.json ]; then \
		jq '.name = "@flexprice/mcp-server"' api/mcp/package.json > api/mcp/package.json.tmp && mv api/mcp/package.json.tmp api/mcp/package.json; \
		echo "✓ MCP package name set to @flexprice/mcp-server"; \
	fi

# =============================================================================
# SDK tests: single command runs all SDKs (published integration tests)
# =============================================================================
# Require FLEXPRICE_API_KEY and FLEXPRICE_API_HOST.
# Dependencies are installed automatically before each test run.
.PHONY: test-sdk test-sdks

# Run all SDK integration tests (Go, Python, TypeScript). Installs deps first to avoid missing-package issues.
# Requires FLEXPRICE_API_KEY and FLEXPRICE_API_HOST to be set (export them so tests can call the API).
test-sdk test-sdks:
	@if [ -z "$$FLEXPRICE_API_KEY" ] || [ -z "$$FLEXPRICE_API_HOST" ]; then \
		echo ""; \
		echo "❌ SDK tests need API credentials. Set and export:"; \
		echo "   export FLEXPRICE_API_KEY=\"your-api-key\""; \
		echo "   export FLEXPRICE_API_HOST=\"us.api.flexprice.io/v1\"   # or localhost:8080/v1 for local"; \
		echo ""; \
		exit 1; \
	fi
	@echo "Running SDK tests (Go, Python, TypeScript)..."
	@echo "  FLEXPRICE_API_HOST=$$FLEXPRICE_API_HOST"
	@echo "--- Go (install deps + test) ---"; (cd api/tests/go && GOPRIVATE=github.com/flexprice/* go mod tidy && GOPRIVATE=github.com/flexprice/* go mod download && GOPRIVATE=github.com/flexprice/* go run -tags published test_sdk.go) || true
	@echo "--- Python (install deps + test) ---"; (cd api/tests/python && \
		PY=; \
		for c in python3.13 python3.12 python3.11 python3.10 python3; do \
			if command -v $$c >/dev/null 2>&1 && $$c -c 'import sys; sys.exit(0 if sys.version_info>=(3,10) else 1)' 2>/dev/null; then PY=$$c; break; fi; \
		done; \
		if [ -z "$$PY" ]; then \
			echo "❌ Python 3.10+ required (PyPI flexprice). macOS: brew install python@3.12  (then re-run; we try python3.12 … python3.10 before python3)"; \
			exit 1; \
		fi; \
		if [ ! -d .venv ] || ! [ -x .venv/bin/python ] || ! .venv/bin/python -c 'import sys; sys.exit(0 if sys.version_info>=(3,10) else 1)' 2>/dev/null || ! .venv/bin/python -m pip --version >/dev/null 2>&1; then \
			rm -rf .venv && $$PY -m venv .venv; \
		fi && \
		echo "  using $$(.venv/bin/python --version)" && \
		.venv/bin/python -m pip install -q --upgrade pip setuptools wheel && \
		.venv/bin/python -m pip install -q -r requirements.txt && \
		.venv/bin/python test_sdk.py) || true
	@echo "--- TypeScript (install deps + test) ---"; (cd api/tests/ts && npm install && npm test) || true
	@echo "✓ All SDK tests finished"

# Run the orchestrated sanity integration test suite.
# Usage:
#   export FLEXPRICE_API_KEY=sk_...
#   make test-suite
# Host defaults to localhost:8080/v1 (http for localhost, https for remote).
test-suite:
	@if [ -z "$$FLEXPRICE_API_KEY" ]; then \
		echo ""; \
		echo "❌ Need an API key:"; \
		echo "   export FLEXPRICE_API_KEY=sk_..."; \
		echo "   make test-suite"; \
		echo ""; \
		exit 1; \
	fi
	@cd integration-testing-suite/go && go run .

.PHONY: sdk-all test-sdk test-sdks test-suite

# -----------------------------------------------------------------------
# Loglint — ctx-first logger enforcement
# -----------------------------------------------------------------------

.PHONY: build-loglint lint lint-ci

## build-loglint: compile the loglint vettool binary into tools/bin/
build-loglint:
	cd tools/loglint && go mod download && go build -o ../bin/loglint ./main.go

## lint: run loglint on internal/ (warnings + errors, non-blocking)
lint: build-loglint
	go vet -vettool=./tools/bin/loglint ./internal/... ; echo "Loglint complete"

## lint-ci: run loglint on internal/ (errors only — LL008 warnings ignored)
lint-ci: build-loglint
	@out=$$(go vet -vettool=./tools/bin/loglint ./internal/... 2>&1 | grep -v "^#" | grep -v "warning:"); if [ -n "$$out" ]; then echo "$$out"; exit 1; fi
