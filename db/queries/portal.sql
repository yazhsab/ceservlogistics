-- Portals and consoles (M27, M28, M29).
--
-- Everything here is a *narrow projection*. The internal API returns a full
-- shipment because an operations screen shows one; a scanner needs an AWB, a
-- status and a destination, and sending it the other forty fields costs battery
-- and bandwidth on a handheld over a branch's mobile connection.
--
-- The scoping predicate is not optional in any of these. A customer portal
-- query without a customer filter and a franchise query without a unit filter
-- are the same bug — the whole tenant's data leaving through a screen that was
-- supposed to show one account's.

-- ---------------------------------------------------------------------------
-- M27 Customer portal
-- ---------------------------------------------------------------------------

-- name: PortalCustomerSummary :one
-- The customer's own dashboard tiles.
--
-- Bounded by date on purpose: an account with five years of history must not
-- produce a five-year aggregate every time somebody opens the page.
SELECT
    count(*)::bigint AS total,
    count(*) FILTER (WHERE current_status = 'DELIVERED')::bigint AS delivered,
    count(*) FILTER (WHERE current_status IN
        ('BOOKED','PICKUP_SCHEDULED','PICKUP_ASSIGNED','PICKED_UP',
         'ORIGIN_BRANCH_RECEIVED','ORIGIN_BAGGED','ORIGIN_DISPATCHED','IN_TRANSIT',
         'TRANSIT_HUB_RECEIVED','TRANSIT_HUB_DISPATCHED','DESTINATION_HUB_RECEIVED',
         'DESTINATION_BRANCH_RECEIVED','OUT_FOR_DELIVERY'))::bigint AS in_progress,
    count(*) FILTER (WHERE current_status IN ('NDR','DELIVERY_FAILED'))::bigint AS exceptions,
    count(*) FILTER (WHERE current_status IN
        ('RTO_INITIATED','RTO_IN_TRANSIT','RTO_DELIVERED'))::bigint AS returning,
    COALESCE(SUM(total_amount_minor), 0)::bigint AS spend_minor,
    COALESCE(SUM(cod_amount_minor) FILTER (WHERE payment_mode = 'COD'), 0)::bigint
        AS cod_booked_minor
FROM shipments
WHERE organization_id = $1
  AND customer_id = ANY(sqlc.arg('customer_ids')::bigint[])
  AND created_at >= sqlc.arg('since')::timestamptz;

-- name: PortalCustomerAccounts :many
-- The accounts this portal user may act for, with their credit position.
SELECT c.id, c.public_id, c.code, c.name, c.customer_type, c.status,
       cp.credit_limit_minor, cp.credit_used_minor, cp.payment_terms_days,
       cp.currency, cp.credit_status
FROM customers c
LEFT JOIN credit_profiles cp ON cp.customer_id = c.id
WHERE c.organization_id = $1 AND c.id = ANY(sqlc.arg('customer_ids')::bigint[])
ORDER BY c.name;

-- name: PortalCustomerInvoices :many
SELECT i.public_id, i.invoice_number, i.status, i.issue_date, i.due_date,
       i.total_minor, i.paid_minor, i.currency, i.period_start, i.period_end,
       c.public_id AS customer_public_id, c.name AS customer_name
FROM invoices i
JOIN customers c ON c.id = i.customer_id
WHERE i.organization_id = $1
  AND i.customer_id = ANY(sqlc.arg('customer_ids')::bigint[])
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status')::text)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR i.id < sqlc.narg('cursor_id')::bigint)
ORDER BY i.id DESC
LIMIT sqlc.arg('row_limit');

-- ---------------------------------------------------------------------------
-- M28 Franchise portal
-- ---------------------------------------------------------------------------

-- name: PortalFranchiseSummary :one
-- What the franchise booked and delivered in a period, read from the rollup
-- rather than from shipments.
SELECT
    COALESCE(SUM(booked_count), 0)::bigint    AS booked,
    COALESCE(SUM(delivered_count), 0)::bigint AS delivered,
    COALESCE(SUM(ndr_count), 0)::bigint       AS ndr,
    COALESCE(SUM(rto_count), 0)::bigint       AS rto,
    COALESCE(SUM(revenue_minor), 0)::bigint   AS revenue_minor,
    COALESCE(SUM(cod_amount_minor), 0)::bigint AS cod_booked_minor
FROM shipment_daily_stats
WHERE organization_id = $1
  AND operating_unit_id = ANY(sqlc.arg('unit_ids')::bigint[])
  AND stat_date BETWEEN sqlc.arg('from_date') AND sqlc.arg('to_date');

-- name: PortalFranchiseEarnings :one
-- Commission earned, and how much of it a settlement has already claimed.
--
-- "Earned" is the *net* of the signed entry types, matching
-- SumCommissionByFranchise exactly: an EARNED entry is positive, a REVERSAL is
-- negative, an ADJUSTMENT is either. Counting only EARNED would show a
-- franchise money that was later reversed, and the settlement engine would
-- disagree with the dashboard about what they are owed.
SELECT
    COALESCE(SUM(e.amount_minor), 0)::bigint AS earned_minor,
    COALESCE(SUM(e.amount_minor) FILTER (WHERE e.settlement_id IS NOT NULL), 0)::bigint
        AS settled_minor,
    count(*)::bigint AS entry_count
FROM commission_entries e
WHERE e.organization_id = $1
  AND e.franchise_id = sqlc.arg('franchise_id')
  AND e.earned_on BETWEEN sqlc.arg('from_date') AND sqlc.arg('to_date');

-- name: PortalFranchiseCODPosition :one
-- Cash this franchise is holding and has not remitted.
SELECT
    COALESCE(SUM(collected_minor - remitted_minor), 0)::bigint AS in_custody_minor,
    count(*)::bigint AS obligation_count,
    count(*) FILTER (WHERE collected_at < now() - interval '48 hours')::bigint
        AS aged_over_48h
FROM cod_obligations
WHERE organization_id = $1
  AND (origin_franchise_id = sqlc.arg('franchise_id')
       OR destination_franchise_id = sqlc.arg('franchise_id'))
  AND status IN ('AGENT_COLLECTED','BRANCH_RECEIVED','FRANCHISE_CONFIRMED','RECONCILED');

-- name: PortalFranchiseShipments :many
-- A franchise's shipments, with the relationship stated rather than implied.
--
-- The shared ListShipments scope predicate matches origin OR destination OR
-- booking unit, which is right for a staff listing and wrong here: it made the
-- portal show a destination franchise three shipments while the summary beside
-- it — read from the rollup, which is keyed by booking unit — said zero. Two
-- numbers on one screen that disagree is worse than either being absent.
--
-- So the caller says which relationship it means. ORIGIN matches what the
-- franchise booked and is what the summary counts; DESTINATION matches what it
-- must deliver; ANY is both, for a franchise that wants its whole workload.
SELECT s.public_id, s.awb, s.current_status, s.piece_count,
       s.destination_pincode, s.payment_mode, s.total_amount_minor,
       s.cod_amount_minor, s.currency, s.booked_at, s.created_at, s.id,
       c.name AS customer_name, sv.code AS service_code,
       CASE WHEN COALESCE(s.booking_unit_id, s.origin_branch_id) = sqlc.arg('unit_id')
            THEN 'ORIGIN' ELSE 'DESTINATION' END AS franchise_role
FROM shipments s
JOIN customers c ON c.id = s.customer_id
JOIN courier_services sv ON sv.id = s.courier_service_id
WHERE s.organization_id = $1
  AND (
        (sqlc.arg('role')::text IN ('ORIGIN','ANY')
         AND COALESCE(s.booking_unit_id, s.origin_branch_id) = sqlc.arg('unit_id'))
     OR (sqlc.arg('role')::text IN ('DESTINATION','ANY')
         AND s.destination_branch_id = sqlc.arg('unit_id'))
      )
  AND (sqlc.narg('status')::text IS NULL OR s.current_status = sqlc.narg('status')::text)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR s.id < sqlc.narg('cursor_id')::bigint)
ORDER BY s.id DESC
LIMIT sqlc.arg('row_limit');

-- name: PortalFranchiseSettlements :many
SELECT s.public_id, s.settlement_number, s.status, s.period_start, s.period_end,
       s.net_amount_minor, s.paid_minor, s.currency, s.approved_at, s.closed_at, s.created_at
FROM settlements s
WHERE s.organization_id = $1
  AND s.franchise_id = sqlc.arg('franchise_id')
  AND (sqlc.narg('status')::text IS NULL OR s.status = sqlc.narg('status')::text)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR s.id < sqlc.narg('cursor_id')::bigint)
ORDER BY s.id DESC
LIMIT sqlc.arg('row_limit');

-- ---------------------------------------------------------------------------
-- M29 Hub and branch console
--
-- Deliberately thin. A handheld scanner over a branch's mobile connection is
-- the worst network in the system, and these are the queries it runs most.
-- ---------------------------------------------------------------------------

-- name: ConsoleLookupBarcode :one
-- One parcel, by AWB or piece barcode, with only what a scanner shows.
--
-- Ten columns rather than the shipment's forty. The operator is looking at a
-- 3-inch screen deciding where to put the box; the customer's email address and
-- the pricing breakdown are not part of that decision.
SELECT s.public_id, s.awb, s.current_status, s.piece_count,
       s.destination_pincode, s.payment_mode, s.cod_amount_minor, s.currency,
       s.is_held, s.movement_direction,
       db.code AS destination_branch_code, db.name AS destination_branch_name,
       cu.code AS custody_unit_code,
       ras.city_name AS destination_city
FROM shipments s
LEFT JOIN operating_units db ON db.id = s.destination_branch_id
LEFT JOIN operating_units cu ON cu.id = s.current_custody_unit_id
LEFT JOIN shipment_address_snapshots ras
       ON ras.shipment_id = s.id AND ras.role = 'RECIPIENT'
WHERE s.organization_id = $1
  AND (s.awb = sqlc.arg('barcode')
       OR EXISTS (SELECT 1 FROM shipment_packages sp
                   WHERE sp.shipment_id = s.id
                     AND sp.piece_barcode = sqlc.arg('barcode')));

-- name: ConsoleWorkQueue :many
-- What is sitting at this facility, compact and paginated.
SELECT s.public_id, s.awb, s.current_status, s.piece_count,
       s.destination_pincode, s.payment_mode, s.cod_amount_minor,
       s.promised_delivery_at, s.is_held,
       db.code AS destination_branch_code
FROM shipments s
LEFT JOIN operating_units db ON db.id = s.destination_branch_id
WHERE s.organization_id = $1
  AND s.current_custody_unit_id = sqlc.arg('unit_id')
  AND s.current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST')
  AND (sqlc.narg('status')::text IS NULL OR s.current_status = sqlc.narg('status')::text)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR s.id < sqlc.narg('cursor_id')::bigint)
ORDER BY s.id DESC
LIMIT sqlc.arg('row_limit');

-- name: ConsoleFacilitySummary :one
-- The counts a facility screen shows above its queue. One row, four numbers.
SELECT
    count(*)::bigint AS in_custody,
    count(*) FILTER (WHERE current_status = 'OUT_FOR_DELIVERY')::bigint AS out_for_delivery,
    count(*) FILTER (WHERE current_status IN ('NDR','DELIVERY_FAILED'))::bigint AS exceptions,
    count(*) FILTER (WHERE is_held)::bigint AS held,
    count(*) FILTER (WHERE promised_delivery_at IS NOT NULL
                       AND promised_delivery_at < now())::bigint AS overdue
FROM shipments
WHERE organization_id = $1
  AND current_custody_unit_id = sqlc.arg('unit_id')
  AND current_status NOT IN ('DELIVERED','RTO_DELIVERED','CANCELLED','LOST');

-- name: ConsoleOpenBags :many
SELECT b.public_id, b.bag_code, b.status, b.shipment_count, b.total_weight_grams,
       d.code AS destination_code, d.name AS destination_name, b.created_at
FROM bags b
LEFT JOIN operating_units d ON d.id = b.destination_unit_id
WHERE b.organization_id = $1
  AND b.origin_unit_id = sqlc.arg('unit_id')
  AND b.status IN ('OPEN','CLOSED')
ORDER BY b.id DESC
LIMIT sqlc.arg('row_limit');

-- name: ConsoleInboundManifests :many
-- What is on its way here, so a facility can staff for it.
SELECT m.public_id, m.manifest_code, m.status, m.bag_count, m.total_shipment_count,
       m.total_weight_grams, m.dispatched_at,
       o.code AS origin_code, o.name AS origin_name
FROM manifests m
LEFT JOIN operating_units o ON o.id = m.origin_unit_id
WHERE m.organization_id = $1
  AND m.destination_unit_id = sqlc.arg('unit_id')
  AND m.status IN ('DISPATCHED','IN_TRANSIT')
ORDER BY m.dispatched_at NULLS LAST, m.id DESC
LIMIT sqlc.arg('row_limit');
