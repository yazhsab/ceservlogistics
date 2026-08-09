-- Commission engine (M21).
--
-- Two things here decide money, and both are deliberate:
--
--   ResolveCommissionRule  picks exactly one rule from the candidates, with a
--                          total order so the answer never depends on plan
--                          shape or row insertion order.
--   GetEffectiveRuleVersion picks the version in force on a date, so a
--                          recalculation of a past event uses the past rate.

-- ---------------------------------------------------------------------------
-- Schemes
-- ---------------------------------------------------------------------------

-- name: CreateCommissionScheme :one
INSERT INTO commission_schemes (public_id, organization_id, code, name, description,
                                currency, is_default, status, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: GetSchemeByCode :one
SELECT * FROM commission_schemes WHERE organization_id = $1 AND code = $2;

-- name: GetSchemeByPublicID :one
SELECT * FROM commission_schemes WHERE organization_id = $1 AND public_id = $2;

-- name: GetDefaultScheme :one
SELECT * FROM commission_schemes
WHERE organization_id = $1 AND is_default AND status = 'ACTIVE';

-- name: ListCommissionSchemes :many
SELECT * FROM commission_schemes
WHERE organization_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY code
LIMIT $2 OFFSET $3;

-- ---------------------------------------------------------------------------
-- Rules
-- ---------------------------------------------------------------------------

-- name: CreateCommissionRule :one
INSERT INTO commission_rules (
    public_id, organization_id, scheme_id, code, name, commission_type, recipient_role,
    franchise_id, franchise_category, operating_unit_id, service_id,
    origin_zone_id, destination_zone_id, customer_category, payment_mode,
    priority, status, description, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
RETURNING *;

-- name: GetRuleByPublicID :one
SELECT r.*, s.code AS scheme_code, s.name AS scheme_name
FROM commission_rules r
JOIN commission_schemes s ON s.id = r.scheme_id
WHERE r.organization_id = $1 AND r.public_id = $2;

-- name: GetRuleByID :one
SELECT * FROM commission_rules WHERE organization_id = $1 AND id = $2;

-- name: ResolveCommissionRule :many
-- The precedence query. Returns every rule that matches the facts of a
-- shipment, most specific first; the service takes the head of the list.
--
-- A rule matches when each of its scope columns is either NULL ("any") or
-- equal to the corresponding fact. Ordering is a total order, so the winner is
-- never ambiguous:
--
--   1. specificity  — more bound dimensions wins (maintained by trigger)
--   2. priority     — the manual tiebreak, for genuine ties
--   3. id           — oldest wins, so adding a rule cannot silently displace an
--                     equally-specific existing one
--
-- Returning the full candidate list rather than a single row lets the
-- simulation endpoint explain *why* a rule won, which is the difference
-- between a franchise accepting a number and disputing it.
SELECT r.*, s.code AS scheme_code
FROM commission_rules r
JOIN commission_schemes s ON s.id = r.scheme_id
WHERE r.organization_id = $1
  AND r.status = 'ACTIVE'
  AND s.status = 'ACTIVE'
  AND r.commission_type = sqlc.arg('commission_type')
  AND (sqlc.narg('scheme_id')::bigint IS NULL OR r.scheme_id = sqlc.narg('scheme_id')::bigint)
  AND (r.franchise_id        IS NULL OR r.franchise_id = sqlc.narg('franchise_id')::bigint)
  AND (r.franchise_category  IS NULL OR r.franchise_category = sqlc.narg('franchise_category')::text)
  AND (r.operating_unit_id   IS NULL OR r.operating_unit_id = sqlc.narg('operating_unit_id')::bigint)
  AND (r.service_id          IS NULL OR r.service_id = sqlc.narg('service_id')::bigint)
  AND (r.origin_zone_id      IS NULL OR r.origin_zone_id = sqlc.narg('origin_zone_id')::bigint)
  AND (r.destination_zone_id IS NULL OR r.destination_zone_id = sqlc.narg('destination_zone_id')::bigint)
  AND (r.customer_category   IS NULL OR r.customer_category = sqlc.narg('customer_category')::text)
  AND (r.payment_mode        IS NULL OR r.payment_mode = sqlc.narg('payment_mode')::text)
ORDER BY r.specificity DESC, r.priority DESC, r.id ASC;

-- name: ListCommissionRules :many
SELECT r.*, s.code AS scheme_code
FROM commission_rules r
JOIN commission_schemes s ON s.id = r.scheme_id
WHERE r.organization_id = $1
  AND (sqlc.narg('scheme_id')::bigint IS NULL OR r.scheme_id = sqlc.narg('scheme_id')::bigint)
  AND (sqlc.narg('commission_type')::text IS NULL OR r.commission_type = sqlc.narg('commission_type')::text)
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status')::text)
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR r.franchise_id = sqlc.narg('franchise_id')::bigint)
ORDER BY r.commission_type, r.specificity DESC, r.priority DESC, r.id
LIMIT $2 OFFSET $3;

-- name: CountCommissionRules :one
SELECT count(*) FROM commission_rules r
WHERE r.organization_id = $1
  AND (sqlc.narg('scheme_id')::bigint IS NULL OR r.scheme_id = sqlc.narg('scheme_id')::bigint)
  AND (sqlc.narg('commission_type')::text IS NULL OR r.commission_type = sqlc.narg('commission_type')::text)
  AND (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status')::text)
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR r.franchise_id = sqlc.narg('franchise_id')::bigint);

-- name: UpdateRuleStatus :one
UPDATE commission_rules
   SET status = $3
 WHERE organization_id = $1 AND id = $2
RETURNING *;

-- ---------------------------------------------------------------------------
-- Rule versions
-- ---------------------------------------------------------------------------

-- name: CreateRuleVersion :one
INSERT INTO commission_rule_versions (
    public_id, organization_id, rule_id, version_no, calculation_method, currency,
    fixed_amount_minor, rate_bp, basis, slabs,
    min_amount_minor, max_amount_minor, effective_from, effective_to,
    status, notes, created_by
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
RETURNING *;

-- name: NextRuleVersionNumber :one
SELECT COALESCE(MAX(version_no), 0) + 1 AS next_version FROM commission_rule_versions
WHERE rule_id = $1;

-- name: GetEffectiveRuleVersion :one
-- The version in force for a rule on a date.
--
-- This is what makes historical commission stable: a recalculation for an event
-- in March finds March's version, not today's, because the qualifying date is
-- passed in rather than defaulting to now().
SELECT * FROM commission_rule_versions
WHERE rule_id = $1
  AND status = 'ACTIVE'
  AND effective_from <= sqlc.arg('as_of')::date
  AND (effective_to IS NULL OR effective_to >= sqlc.arg('as_of')::date)
ORDER BY effective_from DESC, id DESC
LIMIT 1;

-- name: GetRuleVersionByID :one
SELECT * FROM commission_rule_versions WHERE organization_id = $1 AND id = $2;

-- name: ListRuleVersions :many
SELECT * FROM commission_rule_versions
WHERE rule_id = $1
ORDER BY version_no DESC;

-- name: SupersedePriorVersions :exec
-- Closes the open-ended predecessor when a new version starts, so the two do
-- not both claim to be in force on the same day.
UPDATE commission_rule_versions
   SET effective_to = (sqlc.arg('effective_from')::date - 1), status = 'SUPERSEDED'
 WHERE rule_id = sqlc.arg('rule_id')
   AND id <> sqlc.arg('new_version_id')
   AND status = 'ACTIVE'
   AND effective_from < sqlc.arg('effective_from')::date
   AND (effective_to IS NULL OR effective_to >= sqlc.arg('effective_from')::date);

-- ---------------------------------------------------------------------------
-- Calculations
-- ---------------------------------------------------------------------------

-- name: CreateCommissionCalculation :one
INSERT INTO commission_calculations (
    public_id, organization_id, commission_type, shipment_id,
    qualifying_event, qualifying_event_id, qualified_at,
    recipient_type, recipient_id, franchise_id, operating_unit_id,
    rule_id, rule_version_id, rule_code, rule_version_no,
    calculation_method, basis, base_amount_minor, rate_bp,
    gross_amount_minor, amount_minor, currency,
    calculation_inputs, calculation_trace, status, created_by, request_id
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)
RETURNING *;

-- name: FindCalculationForShipment :one
-- The idempotency probe, mirroring commission_calculations_shipment_idx.
SELECT * FROM commission_calculations
WHERE organization_id = $1 AND shipment_id = $2 AND commission_type = $3
  AND recipient_type = $4 AND recipient_id = $5
  AND status IN ('CALCULATED','POSTED');

-- name: GetCalculationByPublicID :one
SELECT c.*, s.awb, f.code AS franchise_code, f.name AS franchise_name
FROM commission_calculations c
LEFT JOIN shipments s ON s.id = c.shipment_id
LEFT JOIN franchises f ON f.id = c.franchise_id
WHERE c.organization_id = $1 AND c.public_id = $2;

-- name: GetCalculationByID :one
SELECT * FROM commission_calculations WHERE organization_id = $1 AND id = $2;

-- name: MarkCalculationPosted :one
UPDATE commission_calculations
   SET status = 'POSTED', journal_transaction_id = $3, posted_at = now()
 WHERE organization_id = $1 AND id = $2 AND status = 'CALCULATED'
RETURNING *;

-- name: MarkCalculationReversed :one
UPDATE commission_calculations
   SET status = 'REVERSED', reversed_by_id = $3
 WHERE organization_id = $1 AND id = $2 AND status = 'POSTED'
RETURNING *;

-- name: ListCommissionCalculations :many
SELECT c.*, s.awb, f.code AS franchise_code
FROM commission_calculations c
LEFT JOIN shipments s ON s.id = c.shipment_id
LEFT JOIN franchises f ON f.id = c.franchise_id
WHERE c.organization_id = $1
  AND (sqlc.narg('commission_type')::text IS NULL OR c.commission_type = sqlc.narg('commission_type')::text)
  AND (sqlc.narg('status')::text IS NULL OR c.status = sqlc.narg('status')::text)
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR c.franchise_id = sqlc.narg('franchise_id')::bigint)
  AND (sqlc.narg('shipment_id')::bigint IS NULL OR c.shipment_id = sqlc.narg('shipment_id')::bigint)
  AND (sqlc.narg('from_date')::timestamptz IS NULL OR c.qualified_at >= sqlc.narg('from_date')::timestamptz)
  AND (sqlc.narg('to_date')::timestamptz IS NULL OR c.qualified_at <= sqlc.narg('to_date')::timestamptz)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR c.id < sqlc.narg('cursor_id')::bigint)
ORDER BY c.id DESC
LIMIT $2;

-- name: CountCommissionCalculations :one
SELECT count(*) FROM commission_calculations c
WHERE c.organization_id = $1
  AND (sqlc.narg('commission_type')::text IS NULL OR c.commission_type = sqlc.narg('commission_type')::text)
  AND (sqlc.narg('status')::text IS NULL OR c.status = sqlc.narg('status')::text)
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR c.franchise_id = sqlc.narg('franchise_id')::bigint)
  AND (sqlc.narg('shipment_id')::bigint IS NULL OR c.shipment_id = sqlc.narg('shipment_id')::bigint);

-- name: SweepCommissionForSettlement :many
-- Posted commission for a franchise in a period that is not on another
-- settlement.
--
-- "not on ANOTHER settlement" rather than "not on any": a recalculation has
-- already claimed its own rows, and excluding them would make the rebuild find
-- nothing and silently empty the statement it is rebuilding.
--
-- FOR UPDATE so a concurrent generation for the same franchise blocks rather
-- than sweeping the same rows onto two statements.
SELECT * FROM commission_calculations
WHERE organization_id = $1
  AND franchise_id = $2
  AND status = 'POSTED'
  AND (settlement_id IS NULL
       OR settlement_id = sqlc.narg('settlement_id')::bigint)
  AND qualified_at >= sqlc.arg('period_start')::timestamptz
  AND qualified_at < sqlc.arg('period_end')::timestamptz
ORDER BY qualified_at, id
FOR UPDATE;

-- name: AttachCalculationsToSettlement :exec
UPDATE commission_calculations
   SET settlement_id = sqlc.arg('settlement_id')
 WHERE organization_id = sqlc.arg('organization_id')
   AND id = ANY(sqlc.arg('ids')::bigint[]);

-- name: DetachCalculationsFromSettlement :exec
-- Used when a settlement is cancelled, so its commission returns to the pool
-- rather than being stranded.
UPDATE commission_calculations
   SET settlement_id = NULL
 WHERE organization_id = sqlc.arg('organization_id')
   AND settlement_id = sqlc.arg('settlement_id');

-- ---------------------------------------------------------------------------
-- Entries
-- ---------------------------------------------------------------------------

-- name: CreateCommissionEntry :one
INSERT INTO commission_entries (
    public_id, organization_id, calculation_id, franchise_id, operating_unit_id,
    entry_type, commission_type, amount_minor, currency, shipment_id, earned_on, memo
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: ListCommissionEntries :many
SELECT e.*, s.awb
FROM commission_entries e
LEFT JOIN shipments s ON s.id = e.shipment_id
WHERE e.organization_id = $1
  AND (sqlc.narg('franchise_id')::bigint IS NULL OR e.franchise_id = sqlc.narg('franchise_id')::bigint)
  AND (sqlc.narg('from_date')::date IS NULL OR e.earned_on >= sqlc.narg('from_date')::date)
  AND (sqlc.narg('to_date')::date IS NULL OR e.earned_on <= sqlc.narg('to_date')::date)
  AND (sqlc.narg('cursor_id')::bigint IS NULL OR e.id < sqlc.narg('cursor_id')::bigint)
ORDER BY e.id DESC
LIMIT $2;

-- name: SumCommissionByFranchise :one
-- What a franchise has earned in a window, by entry type, for the settlement
-- header and the franchise dashboard.
SELECT
    COALESCE(SUM(amount_minor) FILTER (WHERE entry_type = 'EARNED'), 0)::bigint     AS earned_minor,
    COALESCE(SUM(amount_minor) FILTER (WHERE entry_type = 'REVERSAL'), 0)::bigint   AS reversed_minor,
    COALESCE(SUM(amount_minor) FILTER (WHERE entry_type = 'ADJUSTMENT'), 0)::bigint AS adjusted_minor,
    COALESCE(SUM(amount_minor), 0)::bigint                                          AS net_minor,
    count(*)::bigint                                                                AS entry_count
FROM commission_entries
WHERE organization_id = $1 AND franchise_id = $2
  AND earned_on >= sqlc.arg('period_start')::date
  AND earned_on <= sqlc.arg('period_end')::date;

-- name: CountShipmentsForVolumeIncentive :one
-- Qualifying volume for a VOLUME_INCENTIVE rule: delivered shipments booked at
-- the franchise's operating unit in a window.
--
-- A franchise owns one operating unit (franchises.operating_unit_id), so the
-- join runs from the franchise to the unit and then matches the booking unit.
SELECT count(*)::bigint AS shipment_count,
       COALESCE(SUM(s.total_amount_minor), 0)::bigint AS revenue_minor
FROM shipments s
JOIN franchises f ON f.operating_unit_id = s.booking_unit_id
WHERE s.organization_id = $1
  AND f.id = $2
  AND s.current_status = 'DELIVERED'
  AND s.created_at >= sqlc.arg('period_start')::timestamptz
  AND s.created_at < sqlc.arg('period_end')::timestamptz;
