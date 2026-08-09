# Deploying to a Hostinger VPS

Written from an actual deployment to a KVM 4 (4 vCPU / 16 GB / 200 GB, Ubuntu
24.04) that was **already serving another production site**. Every step below
was executed; §10 lists the things that went wrong, because those are the parts
worth reading twice.

If your VPS is empty, §3 is simpler than described and you can run the stack's
own nginx. If something else is already on it, follow this exactly.

---

## 1. Before you start

| | |
|---|---|
| Server | Hostinger KVM 4, Ubuntu 24.04 LTS |
| Access | `ssh root@YOUR_IP` |
| Domain | An A record you control, pointing at the VPS |
| Storage | S3-compatible, or MinIO on the box (§6) |

**Check what is already running before anything else.** This single command
decides whether §3 applies to you:

```bash
ss -tlnp | grep LISTEN
docker ps 2>/dev/null || echo "docker not installed"
systemctl is-active nginx apache2
```

If nginx, Apache, or anything else already owns `:80` and `:443`, you are in
the shared-host case. Do not stop it. §3 explains why.

---

## 2. SSH keys first

Password auth over SSH rate-limits aggressively, and a deployment makes many
connections. Ten minutes of "Permission denied, please try again" is not a
mystery worth solving twice:

```bash
# On your machine.
ssh-keygen -t ed25519 -f ~/.ssh/vps_deploy -C "courier-deploy"
ssh-copy-id -i ~/.ssh/vps_deploy.pub root@YOUR_IP
ssh -i ~/.ssh/vps_deploy root@YOUR_IP 'echo ok'
```

**Rotate the root password** Hostinger emailed you, and consider disabling
password auth entirely once the key works.

---

## 3. The shared-host decision

The production compose file ships an nginx bound to `:80` and `:443`. If the
host already runs one, that container cannot bind, and the tempting fix —
stopping the host nginx — takes the other site down.

**Use the host nginx as the single ingress.** `deploy/docker-compose.vps.yml`
is the overlay that arranges this:

- No nginx container.
- The API published on `127.0.0.1:8080` and `:8081` only, never on a public
  interface. `ufw` already allows 80/443; there is no second front door.
- Two named API services rather than `replicas: 2`, because compose cannot give
  each replica its own host port, and one instance means a restart drops the
  listener.
- Its own PostgreSQL 16 and Redis on the compose network, unpublished. Sharing
  the host's PostgreSQL would mean two applications in one cluster, one set of
  tuning and one restart blast radius — and probably a version this codebase
  has never been tested against.

`deploy/postgres/postgresql.vps.conf` drops `shared_buffers` to 1536MB and
`effective_cache_size` to 3GB, because the production file assumes it owns the
machine and on a shared host it does not.

---

## 4. Install Docker

```bash
ssh -i ~/.ssh/vps_deploy root@YOUR_IP
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y ca-certificates curl gnupg
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] \
  https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable" \
  > /etc/apt/sources.list.d/docker.list
apt-get update -qq
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
docker --version && docker compose version
```

Additive: it reconfigures nothing that is already running. Verify the other
site still answers before continuing.

---

## 5. Get the code

**Clone on the server. Do not copy from a Mac.**

```bash
git clone https://github.com/yazhsab/ceservlogistics.git /opt/courier-os
cd /opt/courier-os
```

If you must transfer from macOS, `COPYFILE_DISABLE=1 tar …` — see §10.1 for
what happens otherwise.

---

## 6. Configuration

```bash
cd /opt/courier-os
umask 077
PGPW=$(openssl rand -base64 30 | tr -d '/+=' | head -c 32)
MINIOKEY=$(openssl rand -hex 12)
MINIOSEC=$(openssl rand -base64 32 | tr -d '/+=' | head -c 36)

cat > deploy/.env <<EOF
IMAGE=courier-os:latest
VERSION=5.0.0
GIT_COMMIT=$(git rev-parse --short HEAD)

POSTGRES_USER=courier
POSTGRES_PASSWORD=${PGPW}
POSTGRES_DB=courier_os
DATABASE_URL=postgres://courier:${PGPW}@postgres:5432/courier_os?sslmode=disable

JWT_SECRET=$(openssl rand -base64 48 | tr -d '\n')
CORS_ALLOWED_ORIGINS=https://admin.yourdomain.com

MARKET_COUNTRY=NG
MARKET_CURRENCY=NGN
MARKET_TIMEZONE=Africa/Lagos

STORAGE_DRIVER=s3
STORAGE_ENDPOINT=http://minio:9000
STORAGE_BUCKET=courier-pod
STORAGE_ACCESS_KEY=${MINIOKEY}
STORAGE_SECRET_KEY=${MINIOSEC}
STORAGE_REGION=us-east-1

PUBLIC_TRACKING_URL=https://admin.yourdomain.com/track
EOF
chmod 600 deploy/.env
```

**Object storage is not optional.** Production refuses the filesystem driver,
deliberately: POD photos written into a container layer vanish on the next
deploy, and a disputed delivery six months later is argued from those files.
The overlay includes MinIO, which stores them on the VPS disk. External storage
(Cloudflare R2, Backblaze B2) is better — it survives losing the server — and
is a change of four variables.

---

## 7. Build and start

```bash
cd /opt/courier-os
docker build -t courier-os:latest \
  --build-arg VERSION=5.0.0 \
  --build-arg GIT_COMMIT=$(git rev-parse --short HEAD) \
  --build-arg BUILT_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ) .

docker compose -f deploy/docker-compose.prod.yml \
               -f deploy/docker-compose.vps.yml \
               --env-file deploy/.env up -d
```

That command is long enough to be worth aliasing:

```bash
echo "alias dcc='docker compose -f /opt/courier-os/deploy/docker-compose.prod.yml -f /opt/courier-os/deploy/docker-compose.vps.yml --env-file /opt/courier-os/deploy/.env'" >> ~/.bashrc
source ~/.bashrc
```

Check it:

```bash
dcc ps                       # all services healthy
curl -s localhost:8080/readyz
curl -s localhost:8081/readyz
```

Migrations run automatically as a gated step: the API will not start until they
have completed successfully.

---

## 8. Nginx and TLS

`/etc/nginx/sites-available/courier-os`:

```nginx
upstream courier_api {
    least_conn;
    server 127.0.0.1:8080 max_fails=2 fail_timeout=5s;
    server 127.0.0.1:8081 max_fails=2 fail_timeout=5s;
    keepalive 32;
}

server {
    listen 80;
    listen [::]:80;
    server_name admin.yourdomain.com api.yourdomain.com;

    location /.well-known/acme-challenge/ { root /var/www/html; }

    location / {
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Request-Id      $request_id;
        proxy_connect_timeout 5s;
        proxy_read_timeout    60s;
        proxy_pass http://courier_api;
    }
}
```

**Do not mark this `default_server`.** It must match only its own
`server_name`, so it cannot capture traffic for anything else on the host.

```bash
ln -sf /etc/nginx/sites-available/courier-os /etc/nginx/sites-enabled/
nginx -t && systemctl reload nginx     # never reload on a failed test
```

`$request_id` is deliberate: nginx mints one, the application adopts it, and
the same id appears in the nginx access log, the application log and the error
envelope returned to the client. One value threads all three.

### The certificate

**Verify DNS against the authoritative nameserver, not a public resolver:**

```bash
dig +short NS yourdomain.com
dig +short admin.yourdomain.com @ns1.your-authoritative-ns.com
```

If the authoritative server returns nothing, the record is not in the zone —
that is not propagation, and waiting will not fix it. See §10.5.

Once it resolves:

```bash
certbot --nginx -d admin.yourdomain.com -d api.yourdomain.com
systemctl reload nginx
curl -sI https://admin.yourdomain.com/livez | head -1
```

Certbot rewrites the block to add TLS and an HTTP→HTTPS redirect, and installs
a renewal timer.

---

## 9. Bootstrap and backups

```bash
dcc run --rm --entrypoint /app/migrate \
  -e BOOTSTRAP_ADMIN_EMAIL=admin@yourdomain.com \
  -e BOOTSTRAP_ADMIN_PASSWORD='choose-a-strong-one' \
  -e BOOTSTRAP_ORG_CODE=YOURORG \
  -e BOOTSTRAP_ORG_NAME='Your Company' \
  migrate bootstrap
```

The account is created with a forced password change on first login. `migrate
demo` exists for local work and **refuses to run in production**, which is
correct — nobody wants demo shipments in a real ledger.

`/etc/cron.d/courier-os`:

```cron
SHELL=/bin/bash
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
COMPOSE_FILE=/opt/courier-os/deploy/docker-compose.prod.yml
BACKUP_DIR=/opt/courier-os/deploy/backups
POSTGRES_USER=courier
POSTGRES_DB=courier_os
AGE_RECIPIENT=age1...            # set this
RCLONE_REMOTE=s3:your-bucket     # and this

0 2 * * * root /opt/courier-os/deploy/scripts/backup.sh >> /var/log/courier-backup.log 2>&1
0 4 1 * * root /opt/courier-os/deploy/scripts/restore-test.sh >> /var/log/courier-restore-test.log 2>&1
```

Run it once by hand rather than trusting cron:

```bash
/opt/courier-os/deploy/scripts/backup.sh
```

**Without `AGE_RECIPIENT` and `RCLONE_REMOTE` the backup is unencrypted and on
the same disk as the database.** That protects you from a dropped table and
from nothing that loses the server. The script warns loudly about both.

**MinIO needs its own backup.** `backup.sh` covers PostgreSQL only. If you use
on-box MinIO, add the `minio-data` volume to your off-server copy — that is
where the delivery evidence lives.

---

## 10. What actually went wrong

Five traps, all hit on a real deployment.

### 10.1 macOS `tar` breaks the migrations

Copying the tree from a Mac carried **557 AppleDouble `._` files**. The
migrator refused to start:

```
migrate: migration "._0001_platform_functions.down.sql" has an invalid version prefix
```

That refusal is correct — silently skipping unrecognised files in a migrations
directory would be far worse. Deleting them on the server was not enough,
because migrations are baked into the image and it needed a rebuild.

**Clone on the server.** If you must copy: `COPYFILE_DISABLE=1 tar -czf …`.

### 10.2 Nginx would not start at all

`nginx.conf` includes `/etc/nginx/proxy_params_courier`, and the compose file
did not mount it:

```
[emerg] open() "/etc/nginx/proxy_params_courier" failed (2: No such file or directory)
```

Nginx crash-looped — the whole ingress, not one route. Fixed in the compose
file. It had never been caught because the stack had been built and tested as
an image, never started as a stack.

### 10.3 Two replicas, one port

The base file asks for `replicas: 2`. With a published host port:

```
Bind for 127.0.0.1:8080 failed: port is already allocated
```

The overlay sets `replicas: 1` on `api` and adds `api-b` as a second service on
`:8081`. The host nginx balances them.

### 10.4 The worker looked broken and was not

The worker inherits the image's `HEALTHCHECK`, which probes `:8080/livez` — the
API. The worker serves no HTTP there, so a healthy worker reported `unhealthy`
forever.

Chasing that turned up something worse: **every `courier_jobs_*` metric was
invisible**, because those are recorded by whichever process runs the job and
the worker had no scrape endpoint. It now serves `/livez`, `/readyz` and
`/metrics` on `WORKER_HEALTH_ADDR` (default `:8090`).

### 10.5 "DNS is still propagating" usually is not

An A record added in the wrong panel looks exactly like slow propagation. The
distinguishing check:

```bash
dig +short SOA yourdomain.com @ns1.your-authoritative-ns.com
```

The serial encodes the last edit date — `2026062401` is 24 June. If it predates
your change, the record never reached this zone. Propagation delays affect
resolvers; the authoritative server has the record immediately.

A domain can have its DNS served somewhere other than where you bought it or
where the VPS lives. `ftp` and `cpanel` records pointing at a different IP are
a strong hint the zone is managed by a shared-hosting panel.

---

## 11. Day two

### Redeploy

```bash
cd /opt/courier-os
/opt/courier-os/deploy/scripts/backup.sh        # before anything
git pull --ff-only
docker build -t courier-os:latest \
  --build-arg VERSION=$(git describe --tags --always) \
  --build-arg GIT_COMMIT=$(git rev-parse --short HEAD) \
  --build-arg BUILT_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ) .
dcc run --rm migrate up
dcc up -d --force-recreate api
sleep 10 && curl -sf localhost:8080/readyz && dcc up -d --force-recreate api-b
```

Recreating one instance at a time is what makes it zero-downtime — measured at
120 consecutive successful probes through a rolling restart.

### Rollback

```bash
curl -s localhost:9090/metrics | grep courier_app_build_info   # what is running
docker tag courier-os:latest courier-os:rollback-$(date +%F)
IMAGE=courier-os:previous dcc up -d api api-b
```

**If the release contained a migration, stop and think.** Reverting the image
does not revert the schema. Additive migrations are safe to roll back under;
destructive ones are not. `docs/DISASTER_RECOVERY.md` §3.6 covers it, and the
pre-migration backup exists for exactly this.

### Health

```bash
dcc ps
curl -s localhost:8080/readyz
curl -s localhost:9090/metrics | grep -E 'courier_app_(build_info|dependency_up)'
dcc exec worker wget -qO- http://127.0.0.1:8090/metrics | grep courier_jobs_queue_depth
dcc logs --tail=100 api
df -h / && free -m
```

`docs/RUNBOOK.md` is the one to open when something is wrong.

---

## 12. Capacity on a KVM 4

Measured on this stack, though on faster hardware than a KVM 4 — assume 3–5×
slower and the margins still hold:

| | Peak | Limit |
|---|---|---|
| API RSS | 185 MB | 640 MB each |
| CPU | 96% of one core at 241 req/s | 4 cores |
| **DB pool** | **4 acquired** | **20 — 80% idle** |
| Slow queries | 0 | — |
| Redis | 65 MB | 384 MB |

Two things worth knowing: **CPU binds before connections do**, and the pool is
sized far more generously than it needs to be — 10–12 would do, and would give
PostgreSQL back some memory.

Total footprint alongside another small site: roughly 7.5 GB of 15 GB.
