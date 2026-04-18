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
#   API:         http://flexprice.local  (add 127.0.0.1 flexprice.local to /etc/hosts)
#   Temporal UI: kubectl port-forward -n flexprice svc/flexprice-temporal-ui 8088:8088
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
info "Installing ingress-nginx for kind..."
kubectl apply -f "$INGRESS_NGINX_MANIFEST"

info "Waiting for ingress-nginx to be ready..."
kubectl rollout status deployment/ingress-nginx-controller \
  -n ingress-nginx --timeout=120s

# ── Namespace ─────────────────────────────────────────────────────────────────
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

# ── Helm dependencies ─────────────────────────────────────────────────────────
info "Updating helm dependencies..."
helm dependency update "$CHART"

# ── Phase 1: Infra only ───────────────────────────────────────────────────────
info "Phase 1/3 — deploying infrastructure (postgres, clickhouse, kafka, redis, temporal)..."
helm upgrade --install "$RELEASE" "$CHART" \
  -f "$BASE_VALUES" -f "$LOCAL_VALUES" \
  --set api.enabled=false \
  --set consumer.enabled=false \
  --set worker.enabled=false \
  --set migration.enabled=false \
  -n "$NAMESPACE" \
  --wait --timeout 10m

# ── Phase 2: Migration ────────────────────────────────────────────────────────
info "Phase 2/3 — running migrations..."
helm upgrade "$RELEASE" "$CHART" \
  -f "$BASE_VALUES" -f "$LOCAL_VALUES" \
  --set api.enabled=false \
  --set consumer.enabled=false \
  --set worker.enabled=false \
  --set migration.enabled=true \
  -n "$NAMESPACE" \
  --wait --timeout 5m

# ── Phase 3: App pods ─────────────────────────────────────────────────────────
info "Phase 3/3 — deploying application pods (api, consumer, worker)..."
helm upgrade "$RELEASE" "$CHART" \
  -f "$BASE_VALUES" -f "$LOCAL_VALUES" \
  -n "$NAMESPACE" \
  --wait --timeout 5m

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  FlexPrice is up!${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "  API:         http://flexprice.local"
echo ""
echo "  If http://flexprice.local doesn't resolve, add to /etc/hosts:"
echo "    echo '127.0.0.1 flexprice.local' | sudo tee -a /etc/hosts"
echo ""
echo "  Temporal UI: kubectl port-forward -n ${NAMESPACE} svc/flexprice-temporal-ui 8088:8088"
echo "               then open http://localhost:8088"
echo ""
echo "  Teardown:    kind delete cluster --name ${CLUSTER_NAME}"
echo ""
