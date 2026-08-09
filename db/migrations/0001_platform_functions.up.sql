-- 0001 Platform functions and shared domains.
--
-- Conventions used by every later migration:
--   * internal keys are bigint identity columns and are never exposed;
--   * every externally addressable row also carries an opaque public_id;
--   * enum-like columns are text with a CHECK constraint rather than a native
--     enum type, because ALTER TYPE ... ADD VALUE cannot run inside a
--     transaction and cannot remove values, which makes native enums hostile to
--     safe rolling deployments;
--   * money is bigint minor units and always accompanied by a currency;
--   * timestamps are timestamptz.

-- btree_gist lets a GiST exclusion constraint mix plain equality columns with a
-- range column. Migration 0009 uses it to make overlapping weight slabs
-- impossible at the storage layer instead of hoping application code checks.
-- It ships with postgresql-contrib and with the official postgres image; the
-- deployment guide lists it as a prerequisite.
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Maintains updated_at on UPDATE.
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

-- Blocks UPDATE and DELETE on append-only tables (Constitution §13, §34).
-- Operational and audit history must not be rewritable through the application
-- role; corrections are expressed as new rows, never edits.
CREATE OR REPLACE FUNCTION reject_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'table %.% is append-only; % is not permitted',
        TG_TABLE_SCHEMA, TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'P0001';
END;
$$;

-- Increments a row version column so services can detect lost updates.
CREATE OR REPLACE FUNCTION bump_row_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at := now();
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;

-- Guards public identifier shape at the database boundary so a bug in one
-- module cannot persist a malformed or internally-derived identifier.
CREATE OR REPLACE FUNCTION is_public_id(value text, prefix text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT value ~ ('^' || prefix || '_[0-9A-HJKMNP-TV-Z]{26}$');
$$;

-- Generates a public identifier for SEED AND REFERENCE DATA ONLY.
--
-- random() is not a cryptographic source. Business objects always receive their
-- public_id from internal/platform/publicid, which uses crypto/rand and encodes
-- a timestamp for index locality. This function exists so that the fixed
-- permission and system-role catalogue can be seeded in pure SQL, where
-- predictability is irrelevant because the values are public and identical in
-- every deployment's documentation.
CREATE OR REPLACE FUNCTION gen_seed_public_id(prefix text)
RETURNS text
LANGUAGE plpgsql
VOLATILE
AS $$
DECLARE
    alphabet constant text := '0123456789ABCDEFGHJKMNPQRSTVWXYZ';
    result   text := '';
    i        integer;
BEGIN
    FOR i IN 1..26 LOOP
        result := result || substr(alphabet, 1 + floor(random() * 32)::integer, 1);
    END LOOP;
    RETURN prefix || '_' || result;
END;
$$;
