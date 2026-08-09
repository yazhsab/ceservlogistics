package notification

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// statusEvents maps a shipment status to the notification event it raises.
//
// Only milestones a recipient would want a message about. A parcel being bagged
// at the origin branch is a real event and it is on the tracking page; texting
// someone about it would be noise, and noise is how a customer learns to ignore
// the message that actually mattered.
var statusEvents = map[shipment.Status]string{
	shipment.StatusBooked:         EventShipmentBooked,
	shipment.StatusPickedUp:       EventPickupCompleted,
	shipment.StatusInTransit:      EventShipmentInTransit,
	shipment.StatusOutForDelivery: EventShipmentOutForDelivery,
	shipment.StatusDelivered:      EventShipmentDelivered,
	shipment.StatusNDR:            EventShipmentNDR,
	shipment.StatusDeliveryFailed: EventShipmentNDR,
	shipment.StatusRTOInitiated:   EventShipmentRTO,
}

// ShipmentObserver notifies the recipient about milestones.
//
// Registered on the transition engine, so no module has to remember to call it.
// It runs inside the transition's transaction and only writes rows — the
// provider is contacted later by a worker, which is what keeps a shipment's
// success independent of any SMS gateway's availability.
//
// It never returns an error for a business reason. A missing template, a
// recipient with no phone number, an opted-out customer: each produces a
// SUPPRESSED notification row saying so. Failing a delivery scan because a text
// message could not be composed would be absurd.
func (s *Service) ShipmentObserver(trackingBase string) shipment.Observer {
	return func(ctx context.Context, tx pgx.Tx, p *tenant.Principal, res *shipment.Result) error {
		eventType, ok := statusEvents[res.To]
		if !ok {
			return nil
		}
		sh := res.Shipment
		q := s.q.WithTx(tx)

		recipient, err := s.recipientOf(ctx, q, sh.ID)
		if err != nil {
			// A shipment with no recipient snapshot is a data problem worth
			// knowing about, but not one that should block a scan.
			s.log.Warn("could not resolve a notification recipient",
				"shipmentId", sh.PublicID, "error", err)
			return nil
		}

		vars := map[string]string{
			"awb":              sh.Awb,
			"organizationName": p.OrganizationCode,
			"trackingUrl":      trackingBase + sh.Awb,
			"recipientName":    recipient.ContactName,
			"destinationCity":  recipient.CityName,
			"status":           string(res.To),
			"deliveredAt":      res.Event.OccurredAt.Format("2 Jan 2006 15:04"),
			"receivedBy":       recipient.ContactName,
			"codLine":          "",
			"reason":           "",
			"nextAttemptDate":  "",
		}
		if sh.PaymentMode == "COD" && sh.CodAmountMinor > 0 {
			vars["codLine"] = " Please keep " +
				strconv.FormatInt(sh.CodAmountMinor/100, 10) + " ready."
		}
		if res.Event.ReasonCode != nil {
			vars["reason"] = *res.Event.ReasonCode
		}

		// The dedupe key is the transition, not the status: a parcel that is
		// re-scanned into transit at three hubs stays one IN_TRANSIT message,
		// while a genuine second delivery attempt is a genuine second message.
		dedupe := "ship:" + sh.PublicID + ":" + string(res.To)

		// SMS is the channel every recipient has. Email is sent as well when
		// there is an address, because a delivery confirmation is the one people
		// go looking for later.
		if _, err := s.Raise(ctx, tx, p, Request{
			EventType: eventType, Channel: ChannelSMS,
			RecipientType: "CUSTOMER", RecipientAddress: recipient.Phone,
			RecipientName: recipient.ContactName,
			Variables:     vars, ShipmentID: &sh.ID,
			Reference: sh.Awb, DedupeKey: dedupe + ":SMS",
		}); err != nil {
			return err
		}
		if recipient.Email != nil && *recipient.Email != "" {
			if _, err := s.Raise(ctx, tx, p, Request{
				EventType: eventType, Channel: ChannelEmail,
				RecipientType: "CUSTOMER", RecipientAddress: *recipient.Email,
				RecipientName: recipient.ContactName,
				Variables:     vars, ShipmentID: &sh.ID,
				Reference: sh.Awb, DedupeKey: dedupe + ":EMAIL",
			}); err != nil {
				return err
			}
		}
		return nil
	}
}

func (s *Service) recipientOf(
	ctx context.Context, q *dbgen.Queries, shipmentID int64,
) (*dbgen.ShipmentAddressSnapshot, error) {
	rows, err := q.ListShipmentAddressSnapshots(ctx, shipmentID)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].Role == "RECIPIENT" {
			return &rows[i], nil
		}
	}
	return nil, errNoRecipient
}

var errNoRecipient = errors.New("shipment has no recipient address snapshot")
