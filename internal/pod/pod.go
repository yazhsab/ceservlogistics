// Package pod implements M19: proof of delivery and its artifacts.
//
// A POD is the evidence that settles a dispute months later, so the constraints
// are about integrity rather than convenience:
//
//   - The record is append-only at the database level. Nobody edits a POD.
//   - Artifacts live in private object storage; the database keeps the key,
//     size, MIME type and SHA-256 (§31). The checksum is what proves the file
//     served today is the file uploaded then.
//   - Content type is decided by sniffing the bytes, never by trusting the
//     filename or the client's Content-Type header.
//   - Retrieval is authorized, audited, and served through a short-lived signed
//     URL rather than a public link.
package pod

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/storage"
	"github.com/ceserve/courier-os/internal/shipment"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Artifact types a POD can carry.
var ArtifactTypes = []string{"SIGNATURE", "PHOTO", "ID_PROOF", "DOCUMENT", "AUDIO"}

// POD types.
var PODTypes = []string{"DELIVERY", "RTO_RETURN", "CUSTOMER_PICKUP", "HANDOVER"}

// allowedMIME is the upload allowlist (§31). It is an allowlist rather than a
// blocklist because the failure mode of getting a blocklist wrong is storing
// and later serving executable content.
var allowedMIME = map[string]string{
	"image/jpeg":      "jpg",
	"image/png":       "png",
	"image/webp":      "webp",
	"application/pdf": "pdf",
}

// purposeForArtifact maps an artifact type to a storage purpose, which drives
// retention policy.
var purposeForArtifact = map[string]string{
	"SIGNATURE": "POD_SIGNATURE",
	"PHOTO":     "POD_PHOTO",
	"ID_PROOF":  "POD_DOCUMENT",
	"DOCUMENT":  "POD_DOCUMENT",
	"AUDIO":     "POD_DOCUMENT",
}

// Service implements the POD workflow.
type Service struct {
	db       *database.DB
	q        *dbgen.Queries
	store    storage.Store
	units    *ops.Resolver
	trans    *shipment.Transitioner
	audit    *audit.Recorder
	log      *slog.Logger
	maxBytes int64
	urlTTL   time.Duration
	// retention bounds how long evidence is kept. POD is normally required for
	// the life of a commercial dispute, so the default is generous.
	retention time.Duration
}

// NewService builds the POD service.
func NewService(
	db *database.DB, q *dbgen.Queries, store storage.Store, units *ops.Resolver,
	trans *shipment.Transitioner, rec *audit.Recorder, log *slog.Logger, maxBytes int64,
) *Service {
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	return &Service{
		db: db, q: q, store: store, units: units, trans: trans, audit: rec, log: log,
		maxBytes: maxBytes, urlTTL: 15 * time.Minute, retention: 7 * 365 * 24 * time.Hour,
	}
}

// MaxUploadBytes is published so the contract can state the limit.
func (s *Service) MaxUploadBytes() int64 { return s.maxBytes }

// AllowedMIMETypes lists what may be uploaded.
func (s *Service) AllowedMIMETypes() []string {
	out := make([]string, 0, len(allowedMIME))
	for m := range allowedMIME {
		out = append(out, m)
	}
	return out
}

// SubmitInput is a proof of delivery.
type SubmitInput struct {
	Barcode      string
	PODType      string
	Recipient    string
	Relationship string
	Phone        string
	IDType       string
	IDNumber     string
	Latitude     *float64
	Longitude    *float64
	Accuracy     *int
	Remarks      string
	DeliveredAt  *time.Time
	Facility     *ops.Facility
	Device       ops.Device
	// Artifacts are already-parsed multipart files.
	Artifacts []ArtifactUpload
}

// ArtifactUpload is one file being attached.
type ArtifactUpload struct {
	Type    string
	Caption string
	Header  *multipart.FileHeader
}

// Submit records a proof of delivery with its evidence.
//
// Uploads happen before the transaction opens. That is not just for speed: a
// transaction held open across several network round trips to object storage is
// exactly the pool-starvation pattern that has to be avoided on a small VPS
// (§28). If the transaction then fails, the orphaned objects are swept by
// retention rather than left to leak.
func (s *Service) Submit(ctx context.Context, p *tenant.Principal, in SubmitInput) (*Detail, error) {
	resolved, err := s.q.ResolveScanBarcode(ctx, dbgen.ResolveScanBarcodeParams{
		Barcode: in.Barcode, OrganizationID: p.OrganizationID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Shipment")
	}

	podType := orDefault(in.PODType, "DELIVERY")
	if existing, gErr := s.q.GetProofOfDeliveryForShipment(ctx,
		dbgen.GetProofOfDeliveryForShipmentParams{
			ShipmentID: resolved.ID, PodType: podType,
		}); gErr == nil {
		return nil, apierr.Conflict(apierr.CodeDuplicate,
			"Proof of delivery has already been submitted for this shipment.").
			WithDetail("proofOfDeliveryId", existing.PublicID)
	} else if !ops.IsNoRows(gErr) {
		return nil, apierr.Internal(gErr)
	}

	stored, err := s.uploadArtifacts(ctx, p, in.Artifacts)
	if err != nil {
		return nil, err
	}

	var detail *Detail
	err = s.db.InTx(ctx, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		sh, lErr := q.LockShipmentByIDForUpdate(ctx, dbgen.LockShipmentByIDForUpdateParams{
			ID: resolved.ID, OrganizationID: p.OrganizationID,
		})
		if lErr != nil {
			return ops.NotFoundOr(lErr, "Shipment")
		}
		// A POD is evidence of something that happened, so the shipment must
		// already be in a state where it did.
		if err := s.checkDeliverableState(sh, podType); err != nil {
			return err
		}

		delivered := time.Now()
		if in.DeliveredAt != nil {
			delivered = *in.DeliveredAt
		}

		params := dbgen.CreateProofOfDeliveryParams{
			PublicID: publicid.New(publicid.PrefixProofOfDelivery), OrganizationID: p.OrganizationID,
			ShipmentID: sh.ID, PodType: podType,
			RecipientName: in.Recipient, RecipientRelationship: orDefault(in.Relationship, "SELF"),
			RecipientPhone:  ops.Optional(in.Phone),
			RecipientIDType: ops.Optional(in.IDType),
			// The identity document is masked before storage: §35 keeps sensitive
			// identifiers out of the database when a partial serves the purpose.
			RecipientIDMasked: ops.Optional(maskIdentifier(in.IDNumber)),
			SignatureCaptured: hasArtifact(stored, "SIGNATURE"),
			PhotoCaptured:     hasArtifact(stored, "PHOTO"),
			DeliveredAt:       delivered,
			DeliveredByUserID: &p.UserID,
			Latitude:          in.Latitude, Longitude: in.Longitude,
			DeviceID: ops.Optional(in.Device.ID), DeviceModel: ops.Optional(in.Device.Model),
			Remarks: ops.Optional(in.Remarks), Metadata: []byte("{}"),
		}
		if in.Facility != nil {
			params.OperatingUnitID = &in.Facility.ID
		}
		if in.Accuracy != nil {
			v := int32(*in.Accuracy)
			params.LocationAccuracyM = &v
		}
		// The OTP flag is copied from the delivery attempt rather than accepted
		// from the client: a POD claiming OTP verification that never happened
		// would be evidence of nothing.
		params.OtpVerified = s.otpWasVerified(ctx, q, sh.ID)

		created, cErr := q.CreateProofOfDelivery(ctx, params)
		if cErr != nil {
			if ops.IsUnique(cErr, "proof_of_delivery_unique") {
				return apierr.Conflict(apierr.CodeDuplicate,
					"Proof of delivery has already been submitted for this shipment.")
			}
			return apierr.Internal(fmt.Errorf("create proof of delivery: %w", cErr))
		}

		for _, obj := range stored {
			seq, sErr := q.NextPODArtifactSequence(ctx, dbgen.NextPODArtifactSequenceParams{
				ProofOfDeliveryID: created.ID, ArtifactType: obj.artifactType,
			})
			if sErr != nil {
				return apierr.Internal(sErr)
			}
			if _, aErr := q.CreatePODArtifact(ctx, dbgen.CreatePODArtifactParams{
				PublicID: publicid.New(publicid.PrefixPODArtifact), OrganizationID: p.OrganizationID,
				ProofOfDeliveryID: created.ID, StoredObjectID: obj.objectID,
				ArtifactType: obj.artifactType, Sequence: seq, Caption: ops.Optional(obj.caption),
			}); aErr != nil {
				return apierr.Internal(fmt.Errorf("attach artifact: %w", aErr))
			}
		}

		if _, eErr := s.trans.RecordEvent(ctx, tx, in.Device.Actor(p, in.Facility), sh, "POD",
			"Proof of delivery recorded", shipment.Request{
				OccurredAt: in.DeliveredAt,
				Metadata: map[string]any{
					"podType": podType, "recipientName": in.Recipient,
					"artifactCount": len(stored),
				},
			}); eErr != nil {
			return eErr
		}

		if aErr := s.audit.RecordTx(ctx, tx, audit.FromPrincipal(ctx, audit.Entry{
			Action: audit.ActionPODSubmitted, ResourceType: "proof_of_delivery",
			ResourceID: &created.ID, ResourcePublicID: created.PublicID,
			OperatingUnitID: facilityID(in.Facility),
			After: map[string]any{
				"awb": sh.Awb, "podType": podType, "recipientName": in.Recipient,
				"relationship": params.RecipientRelationship,
				"artifacts":    artifactSummary(stored), "otpVerified": params.OtpVerified,
			},
		})); aErr != nil {
			return apierr.Internal(aErr)
		}

		var dErr error
		detail, dErr = s.loadDetail(ctx, q, p, created.PublicID, false)
		return dErr
	})
	return detail, err
}

// checkDeliverableState refuses evidence for something that has not happened.
func (s *Service) checkDeliverableState(sh dbgen.Shipment, podType string) error {
	status := shipment.Status(sh.CurrentStatus)
	switch podType {
	case "DELIVERY", "CUSTOMER_PICKUP", "HANDOVER":
		if status != shipment.StatusDelivered {
			return apierr.Conflict("SHIPMENT_INVALID_STATE",
				"Proof of delivery can only be submitted for a delivered shipment.").
				WithDetail("currentStatus", sh.CurrentStatus)
		}
	case "RTO_RETURN":
		if status != shipment.StatusRTODelivered {
			return apierr.Conflict("SHIPMENT_INVALID_STATE",
				"A return proof can only be submitted once the shipment has been returned to the sender.").
				WithDetail("currentStatus", sh.CurrentStatus)
		}
	}
	return nil
}

// otpWasVerified reads the fact off the delivery attempt rather than the client.
func (s *Service) otpWasVerified(ctx context.Context, q *dbgen.Queries, shipmentID int64) bool {
	attempts, err := q.ListDeliveryAttempts(ctx, shipmentID)
	if err != nil {
		return false
	}
	for _, a := range attempts {
		if a.Outcome == "DELIVERED" {
			return a.OtpVerified
		}
	}
	return false
}

type storedArtifact struct {
	objectID     int64
	artifactType string
	caption      string
	mime         string
	size         int64
}

// uploadArtifacts validates and stores every file.
//
// Validation order matters: size first (cheap, and stops a large upload
// consuming bandwidth), then the sniffed content type, then storage. The
// declared Content-Type and the filename extension are both ignored for the
// decision — §31: "Never trust filename extension alone."
func (s *Service) uploadArtifacts(
	ctx context.Context, p *tenant.Principal, uploads []ArtifactUpload,
) ([]storedArtifact, error) {
	out := make([]storedArtifact, 0, len(uploads))
	for i, up := range uploads {
		field := fmt.Sprintf("artifacts[%d]", i)
		if !contains(ArtifactTypes, up.Type) {
			return nil, apierr.Validation("Unknown artifact type.",
				map[string]any{"field": field + ".type", "allowed": ArtifactTypes})
		}
		if up.Header == nil {
			return nil, apierr.Validation("An artifact file is required.",
				map[string]any{"field": field + ".file"})
		}
		if up.Header.Size > s.maxBytes {
			return nil, apierr.New(http.StatusRequestEntityTooLarge, apierr.CodePayloadTooLarge,
				"This file is larger than the upload limit.").
				WithDetail("field", field).
				WithDetail("maxBytes", s.maxBytes).
				WithDetail("receivedBytes", up.Header.Size)
		}

		f, err := up.Header.Open()
		if err != nil {
			return nil, apierr.Validation("The uploaded file could not be read.",
				map[string]any{"field": field})
		}

		// Sniff the leading bytes to decide the type, then rewind.
		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		detected := http.DetectContentType(head[:n])
		detected = strings.TrimSpace(strings.Split(detected, ";")[0])
		ext, ok := allowedMIME[detected]
		if !ok {
			f.Close()
			return nil, apierr.New(http.StatusUnsupportedMediaType, apierr.CodeUnsupportedMedia,
				"This file type is not accepted as delivery evidence.").
				WithDetail("field", field).
				WithDetail("detectedType", detected).
				WithDetail("allowed", s.AllowedMIMETypes())
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return nil, apierr.Internal(err)
		}

		purpose := purposeForArtifact[up.Type]
		key := storage.GenerateKey(p.OrganizationPublicID, purpose, ext)
		obj, err := s.store.Put(ctx, key, f, storage.PutOptions{
			ContentType: detected, MaxBytes: s.maxBytes,
			Metadata: map[string]string{"organization": p.OrganizationPublicID, "purpose": purpose},
		})
		f.Close()
		if err != nil {
			if err == storage.ErrTooLarge {
				return nil, apierr.New(http.StatusRequestEntityTooLarge, apierr.CodePayloadTooLarge,
					"This file is larger than the upload limit.").
					WithDetail("field", field).WithDetail("maxBytes", s.maxBytes)
			}
			return nil, apierr.Internal(fmt.Errorf("store artifact: %w", err))
		}

		retention := time.Now().Add(s.retention)
		record, err := s.q.CreateStoredObject(ctx, dbgen.CreateStoredObjectParams{
			PublicID: publicid.New(publicid.PrefixStoredObject), OrganizationID: p.OrganizationID,
			ObjectKey: obj.Key, Bucket: obj.Bucket, Purpose: purpose,
			MimeType: detected, SizeBytes: obj.Size, ChecksumSha256: obj.Checksum,
			OriginalFilename: ops.Optional(safeFilename(up.Header.Filename)),
			UploadedByUserID: &p.UserID, RetentionUntil: &retention,
			Metadata: []byte("{}"),
		})
		if err != nil {
			return nil, apierr.Internal(fmt.Errorf("record stored object: %w", err))
		}
		out = append(out, storedArtifact{
			objectID: record.ID, artifactType: up.Type, caption: up.Caption,
			mime: detected, size: obj.Size,
		})
	}
	return out, nil
}

// Download issues a signed URL, or streams the bytes when the backend cannot
// presign.
//
// Every retrieval is authorized and logged (§31, §34): a delivery dispute
// frequently turns on who had access to the evidence.
func (s *Service) Download(
	ctx context.Context, p *tenant.Principal, podID, artifactID string,
) (signedURL string, body io.ReadCloser, mime string, err error) {
	detail, err := s.loadDetail(ctx, s.q, p, podID, true)
	if err != nil {
		return "", nil, "", err
	}
	var target *Artifact
	for i := range detail.Artifacts {
		if detail.Artifacts[i].ID == artifactID {
			target = &detail.Artifacts[i]
			break
		}
	}
	if target == nil {
		return "", nil, "", apierr.NotFound("POD artifact")
	}

	// Audit before serving: a download that fails after the log is a smaller
	// problem than a download that succeeds without one.
	if lErr := s.q.RecordObjectAccess(ctx, dbgen.RecordObjectAccessParams{
		OrganizationID: p.OrganizationID, StoredObjectID: target.objectID,
		AccessedByUserID: p.ActorUserID(), AccessType: "SIGNED_URL",
		RequestID: ops.Optional(httpx.RequestID(ctx)),
	}); lErr != nil {
		return "", nil, "", apierr.Internal(lErr)
	}

	url, err := s.store.SignedURL(ctx, target.objectKey, s.urlTTL)
	if err != nil {
		return "", nil, "", apierr.Internal(err)
	}
	if url != "" {
		return url, nil, target.MimeType, nil
	}
	rc, _, err := s.store.Get(ctx, target.objectKey)
	if err != nil {
		if err == storage.ErrNotFound {
			return "", nil, "", apierr.NotFound("POD artifact")
		}
		return "", nil, "", apierr.Internal(err)
	}
	return "", rc, target.MimeType, nil
}

func hasArtifact(list []storedArtifact, kind string) bool {
	for _, a := range list {
		if a.artifactType == kind {
			return true
		}
	}
	return false
}

func artifactSummary(list []storedArtifact) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, map[string]any{
			"type": a.artifactType, "mimeType": a.mime, "sizeBytes": a.size,
		})
	}
	return out
}

// maskIdentifier keeps only the last four characters of a government id.
func maskIdentifier(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 4 {
		return strings.Repeat("*", len(trimmed))
	}
	return strings.Repeat("*", len(trimmed)-4) + trimmed[len(trimmed)-4:]
}

// safeFilename keeps a display name without letting it influence any path.
func safeFilename(name string) string {
	name = strings.TrimSpace(name)
	if idx := strings.LastIndexAny(name, `/\`); idx >= 0 {
		name = name[idx+1:]
	}
	if len(name) > 120 {
		name = name[:120]
	}
	var b bytes.Buffer
	for _, r := range name {
		if r < 32 || r == 127 {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func facilityID(f *ops.Facility) *int64 {
	if f == nil {
		return nil
	}
	id := f.ID
	return &id
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
