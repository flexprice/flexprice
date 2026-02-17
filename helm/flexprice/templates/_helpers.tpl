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
Create environment variables from configuration
*/}}
{{- define "flexprice.env" -}}
- name: FLEXPRICE_SERVER_ADDRESS
  value: {{ .Values.server.address | quote }}
- name: FLEXPRICE_POSTGRES_HOST
  value: {{ if .Values.postgres.external.enabled }}{{ .Values.postgres.host | quote }}{{ else }}{{ include "flexprice.fullname" . }}-postgres{{ end }}
- name: FLEXPRICE_POSTGRES_PORT
  value: {{ if .Values.postgres.external.enabled }}{{ .Values.postgres.port | quote }}{{ else }}{{ .Values.postgres.internal.service.port | quote }}{{ end }}
- name: FLEXPRICE_POSTGRES_USER
  value: {{ .Values.postgres.user | quote }}
- name: FLEXPRICE_POSTGRES_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.fullname" . }}-secrets
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
- name: FLEXPRICE_CLICKHOUSE_ADDRESS
  value: {{ if .Values.clickhouse.external.enabled }}{{ .Values.clickhouse.address | quote }}{{ else }}{{ include "flexprice.fullname" . }}-clickhouse:{{ .Values.clickhouse.internal.service.nativePort }}{{ end }}
- name: FLEXPRICE_CLICKHOUSE_USERNAME
  value: {{ .Values.clickhouse.username | quote }}
- name: FLEXPRICE_CLICKHOUSE_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.fullname" . }}-secrets
      key: clickhouse-password
- name: FLEXPRICE_CLICKHOUSE_DATABASE
  value: {{ .Values.clickhouse.database | quote }}
- name: FLEXPRICE_CLICKHOUSE_TLS
  value: {{ .Values.clickhouse.tls | quote }}
- name: FLEXPRICE_KAFKA_BROKERS
  value: {{ if .Values.kafka.external.enabled }}{{ join "," .Values.kafka.brokers | quote }}{{ else }}{{ include "flexprice.fullname" . }}-kafka:{{ .Values.kafka.internal.service.internalPort }}{{ end }}
- name: FLEXPRICE_KAFKA_CONSUMER_GROUP
  value: {{ .Values.kafka.consumerGroup | quote }}
- name: FLEXPRICE_KAFKA_TOPIC
  value: {{ .Values.kafka.topic | quote }}
- name: FLEXPRICE_KAFKA_TOPIC_LAZY
  value: {{ .Values.kafka.topicLazy | quote }}
- name: FLEXPRICE_KAFKA_TLS
  value: {{ .Values.kafka.tls | quote }}
- name: FLEXPRICE_KAFKA_USE_SASL
  value: {{ .Values.kafka.useSASL | quote }}
- name: FLEXPRICE_KAFKA_CLIENT_ID
  value: {{ .Values.kafka.clientId | quote }}
{{- if .Values.kafka.useSASL }}
- name: FLEXPRICE_KAFKA_SASL_USER
  value: {{ .Values.kafka.saslUser | quote }}
- name: FLEXPRICE_KAFKA_SASL_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.fullname" . }}-secrets
      key: kafka-sasl-password
{{- end }}
- name: FLEXPRICE_TEMPORAL_ADDRESS
  value: {{ if .Values.temporal.external.enabled }}{{ .Values.temporal.address | quote }}{{ else }}{{ include "flexprice.fullname" . }}-temporal:{{ .Values.temporal.internal.server.service.port }}{{ end }}
- name: FLEXPRICE_TEMPORAL_TASK_QUEUE
  value: {{ .Values.temporal.taskQueue | quote }}
- name: FLEXPRICE_TEMPORAL_NAMESPACE
  value: {{ .Values.temporal.namespace | quote }}
- name: FLEXPRICE_TEMPORAL_TLS
  value: {{ .Values.temporal.tls | quote }}
{{- if .Values.temporal.apiKey }}
- name: FLEXPRICE_TEMPORAL_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.fullname" . }}-secrets
      key: temporal-api-key
{{- end }}
- name: FLEXPRICE_LOGGING_LEVEL
  value: {{ .Values.logging.level | quote }}
- name: FLEXPRICE_AUTH_PROVIDER
  value: {{ .Values.auth.provider | quote }}
- name: FLEXPRICE_AUTH_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.fullname" . }}-secrets
      key: auth-secret
{{- if eq .Values.auth.provider "supabase" }}
- name: FLEXPRICE_AUTH_SUPABASE_BASE_URL
  value: {{ .Values.auth.supabase.baseUrl | quote }}
- name: FLEXPRICE_AUTH_SUPABASE_SERVICE_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.fullname" . }}-secrets
      key: supabase-service-key
{{- end }}
- name: FLEXPRICE_AUTH_API_KEY_HEADER
  value: {{ .Values.auth.apiKey.header | quote }}
{{- if .Values.sentry.enabled }}
- name: FLEXPRICE_SENTRY_ENABLED
  value: "true"
- name: FLEXPRICE_SENTRY_DSN
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.fullname" . }}-secrets
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
      name: {{ include "flexprice.fullname" . }}-secrets
      key: pyroscope-basic-auth-password
{{- end }}
{{- end }}
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
{{- if .Values.cache.enabled }}
- name: FLEXPRICE_CACHE_ENABLED
  value: "true"
{{- end }}
{{- if .Values.secrets.encryptionKey }}
- name: FLEXPRICE_SECRETS_ENCRYPTION_KEY
  value: {{ .Values.secrets.encryptionKey | quote }}
{{- else }}
- name: FLEXPRICE_SECRETS_ENCRYPTION_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.fullname" . }}-secrets
      key: encryption-key
{{- end }}
{{- if .Values.email.enabled }}
- name: FLEXPRICE_EMAIL_ENABLED
  value: "true"
- name: FLEXPRICE_EMAIL_RESEND_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "flexprice.fullname" . }}-secrets
      key: email-resend-api-key
- name: FLEXPRICE_EMAIL_FROM_ADDRESS
  value: {{ .Values.email.fromAddress | quote }}
- name: FLEXPRICE_EMAIL_REPLY_TO
  value: {{ .Values.email.replyTo | quote }}
- name: FLEXPRICE_EMAIL_CALENDAR_URL
  value: {{ .Values.email.calendarUrl | quote }}
{{- end }}
- name: FLEXPRICE_EVENT_PROCESSING_TOPIC
  value: {{ .Values.eventProcessing.topic | quote }}
- name: FLEXPRICE_EVENT_PROCESSING_RATE_LIMIT
  value: {{ .Values.eventProcessing.rateLimit | quote }}
- name: FLEXPRICE_EVENT_PROCESSING_CONSUMER_GROUP
  value: {{ .Values.eventProcessing.consumerGroup | quote }}
{{- range $key, $value := .Values.extraEnv }}
- name: {{ $key | quote }}
  value: {{ $value | quote }}
{{- end }}
{{- end }}

