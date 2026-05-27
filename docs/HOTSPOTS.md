# Hotspot ledger

Identified structural risk areas backed by observable signals (size, global state, multiplicity of dependents, infra sensitivity). Severity is **engineering judgment**, not a runtime metric dashboard.

Legend: **Impact radius** estimates blast radius across teams/features.

---

## 1. Megaclass service files (`internal/service/subscription.go`, `invoice.go`, `billing.go`)

| Attribute | Evidence |
| --------- | ------- |
| **Why risky** | Multi-thousand-line files concentrate subscription math, entitlement transitions, payment coupling, Temporal triggers, coupon/tax/proration intersections — high cognitive load + merge/conflict churn. |
| **Impact radius** | Entire revenue stack (subscriptions + invoices + usage rating + integrations). |
| **Suggested improvements** | Extract cohesive subdomains (e.g., renewal engine, amendment engine, consolidation jobs) behind internal packages with explicit interfaces; add characterization tests along seams before slicing. |

---

## 2. `ServiceParams` dependency hub (`internal/service/factory.go`)

| Attribute | Evidence |
| --------- | ------- |
| **Why risky** | Every new repo/publisher/pubsub knob tends to accumulate in one god struct; hides real dependencies of individual services. |
| **Impact radius** | All service constructors via Fx wiring. |
| **Suggested improvements** | Introduce narrower parameter structs grouped by bounded context; migrate new services away from embedding full `ServiceParams` when feasible. |

---

## 3. Global Temporal singleton (`InitializeGlobalTemporalService` / `GetGlobalTemporalService`)

| Attribute | Evidence |
| --------- | ------- |
| **Why risky** | Cross-layer hidden dependency; impedes deterministic tests; hides lifecycle ownership (startup only in Fx path). Dispersed usages across handlers, deep services, activities. |
| **Impact radius** | Any workflow-starting pathway (billing, ingestion side effects, customer ops). |
| **Suggested improvements** | Prefer injecting `TemporalService` interface via constructors on new/edited flows; converge existing call sites gradually. |

---

## 4. Temporal registration duplication (`internal/temporal/registration.go`)

| Attribute | Evidence |
| --------- | ------- |
| **Why risky** | Spins up local `service.New*` instances (e.g., plan/export/task services) in addition to Fx graph — duplication risk divergent configuration or missed wiring updates. |
| **Impact radius** | All workflow/activity executions. |
| **Suggested improvements** | Where safe, reuse Fx-provided singletons rather than reconstructing graphs; alternatively isolate a **`temporal.DependencyBundle`** mirrored from Fx. |

---

## 5. Giant HTTP adapters (`internal/api/router.go`, `internal/api/v1/webhook.go`)

| Attribute | Evidence |
| --------- | ------- |
| **Why risky** | Routing tables and provider-specific webhook multiplexing swell over time → missed auth or mis-wired RBAC regressions harder to grep-review. |
| **Impact radius** | External surface area security + SLA of webhook ingestion. |
| **Suggested improvements** | Modular route registration per subdomain; table-driven webhook dispatch with exhaustive tests adding new providers. |

---

## 6. Webhook outbound pipeline coupling (`internal/webhook/payload` + downstream services)

| Attribute | Evidence |
| --------- | ------- |
| **Why risky** | Payload assembly touches many aggregate readers; schema drift silently breaks receivers. Heavy reliance on ancillary services raises fan-out coupling. |
| **Impact radius** | Customer integrations, partner automation, auditing. |
| **Suggested improvements** | Version payloads explicitly (schema semver or compatibility tests); constrain builder surface to DTO adapters. |

---

## 7. Generated Ent layer (`internal/repository/ent/**`)

| Attribute | Evidence |
| --------- | ------- |
| **Why risky** | Easy to mistakenly hand-edit regenerated files; merges can produce subtle runtime mismatches versus schema snapshots. Risky DDL from schema drift if migrations not staged carefully. |
| **Impact radius** | All transactional persistence paths. |
| **Suggested improvements** | Always edit schema source; enforce CI diff checks on regenerated code; segregate destructive migrations with expand/contract playbook. |

---

## 8. Kafka topic / consumer-group matrix (`internal/config/config.yaml` + registrations)

| Attribute | Evidence |
| --------- | ------- |
| **Why risky** | Multiple consumer sections (`events`, `events_lazy`, `events_post_processing`, `wallet_alert`, benchmarking, onboarding, transform pipelines) multiply misconfiguration probabilities (wrong group ID → duplicate processing). |
| **Impact radius** | Metering accuracy, downstream billing completeness, alerting. |
| **Suggested improvements** | Centralize naming constants; CI validation asserting expected handler ↔ topic coupling; dashboards for lag per documented pair. |

---

## 9. Feature usage tracker complexity (`internal/service/feature_usage_tracking.go` reported large + Temporal pings)

| Attribute | Evidence |
| --------- | ------- |
| **Why risky** | Computation + ClickHouse interplay + Temporal scheduling intertwined — difficult to replay deterministically vs raw events repository. |
| **Impact radius** | Entitlements, usage limits, alerting, dashboards, exports depending on aggregates. |
| **Suggested improvements** | Split read vs write pathways; crystallize deterministic recompute jobs with idempotency markers. |

---

## 10. Enterprise layer drift risk (`internal/ee/**`)

| Attribute | Evidence |
| --------- | ------- |
| **Why risky** | Divergent forks of OSS behavior can desynchronize public expectations if not guarded by build tags / Fx wiring clarity. Licensing boundary mistakes are legal + operational liabilities. |
| **Impact radius** | Commercial installs + upstream merges. |
| **Suggested improvements** | Keep EE patches minimal overlays; enforce module boundaries (`//go:build` if applicable) aligned with README licensing notes. |

---

## Architecture violation smells to watch during review

| Smell | Example direction |
| ----- | ----------------- |
| Domain importing repository | Circular build / broken layering |
| API handler querying Ent client | Bypasses transactional service rules |
| New global mutable singleton | Complicates concurrency + tests |
| Direct cross-aggregate updates skipping service | Bypass hooks (auditing/webhooks/schedules) |

---

## Updating this ledger

Append new hotspots rather than overwriting historical context unless risks are eliminated. Prefer linking to commits/PR IDs when retiring an item.
