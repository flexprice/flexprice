# Secrets

Every credential the chart needs is fetched from a single Kubernetes Secret,
named via `secrets.existingSecret` in your values overrides. The chart never
templates plaintext credentials into a manifest — it only references this
Secret by name.

> If `secrets.existingSecret` is unset, the chart renders its own Secret from
> plaintext fields in `values.yaml`. **Suitable for local dev only.**

## Minimum Secret (copy-paste)

The fastest way to get an install working — every key the chart looks up:

```bash
kubectl create namespace flexprice
kubectl create secret generic flexprice-secrets -n flexprice \
  --from-literal=encryption-key="$(openssl rand -hex 32)" \
  --from-literal=auth-secret="$(openssl rand -hex 32)" \
  --from-literal=postgres-password='REPLACE-WITH-PG-PW' \
  --from-literal=clickhouse-password='REPLACE-WITH-CH-PW' \
  --from-literal=kafka-sasl-password='REPLACE-WITH-KAFKA-PW' \
  --from-literal=redis-password='REPLACE-WITH-REDIS-PW' \
  --from-literal=temporal-api-key='REPLACE-WITH-TEMPORAL-KEY'
```

Then set in your values:

```yaml
secrets:
  existingSecret: flexprice-secrets
```

If you don't need a particular feature, you can omit its key — the chart
only references keys for components that are enabled.

## Key inventory

| Key                             | Required when                                                    | What it is                                                  |
|---------------------------------|------------------------------------------------------------------|-------------------------------------------------------------|
| `encryption-key`                | always                                                           | Symmetric key for at-rest sensitive-field encryption (hex/64). |
| `auth-secret`                   | `auth.provider=flexprice` (default)                              | JWT signing secret.                                         |
| `postgres-password`             | always (external Postgres or bundled subchart)                   | Postgres user password.                                     |
| `clickhouse-password`           | always                                                           | ClickHouse user password.                                   |
| `kafka-sasl-password`           | `kafkaConfig.useSASL=true`                                       | Kafka SCRAM password (MSK IAM not supported — see below).   |
| `redis-password`                | `redisConfig.auth.enabled=true` *or* external Redis with auth   | Redis password.                                             |
| `temporal-api-key`              | `temporalConfig.external.enabled=true` and Temporal Cloud        | Temporal Cloud API key.                                     |
| `supabase-service-key`          | `auth.provider=supabase`                                         | Supabase service-role JWT.                                  |
| `sentry-dsn`                    | `sentry.enabled=true`                                            | Sentry project DSN.                                         |
| `pyroscope-basic-auth-password` | `pyroscope.enabled=true` with basic auth                         | Pyroscope password.                                         |
| `logging-otel-auth-value`       | `logging.otel.enabled=true` with auth                            | OTLP exporter header value (e.g. SigNoz ingestion key).     |
| `email-resend-api-key`          | `email.enabled=true`                                             | Resend API key.                                             |
| `svix-auth-token`               | `webhook.svixConfig.enabled=true`                                | Svix auth token.                                            |
| `stripe-api-key`                | `integrations.stripe.enabled=true`                               | Stripe secret key.                                          |
| `chargebee-api-key`             | `integrations.chargebee.enabled=true`                            | Chargebee API key.                                          |
| `razorpay-api-secret`           | `integrations.razorpay.enabled=true`                             | Razorpay secret.                                            |
| `quickbooks-client-secret`      | `integrations.quickbooks.enabled=true`                           | QuickBooks OAuth client secret.                             |
| `hubspot-api-key`               | `integrations.hubspot.enabled=true`                              | HubSpot API key.                                            |
| `gemini-api-key`                | `gemini.apiKey` referenced                                       | Google Gemini API key.                                      |

The canonical list lives in [`flexprice/templates/_helpers.tpl`](../flexprice/templates/_helpers.tpl)
under each `secretKeyRef.key:` reference. Audit before promoting changes.

## MSK / Kafka SASL note

`kafka-sasl-password` is **only** the SCRAM password. **MSK IAM SASL is not
supported by the app today** — `internal/kafka/base.go` only handles
`SCRAM-SHA-256/512`. Provision MSK with SCRAM users:

```bash
# In MSK console: Cluster → Secret manager configuration → Associate secrets
# Create the SCRAM secret in AWS Secrets Manager:
aws secretsmanager create-secret \
  --name AmazonMSK_flexprice \
  --secret-string '{"username":"flexprice","password":"REPLACE-WITH-MSK-PW"}'

# Then mirror the password into the Kubernetes secret as kafka-sasl-password.
```

## External Secrets Operator (recommended for prod)

Don't keep production credentials in plain Kubernetes Secrets. Use
[external-secrets.io](https://external-secrets.io) to sync from a secret
manager.

Example for **AWS Secrets Manager**:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ClusterSecretStore
metadata:
  name: aws-secrets-manager
spec:
  provider:
    aws:
      service: SecretsManager
      region: us-east-1
      auth:
        jwt:
          serviceAccountRef:
            name: external-secrets
            namespace: external-secrets
---
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: flexprice-secrets
  namespace: flexprice
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secrets-manager
    kind: ClusterSecretStore
  target:
    name: flexprice-secrets
    creationPolicy: Owner
  data:
    - secretKey: encryption-key
      remoteRef: { key: flexprice/prod/encryption-key }
    - secretKey: postgres-password
      remoteRef: { key: flexprice/prod/rds-password }
    - secretKey: clickhouse-password
      remoteRef: { key: flexprice/prod/clickhouse-password }
    # … and so on for each key in your inventory
```

After the ExternalSecret reconciles, the `flexprice-secrets` Secret exists in
the namespace and the chart consumes it normally.

Equivalent stores exist for **GCP Secret Manager**, **Azure Key Vault**, and
**HashiCorp Vault** — see external-secrets.io docs for the provider blocks.

## Sealed Secrets alternative

If you can't run External Secrets Operator,
[Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets) lets you
commit encrypted secrets to git:

```bash
kubeseal --format yaml < flexprice-secrets.yaml > flexprice-secrets-sealed.yaml
```

Commit `flexprice-secrets-sealed.yaml`; the controller decrypts it into the
real `flexprice-secrets` at apply time.

## Rotating a credential

1. Update the upstream (RDS user password, ClickHouse user, etc.).
2. Update the Kubernetes Secret:
   ```bash
   kubectl -n flexprice patch secret flexprice-secrets \
     -p='{"stringData":{"postgres-password":"NEW-PW"}}'
   ```
3. Restart the pods so they re-read the env:
   ```bash
   kubectl -n flexprice rollout restart deploy/flexprice-api deploy/flexprice-consumer deploy/flexprice-worker
   ```

Pods re-read the Secret at process startup, **not** continuously. Without a
rollout, the running pods keep the old password and start failing once the
upstream rotates the credential.

## Postgres TLS verify-full (advanced)

`sslmode: require` (the default in `values-prod.example.yaml`) does TLS but
**does not verify** the server certificate. For `sslmode: verify-full` you
need the RDS CA bundle inside each pod. The app doesn't yet wire a DSN-level
`sslrootcert` parameter, so use the chart's `extraVolumes` /
`extraVolumeMounts`:

```bash
# 1. Pull the AWS RDS global CA bundle into a ConfigMap
curl -O https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem
kubectl -n flexprice create configmap rds-ca \
  --from-file=ca-bundle.pem=global-bundle.pem
```

```yaml
# 2. Mount it on every workload via the chart's passthrough values
extraVolumes:
  - name: rds-ca
    configMap:
      name: rds-ca
extraVolumeMounts:
  - name: rds-ca
    mountPath: /etc/ssl/certs/rds-ca.pem
    subPath: ca-bundle.pem
    readOnly: true
```

The Go Postgres driver picks up the system trust store at
`/etc/ssl/certs/`, so this is enough to make `sslmode: verify-full` work
once the app is patched to honour it. Until then, stick with
`sslmode: require` — you still get TLS in transit; you just don't verify the
hostname.

## What you should NOT put in the Secret

These belong in `values.yaml` (or `values-prod.yaml`), not in a Secret:

- Hostnames (`postgres.host`, `kafkaConfig.brokers`, ...)
- Database/topic/namespace names
- Pool sizes, timeouts, log levels
- `auth.provider`, `auth.apiKey.header`

The Secret is for **opaque credentials only**. Everything else is config.
