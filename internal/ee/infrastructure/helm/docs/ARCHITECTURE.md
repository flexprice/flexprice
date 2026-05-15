# FlexPrice Helm — Deployment Architecture

This chart deploys three application workloads plus a migration job. Stateful
backing services (Postgres, Kafka, ClickHouse, Redis, Temporal) are **out of
scope for production** and expected to be externally managed.

## In-cluster vs external

```
┌──────────────────────────── Kubernetes cluster ─────────────────────────────┐
│                                                                              │
│   ┌────────────┐    ┌─────────────┐    ┌──────────────┐                      │
│   │  api       │    │  consumer   │    │  worker      │                      │
│   │  Deployment│    │  Deployment │    │  Deployment  │                      │
│   │  + HPA+PDB │    │  + HPA      │    │  + HPA(opt)  │                      │
│   └─────┬──────┘    └──────┬──────┘    └──────┬───────┘                      │
│         │ HTTP             │ Kafka            │ Temporal                     │
│         │ +                │ + DB             │ + DB                         │
│         │ DB+CH+Redis      │ + ClickHouse     │                              │
│         ▼                  ▼                  ▼                              │
│   ┌──────────────────────────────────────────────────────────────────────┐   │
│   │  Service (ClusterIP) — flexprice-api:80 → :8080                       │   │
│   └──────────────────────────────────────────────────────────────────────┘   │
│         ▲                                                                    │
│         │ HTTPS via cert-manager                                             │
│   ┌─────┴──────┐                                                             │
│   │  Ingress   │                                                             │
│   │  (nginx)   │                                                             │
│   └────────────┘                                                             │
│                                                                              │
│   Helm pre-install / pre-upgrade hook ─────────────────────────────────┐     │
│   ┌───────────────────────────────────────────────────────────────┐    │     │
│   │  migration Job → Postgres schema, ClickHouse schema, Kafka    │    │     │
│   │  topics, seed data                                            │    │     │
│   └───────────────────────────────────────────────────────────────┘    │     │
│                                                                        │     │
│   Helm post-install / post-upgrade hook ───────────────────────────────┤     │
│   ┌───────────────────────────────────────────────────────────────┐    │     │
│   │  temporal-bootstrap Job → register Temporal namespaces        │    │     │
│   └───────────────────────────────────────────────────────────────┘    │     │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
                                  │       │       │       │       │
              (external endpoints, configured via values-prod.yaml)
                                  │       │       │       │       │
                          ┌───────┘       │       │       │       └───────┐
                          ▼               ▼       ▼       ▼               ▼
                     ┌─────────┐   ┌──────────┐ ┌────┐ ┌────────┐ ┌──────────────┐
                     │  RDS    │   │ MSK /    │ │CH  │ │Elasti- │ │ Temporal     │
                     │ Postgres│   │ Confluent│ │Cloud│ │Cache   │ │ Cloud / self │
                     │         │   │ Kafka    │ │     │ │Redis   │ │  hosted      │
                     └─────────┘   └──────────┘ └────┘ └────────┘ └──────────────┘
```

## Component responsibilities

| Workload   | Deployment mode env             | Responsibilities                                                   |
|------------|----------------------------------|--------------------------------------------------------------------|
| `api`      | `FLEXPRICE_DEPLOYMENT_MODE=api`  | HTTP API surface; reads/writes Postgres + ClickHouse, queries Redis. |
| `consumer` | `FLEXPRICE_DEPLOYMENT_MODE=consumer` | Kafka consumer for `events`, `events_lazy`, `events_post_processing`; writes ClickHouse. |
| `worker`   | `FLEXPRICE_DEPLOYMENT_MODE=temporal_worker` | Temporal worker; runs billing/invoice workflows + activities.       |

## Local dev topology

When `values-local.yaml` is layered on top, bundled subcharts are flipped on:

```
[ api ][ consumer ][ worker ]
       │     │     │
       └──┬──┴──┬──┘
          ▼     ▼
   ┌────────────────┐  ┌──────┐  ┌──────┐  ┌──────────┐
   │ bitnami/       │  │bitnami│  │bitnami│  │temporalio│
   │ postgresql     │  │/kafka │  │/redis │  │/temporal │
   └────────────────┘  └──────┘  └──────┘  └──────────┘
   + hand-rolled ClickHouse StatefulSet (clickhouse.mode: standalone)
```

Not a supported production topology — no backups, HA, or rolling upgrades.

## Hook ordering

| Phase                   | Hook                          | Weight | Resource                                             |
|-------------------------|-------------------------------|-------:|------------------------------------------------------|
| pre-install/pre-upgrade | migration                     |     -5 | [`templates/jobs/migration.yaml`](../flexprice/templates/jobs/migration.yaml) |
| install/upgrade         | all Deployments + Services    |      — | api, consumer, worker, ingress, …                    |
| post-install/post-upgrade | temporal-namespace-bootstrap |      5 | [`templates/jobs/temporal-namespace-bootstrap.yaml`](../flexprice/templates/jobs/temporal-namespace-bootstrap.yaml) |
| test                    | api health probe              |      — | [`templates/tests/test-api-health.yaml`](../flexprice/templates/tests/test-api-health.yaml) |

## Configuration sources, in resolution order

1. `internal/config/config.yaml` (image-baked defaults).
2. Environment variables `FLEXPRICE_*` — set by the chart via the `flexprice.env` helper.
3. `.env` file (only used during local dev outside Kubernetes).

The chart's `templates/_helpers.tpl` is the canonical mapping from Helm values
to env vars. Every key in `values.yaml` either becomes an env var here or feeds
into a manifest field (image, resources, replicas, etc.).
