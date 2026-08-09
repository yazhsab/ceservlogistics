# Release 1 — Frontend Contract

**Audience:** the engineer building the frontend.
**Authority:** `docs/openapi.yaml` is the machine-readable contract. This
document explains the *behaviour* behind it — the rules you cannot infer from
schemas alone. You should never need to read Go source to build a screen.

---

## 1. Terminology

| Term | What it means |
|---|---|
| **Organization** | A tenant: one courier company on the platform. Everything you see belongs to exactly one. |
| **Operating unit** | A facility. Hubs and branches are the *same* entity, distinguished by `unitType`. |
| **Hub** | `REGIONAL_HUB`, `TRANSIT_HUB`, `DELIVERY_HUB`. Line haul runs hub to hub. |
| **Branch** | `COMPANY_BRANCH`, `FRANCHISE_BRANCH`. Books, picks up and delivers. Its `parent` is its hub. |
| **Franchise** | A commercial agreement operating one `FRANCHISE_BRANCH`. |
| **Courier service** | A product: "Express Air", "Surface Economy". All behaviour is configuration. |
| **Zone** | A tenant-defined pricing region. Postcodes are mapped into zones. |
| **Service area** | A declaration that a unit serves a postcode for pickup, delivery or both. |
| **Postcode** | The routing key for everything: serviceability, zones, pricing, service areas. Called `pincode` on the wire, for compatibility. Six digits, non-zero lead, in both Nigeria and India. |
| **Area** | A named place inside a postcode — "Ikeja GRA", "Wuse II". What a sender actually knows. Search these to find the postcode. |
| **LGA** | Nigeria's Local Government Area, the level between state and city. The API calls it `district`; ask the server what to *label* it. |
| **Route** | A configured path between two facilities, made of ordered legs. |
| **AWB** | The human-readable shipment number, e.g. `CSV260808000001`. Print it, scan it, let users search it. |
| **Public ID** | The opaque identifier used by the API, e.g. `shp_01KZG...`. Use it in URLs; never show it to end users. |
| **Snapshot** | An immutable copy taken at booking (address, price, route). It never changes, even if the source data does. |

---

## 2. Authentication

### The flow

1. `POST /api/v1/auth/login` → `{ tokens, user }`.
2. Store `accessToken` in memory; send it as `Authorization: Bearer <token>`.
3. It expires after **15 minutes**. On a `401` with code `TOKEN_EXPIRED`, call
   `POST /api/v1/auth/refresh` with the refresh token, then retry the original
   request once.
4. The refresh call returns a **new refresh token**. You must replace the stored
   one. The old one is dead the instant the new one is issued.

### Rules you must respect

- **Never retry a refresh with a token you already exchanged.** The server
  treats a replayed refresh token as a leak and revokes every session from that
  login. The user is signed out everywhere. If two tabs refresh concurrently,
  serialise it: a single refresh promise shared across tabs, or a lock.
- On `TOKEN_REVOKED`, clear all state and send the user to sign-in. Do not retry.
- `user.mustChangePassword === true` means block the app and force a password
  change first.
- Drive menus and buttons from `user.permissions`. The server enforces them
  regardless — a hidden button is convenience, not security.

### Failure codes

| Code | HTTP | What to show |
|---|---|---|
| `INVALID_CREDENTIALS` | 401 | "Email or password is incorrect." Never say which. |
| `ACCOUNT_LOCKED` | 401 | "Too many attempts. Try again in a few minutes." |
| `ACCOUNT_INACTIVE` | 401 | "Contact your administrator." |
| `TOKEN_EXPIRED` | 401 | Refresh silently; do not show anything. |
| `TOKEN_INVALID` / `TOKEN_REVOKED` | 401 | Sign out. |

Login and password reset are rate limited. On `429`, honour `Retry-After`.

---

## 3. The error envelope

Every failure, without exception:

```json
{
  "error": {
    "code": "SHIPMENT_NOT_SERVICEABLE",
    "message": "No branch currently serves pickups at the origin PIN code.",
    "details": { "reasonCode": "ORIGIN_NOT_SERVICEABLE", "originPincode": "560001" },
    "requestId": "req_01KZG..."
  }
}
```

- **Switch on `error.code`**, not on the HTTP status alone.
- `error.message` is written for end users. Show it.
- `error.details` shape depends on the code — see the tables below.
- `requestId` also appears in the `X-Request-Id` response header. Show it on
  error screens; support will ask for it.
- Stack traces and database messages are never returned.

### Validation failures (`422 VALIDATION_FAILED`)

```json
{ "error": { "code": "VALIDATION_FAILED", "message": "One or more fields failed validation.",
  "details": { "fields": [
    { "field": "sender.pincode", "message": "Must be a 6-digit PIN code not starting with 0." },
    { "field": "packages[0].actualWeightGrams", "message": "Must be between 1 and 10000000." }
  ] } } }
```

`field` uses dotted paths with array indices, so you can map each message onto
its input directly.

**Unknown fields are rejected.** Sending a property the endpoint does not define
returns 422 naming it. Do not echo back a whole GET response as a PATCH body —
send only the fields you are changing.

---

## 4. Pagination

**Cursor** (shipments, audit events) — for high-volume, time-ordered data:

```
GET /api/v1/shipments?limit=25
→ { "data": [...], "pagination": { "limit": 25, "hasMore": true, "nextCursor": "eyJ0Ijoi..." } }
GET /api/v1/shipments?limit=25&cursor=eyJ0Ijoi...
```

Cursors are opaque; never parse or construct one. There is no page number and
no total — build "load more" or infinite scroll, not a numbered pager. Depth
costs nothing: page 400 is as fast as page 1.

**Offset** (configuration lists) — for small, browsable data:

```
GET /api/v1/customers?page=2&limit=50
→ { "data": [...], "pagination": { "page": 2, "pageSize": 50, "totalItems": 340, "totalPages": 7, "hasMore": true } }
```

`limit` is capped at 100 everywhere.

---

## 5. Money and weight

- **Every amount is an integer in minor units.** `totalAmountMinor: 10488` is
  ₹104.88. Divide by 100 for display only, using the `currency` field beside it.
- **Never do arithmetic on money in the browser.** Do not sum line items, do not
  compute tax, do not recompute a total. The server's figure is authoritative
  and its rounding rules (half-up, once per line item) will not match yours.
- **Percentages are basis points.** `percentageBp: 1850` is 18.50%. Divide by
  100 to display.
- **Weights are grams**, dimensions are **millimetres**.

Chargeable weight is derived, not supplied:

```
volumetric(g) = length(mm) x width(mm) x height(mm) / volumetricDivisor
chargeable    = max(actual, volumetric, lane minimum, product minimum)
                rounded UP to the product's weightRoundingGrams
```

The quote's `weight.explanation` states this in words for the exact shipment.
Show it: it is the single most common billing question.

---

## 6. Booking a shipment

### The screen flow

1. Collect sender and recipient postcodes and the product. **Use an
   autocomplete backed by `GET /api/v1/geography/places`, not a plain text
   field** — see below.
2. `POST /api/v1/serviceability/check` — is the lane carried, by when?
3. Collect packages.
4. `POST /api/v1/pricing/quote` — what will it cost?
5. `POST /api/v1/shipments` — book it.

Steps 2 and 3 are optional but strongly recommended: they let you show coverage
and price before the user commits, and they use exactly the same engine as
booking, so the quoted price is the price charged.

### Finding the postcode — do not ask for it directly

Everything downstream is keyed on postcode, but **assuming the sender knows
theirs is the single fastest way to make booking impossible.** Postcode adoption
varies sharply by market: in Nigeria addresses are given by area, LGA and
landmark, and most senders cannot tell you their six digits.

`GET /api/v1/geography/places?q=<what they typed>` resolves free text to
postcode candidates. It matches on area name, city, LGA, post office name and
code prefix, all at once:

```
GET /api/v1/geography/places?q=Ikeja%20GRA

{
  "data": [{
    "id": "pin_01J8Z…", "code": "100001",
    "label": "Ikeja GRA, Ikeja, Lagos",
    "matchedOn": "AREA",
    "area": "Ikeja GRA", "city": "Lagos",
    "district": "Ikeja", "state": "Lagos", "stateCode": "LA",
    "isRemote": false
  }],
  "districtLabel": "LGA"
}
```

- Render `label`. It is already in address order with repeated names collapsed
  (Lagos is both a city and a state), so you do not have to assemble it.
- Submit `code` as the address `pincode`. **The booking contract is unchanged** —
  this is a lookup, not a new address format.
- `matchedOn` (`CODE`, `AREA`, `CITY`, `OFFICE`, `DISTRICT`) says which field
  produced the hit. Worth showing, so the sender can see why a row is listed.
- Minimum two characters. Results are capped at `limit`, default 20, max 50.
- A postcode matched several ways appears **once**, classified by its strongest
  match. You will not get "Ikeja" three times.
- LIKE wildcards are literal text. Typing `%` searches for a percent sign.

`districtLabel` is what this market calls that administrative level — `"LGA"` in
Nigeria, `"District"` in India. **Label the field with it rather than
hardcoding**, and use `GET /api/v1/geography/districts?state=LA` when you need
the list itself; it returns the same word in `label`.

### Idempotency — read this

`POST /api/v1/shipments` accepts an `Idempotency-Key` header.

**Always send one.** Generate a UUID when the user opens the booking form, keep
it for the lifetime of that form, and send it with every submit attempt.

- Same key, same body → the original shipment is returned with
  `Idempotent-Replay: true`. No second shipment is created.
- Same key, **different** body → `409 IDEMPOTENCY_KEY_REUSED`. Generate a new
  key when the user materially edits the form after a failed submit.
- Same key, still in flight → `409 IDEMPOTENCY_IN_PROGRESS`. Wait a moment and
  retry the same request.

Without a key, a network timeout followed by a user retry creates **two
shipments and two AWBs**. There is no way to undo that except cancelling one.

As a second line of defence, `referenceNumber` is unique per customer among
non-cancelled shipments: reusing one returns `409 DUPLICATE_RESOURCE`.

### What the server does

One atomic transaction: validate customer, sender, recipient and packages;
check serviceability; resolve the route; price it; check credit; allocate a
unique AWB; write the shipment, packages, three snapshots, the `BOOKED` event
and an audit record. It all lands, or none of it does. A `409` or `422` means
nothing was created.

### Server-owned fields

Never send these; they are rejected as unknown fields:

`organizationId`, `awb`, `status`, `totalAmountMinor`, `chargeableWeightGrams`,
`originBranchId`, `destinationBranchId`, `routeId`, any `*Id` you did not
receive from the API.

### Booking failures

| Code | HTTP | Meaning | What to do |
|---|---|---|---|
| `VALIDATION_FAILED` | 422 | A field is wrong. | Highlight `details.fields`. |
| `SHIPMENT_NOT_SERVICEABLE` | 409 | The lane cannot be carried. | Show `message`; branch on `details.reasonCode`. |
| `CONFLICT` | 409 | Credit limit exceeded, or the customer is suspended. | Show `message` and the credit figures in `details`. |
| `DUPLICATE_RESOURCE` | 409 | `referenceNumber` already used. | Ask for a different reference. |
| `IDEMPOTENCY_KEY_REUSED` | 409 | Same key, changed body. | New key, resubmit. |
| `IDEMPOTENCY_IN_PROGRESS` | 409 | A concurrent attempt is running. | Wait ~1s, retry the same request. |
| `NOT_FOUND` | 404 | The customer is not yours or does not exist. | Reload the customer picker. |

### Serviceability reason codes

| `reasonCode` | Suggested message |
|---|---|
| `ORIGIN_NOT_SERVICEABLE` | "We do not collect from this PIN code." |
| `DESTINATION_NOT_SERVICEABLE` | "We do not deliver to this PIN code." |
| `NO_ROUTE_AVAILABLE` | "No route is configured for this lane and service." |
| `DENIED_BY_RULE` | Show `reasonMessage` — it is written by the operator. |
| `ORIGIN_TEMPORARILY_CLOSED` | "Collection is suspended here until <date>." |
| `DESTINATION_TEMPORARILY_CLOSED` | "Delivery is suspended here until <date>." |
| `ORIGIN_HUB_NOT_CONFIGURED` / `DESTINATION_HUB_NOT_CONFIGURED` | A configuration gap: tell the user to contact operations. |
| `PINCODE_NOT_FOUND` | "We do not recognise this PIN code." |

An unserviceable lane from `/serviceability/check` is a **200**, not an error —
it is a valid answer. Read `serviceable`.

---

## 7. Shipment states

Twenty-two states are published; **Release 1 drives only `BOOKED` and
`CANCELLED`**. The rest arrive with Release 2 operations, so build your status
chip and filter against the full enum now and they will keep working.

Terminal states (nothing follows): `DELIVERED`, `RTO_DELIVERED`, `CANCELLED`,
`LOST`.

**Do not hard-code which actions are available.** Every shipment response
carries `allowedTransitions` — the states reachable *right now, in this build*.
Render one action button per entry. In Release 1 a booked shipment returns
`["CANCELLED"]` and a cancelled one returns `[]`.

Cancellation requires a `reason` of at least five characters. It is recorded on
the event and in the audit trail, and a credit booking has its reserved credit
released.

### History

`GET /api/v1/shipments/{id}/events` returns the append-only log, oldest first.
`description` is written for customers; show it directly. Events can never be
edited or deleted — a correction is always a new event.

---

## 8. Fields users may edit, and fields they may not

| Entity | Editable | Never editable | Why |
|---|---|---|---|
| Shipment | `reason` on cancel only | `awb`, `status`, all charges, all snapshots, packages | The snapshots are the legal record of what was agreed. |
| Organization | name, legalName, timezone, contacts, GST, settings | `code`, `currency`, `awbPrefix`, `status` | Changing the AWB prefix would break AWB uniqueness. |
| Operating unit | name, address, contact, hours, parent, status | `code`, `unitType` | Codes appear on printed labels and in routing configuration. |
| Courier service | name, limits, SLA, COD/insurance flags, status | `code` | Rate cards and routes reference it by code. |
| Customer | name, contacts, addresses, GST/PAN, credit terms | `code`, `customerType`, `creditUsedMinor` | Credit usage is derived from bookings, never set. |
| Rate card version | everything, **while `DRAFT`** | everything, once `ACTIVE` | Historical shipments must never reprice. Create a new version. |
| Role | custom roles only | system roles (`isSystem: true`) | They are shared by every tenant. |
| Audit event, shipment event | nothing | everything | Append-only, enforced by the database. |

### Optimistic concurrency

Operating units, franchises, courier services and customers carry a `version`.
When updating, send `expectedVersion` with the value you last read. If someone
else changed the record meanwhile you get:

```json
{ "error": { "code": "CONCURRENT_MODIFICATION",
  "message": "This ... was modified by someone else. Reload it and try again.",
  "details": { "expectedVersion": 3, "currentVersion": 4 } } }
```

Reload, show the user what changed, and let them decide. Do not auto-retry.

---

## 9. Pricing

`POST /api/v1/pricing/quote` returns a complete, self-explaining breakdown.

```json
{
  "currency": "INR",
  "rateCardCode": "RETAIL", "rateCardVersion": 1,
  "weight": { "actualWeightGrams": 500, "volumetricWeightGrams": 600,
              "chargeableWeightGrams": 1000,
              "explanation": "actual=500g volumetric=600g (divisor 5000); volumetric basis selected, rounded up to the nearest 500g -> 1000g" },
  "freightMinor": 7500, "surchargeTotalMinor": 1388, "discountTotalMinor": 0,
  "taxableMinor": 8888, "taxTotalMinor": 1600, "totalMinor": 10488,
  "lineItems": [
    { "kind": "FREIGHT", "code": "FREIGHT", "label": "Freight charge", "amountMinor": 7500,
      "explanation": "base 500g at INR 50.00 plus 1 x 500g at INR 25.00 = INR 25.00" },
    { "kind": "SURCHARGE", "code": "FUEL", "label": "Fuel surcharge", "amountMinor": 1388,
      "basisMinor": 7500, "rate": "18.50%",
      "explanation": "18.50% on freight of INR 75.00 = INR 13.88" },
    { "kind": "TAX", "code": "IGST18", "label": "IGST 18%", "amountMinor": 1600,
      "basisMinor": 8888, "rate": "18.00%",
      "explanation": "IGST 18.00% on taxable value INR 88.88 = INR 16.00" }
  ]
}
```

Render `lineItems` in order; they are already ordered freight → surcharges →
discounts → tax. `amountMinor` is negative for discounts. Every line carries an
`explanation` safe to show to a customer.

The arithmetic is:
`total = freight + surcharges − discounts + tax`. Display it, do not derive it.

Tax depends on the lane: an intra-state lane attracts CGST + SGST, an
inter-state lane attracts IGST. The server decides; you just render the lines.

**Configuration failures** come back as `422` with `details.code`:

| `details.code` | Meaning |
|---|---|
| `RATE_CARD_NOT_FOUND` | No rate card applies. An administrator must set a default retail card or assign one to the customer. |
| `RATE_NOT_CONFIGURED` | No rate exists for this lane on the active card. |

These are operator problems, not user errors. Say so.

---

## 10. Labels

`GET /api/v1/shipments/{id}/label` returns everything a label needs:

- `barcodePayload` — encode as **Code128**. It is exactly the AWB, so a scan
  anywhere in the network produces a value the API accepts.
- `qrPayload` — a pipe-delimited record prefixed `CSV1|` for handheld scanners.
- `routingCode` — the sortation string. Print it large; hub staff read it.
- `pieces[]` — one barcode per piece for multi-piece shipments.

`?format=ZPL` returns a ready-to-print 4x6 inch ZPL II document as
`text/plain`, for Zebra-compatible counter printers. PDF rendering is a later
release; for now, render the JSON payload in the browser or send the ZPL
directly to the printer.

A cancelled shipment returns `409` — no label.

---

## 11. Permissions

`GET /api/v1/auth/me` returns `permissions` (effective codes),
`operatingUnitIds` (the units this principal is scoped to) and
`hasOrganizationWideAccess`.

Scope rules:

- `hasOrganizationWideAccess: true` → the user sees the whole tenant.
- Otherwise the user sees only shipments and customers belonging to
  `operatingUnitIds`. Lists are filtered server-side; detail requests for
  out-of-scope objects return **404, not 403**, so you cannot distinguish
  "does not exist" from "not yours". Treat both as not found.
- `shipment.read_all` lifts the unit restriction for support and finance roles.

The twelve system roles are `SUPER_ADMIN`, `ORG_ADMIN`, `OPERATIONS_ADMIN`,
`HUB_MANAGER`, `BRANCH_MANAGER`, `FRANCHISE_OWNER`, `FRANCHISE_OPERATOR`,
`FINANCE_MANAGER`, `CUSTOMER_SUPPORT`, `PICKUP_AGENT`, `DELIVERY_AGENT`,
`CUSTOMER`. Roles whose `scopeRequired` is true must be granted at a specific
operating unit.

A `403` always names what was needed:

```json
{ "error": { "code": "FORBIDDEN", "message": "You do not have permission to perform this action.",
  "details": { "requiredPermission": "shipment.create" } } }
```

---

## 12. Bulk import

Uploads are asynchronous:

1. `POST /api/v1/geography/imports/pincodes` (multipart, field name `file`,
   `.csv`, max 25 MB) → **202** with `{ id, totalRows, statusUrl }`.
2. Poll `statusUrl` every few seconds. `status` moves
   `PENDING → PROCESSING → COMPLETED | COMPLETED_WITH_ERRORS | FAILED`.
3. Show progress from `processedRows / totalRows`.
4. On `COMPLETED_WITH_ERRORS`, `errorSummary` gives counts by error code and
   `errorsUrl` returns the rejected rows with reasons. `errorFileUrl` streams
   them as CSV the operator can correct and re-upload.

Rejected rows never block the rest: a 19,000-row file with 12 bad rows imports
18,988 and reports the 12.

### Postcode CSV columns

`pincode` and `state` are required. Everything else is optional, extra columns
are ignored:

| Column | Notes |
|---|---|
| `pincode` | Six digits, non-zero lead. |
| `state` | Matched by name against the country's states. |
| `district` | The LGA in Nigeria. Created if absent. |
| `city`, `city_tier` | Tier is `METRO`/`TIER_1`/`TIER_2`/`TIER_3`/`OTHER`. |
| `locality` | **An area name.** One per row; repeat the postcode to attach several. |
| `office_name` | As published by the postal authority. |
| `latitude`, `longitude` | Decimal degrees. |
| `is_remote` | Platform-wide remote-area default. |

`locality` is the column that decides whether senders can find a postcode at
all — it is what `GET /api/v1/geography/places` searches. A file loaded without
it produces a dataset only findable by people who already know the code.

---

## 13. Routing, for the operations console

`POST /api/v1/serviceability/check` returns the resolved path:
`originBranch → originHub → …legs… → destinationHub → destinationBranch`, plus
`slaHours` and `promisedDeliveryAt`.

`resolutionSource` tells you *why* that path was chosen:

| Value | Meaning |
|---|---|
| `OVERRIDE` | A manual override for this exact lane. Show a badge — it is an exception. |
| `SERVICE_ROUTE` | A route bound to this product. |
| `GENERIC_ROUTE` | A route serving all products. |
| `FALLBACK_ROUTE` | The fallback; the preferred route did not match. Worth surfacing. |
| `LOCAL_DELIVERY` | Both ends share a hub; no line haul. |

`cutoffApplied: true` means the booking fell after the cut-off and joins the
next despatch — say so, or users will query the promised date.

Administrators with `routing.debug` can call `POST /api/v1/serviceability/debug`
for the full `explanation`: every candidate considered at each step and why it
won or lost. Render it as a timeline; it is the fastest way to answer "why did
this parcel route that way?".

---

## 14. Rate card editing

The lifecycle is strict, and the UI should make it obvious:

1. Create a rate card (`RETAIL`, `BUSINESS` or `FRANCHISE`).
2. Create a **draft** version.
3. Add zone rates, weight slabs, surcharges and discounts. Editable only while
   `DRAFT` — check `editable` on the version response.
4. Activate it. This supersedes the previous active version atomically.
5. From this moment the version is **immutable**. Any write returns
   `409 IMMUTABLE_RESOURCE`.

To change prices, create a new version. Do not offer an "edit" button on an
active version; offer "create new version" instead.

Overlapping weight slabs for the same lane are rejected with `409` — the
database enforces it, so ambiguous pricing is impossible.

---

## 15. Things that will bite you

1. **Do not compute money.** Ever. Not even a subtotal.
2. **Do not cache a quote and book against it later.** Prices are
   effective-dated; book from a fresh quote.
3. **Do not construct public IDs or AWBs.** They are server-generated.
4. **Do not assume 404 means "deleted".** It may mean "not in your scope".
5. **Do not send a whole GET response back as a PATCH body.** Unknown fields
   are rejected.
6. **Do not retry a non-idempotent POST without an `Idempotency-Key`.**
7. **Do not parse a cursor.** It is opaque and its format may change.
8. **Do not hard-code status lists.** Use `allowedTransitions`.
9. **Timestamps are RFC3339 with a timezone.** Convert to the organization's
   `timezone` for display; never assume the browser's zone matches.
10. **`X-Request-Id`** is on every response. Log it, and show it on error
    screens.
