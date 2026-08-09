#!/usr/bin/env bash
#
# PostgreSQL backup: dump, compress, encrypt, ship off-server, prune, verify.
#
# Run from cron on the VPS. Every step is fail-fast, and the script is noisy
# about what it did — a backup that silently produced a zero-byte file is worse
# than no backup, because it looks like protection.
#
#   0 2 * * * /opt/courier-os/deploy/scripts/backup.sh >> /var/log/courier-backup.log 2>&1
#
# Requires: docker compose, gzip, and one of age/gpg for encryption.
# Optional:  rclone (off-server copy), curl (dead-man's-switch ping).

set -Eeuo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
COMPOSE_FILE="${COMPOSE_FILE:-/opt/courier-os/deploy/docker-compose.prod.yml}"
BACKUP_DIR="${BACKUP_DIR:-/opt/courier-os/backups}"
PG_SERVICE="${PG_SERVICE:-postgres}"
PG_USER="${POSTGRES_USER:-courier}"
PG_DB="${POSTGRES_DB:-courier_os}"

# Retention. Local is short because the disk is shared with the database;
# remote is where the real history lives.
RETAIN_LOCAL_DAYS="${RETAIN_LOCAL_DAYS:-7}"
RETAIN_REMOTE_DAYS="${RETAIN_REMOTE_DAYS:-30}"

# Encryption. age is preferred: one public key on the server, private key kept
# off it, so a stolen VPS yields no readable backups.
AGE_RECIPIENT="${AGE_RECIPIENT:-}"          # age1... public key
GPG_RECIPIENT="${GPG_RECIPIENT:-}"          # fallback

# Off-server destination, e.g. "s3:courier-backups/postgres".
RCLONE_REMOTE="${RCLONE_REMOTE:-}"

# Optional dead-man's switch (healthchecks.io, Better Uptime, ...). Pinged only
# on success, so a *missing* ping is the alert. A backup system that can only
# alert when it runs cannot tell you it stopped running.
HEARTBEAT_URL="${HEARTBEAT_URL:-}"

TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
STEM="courier_os_${TIMESTAMP}"
WORK="${BACKUP_DIR}/.work.$$"

log()  { printf '%s [backup] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
die()  { printf '%s [backup] FATAL %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; exit 1; }

cleanup() { rm -rf "${WORK}"; }
trap cleanup EXIT
trap 'die "failed at line ${LINENO}"' ERR

mkdir -p "${BACKUP_DIR}" "${WORK}"
chmod 700 "${BACKUP_DIR}"

# ---------------------------------------------------------------------------
# 1. Dump
#
# Custom format (-Fc), not plain SQL: it is compressed, it can be restored
# selectively, and pg_restore can list its contents — which is what makes
# verification possible without a full restore.
#
# Dumped from inside the container so the client version always matches the
# server version. A version mismatch here is a restore that fails at the worst
# possible moment.
# ---------------------------------------------------------------------------
DUMP="${WORK}/${STEM}.dump"
log "dumping ${PG_DB}"
docker compose -f "${COMPOSE_FILE}" exec -T "${PG_SERVICE}" \
    pg_dump -U "${PG_USER}" -d "${PG_DB}" --format=custom --compress=6 \
            --no-owner --no-privileges \
    > "${DUMP}"

DUMP_BYTES=$(wc -c < "${DUMP}")
[ "${DUMP_BYTES}" -gt 4096 ] || die "dump is ${DUMP_BYTES} bytes — refusing to keep it"
log "dumped ${DUMP_BYTES} bytes"

# ---------------------------------------------------------------------------
# 2. Verify the dump is readable before it is trusted
#
# This is the step that separates a backup from a file. pg_restore --list parses
# the archive's table of contents; a truncated or corrupt dump fails here rather
# than six months from now during an actual recovery.
# ---------------------------------------------------------------------------
log "verifying archive structure"
TOC="${WORK}/${STEM}.toc"
docker compose -f "${COMPOSE_FILE}" exec -T "${PG_SERVICE}" pg_restore --list < "${DUMP}" > "${TOC}" \
    || die "pg_restore --list failed: the dump is not readable"

TABLE_COUNT=$(grep -c 'TABLE DATA' "${TOC}" || true)
[ "${TABLE_COUNT}" -ge 40 ] || die "archive lists only ${TABLE_COUNT} tables — expected 40+; refusing"
log "archive lists ${TABLE_COUNT} tables"

# ---------------------------------------------------------------------------
# 3. Checksum, then encrypt
#
# The checksum is taken over the plaintext dump and stored alongside, so a
# restore can prove it got back exactly what was dumped — not merely that the
# ciphertext decrypted.
# ---------------------------------------------------------------------------
sha256sum "${DUMP}" | awk '{print $1}' > "${WORK}/${STEM}.sha256"

ARTIFACT="${DUMP}"
if [ -n "${AGE_RECIPIENT}" ]; then
    command -v age >/dev/null || die "AGE_RECIPIENT is set but age is not installed"
    log "encrypting with age"
    age -r "${AGE_RECIPIENT}" -o "${DUMP}.age" "${DUMP}"
    ARTIFACT="${DUMP}.age"
    rm -f "${DUMP}"
elif [ -n "${GPG_RECIPIENT}" ]; then
    command -v gpg >/dev/null || die "GPG_RECIPIENT is set but gpg is not installed"
    log "encrypting with gpg"
    gpg --batch --yes --trust-model always -r "${GPG_RECIPIENT}" -o "${DUMP}.gpg" -e "${DUMP}"
    ARTIFACT="${DUMP}.gpg"
    rm -f "${DUMP}"
else
    # Loud, because an unencrypted backup on a rented VPS is a data breach
    # waiting for a disk to be resold.
    log "WARNING: no AGE_RECIPIENT or GPG_RECIPIENT set — backup is NOT encrypted"
fi

mv "${ARTIFACT}" "${BACKUP_DIR}/"
mv "${WORK}/${STEM}.sha256" "${BACKUP_DIR}/"
chmod 600 "${BACKUP_DIR}/$(basename "${ARTIFACT}")" "${BACKUP_DIR}/${STEM}.sha256"
FINAL="${BACKUP_DIR}/$(basename "${ARTIFACT}")"
log "stored ${FINAL} ($(wc -c < "${FINAL}") bytes)"

# ---------------------------------------------------------------------------
# 4. Off-server copy
#
# A backup on the same disk as the database protects against exactly one
# failure — somebody dropping a table — and none of the ones that lose the host.
# ---------------------------------------------------------------------------
if [ -n "${RCLONE_REMOTE}" ]; then
    command -v rclone >/dev/null || die "RCLONE_REMOTE is set but rclone is not installed"
    log "copying to ${RCLONE_REMOTE}"
    rclone copy "${FINAL}" "${RCLONE_REMOTE}/" --checksum
    rclone copy "${BACKUP_DIR}/${STEM}.sha256" "${RCLONE_REMOTE}/" --checksum
    # Confirm it arrived rather than trusting the exit code.
    rclone lsf "${RCLONE_REMOTE}/$(basename "${FINAL}")" >/dev/null \
        || die "remote copy is not listable after upload"
    log "remote copy confirmed"
    rclone delete "${RCLONE_REMOTE}/" --min-age "${RETAIN_REMOTE_DAYS}d" || true
else
    log "WARNING: RCLONE_REMOTE not set — this backup exists only on this host"
fi

# ---------------------------------------------------------------------------
# 5. Prune local copies
# ---------------------------------------------------------------------------
find "${BACKUP_DIR}" -maxdepth 1 -name 'courier_os_*' -type f \
     -mtime "+${RETAIN_LOCAL_DAYS}" -print -delete | sed 's/^/pruned /' || true

REMAINING=$(find "${BACKUP_DIR}" -maxdepth 1 -name 'courier_os_*.dump*' -type f | wc -l | tr -d ' ')
log "local backups retained: ${REMAINING}"

# ---------------------------------------------------------------------------
# 6. Heartbeat
# ---------------------------------------------------------------------------
if [ -n "${HEARTBEAT_URL}" ]; then
    curl -fsS -m 10 "${HEARTBEAT_URL}" >/dev/null && log "heartbeat sent" || log "heartbeat failed"
fi

log "backup complete: ${FINAL}"
