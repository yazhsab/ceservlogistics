#!/usr/bin/env bash
#
# Restore a backup into a named database.
#
#   restore.sh <backup-file> [target-db]
#
# Defaults the target to courier_os_restore rather than the live database. That
# default is the whole safety design: the dangerous operation has to be typed
# out in full, so nobody restores over production by pressing up-arrow.
#
# Restoring over the live database requires --force AND the exact database name,
# and it still refuses while the API is running.

set -Eeuo pipefail

COMPOSE_FILE="${COMPOSE_FILE:-/opt/courier-os/deploy/docker-compose.prod.yml}"
PG_SERVICE="${PG_SERVICE:-postgres}"
PG_USER="${POSTGRES_USER:-courier}"
LIVE_DB="${POSTGRES_DB:-courier_os}"
AGE_IDENTITY="${AGE_IDENTITY:-}"     # private key file, kept OFF this host normally

FORCE=0
ARGS=()
for arg in "$@"; do
    case "${arg}" in
        --force) FORCE=1 ;;
        *) ARGS+=("${arg}") ;;
    esac
done

BACKUP="${ARGS[0]:-}"
TARGET_DB="${ARGS[1]:-courier_os_restore}"

log() { printf '%s [restore] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
die() { printf '%s [restore] FATAL %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; exit 1; }

[ -n "${BACKUP}" ] || die "usage: restore.sh <backup-file> [target-db] [--force]"
[ -f "${BACKUP}" ] || die "no such file: ${BACKUP}"

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

# ---------------------------------------------------------------------------
# Guard rails
# ---------------------------------------------------------------------------
if [ "${TARGET_DB}" = "${LIVE_DB}" ]; then
    [ "${FORCE}" -eq 1 ] || die "refusing to restore over the live database ${LIVE_DB} without --force"

    # An API writing into a database being restored produces a mixture of old
    # and new rows that looks successful and is not recoverable from.
    if docker compose -f "${COMPOSE_FILE}" ps --status running api 2>/dev/null | grep -q api; then
        die "the api service is running; stop it first: docker compose stop api worker"
    fi
    log "WARNING: restoring over the LIVE database ${LIVE_DB} in 10 seconds — Ctrl-C to abort"
    sleep 10
fi

# ---------------------------------------------------------------------------
# Decrypt
# ---------------------------------------------------------------------------
DUMP="${WORK}/restore.dump"
case "${BACKUP}" in
    *.age)
        [ -n "${AGE_IDENTITY}" ] || die "backup is age-encrypted but AGE_IDENTITY is not set"
        log "decrypting with age"
        age -d -i "${AGE_IDENTITY}" -o "${DUMP}" "${BACKUP}"
        ;;
    *.gpg)
        log "decrypting with gpg"
        gpg --batch --yes -o "${DUMP}" -d "${BACKUP}"
        ;;
    *)
        cp "${BACKUP}" "${DUMP}"
        ;;
esac

# ---------------------------------------------------------------------------
# Integrity
#
# Checked against the checksum taken at backup time, over the *plaintext*, so
# this proves the data round-tripped — not merely that decryption succeeded.
# ---------------------------------------------------------------------------
SUMFILE="${BACKUP%%.age}"; SUMFILE="${SUMFILE%%.gpg}"; SUMFILE="${SUMFILE%.dump}.sha256"
if [ -f "${SUMFILE}" ]; then
    EXPECTED="$(cat "${SUMFILE}")"
    ACTUAL="$(sha256sum "${DUMP}" | awk '{print $1}')"
    [ "${EXPECTED}" = "${ACTUAL}" ] || die "checksum mismatch: expected ${EXPECTED}, got ${ACTUAL}"
    log "checksum verified"
else
    log "WARNING: no .sha256 alongside the backup; integrity not verified"
fi

# ---------------------------------------------------------------------------
# Restore
# ---------------------------------------------------------------------------
log "recreating ${TARGET_DB}"
docker compose -f "${COMPOSE_FILE}" exec -T "${PG_SERVICE}" \
    psql -U "${PG_USER}" -d postgres -v ON_ERROR_STOP=1 \
    -c "DROP DATABASE IF EXISTS ${TARGET_DB} WITH (FORCE);" \
    -c "CREATE DATABASE ${TARGET_DB};"

log "restoring"
# --exit-on-error is deliberate: a partial restore that reports success is how
# you discover three tables are missing during the next incident.
#
# Single-threaded, and not by oversight: pg_restore refuses --jobs when the
# archive arrives on stdin ("parallel restore from standard input is not
# supported"), and streaming through `docker exec` is what avoids needing a
# second copy of the dump inside the container. On a database this size the
# restore is seconds either way; if it ever grows enough to matter, copy the
# dump into the container first and add --jobs there.
docker compose -f "${COMPOSE_FILE}" exec -T "${PG_SERVICE}" \
    pg_restore -U "${PG_USER}" -d "${TARGET_DB}" --no-owner --no-privileges \
               --exit-on-error < "${DUMP}"

# ---------------------------------------------------------------------------
# Prove it
# ---------------------------------------------------------------------------
log "verifying restored database"
docker compose -f "${COMPOSE_FILE}" exec -T "${PG_SERVICE}" \
    psql -U "${PG_USER}" -d "${TARGET_DB}" -v ON_ERROR_STOP=1 -X <<'SQL'
\echo '--- schema ---'
SELECT count(*) AS tables FROM information_schema.tables WHERE table_schema = 'public';
SELECT max(version) AS schema_version FROM schema_migrations;

\echo '--- core row counts ---'
SELECT 'organizations' AS t, count(*) FROM organizations
UNION ALL SELECT 'users', count(*) FROM users
UNION ALL SELECT 'shipments', count(*) FROM shipments
UNION ALL SELECT 'shipment_events', count(*) FROM shipment_events
UNION ALL SELECT 'journal_transactions', count(*) FROM journal_transactions
ORDER BY 1;

\echo '--- financial invariant: every posted journal must balance ---'
SELECT count(*) AS unbalanced_transactions FROM (
    SELECT jt.id
    FROM journal_transactions jt
    JOIN journal_entries je ON je.transaction_id = jt.id
    WHERE jt.status = 'POSTED'
    GROUP BY jt.id
    HAVING sum(je.debit_minor) <> sum(je.credit_minor)
) bad;
SQL

log "restore complete into ${TARGET_DB}"
log "NOTE: unbalanced_transactions above MUST be 0. Anything else means the"
log "      restored data is not trustworthy and the backup should be rejected."
