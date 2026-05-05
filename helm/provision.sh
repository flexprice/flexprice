#!/usr/bin/env bash
# provision.sh — One-command FlexPrice deployer (dev + prod)
#
# Modes:
#   --mode dev    (default) — kind cluster, local image build, inline secrets, integration tests
#   --mode prod              — assumes cluster + ingress + external infra; secrets from env vars; health checks
#
# Prerequisites:
#   dev:   kind, kubectl, helm, docker
#   prod:  kubectl, helm, curl, nc  (psql, redis-cli for DB pings — optional)
#
# ── Dev usage ─────────────────────────────────────────────────────────────────
#   cd helm/
#   ./provision.sh                                       # kind + local build
#   ./provision.sh --source ghcr --version v1.0.0-pre    # pull from GHCR
#   ./provision.sh --skip-frontend --skip-tests          # faster
#
# ── Prod usage ────────────────────────────────────────────────────────────────
#   POSTGRES_PASSWORD=...    \
#   CLICKHOUSE_PASSWORD=...  \
#   AUTH_SECRET=...          \
#   ENCRYPTION_KEY=...       \
#   INGRESS_HOST=api.example.com \
#   ./provision.sh --mode prod \
#     --values flexprice/values.yaml \
#     --values-extra values-prod.yaml \
#     --skip-infra
#
# ── Common flags ──────────────────────────────────────────────────────────────
#   --mode dev|prod          Workflow profile                            (default: dev)
#   --release <name>         Helm release name                           (default: flexprice)
#   --namespace <ns>         Kubernetes namespace                        (default: flexprice)
#   --values <path>          Base values file                            (default: ./flexprice/values.yaml)
#   --values-extra <path>    Override values file (layered on top)
#   --chart <path>           Local chart path                            (default: ./flexprice)
#   --timeout <sec>          Per-step timeout                            (default: 300)
#   --dry-run                Print commands, do not execute              (prod mode only)
#
# ── Dev-mode flags ────────────────────────────────────────────────────────────
#   --source local|ghcr      Image source                                (default: local)
#   --version <tag>          Chart/image version (ghcr)                  (default: v1.0.0-pre)
#   --image-tag <tag>        Override image tag separately
#   --cluster <name>         Kind cluster name                           (default: flexprice-local)
#   --skip-cluster           Reuse existing kind cluster
#   --skip-tests             Skip integration sanity test
#   --skip-frontend          Skip frontend build/deploy
#
# ── Prod-mode flags ───────────────────────────────────────────────────────────
#   --skip-infra             Skip in-cluster infra (using RDS, MSK, etc.)
#   --skip-db-ping           Skip DB connectivity checks
#   --skip-ext-health        Skip external https://$INGRESS_HOST/health curl
#
# Required env vars (prod, unless --dry-run):
#   POSTGRES_PASSWORD, CLICKHOUSE_PASSWORD, AUTH_SECRET, ENCRYPTION_KEY
# Optional env vars (prod, included in Secret if set):
#   REDIS_PASSWORD, KAFKA_SASL_PASSWORD, TEMPORAL_API_KEY, SUPABASE_SERVICE_KEY,
#   SENTRY_DSN, PYROSCOPE_PASSWORD, RESEND_API_KEY, SVIX_TOKEN, LOGGING_OTEL_AUTH_VALUE,
#   INGRESS_HOST (for external health check)

set -euo pipefail

# ── Defaults ──────────────────────────────────────────────────────────────────
MODE="dev"
SOURCE="local"
VERSION="v1.0.0-pre"
IMAGE_TAG=""
CLUSTER_NAME="flexprice-local"
NAMESPACE="flexprice"
RELEASE="flexprice"
CHART="./flexprice"
VALUES="./flexprice/values.yaml"
VALUES_EXTRA=""
TIMEOUT="300"
SKIP_CLUSTER="false"
SKIP_TESTS="false"
SKIP_FRONTEND="false"
SKIP_INFRA="false"
SKIP_DB_PING="false"
SKIP_EXT_HEALTH="false"
DRY_RUN="false"
GHCR_CHART_REF="oci://ghcr.io/flexprice/charts/flexprice"
GHCR_IMAGE_REPO="ghcr.io/flexprice/flexprice"
LOCAL_IMAGE_REPO="flexprice-app"
LOCAL_IMAGE_TAG="local"
LOCAL_VALUES_DEV="./values-local.yaml"
INGRESS_NGINX_MANIFEST="https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.10.1/deploy/static/provider/kind/deploy.yaml"

# ── Parse args ────────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)             MODE="$2";              shift 2 ;;
    --release)          RELEASE="$2";           shift 2 ;;
    --namespace)        NAMESPACE="$2";         shift 2 ;;
    --values)           VALUES="$2";            shift 2 ;;
    --values-extra)     VALUES_EXTRA="$2";      shift 2 ;;
    --chart)            CHART="$2";             shift 2 ;;
    --timeout)          TIMEOUT="$2";           shift 2 ;;
    --dry-run)          DRY_RUN="true";         shift ;;
    # dev flags
    --source)           SOURCE="$2";            shift 2 ;;
    --version)          VERSION="$2";           shift 2 ;;
    --image-tag)        IMAGE_TAG="$2";         shift 2 ;;
    --cluster)          CLUSTER_NAME="$2";      shift 2 ;;
    --skip-cluster)     SKIP_CLUSTER="true";    shift ;;
    --skip-tests)       SKIP_TESTS="true";      shift ;;
    --skip-frontend)    SKIP_FRONTEND="true";   shift ;;
    # prod flags
    --skip-infra)       SKIP_INFRA="true";      shift ;;
    --skip-db-ping)     SKIP_DB_PING="true";    shift ;;
    --skip-ext-health)  SKIP_EXT_HEALTH="true"; shift ;;
    -h|--help)          sed -n '2,55p' "$0"; exit 0 ;;
    *) echo "Unknown arg: $1" >&2; exit 1 ;;
  esac
done

[[ "$MODE" == "dev"  || "$MODE" == "prod" ]] || { echo "--mode must be dev|prod" >&2; exit 1; }
[[ "$SOURCE" == "local" || "$SOURCE" == "ghcr" ]] || { echo "--source must be local|ghcr" >&2; exit 1; }
[[ -z "$IMAGE_TAG" ]] && IMAGE_TAG="$VERSION"

# ── Colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'
log()  { echo -e "${BOLD}[$(date +%H:%M:%S)]${NC} $*"; }
info() { echo -e "${GREEN}[info]${NC}  $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }
warn() { echo -e "${YELLOW}⚠${NC}  $*"; }
err()  { echo -e "${RED}✗${NC} $*"; }
die()  { err "$*" >&2; exit 1; }
step() { echo -e "\n${CYAN}${BOLD}━━━ $* ━━━${NC}"; }

# ── Mode-specific resolution ──────────────────────────────────────────────────
if [[ "$MODE" == "dev" ]]; then
  if [[ "$SOURCE" == "ghcr" ]]; then
    HELM_CHART="$GHCR_CHART_REF"
    CHART_VERSION_FLAG=(--version "$VERSION")
    IMAGE_REPO="$GHCR_IMAGE_REPO"
    IMAGE_PULL_POLICY="IfNotPresent"
  else
    HELM_CHART="$CHART"
    CHART_VERSION_FLAG=()
    IMAGE_REPO="$LOCAL_IMAGE_REPO"
    IMAGE_TAG="$LOCAL_IMAGE_TAG"
    IMAGE_PULL_POLICY="Never"
  fi
  # Default to layering values-local.yaml on dev
  [[ -z "$VALUES_EXTRA" && -f "$LOCAL_VALUES_DEV" ]] && VALUES_EXTRA="$LOCAL_VALUES_DEV"
else
  HELM_CHART="$CHART"
  CHART_VERSION_FLAG=()
  IMAGE_REPO=""    # Honour values file in prod — never override
fi

# ── Helpers ───────────────────────────────────────────────────────────────────
run() {
  if [[ "$DRY_RUN" == "true" ]]; then
    echo -e "${BLUE}[dry-run]${NC} $*"
  else
    "$@"
  fi
}

helm_values_flags() {
  local flags=(--values "$VALUES")
  [[ -n "$VALUES_EXTRA" ]] && flags+=(--values "$VALUES_EXTRA")
  printf '%s\n' "${flags[@]}"
}

helm_image_flags() {
  # Only set image flags in dev; prod must come fully from values files
  if [[ "$MODE" == "dev" ]]; then
    printf '%s\n' \
      --set "image.repository=$IMAGE_REPO" \
      --set "image.tag=$IMAGE_TAG" \
      --set "image.pullPolicy=$IMAGE_PULL_POLICY"
    : # GHCR images are public — no pull-secret wiring needed
  fi
}

wait_for_local_port() {
  local port="$1" retries=30 i=1
  while [[ $i -le $retries ]]; do
    nc -z 127.0.0.1 "$port" 2>/dev/null && return 0
    sleep 1; i=$((i + 1))
  done
  return 1
}

start_port_forward() {
  local svc="$1" local_port="$2" remote_port="$3"
  kubectl port-forward --namespace "$NAMESPACE" \
    "svc/${svc}" "${local_port}:${remote_port}" &>/dev/null &
  echo $!
}

wait_statefulset() {
  local full="${RELEASE}-$1"
  log "Waiting for StatefulSet: $full"
  if [[ "$DRY_RUN" == "true" ]]; then warn "[dry-run] would wait for $full"; return; fi
  kubectl rollout status "statefulset/${full}" \
    --namespace "$NAMESPACE" --timeout "${TIMEOUT}s" \
    || warn "StatefulSet $full not found or timed out — may be external/disabled, skipping"
}

# ── Preflight ─────────────────────────────────────────────────────────────────
check_prereqs() {
  step "Prerequisites"
  local needed=(kubectl helm)
  [[ "$MODE" == "dev" ]] && needed+=(kind docker)
  [[ "$MODE" == "prod" ]] && needed+=(curl nc)

  local missing=()
  for cmd in "${needed[@]}"; do
    command -v "$cmd" &>/dev/null || missing+=("$cmd")
  done
  [[ ${#missing[@]} -eq 0 ]] || die "Missing tools: ${missing[*]}"
  ok "All tools present: ${needed[*]}"

  if [[ "$MODE" == "prod" ]]; then
    kubectl cluster-info &>/dev/null || die "Cannot reach cluster. Check kubeconfig."
    ok "Cluster reachable: $(kubectl config current-context)"
  fi

  if [[ "$SOURCE" == "local" || "$MODE" == "prod" ]] && [[ -d "$CHART" ]]; then
    if [[ ! -d "$CHART/charts" ]] || [[ -z "$(ls -A "$CHART/charts" 2>/dev/null)" ]]; then
      info "Building helm dependencies..."
      run helm dependency build "$CHART"
    else
      ok "Helm dependencies present"
    fi
  fi
}

check_required_secrets() {
  [[ "$MODE" != "prod" ]] && return
  [[ "$DRY_RUN" == "true" ]] && { warn "Dry-run: skipping secret validation"; return; }
  local missing=()
  [[ -z "${POSTGRES_PASSWORD:-}"   ]] && missing+=("POSTGRES_PASSWORD")
  [[ -z "${CLICKHOUSE_PASSWORD:-}" ]] && missing+=("CLICKHOUSE_PASSWORD")
  [[ -z "${AUTH_SECRET:-}"         ]] && missing+=("AUTH_SECRET")
  [[ -z "${ENCRYPTION_KEY:-}"      ]] && missing+=("ENCRYPTION_KEY")
  if [[ ${#missing[@]} -gt 0 ]]; then
    err "Missing required env vars:"
    for v in "${missing[@]}"; do echo "  export $v=<value>"; done
    exit 1
  fi
  ok "Required secrets present in environment"
}

# ── Cluster + ingress (dev only) ──────────────────────────────────────────────
setup_cluster_dev() {
  step "Kind cluster"
  if [[ "$SKIP_CLUSTER" == "true" ]] || kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    warn "kind cluster '${CLUSTER_NAME}' exists — skipping creation"
  else
    info "Creating kind cluster '${CLUSTER_NAME}'..."
    kind create cluster --config kind-cluster.yaml --name "$CLUSTER_NAME"
  fi
  kubectl config use-context "kind-${CLUSTER_NAME}"

  step "ingress-nginx"
  if kubectl get deployment ingress-nginx-controller -n ingress-nginx &>/dev/null; then
    warn "ingress-nginx already installed — skipping"
  else
    info "Installing ingress-nginx for kind..."
    kubectl apply -f "$INGRESS_NGINX_MANIFEST"
    info "Waiting for ingress-nginx..."
    kubectl rollout status deployment/ingress-nginx-controller -n ingress-nginx --timeout=120s
  fi
}

# ── Namespace + Secret ────────────────────────────────────────────────────────
provision_namespace_secrets() {
  step "Namespace + Secret"
  run kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
  ok "Namespace: $NAMESPACE"

  if [[ "$MODE" == "dev" ]]; then
    # GHCR FlexPrice images are public — no pull-secret needed
    return
  fi

  # Prod: build kubernetes Secret from env vars
  local literals=(
    --from-literal="postgres-password=${POSTGRES_PASSWORD:-changeme}"
    --from-literal="clickhouse-password=${CLICKHOUSE_PASSWORD:-changeme}"
    --from-literal="auth-secret=${AUTH_SECRET:-changeme}"
    --from-literal="encryption-key=${ENCRYPTION_KEY:-changeme}"
  )
  [[ -n "${REDIS_PASSWORD:-}"          ]] && literals+=(--from-literal="redis-password=${REDIS_PASSWORD}")
  [[ -n "${KAFKA_SASL_PASSWORD:-}"     ]] && literals+=(--from-literal="kafka-sasl-password=${KAFKA_SASL_PASSWORD}")
  [[ -n "${TEMPORAL_API_KEY:-}"        ]] && literals+=(--from-literal="temporal-api-key=${TEMPORAL_API_KEY}")
  [[ -n "${SUPABASE_SERVICE_KEY:-}"    ]] && literals+=(--from-literal="supabase-service-key=${SUPABASE_SERVICE_KEY}")
  [[ -n "${SENTRY_DSN:-}"              ]] && literals+=(--from-literal="sentry-dsn=${SENTRY_DSN}")
  [[ -n "${PYROSCOPE_PASSWORD:-}"      ]] && literals+=(--from-literal="pyroscope-basic-auth-password=${PYROSCOPE_PASSWORD}")
  [[ -n "${RESEND_API_KEY:-}"          ]] && literals+=(--from-literal="email-resend-api-key=${RESEND_API_KEY}")
  [[ -n "${SVIX_TOKEN:-}"              ]] && literals+=(--from-literal="svix-auth-token=${SVIX_TOKEN}")
  [[ -n "${LOGGING_OTEL_AUTH_VALUE:-}" ]] && literals+=(--from-literal="logging-otel-auth-value=${LOGGING_OTEL_AUTH_VALUE}")

  if [[ "$DRY_RUN" == "true" ]]; then
    echo -e "${BLUE}[dry-run]${NC} kubectl create secret generic ${RELEASE}-secrets [...]"
  else
    kubectl create secret generic "${RELEASE}-secrets" \
      --namespace "$NAMESPACE" \
      "${literals[@]}" \
      --dry-run=client -o yaml | kubectl apply -f -
  fi
  ok "Secret ${RELEASE}-secrets created/updated"
}

# ── GHCR login (dev) ──────────────────────────────────────────────────────────
# GHCR login is no longer needed — FlexPrice images and chart are published as public packages.

# ── Build + load API image (dev/local source only) ────────────────────────────
build_load_image_dev() {
  [[ "$MODE" == "dev" && "$SOURCE" == "local" ]] || return
  step "Build & load API image"
  local api_dir
  api_dir="$(cd "$(dirname "$0")/.." && pwd)"
  info "Building API image from $api_dir ..."
  docker build -f "$api_dir/Dockerfile.local" -t "${LOCAL_IMAGE_REPO}:${LOCAL_IMAGE_TAG}" "$api_dir"
  info "Loading into kind..."
  kind load docker-image "${LOCAL_IMAGE_REPO}:${LOCAL_IMAGE_TAG}" --name "$CLUSTER_NAME"
}

# ── Phase 1: Infra ────────────────────────────────────────────────────────────
deploy_infra() {
  if [[ "$MODE" == "prod" && "$SKIP_INFRA" == "true" ]]; then
    log "--skip-infra set — using external infra (RDS, MSK, ElastiCache, Temporal Cloud, etc.)"
    return
  fi
  step "Phase 1 — Infra (postgres, clickhouse, kafka, redis, temporal)"
  local vf imgf
  mapfile -t vf   < <(helm_values_flags)
  mapfile -t imgf < <(helm_image_flags)
  run helm upgrade --install "$RELEASE" "$HELM_CHART" \
    "${CHART_VERSION_FLAG[@]}" \
    "${vf[@]}" \
    "${imgf[@]}" \
    --namespace "$NAMESPACE" \
    --set api.enabled=false \
    --set consumer.enabled=false \
    --set worker.enabled=false \
    --set frontend.enabled=false \
    --set migration.enabled=false \
    --set ingress.enabled=false \
    --timeout "${TIMEOUT}s" --wait

  wait_statefulset "postgresql"
  wait_statefulset "kafka-controller"
  wait_statefulset "redis-master"
  wait_statefulset "clickhouse"
  ok "Infra ready"

  # Restart Temporal to refresh DB DNS (race fix)
  if [[ "$MODE" == "dev" ]]; then
    info "Restarting Temporal server pods to refresh DB connection..."
    for dep in \
      "${RELEASE}-temporal-frontend" \
      "${RELEASE}-temporal-history" \
      "${RELEASE}-temporal-matching" \
      "${RELEASE}-temporal-worker"; do
      kubectl rollout restart deployment "$dep" -n "$NAMESPACE" 2>/dev/null || true
    done
    # Bootstrap Temporal default namespace
    info "Bootstrapping Temporal 'default' namespace..."
    kubectl exec -n "$NAMESPACE" "deploy/${RELEASE}-temporal-admintools" -- \
      temporal operator namespace create --namespace default --retention 72h 2>&1 \
      | grep -v 'already registered' || true
  fi
}

# ── Phase 2: DB ping (prod) ───────────────────────────────────────────────────
ping_postgres()   { local port=55432; local pid; pid=$(start_port_forward "${RELEASE}-postgresql" $port 5432); wait_for_local_port $port || { kill $pid; return 1; }; PGPASSWORD="${POSTGRES_PASSWORD:-}" psql -h 127.0.0.1 -p $port -U flexprice -d flexprice -c "SELECT 1" &>/dev/null && ok "PostgreSQL: healthy" || { err "PostgreSQL ping failed"; kill $pid; return 1; }; kill $pid 2>/dev/null || true; }
ping_clickhouse() { local port=58123; local pid; pid=$(start_port_forward "${RELEASE}-clickhouse" $port 8123); wait_for_local_port $port || { kill $pid; return 1; }; curl -sf "http://127.0.0.1:${port}/ping" &>/dev/null && ok "ClickHouse: healthy" || { err "ClickHouse ping failed"; kill $pid; return 1; }; kill $pid 2>/dev/null || true; }
ping_redis()      { local port=56379; local pid; pid=$(start_port_forward "${RELEASE}-redis-master" $port 6379); wait_for_local_port $port || { kill $pid; return 1; }; local cmd=(redis-cli -h 127.0.0.1 -p $port); [[ -n "${REDIS_PASSWORD:-}" ]] && cmd+=(-a "${REDIS_PASSWORD}"); "${cmd[@]}" PING 2>/dev/null | grep -q "PONG" && ok "Redis: healthy" || { err "Redis ping failed"; kill $pid; return 1; }; kill $pid 2>/dev/null || true; }
ping_kafka()      { local port=59092; local pid; pid=$(start_port_forward "${RELEASE}-kafka" $port 9092); wait_for_local_port $port || { kill $pid; return 1; }; nc -z 127.0.0.1 $port 2>/dev/null && ok "Kafka: healthy (port open)" || { err "Kafka unreachable"; kill $pid; return 1; }; kill $pid 2>/dev/null || true; }

ping_databases() {
  [[ "$MODE" != "prod" ]] && return
  [[ "$SKIP_DB_PING" == "true" || "$SKIP_INFRA" == "true" || "$DRY_RUN" == "true" ]] && { warn "DB ping skipped"; return; }
  step "Phase 2 — Database connectivity"
  command -v psql      &>/dev/null && ping_postgres   || warn "psql missing — skipping postgres ping"
  ping_clickhouse
  command -v redis-cli &>/dev/null && ping_redis      || warn "redis-cli missing — skipping redis ping"
  ping_kafka
}

# ── Phase 3: Migrations ───────────────────────────────────────────────────────
run_migrations() {
  step "Phase 3 — Migrations"
  local vf imgf
  mapfile -t vf   < <(helm_values_flags)
  mapfile -t imgf < <(helm_image_flags)
  run helm upgrade --install "$RELEASE" "$HELM_CHART" \
    "${CHART_VERSION_FLAG[@]}" \
    "${vf[@]}" \
    "${imgf[@]}" \
    --namespace "$NAMESPACE" \
    --set api.enabled=false \
    --set consumer.enabled=false \
    --set worker.enabled=false \
    --set frontend.enabled=false \
    --set migration.enabled=true \
    --set ingress.enabled=false \
    --timeout "${TIMEOUT}s" --wait
  ok "Migrations complete"
}

# ── Phase 4: App pods ─────────────────────────────────────────────────────────
deploy_services() {
  step "Phase 4 — Deploy app (api, consumer, worker)"
  local vf imgf
  mapfile -t vf   < <(helm_values_flags)
  mapfile -t imgf < <(helm_image_flags)
  run helm upgrade --install "$RELEASE" "$HELM_CHART" \
    "${CHART_VERSION_FLAG[@]}" \
    "${vf[@]}" \
    "${imgf[@]}" \
    --namespace "$NAMESPACE" \
    --set api.enabled=true \
    --set consumer.enabled=true \
    --set worker.enabled=true \
    --set frontend.enabled=false \
    --set migration.enabled=false \
    --set ingress.enabled=false \
    --timeout "${TIMEOUT}s" --wait

  if [[ "$DRY_RUN" != "true" ]]; then
    for d in api consumer worker; do
      kubectl rollout status "deployment/${RELEASE}-${d}" \
        --namespace "$NAMESPACE" --timeout "${TIMEOUT}s" \
        || warn "${RELEASE}-${d} rollout timed out"
    done

    # Internal health check via port-forward
    local port=58080 pid
    pid=$(start_port_forward "${RELEASE}" $port 80)
    if wait_for_local_port $port && curl -sf "http://127.0.0.1:${port}/health" &>/dev/null; then
      ok "Internal /health → 200"
    else
      warn "Internal health check failed"
    fi
    kill $pid 2>/dev/null || true
  fi
}

# ── Phase 5: Ingress ──────────────────────────────────────────────────────────
deploy_ingress() {
  step "Phase 5 — Ingress"
  local vf imgf
  mapfile -t vf   < <(helm_values_flags)
  mapfile -t imgf < <(helm_image_flags)
  run helm upgrade --install "$RELEASE" "$HELM_CHART" \
    "${CHART_VERSION_FLAG[@]}" \
    "${vf[@]}" \
    "${imgf[@]}" \
    --namespace "$NAMESPACE" \
    --set api.enabled=true \
    --set consumer.enabled=true \
    --set worker.enabled=true \
    --set frontend.enabled=false \
    --set migration.enabled=false \
    --set ingress.enabled=true \
    --timeout "${TIMEOUT}s" --wait
  ok "Ingress deployed"
  [[ "$DRY_RUN" != "true" ]] && (kubectl get ingress -n "$NAMESPACE" -o wide || true)
}

# ── Phase 6 (dev): Integration tests ──────────────────────────────────────────
integration_tests_dev() {
  [[ "$MODE" != "dev" || "$SKIP_TESTS" == "true" ]] && return
  step "Integration sanity test"
  local suite_dir
  suite_dir="$(cd "$(dirname "$0")/../integration-testing-suite/go" 2>/dev/null && pwd || true)"
  [[ -d "$suite_dir" ]] || { warn "integration-testing-suite/go not found — skipping"; return; }
  info "Building test image..."
  docker build -t flexprice-integration-test:local "$suite_dir"
  kind load docker-image flexprice-integration-test:local --name "$CLUSTER_NAME"
  kubectl run integration-test --rm --attach --restart=Never \
    -n "$NAMESPACE" \
    --image=flexprice-integration-test:local \
    --image-pull-policy=IfNotPresent \
    --env="FLEXPRICE_API_KEY=sk_local_flexprice_test_key" \
    --env="FLEXPRICE_API_HOST=flexprice.$NAMESPACE.svc.cluster.local/v1" \
    --env="FLEXPRICE_INSECURE=true" \
    || warn "Integration tests failed — see output"
}

# ── Phase 7 (dev): Frontend ───────────────────────────────────────────────────
frontend_dev() {
  [[ "$MODE" != "dev" || "$SKIP_FRONTEND" == "true" || "$SOURCE" != "local" ]] && return
  step "Frontend"
  local fe_dir
  fe_dir="$(cd "$(dirname "$0")/../../flexprice-front" 2>/dev/null && pwd || true)"
  [[ -d "$fe_dir" ]] || { warn "flexprice-front not found — skipping"; return; }
  info "Building frontend image..."
  docker build \
    --build-arg VITE_API_URL="http://api.flexprice.local/v1" \
    --build-arg VITE_ENVIRONMENT="self-hosted" \
    -t flexprice-frontend:local "$fe_dir"
  kind load docker-image flexprice-frontend:local --name "$CLUSTER_NAME"

  local vf imgf
  mapfile -t vf   < <(helm_values_flags)
  mapfile -t imgf < <(helm_image_flags)
  helm upgrade "$RELEASE" "$HELM_CHART" \
    "${CHART_VERSION_FLAG[@]}" \
    "${vf[@]}" \
    "${imgf[@]}" \
    --namespace "$NAMESPACE" \
    --set api.enabled=true \
    --set consumer.enabled=true \
    --set worker.enabled=true \
    --set frontend.enabled=true \
    --set migration.enabled=false \
    --set ingress.enabled=true \
    --wait --timeout 3m
}

# ── Phase 8 (prod): External health check ─────────────────────────────────────
external_health_check_prod() {
  [[ "$MODE" != "prod" ]] && return
  [[ "$SKIP_EXT_HEALTH" == "true" ]] && { warn "External health skipped"; return; }
  [[ -z "${INGRESS_HOST:-}" ]] && { warn "INGRESS_HOST not set — skipping external health check"; return; }
  [[ "$DRY_RUN" == "true" ]] && { warn "[dry-run] would curl https://${INGRESS_HOST}/health"; return; }

  step "External health check"
  log "Waiting for DNS / LB to propagate (up to 120s)..."
  local retries=24 i=1 http_code
  while [[ $i -le $retries ]]; do
    http_code=$(curl -sk -o /dev/null -w "%{http_code}" "https://${INGRESS_HOST}/health" 2>/dev/null || echo "000")
    [[ "$http_code" == "200" ]] && { ok "External /health → 200"; return; }
    echo "  [$i/$retries] HTTP $http_code — retrying in 5s..."
    sleep 5; i=$((i + 1))
  done
  err "External health check failed: https://${INGRESS_HOST}/health"
  err "Debug: kubectl describe ingress -n $NAMESPACE"
  exit 1
}

# ── Summary ───────────────────────────────────────────────────────────────────
print_summary_dev() {
  echo ""
  echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${GREEN}  FlexPrice is up! (mode=dev, source=$SOURCE, tag=$IMAGE_TAG)${NC}"
  echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo ""
  echo "  UI:          http://flexprice.local"
  echo "  API:         http://api.flexprice.local"
  echo "  Temporal UI: http://temporal.flexprice.local"
  echo ""
  echo "  /etc/hosts:  echo '127.0.0.1 flexprice.local api.flexprice.local temporal.flexprice.local' | sudo tee -a /etc/hosts"
  echo "  Teardown:    kind delete cluster --name ${CLUSTER_NAME}"
  echo ""
}

print_summary_prod() {
  echo ""
  echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${GREEN}  Provisioning complete (mode=prod)${NC}"
  echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo ""
  echo -e "  Release:    ${CYAN}$RELEASE${NC}"
  echo -e "  Namespace:  ${CYAN}$NAMESPACE${NC}"
  [[ -n "$VALUES_EXTRA" ]] && echo -e "  Overrides:  ${CYAN}$VALUES_EXTRA${NC}"
  [[ -n "${INGRESS_HOST:-}" ]] && echo -e "  Endpoint:   ${GREEN}${BOLD}https://${INGRESS_HOST}${NC}"
  echo ""
  echo "  kubectl get pods -n $NAMESPACE"
  echo "  kubectl logs -n $NAMESPACE -l app.kubernetes.io/name=flexprice -f"
  echo ""
}

# ── Main ──────────────────────────────────────────────────────────────────────
echo -e "${BOLD}FlexPrice deployer${NC}  mode=${CYAN}$MODE${NC}  release=${CYAN}$RELEASE${NC}  ns=${CYAN}$NAMESPACE${NC}  dry-run=${CYAN}$DRY_RUN${NC}"
[[ -n "$VALUES_EXTRA" ]] && echo -e "  extra-values=${CYAN}$VALUES_EXTRA${NC}"

check_prereqs
check_required_secrets

if [[ "$MODE" == "dev" ]]; then
  setup_cluster_dev
fi

provision_namespace_secrets
build_load_image_dev

deploy_infra
ping_databases
run_migrations
deploy_services
deploy_ingress
external_health_check_prod
integration_tests_dev
frontend_dev

if [[ "$MODE" == "dev" ]]; then
  print_summary_dev
else
  print_summary_prod
fi
