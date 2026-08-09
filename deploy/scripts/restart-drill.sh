#!/usr/bin/env bash
#
# Restart drill: kill the API mid-operation and prove nothing was duplicated.
#
#   restart-drill.sh <api-binary> <database-url>
#
# The integration suite already proves idempotency and concurrency under normal
# conditions. This proves the harder case the constitution asks for (§41): a
# process that dies *during* a write, and a client that retries afterwards.
#
# SIGKILL, not SIGTERM. A graceful shutdown drains in-flight requests, which is
# the easy case and the one already covered. SIGKILL is a power loss, an OOM
# kill, or a container evicted mid-transaction — the connection dies with the
# statement still open, and PostgreSQL rolls it back. What this checks is that
# the *client's retry* then produces exactly one effect rather than a second
# shipment, a second AWB, or a second ledger posting.
#
# # The two-phase retry, and why it is the real contract
#
# A killed request leaves its idempotency key IN_PROGRESS: the reservation was
# written, the work was not. The server then has to answer a retry without
# knowing whether the original is still running somewhere else, and it takes the
# only safe position — it refuses for a lease window
# (idempotency.DefaultStaleAfter, 2 minutes) and reclaims the key afterwards.
#
# So the drill asserts both halves:
#
#   * an immediate retry is REFUSED with 409, and creates nothing. Reclaiming
#     instantly would risk executing twice a request that had not actually died.
#   * a retry after the lease expires SUCCEEDS, exactly once per key.
#
# An earlier version of this script asserted only the second half, retried
# immediately, and reported a defect that was the system behaving correctly.
# STALE_AFTER below must match the server's lease.

set -Euo pipefail

API_BIN="${1:?usage: restart-drill.sh <api-binary> <database-url>}"
DB_URL="${2:?usage: restart-drill.sh <api-binary> <database-url>}"
PORT="${PORT:-8094}"
BASE="http://127.0.0.1:${PORT}"
EMAIL="${EMAIL:-admin@demo.test}"
PASSWORD="${PASSWORD:-DemoPassw0rd!2026}"
PSQL="${PSQL:-docker exec -i cos-pg psql -U courier -d courier_os_dr -X -t -A -c}"
# Must match idempotency.DefaultStaleAfter, plus a little slack.
STALE_AFTER="${STALE_AFTER:-130}"

pass=0; fail=0
ok()   { printf '  PASS  %s\n' "$*"; pass=$((pass+1)); }
bad()  { printf '  FAIL  %s\n' "$*"; fail=$((fail+1)); }
log()  { printf '\n== %s\n' "$*"; }

start_api() {
    DATABASE_URL="${DB_URL}" \
    REDIS_URL="${REDIS_URL:-redis://localhost:56379/6}" \
    JWT_SECRET="${JWT_SECRET:-restart-drill-secret-0123456789ab}" \
    STORAGE_DRIVER=filesystem STORAGE_DIR="${STORAGE_DIR:-/tmp/restart-drill}" \
    HTTP_ADDR="127.0.0.1:${PORT}" METRICS_ADDR="127.0.0.1:$((PORT+1000))" \
    LOG_LEVEL=error RATE_LIMIT_PER_MINUTE=100000 \
    "${API_BIN}" >/tmp/restart-drill.log 2>&1 &
    API_PID=$!
    for _ in $(seq 1 40); do
        curl -sf "${BASE}/livez" >/dev/null 2>&1 && return 0
        sleep 0.5
    done
    echo "api did not come up" >&2; exit 1
}

kill_api_hard() { kill -9 "${API_PID}" 2>/dev/null || true; wait "${API_PID}" 2>/dev/null || true; }

token() {
    curl -s -X POST "${BASE}/api/v1/auth/login" -H 'Content-Type: application/json' \
        -d "{\"email\":\"${EMAIL}\",\"password\":\"${PASSWORD}\"}" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin)["tokens"]["accessToken"])'
}

count() { ${PSQL} "$1"; }

booking_body() {
    cat <<JSON
{"customerId":"${CUSTOMER_ID}","serviceCode":"EXPRESS","paymentMode":"PREPAID",
 "contentDescription":"restart drill $1",
 "sender":{"contactName":"Adebayo Okonkwo","phone":"08031234567","line1":"1 Marina",
           "city":"Lagos","state":"Lagos","pincode":"100001"},
 "recipient":{"contactName":"Chidinma Eze","phone":"08099887766","line1":"2 Wuse",
              "city":"Abuja","state":"Federal Capital Territory","pincode":"900001"},
 "packages":[{"actualWeightGrams":1200}]}
JSON
}

# ---------------------------------------------------------------------------
log "starting api"
start_api
TOKEN="$(token)"
CUSTOMER_ID="$(count 'SELECT public_id FROM customers LIMIT 1')"
UNIT_ID="$(count "SELECT public_id FROM operating_units WHERE unit_type LIKE '%BRANCH%' LIMIT 1")"
echo "  api pid ${API_PID}, customer ${CUSTOMER_ID}"

# ---------------------------------------------------------------------------
log "1. booking: kill during a burst, restart, retry every key"

BEFORE="$(count 'SELECT count(*) FROM shipments')"
STAMP="$(date +%s)"
KEYS=()
BODIES=()
for i in $(seq 0 11); do
    KEYS+=("restart-drill-${STAMP}-${i}")
    BODIES+=("$(booking_body "${i}")")
done

# Fire the burst and kill the process while it is mid-flight.
for i in "${!KEYS[@]}"; do
    curl -s -o /dev/null -X POST "${BASE}/api/v1/shipments" \
        -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
        -H "Idempotency-Key: ${KEYS[$i]}" --data "${BODIES[$i]}" &
done
sleep 0.12
kill_api_hard
wait 2>/dev/null || true

MID="$(count 'SELECT count(*) FROM shipments')"
STUCK="$(count "SELECT count(*) FROM idempotency_keys
  WHERE idempotency_key LIKE 'restart-drill-${STAMP}-%' AND status = 'IN_PROGRESS'")"
echo "  shipments ${BEFORE} -> ${MID}; keys left IN_PROGRESS: ${STUCK}"

start_api
TOKEN="$(token)"

# --- half one: an immediate retry must be refused, not executed -------------
IMMEDIATE_CREATED_BEFORE="$(count 'SELECT count(*) FROM shipments')"
CONFLICTS=0
for i in "${!KEYS[@]}"; do
    CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST "${BASE}/api/v1/shipments" \
        -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
        -H "Idempotency-Key: ${KEYS[$i]}" --data "${BODIES[$i]}")"
    [ "${CODE}" = "409" ] && CONFLICTS=$((CONFLICTS+1))
done
IMMEDIATE_CREATED="$(( $(count 'SELECT count(*) FROM shipments') - IMMEDIATE_CREATED_BEFORE ))"
echo "  immediate retries: ${CONFLICTS}/${#KEYS[@]} refused with 409, ${IMMEDIATE_CREATED} created"

if [ "${IMMEDIATE_CREATED}" -eq 0 ] && [ "${CONFLICTS}" -ge "${STUCK}" ]; then
    ok "an immediate retry after a crash is refused, not executed twice"
else
    bad "immediate retry created ${IMMEDIATE_CREATED} shipments and refused only ${CONFLICTS}"
fi

# --- half two: after the lease expires, the retry must succeed exactly once --
echo "  waiting ${STALE_AFTER}s for the idempotency lease to go stale"
sleep "${STALE_AFTER}"

for i in "${!KEYS[@]}"; do
    curl -s -o /dev/null -X POST "${BASE}/api/v1/shipments" \
        -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
        -H "Idempotency-Key: ${KEYS[$i]}" --data "${BODIES[$i]}"
done
# And once more, to prove the completed key now replays instead of re-creating.
for i in "${!KEYS[@]}"; do
    curl -s -o /dev/null -X POST "${BASE}/api/v1/shipments" \
        -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
        -H "Idempotency-Key: ${KEYS[$i]}" --data "${BODIES[$i]}"
done

AFTER="$(count 'SELECT count(*) FROM shipments')"
CREATED=$((AFTER - BEFORE))
echo "  shipments ${BEFORE} -> ${AFTER} (created ${CREATED} for ${#KEYS[@]} keys, retried twice)"

if [ "${CREATED}" -eq "${#KEYS[@]}" ]; then
    ok "each key produced exactly one shipment across a hard restart and two retries"
else
    bad "${#KEYS[@]} keys produced ${CREATED} shipments — a retry duplicated or lost work"
fi

log "2. AWB uniqueness survived the restart"

DUPES="$(count "SELECT count(*) FROM (SELECT awb FROM shipments GROUP BY awb HAVING count(*) > 1) d")"
if [ "${DUPES}" = "0" ]; then
    ok "no duplicate AWB"
else
    bad "${DUPES} duplicated AWBs — allocation is not restart-safe"
fi

GAPS="$(count "SELECT count(*) FROM shipments WHERE awb IS NULL OR awb = ''")"
[ "${GAPS}" = "0" ] && ok "no shipment without an AWB" || bad "${GAPS} shipments have no AWB"

# ---------------------------------------------------------------------------
log "3. no partially written shipment"

# A shipment with no charge snapshot or no address snapshot would mean a
# transaction committed halfway, which the whole design forbids.
# BULK% rows are synthetic load-test fixtures inserted straight into the table;
# they never went through booking and have no snapshots by construction.
# Including them would report 150,000 torn writes that are nothing of the kind.
ORPHAN_CHARGES="$(count "SELECT count(*) FROM shipments s
  LEFT JOIN shipment_charge_snapshots c ON c.shipment_id = s.id
  WHERE c.id IS NULL AND s.awb NOT LIKE 'BULK%'")"
ORPHAN_ADDR="$(count "SELECT count(*) FROM shipments s
  LEFT JOIN shipment_address_snapshots a ON a.shipment_id = s.id
  WHERE a.id IS NULL AND s.awb NOT LIKE 'BULK%'")"
ORPHAN_EVENTS="$(count "SELECT count(*) FROM shipments s
  LEFT JOIN shipment_events e ON e.shipment_id = s.id
  WHERE e.id IS NULL AND s.awb NOT LIKE 'BULK%'")"

[ "${ORPHAN_CHARGES}" = "0" ] && ok "every shipment has its price snapshot" \
    || bad "${ORPHAN_CHARGES} shipments have no charge snapshot — a torn write"
[ "${ORPHAN_ADDR}" = "0" ] && ok "every shipment has its address snapshot" \
    || bad "${ORPHAN_ADDR} shipments have no address snapshot — a torn write"
[ "${ORPHAN_EVENTS}" = "0" ] && ok "every shipment has at least one event" \
    || bad "${ORPHAN_EVENTS} shipments have no event — history is not append-only"

# ---------------------------------------------------------------------------
log "4. scans: kill mid-burst, retry, no duplicate accepted scans"

AWB="$(count "SELECT awb FROM shipments WHERE awb NOT LIKE 'BULK%' ORDER BY id DESC LIMIT 1")"
SCANS_BEFORE="$(count "SELECT count(*) FROM scan_events")"

for i in $(seq 1 8); do
    curl -s -o /dev/null -X POST "${BASE}/api/v1/scans" \
        -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
        -d "{\"barcode\":\"${AWB}\",\"scanType\":\"RECEIVE\",\"operatingUnitId\":\"${UNIT_ID}\"}" &
done
sleep 0.1
kill_api_hard
wait 2>/dev/null || true
start_api
TOKEN="$(token)"

for i in $(seq 1 8); do
    curl -s -o /dev/null -X POST "${BASE}/api/v1/scans" \
        -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
        -d "{\"barcode\":\"${AWB}\",\"scanType\":\"RECEIVE\",\"operatingUnitId\":\"${UNIT_ID}\"}"
done

# A repeated scan of the same shipment at the same unit is a no-op by design,
# so the event count must not grow once per retry.
SCANS_AFTER="$(count "SELECT count(*) FROM scan_events")"
STATUS_EVENTS="$(count "SELECT count(*) FROM shipment_events WHERE shipment_id =
  (SELECT id FROM shipments WHERE awb = '${AWB}') AND to_status = 'ORIGIN_BRANCH_RECEIVED'")"
echo "  scan_events ${SCANS_BEFORE} -> ${SCANS_AFTER}; ORIGIN_BRANCH_RECEIVED events: ${STATUS_EVENTS}"
if [ "${STATUS_EVENTS}" -le 1 ]; then
    ok "repeated scans across a restart produced at most one state change"
else
    bad "${STATUS_EVENTS} receive events for one shipment — a retry moved it twice"
fi

# ---------------------------------------------------------------------------
log "5. money: the ledger still balances"

UNBALANCED="$(count "SELECT count(*) FROM (
  SELECT jt.id FROM journal_transactions jt
  JOIN journal_entries je ON je.transaction_id = jt.id
  WHERE jt.status = 'POSTED' GROUP BY jt.id
  HAVING sum(je.debit_minor) <> sum(je.credit_minor)) b")"
[ "${UNBALANCED}" = "0" ] && ok "every posted journal balances" \
    || bad "${UNBALANCED} posted journals do not balance"

ORPHAN_ENTRIES="$(count "SELECT count(*) FROM journal_entries je
  LEFT JOIN journal_transactions jt ON jt.id = je.transaction_id WHERE jt.id IS NULL")"
[ "${ORPHAN_ENTRIES}" = "0" ] && ok "no ledger entry without its transaction" \
    || bad "${ORPHAN_ENTRIES} orphaned ledger entries"

# ---------------------------------------------------------------------------
log "6. the process is healthy after all of it"
curl -sf "${BASE}/readyz" >/dev/null && ok "readyz answers" || bad "readyz does not answer"

kill -TERM "${API_PID}" 2>/dev/null || true
wait "${API_PID}" 2>/dev/null || true

printf '\n== %d passed, %d failed\n' "${pass}" "${fail}"
[ "${fail}" -eq 0 ]
