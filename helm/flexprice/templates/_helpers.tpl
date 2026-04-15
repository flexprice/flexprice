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
Create the name of the service account to use
*/}}
{{- define "flexprice.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "flexprice.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
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
Resolve the PostgreSQL host.
Uses the bitnami/postgresql subchart service name when internal, or the user-supplied host when external.
*/}}
{{- define "flexprice.postgresHost" -}}
{{- if .Values.postgres.external.enabled -}}
{{- .Values.postgres.host }}
{{- else -}}
{{- printf "%s-postgresql" .Release.Name }}
{{- end -}}
{{- end }}

{{/*
Resolve the PostgreSQL port.
*/}}
{{- define "flexprice.postgresPort" -}}
{{- if .Values.postgres.external.enabled -}}
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
{{- printf "chi-%s-flexprice-0-0:9000" (include "flexprice.fullname" .) }}
{{- else -}}
{{- printf "%s-clickhouse:9000" (include "flexprice.fullname" .) }}
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
Create environment variables from configuration.
All service addresses are resolved via named templates above so this block stays clean.
*/}}
{{- define "flexprice.env" -}}
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
{{- /* ---- Kafka ---- */}}
- name: FLEXPRICE_KAFKA_BROKERS
  value: {{ include "flexprice.kafkaBrokers" . | quote }}
- name: FLEXPRICE_KAFKA_CONSUMER_GROUP
  value: {{ .Values.kafkaConfig.consumerGroup | quote }}
- name: FLEXPRICE_KAFKA_TOPIC
  value: {{ .Values.kafkaConfig.topic | quote }}
- name: FLEXPRICE_KAFKA_TOPIC_LAZY
  value: {{ .Values.kafkaConfig.topicLazy | quote }}
- name: FLEXPRICE_KAFKA_TLS
  value: {{ .Values.kafkaConfig.tls | quote }}
- name: FLEXPRICE_KAFKA_USE_SASL
  value: {{ .Values.kafkaConfig.useSASL | quote }}
- name: FLEXPRICE_KAFKA_CLIENT_ID
  value: {{ .Values.kafkaConfig.clientId | quote }}
{{- if .Values.kafkaConfig.useSASL }}
- name: FLEXPRICE_KAFKA_SASL_USER
  value: {{ .Values.kafkaConfig.saslUser | quote }}
- name: FLEXPRICE_KAFKA_SASL_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: kafka-sasl-password
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
{{- /* ---- Observability ---- */}}
{{- if .Values.sentry.enabled }}
- name: FLEXPRICE_SENTRY_ENABLED
  value: "true"
- name: FLEXPRICE_SENTRY_DSN
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: sentry-dsn
- name: FLEXPRICE_SENTRY_ENVIRONMENT
  value: {{ .Values.sentry.environment | quote }}
- name: FLEXPRICE_SENTRY_SAMPLE_RATE
  value: {{ .Values.sentry.sampleRate | quote }}
{{- end }}
{{- if .Values.pyroscope.enabled }}
- name: FLEXPRICE_PYROSCOPE_ENABLED
  value: "true"
- name: FLEXPRICE_PYROSCOPE_SERVER_ADDRESS
  value: {{ .Values.pyroscope.serverAddress | quote }}
- name: FLEXPRICE_PYROSCOPE_APPLICATION_NAME
  value: {{ .Values.pyroscope.applicationName | quote }}
- name: FLEXPRICE_PYROSCOPE_SAMPLE_RATE
  value: {{ .Values.pyroscope.sampleRate | quote }}
- name: FLEXPRICE_PYROSCOPE_DISABLE_GC_RUNS
  value: {{ .Values.pyroscope.disableGCRuns | quote }}
{{- if .Values.pyroscope.basicAuthUser }}
- name: FLEXPRICE_PYROSCOPE_BASIC_AUTH_USER
  value: {{ .Values.pyroscope.basicAuthUser | quote }}
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
{{- if .Values.email.enabled }}
- name: FLEXPRICE_EMAIL_ENABLED
  value: "true"
- name: FLEXPRICE_EMAIL_RESEND_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.secretName" . }}
      key: email-resend-api-key
- name: FLEXPRICE_EMAIL_FROM_ADDRESS
  value: {{ .Values.email.fromAddress | quote }}
- name: FLEXPRICE_EMAIL_REPLY_TO
  value: {{ .Values.email.replyTo | quote }}
- name: FLEXPRICE_EMAIL_CALENDAR_URL
  value: {{ .Values.email.calendarUrl | quote }}
{{- end }}
{{- /* ---- Event processing ---- */}}
- name: FLEXPRICE_EVENT_PROCESSING_TOPIC
  value: {{ .Values.eventProcessing.topic | quote }}
- name: FLEXPRICE_EVENT_PROCESSING_RATE_LIMIT
  value: {{ .Values.eventProcessing.rateLimit | quote }}
- name: FLEXPRICE_EVENT_PROCESSING_CONSUMER_GROUP
  value: {{ .Values.eventProcessing.consumerGroup | quote }}
{{- /* ---- Extra env vars (passthrough) ---- */}}
{{- range $key, $value := .Values.extraEnv }}
- name: {{ $key | quote }}
  value: {{ $value | quote }}
{{- end }}
{{- end }}
