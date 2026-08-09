// Package tracking implements M20: public shipment tracking by AWB.
//
// This is the only unauthenticated endpoint in the system that reads business
// data, so its design is subtractive: it starts from "expose nothing" and adds
// back only what a consignee needs to know.
//
// Never exposed (§M20):
//
//	internal notes          shipment_events.internal_remarks is not selected
//	employee identities     no actor name reaches the response
//	facility metadata       only a city, and only when the milestone permits it
//	finance                 no rate card, no charges beyond the COD due
//	internal public ids     no shp_, no ou_, no cus_ — only the AWB
//
// The event stream is normalised into milestones so the customer sees a
// journey, not a scan log. The mapping is data (tracking_milestones), so an
// operator can retitle a milestone without a deployment.
package tracking

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/cache"
	"github.com/ceserve/courier-os/internal/platform/telemetry"
	"github.com/ceserve/courier-os/internal/shipment"
)

// Milestone codes, in customer-facing order.
var MilestoneCodes = []string{
	"BOOKED", "PICKUP", "IN_TRANSIT", "OUT_FOR_DELIVERY", "DELIVERED",
	"EXCEPTION", "RETURNING", "RETURNED", "CANCELLED",
}

// Event is one entry on the public timeline.
type Event struct {
	Milestone   string    `json:"milestone"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Location    string    `json:"location,omitempty"`
	OccurredAt  time.Time `json:"occurredAt"`
}

// Result is the public tracking response.
type Result struct {
	AWB              string     `json:"awb"`
	Status           string     `json:"status"`
	Milestone        string     `json:"milestone"`
	StatusTitle      string     `json:"statusTitle"`
	StatusDetail     string     `json:"statusDescription"`
	Service          string     `json:"service,omitempty"`
	Origin           string     `json:"origin,omitempty"`
	Destination      string     `json:"destination,omitempty"`
	PieceCount       int        `json:"pieceCount"`
	BookedAt         time.Time  `json:"bookedAt"`
	LastUpdatedAt    time.Time  `json:"lastUpdatedAt"`
	ExpectedDelivery *string    `json:"expectedDelivery,omitempty"`
	DeliveredAt      *time.Time `json:"deliveredAt,omitempty"`
	// AmountDueMinor is shown only for COD, because the recipient needs to have
	// the money ready. No other financial figure is published.
	AmountDueMinor *int64  `json:"amountDueOnDeliveryMinor,omitempty"`
	Currency       string  `json:"currency,omitempty"`
	IsReturning    bool    `json:"isReturning"`
	Events         []Event `json:"events"`
}

// Service resolves public tracking queries.
type Service struct {
	q       *dbgen.Queries
	cache   *cache.Cache
	log     *slog.Logger
	metrics *telemetry.Metrics
	ttl     time.Duration
	// maxEvents bounds the timeline. A parcel with a pathological event history
	// must not be able to produce an unbounded response.
	maxEvents int32
}

// NewService builds the tracking service.
func NewService(q *dbgen.Queries, c *cache.Cache, log *slog.Logger, m *telemetry.Metrics, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &Service{q: q, cache: c, log: log, metrics: m, ttl: ttl, maxEvents: 100}
}

// Track resolves an AWB to a customer-safe timeline.
//
// The AWB is the only input, and it is deliberately not tenant-scoped: AWBs are
// globally unique because a consignee tracking a parcel has no idea which
// tenant carried it. What protects the data is that the response contains
// nothing worth harvesting — no names, no addresses, no identifiers that lead
// anywhere else — plus aggressive rate limiting on the route.
func (s *Service) Track(ctx context.Context, awb string) (*Result, error) {
	normalized := normalizeAWB(awb)
	if !shipment.ValidAWB(normalized) {
		// Deliberately the same answer as a real miss, so the endpoint cannot be
		// used to learn which AWB formats a tenant uses.
		return nil, notFound()
	}

	key := s.cache.Key("track", normalized)
	var cached Result
	if err := s.cache.GetJSON(ctx, key, &cached); err == nil && cached.AWB != "" {
		s.metrics.RecordBusiness("tracking", "cache_hit")
		return &cached, nil
	}

	row, err := s.q.GetPublicTrackingShipment(ctx, normalized)
	if err != nil {
		if isNoRows(err) {
			s.metrics.RecordBusiness("tracking", "not_found")
			return nil, notFound()
		}
		return nil, apierr.Internal(err)
	}

	milestones, err := s.loadMilestones(ctx, row.OrganizationID)
	if err != nil {
		return nil, err
	}
	current := milestones[row.CurrentStatus]

	result := &Result{
		AWB: row.Awb, Status: row.CurrentStatus,
		Milestone: current.code, StatusTitle: current.title, StatusDetail: current.description,
		Service:     row.ServiceName,
		Origin:      cityOrRegion(row.OriginCity, row.OriginPincode),
		Destination: cityOrRegion(row.DestinationCity, row.DestinationPincode),
		PieceCount:  int(row.PieceCount),
		BookedAt:    row.BookedAt, LastUpdatedAt: row.StatusChangedAt,
		DeliveredAt: row.DeliveredAt,
		IsReturning: row.MovementDirection == string(shipment.DirectionReverse),
		Events:      []Event{},
	}
	if row.PaymentMode == "COD" && row.CodAmountMinor > 0 {
		amount := row.CodAmountMinor
		result.AmountDueMinor = &amount
		result.Currency = row.Currency
	}

	if est, eErr := s.q.GetShipmentDeliveryEstimate(ctx, row.ShipmentID); eErr == nil {
		if d := expectedDelivery(est, row.CurrentStatus); d != "" {
			result.ExpectedDelivery = &d
		}
	}

	events, err := s.q.ListPublicTrackingEvents(ctx, dbgen.ListPublicTrackingEventsParams{
		ShipmentID: row.ShipmentID, PageSize: s.maxEvents,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	result.Events = s.buildTimeline(events, milestones)

	// A short TTL: tracking is read far more often than a parcel moves, and a
	// minute of staleness is invisible to a customer but removes most of the
	// database load from the busiest public endpoint.
	s.cache.SetJSON(ctx, key, result, s.ttl)
	s.metrics.RecordBusiness("tracking", "resolved")
	return result, nil
}

// buildTimeline turns raw events into customer-facing milestones.
//
// Consecutive events that map to the same milestone are collapsed — a parcel
// crossing four transit hubs should read as "in transit", updated, not as four
// near-identical lines — except where the location changed, which is the one
// thing a customer actually wants from a transit update.
func (s *Service) buildTimeline(
	rows []dbgen.ListPublicTrackingEventsRow, milestones map[string]milestone,
) []Event {
	out := make([]Event, 0, len(rows))
	var lastMilestone, lastLocation string

	for _, r := range rows {
		m, ok := milestones[r.ToStatus]
		if !ok {
			// A status with no configured milestone is not published at all,
			// rather than leaked with a raw status string.
			continue
		}
		location := ""
		if m.showLocation {
			location = publicLocation(r)
		}
		if m.code == lastMilestone && location == lastLocation {
			continue
		}
		out = append(out, Event{
			Milestone: m.code, Title: m.title,
			// The stored description is written for operations; the public text
			// comes from the milestone configuration.
			Description: m.description,
			Location:    location, OccurredAt: r.OccurredAt,
		})
		lastMilestone, lastLocation = m.code, location
	}
	return out
}

// publicLocation reduces a facility to a city.
//
// Naming the facility would tell an observer where a courier network's sort
// hubs are and which one is currently holding a given parcel. A city is enough
// for a customer and useless to anyone else.
func publicLocation(r dbgen.ListPublicTrackingEventsRow) string {
	if r.LocationCity != nil && *r.LocationCity != "" {
		return *r.LocationCity
	}
	if r.LocationPincode != nil && *r.LocationPincode != "" {
		return *r.LocationPincode
	}
	return ""
}

func cityOrRegion(city *string, pincode string) string {
	if city != nil && *city != "" {
		return *city
	}
	return pincode
}

// expectedDelivery gives the customer a date they can plan around.
func expectedDelivery(est dbgen.GetShipmentDeliveryEstimateRow, status string) string {
	if shipment.IsTerminal(shipment.Status(status)) {
		return ""
	}
	// A rescheduled NDR beats the original promise: showing the stale promise
	// after the customer themselves asked for a later day is worse than showing
	// nothing.
	if est.NdrNextAttemptAt != nil {
		return est.NdrNextAttemptAt.Format("2006-01-02")
	}
	if est.PromisedDeliveryAt != nil {
		return est.PromisedDeliveryAt.Format("2006-01-02")
	}
	return ""
}

type milestone struct {
	code         string
	title        string
	description  string
	showLocation bool
}

// loadMilestones resolves the tenant's milestone wording, falling back to the
// platform defaults.
func (s *Service) loadMilestones(ctx context.Context, orgID int64) (map[string]milestone, error) {
	rows, err := s.q.ListTrackingMilestones(ctx, &orgID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	out := make(map[string]milestone, len(rows))
	for _, r := range rows {
		out[r.ShipmentStatus] = milestone{
			code: r.MilestoneCode, title: r.PublicTitle,
			description: r.PublicDescription, showLocation: r.ShowLocation,
		}
	}
	return out, nil
}

// notFound is the single answer for every miss: an unknown AWB, a malformed
// one, and a well-formed one that does not exist are indistinguishable.
func notFound() error {
	return apierr.NotFound("Shipment").
		WithDetail("hint", "Check the tracking number and try again. It can take a short time to appear after booking.")
}

func normalizeAWB(awb string) string {
	return strings.ToUpper(strings.TrimSpace(awb))
}

func isNoRows(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no rows")
}
