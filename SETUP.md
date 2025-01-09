# FlexPrice Setup Guide

## Prerequisites

- Go 1.23.0 or later
- Docker and Docker Compose
- Make
- PostgreSQL client (psql)
- Git

## Local Development Setup

1. Clone the repository:
```bash
git clone https://github.com/flexprice/flexprice
cd flexprice
```

2. Set up the local environment:
```bash
make setup-local
```

This command will:
- Start all required Docker containers (PostgreSQL, ClickHouse, Kafka, Redis)
- Run database migrations for PostgreSQL and ClickHouse
- Seed initial data
- Create required Kafka topics

3. Verify the setup:
```bash
# Check Docker containers
docker compose ps

# Verify PostgreSQL connection
psql -h localhost -U flexprice -d flexprice

# Check Kafka UI
open http://localhost:8084
```

## Development Credentials

### PostgreSQL
- Host: localhost
- Port: 5432
- Database: flexprice
- Username: flexprice
- Password: flexprice123

### ClickHouse
- Host: localhost
- Port: 9000
- Database: flexprice
- Username: flexprice
- Password: flexprice123

### Kafka
- Bootstrap Server: localhost:29092
- UI: http://localhost:8084

## API Documentation

The API documentation is available in OpenAPI 3.0 format at `docs/swagger/swagger-3-0.json`.

### Setting up Postman Collection

1. Open Postman
2. Click on "Import" in the top left
3. Select "Import File"
4. Choose `docs/swagger/swagger-3-0.json`
5. Click "Import"
6. Create a new environment for local development:
   - Name: Local
   - Variable: `baseUrl`
   - Initial Value: `http://localhost:8080/v1`
   - Current Value: `http://localhost:8080/v1`

   - Variable: `apiKey`
   - Initial Value: `0cc505d7b917e0b1f25ccbea029dd43f4002edfea46b7f941f281911246768fe`
   - Current Value: `0cc505d7b917e0b1f25ccbea029dd43f4002edfea46b7f941f281911246768fe`

## Common Development Tasks

### Reset Local Environment
```bash
make clean-docker
make setup-local
```

### Run Migrations
```bash
make migrate-postgres
make migrate-clickhouse
```

### Seed Data
```bash
make seed-db
```

### Initialize Kafka Topics
```bash
make init-kafka
```

## Troubleshooting

### Database Connection Issues
1. Ensure Docker containers are running:
```bash
docker compose ps
```

2. Check database logs:
```bash
docker compose logs postgres
docker compose logs clickhouse
```

### Kafka Issues
1. Verify Kafka is running:
```bash
docker compose logs kafka
```

2. Check topic list:
```bash
docker compose exec kafka kafka-topics --bootstrap-server kafka:9092 --list
```

3. View Kafka UI at http://localhost:8084

## Additional Resources

- [Go Documentation](https://golang.org/doc/)
- [PostgreSQL Documentation](https://www.postgresql.org/docs/)
- [ClickHouse Documentation](https://clickhouse.com/docs/)
