package manifest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/storage"
)

// DocumentBuilder renders a manifest into a printable document.
//
// Constitution §38: large reports are generated in the background and served
// from object storage, never assembled synchronously in a request. A manifest
// with three hundred lines is exactly the shape that rule exists for.
type DocumentBuilder struct {
	svc   *Service
	store storage.Store
}

// NewDocumentBuilder wires the background renderer.
func NewDocumentBuilder(s *Service, store storage.Store) *DocumentBuilder {
	return &DocumentBuilder{svc: s, store: store}
}

// RegisterHandlers attaches the renderer to a worker.
func (b *DocumentBuilder) RegisterHandlers(w *jobs.Worker) {
	w.Register(JobGenerateManifestPDF, b.generate)
}

type documentPayload struct {
	ManifestID string `json:"manifestId"`
}

// generate renders the document and records its object key.
//
// It renders from the frozen closure snapshot rather than from live tables, so
// the document a driver signed and a copy printed a month later say the same
// thing even if a correction has been applied since.
func (b *DocumentBuilder) generate(ctx context.Context, job jobs.Job) error {
	var payload documentPayload
	if err := job.Decode(&payload); err != nil {
		return err
	}
	if job.OrganizationID == nil {
		return fmt.Errorf("manifest document job has no organization")
	}

	row, err := b.svc.q.GetManifestByPublicID(ctx, dbgen.GetManifestByPublicIDParams{
		PublicID: payload.ManifestID, OrganizationID: *job.OrganizationID,
	})
	if err != nil {
		return fmt.Errorf("load manifest %s: %w", payload.ManifestID, err)
	}
	if len(row.ClosedContents) == 0 {
		return fmt.Errorf("manifest %s has no closure snapshot to render", payload.ManifestID)
	}
	var snap ContentSnapshot
	if err := json.Unmarshal(row.ClosedContents, &snap); err != nil {
		return fmt.Errorf("decode manifest snapshot: %w", err)
	}

	doc := renderManifest(row, snap)
	key := storage.GenerateKey(orgKeyFor(row), "MANIFEST_DOCUMENT", "txt")
	if _, err := b.store.Put(ctx, key, bytes.NewReader(doc), storage.PutOptions{
		ContentType: "text/plain; charset=utf-8",
		MaxBytes:    8 << 20,
		Metadata:    map[string]string{"manifest": row.ManifestCode},
	}); err != nil {
		return fmt.Errorf("store manifest document: %w", err)
	}

	if _, err := b.svc.q.CreateStoredObject(ctx, dbgen.CreateStoredObjectParams{
		PublicID: publicid.New(publicid.PrefixStoredObject), OrganizationID: row.OrganizationID,
		ObjectKey: key, Bucket: b.store.Bucket(), Purpose: "MANIFEST_DOCUMENT",
		MimeType: "text/plain; charset=utf-8", SizeBytes: int64(len(doc)),
		ChecksumSha256: checksum(doc),
		Metadata:       []byte("{}"),
	}); err != nil {
		return fmt.Errorf("record manifest document: %w", err)
	}
	if _, err := b.svc.q.SetManifestDocument(ctx, dbgen.SetManifestDocumentParams{
		ID: row.ID, OrganizationID: row.OrganizationID, DocumentObjectKey: &key,
	}); err != nil {
		return fmt.Errorf("attach manifest document: %w", err)
	}
	return nil
}

// renderManifest produces the printable body.
//
// It is fixed-width text rather than a PDF: a manifest is printed on a thermal
// or dot-matrix printer at a loading bay, where plain text is what those
// machines accept, and a PDF renderer would be a large dependency for a
// document nobody reads on a screen. The object is served through the same
// signed-URL path as any other artifact, so swapping the format later changes
// only this function.
func renderManifest(m dbgen.GetManifestByPublicIDRow, snap ContentSnapshot) []byte {
	var b strings.Builder
	line := strings.Repeat("=", 78)

	fmt.Fprintf(&b, "%s\n", line)
	fmt.Fprintf(&b, "MANIFEST  %s\n", m.ManifestCode)
	fmt.Fprintf(&b, "%s\n", line)
	fmt.Fprintf(&b, "From        : %s (%s)\n", m.OriginName, m.OriginCode)
	fmt.Fprintf(&b, "To          : %s (%s)\n", m.DestinationName, m.DestinationCode)
	fmt.Fprintf(&b, "Direction   : %s\n", m.Direction)
	if m.TripCode != nil {
		fmt.Fprintf(&b, "Trip        : %s (%s)\n", *m.TripCode, ops.Deref(m.TripMode))
	}
	fmt.Fprintf(&b, "Closed at   : %s\n", snap.ClosedAt.Format(time.RFC1123))
	fmt.Fprintf(&b, "Bags        : %d\n", snap.BagCount)
	fmt.Fprintf(&b, "Loose       : %d\n", snap.LooseCount)
	fmt.Fprintf(&b, "Shipments   : %d\n", snap.ShipmentCount)
	fmt.Fprintf(&b, "Pieces      : %d\n", snap.PieceCount)
	fmt.Fprintf(&b, "Weight      : %.3f kg\n", float64(snap.WeightGrams)/1000)
	fmt.Fprintf(&b, "%s\n\n", line)

	if len(snap.Bags) > 0 {
		fmt.Fprintf(&b, "BAGS\n%s\n", strings.Repeat("-", 78))
		fmt.Fprintf(&b, "%-26s %-10s %6s %6s %10s\n", "BAG CODE", "DEST", "SHPMTS", "PIECES", "WEIGHT KG")
		for _, bag := range snap.Bags {
			fmt.Fprintf(&b, "%-26s %-10s %6d %6d %10.3f\n",
				truncate(bag.BagCode, 26), truncate(bag.Destination, 10),
				bag.ShipmentCount, bag.PieceCount, float64(bag.WeightGrams)/1000)
		}
		fmt.Fprintln(&b)
	}

	if len(snap.Loose) > 0 {
		fmt.Fprintf(&b, "LOOSE SHIPMENTS\n%s\n", strings.Repeat("-", 78))
		fmt.Fprintf(&b, "%-22s %-10s %6s %10s\n", "AWB", "DEST PIN", "PIECES", "WEIGHT KG")
		for _, l := range snap.Loose {
			fmt.Fprintf(&b, "%-22s %-10s %6d %10.3f\n",
				truncate(l.AWB, 22), truncate(l.Destination, 10),
				l.PieceCount, float64(l.WeightGrams)/1000)
		}
		fmt.Fprintln(&b)
	}

	fmt.Fprintf(&b, "%s\n", line)
	fmt.Fprintf(&b, "Dispatched by ...........................  Received by ...........................\n")
	fmt.Fprintf(&b, "Signature     ...........................  Signature   ...........................\n")
	fmt.Fprintf(&b, "Date / time   ...........................  Date / time ...........................\n")
	fmt.Fprintf(&b, "%s\n", line)
	return []byte(b.String())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func orgKeyFor(m dbgen.GetManifestByPublicIDRow) string {
	return fmt.Sprintf("org%d", m.OrganizationID)
}

func checksum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

// ErrNoDocument is returned when a manifest has no generated document yet.
var ErrNoDocument = apierr.NotFound("Manifest document")
