# Release 3 — Franchise Finance, COD and Settlement

Frontend contract for M21 Commission, M22 Ledger, M23 COD, M24 Settlement,
M25 Billing.

Base path `/api/v1`. Every endpoint requires a bearer token unless stated.
All monetary values are **integer minor units** — `12550` is ₹125.50. There are
no decimal amounts anywhere in this API, in either direction.

---

## 1. The one idea to hold onto

**Nothing in finance stores a balance.** There is no `balance` column anywhere
in Release 3. Every number this API returns is a `SUM()` computed at read time
over immutable rows.

That has consequences you will feel while building screens:

- A balance is never "updated". Something is *posted*, and the balance moves
  because the sum changed.
- Nothing is ever edited to correct it. A mistake is corrected by posting its
  opposite, and both stay visible.
- "What does this franchise owe?" and "what is this branch holding?" are
  queries, not fields. They can be asked as of any date.

If a screen you are designing needs an editable money field, it is almost
certainly modelling something the backend expresses as a new row instead.

---

## 2. Ledger terminology

### Debit and credit

Forget "debit = money out". In double-entry, debit and credit are just the two
sides of every transaction, and **which side increases an account depends on the
account**:

| Account type | Normal balance | Increases on | Example |
| --- | --- | --- | --- |
| `ASSET` | `DEBIT` | debit | Cash, receivables |
| `EXPENSE` | `DEBIT` | debit | Commission expense |
| `LIABILITY` | `CREDIT` | credit | COD owed to a consignor |
| `EQUITY` | `CREDIT` | credit | Retained earnings |
| `REVENUE` | `CREDIT` | credit | Freight revenue |

Every account carries `normalBalance`, and `balanceMinor` is **already signed
for you**: a positive balance means "more of what this account is for". A
receivable with `balanceMinor: 67850` means ₹678.50 is owed *to* us; a liability
with `balanceMinor: 200000` means ₹2,000 is owed *by* us. You never need to
apply a sign yourself.

For display, `debitMinor` and `creditMinor` are separate fields on every entry.
Exactly one is non-zero. Render them as two columns, which is what an
accountant expects.

### Transactions and entries

A **journal transaction** is the unit that balances. It has two or more
**entries**, and `SUM(debit) == SUM(credit)` across them, always. The database
enforces this with a deferred constraint; an unbalanced transaction cannot exist.

```json
{
  "transaction": {
    "id": "jrn_01K...", "transactionNumber": "JV-202608-000001",
    "status": "POSTED", "postingDate": "2026-08-08",
    "totalMinor": 67850, "currency": "INR",
    "sourceType": "INVOICE", "purpose": "ISSUE",
    "description": "Invoice INV/2026-27/000042"
  },
  "entries": [
    {"accountCode": "1200", "debitMinor": 67850, "creditMinor": 0},
    {"accountCode": "4000", "debitMinor": 0, "creditMinor": 57500},
    {"accountCode": "2300", "debitMinor": 0, "creditMinor": 10350}
  ]
}
```

### Control and subsidiary accounts

A **control account** is organization-wide (`2200 Franchise Payable`). A
**subsidiary account** hangs off it for one counterparty
(`2200.FRN.7`, "Franchise Payable — franchise 7"), and is created automatically
the first time that party is posted to.

This matters for display: **a control account with subsidiaries reads zero**,
because postings land on the subsidiary. To show "total franchise payable"
across the network you want the rollup, not the control account's own balance.
`GET /ledger/accounts/{id}` returns the account's own balance; the trial balance
lists subsidiaries individually.

### Statuses

| Status | Meaning | In the books? |
| --- | --- | --- |
| `DRAFT` | Being assembled, may not balance | No |
| `POSTED` | Final and immutable | Yes |
| `REVERSED` | Posted, then cancelled by a mirror transaction | **Yes** |

`REVERSED` still counts. The original and its mirror cancel arithmetically. Show
a reversed transaction struck through with a link to its reversal — do not hide
it, and do not subtract it yourself.

---

## 3. Ledger endpoints

| Method | Path | Permission |
| --- | --- | --- |
| GET | `/ledger/accounts` | `ledger.read` |
| POST | `/ledger/accounts` | `ledger.account_manage` |
| GET | `/ledger/accounts/{accountId}` | `ledger.read` |
| GET | `/ledger/accounts/{accountId}/statement` | `ledger.read` |
| GET | `/ledger/journals` | `ledger.read` |
| POST | `/ledger/journals` | `ledger.post` |
| GET | `/ledger/journals/{journalId}` | `ledger.read` |
| POST | `/ledger/journals/{journalId}/reverse` | `ledger.reverse` |
| GET | `/ledger/trial-balance` | `ledger.read` |
| GET | `/ledger/health` | `ledger.read` |
| GET | `/ledger/periods` | `ledger.read` |
| POST | `/ledger/periods/{periodId}/close` | `ledger.period_manage` |
| POST | `/ledger/periods/{periodId}/reopen` | `ledger.period_manage` |

**`GET /ledger/health`** returns `200` with `{"balanced": true|false, ...}`.
A `false` with a `200` is not a bug: you asked a question and got a truthful
answer. Surface it as a red banner on the finance dashboard; it means the books
are inconsistent and is a production incident.

**`GET /ledger/accounts/{id}/statement`** returns a running balance computed in
the database. Do not accumulate it client-side — ordering and rounding must match
the server.

**Accounting periods** gate posting. A closed period refuses everything with
`409 PERIOD_CLOSED`. Reopening requires a reason of at least 10 characters and
is loudly audited; treat it as a destructive action in the UI.

### Default chart of accounts

Seeded for every organization. These codes are referenced from server code and
will not change.

`1000` Cash and Bank · `1100/1110/1120` COD Receivable from agents / branches /
franchises · `1200` Trade Receivable · `1300` Franchise Receivable · `1400` COD
Shortage Recoverable · `2000` COD Payable to Consignors · `2100` Commission
Payable · `2200` Franchise Payable · `2300` Tax Payable · `2310` Withholding Tax
Payable · `2500` COD Excess Payable · `3000/3100` Equity · `4000` Freight
Revenue · `4100` Surcharge Revenue · `4200` COD Fee Revenue · `4300` Other
Revenue · `5000` Commission Expense · `5100` Incentive Expense · `5200` COD
Shortage Written Off · `5300` Discount Allowed

**`2100` versus `2200` is the distinction to understand.** Commission *earned*
accrues to `2100`. Only when a settlement containing it is approved does it move
to the franchise's own `2200` subsidiary. So a franchise's payable balance shows
**agreed, settled** amounts — not everything they have earned this month. To show
"earned so far", read commission calculations, not the ledger balance.

---

## 4. Commission

### How a rule is chosen

Every rule has a **scope**: franchise, franchise category, operating unit,
service, zones, customer category, payment mode. Any of these may be unset,
meaning "any". When several rules match, the winner is decided by a **total
order** so the answer is never ambiguous:

1. **specificity** — how many scope dimensions are bound (computed by the
   database, weighted: franchise 32, unit 16, category 8, service 4, zone 2,
   customer category / payment mode 1)
2. **priority** — a manual tiebreak, higher wins
3. **id** — oldest wins, so adding a rule never silently displaces an
   equally-specific existing one

`POST /commission/simulate` returns the full candidate list with `selected: true`
on the winner and each one's specificity. **Show this.** A franchise that can see
*why* a rule applied argues about policy; one that cannot argues about the number.

### Methods

| Method | Fields | Example |
| --- | --- | --- |
| `FIXED` | `fixedAmountMinor` | ₹42.50 per delivery → `4250` |
| `PERCENTAGE` | `rateBp`, `basis` | 12% of freight → `rateBp: 1200`, `basis: "FREIGHT"` |
| `SLAB` | `slabs[]`, `basis` | banded, see below |

Rates are **basis points**: `1200` = 12%, `900` = 9%, `50` = 0.5%. Never send a
percentage as a float.

`basis` names which number the rate applies to, and this is worth surfacing in
the rule editor because it is the most common source of dispute:
`FREIGHT`, `SURCHARGE`, `FREIGHT_PLUS_SURCHARGE`, `TOTAL_BEFORE_TAX`, `TOTAL`,
`COD_AMOUNT`, `CHARGEABLE_WEIGHT`, `SHIPMENT_COUNT`.

Slab bands must be **contiguous and start at 0**, and the last must be
open-ended. The API rejects a table with a gap at configuration time rather than
silently paying zero later:

```json
"slabs": [
  {"fromMinor": 0,      "toMinor": 50000,  "amountMinor": 1000, "label": "up to ₹500"},
  {"fromMinor": 50000,  "toMinor": 200000, "amountMinor": 2500, "label": "₹500–₹2000"},
  {"fromMinor": 200000, "toMinor": null,   "amountMinor": 5000, "label": "above ₹2000"}
]
```

A band boundary belongs to the **upper** band: a basis of exactly `50000` pays
`2500`.

### Versions

Rates change by adding a **version** with a later `effectiveFrom`, never by
editing. A version that has produced a calculation is frozen — the database
refuses to alter it. This is what guarantees historical commission is stable:
a recalculation for a March delivery finds March's version, not today's.

Show the version history on the rule detail screen with effective dates. An
operator editing a rate needs to understand they are creating a version.

### Calculations

A calculation stores everything needed to explain itself: the rule version, the
inputs, and a step-by-step `calculationTrace`. Render the trace verbatim on the
detail screen:

```json
{
  "method": "PERCENTAGE", "basis": "FREIGHT", "basisValue": 50000,
  "rateBp": 1200, "grossAmountMinor": 6000, "amountMinor": 6000,
  "clamped": false,
  "trace": [
    {"description": "Basis FREIGHT", "value": 50000},
    {"description": "Apply 12% of basis", "value": 6000,
     "detail": "50000 × 1200 bp, rounded half-up"}
  ]
}
```

`clamped: true` means a minimum or maximum changed the answer; `grossAmountMinor`
holds the pre-clamp figure. Show both, or the number looks like an error.

Rounding is **half-up** everywhere, matching the pricing engine.

### Statuses

`CALCULATED` → `POSTED` → `REVERSED`, or `CANCELLED` from `CALCULATED`.

Only `CALCULATED` can be cancelled. A `POSTED` commission is corrected by
reversal, which requires a reason of at least 10 characters and creates a
negative entry so the franchise's history shows both the earning and its removal.

---

## 5. COD

### The custody chain

COD is a custody problem before it is an accounting problem. Cash moves:

```
consignee → agent → branch → franchise → head office → consignor
```

and **at every hop somebody is liable for it**. The obligation's `status` says
where it is; `custodianType` and `custodianId` say who holds it.

| Status | Who holds the cash |
| --- | --- |
| `EXPECTED` | Nobody — not yet collected |
| `AGENT_COLLECTED` | The delivery agent |
| `BRANCH_RECEIVED` | The branch |
| `FRANCHISE_CONFIRMED` | The franchise |
| `RECONCILED` | Counted and agreed, still held |
| `REMITTED` | Sent onward |
| `CLOSED` | Complete |
| `CANCELLED` | Shipment cancelled before collection |
| `WRITTEN_OFF` | Irrecoverable |

### Hand-offs are two-sided

A custody transfer has two halves and both are required:

1. `POST /cod/transfers` — the giver **declares** what they are handing over
2. `POST /cod/transfers/{id}/accept` — the receiver **counts** and accepts

Until accepted, the money is still the giver's liability. That is deliberate: an
unaccepted hand-in is exactly the dispute a real network argues about, and the
UI should make the pending state visible to both sides.

If `acceptedMinor` differs from `declaredMinor`, a `varianceReason` is
**required** — the API returns `422` without one. A shortfall does not vanish; it
moves to COD Shortage Recoverable against the party that declared it, so somebody
remains accountable.

### Reconciliation

`POST /cod/reconciliations` opens a count against one party and seeds an item per
obligation they hold. Only **one open reconciliation per party** — a second
returns `409`, because two simultaneous counts of the same cash would each see
the other's uncounted rows and both look short.

Count each line with `/count`, then `/complete`. The result is `COMPLETED` or
`COMPLETED_WITH_VARIANCE`; a variance needs a reason and posts the shortfall or
excess to the ledger.

### Custody position

`GET /cod/custody/{partyType}/{partyId}` returns the holding derived **twice**:

```json
{
  "partyType": "OPERATING_UNIT", "partyId": "12",
  "heldMinor": 200000,           // from the obligation rows
  "ledgerBalanceMinor": 200000,  // from the ledger
  "reconciled": true
}
```

`reconciled: false` means the operational record and the books have diverged.
That is a finance incident, not a rounding difference — surface it prominently.

### Adjustments need two people

`POST /cod/adjustments` raises a correction; it moves no money. `POST
/cod/adjustments/{id}/approve` posts it, and **the approver must be someone
else**. The API returns `403` if the same user tries both. Reason is required,
minimum 10 characters.

Types: `SHORTAGE_RECOVERY`, `SHORTAGE_WRITE_OFF`, `EXCESS_REFUND`,
`EXCESS_RETAINED`, `WAIVER`, `CORRECTION`, `DISPUTE_RESOLUTION`.

### Disputes

A dispute parks a contested amount **without blocking the rest**. Other shipments
in the same hand-in still settle. One open dispute per obligation.

### Idempotency

`POST /cod/collections` is the most-retried finance call in the platform — it
runs on a handheld, at a doorstep, over a mobile network. Send
`X-Device-Id` and `X-Device-Event-Id` headers. A replay returns `200` with
`"duplicate": true` and the **original** collection. Do not treat that as an
error; it means the money was banked exactly once.

---

## 6. Settlement

A settlement is the period statement between head office and one franchise.

### States

```
DRAFT → CALCULATED → UNDER_REVIEW → APPROVED → PARTIALLY_PAID → PAID → CLOSED
                 └──────────────────→ CANCELLED (only before approval)
```

| State | Editable? | What can happen |
| --- | --- | --- |
| `DRAFT`, `CALCULATED` | Yes | recalculate, submit, cancel |
| `UNDER_REVIEW` | Yes | approve, reject (back to `CALCULATED`), cancel |
| `APPROVED` | **No** | pay, adjust |
| `PARTIALLY_PAID` | **No** | pay, adjust |
| `PAID` | **No** | close |
| `CLOSED`, `CANCELLED` | **No** | nothing |

**An approved settlement never recalculates.** `POST /{id}/recalculate` returns
`409 SETTLEMENT_FROZEN`. Corrections are `settlement_adjustments` — additive
rows with their own maker/checker. Disable the recalculate button from `APPROVED`
onward and offer "raise an adjustment" instead.

### Reproducibility

Recalculating over unchanged sources produces **identical** lines and an
identical `calculationHash`. If you show the hash, a franchise can verify their
statement did not change between two viewings.

Every line carries `sourceType` and `sourceId`. Make lines clickable through to
the commission calculation or COD obligation that produced them — the data is
there specifically so a franchise can drill into any number.

### The net can be negative

`netAmountMinor` positive means head office owes the franchise. **Negative means
the franchise owes head office**, which is normal: COD they are holding reduces
what they are owed. A franchise that collected more COD than it earned in
commission owes money back.

Line categories: `BOOKING_COMMISSION`, `PICKUP_COMMISSION`, `ORIGIN_HANDLING`,
`DESTINATION_HANDLING`, `DELIVERY_COMMISSION`, `COD_COMMISSION`,
`VOLUME_INCENTIVE`, `CUSTOM_COMMISSION`, `COD_LIABILITY`, `CHARGE`, `PENALTY`,
`INCENTIVE`, `ADJUSTMENT`, `TAX`, `WITHHOLDING`, `OPENING_BALANCE`.

`GET /settlements/{id}` returns `byCategory` totals alongside the lines, so a
summary panel does not need client-side grouping.

### Generation is idempotent

`POST /settlements` for a franchise and period that already has one returns
`200` with `"replayed": true` rather than `201`. Nothing was created. Do not show
a "settlement created" toast on a replay.

---

## 7. Billing

### Draft then issue

Creating an invoice is two steps on purpose:

1. `POST /invoices` — builds a **draft** with lines, reviewable and discardable
2. `POST /invoices/{id}/issue` — allocates the statutory number and posts it

An abandoned draft does not burn an invoice number. Until issued,
`invoiceNumber` is a `DRAFT-…` placeholder — never show it as if it were real.

### Numbering

Numbers look like `INV/2026-27/000042` — prefix, financial year, zero-padded
sequence. The series restarts each financial year (April–March) and is **gapless**:
concurrent issuers get distinct consecutive numbers. Tax authorities care about
gaps, so never fabricate or reformat a number client-side.

### Lines are snapshots

An invoice line copies the amount, description, HSN/SAC code and tax **at the
moment of issue**. It never re-reads a rate card. Editing a rate card tomorrow
cannot change an invoice issued today.

An issued invoice is immutable — editing the total, number, date or lines is
refused by the database. Corrections are credit or debit notes.

Set `"summary": true` for one rolled-up line instead of one per shipment. Every
AWB is still recorded in `invoice_shipments`, so a detail view remains possible
for a 4,000-shipment month.

### Tax is configuration

The tax engine is generic. Indian GST is expressed as configured components:

```json
// intra-state
"taxes": [
  {"code": "CGST", "name": "Central GST", "rateBp": 900, "metadata": {"placeOfSupply": "29"}},
  {"code": "SGST", "name": "State GST",   "rateBp": 900, "metadata": {"placeOfSupply": "29"}}
]
// inter-state
"taxes": [
  {"code": "IGST", "name": "Integrated GST", "rateBp": 1800, "metadata": {"placeOfSupply": "07"}}
]
```

Nothing in the code special-cases GST. A different regime is a different set of
components.

### Statuses

`DRAFT` → `ISSUED` → `PARTIALLY_PAID` → `PAID`, plus `OVERDUE`, `CANCELLED`,
`WRITTEN_OFF`. An invoice becomes `PAID` when `paid + credited >= total`, so a
credit note can settle one without any payment.

### Credit and debit notes need two people

`POST /credit-notes` raises a draft; `POST /credit-notes/{id}/issue` approves,
numbers and posts it — and refuses if the same user raised it. Anything that
reduces a receivable needs a second person.

A credit note cannot exceed the outstanding amount (`409 CREDIT_NOTE_EXCEEDS_INVOICE`).

### Payment idempotency

`POST /invoices/{id}/payments` is idempotent on `reference`. A payment gateway
retrying a webhook with the same reference returns the original payment and does
not bank twice.

---

## 8. Maker/checker — what it is and is not

Five operations require two different people:

| Operation | Maker permission | Checker permission |
| --- | --- | --- |
| COD adjustment | `cod.adjust_request` | `cod.adjust_approve` |
| Settlement approval | `settlement.calculate` | `settlement.approve` |
| Settlement adjustment | `settlement.adjust_request` | `settlement.adjust_approve` |
| Credit / debit note | `creditnote.create` | `creditnote.approve` |
| Journal reversal | `ledger.post` | `ledger.reverse` |

**Holding both permissions is normal.** A `FINANCE_MANAGER` holds both halves of
every pair, because an organization may have one finance person. What is refused
is the *same human* acting on *both halves of one document*.

This means you cannot decide button visibility from permissions alone. A user
may hold `settlement.approve` and still be refused on a settlement they
calculated. Either:

- read `calculatedBy` / `requestedBy` from the record and compare to the current
  user, or
- show the button and handle `403` gracefully.

The second is safer — the rule is enforced server-side in three places (service
check, SQL `WHERE`, and a CHECK constraint), and the message explains itself.

---

## 9. Immutable states — what can never be edited

| Object | Frozen when | Correct by |
| --- | --- | --- |
| Journal transaction | `POSTED` | reversal |
| Journal entry | parent is `POSTED` | reversal |
| Commission rule version | it has produced a calculation | new version |
| Commission calculation | `POSTED` | reversal |
| COD obligation `expectedMinor` | always — fixed at booking | adjustment |
| COD collection | always — append-only | adjustment |
| Settlement totals and lines | `APPROVED` onward | settlement adjustment |
| Invoice header and lines | `ISSUED` | credit / debit note |
| Accounting period | `CLOSED` | reopen (audited) |

All of these are enforced by database triggers, not just application checks. An
attempt returns `409` with a code naming the object — never assume a `PATCH` will
work because a field is visible in a response.

### Corrective actions available

| Situation | Action | Endpoint |
| --- | --- | --- |
| Wrong journal posted | reverse it | `POST /ledger/journals/{id}/reverse` |
| Commission paid in error | reverse it | `POST /commission/calculations/{id}/reverse` |
| COD short at hand-in | accept with variance, then adjust | `/cod/transfers/{id}/accept`, `/cod/adjustments` |
| COD unrecoverable | shortage write-off | `POST /cod/adjustments` (needs approval) |
| Settlement wrong before approval | recalculate or cancel | `/recalculate`, `/cancel` |
| Settlement wrong after approval | adjustment | settlement adjustment |
| Invoice overstated | credit note | `POST /credit-notes` |
| Invoice understated | debit note | `POST /credit-notes` with `noteType: "DEBIT"` |

---

## 10. Error codes

| Code | HTTP | Meaning |
| --- | --- | --- |
| `LEDGER_UNBALANCED` | 409 | Debits ≠ credits. Details carry both totals and the difference. |
| `PERIOD_CLOSED` | 409 | Posting into a closed period, or no period covers the date |
| `LEDGER_ACCOUNT_MISSING` | 409 | Account code not in the chart |
| `LEDGER_ALREADY_REVERSED` | 409 | Journal already reversed |
| `COMMISSION_NO_RULE` | 409 | No active rule matches |
| `COMMISSION_NO_VERSION` | 409 | Rule has no version in force on that date |
| `COMMISSION_INVALID_SLABS` | 409 | Gap, overlap, or unbounded middle band |
| `COMMISSION_ALREADY_POSTED` | 409 | Already posted |
| `COD_ALREADY_COLLECTED` | 409 | Already collected — check `duplicate` first |
| `COD_WRONG_CUSTODIAN` | 409 | This party is not holding it |
| `COD_INVALID_STATE` | 409 | Not in a state that allows this |
| `COD_IMMUTABLE` | 409 | Expected amount is fixed at booking |
| `SETTLEMENT_FROZEN` | 409 | Approved; raise an adjustment |
| `SETTLEMENT_OVERPAYMENT` | 409 | Exceeds outstanding; details carry the remainder |
| `SETTLEMENT_NOT_APPROVED` | 409 | Only approved settlements can be paid |
| `INVOICE_ALREADY_ISSUED` | 409 | Already issued |
| `INVOICE_OVERPAYMENT` | 409 | Exceeds outstanding |
| `CREDIT_NOTE_EXCEEDS_INVOICE` | 409 | More than outstanding |
| `BILLING_NOTHING_TO_BILL` | 409 | No unbilled shipments in the period |
| `FORBIDDEN` | 403 | Missing permission, **or** maker/checker violation |

Standard envelope throughout:

```json
{"error": {"code": "LEDGER_UNBALANCED",
  "message": "The transaction does not balance: debits 10000, credits 6000.",
  "details": {"totalDebitMinor": 10000, "totalCreditMinor": 6000, "differenceMinor": 4000},
  "requestId": "req_01K..."}}
```

Messages are written for an operator to read. Show them.

---

## 11. Pagination and filtering

Every list paginates. Registers use `limit` (default 50, max 200) with a
`cursor` of the last id seen; configuration lists use `limit`/`offset` and return
`total`.

Common filters: `status`, `franchiseId`, `customerId`, `from`/`to` (`YYYY-MM-DD`).
Dates in request bodies are `YYYY-MM-DD`; timestamps in responses are RFC 3339.

---

## 11A. Correcting a settled statement

An approved settlement cannot be recalculated: its net is in the ledger and the
franchise has been told what they are owed. §26 requires an explicit adjustment,
and this is it.

```
POST /api/v1/settlements/{id}/adjustments        settlement.adjust_request
POST /api/v1/settlement-adjustments/{id}/approve settlement.adjust_approve
POST /api/v1/settlement-adjustments/{id}/reject  settlement.adjust_approve
GET  /api/v1/settlement-adjustments              the approval queue
```

**Raising moves no money.** A different person decides it — enforced in the
service, in the UPDATE's `WHERE`, and by the
`settlement_adjustments_maker_is_not_checker` CHECK. Rejection is held to the
same rule: quietly withdrawing your own correction leaves the same hole in the
record as approving it, and a rejection carries its own reason.

**Where the money lands depends on the statement's state**, and this is the part
a client has to understand:

| Settlement status | On approval | Adjustment status |
|---|---|---|
| DRAFT, CALCULATED, UNDER_REVIEW | folded into the next recalculation; the settlement's own approval posts the net including it | `APPROVED` |
| APPROVED, PARTIALLY_PAID, PAID, CLOSED | posts its own balanced journal immediately | `APPLIED` |
| CANCELLED | refused — it never became anybody's money | — |

The response's `status` and `posted` fields say which happened. Doing both would
count the correction twice, which is why the two paths are mutually exclusive
rather than "post and also include".

Amounts are signed minor units: positive increases what the network owes the
franchise, negative reduces it. Zero is refused.

---

## 12. What is not in this release

Reviewed 9 August 2026, after Release 4. Three of the original entries have
since been delivered and are struck out below rather than deleted, because a
frontend that read this contract earlier was told they were missing.

- **No PDF rendering.** `documentObjectId` on an invoice will be null. Generation
  is asynchronous and comes later; the contract will not change.
- **No billing run endpoint.** The schema and queries exist; the bulk sweep is
  driven per customer for now. *Still true.*
- ~~**No customer-facing portal endpoints.**~~ **Delivered in Release 4 (M27).**
  A customer reads their own invoices at
  `GET /api/v1/portal/customer/invoices`, scoped by the `customer_users` link.
  See [`release-4.md`](release-4.md) §3B.
- ~~**No notifications.**~~ **Delivered in Release 4 (M26).** Shipment
  milestones raise customer notifications through the transition engine.
  Invoices still do not: `INVOICE_ISSUED` has a seeded template but nothing
  raises it, so an invoice still reaches a customer only if somebody sends it.
- **No automatic commission on delivery.** Commission is calculated when called;
  the delivery → commission hook is not yet wired, so an operator or job triggers
  it. *Still true.*
- ~~**No settlement adjustment endpoint.**~~ **Delivered.** `Generate` used to
  tell an operator "raise an adjustment instead" and point at a route that did
  not exist. The workflow is now
  `POST /api/v1/settlements/{id}/adjustments` (maker) and
  `POST /api/v1/settlement-adjustments/{id}/approve` (checker) — see §11A.
