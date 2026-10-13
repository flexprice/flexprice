#!/usr/bin/env bash
# Asserts the GCE ingress path renders, and that load-bearing defaults hold.
#
# Two independent checks, because they fail differently:
#
#   render   a values file using ingress.gce must emit the Ingress,
#            BackendConfig, FrontendConfig and Certificate, with its
#            static IP, hosts and TLS secret intact. A resolver that
#            stops reading ingress.gce renders a degraded object
#            rather than erroring.
#
#   defaults values.yaml defaults are invisible to a render diff when
#            every consumer sets the key explicitly. A default that
#            silently halves a timeout changes no rendered object.

set -euo pipefail

HELM_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART="$HELM_DIR/flexprice"
FIXTURE="$HELM_DIR/values-gce.example.yaml"
OUT=$(mktemp)
trap 'rm -f "$OUT"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

echo "== render $FIXTURE"
helm template flexprice "$CHART" -f "$FIXTURE" > "$OUT"

# An empty render reports as "no differences" downstream.
lines=$(wc -l < "$OUT" | tr -d ' ')
[ "$lines" -gt 200 ] || fail "render produced $lines lines, expected >200"
echo "   $lines lines"

echo "== GCE objects present"
for kind in Ingress BackendConfig FrontendConfig Certificate; do
  grep -qE "^kind: $kind$" "$OUT" || fail "$kind absent from the GCE render"
  echo "   $kind ok"
done

echo "== Ingress fields that degrade silently"
python3 - "$OUT" <<'PY'
import sys, re, yaml

docs = [d for d in open(sys.argv[1]).read().split('\n---\n') if d.strip()]

def first(kind):
    for d in docs:
        if re.search(rf'^kind: {kind}$', d, re.M):
            return yaml.safe_load(d)
    sys.exit(f"FAIL: no {kind} in render")

ing = first('Ingress')
ann = ing['metadata'].get('annotations') or {}

checks = [
    ("static IP annotation",
     ann.get('kubernetes.io/ingress.global-static-ip-name'),
     lambda v: bool(v)),
    ("hosts are the fixture's, not the chart example",
     [r['host'] for r in ing['spec']['rules']],
     lambda v: v and all('flexprice.example.com' != h for h in v)),
    ("TLS secret set",
     [t.get('secretName') for t in ing['spec'].get('tls') or []],
     lambda v: v and all(v)),
]

bc = first('BackendConfig')
checks.append(("BackendConfig timeoutSec", bc['spec'].get('timeoutSec'),
               lambda v: v == 60))

bad = False
for name, got, ok in checks:
    if ok(got):
        print(f"   {name}: {got}")
    else:
        print(f"FAIL: {name}: got {got!r}", file=sys.stderr)
        bad = True
sys.exit(1 if bad else 0)
PY

echo "== values.yaml defaults"
python3 - "$CHART/values.yaml" <<'PY'
import sys, yaml

gce = (yaml.safe_load(open(sys.argv[1]))['ingress'] or {}).get('gce') or {}

# Pinned because a render diff cannot see them: every real consumer
# sets these explicitly, so a changed default shifts no rendered object.
expect = {
    "backendConfig.timeoutSec": (60,
        "halving this cuts requests that finish within 60s"),
    "tls.mode": ("certManager",
        "managed validates over HTTP after DNS moves, forcing a TLS gap"),
}

def dig(d, path):
    for k in path.split('.'):
        if not isinstance(d, dict) or k not in d:
            return KeyError
        d = d[k]
    return d

bad = False
for path, (want, why) in expect.items():
    got = dig(gce, path)
    if got != want:
        print(f"FAIL: ingress.gce.{path} is {got!r}, expected {want!r} — {why}", file=sys.stderr)
        bad = True
    else:
        print(f"   ingress.gce.{path} = {got}")

# Nil here is a render-time crash, not a wrong value.
for path, kind in [("ownService", bool), ("service.annotations", dict)]:
    got = dig(gce, path)
    if got is KeyError or not isinstance(got, kind):
        print(f"FAIL: ingress.gce.{path} must be a {kind.__name__}, got {got!r}", file=sys.stderr)
        bad = True
    else:
        print(f"   ingress.gce.{path} is {kind.__name__}")

sys.exit(1 if bad else 0)
PY

echo "== the removed gceIngress key must fail loudly"
legacy=$(mktemp); trap 'rm -f "$OUT" "$legacy"' EXIT
cat > "$legacy" <<'YAML'
secrets:
  existingSecret: flexprice-secrets
gceIngress:
  enabled: true
YAML
if helm template flexprice "$CHART" -f "$legacy" >/dev/null 2>&1; then
  fail "a values file setting gceIngress rendered instead of failing"
fi
echo "   rejected"

echo "OK"
