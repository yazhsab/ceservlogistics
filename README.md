# Courier Operating System

A production-grade, multi-tenant Courier Operating System for a franchise-based
courier network. The backend owns every business and financial rule; the
frontend consumes it through the OpenAPI contract.

**Release 1 is complete to the published contract boundary**: platform,
frontend foundation, auth/RBAC, network, geography, serviceability/routing,
products, pricing, customers, and shipment booking. See the
[`backend report`](docs/releases/release-1-backend.md),
[`frontend report`](docs/releases/release-1-frontend.md), and
[`frontend/backend gap register`](docs/frontend-backend-gaps/release-1.md).

---

## Quick start

```bash
docker compose up -d          # PostgreSQL, Redis, migrations, API, worker
curl -s localhost:8080/readyz | jq
```

Or run it directly:

```bash
docker compose up -d postgres redis

export DATABASE_URL="postgres://courier:courier@localhost:5432/courier_os?sslmode=disable"
export REDIS_URL="redis://localhost:6379/0"
export JWT_SECRET="$(openssl rand -base64 48)"
export CORS_ALLOWED_ORIGINS="http://127.0.0.1:3000,http://localhost:3000"

go run ./cmd/migrate up
go run ./cmd/migrate demo     # a bookable demo tenant; prints its credentials
go run ./cmd/api              # :8080
go run ./cmd/worker           # background jobs, in another terminal
```

Then book something:

```bash
TOKEN=$(curl -s localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@demo.test","password":"DemoPassw0rd!2026"}' \
  | jq -r .tokens.accessToken)

curl -s localhost:8080/api/v1/pricing/quote \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"originPincode":"560001","destinationPincode":"110001",
       "serviceCode":"EXPRESS","paymentMode":"PREPAID",
       "packages":[{"actualWeightGrams":500,"lengthMm":200,"widthMm":150,"heightMm":100}]}' | jq
```

Run the web application in a second terminal:

```bash
cd web
npm ci
npm run api:generate
npm run dev
```

Vite proxies `/api` to the local API on port 8080. Open
`http://127.0.0.1:3000` and sign in with the demo credentials printed by
`go run ./cmd/migrate demo`.

---

## Documentation

| Document                                                                             | For                                                                           |
| ------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------- |
| [`docs/openapi.yaml`](docs/openapi.yaml)                                             | The machine-readable contract. 86 operations.                                 |
| [`docs/contracts/release-1.md`](docs/contracts/release-1.md)                         | **Start here if you are building the frontend.** Behaviour, not just schemas. |
| [`docs/architecture.md`](docs/architecture.md)                                       | How the system is put together and why.                                       |
| [`docs/deployment.md`](docs/deployment.md)                                           | Production runbook: deploy, backup, restore, disaster scenarios.              |
| [`docs/adr/`](docs/adr/)                                                             | Seven architecture decision records.                                          |
| [`docs/releases/release-1-backend.md`](docs/releases/release-1-backend.md)           | What shipped, with evidence and known risks.                                  |
| [`docs/releases/release-1-frontend.md`](docs/releases/release-1-frontend.md)         | Frontend routes, UX decisions, tests, visual QA, and risks.                   |
| [`docs/frontend-backend-gaps/release-1.md`](docs/frontend-backend-gaps/release-1.md) | Backend contract gaps intentionally not simulated by the UI.                  |
| [`CLAUDE.md`](CLAUDE.md)                                                             | The engineering constitution this code is built to.                           |

---

## Development

```bash
go build ./...            # all three binaries
go vet ./...
gofmt -l . | grep -v internal/dbgen    # must be empty

# Tests. Containers start automatically via testcontainers; point at your own
# database with TEST_DATABASE_URL / TEST_REDIS_URL to skip that and go faster.
go test -race ./...

# Query plans for the hot paths (writes docs/releases/explain-analyze.md)
go test -tags explain ./tests/perf/ -run TestExplainHotQueries -v

# Load profile
k6 run -e BASE_URL=http://localhost:8080 -e EMAIL=admin@demo.test \
       -e PASSWORD='DemoPassw0rd!2026' -e CUSTOMER_ID=cus_... \
       tests/load/courier-load.js
```

Frontend quality gate:

```bash
cd web
npm run api:generate
npm run typecheck
npm run lint
npm test
npm run build
npm run test:e2e
npm audit
```

### Changing the database

1. Add `db/migrations/NNNN_name.up.sql` and a matching `.down.sql`.
2. Add or edit queries in `db/queries/*.sql`.
3. `sqlc generate` — never hand-edit `internal/dbgen`; CI regenerates and fails
   on a diff.
4. `go run ./cmd/migrate up`, then `down --steps 1`, then `up` again. A
   migration that cannot be rolled back and re-applied is not finished.

### Commands

```
migrate up | down --steps N | status | validate
migrate bootstrap        # platform tenant + first SUPER_ADMIN (idempotent)
migrate demo             # a bookable demo tenant (refused in production)
```

---

## Layout

```
cmd/{api,worker,migrate}   three binaries, one module
internal/api               routing and middleware order — the only place that
                           knows the whole module graph
internal/<module>          business modules; each owns its types, service,
                           queries, handlers, validation and authorization
internal/platform          infrastructure with no business knowledge
internal/dbgen             generated by sqlc
db/{migrations,queries}    SQL, embedded into the binaries
deploy/                    Nginx, PostgreSQL config, production compose
tests/{harness,integration,contract,perf,load}
web/                      React/TypeScript/Vite frontend and Playwright tests
```

---

## The four rules

1. **The tenant comes from the token, never the request.** Out-of-scope objects
   return 404, not 403.
2. **Money is integer minor units.** Percentages are basis points. No float ever
   touches an authoritative amount.
3. **History is append-only**, enforced by database triggers rather than by
   convention.
4. **Correctness comes from PostgreSQL** — unique constraints, row locks,
   compare-and-swap, exclusion constraints — never from a process-local lock.
