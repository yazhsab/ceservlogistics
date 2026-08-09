-- 0010 Bulk import jobs (M03).
--
-- Constitution §M03: bulk import must run asynchronously, expose job state, row
-- counts, errors, rejected rows and an error file, and must not load the whole
-- dataset into process memory.
--
-- The pipeline is: the API streams the uploaded CSV row by row into
-- import_rows (batched COPY), enqueues a job, and returns 202. The worker then
-- processes import_rows in chunks with a keyset cursor, so peak memory is one
-- chunk regardless of file size. Rejected rows keep their error in place and are
-- streamed back out as CSV on demand — no object storage is required for
-- Release 1.

CREATE TABLE import_jobs (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id       text        NOT NULL UNIQUE CHECK (is_public_id(public_id, 'imp')),
    -- Pincode imports are platform-wide (organization_id NULL, SUPER_ADMIN);
    -- zone-mapping imports are tenant-scoped.
    organization_id bigint      REFERENCES organizations (id) ON DELETE CASCADE,
    import_type     text        NOT NULL CHECK (import_type IN ('PINCODE','ZONE_MAPPING','SERVICE_AREA')),
    status          text        NOT NULL DEFAULT 'PENDING'
                                CHECK (status IN ('PENDING','PROCESSING','COMPLETED','COMPLETED_WITH_ERRORS','FAILED','CANCELLED')),
    file_name       text        NOT NULL,
    file_size_bytes bigint      NOT NULL CHECK (file_size_bytes >= 0),
    file_checksum   text,
    total_rows      integer     NOT NULL DEFAULT 0 CHECK (total_rows >= 0),
    processed_rows  integer     NOT NULL DEFAULT 0 CHECK (processed_rows >= 0),
    success_rows    integer     NOT NULL DEFAULT 0 CHECK (success_rows >= 0),
    failed_rows     integer     NOT NULL DEFAULT 0 CHECK (failed_rows >= 0),
    -- Aggregated error counts by code so the UI can summarise without paging
    -- through every rejected row: {"UNKNOWN_STATE": 12, "BAD_PINCODE": 3}
    error_summary   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    failure_reason  text,
    options         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    job_id          bigint      REFERENCES jobs (id) ON DELETE SET NULL,
    started_at      timestamptz,
    completed_at    timestamptz,
    created_by      bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX import_jobs_org_idx ON import_jobs (organization_id, import_type, created_at DESC);
CREATE INDEX import_jobs_status_idx ON import_jobs (status, created_at DESC);

CREATE TRIGGER import_jobs_set_updated_at BEFORE UPDATE ON import_jobs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE import_rows (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    import_job_id  bigint      NOT NULL REFERENCES import_jobs (id) ON DELETE CASCADE,
    row_number     integer     NOT NULL CHECK (row_number >= 1),
    -- The parsed CSV record, keyed by header name.
    raw            jsonb       NOT NULL,
    status         text        NOT NULL DEFAULT 'PENDING'
                               CHECK (status IN ('PENDING','SUCCEEDED','FAILED','SKIPPED')),
    error_code     text,
    error_message  text,
    processed_at   timestamptz,
    CONSTRAINT import_rows_number_unique UNIQUE (import_job_id, row_number),
    CONSTRAINT import_rows_error_present CHECK (status <> 'FAILED' OR error_code IS NOT NULL)
);

-- The worker claims chunks with `WHERE import_job_id = $1 AND status='PENDING'
-- AND id > $cursor ORDER BY id LIMIT n`, which this index serves directly.
CREATE INDEX import_rows_pending_idx ON import_rows (import_job_id, id) WHERE status = 'PENDING';
CREATE INDEX import_rows_failed_idx ON import_rows (import_job_id, row_number) WHERE status = 'FAILED';
