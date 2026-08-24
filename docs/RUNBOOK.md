# Runbook

For whoever is on call. Assumes SSH to the VPS and `cd /opt/courier-os`.

Written to be read at 3am: symptom first, then the check, then the action.
Where an action is destructive it says so before the command, not after.

---

## 0. First ninety seconds

Whatever the alert, start here.

```bash
docker compose -f deploy/docker-compose.prod.yml ps          # what is running
curl -s localhost:8080/readyz                                # dependencies
curl -s localhost:9090/metrics | grep courier_app_build_info # which build
df -h /                                                      # disk
free -m                                                      # memory
docker compose -f deploy/docker-compose.prod.yml logs --tail=100 api
```

Those five answers separate almost every incident into one of: **out of disk**,
**out of memory**, **database unreachable**, **bad deploy**, or **slow query**.

---

## 1. API returns 5xx

**Check**

```bash
docker compose logs --tail=200 api | grep '"level":"ERROR"'
curl -s localhost:9090/metrics | grep courier_app_dependency_up
curl -s localhost:9090/metrics | grep 'courier_db_pool_connections'
```

**Diagnose**

| Signal | Cause | Go to |
|---|---|---|
| `dependency_up{postgres} 0` | Database down or unreachable | §3 |
| `dependency_up{redis} 0` | Redis down | §4 |
| `acquired` ≈ `max` | Pool exhausted | §2 |
| Errors started at a deploy | Bad release | §7 |
| Nothing obvious | Take the `request_id` from an error and grep it | below |

**Trace a single failure end to end**

```bash
docker compose logs api | grep "req_01KZJT..."
```

Every error envelope returned to a client carries `requestId`. Ask the reporter
for it — it is the fastest path to the actual failure.

---

## 2. Everything is slow / pool exhausted

The characteristic signature is **every route slowing at once**. That is almost
never "the app got slow"; it is connection starvation caused by something
holding connections.

**Check**

```bash
curl -s localhost:9090/metrics | grep courier_db_pool_connections
docker compose logs --tail=300 api | grep '"msg":"slow query"'
```

**Find the culprit in the database**

```bash
docker compose exec postgres psql -U courier -d courier_os -c "
SELECT pid, state, wait_event_type, wait_event,
       now() - query_start AS duration, left(query, 120) AS query
FROM pg_stat_activity
WHERE state <> 'idle' AND backend_type = 'client backend'
ORDER BY duration DESC LIMIT 20;"
```

**Locks**

```bash
docker compose exec postgres psql -U courier -d courier_os -c "
SELECT blocked.pid AS blocked_pid, blocking.pid AS blocking_pid,
       left(blocked.query,80) AS blocked_query,
       left(blocking.query,80) AS blocking_query
FROM pg_stat_activity blocked
JOIN pg_stat_activity blocking ON blocking.pid = ANY(pg_blocking_pids(blocked.pid))
WHERE cardinality(pg_blocking_pids(blocked.pid)) > 0;"
```

**Act**

Terminating a backend rolls its transaction back. That is safe for a read; for a
write it means the operation did not happen, which is the correct outcome for
something already stuck. Cancel first, terminate only if cancel does nothing:

```bash
# Ask it to stop.
docker compose exec postgres psql -U courier -d courier_os -c "SELECT pg_cancel_backend(<pid>);"
# Destructive: kills the connection and rolls back.
docker compose exec postgres psql -U courier -d courier_os -c "SELECT pg_terminate_backend(<pid>);"
```

Then find out why it was slow — `docs/releases/explain-analyze*.md` has the
method. A statement that got slow usually means a table grew past an index's
usefulness, or a plan flipped.

---

## 3. PostgreSQL is down or unreachable

```bash
docker compose ps postgres
docker compose logs --tail=100 postgres
df -h /                       # a full disk stops Postgres writing
```

**If the disk is full**, that is the cause and the fix is §6. Postgres will not
accept writes with no space, and it will not start cleanly either.

**If it is a crash loop**, read the log before restarting — a repeated restart
of a corrupt cluster makes recovery harder, not easier. Suspected corruption
goes to `docs/DISASTER_RECOVERY.md` §3.

**Restart**

```bash
docker compose restart postgres
sleep 10 && curl -s localhost:8080/readyz
```

The API tolerates this: pgx reconnects, and in-flight requests fail with 5xx for
the duration. No restart of the API is needed.

---

## 4. Redis is down

**Impact is deliberately limited.** Redis holds cache, rate-limit counters, OTP
and short-lived coordination — never authoritative business state. Losing it
means slower reads and weaker rate limiting, **not data loss**.

```bash
docker compose restart redis
docker compose exec redis redis-cli ping   # expect PONG
```

Cache repopulates on demand. Nothing to restore.

---

## 5. Worker backlog

```bash
curl -s localhost:9090/metrics | grep -E 'courier_jobs_(queue_depth|in_flight|processed_total)'
docker compose logs --tail=200 worker
```

**Is it stuck or just busy?** Rising `queue_depth` with `processed_total`
*also* rising is a busy system — wait, or raise `WORKER_CONCURRENCY`. Rising
depth with a **flat** `processed_total` is a stuck or dead worker.

```bash
docker compose restart worker
```

Restarting is safe. Jobs are claimed with a lease and re-run if not completed;
handlers are idempotent, which is proven by the restart tests in
`tests/integration/`.

**Inspect the queue**

```bash
docker compose exec postgres psql -U courier -d courier_os -c "
SELECT queue, status, count(*) FROM jobs GROUP BY 1,2 ORDER BY 3 DESC;"

-- Dead letters, with the reason.
SELECT id, job_type, attempts, left(last_error, 160)
FROM jobs WHERE status = 'DEAD' ORDER BY updated_at DESC LIMIT 20;
```

A dead-lettered job is a decision, not an error to clear blindly: read
`last_error` first, fix the cause, then requeue.

---

## 6. Disk filling up

```bash
df -h /
du -sh /var/lib/docker/containers/*/*.log | sort -rh | head
docker system df
```

**Safe to reclaim, in order:**

```bash
docker image prune -af          # old images — safe
docker builder prune -af        # build cache — safe
```

Container logs are already capped at 50 MB × 5 per service by
`deploy/docker-compose.prod.yml`. If they are large anyway, that cap is not
being applied — check the compose file in use is the production one.

**Do not** delete anything under the `postgres-data` volume. If PostgreSQL is
the thing filling the disk, the answer is table growth or bloat:

```bash
docker compose exec postgres psql -U courier -d courier_os -c "
SELECT relname, pg_size_pretty(pg_total_relation_size(relid)) AS size
FROM pg_catalog.pg_statio_user_tables ORDER BY pg_total_relation_size(relid) DESC LIMIT 15;"
```

`shipment_events` and `audit_events` are append-only and grow forever by design.
Archiving them is a planned operation, not an emergency one.

---

## 7. A deploy made things worse

**Confirm which build is running**

```bash
curl -s localhost:9090/metrics | grep courier_app_build_info
```

**Roll back**

```bash
export IMAGE_TAG=<previous-tag>
docker compose -f deploy/docker-compose.prod.yml up -d api worker
curl -s localhost:8080/readyz
```

**If the release included a migration, stop and think.** Rolling the image back
does not roll the schema back, and an older binary against a newer schema may
work or may not. `docs/DISASTER_RECOVERY.md` §6 covers this properly. The short
version: **forward-fix is usually safer than rolling a schema back**, and the
pre-migration backup exists for the case where it is not.

---

## 8. TLS / certificate problems

```bash
echo | openssl s_client -connect api.example.com:443 -servername api.example.com 2>/dev/null \
  | openssl x509 -noout -dates -subject
docker compose logs --tail=50 nginx
```

**Renew**

```bash
docker compose run --rm certbot renew
docker compose exec nginx nginx -s reload
```

A reload is not a restart: existing connections are served by the old worker
processes until they finish. No dropped requests.

---

## 9. Suspected security incident

Signals: `token_reuse` non-zero, a spike in `login{outcome="failed"}`, or
unexpected `4xx` on partner endpoints.

```bash
curl -s localhost:9090/metrics | grep 'courier_business_events_total{event="token_reuse"'
docker compose logs api | grep '"status":401' | tail -50

docker compose exec postgres psql -U courier -d courier_os -c "
SELECT actor_user_id, action, resource_type, request_id, created_at
FROM audit_events ORDER BY created_at DESC LIMIT 50;"
```

**Contain**

```sql
-- Revoke every session for one user.
UPDATE sessions SET revoked_at = now() WHERE user_id = <id> AND revoked_at IS NULL;
-- Deactivate the account.
UPDATE users SET status = 'INACTIVE' WHERE id = <id>;
-- Revoke a partner key.
UPDATE api_keys SET revoked_at = now() WHERE public_id = 'key_...';
```

All three take effect on the next request — sessions are checked per request,
not cached to expiry.

Then read `docs/SECURITY_REVIEW.md` §3 for what the controls are supposed to
prevent, and preserve logs before they rotate: 50 MB × 5 is not long during an
attack.

### Webhook egress / SSRF containment

The application accepts webhook destinations only as public HTTPS/443 URLs. It
validates every DNS answer at registration, immediately before delivery, and in
the dialer that pins the actual connection. Redirects and environment HTTP
proxies are disabled for webhook delivery.

Keep an independent infrastructure egress policy anyway. The API and worker
containers should be able to reach approved recursive DNS and public TCP/443,
but must not reach:

- loopback, RFC1918, carrier-grade NAT, link-local, multicast, unspecified, and
  IPv6 ULA ranges;
- the Docker bridge, database, Redis, metrics, host-management, or other private
  service networks;
- cloud metadata endpoints, including `169.254.169.254` and provider-specific
  IPv6 metadata addresses.

Apply and test those rules in the hosting firewall/container network layer; do
not rely on application validation as the only barrier. After a firewall or DNS
change, register a known public test receiver and confirm one delivery, then
confirm private literals and a hostname resolving to a private address are
rejected. A `302`/`307` response must be recorded as a failed attempt and must
not cause a second request.

---

## 10. Routine

**Daily** — automated, verify it happened:

```bash
ls -la /opt/courier-os/backups/ | tail -5
grep backup /var/log/syslog | tail -5
```

**Weekly**

```bash
docker compose exec postgres psql -U courier -d courier_os -c "
SELECT relname, n_dead_tup, last_autovacuum
FROM pg_stat_user_tables WHERE n_dead_tup > 10000 ORDER BY n_dead_tup DESC LIMIT 10;"
df -h /
```

**Monthly** — the one that matters:

```bash
./deploy/scripts/restore-test.sh
```

**A backup that has never been restored is not a backup.** This script restores
the latest backup into a throwaway database and verifies it. Run it, and read
the output rather than the exit code alone.

**Quarterly**

```bash
govulncheck ./...
```

---

## 11. Escalation

Before escalating, collect — it is what anyone will ask for first:

```bash
docker compose ps
curl -s localhost:9090/metrics > /tmp/metrics.txt
docker compose logs --tail=1000 > /tmp/logs.txt
df -h / && free -m
docker compose exec postgres psql -U courier -d courier_os -c \
  "SELECT count(*), state FROM pg_stat_activity GROUP BY state;"
```

Include the **build version**, the **request id** of a failing request, and what
changed most recently. Those three answer most of the first round of questions.
