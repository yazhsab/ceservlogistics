package pod

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Artifact is one piece of delivery evidence.
type Artifact struct {
	ID         string    `json:"id"`
	Type       string    `json:"artifactType"`
	Sequence   int       `json:"sequence"`
	Caption    string    `json:"caption,omitempty"`
	MimeType   string    `json:"mimeType"`
	SizeBytes  int64     `json:"sizeBytes"`
	Checksum   string    `json:"checksumSha256"`
	CapturedAt time.Time `json:"capturedAt"`
	// DownloadURL is the endpoint that issues a signed link; the signed link
	// itself is never embedded in a list response.
	DownloadURL string `json:"downloadUrl"`

	objectID  int64
	objectKey string
}

// Detail is the full proof of delivery.
type Detail struct {
	ID           string     `json:"id"`
	PODType      string     `json:"podType"`
	ShipmentID   string     `json:"shipmentId"`
	AWB          string     `json:"awb"`
	Recipient    string     `json:"recipientName"`
	Relationship string     `json:"recipientRelationship"`
	Phone        string     `json:"recipientPhone,omitempty"`
	IDType       string     `json:"recipientIdType,omitempty"`
	IDMasked     string     `json:"recipientIdMasked,omitempty"`
	OTPVerified  bool       `json:"otpVerified"`
	Signature    bool       `json:"signatureCaptured"`
	Photo        bool       `json:"photoCaptured"`
	DeliveredAt  time.Time  `json:"deliveredAt"`
	RecordedAt   time.Time  `json:"recordedAt"`
	DeliveredBy  *ops.Ref   `json:"deliveredBy,omitempty"`
	Facility     string     `json:"facilityCode,omitempty"`
	Latitude     *float64   `json:"latitude,omitempty"`
	Longitude    *float64   `json:"longitude,omitempty"`
	Accuracy     *int       `json:"locationAccuracyM,omitempty"`
	Device       string     `json:"deviceModel,omitempty"`
	Remarks      string     `json:"remarks,omitempty"`
	Artifacts    []Artifact `json:"artifacts"`
}

// Get returns one proof of delivery.
func (s *Service) Get(ctx context.Context, p *tenant.Principal, id string) (*Detail, error) {
	return s.loadDetail(ctx, s.q, p, id, false)
}

// GetForShipment returns the POD attached to a shipment.
func (s *Service) GetForShipment(
	ctx context.Context, p *tenant.Principal, shipmentID int64, podType string,
) (*Detail, error) {
	row, err := s.q.GetProofOfDeliveryForShipment(ctx, dbgen.GetProofOfDeliveryForShipmentParams{
		ShipmentID: shipmentID, PodType: orDefault(podType, "DELIVERY"),
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Proof of delivery")
	}
	return s.loadDetail(ctx, s.q, p, row.PublicID, false)
}

func (s *Service) loadDetail(
	ctx context.Context, q *dbgen.Queries, p *tenant.Principal, publicID string, internal bool,
) (*Detail, error) {
	row, err := q.GetProofOfDelivery(ctx, dbgen.GetProofOfDeliveryParams{
		PublicID: publicID, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Proof of delivery")
	}
	if err := s.authorize(p, row.CustomerID, row.DestinationBranchID,
		row.OriginBranchID, row.BookingUnitID); err != nil {
		return nil, err
	}

	d := &Detail{
		ID: row.PublicID, PODType: row.PodType,
		ShipmentID: row.ShipmentPublicID, AWB: row.Awb,
		Recipient: row.RecipientName, Relationship: row.RecipientRelationship,
		Phone: ops.Deref(row.RecipientPhone), IDType: ops.Deref(row.RecipientIDType),
		IDMasked:    ops.Deref(row.RecipientIDMasked),
		OTPVerified: row.OtpVerified, Signature: row.SignatureCaptured,
		Photo: row.PhotoCaptured, DeliveredAt: row.DeliveredAt, RecordedAt: row.RecordedAt,
		Facility: ops.Deref(row.UnitCode),
		Latitude: row.Latitude, Longitude: row.Longitude,
		Device:    ops.Deref(row.DeviceModel),
		Remarks:   ops.Deref(row.Remarks),
		Artifacts: []Artifact{},
	}
	if row.LocationAccuracyM != nil {
		v := int(*row.LocationAccuracyM)
		d.Accuracy = &v
	}
	if row.DeliveredByPublicID != nil {
		d.DeliveredBy = &ops.Ref{ID: *row.DeliveredByPublicID, Name: ops.Deref(row.DeliveredByName)}
	}

	artifacts, err := q.ListPODArtifacts(ctx, row.ID)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	for _, a := range artifacts {
		art := Artifact{
			ID: a.PublicID, Type: a.ArtifactType, Sequence: int(a.Sequence),
			Caption: ops.Deref(a.Caption), MimeType: a.MimeType, SizeBytes: a.SizeBytes,
			Checksum: hex.EncodeToString(a.ChecksumSha256), CapturedAt: a.CapturedAt,
			DownloadURL: "/api/v1/pod/" + row.PublicID + "/artifacts/" + a.PublicID + "/download",
		}
		if internal {
			// The object key is needed to serve the file but must never be
			// serialised: it is the only thing between a leaked response and a
			// direct fetch from the bucket.
			art.objectKey = a.ObjectKey
			art.objectID = a.ObjectID
		}
		d.Artifacts = append(d.Artifacts, art)
	}
	return d, nil
}

// authorize decides who may see a POD.
//
// A portal user sees their own shipments; staff see PODs for shipments that
// touched a facility in their scope. Support and finance hold shipment.read_all
// and so see everything, which is what they need to settle a dispute.
func (s *Service) authorize(
	p *tenant.Principal, customerID int64, destBranch, originBranch, bookingUnit *int64,
) error {
	if p.IsPortalUser {
		if !p.IsCustomerInScope(customerID) {
			return apierr.NotFound("Proof of delivery")
		}
		return nil
	}
	scope := p.UnitScope("shipment.read_all")
	if scope == nil {
		return nil
	}
	for _, id := range scope {
		for _, candidate := range []*int64{destBranch, originBranch, bookingUnit} {
			if candidate != nil && *candidate == id {
				return nil
			}
		}
	}
	return apierr.NotFound("Proof of delivery")
}
