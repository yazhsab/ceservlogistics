# Release 2 — Physical Courier Operations

**Audience:** engineers building the operations console, the field-agent app and
the public tracking page.

This document explains behaviour. [`docs/openapi.yaml`](../openapi.yaml) is the
machine-readable contract and always wins on exact field names and types; read
this for *why* an endpoint behaves the way it does and what will happen when it
refuses you.

Release 1 turned a customer's intent into a priced, routed shipment. Release 2
moves the physical parcel. The difference matters for the UI: Release 1
endpoints mostly succeed or fail on validation, whereas Release 2 endpoints
routinely refuse on **state** and **custody** — and those refusals are normal
operating conditions, not bugs to hide.

---

## 1. The vocabulary

| Term | What it means |
|---|---|
| **AWB** | The human-readable shipment number. Globally unique. The only identifier shown publicly. |
| **Piece barcode** | Per-package label, e.g. `<AWB>-01`. Any endpoint taking a `barcode` accepts either. |
| **Operating unit** | A branch or hub. Called a *facility* throughout. |
| **Custody** | Which facility, agent, bag or trip physically holds a parcel right now. |
| **Bag** | A sealed container of shipments moving between two facilities. |
| **Manifest** | The handover document: bags plus loose shipments, from one facility to another. |
| **Trip** | The vehicle movement that carries manifests. |
| **Delivery run** | One agent's ordered list of stops for a day. |
| **NDR** | Non-delivery report: the case opened when a delivery fails. |
| **RTO** | Return to sender, modelled as reverse movement over the same network. |
| **POD** | Proof of delivery: recipient details plus signature and photo evidence. |
| **Milestone** | The customer-safe summary of a status, used by public tracking. |

### Direction

Every shipment carries `movementDirection`: `FORWARD` or `REVERSE`. It flips to
`REVERSE` when an RTO starts. **The state machine refuses forward transitions on
a reverse-moving parcel**, which is what stops a returning parcel being
delivered to the original consignee by mistake. Show returning parcels
distinctly in every list.

---

## 2. The shipment lifecycle

All 22 statuses, and every legal move between them. Anything not drawn here is
refused with `SHIPMENT_INVALID_STATE`.

```
                                    ┌──────────┐
                                    │  BOOKED  │
                                    └────┬─────┘
             ┌───────────────────────────┼───────────────────────────┐
             ▼                           ▼                           ▼
   ┌───────────────────┐      ┌────────────────────┐        ┌─────────────┐
   │ PICKUP_SCHEDULED  │─────▶│  PICKUP_ASSIGNED   │        │  CANCELLED  │
   └─────────┬─────────┘◀─────└─────────┬──────────┘        └─────────────┘
             │                          ▼
             │                   ┌─────────────┐
             │                   │  PICKED_UP  │
             │                   └──────┬──────┘
             └──────────────┬───────────┘
                            ▼
              ┌──────────────────────────┐
              │  ORIGIN_BRANCH_RECEIVED  │◀──────────────┐
              └───┬──────────────────┬───┘               │
                  ▼                  │                   │ removed from bag
          ┌────────────────┐         │                   │
          │  ORIGIN_BAGGED │─────────┼───────────────────┘
          └───────┬────────┘         │ loose shipment
                  ▼                  ▼
           ┌──────────────────────────────┐
           │      ORIGIN_DISPATCHED       │
           └──────────────┬───────────────┘
                          ▼
                  ┌──────────────┐
        ┌────────▶│  IN_TRANSIT  │◀────────┐
        │         └──────┬───────┘         │
        │      ┌─────────┼─────────┐       │
        │      ▼         ▼         ▼       │
        │  ┌────────┐ ┌───────┐ ┌────────┐ │
        │  │TRANSIT_│ │DESTIN-│ │DESTIN- │ │
        │  │  HUB_  │ │ATION_ │ │ATION_  │ │
        │  │RECEIVED│ │ HUB_  │ │BRANCH_ │ │
        │  └───┬────┘ │RECEIVD│ │RECEIVED│ │
        │      ▼      └───┬───┘ └───┬────┘ │
        │  ┌─────────────────┐      │      │
        └──│TRANSIT_HUB_     │◀─────┘      │
           │   DISPATCHED    │  (misroute) │
           └─────────────────┘─────────────┘
                          │
                          ▼
              ┌────────────────────────────┐
              │ DESTINATION_BRANCH_RECEIVED│◀───────┐
              └──────────┬─────────────────┘        │
                         ▼                          │
                ┌──────────────────┐                │
        ┌──────▶│ OUT_FOR_DELIVERY │                │
        │       └────┬────────┬────┘                │
        │            ▼        ▼                     │
        │   ┌───────────┐  ┌──────────────────┐     │
        │   │ DELIVERED │  │ DELIVERY_FAILED  │─────┘  back to the shelf
        │   └───────────┘  └────────┬─────────┘
        │                           ▼
        │                      ┌─────────┐
        └──────────────────────│   NDR   │
              reattempt        └────┬────┘
                                    ▼
                          ┌──────────────────┐
                          │  RTO_INITIATED   │
                          └────────┬─────────┘
                                   ▼
                          ┌──────────────────┐
                          │  RTO_IN_TRANSIT  │  ← reverse movement records
                          └────────┬─────────┘    events, not status changes
                                   ▼
                          ┌──────────────────┐
                          │  RTO_DELIVERED   │
                          └──────────────────┘

Exception states, reachable from any custody-holding state:
  LOST      terminal
  DAMAGED   → RTO_INITIATED, DELIVERED (accepted damaged), or back to the branch
```

**Terminal states** — `DELIVERED`, `RTO_DELIVERED`, `CANCELLED`, `LOST` — admit
nothing further. Everything else has at least one exit.

Every `SHIPMENT_INVALID_STATE` error carries `details.allowedTransitions`, so a
UI can show what *is* possible rather than a dead end. Use it.

### Reverse movement has one in-flight status

A returning parcel stays `RTO_IN_TRANSIT` from dispatch until it is handed back.
Its progress is tracked by the RTO case's `legs` array and by the event stream —
not by a mirrored ladder of statuses. To show "where is my return", read
`GET /rto/{caseId}` and render the legs; do not expect the shipment status to
change at each facility.

---

## 3. Operational scanning

`POST /api/v1/scans` is the endpoint the handheld hits. Two things about it are
unlike the rest of the API.

**It returns 200 even when the parcel could not move.** The `outcome` field is
the answer:

| `outcome` | Meaning | What the UI should do |
|---|---|---|
| `ACCEPTED` | The scan applied; the parcel moved. | Green. Show `nextAction`. |
| `DUPLICATE` | This device event was already recorded. | Amber. Not an error — a retry landed. |
| `REJECTED` | Recorded, but the parcel did not move. | Red. Show `rejectionMessage`. |

A 4xx from this endpoint means the *request* was wrong — a malformed barcode, a
missing permission — not that the parcel refused. Handle the two differently:
one is a bug in your call, the other is a fact about the parcel.

**Every scan is recorded, including rejections.** That is deliberate: a device
scanning parcels into the wrong facility is an operational problem that is
invisible if failures only reach the handset. `GET /api/v1/scans?outcome=REJECTED`
is a supervisor's view of it.

### Scan types

| Type | Permission | Effect |
|---|---|---|
| `RECEIVE` | `scan.inbound` | Takes custody. The resulting status depends on where the facility sits on the shipment's route — the server works it out; the scanner does not choose. |
| `ARRIVAL` | `scan.inbound` | Same as receive; kept distinct because hubs run the gate and the inbound desk separately. |
| `DEPARTURE` | `scan.outbound` | Records the parcel leaving. Normally driven by manifest dispatch. |
| `SORT` | `scan.sort` | Records a routing decision. Does **not** change status or custody. |
| `HOLD` | `scan.hold` | Stops the parcel. Requires `reason`. |
| `RELEASE` | `scan.hold` | Lifts the hold. |
| `DAMAGE` | `scan.exception` | Moves to `DAMAGED`. Requires `reason`. |
| `EXCEPTION` | `scan.exception` | Records a problem without moving the parcel. Requires `reason`. |

### Bulk scanning

`POST /api/v1/scans/bulk` takes up to **200** barcodes. Each is its own
transaction: one bad parcel in a trolley of two hundred does not lose the rest.
The response reports every barcode individually. Render it as a short exception
list, not as a wall of successes.

### Holds actually stop things

A held parcel is refused by bagging, manifest dispatch and delivery dispatch
with `SHIPMENT_ON_HOLD`. `isHeld` and `holdReason` appear on the delivery queue
and the custody stocktake — surface them, because a supervisor's first question
about a stuck parcel is why.

---

## 4. Offline and retry safety

Field applications lose connectivity. Two mechanisms make retries safe, and the
API expects you to use one of them.

### Device event ids (preferred for the field app)

Send both headers with any operational write:

```
X-Device-Id:        handset-4821
X-Device-Event-Id:  a-locally-generated-unique-id
```

Generate the event id **before** you queue the request, and keep it across
retries. Replaying it returns the original result rather than repeating the
operation:

- a scan comes back with `outcome: DUPLICATE` and the original `scanId`
- a delivery attempt comes back with `replayed: true`
- a pickup completion comes back with `replayedAttemptId` set

### Idempotency keys (preferred for the console)

`Idempotency-Key` works as it does in Release 1.

`POST /deliveries/attempts` **requires one of the two** and returns
`IDEMPOTENCY_KEY_REQUIRED` without either — a duplicate delivery is not
something to leave to chance.

### Client time versus server time

Operational writes accept an `occurredAt` for scans made offline. It is recorded
verbatim, but the value used for ordering is clamped to a plausible window
(5 minutes ahead, 7 days behind). A device with a wrong clock cannot reorder
history, and a deliberate backdate cannot escape an audit window. Every event
therefore carries both `occurredAt` (what the device claimed) and `recordedAt`
(when the server received it). Show the first; reconcile with the second.

---

## 5. Bag lifecycle

```
OPEN ──▶ CLOSED ──▶ DISPATCHED ──▶ RECEIVED ──▶ OPENED ──▶ RECONCILED
 │         │                                       │
 │         └──▶ OPEN (reopened before dispatch)    └──▶ RECONCILED
 └──▶ CANCELLED
```

Every bag response carries `allowedTransitions`. Render buttons from it.

**Contents are mutable only while `OPEN`.** This is enforced by a database
trigger, not by the service, so it holds even if a handler is defective. After
closure:

- adding is refused with `BAG_NOT_OPEN`
- removing is refused with `BAG_CONTENTS_FROZEN`
- the only way to change contents is `POST /bags/{id}/exception-correction`,
  which requires the `bag.exception_edit` permission, a written reason, and a
  linked exception record

**Closure produces `declaredContents`** — a frozen snapshot of what the bag held
and what it weighed. Reconciliation at the far end compares scans against *this*,
never against a live read, and a later correction never rewrites it. When you
show a discrepancy, show both the declaration and the count.

Adding items is a batch call. Common per-item refusals:

| `reason` | What happened |
|---|---|
| `BAG_MEMBERSHIP_CONFLICT` | The shipment is already inside another bag. |
| `BAG_DESTINATION_MISMATCH` | It is not routed through this bag's destination. |
| `BAG_SERVICE_MISMATCH` | The bag is restricted to a different service. |
| `BAG_DIRECTION_MISMATCH` | Forward parcel into a reverse bag, or vice versa. |
| `CUSTODY_VIOLATION` | The parcel is physically at another facility. |
| `SHIPMENT_ON_HOLD` | It is held. |
| `SHIPMENT_INVALID_STATE` | It is not at a stage where bagging makes sense. |

---

## 6. Manifest lifecycle

```
DRAFT ──▶ CLOSED ──▶ DISPATCHED ──▶ RECEIVED ──▶ RECONCILED
  │         │
  │         └──▶ DRAFT (reopened before dispatch)
  └──▶ CANCELLED
```

Only **closed** bags can be loaded: an open bag has no frozen declaration, so
loading one would publish a manifest describing contents that can still change.
A shipment already inside a bag cannot also be manifested loose.

Closure freezes the contents and queues the printable document, generated in the
background. Poll `documentReady` on the manifest; it flips to `true` when the
document exists. (Release 2 renders fixed-width text suited to a loading-bay
printer — see the release notes for why, and for what will change.)

Dispatch and receipt move **every** shipment on the manifest in one transaction.
There is no partial state to render.

---

## 7. Trip lifecycle

```
PLANNED ──▶ LOADING ──▶ DEPARTED ──▶ IN_TRANSIT ──▶ ARRIVED ──▶ CLOSED
   │           │                          ▲            │
   │           │                          └────────────┘  multi-leg
   └───────────┴──▶ CANCELLED                (intermediate stops)
```

**Departure validates before it moves anything**, and each refusal has its own
code so the UI can point at the fix:

| Code | Fix |
|---|---|
| `TRIP_HAS_DRAFT_MANIFEST` | Close the manifests. |
| `TRIP_NO_VEHICLE` | Assign a vehicle (road trips). |
| `TRIP_NO_DRIVER` | Assign a driver (road trips). |
| `TRIP_NO_REFERENCE` | Supply the flight, train or partner reference (non-road). |
| `TRIP_EMPTY` | Attach a manifest. |

**Arrival does not receive the parcels.** A vehicle reaching the gate and a hub
counting what came off it are different events; conflating them is how shipments
end up marked received at a facility that never saw them. After `POST /arrive`,
run `POST /manifests/{id}/receive` or inbound scans.

Multi-leg trips: legs are validated as a chain at creation. Attach a manifest to
a specific `legSequence` when it travels only part of the way, or a parcel for
an intermediate stop will be carried past it.

A vehicle or driver already on a live trip is refused with `VEHICLE_IN_USE` or
`DRIVER_IN_USE`.

---

## 8. Hub operations

`GET /hub/summary` returns the whole facility dashboard in one round trip —
fifteen counts, each index-served. Poll it; do not assemble the dashboard from
fifteen list calls.

`GET /hub/inbound` returns manifests dispatched towards this facility and trips
on the road to it, oldest first because that is what is most overdue.

### Reconciliation

The count that answers "does what arrived match what was declared".

1. `POST /reconciliations` with a received bag or manifest. The expected list is
   materialised from the **closure declaration**.
2. `POST /reconciliations/{id}/scan` with what you actually scanned. Declared
   items become `MATCHED`; undeclared ones become `EXCESS` and raise an
   exception immediately.
3. `POST /reconciliations/{id}/complete`. Everything still unscanned becomes
   `MISSING` and raises an exception.

The final status is `COMPLETED` or `COMPLETED_WITH_EXCEPTIONS`. Only one count
per subject can be live, so two clerks cannot both be counting: a second attempt
gets `RECONCILIATION_IN_PROGRESS`.

### Exceptions

Types: `MISSING`, `EXCESS`, `DAMAGED`, `MISROUTED`, `SEAL_BROKEN`,
`UNKNOWN_BARCODE`, `WEIGHT_MISMATCH`, `COUNT_MISMATCH`, `WRONG_FACILITY`,
`CUSTODY_OVERRIDE`, `OTHER`.

**Closing an exception requires both an action and a note.** The API refuses
without them, and so does the database. Make both fields mandatory in the form
rather than letting the user discover this on submit.

---

## 9. Delivery

### The queue

`GET /deliveries/queue?operatingUnitId=…` is the branch's work list: everything
that could go out today, most urgent first, with any NDR context attached. It
excludes parcels already on a live run by default (`excludeAssigned=true`) and
hides held parcels unless you ask for them.

### The run

```
PLANNED ──▶ ASSIGNED ──▶ DISPATCHED ──▶ IN_PROGRESS ──▶ COMPLETED ──▶ CLOSED
```

`POST /delivery-runs/{id}/dispatch` is the **only** way a parcel reaches
`OUT_FOR_DELIVERY`, and it checks that the run's branch actually holds the
parcel. A run at the wrong branch cannot dispatch a parcel it does not have.

A shipment can be on only one live run: a second attempt gets
`SHIPMENT_ALREADY_ASSIGNED` naming the other run and agent.

### The attempt

`POST /deliveries/attempts` is the most concurrency-sensitive call in the
system. Guarantees you can rely on:

- **Exactly one success per shipment, ever.** Concurrent attempts produce one
  200 and the rest get `ALREADY_DELIVERED`. This is a database constraint.
- **Only the holding agent may complete it.** Another agent gets 403 with
  `CUSTODY_VIOLATION`. A supervisor with `delivery.manage` may close out for
  them.
- **A replayed device event returns the original attempt** with
  `replayed: true`.

COD is strict: the collected amount must equal the amount due, in integer minor
units, and a payment mode is required. Under-collection is refused with
`COD_AMOUNT_MISMATCH` rather than silently accepted — it is a loss somebody will
be asked to cover. Show the amount due prominently on the stop.

### OTP

`POST /deliveries/otp` returns the code **once**. Only its digest is stored; it
cannot be read back from the API, the database or a log. Send it to the
consignee immediately and do not persist it client-side. Issuing a second code
invalidates the first.

Verification happens inside the delivery attempt: pass `otp` on the attempt.
Wrong codes count against a five-attempt budget.

---

## 10. NDR

A failed delivery opens or advances an NDR case automatically; the attempt
response returns `ndrCaseId`. The case is a workflow, not a status:

- **`attempts`** — append-only record of each failed visit
- **`actions`** — the instruction currently in force, and its history
- **`availableActions`** — what an operator may choose next

Actions: `REATTEMPT`, `RESCHEDULE`, `CONTACT_REQUIRED`, `ADDRESS_CORRECTION`,
`CUSTOMER_PICKUP`, `RTO`, `ESCALATE`.

Setting an action supersedes any pending one, so an agent is never shown two
conflicting instructions. `REATTEMPT` returns the parcel to the branch shelf
(`DESTINATION_BRANCH_RECEIVED`). `RTO` hands off to the returns workflow and
resolves the case.

**Reasons are configurable per tenant** (`GET /ndr/reasons`). Each carries a
default action, an attempt budget, and whether exhausting it triggers an
automatic return. Build the reason picker from the API, not from a hardcoded
list: the catalogue is data.

When the budget runs out and the reason says so, the platform starts the return
by itself. The event is attributed to `SYSTEM`, not to the agent who recorded
the last attempt — they did not choose it, and the history should say so.

### Corrected addresses

An `ADDRESS_CORRECTION` stores the new address **on the case**. It never
rewrites the shipment's booking snapshot. Both are visible, and the agent should
see both: where it was originally sent, and where it should go now.

---

## 11. RTO

A return reuses the forward network: the same bags, manifests and trips, filtered
by direction so forward and reverse traffic never mix in one container.

```
INITIATED ──▶ IN_TRANSIT ──▶ AT_ORIGIN_BRANCH ──▶ RETURNED
                    │                                 ▲
                    └──▶ RETURN_FAILED ───────────────┘
```

The reverse route is resolved by the same routing engine with origin and
destination swapped, and published as `legs` with a `routeExplanation` recording
how it was chosen. The return address comes from the **sender's booking
snapshot**, not the customer's current address: using the current one would send
the parcel wherever they have since moved.

`POST /rto/receive` records the parcel arriving at a facility on its way back.
The shipment status stays `RTO_IN_TRANSIT` — advance the leg display, not the
status display.

RTO charges are calculated (`rtoChargeMinor`, `chargeBearer`) but **not settled**:
that lands with commission and settlement in Release 3. Show the figure as
indicative.

---

## 12. Proof of delivery

`POST /api/v1/pod` takes `multipart/form-data`. File fields are named after the
artifact type in lower case: `signature`, `photo`, `id_proof`, `document`,
`audio`. At least one is required.

- **The content type is decided by sniffing the bytes**, never from the filename
  or your `Content-Type`. Only JPEG, PNG, WebP and PDF are accepted; anything
  else is 415.
- Files over the configured limit are 413. Compress on the device.
- Every file's SHA-256 is stored, which is what proves the file served in a
  dispute is the file uploaded on the day.
- A government ID is **masked before storage** — only the last four characters
  survive. Do not expect to read it back.
- `otpVerified` is copied from the delivery attempt, not accepted from you.

The record is append-only. There is no edit endpoint and there will not be one.

Downloading an artifact returns a `307` to a short-lived signed URL, or streams
the bytes when the backend cannot presign. Treat the signed URL as a secret: do
not log it or put it in a shareable link. Every retrieval is audited.

---

## 13. Public tracking

`GET /api/v1/track/{awb}` — **no authentication**, aggressively rate limited,
cached for 60 seconds.

It is deliberately the narrowest endpoint in the system. It never returns:

- internal identifiers of any kind — no `shp_`, `ou_`, `cus_`; the AWB is the
  only identifier a consignee gets
- internal remarks or employee names
- facility names or codes (locations are reduced to a **city**, and only where
  the milestone permits it — naming a facility would tell an observer where a
  network's sort hubs are)
- any financial figure except the COD amount the recipient must have ready

Events are normalised into **milestones** — `BOOKED`, `PICKUP`, `IN_TRANSIT`,
`OUT_FOR_DELIVERY`, `DELIVERED`, `EXCEPTION`, `RETURNING`, `RETURNED`,
`CANCELLED` — and consecutive events with the same milestone are collapsed
unless the location changed. The customer sees a journey, not a scan log.

An unknown AWB and a malformed one return an **identical 404**, so the endpoint
cannot be used to discover which numbers exist. Do not write UI copy that
distinguishes them.

`expectedDelivery` reflects a rescheduled NDR date when there is one, in
preference to the original promise.

---

## 14. What each role can do

Permissions come back on `/auth/me`. Drive the UI from them; do not infer from
the role code.

| | Pickup | Scan | Bag | Manifest | Trip | Delivery | NDR | RTO | POD |
|---|---|---|---|---|---|---|---|---|---|
| **ORG_ADMIN** | full | full | full | full | full | full | full | full | full |
| **OPERATIONS_ADMIN** | plan | full | full | full | full | plan | manage | manage | read |
| **HUB_MANAGER** | — | full | full | full | plan, depart, arrive | — | read | read | read |
| **BRANCH_MANAGER** | full | full | full | full | read, depart, arrive | full | manage | manage | full |
| **FRANCHISE_OWNER** | full | full | full | full | read | full | manage | manage | full |
| **FRANCHISE_OPERATOR** | raise | in, out, sort | open, close, receive, open | read, build, receive | — | read | read | read | read |
| **FINANCE_MANAGER** | read | read | read | read | read | read | read | read | read |
| **CUSTOMER_SUPPORT** | raise, cancel | read | read | read | read | read | manage | manage | read |
| **PICKUP_AGENT** | respond, complete | inbound | — | — | — | — | — | — | — |
| **DELIVERY_AGENT** | — | inbound, read | — | — | — | complete | manage | read | submit |
| **CUSTOMER** (portal) | raise, cancel | — | — | — | — | read | read | read | read |

Two rules that matter for the field app:

- A **delivery agent** can complete the deliveries in front of them and submit
  POD. They cannot build a run, reassign work, plan a trip, start a return or
  configure NDR reasons — all 403.
- A **pickup agent** can respond to what was assigned and record the visit. They
  cannot assign work to themselves.

### Operating-unit scope

Most roles are scoped to specific facilities. A branch manager acting at another
branch gets **403**, not 404 — this is within their tenant, so the object's
existence is not a secret. An object belonging to *another tenant* is always
**404**.

Pass the facility as `?operatingUnitId=` or `X-Operating-Unit`. If your role
covers exactly one facility you may omit it; if it covers several, the API asks
you to be explicit rather than guessing which of three hubs you are standing in.

---

## 15. Error codes

Beyond the Release 1 envelope, Release 2 adds these. All follow the standard
shape with `error.code`, `error.message` and `error.details`.

### State and custody

| Code | Meaning |
|---|---|
| `SHIPMENT_INVALID_STATE` | The move is not legal from here. `details.allowedTransitions` lists what is. |
| `CUSTODY_VIOLATION` | The parcel is not where you are, or not with you. |
| `SHIPMENT_ON_HOLD` | Held. `details.holdReason` says why. |
| `CONCURRENT_MODIFICATION` | Somebody changed it between your read and your write. Reload and retry. |

### Bagging and manifest

| Code | Meaning |
|---|---|
| `BAG_NOT_OPEN` | Contents change only while open. |
| `BAG_CONTENTS_FROZEN` | Closed. Use the exception-correction workflow. |
| `BAG_MEMBERSHIP_CONFLICT` | The shipment is in another bag. |
| `BAG_DESTINATION_MISMATCH` | Not routed through this bag's destination. |
| `BAG_SERVICE_MISMATCH` / `BAG_DIRECTION_MISMATCH` | Restriction violated. |
| `BAG_CAPACITY_EXCEEDED` | Weight or count limit reached. |
| `BAG_WRONG_DESTINATION` | Received at a facility it was not addressed to. |
| `BAG_EMPTY` / `MANIFEST_EMPTY` | Nothing to close. |
| `BAG_NOT_CLOSED` | Only closed bags go on a manifest. |
| `MANIFEST_NOT_DRAFT` / `MANIFEST_CONTENTS_FROZEN` | Same rule, one level up. |
| `MANIFEST_WRONG_DESTINATION` | Addressed elsewhere. |
| `MANIFEST_ORIGIN_MISMATCH` / `MANIFEST_LEG_MISMATCH` / `MANIFEST_TRIP_MISMATCH` | Routing mismatch. |
| `BAG_ALREADY_MANIFESTED` / `SHIPMENT_ALREADY_MANIFESTED` | Already travelling. |
| `SHIPMENT_IN_BAG` | Load the bag, not the parcel. |
| `SEAL_MISMATCH` | The presented seal is not on this bag. |

### Line haul

`TRIP_INVALID_STATE`, `TRIP_HAS_DRAFT_MANIFEST`, `TRIP_NO_VEHICLE`,
`TRIP_NO_DRIVER`, `TRIP_NO_REFERENCE`, `TRIP_EMPTY`, `TRIP_WRONG_FACILITY`,
`TRIP_MANIFEST_UNRECEIVED`, `TRIP_ALREADY_DISPATCHED`, `VEHICLE_IN_USE`,
`DRIVER_IN_USE`.

### Delivery, NDR and RTO

| Code | Meaning |
|---|---|
| `ALREADY_DELIVERED` | Somebody else completed it first. |
| `SHIPMENT_ALREADY_ASSIGNED` | Out with another agent. |
| `COD_NOT_COLLECTED` / `COD_AMOUNT_MISMATCH` | The money does not add up. |
| `OTP_NOT_ISSUED` / `OTP_INVALID` / `OTP_EXPIRED` / `OTP_ATTEMPTS_EXCEEDED` | Code problems. |
| `DELIVERY_RUN_INVALID_STATE` / `DELIVERY_RUN_EMPTY` | Run lifecycle. |
| `NDR_CASE_CLOSED` | Already resolved. |
| `RTO_INVALID_STATE` / `RTO_CLOSED` / `RTO_NOT_ROUTABLE` | Return lifecycle. |
| `RTO_RECEIVE_REQUIRED` | Use the RTO receive endpoint for a returning parcel. |

### Hub and reconciliation

`RECONCILIATION_IN_PROGRESS`, `RECONCILIATION_CLOSED`, `EXCEPTION_CLOSED`,
`BAG_NO_DECLARATION`, `MANIFEST_NO_DECLARATION`.

### Scan rejection codes

Returned inside a 200 response as `rejectionCode`: `UNKNOWN_BARCODE`,
`CUSTODY_VIOLATION`, `SHIPMENT_INVALID_STATE`, `SHIPMENT_ON_HOLD`,
`DUPLICATE_IN_BATCH`, and the validation codes above.

---

## 16. Fields you must never edit

Editable through the API:

- pickup window and instructions (before completion)
- bag and manifest contents (while `OPEN` / `DRAFT`)
- trip vehicle, driver and schedule (before departure)
- delivery run stops and agent (before dispatch)
- NDR action, next attempt, corrected address
- exception status, assignee and resolution

**Never editable, by anyone, through any endpoint:**

- `shipment_events` — the operational history, append-only, enforced by trigger
- `scan_events` — including the rejected ones
- a bag's or manifest's `declaredContents` — the frozen closure snapshot
- `delivery_attempts`, `pickup_attempts`, `ndr_attempts` — the visit record
- `proof_of_delivery` and its artifacts
- the object-access log

If a workflow seems to need one of these changed, it needs a *correction* —
a new fact layered on top — not an edit. That is what the exception, reversal
and adjustment workflows are for.

---

## 17. Pagination, filtering and sorting

Unchanged from Release 1. Operational lists are keyset paginated:

```
GET /api/v1/bags?limit=25&status=OPEN
→ { "data": [...], "pagination": { "hasMore": true, "nextCursor": "…" } }
```

Pass `nextCursor` back as `?cursor=`. Cursors are opaque; do not parse them.
Depth does not degrade the query — paging back through a busy day costs the same
as the first page.

Low-volume configuration lists (carriers, vehicles, drivers, pickup runs) use
`limit` and `offset` instead, because keyset pagination would be ceremony for
tables with tens of rows.

---

## 18. Building the field app: a worked sequence

```
1.  POST /auth/login                          → token, permissions
2.  GET  /delivery-runs?mine=true             → today's run
3.  GET  /delivery-runs/{id}                  → ordered stops with addresses
        (dispatch is done by the branch, not the agent)
4.  per stop:
    POST /deliveries/otp                      → if the customer requires one
    POST /deliveries/attempts                 → with X-Device-Id + X-Device-Event-Id
         outcome DELIVERED  → POST /pod  (multipart, signature and/or photo)
         outcome FAILED     → response carries ndrCaseId; show the reason picker
                              from GET /ndr/reasons
5.  end of run: undelivered parcels are scanned back in with
    POST /scans  scanType=RECEIVE  at the branch
```

Queue every write locally with its device event id. On reconnect, replay in
order; duplicates are safe and are reported as such.

---

## 19. What is not in this release

So you do not build UI expecting it:

- **Notifications.** No SMS or email is sent. OTPs and NDR updates are returned
  to the caller to deliver. (Release 4.)
- **Financial settlement.** COD is captured as an operational fact and RTO
  charges are calculated, but nothing posts to a ledger and no commission is
  computed. (Release 3.)
- **Webhooks.** No outbound events. Poll.
- **Route optimisation.** Delivery run stop order is whatever you supply.
- **Live vehicle tracking.** Trip events accept a position, but there is no
  streaming location feed.
- **PDF manifests.** The generated document is fixed-width text.

---

## 20. Where to look next

| Document | For |
|---|---|
| [`docs/openapi.yaml`](../openapi.yaml) | Exact schemas, 173 operations. |
| [`docs/contracts/release-1.md`](release-1.md) | Booking, pricing, customers, auth. |
| [`docs/releases/release-2-backend.md`](../releases/release-2-backend.md) | What shipped, with evidence and known limitations. |
| [`docs/adr/`](../adr/) | Why the design is what it is. ADR 0009 explains the RTO model. |
