# Deployment

Target: **Hostinger VPS KVM 4** — 4 vCPU, 16 GB RAM, 200 GB NVMe, Ubuntu 24.04
LTS, Docker Compose, Nginx, TLS.

---

## 1. Prerequisites

```bash
# Docker Engine and the compose plugin
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker "$USER"   # log out and back in

docker --version && docker compose version
```

PostgreSQL needs the `btree_gist` extension (ships with the official image and
with `postgresql-contrib`). Migration `0001` creates it; the database user must
be able to, which the compose superuser is.

---

## 2. Configuration

```bash
sudo mkdir -p /opt/courier-os && cd /opt/courier-os
# copy deploy/ from the repository, then:
cp .env.example .env
chmod 600 .env
```

Fill in `.env`. The values that must be set and must not be guessed:

```bash
# 48+ bytes of entropy. Rotating this invalidates every access token.
JWT_SECRET=$(openssl rand -base64 48)

POSTGRES_USER=courier
POSTGRES_PASSWORD=$(openssl rand -base64 32 | tr -d '/+=')
POSTGRES_DB=courier_os
DATABASE_URL=postgres://courier:<password>@postgres:5432/courier_os?sslmode=disable

# Full origins only. A wildcard is refused in production.
CORS_ALLOWED_ORIGINS=https://app.ceservlogistics.com,https://customer.ceservlogistics.com,https://track.ceservlogistics.com

IMAGE=ghcr.io/yourorg/courier-os:1.0.0
WEB_IMAGE=ghcr.io/yourorg/courier-os-web:1.0.0
VERSION=1.0.0
PUBLIC_TRACKING_URL=https://track.ceservlogistics.com/
```

The configuration layer validates everything at startup and refuses to run with
an insecure production setting: a short `JWT_SECRET`, a wildcard CORS origin,
debug routes enabled, or rate limiting disabled. A failure prints every problem
at once, before anything binds a port.

`sslmode=disable` is correct here because PostgreSQL is on the compose network
and never published. If you move the database off-box, switch to `require` and
supply a CA.

---

## 3. TLS

```bash
sudo apt install -y certbot
sudo certbot certonly --standalone \
  -d api.ceservlogistics.com \
  -d app.ceservlogistics.com \
  -d customer.ceservlogistics.com \
  -d track.ceservlogistics.com

sudo mkdir -p /opt/courier-os/nginx/certs
sudo cp /etc/letsencrypt/live/api.ceservlogistics.com/fullchain.pem /opt/courier-os/nginx/certs/
sudo cp /etc/letsencrypt/live/api.ceservlogistics.com/privkey.pem   /opt/courier-os/nginx/certs/
```

Renewal, with a reload rather than a restart so connections are not dropped:

```bash
sudo crontab -e
0 3 * * 1 certbot renew --quiet --deploy-hook \
  'cp /etc/letsencrypt/live/api.ceservlogistics.com/*.pem /opt/courier-os/nginx/certs/ && \
   docker compose -f /opt/courier-os/docker-compose.prod.yml exec nginx nginx -s reload'
```

Verify with `curl -vI https://api.yourdomain.com/livez` and check the grade at
ssllabs.com. TLS 1.0 and 1.1 are disabled; HSTS is set for 180 days.

Create four DNS `A` records, all pointing to the production VPS public IP:

| Host | Purpose |
|---|---|
| `app.ceservlogistics.com` | Staff and franchise application |
| `customer.ceservlogistics.com` | Customer self-service portal |
| `track.ceservlogistics.com` | Standalone public AWB tracking |
| `api.ceservlogistics.com` | Go API |

The canonical domain is `ceservlogistics.com` (without the extra `i` in
`logisitics`). The public website already uses this spelling.

---

## 4. First deployment

```bash
cd /opt/courier-os
docker compose -f docker-compose.prod.yml pull

# Migrations run as their own step and must succeed before the API starts;
# the compose dependency enforces that.
docker compose -f docker-compose.prod.yml run --rm migrate up
docker compose -f docker-compose.prod.yml run --rm migrate status

# Create the platform operator and the first SUPER_ADMIN.
BOOTSTRAP_ADMIN_EMAIL=ops@yourdomain.com \
BOOTSTRAP_ADMIN_PASSWORD='<a strong password>' \
  docker compose -f docker-compose.prod.yml run --rm migrate bootstrap

docker compose -f docker-compose.prod.yml up -d
```

`bootstrap` is idempotent — running it again changes nothing — and the account
it creates has `mustChangePassword: true`. **Remove the bootstrap variables from
the environment once the account exists.**

Verify:

```bash
curl -s https://api.yourdomain.com/livez
curl -s https://api.yourdomain.com/readyz | jq
curl -s https://api.yourdomain.com/version | jq
```

`readyz` returns 503 while any required dependency is down. A Redis outage
reports `degraded` and readiness still passes — Redis is a cache, not a system
of record.

---

## 5. Routine deployment

```bash
cd /opt/courier-os
export VERSION=1.1.0 IMAGE=ghcr.io/yourorg/courier-os:1.1.0

docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml run --rm migrate validate   # no edited migrations
docker compose -f docker-compose.prod.yml run --rm migrate up

# Rolling: two replicas, so the listener is never empty.
docker compose -f docker-compose.prod.yml up -d --no-deps --scale api=2 api
docker compose -f docker-compose.prod.yml up -d --no-deps worker
```

Migrations take a PostgreSQL advisory lock, so two instances starting at once
cannot apply the same migration twice. The API refuses to start against an
unmigrated database rather than failing confusingly at the first query.

### Rollback

```bash
export IMAGE=ghcr.io/yourorg/courier-os:1.0.0
docker compose -f docker-compose.prod.yml up -d --no-deps api worker
```

**Roll back the image first, and only roll back the schema if you must.** Every
migration has a `down`, but a rollback that drops a column discards data written
since the deploy. Prefer forward fixes. If a schema rollback is genuinely
required:

```bash
docker compose -f docker-compose.prod.yml run --rm migrate down --steps 1
```

Take a backup first (§7). Migrations are written to be additive where possible
precisely so this is rarely needed.

---

## 6. Operations

```bash
# Logs (JSON; pipe through jq)
docker compose -f docker-compose.prod.yml logs -f api | jq -R 'fromjson? // .'

# Errors only
docker compose -f docker-compose.prod.yml logs api | jq -R 'fromjson? // empty | select(.level=="ERROR")'

# Trace one request end to end
docker compose -f docker-compose.prod.yml logs | grep req_01KZG...

# Metrics (loopback-bound; never exposed publicly)
curl -s localhost:9090/metrics | grep courier_
```

Log rotation is configured in compose (50 MB x 5 per container). Without it a
chatty container can fill the disk and take the database down.

### What to watch

| Signal | Query | Act when |
|---|---|---|
| Error rate | `courier_http_requests_total{status="5xx"}` | any sustained non-zero |
| Latency | `courier_http_request_duration_seconds` p95 by route | booking p95 > 600 ms |
| Pool saturation | `courier_db_pool_connections{state="acquired"}` | sustained near `max` |
| Job backlog | `courier_jobs_queue_depth` | growing without draining |
| Dead letters | `courier_jobs_processed_total{outcome="failed"}` | any |
| Disk | `df -h` | above 75% |
| Slow queries | `log_min_duration_statement=500ms` in the Postgres log | new entries |

---

## 7. Backup and restore

### Nightly backup

```bash
cat > /opt/courier-os/backup.sh <<'SH'
#!/usr/bin/env bash
set -euo pipefail
cd /opt/courier-os
source .env
STAMP=$(date +%Y%m%d-%H%M%S)
OUT="/opt/courier-os/backups/courier-os-${STAMP}.dump"

# Custom format: compressed, parallel-restorable, selective.
docker compose -f docker-compose.prod.yml exec -T postgres \
  pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --compress=9 > "$OUT"

# A backup that has not been read is not a backup.
docker compose -f docker-compose.prod.yml exec -T postgres \
  pg_restore --list < "$OUT" > /dev/null
echo "verified $OUT ($(du -h "$OUT" | cut -f1))"

# Off-server copy. A backup on the same disk does not survive disk loss.
rclone copy "$OUT" remote:courier-os-backups/

find /opt/courier-os/backups -name '*.dump' -mtime +14 -delete
SH
chmod +x /opt/courier-os/backup.sh

# 02:30 daily
(crontab -l 2>/dev/null; echo "30 2 * * * /opt/courier-os/backup.sh >> /var/log/courier-backup.log 2>&1") | crontab -
```

### Restore

```bash
cd /opt/courier-os
docker compose -f docker-compose.prod.yml stop api worker

docker compose -f docker-compose.prod.yml exec -T postgres \
  psql -U "$POSTGRES_USER" -d postgres -c \
  "DROP DATABASE IF EXISTS ${POSTGRES_DB}_restore; CREATE DATABASE ${POSTGRES_DB}_restore;"

docker compose -f docker-compose.prod.yml exec -T postgres \
  pg_restore -U "$POSTGRES_USER" -d "${POSTGRES_DB}_restore" -j 2 < backups/<file>.dump

# Sanity-check the restored copy before swapping it in.
docker compose -f docker-compose.prod.yml exec -T postgres \
  psql -U "$POSTGRES_USER" -d "${POSTGRES_DB}_restore" -c \
  "SELECT (SELECT count(*) FROM shipments) AS shipments,
          (SELECT count(*) FROM shipment_events) AS events,
          (SELECT max(version) FROM schema_migrations) AS schema_version;"

# Swap, then restart.
docker compose -f docker-compose.prod.yml exec -T postgres psql -U "$POSTGRES_USER" -d postgres -c \
  "ALTER DATABASE ${POSTGRES_DB} RENAME TO ${POSTGRES_DB}_old;
   ALTER DATABASE ${POSTGRES_DB}_restore RENAME TO ${POSTGRES_DB};"
docker compose -f docker-compose.prod.yml up -d api worker
```

**Rehearse this quarterly.** A restore procedure that has never been executed is
a hypothesis, not a plan. Record the date and the measured restore time.

---

## 8. Disaster scenarios

| Scenario | Response | Data loss |
|---|---|---|
| **Total VPS loss** | Provision a new box, install Docker, restore `.env` and certificates from your secret store, restore the latest dump, `up -d`. | Up to 24 h — one backup interval. Shorten with WAL archiving if unacceptable. |
| **Database corruption** | Stop the API, restore into `_restore`, verify counts, swap. | Since the last good backup. |
| **Redis loss** | None required; the API degrades to always-miss and recovers automatically. Restart Redis when convenient. | None. Redis holds only rebuildable state. |
| **Failed migration** | The migration ran in a transaction and rolled back; the advisory lock is released. Read the error, fix the migration, re-run. If it was a `no-transaction` migration, inspect `schema_migrations` and reconcile by hand. | None. |
| **Bad deployment** | Roll the image back (§5). Do not roll back the schema unless the new schema is the cause. | None. |
| **TLS expiry** | `certbot renew --force-renewal` and reload Nginx. Monitor expiry rather than discovering it from users. | None (outage only). |
| **Disk full** | Check `docker system df`; prune images, verify log rotation, trim old backups. PostgreSQL stops accepting writes before it corrupts anything. | None. |
| **Credential compromise** | Rotate `JWT_SECRET` (signs every user out), rotate the database password, `POST /users/{id}/revoke-sessions` for affected accounts, review `/api/v1/audit-events`. | None. |

---

## 9. Security checklist

- [ ] `.env` is `chmod 600` and not in version control
- [ ] `JWT_SECRET` is 48+ random bytes and unique to this environment
- [ ] `CORS_ALLOWED_ORIGINS` lists exact origins; no wildcard
- [ ] `TRUSTED_PROXY_CIDRS` is the Nginx address only — a wide range lets a
      client spoof `X-Forwarded-For` and bypass rate limiting
- [ ] `ENABLE_DEBUG_ROUTES=false` (the config layer enforces it in production)
- [ ] `RATE_LIMIT_ENABLED=true` (likewise)
- [ ] PostgreSQL and Redis ports are not published to the host
- [ ] `/metrics` is bound to loopback and returns 404 through Nginx
- [ ] TLS 1.2+ only; HSTS set
- [ ] Bootstrap credentials removed from the environment after first use
- [ ] Off-server backups verified and a restore rehearsed
- [ ] `ufw` allows only 22, 80 and 443

---

## 10. Tuning

`deploy/postgres/postgresql.prod.conf` is sized for this box. If you move to
larger hardware, scale `shared_buffers` to ~25% of RAM and
`effective_cache_size` to ~50%, and raise `DATABASE_MAX_CONNS` only after
confirming from `courier_db_pool_connections` that the pool is genuinely the
constraint — more connections on four cores makes things slower, not faster.

Measured on this configuration (see `docs/releases/release-1-backend.md` for the
full run): booking p95 30 ms, quote p95 4 ms, PIN code lookup p95 3 ms, at 33
requests per second with zero errors and 73 MB of heap.
