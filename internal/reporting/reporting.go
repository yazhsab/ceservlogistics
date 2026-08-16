// Package reporting is the export engine (M31).
//
// # A report is a job, not a request
//
// The specification is explicit: "Do not build the whole report in RAM" and
// "Large reports must not be generated synchronously in HTTP requests." Both
// follow from the same fact — a year of shipments for a busy tenant is millions
// of rows, and there is no request timeout long enough or heap large enough to
// make that a synchronous operation on a 16 GB box.
//
// So the shape is fixed:
//
//	POST /reports        →  202, a QUEUED run
//	worker               →  cursor-read → CSV chunk → object storage
//	GET  /reports/{id}   →  status, row count, size
//	GET  .../download    →  302 to a short-lived signed URL
//
// # Bounded memory, whatever the report size
//
// Rows are read in chunks by keyset (`id > after_id`), formatted, and written
// straight into the upload stream through an io.Pipe. Peak memory is one chunk,
// not one report. Nothing accumulates: there is no slice of results anywhere in
// this file, which is deliberate and is the property most worth preserving if
// this code is ever edited.
//
// Offset paging is banned here for the same reason. A million-row export paged
// by OFFSET re-scans the rows it skips on every chunk, turning a linear export
// into a quadratic one.
//
// # Exports expire
//
// A finished report is customer data sitting in a bucket. The download URL is
// signed and short-lived, the run expires, and the sweep deletes the object as
// well as the record — expiring the row while leaving the file behind would be
// a slow leak of exactly the data most worth leaking.
package reporting

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/ops"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/platform/storage"
	"github.com/ceserve/courier-os/internal/tenant"
)

// JobTypeRun generates one report.
const JobTypeRun = "report.run"

// Permissions.
const (
	PermRead    = "report.read"
	PermRun     = "report.run"
	PermFinance = "report.finance"
)

// Report types.
const (
	TypeShipmentVolume      = "SHIPMENT_VOLUME"
	TypeBranchPerformance   = "BRANCH_PERFORMANCE"
	TypeFranchisePerf       = "FRANCHISE_PERFORMANCE"
	TypeServicePerformance  = "SERVICE_PERFORMANCE"
	TypeSLA                 = "SLA"
	TypeNDR                 = "NDR"
	TypeRTO                 = "RTO"
	TypeCODAging            = "COD_AGING"
	TypeCommission          = "COMMISSION"
	TypeSettlement          = "SETTLEMENT"
	TypeRevenue             = "REVENUE"
	TypeExceptions          = "EXCEPTIONS"
	TypeVAT                 = "NIGERIA_VAT"
	TypeWithholdingTax      = "NIGERIA_WITHHOLDING_TAX"
	TypeProfitLoss          = "PROFIT_AND_LOSS"
	TypeBalanceSheet        = "BALANCE_SHEET"
	TypeCashBook            = "CASH_AND_BANK_BOOK"
	TypeReceivablesAging    = "RECEIVABLES_AGING"
	TypeFranchiseCollection = "FRANCHISE_COLLECTIONS"
)

// AllTypes is the catalogue, matching the CHECK on report_runs.report_type.
var AllTypes = []string{
	TypeShipmentVolume, TypeBranchPerformance, TypeFranchisePerf,
	TypeServicePerformance, TypeSLA, TypeNDR, TypeRTO, TypeCODAging,
	TypeCommission, TypeSettlement, TypeRevenue, TypeExceptions,
	TypeVAT, TypeWithholdingTax, TypeProfitLoss, TypeBalanceSheet,
	TypeCashBook, TypeReceivablesAging, TypeFranchiseCollection,
}

// financeTypes need report.finance on top of report.run.
//
// A branch supervisor may legitimately export their own volume; commission and
// settlement figures are somebody's income and somebody else's liability.
var financeTypes = map[string]bool{
	TypeCommission: true, TypeSettlement: true, TypeRevenue: true, TypeCODAging: true,
	TypeVAT: true, TypeWithholdingTax: true, TypeProfitLoss: true,
	TypeBalanceSheet: true, TypeCashBook: true, TypeReceivablesAging: true,
	TypeFranchiseCollection: true,
}

// RequiresFinance reports whether a type is gated behind report.finance.
func RequiresFinance(reportType string) bool { return financeTypes[reportType] }

// Statuses.
const (
	StatusQueued    = "QUEUED"
	StatusRunning   = "RUNNING"
	StatusCompleted = "COMPLETED"
	StatusFailed    = "FAILED"
	StatusExpired   = "EXPIRED"
	StatusCancelled = "CANCELLED"
)

// chunkSize is how many rows are read and written at a time.
//
// Large enough that the per-round-trip cost is amortised, small enough that a
// chunk of the widest report is well under a megabyte. The number that matters
// is that it is *constant*: peak memory does not move with report size.
const chunkSize = 2000

// downloadTTL is how long a finished export stays retrievable.
const downloadTTL = 24 * time.Hour

// Service queues and generates reports.
type Service struct {
	db      *database.DB
	q       *dbgen.Queries
	jobs    *jobs.Enqueuer
	storage storage.Store
	audit   *audit.Recorder
	log     *slog.Logger
}

func NewService(
	db *database.DB, q *dbgen.Queries, enq *jobs.Enqueuer,
	store storage.Store, rec *audit.Recorder, log *slog.Logger,
) *Service {
	return &Service{db: db, q: q, jobs: enq, storage: store, audit: rec, log: log}
}

// RunRequest describes a report to generate.
type RunRequest struct {
	ReportType  string
	Format      string
	PeriodStart time.Time
	PeriodEnd   time.Time
	UnitID      *int64
	Status      string
}

// Queue accepts a report request and returns immediately.
//
// Nothing is generated here. The HTTP response is a receipt, not a result,
// which is the whole point: a caller that waited for the file would hold a
// connection for minutes and time out on anything worth exporting.
func (s *Service) Queue(
	ctx context.Context, p *tenant.Principal, in RunRequest,
) (*dbgen.ReportRun, error) {
	if !validType(in.ReportType) {
		return nil, apierr.Validation("Unknown report type.",
			map[string]any{"reportType": in.ReportType, "allowed": AllTypes})
	}
	if in.Format == "" {
		in.Format = "CSV"
	}
	if in.Format != "CSV" && in.Format != "JSON" {
		return nil, apierr.Validation("Unsupported format.",
			map[string]any{"format": "expected CSV or JSON"})
	}
	if in.PeriodEnd.Before(in.PeriodStart) {
		return nil, apierr.Validation("The period ends before it starts.", nil)
	}
	// Unbounded is not a period. A report with no end date would walk the whole
	// table for ever and produce a file nobody can open.
	if in.PeriodEnd.Sub(in.PeriodStart) > 400*24*time.Hour {
		return nil, apierr.Validation(
			"A report may cover at most 400 days. Split a longer export.",
			map[string]any{"maxDays": 400})
	}

	params, err := encodeParams(in)
	if err != nil {
		return nil, apierr.Internal(err)
	}

	start, end := in.PeriodStart, in.PeriodEnd
	run, err := s.q.CreateReportRun(ctx, dbgen.CreateReportRunParams{
		PublicID: publicid.New("rpt"), OrganizationID: p.OrganizationID,
		ReportType: in.ReportType, Format: in.Format, Parameters: params,
		PeriodStart: &start, PeriodEnd: &end,
		RequestedBy: p.ActorUserID(), RequestID: ops.Optional(httpx.RequestID(ctx)),
	})
	if err != nil {
		return nil, apierr.Internal(err)
	}

	jobID, err := s.jobs.Enqueue(ctx, JobTypeRun, map[string]any{
		"organizationId": p.OrganizationID, "runId": run.ID,
	}, jobs.EnqueueOptions{
		OrganizationID: &p.OrganizationID, Queue: "reports",
		DedupeKey: "rpt:" + run.PublicID, MaxAttempts: 3,
	})
	if err != nil {
		// Unlike a notification, a report with no job is useless rather than
		// merely unsent: nobody is waiting on a side effect, they are waiting on
		// the file. Fail loudly.
		return nil, apierr.Unavailable("The report could not be queued. Try again shortly.")
	}
	_ = jobID

	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "report.queued", ResourceType: "report_run",
		ResourceID: &run.ID, ResourcePublicID: run.PublicID,
		Metadata: map[string]any{
			"reportType": in.ReportType, "format": in.Format,
			"periodStart": start.Format("2006-01-02"), "periodEnd": end.Format("2006-01-02"),
		},
	}))
	return &run, nil
}

// Generate produces one report. Called by the worker.
//
// The claim is QUEUED -> RUNNING and nothing else: a run already RUNNING is not
// restarted, because two workers streaming into the same object key would both
// write and the loser would leave a truncated file behind a COMPLETED record.
func (s *Service) Generate(ctx context.Context, orgID, runID int64) error {
	run, err := s.q.StartReportRun(ctx, runID)
	if err != nil {
		if ops.IsNoRows(err) {
			// Already running, finished or cancelled. Nothing to do, and not an
			// error — returning one would make the queue retry a report that is
			// either in progress or already delivered.
			return nil
		}
		return err
	}

	rows, size, objectID, genErr := s.stream(ctx, orgID, run)
	if genErr != nil {
		if _, fErr := s.q.FailReportRun(ctx, dbgen.FailReportRunParams{
			OrganizationID: orgID, ID: run.ID,
			ErrorMessage: ptr(truncate(genErr.Error(), 2000)),
		}); fErr != nil {
			s.log.Error("could not record a report failure",
				"runId", run.PublicID, "error", fErr)
		}
		return genErr
	}

	if _, err := s.q.CompleteReportRun(ctx, dbgen.CompleteReportRunParams{
		OrganizationID: orgID, ID: run.ID,
		ObjectID: &objectID, RowCount: rows, ByteSize: size,
		Ttl: pgtype.Interval{Microseconds: downloadTTL.Microseconds(), Valid: true},
	}); err != nil {
		return err
	}
	s.log.Info("report generated",
		"runId", run.PublicID, "type", run.ReportType, "rows", rows, "bytes", size)
	return nil
}

// stream walks the report's rows and uploads them without ever holding more
// than one chunk.
//
// The pipe is what makes that true: the writer goroutine produces chunks while
// the storage client consumes them, so neither side buffers the whole file. A
// bytes.Buffer here would be the exact thing §38 forbids.
func (s *Service) stream(
	ctx context.Context, orgID int64, run dbgen.ReportRun,
) (rowCount, byteSize int64, objectID int64, err error) {
	source, err := s.sourceFor(run)
	if err != nil {
		return 0, 0, 0, err
	}

	pr, pw := io.Pipe()
	counted := &countingWriter{w: pw}

	// The producer. Any failure closes the pipe with the error, which surfaces
	// on the consumer side as a failed upload rather than a truncated file
	// silently accepted as complete.
	go func() {
		writer := csv.NewWriter(counted)
		var writeErr error
		defer func() {
			writer.Flush()
			if writeErr == nil {
				writeErr = writer.Error()
			}
			_ = pw.CloseWithError(writeErr)
		}()

		if writeErr = writer.Write(source.header); writeErr != nil {
			return
		}
		var afterID int64
		for {
			batch, bErr := source.fetch(ctx, orgID, run, afterID)
			if bErr != nil {
				writeErr = bErr
				return
			}
			if len(batch) == 0 {
				return
			}
			for _, record := range batch {
				if writeErr = writer.Write(record.fields); writeErr != nil {
					return
				}
				rowCount++
				afterID = record.id
			}
			// Flush per chunk so the consumer makes progress and the CSV
			// writer's own buffer stays bounded too.
			writer.Flush()
			if writeErr = writer.Error(); writeErr != nil {
				return
			}
			if len(batch) < chunkSize {
				return
			}
		}
	}()

	key := storage.GenerateKey(run.PublicID, "reports", ".csv")
	object, err := s.storage.Put(ctx, key, pr, storage.PutOptions{
		ContentType: "text/csv",
		Metadata: map[string]string{
			"reportType": run.ReportType,
			"runId":      run.PublicID,
		},
	})
	if err != nil {
		_ = pr.CloseWithError(err)
		return 0, 0, 0, fmt.Errorf("upload report: %w", err)
	}

	stored, err := s.q.CreateStoredObject(ctx, dbgen.CreateStoredObjectParams{
		PublicID: publicid.New("obj"), OrganizationID: orgID,
		ObjectKey: object.Key, Bucket: s.storage.Bucket(), Purpose: "EXPORT",
		MimeType: "text/csv", SizeBytes: object.Size,
		ChecksumSha256:   object.Checksum,
		OriginalFilename: ops.Optional(run.ReportType + ".csv"),
		Metadata:         []byte("{}"),
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("record report object: %w", err)
	}
	return rowCount, object.Size, stored.ID, nil
}

// countingWriter tracks bytes written without buffering them.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.n += int64(n)
	return n, err
}

// ---------------------------------------------------------------------------
// Reading
// ---------------------------------------------------------------------------

// Get returns one run.
func (s *Service) Get(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.GetReportRunByPublicIDRow, error) {
	run, err := s.q.GetReportRunByPublicID(ctx, dbgen.GetReportRunByPublicIDParams{
		OrganizationID: p.OrganizationID, PublicID: publicID,
	})
	if err != nil {
		return nil, ops.NotFoundOr(err, "Report run")
	}
	return &run, nil
}

// ListFilter narrows a run listing.
type ListFilter struct {
	ReportType string
	Status     string
	Mine       bool
	CursorID   *int64
	Limit      int32
}

// List returns report runs, newest first.
func (s *Service) List(
	ctx context.Context, p *tenant.Principal, f ListFilter,
) ([]dbgen.ListReportRunsRow, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	params := dbgen.ListReportRunsParams{
		OrganizationID: p.OrganizationID,
		ReportType:     ops.Optional(f.ReportType), Status: ops.Optional(f.Status),
		CursorID: f.CursorID, RowLimit: f.Limit,
	}
	if f.Mine {
		params.RequestedBy = p.ActorUserID()
	}
	rows, err := s.q.ListReportRuns(ctx, params)
	if err != nil {
		return nil, apierr.Internal(err)
	}
	return rows, nil
}

// Download returns a signed URL, or a stream when the backend cannot presign.
//
// The URL is short-lived because the file is customer data: an export link that
// worked for ever would be a credential nobody thinks of as one.
func (s *Service) Download(
	ctx context.Context, p *tenant.Principal, publicID string,
) (signedURL string, body io.ReadCloser, filename string, err error) {
	run, err := s.Get(ctx, p, publicID)
	if err != nil {
		return "", nil, "", err
	}
	switch run.Status {
	case StatusCompleted:
	case StatusExpired:
		return "", nil, "", apierr.Conflict("REPORT_EXPIRED",
			"This report's download window has passed. Run it again.")
	default:
		return "", nil, "", apierr.Conflict("REPORT_NOT_READY",
			"This report is not ready to download.").WithDetail("status", run.Status)
	}
	if run.ObjectKey == nil {
		return "", nil, "", apierr.Internal(fmt.Errorf("report %s has no object", publicID))
	}

	filename = run.ReportType + "-" + run.PublicID + ".csv"

	// A signed URL keeps the bytes off the API entirely. The filesystem store
	// cannot presign, so it falls back to streaming through the request — which
	// is fine for development and never used in production.
	if url, sErr := s.storage.SignedURL(ctx, *run.ObjectKey, 10*time.Minute); sErr == nil && url != "" {
		s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
			Action: "report.downloaded", ResourceType: "report_run",
			ResourceID: &run.ID, ResourcePublicID: run.PublicID,
			Metadata: map[string]any{"via": "signed-url"},
		}))
		return url, nil, filename, nil
	}

	reader, _, gErr := s.storage.Get(ctx, *run.ObjectKey)
	if gErr != nil {
		return "", nil, "", apierr.Internal(gErr)
	}
	s.audit.Record(ctx, audit.FromPrincipal(ctx, audit.Entry{
		Action: "report.downloaded", ResourceType: "report_run",
		ResourceID: &run.ID, ResourcePublicID: run.PublicID,
		Metadata: map[string]any{"via": "stream"},
	}))
	return "", reader, filename, nil
}

// Cancel stops a run that has not finished.
func (s *Service) Cancel(
	ctx context.Context, p *tenant.Principal, publicID string,
) (*dbgen.ReportRun, error) {
	run, err := s.Get(ctx, p, publicID)
	if err != nil {
		return nil, err
	}
	cancelled, err := s.q.CancelReportRun(ctx, dbgen.CancelReportRunParams{
		OrganizationID: p.OrganizationID, ID: run.ID,
	})
	if err != nil {
		if ops.IsNoRows(err) {
			return nil, apierr.Conflict(apierr.CodeConflict,
				"This report has already finished.").WithDetail("status", run.Status)
		}
		return nil, apierr.Internal(err)
	}
	return &cancelled, nil
}

// ---------------------------------------------------------------------------
// Housekeeping
// ---------------------------------------------------------------------------

// ExpireRuns marks lapsed exports EXPIRED and deletes their objects.
//
// Both halves matter. Expiring the record while leaving the file in the bucket
// would leave exported customer data sitting there indefinitely behind a key
// that a signed URL once revealed.
func (s *Service) ExpireRuns(ctx context.Context) (int, error) {
	rows, err := s.q.ExpireReportRuns(ctx)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		if row.ObjectID == nil {
			continue
		}
		key, gErr := s.q.GetStoredObjectKey(ctx, *row.ObjectID)
		if gErr != nil {
			continue
		}
		if dErr := s.storage.Delete(ctx, key); dErr != nil {
			s.log.Warn("could not delete an expired report object",
				"runId", row.PublicID, "error", dErr)
		}
	}
	return len(rows), nil
}

// RegisterHandlers wires report generation into a worker.
func (s *Service) RegisterHandlers(w *jobs.Worker) {
	w.Register(JobTypeRun, func(ctx context.Context, job jobs.Job) error {
		var payload struct {
			OrganizationID int64 `json:"organizationId"`
			RunID          int64 `json:"runId"`
		}
		if err := job.Decode(&payload); err != nil {
			return err
		}
		return s.Generate(ctx, payload.OrganizationID, payload.RunID)
	})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func validType(t string) bool {
	for _, known := range AllTypes {
		if known == t {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func minorString(v int64) string { return strconv.FormatInt(v, 10) }

func timeString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func dateString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func ptr[T any](v T) *T { return &v }

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
