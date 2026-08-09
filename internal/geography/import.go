package geography

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/ceserve/courier-os/internal/audit"
	"github.com/ceserve/courier-os/internal/auth"
	"github.com/ceserve/courier-os/internal/dbgen"
	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/database"
	"github.com/ceserve/courier-os/internal/platform/httpx"
	"github.com/ceserve/courier-os/internal/platform/jobs"
	"github.com/ceserve/courier-os/internal/platform/pagination"
	"github.com/ceserve/courier-os/internal/platform/publicid"
	"github.com/ceserve/courier-os/internal/tenant"
)

// Import limits. The row cap bounds worst-case staging cost; the Indian PIN
// code dataset is roughly 19,300 rows, so 250,000 leaves ample headroom while
// refusing an absurd upload.
const (
	maxImportRows    = 250_000
	stagingBatchSize = 1_000
	processBatchSize = 500
)

// ImportService stages and processes bulk geography uploads.
//
// Constitution §M03 requires asynchronous import that never loads the whole
// dataset into process memory. The upload is streamed row by row into the
// import_rows staging table in batches via COPY; the worker then walks that
// table with a keyset cursor. Peak memory is one batch either way, whether the
// file has 100 rows or 100,000.
type ImportService struct {
	db       *database.DB
	q        *dbgen.Queries
	geo      *Service
	enqueuer *jobs.Enqueuer
	audit    *audit.Recorder
	log      *slog.Logger
	maxBytes int64
}

// NewImportService builds the import service.
func NewImportService(
	db *database.DB, q *dbgen.Queries, geo *Service, enq *jobs.Enqueuer,
	rec *audit.Recorder, log *slog.Logger, maxBytes int64,
) *ImportService {
	return &ImportService{db: db, q: q, geo: geo, enqueuer: enq, audit: rec, log: log, maxBytes: maxBytes}
}

// Routes mounts the import endpoints.
func (s *ImportService) Routes(r chi.Router) {
	r.Route("/imports", func(ir chi.Router) {
		ir.With(auth.RequirePermission("import.read")).Get("/", httpx.Wrap(s.list))
		ir.With(auth.RequirePermission("import.read")).Get("/{importId}", httpx.Wrap(s.get))
		ir.With(auth.RequirePermission("import.read")).Get("/{importId}/errors", httpx.Wrap(s.listErrors))
		ir.With(auth.RequirePermission("import.read")).Get("/{importId}/errors.csv", httpx.Wrap(s.errorsCSV))
		// PIN code imports rewrite shared reference data, so they need the
		// platform-level permission; zone mappings are tenant-scoped.
		ir.With(auth.RequirePermission("pincode.manage")).Post("/pincodes", httpx.Wrap(s.uploadPincodes))
		ir.With(auth.RequirePermission("zone.manage")).Post("/zone-mappings", httpx.Wrap(s.uploadZoneMappings))
	})
}

// Import types.
const (
	TypePincode     = "PINCODE"
	TypeZoneMapping = "ZONE_MAPPING"
)

// expectedHeaders documents the accepted CSV columns per import type. Extra
// columns are ignored; missing required columns fail the upload immediately
// rather than producing 19,000 identical row errors.
//
// A PINCODE file requires pincode and state, and may also carry: district
// (the LGA in Nigeria), city, city_tier, locality, office_name, latitude,
// longitude, is_remote. Of those, locality is the one that decides whether a
// sender can find the code at all — see GET /api/v1/geography/places. A file
// listing several areas against one code repeats the code on each row.
var expectedHeaders = map[string][]string{
	TypePincode:     {"pincode", "state"},
	TypeZoneMapping: {"pincode", "zone_code"},
}

func (s *ImportService) uploadPincodes(w http.ResponseWriter, r *http.Request) error {
	return s.upload(w, r, TypePincode, nil)
}

func (s *ImportService) uploadZoneMappings(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	orgID := p.OrganizationID
	return s.upload(w, r, TypeZoneMapping, &orgID)
}

// upload streams a multipart CSV into the staging table and enqueues the job.
func (s *ImportService) upload(w http.ResponseWriter, r *http.Request, importType string, orgID *int64) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBytes)

	// ReadForm(0) would buffer to disk; instead the part is streamed directly
	// into the CSV reader so nothing larger than one batch is ever resident.
	mr, err := r.MultipartReader()
	if err != nil {
		return apierr.New(http.StatusUnsupportedMediaType, apierr.CodeUnsupportedMedia,
			"The request must be a multipart/form-data upload containing a 'file' part.")
	}
	part, err := mr.NextPart()
	for err == nil && part.FormName() != "file" {
		_ = part.Close()
		part, err = mr.NextPart()
	}
	if err != nil {
		return apierr.Validation("The upload must include a 'file' part containing the CSV.", nil)
	}
	defer func() { _ = part.Close() }()

	fileName := part.FileName()
	if fileName == "" {
		fileName = "upload.csv"
	}
	if !strings.HasSuffix(strings.ToLower(fileName), ".csv") {
		return apierr.Validation("Only .csv files are accepted for bulk import.",
			map[string]any{"fileName": fileName})
	}

	job, err := s.q.CreateImportJob(r.Context(), dbgen.CreateImportJobParams{
		PublicID:       publicid.New(publicid.PrefixImportJob),
		OrganizationID: orgID,
		ImportType:     importType,
		FileName:       fileName,
		FileSizeBytes:  0,
		Options:        []byte("{}"),
		CreatedBy:      &p.UserID,
	})
	if err != nil {
		return apierr.Internal(fmt.Errorf("create import job: %w", err))
	}

	total, size, checksum, err := s.stage(r.Context(), job.ID, importType, part)
	if err != nil {
		// The job row is kept and marked FAILED so the operator can see what
		// happened rather than the upload vanishing silently.
		s.failJob(r.Context(), job.ID, err)
		return err
	}

	if err := s.q.SetImportJobTotals(r.Context(), dbgen.SetImportJobTotalsParams{
		ID: job.ID, TotalRows: int32(total),
	}); err != nil {
		return apierr.Internal(err)
	}
	_ = s.q.FinishImportJobFileMeta(r.Context(), dbgen.FinishImportJobFileMetaParams{
		ID: job.ID, FileSizeBytes: size, FileChecksum: &checksum,
	})

	queueName := jobs.QueueImport
	jobType := jobs.TypePincodeImport
	if importType == TypeZoneMapping {
		jobType = jobs.TypeZoneMappingImport
	}
	if _, err := s.enqueuer.Enqueue(r.Context(), jobType, map[string]any{
		"importJobId": job.ID,
	}, jobs.EnqueueOptions{
		Queue: queueName, OrganizationID: orgID, MaxAttempts: 3, CreatedBy: &p.UserID,
	}); err != nil {
		s.failJob(r.Context(), job.ID, err)
		return apierr.Internal(fmt.Errorf("enqueue import: %w", err))
	}

	s.audit.Record(r.Context(), audit.FromPrincipal(r.Context(), audit.Entry{
		Action: audit.ActionImportStarted, ResourceType: "import_job",
		ResourceID: &job.ID, ResourcePublicID: job.PublicID,
		After: map[string]any{"importType": importType, "fileName": fileName, "totalRows": total},
	}))

	return httpx.Accepted(w, map[string]any{
		"id": job.PublicID, "importType": importType, "status": "PENDING",
		"fileName": fileName, "totalRows": total,
		"statusUrl": "/api/v1/geography/imports/" + job.PublicID,
	})
}

// stage parses the CSV and COPYs batches into import_rows.
func (s *ImportService) stage(ctx context.Context, jobID int64, importType string, src io.Reader) (int, int64, string, error) {
	hasher := sha256.New()
	counter := &countingReader{r: io.TeeReader(src, hasher)}
	reader := csv.NewReader(counter)
	reader.TrimLeadingSpace = true
	reader.ReuseRecord = true
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return 0, 0, "", apierr.Validation("The CSV file is empty or unreadable.", nil)
	}
	index := map[string]int{}
	for i, h := range header {
		index[normaliseHeader(h)] = i
	}
	for _, required := range expectedHeaders[importType] {
		if _, ok := index[required]; !ok {
			return 0, 0, "", apierr.Validation("The CSV is missing a required column.",
				map[string]any{"missingColumn": required, "requiredColumns": expectedHeaders[importType]})
		}
	}

	batch := make([]dbgen.InsertImportRowsParams, 0, stagingBatchSize)
	rowNumber := 0
	for {
		record, rErr := reader.Read()
		if errors.Is(rErr, io.EOF) {
			break
		}
		if rErr != nil {
			return 0, 0, "", apierr.Validation("The CSV could not be parsed.",
				map[string]any{"row": rowNumber + 2, "error": rErr.Error()})
		}
		rowNumber++
		if rowNumber > maxImportRows {
			return 0, 0, "", apierr.Validation("The CSV exceeds the maximum supported row count.",
				map[string]any{"maxRows": maxImportRows})
		}
		payload := map[string]string{}
		for name, i := range index {
			if i < len(record) {
				payload[name] = strings.TrimSpace(record[i])
			}
		}
		raw, mErr := json.Marshal(payload)
		if mErr != nil {
			return 0, 0, "", apierr.Internal(mErr)
		}
		batch = append(batch, dbgen.InsertImportRowsParams{
			ImportJobID: jobID, RowNumber: int32(rowNumber), Raw: raw,
		})
		if len(batch) >= stagingBatchSize {
			if _, cErr := s.q.InsertImportRows(ctx, batch); cErr != nil {
				return 0, 0, "", apierr.Internal(fmt.Errorf("stage import rows: %w", cErr))
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if _, cErr := s.q.InsertImportRows(ctx, batch); cErr != nil {
			return 0, 0, "", apierr.Internal(fmt.Errorf("stage import rows: %w", cErr))
		}
	}
	if rowNumber == 0 {
		return 0, 0, "", apierr.Validation("The CSV contains a header but no data rows.", nil)
	}
	return rowNumber, counter.n, hex.EncodeToString(hasher.Sum(nil)), nil
}

func (s *ImportService) failJob(ctx context.Context, jobID int64, cause error) {
	reason := cause.Error()
	if ae := new(apierr.Error); errors.As(cause, &ae) {
		reason = ae.Message
	}
	if err := s.q.FinishImportJob(context.WithoutCancel(ctx), dbgen.FinishImportJobParams{
		ID: jobID, Status: "FAILED", ErrorSummary: []byte("{}"), FailureReason: &reason,
	}); err != nil {
		s.log.Error("failed to mark import job failed", slog.String("error", err.Error()))
	}
}

// ---- worker handlers -------------------------------------------------------

// RegisterHandlers wires the import job types into the worker.
func (s *ImportService) RegisterHandlers(w *jobs.Worker) {
	w.Register(jobs.TypePincodeImport, s.processJob(TypePincode))
	w.Register(jobs.TypeZoneMappingImport, s.processJob(TypeZoneMapping))
}

type importPayload struct {
	ImportJobID int64 `json:"importJobId"`
}

// processJob returns the worker handler for one import type.
func (s *ImportService) processJob(importType string) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		var payload importPayload
		if err := job.Decode(&payload); err != nil {
			return fmt.Errorf("%w: %v", jobs.ErrPermanent, err)
		}
		return s.Process(ctx, payload.ImportJobID, importType)
	}
}

// Process walks the staged rows in chunks and applies them.
//
// Each chunk is its own transaction, so a failure part-way through leaves
// earlier chunks applied and the cursor recorded — the job is resumable rather
// than all-or-nothing, which matters for a 19,000 row file on a small box.
func (s *ImportService) Process(ctx context.Context, importJobID int64, importType string) error {
	job, err := s.q.GetImportJobByID(ctx, importJobID)
	if err != nil {
		return fmt.Errorf("%w: import job %d not found", jobs.ErrPermanent, importJobID)
	}
	if job.Status == "COMPLETED" || job.Status == "COMPLETED_WITH_ERRORS" {
		return nil // already processed; a retried job must not double-apply
	}
	if err := s.q.StartImportJob(ctx, importJobID); err != nil {
		return fmt.Errorf("start import job: %w", err)
	}

	country, err := s.q.GetCountryByISO2(ctx, DefaultCountry)
	if err != nil {
		return fmt.Errorf("%w: country %s is not configured", jobs.ErrPermanent, DefaultCountry)
	}

	errorCounts := map[string]int{}
	var cursor int64
	var succeeded, failed int

	for {
		rows, rErr := s.q.ClaimImportRowBatch(ctx, dbgen.ClaimImportRowBatchParams{
			ImportJobID: importJobID, CursorID: cursor, RowLimit: processBatchSize,
		})
		if rErr != nil {
			return fmt.Errorf("claim import rows: %w", rErr)
		}
		if len(rows) == 0 {
			break
		}

		okIDs := make([]int64, 0, len(rows))
		type rowFailure struct {
			id      int64
			code    string
			message string
		}
		var failures []rowFailure

		err = s.db.InTx(ctx, func(tx pgx.Tx) error {
			qtx := s.q.WithTx(tx)
			for _, row := range rows {
				cursor = row.ID
				var record map[string]string
				if uErr := json.Unmarshal(row.Raw, &record); uErr != nil {
					failures = append(failures, rowFailure{row.ID, "MALFORMED_ROW", "The staged row could not be decoded."})
					continue
				}
				var applyErr *rowError
				switch importType {
				case TypePincode:
					applyErr = s.applyPincodeRow(ctx, qtx, country.ID, record)
				case TypeZoneMapping:
					applyErr = s.applyZoneMappingRow(ctx, qtx, *job.OrganizationID, country.ID, record)
				default:
					applyErr = &rowError{Code: "UNSUPPORTED_TYPE", Message: "Unsupported import type."}
				}
				if applyErr != nil {
					failures = append(failures, rowFailure{row.ID, applyErr.Code, applyErr.Message})
					continue
				}
				okIDs = append(okIDs, row.ID)
			}
			if len(okIDs) > 0 {
				if _, mErr := qtx.MarkImportRowsSucceeded(ctx, okIDs); mErr != nil {
					return mErr
				}
			}
			for _, f := range failures {
				if mErr := qtx.MarkImportRowFailed(ctx, dbgen.MarkImportRowFailedParams{
					ID: f.id, ErrorCode: &f.code, ErrorMessage: &f.message,
				}); mErr != nil {
					return mErr
				}
			}
			return qtx.RecordImportProgress(ctx, dbgen.RecordImportProgressParams{
				ID:             importJobID,
				ProcessedDelta: int32(len(rows)),
				SuccessDelta:   int32(len(okIDs)),
				FailedDelta:    int32(len(failures)),
			})
		})
		if err != nil {
			return fmt.Errorf("apply import batch: %w", err)
		}
		succeeded += len(okIDs)
		failed += len(failures)
		for _, f := range failures {
			errorCounts[f.code]++
		}
	}

	// Reference data changed underneath every cached lookup, so the caches are
	// dropped explicitly rather than waiting for TTLs.
	switch importType {
	case TypePincode:
		s.geo.cache.DeletePrefix(ctx, s.geo.cache.Key("pincode")+":")
	case TypeZoneMapping:
		if job.OrganizationID != nil {
			s.geo.InvalidateOrganizationZones(ctx, *job.OrganizationID)
		}
	}

	status := "COMPLETED"
	if failed > 0 {
		status = "COMPLETED_WITH_ERRORS"
	}
	summary, _ := json.Marshal(errorCounts)
	if fErr := s.q.FinishImportJob(ctx, dbgen.FinishImportJobParams{
		ID: importJobID, Status: status, ErrorSummary: summary,
	}); fErr != nil {
		return fmt.Errorf("finish import job: %w", fErr)
	}

	orgID := job.OrganizationID
	s.audit.Record(ctx, audit.Entry{
		OrganizationID: orgID, ActorType: audit.ActorSystem,
		Action: audit.ActionImportCompleted, ResourceType: "import_job",
		ResourceID: &importJobID, ResourcePublicID: job.PublicID,
		After: map[string]any{
			"status": status, "successRows": succeeded, "failedRows": failed, "errors": errorCounts,
		},
	})
	s.log.Info("import completed",
		slog.String("import_type", importType),
		slog.String("import_job", job.PublicID),
		slog.Int("succeeded", succeeded), slog.Int("failed", failed))
	return nil
}

// rowError is a per-row validation failure recorded against the staged row.
type rowError struct {
	Code    string
	Message string
}

func (s *ImportService) applyPincodeRow(ctx context.Context, q *dbgen.Queries, countryID int64, rec map[string]string) *rowError {
	code := strings.ToUpper(strings.TrimSpace(rec["pincode"]))
	if !isPincode(code) {
		return &rowError{"INVALID_PINCODE", "pincode must be a 6-digit code not starting with 0."}
	}
	stateName := strings.TrimSpace(rec["state"])
	if stateName == "" {
		return &rowError{"MISSING_STATE", "state is required."}
	}
	state, err := q.GetStateByName(ctx, dbgen.GetStateByNameParams{CountryID: countryID, Lower: stateName})
	if err != nil {
		if database.IsNoRows(err) {
			return &rowError{"UNKNOWN_STATE", fmt.Sprintf("State %q is not configured.", stateName)}
		}
		return &rowError{"STATE_LOOKUP_FAILED", "The state could not be resolved."}
	}

	var cityID *int64
	if cityName := strings.TrimSpace(rec["city"]); cityName != "" {
		city, cErr := q.UpsertCity(ctx, dbgen.UpsertCityParams{
			PublicID: publicid.New(publicid.PrefixCity), StateID: state.ID,
			Name: cityName, Tier: cityTier(rec["city_tier"]),
		})
		if cErr != nil {
			return &rowError{"CITY_UPSERT_FAILED", "The city could not be created."}
		}
		cityID = &city.ID
	}

	var districtID *int64
	if districtName := strings.TrimSpace(rec["district"]); districtName != "" {
		district, dErr := q.UpsertDistrict(ctx, dbgen.UpsertDistrictParams{
			PublicID: publicid.New(publicid.PrefixDistrict), StateID: state.ID,
			Code: strings.ToUpper(strings.ReplaceAll(districtName, " ", "_")), Name: districtName,
		})
		if dErr != nil {
			return &rowError{"DISTRICT_UPSERT_FAILED", "The district could not be created."}
		}
		districtID = &district.ID
	}

	lat, latErr := parseCoordinate(rec["latitude"], -90, 90)
	if latErr != nil {
		return &rowError{"INVALID_LATITUDE", latErr.Error()}
	}
	lng, lngErr := parseCoordinate(rec["longitude"], -180, 180)
	if lngErr != nil {
		return &rowError{"INVALID_LONGITUDE", lngErr.Error()}
	}

	pin, err := q.UpsertPincode(ctx, dbgen.UpsertPincodeParams{
		PublicID: publicid.New(publicid.PrefixPincode), CountryID: countryID, Code: code,
		StateID: state.ID, DistrictID: districtID, CityID: cityID,
		OfficeName: optionalText(rec["office_name"]),
		Latitude:   lat, Longitude: lng, IsRemote: parseBool(rec["is_remote"]),
	})
	if err != nil {
		return &rowError{"PINCODE_UPSERT_FAILED", "The PIN code could not be saved."}
	}

	// The area name, which is what a sender in a market with poor postcode
	// adoption actually searches by. The localities table has always existed;
	// until now nothing could bulk-load it, so the one column that makes a
	// postcode findable was the one column the importer could not fill.
	//
	// A file may list several areas against a code, so a row carries one and
	// repeats the code — the same shape the postal datasets ship in.
	if locality := strings.TrimSpace(rec["locality"]); locality != "" {
		if len(locality) > 120 {
			return &rowError{"INVALID_LOCALITY", "locality must be 120 characters or fewer."}
		}
		if _, err := q.UpsertLocality(ctx, dbgen.UpsertLocalityParams{
			PublicID: publicid.New(publicid.PrefixLocality), PincodeID: pin.ID, Name: locality,
		}); err != nil {
			return &rowError{"LOCALITY_UPSERT_FAILED", "The area name could not be saved."}
		}
	}
	return nil
}

func (s *ImportService) applyZoneMappingRow(ctx context.Context, q *dbgen.Queries, orgID, countryID int64, rec map[string]string) *rowError {
	code := strings.ToUpper(strings.TrimSpace(rec["pincode"]))
	if !isPincode(code) {
		return &rowError{"INVALID_PINCODE", "pincode must be a 6-digit code not starting with 0."}
	}
	// The job's country, not the package default. Both India and Nigeria use
	// six-digit codes with a non-zero lead, so resolving against the wrong
	// country can find a real row for the wrong place rather than failing.
	pincodeID, err := q.GetPincodeIDByCodeInCountry(ctx, dbgen.GetPincodeIDByCodeInCountryParams{
		Code: code, CountryID: countryID,
	})
	if err != nil {
		if database.IsNoRows(err) {
			return &rowError{"UNKNOWN_PINCODE", "This PIN code is not present in the platform dataset."}
		}
		return &rowError{"PINCODE_LOOKUP_FAILED", "The PIN code could not be resolved."}
	}
	zoneCode := strings.ToUpper(strings.TrimSpace(rec["zone_code"]))
	if zoneCode == "" {
		return &rowError{"MISSING_ZONE", "zone_code is required."}
	}
	zone, err := q.GetZoneByCode(ctx, dbgen.GetZoneByCodeParams{OrganizationID: orgID, Code: zoneCode})
	if err != nil {
		if database.IsNoRows(err) {
			return &rowError{"UNKNOWN_ZONE", fmt.Sprintf("Zone %q does not exist.", zoneCode)}
		}
		return &rowError{"ZONE_LOOKUP_FAILED", "The zone could not be resolved."}
	}

	var remoteOverride *bool
	if raw := strings.TrimSpace(rec["is_remote"]); raw != "" {
		v := parseBool(raw)
		remoteOverride = &v
	}
	if _, err := q.UpsertPincodeZoneMapping(ctx, dbgen.UpsertPincodeZoneMappingParams{
		PublicID: publicid.New(publicid.PrefixZoneMapping), OrganizationID: orgID,
		PincodeID: pincodeID, ZoneID: zone.ID, IsRemoteOverride: remoteOverride,
		EffectiveFrom: time.Now(),
	}); err != nil {
		return &rowError{"MAPPING_UPSERT_FAILED", "The zone mapping could not be saved."}
	}
	return nil
}

// ---- read endpoints --------------------------------------------------------

func (s *ImportService) list(w http.ResponseWriter, r *http.Request) error {
	p, err := tenant.Require(r)
	if err != nil {
		return err
	}
	limit, err := pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return err
	}
	page, err := httpx.QueryInt(r, "page", 1, 1, 1000)
	if err != nil {
		return err
	}
	importType, err := httpx.QueryEnum(r, "importType", []string{TypePincode, TypeZoneMapping})
	if err != nil {
		return err
	}
	params := dbgen.ListImportJobsParams{
		RowLimit: int32(limit), RowOffset: int32((page - 1) * limit),
	}
	// PIN code imports are platform-scoped (organization_id NULL); tenants see
	// only their own zone-mapping imports.
	if importType == TypePincode && p.IsSuperAdmin {
		params.OrganizationID = nil
	} else {
		params.OrganizationID = &p.OrganizationID
	}
	if importType != "" {
		params.ImportType = &importType
	}
	rows, err := s.q.ListImportJobs(r.Context(), params)
	if err != nil {
		return apierr.Internal(err)
	}
	var total int64
	items := make([]map[string]any, 0, len(rows))
	for _, j := range rows {
		total = j.TotalCount
		items = append(items, importJobView(listRowToImportJob(j)))
	}
	return httpx.OK(w, pagination.NewOffsetPage(items, page, limit, total, "createdAt", "desc"))
}

func (s *ImportService) get(w http.ResponseWriter, r *http.Request) error {
	job, err := s.loadJob(r)
	if err != nil {
		return err
	}
	return httpx.OK(w, importJobView(job))
}

func (s *ImportService) listErrors(w http.ResponseWriter, r *http.Request) error {
	job, err := s.loadJob(r)
	if err != nil {
		return err
	}
	limit, err := pagination.ClampLimit(httpx.Query(r, "limit"))
	if err != nil {
		return err
	}
	cursorID, err := httpx.QueryInt(r, "cursor", 0, 0, 1<<31-1)
	if err != nil {
		return err
	}
	rows, err := s.q.ListImportRowErrors(r.Context(), dbgen.ListImportRowErrorsParams{
		ImportJobID: job.ID, CursorID: int64(cursorID), RowLimit: int32(limit + 1),
	})
	if err != nil {
		return apierr.Internal(err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]map[string]any, 0, len(rows))
	var lastID int64
	for _, e := range rows {
		lastID = e.ID
		var record map[string]string
		_ = json.Unmarshal(e.Raw, &record)
		items = append(items, map[string]any{
			"rowNumber": e.RowNumber, "errorCode": e.ErrorCode,
			"errorMessage": e.ErrorMessage, "row": record,
		})
	}
	body := map[string]any{"data": items, "pagination": map[string]any{"limit": limit, "hasMore": hasMore}}
	if hasMore {
		body["pagination"].(map[string]any)["nextCursor"] = strconv.FormatInt(lastID, 10)
	}
	return httpx.OK(w, body)
}

// errorsCSV streams the rejected rows back as a CSV the operator can correct
// and re-upload. It is streamed from a keyset cursor, never assembled in RAM.
func (s *ImportService) errorsCSV(w http.ResponseWriter, r *http.Request) error {
	job, err := s.loadJob(r)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", "import-"+job.PublicID+"-errors.csv"))
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"row_number", "error_code", "error_message", "row_data"})

	var cursor int64
	for {
		rows, rErr := s.q.ListImportRowErrors(r.Context(), dbgen.ListImportRowErrorsParams{
			ImportJobID: job.ID, CursorID: cursor, RowLimit: processBatchSize,
		})
		if rErr != nil || len(rows) == 0 {
			return nil
		}
		for _, e := range rows {
			cursor = e.ID
			_ = cw.Write([]string{
				strconv.Itoa(int(e.RowNumber)),
				derefStr(e.ErrorCode),
				derefStr(e.ErrorMessage),
				string(e.Raw),
			})
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			return nil // client disconnected
		}
	}
}

func (s *ImportService) loadJob(r *http.Request) (dbgen.ImportJob, error) {
	p, err := tenant.Require(r)
	if err != nil {
		return dbgen.ImportJob{}, err
	}
	publicID, err := httpx.PathPublicID(r, "importId", publicid.PrefixImportJob, "Import job")
	if err != nil {
		return dbgen.ImportJob{}, err
	}
	// Try the tenant-scoped record first, then the platform-scoped one for
	// super admins. A tenant never sees another tenant's import.
	job, err := s.q.GetImportJobByPublicID(r.Context(), dbgen.GetImportJobByPublicIDParams{
		PublicID: publicID, OrganizationID: &p.OrganizationID,
	})
	if err == nil {
		return job, nil
	}
	if !database.IsNoRows(err) {
		return dbgen.ImportJob{}, apierr.Internal(err)
	}
	if p.IsSuperAdmin {
		job, err = s.q.GetImportJobByPublicID(r.Context(), dbgen.GetImportJobByPublicIDParams{
			PublicID: publicID, OrganizationID: nil,
		})
		if err == nil {
			return job, nil
		}
	}
	return dbgen.ImportJob{}, apierr.NotFound("Import job")
}

func importJobView(j dbgen.ImportJob) map[string]any {
	var summary map[string]int
	_ = json.Unmarshal(j.ErrorSummary, &summary)
	out := map[string]any{
		"id": j.PublicID, "importType": j.ImportType, "status": j.Status,
		"fileName": j.FileName, "fileSizeBytes": j.FileSizeBytes,
		"totalRows": j.TotalRows, "processedRows": j.ProcessedRows,
		"successRows": j.SuccessRows, "failedRows": j.FailedRows,
		"errorSummary": summary, "createdAt": j.CreatedAt,
		"startedAt": j.StartedAt, "completedAt": j.CompletedAt,
	}
	if j.FailureReason != nil {
		out["failureReason"] = *j.FailureReason
	}
	if j.FailedRows > 0 {
		out["errorsUrl"] = "/api/v1/geography/imports/" + j.PublicID + "/errors"
		out["errorFileUrl"] = "/api/v1/geography/imports/" + j.PublicID + "/errors.csv"
	}
	return out
}

// ---- parsing helpers -------------------------------------------------------

func normaliseHeader(h string) string {
	h = strings.TrimSpace(strings.ToLower(h))
	h = strings.TrimPrefix(h, "\ufeff") // strip a UTF-8 BOM on the first column
	return strings.ReplaceAll(h, " ", "_")
}

func isPincode(s string) bool {
	if len(s) != 6 || s[0] == '0' {
		return false
	}
	for i := 0; i < 6; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func parseCoordinate(raw string, min, max float64) (*float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("%q is not a number", raw)
	}
	if v < min || v > max {
		return nil, fmt.Errorf("%v is outside the valid range [%v, %v]", v, min, max)
	}
	return &v, nil
}

func cityTier(raw string) string {
	tier := strings.ToUpper(strings.TrimSpace(raw))
	switch tier {
	case "METRO", "TIER_1", "TIER_2", "TIER_3", "OTHER":
		return tier
	default:
		return "OTHER"
	}
}

func optionalText(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// countingReader tracks how many bytes were consumed so the job row can record
// the uploaded size without buffering the file.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// listRowToImportJob narrows a list row to the shared view model. sqlc flattens
// SELECT i.*, count(*) OVER () into a row type rather than embedding the table
// struct, so the projection is spelled out here once.
func listRowToImportJob(r dbgen.ListImportJobsRow) dbgen.ImportJob {
	return dbgen.ImportJob{
		ID: r.ID, PublicID: r.PublicID, OrganizationID: r.OrganizationID,
		ImportType: r.ImportType, Status: r.Status, FileName: r.FileName,
		FileSizeBytes: r.FileSizeBytes, FileChecksum: r.FileChecksum,
		TotalRows: r.TotalRows, ProcessedRows: r.ProcessedRows,
		SuccessRows: r.SuccessRows, FailedRows: r.FailedRows,
		ErrorSummary: r.ErrorSummary, FailureReason: r.FailureReason,
		Options: r.Options, JobID: r.JobID, StartedAt: r.StartedAt,
		CompletedAt: r.CompletedAt, CreatedBy: r.CreatedBy,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}
