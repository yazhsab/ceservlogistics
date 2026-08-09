# EXPLAIN ANALYZE — operations command centre (M30)

Captured 9 August 2026 against PostgreSQL 16 in the project's container, on a
synthetic dataset of **200,000 shipments** and **401 rollup rows**, after
`VACUUM ANALYZE`.

The specification for this module is a performance requirement, not a feature
list: *"Do not run full-table aggregate scans on every request."* The command
centre is polled continuously by every supervisor in the network, so these plans
are the module's acceptance criteria.

Every measurement below was taken twice: once as first written, and once after
the fix. Both are shown, because the first number is why the second exists.

---

## 1. Period totals — the rollup

```sql
SELECT SUM(booked_count), SUM(delivered_count), SUM(revenue_minor)
FROM shipment_daily_stats
WHERE organization_id = $1
  AND stat_date BETWEEN CURRENT_DATE - 29 AND CURRENT_DATE;
```

```
Bitmap Heap Scan on shipment_daily_stats (actual rows=30 loops=1)
  -> Bitmap Index Scan on shipment_daily_stats_range_idx (actual rows=30)
     Index Cond: (organization_id = $0 AND stat_date >= CURRENT_DATE - 29
                                       AND stat_date <= CURRENT_DATE)
Execution Time: 0.077 ms
```

**30 rows read for a 30-day dashboard.** The counters are maintained by a trigger
in the same transaction as each status change, so this is exact rather than
eventually consistent, and its cost is a function of the window rather than of
the shipment table.

---

## 2. Live backlog by status

### As first written — a sequential scan

The query was `current_status = ANY(<17 live statuses>)`, relying on
`shipments_org_status_idx`:

```
Finalize GroupAggregate (actual time=17.072..19.385 rows=5)
  -> Parallel Seq Scan on shipments (actual rows=40000 loops=3)
Execution Time: 19.451 ms
```

A **parallel sequential scan of the whole table** — exactly what the module
exists to avoid, on its most-polled endpoint. Two things were wrong:

1. The predicate matched a large fraction of rows, so the planner correctly
   preferred a scan. That is a property of the data distribution, and a plan
   that depends on the distribution staying favourable is not a plan.
2. `= ANY(live statuses)` cannot be proved to imply a `NOT IN (terminal
   statuses)` index predicate, so a partial index would not have been used
   anyway.

### After — a partial index, and a predicate that matches it

```sql
CREATE INDEX shipments_live_backlog_idx
    ON shipments (organization_id, current_status)
    WHERE current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST');
```

```
Parallel Index Only Scan using shipments_live_backlog_idx (actual rows=40000 loops=3)
  Heap Fetches: 0
Execution Time: 8.548 ms
```

**Index-only, no heap fetches, 2.3× faster** — but the number that matters is
not the milliseconds. It is the size:

| Object | Size |
|---|---|
| `shipments` | 63 MB |
| `shipments_live_backlog_idx` | 848 kB |

The index covers only work still in play, so it tracks the **backlog**, not the
history. A tenant with two million delivered shipments and four thousand in
progress has a four-thousand-entry index and the same response time. The
sequential scan would have grown for ever.

The predicate is written as `NOT IN` the four terminal states rather than `IN`
the seventeen live ones deliberately: a status added in a later release is live
by default, so a new state cannot silently drop out of an operator's backlog.

---

## 3. Backlog by facility

```
Parallel Index Only Scan using shipments_live_custody_idx (actual rows=60000 loops=2)
  Heap Fetches: 0
Execution Time: 13.573 ms
```

Keyed by **custody**, not routing: the question is which facility is holding
work, and a parcel routed through a hub it has not reached is not that hub's
problem.

---

## 4. SLA breaches

### As first written — a sequential scan

`promised_delivery_at < now()` network-wide had no usable index. Release 2's
`shipments_destination_promised_idx` leads with `destination_branch_id`, which
serves a per-branch question and cannot serve this one:

```
Parallel Seq Scan on shipments (actual rows=39667 loops=3)
Execution Time: 85.117 ms
```

### After

```sql
CREATE INDEX shipments_sla_breach_idx
    ON shipments (organization_id, promised_delivery_at)
    WHERE promised_delivery_at IS NOT NULL
      AND current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST');
```

```
Index Only Scan using shipments_sla_breach_idx (actual rows=5000 loops=1)
  Index Cond: (organization_id = $0 AND promised_delivery_at < now())
  Heap Fetches: 0
Execution Time: 0.404 ms
```

**210× faster**, and the index is 872 kB because it holds only outstanding
commitments — not every delivery ever promised.

### A note on the measurement

The first version of this dataset made *every* live shipment breached, and the
planner still chose a sequential scan even with the index present — correctly,
because the predicate matched 60% of the table. The number above was taken
against a realistic distribution (5,000 breaches in 200,000 shipments, 2.5%).

That is worth stating rather than hiding: the index is the right structure, but
if a tenant's SLA performance ever collapses to the point where most outstanding
parcels are late, this query degrades to a scan. At that point the dashboard
being slow is not the operator's most pressing problem.

### Heap fetches and autovacuum

Immediately after the bulk update that created the breaches, the same plan
reported `Heap Fetches: 124000` and ran in 58 ms — the visibility map was stale.
After `VACUUM ANALYZE` it dropped to zero and 0.4 ms.

This is a real production consideration, not a measurement artefact: shipments
is a high-churn table, and index-only scans depend on the visibility map being
current. Autovacuum settings for this table should be checked before the command
centre is put under load.

---

## 5. Summary

| Query | Before | After | Structure |
|---|---|---|---|
| Period totals (30 days) | — | **0.077 ms** | rollup, 30 rows read |
| Live backlog by status | 19.45 ms (seq scan) | **8.55 ms** | partial index, 848 kB |
| Backlog by facility | — | **13.57 ms** | partial index |
| SLA breaches | 85.12 ms (seq scan) | **0.40 ms** | partial index, 872 kB |

Three partial indexes, totalling under 2 MB against a 63 MB table, and none of
them grows with delivered history.

## 6. Residual risk

- **The rollup trigger adds a write to every status change.** That is the
  deliberate trade — a shipment changes status roughly a dozen times in its life,
  while a dashboard is polled continuously — but it has not been measured under
  the scan throughput of a peak hub shift. It should be, before Release 5.
- **`operational_snapshots` grows on a timer.** Retention is enforced by a
  90-day sweep; the guard in migration 0030 refuses to delete anything younger,
  so the floor cannot be shortened by a caller.
- **Autovacuum tuning on `shipments` is untested.** See §4 above.
