#!/usr/bin/env bash
# provision.sh — Ordered FlexPrice cluster provisioning
#
# Usage:
#   ./provision.sh [OPTIONS]
#
# Options:
#   --release    Helm release name       (default: flexprice)
#   --namespace  Kubernetes namespace    (default: flexprice)
#   --values     Path to values file     (default: ./flexprice/values.yaml)
#   --chart      Path to chart dir       (default: ./flexprice)
#   --timeout    Per-step timeout (sec)  (default: 300)
#   --skip-infra Skip infra install (for upgrades where infra is already up)
#   --dry-run    Print commands, do not execute
#
# Environment variables (required unless --dry-run):
#   POSTGRES_PASSWORD        Password for PostgreSQL
#   CLICKHOUSE_PASSWORD      Password for ClickHouse
#   AUTH_SECRET              FlexPrice auth secret (JWT signing key)
#   ENCRYPTION_KEY           FlexPrice secrets encryption key
#
# Optional environment variables:
#   REDIS_PASSWORD           Password for Redis (if auth enabled)
#   KAFKA_SASL_PASSWORD      Kafka SASL password (if SASL enabled)
#   SUPABASE_SERVICE_KEY     Supabase service key (if auth.provider=supabase)
#   SENTRY_DSN               Sentry DSN (if sentry.enabled=true)
#   INGRESS_HOST             External hostname for health check (e.g. api.example.com)

set -euo pipefail

# ── Defaults ──────────────────────────────────────────────────────────────────
RELEASE="${RELEASE:-flexprice}"
NAMESPACE="${NAMESPACE:-flexprice}"
VALUES="${VALUES:-./flexprice/values.yaml}"
CHART="${CHART:-./flexprice}"
TIMEOUT="${TIMEOUT:-300}"
SKIP_INFRA="${SKIP_INFRA:-false}"
DRY_RUN="${DRY_RUN:-false}"

# Parse args
while [[ $# -gt 0 ]]; do
  case $1 in
    --release)    RELEASE="$2";    shift 2 ;;
    --namespace)  NAMESPACE="$2";  shift 2 ;;
    --values)     VALUES="$2";     shift 2 ;;
    --chart)      CHART="$2";      shift 2 ;;
    --timeout)    TIMEOUT="$2";    shift 2 ;;
    --skip-infra) SKIP_INFRA=true; shift   ;;
    --dry-run)    DRY_RUN=true;    shift   ;;
    *) echo "Unknown option: $1"; exit 1   ;;
  esac
done

# ── Colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

log()   { echo -e "${BOLD}[$(date +%H:%M:%S)]${NC} $*"; }
ok()    { echo -e "${GREEN}✓${NC} $*"; }
warn()  { echo -e "${YELLOW}⚠${NC}  $*"; }
err()   { echo -e "${RED}✗${NC} $*"; }
step()  { echo -e "\n${CYAN}${BOLD}━━━ $* ━━━${NC}"; }
run()   {
  if [[ "$DRY_RUN" == "true" ]]; then
    echo -e "${BLUE}[dry-run]${NC} $*"
  else
    eval "$@"
  fi
}

# ── Prerequisite check ────────────────────────────────────────────────────────
check_prereqs() {
  step "Checking prerequisites"
  local missing=()
  for cmd in kubectl helm psql clickhouse-client nc; do
    if ! command -v "$cmd" &>/dev/null; then
      missing+=("$cmd")
    fi
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    err "Missing required tools: ${missing[*]}"
    echo "  psql:               brew install libpq / apt install postgresql-client"
    echo "  clickhouse-client:  https://clickhouse.com/docs/en/install"
    echo "  nc:                 brew install netcat / apt install netcat"
    exit 1
  fi
  ok "All tools present"

  # Kubernetes connectivity
  if ! kubectl cluster-info &>/dev/null; then
    err "Cannot reach Kubernetes cluster. Check your kubeconfig."
    exit 1
  fi
  ok "Kubernetes cluster reachable: $(kubectl config current-context)"

  # Helm dependencies
  if [[ ! -d "$CHART/charts" ]] || [[ -z "$(ls -A "$CHART/charts" 2>/dev/null)" ]]; then
    warn "Helm dependencies not fetched. Running helm dependency build..."
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

# ── Step 1: Create namespace + inject secrets ─────────────────────────────────
provision_secrets() {
  step "Step 1 — Namespace + Kubernetes Secrets"

  run kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml \| kubectl apply -f -
  ok "Namespace: $NAMESPACE"

  # Build stringData for the single shared secret
  # Matches what secret.yaml expects
  local secret_args=(
    "postgres-password=${POSTGRES_PASSWORD:-changeme}"
    "clickhouse-password=${CLICKHOUSE_PASSWORD:-changeme}"
    "auth-secret=${AUTH_SECRET:-changeme}"
    "encryption-key=${ENCRYPTION_KEY:-changeme}"
  )
  [[ -n "${REDIS_PASSWORD:-}"       ]] && secret_args+=("redis-password=${REDIS_PASSWORD}")
  [[ -n "${KAFKA_SASL_PASSWORD:-}"  ]] && secret_args+=("kafka-sasl-password=${KAFKA_SASL_PASSWORD}")
  [[ -n "${SUPABASE_SERVICE_KEY:-}" ]] && secret_args+=("supabase-service-key=${SUPABASE_SERVICE_KEY}")
  [[ -n "${SENTRY_DSN:-}"           ]] && secret_args+=("sentry-dsn=${SENTRY_DSN}")

  # Build --from-literal args
  local from_literals=""
  for kv in "${secret_args[@]}"; do
    from_literals+=" --from-literal=${kv}"
  done

  run kubectl create secret generic "${RELEASE}-secrets" \
    --namespace "$NAMESPACE" \
    --dry-run=client -o yaml \
    $from_literals \| kubectl apply -f -

  ok "Secret ${RELEASE}-secrets created/updated"
}

# ── Step 2: Deploy infra + wait for healthy ───────────────────────────────────
deploy_infra() {
  step "Step 2 — Deploy databases and wait for healthy"

  # Disable migration + app pods, enable only infra
  run helm upgrade --install "$RELEASE" "$CHART" \
    --namespace "$NAMESPACE" \
    --values "$VALUES" \
    --set migration.enabled=false \
    --set api.enabled=false \
    --set consumer.enabled=false \
    --set worker.enabled=false \
    --set ingress.enabled=false \
    --timeout "${TIMEOUT}s" \
    --wait

  ok "Infra resources submitted"

  # Wait for each StatefulSet
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
  run kubectl rollout status statefulset/"$full" \
    --namespace "$NAMESPACE" \
    --timeout "${TIMEOUT}s" || {
      warn "StatefulSet $full not found or timed out — may be external/disabled, skipping"
    }
}

# ── Step 3: Ping each database ────────────────────────────────────────────────
ping_databases() {
  step "Step 3 — Database connectivity checks"

  # Resolve actual endpoints via port-forward in background
  # We open one port-forward per DB, run the ping, then kill

  ping_postgres
  ping_clickhouse
  ping_redis
  ping_kafka
}

ping_postgres() {
  log "Pinging PostgreSQL..."
  if [[ "$DRY_RUN" == "true" ]]; then
    warn "[dry-run] would psql -c 'SELECT 1'"
    return
  fi
  local pf_port=55432
  kubectl port-forward --namespace "$NAMESPACE" \
    "svc/${RELEASE}-postgresql" ${pf_port}:5432 &>/dev/null &
  local pf_pid=$!
  sleep 2
  if PGPASSWORD="${POSTGRES_PASSWORD:-}" psql \
      -h 127.0.0.1 -p $pf_port \
      -U flexprice -d flexprice \
      -c "SELECT 1" &>/dev/null; then
    ok "PostgreSQL: healthy"
  else
    err "PostgreSQL: ping failed"
    kill $pf_pid 2>/dev/null; exit 1
  fi
  kill $pf_pid 2>/dev/null || true
}

ping_clickhouse() {
  log "Pinging ClickHouse..."
  if [[ "$DRY_RUN" == "true" ]]; then
    warn "[dry-run] would clickhouse-client --query 'SELECT 1'"
    return
  fi
  local pf_port=58123
  kubectl port-forward --namespace "$NAMESPACE" \
    "svc/${RELEASE}-clickhouse" ${pf_port}:8123 &>/dev/null &
  local pf_pid=$!
  sleep 2
  if curl -sf "http://127.0.0.1:${pf_port}/ping" &>/dev/null; then
    ok "ClickHouse: healthy"
  else
    err "ClickHouse: ping failed"
    kill $pf_pid 2>/dev/null; exit 1
  fi
  kill $pf_pid 2>/dev/null || true
}

ping_redis() {
  log "Pinging Redis..."
  if [[ "$DRY_RUN" == "true" ]]; then
    warn "[dry-run] would redis-cli PING"
    return
  fi
  local pf_port=56379
  kubectl port-forward --namespace "$NAMESPACE" \
    "svc/${RELEASE}-redis-master" ${pf_port}:6379 &>/dev/null &
  local pf_pid=$!
  sleep 2
  local redis_cmd="redis-cli -h 127.0.0.1 -p ${pf_port}"
  [[ -n "${REDIS_PASSWORD:-}" ]] && redis_cmd+=" -a ${REDIS_PASSWORD}"
  if $redis_cmd PING 2>/dev/null | grep -q "PONG"; then
    ok "Redis: healthy"
  else
    err "Redis: ping failed"
    kill $pf_pid 2>/dev/null; exit 1
  fi
  kill $pf_pid 2>/dev/null || true
}

ping_kafka() {
  log "Pinging Kafka..."
  if [[ "$DRY_RUN" == "true" ]]; then
    warn "[dry-run] would nc -z kafka-broker 9092"
    return
  fi
  local pf_port=59092
  kubectl port-forward --namespace "$NAMESPACE" \
    "svc/${RELEASE}-kafka" ${pf_port}:9092 &>/dev/null &
  local pf_pid=$!
  sleep 2
  if nc -z 127.0.0.1 $pf_port 2>/dev/null; then
    ok "Kafka: healthy (port open)"
  else
    err "Kafka: unreachable"
    kill $pf_pid 2>/dev/null; exit 1
  fi
  kill $pf_pid 2>/dev/null || true
}

# ── Step 4: Run migrations ─────────────────────────────────────────────────────
run_migrations() {
  step "Step 4 — Migrations, topics, schema setup"

  # Enable migration job only, apps still off
  run helm upgrade "$RELEASE" "$CHART" \
    --namespace "$NAMESPACE" \
    --values "$VALUES" \
    --set api.enabled=false \
    --set consumer.enabled=false \
    --set worker.enabled=false \
    --set ingress.enabled=false \
    --set migration.enabled=true \
    --timeout "${TIMEOUT}s" \
    --wait

  # The migration job is a pre-upgrade hook — it runs before helm marks upgrade complete.
  # --wait above blocks until the hook job finishes. Verify it succeeded.
  local job="${RELEASE}-migration"
  log "Checking migration job status..."

  if [[ "$DRY_RUN" == "true" ]]; then
    warn "[dry-run] would check job status"
    return
  fi

  # Find the actual job name (it has a checksum suffix)
  local actual_job
  actual_job=$(kubectl get jobs --namespace "$NAMESPACE" \
    -l "app.kubernetes.io/component=migration" \
    -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null || echo "")

  if [[ -z "$actual_job" ]]; then
    warn "No migration job found — may have been cleaned up (hook-delete-policy). Assuming success."
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

  run helm upgrade "$RELEASE" "$CHART" \
    --namespace "$NAMESPACE" \
    --values "$VALUES" \
    --set migration.enabled=true \
    --set api.enabled=true \
    --set consumer.enabled=true \
    --set worker.enabled=true \
    --set ingress.enabled=false \
    --timeout "${TIMEOUT}s" \
    --wait

  ok "API, Consumer, Worker pods deployed"

  # Verify rollout
  for deploy in api consumer worker; do
    local name="${RELEASE}-${deploy}"
    log "Checking rollout: $name"
    run kubectl rollout status deployment/"$name" \
      --namespace "$NAMESPACE" \
      --timeout "${TIMEOUT}s" || warn "$name not found or timed out"
  done

  # Internal health check via port-forward
  if [[ "$DRY_RUN" != "true" ]]; then
    local pf_port=58080
    kubectl port-forward --namespace "$NAMESPACE" \
      "svc/${RELEASE}-api" ${pf_port}:80 &>/dev/null &
    local pf_pid=$!
    sleep 3
    if curl -sf "http://127.0.0.1:${pf_port}/health" &>/dev/null; then
      ok "Internal health check: /health → 200"
    else
      warn "Internal health check failed — pod may still be starting"
    fi
    kill $pf_pid 2>/dev/null || true
  fi
}

# ── Step 6: Deploy ingress ─────────────────────────────────────────────────────
deploy_ingress() {
  step "Step 6 — Deploy Ingress"

  run helm upgrade "$RELEASE" "$CHART" \
    --namespace "$NAMESPACE" \
    --values "$VALUES" \
    --set ingress.enabled=true \
    --timeout "${TIMEOUT}s" \
    --wait

  ok "Ingress deployed"

  if [[ "$DRY_RUN" != "true" ]]; then
    log "Ingress rules:"
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
  local retries=24
  local i=1
  while [[ $i -le $retries ]]; do
    local http_code
    http_code=$(curl -sk -o /dev/null -w "%{http_code}" \
      "https://${INGRESS_HOST}/health" 2>/dev/null || echo "000")
    if [[ "$http_code" == "200" ]]; then
      ok "External health check: https://${INGRESS_HOST}/health → 200"
      return
    fi
    echo "  [$i/$retries] Got HTTP $http_code, retrying in 5s..."
    sleep 5
    i=$((i + 1))
  done

  err "External health check timed out: https://${INGRESS_HOST}/health"
  err "Check: kubectl describe ingress -n $NAMESPACE"
  err "Check: kubectl get events -n $NAMESPACE --sort-by='.lastTimestamp' | tail -20"
  exit 1
}

# ── Summary ───────────────────────────────────────────────────────────────────
print_summary() {
  step "Provisioning complete"
  echo ""
  echo -e "  ${BOLD}Release:${NC}    $RELEASE"
  echo -e "  ${BOLD}Namespace:${NC}  $NAMESPACE"
  echo ""
  if [[ -n "${INGRESS_HOST:-}" ]]; then
    echo -e "  ${GREEN}${BOLD}API endpoint:${NC} https://${INGRESS_HOST}"
  fi
  echo ""
  echo "  kubectl get pods -n $NAMESPACE"
  echo "  kubectl logs -n $NAMESPACE -l app.kubernetes.io/name=flexprice -f"
  echo ""
}

# ── Main ───────────────────────────────────────────────────────────────────────
main() {
  echo -e "${BOLD}FlexPrice Cluster Provisioner${NC}"
  echo -e "  Release: ${CYAN}$RELEASE${NC}  Namespace: ${CYAN}$NAMESPACE${NC}  Dry-run: ${CYAN}$DRY_RUN${NC}"
  echo ""

  check_prereqs
  check_required_secrets

  provision_secrets

  if [[ "$SKIP_INFRA" == "false" ]]; then
    deploy_infra
    ping_databases
  else
    log "--skip-infra set, assuming databases already running"
    ping_databases
  fi

  run_migrations
  deploy_services
  deploy_ingress
  external_health_check
  print_summary
}

main "$@"
