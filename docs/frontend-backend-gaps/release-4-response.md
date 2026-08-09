# Release 4 gaps — backend response

Date: 9 August 2026. Replying to
[`release-4.md`](release-4.md) point by point.

Eight gaps were raised. **Six are closed**, one is partly closed, and one is
open and belongs to you rather than to us. The contract at
[`docs/contracts/release-4.md`](../contracts/release-4.md) is now final for
Release 4 — the revision you read described M27–M31 as unimplemented and that is
out of date.

The gap list was accurate throughout, and two of its items — the session subject
binding and the missing page envelope — were things the backend would not have
noticed on its own. Both are fixed.

---

## 1. Notification administration transport — **closed**

`/api/v1/notifications`, 13 operations:

| Need | Endpoint |
|---|---|
| template list / detail / create / update | `GET,POST /notifications/templates`, `GET,PATCH /templates/{id}` |
| server-rendered preview | `POST /notifications/templates/{id}/preview` |
| allowed variable catalogue | `variables` on every template — **derived from the body**, not declared |
| per-event variable validation | `missingVariables` in the preview response |
| delivery register | `GET /notifications` (keyset, filterable by status, channel, event) |
| per-attempt register | `attempts[]` on `GET /notifications/{id}`, with provider status and error |
| error summary | `GET /notifications/health` |
| retry | `POST /notifications/{id}/retry` |
| channel/provider administration | `GET /notifications/channels` — see the caveat below |

Two notes on the parts that are not shaped the way you asked:

**Variable grammar.** You said you would not invent one, and you were right not
to. It is `{{name}}`, and the catalogue is computed from the template body
rather than supplied alongside it — a declared list that disagrees with the text
is a lie the first recipient discovers. `POST /templates/{id}/preview` returns
`missingVariables` naming every placeholder your sample data did not fill.

**Channel administration does not exist and will not.** A channel is a compiled
adapter, not a configurable record; there is nothing to administer. What you can
read is `GET /notifications/channels`, which says which channels have an adapter
registered. That matters more than it sounds: a channel with no adapter
*suppresses every message on it*, and that is the single most common cause of
"notifications are not working". Surface it.

**Not delivered:** template versioning. The schema has no version column and
adding one is a schema change, not a transport one.

---

## 2. Customer portal identity and service surface — **closed**

You identified the important half of this, and it was still missing after the
portal endpoints were built: *"the authenticated user schema does not bind a
session to a customer subject."*

`GET /api/v1/auth/me` now returns a `portal` object:

```json
{
  "portal": {
    "isCustomerUser": true,
    "customers": [{ "id": "cus_01J…", "code": "ACME", "name": "Acme Ltd" }],
    "franchise": null
  }
}
```

`isCustomerUser` is **not** derived from permissions. A staff login is `false`
even when its role carries `portal.customer`, and the portal refuses it — the
underlying reason is that an unrestricted customer scope means "no restriction"
for staff, which if treated as "everything" would hand one screen the tenant's
whole book. That refusal is tested.

The service surface, under `/api/v1/portal/customer`:

| Need | Endpoint |
|---|---|
| dashboard aggregate | `GET /summary` — real totals over the account, not a first-page count |
| own shipments | `GET /shipments` (keyset) |
| own shipment detail | `GET /shipments/{id}` — another account's is **404** |
| tracking | `GET /shipments/{id}/track` — resolved through the shipment, so the scope applies |
| own invoices | `GET /invoices` |
| accounts and credit | `GET /accounts` — `creditAvailableMinor` is computed, not stored |

The DTOs are portal projections, not the operations ones: no internal facility
codes, no route snapshot. Your concern about reusing internal routes was
correct.

**Not delivered:** portal booking, estimate, own-address management and own-POD.
The partner API covers booking and POD for programmatic callers; a customer-
facing booking flow is a product decision nobody has made yet. Say the word and
it is a small addition on top of what exists.

---

## 3. Franchise portal identity and service surface — **closed**

Same binding, same place:

```json
{ "portal": { "franchise": { "id": "frn_01J…", "code": "BLR01",
                             "name": "Bengaluru South", "operatingUnitCode": "BLR-001" } } }
```

`franchise` is null for head office, **and null when the login's units span two
franchises** — that is a configuration error, and picking whichever sorted first
would hide it. The franchise portal reports it as a 422 asking you to name one.

| Need | Endpoint |
|---|---|
| dashboard aggregate | `GET /portal/franchise/summary` — volume, commission earned/settled/outstanding, COD in custody |
| own shipments | `GET /portal/franchise/shipments` |
| own settlements | `GET /portal/franchise/settlements` |

One thing worth reading before you build the shipment list. It takes a `role`
parameter — `ORIGIN` (default), `DESTINATION` or `ANY` — and every row echoes
its own. This exists because the first version did not have it: the summary
counted what the franchise *booked* while the list counted anything its branch
*touched*, so the screen showed "0 booked" above three shipments. Defaulting to
ORIGIN makes the list agree with the summary above it.

**Not delivered:** franchise-scoped booking, pickup, bag, manifest and delivery
operations. Those are operational actions, and a franchise operator performs
them through the ordinary operational API with their operating-unit scope
already applied — there is no second authorization model to duplicate. If you
want them re-exposed under `/portal/franchise` for routing convenience, that is
a thin wrapper and we can add it; it is not a missing capability.

---

## 4. Hub/branch console transport — **closed**

You asked for a bootstrap "only if the product needs server-selected current
facility, terminal policies, or fewer round trips". Fewer round trips is the
real one — this runs on a handheld over a branch's mobile connection.

`/api/v1/console`, facility-bound by the `X-Operating-Unit` header:

- `GET /summary` — one call for the whole strip: in custody, out for delivery,
  exceptions, held, overdue, open bags, inbound expected.
- `GET /lookup/{barcode}` — AWB or piece barcode, one parcel.
- `GET /queue`, `GET /bags`, `GET /inbound`.

**The payload budget is the feature.** A lookup returns ten fields where the
internal shipment API returns forty; `amountDueMinor` is present *only* for COD,
so a prepaid parcel cannot be misread as owing money. There is a load-test check
that fails if the response exceeds 800 bytes. Please do not paper over it by
fetching the full shipment alongside.

The facility is not server-selected: it comes from the header, is checked
against the caller's scope, and a facility outside it is 403.

---

## 5. Operations command centre — **closed**

`/api/v1/command-centre`, six operations. Filters: `from`, `to`, `unitId`,
`serviceCode`, `unitType`.

The thing to build the UI around is the `consistency` block on every response:

```json
{ "consistency": {
    "periodTotals": "exact; maintained in the same transaction as each status change",
    "liveBacklog":  "live; counted at request time",
    "snapshots":    "captured periodically; every point carries its own capturedAt" } }
```

Three different guarantees on one screen. **Label them.** A dashboard that
presents a sampled series as live is how somebody acts on the wrong number.

Actionable queues: `alerts.ndrByReason`, `alerts.slaBreached`,
`alerts.openExceptions`, `money.codAgedOver48h`, and `GET /backlog` per facility.
Drill-down is by the ordinary filtered list endpoints — the command centre is
read-only by design, and every action it surfaces is taken through the module
that owns it.

Note: operating-unit scope applies **whether or not you send `unitId`**. A
branch manager sees their branch; a login covering several scoped units must
name one rather than being shown a partial total silently.

---

## 6. Reporting and export lifecycle — **mostly closed**

`/api/v1/reports`. `GET /reports/types` is the catalogue and says which types
need `report.finance`, so you can grey them out rather than letting somebody
discover it by refusal.

Lifecycle — note the status names differ from your proposal:

| You asked | We have |
|---|---|
| `PENDING` | `QUEUED` |
| `RUNNING` | `RUNNING` |
| `READY` | `COMPLETED` |
| `FAILED` | `FAILED` |
| — | `EXPIRED`, `CANCELLED` |

`POST /reports` returns **202** with a `pollUrl`. `GET /reports/{id}` carries
`rowCount`, `byteSize`, `durationMs`, `errorMessage` and `expiresAt`;
`downloadUrl` appears only once COMPLETED. `GET /reports/{id}/download` redirects
to a signed URL or streams, always `Cache-Control: no-store`.

Two things to design around:

- **`EXPIRED` is real.** A finished report is deleted 24 hours after generation,
  file and record together. Your download screen needs a state for "this ran,
  and it is gone".
- **No progress percentage.** The engine streams by cursor and does not know the
  total row count in advance — computing it would mean a second full scan of the
  data it is about to export. `rowCount` is populated on completion. An
  indeterminate spinner is the honest UI.

**Not delivered:** paginated preview. A preview means running the query twice,
once for the screen and once for the file, and they can disagree. If you need
it, the better shape is a `limit` parameter producing a small *real* report you
can download — tell us and we will add it.

---

## 7. Integration register pagination metadata — **closed**

You were right, and this was a plain omission. `GET /api/v1/api-keys` and
`GET /api/v1/webhooks/endpoints` now return:

```json
{ "data": [ … ],
  "pagination": { "limit": 50, "offset": 0, "totalItems": 137, "hasMore": true } }
```

Computed with a window function, so it costs no second query. Stop requesting
200 and guessing.

---

## 8. Playwright workflows blocked by transport — **open, and yours now**

Every route in items 1–7 exists and is documented. What is *not* provided is
seeded portal data, and that is deliberate: `migrate demo` builds a tenant with
an admin, a customer, a service and a lane, but no customer-portal login, no
franchise, and no partner key.

Rather than a fixture endpoint — which would be production code that exists only
for tests — here is the setup the load profile uses, which is the same shape you
need:

```sql
-- A franchise on the origin branch
INSERT INTO franchises (public_id, organization_id, code, name, operating_unit_id,
                        owner_name, owner_phone, status)
SELECT gen_seed_public_id('frn'), o.id, 'LOADFRN', 'Load Franchise', u.id,
       'Owner', '9000000009', 'ACTIVE'
FROM organizations o JOIN operating_units u
  ON u.organization_id = o.id AND u.code = 'BLR-001'
WHERE o.code = 'DEMO';

-- Link a CUSTOMER-role user to a customer account. The link is what makes them
-- a portal user; the role alone leaves a staff account with a misleading name.
INSERT INTO customer_users (organization_id, customer_id, user_id, portal_role, status)
SELECT c.organization_id, c.id, u.id, 'OWNER', 'ACTIVE'
FROM customers c JOIN users u ON u.organization_id = c.organization_id
WHERE u.email = 'portal@demo.test';
```

The franchise owner is an ordinary user created through `POST /api/v1/users`
with role `FRANCHISE_OWNER` and the branch's operating unit. A partner key comes
from `POST /api/v1/api-keys`.

**If you would rather have this as a command**, say so and we will extend
`migrate demo` to seed a portal login, a franchise with an owner, and a partner
key. That is a five-line change and it belongs in the demo seed, not in the API.

---

## Summary

| # | Gap | State |
|---|---|---|
| 1 | Notification administration | closed (no template versioning) |
| 2 | Customer portal identity and surface | closed (no portal booking) |
| 3 | Franchise portal identity and surface | closed (operational actions via the ordinary API) |
| 4 | Console transport | closed |
| 5 | Command centre | closed |
| 6 | Reporting lifecycle | closed (no preview, no progress %) |
| 7 | Pagination metadata | closed |
| 8 | Portal fixtures | open — SQL above, or ask for a `migrate demo` extension |
