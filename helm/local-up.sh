#!/usr/bin/env bash
# local-up.sh — One-command FlexPrice stack on kind
#
# Prerequisites:
#   - kind    (https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
#   - kubectl (https://kubernetes.io/docs/tasks/tools/)
#   - helm    (https://helm.sh/docs/intro/install/)
#
# Usage:
#   cd helm/
#   ./local-up.sh
#
# Access after startup:
#   UI:          http://flexprice.local
#   API:         http://api.flexprice.local
#   Temporal UI: http://temporal.flexprice.local
#   (add all three to /etc/hosts: 127.0.0.1 flexprice.local api.flexprice.local temporal.flexprice.local)
#
# Teardown:
#   kind delete cluster --name flexprice-local

set -euo pipefail

CLUSTER_NAME="flexprice-local"
NAMESPACE="flexprice"
RELEASE="flexprice"
CHART="./flexprice"
BASE_VALUES="./flexprice/values.yaml"
LOCAL_VALUES="./values-local.yaml"
INGRESS_NGINX_MANIFEST="https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.10.1/deploy/static/provider/kind/deploy.yaml"

# ── Colours ───────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${GREEN}[local-up]${NC} $*"; }
warn()  { echo -e "${YELLOW}[local-up]${NC} $*"; }
die()   { echo -e "${RED}[local-up] ERROR:${NC} $*" >&2; exit 1; }

# ── Preflight ─────────────────────────────────────────────────────────────────
for cmd in kind kubectl helm; do
  command -v "$cmd" &>/dev/null || die "'$cmd' not found. Install it first."
done

[[ -f "$LOCAL_VALUES" ]] || die "values-local.yaml not found in $(pwd). Copy the template from the spec."
[[ -f "kind-cluster.yaml" ]] || die "kind-cluster.yaml not found. Run this script from the helm/ directory."

# ── Cluster ───────────────────────────────────────────────────────────────────
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  warn "kind cluster '${CLUSTER_NAME}' already exists — skipping creation"
else
  info "Creating kind cluster '${CLUSTER_NAME}'..."
  kind create cluster --config kind-cluster.yaml --name "$CLUSTER_NAME"
fi

kubectl config use-context "kind-${CLUSTER_NAME}"

# ── ingress-nginx ─────────────────────────────────────────────────────────────
if kubectl get deployment ingress-nginx-controller -n ingress-nginx &>/dev/null; then
  warn "ingress-nginx already installed — skipping"
else
  info "Installing ingress-nginx for kind..."
  kubectl apply -f "$INGRESS_NGINX_MANIFEST"

  info "Waiting for ingress-nginx to be ready..."
  kubectl rollout status deployment/ingress-nginx-controller \
    -n ingress-nginx --timeout=120s
fi

# ── Namespace ─────────────────────────────────────────────────────────────────
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

# ── Helm dependencies ─────────────────────────────────────────────────────────
info "Updating helm dependencies..."
CHART_DIR="$(cd "$(dirname "$0")/${CHART}" && pwd)"
(cd "$CHART_DIR" && helm dependency update .)

# ── Phase 1: Infra only ───────────────────────────────────────────────────────
info "Phase 1/5 — deploying infrastructure (postgres, clickhouse, kafka, redis, temporal)..."
helm upgrade --install "$RELEASE" "$CHART" \
  -f "$BASE_VALUES" -f "$LOCAL_VALUES" \
  --set api.enabled=false \
  --set consumer.enabled=false \
  --set worker.enabled=false \
  --set frontend.enabled=false \
  --set migration.enabled=false \
  -n "$NAMESPACE" \
  --wait --timeout 10m

# Restart Temporal server pods after schema jobs complete to avoid stale DNS race
info "Restarting Temporal server pods to pick up fresh DB connection..."
for dep in flexprice-temporal-frontend flexprice-temporal-history flexprice-temporal-matching flexprice-temporal-worker; do
  kubectl rollout restart deployment "$dep" -n "$NAMESPACE" 2>/dev/null || true
done

# ── Temporal namespace bootstrap ──────────────────────────────────────────────
info "Bootstrapping Temporal 'default' namespace..."
kubectl exec -n "$NAMESPACE" deploy/flexprice-temporal-admintools -- \
  temporal operator namespace create --namespace default --retention 72h 2>&1 | grep -v 'already registered' || true

# ── API image ─────────────────────────────────────────────────────────────────
API_DIR="$(cd "$(dirname "$0")/.." && pwd)"
info "Building API image from $API_DIR ..."
docker build -f "$API_DIR/Dockerfile.local" -t flexprice-app:local "$API_DIR"
info "Loading API image into kind cluster..."
kind load docker-image flexprice-app:local --name "$CLUSTER_NAME"

# ── Phase 2: Migration ────────────────────────────────────────────────────────
info "Phase 2/5 — running migrations..."
helm upgrade "$RELEASE" "$CHART" \
  -f "$BASE_VALUES" -f "$LOCAL_VALUES" \
  --set api.enabled=false \
  --set consumer.enabled=false \
  --set worker.enabled=false \
  --set frontend.enabled=false \
  --set migration.enabled=true \
  -n "$NAMESPACE" \
  --wait --timeout 5m

# ── Phase 3: App pods (api, consumer, worker) ─────────────────────────────────
info "Phase 3/5 — deploying application pods (api, consumer, worker)..."
helm upgrade "$RELEASE" "$CHART" \
  -f "$BASE_VALUES" -f "$LOCAL_VALUES" \
  --set api.enabled=true \
  --set consumer.enabled=true \
  --set worker.enabled=true \
  --set frontend.enabled=false \
  --set migration.enabled=false \
  -n "$NAMESPACE" \
  --wait --timeout 5m

# ── Phase 4: Integration sanity test ─────────────────────────────────────────
info "Phase 4/5 — running integration sanity test..."
SUITE_DIR="$(cd "$(dirname "$0")/../integration-testing-suite/go" && pwd)"
info "Building integration test image from $SUITE_DIR ..."
docker build -t flexprice-integration-test:local "$SUITE_DIR"
info "Loading integration test image into kind cluster..."
kind load docker-image flexprice-integration-test:local --name "$CLUSTER_NAME"

info "Running integration sanity test (logs follow)..."
kubectl run integration-test --rm --attach --restart=Never \
  -n "$NAMESPACE" \
  --image=flexprice-integration-test:local \
  --image-pull-policy=IfNotPresent \
  --env="FLEXPRICE_API_KEY=sk_local_flexprice_test_key" \
  --env="FLEXPRICE_API_HOST=flexprice.$NAMESPACE.svc.cluster.local/v1" \
  --env="FLEXPRICE_INSECURE=true" \
  || warn "Integration tests finished with failures — check output above"

# ── Frontend image ────────────────────────────────────────────────────────────
FRONTEND_DIR="$(cd "$(dirname "$0")/../../flexprice-front" 2>/dev/null && pwd || true)"
if [[ -d "$FRONTEND_DIR" ]]; then
  info "Building frontend image from $FRONTEND_DIR ..."
  docker build \
    --build-arg VITE_API_URL="http://api.flexprice.local/v1" \
    --build-arg VITE_ENVIRONMENT="self-hosted" \
    -t flexprice-frontend:local \
    "$FRONTEND_DIR"
  info "Loading frontend image into kind cluster..."
  kind load docker-image flexprice-frontend:local --name "$CLUSTER_NAME"
else
  warn "flexprice-front directory not found at $FRONTEND_DIR — skipping frontend build"
fi

# ── Phase 5: Frontend ─────────────────────────────────────────────────────────
info "Phase 5/5 — deploying frontend..."
helm upgrade "$RELEASE" "$CHART" \
  -f "$BASE_VALUES" -f "$LOCAL_VALUES" \
  --set api.enabled=true \
  --set consumer.enabled=true \
  --set worker.enabled=true \
  --set frontend.enabled=true \
  --set migration.enabled=false \
  -n "$NAMESPACE" \
  --wait --timeout 3m

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  FlexPrice is up!${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "  UI:          http://flexprice.local"
echo "  API:         http://api.flexprice.local"
echo "  Temporal UI: http://temporal.flexprice.local"
echo ""
echo "  If these don't resolve, add to /etc/hosts:"
echo "    echo '127.0.0.1 flexprice.local api.flexprice.local temporal.flexprice.local' | sudo tee -a /etc/hosts"
echo ""
echo "  Teardown:    kind delete cluster --name ${CLUSTER_NAME}"
echo ""
