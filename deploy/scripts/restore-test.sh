#!/usr/bin/env bash
#
# Monthly restore drill.
#
# Takes the newest backup, restores it into a throwaway database, checks that
# what came back is coherent, and destroys it. Exits non-zero on any failure so
# cron mails you.
#
#   0 4 1 * * /opt/courier-os/deploy/scripts/restore-test.sh >> /var/log/courier-restore-test.log 2>&1
#
# This exists because of one rule in the constitution (§39): a backup is not
# valid until a restore is tested. Everything else in the backup system is
# theatre until this script has run and passed.

set -Eeuo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-/opt/courier-os/deploy/docker-compose.prod.yml}"
BACKUP_DIR="${BACKUP_DIR:-/opt/courier-os/backups}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PG_SERVICE="${PG_SERVICE:-postgres}"
PG_USER="${POSTGRES_USER:-courier}"
LIVE_DB="${POSTGRES_DB:-courier_os}"
TEST_DB="courier_os_restoretest"
MAX_AGE_HOURS="${MAX_AGE_HOURS:-30}"

log() { printf '%s [restore-test] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
die() { printf '%s [restore-test] FAIL %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; exit 1; }

psql_test() {
    docker compose -f "${COMPOSE_FILE}" exec -T "${PG_SERVICE}" \
        psql -U "${PG_USER}" -d "${TEST_DB}" -X -t -A -c "$1"
}

cleanup() {
    docker compose -f "${COMPOSE_FILE}" exec -T "${PG_SERVICE}" \
        psql -U "${PG_USER}" -d postgres -X -q \
        -c "DROP DATABASE IF EXISTS ${TEST_DB} WITH (FORCE);" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# 1. The newest backup, and it must be recent
#
# A restore drill that passes against a three-week-old backup proves the restore
# path and hides the fact that backups stopped running.
# ---------------------------------------------------------------------------
LATEST="$(find "${BACKUP_DIR}" -maxdepth 1 -name 'courier_os_*.dump*' -type f -print0 \
          | xargs -0 ls -t 2>/dev/null | head -1 || true)"
[ -n "${LATEST}" ] || die "no backup found in ${BACKUP_DIR}"

AGE_SECONDS=$(( $(date +%s) - $(stat -c %Y "${LATEST}" 2>/dev/null || stat -f %m "${LATEST}") ))
AGE_HOURS=$(( AGE_SECONDS / 3600 ))
log "newest backup: ${LATEST} (${AGE_HOURS}h old)"
[ "${AGE_HOURS}" -le "${MAX_AGE_HOURS}" ] \
    || die "newest backup is ${AGE_HOURS}h old, limit ${MAX_AGE_HOURS}h — backups are not running"

# ---------------------------------------------------------------------------
# 2. Restore into a throwaway database
# ---------------------------------------------------------------------------
log "restoring into ${TEST_DB}"
"${SCRIPT_DIR}/restore.sh" "${LATEST}" "${TEST_DB}" >/dev/null || die "restore.sh failed"

# ---------------------------------------------------------------------------
# 3. Is what came back coherent?
#
# Structure, then content, then the one invariant that must never be false.
# ---------------------------------------------------------------------------
TABLES=$(psql_test "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';")
log "tables: ${TABLES}"
[ "${TABLES}" -ge 100 ] || die "only ${TABLES} tables restored"

SCHEMA_VERSION=$(psql_test "SELECT coalesce(max(version),0) FROM schema_migrations;")
log "schema version: ${SCHEMA_VERSION}"
[ "${SCHEMA_VERSION}" -ge 33 ] || die "schema version ${SCHEMA_VERSION} is behind the application"

ORGS=$(psql_test "SELECT count(*) FROM organizations;")
[ "${ORGS}" -ge 1 ] || die "no organizations in the restored database"
log "organizations: ${ORGS}"

# The financial invariant. If this is not zero the backup is not usable, no
# matter how cleanly it restored.
UNBALANCED=$(psql_test "
SELECT count(*) FROM (
    SELECT jt.id FROM journal_transactions jt
    JOIN journal_entries je ON je.transaction_id = jt.id
    WHERE jt.status = 'POSTED'
    GROUP BY jt.id HAVING sum(je.debit_minor) <> sum(je.credit_minor)
) bad;")
[ "${UNBALANCED}" = "0" ] || die "${UNBALANCED} posted journals do not balance in the restored data"
log "double-entry invariant holds"

# Foreign keys are enforced by the restore itself, but an orphaned event would
# mean the dump captured a torn state.
ORPHANS=$(psql_test "
SELECT count(*) FROM shipment_events se
LEFT JOIN shipments s ON s.id = se.shipment_id WHERE s.id IS NULL;")
[ "${ORPHANS}" = "0" ] || die "${ORPHANS} shipment events reference missing shipments"
log "referential integrity holds"

# ---------------------------------------------------------------------------
# 4. Compare against live, when live is present
#
# A restored database with dramatically fewer rows than production restored
# *something* — just not everything.
# ---------------------------------------------------------------------------
if docker compose -f "${COMPOSE_FILE}" exec -T "${PG_SERVICE}" \
       psql -U "${PG_USER}" -d "${LIVE_DB}" -X -t -A -c "SELECT 1" >/dev/null 2>&1; then
    LIVE_SHIPMENTS=$(docker compose -f "${COMPOSE_FILE}" exec -T "${PG_SERVICE}" \
        psql -U "${PG_USER}" -d "${LIVE_DB}" -X -t -A -c "SELECT count(*) FROM shipments;")
    TEST_SHIPMENTS=$(psql_test "SELECT count(*) FROM shipments;")
    log "shipments — live ${LIVE_SHIPMENTS}, restored ${TEST_SHIPMENTS}"
    if [ "${LIVE_SHIPMENTS}" -gt 100 ]; then
        # 90%: the backup is older than live, so some drift is expected and
        # correct. A large gap is not.
        MIN=$(( LIVE_SHIPMENTS * 90 / 100 ))
        [ "${TEST_SHIPMENTS}" -ge "${MIN}" ] \
            || die "restored ${TEST_SHIPMENTS} shipments against ${LIVE_SHIPMENTS} live (min ${MIN})"
    fi
fi

log "PASS — ${LATEST} restored and verified"
log "This is the evidence that the backup is real. Record the date."
