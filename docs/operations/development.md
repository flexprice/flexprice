# Developing Flexprice locally

Commands and environment notes for running the backend. The rules for writing code are in [AGENTS.md](../../AGENTS.md); this page is only about operating the stack.

## Toolchain

Go version from `go.mod`. `make test` installs `typst` into `~/.local/bin` through `scripts/install-typst.sh` (needs network). Add `$HOME/.local/bin` to `PATH`.

## Two ways to run

1. **Self-contained local Docker** (default). The committed `.env.local` points every dependency at local containers. No secrets needed.
2. **Against shared staging infra** ([REMOTE_DEV_INSTANCE_SETUP.md](../REMOTE_DEV_INSTANCE_SETUP.md)). A `./.env` (chmod 600, never committed) holds staging credentials and `make run-server` uses it.

The targets differ in which env file they load, and that decides which database you touch:

| Target | Loads | Use for |
|---|---|---|
| `make run-server`, `make migrate-ent` | `.env` only | staging or whatever `.env` points at |
| `make run-local*`, `make migrate-local` | `.env`, then `.env.local` on top | local Docker |

**Always `make migrate-local` for local Ent migrations.** `make migrate-ent` reads `.env`, which can point at production.

## Bring up the local stack

```bash
docker compose up -d postgres kafka clickhouse temporal temporal-ui
make migrate-postgres        # extensions
make migrate-clickhouse      # migrations/clickhouse/*
make migrate-local           # Ent schema, from .env.local
make seed-db                 # default tenant and environment
make init-kafka              # topics
make run-local               # API + consumer + workers in one process
curl http://localhost:8080/health   # {"status":"ok"}
```

Auth for `/v1/*` locally: `-H "x-api-key: sk_local_flexprice_test_key" -H "x-environment-id: 00000000-0000-0000-0000-000000000000"`.

`make dev-setup` does the Docker-based setup end to end; `make up` / `make down` start and stop everything through Compose, and `make restart-flexprice` restarts only the Flexprice services.

### Caveats

- Temporal is off by default locally (`FLEXPRICE_TEMPORAL_ENABLED="false"` in `.env.local`). Turn it on, and start the container, only when testing workflows.
- `make init-kafka` does not create the webhook topic `flexprice_system_events`; without it the server logs a harmless reconnect loop. Create it once:
  `docker compose exec -T kafka kafka-topics --bootstrap-server kafka:9092 --create --if-not-exists --topic flexprice_system_events --partitions 1 --replication-factor 1`.
- Tests need no infrastructure: `internal/testutil` provides in-memory stores.

## Deployment modes

`FLEXPRICE_DEPLOYMENT_MODE`: `local` (everything in one process), `api`, `consumer`, `temporal_worker`, `admin` (admin portal API only; `/v1` needs `FLEXPRICE_ADMIN_SECRET`; `make run-local-admin`). Docker Compose runs `flexprice-api`, `flexprice-consumer` and `flexprice-worker` separately.

## Configuration

Viper, later sources win: `internal/config/config.yaml`, then `FLEXPRICE_*` environment variables (`FLEXPRICE_POSTGRES_HOST` sets `postgres.host`), then `.env` via godotenv.

## Everyday commands

| Task | Command |
|---|---|
| Run one suite | `go test -race ./internal/ee/service -run TestWalletServiceSuite` |
| Everything CI checks on your branch | `make check-changed` (`LINT_BASE=origin/develop` by default) |
| Full test suite | `make test`, `make test-coverage` |
| Logging lint, whole repo | `make lint-ci` |
| Ent code from schema | `make generate-ent` |
| Migration SQL for production | `make generate-migration` |
| Swagger 2.0 and OpenAPI 3.0 into `docs/swagger/` | `make swagger` |
| SDKs and MCP from the spec | `make swagger && make sdk-all` ([api/README.md](../../api/README.md)) |
| PostgreSQL shell | `docker compose exec postgres psql -U flexprice -d flexprice` |
| ClickHouse shell | `docker compose exec clickhouse clickhouse-client --user=flexprice --password=flexprice123 --database=flexprice` |
| Service logs | `docker compose logs -f flexprice-api` (or `-consumer`, `-worker`) |

Local URLs: API `http://localhost:8080`, Temporal UI `http://localhost:8088`, Kafka UI `http://localhost:8084` (`docker compose --profile dev up -d kafka-ui`), ClickHouse HTTP `http://localhost:8123`. SDK base URLs include `/v1`.

Every ClickHouse query is bounded by a hardcoded `max_memory_usage` of 90 GB.

## Integration sanity suite

`integration-testing-suite/go` runs an end-to-end billing lifecycle against a running server (`make test-suite`). Mint a DB-backed key first; it embeds the environment:

```bash
curl -s -X POST localhost:8080/v1/auth/signup -H 'Content-Type: application/json' \
  -d '{"email":"dev@example.com","password":"password12345","tenant_name":"Dev"}'
# GET /v1/environments with the JWT -> env id, then:
curl -s -X POST localhost:8080/v1/secrets/api/keys -H "Authorization: Bearer <token>" \
  -H "X-Environment-ID: <env>" -H 'Content-Type: application/json' \
  -d '{"name":"suite","type":"private_key"}'
FLEXPRICE_API_KEY=<sk_...> FLEXPRICE_API_HOST=localhost:8080/v1 make test-suite
```

The suite fails only on core (Phase 1–5) failures. Phase 7 cleanup failures are reported but non-fatal; the Phase 4 usage wait may time out on a cold consumer. The suite pins the latest Go SDK, whose Go requirement may be newer than the local toolchain; let Go download it (do not force `GOTOOLCHAIN=local`).

## Cursor Cloud VM

Docker is installed but no service manager runs it. Once per session:

```bash
sudo dockerd > /tmp/dockerd.log 2>&1 &   # wait ~5s
sudo chmod 666 /var/run/docker.sock
```

The daemon uses `fuse-overlayfs` with the containerd snapshotter off (`/etc/docker/daemon.json`); the VM kernel lacks overlay2, so leave that alone. Then follow [Bring up the local stack](#bring-up-the-local-stack). `make lint` is non-blocking there; `make lint-ci` is the gate.

## Knowledge graph

When `graphify-out/graph.json` exists, `graphify query "<question>"`, `graphify path "<A>" "<B>"` and `graphify explain "<concept>"` answer codebase questions faster than browsing; `graphify update .` refreshes it after structural changes. Orientation docs: [REPO_MAP.md](../REPO_MAP.md), [ARCHITECTURE.md](../ARCHITECTURE.md), [FLOWS/](../FLOWS/).
