package partner

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// The event catalogue.
//
// These are *business* events, not database rows: "shipment.delivered" is a
// promise about meaning that will outlive any particular column. A partner
// builds against them, so adding one is cheap and changing what one means is
// not — a renamed event silently stops firing for every consumer subscribed to
// the old name.
const (
	EventShipmentBooked        = "shipment.booked"
	EventShipmentPickedUp      = "shipment.picked_up"
	EventShipmentInTransit     = "shipment.in_transit"
	EventShipmentOutForDeliver = "shipment.out_for_delivery"
	EventShipmentDelivered     = "shipment.delivered"
	EventShipmentDeliveryFail  = "shipment.delivery_failed"
	EventShipmentNDR           = "shipment.ndr"
	EventShipmentRTOInitiated  = "shipment.rto_initiated"
	EventShipmentRTODelivered  = "shipment.rto_delivered"
	EventShipmentCancelled     = "shipment.cancelled"
	EventShipmentLost          = "shipment.lost"
	EventShipmentDamaged       = "shipment.damaged"
	EventPickupCompleted       = "pickup.completed"
	EventPODCaptured           = "pod.captured"
	EventCODCollected          = "cod.collected"
)

// AllEvents is the catalogue a partner may subscribe to.
var AllEvents = []string{
	EventShipmentBooked, EventShipmentPickedUp, EventShipmentInTransit,
	EventShipmentOutForDeliver, EventShipmentDelivered, EventShipmentDeliveryFail,
	EventShipmentNDR, EventShipmentRTOInitiated, EventShipmentRTODelivered,
	EventShipmentCancelled, EventShipmentLost, EventShipmentDamaged,
	EventPickupCompleted, EventPODCaptured, EventCODCollected,
}

// IsKnownEvent rejects a typo at subscription time rather than leaving a
// partner waiting for an event that will never fire.
func IsKnownEvent(e string) bool {
	for _, known := range AllEvents {
		if known == e {
			return true
		}
	}
	return false
}

// SignatureTolerance is how old a timestamp a consumer should accept. Published
// so the contract and the code cannot disagree about it.
func SignatureTolerance() time.Duration { return signatureTolerance }

// statusEvents maps a shipment status to the partner event it raises.
//
// Not every status is published. A partner cares that a parcel moved, not that
// it was bagged: internal handling steps are operational detail, and publishing
// them would both leak network structure and bury the events that matter under
// ones that do not.
var statusEvents = map[shipment.Status]string{
	shipment.StatusBooked:                    EventShipmentBooked,
	shipment.StatusPickedUp:                  EventShipmentPickedUp,
	shipment.StatusInTransit:                 EventShipmentInTransit,
	shipment.StatusOutForDelivery:            EventShipmentOutForDeliver,
	shipment.StatusDelivered:                 EventShipmentDelivered,
	shipment.StatusDeliveryFailed:            EventShipmentDeliveryFail,
	shipment.StatusNDR:                       EventShipmentNDR,
	shipment.StatusRTOInitiated:              EventShipmentRTOInitiated,
	shipment.StatusRTODelivered:              EventShipmentRTODelivered,
	shipment.StatusCancelled:                 EventShipmentCancelled,
	shipment.StatusLost:                      EventShipmentLost,
	shipment.StatusDamaged:                   EventShipmentDamaged,
	shipment.StatusDestinationBranchReceived: EventShipmentInTransit,
}

// ShipmentObserver publishes shipment state changes to subscribed partners.
//
// Registered on the transition engine, so it fires for every state change in
// the platform without any module having to remember to call it — a module that
// forgets is a partner integration that silently misses events.
//
// It runs inside the transition's transaction and only writes rows. The HTTP
// call happens later, on a worker.
func (s *WebhookService) ShipmentObserver() shipment.Observer {
	return func(ctx context.Context, tx pgx.Tx, p *tenant.Principal, res *shipment.Result) error {
		eventType, ok := statusEvents[res.To]
		if !ok {
			return nil
		}
		sh := res.Shipment

		// The event id is the shipment event's public id: stable, unique, and
		// already the identity of "this thing happened once". A retry that
		// re-runs the transition cannot produce a second one, which is what
		// makes both our dedupe index and the consumer's dedupe work.
		_, err := s.Emit(ctx, tx, p, eventType, res.Event.PublicID, map[string]any{
			"shipmentId": sh.PublicID,
			"awb":        sh.Awb,
			"status":     string(res.To),
			"fromStatus": string(res.From),
			"occurredAt": res.Event.OccurredAt.UTC().Format(time.RFC3339),
			"reference":  sh.ReferenceNumber,
			"reasonCode": res.Event.ReasonCode,
		}, &sh.ID)
		return err
	}
}

// ---------------------------------------------------------------------------
// Endpoint and delivery administration
// ---------------------------------------------------------------------------

// ListEndpoints returns the tenant's endpoints. Never returns signing secrets.
func (s *WebhookService) ListEndpoints(
	ctx context.Context, p *tenant.Principal, limit, offset int32,
) ([]dbgen.ListWebhookEndpointsRow, error) {
	return s.q.ListWebhookEndpoints(ctx, dbgen.ListWebhookEndpointsParams{
		OrganizationID: p.OrganizationID, Limit: limit, Offset: offset,
	})
}

// GetEndpoint resolves an endpoint by public id within the tenant.
func (s *WebhookService) GetEndpoint(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.WebhookEndpoint, error) {
	e, err := s.q.GetWebhookEndpointByPublicID(ctx, dbgen.GetWebhookEndpointByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Webhook endpoint")
	}
	return &e, nil
}

// SetEndpointStatus pauses, resumes or disables an endpoint.
//
// Resuming clears the consecutive-failure counter: an operator who has fixed
// their server should get the full failure budget again, not the one attempt
// left over from before.
func (s *WebhookService) SetEndpointStatus(
	ctx context.Context, p *tenant.Principal, publicID, status, reason string,
) (*dbgen.WebhookEndpoint, error) {
	endpoint, err := s.GetEndpoint(ctx, p, publicID)
	if err != nil {
		return nil, err
	}
	updated, err := s.q.SetWebhookEndpointStatus(ctx, dbgen.SetWebhookEndpointStatusParams{
		OrganizationID: p.OrganizationID, ID: endpoint.ID,
		Status: status, DisabledReason: ops.Optional(reason),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "webhook.endpoint.status_changed", ResourceType: "webhook_endpoint",
		ResourceID: &endpoint.ID, ResourcePublicID: endpoint.PublicID, Reason: reason,
		Before: map[string]any{"status": endpoint.Status},
		After:  map[string]any{"status": updated.Status},
	}))
	return &updated, nil
}

// Subscribe adds an event to an endpoint. Idempotent: re-subscribing to an
// event already held reactivates it rather than failing.
func (s *WebhookService) Subscribe(
	ctx context.Context, p *tenant.Principal, endpointPublicID, eventType string,
) (*dbgen.WebhookSubscription, error) {
	if !IsKnownEvent(eventType) {
		return nil, apierr.Validation("Unknown event type.",
			map[string]any{"eventType": eventType, "allowed": AllEvents})
	}
	endpoint, err := s.GetEndpoint(ctx, p, endpointPublicID)
	if err != nil {
		return nil, err
	}
	sub, err := s.q.CreateWebhookSubscription(ctx, dbgen.CreateWebhookSubscriptionParams{
		PublicID: publicid.New("whs"), OrganizationID: p.OrganizationID,
		EndpointID: endpoint.ID, EventType: eventType, Filter: []byte("{}"),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return &sub, nil
}

// Unsubscribe removes an event from an endpoint.
func (s *WebhookService) Unsubscribe(
	ctx context.Context, p *tenant.Principal, endpointPublicID, eventType string,
) error {
	endpoint, err := s.GetEndpoint(ctx, p, endpointPublicID)
	if err != nil {
		return err
	}
	if err := s.q.DeleteWebhookSubscription(ctx, dbgen.DeleteWebhookSubscriptionParams{
		OrganizationID: p.OrganizationID, EndpointID: endpoint.ID, EventType: eventType,
	}); err != nil {
		return apierr.Internal(err)
	}
	return nil
}

// ListSubscriptions returns what an endpoint has asked for.
func (s *WebhookService) ListSubscriptions(
	ctx context.Context, p *tenant.Principal, endpointPublicID string,
) ([]dbgen.WebhookSubscription, error) {
	endpoint, err := s.GetEndpoint(ctx, p, endpointPublicID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListWebhookSubscriptions(ctx, endpoint.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// DeliveryFilter narrows a delivery listing.
type DeliveryFilter struct {
	EndpointID *int64
	Status     string
	EventType  string
	CursorID   *int64
	Limit      int32
}

// ListDeliveries returns delivery history, newest first, by keyset cursor.
//
// Keyset rather than offset because this table grows with every event times
// every subscriber, and an operator debugging an integration always starts at
// the newest page (§27).
func (s *WebhookService) ListDeliveries(
	ctx context.Context, p *tenant.Principal, f DeliveryFilter,
) ([]dbgen.ListWebhookDeliveriesRow, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	rows, err := s.q.ListWebhookDeliveries(ctx, dbgen.ListWebhookDeliveriesParams{
		OrganizationID: p.OrganizationID, EndpointID: f.EndpointID,
		Status: ops.Optional(f.Status), EventType: ops.Optional(f.EventType),
		CursorID: f.CursorID, Limit: f.Limit,
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// GetDelivery returns one delivery, including the exact payload that was sent.
func (s *WebhookService) GetDelivery(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.GetDeliveryByPublicIDRow, error) {
	d, err := s.q.GetDeliveryByPublicID(ctx, dbgen.GetDeliveryByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Webhook delivery")
	}
	return &d, nil
}

// ListAttempts returns every attempt made on a delivery, in order.
func (s *WebhookService) ListAttempts(
	ctx context.Context, p *tenant.Principal, publicID string,
) ([]dbgen.WebhookAttempt, error) {
	d, err := s.GetDelivery(ctx, p, publicID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListWebhookAttempts(ctx, d.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// ReclaimStuck returns deliveries abandoned by a crashed worker to PENDING.
//
// The claim is strictly PENDING -> SENDING, so a worker that dies mid-send
// leaves its row SENDING forever without this. Called by the worker on a timer.
func (s *WebhookService) ReclaimStuck(
	ctx context.Context, orgID *int64, staleAfter time.Duration,
) (int, error) {
	rows, err := s.q.ReclaimStuckDeliveries(ctx, dbgen.ReclaimStuckDeliveriesParams{
		OrganizationID: orgID,
		StaleAfter:     interval(staleAfter),
	})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// SweepDue re-enqueues webhook deliveries that are due but have no job behind
// them, for the same reason SweepDue exists on the notification service: Emit()
// logs and continues when the enqueue fails, so a delivery row can outlive its
// job. Repeating it is safe — the dedupe key is the delivery's public id.
func (s *WebhookService) SweepDue(ctx context.Context, limit int32) (int, error) {
	rows, err := s.q.ListDueWebhookDeliveries(ctx, dbgen.ListDueWebhookDeliveriesParams{
		RowLimit: limit,
	})
	if err != nil {
		return 0, err
	}
	requeued := 0
	for _, row := range rows {
		id, err := s.jobs.Enqueue(ctx, JobTypeWebhookSend, map[string]any{
			"organizationId": row.OrganizationID,
			"deliveryId":     row.ID,
		}, jobs.EnqueueOptions{
			OrganizationID: &row.OrganizationID,
			Queue:          "webhooks",
			DedupeKey:      "whd:" + row.PublicID,
			MaxAttempts:    6,
		})
		if err != nil {
			s.log.Warn("could not re-enqueue a due webhook delivery",
				"deliveryId", row.PublicID, "error", err)
			continue
		}
		if id != "" {
			requeued++
		}
	}
	return requeued, nil
}
