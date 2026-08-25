{{/*
Expand the name of the chart.
*/}}
{{- define "flexprice.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "flexprice.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "flexprice.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "flexprice.labels" -}}
helm.sh/chart: {{ include "flexprice.chart" . }}
{{ include "flexprice.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.labels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "flexprice.selectorLabels" -}}
app.kubernetes.io/name: {{ include "flexprice.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Object labels for a component's Kubernetes resources (Deployment, Service, HPA,
PDB, Ingress, ServiceAccount, ...).
Usage: include "flexprice.componentLabels" (dict "ctx" . "component" "api")

Emits the common labels, then operator-supplied labels from
`.Values.<component>.labels`, then the component key.

The component key goes last so it can't be overridden from values — several
resources (PDB, NetworkPolicy, Service) build selectors from it, and a Service
whose object labels disagree with the pods it targets is a debugging trap. The
remaining common labels stay overridable, so an operator who really wants their
own `app.kubernetes.io/version` can still set it.

Never use this for `spec.selector.matchLabels`: selectors are immutable on
Deployments and StatefulSets, so a user adding a label would break every
subsequent `helm upgrade` with "field is immutable". Selectors stay on
`flexprice.selectorLabels` + the component key.
*/}}
{{- define "flexprice.componentLabels" -}}
{{- $ctx := .ctx -}}
{{- /* `dig` can't walk .Values (chartutil.Values, not map[string]interface{}),
       so resolve the component block with `index` and guard it being unset. */ -}}
{{- $cfg := index $ctx.Values .component | default dict -}}
{{ include "flexprice.labels" $ctx }}
{{- with (get $cfg "labels") }}
{{- toYaml . | nindent 0 }}
{{ end }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{/*
Pod template labels for a component.
Usage: include "flexprice.componentPodLabels" (dict "ctx" . "component" "api")

Global `.Values.podLabels`, then `.Values.<component>.podLabels`, and finally the
selector labels + component key. Pod labels are mutable, so adding one here only
triggers a normal rolling update. This is the hook log shippers (Filebeat/ELK,
Fluent Bit, Datadog) should target, since they enrich from pod metadata.

The selector-owned keys (app.kubernetes.io/name, /instance, /component) are
emitted LAST on purpose. YAML resolves duplicate keys to the final occurrence, so
this makes them impossible to override from values: a user who sets
`podLabels.app.kubernetes.io/component` gets their value silently discarded
rather than a pod that no longer matches its own Deployment selector, Service,
PDB, and NetworkPolicy. Reordering these lines reintroduces that bug.
*/}}
{{- define "flexprice.componentPodLabels" -}}
{{- $ctx := .ctx -}}
{{- $cfg := index $ctx.Values .component | default dict -}}
{{- with $ctx.Values.podLabels }}
{{- toYaml . | nindent 0 }}
{{ end }}
{{- with (get $cfg "podLabels") }}
{{- toYaml . | nindent 0 }}
{{ end }}
{{- include "flexprice.selectorLabels" $ctx }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{/*
Default ServiceAccount name (shared across components when per-workload SAs are off).
*/}}
{{- define "flexprice.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "flexprice.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Per-component ServiceAccount name.
Usage: include "flexprice.componentServiceAccountName" (dict "ctx" . "component" "api")

When serviceAccount.perComponent=true, returns "<fullname>-<component>" (or the
component-specific override under serviceAccount.components.<component>.name).
Otherwise falls back to the shared flexprice.serviceAccountName so the chart
keeps working for operators who don't want per-workload IAM (IRSA) separation.
*/}}
{{- define "flexprice.componentServiceAccountName" -}}
{{- $ctx := .ctx -}}
{{- $component := .component -}}
{{- if $ctx.Values.serviceAccount.perComponent -}}
  {{- $override := dig "components" $component "name" "" $ctx.Values.serviceAccount -}}
  {{- if $override -}}
    {{- $override -}}
  {{- else -}}
    {{- printf "%s-%s" (include "flexprice.fullname" $ctx) $component -}}
  {{- end -}}
{{- else -}}
  {{- include "flexprice.serviceAccountName" $ctx -}}
{{- end -}}
{{- end }}

{{/*
Resolve the Secret name holding FlexPrice credentials.
If secrets.existingSecret is set, the user is responsible for creating and populating
that Secret out-of-band (kubectl, ExternalSecrets, SOPS, Vault, etc). In that case the
chart renders no Secret and only references this name from every secretKeyRef.
If secrets.existingSecret is empty, the chart renders its own Secret named "<fullname>-secrets"
from plaintext values — suitable for dev only.
*/}}
{{- define "flexprice.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret }}
{{- else -}}
{{- printf "%s-secrets" (include "flexprice.fullname" .) }}
{{- end -}}
{{- end }}

{{/*
Name of the chart-managed Retain StorageClass.
Returns "" when data protection is disabled — callers should guard with
`{{- if .Values.dataProtection.retainStorage }}` before using the value
as a storageClass field.
*/}}
{{- define "flexprice.retainStorageClassName" -}}
{{- if and .Values.dataProtection .Values.dataProtection.retainStorage .Values.dataProtection.storageClass.provisioner -}}
{{- if .Values.dataProtection.storageClass.name -}}
{{- .Values.dataProtection.storageClass.name -}}
{{- else -}}
{{- printf "%s-retain" (include "flexprice.fullname" .) -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/*
Pick the storageClass for an in-cluster DB component.
Usage: include "flexprice.componentStorageClass" (dict "ctx" . "override" .Values.postgres.internal.persistence.storageClass)
Precedence: explicit per-component override > chart Retain SC > "" (cluster default).
*/}}
{{- define "flexprice.componentStorageClass" -}}
{{- if .override -}}
{{- .override -}}
{{- else -}}
{{- include "flexprice.retainStorageClassName" .ctx -}}
{{- end -}}
{{- end }}

{{/*
Render `helm.sh/resource-policy: keep` annotation key/value when data protection
is enabled. Intended to be embedded under `metadata.annotations:` of resources
that own data (StatefulSets, PVCs, the StorageClass itself). When the feature
is disabled this renders nothing — keep the parent `annotations:` block
guarded with `{{- if ... }}` or otherwise tolerant of an empty mapping.
*/}}
{{- define "flexprice.keepAnnotation" -}}
{{- if and .Values.dataProtection .Values.dataProtection.retainStorage -}}
helm.sh/resource-policy: keep
{{- end -}}
{{- end }}

{{/*
Migration helper-image reference.
Usage: include "flexprice.migrationImage" (dict "ctx" . "name" "postgres")
       name ∈ {postgres, busybox, clickhouse}
Produces: <registry>/<repository>:<tag>  (registry optional)
Backwards-compatible with the old hardcoded refs when the values block is unset.
*/}}
{{- define "flexprice.migrationImage" -}}
{{- $img := dig "images" .name dict .ctx.Values.migration -}}
{{- $registry := default "" $img.registry -}}
{{- $repo := default "" $img.repository -}}
{{- $tag := default "" $img.tag -}}
{{- if $registry -}}{{- printf "%s/%s:%s" $registry $repo $tag -}}
{{- else -}}{{- printf "%s:%s" $repo $tag -}}
{{- end -}}
{{- end }}

{{/*
Resolve the PostgreSQL host.
Uses the bitnami/postgresql subchart service name when internal, or the
user-supplied host when external.

Under the config-autobind shape (.Values.env set), the address is declared as
env.FLEXPRICE_POSTGRES_HOST and that is what the application reads. Templates
that cannot go through the env block — the migration Job's init containers and
the legacy ConfigMap — resolve the address through this helper instead, so
prefer the autobind value when it is present and fall back to postgres.host.

Without this, the address has to be declared TWICE in every values file and the
two copies silently disagree the moment one is updated: the app connects fine
while the migration Job loops "[n/60] Not ready yet..." against a dead IP. Every
region currently mirrors the value by hand; asia-south1 even carries a comment
telling the next person to keep them in sync. This makes the mirror unnecessary
— a values file may still set postgres.host, and it is used only when the
autobind key is absent, so existing regions render unchanged.
*/}}
{{- define "flexprice.postgresHost" -}}
{{- if and .Values.env (index .Values.env "FLEXPRICE_POSTGRES_HOST") -}}
{{- index .Values.env "FLEXPRICE_POSTGRES_HOST" -}}
{{- else if .Values.postgres.external.enabled -}}
{{- .Values.postgres.host }}
{{- else -}}
{{- printf "%s-postgresql" .Release.Name }}
{{- end -}}
{{- end }}

{{/*
Resolve the PostgreSQL port. Same autobind precedence as the host above.
*/}}
{{- define "flexprice.postgresPort" -}}
{{- if and .Values.env (index .Values.env "FLEXPRICE_POSTGRES_PORT") -}}
{{- index .Values.env "FLEXPRICE_POSTGRES_PORT" | toString -}}
{{- else if .Values.postgres.external.enabled -}}
{{- .Values.postgres.port | toString }}
{{- else -}}
5432
{{- end -}}
{{- end }}

{{/*
Resolve the Kafka brokers string.
Uses the bitnami/kafka subchart service name when internal, or the user-supplied brokers when external.
*/}}
{{- define "flexprice.kafkaBrokers" -}}
{{- if .Values.kafkaConfig.external.enabled -}}
{{- join "," .Values.kafkaConfig.brokers }}
{{- else -}}
{{- printf "%s-kafka:9092" .Release.Name }}
{{- end -}}
{{- end }}

{{/*
Resolve the Redis host.
Uses the bitnami/redis subchart service name when internal, or the user-supplied host when external.
*/}}
{{- define "flexprice.redisHost" -}}
{{- if .Values.redisConfig.external.enabled -}}
{{- .Values.redisConfig.host }}
{{- else -}}
{{- printf "%s-redis-master" .Release.Name }}
{{- end -}}
{{- end }}

{{/*
Resolve the Redis port.
*/}}
{{- define "flexprice.redisPort" -}}
{{- if .Values.redisConfig.external.enabled -}}
{{- .Values.redisConfig.port | toString }}
{{- else -}}
6379
{{- end -}}
{{- end }}

{{/*
Resolve the ClickHouse address (host:port) based on clickhouse.mode.
  standalone — ClusterIP Service rendered by this chart: <fullname>-clickhouse:9000
  altinity   — Altinity operator creates: chi-<fullname>-flexprice-0-0:9000
  external   — user-supplied clickhouse.address
*/}}
{{- define "flexprice.clickhouseAddress" -}}
{{- if eq .Values.clickhouse.mode "external" -}}
{{- .Values.clickhouse.address }}
{{- else if eq .Values.clickhouse.mode "altinity" -}}
{{- /* Namespace-qualified when clickhouse.namespace is set, matching what the
       standalone branch below already does via flexprice.clickhouseServiceHost.
       The operator's Service lives in the ClickHouse namespace, so the bare
       name only resolves for consumers in that same namespace — the migration
       Job runs in the release namespace and failed with
       "nc: bad address 'chi-flexprice-flexprice-0-0'". */ -}}
{{- $chSvc := printf "chi-%s-flexprice-0-0" (include "flexprice.fullname" .) -}}
{{- if .Values.clickhouse.namespace -}}
{{- printf "%s.%s.svc.cluster.local:9000" $chSvc .Values.clickhouse.namespace }}
{{- else -}}
{{- printf "%s:9000" $chSvc }}
{{- end -}}
{{- else -}}
{{- printf "%s:9000" (include "flexprice.clickhouseServiceHost" .) }}
{{- end -}}
{{- end }}

{{/*
Namespace the in-cluster ClickHouse (standalone/altinity) is deployed into.
Defaults to the release namespace for backward compatibility; set
clickhouse.namespace to isolate ClickHouse in its own namespace.
*/}}
{{- define "flexprice.clickhouseNamespace" -}}
{{- .Values.clickhouse.namespace | default .Release.Namespace -}}
{{- end }}

{{/*
Service host for the standalone ClickHouse. When clickhouse.namespace is set,
returns the cross-namespace FQDN so consumers in other namespaces (app,
migration job) resolve it; otherwise the short in-namespace service name
(zero-diff from previous behavior).
*/}}
{{- define "flexprice.clickhouseServiceHost" -}}
{{- $svc := printf "%s-clickhouse" (include "flexprice.fullname" .) -}}
{{- if .Values.clickhouse.namespace -}}
{{- printf "%s.%s.svc.cluster.local" $svc .Values.clickhouse.namespace -}}
{{- else -}}
{{- $svc -}}
{{- end -}}
{{- end }}

{{/*
Resolve the Temporal address.
Uses the temporalio/temporal subchart service name when internal, or user-supplied address when external.
*/}}
{{- define "flexprice.temporalAddress" -}}
{{- if .Values.temporalConfig.external.enabled -}}
{{- .Values.temporalConfig.address }}
{{- else -}}
{{- printf "%s-temporal-frontend:7233" .Release.Name }}
{{- end -}}
{{- end }}

{{/*
Kafka broker CA volume. Renders nothing unless kafkaConfig.tlsCASecret.name is
set, so clusters using publicly-trusted brokers (MSK, Confluent Cloud) are
untouched.

The Secret is expected to already exist — created by an ExternalSecret, sealed
secret, or by hand. The chart never renders certificate material itself, which
keeps PEM bytes out of values files. Its `key` must hold a PEM bundle; JKS/PKCS12
truststores are not readable by Go and must be exported with
`keytool -exportcert -rfc` first.
*/}}
{{- define "flexprice.kafkaTLSVolume" -}}
{{- if .Values.kafkaConfig.tlsCASecret.name }}
- name: kafka-tls-ca
  secret:
    secretName: {{ .Values.kafkaConfig.tlsCASecret.name | quote }}
    items:
      - key: {{ .Values.kafkaConfig.tlsCASecret.key | default "ca.crt" | quote }}
        path: {{ .Values.kafkaConfig.tlsCASecret.key | default "ca.crt" | quote }}
{{- end }}
{{- end }}

{{/*
Mount for the Kafka broker CA. Path must match FLEXPRICE_KAFKA_TLS_CA_CERT_FILE
in "flexprice.env".
*/}}
{{- define "flexprice.kafkaTLSVolumeMounts" -}}
{{- if .Values.kafkaConfig.tlsCASecret.name }}
- name: kafka-tls-ca
  mountPath: {{ .Values.kafkaConfig.tlsCASecret.mountPath | default "/etc/flexprice/kafka-tls" | quote }}
  readOnly: true
{{- end }}
{{- end }}

{{/*
Create environment variables from configuration.
All service addresses are resolved via named templates above so this block stays clean.
*/}}
{{- define "flexprice.env" -}}
{{- if .Values.env }}
{{- /* Generic env map — MIGRATED environments (config-autobind final-goal shape). The app
       reads the baked config.yaml for defaults; every per-env override is an explicit env var
       in .Values.env, and every secret is a name->secret-key entry in .Values.secretEnv. No
       per-key template maintenance. When .Values.env is set the ConfigMap is not rendered. */}}
{{- range $k, $v := .Values.env }}
- name: {{ $k }}
  value: {{ $v | quote }}
{{- end }}
{{- range $name, $key := .Values.secretEnv }}
- name: {{ $name }}
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" $ }}
      key: {{ $key }}
{{- end }}
{{- else }}
{{- /* Legacy structured env — UN-MIGRATED environments (unchanged; ConfigMap still rendered). */ -}}
- name: FLEXPRICE_SERVER_ADDRESS
  value: ":8080"
{{- /* ---- PostgreSQL ---- */}}
- name: FLEXPRICE_POSTGRES_HOST
  value: {{ include "flexprice.postgresHost" . | quote }}
- name: FLEXPRICE_POSTGRES_PORT
  value: {{ include "flexprice.postgresPort" . | quote }}
- name: FLEXPRICE_POSTGRES_USER
  value: {{ .Values.postgres.user | quote }}
- name: FLEXPRICE_POSTGRES_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: postgres-password
- name: FLEXPRICE_POSTGRES_DBNAME
  value: {{ .Values.postgres.dbname | quote }}
- name: FLEXPRICE_POSTGRES_SSLMODE
  value: {{ if .Values.postgres.external.enabled }}{{ .Values.postgres.sslmode | quote }}{{ else }}"disable"{{ end }}
- name: FLEXPRICE_POSTGRES_MAX_OPEN_CONNS
  value: {{ .Values.postgres.maxOpenConns | quote }}
- name: FLEXPRICE_POSTGRES_MAX_IDLE_CONNS
  value: {{ .Values.postgres.maxIdleConns | quote }}
- name: FLEXPRICE_POSTGRES_CONN_MAX_LIFETIME_MINUTES
  value: {{ .Values.postgres.connMaxLifetimeMinutes | quote }}
- name: FLEXPRICE_POSTGRES_AUTO_MIGRATE
  value: {{ .Values.postgres.autoMigrate | quote }}
{{- if .Values.postgres.readerHost }}
- name: FLEXPRICE_POSTGRES_READER_HOST
  value: {{ .Values.postgres.readerHost | quote }}
- name: FLEXPRICE_POSTGRES_READER_PORT
  value: {{ .Values.postgres.readerPort | default "5432" | quote }}
{{- end }}
{{- /* ---- ClickHouse ---- */}}
- name: FLEXPRICE_CLICKHOUSE_ADDRESS
  value: {{ include "flexprice.clickhouseAddress" . | quote }}
- name: FLEXPRICE_CLICKHOUSE_USERNAME
  value: {{ .Values.clickhouse.username | quote }}
- name: FLEXPRICE_CLICKHOUSE_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: clickhouse-password
- name: FLEXPRICE_CLICKHOUSE_DATABASE
  value: {{ .Values.clickhouse.database | quote }}
- name: FLEXPRICE_CLICKHOUSE_TLS
  value: {{ .Values.clickhouse.tls | quote }}
- name: FLEXPRICE_CLICKHOUSE_TLS_SKIP_VERIFY
  value: {{ .Values.clickhouse.tlsSkipVerify | default false | quote }}
- name: FLEXPRICE_CLICKHOUSE_MAX_MEMORY_USAGE
  value: {{ .Values.clickhouse.maxMemoryUsageGB | default 90 | quote }}
{{- /* ---- Kafka ---- */}}
- name: FLEXPRICE_KAFKA_BROKERS
  value: {{ include "flexprice.kafkaBrokers" . | quote }}
- name: FLEXPRICE_KAFKA_CONSUMER_GROUP
  value: {{ .Values.kafkaConfig.consumerGroup | quote }}
- name: FLEXPRICE_KAFKA_TOPIC
  value: {{ .Values.kafkaConfig.topic | quote }}
- name: FLEXPRICE_KAFKA_TOPIC_LAZY
  value: {{ .Values.kafkaConfig.topicLazy | quote }}
{{- /*
  topic_bulk has no default in the Go config (unlike topic/topic_lazy, which are
  `validate:"required"` and fail fast when unset). An empty TopicBulk therefore
  passes validation and only surfaces when a batched-ingest publish is attempted
  against topic "", so emit it only when configured rather than emitting an
  empty string.
*/}}
{{- with .Values.kafkaConfig.topicBulk }}
- name: FLEXPRICE_KAFKA_TOPIC_BULK
  value: {{ . | quote }}
{{- end }}
- name: FLEXPRICE_KAFKA_TLS
  value: {{ .Values.kafkaConfig.tls | quote }}
{{- /*
  Private/self-signed broker CA. Not gated on useSASL — a plain-TLS broker can
  present a private CA too. The path must match the mount in
  `flexprice.kafkaTLSVolumeMounts`. The file must be PEM; a JKS truststore has
  to be exported first with `keytool -exportcert -rfc`.
*/}}
{{- if .Values.kafkaConfig.tlsCASecret.name }}
- name: FLEXPRICE_KAFKA_TLS_CA_CERT_FILE
  value: {{ printf "%s/%s" (.Values.kafkaConfig.tlsCASecret.mountPath | default "/etc/flexprice/kafka-tls") (.Values.kafkaConfig.tlsCASecret.key | default "ca.crt") | quote }}
{{- end }}
- name: FLEXPRICE_KAFKA_USE_SASL
  value: {{ .Values.kafkaConfig.useSASL | quote }}
- name: FLEXPRICE_KAFKA_CLIENT_ID
  value: {{ .Values.kafkaConfig.clientId | quote }}
{{- if .Values.kafkaConfig.useSASL }}
- name: FLEXPRICE_KAFKA_SASL_MECHANISM
  value: {{ .Values.kafkaConfig.saslMechanism | quote }}
- name: FLEXPRICE_KAFKA_SASL_USER
  value: {{ .Values.kafkaConfig.saslUser | quote }}
{{- /*
  OAUTHBEARER (e.g. GCP Managed Kafka) carries no static password — the token
  comes from a sarama AccessTokenProvider (Application Default Credentials under
  Workload Identity). Only wire the password secretKeyRef for password-based
  mechanisms (PLAIN / SCRAM); otherwise the env references a kafka-sasl-password
  key that secret.yaml never renders (it gates on .saslPassword), which fails
  the pod with CreateContainerConfigError.
*/}}
{{- if ne .Values.kafkaConfig.saslMechanism "OAUTHBEARER" }}
- name: FLEXPRICE_KAFKA_SASL_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: kafka-sasl-password
{{- end }}
{{- end }}
{{- /* ---- Redis ---- */}}
- name: FLEXPRICE_REDIS_HOST
  value: {{ include "flexprice.redisHost" . | quote }}
- name: FLEXPRICE_REDIS_PORT
  value: {{ include "flexprice.redisPort" . | quote }}
- name: FLEXPRICE_REDIS_DB
  value: {{ .Values.redisConfig.db | default 0 | quote }}
- name: FLEXPRICE_REDIS_USE_TLS
  value: {{ .Values.redisConfig.useTLS | default false | quote }}
- name: FLEXPRICE_REDIS_TIMEOUT
  value: {{ .Values.redisConfig.timeout | default "5s" | quote }}
{{- if .Values.redisConfig.password }}
- name: FLEXPRICE_REDIS_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: redis-password
{{- end }}
{{- /* ---- Temporal ---- */}}
- name: FLEXPRICE_TEMPORAL_ENABLED
  value: {{ .Values.temporalConfig.enabled | quote }}
- name: FLEXPRICE_TEMPORAL_ADDRESS
  value: {{ include "flexprice.temporalAddress" . | quote }}
- name: FLEXPRICE_TEMPORAL_TASK_QUEUE
  value: {{ .Values.temporalConfig.taskQueue | quote }}
- name: FLEXPRICE_TEMPORAL_NAMESPACE
  value: {{ .Values.temporalConfig.namespace | quote }}
- name: FLEXPRICE_TEMPORAL_TLS
  value: {{ .Values.temporalConfig.tls | quote }}
{{- if .Values.temporalConfig.apiKey }}
- name: FLEXPRICE_TEMPORAL_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: temporal-api-key
{{- end }}
{{- /* ---- Auth ---- */}}
- name: FLEXPRICE_LOGGING_LEVEL
  value: {{ .Values.logging.level | quote }}
- name: FLEXPRICE_AUTH_PROVIDER
  value: {{ .Values.auth.provider | quote }}
- name: FLEXPRICE_AUTH_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: auth-secret
{{- if eq .Values.auth.provider "supabase" }}
- name: FLEXPRICE_AUTH_SUPABASE_BASE_URL
  value: {{ .Values.auth.supabase.baseUrl | quote }}
- name: FLEXPRICE_AUTH_SUPABASE_SERVICE_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: supabase-service-key
{{- end }}
- name: FLEXPRICE_AUTH_API_KEY_HEADER
  value: {{ .Values.auth.apiKey.header | quote }}
{{- if .Values.auth.apiKey.keys }}
- name: FLEXPRICE_AUTH_API_KEY_KEYS
  value: {{ .Values.auth.apiKey.keys | toJson | quote }}
{{- end }}
{{- /* ---- Billing ---- */}}
{{- if .Values.billing.tenantId }}
- name: FLEXPRICE_BILLING_TENANT_ID
  value: {{ .Values.billing.tenantId | quote }}
{{- end }}
{{- if .Values.billing.environmentId }}
- name: FLEXPRICE_BILLING_ENVIRONMENT_ID
  value: {{ .Values.billing.environmentId | quote }}
{{- end }}
{{- /* ---- Observability ---- */}}
- name: FLEXPRICE_SENTRY_ENABLED
  value: {{ .Values.app.observability.sentry.enabled | quote }}
{{- if .Values.app.observability.sentry.enabled }}
- name: FLEXPRICE_SENTRY_DSN
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: sentry-dsn
- name: FLEXPRICE_SENTRY_ENVIRONMENT
  value: {{ .Values.app.observability.sentry.environment | quote }}
- name: FLEXPRICE_SENTRY_SAMPLE_RATE
  value: {{ .Values.app.observability.sentry.sampleRate | quote }}
{{- end }}
- name: FLEXPRICE_PYROSCOPE_ENABLED
  value: {{ .Values.app.observability.pyroscope.enabled | quote }}
{{- if .Values.app.observability.pyroscope.enabled }}
- name: FLEXPRICE_PYROSCOPE_SERVER_ADDRESS
  value: {{ .Values.app.observability.pyroscope.serverAddress | quote }}
- name: FLEXPRICE_PYROSCOPE_APPLICATION_NAME
  value: {{ .Values.app.observability.pyroscope.applicationName | quote }}
- name: FLEXPRICE_PYROSCOPE_SAMPLE_RATE
  value: {{ .Values.app.observability.pyroscope.sampleRate | quote }}
- name: FLEXPRICE_PYROSCOPE_DISABLE_GC_RUNS
  value: {{ .Values.app.observability.pyroscope.disableGCRuns | quote }}
{{- if .Values.app.observability.pyroscope.basicAuthUser }}
- name: FLEXPRICE_PYROSCOPE_BASIC_AUTH_USER
  value: {{ .Values.app.observability.pyroscope.basicAuthUser | quote }}
- name: FLEXPRICE_PYROSCOPE_BASIC_AUTH_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: pyroscope-basic-auth-password
{{- end }}
{{- end }}
{{- /* ---- S3 ---- */}}
{{- if .Values.s3.enabled }}
- name: FLEXPRICE_S3_ENABLED
  value: "true"
- name: FLEXPRICE_S3_REGION
  value: {{ .Values.s3.region | quote }}
- name: FLEXPRICE_S3_INVOICE_BUCKET
  value: {{ .Values.s3.invoice.bucket | quote }}
- name: FLEXPRICE_S3_INVOICE_PRESIGN_EXPIRY_DURATION
  value: {{ .Values.s3.invoice.presignExpiryDuration | quote }}
{{- end }}
{{- /* ---- Cache ---- */}}
{{- if .Values.cache.enabled }}
- name: FLEXPRICE_CACHE_ENABLED
  value: "true"
{{- end }}
{{- /* ---- Encryption ---- */}}
{{- if .Values.secrets.encryptionKey }}
- name: FLEXPRICE_SECRETS_ENCRYPTION_KEY
  value: {{ .Values.secrets.encryptionKey | quote }}
{{- else }}
- name: FLEXPRICE_SECRETS_ENCRYPTION_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: encryption-key
{{- end }}
{{- /* ---- Email ---- */}}
{{- if .Values.app.email.enabled }}
- name: FLEXPRICE_EMAIL_ENABLED
  value: "true"
- name: FLEXPRICE_EMAIL_RESEND_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: email-resend-api-key
- name: FLEXPRICE_EMAIL_FROM_ADDRESS
  value: {{ .Values.app.email.fromAddress | quote }}
- name: FLEXPRICE_EMAIL_REPLY_TO
  value: {{ .Values.app.email.replyTo | quote }}
- name: FLEXPRICE_EMAIL_CALENDAR_URL
  value: {{ .Values.app.email.calendarUrl | quote }}
{{- end }}
{{- /* ---- Event processing ---- */}}
- name: FLEXPRICE_EVENT_PROCESSING_TOPIC
  value: {{ .Values.eventProcessing.topic | quote }}
- name: FLEXPRICE_EVENT_PROCESSING_RATE_LIMIT
  value: {{ .Values.eventProcessing.rateLimit | quote }}
- name: FLEXPRICE_EVENT_PROCESSING_CONSUMER_GROUP
  value: {{ .Values.eventProcessing.consumerGroup | quote }}
- name: FLEXPRICE_EVENT_PROCESSING_LAZY_CONSUMER_GROUP
  value: {{ .Values.eventProcessingLazy.consumerGroup | quote }}
{{- /* ---- Raw event consumption ---- */}}
- name: FLEXPRICE_RAW_EVENT_CONSUMPTION_ENABLED
  value: {{ .Values.rawEventConsumption.enabled | default false | quote }}
{{- if .Values.rawEventConsumption.enabled }}
- name: FLEXPRICE_RAW_EVENT_CONSUMPTION_TOPIC
  value: {{ .Values.rawEventConsumption.topic | default "raw_events" | quote }}
- name: FLEXPRICE_RAW_EVENT_CONSUMPTION_OUTPUT_TOPIC
  value: {{ .Values.rawEventConsumption.outputTopic | default "events" | quote }}
- name: FLEXPRICE_RAW_EVENT_CONSUMPTION_CONSUMER_GROUP
  value: {{ .Values.rawEventConsumption.consumerGroup | default "raw_events_consumer" | quote }}
- name: FLEXPRICE_RAW_EVENT_CONSUMPTION_RATE_LIMIT
  value: {{ .Values.rawEventConsumption.rateLimit | default 100 | quote }}
{{- end }}
{{- /* ---- DynamoDB ---- */}}
- name: FLEXPRICE_DYNAMODB_IN_USE
  value: {{ .Values.dynamodb.inUse | quote }}
{{- /* ---- Webhook / Svix ---- */}}
{{- if .Values.webhook.svixConfig.enabled }}
{{- /* Legacy alias: pre-config-autobind images bind webhook.svix_config.auth_token from this
       name via an explicit v.BindEnv. Kept so a rollback to an older image still gets the token. */}}
- name: FLEXPRICE_SVIX_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: svix-auth-token
{{- /* Canonical name the reflection env-binder derives from webhook.svix_config.auth_token.
       Config-autobind images read THIS (no BindEnv needed). Same secret as the legacy alias
       above, so both images resolve the same token — forward- and backward-compatible. */}}
- name: FLEXPRICE_WEBHOOK_SVIX_CONFIG_AUTH_TOKEN
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: svix-auth-token
{{- end }}
{{- /* ---- Redis extended ---- */}}
{{- if .Values.redisExtended.keyPrefix }}
- name: FLEXPRICE_REDIS_KEY_PREFIX
  value: {{ .Values.redisExtended.keyPrefix | quote }}
{{- end }}
- name: FLEXPRICE_REDIS_CLUSTER_MODE
  value: {{ .Values.redisExtended.clusterMode | quote }}
- name: FLEXPRICE_REDIS_POOL_SIZE
  value: {{ .Values.redisExtended.poolSize | quote }}
{{- /* ---- Logging extended ---- */}}
- name: FLEXPRICE_LOGGING_FORMAT
  value: {{ .Values.logging.format | default "json" | quote }}
{{- if .Values.logging.environment }}
- name: FLEXPRICE_LOGGING_ENVIRONMENT
  value: {{ .Values.logging.environment | quote }}
{{- end }}
{{- if .Values.logging.otel.enabled }}
- name: FLEXPRICE_LOGGING_OTEL_ENABLED
  value: "true"
- name: FLEXPRICE_LOGGING_OTEL_ENDPOINT
  value: {{ .Values.logging.otel.endpoint | quote }}
- name: FLEXPRICE_LOGGING_OTEL_INSECURE
  value: {{ .Values.logging.otel.insecure | quote }}
- name: FLEXPRICE_LOGGING_OTEL_AUTH_HEADER
  value: {{ .Values.logging.otel.authHeader | quote }}
- name: FLEXPRICE_LOGGING_OTEL_AUTH_VALUE
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: logging-otel-auth-value
{{- end }}
{{- /* ---- OpenTelemetry traces (APM/RED metrics) ---- */}}
{{- if .Values.otel.enabled }}
- name: FLEXPRICE_OTEL_ENABLED
  value: "true"
{{- if .Values.otel.serviceName }}
- name: FLEXPRICE_OTEL_SERVICE_NAME
  value: {{ .Values.otel.serviceName | quote }}
{{- end }}
{{- if .Values.otel.traces.enabled }}
- name: FLEXPRICE_OTEL_TRACES_ENABLED
  value: "true"
- name: FLEXPRICE_OTEL_TRACES_ENDPOINT
  value: {{ .Values.otel.traces.endpoint | quote }}
{{- if .Values.otel.traces.authHeader }}
- name: FLEXPRICE_OTEL_TRACES_AUTH_HEADER
  value: {{ .Values.otel.traces.authHeader | quote }}
- name: FLEXPRICE_OTEL_TRACES_AUTH_VALUE
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: logging-otel-auth-value
{{- end }}
{{- if .Values.otel.traces.sampleRate }}
- name: FLEXPRICE_OTEL_TRACES_SAMPLE_RATE
  value: {{ .Values.otel.traces.sampleRate | quote }}
{{- end }}
{{- end }}
{{- end }}
{{- /* ---- App URLs ---- */}}
{{- if .Values.app.customerPortalUrl }}
- name: FLEXPRICE_CUSTOMER_PORTAL_URL
  value: {{ .Values.app.customerPortalUrl | quote }}
{{- end }}
{{- if .Values.app.oauthRedirectUri }}
- name: FLEXPRICE_OAUTH_REDIRECT_URI
  value: {{ .Values.app.oauthRedirectUri | quote }}
{{- end }}
{{- end }}
{{- /* ---- Extra env vars (passthrough; applies in both legacy and migrated modes) ---- */}}
{{- with .Values.extraEnv }}
{{ toYaml . | trim }}
{{- end }}
{{- end }}

{{/*
Topology spread constraints for a component, with a defaulted labelSelector.
Usage: include "flexprice.topologySpreadConstraints" (dict "ctx" . "component" "api")

Resolves `.Values.<component>.topologySpreadConstraints` first, falling back to
the global `.Values.topologySpreadConstraints`.

Any constraint that carries NO labelSelector of its own gets one injected:
the chart's selector labels (which honour nameOverride/fullnameOverride) plus
the component key, so each workload spreads against its own replicas rather
than against every pod in the release.

This defaulting exists because the previous values.yaml hardcoded
`app.kubernetes.io/name: flexprice`. That matched zero pods the moment an
operator set nameOverride, and since the default whenUnsatisfiable is
ScheduleAnyway the scheduler silently ignored the constraint — all replicas
could land in one AZ with nothing in the manifest to show for it.

A constraint that DOES define labelSelector is passed through untouched, so an
operator can still spread against an arbitrary set of pods.

Presence is tested with `hasKey`, not truthiness: an explicit `labelSelector: {}`
is a VALID selector meaning "match every pod in the namespace", but it is falsey
in Go templates. Testing truthiness would take the injection branch and emit
`labelSelector` twice in the same constraint — YAML resolves the duplicate to the
last occurrence, so the operator's explicit selector would be silently discarded
rather than rejected.
*/}}
{{- define "flexprice.topologySpreadConstraints" -}}
{{- $ctx := .ctx -}}
{{- $component := .component -}}
{{- $cfg := index $ctx.Values $component | default dict -}}
{{- $tsc := default $ctx.Values.topologySpreadConstraints (get $cfg "topologySpreadConstraints") -}}
{{- /* `--set topologySpreadConstraints=[]` yields the STRING "[]", not an empty
       list, and `range` over a string is a render error. Anything that is not a
       real list is treated as "no constraints" so the caller's `with` emits
       nothing, matching `topologySpreadConstraints: []` set via a values file. */ -}}
{{- if not (kindIs "slice" $tsc) }}{{- $tsc = list }}{{- end -}}
{{- range $tsc }}
{{- if hasKey . "labelSelector" }}
- {{ toYaml . | nindent 2 | trim }}
{{- else }}
- {{ toYaml . | nindent 2 | trim }}
  labelSelector:
    matchLabels:
      {{- include "flexprice.selectorLabels" $ctx | nindent 6 }}
      app.kubernetes.io/component: {{ $component }}
{{- end }}
{{- end }}
{{- end }}

{{/*
flexprice.image — the app image reference.

Prefers an immutable digest when image.digest is set: renders
"<repository>@<digest>" and ignores the tag entirely. Otherwise renders
"<repository>:<tag>", tag defaulting to .Chart.AppVersion, the prior behaviour.

A digest and a tag cannot both appear in one reference ("repo@sha256:x:tag" is
invalid), so digest wins outright when present. This lets a client pin exactly
the bytes shipped — the workloads and the migration Job all resolve through here.
*/}}
{{- define "flexprice.image" -}}
{{- if .Values.image.digest -}}
{{ .Values.image.repository }}@{{ .Values.image.digest }}
{{- else -}}
{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}
{{- end -}}
{{- end -}}
