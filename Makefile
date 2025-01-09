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
test:
	go test ./...

# Run tests with verbose output
test-verbose:
	go test -v ./...

# Run tests with coverage report
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Database related targets
.PHONY: init-db migrate-postgres migrate-clickhouse seed-db

# Initialize databases and required topics
init-db: up migrate-postgres migrate-clickhouse seed-db
	@echo "Database initialization complete"

# Run postgres migrations
migrate-postgres:
	@echo "Running Postgres migrations..."
	@sleep 5  # Wait for postgres to be ready
	@PGPASSWORD=flexprice123 psql -h localhost -U flexprice -d flexprice -c "CREATE SCHEMA IF NOT EXISTS extensions;"
	@PGPASSWORD=flexprice123 psql -h localhost -U flexprice -d flexprice -c "CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\" SCHEMA extensions;"
	@echo "Running V0 migration..."
	@PGPASSWORD=flexprice123 psql -h localhost -U flexprice -d flexprice -f migrations/postgres/V0__init.sql
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
