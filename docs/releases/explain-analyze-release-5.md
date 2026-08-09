# EXPLAIN ANALYZE — Release 5 mixed workload

Re-measured 9 August 2026 against the dataset the production-gate load profile
actually drives, rather than against the per-release datasets used earlier.

Dataset: **72,793 shipments, 60,000 COD obligations, 38,439 scan events,
12,993 shipment events — 358 MB**, on PostgreSQL 16 with the production
`postgresql.prod.conf`.

---

## 1. Results

Every query the mixed workload drives, measured with a realistic `LIMIT`.

| Query | Plan | Execution |
|---|---|---|
| `GetShipmentByAWB` | Index Scan `shipments_awb_prefix_idx` | **0.022 ms** |
| Public tracking by AWB | Index Scan `shipments_awb_prefix_idx` | **0.079 ms** |
| Shipment timeline | Index Scan `shipment_events_occurred_idx` | **0.072 ms** |
| `ListShipments` (20, no filter) | Index Scan `shipments_org_created_idx` | **0.152 ms** |
| `ListShipmentEvents` | Index Scan | **0.102 ms** |
| `ListCODObligations` (20) | Index Scan `cod_obligations_pkey`, join by pkey | **0.060 ms** |
| Scan events, latest 20 | Index Scan | **0.044 ms** |
| Live backlog aggregate | **Index Only Scan** `shipments_live_backlog_idx` | **5.671 ms** |
| Place search, name | GIN trigram, limit pushed down | **0.373 ms** *(measured at 20k postcodes)* |

Nothing needed a new index. Nothing exceeded 6 ms.

The slowest is the command-centre backlog aggregate at 5.7 ms, and it is slowest
for a legitimate reason: it counts every live shipment, so it scales with the
backlog rather than with the page size. It is an **index-only scan** over the
partial index built for exactly this query, so it reads 3,184 pages instead of
the table. At ten times the backlog it is still well inside a dashboard's budget.

---

## 2. Corroboration from the load run

The single strongest piece of evidence is not in the table above.

Across the full saturation ramp — 72,948 requests at 241.6/s, six times the
expected mix — the `courier_db_slow_queries_total` counter stayed at **zero**.
No statement anywhere in the system crossed 250 ms, and no lock was ever
ungranted.

That covers every query the workload executes, including ones not listed here,
which is a stronger claim than a hand-picked set of plans.

---

## 3. A finding that was not one

Worth recording, because the first version of this document nearly shipped with
a fabricated defect in it.

`ListCODObligations` initially measured at **39.7 ms** with parallel sequential
scans over both `cod_obligations` (60k) and `shipments` (72k). That looked like a
real problem: a keyset-paginated finance list doing two full scans, growing
linearly, and already the slowest query in the system.

The diagnosis was that `ORDER BY o.id DESC` had no supporting index, and the fix
was going to be `cod_obligations (organization_id, id DESC)`.

**Both were wrong.** The harness that extracted the query from `db/queries/`
substituted every sqlc placeholder with `NULL`, including `LIMIT $2` — so it was
measuring `LIMIT NULL`, which means *no limit*, which means the planner
correctly produced all 60,000 rows. With the real `LIMIT 20` the query runs in
**0.060 ms**, and `cod_obligations_pkey` already serves `ORDER BY id DESC`
perfectly well.

The candidate index was created, measured against the correct query, shown to
make no difference at all (0.059 ms against 0.060 ms), and dropped. No migration
was added.

Two things to take from it:

- **An index added on the strength of a bad measurement is permanent cost for no
  benefit** — write amplification on every insert, more pages in cache, a slower
  `VACUUM`. The constitution's "do not add indexes blindly" is about this exact
  failure, and it nearly happened here.
- **A test harness that fakes its inputs measures the harness.** `LIMIT NULL` is
  not a plausible production query; nobody would write it. The substitution was
  convenient and it produced a number that looked meaningful and was not.

---

## 4. What this does not cover

Stated plainly, because the same trap as §3 applies to absence of evidence.

`ListBags`, `ListManifests`, `ListSettlements` and `ListDeliveryRuns` were
measured against **empty tables** and their plans are worthless as evidence — a
sequential scan of zero rows tells you nothing about a sequential scan of
100,000. They are excluded from §1 rather than reported with flattering numbers.

Those tables fill up in operational use, so they should be re-measured against a
production-shaped dataset before the network is running at volume. The queries
themselves follow the same keyset pattern as `ListCODObligations`, which does
hold up at 60k rows, so the expectation is that they are fine — but that is a
reasoned expectation and not a measurement, which is exactly the distinction §3
is about.

---

## 5. Reproducing

```bash
# Seed a production-shaped dataset, then:
docker exec -i cos-pg psql -U courier -d courier_os -X <<'SQL'
EXPLAIN (ANALYZE, BUFFERS)
SELECT o.*, s.awb, s.current_status
FROM cod_obligations o JOIN shipments s ON s.id = o.shipment_id
WHERE o.organization_id = 1 ORDER BY o.id DESC LIMIT 20;
SQL
```

Pass the **same parameters the application passes**. A placeholder replaced with
`NULL` for convenience is how §3 happened.
