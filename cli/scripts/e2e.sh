#!/usr/bin/env bash
#
# End-to-end lifecycle suite for the Flexprice CLI, driving the real binary
# against the India region.
#
#   cli/scripts/e2e.sh                 # prompts for the API key
#   FLEXPRICE_API_KEY=sk_... cli/scripts/e2e.sh
#   cli/scripts/e2e.sh --dry-run       # print every command, call nothing
#
# Builds the full object graph — feature → plan → price → entitlement →
# coupon → customer → subscription → wallet → events → invoice — verifies each
# step, then tears it all down in reverse.
#
# Region is locked to India (https://api.cloud.flexprice.io/v1). It is not a
# flag: this suite creates and deletes real billing objects, and a suite that
# can be pointed anywhere by accident is one that eventually deletes something
# in the wrong place.
#
# Exit status: non-zero only when a CORE phase fails. Cleanup failures are
# reported loudly (they mean leaked objects) but never fail the run, so a
# teardown hiccup cannot mask an otherwise green lifecycle.

set -uo pipefail

CLI_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$CLI_DIR/bin/flexprice"

readonly REGION="in"
readonly REGION_URL="https://api.cloud.flexprice.io/v1"

DRY_RUN=""
[ "${1:-}" = "--dry-run" ] && DRY_RUN=1

# login/whoami reach the real OS keychain otherwise, and an unattended run can
# raise a blocking macOS dialog whose default button is destructive.
# Pinned before HOME is overridden: otherwise `go build` recreates a module
# cache inside the sandbox with read-only dirs that cleanup cannot remove.
export GOMODCACHE="${GOMODCACHE:-$(go env GOMODCACHE)}"
export GOCACHE="${GOCACHE:-$(go env GOCACHE)}"

SANDBOX="$(mktemp -d)"
export HOME="$SANDBOX"
export FLEXPRICE_KEY_BACKEND=file

PASSED=0; FAILED=0; SKIPPED=0; STEP=0
PHASE=""; CORE_FAILED=0; CLEANUP_FAILED=0
declare -a FAILURES=(); declare -a CLEANUP_FAILURES=()
START=$SECONDS

# Entity ids, populated as the graph is built and consumed by teardown.
FEATURE_ID=""; PLAN_ID=""; PRICE_FIXED_ID=""; PRICE_USAGE_ID=""
ENTITLEMENT_ID=""; COUPON_ID=""; CUSTOMER_ID=""; SUBSCRIPTION_ID=""
WALLET_ID=""; INVOICE_ID=""
STAMP="$(date +%s)"
SLUG="clie2e$STAMP"

cleanup_sandbox() { rm -rf "$SANDBOX"; }
trap cleanup_sandbox EXIT

# ── output ──────────────────────────────────────────────────────────────

c() { if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then printf '\033[%sm%s\033[0m' "$1" "$2"; else printf '%s' "$2"; fi; }

phase() {
    PHASE="$1"
    local pad=$(( 58 - ${#1} )); [ $pad -lt 0 ] && pad=0
    printf '\n── %s %s\n\n' "$1" "$(printf '─%.0s' $(seq 1 $pad))"
}

pass() { STEP=$((STEP+1)); PASSED=$((PASSED+1))
    printf '%s %2d. %-44s %s\n' "$(c 32 '[PASS]')" "$STEP" "$1" "${2:-}"; }

fail() { STEP=$((STEP+1)); FAILED=$((FAILED+1))
    printf '%s %2d. %s\n' "$(c 31 '[FAIL]')" "$STEP" "$1"
    [ -n "${2:-}" ] && printf '        → %s\n' "$(head -c 300 <<<"$2")"
    if [[ "$PHASE" == PHASE\ 7* ]]; then
        CLEANUP_FAILED=$((CLEANUP_FAILED+1)); CLEANUP_FAILURES+=("$1")
    else
        CORE_FAILED=$((CORE_FAILED+1)); FAILURES+=("$PHASE — $1")
    fi; }

skip() { STEP=$((STEP+1)); SKIPPED=$((SKIPPED+1))
    printf '%s %2d. %-44s %s\n' "$(c 90 '[SKIP]')" "$STEP" "$1" "${2:-}"; }

# ── CLI plumbing ────────────────────────────────────────────────────────

# fp <args...> — every call is JSON so ids can be read back, and pinned to the
# India region. --force because nothing here has a human to answer a prompt.
fp() {
    if [ -n "$DRY_RUN" ]; then
        printf '    $ flexprice --region %s --output json %s\n' "$REGION" "$*" >&2
        echo '{"id":"dry_run_id","status":"published"}'
        return 0
    fi
    "$BIN" --api-key "$API_KEY" --region "$REGION" --output json --force "$@"
}

# jget <json> <key> — reads a top-level field, empty when absent.
jget() {
    printf '%s' "$1" | python3 -c '
import json,sys
try: print(json.load(sys.stdin).get(sys.argv[1],"") or "")
except Exception: print("")
' "$2" 2>/dev/null
}

# run <cmd...> — captures stdout in RUN_OUT, stderr in RUN_ERR, status in RUN_RC.
#
# The two streams must stay separate. The CLI writes data to stdout and
# spinners, receipts and footers to stderr, so folding them together with 2>&1
# feeds commentary into the JSON parser and every id extraction returns empty.
# That is exactly what --dry-run surfaced here.
RUN_OUT=""; RUN_ERR=""; RUN_RC=0
run() {
    local errfile="$SANDBOX/stderr.$$"
    RUN_OUT=$(fp "$@" 2>"$errfile"); RUN_RC=$?
    RUN_ERR=$(cat "$errfile" 2>/dev/null); rm -f "$errfile"
}

# create <label> <var> <resource> <action> <args...>
# Runs a create, records the returned id into the named variable, and reports.
create() {
    local label="$1" var="$2"; shift 2
    run "$@"
    if [ $RUN_RC -ne 0 ]; then fail "$label" "${RUN_ERR:-$RUN_OUT}"; return 1; fi
    local id; id=$(jget "$RUN_OUT" id)
    if [ -z "$id" ]; then fail "$label" "no id in response: $(head -c 200 <<<"$RUN_OUT")"; return 1; fi
    printf -v "$var" '%s' "$id"
    pass "$label" "$id"
}

# verify <label> <cmd...> — the command must exit 0.
verify() {
    local label="$1"; shift
    run "$@"
    if [ $RUN_RC -eq 0 ]; then pass "$label"; else fail "$label" "${RUN_ERR:-$RUN_OUT}"; fi
}

# verify_field <label> <key> <want> <cmd...>
verify_field() {
    local label="$1" key="$2" want="$3"; shift 3
    run "$@"
    if [ $RUN_RC -ne 0 ]; then fail "$label" "${RUN_ERR:-$RUN_OUT}"; return; fi
    local got; got=$(jget "$RUN_OUT" "$key")
    if [ "$got" = "$want" ]; then pass "$label" "$key=$got"
    else fail "$label" "$key=$got, want $want"; fi
}

# teardown <label> <cmd...> — same as verify but never fails the run.
teardown() { verify "$@"; }

need() { # need <label> <id> — skip a step whose prerequisite never got created
    if [ -z "$2" ]; then skip "$1" "prerequisite missing"; return 1; fi
    return 0
}

# ── header & key ────────────────────────────────────────────────────────

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║          FLEXPRICE CLI — END-TO-END LIFECYCLE SUITE          ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo
echo "Region:   INDIA (locked)  $REGION_URL"
echo "Run tag:  $SLUG"

API_KEY="${FLEXPRICE_API_KEY:-}"
if [ -n "$DRY_RUN" ]; then
    API_KEY="dry-run"
    echo "Mode:     DRY RUN — prints commands, calls nothing"
elif [ -z "$API_KEY" ]; then
    if [ -t 0 ]; then
        printf '\nAPI key (input hidden): '
        read -rs API_KEY
        echo
    else
        echo
        echo "$(c 31 '✗ No API key.')  Set FLEXPRICE_API_KEY or run from a terminal."
        exit 2
    fi
fi

if [ -z "$API_KEY" ] && [ -z "$DRY_RUN" ]; then
    echo "$(c 31 '✗ No API key provided.')"; exit 2
fi

[ -z "$DRY_RUN" ] && echo "API key:  ${API_KEY:0:8}…${API_KEY: -2}"
echo "Started:  $(date -u +%Y-%m-%dT%H:%M:%SZ)"

# ── PHASE 0 ─────────────────────────────────────────────────────────────

phase "PHASE 0: PREFLIGHT"

if (cd "$CLI_DIR" && go build -o bin/flexprice . 2>/dev/null); then
    pass "build CLI"
else
    fail "build CLI" "go build failed"
    echo; echo "$(c 31 'Aborting: cannot build the binary.')"; exit 1
fi

if [ -n "$DRY_RUN" ]; then
    skip "authenticate against India" "dry run"
else
    out=$("$BIN" --api-key "$API_KEY" --region "$REGION" --output json customers list --limit 1 2>&1)
    if [ $? -eq 0 ]; then
        pass "authenticate against India"
    else
        fail "authenticate against India" "$out"
        echo
        echo "$(c 31 '✗ The key was rejected by the India region.')"
        echo "  Keys are region-specific — a US key will not work here."
        exit 1
    fi
fi

# ── PHASE 1 ─────────────────────────────────────────────────────────────

phase "PHASE 1: PRODUCT CATALOG"

create "create metered feature" FEATURE_ID \
    features create --name="E2E Tokens $STAMP" --type=metered \
                    --lookup_key="${SLUG}_tokens" --unit_singular=token --unit_plural=tokens

create "create plan" PLAN_ID \
    plans create --name="E2E Plan $STAMP" --lookup_key="${SLUG}_plan" \
                 --description="Created by cli e2e suite"

if need "create recurring price" "$PLAN_ID"; then
    create "create recurring price" PRICE_FIXED_ID \
        prices create --amount=499 --currency=INR --type=FIXED \
                      --billing_model=FLAT_FEE --billing_period=MONTHLY \
                      --billing_period_count=1 --invoice_cadence=ARREAR \
                      --price_unit_type=FIAT --entity_type=PLAN --entity_id="$PLAN_ID" \
                      --display_name="E2E monthly"
fi

if need "create usage price" "$PLAN_ID"; then
    create "create usage price" PRICE_USAGE_ID \
        prices create --amount=2 --currency=INR --type=USAGE \
                      --billing_model=FLAT_FEE --billing_period=MONTHLY \
                      --billing_period_count=1 --invoice_cadence=ARREAR \
                      --price_unit_type=FIAT --entity_type=PLAN --entity_id="$PLAN_ID" \
                      --display_name="E2E per-token"
fi

need "retrieve plan"    "$PLAN_ID"    && verify "retrieve plan"    plans retrieve "$PLAN_ID"
need "list plan prices" "$PLAN_ID"    && verify "list plan prices" prices list
need "retrieve price"   "$PRICE_FIXED_ID" && verify "retrieve price" prices retrieve "$PRICE_FIXED_ID"
verify "list features" features list --limit 5

# ── PHASE 2 ─────────────────────────────────────────────────────────────

phase "PHASE 2: ENTITLEMENTS & DISCOUNTS"

if need "create entitlement" "$FEATURE_ID$PLAN_ID"; then
    create "create entitlement" ENTITLEMENT_ID \
        entitlements create --feature_id="$FEATURE_ID" --feature_type=metered \
                            --plan_id="$PLAN_ID" --entity_type=plan --entity_id="$PLAN_ID" \
                            --is_enabled=true --grant_quota=100000
fi

need "retrieve entitlement" "$ENTITLEMENT_ID" && \
    verify "retrieve entitlement" entitlements retrieve "$ENTITLEMENT_ID"

create "create coupon" COUPON_ID \
    coupons create --name="E2E 10pct $STAMP" --type=percentage --cadence=once \
                   --percentage_off=10 --coupon_code="${SLUG}_c"

need "retrieve coupon" "$COUPON_ID" && verify "retrieve coupon" coupons retrieve "$COUPON_ID"

# ── PHASE 3 ─────────────────────────────────────────────────────────────

phase "PHASE 3: CUSTOMER & SUBSCRIPTION"

create "create customer" CUSTOMER_ID \
    customers create --external_id="$SLUG" --name="E2E Customer $STAMP" \
                     --email="$SLUG@example.invalid" --address_country=IN

need "retrieve customer" "$CUSTOMER_ID" && \
    verify "retrieve customer" customers retrieve "$CUSTOMER_ID"
need "lookup by external id" "$CUSTOMER_ID" && \
    verify "lookup by external id" customers by-external-id "$SLUG"
need "update customer" "$CUSTOMER_ID" && \
    verify "update customer" customers update "$CUSTOMER_ID" --name="E2E Customer $STAMP (updated)"

if need "create subscription" "$CUSTOMER_ID$PLAN_ID"; then
    create "create subscription" SUBSCRIPTION_ID \
        subscriptions create --customer_id="$CUSTOMER_ID" --plan_id="$PLAN_ID" \
                             --currency=INR --billing_period=MONTHLY \
                             --billing_period_count=1
fi

need "retrieve subscription" "$SUBSCRIPTION_ID" && \
    verify "retrieve subscription" subscriptions retrieve "$SUBSCRIPTION_ID"
need "subscription entitlements" "$SUBSCRIPTION_ID" && \
    verify "subscription entitlements" subscriptions entitlements "$SUBSCRIPTION_ID"
need "customer subscriptions" "$CUSTOMER_ID" && \
    verify "customer subscriptions" customers subscriptions "$SLUG"

# ── PHASE 4 ─────────────────────────────────────────────────────────────

phase "PHASE 4: WALLET"

if need "create wallet" "$CUSTOMER_ID"; then
    create "create wallet" WALLET_ID \
        wallets create --customer_id="$CUSTOMER_ID" --currency=INR \
                       --name="E2E wallet" --initial_credits_to_load=500
fi

need "retrieve wallet"  "$WALLET_ID" && verify "retrieve wallet"  wallets retrieve "$WALLET_ID"
need "wallet balance"   "$WALLET_ID" && verify "wallet balance"   wallets balance "$WALLET_ID"
need "top up wallet"    "$WALLET_ID" && verify "top up wallet"    wallets top-up "$WALLET_ID" --credits_to_add=250
need "wallet transactions" "$WALLET_ID" && verify "wallet transactions" wallets transactions "$WALLET_ID"

# ── PHASE 5 ─────────────────────────────────────────────────────────────

phase "PHASE 5: USAGE INGESTION"

if need "ingest event" "$CUSTOMER_ID"; then
    verify "ingest event" events ingest \
        --event_name="${SLUG}_tokens" --external_customer_id="$SLUG" \
        --properties='{"tokens":100}'
fi

need "list events" "$CUSTOMER_ID" && verify "list events" events list --limit 5
need "customer usage" "$CUSTOMER_ID" && verify "customer usage" customers usage "$SLUG"

# ── PHASE 6 ─────────────────────────────────────────────────────────────

phase "PHASE 6: INVOICES"

verify "list invoices" invoices list --limit 5

if need "find subscription invoice" "$SUBSCRIPTION_ID"; then
    run invoices list --limit 20
    INVOICE_ID=$(printf '%s' "$RUN_OUT" | python3 -c '
import json,sys
try:
    d=json.load(sys.stdin); items=d.get("items") or []
    sub=sys.argv[1]
    print(next((i["id"] for i in items if i.get("subscription_id")==sub), ""))
except Exception: print("")
' "$SUBSCRIPTION_ID" 2>/dev/null)
    if [ -n "$INVOICE_ID" ]; then
        pass "find subscription invoice" "$INVOICE_ID"
        verify "retrieve invoice" invoices retrieve "$INVOICE_ID"
    else
        # Not a failure: invoices are generated asynchronously and a brand-new
        # subscription may legitimately have none yet.
        skip "find subscription invoice" "none generated yet (async)"
        skip "retrieve invoice" "no invoice"
    fi
fi

# ── PHASE 7 ─────────────────────────────────────────────────────────────

phase "PHASE 7: CLEANUP (non-fatal)"

# Reverse creation order: dependents first, or the API rejects the parent.
need "cancel subscription" "$SUBSCRIPTION_ID" && \
    teardown "cancel subscription" subscriptions cancel "$SUBSCRIPTION_ID"
need "terminate wallet"    "$WALLET_ID"       && \
    teardown "terminate wallet"    wallets terminate "$WALLET_ID"
need "delete customer"     "$CUSTOMER_ID"     && \
    teardown "delete customer"     customers delete "$CUSTOMER_ID"
need "delete coupon"       "$COUPON_ID"       && \
    teardown "delete coupon"       coupons delete "$COUPON_ID"
need "delete entitlement"  "$ENTITLEMENT_ID"  && \
    teardown "delete entitlement"  entitlements delete "$ENTITLEMENT_ID"
need "delete usage price"  "$PRICE_USAGE_ID"  && \
    teardown "delete usage price"  prices delete "$PRICE_USAGE_ID"
need "delete fixed price"  "$PRICE_FIXED_ID"  && \
    teardown "delete fixed price"  prices delete "$PRICE_FIXED_ID"
need "delete plan"         "$PLAN_ID"         && \
    teardown "delete plan"         plans delete "$PLAN_ID"
need "delete feature"      "$FEATURE_ID"      && \
    teardown "delete feature"      features delete "$FEATURE_ID"

# ── report ──────────────────────────────────────────────────────────────

DURATION=$(( SECONDS - START ))
TOTAL=$(( PASSED + FAILED + SKIPPED ))

echo
printf '═%.0s' $(seq 1 62); echo
echo "SUMMARY"
printf '═%.0s' $(seq 1 62); echo
printf '  region   INDIA (%s)\n' "$REGION_URL"
printf '  run tag  %s\n' "$SLUG"
printf '  total    %d\n' "$TOTAL"
printf '  %s  %d\n' "$(c 32 'passed ')" "$PASSED"
printf '  %s  %d\n' "$(c 31 'failed ')" "$FAILED"
printf '  %s %d\n'  "$(c 90 'skipped')" "$SKIPPED"
printf '  duration %ds\n' "$DURATION"

if [ ${#FAILURES[@]} -gt 0 ]; then
    echo; echo "FAILED STEPS:"
    for f in "${FAILURES[@]}"; do echo "  ✗ $f"; done
fi

if [ ${#CLEANUP_FAILURES[@]} -gt 0 ]; then
    echo; echo "$(c 33 'CLEANUP FAILURES — these objects were LEAKED and need manual removal:')"
    for f in "${CLEANUP_FAILURES[@]}"; do echo "  ! $f"; done
    echo "  Find them by the run tag: $SLUG"
fi

echo
if [ "$CORE_FAILED" -gt 0 ]; then
    echo "$(c 31 "✗ FAILED — $CORE_FAILED core step(s)")"
    exit 1
fi
echo "$(c 32 '✓ PASSED')"
[ "$CLEANUP_FAILED" -gt 0 ] && echo "  (with $CLEANUP_FAILED leaked object(s) — see above)"
exit 0
