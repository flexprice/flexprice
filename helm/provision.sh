#!/usr/bin/env bash
# provision.sh — Ordered FlexPrice cluster provisioning
#
# Usage:
#   ./provision.sh [OPTIONS]
#
# Options:
#   --release      Helm release name            (default: flexprice)
#   --namespace    Kubernetes namespace          (default: flexprice)
#   --values       Path to base values file      (default: ./flexprice/values.yaml)
#   --values-extra Path to override values file  (optional, layered on top of --values)
#   --chart        Path to chart dir             (default: ./flexprice)
#   --timeout      Per-step timeout (sec)        (default: 300)
#   --skip-infra   Skip infra install (infra is external — RDS, ElastiCache, MSK, etc.)
#   --dry-run      Print commands, do not execute
#
# Required environment variables (unless --dry-run):
#   POSTGRES_PASSWORD        Password for PostgreSQL
#   CLICKHOUSE_PASSWORD      Password for ClickHouse
#   AUTH_SECRET              FlexPrice auth secret (JWT signing key)
#   ENCRYPTION_KEY           FlexPrice secrets encryption key
#
# Optional environment variables (included in secret when set):
#   REDIS_PASSWORD           Redis password (if auth enabled)
#   KAFKA_SASL_PASSWORD      Kafka SASL password (if useSASL=true)
#   TEMPORAL_API_KEY         Temporal Cloud API key
#   SUPABASE_SERVICE_KEY     Supabase service role key (if auth.provider=supabase)
#   SENTRY_DSN               Sentry DSN (if sentry.enabled=true)
#   PYROSCOPE_PASSWORD       Grafana Cloud / Pyroscope basic auth password
#   RESEND_API_KEY           Resend email API key (if email.enabled=true)
#   SVIX_TOKEN               Svix auth token (if webhook.svixConfig.enabled=true)
#   LOGGING_OTEL_AUTH_VALUE  OTel ingestion key (if logging.otel.enabled=true)
#
#   INGRESS_HOST             External hostname for health check (e.g. api.example.com)

set -euo pipefail

# ── Defaults ───────────────────────────────────────────────────────────────────
RELEASE="${RELEASE:-flexprice}"
NAMESPACE="${NAMESPACE:-flexprice}"
VALUES="${VALUES:-./flexprice/values.yaml}"
VALUES_EXTRA="${VALUES_EXTRA:-}"
CHART="${CHART:-./flexprice}"
TIMEOUT="${TIMEOUT:-300}"
SKIP_INFRA="${SKIP_INFRA:-false}"
DRY_RUN="${DRY_RUN:-false}"

# Parse args
while [[ $# -gt 0 ]]; do
  case $1 in
    --release)       RELEASE="$2";       shift 2 ;;
    --namespace)     NAMESPACE="$2";     shift 2 ;;
    --values)        VALUES="$2";        shift 2 ;;
    --values-extra)  VALUES_EXTRA="$2";  shift 2 ;;
    --chart)         CHART="$2";         shift 2 ;;
    --timeout)       TIMEOUT="$2";       shift 2 ;;
    --skip-infra)    SKIP_INFRA=true;    shift   ;;
    --dry-run)       DRY_RUN=true;       shift   ;;
    *) echo "Unknown option: $1"; exit 1          ;;
  esac
done

# ── Colours ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

log()  { echo -e "${BOLD}[$(date +%H:%M:%S)]${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }
warn() { echo -e "${YELLOW}⚠${NC}  $*"; }
err()  { echo -e "${RED}✗${NC} $*"; }
step() { echo -e "\n${CYAN}${BOLD}━━━ $* ━━━${NC}"; }

# Safe run — no eval, args passed as array elements
run() {
  if [[ "$DRY_RUN" == "true" ]]; then
    echo -e "${BLUE}[dry-run]${NC} $*"
  else
    "$@"
  fi
}

# Build the --values flags for every helm call
helm_values_flags() {
  local flags=(--values "$VALUES")
  [[ -n "$VALUES_EXTRA" ]] && flags+=(--values "$VALUES_EXTRA")
  printf '%s\n' "${flags[@]}"
}

# ── Port-forward helpers ───────────────────────────────────────────────────────
# Wait for a local port to accept connections (avoids brittle sleep)
wait_for_local_port() {
  local port="$1"
  local retries=30
  local i=1
  while [[ $i -le $retries ]]; do
    if nc -z 127.0.0.1 "$port" 2>/dev/null; then
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done
  return 1
}

# Start a port-forward and return the PID; caller must kill it
start_port_forward() {
  local svc="$1" local_port="$2" remote_port="$3"
  kubectl port-forward --namespace "$NAMESPACE" \
    "svc/${svc}" "${local_port}:${remote_port}" &>/dev/null &
  echo $!
}

# ── Prerequisite check ─────────────────────────────────────────────────────────
check_prereqs() {
  step "Checking prerequisites"
  local missing=()
  for cmd in kubectl helm curl nc; do
    command -v "$cmd" &>/dev/null || missing+=("$cmd")
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    err "Missing required tools: ${missing[*]}"
    exit 1
  fi
  ok "All tools present"

  if ! kubectl cluster-info &>/dev/null; then
    err "Cannot reach Kubernetes cluster. Check your kubeconfig."
    exit 1
  fi
  ok "Kubernetes cluster reachable: $(kubectl config current-context)"

  if [[ ! -d "$CHART/charts" ]] || [[ -z "$(ls -A "$CHART/charts" 2>/dev/null)" ]]; then
    warn "Helm dependencies not fetched — running helm dependency build..."
    run helm dependency build "$CHART"
  else
    ok "Helm dependencies present"
  fi
}

# ── Required secret validation ─────────────────────────────────────────────────
check_required_secrets() {
  if [[ "$DRY_RUN" == "true" ]]; then
    warn "Dry-run: skipping secret validation"
    return
  fi
  local missing=()
  [[ -z "${POSTGRES_PASSWORD:-}"   ]] && missing+=("POSTGRES_PASSWORD")
  [[ -z "${CLICKHOUSE_PASSWORD:-}" ]] && missing+=("CLICKHOUSE_PASSWORD")
  [[ -z "${AUTH_SECRET:-}"         ]] && missing+=("AUTH_SECRET")
  [[ -z "${ENCRYPTION_KEY:-}"      ]] && missing+=("ENCRYPTION_KEY")
  if [[ ${#missing[@]} -gt 0 ]]; then
    err "Missing required environment variables:"
    for v in "${missing[@]}"; do echo "  export $v=<value>"; done
    exit 1
  fi
  ok "All required secrets present in environment"
}

# ── Step 1: Create namespace + Kubernetes Secret ───────────────────────────────
provision_secrets() {
  step "Step 1 — Namespace + Kubernetes Secret"

  run kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml \
    | kubectl apply -f -
  ok "Namespace: $NAMESPACE"

  # Build --from-literal args as an array — safe for passwords with special chars
  local from_literals=(
    --from-literal="postgres-password=${POSTGRES_PASSWORD:-changeme}"
    --from-literal="clickhouse-password=${CLICKHOUSE_PASSWORD:-changeme}"
    --from-literal="auth-secret=${AUTH_SECRET:-changeme}"
    --from-literal="encryption-key=${ENCRYPTION_KEY:-changeme}"
  )
  [[ -n "${REDIS_PASSWORD:-}"          ]] && from_literals+=(--from-literal="redis-password=${REDIS_PASSWORD}")
  [[ -n "${KAFKA_SASL_PASSWORD:-}"     ]] && from_literals+=(--from-literal="kafka-sasl-password=${KAFKA_SASL_PASSWORD}")
  [[ -n "${TEMPORAL_API_KEY:-}"        ]] && from_literals+=(--from-literal="temporal-api-key=${TEMPORAL_API_KEY}")
  [[ -n "${SUPABASE_SERVICE_KEY:-}"    ]] && from_literals+=(--from-literal="supabase-service-key=${SUPABASE_SERVICE_KEY}")
  [[ -n "${SENTRY_DSN:-}"              ]] && from_literals+=(--from-literal="sentry-dsn=${SENTRY_DSN}")
  [[ -n "${PYROSCOPE_PASSWORD:-}"      ]] && from_literals+=(--from-literal="pyroscope-basic-auth-password=${PYROSCOPE_PASSWORD}")
  [[ -n "${RESEND_API_KEY:-}"          ]] && from_literals+=(--from-literal="email-resend-api-key=${RESEND_API_KEY}")
  [[ -n "${SVIX_TOKEN:-}"              ]] && from_literals+=(--from-literal="svix-auth-token=${SVIX_TOKEN}")
  [[ -n "${LOGGING_OTEL_AUTH_VALUE:-}" ]] && from_literals+=(--from-literal="logging-otel-auth-value=${LOGGING_OTEL_AUTH_VALUE}")

  if [[ "$DRY_RUN" == "true" ]]; then
    echo -e "${BLUE}[dry-run]${NC} kubectl create secret generic ${RELEASE}-secrets \\"
    for lit in "${from_literals[@]}"; do
      echo "    $lit \\"
    done
    echo "  --namespace $NAMESPACE --dry-run=client -o yaml | kubectl apply -f -"
  else
    kubectl create secret generic "${RELEASE}-secrets" \
      --namespace "$NAMESPACE" \
      "${from_literals[@]}" \
      --dry-run=client -o yaml \
      | kubectl apply -f -
  fi

  ok "Secret ${RELEASE}-secrets created/updated"
}

# ── Step 2: Deploy infra + wait ────────────────────────────────────────────────
deploy_infra() {
  step "Step 2 — Deploy databases and wait for healthy"

  local values_flags
  mapfile -t values_flags < <(helm_values_flags)

  run helm upgrade --install "$RELEASE" "$CHART" \
    "${values_flags[@]}" \
    --namespace "$NAMESPACE" \
    --set migration.enabled=false \
    --set api.enabled=false \
    --set consumer.enabled=false \
    --set worker.enabled=false \
    --set ingress.enabled=false \
    --timeout "${TIMEOUT}s" \
    --wait

  ok "Infra resources submitted"

  wait_statefulset "postgresql"
  wait_statefulset "kafka-controller"
  wait_statefulset "redis-master"
  wait_statefulset "clickhouse"

  ok "All infra StatefulSets ready"
}

wait_statefulset() {
  local name="$1"
  local full="${RELEASE}-${name}"
  log "Waiting for StatefulSet: $full"
  if [[ "$DRY_RUN" == "true" ]]; then
    warn "[dry-run] would wait for $full"
    return
  fi
  kubectl rollout status "statefulset/${full}" \
    --namespace "$NAMESPACE" \
    --timeout "${TIMEOUT}s" \
    || warn "StatefulSet $full not found or timed out — may be external/disabled, skipping"
}

# ── Step 3: Ping databases ─────────────────────────────────────────────────────
ping_databases() {
  step "Step 3 — Database connectivity checks"

  if [[ "$SKIP_INFRA" == "true" ]]; then
    warn "--skip-infra set — databases are external; skipping in-cluster port-forward pings"
    warn "Verify connectivity manually before proceeding"
    return
  fi

  ping_postgres
  ping_clickhouse
  ping_redis
  ping_kafka
}

ping_postgres() {
  log "Pinging PostgreSQL..."
  if [[ "$DRY_RUN" == "true" ]]; then warn "[dry-run] would psql -c 'SELECT 1'"; return; fi
  local port=55432
  local pid
  pid=$(start_port_forward "${RELEASE}-postgresql" $port 5432)
  if ! wait_for_local_port $port; then
    err "Port-forward to PostgreSQL timed out"; kill $pid 2>/dev/null; exit 1
  fi
  if PGPASSWORD="${POSTGRES_PASSWORD:-}" psql \
      -h 127.0.0.1 -p $port -U flexprice -d flexprice \
      -c "SELECT 1" &>/dev/null; then
    ok "PostgreSQL: healthy"
  else
    err "PostgreSQL: ping failed"; kill $pid 2>/dev/null; exit 1
  fi
  kill $pid 2>/dev/null || true
}

ping_clickhouse() {
  log "Pinging ClickHouse..."
  if [[ "$DRY_RUN" == "true" ]]; then warn "[dry-run] would curl /ping"; return; fi
  local port=58123
  local pid
  pid=$(start_port_forward "${RELEASE}-clickhouse" $port 8123)
  if ! wait_for_local_port $port; then
    err "Port-forward to ClickHouse timed out"; kill $pid 2>/dev/null; exit 1
  fi
  if curl -sf "http://127.0.0.1:${port}/ping" &>/dev/null; then
    ok "ClickHouse: healthy"
  else
    err "ClickHouse: ping failed"; kill $pid 2>/dev/null; exit 1
  fi
  kill $pid 2>/dev/null || true
}

ping_redis() {
  log "Pinging Redis..."
  if [[ "$DRY_RUN" == "true" ]]; then warn "[dry-run] would redis-cli PING"; return; fi
  local port=56379
  local pid
  pid=$(start_port_forward "${RELEASE}-redis-master" $port 6379)
  if ! wait_for_local_port $port; then
    err "Port-forward to Redis timed out"; kill $pid 2>/dev/null; exit 1
  fi
  local redis_cmd=(redis-cli -h 127.0.0.1 -p $port)
  [[ -n "${REDIS_PASSWORD:-}" ]] && redis_cmd+=(-a "${REDIS_PASSWORD}")
  if "${redis_cmd[@]}" PING 2>/dev/null | grep -q "PONG"; then
    ok "Redis: healthy"
  else
    err "Redis: ping failed"; kill $pid 2>/dev/null; exit 1
  fi
  kill $pid 2>/dev/null || true
}

ping_kafka() {
  log "Pinging Kafka..."
  if [[ "$DRY_RUN" == "true" ]]; then warn "[dry-run] would nc -z kafka 9092"; return; fi
  local port=59092
  local pid
  pid=$(start_port_forward "${RELEASE}-kafka" $port 9092)
  if ! wait_for_local_port $port; then
    err "Port-forward to Kafka timed out"; kill $pid 2>/dev/null; exit 1
  fi
  if nc -z 127.0.0.1 $port 2>/dev/null; then
    ok "Kafka: healthy (port open)"
  else
    err "Kafka: unreachable"; kill $pid 2>/dev/null; exit 1
  fi
  kill $pid 2>/dev/null || true
}

# ── Step 4: Run migrations ─────────────────────────────────────────────────────
run_migrations() {
  step "Step 4 — Migrations, topics, schema setup"

  local values_flags
  mapfile -t values_flags < <(helm_values_flags)

  run helm upgrade --install "$RELEASE" "$CHART" \
    "${values_flags[@]}" \
    --namespace "$NAMESPACE" \
    --set api.enabled=false \
    --set consumer.enabled=false \
    --set worker.enabled=false \
    --set ingress.enabled=false \
    --set migration.enabled=true \
    --timeout "${TIMEOUT}s" \
    --wait

  if [[ "$DRY_RUN" == "true" ]]; then warn "[dry-run] would check migration job status"; return; fi

  local actual_job
  actual_job=$(kubectl get jobs --namespace "$NAMESPACE" \
    -l "app.kubernetes.io/component=migration" \
    -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null || echo "")

  if [[ -z "$actual_job" ]]; then
    warn "No migration job found — may have been cleaned up by hook-delete-policy. Assuming success."
    return
  fi

  local succeeded
  succeeded=$(kubectl get job "$actual_job" --namespace "$NAMESPACE" \
    -o jsonpath='{.status.succeeded}' 2>/dev/null || echo "0")

  if [[ "$succeeded" == "1" ]]; then
    ok "Migration job succeeded: $actual_job"
  else
    err "Migration job did not succeed. Logs:"
    kubectl logs --namespace "$NAMESPACE" \
      -l "app.kubernetes.io/component=migration" --tail=50 || true
    exit 1
  fi
}

# ── Step 5: Deploy application pods ───────────────────────────────────────────
deploy_services() {
  step "Step 5 — Deploy FlexPrice services"

  local values_flags
  mapfile -t values_flags < <(helm_values_flags)

  run helm upgrade --install "$RELEASE" "$CHART" \
    "${values_flags[@]}" \
    --namespace "$NAMESPACE" \
    --set migration.enabled=true \
    --set api.enabled=true \
    --set consumer.enabled=true \
    --set worker.enabled=true \
    --set ingress.enabled=false \
    --timeout "${TIMEOUT}s" \
    --wait

  ok "API, Consumer, Worker pods deployed"

  if [[ "$DRY_RUN" == "true" ]]; then return; fi

  for deploy in api consumer worker; do
    local name="${RELEASE}-${deploy}"
    log "Checking rollout: $name"
    kubectl rollout status "deployment/${name}" \
      --namespace "$NAMESPACE" \
      --timeout "${TIMEOUT}s" \
      || warn "$name not found or timed out"
  done

  local port=58080
  local pid
  pid=$(start_port_forward "${RELEASE}" $port 80)
  if wait_for_local_port $port; then
    if curl -sf "http://127.0.0.1:${port}/health" &>/dev/null; then
      ok "Internal health check: /health → 200"
    else
      warn "Internal health check failed — pod may still be starting"
    fi
  else
    warn "Port-forward for internal health check timed out"
  fi
  kill $pid 2>/dev/null || true
}

# ── Step 6: Deploy ingress ─────────────────────────────────────────────────────
deploy_ingress() {
  step "Step 6 — Deploy Ingress"

  local values_flags
  mapfile -t values_flags < <(helm_values_flags)

  run helm upgrade --install "$RELEASE" "$CHART" \
    "${values_flags[@]}" \
    --namespace "$NAMESPACE" \
    --set ingress.enabled=true \
    --timeout "${TIMEOUT}s" \
    --wait

  ok "Ingress deployed"

  if [[ "$DRY_RUN" != "true" ]]; then
    kubectl get ingress --namespace "$NAMESPACE" -o wide || true
  fi
}

# ── Step 7: External health check ─────────────────────────────────────────────
external_health_check() {
  step "Step 7 — External health check"

  if [[ -z "${INGRESS_HOST:-}" ]]; then
    warn "INGRESS_HOST not set — skipping external health check"
    warn "Set: export INGRESS_HOST=api.your-domain.com"
    return
  fi

  if [[ "$DRY_RUN" == "true" ]]; then
    warn "[dry-run] would curl https://${INGRESS_HOST}/health"
    return
  fi

  log "Waiting for DNS / LB to propagate (up to 120s)..."
  local retries=24 i=1
  while [[ $i -le $retries ]]; do
    local http_code
    http_code=$(curl -sk -o /dev/null -w "%{http_code}" \
      "https://${INGRESS_HOST}/health" 2>/dev/null || echo "000")
    if [[ "$http_code" == "200" ]]; then
      ok "External health check: https://${INGRESS_HOST}/health → 200"
      return
    fi
    echo "  [$i/$retries] HTTP $http_code — retrying in 5s..."
    sleep 5
    i=$((i + 1))
  done

  err "External health check timed out: https://${INGRESS_HOST}/health"
  err "Check: kubectl describe ingress -n $NAMESPACE"
  err "Check: kubectl get events -n $NAMESPACE --sort-by='.lastTimestamp' | tail -20"
  exit 1
}

# ── Summary ────────────────────────────────────────────────────────────────────
print_summary() {
  step "Provisioning complete"
  echo ""
  echo -e "  ${BOLD}Release:${NC}    $RELEASE"
  echo -e "  ${BOLD}Namespace:${NC}  $NAMESPACE"
  [[ -n "$VALUES_EXTRA" ]] && echo -e "  ${BOLD}Overrides:${NC}  $VALUES_EXTRA"
  echo ""
  [[ -n "${INGRESS_HOST:-}" ]] && echo -e "  ${GREEN}${BOLD}API endpoint:${NC} https://${INGRESS_HOST}"
  echo ""
  echo "  kubectl get pods -n $NAMESPACE"
  echo "  kubectl logs -n $NAMESPACE -l app.kubernetes.io/name=flexprice -f"
  echo ""
}

# ── Main ───────────────────────────────────────────────────────────────────────
main() {
  echo -e "${BOLD}FlexPrice Cluster Provisioner${NC}"
  echo -e "  Release: ${CYAN}$RELEASE${NC}  Namespace: ${CYAN}$NAMESPACE${NC}  Dry-run: ${CYAN}$DRY_RUN${NC}"
  [[ -n "$VALUES_EXTRA" ]] && echo -e "  Extra values: ${CYAN}$VALUES_EXTRA${NC}"
  echo ""

  check_prereqs
  check_required_secrets
  provision_secrets

  if [[ "$SKIP_INFRA" == "false" ]]; then
    deploy_infra
  else
    log "--skip-infra set — assuming databases already running"
  fi

  ping_databases
  run_migrations
  deploy_services
  deploy_ingress
  external_health_check
  print_summary
}

main "$@"
