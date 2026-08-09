# Release 3 — Franchise Finance, COD and Settlement

Date: 2026-08-08
Modules: M21 Commission · M22 Ledger · M23 COD · M24 Settlement · M25 Billing

Contract: [`docs/contracts/release-3.md`](../contracts/release-3.md)

This release makes the backend the authoritative record of who owes whom.
Release 2 moved parcels; Release 3 moves the money that follows them, and does
so under double-entry accounting where every figure is derived from immutable
rows rather than stored in an editable field.

---

## 1. The governing decision

**There is no balance column anywhere in Release 3.**

Not on a franchise, not on a customer, not on a COD obligation, not on an
invoice. Every monetary figure this release reports is a `SUM()` over journal
entries or over source rows, computed at read time.

That is the Constitution's requirement (§23, §25) taken literally, and it is
what the rest of the design falls out of:

- correcting a mistake means posting its opposite, never editing history;
- "what does this franchise owe" can be asked as of any past date, because it is
  a query rather than a snapshot;
- two independently-derived views of the same fact can be compared, which is how
  `GET /cod/custody/...` catches divergence between operations and the books.

---

## 2. What works

Verified against a running API on a seeded tenant:

```
 1. chart of accounts   [200] 25 accounts
 2. accounting periods  [200] 1 period(s), first=2026-08
 3. manual journal      [201] JV-202608-000001 total=500000
 4. unbalanced refused  [409] LEDGER_UNBALANCED
 5. trial balance       [200] balanced=True D=500000 C=500000
 6. ledger health       [200] balanced=True unbalanced=0
 7. reversal            [200] JV-202608-000002
 8. after reversal      [200] balanced=True D=1000000 C=1000000
 9. commission simulate [200] matched=False — No active DELIVERY rule matches
14. unauthenticated     [401] UNAUTHORIZED
```

Step 8 is the one to read carefully: after a reversal *both* transactions remain
in the books and the cash account nets to zero. A reversal cancels
arithmetically, not by deletion.

A `DELIVERY_AGENT` is refused with `403` on posting a journal, reading the trial
balance, approving a settlement and issuing an invoice.

| Module | Capability |
| --- | --- |
| M22 Ledger | Chart of accounts with auto-created subsidiary accounts, balanced posting, reversal, account statement with running balance, trial balance, accounting periods, whole-ledger health check |
| M21 Commission | Versioned schemes and rules, deterministic precedence, FIXED/PERCENTAGE/SLAB across all eight commission types, calculation with full arithmetic trace, idempotent posting, simulation sharing the real resolver |
| M23 COD | Obligation per COD shipment, doorstep collection, two-sided custody hand-offs, counted reconciliation with shortage and excess, remittance to head office or consignor, maker/checker adjustments, disputes |
| M24 Settlement | Reproducible period statements, line-level provenance, maker/checker approval, part and full payment, cancellation releasing commission, adjustments after approval |
| M25 Billing | Draft-then-issue invoicing, gapless financial-year numbering, snapshot lines, summary invoices, configurable tax, credit and debit notes, idempotent payments |

---

## 3. Sample accounting flows

Every flow below is what the code actually posts. Amounts are minor units.

### 3.1 Prepaid shipment — invoice issued

A business customer's ₹500 shipment with ₹75 surcharge and 18% IGST.

```
Invoice INV/2026-27/000001                                   total 67,850

DR  1200  Trade Receivable — customer 4        67,850
    CR  4000  Freight Revenue                              57,500
    CR  2300  Tax Payable                                  10,350
```

The customer owes ₹678.50; revenue is recognised net of tax; the authority is
owed the GST. Customer payment then clears the receivable:

```
DR  1000  Cash and Bank                        67,850
    CR  1200  Trade Receivable — customer 4               67,850
```

### 3.2 COD shipment — cash taken at the door

A ₹2,000 COD parcel delivered.

```
DR  1100  COD Receivable — agent 9            200,000
    CR  2000  COD Payable to Consignors                  200,000
```

Two things are true at once and both matter: the **agent owes the network** the
cash they are carrying, and the **network owes the consignor** their money. The
second is independent of where the cash currently sits — it is owed from the
moment it is taken, whoever happens to be holding it.

### 3.3 Origin franchise commission — booking

A franchise books the shipment and earns 12% of freight.

```
Commission calculation, rule BOOK-STD v1, PERCENTAGE 1200bp of FREIGHT
  basis 50,000 × 12% = 6,000

DR  5000  Commission Expense                    6,000
    CR  2100  Commission Payable                            6,000
```

The credit goes to the **accrual** account, not to the franchise's own payable.
That distinction is the whole point of settlement: earning is not the same as
owing an agreed amount.

### 3.4 Destination franchise commission — delivery

The delivering franchise earns a flat ₹42.50.

```
Commission calculation, rule DEL-STD v1, FIXED 4,250

DR  5000  Commission Expense                    4,250
    CR  2100  Commission Payable                            4,250
```

Identical shape, different rule, different recipient. Both accrue to `2100` and
wait for a settlement.

### 3.5 COD remittance — branch banks the cash

First the agent hands in to the branch:

```
DR  1110  COD Receivable — branch 12          200,000
    CR  1100  COD Receivable — agent 9                    200,000
```

Then the branch remits to head office:

```
DR  1000  Cash and Bank                       200,000
    CR  1110  COD Receivable — branch 12                  200,000
```

The branch is now clear. Note what has *not* happened: `2000 COD Payable to
Consignors` is untouched. Head office holding the cash is not the same as the
sender having been paid. Paying them is a separate posting:

```
DR  2000  COD Payable to Consignors           200,000
    CR  1000  Cash and Bank                              200,000
```

If a shortage appears at hand-in — the branch counts ₹1,800 against a declared
₹2,000 — the difference does not evaporate:

```
DR  1110  COD Receivable — branch 12          180,000
DR  1400  COD Shortage Recoverable — agent 9   20,000
    CR  1100  COD Receivable — agent 9                    200,000
```

Somebody remains accountable for the missing ₹200 until an approved adjustment
recovers or writes it off.

### 3.6 Settlement payment — franchise paid

The month's accrued commission is settled for a franchise. On approval:

```
Settlement STL-202608-00001, net 10,250 (6,000 booking + 4,250 delivery)

DR  2100  Commission Payable                   10,250
    CR  2200  Franchise Payable — franchise 7             10,250
```

The accrual becomes an agreed obligation to this specific franchise. Payment
then discharges it:

```
DR  2200  Franchise Payable — franchise 7      10,250
    CR  1000  Cash and Bank                                10,250
```

A part payment posts the same shape for the partial amount and moves the
settlement to `PARTIALLY_PAID`.

When the franchise is also holding COD, that reduces the net — and can invert
it. A franchise holding ₹200,000 of COD against ₹10,250 of commission has a net
of −189,750, meaning **they owe head office**:

```
DR  1300  Franchise Receivable — franchise 7  189,750
    CR  2100  Commission Payable                          189,750
```

and the payment runs inbound rather than outbound.

### 3.7 Journal reversal — correcting a mistake

A journal posted against the wrong customer. The original stays exactly as it
was; a mirror cancels it.

```
Original  JV-202608-000001
DR  1000  Cash and Bank                        50,000
    CR  4000  Freight Revenue                              50,000

Reversal  JV-202608-000002  (reverses JV-202608-000001)
DR  4000  Freight Revenue                      50,000
    CR  1000  Cash and Bank                                50,000
```

The original is marked `REVERSED` and its `reversedById` points at the mirror.
**Both remain in the books** — the trial balance shows 100,000 on each side and
the cash account nets to zero. An auditor sees the mistake and the correction,
not a rewritten past.

---

## 4. Schema

Six migrations, `0020`–`0025`, adding **34 tables**: ledger 4, commission 5,
COD 10, settlement 5, billing 10. Totals after Release 3: **130 tables, 650
indexes, 125 triggers, 723 check constraints, 148 permissions** (36 new).

Migration ladder verified: 25 apply → 25 roll back (only `schema_migrations`
remaining) → 25 reapply → checksums validate.

### The invariant is enforced in the database

The balance rule is a **deferred constraint trigger**, checked at `COMMIT`. It
fires for any writer — application, migration, `psql` — so an unbalanced
transaction cannot reach disk. Nine adversarial attacks written directly in SQL,
all refused:

| Attack | Result |
| --- | --- |
| POSTED transaction, debits 100 vs credits 60 | `LEDGER_UNBALANCED` |
| Entry with both debit and credit set | `journal_entries_one_side` |
| Single-legged POSTED transaction | `LEDGER_UNBALANCED` |
| Mixed currency in one transaction | `LEDGER_CURRENCY_MISMATCH` |
| Edit a posted journal's total | `JOURNAL_IMMUTABLE` |
| Delete a posted journal | `JOURNAL_IMMUTABLE` |
| Edit or delete a posted entry | `JOURNAL_IMMUTABLE` |
| Post into a closed period | `PERIOD_CLOSED` |
| Overlapping accounting periods | exclusion constraint |
| *A correctly balanced transaction* | *commits* |

Maker/checker is enforced three times over — service check, SQL `WHERE`, and a
CHECK constraint — so a single person cannot approve their own COD adjustment,
settlement or credit note however they reach the database. Raw SQL setting
`approved_by = requested_by` is refused.

---

## 5. Files

Hand-written Go, excluding generated code and tests:

```
internal/ledger        2 files   1,240 lines
internal/commission    3 files   1,297 lines
internal/cod           2 files   1,873 lines
internal/settlement    1 file    1,033 lines
internal/billing       1 file    1,022 lines
internal/financeapi    6 files   1,782 lines

db/queries (5 files)             2,006 lines
db/migrations 0020-0025          2,689 lines
tests                            2,920 lines
```

56 test functions across unit and integration suites.

---

## 6. API surface

**54 endpoints** across five modules, mounted under `/api/v1`:

| Module | Endpoints |
| --- | --- |
| COD | 16 |
| Ledger | 13 |
| Commission | 9 |
| Settlement | 8 |
| Billing | 8 |

Three worth calling out:

- **`GET /ledger/health`** — the balance invariant as a monitorable check.
  Returns `200` with `balanced: false` when the books are inconsistent, because
  the caller asked a question and deserves a truthful answer.
- **`GET /cod/custody/{partyType}/{partyId}`** — the holding derived twice, once
  from obligation rows and once from the ledger, with `reconciled` saying whether
  they agree. Two code paths, cross-checked.
- **`POST /commission/simulate`** — runs the *same* resolver and the *same*
  calculator as a real posting, and returns the full candidate rule list with the
  winner marked. A preview cannot disagree with what will be paid.

---

## 7. Test evidence

Full suite under `-race`:

```
ok  internal/commission              1.598s
ok  internal/ledger                  1.598s
ok  internal/platform/money          1.924s
ok  internal/platform/storage        2.363s
ok  internal/pricing                 2.363s
ok  internal/shipment                2.875s
ok  tests/contract                   4.772s
ok  tests/integration              197.304s
```

`gofmt` clean, `go vet` clean, no skipped tests.

### The tests the specification names

| Required | Where | Result |
| --- | --- | --- |
| Commission rule precedence | `TestSlabSelectionIsIndependentOfConfigurationOrder`, resolver ordering | Pass |
| Commission version | `TestSettlementIsReproducible` (version pinning), rule-version guard | Pass |
| Duplicate posting | `TestPostingIsIdempotentOnItsNaturalKey` | Pass |
| Ledger invariants | `TestBalanceInvariantHoldsForRandomPostings` — 20,000 random postings, 9,924 balanced / 10,076 unbalanced | Pass |
| Journal reversal | `TestReversalLeavesTheOriginalIntactAndNetsToZero` | Pass |
| COD custody | `TestCODCustodyChainKeepsBooksAndOperationsInStep` | Pass |
| Settlement reproducibility | `TestSettlementIsReproducible` — identical lines, net and hash | Pass |
| Settlement concurrency | `TestConcurrentSettlementGenerationProducesOne` — 8 concurrent, 1 statement, 1 line | Pass |
| Invoice sequence concurrency | `TestInvoiceNumberingIsConcurrencySafe` — 12 concurrent issues, 12 distinct numbers, **no gaps** | Pass |
| Tenant isolation | Four tests, one per module | Pass |
| Finance permissions | `TestCODAdjustmentRequiresASecondPerson`, `TestApprovedSettlementCannotSilentlyRecalculate`, live 403 checks | Pass |
| Adversarial duplicate retries | `TestConcurrentIdenticalPostingsPayOnce` — 12 concurrent, 1 transaction, franchise paid once | Pass |

---

## 8. Bugs the tests found

Five real defects, all caught by tests rather than by review.

**1. Reversal double-counted the removal.** Balance queries filtered
`status = 'POSTED'`, excluding a reversed original *while counting its mirror* —
subtracting the amount twice and leaving cash at the negative of where it
started. Fixed: reversed transactions stay in the books. Only `DRAFT` is out.

**2. Commission credited the wrong account.** It credited `2200 Franchise
Payable` directly, skipping the `2100` accrual. Settlement approval then
correctly did `DR 2100 / CR 2200`, crediting the franchise a **second** time — a
test showed 8,500 owed where 4,250 was correct. Fixed: commission accrues to
`2100`, and settlement is what moves it to the franchise.

**3. Recalculating a settlement destroyed it.** The sweep excluded commission
already attached to a settlement, including the one being rebuilt, so a
recalculation produced an **empty** statement — net 12,750 became 0.

**4. Tenant codes collided.** `publicid.New("x")[2:8]` takes the ULID's
*timestamp* prefix; two tenants created in the same millisecond clashed. Same
bug class as Release 2's `RandomKey`.

**5. `money.Currency.Valid()` was a shape check** — three uppercase letters —
despite a doc comment saying "supported". `XYZ` would have entered the ledger and
been summed as money. Added `Supported()` and used it in the ledger rather than
changing existing semantics.

Two smaller ones: invoice sequence rows seeded with `scope_key=''` were dead
configuration (allocation scopes by financial year), now read as the
configuration template; and provisioning did not seed finance for *new* tenants,
the same gap Release 2 hit with NDR reasons.

---

## 9. Deployment notes

Migrations `0020`–`0025` are additive. `0020` installs the `btree_gist`
extension for the accounting-period exclusion constraint.

```bash
./migrate up
```

New tenants get their chart of accounts, an open accounting period, a default
commission scheme and invoice numbering series from `provision.Bootstrap`.
Existing organizations were seeded by the migrations.

No new configuration is required. Withholding tax is off by default
(`settlement.Service.WithholdingBp = 0`); set it per deployment if the
jurisdiction requires it.

---

## 10. Known risks and limitations

**Not implemented.**

- *Automatic commission on delivery.* Commission is calculated when called. The
  delivery → commission hook is not wired, so an operator or a job must trigger
  it. This is the most significant gap: the engine is complete and proven, but
  nothing calls it automatically yet.
- *PDF rendering.* Invoices and settlement statements have a
  `documentObjectId` field and it is always null. Generation is designed to be
  asynchronous through the existing job queue and object storage.
- *Bulk billing run.* The `billing_runs` table, queries and status machine exist;
  the sweep is driven per customer.
- *COD write-off automation and ageing.* Adjustments are manual.
- *Notifications.* Nothing is sent to anyone. An invoice reaches a customer only
  if a human sends it.

**Risks to watch.**

- *Finance has no load testing.* Release 2's operational endpoints were measured
  under k6; the finance endpoints have not been. The posting path takes row locks
  and does several writes per call, so settlement generation across a large
  franchise network is the one to profile first.
- *No EXPLAIN evidence for finance queries.* The indexes were designed against
  the access patterns, but unlike Release 2 there is no captured query-plan
  document. `SweepCommissionForSettlement` and `GetCustomerOutstanding` are the
  candidates to check at volume.
- *The accrual/settlement distinction is subtle.* Bug 2 above was a design error
  that produced plausible-looking numbers. Anyone extending commission or
  settlement should read §3.6 first — crediting `2200` from a new code path would
  reintroduce it.
- *Withholding is applied to earnings only.* COD held by a franchise is their
  float, not income, so it is excluded from the withholding base. That is a
  judgement about intent; confirm it against the applicable tax treatment before
  enabling withholding in production.
- *A reopened accounting period changes reported numbers.* It is audited and
  requires a 10-character reason, but there is no second-person approval on it.

---

## 11. Readiness

Release 3 is functionally complete against its specification. All five modules
are implemented, reachable over HTTP, and evidenced by 56 tests including
property-based ledger invariants, concurrency tests on all three
sequence-allocating paths, and adversarial SQL against every immutability guard.

It is not production-ready. Between here and a finance go-live:

1. **Wire commission to delivery**, or commission accrues only when someone
   remembers to ask for it.
2. **Load-test the posting path** and capture query plans, as Release 2 did.
3. **Have an accountant read §3.** The flows are internally consistent and the
   invariants hold, but "the books balance" and "the books are right for this
   business" are different claims, and only the first is proven here.
4. **Decide the withholding treatment** before enabling it.

None of the four is a defect in what was built; each is work not yet done.
