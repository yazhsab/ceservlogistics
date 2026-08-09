# Release 2 — operational query plans

Captured 8 August 2026 against PostgreSQL 16 with a seeded operational dataset.

Dataset: 40000 shipments, 240000 shipment events, 60000 scan events, 5000 bags holding 5000 items, 3000 manifests, 8000 operational exceptions, 4000 NDR cases.

Every query below is asserted to avoid a sequential scan on a high-volume table; the test fails the build if one appears.

## Scanner barcode resolution

The hottest read in the release: every scan starts here, and a hub can run thousands an hour from handheld devices.

```sql
SELECT s.id, s.public_id, s.awb, s.current_status, s.movement_direction,
s.current_custody_unit_id, s.current_custody_user_id, s.current_bag_id,
s.origin_branch_id, s.destination_branch_id, s.is_held,
p.id AS package_id
FROM shipments s
LEFT JOIN shipment_packages p
ON p.piece_barcode = $1 AND p.shipment_id = s.id
WHERE s.organization_id = $2
AND (s.awb = $1
OR s.id = (SELECT shipment_id FROM shipment_packages
WHERE piece_barcode = $1 LIMIT 1))
```

```
Hash Left Join  (cost=8.61..16.50 rows=2 width=124) (actual time=0.011..0.012 rows=1 loops=1)
  Hash Cond: (s.id = p.shipment_id)
  Buffers: shared hit=3
  InitPlan 1 (returns $0)
    ->  Limit  (cost=0.00..0.00 rows=1 width=8) (actual time=0.001..0.001 rows=0 loops=1)
          ->  Seq Scan on shipment_packages  (cost=0.00..0.00 rows=1 width=8) (actual time=0.001..0.001 rows=0 loops=1)
                Filter: (piece_barcode = 'PRF260807000001'::text)
  ->  Bitmap Heap Scan on shipments s  (cost=8.60..16.48 rows=2 width=116) (actual time=0.009..0.009 rows=1 loops=1)
        Recheck Cond: ((awb = 'PRF260807000001'::text) OR (id = $0))
        Filter: (organization_id = '1'::bigint)
        Heap Blocks: exact=1
        Buffers: shared hit=3
        ->  BitmapOr  (cost=8.60..8.60 rows=2 width=0) (actual time=0.006..0.006 rows=0 loops=1)
              Buffers: shared hit=2
              ->  Bitmap Index Scan on shipments_awb_prefix_idx  (cost=0.00..4.30 rows=1 width=0) (actual time=0.005..0.005 rows=1 loops=1)
                    Index Cond: (awb = 'PRF260807000001'::text)
                    Buffers: shared hit=2
              ->  Bitmap Index Scan on shipments_pkey  (cost=0.00..4.30 rows=1 width=0) (actual time=0.002..0.002 rows=0 loops=1)
                    Index Cond: (id = $0)
  ->  Hash  (cost=0.00..0.00 rows=1 width=16) (actual time=0.000..0.000 rows=0 loops=1)
        Buckets: 1024  Batches: 1  Memory Usage: 8kB
        ->  Seq Scan on shipment_packages p  (cost=0.00..0.00 rows=1 width=16) (actual time=0.000..0.000 rows=0 loops=1)
              Filter: (piece_barcode = 'PRF260807000001'::text)
Planning:
  Buffers: shared hit=92 read=5 written=5
Planning Time: 0.282 ms
Execution Time: 0.030 ms
```

## Delivery-ready queue at a branch

Rendered every time a supervisor plans a delivery run, filtered to one branch out of the whole network.

```sql
SELECT s.public_id, s.awb, s.current_status, s.status_changed_at,
s.promised_delivery_at, s.is_held
FROM shipments s
WHERE s.organization_id = $1
AND s.destination_branch_id = $2
AND s.current_status IN ('DESTINATION_BRANCH_RECEIVED','NDR','DELIVERY_FAILED')
AND s.is_held = false
ORDER BY s.promised_delivery_at NULLS LAST, s.status_changed_at, s.id
LIMIT 50
```

```
Limit  (cost=3653.34..3653.47 rows=50 width=84) (actual time=1.821..1.823 rows=50 loops=1)
  Buffers: shared hit=510
  ->  Sort  (cost=3653.34..3667.78 rows=5776 width=84) (actual time=1.821..1.821 rows=50 loops=1)
        Sort Key: promised_delivery_at, status_changed_at, id
        Sort Method: top-N heapsort  Memory: 35kB
        Buffers: shared hit=510
        ->  Bitmap Heap Scan on shipments s  (cost=93.05..3461.47 rows=5776 width=84) (actual time=0.261..1.356 rows=5714 loops=1)
              Recheck Cond: ((destination_branch_id = '4'::bigint) AND (current_status = ANY ('{DESTINATION_BRANCH_RECEIVED,NDR,DELIVERY_FAILED}'::text[])))
              Filter: ((NOT is_held) AND (organization_id = '1'::bigint))
              Heap Blocks: exact=499
              Buffers: shared hit=510
              ->  Bitmap Index Scan on shipments_delivery_queue_idx  (cost=0.00..91.60 rows=5776 width=0) (actual time=0.121..0.121 rows=11428 loops=1)
                    Index Cond: (destination_branch_id = '4'::bigint)
                    Buffers: shared hit=11
Planning:
  Buffers: shared hit=14 read=1 written=1
Planning Time: 0.091 ms
Execution Time: 1.841 ms
```

## Facility custody stocktake

What is physically at a hub right now; the busiest hub dashboard panel.

```sql
SELECT s.public_id, s.awb, s.current_status, s.status_changed_at
FROM shipments s
WHERE s.organization_id = $1
AND s.current_custody_unit_id = $2
AND s.current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST')
ORDER BY s.status_changed_at DESC, s.id DESC
LIMIT 50
```

```
Limit  (cost=3619.13..3619.25 rows=50 width=75) (actual time=2.645..2.647 rows=50 loops=1)
  Buffers: shared hit=506 read=4 written=4
  ->  Sort  (cost=3619.13..3630.47 rows=4538 width=75) (actual time=2.645..2.646 rows=50 loops=1)
        Sort Key: status_changed_at DESC, id DESC
        Sort Method: top-N heapsort  Memory: 38kB
        Buffers: shared hit=506 read=4 written=4
        ->  Bitmap Heap Scan on shipments s  (cost=92.74..3468.38 rows=4538 width=75) (actual time=0.219..0.758 rows=5714 loops=1)
              Recheck Cond: (current_custody_unit_id = '2'::bigint)
              Filter: ((organization_id = '1'::bigint) AND (current_status <> ALL ('{DELIVERED,RTO_DELIVERED,CANCELLED,LOST}'::text[])))
              Heap Blocks: exact=499
              Buffers: shared hit=506 read=4 written=4
              ->  Bitmap Index Scan on shipments_custody_unit_idx  (cost=0.00..91.60 rows=5776 width=0) (actual time=0.133..0.133 rows=11428 loops=1)
                    Index Cond: (current_custody_unit_id = '2'::bigint)
                    Buffers: shared hit=7 read=4 written=4
Planning:
  Buffers: shared hit=6
Planning Time: 0.053 ms
Execution Time: 2.661 ms
```

## Bag contents

Read on every bag screen and on every reconciliation.

```sql
SELECT bi.status, bi.piece_count, s.awb, s.current_status
FROM bag_items bi
JOIN shipments s ON s.id = bi.shipment_id
WHERE bi.bag_id = $1
ORDER BY bi.added_at, bi.id
```

```
Sort  (cost=16.62..16.62 rows=1 width=55) (actual time=0.011..0.011 rows=1 loops=1)
  Sort Key: bi.added_at, bi.id
  Sort Method: quicksort  Memory: 25kB
  Buffers: shared hit=6
  ->  Nested Loop  (cost=0.57..16.61 rows=1 width=55) (actual time=0.008..0.008 rows=1 loops=1)
        Buffers: shared hit=6
        ->  Index Scan using bag_items_unique on bag_items bi  (cost=0.28..8.30 rows=1 width=35) (actual time=0.004..0.004 rows=1 loops=1)
              Index Cond: (bag_id = '1'::bigint)
              Buffers: shared hit=3
        ->  Index Scan using shipments_pkey on shipments s  (cost=0.29..8.31 rows=1 width=36) (actual time=0.003..0.003 rows=1 loops=1)
              Index Cond: (id = bi.shipment_id)
              Buffers: shared hit=3
Planning:
  Buffers: shared hit=38
Planning Time: 0.117 ms
Execution Time: 0.020 ms
```

## Expected inbound manifests

The hub inbound board, polled continuously during a shift.

```sql
SELECT m.public_id, m.manifest_code, m.bag_count, m.total_shipment_count,
m.dispatched_at
FROM manifests m
WHERE m.organization_id = $1
AND m.destination_unit_id = $2
AND m.status = 'DISPATCHED'
ORDER BY m.dispatched_at, m.id
LIMIT 50
```

```
Limit  (cost=10.47..27.46 rows=50 width=71) (actual time=0.098..0.100 rows=50 loops=1)
  Buffers: shared hit=94
  ->  Incremental Sort  (cost=10.47..214.40 rows=600 width=71) (actual time=0.098..0.099 rows=50 loops=1)
        Sort Key: dispatched_at, id
        Presorted Key: dispatched_at
        Full-sort Groups: 1  Sort Method: quicksort  Average Memory: 33kB  Peak Memory: 33kB
        Pre-sorted Groups: 1  Sort Method: top-N heapsort  Average Memory: 32kB  Peak Memory: 32kB
        Buffers: shared hit=94
        ->  Index Scan Backward using manifests_inbound_idx on manifests m  (cost=0.15..191.78 rows=600 width=71) (actual time=0.006..0.064 rows=151 loops=1)
              Index Cond: (destination_unit_id = '4'::bigint)
              Filter: (organization_id = '1'::bigint)
              Buffers: shared hit=94
Planning:
  Buffers: shared hit=31
Planning Time: 0.090 ms
Execution Time: 0.110 ms
```

## Scan history by facility (deep page)

Keyset paginated; the plan must not degrade as an operator pages back through a busy day.

```sql
SELECT se.id, se.public_id, se.raw_barcode, se.scan_type, se.outcome, se.occurred_at
FROM scan_events se
WHERE se.organization_id = $1
AND se.operating_unit_id = $2
AND (se.occurred_at, se.id) < ($3, $4)
ORDER BY se.occurred_at DESC, se.id DESC
LIMIT 50
```

```
Limit  (cost=0.41..16.18 rows=50 width=73) (actual time=0.007..0.044 rows=50 loops=1)
  Buffers: shared hit=54
  ->  Index Scan using scan_events_unit_time_idx on scan_events se  (cost=0.41..4745.93 rows=15054 width=73) (actual time=0.006..0.043 rows=50 loops=1)
        Index Cond: ((operating_unit_id = '2'::bigint) AND (ROW(occurred_at, id) < ROW('2026-08-08 00:51:22.090322+00'::timestamp with time zone, '41034'::bigint)))
        Filter: (organization_id = '1'::bigint)
        Buffers: shared hit=54
Planning:
  Buffers: shared hit=21
Planning Time: 0.068 ms
Execution Time: 0.059 ms
```

## Public tracking by AWB

Unauthenticated and heavily polled by consignees; must be a single unique-index probe.

```sql
SELECT s.awb, s.current_status, s.status_changed_at, s.piece_count,
s.promised_delivery_at, s.booked_at, s.id
FROM shipments s
WHERE s.awb = $1
```

```
Index Scan using shipments_awb_prefix_idx on shipments s  (cost=0.29..8.31 rows=1 width=64) (actual time=0.004..0.004 rows=1 loops=1)
  Index Cond: (awb = 'PRF260807000001'::text)
  Buffers: shared hit=3
Planning:
  Buffers: shared hit=14
Planning Time: 0.034 ms
Execution Time: 0.006 ms
```

## Public tracking timeline

The second query of every public tracking request.

```sql
SELECT e.event_type, e.to_status, e.occurred_at, e.description
FROM shipment_events e
WHERE e.shipment_id = $1 AND e.event_type <> 'REMARK'
ORDER BY e.occurred_at ASC, e.sequence ASC
LIMIT 100
```

```
Limit  (cost=28.02..28.04 rows=6 width=56) (actual time=0.041..0.042 rows=6 loops=1)
  Buffers: shared hit=3 read=6 written=6
  ->  Sort  (cost=28.02..28.04 rows=6 width=56) (actual time=0.041..0.041 rows=6 loops=1)
        Sort Key: occurred_at, sequence
        Sort Method: quicksort  Memory: 25kB
        Buffers: shared hit=3 read=6 written=6
        ->  Bitmap Heap Scan on shipment_events e  (cost=4.47..27.94 rows=6 width=56) (actual time=0.024..0.037 rows=6 loops=1)
              Recheck Cond: (shipment_id = '1'::bigint)
              Filter: (event_type <> 'REMARK'::text)
              Heap Blocks: exact=6
              Buffers: shared hit=3 read=6 written=6
              ->  Bitmap Index Scan on shipment_events_occurred_idx  (cost=0.00..4.46 rows=6 width=0) (actual time=0.022..0.022 rows=6 loops=1)
                    Index Cond: (shipment_id = '1'::bigint)
                    Buffers: shared read=3 written=3
Planning:
  Buffers: shared hit=19
Planning Time: 0.051 ms
Execution Time: 0.054 ms
```

## Open exceptions at a facility

The hub exception panel, filtered to one facility.

```sql
SELECT e.public_id, e.exception_type, e.severity, e.raised_at
FROM operational_exceptions e
WHERE e.organization_id = $1
AND e.operating_unit_id = $2
AND e.status IN ('OPEN','INVESTIGATING')
ORDER BY e.created_at DESC, e.id DESC
LIMIT 50
```

```
Limit  (cost=0.28..47.75 rows=50 width=70) (actual time=0.006..0.051 rows=50 loops=1)
  Buffers: shared hit=85
  ->  Index Scan using operational_exceptions_org_created_idx on operational_exceptions e  (cost=0.28..949.66 rows=1000 width=70) (actual time=0.006..0.049 rows=50 loops=1)
        Index Cond: (organization_id = '1'::bigint)
        Filter: ((status = ANY ('{OPEN,INVESTIGATING}'::text[])) AND (operating_unit_id = '2'::bigint))
        Rows Removed by Filter: 49
        Buffers: shared hit=85
Planning:
  Buffers: shared hit=36
Planning Time: 0.079 ms
Execution Time: 0.056 ms
```

## NDR cases due for reattempt

Swept by the worker and rendered on the branch NDR board.

```sql
SELECT n.public_id, n.case_code, n.next_attempt_at, n.attempt_count
FROM ndr_cases n
WHERE n.organization_id = $1
AND n.status IN ('OPEN','SCHEDULED')
AND n.next_attempt_at IS NOT NULL
AND n.next_attempt_at <= now()
ORDER BY n.next_attempt_at
LIMIT 50
```

```
Limit  (cost=0.28..69.73 rows=50 width=59) (actual time=0.006..0.015 rows=50 loops=1)
  Buffers: shared hit=6
  ->  Index Scan using ndr_cases_due_idx on ndr_cases n  (cost=0.28..205.85 rows=148 width=59) (actual time=0.006..0.014 rows=50 loops=1)
        Index Cond: ((organization_id = '1'::bigint) AND (next_attempt_at <= now()))
        Buffers: shared hit=6
Planning:
  Buffers: shared hit=30
Planning Time: 0.098 ms
Execution Time: 0.019 ms
```


## Addendum — piece-barcode resolution at volume

The scanner barcode-resolution plan at the top of this document shows a `Seq
Scan on shipment_packages`. That is an artefact of the seed, not a missing
index: the perf fixture creates 40,000 shipments but no packages, and
PostgreSQL will always sequential-scan a zero-page table.

Barcode resolution has two arms — AWB and piece barcode — and only the AWB arm
was exercised above. Measured separately, with 196,560 package rows inserted
inside a transaction that was then rolled back:

```sql
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF)
SELECT s.id, s.public_id, s.awb, s.current_status
FROM shipments s
WHERE s.organization_id = $1
  AND (s.awb = $2
       OR s.id = (SELECT p.shipment_id FROM shipment_packages p
                  WHERE p.piece_barcode = $2 LIMIT 1));
```

```
Bitmap Heap Scan on shipments s (actual time=0.017..0.017 rows=1 loops=1)
  Recheck Cond: ((awb = 'PPB000000001350'::text) OR (id = $1))
  Filter: (organization_id = $0)
  Buffers: shared hit=10
  InitPlan 2 (returns $1)
    ->  Limit (actual time=0.007..0.007 rows=1 loops=1)
          ->  Index Scan using shipment_packages_piece_barcode_key on shipment_packages p (actual time=0.007..0.007 rows=1 loops=1)
                Index Cond: (piece_barcode = 'PPB000000001350'::text)
                Buffers: shared hit=4
  ->  BitmapOr (actual time=0.014..0.014 rows=0 loops=1)
        ->  Bitmap Index Scan on shipments_awb_prefix_idx
        ->  Bitmap Index Scan on shipments_pkey
Planning Time: 0.151 ms
Execution Time: 0.042 ms
```

Both arms are index-served. The unique index
`shipment_packages_piece_barcode_key` carries the piece lookup in 4 buffer hits,
and the whole resolution costs 10 hits and 0.042 ms.

Residual gap: the multi-piece path is not covered by the standing perf fixture,
so a regression there would not be caught automatically. Seeding packages in
`tests/perf` would close it.
