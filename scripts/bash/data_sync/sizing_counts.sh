#!/usr/bin/env bash
#
# sizing_counts.sh — Count DISTINCT events per customer per day across all 4 tables
#
# CRITICAL: All tables use ReplacingMergeTree which may have unmerged duplicates.
# We use count(DISTINCT id) everywhere instead of count() for accurate numbers.
#
# Queries are aligned with each table's sort key for maximum efficiency:
#   - VAPI billing_entries:   ORDER BY (org_id, created_at, id)
#   - FP raw_events:          ORDER BY (tenant_id, environment_id, external_customer_id, timestamp, id)
#   - FP events:              ORDER BY (tenant_id, environment_id, timestamp, id)  [bloom on ext_cust_id]
#   - FP feature_usage:       ORDER BY (tenant_id, environment_id, customer_id, timestamp, ...)
#
# All queries use PREWHERE on leading sort key columns and strict memory caps.
# Results are written per-customer so the script can resume after interruption.
#
# Usage:
#   cd scripts/bash/data_sync && source .env.backfill && ./sizing_counts.sh
#
set -euo pipefail

###############################################################################
# Configuration
###############################################################################
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CUSTOMER_FILE="${CUSTOMER_FILE:-${SCRIPT_DIR}/../enterprise-customer-ids.json}"
OUTPUT_DIR="${OUTPUT_DIR:-${SCRIPT_DIR}/sizing_output}"
COMBINED_CSV="${OUTPUT_DIR}/combined_counts.csv"

# Date range — February 2026
START_DATE="${START_DATE:-2026-02-01}"
END_DATE="${END_DATE:-2026-03-01}"

# FlexPrice tenant/env
TENANT_ID="${TENANT_ID:?TENANT_ID required}"
ENVIRONMENT_ID="${ENVIRONMENT_ID:?ENVIRONMENT_ID required}"

# FlexPrice ClickHouse
CH_HOST="${CH_HOST:?CH_HOST required}"
CH_PORT="${CH_PORT:-9000}"
CH_USER="${CH_USER:-default}"
CH_PASSWORD="${CH_PASSWORD:-}"
CH_DB="${CH_DB:-flexprice}"

# VAPI ClickHouse (HTTPS)
VAPI_CH_HOST="${VAPI_CH_HOST:?VAPI_CH_HOST required}"
VAPI_CH_PORT="${VAPI_CH_PORT:-8443}"
VAPI_CH_USER="${VAPI_CH_USER:?VAPI_CH_USER required}"
VAPI_CH_PASSWORD="${VAPI_CH_PASSWORD:?VAPI_CH_PASSWORD required}"
VAPI_CH_DB="${VAPI_CH_DB:-default}"

# Memory limits per query (conservative for production)
CH_MAX_MEMORY="${CH_MAX_MEMORY:-4000000000}"     # 4 GB per query
VAPI_MAX_MEMORY="${VAPI_MAX_MEMORY:-4000000000}"  # 4 GB per query

###############################################################################
# Helpers
###############################################################################
log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

# FlexPrice ClickHouse query (native protocol)
fp_query() {
  local query="$1"
  clickhouse client \
    --host "$CH_HOST" --port "$CH_PORT" \
    --user "$CH_USER" --password "$CH_PASSWORD" \
    --database "$CH_DB" \
    --format TSV \
    --query "
      SET max_memory_usage = ${CH_MAX_MEMORY};
      ${query}
    "
}

# VAPI ClickHouse query (HTTPS — ClickHouse Cloud, POST method)
vapi_query() {
  local query="$1"
  curl -sS \
    --fail-with-body \
    -X POST \
    "https://${VAPI_CH_HOST}:${VAPI_CH_PORT}/?database=${VAPI_CH_DB}&default_format=TSV&max_memory_usage=${VAPI_MAX_MEMORY}" \
    --user "${VAPI_CH_USER}:${VAPI_CH_PASSWORD}" \
    --data "${query}" \
    --max-time 300
}

# Check if a customer's results already exist for a specific table
is_done() {
  local cust_id="$1" table="$2"
  local marker="${OUTPUT_DIR}/progress/${cust_id}_${table}.done"
  [[ -f "$marker" ]]
}

mark_done() {
  local cust_id="$1" table="$2"
  local marker="${OUTPUT_DIR}/progress/${cust_id}_${table}.done"
  touch "$marker"
}

###############################################################################
# Validation
###############################################################################
for cmd in clickhouse curl jq; do
  if ! command -v "$cmd" &>/dev/null; then
    log "ERROR: '$cmd' is required"
    exit 1
  fi
done

if [[ ! -f "$CUSTOMER_FILE" ]]; then
  log "ERROR: Customer file not found: $CUSTOMER_FILE"
  exit 1
fi

###############################################################################
# Setup
###############################################################################
mkdir -p "${OUTPUT_DIR}/per_customer" "${OUTPUT_DIR}/progress" "${OUTPUT_DIR}/per_table"

# Load customer IDs
CUSTOMERS=()
while IFS= read -r cid; do
  CUSTOMERS+=("$cid")
done < <(jq -r '.[]' "$CUSTOMER_FILE")

total_customers=${#CUSTOMERS[@]}

log "================================================================"
log " Data Sync Sizing — Daily DISTINCT Counts"
log "================================================================"
log " Customers:      ${total_customers}"
log " Date range:     ${START_DATE} -> ${END_DATE}"
log " FP ClickHouse:  ${CH_HOST}:${CH_PORT}/${CH_DB}"
log " VAPI CH:        ${VAPI_CH_HOST}:${VAPI_CH_PORT}/${VAPI_CH_DB}"
log " Output:         ${OUTPUT_DIR}"
log " Memory cap:     ${CH_MAX_MEMORY} bytes/query"
log " NOTE:           Using count(DISTINCT id) — ReplacingMergeTree has dupes"
log "================================================================"

###############################################################################
# Main loop — one customer at a time
###############################################################################
SHUTDOWN=false
trap 'log "SIGINT — finishing current customer then exiting."; SHUTDOWN=true' INT TERM

cust_done=0
start_epoch=$(date +%s)

for cust_id in "${CUSTOMERS[@]}"; do
  if [[ "$SHUTDOWN" == "true" ]]; then
    log "Shutdown requested. Exiting."
    break
  fi

  cust_done=$((cust_done + 1))
  cust_file="${OUTPUT_DIR}/per_customer/${cust_id}.csv"

  # Check if fully done (all 4 tables)
  if is_done "$cust_id" "vapi" && is_done "$cust_id" "raw_events" && \
     is_done "$cust_id" "events" && is_done "$cust_id" "feature_usage"; then
    log "[${cust_done}/${total_customers}] ${cust_id} — already complete, skipping"
    continue
  fi

  log "[${cust_done}/${total_customers}] ${cust_id} — querying..."

  # Per-table result files (TSV: day\tcount)
  vapi_file="${OUTPUT_DIR}/per_table/${cust_id}_vapi.tsv"
  raw_file="${OUTPUT_DIR}/per_table/${cust_id}_raw_events.tsv"
  events_file="${OUTPUT_DIR}/per_table/${cust_id}_events.tsv"
  fu_file="${OUTPUT_DIR}/per_table/${cust_id}_feature_usage.tsv"

  # --- VAPI billing_entries ---
  # ORDER BY (org_id, created_at, id) — org_id leading key, optimal
  if ! is_done "$cust_id" "vapi"; then
    log "  -> VAPI billing_entries..."
    if vapi_query "
      SELECT
        toDate(created_at) AS day,
        count(DISTINCT id) AS cnt
      FROM ${VAPI_CH_DB}.billing_entries
      WHERE org_id = '${cust_id}'
        AND created_at >= toDateTime64('${START_DATE} 00:00:00', 3)
        AND created_at <  toDateTime64('${END_DATE} 00:00:00', 3)
      GROUP BY day
      ORDER BY day
    " > "$vapi_file" 2>/dev/null; then
      mark_done "$cust_id" "vapi"
      log "  -> VAPI: $(wc -l < "$vapi_file" | tr -d ' ') days"
    else
      log "  !! VAPI query failed for ${cust_id}"
      > "$vapi_file"  # empty file
    fi
  fi

  # --- FlexPrice raw_events ---
  # ORDER BY (tenant_id, environment_id, external_customer_id, timestamp, id)
  # PREWHERE on first 3 sort key columns — optimal
  if ! is_done "$cust_id" "raw_events"; then
    log "  -> FP raw_events..."
    if fp_query "
      SELECT
        toDate(timestamp) AS day,
        count(DISTINCT id) AS cnt
      FROM ${CH_DB}.raw_events
      PREWHERE tenant_id = '${TENANT_ID}'
        AND environment_id = '${ENVIRONMENT_ID}'
        AND external_customer_id = '${cust_id}'
      WHERE timestamp >= toDateTime64('${START_DATE} 00:00:00', 3)
        AND timestamp <  toDateTime64('${END_DATE} 00:00:00', 3)
      GROUP BY day
      ORDER BY day
    " > "$raw_file" 2>/dev/null; then
      mark_done "$cust_id" "raw_events"
      log "  -> raw_events: $(wc -l < "$raw_file" | tr -d ' ') days"
    else
      log "  !! raw_events query failed for ${cust_id}"
      > "$raw_file"
    fi
  fi

  # --- FlexPrice events ---
  # ORDER BY (tenant_id, environment_id, timestamp, id)
  # external_customer_id NOT in sort key — bloom filter index via WHERE
  if ! is_done "$cust_id" "events"; then
    log "  -> FP events..."
    if fp_query "
      SELECT
        toDate(timestamp) AS day,
        count(DISTINCT id) AS cnt
      FROM ${CH_DB}.events
      PREWHERE tenant_id = '${TENANT_ID}'
        AND environment_id = '${ENVIRONMENT_ID}'
      WHERE timestamp >= toDateTime64('${START_DATE} 00:00:00', 3)
        AND timestamp <  toDateTime64('${END_DATE} 00:00:00', 3)
        AND external_customer_id = '${cust_id}'
      GROUP BY day
      ORDER BY day
    " > "$events_file" 2>/dev/null; then
      mark_done "$cust_id" "events"
      log "  -> events: $(wc -l < "$events_file" | tr -d ' ') days"
    else
      log "  !! events query failed for ${cust_id}"
      > "$events_file"
    fi
  fi

  # --- FlexPrice feature_usage ---
  # ORDER BY (tenant_id, environment_id, customer_id, timestamp, ...)
  # external_customer_id has bloom filter — in WHERE clause
  if ! is_done "$cust_id" "feature_usage"; then
    log "  -> FP feature_usage..."
    if fp_query "
      SELECT
        toDate(timestamp) AS day,
        count(DISTINCT id) AS cnt
      FROM ${CH_DB}.feature_usage
      PREWHERE tenant_id = '${TENANT_ID}'
        AND environment_id = '${ENVIRONMENT_ID}'
      WHERE timestamp >= toDateTime64('${START_DATE} 00:00:00', 3)
        AND timestamp <  toDateTime64('${END_DATE} 00:00:00', 3)
        AND external_customer_id = '${cust_id}'
      GROUP BY day
      ORDER BY day
    " > "$fu_file" 2>/dev/null; then
      mark_done "$cust_id" "feature_usage"
      log "  -> feature_usage: $(wc -l < "$fu_file" | tr -d ' ') days"
    else
      log "  !! feature_usage query failed for ${cust_id}"
      > "$fu_file"
    fi
  fi

  # --- Merge per-table TSVs into per-customer CSV (bash 3 / macOS compatible) ---
  # Tag each file's rows with source, then use awk to pivot
  echo "external_customer_id,date,vapi_count,raw_events_count,events_count,feature_usage_count" > "$cust_file"

  # Build tagged input; if all files are empty, skip merge
  merge_input=""
  [[ -s "$vapi_file" ]]   && merge_input+=$(awk -F'\t' '{print $1"\tvapi\t"$2}' "$vapi_file")$'\n'
  [[ -s "$raw_file" ]]    && merge_input+=$(awk -F'\t' '{print $1"\traw\t"$2}' "$raw_file")$'\n'
  [[ -s "$events_file" ]] && merge_input+=$(awk -F'\t' '{print $1"\tev\t"$2}' "$events_file")$'\n'
  [[ -s "$fu_file" ]]     && merge_input+=$(awk -F'\t' '{print $1"\tfu\t"$2}' "$fu_file")$'\n'

  if [[ -z "$merge_input" ]]; then
    log "  TOTALS: vapi=0 raw=0 events=0 fu=0"
    log "  GAPS:   P1(vapi-raw)=0 P2+3(raw-ev)=0 P4(ev-fu)=0"
    # ETA
    now_epoch=$(date +%s)
    elapsed=$((now_epoch - start_epoch))
    if (( elapsed > 0 && cust_done > 0 )); then
      remaining=$((total_customers - cust_done))
      eta_sec=$((remaining * elapsed / cust_done))
      log "  Progress: ${cust_done}/${total_customers} | Elapsed: $((elapsed/60))m | ETA: ~$((eta_sec / 60))m"
    fi
    continue
  fi

  echo "$merge_input" | awk -v cust="$cust_id" '
    BEGIN { FS="\t" }
    {
      dates[$1] = 1
      if ($2 == "vapi") vapi[$1] = $3
      else if ($2 == "raw") raw[$1] = $3
      else if ($2 == "ev") ev[$1] = $3
      else if ($2 == "fu") fu[$1] = $3
    }
    END {
      # Collect and sort dates portably
      n = 0
      for (d in dates) sorted[n++] = d
      # Simple insertion sort (max ~31 days, trivial)
      for (i = 1; i < n; i++) {
        key = sorted[i]
        j = i - 1
        while (j >= 0 && sorted[j] > key) {
          sorted[j+1] = sorted[j]
          j--
        }
        sorted[j+1] = key
      }
      for (i = 0; i < n; i++) {
        d = sorted[i]
        if (d == "") continue
        v = (d in vapi) ? vapi[d] : 0
        r = (d in raw) ? raw[d] : 0
        e = (d in ev) ? ev[d] : 0
        f = (d in fu) ? fu[d] : 0
        printf "%s,%s,%d,%d,%d,%d\n", cust, d, v, r, e, f
      }
    }
  ' >> "$cust_file"

  # Compute totals for log
  totals=$(awk -F',' 'NR>1 { v+=$3; r+=$4; e+=$5; f+=$6 } END { printf "%d %d %d %d", v, r, e, f }' "$cust_file")
  total_vapi=$(echo "$totals" | awk '{print $1}')
  total_raw=$(echo "$totals" | awk '{print $2}')
  total_ev=$(echo "$totals" | awk '{print $3}')
  total_fu=$(echo "$totals" | awk '{print $4}')

  log "  TOTALS: vapi=${total_vapi} raw=${total_raw} events=${total_ev} fu=${total_fu}"

  p1_gap=$((total_vapi - total_raw))
  p23_gap=$((total_raw - total_ev))
  p4_gap=$((total_ev - total_fu))
  log "  GAPS:   P1(vapi-raw)=${p1_gap} P2+3(raw-ev)=${p23_gap} P4(ev-fu)=${p4_gap}"

  # ETA
  now_epoch=$(date +%s)
  elapsed=$((now_epoch - start_epoch))
  if (( elapsed > 0 && cust_done > 0 )); then
    remaining=$((total_customers - cust_done))
    eta_sec=$((remaining * elapsed / cust_done))
    log "  Progress: ${cust_done}/${total_customers} | Elapsed: $((elapsed/60))m | ETA: ~$((eta_sec / 60))m"
  fi
done

###############################################################################
# Combine all per-customer CSVs into one
###############################################################################
log ""
log "Combining results..."

echo "external_customer_id,date,vapi_count,raw_events_count,events_count,feature_usage_count" > "$COMBINED_CSV"

for f in "${OUTPUT_DIR}"/per_customer/*.csv; do
  [[ -f "$f" ]] || continue
  tail -n +2 "$f" >> "$COMBINED_CSV"
done

total_rows=$(( $(wc -l < "$COMBINED_CSV") - 1 ))
log "Combined CSV: ${COMBINED_CSV} (${total_rows} rows)"

###############################################################################
# Summary report
###############################################################################
SUMMARY_FILE="${OUTPUT_DIR}/summary.txt"
{
  echo "================================================================"
  echo " DATA SYNC SIZING SUMMARY"
  echo " Date range: ${START_DATE} -> ${END_DATE}"
  echo " Generated:  $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  echo "================================================================"
  echo ""

  if [[ -f "$COMBINED_CSV" ]]; then
    awk -F',' 'NR>1 {
      vapi += $3; raw += $4; ev += $5; fu += $6
      if ($3 > 0 || $4 > 0) customers[$1] = 1
    }
    END {
      printf "TOTALS (across %d customers with data):\n", length(customers)
      printf "  VAPI billing_entries:  %d\n", vapi
      printf "  FP raw_events:         %d\n", raw
      printf "  FP events:             %d\n", ev
      printf "  FP feature_usage:      %d\n", fu
      printf "\nPIPELINE GAPS:\n"
      printf "  P1 (vapi -> raw_events):     %d missing (%.2f%%)\n", vapi-raw, (vapi>0 ? (vapi-raw)*100.0/vapi : 0)
      printf "  P2+3 (raw_events -> events): %d missing (%.2f%%)\n", raw-ev, (raw>0 ? (raw-ev)*100.0/raw : 0)
      printf "  P4 (events -> feature_usage):%d missing (%.2f%%)\n", ev-fu, (ev>0 ? (ev-fu)*100.0/ev : 0)
    }' "$COMBINED_CSV"

    echo ""
    echo "PER-CUSTOMER BREAKDOWN:"
    echo "external_customer_id,vapi_total,raw_total,events_total,fu_total,p1_gap,p23_gap,p4_gap"
    awk -F',' 'NR>1 {
      v[$1] += $3; r[$1] += $4; e[$1] += $5; f[$1] += $6
    }
    END {
      for (c in v) {
        printf "%s,%d,%d,%d,%d,%d,%d,%d\n", c, v[c], r[c], e[c], f[c], v[c]-r[c], r[c]-e[c], e[c]-f[c]
      }
    }' "$COMBINED_CSV" | sort -t',' -k2 -nr
  fi
} > "$SUMMARY_FILE"

cat "$SUMMARY_FILE"

log ""
log "================================================================"
log " Files:"
log "   Per-customer: ${OUTPUT_DIR}/per_customer/"
log "   Per-table:    ${OUTPUT_DIR}/per_table/"
log "   Combined:     ${COMBINED_CSV}"
log "   Summary:      ${SUMMARY_FILE}"
log "================================================================"
