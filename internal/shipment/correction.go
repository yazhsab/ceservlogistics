package shipment

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/validate"
	"github.com/ceserve/courier-os/internal/tenant"
)

// CorrectionAddress contains fields that can be corrected without changing the
// resolved geography, route or price. City, state, postal code and country stay
// on the immutable booking snapshot.
type CorrectionAddress struct {
	ContactName string `json:"contactName"`
	CompanyName string `json:"companyName,omitempty"`
	Phone       string `json:"phone"`
	AltPhone    string `json:"altPhone,omitempty"`
	Email       string `json:"email,omitempty"`
	Line1       string `json:"line1"`
	Line2       string `json:"line2,omitempty"`
	Landmark    string `json:"landmark,omitempty"`
}

// CorrectionRequest is deliberately narrower than BookingRequest. Corrections
// retain the AWB and may therefore change only facts that do not affect routing,
// serviceability, pricing, credit, customs or physical package identity.
type CorrectionRequest struct {
	ExpectedVersion     int32             `json:"expectedVersion"`
	Reason              string            `json:"reason"`
	ReferenceNumber     string            `json:"referenceNumber,omitempty"`
	ContentDescription  string            `json:"contentDescription"`
	SpecialInstructions string            `json:"specialInstructions,omitempty"`
	IsFragile           bool              `json:"isFragile,omitempty"`
	Sender              CorrectionAddress `json:"sender"`
	Recipient           CorrectionAddress `json:"recipient"`
}

func ValidateCorrection(req *CorrectionRequest) error {
	v := validate.New()
	if req.ExpectedVersion < 1 {
		v.Add("expectedVersion", "A current shipment version is required.")
	}
	req.Reason = v.Text("reason", req.Reason, 5, 500, true)
	req.ReferenceNumber = v.Text("referenceNumber", req.ReferenceNumber, 0, 64, false)
	req.ContentDescription = v.Text("contentDescription", req.ContentDescription, 2, 500, true)
	req.SpecialInstructions = v.Text("specialInstructions", req.SpecialInstructions, 0, 1000, false)
	validateCorrectionAddress(v, "sender", &req.Sender)
	validateCorrectionAddress(v, "recipient", &req.Recipient)
	return v.Err()
}

func validateCorrectionAddress(v *validate.Validator, prefix string, a *CorrectionAddress) {
	a.ContactName = v.Text(prefix+".contactName", a.ContactName, 2, 160, true)
	a.CompanyName = v.Text(prefix+".companyName", a.CompanyName, 0, 160, false)
	a.Phone = v.Phone(prefix+".phone", a.Phone, true)
	a.AltPhone = v.Phone(prefix+".altPhone", a.AltPhone, false)
	if a.Email != "" {
		a.Email = v.Email(prefix+".email", a.Email)
	}
	a.Line1 = v.Text(prefix+".line1", a.Line1, 3, 200, true)
	a.Line2 = v.Text(prefix+".line2", a.Line2, 0, 200, false)
	a.Landmark = v.Text(prefix+".landmark", a.Landmark, 0, 120, false)
}

// Correct applies a pre-pickup correction while preserving the shipment AWB.
// The booking snapshots are never rewritten: address corrections are append-only
// overlays, and both the event stream and audit log retain the reason and actor.
func (b *Booker) Correct(
	ctx context.Context, p *tenant.Principal, shipmentID string, req CorrectionRequest,
) (*Detail, error) {
	if err := ValidateCorrection(&req); err != nil {
		return nil, err
	}

	var detail *Detail
	err := b.db.InTx(ctx, func(tx pgx.Tx) error {
		q := b.q.WithTx(tx)
		locked, err := q.LockShipmentForUpdate(ctx, dbgen.LockShipmentForUpdateParams{
			PublicID: shipmentID, OrganizationID: p.OrganizationID,
		})
		if err != nil {
			if database.IsNoRows(err) {
				return apierr.NotFound("Shipment")
			}
			return apierr.Internal(err)
		}
		if err := b.authorizeShipmentAccessRaw(p, locked.CustomerID,
			locked.OriginBranchID, locked.DestinationBranchID, locked.BookingUnitID); err != nil {
			return err
		}
		if locked.CurrentStatus != string(StatusBooked) {
			return apierr.Conflict("SHIPMENT_EDIT_LOCKED",
				"Shipment details can only be corrected before pickup activity begins.").
				WithDetail("currentStatus", locked.CurrentStatus)
		}
		if locked.Version != req.ExpectedVersion {
			return apierr.Conflict(apierr.CodeConcurrentModification,
				"This shipment was changed by someone else. Reload and review the latest details.").
				WithDetail("currentVersion", locked.Version)
		}

		current, err := currentCorrectionAddresses(ctx, q, locked.ID)
		if err != nil {
			return err
		}
		before := map[string]any{
			"version": locked.Version, "referenceNumber": locked.ReferenceNumber,
			"contentDescription":  locked.ContentDescription,
			"specialInstructions": locked.SpecialInstructions, "isFragile": locked.IsFragile,
			"sender": current["SENDER"], "recipient": current["RECIPIENT"],
		}

		updated, err := q.CorrectBookedShipment(ctx, dbgen.CorrectBookedShipmentParams{
			ReferenceNumber: optional(req.ReferenceNumber), ContentDescription: req.ContentDescription,
			SpecialInstructions: optional(req.SpecialInstructions), IsFragile: req.IsFragile,
			ID: locked.ID, OrganizationID: p.OrganizationID, ExpectedVersion: req.ExpectedVersion,
		})
		if err != nil {
			if database.IsUniqueViolation(err, "shipments_customer_reference_idx") {
				return apierr.Conflict(apierr.CodeDuplicate,
					"Another active shipment already uses this customer reference.").
					WithDetail("referenceNumber", req.ReferenceNumber)
			}
			if database.IsNoRows(err) {
				return apierr.Conflict(apierr.CodeConcurrentModification,
					"This shipment changed while the correction was being saved. Reload and try again.")
			}
			return apierr.Internal(err)
		}

		actor := p.ActorUserID()
		requestID := optional(httpx.RequestID(ctx))
		for _, item := range []struct {
			role string
			in   CorrectionAddress
		}{
			{role: "SENDER", in: req.Sender},
			{role: "RECIPIENT", in: req.Recipient},
		} {
			base, ok := current[item.role]
			if !ok {
				return apierr.Internal(fmt.Errorf("shipment %s has no %s address snapshot", shipmentID, item.role))
			}
			seq, err := q.NextShipmentAddressCorrectionSequence(ctx, dbgen.NextShipmentAddressCorrectionSequenceParams{
				ShipmentID: locked.ID, Role: item.role,
			})
			if err != nil {
				return apierr.Internal(err)
			}
			_, err = q.CreateShipmentAddressCorrection(ctx, dbgen.CreateShipmentAddressCorrectionParams{
				PublicID: publicid.New(publicid.PrefixShipmentCorrection), OrganizationID: p.OrganizationID,
				ShipmentID: locked.ID, Role: item.role, Sequence: seq,
				ContactName: item.in.ContactName, CompanyName: optional(item.in.CompanyName),
				Phone: item.in.Phone, AltPhone: optional(item.in.AltPhone), Email: optional(item.in.Email),
				Line1: item.in.Line1, Line2: optional(item.in.Line2), Landmark: optional(item.in.Landmark),
				CityName: base.City, StateName: base.State, Pincode: base.Pincode, CountryCode: base.CountryCode,
				Reason: req.Reason, CorrectedByUserID: actor, RequestID: requestID,
			})
			if err != nil {
				return apierr.Internal(fmt.Errorf("record %s correction: %w", item.role, err))
			}
		}

		_, err = q.AppendShipmentEvent(ctx, dbgen.AppendShipmentEventParams{
			PublicID: publicid.New(publicid.PrefixShipmentEvent), OrganizationID: p.OrganizationID,
			ShipmentID: locked.ID, Sequence: updated.EventSequence, EventType: "REMARK",
			FromStatus: &locked.CurrentStatus, ToStatus: locked.CurrentStatus,
			ActorUserID: actor, ActorType: actorTypeFor(p, false), OperatingUnitID: locked.BookingUnitID,
			LocationPincode: &locked.OriginPincode, Description: "Shipment details corrected",
			InternalRemarks: &req.Reason, ReasonCode: strPtr("BOOKING_CORRECTION"),
			RequestID: requestID, Metadata: []byte("{}"),
		})
		if err != nil {
			return apierr.Internal(fmt.Errorf("append correction event: %w", err))
		}

		after := map[string]any{
			"version": updated.Version, "referenceNumber": req.ReferenceNumber,
			"contentDescription":  req.ContentDescription,
			"specialInstructions": req.SpecialInstructions, "isFragile": req.IsFragile,
			"sender": req.Sender, "recipient": req.Recipient,
		}
		if err := b.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionShipmentCorrected, ResourceType: "shipment",
			ResourceID: &locked.ID, ResourcePublicID: locked.PublicID,
			OperatingUnitID: locked.BookingUnitID, Reason: req.Reason,
			Before: before, After: after,
		})); err != nil {
			return apierr.Internal(fmt.Errorf("record shipment correction audit: %w", err))
		}

		full, err := q.GetShipmentByPublicID(ctx, dbgen.GetShipmentByPublicIDParams{
			PublicID: shipmentID, OrganizationID: p.OrganizationID,
		})
		if err != nil {
			return apierr.Internal(err)
		}
		detail, err = b.loadDetail(ctx, q, full)
		return err
	})
	if err != nil {
		return nil, err
	}
	b.metrics.RecordBusiness("booking", "corrected")
	return detail, nil
}

type currentCorrectionAddress struct {
	ContactName string
	CompanyName string
	Phone       string
	AltPhone    string
	Email       string
	Line1       string
	Line2       string
	Landmark    string
	City        string
	State       string
	Pincode     string
	CountryCode string
}

func currentCorrectionAddresses(ctx context.Context, q *dbgen.Queries, shipmentID int64) (map[string]currentCorrectionAddress, error) {
	out := map[string]currentCorrectionAddress{}
	snapshots, err := q.ListShipmentAddressSnapshots(ctx, shipmentID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, a := range snapshots {
		if a.Role != "SENDER" && a.Role != "RECIPIENT" {
			continue
		}
		out[a.Role] = currentCorrectionAddress{
			ContactName: a.ContactName, CompanyName: valueOrEmpty(a.CompanyName), Phone: a.Phone,
			AltPhone: valueOrEmpty(a.AltPhone), Email: valueOrEmpty(a.Email), Line1: a.Line1,
			Line2: valueOrEmpty(a.Line2), Landmark: valueOrEmpty(a.Landmark), City: a.CityName,
			State: a.StateName, Pincode: a.Pincode, CountryCode: a.CountryCode,
		}
	}
	corrections, err := q.ListLatestShipmentAddressCorrections(ctx, shipmentID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, a := range corrections {
		out[a.Role] = currentCorrectionAddress{
			ContactName: a.ContactName, CompanyName: valueOrEmpty(a.CompanyName), Phone: a.Phone,
			AltPhone: valueOrEmpty(a.AltPhone), Email: valueOrEmpty(a.Email), Line1: a.Line1,
			Line2: valueOrEmpty(a.Line2), Landmark: valueOrEmpty(a.Landmark), City: a.CityName,
			State: a.StateName, Pincode: a.Pincode, CountryCode: a.CountryCode,
		}
	}
	return out, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
