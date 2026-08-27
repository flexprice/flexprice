#!/usr/bin/env bash
# bento.test.sh — helm-template assertions for the optional bento collector.
#
# Bento is an optional stream collector (Kafka/MSK -> in-cluster Flexprice API).
# It is a gated app Deployment (like consumer/worker): off by default, config-
# agnostic (client supplies image + config path + source env via values), and
# it reuses the existing flexprice-secrets Secret for MSK SCRAM creds.
#
# Run: bash templates/tests/bento.test.sh   (from the chart root)
set -euo pipefail

CHART_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fail=0
pass=0
check() { # check "desc" <cond-cmd...>  — cond is a grep-style predicate on $OUT
  local desc="$1"; shift
  if "$@"; then pass=$((pass+1)); else echo "  ✗ FAIL: $desc"; fail=$((fail+1)); fi
}
has()  { grep -qE "$1" <<<"$OUT"; }
hasnt() { ! grep -qE "$1" <<<"$OUT"; }

# Minimal values every render needs (chart requires a credential source). Using
# existingSecret both satisfies that gate and matches how bento really runs in
# BYOC — it references the same flexprice-secrets Secret for MSK SCRAM creds.
BASE=(--set secrets.existingSecret=t-flexprice-secrets)

echo "== bento disabled by default renders nothing =="
OUT="$(helm template t "$CHART_DIR" "${BASE[@]}" 2>/dev/null)"
check "no bento Deployment when bento.enabled unset (default false)" \
  hasnt 'name: t-flexprice-bento'

echo "== bento enabled renders a Deployment =="
OUT="$(helm template t "$CHART_DIR" "${BASE[@]}" \
  --set bento.enabled=true \
  --set bento.image.repository=ghcr.io/flexprice/bento-collector \
  --set bento.image.tag=v1.2.1 \
  --set bento.configPath=/app/internal/aws-kafka-to-flexprice.yaml \
  2>/dev/null)"

check "bento Deployment rendered"                 has 'kind: Deployment'
check "bento Deployment named <release>-bento"    has 'name: t-flexprice-bento'
check "bento image wired from values"             has 'ghcr.io/flexprice/bento-collector:v1.2.1'
check "config path passed as -c arg"              has '/app/internal/aws-kafka-to-flexprice.yaml'
# In-cluster API target (user decision: post to the api Service, not external host)
check "FLEXPRICE_API_HOST = in-cluster api svc"   has 'value: "?t-flexprice-api"?'
check "FLEXPRICE_SCHEME http (in-cluster, no TLS)" has 'FLEXPRICE_SCHEME'
# MSK SCRAM creds reused from the existing flexprice-secrets Secret
check "SASL password via secretKeyRef"            has 'key: kafka-sasl-password'
check "SASL username via secretKeyRef"            has 'key: kafka-sasl-username'
check "secret ref points at flexprice secret"     has 'name: t-flexprice-secrets'
# Bento HTTP port + probes on :4195 (bento default)
check "bento http port 4195"                      has 'containerPort: 4195'
check "liveness on /ping"                          has '/ping'
check "readiness on /ready"                        has '/ready'

echo "== bento default image tag =="
OUT="$(helm template t "$CHART_DIR" "${BASE[@]}" --set bento.enabled=true 2>/dev/null)"
check "default image tag is the pinned v1.2.5" has 'ghcr.io/flexprice/bento-collector:v1.2.5'

echo "== bento honors replicaCount + free-form env =="
OUT="$(helm template t "$CHART_DIR" "${BASE[@]}" \
  --set bento.enabled=true \
  --set bento.replicaCount=2 \
  --set bento.env.AWS_KAFKA_TOPIC=events \
  2>/dev/null)"
check "replicaCount honored"     has 'replicas: 2'
check "free-form env rendered"   has 'AWS_KAFKA_TOPIC'

echo
if [[ "$fail" -gt 0 ]]; then
  echo "BENTO TESTS: $pass passed, $fail FAILED"; exit 1
fi
echo "BENTO TESTS: all $pass passed"
