# Release 4 — Productization (contract)

Release 4 covers M26–M32. All seven modules are implemented and callable.

| Module | Status |
|---|---|
| M26 Notifications | complete — delivery, observers and admin API |
| M27 Customer portal | complete |
| M28 Franchise portal | complete |
| M29 Hub/branch console | complete |
| M30 Operations command centre | complete |
| M31 Reporting engine | complete |
| M32 Partner API and webhooks | complete |

The completion report, with measurements and the defects found along the way, is
[`docs/releases/release-4-backend.md`](../releases/release-4-backend.md).

> **For the frontend team:** this contract is now **final** for Release 4. An
> earlier revision described M27, M28, M29 and M31 as unimplemented and M26 as
> having no HTTP surface; all of that is out of date. Two related corrections
> live in [`release-3.md`](release-3.md): the customer portal and shipment
> notifications it listed as missing now exist, and the settlement adjustment
> workflow it implied but did not provide is documented there in §11A.
>
> Your gap list is answered point by point in
> [`../frontend-backend-gaps/release-4-response.md`](../frontend-backend-gaps/release-4-response.md).

The authoritative machine-readable contract is [`docs/openapi.yaml`](../openapi.yaml).
This file explains the behaviour behind it.

---

## 1. M32 — Partner API

### 1.1 What it is

`/api/v1/partner` is the surface another company's systems call. It is a sibling
of the internal API, not a subset of it:

| | Internal `/api/v1/**` | Partner `/api/v1/partner/**` |
|---|---|---|
| Credential | session access token | API key |
| Header | `Authorization: Bearer <accessToken>` | `Authorization: Bearer <keyId>.<secret>` |
| Authorization | permissions from roles | scopes on the key |
| Rate limit key | session | API key |
| Actor in audit | `USER` + `actor_user_id` | `API_KEY`, no user |
| Actor in shipment events | `USER` | `PARTNER` |

The two credential systems never mix, and both directions are tested:

- an API key presented to `/api/v1/users`, `/api/v1/shipments`, `/api/v1/api-keys`
  or any other internal route gets **401**, even when it holds every scope;
- a session token presented to `/api/v1/partner/**` gets **401**.

Every partner endpoint calls the same service the internal API calls — same
rate card, same routing resolver, same state machine, same tables. There is no
"partner shipment". A second code path would be a second set of business rules
to keep in step, and they would not stay in step.

### 1.2 Scopes

| Scope | Grants |
|---|---|
| `serviceability:read` | `POST /partner/serviceability` |
| `pricing:read` | `POST /partner/quotes` |
| `shipment:create` | `POST /partner/shipments` |
| `shipment:read` | `GET /partner/shipments/{id}` |
| `shipment:cancel` | `POST /partner/shipments/{id}/cancel` |
| `tracking:read` | `GET /partner/tracking/{awb}` |
| `pickup:create` | `POST /partner/pickups` |
| `pickup:read` | `GET /partner/pickups/{id}` |
| `label:read` | `GET /partner/shipments/{id}/label` |
| `pod:read` | `GET /partner/shipments/{id}/pod` |
| `webhook:manage` | the `/partner/webhooks/**` group |

`GET /partner/whoami` needs no scope: it reveals only what the caller already
holds, and it is how an integrator confirms a credential without opening a
support ticket.

A key with no scopes can do nothing. That is the intended default for one
created by mistake.

A missing scope produces **403** with the scope named:

```json
{
  "error": {
    "code": "FORBIDDEN",
    "message": "This API key does not hold the shipment:create scope.",
    "details": {
      "requiredScope": "shipment:create",
      "grantedScopes": ["tracking:read"]
    },
    "requestId": "req_..."
  }
}
```

The catalogue is enforced in three places — the Go constant list, a CHECK
constraint on `api_keys.scopes`, and the `PartnerScope` enum in the OpenAPI
document — and a contract test fails if any of them drifts.

### 1.3 Credential lifecycle

Keys are issued by a **user** holding `apikey.manage` (`ORG_ADMIN`, and
`SUPER_ADMIN`). No API key can issue, revoke or list keys, however many scopes
it holds: that is security administration, not integration.

```
POST /api/v1/api-keys        →  201, secret in the response and nowhere else
POST /api/v1/api-keys/{id}/suspend  →  reversible pause
POST /api/v1/api-keys/{id}/revoke   →  one-way, reason required
GET  /api/v1/api-keys/{id}/usage    →  traffic summary + recent calls
```

- The secret is 32 bytes of cryptographic randomness, stored as a SHA-256
  digest. There is no recovery path; a lost key is replaced.

  SHA-256 rather than a password KDF is deliberate and is the same reasoning the
  platform applies to refresh tokens: the input already carries full entropy, so
  stretching buys nothing, and unlike a password this is verified on *every*
  request. Argon2id at the platform's password settings measured ~85 ms of CPU
  and 64 MiB of transient allocation per call, which capped the partner API at
  roughly 45 req/s. Keys issued before this change carry an Argon2id hash and
  still work — there is no migration.
- `secretHint` (the first characters) is returned in listings so two keys can be
  told apart. The secret and the hash never are.
- Verification is constant time and always pays the hashing cost, including for
  an unknown key id, so an attacker cannot learn which key ids exist by timing.
- **Revocation is irreversible.** `api_keys_guard` refuses to move a revoked key
  to any other status, so a leaked credential cannot be brought back even by
  direct SQL. Suspension exists for the reversible case.
- The error distinguishes revoked / suspended / expired **only after the secret
  verifies**. Before that, every failure is the same `API_KEY_INVALID`.

Error codes: `API_KEY_INVALID`, `API_KEY_REVOKED`, `API_KEY_SUSPENDED`,
`API_KEY_EXPIRED`, `API_KEY_IP_NOT_ALLOWED`, `API_SCOPE_MISSING`,
`API_SCOPE_UNKNOWN`.

### 1.4 Network restriction

`allowedCidrs` is optional. Empty means any source. A non-empty list is
enforced against the resolved client IP, and a client address that cannot be
determined **fails closed** — failing open would defeat the restriction exactly
when it matters.

### 1.5 Rate limiting

Per key, not per address. Two partners behind one NAT do not share a budget, and
one partner spread across a fleet does not get many. Each key may carry its own
`rateLimitPerMinute`; a key without one uses `PARTNER_RATE_LIMIT_PER_MINUTE`
(default 120/minute — lower than the session default, because an integration
that needs more should be given an explicit allowance rather than raising the
floor for everybody).

Every response carries `X-RateLimit-Limit`, `X-RateLimit-Remaining` and
`X-RateLimit-Reset`. A throttled request is **429** with `Retry-After`.

### 1.6 Idempotency

`Idempotency-Key` is **required** on `POST /partner/shipments`, unlike on the
internal booking endpoint where it is optional. An operations clerk who
double-submits sees the duplicate and cancels it; an automated retry loop does
not, so an unkeyed retry after a timeout would quietly produce two parcels and
two invoices.

- A repeat with the same key and the same body returns the original response
  with `Idempotent-Replay: true`.
- The same key with a different body is **409**.
- Keys are scoped `(organization, endpoint, key)`. The partner endpoint records
  itself as `POST /api/v1/partner/shipments`, so a key reused across the
  internal and partner surfaces does not collide.

### 1.7 Tenant isolation

A key belongs to an organization and can see nothing outside it. Cross-tenant
reads of a shipment, its label, its POD and its AWB tracking all return **404**,
not 403, so the endpoint cannot be used to confirm that an identifier exists
somewhere. A cross-tenant cancel is also 404 and changes nothing.

Partner tracking differs from the public `/api/v1/track/{awb}`: the public
endpoint resolves an AWB globally, which is correct there because a consignee
has no tenant. The partner endpoint confirms the AWB belongs to the key's
organization first.

---

## 2. M32 — Webhooks

### 2.1 Delivery model

A business event is queued **inside the transaction that caused it** and
delivered later by a worker. That ordering is the point: the state change and
the intent to tell someone commit together, so a partner's outage costs a retry
rather than a shipment, and a shipment that rolled back never announces itself.

Nothing in a request path ever contacts a partner endpoint.

### 2.2 Event catalogue

```
shipment.booked            shipment.picked_up          shipment.in_transit
shipment.out_for_delivery  shipment.delivered          shipment.delivery_failed
shipment.ndr               shipment.rto_initiated      shipment.rto_delivered
shipment.cancelled         shipment.lost               shipment.damaged
pickup.completed           pod.captured                cod.collected
```

Internal handling steps — bagging, manifesting, hub scans — are deliberately not
published. They would leak network structure and bury the events that matter.

Payload:

```json
{
  "id": "evt_01J...",
  "type": "shipment.delivered",
  "createdAt": "2026-08-09T10:15:00Z",
  "data": {
    "shipmentId": "shp_01J...",
    "awb": "QYN260808000001",
    "status": "DELIVERED",
    "fromStatus": "OUT_FOR_DELIVERY",
    "occurredAt": "2026-08-09T10:14:58Z",
    "reference": "your-order-1234",
    "reasonCode": null
  }
}
```

`id` is the shipment event's public id: stable, unique, and already the identity
of "this thing happened once".

### 2.3 Headers

| Header | Meaning |
|---|---|
| `Webhook-Signature` | `v1=<hex HMAC-SHA256>` |
| `Webhook-Id` | the event id — **dedupe on this** |
| `Webhook-Timestamp` | Unix seconds, part of the signed string |
| `Webhook-Event` | the event type |
| `Webhook-Attempt` | 1-based attempt number |

### 2.4 Verifying a signature

The signed string is `"<timestamp>.<body>"`, not the body alone. Including the
timestamp is what makes a captured request un-replayable by a third party: a
consumer that rejects old timestamps cannot be fed yesterday's valid payload.

```
expected = "v1=" + hex(HMAC_SHA256(signingSecret, timestamp + "." + rawBody))
```

A correct consumer:

1. reads the **raw** body before any JSON parsing — re-serializing changes bytes
   and breaks the MAC;
2. rejects a `Webhook-Timestamp` more than **5 minutes** old;
3. compares in constant time;
4. rejects a prefix it does not recognise, so the scheme can change without
   silently breaking them.

The scheme is versioned (`v1=`) for exactly that reason.

### 2.5 Endpoints

- The URL must be **HTTPS**. Enforced in the service and by a CHECK constraint,
  so a code path that forgot the check could not create one either.
- The signing secret is returned **once**, at creation.
- 20 consecutive failures pause the endpoint automatically. A partner whose
  server has been down for a day should not still be generating retries.
  Resuming clears the counter, so an operator who has fixed their server gets
  the full budget back rather than the one attempt left over from before.

### 2.6 Retry and dead-lettering

- Backoff: 10s, 20s, 40s, 80s, … capped at **30 minutes**. The interval is both
  written to `next_attempt_at` and handed to the job queue, so the value an
  operator reads is the one that actually happens.
- Budget: 6 attempts per delivery by default (`maxAttempts` on the endpoint).
- A 2xx response is success. Anything else, including a transport error, is a
  failure and is retried until the budget is exhausted, then `DEAD_LETTER`.
- Every attempt is recorded in `webhook_attempts` — status code, the first 2 KiB
  of the response, the error, the duration. That history is the evidence in a
  "you never called us" dispute.
- Retries are scheduled as `now() + interval` **in the database**. The
  application clock and the database clock are different clocks; scheduling
  from the application's and comparing against the database's makes a delivery
  that should go out immediately wait for the next poll whenever the two drift.
- If a delivery row is created but its send job is not (a queue failure, which
  is logged and swallowed so it cannot roll back the shipment), a maintenance
  sweep re-enqueues it within 15 minutes.

### 2.7 Duplicate suppression

`webhook_deliveries_event_idx` is unique on `(endpoint_id, event_id)
WHERE replay_of_id IS NULL`. A business event raised twice — a retried
transition, a re-scanned parcel — produces one delivery per subscribed endpoint,
not two.

The claim is `PENDING → SENDING` and nothing else. It deliberately does not
accept a row already `SENDING`: that would let a second worker deliver a webhook
the first is still sending. A worker that dies mid-send is recovered by a
staleness sweep, not by a permissive claim.

**At-least-once, not exactly-once.** A consumer that times out after processing
will be called again. Dedupe on `Webhook-Id`.

### 2.8 Replay

`POST /api/v1/webhooks/deliveries/{id}/replay` (permission `webhook.replay`)
creates a **new** delivery pointing at the original. The original's attempt
history is evidence and must survive; the payload and the event id are copied
byte for byte, so a consumer that already processed the event recognises it
through their own dedupe. A delivery in flight cannot be replayed (409).

---

## 3. M26 — Notifications

### 3.1 Model

Provider-neutral. The domain raises a notification; an adapter sends it. No
provider SDK appears anywhere in the shipment, delivery or finance modules.

Channels: `SMS`, `EMAIL`, `WHATSAPP`, `PUSH`. Until a provider is configured,
every channel has a sender that records what it would have sent — deliberately
not "no sender", because a missing sender suppresses the message and an operator
would rather see the composed text than a row saying nothing happened.

### 3.2 Raising is never a failure path

A notification is raised inside the caller's transaction and never contacts a
provider. When it cannot be sent, it is recorded as `SUPPRESSED` with a reason
rather than raising an error:

| `suppressedReason` | Meaning |
|---|---|
| `NO_ADDRESS` | the recipient has no valid address for the channel |
| `NO_TEMPLATE` | no template for (event, channel, locale) |
| `NO_SENDER` | no adapter registered for the channel |
| `OPTED_OUT` | the recipient's preference says no |

Failing a delivery scan because a text message could not be composed would be
absurd. **Shipment success does not depend on provider availability.**

### 3.3 Events raised by shipment transitions

| Status | Event |
|---|---|
| `BOOKED` | `SHIPMENT_BOOKED` |
| `PICKED_UP` | `PICKUP_COMPLETED` |
| `IN_TRANSIT` | `SHIPMENT_IN_TRANSIT` |
| `OUT_FOR_DELIVERY` | `SHIPMENT_OUT_FOR_DELIVERY` |
| `DELIVERED` | `SHIPMENT_DELIVERED` |
| `NDR`, `DELIVERY_FAILED` | `SHIPMENT_NDR` |
| `RTO_INITIATED` | `SHIPMENT_RTO` |

Only milestones a recipient would want a message about. A parcel being bagged at
the origin branch is a real event and it is on the tracking page; texting
someone about it is noise, and noise is how a customer learns to ignore the
message that mattered.

The dedupe key is the transition, not the status: a parcel re-scanned into
transit at three hubs stays one `IN_TRANSIT` message, while a genuine second
delivery attempt is a genuine second message.

### 3.4 Delivery and retry

- The claim is `PENDING → SENDING` only, for the same reason webhooks use:
  accepting a row already `SENDING` would let a second worker send the same
  message and the customer would be told twice. Recovery is a staleness sweep
  (`ReclaimStuckNotifications`), not a permissive claim.
- 5 attempts; backoff 1m, 2m, 4m, … capped at 30 minutes; then `DEAD_LETTER`.
  As with webhooks, the stored `next_attempt_at` and the queue's schedule are
  the same value.
- A message whose send job was never enqueued is recovered by the same
  15-minute maintenance sweep.
- A `Permanent` provider error (invalid number, rejected address) does not
  retry; a `Transient` one does. An unclassified error defaults to retryable,
  because an unrecognised failure is more often a blip than a permanent refusal.
- Attempts are recorded per try with provider status and error.

---

## 3A. M30 — Operations command centre

### 3A.1 Two kinds of number, and the response says which

`GET /api/v1/command-centre` returns a `consistency` block naming the guarantee
behind each section, because a dashboard that cannot tell an exact figure from a
sampled one will present both as live and somebody will act on the wrong one.

| Section | Guarantee | How |
|---|---|---|
| `period` | **exact** | an incrementally maintained rollup, updated by a trigger in the same transaction as each status change. No staleness window. |
| `live` | **live** | counted at request time over a partial index of in-progress work. |
| `snapshots` | **sampled** | captured on a timer; every point carries its own `capturedAt`. |

### 3A.2 Endpoints

```
GET /api/v1/command-centre            overview: period, live, alerts, movement, money
GET /api/v1/command-centre/trend      daily series
GET /api/v1/command-centre/units      performance by facility
GET /api/v1/command-centre/services   performance by product
GET /api/v1/command-centre/backlog    live custody distribution
GET /api/v1/command-centre/snapshots  captured backlog series
```

All require `command.read`. All are read-only: every action the dashboard
surfaces is taken through the module that owns it.

### 3A.3 Scope

Operating-unit scope applies here as everywhere else, and it applies **whether
or not the client asks**:

- a principal with an unscoped role sees the network;
- a principal scoped to one unit is confined to it even with no `unitId`;
- a principal scoped to several must name one — it is not shown a partial total
  silently;
- naming a unit outside scope is **403**;
- a principal with no unit grants at all is **403**, not an empty dashboard.

Without this the command centre would be the one place in the platform where
operating-unit scope did not apply, and a branch manager would read the whole
network's figures off it.

### 3A.4 Bounds

The window defaults to the last 30 days and is capped at **400 days**. A
backwards window is 422 rather than an empty result. A dashboard is not a
reporting engine; longer periods belong to M31.

League tables and series are bounded (default 20, max 200).

### 3A.5 Rates are basis points

`deliveryRateBasisPoints` and `ndrRateBasisPoints` are integers where 10000 =
100%, for the same reason money is integer minor units: a percentage carried as
a float is a rounding argument waiting to happen, and these sit next to counts
they have to agree with. A zero denominator yields 0, not an error and not NaN.

### 3A.6 Cost

Documented with plans and measurements in
[`docs/releases/explain-analyze-command-centre.md`](../releases/explain-analyze-command-centre.md).
Summary at 200,000 shipments:

| Query | Time | Structure |
|---|---|---|
| Period totals (30 days) | 0.077 ms | rollup, 30 rows read |
| Live backlog by status | 8.55 ms | partial index, 848 kB |
| Backlog by facility | 13.57 ms | partial index |
| SLA breaches | 0.40 ms | partial index, 872 kB |

Two of those started as sequential scans (19 ms and 85 ms) and were fixed with
partial indexes in migration 0030. The indexes cover only work still in play, so
they track the backlog rather than the history: a tenant with two million
delivered shipments and four thousand in progress has the same response time.

### 3A.7 Snapshots and retention

Captured per tenant on the worker's 15-minute sweep. `operational_snapshots` is
append-only for UPDATE unconditionally, and for DELETE within a **90-day floor**
enforced by a database trigger — housekeeping can drop last year, nobody can
drop last week. A caller asking to purge inside the floor gets an error, not a
shortened window.

---

## 3B. M27, M28, M29 — the audience surfaces

Three surfaces, one rule each. The scope is always derived from the
authenticated principal, never from a parameter; a client-supplied account or
facility is a business input that gets checked, never an authorization claim.

| Surface | Base | Scope | Refusal |
|---|---|---|---|
| Customer | `/api/v1/portal/customer` | the `customer_users` link | staff login → 403; another account → 404 |
| Franchise | `/api/v1/portal/franchise` | operating-unit grants → franchise | another franchise → 404; ambiguous scope → 422 |
| Console | `/api/v1/console` | one facility, via `X-Operating-Unit` | out-of-scope facility → 403 |

**The customer portal refuses a staff login.** `Principal.CustomerScope()`
returns nil for staff meaning *unrestricted*; treating that as "everything" here
would hand one screen the tenant's whole book.

**The franchise listing states its relationship.** `role` defaults to `ORIGIN` —
what the franchise booked, which is what the summary beside it counts.
`DESTINATION` is its inbound workload. Every row echoes its own role. Before
this, the summary said 0 while the list showed 3, which is worse than either
being absent.

**The console is a payload budget.** A lookup returns ten fields against the
shipment's forty, and `amountDueMinor` is present only for COD so a prepaid
parcel cannot be misread. A load-test check fails above 800 bytes.

---

## 3C. M31 — Reporting

`POST /api/v1/reports` returns **202** with a poll URL. Generation happens on a
worker that reads by keyset cursor, formats each 2,000-row chunk and writes it
straight into an upload stream: peak memory is one chunk, not one report.

```
POST /api/v1/reports              → 202, a QUEUED run
GET  /api/v1/reports/{id}         → status, rowCount, byteSize, expiresAt
GET  /api/v1/reports/{id}/download → 302 to a signed URL, or a stream
POST /api/v1/reports/{id}/cancel  → while QUEUED or RUNNING
```

- Period capped at **400 days**; a longer export must be split.
- Commission, settlement, revenue and COD-aging need `report.finance` on top of
  `report.run`. `GET /reports/types` says which, so a UI can grey them out.
- A finished report expires after **24 hours**, and the sweep deletes the object
  as well as the record — expiring one and leaving the other would leave
  exported customer data in the bucket indefinitely.
- The claim is `QUEUED → RUNNING` only: two workers streaming into one object
  key would both write, and the loser would leave a truncated file behind a
  COMPLETED record.

---

## 4. Deployment notes

New configuration:

| Variable | Default | Meaning |
|---|---|---|
| `PARTNER_RATE_LIMIT_PER_MINUTE` | `120` | fallback rate limit for a key without its own |
| `PUBLIC_TRACKING_URL` | `https://track.example.com/` | goes into notification bodies; must be the page a recipient can open, not the API base |
| `WORKER_QUEUES` | `default,import,notifications,webhooks,reports` | the three new queues must be listed or nothing is delivered or generated |

Maintenance (runs every 15 minutes in `cmd/worker`):

- `ExpireAPIKeys` — an expired key stops working whether or not anyone
  remembered to revoke it;
- `ReclaimStuckNotifications` / `ReclaimStuckDeliveries` — 15-minute staleness
  window, well past any send timeout.

---

## 5. Known limitations

- **No real provider adapters.** Every channel is wired to a logging sender.
  Adding a provider means implementing `notification.Sender` and registering it;
  nothing else changes.
- **`pod.captured` and `cod.collected` are in the catalogue but not yet
  emitted.** Only shipment status transitions raise events today. A partner can
  subscribe to them and will receive nothing until the POD and COD modules call
  `Emit`.
- **Webhook payload filters are stored but not applied.** `webhook_subscriptions.filter`
  exists in the schema; every subscriber to an event receives every instance of it.
- **No per-endpoint circuit breaker beyond the 20-failure pause.** A slow
  endpoint consumes a worker slot for its timeout on every attempt.
