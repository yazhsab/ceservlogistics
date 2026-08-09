// Package analytics is the operations command centre (M30).
//
// # The one rule this module exists to keep
//
// "Do not run full-table aggregate scans on every request." A command centre is
// polled continuously, by every supervisor in the network at once, and the
// naive implementation — count shipments grouped by status, sum revenue over a
// date range — would put a sequential scan of the largest table in the system
// on a thirty-second timer.
//
// So there are exactly two shapes of question here, and each has one answer:
//
//   - "How many moved in this period?" reads shipment_daily_stats, a rollup the
//     database maintains incrementally as statuses change. Reading 30 days costs
//     30 small rows. It is updated in the same transaction as the status change,
//     so it is **exact, not eventually consistent** — there is no staleness
//     window to document.
//
//   - "How many are sitting there right now?" is a live count over an indexed
//     current_status. That is an index-only count, not an aggregate over
//     history, and it is deliberately restricted to in-progress statuses: a
//     backlog gauge is the number an operator is about to act on, so it may not
//     be stale, and terminal statuses have no backlog to report anyway.
//
// The single genuinely stale thing in this package is the snapshot series,
// which exists for trend charts. Every snapshot carries capturedAt so a chart
// can state its own age rather than implying it is live.
package analytics

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// JobTypeSnapshot captures one tenant's operational snapshot.
const JobTypeSnapshot = "analytics.snapshot"

// Permissions.
const PermCommandRead = "command.read"

// terminalStatuses are the states a shipment never leaves.
//
// The live-backlog queries define their own predicate as NOT IN this set,
// matching shipments_live_backlog_idx exactly — see the comment on that index
// for why the shape matters. This list exists so the snapshot code and the
// contract documentation cannot disagree with the SQL about what "still in
// play" means.
var terminalStatuses = []string{
	string(shipment.StatusDelivered),
	string(shipment.StatusRTODelivered),
	string(shipment.StatusCancelled),
	string(shipment.StatusLost),
}

// IsTerminal reports whether a status is outside the operational backlog.
func IsTerminal(status string) bool {
	for _, t := range terminalStatuses {
		if t == status {
			return true
		}
	}
	return false
}

// Service answers command-centre questions.
type Service struct {
	db   *database.DB
	q    *dbgen.Queries
	jobs *jobs.Enqueuer
	log  *slog.Logger
}

func NewService(db *database.DB, q *dbgen.Queries, enq *jobs.Enqueuer, log *slog.Logger) *Service {
	return &Service{db: db, q: q, jobs: enq, log: log}
}

// ---------------------------------------------------------------------------
// Views
// ---------------------------------------------------------------------------

// Window is the period a dashboard is asking about.
type Window struct {
	From time.Time
	To   time.Time
	// UnitID narrows every figure to one facility. Nil is network-wide.
	UnitID *int64
	// ServiceID narrows the period totals to one product.
	ServiceID *int64
}

// Overview is the command centre's landing payload.
type Overview struct {
	Period      PeriodTotals    `json:"period"`
	Live        LiveBacklog     `json:"live"`
	Alerts      Alerts          `json:"alerts"`
	Movement    Movement        `json:"movement"`
	Money       MoneyInFlight   `json:"money"`
	Consistency ConsistencyNote `json:"consistency"`
}

// PeriodTotals comes from the rollup and is exact.
type PeriodTotals struct {
	From           string `json:"from"`
	To             string `json:"to"`
	Booked         int64  `json:"booked"`
	PickedUp       int64  `json:"pickedUp"`
	InTransit      int64  `json:"inTransit"`
	OutForDelivery int64  `json:"outForDelivery"`
	Delivered      int64  `json:"delivered"`
	NDR            int64  `json:"ndr"`
	RTO            int64  `json:"rto"`
	Cancelled      int64  `json:"cancelled"`
	Damaged        int64  `json:"damaged"`
	Lost           int64  `json:"lost"`
	SLABreaches    int64  `json:"slaBreaches"`
	RevenueMinor   int64  `json:"revenueMinor"`
	CODAmountMinor int64  `json:"codAmountMinor"`
	WeightGrams    int64  `json:"weightGrams"`
	// DeliveryRateBP is delivered as a proportion of booked, in basis points.
	// Integer, because a percentage rendered from a float is a rounding
	// argument waiting to happen.
	DeliveryRateBP int64 `json:"deliveryRateBasisPoints"`
	NDRRateBP      int64 `json:"ndrRateBasisPoints"`
}

// LiveBacklog is counted now, not rolled up.
type LiveBacklog struct {
	ByStatus map[string]int64 `json:"byStatus"`
	Total    int64            `json:"total"`
	OnHold   int64            `json:"onHold"`
}

// Alerts is what somebody has to do something about.
type Alerts struct {
	SLABreached        int64            `json:"slaBreached"`
	SLABreachedToday   int64            `json:"slaBreachedToday"`
	OpenNDR            int64            `json:"openNdr"`
	NDRByReason        map[string]int64 `json:"ndrByReason"`
	OpenExceptions     int64            `json:"openExceptions"`
	CriticalExceptions int64            `json:"criticalExceptions"`
}

// Movement is the "is the network actually moving" strip.
type Movement struct {
	ScansLastHour     int64            `json:"scansLastHour"`
	ActiveTrips       map[string]int64 `json:"activeTrips"`
	OpenBags          int64            `json:"openBags"`
	DraftManifests    int64            `json:"draftManifests"`
	ManifestsInFlight int64            `json:"manifestsInFlight"`
}

// MoneyInFlight is the finance number that belongs on an operations screen,
// because it is the one that grows quietly while nobody is looking at finance.
type MoneyInFlight struct {
	CODOutstandingMinor int64  `json:"codOutstandingMinor"`
	CODObligations      int64  `json:"codObligations"`
	CODAgedOver48h      int64  `json:"codAgedOver48h"`
	Currency            string `json:"currency"`
}

// ConsistencyNote tells the client which figures are exact and which are not,
// so a dashboard can label them honestly instead of implying everything is live.
type ConsistencyNote struct {
	PeriodTotals string `json:"periodTotals"`
	LiveBacklog  string `json:"liveBacklog"`
	Snapshots    string `json:"snapshots"`
}

var consistency = ConsistencyNote{
	PeriodTotals: "exact; maintained in the same transaction as each status change",
	LiveBacklog:  "live; counted at request time",
	Snapshots:    "captured periodically; every point carries its own capturedAt",
}

// Overview assembles the landing payload.
//
// Nine bounded queries rather than one join: each is individually indexed and
// individually cheap, and a join across rollup, shipments, NDR cases,
// exceptions and COD would produce a plan no index could serve.
func (s *Service) Overview(ctx context.Context, p *tenant.Principal, w Window) (*Overview, error) {
	from, to, err := normaliseWindow(w.From, w.To)
	if err != nil {
		return nil, err
	}

	totals, err := s.q.SummariseDailyStats(ctx, dbgen.SummariseDailyStatsParams{
		OrganizationID: p.OrganizationID,
		FromDate:       from, ToDate: to,
		OperatingUnitID: w.UnitID, ServiceID: w.ServiceID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}

	period := PeriodTotals{
		From: from.Format(dateLayout), To: to.Format(dateLayout),
		Booked: totals.Booked, PickedUp: totals.PickedUp, InTransit: totals.InTransit,
		OutForDelivery: totals.OutForDelivery, Delivered: totals.Delivered,
		NDR: totals.Ndr, RTO: totals.Rto, Cancelled: totals.Cancelled,
		Damaged: totals.Damaged, Lost: totals.Lost, SLABreaches: totals.SlaBreaches,
		RevenueMinor: totals.RevenueMinor, CODAmountMinor: totals.CodAmountMinor,
		WeightGrams: totals.WeightGrams,
	}
	period.DeliveryRateBP = rateBP(totals.Delivered, totals.Booked)
	period.NDRRateBP = rateBP(totals.Ndr, totals.Booked)

	live, err := s.liveBacklog(ctx, p, w.UnitID)
	if err != nil {
		return nil, err
	}
	alerts, err := s.alerts(ctx, p, w.UnitID)
	if err != nil {
		return nil, err
	}
	movement, err := s.movement(ctx, p)
	if err != nil {
		return nil, err
	}
	money, err := s.money(ctx, p)
	if err != nil {
		return nil, err
	}

	return &Overview{
		Period: period, Live: *live, Alerts: *alerts,
		Movement: *movement, Money: *money, Consistency: consistency,
	}, nil
}

func (s *Service) liveBacklog(
	ctx context.Context, p *tenant.Principal, unitID *int64,
) (*LiveBacklog, error) {
	rows, err := s.q.CountLiveShipmentsByStatus(ctx, dbgen.CountLiveShipmentsByStatusParams{
		OrganizationID: p.OrganizationID, OperatingUnitID: unitID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := &LiveBacklog{ByStatus: make(map[string]int64, len(rows))}
	for _, r := range rows {
		out.ByStatus[r.CurrentStatus] = r.Count
		out.Total += r.Count
	}

	held, err := s.q.CountHeldShipments(ctx, p.OrganizationID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out.OnHold = held
	return out, nil
}

func (s *Service) alerts(
	ctx context.Context, p *tenant.Principal, unitID *int64,
) (*Alerts, error) {
	sla, err := s.q.CountSLABreaches(ctx, dbgen.CountSLABreachesParams{
		OrganizationID: p.OrganizationID, OperatingUnitID: unitID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	ndr, err := s.q.CountOpenNDRCases(ctx, dbgen.CountOpenNDRCasesParams{
		OrganizationID: p.OrganizationID, OperatingUnitID: unitID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	exceptions, err := s.q.CountOpenExceptions(ctx, dbgen.CountOpenExceptionsParams{
		OrganizationID: p.OrganizationID, OperatingUnitID: unitID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}

	out := &Alerts{
		SLABreached: sla.Breached, SLABreachedToday: sla.BreachedToday,
		NDRByReason: make(map[string]int64, len(ndr)),
	}
	for _, r := range ndr {
		reason := r.ReasonCode
		if reason == "" {
			reason = "UNSPECIFIED"
		}
		out.NDRByReason[reason] += r.Count
		out.OpenNDR += r.Count
	}
	for _, e := range exceptions {
		out.OpenExceptions += e.Count
		if e.Severity == "CRITICAL" {
			out.CriticalExceptions += e.Count
		}
	}
	return out, nil
}

func (s *Service) movement(ctx context.Context, p *tenant.Principal) (*Movement, error) {
	scans, err := s.q.CountRecentScans(ctx, dbgen.CountRecentScansParams{
		OrganizationID: p.OrganizationID,
		Window:         pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	trips, err := s.q.CountActiveTrips(ctx, p.OrganizationID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	containers, err := s.q.CountOpenBagsAndManifests(ctx, p.OrganizationID)
	if err != nil {
		return nil, apierr.Internal(err)
	}

	out := &Movement{
		ScansLastHour: scans, ActiveTrips: make(map[string]int64, len(trips)),
		OpenBags: containers.OpenBags, DraftManifests: containers.DraftManifests,
		ManifestsInFlight: containers.ManifestsInFlight,
	}
	for _, t := range trips {
		out.ActiveTrips[t.Status] = t.Count
	}
	return out, nil
}

func (s *Service) money(ctx context.Context, p *tenant.Principal) (*MoneyInFlight, error) {
	cod, err := s.q.SummariseCODInCustody(ctx, p.OrganizationID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return &MoneyInFlight{
		CODOutstandingMinor: cod.OutstandingMinor,
		CODObligations:      cod.ObligationCount,
		CODAgedOver48h:      cod.AgedOver48h,
		Currency:            p.OrganizationCurrency,
	}, nil
}

// ---------------------------------------------------------------------------
// Series and league tables
// ---------------------------------------------------------------------------

// DayPoint is one point on a trend chart.
type DayPoint struct {
	Date           string `json:"date"`
	Booked         int64  `json:"booked"`
	Delivered      int64  `json:"delivered"`
	NDR            int64  `json:"ndr"`
	RTO            int64  `json:"rto"`
	SLABreaches    int64  `json:"slaBreaches"`
	RevenueMinor   int64  `json:"revenueMinor"`
	CODAmountMinor int64  `json:"codAmountMinor"`
}

// Trend returns the daily series for a window.
func (s *Service) Trend(ctx context.Context, p *tenant.Principal, w Window) ([]DayPoint, error) {
	from, to, err := normaliseWindow(w.From, w.To)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListDailyStatsByDate(ctx, dbgen.ListDailyStatsByDateParams{
		OrganizationID: p.OrganizationID, FromDate: from, ToDate: to,
		OperatingUnitID: w.UnitID,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]DayPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, DayPoint{
			Date: r.StatDate.Format(dateLayout), Booked: r.Booked, Delivered: r.Delivered,
			NDR: r.Ndr, RTO: r.Rto, SLABreaches: r.SlaBreaches,
			RevenueMinor: r.RevenueMinor, CODAmountMinor: r.CodAmountMinor,
		})
	}
	return out, nil
}

// UnitPerformance is one row of the league table.
type UnitPerformance struct {
	UnitID         string `json:"unitId"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	UnitType       string `json:"unitType"`
	Booked         int64  `json:"booked"`
	Delivered      int64  `json:"delivered"`
	NDR            int64  `json:"ndr"`
	RTO            int64  `json:"rto"`
	SLABreaches    int64  `json:"slaBreaches"`
	RevenueMinor   int64  `json:"revenueMinor"`
	DeliveryRateBP int64  `json:"deliveryRateBasisPoints"`
}

// ByUnit ranks operating units over a window.
func (s *Service) ByUnit(
	ctx context.Context, p *tenant.Principal, w Window, unitType string, limit int32,
) ([]UnitPerformance, error) {
	from, to, err := normaliseWindow(w.From, w.To)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListDailyStatsByUnit(ctx, dbgen.ListDailyStatsByUnitParams{
		OrganizationID: p.OrganizationID, FromDate: from, ToDate: to,
		UnitType: ops.Optional(unitType), RowLimit: clampLimit(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]UnitPerformance, 0, len(rows))
	for _, r := range rows {
		out = append(out, UnitPerformance{
			UnitID: r.UnitPublicID, Code: r.UnitCode, Name: r.UnitName,
			UnitType: r.UnitType, Booked: r.Booked, Delivered: r.Delivered,
			NDR: r.Ndr, RTO: r.Rto, SLABreaches: r.SlaBreaches,
			RevenueMinor: r.RevenueMinor, DeliveryRateBP: rateBP(r.Delivered, r.Booked),
		})
	}
	return out, nil
}

// ServicePerformance is one row of the per-product table.
type ServicePerformance struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	Booked         int64  `json:"booked"`
	Delivered      int64  `json:"delivered"`
	NDR            int64  `json:"ndr"`
	SLABreaches    int64  `json:"slaBreaches"`
	RevenueMinor   int64  `json:"revenueMinor"`
	DeliveryRateBP int64  `json:"deliveryRateBasisPoints"`
}

// ByService ranks courier services over a window.
func (s *Service) ByService(
	ctx context.Context, p *tenant.Principal, w Window, limit int32,
) ([]ServicePerformance, error) {
	from, to, err := normaliseWindow(w.From, w.To)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListDailyStatsByService(ctx, dbgen.ListDailyStatsByServiceParams{
		OrganizationID: p.OrganizationID, FromDate: from, ToDate: to,
		RowLimit: clampLimit(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]ServicePerformance, 0, len(rows))
	for _, r := range rows {
		out = append(out, ServicePerformance{
			Code: r.ServiceCode, Name: r.ServiceName, Booked: r.Booked,
			Delivered: r.Delivered, NDR: r.Ndr, SLABreaches: r.SlaBreaches,
			RevenueMinor: r.RevenueMinor, DeliveryRateBP: rateBP(r.Delivered, r.Booked),
		})
	}
	return out, nil
}

// UnitBacklog is where the parcels physically are.
type UnitBacklog struct {
	UnitID string `json:"unitId"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	Count  int64  `json:"count"`
}

// Backlog returns the live custody distribution.
func (s *Service) Backlog(
	ctx context.Context, p *tenant.Principal, limit int32,
) ([]UnitBacklog, error) {
	rows, err := s.q.CountBacklogByUnit(ctx, dbgen.CountBacklogByUnitParams{
		OrganizationID: p.OrganizationID, RowLimit: clampLimit(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]UnitBacklog, 0, len(rows))
	for _, r := range rows {
		out = append(out, UnitBacklog{
			UnitID: r.UnitPublicID, Code: r.UnitCode, Name: r.UnitName, Count: r.Count,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Snapshots
// ---------------------------------------------------------------------------

// SnapshotPoint is one captured backlog reading.
type SnapshotPoint struct {
	CapturedAt        time.Time `json:"capturedAt"`
	InCustody         int32     `json:"inCustody"`
	AwaitingPickup    int32     `json:"awaitingPickup"`
	Inbound           int32     `json:"inbound"`
	Outbound          int32     `json:"outbound"`
	ReadyForDelivery  int32     `json:"readyForDelivery"`
	NDROpen           int32     `json:"ndrOpen"`
	ExceptionsOpen    int32     `json:"exceptionsOpen"`
	CODInCustodyMinor int64     `json:"codInCustodyMinor"`
}

// Snapshots returns the captured backlog series.
//
// The one eventually-consistent thing in this package. Every point carries its
// own capturedAt so a chart states its age rather than implying it is live.
func (s *Service) Snapshots(
	ctx context.Context, p *tenant.Principal, since time.Time, unitID *int64, limit int32,
) ([]SnapshotPoint, error) {
	rows, err := s.q.ListOperationalSnapshots(ctx, dbgen.ListOperationalSnapshotsParams{
		OrganizationID: p.OrganizationID, Since: since,
		OperatingUnitID: unitID, RowLimit: clampLimit(limit),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make([]SnapshotPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, SnapshotPoint{
			CapturedAt: r.CapturedAt, InCustody: r.InCustodyCount,
			AwaitingPickup: r.AwaitingPickupCount, Inbound: r.InboundCount,
			Outbound: r.OutboundCount, ReadyForDelivery: r.ReadyForDeliveryCount,
			NDROpen: r.NdrOpenCount, ExceptionsOpen: r.ExceptionOpenCount,
			CODInCustodyMinor: r.CodInCustodyMinor,
		})
	}
	return out, nil
}

// CaptureSnapshot records one point for a tenant.
//
// Called by the worker on a timer. It runs the same live counts the dashboard
// does and stores the result, so a trend chart does not have to reconstruct
// history that was never recorded.
func (s *Service) CaptureSnapshot(ctx context.Context, orgID int64) error {
	counts, err := s.q.CountLiveShipmentsByStatus(ctx, dbgen.CountLiveShipmentsByStatusParams{
		OrganizationID: orgID,
	})
	if err != nil {
		return err
	}
	byStatus := map[string]int64{}
	var inCustody int64
	for _, c := range counts {
		byStatus[c.CurrentStatus] = c.Count
		inCustody += c.Count
	}

	ndr, err := s.q.CountOpenNDRCases(ctx, dbgen.CountOpenNDRCasesParams{OrganizationID: orgID})
	if err != nil {
		return err
	}
	var ndrOpen int64
	for _, r := range ndr {
		ndrOpen += r.Count
	}

	exceptions, err := s.q.CountOpenExceptions(ctx, dbgen.CountOpenExceptionsParams{
		OrganizationID: orgID,
	})
	if err != nil {
		return err
	}
	var exceptionsOpen int64
	for _, e := range exceptions {
		exceptionsOpen += e.Count
	}

	cod, err := s.q.SummariseCODInCustody(ctx, orgID)
	if err != nil {
		return err
	}

	_, err = s.q.CaptureOperationalSnapshot(ctx, dbgen.CaptureOperationalSnapshotParams{
		OrganizationID: orgID,
		// Network-wide. A per-unit series is a future refinement; capturing one
		// row per unit per interval would multiply this table by the size of
		// the network for a chart nobody has asked for yet.
		OperatingUnitID:       nil,
		InCustodyCount:        int32(inCustody),
		AwaitingPickupCount:   int32(byStatus[string(shipment.StatusPickupScheduled)] + byStatus[string(shipment.StatusPickupAssigned)]),
		InboundCount:          int32(byStatus[string(shipment.StatusInTransit)] + byStatus[string(shipment.StatusTransitHubReceived)]),
		OutboundCount:         int32(byStatus[string(shipment.StatusOriginDispatched)] + byStatus[string(shipment.StatusTransitHubDispatched)]),
		ReadyForDeliveryCount: int32(byStatus[string(shipment.StatusDestinationBranchReceived)] + byStatus[string(shipment.StatusOutForDelivery)]),
		NdrOpenCount:          int32(ndrOpen),
		ExceptionOpenCount:    int32(exceptionsOpen),
		CodInCustodyMinor:     cod.OutstandingMinor,
	})
	return err
}

// RegisterHandlers wires snapshot capture into a worker.
func (s *Service) RegisterHandlers(w *jobs.Worker) {
	w.Register(JobTypeSnapshot, func(ctx context.Context, job jobs.Job) error {
		var payload struct {
			OrganizationID int64 `json:"organizationId"`
		}
		if err := job.Decode(&payload); err != nil {
			return err
		}
		return s.CaptureSnapshot(ctx, payload.OrganizationID)
	})
}

// CaptureAll snapshots every active tenant. Called from the worker's timer.
func (s *Service) CaptureAll(ctx context.Context) (int, error) {
	ids, err := s.q.ListOrganizationIDs(ctx)
	if err != nil {
		return 0, err
	}
	captured := 0
	for _, id := range ids {
		if err := s.CaptureSnapshot(ctx, id); err != nil {
			// One tenant's failure must not stop the sweep for the rest.
			s.log.Warn("could not capture an operational snapshot",
				"organizationId", id, "error", err)
			continue
		}
		captured++
	}
	return captured, nil
}

// PurgeSnapshots drops readings older than the retention window.
//
// The table's guard refuses to delete anything younger than 90 days whatever
// this is asked for, so a mistake here shortens nothing: the floor lives in the
// database, not in the caller.
func (s *Service) PurgeSnapshots(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.q.PurgeOldSnapshots(ctx, pgtype.Interval{
		Microseconds: olderThan.Microseconds(), Valid: true,
	})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

const dateLayout = "2006-01-02"

// maxWindowDays bounds a dashboard query. Reading the rollup is cheap per day,
// but "cheap per day" times an unbounded range is still unbounded.
const maxWindowDays = 400

func normaliseWindow(from, to time.Time) (time.Time, time.Time, error) {
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.AddDate(0, 0, -29)
	}
	from = from.UTC().Truncate(24 * time.Hour)
	to = to.UTC().Truncate(24 * time.Hour)
	if to.Before(from) {
		return time.Time{}, time.Time{}, apierr.Validation(
			"The end of the window is before its start.",
			map[string]any{"from": from.Format(dateLayout), "to": to.Format(dateLayout)})
	}
	if to.Sub(from) > maxWindowDays*24*time.Hour {
		return time.Time{}, time.Time{}, apierr.Validation(
			"The window is too wide for a dashboard query. Use a report for a longer period.",
			map[string]any{"maxDays": maxWindowDays})
	}
	return from, to, nil
}

// rateBP expresses part/whole in basis points.
//
// Integer arithmetic, like every other proportion in the platform: a percentage
// carried as a float is a rounding argument waiting to happen, and this one is
// read off a screen next to a count that has to agree with it.
func rateBP(part, whole int64) int64 {
	if whole <= 0 {
		return 0
	}
	return part * 10_000 / whole
}

func clampLimit(n int32) int32 {
	switch {
	case n <= 0:
		return 20
	case n > 200:
		return 200
	default:
		return n
	}
}
