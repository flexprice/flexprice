# Backup & Restore

This chart does not include a backup story — backups belong out-of-band
because they're stateful, retention-policy-driven, and very
deployment-specific. This doc lists the recommended strategies per
backing service.

## Postgres

### Managed (RDS / Cloud SQL / Azure DB)

Use the cloud provider's automated backups. Default settings:
- **RDS**: Settings → Modify → Backup retention 7+ days, snapshot every
  24h. Enable point-in-time-recovery (PITR).
- **Cloud SQL**: `automatedBackups: { enabled: true, startTime: "02:00",
  retainedBackups: 30 }`.
- **Azure DB for PostgreSQL**: built-in PITR up to 35 days.

Nothing to wire in the chart.

### Self-hosted Postgres (bundled bitnami subchart — NOT recommended for prod)

If you're running the bundled subchart for some reason, schedule pgdump to
S3 via a CronJob:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: flexprice-pgdump
  namespace: flexprice
spec:
  schedule: "0 2 * * *"           # daily at 02:00 UTC
  successfulJobsHistoryLimit: 7
  failedJobsHistoryLimit: 3
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          serviceAccountName: flexprice-backup    # has S3 write perms
          containers:
            - name: pgdump
              image: postgres:15.3-alpine
              env:
                - name: PGHOST
                  value: flexprice-postgresql
                - name: PGUSER
                  value: flexprice
                - name: PGPASSWORD
                  valueFrom:
                    secretKeyRef: { name: flexprice-secrets, key: postgres-password }
                - name: PGDATABASE
                  value: flexprice
                - name: S3_BUCKET
                  value: flexprice-backups-prod
              command:
                - /bin/sh
                - -ec
                - |
                  DATE=$(date -u +%Y%m%dT%H%M%S)
                  pg_dump --format=custom --no-owner --no-acl \
                    | aws s3 cp - s3://$S3_BUCKET/postgres/flexprice-$DATE.dump
```

For restore:

```bash
aws s3 cp s3://flexprice-backups-prod/postgres/flexprice-20260601T020000.dump /tmp/dump
pg_restore --clean --if-exists -d "postgresql://flexprice:$PW@host:5432/flexprice" /tmp/dump
```

## ClickHouse

### ClickHouse Cloud

Automatic. Per-org configurable retention. Nothing to wire.

### Self-hosted (Altinity or chart standalone)

Use [clickhouse-backup](https://github.com/Altinity/clickhouse-backup) as a
sidecar or CronJob. Altinity ships an
[official sidecar pattern](https://github.com/Altinity/clickhouse-operator/tree/master/docs/chi-examples)
that uploads to S3.

Quick CronJob skeleton:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: flexprice-clickhouse-backup
  namespace: flexprice
spec:
  schedule: "0 3 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          serviceAccountName: flexprice-backup
          containers:
            - name: ch-backup
              image: altinity/clickhouse-backup:2.5.10
              env:
                - name: CLICKHOUSE_HOST
                  value: flexprice-clickhouse
                - name: CLICKHOUSE_USER
                  value: default
                - name: CLICKHOUSE_PASSWORD
                  valueFrom:
                    secretKeyRef: { name: flexprice-secrets, key: clickhouse-password }
                - name: S3_BUCKET
                  value: flexprice-backups-prod
                - name: S3_PATH
                  value: clickhouse/
                - name: REMOTE_STORAGE
                  value: s3
              args: ["create_remote", "scheduled-$(date -u +%Y%m%dT%H%M%S)"]
```

ClickHouse backups are part-level, not row-level — restore replaces
partitions atomically. Expect tens of GB to multi-TB depending on event
retention.

## Kafka

You do **not** back up Kafka topics — they're a transient event log, not a
system of record. The system of record is Postgres + ClickHouse.

If you need to replay events for a debug scenario, MSK / Confluent Cloud
let you configure topic retention up to "forever" — set
`kafkaConfig.routeTenantsOnLazyMode` and `event_post_processing_backfill`
to control replay topics.

## Redis

Pure cache. No backup.

If you somehow keep production state in Redis (don't), use ElastiCache /
MemoryDB automatic snapshots.

## Temporal

### Temporal Cloud

Automatic. Closed workflow executions retained per the namespace's
retention setting (default 72h).

### Self-hosted Temporal

Back up the Temporal Postgres (separate from your FlexPrice Postgres if
you split them). The visibility store is denormalised — you don't strictly
need to back it up, but doing so speeds up DR.

## Test your restore quarterly

Untested backups are not backups. Schedule:

1. Take a fresh dump → restore into a staging cluster.
2. Verify row counts match production within ±0.1%.
3. Run the chart's `helm test flexprice -n flexprice-rc` against the
   restored cluster.
4. Document RTO (time to fully operational) and RPO (max data loss
   window) — these go in your DR plan.

[`docs/PRE-SHIP-VALIDATION.md`](PRE-SHIP-VALIDATION.md) §6 walks through
the DR gate as part of the release checklist.

## Encryption at rest

The chart's `secrets.encryption-key` (set via the Kubernetes Secret)
encrypts sensitive fields at the application layer **before** they hit
Postgres. **Lose this key, lose those fields** — back it up
independently:

```bash
aws secretsmanager create-secret \
  --name flexprice/prod/encryption-key \
  --secret-string "$ENCRYPTION_KEY" \
  --kms-key-id alias/aws/secretsmanager
```

The encryption key should be rotated rarely (or never) and stored
out-of-band from your normal credential rotation cycle.
