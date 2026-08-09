# Disaster Recovery — M35

What is protected, how long recovery takes, and what is genuinely lost in each
failure. Written to be usable during an incident, and honest about the cases it
does not cover.

The constitution's rule (§39) is the one that shaped this: **a backup is not
valid until a restore has been tested.** Everything here has been executed, and
§7 records the results.

---

## 1. What the system is made of, and what protects each part

| Component | Authoritative? | Protection | Loss if it dies unprotected |
|---|---|---|---|
| PostgreSQL | **Yes** — all business truth | Nightly encrypted dump, off-server | Everything. Shipments, ledger, COD, settlements |
| Object storage (S3) | Yes — POD photos, signatures | Provider durability + versioning | Delivery evidence; a disputed delivery becomes unprovable |
| Redis | **No** | None, by design | Cache and rate-limit counters. Rebuilt on demand |
| Application | No | Git + registry image | Nothing; redeploy |
| Nginx / TLS | No | Config in git, certs re-issuable | Nothing; ~5 minutes to re-issue |

**Redis holding no authoritative state is a design decision, not an accident**
(constitution §29), and it is what makes this list short. Losing Redis is an
inconvenience. Losing PostgreSQL is the business.

---

## 2. RPO and RTO

| | Target | Basis |
|---|---|---|
| **RPO** | **24 hours** | Nightly dump at 02:00 UTC. Worst case is a failure at 01:59, losing a full day |
| **RTO** | **≤ 1 hour** for total host loss | Measured restore is minutes; the hour is provisioning, DNS and verification |

**The 24-hour RPO is the honest weak point of this design.** For a courier
network handling COD, losing a day of shipments and cash custody records is
severe: the parcels physically exist, so operations can be reconstructed from
paper and from franchise records, but it is a genuinely bad day.

If that is unacceptable — and past meaningful COD volume it should be —
**enable continuous archiving.** With WAL archiving to object storage the RPO
becomes the archive interval, typically under five minutes. It is not enabled
here because it needs a second storage target and a restore procedure with more
moving parts, and shipping a simple thing that works beats shipping a
sophisticated thing that has never been rehearsed. It is the first thing to add.

### Measured

Against 150,025 shipments in a 167 MB database:

| Step | Time | Output |
|---|---|---|
| `pg_dump -Fc --compress=6` | < 1 s | 4.8 MB |
| `pg_restore --list` (integrity) | < 1 s | 144 tables |
| Restore into a clean database | **6 s** | 124 MB, 144 tables |
| Verification queries | < 1 s | all pass |

**Caveat, stated plainly:** these were measured on an M-series Mac under
OrbStack, which has faster storage than a Hostinger KVM 4. Assume **3–5× slower**
on the target — call it 30 seconds for a restore of this size. Even generously
padded, the database restore is not what makes the RTO an hour; provisioning the
replacement host is.

---

## 3. Scenarios

### 3.1 Total VPS loss

*Provider outage, disk failure, account suspension, host destroyed.*

**Detection:** external uptime monitor. Nothing on the host survives to alert.

**Recovery**

1. Provision a new VPS (Ubuntu 24.04, same size or larger).
2. Install Docker, clone the repository, restore `.env` from your secret store.
3. Retrieve the newest backup and the age private key from off-server storage.
4. Bring up PostgreSQL only:
   ```bash
   docker compose -f deploy/docker-compose.prod.yml up -d postgres
   ```
5. Restore:
   ```bash
   AGE_IDENTITY=/root/age-key.txt \
     ./deploy/scripts/restore.sh /tmp/courier_os_YYYYMMDD.dump.age courier_os --force
   ```
6. Start everything, point DNS at the new address, re-issue TLS.
7. Verify with the smoke tests in `docs/PRODUCTION_READINESS.md`.

**Lost:** up to 24 hours of data. **RTO:** ~1 hour, dominated by provisioning
and DNS propagation.

**The dependency that fails this scenario:** if the age private key lived only
on the destroyed host, the backups are unreadable and the answer is "we have no
data". Keep it in a password manager, and verify you can retrieve it *before*
you need it.

### 3.2 PostgreSQL corruption

*Postgres will not start, or reports corrupt pages.*

**Do not restart it repeatedly.** Each attempt on a damaged cluster can make
recovery harder. Read the log first.

```bash
docker compose logs postgres | tail -100
docker compose stop api worker          # stop writers before anything else
```

If the data directory is salvageable, dump what you can before overwriting:

```bash
docker compose exec postgres pg_dump -U courier -d courier_os -Fc > /tmp/emergency.dump
```

Then restore from the last good backup. Prefer restoring into a **new** database
and switching over, rather than overwriting: it leaves the damaged copy
available for forensics, and forensics is how you find out whether this was
hardware or something you did.

**Lost:** back to the last backup, unless the emergency dump succeeded.

### 3.3 Redis loss

*Container gone, data flushed, memory evicted.*

```bash
docker compose restart redis
docker compose exec redis redis-cli ping
```

**Nothing to restore.** Cache repopulates on demand. Rate-limit counters reset,
which briefly widens the login-attempt budget — acceptable, and the per-account
budget still applies. OTPs in flight are invalidated and must be resent.

**Lost:** nothing authoritative. **RTO:** under a minute.

### 3.4 Object storage failure

*S3 endpoint unreachable, bucket deleted, credentials revoked.*

**Unreachable:** POD upload fails and the delivery cannot be completed with
evidence. Shipment data is unaffected — the database holds the metadata, and the
artifacts are re-uploadable once storage returns.

**Bucket actually lost:** delivery evidence is gone and cannot be rebuilt. The
database still knows a POD existed, its checksum and who uploaded it, so the
audit trail survives; the photograph does not.

**Mitigation, and it belongs to the provider not to this repository:** enable
**versioning** and a **lifecycle policy** on the bucket, and use a separate
credential with no delete permission for the application. A compromised
application credential should not be able to erase delivery evidence.

### 3.5 TLS / certificate failure

*Expired certificate, failed renewal, revoked chain.*

```bash
echo | openssl s_client -connect api.example.com:443 2>/dev/null | openssl x509 -noout -dates
docker compose run --rm certbot renew
docker compose exec nginx nginx -s reload
```

**No data at risk.** The API is unreachable until fixed, which for a courier
network means branches cannot book — so it is a severity-1 outage with a
five-minute fix. Monitor certificate expiry; do not learn about it from
customers.

### 3.6 Bad deployment

*A release breaks the application.*

```bash
curl -s localhost:9090/metrics | grep courier_app_build_info   # what is running
export IMAGE_TAG=<previous>
docker compose -f deploy/docker-compose.prod.yml up -d api worker
```

**If the release contained a migration, stop and think before rolling back.**
Reverting the image does not revert the schema. Three cases:

| Migration was | Do |
|---|---|
| Additive (new table/column, nullable) | Roll the image back. The old binary ignores what it does not know about |
| Destructive (dropped or renamed a column) | **Do not roll back blind.** The old binary will query a column that no longer exists. Forward-fix, or restore the pre-migration backup |
| Unknown | Treat as destructive |

This is why `deploy.sh` takes a backup immediately before running migrations.
That backup is the only thing that makes case two survivable.

### 3.7 Accidental data destruction

*A wrong `DELETE`, a script run against production, a dropped table.*

The most likely disaster on this list, and the one the nightly backup handles
worst — you lose everything since 02:00, including the good work.

Preferred order:

1. **Stop writers immediately.** `docker compose stop api worker`.
2. Restore the backup into a **separate** database — never over the live one.
3. Copy back only the affected rows, with the live database still holding
   everything else.

That keeps the blast radius at the damaged table instead of the whole day.

Some damage is not recoverable this way and should not be: posted journals are
immutable by trigger, and shipment events are append-only. **A "correction" that
requires editing financial history is a reversal, not a restore** (constitution
§23).

---

## 4. Setting it up

```bash
# On the VPS.
mkdir -p /opt/courier-os/backups && chmod 700 /opt/courier-os/backups
apt-get install -y age rclone

# Generate the encryption key. Keep the PRIVATE half off this host.
age-keygen -o /root/age-key.txt
grep 'public key' /root/age-key.txt      # -> AGE_RECIPIENT

# Off-server destination.
rclone config                             # e.g. an S3-compatible bucket
```

`/etc/cron.d/courier-backup`:

```cron
AGE_RECIPIENT=age1...
RCLONE_REMOTE=s3:courier-backups/postgres
HEARTBEAT_URL=https://hc-ping.com/...

0 2 * * * root /opt/courier-os/deploy/scripts/backup.sh >> /var/log/courier-backup.log 2>&1
0 4 1 * * root /opt/courier-os/deploy/scripts/restore-test.sh >> /var/log/courier-restore-test.log 2>&1
```

Three things about that crontab are deliberate:

- **The private key is not on the server.** A stolen VPS yields encrypted
  backups and no way to read them.
- **`HEARTBEAT_URL` is pinged only on success**, so the alert is a *missing*
  ping. A backup system that can only alert when it runs cannot tell you it
  stopped running — which is exactly how backups fail.
- **The restore drill is scheduled, monthly.** Not "when we remember".

---

## 5. What the backup script actually does

`deploy/scripts/backup.sh`, in order, failing fast at each step:

1. `pg_dump -Fc --compress=6` from inside the container, so client and server
   versions always match.
2. **Refuses a dump under 4 KB.** A zero-byte backup that looks like protection
   is worse than none.
3. **`pg_restore --list` on the fresh dump**, and refuses if it lists fewer than
   40 tables. This is the step that separates a backup from a file: a truncated
   archive fails here, today, rather than during a recovery six months from now.
4. SHA-256 over the **plaintext**, stored alongside — so a restore can prove it
   got back what was dumped, not merely that decryption succeeded.
5. Encrypt with age (or gpg), and **warn loudly if neither is configured**.
6. Copy off-server, then `rclone lsf` to confirm it arrived rather than trusting
   an exit code.
7. Prune: 7 days local, 30 days remote.
8. Ping the heartbeat.

`restore.sh` refuses to touch the live database without `--force` *and* the
exact database name, and refuses entirely while the API is running — a writer
during a restore produces a mixture of old and new rows that looks successful
and is not recoverable from.

---

## 6. Verification: what the drill checks

`restore-test.sh` fails, loudly, if:

- the newest backup is older than 30 hours — **catches backups having silently
  stopped**, which a restore drill alone would hide;
- the restore does not exit 0;
- fewer than 100 tables came back;
- `schema_migrations` is behind the application;
- there are no organizations;
- **any posted journal does not balance**;
- any shipment event references a missing shipment;
- restored shipments are under 90% of live.

The financial invariant is the one that matters. A backup can restore perfectly
and still be useless if the data inside it is inconsistent.

---

## 7. Drill results — 9 August 2026

Executed against a database of **150,025 shipments / 167 MB**.

```
dump                4.8 MB in <1s
pg_restore --list   144 TABLE DATA entries
sha256              cc29b4c37741b4b71d013f23138b74f6e0595b76d324542e50e2a13b727401e5
restore             exit 0 in 6s

tables                       144
schema_version                33
shipments                150,025
organizations                  1
unbalanced_posted_journals     0
orphan_events                  0
```

**The invariant check was also proven to work, not just to pass.** A run against
data with no journals returns 0 vacuously, which is worthless as evidence. So a
deliberately unbalanced POSTED journal was inserted inside a transaction:

```
--- verification query WITH one deliberately unbalanced POSTED journal ---
 unbalanced_detected
---------------------
                   1
ROLLBACK
```

The check detects a violation. That is the difference between a test that passes
and a test that means something.

### Two real defects the drill found

Worth recording, because they are the return on actually running it rather than
writing it:

1. **`pg_restore --jobs` is incompatible with stdin.** The first version of
   `restore.sh` piped the archive through `docker compose exec` *and* asked for
   parallel restore. It failed with "parallel restore from standard input is not
   supported" — a restore script that has never been run is a restore script
   that does not work.
2. **The verification query referenced a column that does not exist.**
   `journal_entries.journal_transaction_id` is actually `transaction_id`. The
   query would have errored during a real recovery, at the worst possible time.

Both are fixed. Both were invisible until the scripts were executed.

---

## 8. Residual risks

| Risk | Severity | Status |
|---|---|---|
| 24-hour RPO | **High for COD volume** | Accepted for launch. WAL archiving is the fix and is the first thing to add |
| Backups are on one provider | Medium | Use a different provider from the VPS. A single account suspension should not take both |
| Object storage is not backed up separately | Medium | Relies on provider durability. Enable versioning + a no-delete application credential |
| Drill runs monthly, not weekly | Low | A month of undetected backup failure is survivable given the heartbeat catches a stopped cron within a day |
| No automated failover | Accepted | Single-host by design (constitution §2). HA is a different architecture, not a setting |
| The restore has never been executed on the target hardware | Medium | Measured under OrbStack. **Run the drill once on the real VPS after deployment** and record the number here |

That last row is the one to close first after go-live. Every timing in this
document is measured on a developer machine, and an RTO you have not measured on
the hardware you will actually recover onto is an estimate wearing a number's
clothing.
