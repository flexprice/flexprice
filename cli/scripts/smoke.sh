#!/usr/bin/env bash
#
# Orchestrated smoke suite for the Flexprice CLI, modelled on
# integration-testing-suite/go but driving the binary rather than the SDK.
#
#   cli/scripts/smoke.sh                      # phases 1-4, no server needed
#   FLEXPRICE_API_KEY=sk_... cli/scripts/smoke.sh
#   FLEXPRICE_API_KEY=sk_... FLEXPRICE_SMOKE_WRITE=1 cli/scripts/smoke.sh
#
# Phases 1-4 assert the CLI's own contract — stream separation, exit codes,
# help structure, safety refusals — none of which needs an API, so the suite is
# useful on any machine. Phases 5-7 skip cleanly without a key.
#
# Exit status follows the Go suite: non-zero only when a CORE phase fails.
# Cleanup failures are reported but never fail the run, so a leaked test
# customer does not mask a green suite.

set -uo pipefail

CLI_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$CLI_DIR/bin/flexprice"

# Every invocation runs against a throwaway HOME with the encrypted-file
# keyring. login/whoami/init reach the real OS keychain otherwise, and an
# unattended run can raise a blocking macOS dialog with a destructive default.
# Go caches are pinned to the real ones first: overriding HOME otherwise makes
# `go build` recreate a module cache inside the sandbox with read-only dirs,
# which then fails to clean up.
export GOMODCACHE="${GOMODCACHE:-$(go env GOMODCACHE)}"
export GOCACHE="${GOCACHE:-$(go env GOCACHE)}"

SANDBOX="$(mktemp -d)"
export HOME="$SANDBOX"
export FLEXPRICE_KEY_BACKEND=file
# Non-interactive by construction: nothing here should ever wait on a prompt.
export FLEXPRICE_SMOKE_START=$SECONDS

WRITE_TESTS="${FLEXPRICE_SMOKE_WRITE:-}"
API_KEY="${FLEXPRICE_API_KEY:-}"
API_REGION="${FLEXPRICE_SMOKE_REGION:-us}"
BASE_URL="${FLEXPRICE_SMOKE_BASE_URL:-}"

PASSED=0
FAILED=0
SKIPPED=0
STEP=0
PHASE=""
CORE_FAILED=0
CLEANUP_FAILED=0
declare -a FAILURES=()
declare -a CLEANUP_FAILURES=()

# Entity ids created by phase 6, cleaned up in phase 7.
CREATED_CUSTOMER=""

cleanup() { rm -rf "$SANDBOX"; }
trap cleanup EXIT

# ── output helpers ──────────────────────────────────────────────────────

is_tty() { [ -t 1 ]; }
c() { if is_tty && [ -z "${NO_COLOR:-}" ]; then printf '\033[%sm%s\033[0m' "$1" "$2"; else printf '%s' "$2"; fi; }

phase() {
    PHASE="$1"
    local pad=$(( 58 - ${#1} ))
    [ $pad -lt 0 ] && pad=0
    printf '\n── %s %s\n\n' "$1" "$(printf '─%.0s' $(seq 1 $pad))"
}

pass() {
    STEP=$((STEP+1)); PASSED=$((PASSED+1))
    printf '%s %2d. %s\n' "$(c '32' '[PASS]')" "$STEP" "$1"
}

fail() {
    STEP=$((STEP+1)); FAILED=$((FAILED+1))
    printf '%s %2d. %s\n' "$(c '31' '[FAIL]')" "$STEP" "$1"
    [ -n "${2:-}" ] && printf '        → %s\n' "$2"
    if [[ "$PHASE" == PHASE\ 7* ]]; then
        CLEANUP_FAILED=$((CLEANUP_FAILED+1)); CLEANUP_FAILURES+=("$1")
    else
        CORE_FAILED=$((CORE_FAILED+1)); FAILURES+=("$PHASE — $1")
    fi
}

skip() {
    STEP=$((STEP+1)); SKIPPED=$((SKIPPED+1))
    printf '%s %2d. %s\n' "$(c '90' '[SKIP]')" "$STEP" "$1"
    [ -n "${2:-}" ] && printf '        reason: %s\n' "$2"
}

# ── assertion helpers ───────────────────────────────────────────────────

# ok <name> <cmd...> — the command must exit 0.
ok() {
    local name="$1"; shift
    local out; out=$("$@" 2>&1)
    if [ $? -eq 0 ]; then pass "$name"; else fail "$name" "exit non-zero: $(head -1 <<<"$out")"; fi
}

# exits_with <name> <want> <cmd...> — assert an exact exit code. Exit codes are
# a public contract that scripts depend on, so they are asserted by value.
exits_with() {
    local name="$1" want="$2"; shift 2
    local out; out=$("$@" 2>&1); local got=$?
    if [ "$got" -eq "$want" ]; then pass "$name ($want)"
    else fail "$name" "exit $got, want $want: $(head -1 <<<"$out")"; fi
}

# stdout_has / stderr_has / stdout_lacks <name> <needle> <cmd...>
stdout_has() {
    local name="$1" needle="$2"; shift 2
    local out; out=$("$@" 2>/dev/null)
    if grep -qF -- "$needle" <<<"$out"; then pass "$name"
    else fail "$name" "stdout missing '$needle'"; fi
}

stderr_has() {
    local name="$1" needle="$2"; shift 2
    local err; err=$("$@" 2>&1 1>/dev/null)
    if grep -qF -- "$needle" <<<"$err"; then pass "$name"
    else fail "$name" "stderr missing '$needle'"; fi
}

stdout_lacks() {
    local name="$1" needle="$2"; shift 2
    local out; out=$("$@" 2>/dev/null)
    if grep -qF -- "$needle" <<<"$out"; then fail "$name" "stdout unexpectedly contains '$needle'"
    else pass "$name"; fi
}

# stdout_empty <name> <cmd...> — nothing on stdout at all.
stdout_empty() {
    local name="$1"; shift
    local out; out=$("$@" 2>/dev/null)
    if [ -z "$out" ]; then pass "$name"
    else fail "$name" "stdout not empty: $(head -c 80 <<<"$out")"; fi
}

# no_ansi <name> <cmd...> — neither stream may carry escape sequences.
# Asserting ABSENCE is the point: this is what stops CI logs filling with
# escape codes, and it is the assertion the unit tests were initially too weak
# to make.
no_ansi() {
    local name="$1"; shift
    local both; both=$("$@" 2>&1)
    if grep -q $'\033' <<<"$both"; then fail "$name" "escape sequence present"
    else pass "$name"; fi
}

# valid_json <name> <cmd...>
valid_json() {
    local name="$1"; shift
    local out; out=$("$@" 2>/dev/null)
    if [ -z "$out" ]; then fail "$name" "no stdout to parse"; return; fi
    if printf '%s' "$out" | python3 -c 'import json,sys; json.load(sys.stdin)' 2>/dev/null; then
        pass "$name"
    else
        fail "$name" "stdout is not valid JSON: $(head -c 80 <<<"$out")"
    fi
}

fp() { "$BIN" "$@"; }

# ── header ──────────────────────────────────────────────────────────────

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║                 FLEXPRICE CLI SMOKE SUITE                    ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo
echo "Binary:   $BIN"
echo "Sandbox:  $SANDBOX (encrypted-file keyring; real keychain untouched)"
if [ -n "$API_KEY" ]; then
    echo "API key:  ${API_KEY:0:8}…${API_KEY: -2}   region: $API_REGION"
else
    echo "API key:  (none — live phases will skip)"
fi
echo "Started:  $(date -u +%Y-%m-%dT%H:%M:%SZ)"

# ── PHASE 1 ─────────────────────────────────────────────────────────────

phase "PHASE 1: BUILD & DISCOVERY"

if (cd "$CLI_DIR" && go build -o bin/flexprice . 2>/dev/null); then
    pass "build"
else
    fail "build" "go build failed — nothing else can run"
    echo; echo "$(c '31' 'Aborting: the binary could not be built.')"; exit 1
fi

ok           "version exits 0"              fp version
stdout_has   "version names the binary"     "flexprice"        fp version
stdout_has   "version reports spec size"    "embedded OpenAPI" fp version
ok           "help exits 0"                 fp --help
stdout_has   "resources lists customers"    "customers"        fp resources

for group in "Setup" "Core billing" "Usage & metering" "Credits & discounts" \
             "Catalog & pricing" "Platform" "Automation" "Advanced"; do
    stdout_has "help renders group: $group" "$group" fp --help
done

# The old flat help said "Operations on <x>" for all 34 resources. If that
# string is back, the group table lost its descriptions.
stdout_lacks "no placeholder descriptions"  "Operations on" fp --help
# Anything here means a command was added without a group.
stdout_lacks "nothing in Additional Commands" "Additional Commands" fp --help

ok           "completion bash"              fp completion bash
ok           "customers --help"             fp customers --help
stdout_has   "subcommand help lists actions" "create"    fp customers --help

# ── PHASE 2 ─────────────────────────────────────────────────────────────

phase "PHASE 2: OUTPUT CONTRACT"

# stdout is a machine contract; stderr is the conversation. This is the
# guarantee that `--output json > file.json` stays parseable.
stdout_empty "unknown command: nothing on stdout"        fp definitely-not-a-command
stderr_has   "unknown command: names the mistake" "unknown command" fp definitely-not-a-command
stdout_empty "auth error: nothing on stdout"             fp customers list
stderr_has   "auth error: on stderr"    "not authenticated" fp customers list
stderr_has   "auth error: names the fix" "flexprice init"   fp customers list
stderr_has   "errors carry a failure icon" "✗"              fp customers list

# Colour must be suppressed on request and by convention, on both streams.
no_ansi "no colour: --no-color"            fp --no-color customers list
no_ansi "no colour: NO_COLOR env"          env NO_COLOR=1 "$BIN" customers list
no_ansi "no colour: TERM=dumb"             env TERM=dumb "$BIN" customers list
no_ansi "no colour: piped (not a TTY)"     fp customers list

# --quiet suppresses commentary but must never hide a failure.
stderr_has "quiet still reports failures" "not authenticated" fp --quiet customers list

# ── PHASE 3 ─────────────────────────────────────────────────────────────

phase "PHASE 3: SAFETY RAILS"

# Exit codes are a public contract; scripts branch on them.
exits_with "usage error"        2 fp --output not-a-format customers list
exits_with "auth failure"       3 fp customers list

# A destructive command with no terminal must REFUSE, not proceed. This is the
# behaviour change that replaced the old bypass, and it is the single most
# important assertion in this suite: the old code deleted with nothing asked.
out=$(fp customers delete cust_smoke_test 2>&1); rc=$?
if [ $rc -ne 0 ] && grep -qF -- "--force" <<<"$out"; then
    pass "destructive refuses without --force"
else
    fail "destructive refuses without --force" "exit $rc: $(head -1 <<<"$out")"
fi

stderr_has "refusal names the target" "cust_smoke_test" fp customers delete cust_smoke_test

# --no-input must refuse rather than hang waiting for an answer nobody can give.
out=$(fp login --no-input 2>&1); rc=$?
if [ $rc -ne 0 ] && grep -qF -- "--region" <<<"$out"; then
    pass "--no-input refuses and names --region"
else
    fail "--no-input refuses and names --region" "exit $rc: $(head -1 <<<"$out")"
fi

# --force must skip the prompt. Pointed at a closed port so it fails on the
# network rather than the confirmation: reaching a transport error proves the
# gate was passed.
out=$(fp customers delete cust_smoke_test --force --api-key sk_smoke \
        --base-url http://127.0.0.1:1/v1 2>&1)
if grep -qE 'connection refused|no such host|dial tcp' <<<"$out"; then
    pass "--force skips the prompt and proceeds"
else
    fail "--force skips the prompt and proceeds" "$(head -1 <<<"$out")"
fi

# ── PHASE 4 ─────────────────────────────────────────────────────────────

phase "PHASE 4: PROFILES & CONFIG"

exits_with "whoami with no profile" 1 fp whoami
stderr_has  "whoami points at init" "flexprice init" fp whoami

mkdir -p "$SANDBOX/.flexprice"
cat > "$SANDBOX/.flexprice/config.toml" <<'EOF'
default_profile = "smoke"
[profiles.smoke]
region = "us"
base_url = "https://us.api.flexprice.io/v1"
label = "smoke-test"
key_ref = "keyring:smoke"
EOF

stdout_has "whoami reports the profile" "smoke"       fp whoami
stdout_has "whoami reports the region"  "us"          fp whoami
stdout_has "whoami reports the backend" "file"        fp whoami
stdout_has "whoami flags a missing key" "not stored"  fp whoami
# whoami is a result people parse, so it must stay on stdout.
stdout_has "whoami writes to stdout"    "Profile:"    fp whoami
ok         "config list"                              fp config list

# ── PHASE 5 ─────────────────────────────────────────────────────────────

phase "PHASE 5: LIVE API (READ-ONLY)"

if [ -z "$API_KEY" ]; then
    for t in "authenticated read" "table output" "json output" "yaml output" \
             "pagination --limit" "status footer"; do
        skip "$t" "FLEXPRICE_API_KEY not set"
    done
else
    LIVE=(--api-key "$API_KEY")
    [ -n "$BASE_URL" ] && LIVE+=(--base-url "$BASE_URL") || LIVE+=(--region "$API_REGION")

    ok         "authenticated read"                fp "${LIVE[@]}" customers list --limit 1
    stdout_has "table output has a header" "ID"    fp "${LIVE[@]}" customers list --limit 1
    valid_json "json output parses"                fp "${LIVE[@]}" --output json customers list --limit 1
    ok         "yaml output"                       fp "${LIVE[@]}" --output yaml customers list --limit 1
    ok         "pagination --limit"                fp "${LIVE[@]}" customers list --limit 2
    stderr_has "status footer names the profile" "profile:" fp "${LIVE[@]}" customers list --limit 1

    # The contract that matters most for scripting: with stdout redirected,
    # the JSON must be parseable on its own, with no commentary mixed in.
    tmp_json="$SANDBOX/out.json"
    fp "${LIVE[@]}" --output json customers list --limit 1 > "$tmp_json" 2>/dev/null
    if python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$tmp_json" 2>/dev/null; then
        pass "redirected stdout is pure JSON"
    else
        fail "redirected stdout is pure JSON" "file did not parse"
    fi
fi

# ── PHASE 6 ─────────────────────────────────────────────────────────────

phase "PHASE 6: LIVE LIFECYCLE (WRITE)"

if [ -z "$API_KEY" ]; then
    for t in "create customer" "retrieve customer" "receipt on create"; do
        skip "$t" "FLEXPRICE_API_KEY not set"
    done
elif [ -z "$WRITE_TESTS" ]; then
    for t in "create customer" "retrieve customer" "receipt on create"; do
        skip "$t" "set FLEXPRICE_SMOKE_WRITE=1 to allow writes"
    done
else
    STAMP="$(date +%s)"
    EXT_ID="cli-smoke-$STAMP"

    created=$(fp "${LIVE[@]}" --output json customers create \
                --external_id="$EXT_ID" --email="$EXT_ID@example.invalid" 2>/dev/null)
    CREATED_CUSTOMER=$(printf '%s' "$created" \
        | python3 -c 'import json,sys; print(json.load(sys.stdin).get("id",""))' 2>/dev/null)

    if [ -n "$CREATED_CUSTOMER" ]; then
        pass "create customer ($CREATED_CUSTOMER)"
    else
        fail "create customer" "no id in response"
    fi

    if [ -n "$CREATED_CUSTOMER" ]; then
        ok "retrieve customer" fp "${LIVE[@]}" customers retrieve "$CREATED_CUSTOMER"
        # A mutation must confirm itself on stderr, never on stdout.
        stderr_has "receipt on create" "Created" \
            fp "${LIVE[@]}" customers create \
               --external_id="$EXT_ID-b" --email="$EXT_ID-b@example.invalid"
    else
        skip "retrieve customer" "create failed"
        skip "receipt on create" "create failed"
    fi
fi

# ── PHASE 7 ─────────────────────────────────────────────────────────────

phase "PHASE 7: CLEANUP (non-fatal)"

if [ -z "$CREATED_CUSTOMER" ]; then
    skip "delete test customer" "nothing was created"
else
    ok "delete test customer" fp "${LIVE[@]}" customers delete "$CREATED_CUSTOMER" --force
fi

# ── report ──────────────────────────────────────────────────────────────

DURATION=$(( SECONDS - FLEXPRICE_SMOKE_START ))
TOTAL=$(( PASSED + FAILED + SKIPPED ))

echo
printf '═%.0s' $(seq 1 62); echo
echo "SUMMARY"
printf '═%.0s' $(seq 1 62); echo
printf '  total    %d\n' "$TOTAL"
printf '  %s   %d\n' "$(c '32' 'passed ')" "$PASSED"
printf '  %s   %d\n' "$(c '31' 'failed ')" "$FAILED"
printf '  %s  %d\n'  "$(c '90' 'skipped')" "$SKIPPED"
printf '  duration %ds\n' "$DURATION"

if [ ${#FAILURES[@]} -gt 0 ]; then
    echo
    echo "FAILED STEPS:"
    for f in "${FAILURES[@]}"; do echo "  ✗ $f"; done
fi

if [ ${#CLEANUP_FAILURES[@]} -gt 0 ]; then
    echo
    echo "CLEANUP FAILURES (non-fatal — may have leaked test data):"
    for f in "${CLEANUP_FAILURES[@]}"; do echo "  ! $f"; done
fi

if [ -z "$API_KEY" ]; then
    echo
    echo "Live phases skipped. To run them:"
    echo "    FLEXPRICE_API_KEY=sk_... cli/scripts/smoke.sh"
    echo "    FLEXPRICE_API_KEY=sk_... FLEXPRICE_SMOKE_WRITE=1 cli/scripts/smoke.sh   # also creates/deletes a customer"
fi

echo
if [ "$CORE_FAILED" -gt 0 ]; then
    echo "$(c '31' "✗ FAILED — $CORE_FAILED core step(s)")"
    exit 1
fi
echo "$(c '32' '✓ PASSED')"
[ "$CLEANUP_FAILED" -gt 0 ] && echo "  (with $CLEANUP_FAILED non-fatal cleanup failure(s))"
exit 0
